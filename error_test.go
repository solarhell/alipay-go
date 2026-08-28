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
