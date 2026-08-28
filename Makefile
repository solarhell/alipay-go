.PHONY: all
all: test vet check-gofmt check-fix

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

.PHONY: check-gofmt
check-gofmt:
	@files=$$(gofmt -l .); \
	if [ -n "$$files" ]; then echo "以下文件未格式化，运行 gofmt -w ."; echo "$$files"; exit 1; fi

# go fix 会把过时写法改成当前版本的写法（比如 errors.As 改 errors.AsType）。
# 它直接改文件，所以这里跑完再看有没有留下改动。
.PHONY: check-fix
check-fix:
	@if ! git diff --quiet; then \
		echo "工作区有未提交改动，check-fix 需要干净的工作区才能分辨是不是 go fix 改的"; exit 1; \
	fi
	@go fix ./...
	@if ! git diff --quiet; then \
		echo "go fix 改动了以下文件，请检查并提交："; git diff --stat; exit 1; \
	fi

# 确认提交的生成产物与 OPENAPI_VERSION 指向的规范一致。
.PHONY: check-generate
check-generate:
	@if ! git diff --quiet; then \
		echo "工作区有未提交改动，check-generate 需要干净的工作区"; exit 1; \
	fi
	@$(MAKE) generate
	@if ! git diff --quiet; then \
		echo "生成产物与规范不一致，请运行 make generate 并提交："; git diff --stat; exit 1; \
	fi
