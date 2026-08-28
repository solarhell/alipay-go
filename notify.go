package alipay

import (
	"crypto"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"crypto/rsa"
)

// NotifySuccess 是处理完异步通知后必须回给支付宝的响应体。
//
// 回别的内容（包括 200 但内容不对）都会被判为处理失败，支付宝会在 24 小时内
// 按 4m、10m、10m、1h、2h、6h、15h 的间隔重复通知，直到收到这七个字节。
const NotifySuccess = "success"

// Notification 是一条验签通过的异步通知。
type Notification struct {
	values url.Values
}

// ParseNotification 从异步通知请求中解析并验证通知内容。
//
// 它做两件调用方漏掉就会出事的检查：验签，以及核对 app_id 是不是自己的应用——
// 后者能挡住拿别的应用（比如攻击者自己的沙箱应用）给你的 notify_url 发通知。
//
// 它不核对金额，因为 SDK 不知道这笔订单应该是多少钱。这一步必须由调用方自己做：
// 拿 out_trade_no 找到本地订单，比对 total_amount 一致再发货。
//
// 通知会重复送达，处理逻辑必须幂等——同一个 out_trade_no 只能发一次货。
func (c *Client) ParseNotification(r *http.Request) (*Notification, error) {
	if err := r.ParseForm(); err != nil {
		return nil, fmt.Errorf("alipay: 解析异步通知表单: %w", err)
	}
	return c.VerifyNotification(r.PostForm)
}

// VerifyNotification 验证一组已解析的通知参数，供不便直接交出 *http.Request
// 的场景使用（例如通知先落了队列）。
func (c *Client) VerifyNotification(values url.Values) (*Notification, error) {
	sign := values.Get("sign")
	if sign == "" {
		return nil, fmt.Errorf("%w: 异步通知缺少 sign 参数", ErrSignature)
	}
	if err := verifyNotifySign(c.alipayPubKey, values, sign); err != nil {
		return nil, err
	}

	// 验签只能证明报文来自支付宝，不能证明它是发给你的。
	if appID := values.Get("app_id"); appID != "" && appID != c.signer.appID {
		return nil, fmt.Errorf("alipay: 异步通知的 app_id 是 %s，不是本应用 %s", appID, c.signer.appID)
	}
	return &Notification{values: values}, nil
}

// verifyNotifySign 按 v1 规则验证异步通知的签名。
//
// 规则与 v3 接口那套毫无关系：把除 sign、sign_type 之外的参数按参数名字典序
// 拼成 k=v&k=v 再验。异步通知至今没有 v3 版本，所以这段 v1 逻辑还得留着。
//
// 值取表单解码之后的原文，不重新编码——支付宝签的就是解码后的内容。
func verifyNotifySign(pub *rsa.PublicKey, values url.Values, sign string) error {
	keys := make([]string, 0, len(values))
	for k, v := range values {
		if k == "sign" || k == "sign_type" || len(v) == 0 || v[0] == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte('&')
		}
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(values.Get(k))
	}

	raw, err := base64.StdEncoding.DecodeString(sign)
	if err != nil {
		return fmt.Errorf("%w: 异步通知的 sign 不是合法的 base64", ErrSignature)
	}
	digest := sha256.Sum256([]byte(sb.String()))
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, digest[:], raw); err != nil {
		return fmt.Errorf("%w: 异步通知验签失败，可能被伪造，也可能配置了错误的支付宝公钥", ErrSignature)
	}
	return nil
}

// Get 返回通知中某个参数的值，参数不存在时返回空字符串。
func (n *Notification) Get(key string) string { return n.values.Get(key) }

// Values 返回通知的全部参数。
func (n *Notification) Values() url.Values { return n.values }

// OutTradeNo 是商户订单号，用它在本地找到对应的订单。
func (n *Notification) OutTradeNo() string { return n.values.Get("out_trade_no") }

// TradeNo 是支付宝交易号。
func (n *Notification) TradeNo() string { return n.values.Get("trade_no") }

// TradeStatus 是交易状态。
func (n *Notification) TradeStatus() TradeStatus {
	return TradeStatus(n.values.Get("trade_status"))
}

// TotalAmount 是订单金额，单位元的字符串（如 "0.01"）。
//
// 保持字符串原样而不转成浮点：金额比对必须精确，浮点会引入误差。要计算就用
// decimal 一类的定点数类型。
func (n *Notification) TotalAmount() string { return n.values.Get("total_amount") }

// Paid 报告这条通知是否意味着钱已经到账。
//
// 支付宝会为一笔交易的多个阶段发送通知，只有 TRADE_SUCCESS 和 TRADE_FINISHED
// 表示收到钱；把其他状态也当成功就是无条件发货。
func (n *Notification) Paid() bool { return n.TradeStatus().Paid() }
