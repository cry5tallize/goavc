# Pure Go H.264/AVC Encoder & Decoder — 实施路线图

> **项目定位：** 从零构建一个高性能、跨平台、零 CGO 依赖的 Go H.264/AVC 软件编解码器。
>
> **技术路线：** Pure Go + Go Assembly（amd64 SSE2/AVX2、arm64 NEON），Encoder / Decoder 共享同一套预测、变换、重建、滤波内核。
>
> **核心参考：** [thesyncim/goh264](https://github.com/thesyncim/goh264)（仅 Decoder 架构与验证体系参考；该项目为 Decoder-only）。

| 项目 | 内容 |
|---|---|
| 文档版本 | v1.0 |
| 状态 | 生效（Active） |
| 适用范围 | 本仓库全部里程碑、验收与发布判定 |
| 关联文档 | `docs/architecture.md`、`docs/supported-features.md`、`docs/implementation-status.md`、`docs/conformance.md`、`docs/performance.md` |
| 变更方式 | 里程碑范围变更走 §15 流程；日常状态更新只改 Feature Matrix / Translation Ledger，不改本文档 |

---

## 0. 如何使用本文档

本文档是项目的**唯一排期与门禁来源**。三层文档分工如下：

| 层级 | 文档 | 回答的问题 | 更新频率 |
|---|---|---|---|
| 计划层 | `docs/Roadmap.md`（本文） | 做什么、按什么顺序、什么算完成 | 里程碑级 |
| 能力层 | `docs/supported-features.md` + Feature Matrix | 当前能解/能编什么，边界在哪 | 每个 PR |
| 实现层 | `docs/implementation-status.md`（Translation Ledger） | 每个规范模块实现到哪一步、对应哪些测试、遗留什么问题 | 每个 PR |

**门禁规则（必须遵守）：**

1. 里程碑的 **Exit Criteria** 未全部满足，不得宣称该里程碑完成，后续里程碑即使已开工也不得对外发布。
2. 每个里程碑的 **Out of Scope** 清单是硬边界。范围内的功能必须真正可用；范围外的功能必须显式返回 `ErrUnsupported`，不得"看似支持、实际不支持"。
3. 每条 Exit Criteria 必须能映射到至少一个可执行检查（单元测试 / Oracle 差分 / Fuzz 语料 / Benchmark 记录 / CI 步骤）。无法自动化的项必须写明人工核验方式与记录位置。
4. 只要出现"为了通过门禁而放宽断言"的情况，视为里程碑回退，按 §15 处理。

**状态图例：** `TODO` 未开始 · `WIP` 进行中 · `Partial` 部分可用（存在已知不支持路径）· `Conformant` 通过规定验证 · `Unsupported` 明确拒绝并返回错误

---

## 1. 项目定位

### 1.1 目标

- 完全 Go 实现，**不依赖** CGO、C/C++、FFmpeg、x264、OpenH264 或任何外部编解码动态库。
- 同时提供 **Encoder** 与 **Decoder**，二者共享底层算法内核，避免两套预测/变换/重建实现。
- 输入输出覆盖：原始 YUV 帧、Annex-B 码流、AVCC（长度前缀）码流、`[]byte`、`io.Reader`/`io.Writer`。
- 同时支持**整帧批处理**与**增量流式处理**（任意分块、跨 Buffer 边界、背压、上下文取消）。
- Profile 路线：Baseline → Main → High → High 10 / High 4:2:2 / High 4:4:4 Predictive。
- 熵编码：CAVLC 与 CABAC 完整双实现。
- 平台：Windows / Linux / macOS × amd64 / arm64，其他架构走 Pure Go 标量回退。
- 可用 FFmpeg 做**开发期**独立交叉验证（Oracle），但 FFmpeg 永不进入运行时依赖。

### 1.2 非目标（第一版明确排除）

- MVC / SVC / MVCD / 3D-AVC 等多视图、可伸缩、三维扩展（作为独立扩展项目）。
- MP4 Demux / Mux、RTP / WebRTC 打包（未来以独立适配层接入，核心 Codec 只提供 Annex-B / AVCC + Access Unit 接口）。
- RGB 色彩空间转换管线（Frame 只承载原生 YUV；转换交由调用方或独立适配层）。
- 极端压缩率算法（如 full RDOQ、多 pass 心理视觉模型）——留待 v1.x 按基准数据决定。
- 追求与 x264 字节级一致的码流（目标是与规范一致、可互操作）。

### 1.3 交付形态

1. 可作为库导入的 Go Module（公共 API 见 §5.3）。
2. `cmd/h264enc`、`cmd/h264dec`、`cmd/h264probe` 三个 CLI 工具。
3. 可复现的验证体系：测试语料 manifest、Oracle 差分、Fuzz 语料、N 组基准报告。
4. 完整文档：架构、支持范围、实现状态、一致性、性能、许可与专利说明。

---

## 2. 不可违反的约束

| 项目 | 约束 | 违规判定方式 |
|---|---|---|
| CGO | 禁止 | CI 全部构建/测试以 `CGO_ENABLED=0` 执行 |
| C/C++ 源码 | 禁止 | 仓库内不允许 `.c/.cc/.cpp/.h` 实现文件 |
| 外部动态库 | 禁止 | `go list -deps` 与二进制 `ldd`/`dumpbin` 检查无第三方 codec 依赖 |
| 运行时第三方依赖 | 尽量为零 | `go.mod` 的 `require` 在 v1.0 前必须为空（仅 stdlib） |
| Go Assembly | 允许，但必须有回退 | 每个汇编 kernel 必须有同名 Go 标量实现 + 一致性测试 |
| `unsafe` | 仅必要处使用 | 每次引入需在 PR 说明理由与边界检查方式 |
| SIMD | SSE2 / AVX2 / NEON | 运行时分发，禁止强制要求 AVX2 |
| 标准实现 | H.264/AVC 规范 | 以 Oracle 差分 + 规范条款引用为准 |
| 默认后端 | Pure Go CPU | 无汇编环境功能完整，仅速度下降 |
| 编译模式 | 必须支持 `CGO_ENABLED=0` | 构建矩阵全绿 |

**硬性红线：** 任何性能优化都不得改变编码/解码结果的规范正确性。优化前后必须做**逐比特/逐像素等价性**验证（编码侧允许码流不同，但必须与优化前的本地重建逐像素一致，且 FFmpeg 可解）。

---

## 3. 性能目标与测量口径

采用 **Balanced** 取向：不以极端压缩率或极端速度牺牲另一方。

| 场景 | 初始目标（暂定） | 状态 |
|---|---|---|
| 1080p 8-bit YUV420 解码 | 30 FPS | TODO |
| 720p 8-bit YUV420 编码 | 30 FPS（M8优化目标） | M5复审：标量实测未达，不承诺实时；样本见performance.md§14 |
| 1080p 8-bit YUV420 编码 | 后续优化目标（v1.x） | TODO |
| 连续流式处理 | 长时运行，内存无持续增长 | TODO |
| 核心 SIMD Kernel | 尽量零堆分配 | TODO |
| 多实例并行 | 独立实例互不干扰，可线性扩展 | TODO |

> 以上是**项目目标而非承诺**。所有数字必须在基准硬件上实测后回填，并注明测量条件。

**测量口径（固定，避免自利对比）：**

- 基准硬件登记：CPU 型号、核心数、内存频率、OS、Go 版本、编译标志，写入 `docs/performance.md` 表头。
- 每条性能数字必须附：输入语料名 + SHA-256、分辨率、Profile、QP 或码率、GOP 结构、线程数、是否启用 SIMD、commit hash。
- 报告字段：Encode/Decode FPS、平均帧延迟、P95 帧延迟、CPU 使用率、峰值内存、`alloc/op`、`B/op`、码率、PSNR、SSIM、I/P/B 比例。
- 对比 x264 / libavcodec 时必须使用**相同输入、分辨率、Profile、帧率**，并分别在等码率与等 QP 两种条件下报告速度与画质。
- 目标修订：M2 与 M5 结束后各做一次目标复审，把"暂定"改为"实测 + 承诺"或下调，并在本表更新状态列。

---

## 4. 参考项目与知识产权边界

### 4.1 从 goh264 借鉴的内容（架构与工程思想）

| goh264 设计 | 本项目采用方式 |
|---|---|
| 根包 Public API | 保持简洁统一的公共 API |
| `internal/h264` | 拆分为职责明确的内部模块子包 |
| SPS/PPS/NAL 分离 | 保持独立模块 |
| CAVLC/CABAC 分离 | 独立熵编码子系统 |
| Prediction / Motion Compensation | Encoder 与 Decoder 共享 |
| Transform / IDCT | 正反变换共享同一数学定义与测试 |
| Deblocking | Encoder 本地重建与 Decoder 共用 |
| DPB | 独立参考帧管理 |
| Clone / Validate | 明确的内存所有权接口 |
| FFmpeg Oracle | 建立差分测试体系 |
| Corpus Manifest | 固定测试向量清单与 SHA-256 |
| Fuzz / Benchmark | 作为 CI 质量门禁 |
| Translation Ledger | 逐规范模块的实现状态登记表 |

### 4.2 边界规则（必须执行）

1. **只借鉴架构与验证方法，不复制受许可证约束的实现代码。** goh264 为 LGPL-2.1 且采用 FFmpeg 结构移植路线，本项目不直接复制或改写其代码。
2. **以行为而非源码转写：** 实现依据 H.264/AVC 规范条款 + 公开语法表 + 自有推导，对照手段是**码流输入输出行为**与 FFmpeg/JM 的**解码结果**，不是逐行翻译他人源码。
3. 任何未来对第三方代码的复用，必须先在 `docs/licensing.md` 完成许可证兼容性审查并记录结论，再动代码。
4. **开源许可证 ≠ 专利授权。** H.264 专利许可与 LGPL/BSD 等代码许可是两个独立问题，分别记录、分别审查，不与代码许可混淆。

---

## 5. 架构与依赖规则

### 5.1 分层

```text
Public API  (encoder.go / decoder.go / frame.go / packet.go / config.go / errors.go)
    │
    ├── Encoder
    ├── Decoder
    └── Stream
         │
         ▼
    Codec Pipeline
         │
         ├── Syntax        (SPS / PPS / Slice / SEI / VUI / ScalingList)
         ├── NAL           (Annex-B / AVCC / avcC / Access Unit)
         ├── Entropy       (CAVLC / CABAC)
         ├── DPB           (POC / Reference / Reorder / Marking)
         └── Shared Codec Core
                 │
                 ├── Prediction
                 ├── Transform
                 ├── Motion
                 ├── Filter
                 └── SIMD  ──► Generic fallback（强制存在）
```

### 5.2 依赖规则（R1–R6）

| 编号 | 规则 | 检查方式 |
|---|---|---|
| R1 | Encoder 与 Decoder 不得相互 import | 依赖图检查脚本（`go list -deps`） |
| R2 | Shared Core 不得依赖 Encoder / Decoder | 同上 |
| R3 | SIMD 只作为底层实现，不得被上层业务逻辑直接调用（只经 dispatch） | code review + 依赖检查 |
| R4 | Public API 不暴露内部实现类型（仅暴露 `Frame`/`Packet`/`Config`/错误值） | API surface 快照测试 |
| R5 | 禁止循环依赖 | `go build` + 依赖图检查 |
| R6 | 禁止把 H.264 逻辑塞进单个巨型文件（建议单文件 ≤ 800 行有效代码） | `gofmt`/lint 规则 + review |

### 5.3 公共 API 契约（M1 冻结草案，M4 冻结 v1）

```go
type PixelFormat uint8
const (
    YUV420P PixelFormat = iota
    YUV422P
    YUV444P
    YUV420P10LE
    YUV422P10LE
    YUV444P10LE
)

type Plane struct {
    Data   []byte
    Stride int
}

type Frame struct {
    Width, Height int
    Format        PixelFormat
    Planes        [3]Plane
    PTS, Duration int64
    KeyFrame      bool
}

type Packet struct {
    Data          []byte
    PTS, DTS      int64
    Duration      int64
    KeyFrame      bool
}

// 统一错误
var (
    ErrAgain, ErrEOF, ErrInvalidData, ErrUnsupported, ErrResourceLimit error
)

// Encoder: SendFrame / ReceivePacket / Flush / Reset / Close
// Decoder: SendPacket / ReceiveFrame / Flush / Reset / Close
```

**API 状态机（必须一致实现于 Encoder 与 Decoder）：**

- 输入已消费后，可重复调用 Receive。
- 暂时无输出 → `ErrAgain`。
- Flush 后继续接受剩余输出，全部排空后 → `io.EOF`。
- Reset 后回到初始状态（Encoder 清 GOP/RC 状态；Decoder 清 DPB 但可保留最近有效 SPS/PPS 或强制等待 IDR，二者择一并在文档中固定）。
- 实例**非并发安全**；多路并行请使用多个实例，禁止共享实例。

**Frame / Packet 内存所有权：**

| 场景 | 契约 |
|---|---|
| `SendFrame(*Frame)` | 调用成功后 Encoder 可持有该 Frame 直到内部消费完毕；调用方在 Send 返回后即可复用/释放（实现须在 Send 内完成必要拷贝或引用计数） |
| `ReceiveFrame() (*Frame, error)` | 返回的 Frame 归调用方所有；调用 `Release()` 归还池，未 Release 则退化为 GC 回收（不得泄漏引用） |
| `SendPacket(Packet)` | Decoder 不保留 `Data` 引用超过该次调用；内部如需保留则拷贝 |
| `ReceivePacket() (Packet, error)` | `Data` 由调用方持有，重复使用同一 Packet 变量不应导致内容被覆盖 |

> 上述契约需要在 M1 写出实现说明，在 M4/M5 用测试固定（含 race 检测）。

---

## 6. 里程碑总览

### 6.1 依赖关系

```text
M0 工程基础
 └─► M1 Bitstream Foundation
      └─► M2 Decoder Baseline ──► M3 Decoder Main/High
           │                          │
           └──────────┬───────────────┘
                      ▼
                 M4 Encoder Baseline ──► M5 Encoder Main/High
                                            │
M1 ─────────────────────────────────────────┼──► M6 Stream / I/O
                                            │
M2/M3 ──────────────► M7 高级 AVC 特性 ◄────┘
M4/M5 ──────────────► M8 SIMD / 并行
M6/M7/M8 ───────────► M9 生产化与发布
```

### 6.2 概览

| 里程碑 | 主题 | 关键交付 | 规模 | 状态 |
|---|---|---|---|---|
| M0 | 工程基础与规范准备 | 骨架、API 草案、CI、Oracle 框架、语料 manifest | S | WIP（本地通过，待 CI） |
| M1 | Bitstream Foundation | BitReader/Writer、RBSP/EBSP、NAL、Annex-B/AVCC、avcC、AU 组装、SPS/PPS | M | WIP（本地门禁通过，待 CI） |
| M2 | Decoder Baseline | CAVLC + Intra + P + MC + 4×4 IDCT + Deblock + 基础 DPB，与 FFmpeg 逐像素一致 | L | WIP（本地门禁通过，待 CI；见 gates/m2.md） |
| M3 | Decoder Main/High | CABAC、B Slice、POC、RefList、MMCO、8×8、ScalingMatrix、延迟输出 | L | WIP（本地门禁通过，待 CI；见 gates/m3.md） |
| M4 | Encoder Baseline | SPS/PPS Writer、IDR、I_PCM、I4×4/I16×16、CAVLC、CQP、本地重建、Annex-B | L | WIP（本地门禁通过，待 CI；见 gates/m4.md） |
| M5 | Encoder Main/High | P Slice、ME/MC、亚像素、模式决策、RD、B Frame、CABAC、8×8、GOP、RC | XL | WIP（M5a/b/c/d本地门禁已关闭，待远端CI；见gates/m5c.md、gates/m5d.md） |
| M6 | 完整流式处理 | 增量 Reader、AU 组装、YUV I/O、Pipeline、背压、取消、Flush | M | WIP（接口、8-bit YUV读写、增量Annex-B/AVCC NAL Reader、有界AU Reader与Annex-B/AVCC AU Writer完成；pipeline门禁未关闭，见gates/m6.md） |
| M7 | 高级 AVC 特性 | High10/422/444、Interlaced、MBAFF/PAFF、FMO、Data Partitioning 等 | XL | TODO |
| M8 | SIMD 与并行 | SSE2/AVX2/NEON、dispatch、pool、slice 并行、pipeline、profile | L | TODO |
| M9 | 生产化与发布 | 全量测试、Fuzz、race、多平台、稳定性、文档、v1.0 | M | TODO |

**关键路径：** M0 → M1 → M2 → M4 → M5。M3 可与 M4 并行（互不阻塞）；M6 只依赖 M1 + M4/M5 的输出接口；M8 只能优化已通过一致性验证的 kernel。

---

## 7. 里程碑详情

### M0 — 工程基础与规范准备

**目标：** 建立可持续开发的骨架，让"能否无 CGO 构建"与"如何验证正确性"在写业务代码之前就有答案。

**范围内：**
- Go Module、目录结构（见 `docs/architecture.md`）、`LICENSE`、`PATENTS.md`、`Makefile`。
- 公共 API 草案与错误值定义（编译通过、文档化，允许后续微调）。
- Profile / Level 支持矩阵初版（全部标记 `TODO`）。
- 错误处理规范、内存所有权规范、资源上限规范三份短文档。
- 测试语料 `testdata/manifest.json` schema 与校验工具（SHA-256 校验必过）。
- FFmpeg Oracle 工具（仅开发期）：封装解码到 raw YUV、参数固定、结果目录约定。
- Benchmark 框架与基准硬件登记模板。
- CI：`CGO_ENABLED=0 go build/test/vet`，amd64/arm64 交叉编译。
- `docs/licensing.md`（许可证 + 专利两个独立章节）。

**范围外：** 任何实际编解码逻辑；SIMD；流式 I/O。

**退出标准：**
- [ ] `CGO_ENABLED=0 go test ./...` 通过（含空包占位测试）。
- [ ] `CGO_ENABLED=0 GOARCH=amd64/arm64 go build ./...` 通过（三平台交叉编译）。
- [ ] `go vet ./...` 与 `gofmt -l` 干净。
- [ ] manifest 校验工具能对缺失/哈希不符的语料报错。
- [ ] Oracle 工具能对一份已知 H.264 样本产出可复现的 YUV，并记录到 `docs/performance.md`/`docs/conformance.md` 模板。
- [ ] R1–R6 依赖检查脚本（至少 R5/R1/R2）可执行。
- [ ] `PATENTS.md` 与 `docs/licensing.md` 存在且明确"代码许可与专利许可独立"。

**风险：** 目录/API 过早固化导致返工 → 缓解：M0–M1 期间 API 只做"草案"标记，M1 结束才冻结 v0 形状。

---

### M1 — Bitstream Foundation

**目标：** 建立可靠的码流读写层。这一层出问题，后面所有模块的 bug 都会伪装成算法 bug。

**范围内：**
- `bitstream`：64-bit bit reservoir 的 `BitReader`/`BitWriter`、`ReadBits`、UE/SE、`ReadByte`、ByteAlign、bit position、bounded 语义（越界返回错误而非 panic）。
- RBSP ↔ EBSP：emulation prevention 插入/移除、trailing bits 校验、非法尾位报错。
- NAL：header 解析（forbidden_zero_bit / nal_ref_idc / type）、payload 视图、Annex-B 起始码（3/4 字节）、AVCC 1/2/3/4 字节长度前缀。
- `avcC` 配置记录解析与生成（含 SPS/PPS 提取与更新）。
- Access Unit Assembler 的**接口与判定规则**（AUD / slice header picture identity / `first_mb_in_slice==0` / `frame_num` 变化 / POC 变化），先实现"按 NAL 序列组装 + 显式 AU 边界提示"，跨 Buffer 分块在 M6 完善。
- `syntax`：SPS / PPS / VUI / ScalingList 完整解析与**重序列化**（round-trip）。

**范围外：** Slice header 之外宏块层语法；CABAC 初始化表（M3）；实际解码重建。

**退出标准：**
- [ ] 对语料中全部 SPS/PPS：`parse → serialize → parse` 得到结构相等（字段级深比较），且重序列化字节与原始 RBSP 一致（允许尾位规范化差异需单独标注）。
- [ ] Annex-B / AVCC 扫描器对任意分块输入（含起始码与 NAL header 被切开的最坏情况）产出相同 NAL 序列。
- [ ] EBSP/RBSP 双向转换对随机码流（含大量 `00 00 0x` 模式）保持可逆。
- [ ] Exp-Golomb 与 `ReadBits` 边界：越界、超长码字、全 1 前缀均返回 `ErrInvalidData`，无 panic、无死循环（有 fuzz 语料）。
- [ ] NAL type 1–23 全部可识别；保留/未知类型按规范忽略或报错，策略写入文档。
- [ ] `avcC` 可解析 x264/FFmpeg 产出的真实 avcC 并还原出原始 SPS/PPS 字节。

**风险：** RBSP trailing bits 与 `cabac_zero_word` 处理规则混淆 → 在 `docs/architecture.md` 明确两者归属与校验时机。

---

### M2 — Decoder Baseline（首个可用闭环）

**目标：** 解码真实 Baseline I/P 视频，输出与 FFmpeg **逐像素一致**的 YUV420。

**范围内：**
- Slice header（Baseline 子集）。
- I_PCM 宏块（作为最短验证路径）。
- CAVLC 解码：coeff_token / trailing ones / total zeros / run before / level prefix / suffix / nC context。
- Intra 4×4（9 模式）、Intra 16×16（4 模式）、Chroma 预测（8×8，4 模式）。
- 4×4 inverse transform、inverse quantization、Hadamard（luma DC / chroma DC）。
- 4:2:0 8-bit 反扫描与重建。
- P Slice：MV 预测、整像素/亚像素 MC（4×4/8×8/16×16 分割至少到 8×8）、P_Skip。
- Deblocking（luma + chroma，边界强度 bS 计算，alpha/beta 表）。
- 基础 DPB：单参考帧 + `frame_num` 校验；无重排 POC type 0/2（type 2 提前接入 x264 Baseline 语料）。
- 错误处理：非法码流返回明确错误并进入"等待 IDR"状态。

**范围外：** CABAC、B Slice、8×8 transform、ScalingMatrix、MMCO、Weighted Prediction、多参考帧 reordering、字段编码。

**退出标准：** 本地证据见 [M2 门禁记录](gates/m2.md)，远端 CI 尚待执行。
- [x] 至少 3 份 Baseline 语料（不同分辨率、含 I/P、含多 slice）解码输出与 FFmpeg `-f rawvideo -pix_fmt yuv420p` **Y/U/V 三平面逐像素完全相等**（按显示顺序、按 crop 尺寸比较）。
- [x] 多 slice 帧可正确解码（slice 边界滤波强度正确）。
- [x] P_Skip 与 MV 边界裁剪（超出图像边界）与 Oracle 一致。
- [x] CAVLC 对 `nC = -1`（chroma DC）与 16×16 DC 的特殊路径有独立测试。
- [x] 语料中所有 Baseline 样本 `manifest.json` 状态标为 `Conformant`；不支持的码流标 `Unsupported` 并验证返回 `ErrUnsupported`。
- [x] 1080p 解码基准记录首次实测值到 `docs/performance.md`（作为 M8 优化基线）。
- [x] 解码路径 `alloc/op` 基线记录（用于 M8 目标）。

**风险：**
- 差分测试"看起来对但顺序错" → 强制按 PTS/显示顺序对齐比较，禁止按解码顺序比较。
- 亚像素插值舍入偏差 → 严格按规范 `(a - 5b + 20c + 20d - 5e + f + 16) >> 5` 与 `Clip1` 实现，逐 kernel 单测。

---

### M3 — Decoder Main / High

**目标：** 覆盖主流 Main / High 8-bit YUV420 码流。

**范围内：**
- CABAC：算术解码器（range/offset）、上下文初始化（`init_idc` 26/27）、状态转移表、全部宏块层 binarization（mb_type、sub_mb_type、mvd、ref_idx、CBP、residual、terminate）。
- B Slice：双向预测、direct（temporal / spatial）、list 0/1 组合、B_Skip。
- POC type 0/1/2、`pic_order_cnt_lsb` 回绕、输出重排序。
- Reference list 构造与 reordering、多参考帧。
- MMCO（含长期参考帧）。
- Weighted Prediction（explicit / implicit）。
- 8×8 transform（High profile）、ScalingMatrix（含 SPS/PPS 级与默认表）。
- 完整 Deblocking（含 `disable_deblocking_filter_idc`、slice 边界、`slice_alpha_c0_offset`）。
- 延迟输出与 `Flush`（DPB 中的剩余帧在 Flush 时按显示顺序吐出，最后返回 `io.EOF`）。

**范围外：** 字段编码 / MBAFF；High10/422/444；FMO/ASO；Data Partitioning；SP/SI；Redundant Slice。

**退出标准：** 本地证据见 [M3 门禁记录](gates/m3.md)，远端 CI 尚待执行。
- [x] 至少 3 份 Main 与 3 份 High 语料（x264 默认参数，含 B 帧、多参考、CABAC）逐像素一致。
- [x] B 帧重排序：输出顺序与 Oracle 完全一致，PTS 映射正确。
- [x] `Flush` 后剩余帧数量与顺序正确，随后返回 `io.EOF`；未 Flush 时 `ReceiveFrame` 返回 `ErrAgain` 的时机正确。
- [x] MMCO / 长期参考帧场景有专设语料（`manifest.json` 标注 `mmco` 标签）。
- [x] CABAC 每个 context 有独立单元测试（初值、状态转移、MPS 翻转、terminate）。
- [x] 8×8 transform 与 ScalingMatrix 组合有解析解对照测试（构造已知输入 → 期望输出逐系数）。
- [x] 遇到字段编码 / 未支持工具时返回 `ErrUnsupported`，且不污染后续状态（有测试）。
- [x] `go test -race` 通过；解码任意语料无 panic（fuzz smoke 覆盖 slice header + CABAC 入口）。

**风险：**
- CABAC 一个 context 索引错位会导致整帧花屏但难定位 → 缓解：逐语法元素与参考实现做 trace 对比工具（开发期 dump），并在 ledger 记录每个 context 的消费位置。
- POC type 1 边界情况多 → 用 JM 生成的定向语料覆盖。

---

### M4 — Encoder Baseline（首个可用编码闭环）

**目标：** 让 FFmpeg 能正确解码 Go Encoder 产生的 Baseline 码流，且本地重建与解码器输出一致。

**范围内：**
- SPS / PPS 生成（Baseline profile_idc、level 校验、VUI 最小集）。
- IDR picture 写出（含 `idr_pic_id`、`frame_num=0`、POC 重置）。
- I_PCM 编码（最短闭环，用于验证 bitstream/NAL/切片通道，不作为最终方案）。
- Intra 预测：I4×4（9 模式，含边界模式可用性判断与最可能模式）、I16×16（4 模式）、chroma（4 模式）。
- Forward 4×4 integer transform、量化（含 `mb_qp_delta`）、CAVLC 编码（系数扫描、level 前缀/后缀、转义）。
- CQP 码率控制（固定 QP，允许按宏块自适应 delta 的最小实现）。
- 本地重建（inverse quant + inverse transform + 预测相加），与解码路径共用同一套 kernel。
- Deblocking（与 Decoder 共用实现）。
- Annex-B Writer。
- 一致性自检：本地重建帧必须与"把我们自己的码流喂给 Decoder"的输出逐像素相等（自校验测试）。

**范围外：** P Slice、运动估计、CABAC、B 帧、GOP 结构、rate control（ABR/CBR）。

**退出标准：**
- [x] FFmpeg 能解码全部产出码流，无 warning/error，`ffprobe` 识别 profile/level/分辨率正确。
- [x] 自校验：`encode → decode` 与 encoder 本地重建逐像素一致（I_PCM 与 Intra 两条路径各测）。
- [x] Intra 9 + 4 + 4 模式全部被实际选中过（用合成图案构造测试，断言模式分布）。
- [x] CAVLC 编码器对极端系数分布（全零、长游程、大 level 转义）有定向测试，且解码器可逆回原始系数。
- [x] 生成的 SPS/PPS 能被 M1 的 parser round-trip。
- [x] 输出码率与 PSNR 记录到 `docs/performance.md`（作为 M5 对比基线）。
- [x] 单帧内多个 slice 可选开启（参数化，不强求默认）。

本地逐项证据见 [M4 门禁记录](gates/m4.md)，远端 CI 尚待执行。

**风险：** 本地重建与解码路径"看起来一致但舍入不同" → 强制共用同一 kernel 并加"双路径对比测试"，禁止 encoder 内另写一套反变换。

---

### M5 — Encoder Main / High（含 Inter 与 RC）

**目标：** 对连续视频实现真正压缩，码流可被 FFmpeg 正常解码，画质/码率达到可用水平。

**范围内（按子阶段推进，每阶段独立可验收）：**

**M5a — Inter 基础：**
- P Slice、`ref_idx`（单参考起步）、MV 预测（A/B/C 中值规则）、MV 编码。
- Motion Estimation：SAD full search（小范围）+ Diamond Search 默认；搜索范围自适应；边界裁剪。
- Motion Compensation：整像素 + 1/2 + 1/4 像素（6-tap luma、bilinear chroma）。
- 分割模式：16×16、16×8、8×16、8×8（8×8 以下子分割后置）。
- P_Skip / P_16x16 早期决策。

**M5b — 模式决策与 RD：**
- SATD（Hadamard）作为代价函数。
- `J = D + λR`，λ 由 QP 推导；MVP 比特估算。
- 宏块模式全比较（Intra vs Inter），含 early skip / early terminate。
- 自适应 QP（基于活动度或码率反馈）。

**M5c — Main / High 工具：**
- CABAC 编码器（与解码器共享上下文与状态表）；cabac init、terminate、`cabac_zero_word`。
- B Slice（含 B_Skip/B_Direct 的一种可用策略）、参考列表构造、B 帧重排序输出。
- 8×8 transform + ScalingMatrix。
- Weighted Prediction（至少 explicit）。
- Main / High SPS/PPS 生成。

**M5d — GOP 与码率控制：**
- GOP 结构、IDR 间隔、B 帧位置（如 IBBP）、`MaxBFrames` 参数。
- Lookahead（小窗口，用于场景切换与帧类型决策）。
- Rate Control：ABR → CBR → VBV，独立模块，不与模式决策耦合；输出 `qp` 序列可观测。
- Scene change detection（自适应 IDR / 帧内刷新）。

**范围外：** 多 pass 编码；心理视觉优化；8×8 以下子分割的性能极致化；字段编码。

**退出标准：**
- [x] 720p/1080p 连续视频（≥ 300 帧）编码 → FFmpeg 与自研 Decoder 均可解码，无错误。High/B2/8×8/P权重/Lookahead，证据与哈希见`gates/m5cd.md`。
- [x] 双解码器一致性：FFmpeg 输出与自研 Decoder 输出逐像素一致（同一码流），且与本地重建一致。上述各300帧逐帧/整段哈希通过，不等同全Profile认证。
- [x] 等 QP 下 PSNR 与 x264（`-preset veryfast -tune psnr` 同 QP）差距在预设阈值内（阈值在 M5 开始时写入 `docs/conformance.md`，例如 Y-PSNR 差值 ≤ 1.5 dB）。两尺寸×QP18/26/34全部通过，最大绝对Y差1.411639dB；历史失败与新RD取舍均保留，见performance.md§12/14。
- [x] 等码率下 PSNR 与 x264 差距记录并公开（不做选择性报告）。六组实测匹配误差均≤1%，完整PSNR/SSIM与明显落后的压缩效率见performance.md§14。
- [x] ABR：目标码率 ±10% 内（长序列平均）；CBR：窗口内波动有记录；VBV 不越界。两尺寸×三模式各300帧三方Oracle；ABR偏差+0.475360%/−0.769220%，CBR十窗口达目标，VBV300步独立充放水逐项核对，见gates/m5d.md。
- [x] GOP 参数变化（`GOPSize`、`MaxBFrames`、IDR 间隔）均正确反映在码流中（用 `h264probe` 断言）。cmd/h264probe实际断言图片数、连续B、IDR间隔并记录POC/帧型/各图slice数；独立ffprobe及测试另覆盖GOP非IDR I、B1/2/3/16、PTS/DTS、强制IDR和Lookahead边界；大尺寸每组IDR5/P100/B195及0/60/120/180/240关键帧正确。
- [x] 编码器 CPU profile 有记录，热点排名写入 `docs/performance.md`（为 M8 定向优化）。High/B2/8×8/权重/Lookahead 60帧profile及输入/输出哈希见§11；并发负载不作吞吐承诺。
- [x] `ErrUnsupported`：不支持的 Profile/Level/格式在 `NewEncoder` 阶段报错，而非产出坏码流。encoder.New的Configuration、High矩阵/8×8选项校验及Level预算事务测试通过；完整Profile仍Partial，明确边界见支持矩阵。

**风险：** 模式决策过早引入 RD 导致性能不达标 → 缓解：M5a 先出"可用非 RD 版本"，M5b 再加 RD，两者都有基准数据可回退。

---

### M6 — 完整流式处理

**目标：** 用 `io.Reader`/`io.Writer` 处理大文件与持续实时流，内存有界、可取消、可背压。

**范围内：**
- 增量 Annex-B Reader（跨 chunk 起始码/NAL 边界、不完整数据等待、最大 NAL 限制）。
- 增量 AVCC Reader（length size 可配置或从 avcC 读取）。
- Access Unit 组装（AUD / slice header picture identity / `first_mb_in_slice` / `frame_num` / POC 变化）。
- YUV Reader / Writer（420/422/444，8-bit；高位深在 M7）。
- `FrameReader` / `FrameWriter` / `AccessUnitReader` / `AccessUnitWriter` 接口。
- `EncodeStream(ctx, src, dst)`、`DecodeStream(ctx, src, dst)`。
- 背压与有界缓冲；上下文取消（`ctx.Done()` 及时返回）；EOF → Flush 语义。
- 长时运行测试（≥ 1 小时循环输入，RSS 与 alloc 曲线平稳）。

**范围外：** MP4 / RTP / WebRTC 适配；网络传输层。

**退出标准：**
- [ ] 对同一码流，任意 chunk 大小（1 byte / 7 / 4096 / 整文件）解码输出完全一致（含最坏跨边界样本）。
- [ ] 内存有界：`MaxInputBuffer` 生效，超限返回 `ErrResourceLimit`；1 小时测试 RSS 增长率 < 预设阈值。
- [ ] 取消：`ctx` 取消后 ≤ 1 帧内返回 `context.Canceled`，无 goroutine 泄漏（`goleak` 风格检查）。
- [ ] EOF：输入结束后自动 flush，输出完整，最终返回 `io.EOF`（而非错误）。
- [ ] 不完整尾数据：不 panic、不误报为合法帧，返回明确错误或按策略丢弃并记录。
- [ ] `EncodeStream`/`DecodeStream` 的背压测试：消费者慢于生产者时不发生无界内存增长。

**风险：** AU 边界误判导致帧合并/拆分 → 用 manifest 中多 slice + 多帧无 AUD 的语料做定向回归。

---

### M7 — 高级 AVC 特性

**目标：** 按支持矩阵覆盖扩展 Profile 与高级工具，每一项独立验收、独立标注。

**范围内与顺序（每项完成后更新 Feature Matrix）：**
1. High 10（10-bit 位深：像素存储、变换/量化位宽、QP 范围、clip 范围）。
2. High 4:2:2（chroma 4:2:2 预测与变换、plane 尺寸）。
3. High 4:4:4 Predictive（含 8×8 chroma、独立 CBP、residual 预测行为）。
4. 整数化位深抽象（把 bit depth / chroma format 从"隐式 8-bit 420"变为模板化参数）。
5. Interlaced：frame/field 编码、`mb_field_decoding_flag`、field MV 预测。
6. MBAFF / PAFF。
7. FMO / ASO（slice group map，含 type 0–6）。
8. Data Partitioning（A/B/C 分区）。
9. SP / SI Slice。
10. Redundant Slice。
11. VUI / HRD 完整支持（含 `hrd_parameters` 生成与校验）。
12. ScalingMatrix 完整（SPS/PPS 级、4×4 与 8×8、flat 与自定义）。

**范围外：** MVC / SVC / 3D-AVC。

**退出标准：**
- [ ] 每一项在 Feature Matrix 中从 `TODO` → `Conformant` 或 `Unsupported`，不允许长期停留 `Partial` 而无说明。
- [ ] 每项至少 1 份真实语料通过 Oracle 逐像素一致（高位深按位深比较，含 10-bit 的 LE 打包）。
- [ ] 高位深重构：8-bit 语料在 10-bit 路径下行为不回归（全套 M2/M3 测试在 10-bit 配置下通过）。
- [ ] Interlaced / MBAFF 有专设语料（x264 `-tff/-bff/-interlaced`），两种 field order 均验证。
- [ ] 不支持的组合（例如 4:4:4 + 某些工具）显式返回 `ErrUnsupported`，并在文档列出组合级限制。

**风险：** 位深改造触及全部 kernel → 缓解：M7 之前 kernel 签名已参数化（`bitDepth` 作为编译期或运行期常量），避免大范围重写。

---

### M8 — SIMD 与并行优化

**目标：** 在**不改变结果**的前提下把关键 kernel 提速，并给出可复现基准。

**范围内：**
- amd64：SSE2 基线 + AVX2 运行时分发（`golang.org/x/sys` 不可用 → 用 `cpuid` 汇编/`internal/cpu` 等价自实现）。
- arm64：NEON。
- 其他架构：Pure Go 回退（必须保持全部测试通过）。
- Kernel 顺序（按 profile 热点决定，初始）：SAD → SATD → MC/亚像素 → 4×4/8×8 transform → 量化/反量化 → deblocking → residual 相加。
- Buffer pool（frame / picture / scratch）、零分配热路径、切片扩容消除、接口装箱消除。
- Slice 级并行、frame pipeline、ME 候选并行（必须先完成依赖分析：宏块预测依赖、slice 边界滤波、DPB 访问）。
- CPU profile 驱动的迭代（每次优化有 before/after）。

**范围外：** GPU、汇编化的熵编码（CAVLC/CABAC 只做表与分支优化）。

**退出标准：**
- [ ] 每个 kernel：Generic 与 SSE2/AVX2/NEON 输出**逐元素相等**（含边界裁剪、非对齐 stride、奇数尺寸、极值输入）。
- [ ] 无汇编环境（`GOARCH=386`/纯 Go 构建标签）下测试全绿。
- [ ] 端点基准：1080p 解码 ≥ 30 FPS、720p 编码 ≥ 30 FPS（在登记硬件上），或给出明确的差距与原因。
- [ ] 热路径零堆分配（`testing.AllocsPerRun` 断言，kernel 级为 0）。
- [ ] 并行版本与串行版本输出逐比特一致；`-race` 全绿。
- [ ] 基准对比表（Generic vs SSE2 vs AVX2 vs NEON）写入 `docs/performance.md`。
- [ ] 汇编文件全部集中在 `internal/simd`，无散落（R3 检查）。

---

### M9 — 生产化与发布

**目标：** 可被第三方依赖的 v1.0。

**范围内：**
- 单元/集成/一致性测试补齐（覆盖率不作为唯一指标，但关键模块需行覆盖记录）。
- Fuzz：Annex-B、AVCC、RBSP、Exp-Golomb、SPS/PPS、Slice header、CAVLC、CABAC、宏块、DPB、SEI；语料库入仓并随 CI 增广。
- `-race` 全量通过；多平台原生 runner 上跑汇编测试。
- 长时稳定性（≥ 8 小时混合输入）。
- API 文档（godoc 完整，含所有权与并发说明）、支持范围文档、一致性报告、性能报告。
- 许可与专利文档定稿（`LICENSE`、`PATENTS.md`、`docs/licensing.md`）。
- 版本标签、CHANGELOG、示例代码（`examples/`：文件转码、流式解码、帧级 API）。

**退出标准：** 见 §12 发布标准（v1.0 必须全部勾选）。

---

## 8. 并行工作轨道

当单人/小团队推进时，按"轨道"而不是"里程碑"分配注意力，里程碑是**门禁**而非工作单元。

| 轨道 | 覆盖模块 | 活跃区间 | 阻塞关系 |
|---|---|---|---|
| T1 Bitstream / Syntax | `bitstream`、`nal`、`syntax` | M1 起全程 | 阻塞一切 |
| T2 Decoder | `decoder`、`entropy/cavlc`、`entropy/cabac`、`dpb` | M2–M3 | 依赖 T1 |
| T3 Shared Core | `prediction`、`transform`、`filter`、`picture` | M2 起全程 | 被 T2/T4 依赖 |
| T4 Encoder | `encoder`、`motion`、rate control | M4–M5 | 依赖 T1+T3 |
| T5 Stream | `stream` | M1 接口、M6 完整 | 依赖 T1、T4 输出 |
| T6 SIMD / Perf | `internal/simd`、`internal/buffer` | M8（+ M2/M4 期间的标量基线埋点） | 依赖 T3 稳定 |
| T7 Test Infra | `tests/`、`testdata`、Oracle、bench、fuzz | M0 起全程 | 无（自驱） |

**规则：** T3 的 kernel 在被 T2/T4 使用时必须已有"Generic + 一致性测试"；T6 只能优化已有测试的 kernel，不允许"边写汇编边定语义"。

---

## 9. 验证体系

**核心原则：不得用自己写的 Encoder 与 Decoder 互相验证作为唯一正确性证据。**

### 9.1 Decoder Oracle 差分

```text
H.264 输入 ──┬──► Go Decoder ──► YUV(A) ──┐
             └──► FFmpeg     ──► YUV(B) ──┴──► 逐平面逐像素比较
```

- 比较 Y/U/V 三平面，按**显示顺序**、按 crop 后有效区域，不通过 RGB 转换。
- 报告首个不一致像素坐标 + 平面 + 周边 8×8 区域（便于定位）。
- 允许的差异：仅限"规范允许的近似"（H.264 无此类项）→ **本项目要求完全相等**。

### 9.2 Encoder 兼容性

```text
原始 YUV ─► Go Encoder ─► H.264 ─┬─► FFmpeg ─► Recon(F)
                                 ├─► Go Dec ─► Recon(G)
                                 └─► 本地重建 ─► Recon(L)
                   要求：Recon(F) == Recon(G) == Recon(L) 逐像素
```

不要求与 x264 字节一致；要求：规范合法、双解码器可解、本地重建一致、码率/画质达标。

### 9.3 语料 manifest（字段固定）

```json
{
  "name": "baseline_352x288_ip_001",
  "path": "testdata/baseline/xxx.264",
  "sha256": "…",
  "source": "x264|ffmpeg-fate|jm|synthetic|corrupt",
  "profile": "baseline|main|high|high10|high422|high444",
  "level": "3.1",
  "width": 352, "height": 288,
  "pixFmt": "yuv420p",
  "bitDepth": 8,
  "entropy": "cavlc|cabac",
  "sliceTypes": ["I", "P"],
  "sliceCount": 3,
  "refFrames": 1,
  "tags": ["mmco", "weighted_pred", "multi_slice"],
  "expectedYUV": "testdata/ref/xxx.yuv",
  "expectedYUVSha256": "…",
  "status": "conformant|unsupported|wip",
  "notes": ""
}
```

- CI 校验：每个条目 SHA-256 必对、`status=conformant` 必跑 Oracle、`unsupported` 必断言返回 `ErrUnsupported`。
- 语料体积控制：大语料不入库，manifest 记录下载来源与哈希，本地/Oracle runner 按需拉取。

### 9.4 Fuzz

必测入口：Annex-B、AVCC、RBSP、Exp-Golomb、SPS、PPS、Slice header、CAVLC、CABAC、宏块解析、DPB 操作、SEI。

必守不变量：不 panic、不越界、无整数溢出、无死循环、无无界分配、损坏输入不污染后续实例状态（用"坏样本后接好样本"的测试断言）。

### 9.5 门禁矩阵

| 检查 | 触发 | 失败即阻断 |
|---|---|---|
| `CGO_ENABLED=0 go test ./...` | 每 PR | 是 |
| `go vet` + `gofmt -l` | 每 PR | 是 |
| Kernel Parity（Generic vs SIMD） | 每 PR | 是 |
| Fuzz Smoke（短时） | 每 PR | 是 |
| Decoder Oracle（小语料集） | 每 PR | 是 |
| Decoder Oracle（全量语料） | 每日 | 是 |
| Encoder Compatibility | 每日 | 是 |
| Allocation Budget | 每 PR（热路径包） | 是 |
| Benchmark Canary（回归 > 10% 告警） | 每日 | 告警（人工判定） |
| Corpus Manifest Validation | 每 PR | 是 |
| `-race` 全量 | 每日 | 是 |

---

## 10. 内存所有权与安全策略

H.264 码流属于**不可信输入**，Decoder 默认全路径边界检查。

### 10.1 资源上限（默认值，可配置）

| 项 | 默认上限 | 超限行为 |
|---|---|---|
| 最大分辨率 | 4096×2304（可配） | `ErrResourceLimit` |
| 最大 Frame Buffer | 由分辨率 × 位深推导，附安全系数 | `ErrResourceLimit` |
| 最大 NAL 大小 | 8 MiB（可配） | `ErrResourceLimit` |
| 最大 slice 数 / 帧 | 由 `MaxSliceCount` 配置 | `ErrResourceLimit` |
| 最大 DPB 帧数 | 按 level 上限（`max_dec_frame_buffering`） | 裁剪或报错（策略入文档） |
| 最大输入缓冲（流式） | 可配，默认 = 2 × 最大 NAL | `ErrResourceLimit` |
| 最大 Side Data | 可配 | `ErrResourceLimit` |

### 10.2 内存纪律

- 所有 plane 访问前验证 `width/height/stride/bitDepth` 与 `len(Data)` 的一致性。
- 所有尺寸乘法用显式溢出检查（`if a != 0 && b > math.MaxInt/a`）。
- DPB 与 frame 一律走受控 pool；不允许长生命周期的大对象反复分配。
- 调用方持有的 buffer 不得被提前复用（引用计数或拷贝策略必须在代码里写明）。

### 10.3 错误恢复状态机

```text
Init ──valid SPS/PPS──► Decoding ──error──► WaitingForIDR ──IDR──► Decoding
                              ▲                    │
                              └──── Reset() ───────┘
```

- 错误时：返回明确错误、丢弃当前 slice 状态、**不得**继续使用可能损坏的参考帧。
- `Reset()`：清 DPB 与 slice 状态，回到 `Init`；SPS/PPS 策略需在文档与测试中固定（推荐清空，强制等待新 SPS + IDR）。
- `WaitingForIDR` 期间，非 IDR 数据被丢弃且计入统计（可通过 API 观测），不产生输出帧。

---

## 11. CI 矩阵与检查项

### 11.1 构建/测试矩阵

| 平台 | 架构 | SIMD 路径 | 说明 |
|---|---|---|---|
| Windows | amd64 | Generic + SSE2 + AVX2 | 原生 runner |
| Windows | arm64 | Generic + NEON | 原生或模拟 runner |
| Linux | amd64 | Generic + SSE2 + AVX2 | 原生 runner（主力） |
| Linux | arm64 | Generic + NEON | 原生 runner |
| macOS | amd64 | Generic + SSE2 + AVX2 | 原生 runner |
| macOS | arm64 | Generic + NEON | 原生 runner |

### 11.2 命令集

```bash
CGO_ENABLED=0 go test ./...
CGO_ENABLED=0 go test -race ./...
CGO_ENABLED=0 GOARCH=amd64 go build ./...
CGO_ENABLED=0 GOARCH=arm64 go build ./...
go vet ./...
```

**规则：** 汇编相关测试必须在对应架构**原生 runner** 执行，交叉编译只能证明"能编译"，不能证明"能跑对"。

FFmpeg 仅用于开发期测试/对照/基准；CI 中的 Oracle 步骤在无 FFmpeg 环境必须**显式跳过并标记 skipped**，不得伪装为通过。

---

## 12. 发布标准（v1.0 门禁）

### Encoder
- [ ] 能编码真实 YUV 视频（420/422/444，8-bit 起）。
- [ ] 生成合法 SPS / PPS（可被 M1 parser 与 FFmpeg 双向接受）。
- [ ] 生成 IDR / P / B Slice。
- [ ] 支持 CAVLC 与 CABAC。
- [ ] 支持 CQP / ABR / CBR（VBV 至少不越界）。
- [ ] 支持 GOP / 参考帧管理 / 本地重建。
- [ ] FFmpeg 与自研 Decoder 均可解码，二者输出与本地重建逐像素一致。

### Decoder
- [ ] 支持 Annex-B / AVCC（含 avcC）。
- [ ] 支持 Baseline / Main / High 8-bit YUV420。
- [ ] 支持 CAVLC / CABAC。
- [ ] 支持 I / P / B。
- [ ] 支持 DPB / POC / MMCO / 重排序。
- [ ] 支持 Deblocking（含 slice 边界）。
- [ ] 支持延迟输出与 Flush。
- [ ] 通过规定的独立测试向量（manifest 全量 `conformant`）。

### Stream
- [ ] 支持 `io.Reader` / `io.Writer` / `[]byte`。
- [ ] 支持 YUV / Annex-B / AVCC 输入输出。
- [ ] 支持增量输入（任意 chunk）。
- [ ] 支持取消与背压，无泄漏。

### Performance
- [ ] Generic 与 SIMD 结果逐元素一致。
- [ ] amd64 与 arm64 均可用（其他架构 fallback 可用）。
- [ ] 热路径内存分配受控（有 alloc 断言）。
- [ ] 支持多实例并行。
- [ ] 有完整、可复现的性能报告。

### Portability
- [ ] `CGO_ENABLED=0` 全绿。
- [ ] 不依赖系统 FFmpeg / x264 / 任何动态库。
- [ ] `go.mod` 无第三方 require。

### 版本切片建议

| 版本 | 内容 | 对应里程碑 |
|---|---|---|
| v0.1 | 库骨架 + Bitstream/NAL 可用 | M0–M1 |
| v0.2 | Baseline Decoder 逐像素一致 | M2 |
| v0.3 | Main/High Decoder | M3 |
| v0.4 | Baseline Encoder（FFmpeg 可解） | M4 |
| v0.5 | Inter + CABAC + B 帧 Encoder | M5 |
| v0.6 | 流式 I/O | M6 |
| v0.7 | SIMD + 并行 | M8 |
| v0.9 | 高级特性 + 性能加固（RC 见 M5d） | M7 |
| v1.0 | 全部发布门禁通过 | M9 |

---

## 13. 追踪机制

### 13.1 Feature Matrix（`docs/supported-features.md`）

按语法元素/工具逐行登记，字段：

| 字段 | 说明 |
|---|---|
| Feature | 规范术语（如 `transform_8x8`、`weighted_pred`、`mbaff`） |
| Spec ref | 规范条款号（如 8.3.1） |
| Decoder | `TODO / WIP / Partial / Conformant / Unsupported` |
| Encoder | 同上 |
| Tests | 对应测试文件/用例名 |
| Corpus | 对应 manifest 条目 |
| Notes | 限制条件、已知缺陷链接 |

### 13.2 Translation Ledger（`docs/implementation-status.md`）

每个规范模块一行，字段：

| 字段 | 说明 |
|---|---|
| Module | 包/文件 |
| Spec clause | 规范章节 |
| Scope | 支持子集（明确写出"不支持 X"） |
| Status | 状态图例之一 |
| Tests | 单测 / Oracle / Fuzz 用例 |
| Known gaps | 已知未覆盖路径（必须具体到条件） |
| Risk | 低/中/高 + 一句话原因 |
| Last verified | 最近一次全绿 commit + 日期 |

**规则：** `Conformant` 必须有 Oracle 或规范解析解测试支撑；只有自研 round-trip 的模块最高只能标 `Partial`。

---

## 14. 风险登记表

| # | 风险 | 影响 | 概率 | 缓解措施 | 触发监控 |
|---|---|---|---|---|---|
| R-01 | CABAC 上下文索引错误导致普遍花屏 | 高 | 高 | 逐语法元素 trace 工具；context 级单测；M3 前先做 terminate/init 验证 | Oracle 首帧不一致即停 |
| R-02 | 差分测试对齐错误（解码序 vs 显示序） | 高（假阳性通过） | 中 | 强制 PTS/显示序比较；manifest 记录帧数；禁止按解码顺序断言 | 比较器只接受显示序输入 |
| R-03 | 本地重建与解码路径分叉 | 高 | 中 | 共用 kernel；双路径对比测试；禁止 encoder 内私写反变换 | M4 自校验测试 |
| R-04 | 性能不达 30 FPS 目标 | 中 | 中 | M2/M4 立即建立基线；M8 前有 profile 热点清单；必要时下调目标并公示 | 每次基准记录 |
| R-05 | 位深/色度格式改造引发大范围重写 | 高 | 中 | kernel 签名早期参数化；M7 前完成抽象层 | 代码评审检查签名 |
| R-06 | 汇编与标量实现语义漂移 | 高 | 中 | 强制 parity 测试 + 边界输入（奇数尺寸/极值） | CI Kernel Parity |
| R-07 | 许可证/专利问题被低估 | 中 | 中 | 独立文档 + 代码只借鉴思想；不复用 LGPL 实现 | M0 门禁 |
| R-08 | 流式 AU 边界误判 | 中 | 中 | 多 slice / 无 AUD 语料回归；chunk 大小矩阵测试 | M6 chunk 测试 |
| R-09 | 恶意码流导致 OOM/长时循环 | 高 | 中 | 资源上限 + 无界分配检查 + fuzz 语料 | Fuzz + `ErrResourceLimit` 测试 |
| R-10 | 范围蔓延（过早做 MVC/MP4/高级工具） | 中 | 高 | 本文档 §1.2 非目标清单为硬边界；新需求走 §15 | 里程碑评审 |

---

## 15. 里程碑变更流程

1. **提案：** 在 issue/PR 描述中写明：变更内容、影响的里程碑、是否移动门禁、对既有验收的影响。
2. **影响评估：** 是否需要更新 Feature Matrix / Ledger / manifest / 性能目标；是否有已通过项回退。
3. **决策记录：** 变更落地时同步更新本文档对应章节与"文档版本"，并在 `docs/implementation-status.md` 记录理由。
4. **禁止的做法：**
   - 静默缩小里程碑范围（例如把 "B 帧" 从 M5 移出却不改文档）。
   - 把 `Conformant` 降级为"能跑通"以通过门禁。
   - 以"后续优化"为由搁置会导致错误输出的已知缺陷。
5. **回退：** 若发现某里程碑在错误前提下被标记完成，立刻改回 `WIP` 并在 Ledger 记录，重新走门禁。

---

## 16. 开发原则（执行纪律）

1. **正确性优先于性能。** 先 Generic，再 Assembly；先 Oracle 一致，再谈速度。
2. **Decoder 优先。** 先建立可验证的解码闭环，Encoder 以它为对照。
3. **共享核心算法。** 预测、变换、反量化、重建、滤波必须同一实现，禁止双写。
4. **不依赖 FFmpeg。** FFmpeg 只存在于开发期验证与基准。
5. **不做伪支持。** 不支持的 Profile/工具返回 `ErrUnsupported`，绝不产出坏码流或静默降级。
6. **不提前过度并行。** 先完成依赖分析，再设计线程调度。
7. **所有优化有基准。** 每次 SIMD/内存优化必须附 before/after 数据。
8. **持续记录边界。** Feature Matrix + Translation Ledger 与代码同 PR 更新。
9. **公共 API 稳定。** 内部可重构，公共 API 变更需走变更流程并记录。
10. **许可与专利独立审查。** 不把开源许可证当作 H.264 专利授权。

---

**最终交付：** 一个真正独立的 Pure Go H.264/AVC Encoder + Decoder，使用 Go Assembly 加速，支持原始视频帧、Annex-B、AVCC、文件与实时流，不需要 CGO 或任何外部编解码动态库。
