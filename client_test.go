package alipay

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// 支付宝那一侧的密钥，与应用私钥分开——混用两把密钥是真实存在的配置错误，
// 测试里就该用两把，才能把它测出来。
var (
	alipayKeyOnce sync.Once
	alipayKeyVal  *rsa.PrivateKey
)

func alipayKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	alipayKeyOnce.Do(func() {
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			panic(err)
		}
		alipayKeyVal = k
	})
	return alipayKeyVal
}

func privPEM(t *testing.T, k *rsa.PrivateKey) string {
	t.Helper()
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)}))
}

func pubPEM(t *testing.T, k *rsa.PublicKey) string {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(k)
	if err != nil {
		t.Fatalf("编码公钥: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

// signResponse 按响应验签规则给报文体签名，模拟支付宝网关的行为。
func signResponse(t *testing.T, w http.ResponseWriter, k *rsa.PrivateKey, body string) {
	t.Helper()
	const (
		timestamp = "1735660800000"
		nonce     = "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
	)
	digest := sha256.Sum256([]byte(timestamp + "\n" + nonce + "\n" + body + "\n"))
	sig, err := rsa.SignPKCS1v15(rand.Reader, k, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("签响应: %v", err)
	}
	w.Header().Set("alipay-timestamp", timestamp)
	w.Header().Set("alipay-nonce", nonce)
	w.Header().Set("alipay-signature", base64.StdEncoding.EncodeToString(sig))
	w.Header().Set("alipay-trace-id", "0b8f1234")
}

// newTestClient 起一个假网关，返回指向它的 Client。
func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	c, err := New("2021000000000000", privPEM(t, key(t)), pubPEM(t, &alipayKey(t).PublicKey), WithBaseURL(srv.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

// TestRequestIsVerifiable 站在支付宝的位置验证我们发出的请求签名。
//
// 这是签名链路的端到端检查：拿 Authorization 头里的 authString 重建待签串，
// 用应用公钥验证——服务端做的正是这件事。
func TestRequestIsVerifiable(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		query  url.Values
		in     any
	}{
		{"POST 带报文体", http.MethodPost, "/v3/alipay/trade/query", nil, map[string]string{"out_trade_no": "T1"}},
		{"GET 带 query", http.MethodGet, "/v3/alipay/data/dataservice/bill/downloadurl/query",
			url.Values{"bill_type": {"trade"}, "bill_date": {"2026-08-01"}}, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)

				auth := r.Header.Get("Authorization")
				rest, ok := strings.CutPrefix(auth, signatureAlgorithm+" ")
				if !ok {
					t.Errorf("Authorization 前缀不对: %q", auth)
					return
				}
				authString, sign, ok := strings.Cut(rest, ",sign=")
				if !ok {
					t.Errorf("Authorization 缺 sign 段: %q", auth)
					return
				}

				// 服务端看到的 path+query 必须与签名时用的 target 一致。
				target := r.URL.Path
				if r.URL.RawQuery != "" {
					target += "?" + r.URL.RawQuery
				}
				digest := sha256.Sum256([]byte(buildSignContent(authString, r.Method, target, body, "")))
				raw, err := base64.StdEncoding.DecodeString(sign)
				if err != nil {
					t.Errorf("sign 不是 base64: %v", err)
					return
				}
				if err := rsa.VerifyPKCS1v15(&key(t).PublicKey, crypto.SHA256, digest[:], raw); err != nil {
					t.Errorf("请求验签失败: %v", err)
				}

				signResponse(t, w, alipayKey(t), `{"trade_no":"2026"}`)
				io.WriteString(w, `{"trade_no":"2026"}`)
			})

			var out struct {
				TradeNo string `json:"trade_no"`
			}
			if err := c.do(context.Background(), tt.method, tt.path, tt.query, tt.in, &out); err != nil {
				t.Fatalf("do: %v", err)
			}
			if out.TradeNo != "2026" {
				t.Errorf("响应未正确解析: %+v", out)
			}
		})
	}
}

func TestResponseVerification(t *testing.T) {
	t.Run("篡改响应体必须被拒", func(t *testing.T) {
		c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			// 按小额签名，却发出大额报文——中间人改金额正是这个形状。
			signResponse(t, w, alipayKey(t), `{"total_amount":"0.01"}`)
			io.WriteString(w, `{"total_amount":"9999.00"}`)
		})
		err := c.do(context.Background(), http.MethodPost, "/v3/alipay/trade/query", nil, nil, nil)
		if !errors.Is(err, ErrSignature) {
			t.Fatalf("期望 ErrSignature，得到 %v", err)
		}
	})

	t.Run("成功响应没有签名头必须被拒", func(t *testing.T) {
		c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, `{"trade_status":"TRADE_SUCCESS"}`)
		})
		err := c.do(context.Background(), http.MethodPost, "/v3/alipay/trade/query", nil, nil, nil)
		if !errors.Is(err, ErrSignature) {
			t.Fatalf("期望 ErrSignature，得到 %v", err)
		}
	})

	t.Run("用错支付宝公钥必须被拒", func(t *testing.T) {
		// 服务端用另一把密钥签名，等价于把「应用公钥」误填成「支付宝公钥」。
		other, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("生成对照密钥: %v", err)
		}
		c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			signResponse(t, w, other, `{"ok":true}`)
			io.WriteString(w, `{"ok":true}`)
		})
		if err := c.do(context.Background(), http.MethodPost, "/v3/alipay/trade/query", nil, nil, nil); !errors.Is(err, ErrSignature) {
			t.Fatalf("期望 ErrSignature，得到 %v", err)
		}
	})
}

func TestErrorResponse(t *testing.T) {
	t.Run("标准业务错误", func(t *testing.T) {
		const body = `{"code":"ACQ.TRADE_NOT_EXIST","message":"交易不存在","links":"https://opendocs.alipay.com/x"}`
		c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			signResponse(t, w, alipayKey(t), body)
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, body)
		})

		err := c.do(context.Background(), http.MethodPost, "/v3/alipay/trade/query", nil, nil, nil)
		var e *Error
		if !errors.As(err, &e) {
			t.Fatalf("期望 *Error，得到 %T: %v", err, err)
		}
		if e.Code != CodeACQTradeNotExist {
			t.Errorf("Code = %q", e.Code)
		}
		if e.Message != "交易不存在" {
			t.Errorf("Message = %q", e.Message)
		}
		if e.Links != "https://opendocs.alipay.com/x" {
			t.Errorf("Links = %q", e.Links)
		}
		if e.StatusCode != http.StatusNotFound {
			t.Errorf("StatusCode = %d", e.StatusCode)
		}
		if e.RequestID != "0b8f1234" {
			t.Errorf("RequestID = %q", e.RequestID)
		}
		if e.Indeterminate() || e.Retryable() {
			t.Error("交易不存在被误判为结果未知或可重试")
		}
	})

	t.Run("系统异常是结果未知", func(t *testing.T) {
		const body = `{"code":"ACQ.SYSTEM_ERROR","message":"系统异常"}`
		c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			signResponse(t, w, alipayKey(t), body)
			w.WriteHeader(http.StatusInternalServerError)
			io.WriteString(w, body)
		})

		err := c.do(context.Background(), http.MethodPost, "/v3/alipay/trade/precreate", nil, nil, nil)
		if !Indeterminate(err) {
			t.Errorf("ACQ.SYSTEM_ERROR 未被判为结果未知: %v", err)
		}
		if Retryable(err) {
			t.Error("结果未知的请求被判为可原样重试")
		}
	})

	t.Run("网关错误不是标准格式时保留原文", func(t *testing.T) {
		// 网关层的 4xx/5xx 本来就没有签名，也不是业务错误格式。这类响应不该被
		// 报成"验签失败"，那会掩盖真正的原因。
		c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			io.WriteString(w, "<html>502 Bad Gateway</html>")
		})

		err := c.do(context.Background(), http.MethodPost, "/v3/alipay/trade/query", nil, nil, nil)
		if errors.Is(err, ErrSignature) {
			t.Fatalf("网关错误被误报成验签失败: %v", err)
		}
		var e *Error
		if !errors.As(err, &e) {
			t.Fatalf("期望 *Error，得到 %T: %v", err, err)
		}
		if !strings.Contains(e.Message, "502 Bad Gateway") {
			t.Errorf("原始报文丢失: %q", e.Message)
		}
		if !e.Indeterminate() {
			t.Error("5xx 未被判为结果未知")
		}
	})
}

func TestTransportFailureIsIndeterminate(t *testing.T) {
	// 连接直接断掉，等价于请求发出后杳无音信——不知道支付宝执行了没有。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	c, err := New("2021000000000000", privPEM(t, key(t)), pubPEM(t, &alipayKey(t).PublicKey), WithBaseURL(srv.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	srv.Close()

	err = c.do(context.Background(), http.MethodPost, "/v3/alipay/trade/precreate", nil, map[string]string{"a": "b"}, nil)
	if _, ok := errors.AsType[*TransportError](err); !ok {
		t.Fatalf("期望 *TransportError，得到 %T: %v", err, err)
	}
	if !Indeterminate(err) {
		t.Error("传输失败未被判为结果未知——下单超时当失败重发就是重复扣款")
	}
	if Retryable(err) {
		t.Error("传输失败被判为可原样重试")
	}
}

func TestNewRejectsBadConfig(t *testing.T) {
	good := privPEM(t, key(t))
	goodPub := pubPEM(t, &alipayKey(t).PublicKey)

	for name, fn := range map[string]func() error{
		"appID 为空":  func() error { _, err := New("", good, goodPub); return err },
		"私钥无法解析":    func() error { _, err := New("2021", "garbage", goodPub); return err },
		"公钥无法解析":    func() error { _, err := New("2021", good, "garbage"); return err },
		"把私钥填进公钥字段": func() error { _, err := New("2021", good, good); return err },
	} {
		t.Run(name, func(t *testing.T) {
			if err := fn(); err == nil {
				t.Fatal("应当被拒绝，却创建成功了")
			}
		})
	}
}
