package alipay

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"strings"
	"testing"
)

func pemEncode(t *testing.T, typ string, der []byte) string {
	t.Helper()
	return string(pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}))
}

// TestParsePrivateKey 覆盖开发者手里可能拿到的每一种私钥形态。
//
// 支付宝的密钥工具按语言给不同格式，控制台复制出来的又往往没有 PEM 头，
// 认不出来就会被当成"私钥格式不对"排查半天——skill 里专门为此立了条规矩。
func TestParsePrivateKey(t *testing.T) {
	k := key(t)
	pkcs1 := x509.MarshalPKCS1PrivateKey(k)
	pkcs8, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		t.Fatalf("编码 PKCS#8: %v", err)
	}

	accepted := map[string]string{
		"PKCS#1 PEM":      pemEncode(t, "RSA PRIVATE KEY", pkcs1),
		"PKCS#8 PEM":      pemEncode(t, "PRIVATE KEY", pkcs8),
		"PKCS#1 裸 base64": base64.StdEncoding.EncodeToString(pkcs1),
		"PKCS#8 裸 base64": base64.StdEncoding.EncodeToString(pkcs8),
		// 从控制台复制常常带着换行和首尾空白。
		"带换行的裸 base64": insertNewlines(base64.StdEncoding.EncodeToString(pkcs8), 64),
		"前后有空白":        "\n  " + base64.StdEncoding.EncodeToString(pkcs1) + "  \n",
	}
	for name, input := range accepted {
		t.Run(name, func(t *testing.T) {
			got, err := ParsePrivateKey(input)
			if err != nil {
				t.Fatalf("应当被接受，却报错: %v", err)
			}
			if !got.Equal(k) {
				t.Error("解析出的私钥与原始密钥不一致")
			}
		})
	}

	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("生成 EC 密钥: %v", err)
	}
	ecDER, err := x509.MarshalPKCS8PrivateKey(ecKey)
	if err != nil {
		t.Fatalf("编码 EC 密钥: %v", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&k.PublicKey)
	if err != nil {
		t.Fatalf("编码公钥: %v", err)
	}

	rejected := map[string]string{
		"空字符串":      "",
		"只有空白":      "   \n\t ",
		"不是 base64": "这显然不是密钥",
		"PEM 缺结尾":   "-----BEGIN RSA PRIVATE KEY-----\n" + base64.StdEncoding.EncodeToString(pkcs1),
		// 把公钥填进私钥字段是很常见的手滑，必须报错而不是静默出错。
		"误填了公钥": pemEncode(t, "PUBLIC KEY", pubDER),
		// 支付宝只支持 RSA2，EC 密钥要给出明确提示。
		"EC 密钥": pemEncode(t, "PRIVATE KEY", ecDER),
	}
	for name, input := range rejected {
		t.Run(name, func(t *testing.T) {
			if _, err := ParsePrivateKey(input); err == nil {
				t.Fatal("应当被拒绝，却解析成功了")
			} else if !strings.Contains(err.Error(), "alipay:") {
				t.Errorf("错误信息缺少 alipay 前缀: %v", err)
			}
		})
	}
}

func TestParsePublicKey(t *testing.T) {
	k := key(t)
	pkix, err := x509.MarshalPKIXPublicKey(&k.PublicKey)
	if err != nil {
		t.Fatalf("编码 PKIX: %v", err)
	}
	pkcs1 := x509.MarshalPKCS1PublicKey(&k.PublicKey)

	accepted := map[string]string{
		"PKIX PEM":        pemEncode(t, "PUBLIC KEY", pkix),
		"PKIX 裸 base64":   base64.StdEncoding.EncodeToString(pkix),
		"PKCS#1 裸 base64": base64.StdEncoding.EncodeToString(pkcs1),
		"带换行":             insertNewlines(base64.StdEncoding.EncodeToString(pkix), 64),
	}
	for name, input := range accepted {
		t.Run(name, func(t *testing.T) {
			got, err := ParsePublicKey(input)
			if err != nil {
				t.Fatalf("应当被接受，却报错: %v", err)
			}
			if !got.Equal(&k.PublicKey) {
				t.Error("解析出的公钥与原始公钥不一致")
			}
		})
	}

	for name, input := range map[string]string{
		"空字符串":      "",
		"不是 base64": "not a key",
		// 误填私钥同样是常见手滑。
		"误填了私钥": pemEncode(t, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(k)),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParsePublicKey(input); err == nil {
				t.Fatal("应当被拒绝，却解析成功了")
			}
		})
	}
}

func insertNewlines(s string, n int) string {
	var b strings.Builder
	for i := 0; i < len(s); i += n {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(s[i:min(i+n, len(s))])
	}
	return b.String()
}
