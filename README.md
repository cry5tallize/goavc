# goavc — Pure Go H.264/AVC Encoder & Decoder

从零实现的 H.264/AVC 软件编解码器：**纯 Go**，允许 Go 汇编（amd64 SSE2/AVX2、arm64 NEON），
**零 CGO、零 C/C++、零外部编解码库**。Encoder 与 Decoder 共享同一套预测、变换、重建、滤波内核。

当前版本：**v1.0.0，本地版本收尾**。提供六种 8/10-bit YUV420/422/444 原生编解码、
CAVLC/CABAC I/P/B、CQP/ABR/CBR/VBV、Annex-B/AVCC 流式适配及三个 CLI。
本次版本整理不新增编解码能力；完整 Profile 仍为 `Partial`，具体组合见[支持矩阵](docs/supported-features.md)。

主要限制：

- 单实例串行、非并发安全，`Threads>1` 返回 `ErrUnsupported`；多路视频使用独立实例。
- PAFF、transform bypass、单色及 9/12/14-bit 像素流水线未支持；MBAFF 已有受限支持。
- SIMD 按槽选择实际后端，不保证所有槽加速；arm64 仅完成交叉编译，未原生验证。
- 不包含 MP4/RTP/WebRTC 封装和 RGB 转换；不承诺所有配置达到实时吞吐。
- 本次不运行 8 小时长稳或远端 CI，不宣称完整生产认证；验证范围与性能见[版本记录](CHANGELOG.md)。

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
├── tools/       依赖检查、语料校验、Oracle 与开发期测量工具
├── benchmarks/  内核与端到端基准
├── testdata/    语料清单与测试向量（见 testdata/README.md）
└── docs/        路线图、架构、支持矩阵、实现状态、性能、许可
```

## 快速开始

需要 Go 1.27.x。在仓库内安装命令行工具（不依赖 FFmpeg）：

```bash
CGO_ENABLED=0 go install ./cmd/h264enc ./cmd/h264dec ./cmd/h264probe

# 原始 YUV420P → Annex-B H.264 → 原始 YUV420P
h264enc -input input.yuv -output out.264 -width 640 -height 360 -profile high -qp 26
h264dec -input out.264 -output decoded.yuv
h264probe -input out.264
```

`go install` 的目标目录须在 PATH 中；Windows PowerShell 用 `$env:CGO_ENABLED='0'` 设置环境变量。
10-bit、场编码、B 帧与 AVCC 参数见[流式和 CLI 用法](docs/stream.md)。
库接入示例见[帧级编码 API](docs/encoder.md)及[流式编解码 API](docs/stream.md#编解码入口)。

公共 API 的使用约定：

- `SendFrame` 返回后可复用或释放输入；`SendPacket` 接收完整 Annex-B AU，不是单个 NAL。
- 接收的 Frame 使用后调用幂等 `Release()`，长期保存用 `Clone()`；Packet.Data 归调用方。
- 用 `errors.Is` 判断 `ErrAgain`（暂时无输出）、`ErrEOF`（排空结束）及其他哨兵错误；输入结束后 `Flush` 并排空输出。
- 同一 codec 不跨 goroutine 并发调用。详细契约见[架构](docs/architecture.md#5-内存所有权)。

### 开发验证

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

模块路径为 `github.com/goavc/h264`。版本标签当前只在本地；本次不执行 push 或远端 release。

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
| [docs/supported-features.md](docs/supported-features.md) | 当前支持组合与已知限制 |
| [docs/implementation-status.md](docs/implementation-status.md) | 逐模块实现状态（Translation Ledger） |
| [docs/stream.md](docs/stream.md) | M6流式API、取消/背压、资源预算、三个CLI与30秒稳定性验收 |
| [docs/performance.md](docs/performance.md) | 基准口径与结果 |
| [docs/licensing.md](docs/licensing.md) | 代码许可与专利许可（两件独立的事） |
| [CHANGELOG.md](CHANGELOG.md) | v1.0.0 版本内容、验证范围与限制 |

## 许可与专利

代码以 MIT 许可发布，见 [LICENSE](LICENSE)。**开源许可证不等于专利授权**：
H.264 涉及独立于代码许可的专利许可问题，详见 [PATENTS.md](PATENTS.md) 与
[docs/licensing.md](docs/licensing.md)。
