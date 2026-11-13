#!/usr/bin/env bash
# 提交前验证脚本：与 Makefile 的 verify 目标等价，供没有 make 的环境使用。
#
# 用法：./scripts/verify.sh
# 退出码：0 通过，非 0 表示有检查项失败（失败项会逐条列出）。
set -uo pipefail

cd "$(dirname "$0")/.." || exit 1
export CGO_ENABLED=0

fail=0
run() {
    local name="$1"
    shift
    printf '==> %s\n' "$name"
    if ! "$@"; then
        printf '!!! %s 失败\n' "$name" >&2
        fail=1
    fi
}

run "vet"      go vet ./...
run "build"    go build ./...
run "test"     go test ./...
run "deps"     go run ./tools/depscheck
run "manifest" go run ./tools/manifest -file testdata/manifest.json

# 格式检查是门禁的一部分：M0 的已知偏差要求在 M1 门禁前转为阻断。
# 没有 gofmt 的环境视为无法完成检查，直接报失败而不是静默通过。
if command -v gofmt >/dev/null 2>&1; then
    out="$(gofmt -l . 2>/dev/null)"
    if [ -n "$out" ]; then
        printf '!!! 以下文件未 gofmt（执行 make fmt 修复）：\n%s\n' "$out" >&2
        fail=1
    fi
else
    printf '!!! 找不到 gofmt，无法执行格式门禁\n' >&2
    fail=1
fi

if [ "$fail" -ne 0 ]; then
    echo "verify: 存在失败项" >&2
    exit 1
fi
echo "verify: 全部通过"
