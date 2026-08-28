package alipay

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
)

// ParsePrivateKey 解析应用私钥。
//
// 支付宝的密钥工具按语言给出不同格式，而开发者从控制台复制出来的往往是一段
// 没有 PEM 头、还带着换行的裸 base64。这里把这些形态全部接住：
//
//   - PKCS#1（-----BEGIN RSA PRIVATE KEY-----），非 Java 语言的密钥工具默认给这个
//   - PKCS#8（-----BEGIN PRIVATE KEY-----），Java 语言默认给这个
//   - 以上两种去掉 PEM 头的裸 base64，换行和空格随意
//
// 之所以全都认而不是要求调用方先转换：私钥格式是集成时最常见的绊脚石，可
// "到底是哪种格式"对调用方并没有意义——程序自己看得出来。
func ParsePrivateKey(key string) (*rsa.PrivateKey, error) {
	der, err := decodeKey(key, "PRIVATE KEY")
	if err != nil {
		return nil, fmt.Errorf("alipay: 解析应用私钥: %w", err)
	}

	if k, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		return k, nil
	}
	k, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, errors.New("alipay: 解析应用私钥: 内容既不是 PKCS#1 也不是 PKCS#8，" +
			"请确认复制的是应用私钥而不是公钥或证书")
	}
	rsaKey, ok := k.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("alipay: 解析应用私钥: 需要 RSA 密钥，得到 %T；"+
			"支付宝要求 RSA2(SHA256withRSA)，请重新生成 RSA 2048 位密钥", k)
	}
	return rsaKey, nil
}

// ParsePublicKey 解析支付宝公钥，用于验证响应和异步通知的签名。
//
// 与 ParsePrivateKey 一样，PEM 格式和裸 base64 都能认。
//
// 注意这里要的是「支付宝公钥」，不是应用公钥——两者都在开放平台页面上，填错
// 会让所有验签失败。应用公钥是你上传给支付宝的，支付宝公钥是支付宝给你的。
func ParsePublicKey(key string) (*rsa.PublicKey, error) {
	der, err := decodeKey(key, "PUBLIC KEY")
	if err != nil {
		return nil, fmt.Errorf("alipay: 解析支付宝公钥: %w", err)
	}

	if k, err := x509.ParsePKIXPublicKey(der); err == nil {
		rsaKey, ok := k.(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("alipay: 解析支付宝公钥: 需要 RSA 密钥，得到 %T", k)
		}
		return rsaKey, nil
	}
	if k, err := x509.ParsePKCS1PublicKey(der); err == nil {
		return k, nil
	}
	return nil, errors.New("alipay: 解析支付宝公钥: 内容无法识别为 RSA 公钥，" +
		"请确认填的是「支付宝公钥」而不是你自己的应用公钥")
}

// decodeKey 把 PEM 或裸 base64 还原成 DER 字节。
func decodeKey(key, kind string) ([]byte, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, errors.New("内容为空")
	}

	if strings.Contains(key, "-----BEGIN") {
		block, _ := pem.Decode([]byte(key))
		if block == nil {
			return nil, errors.New("PEM 格式损坏，请检查是否缺少结尾的 -----END 行")
		}
		if !strings.Contains(block.Type, kind) {
			return nil, fmt.Errorf("PEM 类型是 %q，需要 %s", block.Type, kind)
		}
		return block.Bytes, nil
	}

	// 裸 base64：去掉所有空白后再解码，容忍控制台复制出来的换行。
	compact := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, key)
	der, err := base64.StdEncoding.DecodeString(compact)
	if err != nil {
		return nil, errors.New("既没有 PEM 头，也不是合法的 base64")
	}
	return der, nil
}
