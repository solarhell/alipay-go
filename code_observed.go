package alipay

// 实测确认的错误码。
//
// code_gen.go 里的常量来自官方 OpenAPI 规范，但规范的枚举值并非全部与线上一致
// （见 Code 的说明）。这里补上实测确认的取值，供判断时直接使用——不必自己写
// 字符串字面量，也不必猜到底该用哪个。
//
// 这些值是在真实环境向支付宝发请求得到的，不是从文档抄的。每一条都注明了验证
// 时间和方式，将来支付宝改了取值，可以据此判断结论是否还成立。
const (
	// CodeBillNotExistObserved 是「账单不存在」在线上实际返回的错误码。
	//
	// 规范枚举写的是 BILL_NOT_EXIST（即 CodeBillNotExist），但 v3 网关实际返回
	// 的仍是 v1 时期的 "isp.bill_not_exist"。2026-09-04 用生产商户账号查询当日
	// 尚未生成的账单验证，沙箱同样返回这个值。
	//
	// 这个码是双义的：既可能是查得太早、账单还没生成，也可能是当日确实无交易。
	// 官方建议日账单在次日 10:00 之后下载，可用查询时刻来消歧。
	CodeBillNotExistObserved Code = "isp.bill_not_exist"

	// CodeInvalidArgumentsObserved 是「入参不合法」在线上实际返回的错误码。
	//
	// 规范枚举写的是 INVAILID_ARGUMENTS（注意官方把 INVALID 拼错了，即
	// CodeInvailidArguments），线上实际返回小写的 "invalid_arguments"。
	// 2026-09-04 用生产商户账号查询未来日期的账单验证。
	CodeInvalidArgumentsObserved Code = "invalid_arguments"
)
