package alipay

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
)

// signNotify 按 v1 规则给通知参数签名，模拟支付宝发出的异步通知。
func signNotify(t *testing.T, k *rsa.PrivateKey, values url.Values) url.Values {
	t.Helper()
	keys := make([]string, 0, len(values))
	for key, v := range values {
		if key == "sign" || key == "sign_type" || len(v) == 0 || v[0] == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var parts []string
	for _, key := range keys {
		parts = append(parts, key+"="+values.Get(key))
	}
	digest := sha256.Sum256([]byte(strings.Join(parts, "&")))
	raw, err := rsa.SignPKCS1v15(rand.Reader, k, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("签通知: %v", err)
	}

	out := url.Values{}
	maps.Copy(out, values)
	out.Set("sign", base64.StdEncoding.EncodeToString(raw))
	out.Set("sign_type", "RSA2")
	return out
}

func notifyClient(t *testing.T) *Client {
	t.Helper()
	c, err := New("2021000000000000", privPEM(t, key(t)), pubPEM(t, &alipayKey(t).PublicKey))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func validNotify(t *testing.T) url.Values {
	t.Helper()
	return signNotify(t, alipayKey(t), url.Values{
		"app_id":       {"2021000000000000"},
		"out_trade_no": {"T20260828001"},
		"trade_no":     {"2026082822001400000000000001"},
		"trade_status": {"TRADE_SUCCESS"},
		"total_amount": {"0.01"},
		// 中文和特殊字符要能原样参与验签——subject 里出现中文是常态。
		"subject":     {"测试商品 & 附加信息=1"},
		"gmt_payment": {"2026-08-28 10:00:00"},
		"empty_field": {""},
	})
}

func TestVerifyNotification(t *testing.T) {
	c := notifyClient(t)

	t.Run("合法通知通过并可读出字段", func(t *testing.T) {
		n, err := c.VerifyNotification(validNotify(t))
		if err != nil {
			t.Fatalf("合法通知被拒: %v", err)
		}
		if got := n.OutTradeNo(); got != "T20260828001" {
			t.Errorf("OutTradeNo = %q", got)
		}
		if got := n.TradeNo(); got != "2026082822001400000000000001" {
			t.Errorf("TradeNo = %q", got)
		}
		if got := n.TotalAmount(); got != "0.01" {
			t.Errorf("TotalAmount = %q", got)
		}
		if n.TradeStatus() != TradeStatusSuccess {
			t.Errorf("TradeStatus = %q", n.TradeStatus())
		}
		if !n.Paid() {
			t.Error("TRADE_SUCCESS 未被判为已支付")
		}
		if got := n.Get("subject"); got != "测试商品 & 附加信息=1" {
			t.Errorf("含中文与特殊字符的字段读取有误: %q", got)
		}
	})

	// 篡改金额是这条链路上最直接的攻击：把 0.01 改成 9999 骗发货。
	t.Run("篡改金额必须被拒", func(t *testing.T) {
		v := validNotify(t)
		v.Set("total_amount", "9999.00")
		if _, err := c.VerifyNotification(v); !errors.Is(err, ErrSignature) {
			t.Fatalf("期望 ErrSignature，得到 %v", err)
		}
	})

	t.Run("篡改交易状态必须被拒", func(t *testing.T) {
		v := signNotify(t, alipayKey(t), url.Values{
			"app_id":       {"2021000000000000"},
			"out_trade_no": {"T1"},
			"trade_status": {"WAIT_BUYER_PAY"},
		})
		v.Set("trade_status", "TRADE_SUCCESS")
		if _, err := c.VerifyNotification(v); !errors.Is(err, ErrSignature) {
			t.Fatalf("期望 ErrSignature，得到 %v", err)
		}
	})

	t.Run("追加参数必须被拒", func(t *testing.T) {
		v := validNotify(t)
		v.Set("extra_field", "injected")
		if _, err := c.VerifyNotification(v); !errors.Is(err, ErrSignature) {
			t.Fatalf("期望 ErrSignature，得到 %v", err)
		}
	})

	t.Run("缺 sign 必须被拒", func(t *testing.T) {
		v := validNotify(t)
		v.Del("sign")
		if _, err := c.VerifyNotification(v); !errors.Is(err, ErrSignature) {
			t.Fatalf("期望 ErrSignature，得到 %v", err)
		}
	})

	t.Run("别人的应用发来的通知必须被拒", func(t *testing.T) {
		// 攻击者用自己的沙箱应用给你的 notify_url 发通知——签名是真的，
		// 但这笔交易与你无关。
		v := signNotify(t, alipayKey(t), url.Values{
			"app_id":       {"2099999999999999"},
			"out_trade_no": {"T1"},
			"trade_status": {"TRADE_SUCCESS"},
			"total_amount": {"0.01"},
		})
		_, err := c.VerifyNotification(v)
		if err == nil {
			t.Fatal("别的应用的通知被放行了")
		}
		if !strings.Contains(err.Error(), "app_id") {
			t.Errorf("错误信息未指出 app_id 不符: %v", err)
		}
	})

	t.Run("换一把公钥必须失败", func(t *testing.T) {
		other, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("生成对照密钥: %v", err)
		}
		v := signNotify(t, other, url.Values{
			"app_id":       {"2021000000000000"},
			"trade_status": {"TRADE_SUCCESS"},
		})
		if _, err := c.VerifyNotification(v); !errors.Is(err, ErrSignature) {
			t.Fatalf("期望 ErrSignature，得到 %v", err)
		}
	})
}

func TestParseNotificationFromRequest(t *testing.T) {
	c := notifyClient(t)
	body := validNotify(t).Encode()

	req := httptest.NewRequest(http.MethodPost, "/notify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	n, err := c.ParseNotification(req)
	if err != nil {
		t.Fatalf("ParseNotification: %v", err)
	}
	// 表单里的值经过 URL 编码传输，解码后必须与签名时一致。
	if got := n.Get("subject"); got != "测试商品 & 附加信息=1" {
		t.Errorf("表单往返后字段有变: %q", got)
	}
	if !n.Paid() {
		t.Error("Paid() = false")
	}
}

func TestTradeStatusPaid(t *testing.T) {
	// 只有这两个状态算收到钱，其余一律不算——TRADE_CLOSED 尤其危险，它可能是
	// 付完又全额退了。
	for status, want := range map[TradeStatus]bool{
		TradeStatusSuccess:      true,
		TradeStatusFinished:     true,
		TradeStatusWaitBuyerPay: false,
		TradeStatusClosed:       false,
		"":                      false,
		"UNKNOWN_STATUS":        false,
	} {
		if got := status.Paid(); got != want {
			t.Errorf("TradeStatus(%q).Paid() = %v, 期望 %v", status, got, want)
		}
	}

	t.Run("状态缺失时不算已支付", func(t *testing.T) {
		if Paid(nil) {
			t.Error("Paid(nil) = true，查不到状态却当成已支付")
		}
		s := "TRADE_SUCCESS"
		if !Paid(&s) {
			t.Error("Paid(TRADE_SUCCESS) = false")
		}
	})
}
