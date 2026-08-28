package alipay_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/solarhell/alipay-go"
)

func ExampleNew() {
	client, err := alipay.New(
		os.Getenv("ALIPAY_APP_ID"),
		os.Getenv("ALIPAY_PRIVATE_KEY"), // 应用私钥
		os.Getenv("ALIPAY_PUBLIC_KEY"),  // 支付宝公钥，不是你自己的应用公钥
		alipay.WithSandbox(),            // 生产环境去掉这行
	)
	if err != nil {
		log.Fatal(err)
	}
	_ = client
}

// 扫码支付：预下单拿到二维码码串。
func ExampleClient_Precreate() {
	var client *alipay.Client // 见 ExampleNew

	// 字段都是指针，Go 1.26 起可以直接用 new(表达式) 取地址。
	resp, err := client.Precreate(context.Background(), &alipay.PrecreateRequest{
		OutTradeNo:  new("T20260828001"), // 商户订单号，全局唯一
		TotalAmount: new("0.01"),         // 单位元，字符串避免浮点误差
		Subject:     new("测试商品"),
	})
	if err != nil {
		// 结果未知时订单可能已经建好了，换个订单号重下会变成两笔。
		if alipay.Indeterminate(err) {
			log.Printf("下单结果未知，改用同一订单号查询确认: %v", err)
			return
		}
		log.Fatal(err)
	}

	fmt.Println("二维码码串:", *resp.QrCode)
}

// 主动查询，兼作异步通知的兜底。
func ExampleClient_Query() {
	var client *alipay.Client

	resp, err := client.Query(context.Background(), &alipay.QueryRequest{
		OutTradeNo: new("T20260828001"),
	})
	if err != nil {
		// 订单还没被支付宝创建时会返回这个码，属于正常情况。
		if alipay.IsCode(err, alipay.CodeACQTradeNotExist) {
			fmt.Println("交易尚未创建")
			return
		}
		log.Fatal(err)
	}

	// 只有 TRADE_SUCCESS / TRADE_FINISHED 算收到钱。
	if alipay.Paid(resp.TradeStatus) {
		fmt.Println("已支付，金额:", *resp.TotalAmount)
	}
}

// 处理异步通知。
func ExampleClient_ParseNotification() {
	var client *alipay.Client

	http.HandleFunc("/alipay/notify", func(w http.ResponseWriter, r *http.Request) {
		// 验签，并核对 app_id 是本应用。
		n, err := client.ParseNotification(r)
		if err != nil {
			// 验签不过绝不能采信报文，也不要回 success。
			http.Error(w, "invalid", http.StatusBadRequest)
			return
		}

		if !n.Paid() {
			// 支付宝会为交易的多个阶段发通知，这里只是还没付。
			fmt.Fprint(w, alipay.NotifySuccess)
			return
		}

		// SDK 不知道这笔订单该是多少钱，金额必须由你自己核对。
		order, err := loadOrder(n.OutTradeNo())
		if err != nil || order.Amount != n.TotalAmount() {
			http.Error(w, "amount mismatch", http.StatusBadRequest)
			return
		}

		// 通知会重复送达，发货逻辑必须幂等。
		if err := deliverOnce(order); err != nil {
			// 没处理成功就别回 success，支付宝会重投。
			http.Error(w, "retry later", http.StatusInternalServerError)
			return
		}

		// 处理成功后必须原样回这七个字节，否则支付宝会在 24 小时内反复重投。
		fmt.Fprint(w, alipay.NotifySuccess)
	})
}

// 退款：靠 out_request_no 幂等。
func ExampleClient_Refund() {
	var client *alipay.Client

	req := &alipay.RefundRequest{
		OutTradeNo:   new("T20260828001"),
		RefundAmount: new("0.01"),
		// 同一笔退款重试时必须传同一个 out_request_no，换一个就是再退一笔。
		OutRequestNo: new("R20260828001"),
	}

	_, err := client.Refund(context.Background(), req)
	if alipay.Indeterminate(err) {
		// 不要直接重发，先查这笔退款到底成没成。
		time.Sleep(2 * time.Second)
		if _, qerr := client.RefundQuery(context.Background(), &alipay.RefundQueryRequest{
			OutTradeNo:   req.OutTradeNo,
			OutRequestNo: req.OutRequestNo,
		}); qerr != nil {
			log.Printf("退款结果未知且查询失败，需人工核对: %v", qerr)
		}
		return
	}
	if err != nil {
		log.Fatal(err)
	}
}

// 错误处理的三种分支。
func Example_errorHandling() {
	var client *alipay.Client

	_, err := client.Query(context.Background(), &alipay.QueryRequest{OutTradeNo: new("T1")})

	switch {
	case err == nil:
		// 成功

	case alipay.Indeterminate(err):
		// 结果未知：请求可能已经生效。写操作必须查询确认后再决定，绝不能当失败重发。

	case alipay.Retryable(err):
		// 明确被拒且没有副作用（限流等），退避后原样重试是安全的。

	case errors.Is(err, alipay.ErrSignature):
		// 验签失败：报文不可信，不要采信其中的金额和状态。

	default:
		// 终态失败，按业务处理。
		if e, ok := errors.AsType[*alipay.Error](err); ok {
			log.Printf("错误码 %s: %s (trace %s)", e.Code, e.Message, e.RequestID)
		}
	}
}

type order struct{ Amount string }

func loadOrder(outTradeNo string) (*order, error) { return nil, nil }
func deliverOnce(o *order) error                  { return nil }
