# Pure Go H.264/AVC 编解码器 —— 构建入口
#
# 常规目标都在 CGO_ENABLED=0 下执行，阻止库引入 cgo 运行时依赖。
# race 是开发期工具链例外：Go 的竞态检测器要求 CGO_ENABLED=1 和 C 编译器；
# 它不改变常规构建门禁，也不允许项目源码导入 C 或外部编解码库。
#
# 注意：Makefile 的 recipe 行必须以 TAB 缩进，改成空格会直接报
# "missing separator"。编辑本文件时请勿让编辑器自动展开 TAB。

GO      ?= go
GOFLAGS ?=
CGO_ENABLED := 0
export CGO_ENABLED

.PHONY: all verify build test race bench vet fmt fmt-check deps manifest cross clean help

all: verify

## verify: 提交前必跑的完整检查（不修改任何文件）
verify: fmt-check vet build test deps manifest
	@echo "verify: 全部通过"

## build: 编译所有包（含 tools/ 与 benchmarks/）
build:
	$(GO) build ./...

## test: 单元测试
test:
	$(GO) test ./...

## race: 开发期竞态检测（M3 起为门禁项，需要本机 C 编译器）
race:
	CGO_ENABLED=1 $(GO) test -race ./...

## bench: 基准测试（需要可复现的硬件与空闲机器）
bench:
	$(GO) test -run '^$$' -bench . -benchmem ./benchmarks ./internal/...

## vet: 静态检查
vet:
	$(GO) vet ./...

## fmt: 就地格式化（提交前执行一次即可）
fmt:
	gofmt -w .

## fmt-check: 只检查不修改，用于 PR 前的格式门禁
fmt-check:
	@out="$$(gofmt -l .)"; \
	if [ -n "$$out" ]; then \
		echo "以下文件需要 gofmt："; echo "$$out"; exit 1; \
	fi
	@echo "fmt-check: 通过"

## deps: 依赖方向检查（R1/R2/R3/R5）
deps:
	$(GO) run ./tools/depscheck

## manifest: 语料清单与哈希校验
manifest:
	$(GO) run ./tools/manifest -file testdata/manifest.json

## cross: 交叉编译六种目标组合（仅验证可编译，不代表可运行）
cross:
	GOOS=windows GOARCH=amd64 $(GO) build ./...
	GOOS=windows GOARCH=arm64 $(GO) build ./...
	GOOS=linux   GOARCH=amd64 $(GO) build ./...
	GOOS=linux   GOARCH=arm64 $(GO) build ./...
	GOOS=darwin  GOARCH=amd64 $(GO) build ./...
	GOOS=darwin  GOARCH=arm64 $(GO) build ./...

## clean: 清理构建产物
clean:
	$(GO) clean ./...
	rm -rf out bin

## help: 列出所有目标
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //'
