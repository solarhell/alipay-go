package alipay

import (
	"errors"
	"fmt"
	"net/http"
)

// ErrSignature 表示验签失败：响应或异步通知的签名与内容对不上。
//
// 收到这个错误意味着报文不可信——可能被篡改，也可能是配置了错误的支付宝公钥。
// 无论哪种，都绝不能采信报文里的金额和交易状态，更不能据此发货或改单。
var ErrSignature = errors.New("alipay: 验签失败")

// Error 是支付宝返回的业务错误，对应官方 OpenAPI 规范中的 ErrorResponseModel。
type Error struct {
	// Code 是支付宝的业务错误码，如 ACQ.TRADE_NOT_EXIST。
	Code Code
	// Message 是支付宝返回的中文错误描述。
	Message string
	// Links 是官方给出的解决方案链接，可能为空。
	Links string
	// StatusCode 是 HTTP 状态码。
	StatusCode int
	// RequestID 取自响应头 alipay-trace-id，向支付宝报障时带上它。
	RequestID string
}

func (e *Error) Error() string {
	msg := fmt.Sprintf("alipay: %s: %s", e.Code, e.Message)
	if e.RequestID != "" {
		msg += fmt.Sprintf(" (trace %s)", e.RequestID)
	}
	if e.Links != "" {
		msg += fmt.Sprintf(" (见 %s)", e.Links)
	}
	return msg
}

// indeterminateCodes 收录"支付宝没能给出确定答复"的错误码。
//
// 这个集合宁可多收不可漏收：多收一个，调用方多查一次订单，代价是一次免费的
// 查询请求；漏收一个，调用方把"其实已经成功"当成失败重发，代价是重复下单或
// 重复退款。所以只要官方文档没有明确保证请求未生效，就往这里放。
//
// ACQ.SYSTEM_ERROR 是官方文档点名要求这样处理的：收到后应立即调用
// alipay.trade.query 确认订单状态，再决定下一步，而不是直接重试。
var indeterminateCodes = map[Code]bool{
	CodeACQSystemError: true,
}

// retryableCodes 收录可以原样重试的错误码。
//
// 判据是"支付宝明确拒绝了这次请求、请求没有产生任何副作用"——限流就是典型：
// 请求压根没被执行，退避之后重发是安全的。有副作用嫌疑的一律不放进来。
var retryableCodes = map[Code]bool{
	CodeSystemRateLimit: true,
	CodeUserRateLimit:   true,
}

// Indeterminate 报告这次请求的最终结果是否未知。
//
// 为 true 时，请求有可能已经在支付宝侧生效了。对下单、退款这类有副作用的接口，
// 此时唯一安全的动作是拿原来的商户订单号去查询真实状态（alipay.trade.query /
// alipay.trade.fastpay.refund.query），根据查询结果决定后续，绝不能当作失败
// 直接重发。对查询、账单下载这类只读接口，重试本身无害，可以忽略本方法。
func (e *Error) Indeterminate() bool {
	return indeterminateCodes[e.Code] || e.StatusCode >= 500
}

// Retryable 报告能否原样重试这次请求。
//
// 仅在支付宝明确拒绝、且请求确定未产生副作用时为 true。它与 Indeterminate
// 互斥：结果未知的请求不能直接重试，得先查清楚状态。
func (e *Error) Retryable() bool {
	if e.Indeterminate() {
		return false
	}
	return retryableCodes[e.Code] || e.StatusCode == http.StatusTooManyRequests
}

// TransportError 表示这次请求没能拿到一个完整的 HTTP 响应——连接失败、超时、
// 读响应体中断等。
//
// 它一律被当作"结果未知"：请求可能已经送达支付宝并生效，只是回程的响应丢了。
// 判断连接到底有没有把字节送出去，在 net/http 这一层做不到可靠区分，而猜错的
// 代价是重复扣款，所以这里不猜。
type TransportError struct {
	// Op 是出错时正在执行的操作，如 alipay.trade.precreate。
	Op string
	// Err 是底层错误。
	Err error
}

func (e *TransportError) Error() string {
	return fmt.Sprintf("alipay: %s: %v", e.Op, e.Err)
}

func (e *TransportError) Unwrap() error { return e.Err }

// Indeterminate 恒为 true，理由见 TransportError 的说明。
func (e *TransportError) Indeterminate() bool { return true }

// indeterminate 由能判断自身确定性的错误实现。
type indeterminate interface{ Indeterminate() bool }

// retryable 由能判断自身可重试性的错误实现。
type retryable interface{ Retryable() bool }

// Indeterminate 报告 err 是否意味着请求结果未知。
//
// 它会沿 errors.As 的链路查找，因此调用方包装过的错误同样能被正确识别。
func Indeterminate(err error) bool {
	var target indeterminate
	return errors.As(err, &target) && target.Indeterminate()
}

// Retryable 报告 err 对应的请求能否原样重试。
func Retryable(err error) bool {
	var target retryable
	return errors.As(err, &target) && target.Retryable()
}

// IsCode 报告 err 是否为业务错误且错误码为 codes 之一。
//
//	if alipay.IsCode(err, alipay.CodeACQTradeNotExist) { ... }
func IsCode(err error, codes ...Code) bool {
	var e *Error
	if !errors.As(err, &e) {
		return false
	}
	for _, c := range codes {
		if e.Code == c {
			return true
		}
	}
	return false
}
