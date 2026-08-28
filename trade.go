package alipay

import (
	"context"
	"net/http"
	"net/url"

	"github.com/solarhell/alipay-go/internal/openapi"
)

// 请求与响应类型，字段和文档注释都来自支付宝官方 OpenAPI 规范。
//
// 这里用类型别名而不是重新定义：调用方看到的是短名字，godoc 又能直接跳到
// 生成的类型上，字段一个不少，规范更新后也不需要在这里同步一遍。
type (
	// PrecreateRequest 是统一收单线下交易预创建（扫码支付）的请求。
	PrecreateRequest = openapi.AlipayTradePrecreateModel
	// PrecreateResponse 是预创建的响应，QrCode 即用于生成付款二维码的码串。
	PrecreateResponse = openapi.AlipayTradePrecreateResponseModel

	// QueryRequest 是统一收单交易查询的请求。
	QueryRequest = openapi.AlipayTradeQueryModel
	// QueryResponse 是交易查询的响应。
	QueryResponse = openapi.AlipayTradeQueryResponseModel

	// RefundRequest 是统一收单交易退款的请求。
	RefundRequest = openapi.AlipayTradeRefundModel
	// RefundResponse 是交易退款的响应。
	RefundResponse = openapi.AlipayTradeRefundResponseModel

	// RefundQueryRequest 是统一收单交易退款查询的请求。
	RefundQueryRequest = openapi.AlipayTradeFastpayRefundQueryModel
	// RefundQueryResponse 是退款查询的响应。
	RefundQueryResponse = openapi.AlipayTradeFastpayRefundQueryResponseModel

	// CloseRequest 是统一收单交易关闭的请求。
	CloseRequest = openapi.AlipayTradeCloseModel
	// CloseResponse 是交易关闭的响应。
	CloseResponse = openapi.AlipayTradeCloseResponseModel

	// BillDownloadURLRequest 是查询对账单下载地址的请求。
	BillDownloadURLRequest = openapi.AlipayDataDataserviceBillDownloadurlQueryParams
	// BillDownloadURLResponse 是对账单下载地址的响应。
	BillDownloadURLResponse = openapi.AlipayDataDataserviceBillDownloadurlQueryResponseModel
)

// TradeStatus 是交易状态。
//
// 规范里 trade_status 只是个字符串，但这四个值的差别关系到钱到底收没收到，
// 所以在这里给出常量和 Paid 判断，而不是让每个调用方自己拼字符串。
type TradeStatus string

const (
	// TradeStatusWaitBuyerPay 交易创建，等待买家付款。
	TradeStatusWaitBuyerPay TradeStatus = "WAIT_BUYER_PAY"
	// TradeStatusClosed 未付款交易超时关闭，或支付完成后全额退款。
	TradeStatusClosed TradeStatus = "TRADE_CLOSED"
	// TradeStatusSuccess 交易支付成功。
	TradeStatusSuccess TradeStatus = "TRADE_SUCCESS"
	// TradeStatusFinished 交易结束，不可退款。
	TradeStatusFinished TradeStatus = "TRADE_FINISHED"
)

// Paid 报告这个交易状态是否意味着钱已经到账。
//
// 只有 TRADE_SUCCESS 和 TRADE_FINISHED 算收到钱。WAIT_BUYER_PAY 是还没付，
// TRADE_CLOSED 可能是超时关闭、也可能是付完又全额退了——把它们当成功处理就是
// 无条件发货。
func (s TradeStatus) Paid() bool {
	return s == TradeStatusSuccess || s == TradeStatusFinished
}

// Paid 报告响应或通知里的 trade_status 是否意味着钱已经到账。
//
// 接受指针是为了直接吃生成类型里的 *string 字段；字段缺失时返回 false——查不到
// 状态就不能当成已支付。
func Paid(tradeStatus *string) bool {
	if tradeStatus == nil {
		return false
	}
	return TradeStatus(*tradeStatus).Paid()
}

// Precreate 预创建交易，用于扫码支付：拿响应里的 QrCode 生成二维码给用户扫。
//
// 返回 *Error 且 Indeterminate 为真时，订单可能已经创建，此时应当用同一个
// out_trade_no 调 Query 确认，而不是换个订单号重下。
func (c *Client) Precreate(ctx context.Context, req *PrecreateRequest) (*PrecreateResponse, error) {
	var out PrecreateResponse
	if err := c.do(ctx, http.MethodPost, "/v3/alipay/trade/precreate", nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Query 查询交易状态。
//
// 它同时是异步通知的兜底手段：通知可能延迟、重复甚至丢失，最终发货与否应当以
// 这里查到的 trade_status 为准，用 Paid 判断。
//
// 注意 Precreate 之后立刻查会返回 ACQ.TRADE_NOT_EXIST：预下单只是生成二维码，
// 在有人扫码支付之前交易并不存在。这是正常的，不是错误。
func (c *Client) Query(ctx context.Context, req *QueryRequest) (*QueryResponse, error) {
	var out QueryResponse
	if err := c.do(ctx, http.MethodPost, "/v3/alipay/trade/query", nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Refund 发起退款。
//
// 退款依靠 out_request_no 幂等：同一笔退款重试时必须传同一个 out_request_no，
// 换一个就是再退一笔。返回 Indeterminate 的错误时用 RefundQuery 确认，不要直接重发。
func (c *Client) Refund(ctx context.Context, req *RefundRequest) (*RefundResponse, error) {
	var out RefundResponse
	if err := c.do(ctx, http.MethodPost, "/v3/alipay/trade/refund", nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RefundQuery 查询退款结果。
func (c *Client) RefundQuery(ctx context.Context, req *RefundQueryRequest) (*RefundQueryResponse, error) {
	var out RefundQueryResponse
	if err := c.do(ctx, http.MethodPost, "/v3/alipay/trade/fastpay/refund/query", nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Close 关闭尚未支付的交易。
//
// 交易已经支付时会返回 ACQ.TRADE_STATUS_ERROR 之类的错误，这属于正常结果，
// 不是失败——该走退款而不是关单。
func (c *Client) Close(ctx context.Context, req *CloseRequest) (*CloseResponse, error) {
	var out CloseResponse
	if err := c.do(ctx, http.MethodPost, "/v3/alipay/trade/close", nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// BillDownloadURL 查询对账单下载地址，用于日终对账。
//
// 账单有生成延迟：日账单一般次日 9 点前生成，月账单在次月 3 日前后，太早查会
// 拿到 NO_BILL_DATA 或 BILL_NOT_EXIST，稍后重试即可。
func (c *Client) BillDownloadURL(ctx context.Context, req *BillDownloadURLRequest) (*BillDownloadURLResponse, error) {
	query := url.Values{}
	if req != nil {
		if req.BillType != nil {
			query.Set("bill_type", *req.BillType)
		}
		if req.BillDate != nil {
			query.Set("bill_date", *req.BillDate)
		}
		if req.Smid != nil {
			query.Set("smid", *req.Smid)
		}
	}

	var out BillDownloadURLResponse
	if err := c.do(ctx, http.MethodGet, "/v3/alipay/data/dataservice/bill/downloadurl/query", query, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// String 返回状态的字符串形式。
func (s TradeStatus) String() string { return string(s) }
