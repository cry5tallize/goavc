# goavc — Pure Go H.264/AVC Encoder & Decoder

从零实现的 H.264/AVC 软件编解码器：**纯 Go**，允许 Go 汇编（amd64 SSE2/AVX2、arm64 NEON），
**零 CGO、零 C/C++、零外部编解码库**。Encoder 与 Decoder 共享同一套预测、变换、重建、滤波内核。

> 当前状态：**M7高级工具已通过本地常规门禁**。六种8/10-bit YUV420/422/444原生编解码，
> CAVLC/CABAC I/P/B、MBAFF与TFF/BFF编码、完整矩阵，以及限定FMO/ASO、分区、SP/SI、冗余解码已接入。
> 55条语料登记；新增25份/76帧由实际CLI核对，明确区分FFmpeg原生Oracle与规范解析解。
> 原M2/M3全部27份语料保工具投影至High10并与严格FFmpeg逐采样一致。
> [M7门禁](docs/gates/m7.md)记录证据和发布状态；远端CI未执行，不声明完整Profile认证。
> PAFF、transform bypass、单色及9/12/14-bit仍明确拒绝；详细组合见[支持矩阵](docs/supported-features.md)。

> **M4 Baseline 帧内编码已通过本地门禁**。`encoder.New` 提供 CQP、I4/I16/色度 SAD
> 决策、CAVLC、共享本地重建与去块、独立 IDR Annex-B AU、I_PCM 和 1..64 slice 选项。
> 187 帧通过本地重建/自研解码/FFmpeg 逐像素比较；完整测试、race、fuzz 与六目标构建通过。
> M4当时验收限渐进式8-bit YUV420偶数显示尺寸，后续格式和场编码由M7扩展。M5a接入单参考P帧、四种分区与Skip，
> LevelAuto 支持按实际 AU 预算校验 1080p@30；后续 M5 已完成限定工具与配置的本地验收，完整 Profile 仍为 Partial。
> 见[编码用法](docs/encoder.md)、[M4 门禁](docs/gates/m4.md)和
> [性能基线](docs/performance.md#6-m4-帧内编码基线)。本地标签 `v0.5.0`，远端 CI 待执行。

> **M5a/b 已通过本地子阶段门禁**：连续 P 帧、SATD/RD、活动度 QP、Profile 约束，
> 720p/1080p 各300帧 Oracle，第一项同QP x264对比达到1.5dB阈值。
> 见 [M5b 证据](docs/gates/m5b.md)；后续 M5c/d 本地门禁及标签 `v0.5.3`/`v0.6.0` 见对应门禁页，远端 CI 未执行。

> **M6 流式处理**：YUV、Annex-B/AVCC、AU适配器与同步有界pipeline已接入，
> `EncodeStream`/`DecodeStream`提供背压、取消检查与EOF自动Flush；可能阻塞的端点须支持取消或由所有者中断I/O。
> `cmd/h264enc`、`cmd/h264dec`支持文件/二进制管道，`h264probe -syntax`逐NAL打印语法树。
> 用法与资源/所有权边界见[流式文档](docs/stream.md)，验收结果见[M6门禁](docs/gates/m6.md)。

## 硬约束

| 项目 | 约束 |
|---|---|
| Go | 最新 Go 1.27 补丁版，当前工具链基线 1.27.4；CI 自动选择 1.27.x 最新版 |
| CGO | 编解码库禁止使用；常规构建/测试为 `CGO_ENABLED=0`，仅开发期 race 工具链使用 `CGO_ENABLED=1` |
| C/C++ 源码 | 禁止 |
| 外部动态库 | 禁止 |
| 运行时依赖 | 仅标准库（`go.mod` 无 require） |
| Go 汇编 | 允许，但每个 kernel 必须有 Go 标量实现且输出逐元素一致 |
| SIMD | SSE2 / AVX2 / NEON，运行时分派，禁止强制要求 AVX2 |
| FFmpeg | 仅开发期 Oracle 对比，永不进入运行时依赖 |

## 目录结构

```text
.
├── frame.go packet.go config.go errors.go   公共 API 的类型与配置
├── bitstream/   比特级读写、RBSP/EBSP、Exp-Golomb
├── nal/         NAL、Annex-B、AVCC、avcC、访问单元组装
├── syntax/      SPS / PPS / Slice Header / SEI / VUI / ScalingList
├── internal/
│   ├── picture/ transform/ prediction/ motion/ filter/  共享编解码内核
│   ├── entropy/cavlc/ entropy/cabac/                     熵编码
│   ├── dpb/ buffer/ simd/                                参考帧、缓冲池、SIMD
│   └── testutil/                                        依赖检查、语料清单、YUV 比对
├── decoder/ encoder/ stream/                            流水线与 I/O 适配
├── tools/       depscheck、manifest、oracle 三个开发期工具
├── benchmarks/  内核与端到端基准
├── testdata/    语料清单与测试向量（见 testdata/README.md）
└── docs/        路线图、架构、支持矩阵、实现状态、性能、许可
```

## 快速开始

```bash
# 完整验证（格式、静态检查、构建、测试、依赖规则、语料清单）
make verify

# 没有 make 的环境
./scripts/verify.sh

# 单独跑某一步
CGO_ENABLED=0 go test ./...
go run ./tools/depscheck
go run ./tools/manifest -file testdata/manifest.json
```

格式：`make fmt` 就地格式化；`make fmt-check` 只检查（PR 前执行）。

模块路径当前为 `github.com/goavc/h264`。发布到实际仓库前请改为真实路径，
并同步 `tools/` 下的 import。

## 开发纪律（摘要）

1. **正确性优先于性能**：先写 Go 标量实现并用 Oracle 验证，再做汇编。
2. **Decoder 优先**：先建立可验证的解码闭环，Encoder 以它为对照。
3. **共享内核**：编解码共用预测/变换/反量化/滤波，禁止双写。
4. **不做伪支持**：未支持的 Profile/工具返回 `ErrUnsupported`，绝不产出坏码流。
5. **每次优化有基准**：任何 SIMD 或内存优化必须附前后对比数据。
6. **提交信息**：`type[module]: english subject`，见 [docs/Implementation-Plan.md](docs/Implementation-Plan.md) §1。

## 文档

| 文档 | 内容 |
|---|---|
| [docs/Roadmap.md](docs/Roadmap.md) | 里程碑、退出标准、风险 |
| [docs/Implementation-Plan.md](docs/Implementation-Plan.md) | 提交规范与逐阶段提交计划 |
| [docs/architecture.md](docs/architecture.md) | 分层、依赖规则、内存所有权 |
| [docs/supported-features.md](docs/supported-features.md) | 支持矩阵与 M2 已验证范围 |
| [docs/implementation-status.md](docs/implementation-status.md) | 逐模块实现状态（Translation Ledger） |
| [docs/stream.md](docs/stream.md) | M6流式API、取消/背压、资源预算、三个CLI与30秒稳定性验收 |
| [docs/performance.md](docs/performance.md) | 基准口径与结果 |
| [docs/licensing.md](docs/licensing.md) | 代码许可与专利许可（两件独立的事） |

## 许可与专利

代码以 MIT 许可发布，见 [LICENSE](LICENSE)。**开源许可证不等于专利授权**：
H.264 涉及独立于代码许可的专利许可问题，详见 [PATENTS.md](PATENTS.md) 与
[docs/licensing.md](docs/licensing.md)。
