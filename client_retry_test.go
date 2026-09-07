package alipay

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"
)

// retryServer 前 failTimes 次按 failStatus/failBody 回错误，之后回签名成功。
// 记录每次收到的 Authorization 头，供断言"每次都重签"。
type retryServer struct {
	mu         sync.Mutex
	hits       int
	auths      []string
	failTimes  int
	failStatus int
	failBody   string
}

func (rs *retryServer) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rs.mu.Lock()
		rs.hits++
		n := rs.hits
		rs.auths = append(rs.auths, r.Header.Get("Authorization"))
		rs.mu.Unlock()
		if n <= rs.failTimes {
			w.WriteHeader(rs.failStatus)
			io.WriteString(w, rs.failBody)
			return
		}
		signResponse(t, w, alipayKey(t), `{"ok":true}`)
		io.WriteString(w, `{"ok":true}`)
	}
}

func (rs *retryServer) count() int { rs.mu.Lock(); defer rs.mu.Unlock(); return rs.hits }

func noBackoff(*Client) {}

func newRetryClient(t *testing.T, rs *retryServer, opts ...Option) *Client {
	t.Helper()
	c := newTestClient(t, rs.handler(t))
	for _, o := range opts {
		o(c)
	}
	c.retryBackoff = func(int) time.Duration { return 0 }
	return c
}

// TestRetryOnRateLimitThenSucceed 限流两次后成功：共 3 次请求，且每次 Authorization 不同。
//
// 每次重签是这里最容易踩的坑：nonce 一次性，复用等于重放。
func TestRetryOnRateLimitThenSucceed(t *testing.T) {
	rs := &retryServer{failTimes: 2, failStatus: 400, failBody: `{"code":"app-call-limited","message":"调用次数超限"}`}
	c := newRetryClient(t, rs)

	if err := c.do(context.Background(), http.MethodPost, "/v3/alipay/trade/query", nil, map[string]string{"a": "b"}, nil); err != nil {
		t.Fatalf("两次限流后应成功，得到 %v", err)
	}
	if got := rs.count(); got != 3 {
		t.Fatalf("请求次数 = %d，期望 3（1 次 + 2 次重试）", got)
	}
	seen := map[string]bool{}
	for _, a := range rs.auths {
		if seen[a] {
			t.Fatal("有两次请求复用了同一个 Authorization 头——nonce 被重放")
		}
		seen[a] = true
	}
}

// TestRetryGivesUpAfterMax 一直限流：1 + maxRetries 次后把限流错误原样交回。
func TestRetryGivesUpAfterMax(t *testing.T) {
	rs := &retryServer{failTimes: 100, failStatus: 429, failBody: `{"code":"method-call-limited","message":"x"}`}
	c := newRetryClient(t, rs)

	err := c.do(context.Background(), http.MethodPost, "/v3/alipay/trade/query", nil, nil, nil)
	if !IsCode(err, CodeMethodCallLimited) {
		t.Fatalf("期望 method-call-limited，得到 %v", err)
	}
	if got := rs.count(); got != 1+defaultMaxRetries {
		t.Fatalf("请求次数 = %d，期望 %d", got, 1+defaultMaxRetries)
	}
}

// TestNoRetryOnIndeterminate 结果未知绝不重试：系统异常可能已经执行了。
func TestNoRetryOnIndeterminate(t *testing.T) {
	rs := &retryServer{failTimes: 100, failStatus: 500, failBody: `{"code":"ACQ.SYSTEM_ERROR","message":"系统异常"}`}
	c := newRetryClient(t, rs)

	err := c.do(context.Background(), http.MethodPost, "/v3/alipay/trade/precreate", nil, map[string]string{"a": "b"}, nil)
	if !Indeterminate(err) {
		t.Fatalf("期望结果未知，得到 %v", err)
	}
	if got := rs.count(); got != 1 {
		t.Fatalf("结果未知却重试了：请求次数 = %d", got)
	}
}

// TestNoRetryOnTransportError 传输错误绝不重试：超时那一刻请求可能已到达。
func TestNoRetryOnTransportError(t *testing.T) {
	rs := &retryServer{}
	c := newRetryClient(t, rs)
	// 让请求根本发不出去
	c.baseURL = "http://127.0.0.1:1"

	err := c.do(context.Background(), http.MethodPost, "/v3/alipay/trade/precreate", nil, nil, nil)
	var te *TransportError
	if !errors.As(err, &te) {
		t.Fatalf("期望 TransportError，得到 %T", err)
	}
	if got := rs.count(); got != 0 {
		t.Fatalf("传输错误却重试到了服务端：%d 次", got)
	}
}

// TestRetryRespectsContext 退避期间 ctx 取消要立刻返回，不再发下一次。
func TestRetryRespectsContext(t *testing.T) {
	rs := &retryServer{failTimes: 100, failStatus: 400, failBody: `{"code":"app-call-limited","message":"x"}`}
	c := newRetryClient(t, rs)
	c.retryBackoff = func(int) time.Duration { return 5 * time.Second }

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := c.do(ctx, http.MethodPost, "/v3/alipay/trade/query", nil, nil, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("期望 ctx 超时错误，得到 %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("ctx 取消后没有立刻返回，等完了整个退避")
	}
	if got := rs.count(); got != 1 {
		t.Fatalf("ctx 取消后仍发出了请求：%d 次", got)
	}
}

// TestWithMaxRetriesZeroDisables 关掉重试：限流直接交回。
func TestWithMaxRetriesZeroDisables(t *testing.T) {
	rs := &retryServer{failTimes: 100, failStatus: 400, failBody: `{"code":"app-call-limited","message":"x"}`}
	c := newRetryClient(t, rs, WithMaxRetries(0))

	if err := c.do(context.Background(), http.MethodPost, "/v3/alipay/trade/query", nil, nil, nil); !IsCode(err, CodeAppCallLimited) {
		t.Fatalf("期望 app-call-limited，得到 %v", err)
	}
	if got := rs.count(); got != 1 {
		t.Fatalf("WithMaxRetries(0) 仍重试了：%d 次", got)
	}
}

// TestDefaultRetryBackoffBounds 退避在 [200ms, 3s] 内且封顶。
func TestDefaultRetryBackoffBounds(t *testing.T) {
	for attempt := 0; attempt < 10; attempt++ {
		d := defaultRetryBackoff(attempt)
		if d < retryBaseDelay || d > retryMaxDelay+retryMaxDelay/2 {
			t.Errorf("attempt %d 退避 %v 越界", attempt, d)
		}
	}
}
