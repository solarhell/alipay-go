#!/usr/bin/env bash
#
# 从支付宝官方 OpenAPI 规范生成类型与 HTTP 客户端。
#
# 规范来自 alipay/alipay-sdk-java-all 仓库的 v3/api/openapi.yaml —— 支付宝官方
# 就是用这一份规范生成了自家的 Java / PHP / .NET v3 SDK（见该仓库的
# .openapi-generator/ 目录），唯独没有生成 Go。本脚本补上这一档。
#
# 钉住 commit 而不是取 master：规范一变，生成结果就变；OPENAPI_VERSION 记录
# 当前产物基于哪个版本，升级是一次显式提交，而不是某天 CI 悄悄换掉了类型定义。
#
set -euo pipefail

cd "$(dirname "$0")/.."

SPEC_COMMIT="$(cat OPENAPI_VERSION)"
SPEC_URL="https://raw.githubusercontent.com/alipay/alipay-sdk-java-all/${SPEC_COMMIT}/v3/api/openapi.yaml"
SPEC_FILE="$(mktemp -t alipay-openapi.XXXXXX.yaml)"
trap 'rm -f "$SPEC_FILE"' EXIT

# 生成的接口白名单，忽略注释行与空行
OPERATIONS="$(grep -vE '^\s*(#|$)' scripts/operations.txt | paste -sd, -)"

echo "==> 拉取规范 ${SPEC_COMMIT:0:12}"
curl -fsSL --retry 3 "$SPEC_URL" -o "$SPEC_FILE"
echo "    $(wc -l < "$SPEC_FILE" | tr -d ' ') 行"

# 只生成 types：v3 的签名必须在 HTTP 层逐个请求地做（见 sign.go），生成的
# client 不知道这回事，用不上；而少生成它就少一个 oapi-codegen/runtime 依赖，
# 本 SDK 得以只依赖标准库。
echo "==> 生成 internal/openapi/openapi.gen.go"
echo "    接口: $(echo "$OPERATIONS" | tr ',' '\n' | wc -l | tr -d ' ') 个"
go tool oapi-codegen \
  -generate types \
  -package openapi \
  -include-operation-ids "$OPERATIONS" \
  -o internal/openapi/openapi.gen.go \
  "$SPEC_FILE"

echo "    $(wc -l < internal/openapi/openapi.gen.go | tr -d ' ') 行"

echo "==> 后处理"
go run ./internal/cmd/postgen

gofmt -w internal/openapi/openapi.gen.go code_gen.go
echo "==> 完成"
