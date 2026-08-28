package alipay

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// signatureAlgorithm 是 v3 接口固定使用的签名算法标识，出现在 Authorization 头首部。
const signatureAlgorithm = "ALIPAY-SHA256withRSA"

// signer 用应用私钥为请求生成 Authorization 头。
//
// v3 的签名与 v1 完全不同：v1 把业务参数按 key 排序拼成 k=v&k=v 再签，v3 改成
// 签「认证串 + 方法 + 路径 + 报文体」这一整块。两者不通用，唯一还在用 v1 那套
// 的地方是异步通知的验签（见 notify.go）——那个接口至今没有 v3 版本。
type signer struct {
	appID     string
	appCertSN string // 仅证书模式需要，公钥模式为空
	key       *rsa.PrivateKey

	// 时间与随机数抽出来，测试才能拿到确定的签名内容。
	now      func() time.Time
	newNonce func() (string, error)
}

func newSigner(appID string, key *rsa.PrivateKey, appCertSN string) *signer {
	return &signer{
		appID:     appID,
		appCertSN: appCertSN,
		key:       key,
		now:       time.Now,
		newNonce:  newNonce,
	}
}

// authorization 生成 Authorization 头的值。
//
// target 是带 query 的请求路径（如 /v3/alipay/trade/query 或
// /v3/alipay/data/dataservice/bill/downloadurl/query?bill_type=trade），body 是
// 原样的请求报文体，GET 请求传 nil。
//
// 签名内容的每一段后面都跟一个换行，包括最后一段——报文体为空时留下的正是一个
// 空行，这不是笔误，服务端按同样的规则重建待签串。
func (s *signer) authorization(method, target string, body []byte, appAuthToken string) (string, error) {
	nonce, err := s.newNonce()
	if err != nil {
		return "", fmt.Errorf("alipay: 生成 nonce: %w", err)
	}

	authString := s.authString(nonce, s.now())

	sign, err := s.sign(buildSignContent(authString, method, target, body, appAuthToken))
	if err != nil {
		return "", err
	}
	return signatureAlgorithm + " " + authString + ",sign=" + sign, nil
}

// buildSignContent 拼出待签名内容。
//
// 每一段后面都跟一个换行，包括最后一段——报文体为空时留下的正是一个空行，
// 这不是笔误，服务端按同样的规则重建待签串。
func buildSignContent(authString, method, target string, body []byte, appAuthToken string) string {
	var sb strings.Builder
	sb.WriteString(authString)
	sb.WriteByte('\n')
	sb.WriteString(method)
	sb.WriteByte('\n')
	sb.WriteString(target)
	sb.WriteByte('\n')
	sb.Write(body)
	sb.WriteByte('\n')
	if appAuthToken != "" {
		sb.WriteString(appAuthToken)
		sb.WriteByte('\n')
	}
	return sb.String()
}

// authString 拼装认证串。它既进签名内容，也原样出现在 Authorization 头里，
// 服务端据此重建待签串，所以两处必须是同一个字符串。
func (s *signer) authString(nonce string, now time.Time) string {
	var sb strings.Builder
	sb.WriteString("app_id=")
	sb.WriteString(s.appID)
	if s.appCertSN != "" {
		sb.WriteString(",app_cert_sn=")
		sb.WriteString(s.appCertSN)
	}
	sb.WriteString(",nonce=")
	sb.WriteString(nonce)
	sb.WriteString(",timestamp=")
	sb.WriteString(strconv.FormatInt(now.UnixMilli(), 10))
	return sb.String()
}

func (s *signer) sign(content string) (string, error) {
	digest := sha256.Sum256([]byte(content))
	sig, err := rsa.SignPKCS1v15(rand.Reader, s.key, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("alipay: 签名: %w", err)
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

// verifySignature 校验支付宝响应的签名。
//
// 待验串由响应头 alipay-timestamp、alipay-nonce 和报文体三段拼成，规则与请求
// 签名一致：每段后跟一个换行。
//
// 任何一环缺失都返回 ErrSignature 而不是"跳过校验"：验签是这条链路上唯一能
// 确认报文确实来自支付宝的手段，宁可整笔失败也不能放行一份来历不明的报文。
func verifySignature(pub *rsa.PublicKey, timestamp, nonce, signature string, body []byte) error {
	if timestamp == "" || nonce == "" || signature == "" {
		return fmt.Errorf("%w: 响应缺少签名头（alipay-timestamp/alipay-nonce/alipay-signature）", ErrSignature)
	}

	var sb strings.Builder
	sb.WriteString(timestamp)
	sb.WriteByte('\n')
	sb.WriteString(nonce)
	sb.WriteByte('\n')
	sb.Write(body)
	sb.WriteByte('\n')

	sig, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return fmt.Errorf("%w: 签名不是合法的 base64", ErrSignature)
	}

	digest := sha256.Sum256([]byte(sb.String()))
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, digest[:], sig); err != nil {
		return fmt.Errorf("%w: 报文与签名不匹配，可能被篡改，也可能配置了错误的支付宝公钥", ErrSignature)
	}
	return nil
}

// newNonce 生成一个随机 UUID v4 作为请求的 nonce。
//
// 自己拼而不是引入 uuid 库：这里只需要 16 字节随机数加两处版本位，为它给一个
// 支付 SDK 添一条依赖不值得。
func newNonce() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant RFC 4122
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
