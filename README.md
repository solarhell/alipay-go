# alipay-go

支付宝 Go SDK，接口类型由**支付宝官方 OpenAPI v3 规范生成**，只依赖标准库。

[![CI](https://github.com/solarhell/alipay-go/actions/workflows/ci.yml/badge.svg)](https://github.com/solarhell/alipay-go/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/solarhell/alipay-go.svg)](https://pkg.go.dev/github.com/solarhell/alipay-go)

## 这个库和别的支付宝 Go SDK 有什么不同

- **类型来自官方规范，不是手抄。** 请求、响应、错误码全部由 [alipay-sdk-java-all](https://github.com/alipay/alipay-sdk-java-all) 里那份 openapi.yaml 生成——支付宝官方就是用它生成自家 Java / PHP / .NET 的 v3 SDK，唯独没有生成 Go。字段的中文说明和示例值也一并带了过来。
- **零第三方依赖。** 只用标准库，`go.mod` 里没有 require 任何东西。支付 SDK 的依赖面就是攻击面。
- **走 v3 接口。** 签名方式与 v1 完全不同，报文是标准 JSON，错误用 HTTP 状态码表达。
- **把"结果未知"当成一等公民。** 见下文。
- **连接池按单主机调过。** Go 默认 `MaxIdleConnsPerHost=2` 是为"跟很多主机通信"设计的，支付 SDK 整个进程只跟支付宝一台网关说话，并发一高就在重建 TLS 连接。默认 client 已调成 100/100 并显式开 HTTP/2（2026-09-07 对生产网关压测：640 rps、峰值并发 329、平均 301ms、零错误）；需要代理或别的策略用 `WithHTTPClient` 整体替换。

## 要求

Go 1.27 或更高。用到了 `new(表达式)`（Go 1.26）和 `encoding/json/v2`。

## 安装

```bash
go get github.com/solarhell/alipay-go
```

本仓库**不打 tag**：`go get` 会解析到 `main` 最新提交的伪版本（`v0.0.0-<时间>-<提交>`），
需要钉住某个提交时用 `go get github.com/solarhell/alipay-go@<commit>`。生成产物随代码
一起提交，规范版本由 `OPENAPI_VERSION` 钉住，升级规范是一次显式提交，不需要发布号。

## 快速开始

```go
client, err := alipay.New(
    appID,
    appPrivateKey,   // 应用私钥：PKCS#1 / PKCS#8 / 裸 base64 都认
    alipayPublicKey, // 支付宝公钥，不是你自己上传的应用公钥
)
if err != nil {
    return err
}

// 扫码支付预下单。字段是指针，Go 1.26 起可以直接 new(表达式)。
resp, err := client.Precreate(ctx, &alipay.PrecreateRequest{
    OutTradeNo:  new("T20260828001"),
    TotalAmount: new("0.01"),
    Subject:     new("测试商品"),
})
if err != nil {
    return err
}
fmt.Println(*resp.QrCode) // 拿这个码串生成二维码
```

更多用法见 [pkg.go.dev 上的示例](https://pkg.go.dev/github.com/solarhell/alipay-go#pkg-examples)。

## 错误处理

支付宝这里有一档 Stripe 那种设计里没有的语义：**结果未知**。下单请求超时，或者收到 `ACQ.SYSTEM_ERROR`，订单都可能已经在支付宝侧建好了——当失败重发就是重复扣款。

| 判断 | 含义 | 该怎么做 |
|---|---|---|
| `alipay.Indeterminate(err)` | 请求可能已生效 | **先用同一个订单号查询确认**，绝不能直接重发 |
| `alipay.Retryable(err)` | 明确被拒且无副作用（限流等） | 退避后原样重试 |
| `errors.Is(err, alipay.ErrSignature)` | 验签失败 | 报文不可信，不要采信其中的金额和状态 |
| 以上都不是 | 终态失败 | 按业务处理 |

```go
if alipay.Indeterminate(err) {
    // 查询真实状态，而不是重下一单
}
if alipay.IsCode(err, alipay.CodeACQTradeNotExist) {
    // 错误码是 Code 类型的常量，值就是支付宝原码
}
```

168 个错误码常量由规范生成，与官方原码一一对应，分两类：

- **业务码**（75 个，大写下划线，如 `ACQ.TRADE_NOT_EXIST`）：各接口自己的 `ErrorResponseModel`
- **网关公共码**（93 个，小写连字符，如 `app-call-limited`、`missing-timestamp`）：每个接口的 default 响应都可能返回，覆盖限流、签名、鉴权、网关未知错误等

注意 `ACQ.TRADE_NOT_EXIST`（交易域）和 `TRADE_NOT_EXIST`（账单域）是两个不同的码，分别对应 `CodeACQTradeNotExist` 和 `CodeTradeNotExist`；公共码 `invalid-parameter` 与业务码 `ACQ.INVALID_PARAMETER` 同理。

**限流是公共码** `app-call-limited`（应用级）/ `method-call-limited`（接口级），`Retryable()` 对它们为真；`unknow-error`（网关把请求转给了业务系统、业务系统没答上来）判为**结果未知**。

### ⚠️ 规范里的错误码枚举并非全部与线上一致

支付宝这份 OpenAPI 规范是官方维护的，但**账单域的错误码枚举与网关实际返回对不上**。2026-09-04 用生产商户账号实测：

| 场景 | 规范枚举 | 线上实际返回 |
|---|---|---|
| 账单不存在 | `BILL_NOT_EXIST` | **`isp.bill_not_exist`** |
| 入参不合法 | `INVAILID_ARGUMENTS` | **`invalid_arguments`** |

照规范里的常量判断会**漏判**。SDK 在 [`code_observed.go`](code_observed.go) 里提供了实测确认的取值，直接用它们：

```go
if alipay.IsCode(err, alipay.CodeBillNotExistObserved) {
    // 账单尚未生成，或该日无交易
}
```

生成的常量旁也标注了实测结论，IDE 里悬停即可看到。交易域（`ACQ.*`）目前未发现不一致，`BILL_DATE_BEFORE_REGISTRATION` 也与规范一致。

做资金相关判断前，建议对着真实环境验证一次错误码，不要只依赖规范。

## 异步通知

```go
n, err := client.ParseNotification(r) // 验签 + 核对 app_id
if err != nil {
    http.Error(w, "invalid", http.StatusBadRequest)
    return
}
if n.Paid() { // 只有 TRADE_SUCCESS / TRADE_FINISHED 算收到钱
    // 金额必须你自己核对：SDK 不知道这笔订单该是多少钱
    // 通知会重复送达，发货逻辑必须幂等
}
fmt.Fprint(w, alipay.NotifySuccess) // 处理成功后必须原样回 "success"
```

异步通知至今没有 v3 版本，仍走 v1 的参数排序验签，这些差异 SDK 内部消化了。

## 已生成的接口

| 方法 | 接口 |
|---|---|
| `Precreate` | alipay.trade.precreate |
| `Query` | alipay.trade.query |
| `Refund` | alipay.trade.refund |
| `RefundQuery` | alipay.trade.fastpay.refund.query |
| `Close` | alipay.trade.close |
| `BillDownloadURL` | alipay.data.dataservice.bill.downloadurl.query |

规范里有 739 个接口，这里只生成了扫码支付这条主链路。全量生成会产出几十万行绝大多数人用不到的代码。**需要别的接口，在 `scripts/operations.txt` 里加一行，重跑 `make generate` 即可**——类型、错误码都会自动补齐。

## 开发

```bash
make            # test + vet + gofmt + go fix 检查
make generate   # 从官方规范重新生成（联网）
```

生成产物已提交进仓库，使用这个库不需要任何工具链。`OPENAPI_VERSION` 钉住规范的 commit，升级规范是一次显式提交。

## 状态

签名实现依据支付宝官方仓库中的 [Postman 签名脚本](https://github.com/alipay/alipay-sdk-php-all/blob/master/v3/script/postman_script.js)，待签串的确切字节有测试钉死，并已在**支付宝沙箱完成端到端联调**：

- `alipay.trade.precreate` 返回真实二维码码串——请求签名被支付宝接受，响应验签通过
- `alipay.data.dataservice.bill.downloadurl.query` 调用成功——覆盖 GET 这条独立的签名路径（报文体为空，待签串形状不同）
- 错误响应正确解析成 `*Error`，错误码、中文描述、trace id 都对得上

异步通知已在**生产环境用真实交易验证**（2026-09-04，一笔 0.01 元支付及其退款）：支付通知与退款通知都经 `ParseNotification` 验签解析后正确入账/退款。单元测试另覆盖篡改金额、篡改状态、追加参数、换用公钥等必须被拒的情形。

### 已知未实测项

- **限流码尚无线上样本。** 限流是网关公共码 `app-call-limited` / `method-call-limited`（规范 `CommonErrorType`，v1 写法 `isv.app-call-limited` / `isv.method-call-limited`）。这张表的写法已由线上验证——未签名请求返回的 `missing-timestamp` 就在其中——但限流本身没触发过：2026-09-07 用生产商户凭证对账单下载地址查询压到 **640 rps、2 分钟 36000 次**，全程只返回 `isp.bill_not_exist`。因此这两个码的取值只有规范背书，伴随的 HTTP 状态码也未知；`Retryable()` 同时兜底 HTTP 429。
- **证书模式**（`WithAppCertSN`）未经真实联调，本 SDK 的生产使用方走公钥模式。

## License

MIT
