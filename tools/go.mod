// 代码生成工具单独成一个 module。
//
// oapi-codegen 只在 make generate 时用得到，SDK 运行时一行都不碰它。可只要
// 它的 tool 指令写在主 go.mod 里，它那串依赖（kin-openapi 等）就会跟着躺进去：
// Dependabot 会对着它们报警，使用方做依赖审计也会看到一堆与支付无关的东西。
// 隔到这里之后，主 go.mod 是空的，"只依赖标准库"才名副其实。
module github.com/solarhell/alipay-go/tools

go 1.27

tool github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen

require (
	github.com/dprotaso/go-yit v0.0.0-20220510233725-9ba8df137936 // indirect
	github.com/getkin/kin-openapi v0.144.0 // indirect
	github.com/go-openapi/jsonpointer v0.23.1 // indirect
	github.com/go-openapi/swag/jsonname v0.26.0 // indirect
	github.com/oapi-codegen/oapi-codegen/v2 v2.8.0 // indirect
	github.com/oasdiff/yaml v0.1.1 // indirect
	github.com/oasdiff/yaml3 v0.0.14 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.2 // indirect
	github.com/speakeasy-api/jsonpath v0.6.3 // indirect
	github.com/speakeasy-api/openapi v1.24.0 // indirect
	github.com/vmware-labs/yaml-jsonpath v0.3.2 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/mod v0.38.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	golang.org/x/tools v0.48.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
