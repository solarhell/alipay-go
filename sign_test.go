package alipay

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// 生成一次 2048 位密钥给整个包的测试复用，省下每个用例几十毫秒。
var (
	testKeyOnce sync.Once
	testKey     *rsa.PrivateKey
)

func key(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	testKeyOnce.Do(func() {
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			panic(err)
		}
		testKey = k
	})
	return testKey
}

// fixedSigner 返回一个 nonce 和时间都固定的 signer，让签名内容可精确断言。
func fixedSigner(t *testing.T, appCertSN string) *signer {
	t.Helper()
	s := newSigner("2021000000000000", key(t), appCertSN)
	s.now = func() time.Time { return time.UnixMilli(1735660800000) }
	s.newNonce = func() (string, error) { return "550e8400-e29b-41d4-a716-446655440000", nil }
	return s
}

// TestBuildSignContent 钉住待签名内容的确切字节。
//
// 这是整个 SDK 最不能出错的地方：多一个换行、少一个换行，或者字段顺序变了，
// 支付宝都会返回验签失败，而错误信息不会告诉你差在哪。
func TestBuildSignContent(t *testing.T) {
	const auth = "app_id=2021000000000000,nonce=550e8400-e29b-41d4-a716-446655440000,timestamp=1735660800000"

	tests := []struct {
		name         string
		method       string
		target       string
		body         []byte
		appAuthToken string
		want         string
	}{
		{
			name:   "POST 带报文体",
			method: "POST",
			target: "/v3/alipay/trade/query",
			body:   []byte(`{"out_trade_no":"T1"}`),
			want:   auth + "\nPOST\n/v3/alipay/trade/query\n" + `{"out_trade_no":"T1"}` + "\n",
		},
		{
			// GET 没有报文体，那一段是空的，但换行必须还在，于是出现一个空行。
			name:   "GET 无报文体",
			method: "GET",
			target: "/v3/alipay/data/dataservice/bill/downloadurl/query?bill_type=trade&bill_date=2026-08-01",
			body:   nil,
			want:   auth + "\nGET\n/v3/alipay/data/dataservice/bill/downloadurl/query?bill_type=trade&bill_date=2026-08-01\n\n",
		},
		{
			name:         "带 app_auth_token 时多一段",
			method:       "POST",
			target:       "/v3/alipay/trade/refund",
			body:         []byte(`{}`),
			appAuthToken: "202108BB",
			want:         auth + "\nPOST\n/v3/alipay/trade/refund\n{}\n202108BB\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildSignContent(auth, tt.method, tt.target, tt.body, tt.appAuthToken)
			if got != tt.want {
				t.Errorf("待签串不符\n得到 %q\n期望 %q", got, tt.want)
			}
			if !strings.HasSuffix(got, "\n") {
				t.Error("待签串必须以换行结尾")
			}
		})
	}
}

func TestAuthString(t *testing.T) {
	now := time.UnixMilli(1735660800000)
	nonce := "550e8400-e29b-41d4-a716-446655440000"

	t.Run("公钥模式不带 app_cert_sn", func(t *testing.T) {
		got := fixedSigner(t, "").authString(nonce, now)
		want := "app_id=2021000000000000,nonce=" + nonce + ",timestamp=1735660800000"
		if got != want {
			t.Errorf("authString = %q, 期望 %q", got, want)
		}
	})

	t.Run("证书模式带 app_cert_sn", func(t *testing.T) {
		got := fixedSigner(t, "d0d0f2e7d0").authString(nonce, now)
		want := "app_id=2021000000000000,app_cert_sn=d0d0f2e7d0,nonce=" + nonce + ",timestamp=1735660800000"
		if got != want {
			t.Errorf("authString = %q, 期望 %q", got, want)
		}
	})
}

// TestAuthorizationRoundTrip 验证 Authorization 头里的 authString 与实际签名
// 用的是同一个串——服务端正是靠头里的 authString 重建待签内容的，两处一旦
// 不一致，签名必然验不过。
func TestAuthorizationRoundTrip(t *testing.T) {
	s := fixedSigner(t, "")
	body := []byte(`{"out_trade_no":"T1"}`)

	got, err := s.authorization("POST", "/v3/alipay/trade/query", body, "")
	if err != nil {
		t.Fatalf("authorization: %v", err)
	}

	prefix := signatureAlgorithm + " "
	if !strings.HasPrefix(got, prefix) {
		t.Fatalf("Authorization 未以 %q 开头: %q", prefix, got)
	}
	rest := strings.TrimPrefix(got, prefix)
	before, after, ok := strings.Cut(rest, ",sign=")
	if !ok {
		t.Fatalf("Authorization 缺少 sign 段: %q", got)
	}
	authString, sign := before, after

	// 拿头里的 authString 重建待签串，用公钥验证——这就是服务端做的事。
	content := buildSignContent(authString, "POST", "/v3/alipay/trade/query", body, "")
	digest := sha256.Sum256([]byte(content))
	raw, err := base64.StdEncoding.DecodeString(sign)
	if err != nil {
		t.Fatalf("sign 不是合法 base64: %v", err)
	}
	if err := rsa.VerifyPKCS1v15(&key(t).PublicKey, crypto.SHA256, digest[:], raw); err != nil {
		t.Errorf("用头里的 authString 重建待签串后验签失败: %v", err)
	}
}

func TestVerifySignature(t *testing.T) {
	k := key(t)
	pub := &k.PublicKey
	const (
		timestamp = "1735660800000"
		nonce     = "550e8400-e29b-41d4-a716-446655440000"
	)
	body := []byte(`{"trade_status":"TRADE_SUCCESS","total_amount":"0.01"}`)

	// 按响应验签规则造一个真签名。
	content := timestamp + "\n" + nonce + "\n" + string(body) + "\n"
	digest := sha256.Sum256([]byte(content))
	raw, err := rsa.SignPKCS1v15(rand.Reader, k, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("造签名: %v", err)
	}
	sig := base64.StdEncoding.EncodeToString(raw)

	t.Run("签名正确时通过", func(t *testing.T) {
		if err := verifySignature(pub, timestamp, nonce, sig, body); err != nil {
			t.Errorf("合法签名被拒: %v", err)
		}
	})

	// 下面每一项都必须被拒，且都归为 ErrSignature——放行任何一项，就等于
	// 采信了一份来历不明的报文，金额和交易状态都可能是伪造的。
	rejects := []struct {
		name                              string
		timestamp, nonce, signature, body string
	}{
		{"报文体被篡改", timestamp, nonce, sig, `{"trade_status":"TRADE_SUCCESS","total_amount":"9999.00"}`},
		{"时间戳被篡改", "1735660800001", nonce, sig, string(body)},
		{"nonce 被篡改", timestamp, "00000000-0000-4000-8000-000000000000", sig, string(body)},
		{"签名被篡改", timestamp, nonce, base64.StdEncoding.EncodeToString(append([]byte{0}, raw[1:]...)), string(body)},
		{"缺时间戳头", "", nonce, sig, string(body)},
		{"缺 nonce 头", timestamp, "", sig, string(body)},
		{"缺签名头", timestamp, nonce, "", string(body)},
		{"签名不是 base64", timestamp, nonce, "!!!not-base64!!!", string(body)},
	}
	for _, tt := range rejects {
		t.Run(tt.name, func(t *testing.T) {
			err := verifySignature(pub, tt.timestamp, tt.nonce, tt.signature, []byte(tt.body))
			if err == nil {
				t.Fatal("验签通过了，应当被拒")
			}
			if !errors.Is(err, ErrSignature) {
				t.Errorf("错误未归为 ErrSignature: %v", err)
			}
		})
	}

	t.Run("换一把公钥必须失败", func(t *testing.T) {
		other, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("生成对照密钥: %v", err)
		}
		if err := verifySignature(&other.PublicKey, timestamp, nonce, sig, body); !errors.Is(err, ErrSignature) {
			t.Errorf("用错误的公钥验签未被拒: %v", err)
		}
	})
}

func TestNewNonce(t *testing.T) {
	shape := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	seen := make(map[string]bool, 256)
	for range 256 {
		n, err := newNonce()
		if err != nil {
			t.Fatalf("newNonce: %v", err)
		}
		if !shape.MatchString(n) {
			t.Fatalf("nonce 不是合法的 UUID v4: %q", n)
		}
		if seen[n] {
			t.Fatalf("nonce 重复: %q", n)
		}
		seen[n] = true
	}
}
