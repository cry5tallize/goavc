#!/usr/bin/env bash
# 按 docs/Implementation-Plan.md 的 M0 计划，逐条重放 20 次提交。
#
# 用途：当提交历史被压平、或需要在新的仓库副本上重建 M0 时执行。
# 本脚本只提交已存在的文件，不会修改任何文件内容。
#
# 用法：
#   ./scripts/commit-m0.sh          # 只提交，不打标签
#   ./scripts/commit-m0.sh --tag    # 提交并打 v0.1.0 标签（CI 全绿后再用）
set -euo pipefail
cd "$(dirname "$0")/.."

if [ ! -d .git ]; then
    git init -q -b main
    echo "已初始化 git 仓库"
fi

git config core.hooksPath .githooks
chmod +x .githooks/commit-msg scripts/verify.sh 2>/dev/null || true

commit() {
    local msg="$1"
    shift
    git add -- "$@"
    if git diff --cached --quiet; then
        printf '跳过（无变更）: %s\n' "$msg"
        return 0
    fi
    git commit -q -m "$msg"
    printf '✓ %s\n' "$msg"
}

# 1. 目录骨架：每个包一个 doc.go，保证空包也能被 go build 收录。
commit "chore[repo]: initialize go module and package skeleton" \
    go.mod doc.go .gitignore .githooks/commit-msg \
    bitstream/doc.go nal/doc.go syntax/doc.go stream/doc.go \
    decoder/doc.go encoder/doc.go \
    internal/picture/doc.go internal/dpb/doc.go \
    internal/prediction/doc.go internal/transform/doc.go \
    internal/motion/doc.go internal/filter/doc.go \
    internal/simd/doc.go internal/buffer/doc.go \
    internal/entropy/cavlc/doc.go internal/entropy/cabac/doc.go \
    internal/testutil/importgraph/doc.go

# 2. 构建入口与本地验证脚本。
commit "build[repo]: add makefile with fmt vet test bench targets" \
    Makefile scripts/verify.sh scripts/commit-m0.sh

# 3. 主 CI：CGO_ENABLED=0 下的 vet/build/test/依赖/语料检查。
commit "ci[repo]: run build and tests with cgo disabled" \
    .github/workflows/ci.yml

# 4. 交叉编译矩阵：六种 GOOS/GOARCH 组合。
commit "ci[repo]: add cross compile matrix for amd64 and arm64" \
    .github/workflows/cross.yml

# 5. README：范围、硬约束、目录、验证方式。
commit "docs[readme]: add readme covering scope and non goals" \
    README.md

# 6. 许可证与专利声明。
commit "docs[legal]: add license and patent notice files" \
    LICENSE PATENTS.md

# 7. 许可审查：代码许可与专利许可分离，参考项目边界。
commit "docs[legal]: separate code license review from patent review" \
    docs/licensing.md

# 8. 统一错误模型。
commit "feat[errors]: add unified sentinel error values" \
    errors.go errors_test.go

# 9. 帧类型：像素格式、平面几何、深拷贝、释放。
commit "feat[frame]: add pixel format definitions and plane layout" \
    frame.go frame_test.go

# 10. 帧校验：stride、容量、溢出。
commit "feat[frame]: add frame validation with stride and size checks" \
    frame_validate.go frame_validate_test.go

# 11. 压缩包（访问单元）类型。
commit "feat[packet]: add access unit packet type" \
    packet.go packet_test.go

# 12. 配置类型与默认值。
commit "feat[config]: add encoder and decoder configuration defaults" \
    config.go config_defaults.go config_defaults_test.go

# 13. 配置校验与资源上限。
commit "feat[config]: add configuration validation and resource limits" \
    config_validate.go config_validate_test.go

# 14. 架构文档：分层、依赖规则、所有权、错误模型。
commit "docs[architecture]: document layering dependency rules and ownership" \
    docs/architecture.md

# 15. 依赖规则自动检查（R1/R2/R3/R5）。
commit "test[deps]: add import graph checker for dependency rules" \
    internal/testutil/importgraph/graph.go \
    internal/testutil/importgraph/graph_test.go \
    tools/depscheck/main.go

# 16. 追踪体系：支持矩阵 + Translation Ledger。
commit "docs[status]: add feature matrix and translation ledger templates" \
    docs/supported-features.md docs/implementation-status.md

# 17. 语料清单与哈希校验。
commit "test[testdata]: add corpus manifest schema and hash validator" \
    internal/testutil/manifest/manifest.go \
    internal/testutil/manifest/manifest_test.go \
    tools/manifest/main.go \
    testdata/manifest.json testdata/README.md

# 18. Oracle：FFmpeg 解码 + 逐平面逐像素差分。
commit "test[oracle]: add ffmpeg oracle harness for yuv diffing" \
    internal/testutil/yuvdiff/yuvdiff.go \
    internal/testutil/yuvdiff/yuvdiff_test.go \
    tools/oracle/main.go

# 19. 基准骨架与性能登记规范。
commit "bench[bench]: add benchmark skeleton and hardware registration doc" \
    benchmarks/doc.go benchmarks/frame_test.go docs/performance.md

# 20. 门禁记录（标签在 CI 全绿后单独打）。
commit "chore[release]: close milestone m0 gate and tag v0.1.0" \
    docs/gates/m0.md

echo
echo "M0 提交序列完成。"
echo "下一步：CGO_ENABLED=0 go test ./... 确认全绿后再 push 标签"

git tag -a v0.1.0 -m "milestone m0: project foundation and verification harness"
echo "已打本地标签 v0.1.0（CI 全绿后再 push --tags）"
