// 由 internal/cmd/gencodes 依据 internal/openapi/openapi.gen.go 生成，请勿手工编辑。
// 重新生成：make generate

package alipay

// Code 是支付宝返回的业务错误码。
//
// 常量取自官方 OpenAPI 规范中各接口 ErrorResponseModel 的 code 枚举，值即
// 官方原码；未收录的码同样会被解析出来，直接比较字符串即可，不必等 SDK 更新。
//
// 注意：规范里的枚举值并非全部与线上一致。账单域已确认有出入，相关常量下方
// 标注了实测结论。交易域（ACQ.*）目前未发现不一致。做资金相关判断前，建议对
// 着真实环境验证一次错误码，不要只依赖本文件。
type Code string

// 错误码常量，按官方原码字典序排列。
const (
	CodeACQAccessForbidden                       Code = "ACQ.ACCESS_FORBIDDEN"
	CodeACQAllocAmountValidateError              Code = "ACQ.ALLOC_AMOUNT_VALIDATE_ERROR"
	CodeACQApplyPCMerchantCodeError              Code = "ACQ.APPLY_PC_MERCHANT_CODE_ERROR"
	CodeACQBeyondPayRestriction                  Code = "ACQ.BEYOND_PAY_RESTRICTION"
	CodeACQBeyondPerReceiptDayRestriction        Code = "ACQ.BEYOND_PER_RECEIPT_DAY_RESTRICTION"
	CodeACQBeyondPerReceiptRestriction           Code = "ACQ.BEYOND_PER_RECEIPT_RESTRICTION"
	CodeACQBeyondPerReceiptSingleRestriction     Code = "ACQ.BEYOND_PER_RECEIPT_SINGLE_RESTRICTION"
	CodeACQBuyerEnableStatusForbid               Code = "ACQ.BUYER_ENABLE_STATUS_FORBID"
	CodeACQBuyerError                            Code = "ACQ.BUYER_ERROR"
	CodeACQBuyerNotExist                         Code = "ACQ.BUYER_NOT_EXIST"
	CodeACQBuyerPaymentAmountDayLimitError       Code = "ACQ.BUYER_PAYMENT_AMOUNT_DAY_LIMIT_ERROR"
	CodeACQBuyerPaymentAmountMonthLimitError     Code = "ACQ.BUYER_PAYMENT_AMOUNT_MONTH_LIMIT_ERROR"
	CodeACQBuyerSellerEqual                      Code = "ACQ.BUYER_SELLER_EQUAL"
	CodeACQContextInconsistent                   Code = "ACQ.CONTEXT_INCONSISTENT"
	CodeACQCurrencyNotSupport                    Code = "ACQ.CURRENCY_NOT_SUPPORT"
	CodeACQCustomerValidateError                 Code = "ACQ.CUSTOMER_VALIDATE_ERROR"
	CodeACQDefaultSettleRuleNotExist             Code = "ACQ.DEFAULT_SETTLE_RULE_NOT_EXIST"
	CodeACQDiscordantRepeatRequest               Code = "ACQ.DISCORDANT_REPEAT_REQUEST"
	CodeACQEnterprisePayBizError                 Code = "ACQ.ENTERPRISE_PAY_BIZ_ERROR"
	CodeACQErrorBuyerCertifyLevelLimit           Code = "ACQ.ERROR_BUYER_CERTIFY_LEVEL_LIMIT"
	CodeACQExistForbiddenWord                    Code = "ACQ.EXIST_FORBIDDEN_WORD"
	CodeACQInvalidParameter                      Code = "ACQ.INVALID_PARAMETER"
	CodeACQInvalidReceiveAccount                 Code = "ACQ.INVALID_RECEIVE_ACCOUNT"
	CodeACQInvalidStoreID                        Code = "ACQ.INVALID_STORE_ID"
	CodeACQMerchantPermReceiptDayLimit           Code = "ACQ.MERCHANT_PERM_RECEIPT_DAY_LIMIT"
	CodeACQMerchantPermReceiptSingleLimit        Code = "ACQ.MERCHANT_PERM_RECEIPT_SINGLE_LIMIT"
	CodeACQMerchantPermReceiptSuspendLimit       Code = "ACQ.MERCHANT_PERM_RECEIPT_SUSPEND_LIMIT"
	CodeACQMerchantStatusNotNormal               Code = "ACQ.MERCHANT_STATUS_NOT_NORMAL"
	CodeACQNotAllowPartialRefund                 Code = "ACQ.NOT_ALLOW_PARTIAL_REFUND"
	CodeACQNowTimeAfterExpireTimeError           Code = "ACQ.NOW_TIME_AFTER_EXPIRE_TIME_ERROR"
	CodeACQOnlineTradeVoucherNotAllowRefund      Code = "ACQ.ONLINE_TRADE_VOUCHER_NOT_ALLOW_REFUND"
	CodeACQOverdraftAgreementNotMatch            Code = "ACQ.OVERDRAFT_AGREEMENT_NOT_MATCH"
	CodeACQOverdraftAssignAccountInvalid         Code = "ACQ.OVERDRAFT_ASSIGN_ACCOUNT_INVALID"
	CodeACQPartnerError                          Code = "ACQ.PARTNER_ERROR"
	CodeACQReasonIllegalStatus                   Code = "ACQ.REASON_ILLEGAL_STATUS"
	CodeACQReasonTradeBeenFreezen                Code = "ACQ.REASON_TRADE_BEEN_FREEZEN"
	CodeACQReasonTradeRefundFeeErr               Code = "ACQ.REASON_TRADE_REFUND_FEE_ERR"
	CodeACQReasonTradeStatusInvalid              Code = "ACQ.REASON_TRADE_STATUS_INVALID"
	CodeACQRefundallocUnauthLimit                Code = "ACQ.REFUNDALLOC_UNAUTH_LIMIT"
	CodeACQRefundAccountNotExist                 Code = "ACQ.REFUND_ACCOUNT_NOT_EXIST"
	CodeACQRefundAmtNotEqualTotal                Code = "ACQ.REFUND_AMT_NOT_EQUAL_TOTAL"
	CodeACQRefundChargeError                     Code = "ACQ.REFUND_CHARGE_ERROR"
	CodeACQRefundFeeError                        Code = "ACQ.REFUND_FEE_ERROR"
	CodeACQRefundRoyaltyPayeeAccountNotExist     Code = "ACQ.REFUND_ROYALTY_PAYEE_ACCOUNT_NOT_EXIST"
	CodeACQRiskMerchantIPNotExist                Code = "ACQ.RISK_MERCHANT_IP_NOT_EXIST"
	CodeACQSecondaryMerchantAlipayAccountInvalid Code = "ACQ.SECONDARY_MERCHANT_ALIPAY_ACCOUNT_INVALID"
	CodeACQSecondaryMerchantIDBlank              Code = "ACQ.SECONDARY_MERCHANT_ID_BLANK"
	CodeACQSecondaryMerchantIDInvalid            Code = "ACQ.SECONDARY_MERCHANT_ID_INVALID"
	CodeACQSecondaryMerchantISVPunishIndirect    Code = "ACQ.SECONDARY_MERCHANT_ISV_PUNISH_INDIRECT"
	CodeACQSecondaryMerchantNotMatch             Code = "ACQ.SECONDARY_MERCHANT_NOT_MATCH"
	CodeACQSecondaryMerchantStatusError          Code = "ACQ.SECONDARY_MERCHANT_STATUS_ERROR"
	CodeACQSellerBalanceNotEnough                Code = "ACQ.SELLER_BALANCE_NOT_ENOUGH"
	CodeACQSellerBeenBlocked                     Code = "ACQ.SELLER_BEEN_BLOCKED"
	CodeACQSellerNotExist                        Code = "ACQ.SELLER_NOT_EXIST"
	CodeACQSubGoodsSizeMaxCount                  Code = "ACQ.SUB_GOODS_SIZE_MAX_COUNT"
	CodeACQSystemError                           Code = "ACQ.SYSTEM_ERROR"
	CodeACQTotalFeeExceed                        Code = "ACQ.TOTAL_FEE_EXCEED"
	CodeACQTradeBuyerNotMatch                    Code = "ACQ.TRADE_BUYER_NOT_MATCH"
	CodeACQTradeHasClose                         Code = "ACQ.TRADE_HAS_CLOSE"
	CodeACQTradeHasFinished                      Code = "ACQ.TRADE_HAS_FINISHED"
	CodeACQTradeHasSuccess                       Code = "ACQ.TRADE_HAS_SUCCESS"
	CodeACQTradeNotAllowRefund                   Code = "ACQ.TRADE_NOT_ALLOW_REFUND"
	CodeACQTradeNotExist                         Code = "ACQ.TRADE_NOT_EXIST"
	CodeACQTradeSettleError                      Code = "ACQ.TRADE_SETTLE_ERROR"
	CodeACQTradeStatusError                      Code = "ACQ.TRADE_STATUS_ERROR"
	CodeACQUserNotMatchErr                       Code = "ACQ.USER_NOT_MATCH_ERR"
	CodeBillDateBeforeRegistration               Code = "BILL_DATE_BEFORE_REGISTRATION"

	// 线上实际返回 "isp.bill_not_exist"，本常量匹配不到（2026-09-04 生产商户账号实测）
	CodeBillNotExist Code = "BILL_NOT_EXIST"

	// 线上实际返回 "invalid_arguments"，本常量匹配不到（2026-09-04 生产商户账号实测）
	CodeInvailidArguments Code = "INVAILID_ARGUMENTS"

	CodeNoBillData       Code = "NO_BILL_DATA"
	CodeSystemRateLimit  Code = "SYSTEM_RATE_LIMIT"
	CodeTradeNotExist    Code = "TRADE_NOT_EXIST"
	CodeTypeNotSupported Code = "TYPE_NOT_SUPPORTED"
	CodeUnknownError     Code = "UNKNOWN_ERROR"
	CodeUserRateLimit    Code = "USER_RATE_LIMIT"
)
