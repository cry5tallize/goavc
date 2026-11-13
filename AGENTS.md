# 仓库工作指引

## 项目与进度

goavc 是从零实现的纯 Go H.264/AVC 编解码库，模块为 `github.com/goavc/h264`，根包名为 `h264`。`go.mod` 要求 Go 1.27，CI 使用 1.27.x；代码需兼容该版本。

开始实现前阅读相关文档：

- `docs/Roadmap.md`：里程碑范围、依赖与退出门禁。
- `docs/Implementation-Plan.md`：实施顺序、提交与命名规范。
- `docs/architecture.md`：依赖方向、所有权、错误与并发契约。
- `docs/implementation-status.md` 与 `docs/supported-features.md`：实现状态、证据与具体缺口。

以实际代码、测试和验证证据核对进度，不沿用过时的占位阶段描述。存在实现不等于已通过里程碑门禁，不得据此宣称完整支持某个 Profile。先检查 `git status --short`，保留已有未提交工作。

## 目录与依赖

- 根目录：公共 `Frame`、`Plane`、`Packet`、配置和哨兵错误。
- `bitstream/`：位读写、Exp-Golomb、RBSP/EBSP。
- `nal/`：NAL、Annex-B、AVCC、avcC 与访问单元组装。
- `syntax/`：SPS/PPS、VUI、ScalingList、SEI 和 Slice Header。
- `decoder/`、`encoder/`、`stream/`：流水线与 I/O 适配。
- `internal/`：共享图像、预测、变换、运动、去块、熵编码、DPB、缓冲和 SIMD 内核。
- `internal/testutil/`、`tools/`：依赖检查、语料清单与 Oracle 工具；`benchmarks/` 为基准，`testdata/` 为语料。

遵守架构 R1–R6：编码器与解码器不得相互导入；`internal/*` 不得导入流水线包；`internal/simd` 的调用者必须在 `internal/testutil/importgraph/graph.go` 白名单内；公共 API 不暴露内部类型；禁止导入环；单文件建议不超过 800 行有效代码。用 `tools/depscheck` 检查 R1/R2/R3/R5。

预测、变换、反量化、重建、滤波和图像缓冲只保留一份共享实现，编码器本地重建必须调用解码器使用的同一内核。

## 构建与验证

所有常规 Go 构建、测试和工具运行均设置 `CGO_ENABLED=0`。运行时仅使用标准库，v1.0 前 `go.mod` 不添加 `require`；禁止 CGO、C/C++ 源码和外部编解码库。FFmpeg/ffprobe 仅供开发期 Oracle 使用。

有 Make 和 POSIX shell 时，完整门禁为 `make verify`；无 Make 时使用 Git Bash 执行 `bash scripts/verify.sh`。门禁包含 gofmt 检查、vet、build、test、依赖检查与语料清单校验。

Windows PowerShell 可直接执行对应命令，检查每一步退出码：

```powershell
$env:CGO_ENABLED = '0'
gofmt -l .
go vet ./...
go build ./...
go test ./...
go run ./tools/depscheck
go run ./tools/manifest -file testdata/manifest.json
```

`gofmt -l .` 应无输出。仅格式化本次修改的 Go 文件，避免改动已有工作。Makefile recipe 必须保留 TAB 缩进。

开发时先运行相关包或用例，例如 `go test -run TestCAVLC -v ./internal/entropy/cavlc`；代码变更完成后执行完整门禁。文档改动核对内容与引用即可；报告实际执行结果及未完成项，不把跳过项算作通过。`make cross` 编译 Windows/Linux/macOS 的 amd64/arm64 六种组合，只证明可编译，不证明原生执行或 SIMD 正确。

性能变更需提供同环境前后基准，参见 `docs/performance.md`；可用 `make bench`。并行实现须与串行输出逐比特一致并通过竞态检测；当前 `make race` 继承 `CGO_ENABLED=0`，使用前需处理 Go race 工具链对 CGO 的要求，不得把未执行的检测报告为通过。

## 实现约束

- 正确性优先，先完成标量实现并验证，再优化。汇编仅放在 `internal/simd`，每个内核须有纯 Go 回退及逐元素一致性测试，覆盖奇数尺寸、非对齐 stride 和极值；不得强制要求 AVX2。
- 区分显示尺寸与编码尺寸：公共帧宽高可为奇数，平面尺寸向上取整；内部图像按 16 对齐并复制边缘，输出按 SPS 裁剪。
- 来自码流的尺寸、计数、索引和长度先校验再分配或访问；尺寸运算必须检查溢出并遵守配置的资源上限。
- 用 `errors.Is` 判断错误，用 `%w` 包装上下文。合法但未实现的工具返回 `ErrUnsupported`；损坏数据返回 `ErrInvalidData`；超限返回 `ErrResourceLimit`。`ErrAgain` 是流控，`ErrEOF` 对应 `io.EOF`。禁止静默降级或伪支持。
- 按实际 API 注释与架构维护缓冲所有权：`SendPacket` 返回后不得保留调用方的 `Data` 引用；接收帧用完调用幂等的 `Release()`，需长期保存时 `Clone()`。涉及所有权变更时同时核对实现与契约。
- 编解码实例非并发安全；多路视频使用独立实例。按路线图推进，超出当前实现范围的能力显式拒绝。

## 代码、测试与文档

注释与文档使用中文，说明规范条款（如 `Spec: 8.5.10`）、不变量、边界条件与设计原因，避免复述代码。多词文件名使用 snake_case，平台文件使用 `_amd64.go`、`_arm64.go`、`_generic.go` 或 `.s` 后缀；测试命名遵循 `TestSubject_Behavior`，基准遵循 `BenchmarkKernel_Size_Backend`。生成表通过对应生成器更新，不手改生成输出；TODO 必须关联 issue；使用 `unsafe` 需说明理由与边界检查。

新增行为和修复应有同次变更的有效测试，算法验证优先使用规范解析解或独立 Oracle。自研读写或编解码往返不能单独证明符合规范：仅有自往返证据时，状态最高为 `Partial`；`Conformant` 须有 Oracle 或规范解析解证据。

支持状态发生变化时，同步更新 `docs/implementation-status.md` 和 `docs/supported-features.md`，写清触发条件、已知缺口与验证证据。新增语料遵循 `testdata/README.md`，登记 `testdata/manifest.json` 的元数据、来源与 SHA-256；优先现场生成参考 YUV。

`tools/oracle` 的退出码：0 一致，2 像素差异，3 缺少 FFmpeg（跳过），4 工具或环境错误。区分跳过和失败；需要 Oracle 证据时不得用缺失工具代替验证。

提交保持一个逻辑改动，修复与功能分开。主题格式为 `type[module]: lowercase english subject`，祈使句、总长不超过 72 字符、无尾句号，例如 `docs[repo]: add agent guidelines`。允许的 type/module 及分支规则见 `docs/Implementation-Plan.md`；提交钩子在 `.githooks/`。
