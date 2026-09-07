package alipay

import (
	"errors"
	"fmt"
	"net"
	"testing"
)

func TestErrorMessage(t *testing.T) {
	err := &Error{
		Code:      CodeACQTradeNotExist,
		Message:   "交易不存在",
		Links:     "https://opendocs.alipay.com/x",
		RequestID: "0b8f",
	}
	want := "alipay: ACQ.TRADE_NOT_EXIST: 交易不存在 (trace 0b8f) (见 https://opendocs.alipay.com/x)"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, 期望 %q", got, want)
	}

	// trace 和 links 缺失时不应留下空括号。
	bare := &Error{Code: CodeACQSystemError, Message: "系统异常"}
	if got, want := bare.Error(), "alipay: ACQ.SYSTEM_ERROR: 系统异常"; got != want {
		t.Errorf("Error() = %q, 期望 %q", got, want)
	}
}

func TestErrorClassification(t *testing.T) {
	tests := []struct {
		name                        string
		err                         *Error
		indeterminate, retryableErr bool
	}{
		// 官方要求收到 SYSTEM_ERROR 后查询订单状态，而不是直接重试。
		{"系统异常", &Error{Code: CodeACQSystemError}, true, false},
		{"5xx", &Error{Code: CodeACQTradeNotExist, StatusCode: 502}, true, false},
		{"限流", &Error{Code: CodeSystemRateLimit}, false, true},
		{"429", &Error{Code: CodeUnknownError, StatusCode: 429}, false, true},
		{"交易不存在", &Error{Code: CodeACQTradeNotExist, StatusCode: 200}, false, false},
		{"参数非法", &Error{Code: CodeACQInvalidParameter, StatusCode: 200}, false, false},
		{"余额不足", &Error{Code: CodeACQSellerBalanceNotEnough, StatusCode: 200}, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Indeterminate(); got != tt.indeterminate {
				t.Errorf("Indeterminate() = %v, 期望 %v", got, tt.indeterminate)
			}
			if got := tt.err.Retryable(); got != tt.retryableErr {
				t.Errorf("Retryable() = %v, 期望 %v", got, tt.retryableErr)
			}
			// 结果未知的请求绝不能被判定成可以原样重发，否则就是重复扣款。
			if tt.err.Indeterminate() && tt.err.Retryable() {
				t.Error("Indeterminate 与 Retryable 同时为真")
			}
		})
	}
}

func TestTransportErrorAlwaysIndeterminate(t *testing.T) {
	// 请求没拿到完整响应时，无法知道支付宝那边到底执行了没有，一律按未知处理。
	for _, cause := range []error{
		&net.OpError{Op: "dial", Err: errors.New("connection refused")},
		errors.New("context deadline exceeded"),
	} {
		err := &TransportError{Op: "alipay.trade.precreate", Err: cause}
		if !err.Indeterminate() {
			t.Errorf("TransportError(%v).Indeterminate() = false，期望 true", cause)
		}
		if !errors.Is(err, cause) {
			t.Errorf("TransportError 未透出底层错误 %v", cause)
		}
	}
}

func TestHelpersUnwrapChain(t *testing.T) {
	// 调用方包装过的错误同样要能被识别，否则这几个判断在真实代码里就失效了。
	wrapped := fmt.Errorf("下单失败: %w", &Error{Code: CodeACQSystemError})

	if !Indeterminate(wrapped) {
		t.Error("Indeterminate 未穿透 fmt.Errorf 包装")
	}
	if Retryable(wrapped) {
		t.Error("Retryable 对结果未知的错误返回了 true")
	}
	if !IsCode(wrapped, CodeACQTradeNotExist, CodeACQSystemError) {
		t.Error("IsCode 未穿透 fmt.Errorf 包装")
	}
	if IsCode(wrapped, CodeACQTradeNotExist) {
		t.Error("IsCode 匹配了不该匹配的错误码")
	}

	// 非本 SDK 的错误不应被误判成可重试或结果未知。
	other := errors.New("boom")
	if Indeterminate(other) || Retryable(other) || IsCode(other, CodeACQSystemError) {
		t.Error("对无关错误做出了错误判断")
	}
}

func TestGeneratedCodesCoverKnownValues(t *testing.T) {
	// 这几个码是分类逻辑和调用方判断依赖的，生成器若因 spec 变动漏掉它们，
	// 应当在这里当场失败，而不是等到线上判断落空。
	for name, got := range map[string]Code{
		"CodeACQSystemError":     CodeACQSystemError,
		"CodeACQTradeHasSuccess": CodeACQTradeHasSuccess,
		"CodeACQTradeHasClose":   CodeACQTradeHasClose,
		"CodeACQTradeNotExist":   CodeACQTradeNotExist,
		"CodeTradeNotExist":      CodeTradeNotExist,
		"CodeSystemRateLimit":    CodeSystemRateLimit,
	} {
		if got == "" {
			t.Errorf("%s 为空", name)
		}
	}
	// 两个同名不同域的码必须各自独立，合并会让调用方误判。
	if CodeACQTradeNotExist == CodeTradeNotExist {
		t.Error("ACQ.TRADE_NOT_EXIST 与 TRADE_NOT_EXIST 撞成了同一个值")
	}
}

// TestCommonGatewayCodesClassification 钉住网关公共码的分类。
//
// 公共码此前被生成器整体漏掉（只认 *ErrorResponseModelCode 后缀），限流码因此从未
// 进入分类表。这里把最关键的几条钉死：限流可原样重试、网关侧"业务系统不可用"
// 结果未知、处罚不可重试。
func TestCommonGatewayCodesClassification(t *testing.T) {
	tests := []struct {
		name          string
		code          Code
		indeterminate bool
		retryable     bool
	}{
		{"应用级限流", CodeAppCallLimited, false, true},
		{"接口级限流", CodeMethodCallLimited, false, true},
		{"网关侧业务系统不可用", CodeUnknowError, true, false},
		{"接口被处罚", CodeAppApiPunished, false, false},
		{"签名错误", CodeInvalidSignature, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &Error{Code: tt.code, StatusCode: 400}
			if got := e.Indeterminate(); got != tt.indeterminate {
				t.Errorf("Indeterminate() = %v, 期望 %v", got, tt.indeterminate)
			}
			if got := e.Retryable(); got != tt.retryable {
				t.Errorf("Retryable() = %v, 期望 %v", got, tt.retryable)
			}
		})
	}
}

// TestCommonCodesUseWireFormat 钉住公共码的取值是线上写法（小写连字符）。
//
// missing-timestamp 是 2026-09-07 用未签名请求从生产网关实测拿到的，证明这张表
// 就是线上真实写法；它与业务码的大写下划线风格并存，两者不能混用。
func TestCommonCodesUseWireFormat(t *testing.T) {
	if CodeMissingTimestamp != "missing-timestamp" {
		t.Errorf("CodeMissingTimestamp = %q", CodeMissingTimestamp)
	}
	if CodeAppCallLimited != "app-call-limited" || CodeMethodCallLimited != "method-call-limited" {
		t.Errorf("限流码取值 = %q / %q", CodeAppCallLimited, CodeMethodCallLimited)
	}
	// 公共码与业务码里都有"参数非法"，必须是两个不同的常量、两个不同的值
	if CodeInvalidParameter == CodeACQInvalidParameter {
		t.Error("公共码 invalid-parameter 与业务码 ACQ.INVALID_PARAMETER 撞成同一个值")
	}
}

// TestRateLimitCodeBeatsStatusHeuristic 限流码伴随 5xx 时，码表的明确知识压过"5xx 即未知"。
//
// 限流码伴随的 HTTP 状态码没有线上样本。若网关用 503 回 app-call-limited，此前的逻辑
// 会把它判成结果未知、让调用方白查一遍订单——而限流是网关就地拒绝，根本没执行。
func TestRateLimitCodeBeatsStatusHeuristic(t *testing.T) {
	for _, st := range []int{400, 429, 500, 503} {
		e := &Error{Code: CodeAppCallLimited, StatusCode: st}
		if e.Indeterminate() {
			t.Errorf("HTTP %d + app-call-limited 被判为结果未知", st)
		}
		if !e.Retryable() {
			t.Errorf("HTTP %d + app-call-limited 被判为不可重试", st)
		}
	}
	// 未知码 + 5xx 仍然是结果未知，启发式只让位给码表里明确的可重试码
	if e := (&Error{Code: "some-new-code", StatusCode: 502}); !e.Indeterminate() || e.Retryable() {
		t.Error("未知码 + 5xx 未按结果未知处理")
	}
}

// TestCompoundSpecCodeIsSplit 规范里塞成一条的两个码必须被拆成两个常量。
func TestCompoundSpecCodeIsSplit(t *testing.T) {
	if CodeAppKeySecurityRisk != "app-key-security-risk" || CodeAppCertExpired != "app-cert-expired" {
		t.Errorf("复合枚举未拆开: %q / %q", CodeAppKeySecurityRisk, CodeAppCertExpired)
	}
}
