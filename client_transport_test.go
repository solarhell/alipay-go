package alipay

import (
	"net/http"
	"testing"
)

// TestDefaultHTTPClientTunedForSingleHost 钉住默认连接池不是 Go 的默认值。
//
// Go 默认 MaxIdleConnsPerHost=2 对"只跟一台网关说话"的支付 SDK 是个坑：并发超过 2
// 就在重建 TLS 连接。这里防止有人把默认 client 改回 &http.Client{Timeout: ...}。
func TestDefaultHTTPClientTunedForSingleHost(t *testing.T) {
	c, err := New("2021000000000000", privPEM(t, key(t)), pubPEM(t, &alipayKey(t).PublicKey))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	tr, ok := c.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("默认 Transport 类型 = %T，期望 *http.Transport", c.httpClient.Transport)
	}
	if tr.MaxIdleConnsPerHost < 50 {
		t.Errorf("MaxIdleConnsPerHost = %d，仍是接近 Go 默认值 2 的水平", tr.MaxIdleConnsPerHost)
	}
	if tr.MaxIdleConns < tr.MaxIdleConnsPerHost {
		t.Errorf("MaxIdleConns(%d) < MaxIdleConnsPerHost(%d)，后者会被前者截断", tr.MaxIdleConns, tr.MaxIdleConnsPerHost)
	}
	if !tr.ForceAttemptHTTP2 {
		t.Error("自定义 Transport 未显式开启 HTTP/2，会静默降级到 HTTP/1.1")
	}
	if tr.TLSHandshakeTimeout == 0 || tr.IdleConnTimeout == 0 {
		t.Error("TLS 握手超时或空闲连接超时未设置")
	}
	if c.httpClient.Timeout != defaultTimeout {
		t.Errorf("Client.Timeout = %v，期望 %v", c.httpClient.Timeout, defaultTimeout)
	}
}

// TestWithHTTPClientOverridesDefault 确认调用方仍能完全替换 client。
func TestWithHTTPClientOverridesDefault(t *testing.T) {
	custom := &http.Client{}
	c, err := New("2021000000000000", privPEM(t, key(t)), pubPEM(t, &alipayKey(t).PublicKey), WithHTTPClient(custom))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.httpClient != custom {
		t.Error("WithHTTPClient 没有替换掉默认 client")
	}
}
