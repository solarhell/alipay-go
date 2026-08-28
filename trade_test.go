package alipay

import (
	"context"
	"io"
	"net/http"
	"testing"
)

func ptr[T any](v T) *T { return &v }

// TestEndpoints 钉住每个方法实际打到的 HTTP 方法与路径。
//
// 路径或方法写错，编译器不会有任何意见，支付宝只会回一个 404 或验签失败，
// 而这些都要等到联调时才暴露。
func TestEndpoints(t *testing.T) {
	tests := []struct {
		name       string
		call       func(context.Context, *Client) error
		wantMethod string
		wantPath   string
		wantQuery  string
	}{
		{
			name: "Precreate",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.Precreate(ctx, &PrecreateRequest{
					OutTradeNo: ptr("T1"), TotalAmount: ptr("0.01"), Subject: ptr("测试"),
				})
				return err
			},
			wantMethod: http.MethodPost,
			wantPath:   "/v3/alipay/trade/precreate",
		},
		{
			name: "Query",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.Query(ctx, &QueryRequest{OutTradeNo: ptr("T1")})
				return err
			},
			wantMethod: http.MethodPost,
			wantPath:   "/v3/alipay/trade/query",
		},
		{
			name: "Refund",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.Refund(ctx, &RefundRequest{
					OutTradeNo: ptr("T1"), RefundAmount: ptr("0.01"), OutRequestNo: ptr("R1"),
				})
				return err
			},
			wantMethod: http.MethodPost,
			wantPath:   "/v3/alipay/trade/refund",
		},
		{
			name: "RefundQuery",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.RefundQuery(ctx, &RefundQueryRequest{OutTradeNo: ptr("T1"), OutRequestNo: ptr("R1")})
				return err
			},
			wantMethod: http.MethodPost,
			wantPath:   "/v3/alipay/trade/fastpay/refund/query",
		},
		{
			name: "Close",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.Close(ctx, &CloseRequest{OutTradeNo: ptr("T1")})
				return err
			},
			wantMethod: http.MethodPost,
			wantPath:   "/v3/alipay/trade/close",
		},
		{
			// 账单接口是唯一走 GET 的，参数在 query 上，签名内容也因此不同。
			name: "BillDownloadURL",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.BillDownloadURL(ctx, &BillDownloadURLRequest{
					BillType: ptr("trade"), BillDate: ptr("2026-08-01"),
				})
				return err
			},
			wantMethod: http.MethodGet,
			wantPath:   "/v3/alipay/data/dataservice/bill/downloadurl/query",
			wantQuery:  "bill_date=2026-08-01&bill_type=trade",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotMethod, gotPath, gotQuery string
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath, gotQuery = r.Method, r.URL.Path, r.URL.RawQuery
				signResponse(t, w, alipayKey(t), `{}`)
				io.WriteString(w, `{}`)
			})

			if err := tt.call(context.Background(), c); err != nil {
				t.Fatalf("调用失败: %v", err)
			}
			if gotMethod != tt.wantMethod {
				t.Errorf("HTTP 方法 = %s, 期望 %s", gotMethod, tt.wantMethod)
			}
			if gotPath != tt.wantPath {
				t.Errorf("路径 = %s, 期望 %s", gotPath, tt.wantPath)
			}
			if gotQuery != tt.wantQuery {
				t.Errorf("query = %q, 期望 %q", gotQuery, tt.wantQuery)
			}
		})
	}
}

// TestBillDownloadURLOmitsEmptyParams 确认可选参数缺失时不会拼出空的 query 项，
// 因为 query 会逐字节进入签名内容，多一个 smid= 就会验签失败。
func TestBillDownloadURLOmitsEmptyParams(t *testing.T) {
	var gotQuery string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		signResponse(t, w, alipayKey(t), `{}`)
		io.WriteString(w, `{}`)
	})

	if _, err := c.BillDownloadURL(context.Background(), &BillDownloadURLRequest{BillType: ptr("trade")}); err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if gotQuery != "bill_type=trade" {
		t.Errorf("query = %q, 期望 %q", gotQuery, "bill_type=trade")
	}
}
