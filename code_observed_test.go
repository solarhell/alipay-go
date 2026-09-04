package alipay

import "testing"

// TestObservedCodesDifferFromSpec 钉住实测值与规范枚举值的差异。
//
// 这两组常量看着像重复定义，很容易被当成笔误「顺手统一」掉。这个测试在这里
// 就是为了拦住那一刀：它们本来就该不一样，规范写的那个线上根本不返回，改成
// 一致就等于把判断改回漏判。
func TestObservedCodesDifferFromSpec(t *testing.T) {
	pairs := []struct {
		name           string
		spec, observed Code
	}{
		{"账单不存在", CodeBillNotExist, CodeBillNotExistObserved},
		{"入参不合法", CodeInvailidArguments, CodeInvalidArgumentsObserved},
	}
	for _, p := range pairs {
		t.Run(p.name, func(t *testing.T) {
			if p.spec == p.observed {
				t.Errorf("规范值与实测值变得相同了（都是 %q）。"+
					"若确认支付宝已改为按规范返回，请连同 code_observed.go 的说明一起更新，"+
					"而不是让两者悄悄一致", p.spec)
			}
			if p.observed == "" {
				t.Error("实测值为空")
			}
		})
	}
}

// TestObservedCodesMatchExpectedLiterals 固定实测值的字面量。
//
// 这些值是向真实环境发请求得到的，不是推导出来的，所以只能靠断言字面量来防止
// 被无意改动。
func TestObservedCodesMatchExpectedLiterals(t *testing.T) {
	for _, tt := range []struct {
		got  Code
		want string
	}{
		{CodeBillNotExistObserved, "isp.bill_not_exist"},
		{CodeInvalidArgumentsObserved, "invalid_arguments"},
	} {
		if string(tt.got) != tt.want {
			t.Errorf("实测常量 = %q, 期望 %q", tt.got, tt.want)
		}
	}
}
