# goavc — Pure Go H.264/AVC Encoder & Decoder

从零实现的 H.264/AVC 软件编解码器：**纯 Go**，允许 Go 汇编（amd64 SSE2/AVX2、arm64 NEON），
**零 CGO、零 C/C++、零外部编解码库**。Encoder 与 Decoder 共享同一套预测、变换、重建、滤波内核。

> 当前状态：**M3 开发中，M2 Baseline 已完成本地门禁**。`decoder.New` 接收 Annex-B 访问单元，
> 输出 8-bit YUV420；支持 CAVLC、I/P、单参考、多 slice、P_Skip、去块与裁剪。
> 五份真实 Baseline 语料（含 1080p）通过 FFmpeg 逐像素对比，CI 可核验固定
> Oracle 输出哈希。M3 新增 CABAC 单参考 I/P 的 4×4 路径，三份 Main 样本通过
> FFmpeg 对比。多参考、重排命令、MMCO/长期参考与 POC type 1 已接入并验证。
> 编码器从 M4 开始；B/8×8/加权预测等工具当前返回 `ErrUnsupported`。
> 本地退出证据见 [M2 门禁记录](docs/gates/m2.md)，远端 CI 待执行。
> 进度及边界见 [实现状态](docs/implementation-status.md) 与 [支持矩阵](docs/supported-features.md)。

## 硬约束

| 项目 | 约束 |
|---|---|
| Go | 最新 Go 1.27 补丁版，当前工具链基线 1.27.4；CI 自动选择 1.27.x 最新版 |
| CGO | 禁止，所有目标在 `CGO_ENABLED=0` 下执行 |
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
| [docs/performance.md](docs/performance.md) | 基准口径与结果 |
| [docs/licensing.md](docs/licensing.md) | 代码许可与专利许可（两件独立的事） |

## 许可与专利

代码以 MIT 许可发布，见 [LICENSE](LICENSE)。**开源许可证不等于专利授权**：
H.264 涉及独立于代码许可的专利许可问题，详见 [PATENTS.md](PATENTS.md) 与
[docs/licensing.md](docs/licensing.md)。
