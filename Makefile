.PHONY: all
all: generate test vet lint check-gofmt

# 从官方 OpenAPI 规范重新生成类型与错误码。生成物已提交进仓库，使用者不需要
# 跑这一步，也不需要装任何工具链。
.PHONY: generate
generate:
	./scripts/generate.sh

.PHONY: test
test:
	go test -race -cover ./...

.PHONY: vet
vet:
	go vet ./...

.PHONY: lint
lint:
	go tool staticcheck ./... 2>/dev/null || echo "staticcheck 未安装，跳过"

.PHONY: check-gofmt
check-gofmt:
	@files=$$(gofmt -l .); \
	if [ -n "$$files" ]; then echo "以下文件未格式化："; echo "$$files"; exit 1; fi
