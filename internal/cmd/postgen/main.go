// Command postgen 对 oapi-codegen 的产物做两步加工。
//
// 一、抹平错误码。oapi-codegen 按接口各生成一套错误码类型
// （AlipayTradeCloseErrorResponseModelCode、AlipayTradeQueryErrorResponseModelCode ...），
// 于是同一个 "ACQ.SYSTEM_ERROR" 在六个接口里是六个互不相等的常量，调用方
// 写不出 `if err.Code == alipay.CodeACQSystemError` 这种最基本的判断。本工具
// 把它们抹平成一个全局 Code 类型，值仍是支付宝官方原码，一个字都没改。
//
// 二、剪掉死代码，见 prune.go。
//
// 之所以解析已生成的 Go 代码而不是直接解析 3.6MB 的 YAML：错误码集合天然
// 跟着 scripts/operations.txt 的白名单走——白名单加一个接口，重跑 make generate，
// 它的错误码自动进来，两处不会走散。而且只用标准库，不给 SDK 添依赖。
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
)

func main() {
	log.SetFlags(0)
	in := flag.String("in", "internal/openapi/openapi.gen.go", "生成的 OpenAPI 源文件")
	out := flag.String("out", "code_gen.go", "错误码输出文件")
	pkg := flag.String("package", "alipay", "错误码输出文件的包名")
	flag.Parse()

	src, err := os.ReadFile(*in)
	if err != nil {
		log.Fatalf("读取 %s: %v", *in, err)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, *in, src, parser.ParseComments)
	if err != nil {
		log.Fatalf("解析 %s: %v", *in, err)
	}

	codes := extractCodes(f)
	if len(codes) == 0 {
		log.Fatalf("在 %s 中没有找到任何错误码，生成器的假设可能已经失效", *in)
	}
	gen, err := render(*pkg, *in, codes)
	if err != nil {
		log.Fatalf("生成错误码: %v", err)
	}
	if err := writeFile(*out, gen); err != nil {
		log.Fatalf("写入 %s: %v", *out, err)
	}
	fmt.Printf("    %d 个错误码 -> %s\n", len(codes), *out)

	pruned, removed, err := prune(f, fset, src)
	if err != nil {
		log.Fatalf("剪枝: %v", err)
	}
	if err := writeFile(*in, pruned); err != nil {
		log.Fatalf("写入 %s: %v", *in, err)
	}
	fmt.Printf("    剪掉 %d 个未使用的 DefaultResponse 类型\n", removed)
}

// extractCodes 收集所有 *ErrorResponseModelCode 类型常量的字面值并去重。
func extractCodes(f *ast.File) []string {
	seen := map[string]bool{}
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			ident, ok := vs.Type.(*ast.Ident)
			// 两类枚举：各接口自己的 <Op>ErrorResponseModelCode（业务码），以及每个接口
			// default 响应都引用的 CommonErrorTypeCode（网关公共码：限流、签名、鉴权、
			// 网关未知错误……）。后者曾被漏掉——2026-09-07 才发现限流码 app-call-limited /
			// method-call-limited 就在这张表里，而此前的实测与检索都没找到它。
			if !ok || !(strings.HasSuffix(ident.Name, "ErrorResponseModelCode") || ident.Name == "CommonErrorTypeCode") {
				continue
			}
			for _, v := range vs.Values {
				lit, ok := v.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				s, err := strconv.Unquote(lit.Value)
				if err != nil {
					continue
				}
				seen[s] = true
			}
		}
	}

	codes := make([]string, 0, len(seen))
	for c := range seen {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	return codes
}

// knownMismatches 记录规范枚举值与线上实际返回不一致的错误码。
//
// 支付宝这份 OpenAPI 规范是官方维护的，但账单域的错误码枚举与网关实际返回对
// 不上——照着常量判断会漏判。这里把实测结论直接写进生成的代码，让拿到常量的
// 人当场看见，而不是等线上出了问题再去翻文档。
//
// 发现新的不一致就往这里加一行，重跑 make generate 即可。
var knownMismatches = map[string]string{
	"BILL_NOT_EXIST":     `线上实际返回 "isp.bill_not_exist"，本常量匹配不到（2026-09-04 生产商户账号实测）`,
	"INVAILID_ARGUMENTS": `线上实际返回 "invalid_arguments"，本常量匹配不到（2026-09-04 生产商户账号实测）`,
}

// initialisms 保持全大写的词。ACQ 是支付宝收单域的官方前缀，留着方便和
// 官方文档对照；其余是 Go 惯例中的首字母缩略词。
var initialisms = map[string]string{
	"ACQ": "ACQ", "ID": "ID", "IP": "IP", "ISV": "ISV", "PC": "PC", "URL": "URL",
}

// constName 把官方错误码转成 Go 常量名，不做任何语义改写。
//
//	ACQ.TRADE_NOT_EXIST -> CodeACQTradeNotExist
//	TRADE_NOT_EXIST     -> CodeTradeNotExist
//	app-call-limited    -> CodeAppCallLimited   （公共码是小写连字符风格）
//
// 这两个码在 spec 里同时存在（后者来自账单下载接口），所以域前缀必须保留，
// 否则会撞名——也正因如此，常量名一律与原码一一对应，不合并、不简写。
func constName(code string) string {
	var b strings.Builder
	b.WriteString("Code")
	for _, word := range strings.FieldsFunc(code, func(r rune) bool { return r == '.' || r == '_' || r == '-' || r == ',' || r == ' ' }) {
		if up, ok := initialisms[word]; ok {
			b.WriteString(up)
			continue
		}
		b.WriteString(strings.ToUpper(word[:1]) + strings.ToLower(word[1:]))
	}
	return b.String()
}

func render(pkg, src string, codes []string) ([]byte, error) {
	var b bytes.Buffer
	fmt.Fprintf(&b, `// 由 internal/cmd/gencodes 依据 %s 生成，请勿手工编辑。
// 重新生成：make generate

package %s

// Code 是支付宝返回的业务错误码。
//
// 常量取自官方 OpenAPI 规范中各接口 ErrorResponseModel 的 code 枚举，值即
// 官方原码；未收录的码同样会被解析出来，直接比较字符串即可，不必等 SDK 更新。
//
// 注意：规范里的枚举值并非全部与线上一致。账单域已确认有出入，相关常量下方
// 标注了实测结论。交易域（ACQ.*）目前未发现不一致。做资金相关判断前，建议对
// 着真实环境验证一次错误码，不要只依赖本文件。
//
// 常量分两类：各接口自己的业务码（如 ACQ.TRADE_NOT_EXIST，大写下划线），以及每个
// 接口的 default 响应都会返回的网关公共码（如 app-call-limited，小写连字符）。
// 公共码的写法已由线上验证：未签名请求返回的 missing-timestamp 就在这张表里。
//
// 限流是公共码 app-call-limited（应用调用次数/频率超限）与 method-call-limited
// （单个接口调用次数/频率超限），不是账单域业务码 SYSTEM_RATE_LIMIT / USER_RATE_LIMIT
// ——那两个的线上写法与规范不一致（见上文），且从未在实测中出现。2026-09-07 对生产
// 网关压到 640 rps 也没触发限流，所以这两个公共码的取值只有规范背书，尚无线上样本。
type Code string

// 错误码常量，按官方原码字典序排列。
const (
`, src, pkg)

	names := make(map[string]string, len(codes))
	for _, c := range codes {
		n := constName(c)
		if prev, dup := names[n]; dup {
			return nil, fmt.Errorf("常量名冲突：%q 与 %q 都映射到 %s", prev, c, n)
		}
		names[n] = c
	}

	width := 0
	for n := range names {
		if len(n) > width {
			width = len(n)
		}
	}
	for _, c := range codes {
		if note, ok := knownMismatches[c]; ok {
			fmt.Fprintf(&b, "\n\t// %s\n", note)
			fmt.Fprintf(&b, "\t%s Code = %q\n\n", constName(c), c)
			continue
		}
		fmt.Fprintf(&b, "\t%-*s Code = %q\n", width, constName(c), c)
	}
	b.WriteString(")\n")
	return b.Bytes(), nil
}
