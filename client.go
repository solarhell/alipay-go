package alipay

import (
	"bytes"
	"context"
	"crypto/rsa"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// 用 encoding/json/v2 而不是 v1：v2 默认拒绝重复的 JSON 键、字段名大小写敏感。
// 验签验的是原始字节、解析的是同一份报文，键重复时两者对"这份报文说了什么"
// 的理解就可能分岔——签名过了，读到的却是另一个值。v1 的宽松匹配在别处无伤
// 大雅，在金额和交易状态上不行。

const (
	// ProductionURL 是生产环境网关。
	ProductionURL = "https://openapi.alipay.com"
	// SandboxURL 是沙箱环境网关。
	SandboxURL = "https://openapi-sandbox.dl.alipaydev.com"

	defaultTimeout = 30 * time.Second

	// defaultMaxRetries 是对"网关就地拒绝、没有副作用"的错误（限流）自动重试的默认次数。
	// 限流退避一两次就过；再多只是拖长调用方的等待。
	defaultMaxRetries = 2
	// maxRetriesCap 是 WithMaxRetries 能设到的上限。限流退避封顶 2s，10 次已合计约 20s
	// 的额外等待——再多不是"更健壮"，是把一次明确的拒绝硬拖成一分钟的挂起。
	maxRetriesCap = 10
	// retryBaseDelay / retryMaxDelay 约束退避区间：基础 200ms 逐次翻倍、加抖动、封顶 2s。
	// 调用方多半在等一个 HTTP 响应，总额外延迟控制在秒级。
	retryBaseDelay = 200 * time.Millisecond
	retryMaxDelay  = 2 * time.Second

	// maxResponseBytes 限制读入内存的响应体大小。正常业务响应不到 10KB，
	// 这个上限只为在网关异常时兜底，避免把内存读爆。
	maxResponseBytes = 10 << 20
)

// Client 调用支付宝开放平台 v3 接口。
//
// Client 可以被多个 goroutine 同时使用。
type Client struct {
	signer       *signer
	alipayPubKey *rsa.PublicKey
	baseURL      string
	appAuthToken string
	httpClient   *http.Client

	maxRetries   int
	retryBackoff func(attempt int) time.Duration // 测试注入零退避
}

// Option 定制 Client 的行为。
type Option func(*Client)

// WithSandbox 把请求指向沙箱环境。
func WithSandbox() Option {
	return func(c *Client) { c.baseURL = SandboxURL }
}

// WithBaseURL 覆盖网关地址，用于自建的测试网关。
func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = strings.TrimSuffix(u, "/") }
}

// WithHTTPClient 替换底层的 http.Client，可用于定制超时、代理或连接池。
//
// 不传时用 defaultHTTPClient：它针对"一个进程只跟支付宝一台网关说话"这个形态
// 调过连接池，见该函数说明。自己传 client 时注意 Go 默认 Transport 的
// MaxIdleConnsPerHost 只有 2，并发一高每个请求都在重建 TLS 连接。
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.httpClient = h }
}

// defaultHTTPClient 构造默认的 http.Client。
//
// Go 的 http.DefaultTransport 把 MaxIdleConnsPerHost 定成 2——那是为"一个进程跟很多
// 主机通信"设计的。支付 SDK 恰恰相反：整个进程只跟支付宝一台网关说话，2 条空闲连接
// 意味着并发一旦超过 2，多出来的请求全在重建 TCP+TLS，延迟翻倍、TIME_WAIT 堆积。
// 2026-09-07 对生产网关压测时用默认值跑不到 40 rps 以上，换成本配置后 640 rps
// 平均延迟 301ms、峰值并发 329 连接、零错误。
//
// 各项取值：
//   - MaxIdleConnsPerHost 100：与 MaxIdleConns 相同，因为只有一台主机；空闲连接
//     多于并发峰值就没有意义，100 已经覆盖了绝大多数支付业务的并发
//   - IdleConnTimeout 90s：与 Go 默认一致，支付宝网关不会更早断开
//   - 拨号/TLS 握手各 10s：网关可达性问题应尽快暴露，不该吃满整个请求超时
//   - ForceAttemptHTTP2：显式声明，避免自定义 Transport 时被静默降级到 HTTP/1.1
//   - Client.Timeout 30s：整个请求（含读响应体）的上限，见 defaultTimeout
func defaultHTTPClient() *http.Client {
	return &http.Client{
		Timeout: defaultTimeout,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
}

// WithMaxRetries 设置自动重试次数，0 关闭。默认 2，上限 10（超过按 10 计）。
//
// 只重试 Retryable() 为真的错误——网关就地拒绝、没有副作用的那一类（限流、429）。
// 结果未知的错误（Indeterminate）和传输错误绝不会被重试：请求可能已经执行，
// 重试就是重复下单或重复退款。
func WithMaxRetries(n int) Option {
	return func(c *Client) { c.maxRetries = min(max(n, 0), maxRetriesCap) }
}

// WithAppCertSN 启用证书模式，传入应用公钥证书的 SN。
//
// 公钥模式（默认）不需要它。两种模式在开放平台上二选一，用错会让所有请求验签失败。
func WithAppCertSN(sn string) Option {
	return func(c *Client) { c.signer.appCertSN = sn }
}

// WithAppAuthToken 以服务商身份代商家调用接口。
func WithAppAuthToken(token string) Option {
	return func(c *Client) { c.appAuthToken = token }
}

// New 用应用私钥和支付宝公钥创建客户端。
//
// privateKey 是你的应用私钥，alipayPublicKey 是支付宝给你的公钥——注意不是你
// 自己上传的应用公钥，填错会让所有响应验签失败。两者的格式都很宽松，见
// ParsePrivateKey 与 ParsePublicKey。
func New(appID, privateKey, alipayPublicKey string, opts ...Option) (*Client, error) {
	if appID == "" {
		return nil, errors.New("alipay: appID 为空")
	}
	priv, err := ParsePrivateKey(privateKey)
	if err != nil {
		return nil, err
	}
	pub, err := ParsePublicKey(alipayPublicKey)
	if err != nil {
		return nil, err
	}

	c := &Client{
		signer:       newSigner(appID, priv, ""),
		alipayPubKey: pub,
		baseURL:      ProductionURL,
		httpClient:   defaultHTTPClient(),
		maxRetries:   defaultMaxRetries,
		retryBackoff: defaultRetryBackoff,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// do 执行一次已签名的请求，校验响应签名，并把响应体解到 out。
//
// out 为 nil 时丢弃响应体。业务错误以 *Error 返回，拿不到完整响应的情况以
// *TransportError 返回——后者一律被视为结果未知，理由见 TransportError。
func (c *Client) do(ctx context.Context, method, path string, query url.Values, in, out any) error {
	var body []byte
	if in != nil {
		var err error
		if body, err = json.Marshal(in); err != nil {
			return fmt.Errorf("alipay: %s: 序列化请求: %w", path, err)
		}
	}

	// 签名用的 target 必须和实际请求的 path+query 逐字节一致，所以两处共用
	// 同一个字符串，而不是各自拼一遍。
	target := path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}

	// 重试环。每次尝试都重新签名——nonce 和 timestamp 是一次性的，复用 Authorization
	// 头等于重放，网关可能直接拒绝。只对 Retryable() 为真的错误重试；退避期间
	// 尊重 ctx，调用方的超时或取消立刻生效。
	for attempt := 0; ; attempt++ {
		err := c.attempt(ctx, method, path, target, body, out)
		if err == nil {
			return nil
		}
		if attempt >= c.maxRetries || !Retryable(err) {
			return err
		}
		if err := sleepCtx(ctx, c.retryBackoff(attempt)); err != nil {
			return fmt.Errorf("alipay: %s: 重试等待被取消: %w", path, err)
		}
	}
}

// attempt 发一次已签名的请求并解析响应。
func (c *Client) attempt(ctx context.Context, method, path, target string, body []byte, out any) error {
	auth, err := c.signer.authorization(method, target, body, c.appAuthToken)
	if err != nil {
		return err
	}

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+target, reader)
	if err != nil {
		return fmt.Errorf("alipay: %s: 构造请求: %w", path, err)
	}
	req.Header.Set("Authorization", auth)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.appAuthToken != "" {
		req.Header.Set("alipay-app-auth-token", c.appAuthToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return &TransportError{Op: path, Err: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		// 响应读到一半断了，同样不知道支付宝那边执行到什么程度。
		return &TransportError{Op: path, Err: fmt.Errorf("读取响应: %w", err)}
	}

	return c.handleResponse(path, resp, respBody, out)
}

// defaultRetryBackoff 是第 attempt 次重试前的等待：200ms × 2^attempt，加最多 50% 的
// 随机抖动，封顶 2s。抖动是为了让一批同时被限流的调用方错开再来，而不是同一毫秒
// 再撞一次。
func defaultRetryBackoff(attempt int) time.Duration {
	// 200ms << 4 已经超过封顶值，指数到此为止：再往上移位会溢出成负数，
	// rand.Int64N 收到负数直接 panic。WithMaxRetries 传个大数就能踩到。
	d := retryBaseDelay << min(max(attempt, 0), 4)
	if d > retryMaxDelay {
		d = retryMaxDelay
	}
	return d + time.Duration(rand.Int64N(int64(d)/2+1))
}

// sleepCtx 等待 d，ctx 先结束则立刻返回其错误。
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (c *Client) handleResponse(path string, resp *http.Response, body []byte, out any) error {
	traceID := resp.Header.Get("alipay-trace-id")
	signature := resp.Header.Get("alipay-signature")
	success := resp.StatusCode >= 200 && resp.StatusCode < 300

	// 验签规则：带了签名就必须验过；成功响应没带签名一律拒绝——它承载着金额
	// 和交易状态，无法确认来源就绝不能采信。
	//
	// 错误响应没带签名时不在这里拦下，而是继续解析成 *Error 交回调用方：把网关
	// 层面的 4xx/5xx（它们本就没有签名）统统变成"验签失败"，只会掩盖真正的原因。
	// 伪造错误响应能造成的最坏情况是让调用方以为失败，而这条路已经由
	// Indeterminate 兜住了。
	if signature != "" {
		if err := verifySignature(c.alipayPubKey,
			resp.Header.Get("alipay-timestamp"), resp.Header.Get("alipay-nonce"),
			signature, body); err != nil {
			return fmt.Errorf("alipay: %s: %w", path, err)
		}
	} else if success {
		return fmt.Errorf("alipay: %s: %w: 成功响应未带 alipay-signature 头", path, ErrSignature)
	}

	if !success {
		return parseError(resp.StatusCode, traceID, body)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("alipay: %s: 解析响应: %w", path, err)
	}
	return nil
}

// parseError 把错误响应解析成 *Error。
func parseError(status int, traceID string, body []byte) error {
	e := &Error{StatusCode: status, RequestID: traceID}

	var payload struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Links   string `json:"links"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.Code == "" {
		// 网关层面的错误（限流、鉴权失败、502 等）未必是这个格式，此时保留
		// 原始报文，至少让调用方能看清发生了什么。
		e.Code = Code(http.StatusText(status))
		e.Message = strings.TrimSpace(string(body))
		if e.Message == "" {
			e.Message = fmt.Sprintf("HTTP %d，响应体为空", status)
		}
		return e
	}

	e.Code = Code(payload.Code)
	e.Message = payload.Message
	e.Links = payload.Links
	return e
}
