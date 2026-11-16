# Pure Go H.264/AVC — 实施计划与提交规范

> 本文档把 [`Roadmap.md`](./Roadmap.md) 的 M0–M9 拆解为**可提交、可验证、可二分**的最小工作单元，并固定提交信息、分支、标签、文件与标识符的命名规范。
>
> 计划层文档：`Roadmap.md`（门禁）→ 本文档（执行）→ `implementation-status.md`（状态）。

| 项目 | 内容 |
|---|---|
| 文档版本 | v1.0 |
| 状态 | 生效 |
| 适用 | 全部里程碑的实际提交行为 |
| 强制力 | 提交信息格式、原子性、每提交必绿、门禁映射为**硬性要求** |

---

## 目录

1. [提交规范](#1-提交规范)
2. [命名规范](#2-命名规范)
3. [提交粒度与顺序规则](#3-提交粒度与顺序规则)
4. [M0 实施计划](#m0--工程基础与规范准备)
5. [M1 实施计划](#m1--bitstream-foundation)
6. [M2 实施计划](#m2--decoder-baseline)
7. [M3 实施计划](#m3--decoder-main--high)
8. [M4 实施计划](#m4--encoder-baseline)
9. [M5 实施计划](#m5--encoder-main--highinter--rc)
10. [M6 实施计划](#m6--完整流式处理)
11. [M7 实施计划](#m7--高级-avc-特性)
12. [M8 实施计划](#m8--simd-与并行优化)
13. [M9 实施计划](#m9--生产化与发布)
14. [汇总与门禁映射](#14-汇总与门禁映射)
15. [反例与钩子](#15-反例与钩子)

---

## 1. 提交规范

### 1.1 格式

```text
type[module]: subject

body (可选，说明 why / 规范条款 / 权衡)

footer (可选)
Refs: #123
Spec: 8.3.2.3
Breaking: 破坏性变更说明
Milestone: M2
```

**首行格式固定为：**

```text
^(feat|fix|refactor|perf|test|bench|docs|build|ci|chore|style|revert)\[[a-z0-9]+(/[a-z0-9]+)?\]: [a-z0-9].+$
```

示例：`feat[cavlc]: add residual block decoding with level prefix`

### 1.2 type 取值（严格限定）

| type | 语义 | 典型场景 | 必须带测试 |
|---|---|---|---|
| `feat` | 新增能力 | 新 kernel、新语法解析、新 API | 是 |
| `fix` | 修正错误行为 | 舍入偏差、边界越界、状态机错误 | 是（回归用例） |
| `refactor` | 不改变行为的重构 | 拆文件、参数化位深、抽接口 | 是（行为不变证明） |
| `perf` | 不改变输出的性能改动 | 去分配、循环重排、表预计算 | 是（前后等价 + 基准） |
| `test` | 只动测试/语料/工具 | 补 Oracle、加 fuzz 语料、manifest | — |
| `bench` | 只动基准 | 新增 benchmark、基准脚本 | — |
| `docs` | 只动文档 | 架构、支持矩阵、报告 | — |
| `build` | 构建系统 | Makefile、生成脚本、release 脚本 | — |
| `ci` | CI 配置 | workflow、门禁脚本 | — |
| `chore` | 杂项、脚手架、生成表 | go.mod、目录骨架、表生成器输出 | — |
| `style` | 格式、注释、命名（无语义变化） | gofmt、lint fix | — |
| `revert` | 回滚 | 回滚某个提交 | — |

**禁止使用**（无信息量或不合规范）：`wip`、`update`、`fix bug`、`tmp`、`changes`、`misc`、`1`、`提交`。

### 1.3 module（方括号内容）

取**主要受影响的最深一级包名**，一般不加前缀；跨多个包时取承担语义的那一个，其余写进 body。

| module | 对应目录/职责 |
|---|---|
| `repo` | 仓库级（go.mod、Makefile、CI、目录骨架） |
| `errors` / `frame` / `packet` / `config` | 根包公共类型 |
| `bitstream` | `bitstream/`（reader、writer、golomb、rbsp） |
| `nal` | `nal/`（nal、annexb、avcc、access unit） |
| `syntax` | `syntax/`（sps、pps、slice、sei、vui、scaling list） |
| `picture` | `internal/picture/` |
| `dpb` | `internal/dpb/` |
| `cavlc` | `internal/entropy/cavlc/` |
| `cabac` | `internal/entropy/cabac/` |
| `prediction` | `internal/prediction/` |
| `transform` | `internal/transform/` |
| `motion` | `internal/motion/` |
| `filter` | `internal/filter/` |
| `simd` | `internal/simd/` |
| `buffer` | `internal/buffer/` |
| `decoder` | `decoder/` |
| `encoder` | `encoder/` |
| `stream` | `stream/` |
| `cmd` | `cmd/h264enc`、`h264dec`、`h264probe` |
| `deps` / `oracle` / `testdata` / `fuzz` / `bench` | 测试基础设施 |
| `release` | 里程碑门禁收尾（`chore[release]`） |
| `status` / `roadmap` / `architecture` / `performance` / `conformance` / `api` / `legal` / `readme` | `docs/` 下对应文件、`LICENSE`、`PATENTS.md` |

**二级形式**（用于精确定位子目录，避免歧义）：`feat[entropy/cavlc]: …`。仅当同级存在同名概念时才用二级形式。

### 1.4 subject（主题行）

- **subject 必须英文**；body / footer 可用英文或中文，但同一个提交内必须保持一致。
- 祈使句、现在时、首字母小写：`add` / `fix` / `remove` / `replace` / `rename`，**不用** `added` / `adds` / `adding`。
- 结尾不加句号；不加 `!`（破坏性变更写在 footer）。
- 整行（含 `type[module]:` 前缀）≤ **72 字符**。
- 一句话说清"做了什么"，不写"为什么"（why 进 body），不写文件名（`add` 已隐含），不写实现细节。
- 一个提交只描述**一个**动作；出现 `and` 连接两个独立动作时，拆提交。

**推荐动词表：** `add` `implement` `extend` `support` `fix` `correct` `align` `reject` `remove` `replace` `rename` `split` `merge` `extract` `parameterize` `generalize` `document` `publish` `close` `tag` `guard` `clamp` `emit` `parse` `serialize`

### 1.5 body 与 footer

body 说明 **why** 与**规范依据**，建议格式：

```text
feat[transform]: add inverse 4x4 integer transform

Implements equation 8-338 for the 4x4 inverse core transform.
Coefficients stay within int16 after the second shift, so the kernel
processes rows and columns in place through a 16-element scratch array.

Spec: 8.5.10
Refs: #42
Milestone: M2
```

- `Spec:` 规范条款（强烈建议对算法类提交填写）。
- `Refs:` issue/PR 编号。
- `Milestone:` 里程碑编号（用于提取进度）。
- `Breaking:` 公共 API 或码流行为不兼容变更，必须同时更新 `docs/supported-features.md`。

### 1.6 提交信息语言与一致性

| 位置 | 语言 |
|---|---|
| subject | English（强制，便于 grep/工具化发布说明） |
| body / footer | English 或中文，同一个提交内保持一致 |
| 代码注释（含 godoc） | **中文**，且必须详细：说明规范依据、不变量、边界条件与“为什么这样写”，不接受只复述代码的注释 |
| 文档正文 | 中文（`docs/*.md`） |
| 测试失败信息 | 中文（便于排查；面向上游提交的修复另附英文摘要） |

---

## 2. 命名规范

### 2.1 包名

- 全小写、单数、无下划线、无缩写歧义：`bitstream`、`nal`、`syntax`、`picture`、`dpb`、`cavlc`、`cabac`、`prediction`、`transform`、`motion`、`filter`、`simd`、`buffer`、`decoder`、`encoder`、`stream`。
- 避免 `util`、`common`、`helpers`、`misc`、`types`、`base`（无边界命名一律拒绝）。
- 包名不与其导出标识符重复（`cabac.NewDecoder` 而非 `cabac.NewCabacDecoder`）。

### 2.2 文件名

- 全部小写；单词连写优先（`readbits.go` 不推荐），**多词用 snake_case**：`motion_comp.go`、`intra4x4.go`、`rate_control.go`、`scaling_list.go`。
- 平台/架构后缀固定：`_amd64.go`、`_arm64.go`、`_amd64.s`、`_arm64.s`、`_generic.go`（配 `//go:build !amd64 && !arm64`）。
- 测试：`xxx_test.go`；同包外部测试用 `xxx_external_test.go`（仅公共 API 验证时）。
- 表生成：`tables_gen.go`（生成物）+ `gen_tables.go`（`//go:build ignore` 的生成器）。生成物必须由生成器产出，禁止手改。

### 2.3 导出标识符（公共 API）

| 形态 | 约定 | 例 |
|---|---|---|
| 类型 | 名词，无 `H264` 前缀（包已表达） | `Encoder`、`Decoder`、`Frame`、`Packet`、`EncoderConfig` |
| 构造 | `NewXxx` | `NewEncoder`、`NewDecoder`、`NewAnnexBReader` |
| 方法 | 动词开头 | `SendFrame`、`ReceivePacket`、`Validate`、`Clone`、`Release` |
| 错误值 | `ErrXxx` | `ErrAgain`、`ErrEOF`、`ErrInvalidData`、`ErrUnsupported`、`ErrResourceLimit` |
| 常量 | 驼峰，格式名带位深 | `YUV420P`、`YUV420P10LE`、`HighProfile` |
| 枚举 | 类型名 + 值名前缀一致 | `SliceTypeI`、`SliceTypeP`、`SliceTypeB` |

内部包：不导出（`newReader`、`scanTables`）；需要跨包共享时只导出必要的窄接口。

### 2.4 测试与基准命名

```text
Test<Subject>_<Behavior>              TestBitReader_ReadBits_Truncated
Test<Subject>_<Condition>_<Expect>    TestCAVLC_TotalZeros_AllRun
Benchmark<Kernel>_<Size>_<Backend>    BenchmarkSAD_16x16_AVX2
Benchmark<Op>_<Size>_<Mode>           BenchmarkDecode_1080p_CABAC
Fuzz<Target>                          FuzzAnnexBScanner
Example<API>                          ExampleDecoder_SendPacket
```

表驱动用例的子测试名用小写短语：`t.Run("truncated header", ...)`。

### 2.5 语料与基准数据命名

```text
{profile}_{WxH}_{pixfmt}_{entropy}_{frametypes}_{tag}_{seq}.264
baseline_352x288_yuv420p_cavlc_ip_001.264
high_1280x720_yuv420p_cabac_ibbp_014.264
main_640x480_yuv420p_cabac_mmco_003.264

参考输出：同名 + .yuv      基准结果：bench/{commit}/{name}.txt
乱码语料：malformed/{target}_{case}_{seq}.264
```

manifest 条目 `name` 必须与文件名（去扩展名）完全一致，便于脚本反查。

### 2.6 分支与标签

| 对象 | 格式 | 例 |
|---|---|---|
| 功能分支 | `feat/m{N}-{slug}` | `feat/m2-decoder-baseline`、`feat/m5a-inter` |
| 修复分支 | `fix/{slug}` | `fix/cavlc-nc-context` |
| 性能分支 | `perf/{slug}` | `perf/sad-avx2` |
| 实验分支 | `exp/{slug}`（不合入 main） | `exp/cabac-table-layout` |
| 里程碑标签 | `m{N}-gate` | `m2-gate` |
| 版本标签 | `vX.Y.Z` | `v0.2.0`、`v1.0.0` |
| 基准基线标签 | `bench-{slug}` | `bench-m2-baseline` |

- 分支生命周期 ≤ 1 个里程碑；合入后立即删除。
- `main` 永远处于"每提交必绿"状态；不允许在 `main` 上直接推送（除 gate commit 的 fast-forward 合并）。

### 2.7 PR 标题与描述

- 标题沿用提交格式：`feat[m2]: add cavlc residual decoding`
- 必须包含 footer：`Milestone: M2`
- 描述必填四节：`Scope`（做了什么）/ `Out of scope`（没做什么）/ `Verification`（跑了哪些门禁、附命令与结果）/ `Ledger`（更新了哪些状态行）。

---

## 3. 提交粒度与顺序规则

### 3.1 原子性

一个提交 = 一个可独立解释、可独立回滚、可独立验证的变更。

必须满足：

1. `CGO_ENABLED=0 go build ./...` 通过。
2. `CGO_ENABLED=0 go test ./...` 通过（新增行为必须有对应测试）。
3. 不引入 `TODO` 而没有 issue 链接。
4. 若改变了支持状态，同一提交更新 `docs/implementation-status.md` 与 `docs/supported-features.md`。
5. 默认 diff ≤ 400 行（含测试）；超过 800 行必须在 PR 说明理由（如生成表、样例语料）。

### 3.2 顺序

固定依赖顺序（每个里程碑内部也遵守）：

```text
types/错误 → bitstream → syntax 解析 → 核心 kernel（含测试）
   → 上层编排（decoder/encoder）→ 一致性测试 → 文档/状态 → gate commit
```

- **重构先于功能：** 需要改动既有 kernel 签名时，先单独提交 `refactor[...]`（行为不变、测试全绿），再提交 `feat[...]`。
- **生成物单独提交：** 先 `chore[...]: add generator for ...`，再 `chore[...]: generate ...`，最后才是使用它的 `feat[...]`。
- **测试可以与实现同提交**（推荐），但不得把两个独立 kernel 的实现塞进同一提交。
- **每个 gate commit 前必须有一次 `docs[status]:` 提交**，把本里程碑涉及模块的状态改到位。

### 3.3 修复的插入规则

- 开发中发现的缺陷用 `fix[...]` 提交，**插在**当前工作提交之后、gate commit 之前。
- 缺陷涉及已合并到 `main` 的行为：单独 `fix[...]` PR，不夹带功能改动。
- 已推送的提交不得 `amend`/`force-push`；需要修正时补 `fixup` 后 `rebase -i` 仅在个人未推送分支进行。
- 回滚用 `revert[...]: ...`，并在 body 引用原提交 hash 与原因；回滚必须附回归测试（证明该缺陷不会再静默通过）。

---

## M0 — 工程基础与规范准备

**分支：** `feat/m0-bootstrap` ｜ **目标：** 骨架 + 门禁可运行 ｜ **预计提交：** 20

| # | Commit | 内容 | 验证 |
|---|---|---|---|
| 1 | `chore[repo]: initialize go module and package skeleton` | `go.mod`（无 require）、根包文件占位、目录结构 | `go build ./...` |
| 2 | `build[repo]: add makefile with fmt vet test bench targets` | Makefile、`make verify` 聚合 | `make verify` |
| 3 | `ci[repo]: run build and tests with cgo disabled` | workflow，`CGO_ENABLED=0` | CI 绿 |
| 4 | `ci[repo]: add cross compile matrix for amd64 and arm64` | 三平台 × 双架构交叉编译 | CI 绿 |
| 5 | `docs[readme]: add readme covering scope and non goals` | 定位、约束、非目标 | — |
| 6 | `docs[legal]: add license and patent notice files` | `LICENSE`、`PATENTS.md` | — |
| 7 | `docs[legal]: separate code license review from patent review` | 明确两者独立，记录 goh264 边界规则 | — |
| 8 | `feat[errors]: add unified sentinel error values` | `ErrAgain/ErrEOF/ErrInvalidData/ErrUnsupported/ErrResourceLimit` | 单测：`errors.Is` 链 |
| 9 | `feat[frame]: add pixel format definitions and plane layout` | `PixelFormat`、`Plane`、`Frame` | 编译 + 枚举测试 |
| 10 | `feat[frame]: add frame validation with stride and size checks` | `Validate`、溢出检查 | 表驱动：非法 stride/溢出/位深 |
| 11 | `feat[packet]: add access unit packet type` | `Packet`（含 DTS/KeyFrame） | 编译 |
| 12 | `feat[config]: add encoder and decoder configuration defaults` | `EncoderConfig`/`DecoderConfig` + 默认值 | 默认值测试 |
| 13 | `feat[config]: add configuration validation and resource limits` | 分辨率/码率/QP/上限校验 | 边界测试 |
| 14 | `docs[architecture]: document layering dependency rules and ownership` | 分层、R1–R6、所有权表 | — |
| 15 | `test[deps]: add import graph checker for dependency rules` | 脚本化 R1/R2/R5 | 故意违规能被抓 |
| 16 | `docs[status]: add feature matrix and translation ledger templates` | 两份追踪文档骨架 | — |
| 17 | `test[testdata]: add corpus manifest schema and hash validator` | schema + 校验工具 | 缺文件/哈希错必须报错 |
| 18 | `test[oracle]: add ffmpeg oracle harness for yuv diffing` | 解码到 raw YUV、逐平面比较、首差异定位 | 对已知样本产出报告 |
| 19 | `bench[bench]: add benchmark skeleton and hardware registration doc` | bench 目录 + 报告模板 | `go test -bench` 可跑 |
| 20 | `chore[release]: close milestone m0 gate and tag v0.1.0` | body 勾选 M0 退出标准、附证据 | gate 全部勾选 |

---

## M1 — Bitstream Foundation

**分支：** `feat/m1-bitstream-foundation` ｜ **目标：** 可解析并重新序列化合法 NAL/SPS/PPS ｜ **预计提交：** 32

| # | Commit | 内容 | 验证 |
|---|---|---|---|
| 1 | `feat[bitstream]: add n-bit reader with reservoir and bounds checks` | 64-bit reservoir、`ReadBits`、position、越界错误 | 表驱动 + 截断测试 |
| 2 | `test[bitstream]: add reader fuzz target for truncation and overflow` | fuzz 入口与初始语料 | `go test -fuzz` 短时 |
| 3 | `feat[bitstream]: add exp golomb ue and se decoding` | `ReadUE`/`ReadSE`、超长码字保护 | 已知码字表 |
| 4 | `feat[bitstream]: add n-bit writer with byte alignment` | `WriteBits`、`ByteAlign`、容量增长策略 | round-trip |
| 5 | `feat[bitstream]: add exp golomb ue and se encoding` | `WriteUE`/`WriteSE` | round-trip |
| 6 | `test[bitstream]: add reader writer round trip matrix` | 1..64 bit、极值、跨字节对齐 | 全绿 |
| 7 | `feat[bitstream]: add rbsp to ebsp emulation prevention` | 插入 0x03 规则 | 定向用例 |
| 8 | `feat[bitstream]: add ebsp to rbsp removal with validation` | 移除 + 非法序列报错 | 定向用例 |
| 9 | `feat[bitstream]: add rbsp trailing bits validation` | `rbsp_stop_one_bit` + 补零校验 | 非法尾位必须报错 |
| 10 | `test[bitstream]: add rbsp round trip on synthetic and real nal` | 真实 NAL 抽取往返 | 与原始字节一致 |
| 11 | `feat[nal]: add nal unit header parsing and type table` | header 字段、type 1–23 命名、保留类型策略 | 全类型表 |
| 12 | `feat[nal]: add annexb scanner with start code recovery` | 3/4 字节起始码、连续零处理 | 样本扫描 |
| 13 | `test[nal]: add annexb chunk split matrix tests` | 1/2/3/7/4096/整文件切分 | 输出序列一致 |
| 14 | `feat[nal]: add avcc reader for one to four byte length prefixes` | 长度前缀解析 + 越界保护 | 定向用例 |
| 15 | `feat[nal]: add avcc writer with configurable length size` | 1–4 字节前缀输出 | round-trip |
| 16 | `feat[nal]: add avcc decoder configuration record parser` | avcC（含 SPS/PPS 数组） | 真实 avcC 解析 |
| 17 | `feat[nal]: add avcc configuration record writer` | avcC 生成 | parse→serialize→parse 深比较 |
| 18 | `feat[nal]: add access unit boundary detection rules` | AUD / `first_mb_in_slice` / `frame_num` / POC 变化 | 判定用例 |
| 19 | `feat[nal]: add access unit assembler with buffering rules` | AU 组装、不完整数据等待 | 多 slice 样本 |
| 20 | `feat[syntax]: add sps parsing for baseline through high profiles` | profile/level、frame_mbs_only、crop、VUI 挂接 | 真实 SPS 表 |
| 21 | `feat[syntax]: add sps serialization with round trip guarantee` | 字段完整回写 | `parse→serialize→parse` 相等 |
| 22 | `feat[syntax]: add pps parsing and serialization` | entropy 模式、slice groups 标志、deblock 参数 | round-trip |
| 23 | `feat[syntax]: add vui parsing and serialization` | 宽高比、timing、HRD 标志位、bitstream restriction | round-trip |
| 24 | `feat[syntax]: add scaling list parsing and serialization` | 4×4/8×8、`useDefaultScalingMatrix`、delta 编码 | round-trip |
| 25 | `feat[syntax]: add sei message framing and user data parsing` | SEI payload 类型/长度、user_data_unregistered | 定向用例 |
| 26 | `feat[syntax]: add minimal slice header parsing for au boundaries` | `slice_type`、`frame_num`、`pic_order_cnt_lsb` | AU 判定依赖通过 |
| 27 | `feat[syntax]: add unsupported syntax detection with explicit errors` | 未支持工具 → `ErrUnsupported` | 定向用例 |
| 28 | `test[syntax]: add round trip corpus tests for sps pps vui` | 语料全量 round-trip | 全量通过 |
| 29 | `test[oracle]: add nal level comparison against ffprobe output` | ffprobe JSON 与解析结果对照 | 差异即失败 |
| 30 | `test[testdata]: add baseline and main corpus entries with hashes` | manifest 条目 | 校验工具通过 |
| 31 | `docs[status]: mark bitstream nal and syntax modules conformant` | 三份文档状态行 | — |
| 32 | `chore[release]: close milestone m1 gate and tag v0.2.0` | 退出标准证据 | gate 全部勾选 |

---

## M2 — Decoder Baseline

**分支：** `feat/m2-decoder-baseline` ｜ **目标：** Baseline I/P 与 FFmpeg 逐像素一致 ｜ **预计提交：** 36

| # | Commit | 内容 | 验证 |
|---|---|---|---|
| 1 | `feat[picture]: add plane allocation with stride alignment` | 对齐分配、独立 stride | 尺寸/对齐测试 |
| 2 | `feat[picture]: add frame buffer pool with explicit ownership` | `Release`、复用、并行安全 | 分配/释放计数 |
| 3 | `feat[transform]: add scan tables for 4x4 and 4x4 dc` | zigzag 反扫描表 | 表一致性测试 |
| 4 | `feat[transform]: add inverse quantization for 4x4 blocks` | 规范公式、QP 6 周期、clip | 解析解除表 |
| 5 | `feat[transform]: add inverse 4x4 integer transform` | 行列两遍、二次移位 | 与解析解对比 |
| 6 | `feat[transform]: add inverse hadamard for luma and chroma dc` | 4×4 luma DC、2×2 chroma DC | 解析解对比 |
| 7 | `test[transform]: add analytic vectors for idct and dequant` | 已知输入→期望系数 | 全绿 |
| 8 | `feat[syntax]: extend slice header for baseline decoding fields` | `ref_pic_list`、`cabac_init_idc` 标志、QP 增量 | 真实 slice header |
| 9 | `feat[decoder]: add decoder skeleton with send receive state machine` | `SendPacket`/`ReceiveFrame`/`Flush`/`Reset` | 状态机测试 |
| 10 | `feat[cavlc]: add nc context derivation and coeff token tables` | nC 公式、total coeff / trailing ones 表 | 边界 nC 测试 |
| 11 | `feat[cavlc]: add residual block decoding with level prefix and suffix` | level 编码、`level_prefix` 转义、suffix length 递增 | 定向用例 |
| 12 | `feat[cavlc]: add total zeros and run before decoding tables` | 全表 + 特殊 `total_zeros=-1` | 表覆盖测试 |
| 13 | `test[cavlc]: add residual decoding traces against reference` | 固定 bit 序列→系数 | 全绿 |
| 14 | `feat[prediction]: add intra 4x4 prediction with nine modes` | 模式、边界不可用回退、`most_probable_mode` | 逐模式用例 |
| 15 | `feat[prediction]: add intra 16x16 prediction with four modes` | DC/V/H/Plane | 逐模式用例 |
| 16 | `feat[prediction]: add chroma 8x8 prediction with four modes` | 边界补齐规则 | 逐模式用例 |
| 17 | `feat[decoder]: add macroblock layer for i slices with pcm support` | `mb_type`、`I_PCM`、`mb_qp_delta` | I_PCM 样本 |
| 18 | `feat[decoder]: add residual reconstruction for luma and chroma` | DC 两级逆变换、残差相加 | 单宏块对比 |
| 19 | `feat[decoder]: add idr picture decoding to raw frame output` | 整帧输出、PTS 映射 | 首帧一致 |
| 20 | `test[decoder]: add pixel exact diff for i only streams` | Oracle 首次接入 | I-only 全通过 |
| 21 | `feat[prediction]: add luma half and quarter pel interpolation` | 6-tap、`(a-5b+20c+20d-5e+f+16)>>5` | 解析解对比 |
| 22 | `feat[prediction]: add chroma bilinear interpolation` | bilinear + 舍入 | 解析解对比 |
| 23 | `feat[motion]: add mv predictor with median rule and edge clipping` | A/B/C 中值、边界替换规则 | 边界用例 |
| 24 | `feat[decoder]: add p slice macroblock parsing and skip handling` | `mb_skip_run`、`P_Skip` | P 样本 |
| 25 | `feat[decoder]: add inter reconstruction with motion compensation` | 整/半/四分之一像素 MC | 单宏块对比 |
| 26 | `feat[filter]: add boundary strength derivation for luma and chroma` | bS 判定（含 intra/skip/coeff 条件） | 定向用例 |
| 27 | `feat[filter]: add luma edge filtering with alpha and beta tables` | bS<4 与 bS==4 两套抽头 | 边界像素对比 |
| 28 | `feat[filter]: add chroma edge filtering` | chroma bS 规则 | 边界像素对比 |
| 29 | `feat[filter]: add deblocking orchestration across macroblock edges` | 水平/垂直、内部 4×4 边界 | 整帧对比 |
| 30 | `feat[dpb]: add reference picture buffer with frame num tracking` | 单参考 + `frame_num` gap 检测 | 定向用例 |
| 31 | `feat[decoder]: add poc type 0 and display order output` | POC 计算、输出排序 | 顺序一致 |
| 32 | `feat[decoder]: add error recovery with wait for idr state` | `WaitingForIDR` 状态机 | 坏样本后接好样本 |
| 33 | `feat[decoder]: add resource limits for resolution nal and dpb` | 上限校验 → `ErrResourceLimit` | 超限测试 |
| 34 | `test[oracle]: add baseline corpus differential suite` | 全量 Baseline 差分 | 逐像素一致 |
| 35 | `docs[status]: mark baseline decoder modules conformant` | 状态 + 已知缺口 | — |
| 36 | `chore[release]: close milestone m2 gate and tag v0.3.0` | 含首份解码基准记录 | gate 全部勾选 |

---

## M3 — Decoder Main / High

**分支：** `feat/m3-decoder-main-high` ｜ **目标：** Main/High 8-bit YUV420 全通过 ｜ **预计提交：** 30

| # | Commit | 内容 | 验证 |
|---|---|---|---|
| 1 | `feat[cabac]: add arithmetic decoder with range and offset state` | `decodeBin`、renormalization | 解析解比特序列 |
| 2 | `feat[cabac]: add context state transition tables` | 64 状态转移 + LPS 表 | 表自检 |
| 3 | `feat[cabac]: add context initialization for slice types` | `init_idc` 0–2 初始化表 | 初值表对比 |
| 4 | `feat[cabac]: add residual block decoding with significant coeff flags` | `coded_block_flag`、`last_significant_coeff`、coeff abs/level | 定向用例 |
| 5 | `feat[cabac]: add macroblock layer binarization for i slices` | `mb_type` UEGk、CBP、`mb_qp_delta` | 定向用例 |
| 6 | `feat[cabac]: add motion vector and ref idx binarization` | `mvd` UEG3、`ref_idx` 一元 | 定向用例 |
| 7 | `chore[cabac]: add generator for context tables` | 生成器 + 生成物 | `go generate` 可复现 |
| 8 | `test[cabac]: add per context initialization and transition tests` | 每 context 独立用例 | 全绿 |
| 9 | `feat[decoder]: add cabac slice decoding path` | 与 CAVLC 分流、`cabac_init_idc` | CABAC I/P 样本 |
| 10 | `test[oracle]: add cabac i and p corpus differential suite` | CABAC 语料差分 | 逐像素一致 |
| 11 | `feat[dpb]: add poc type 1 and type 2 derivation` | `delta_pic_order_cnt`、frame_num 推导 | 定向语料 |
| 12 | `feat[dpb]: add reference list construction for p slices` | list0 排序、多参考 | 多参考语料 |
| 13 | `feat[dpb]: add reference list reordering syntax` | `ref_pic_list_reordering` | 定向语料 |
| 14 | `feat[dpb]: add sliding window and mmco marking` | 滑窗 + MMCO 1–6 | `mmco` 标签语料 |
| 15 | `feat[dpb]: add long term reference handling` | `long_term_pic_num`、`lt_idx` | 定向语料 |
| 16 | `feat[decoder]: add b slice macroblock parsing for both entropy modes` | B 宏块层、`sub_mb_type` | B 样本 |
| 17 | `feat[decoder]: add direct prediction temporal and spatial modes` | direct MV 推导 + colocated 访问 | 定向语料 |
| 18 | `feat[decoder]: add bidirectional prediction and list combination` | 双向加权平均、舍入 | 逐像素对比 |
| 19 | `feat[decoder]: add output reordering and delayed frame emission` | 显示顺序缓冲 | 顺序一致 |
| 20 | `feat[decoder]: add flush semantics emitting remaining dpb frames` | Flush → 排空 → `io.EOF` | 状态机测试 |
| 21 | `feat[prediction]: add weighted prediction explicit and implicit` | `pred_weight_table`、implicit 推导 | 定向语料 |
| 22 | `feat[transform]: add 8x8 inverse transform` | High profile 8×8 | 解析解对比 |
| 23 | `feat[transform]: add scaling matrix application in dequant` | 4×4/8×8 反量化乘矩阵 | 定向用例 |
| 24 | `feat[syntax]: add scaling list selection from sps and pps` | fallback 规则链 | round-trip + 使用 |
| 25 | `feat[filter]: add slice level deblocking control and offsets` | `disable_deblocking_filter_idc`、alpha/beta offset | 定向语料 |
| 26 | `feat[decoder]: add unsupported feature detection with explicit errors` | 字段编码/FMO 等 → `ErrUnsupported` | 定向用例 |
| 27 | `test[decoder]: add flush and eagain state machine tests` | `ErrAgain` 时机、重复 Receive | 全绿 |
| 28 | `test[oracle]: add high profile corpus differential suite` | 全量 High 差分 | 逐像素一致 |
| 29 | `docs[status]: mark main and high decoder modules conformant` | 状态 + 缺口 | — |
| 30 | `chore[release]: close milestone m3 gate and tag v0.4.0` | 含 race + fuzz smoke | gate 全部勾选 |

---

## M4 — Encoder Baseline

**分支：** `feat/m4-encoder-baseline` ｜ **目标：** FFmpeg 可解 + 本地重建一致 ｜ **预计提交：** 28

| # | Commit | 内容 | 验证 |
|---|---|---|---|
| 1 | `feat[encoder]: add encoder skeleton with send receive and gop state` | 状态机、缓冲、参数校验 | 状态机测试 |
| 2 | `feat[syntax]: add sps writing for baseline profile` | 字段回写 + level 校验 | parser round-trip |
| 3 | `feat[syntax]: add pps writing for baseline profile` | deblock/QP 参数 | parser round-trip |
| 4 | `feat[bitstream]: add rbsp writer with trailing bits` | 尾位写入 | 与 parser 对齐 |
| 5 | `feat[nal]: add annexb writer with start codes` | 4 字节起始码策略 | 重扫一致 |
| 6 | `feat[encoder]: add idr picture and slice header writing` | `idr_pic_id`、POC 重置 | 码流合法 |
| 7 | `feat[encoder]: add i pcm macroblock encoding` | 16×16 PCM 写出 | 自解码一致 |
| 8 | `test[encoder]: add i pcm round trip through own decoder` | 编→解逐像素 | 全绿 |
| 9 | `feat[encoder]: add intra mode selection with sad cost` | 9+4+4 模式遍历 | 模式分布合理 |
| 10 | `feat[prediction]: add intra 4x4 mode availability and probable mode` | 边界可用性、MPM 编码 | 定向用例 |
| 11 | `feat[transform]: add forward 4x4 integer transform` | 一次变换 + 缩放 | 与反向对比 |
| 12 | `feat[transform]: add 4x4 quantization with mb qp delta` | 量化、`mb_qp_delta` | 量化往返带 loss 分析 |
| 13 | `feat[transform]: add forward hadamard for dc coefficients` | luma/chroma DC | 解析解对比 |
| 14 | `chore[cavlc]: add generator for encoding vlc lookup tables` | 表生成器 | 可复现 |
| 15 | `feat[cavlc]: add residual block encoding with coeff token lookup` | coeff_token 查表 | 解码器可还原 |
| 16 | `feat[cavlc]: add level and run encoding with escape handling` | level 转义、suffix | 极端分布用例 |
| 17 | `test[cavlc]: add encoder decoder symmetry for extreme coefficients` | 全零/长游程/大 level | 全绿 |
| 18 | `feat[encoder]: add intra 4x4 macroblock encoding` | CBP、`transform_size_8x8_flag=0` | 自解码一致 |
| 19 | `feat[encoder]: add intra 16x16 macroblock encoding` | DC 编码 + 4×4 AC | 自解码一致 |
| 20 | `feat[encoder]: add chroma encoding path` | chroma CBP + DC | 自解码一致 |
| 21 | `feat[encoder]: add local reconstruction shared with decoder kernels` | 共用 dequant/IDCT | 双路径对比 |
| 22 | `feat[encoder]: add deblocking on local reconstruction` | 共用 filter | 逐像素对比 |
| 23 | `feat[encoder]: add cqp rate control with per macroblock delta` | 固定 QP + 自适应 delta | QP 序列断言 |
| 24 | `test[encoder]: add self consistency of local recon and decoded output` | L == G | 全绿 |
| 25 | `test[oracle]: add ffmpeg decode compatibility suite for encoder output` | FFmpeg 解码 + ffprobe 校验 | 无 warning |
| 26 | `feat[encoder]: add multi slice option per frame` | slice 切分 | 多 slice 码流可解 |
| 27 | `docs[status]: mark baseline encoder modules conformant` | 状态 + 码率/PSNR 基线 | — |
| 28 | `chore[release]: close milestone m4 gate and tag v0.5.0` | gate 证据 | 全部勾选 |

---

## M5 — Encoder Main / High（Inter + RC）

四个子分支独立合入，每个子阶段有自己的收尾提交。

### M5a — Inter（`feat/m5a-inter`，14）

| # | Commit | 内容 | 验证 |
|---|---|---|---|
| 1 | `feat[motion]: add sad kernel with generic backend` | SAD 16×16/8×8 | 逐值对比 |
| 2 | `feat[motion]: add full search with adaptive range` | 搜索范围裁剪 | 边界用例 |
| 3 | `feat[motion]: add diamond search` | 菱形模板收敛 | 与 full search 差距断言 |
| 4 | `test[motion]: add search correctness and boundary clipping tests` | 出界 MV 裁剪 | 全绿 |
| 5 | `feat[encoder]: add p slice skeleton with reference list` | P 帧结构 | 码流可解 |
| 6 | `feat[encoder]: add mv prediction and mvd coding` | 中值预测 + 差分编码 | 解码器一致 |
| 7 | `feat[encoder]: add inter 16x16 macroblock encoding` | 残差 + CAVLC | 自解码一致 |
| 8 | `feat[encoder]: add inter 16x8 and 8x16 partitions` | 子分割 | 码流可解 |
| 9 | `feat[encoder]: add subpixel refinement with quarter pel search` | 半/四分之一细化 | 与 MC 一致 |
| 10 | `feat[encoder]: add p skip decision with threshold` | skip 判定 | 码流合法 |
| 11 | `test[oracle]: add p slice decode compatibility suite` | FFmpeg 差分 | 无 warning |
| 12 | `bench[encoder]: add inter encode benchmark` | 720p P-only | 记录基线 |
| 13 | `docs[status]: mark inter prediction modules conformant` | 状态 | — |
| 14 | `chore[release]: close milestone m5a gate and tag v0.5.1` | 证据 | 勾选 |

### M5b — RD 与模式决策（`feat/m5b-mode-decision`，8）

| # | Commit | 内容 | 验证 |
|---|---|---|---|
| 1 | `feat[transform]: add satd via hadamard kernel` | SATD 16×16/8×8 | 与解析解对比 |
| 2 | `feat[encoder]: add rd cost with lambda from qp` | `J = D + λR` | 单调性测试 |
| 3 | `feat[encoder]: add rate estimation for mv and header bits` | 比特估算模型 | 误差上限断言 |
| 4 | `feat[encoder]: add full mode decision across intra and inter` | 全模式比较 | 决策覆盖测试 |
| 5 | `feat[encoder]: add early skip and early terminate heuristics` | 剪枝阈值 | 输出不变/质量损失上限 |
| 6 | `feat[encoder]: add adaptive qp from block activity` | 活动度 → QP delta | QP 范围断言 |
| 7 | `test[encoder]: add rd decision coverage and monotonicity tests` | 覆盖率 | 全绿 |
| 8 | `chore[release]: close milestone m5b gate and tag v0.5.2` | 质量对比报告 | 勾选 |

### M5c — Main / High（`feat/m5c-main-high`，15）

| # | Commit | 内容 | 验证 |
|---|---|---|---|
| 1 | `feat[cabac]: add arithmetic encoder with renormalization` | `encodeBin`、range 更新 | 已实现；规范区间向量见 `internal/entropy/cabac` |
| 2 | `feat[cabac]: add context encoding for macroblock and residual` | 全部相关 context | I/P 4×4/8×8/DC/AC及B_Skip/B_Direct残差已验证；B非Direct分区不作完整覆盖承诺 |
| 3 | `feat[cabac]: add cabac termination and zero word handling` | terminate / `cabac_zero_word` | terminate/PCM restart 已验证；逐 slice bin密度预算、终端03封装及5000个MPS密集流读回已验证 |
| 4 | `feat[encoder]: add cabac slice writing path` | 熵模式分流 | I/P Main/High slice 已接入 |
| 5 | `test[cabac]: add encoder decoder symmetry for full slices` | 整 slice 对称 | 288 帧 Main/High 三方像素 Oracle 通过 |
| 6 | `feat[encoder]: add b slice with direct mode strategy` | B 帧编码 | 非参考spatial B_Direct/B_Skip、双列表零MV预测与压缩残差；1120帧三方Oracle |
| 7 | `feat[encoder]: add b frame reordering and poc assignment` | 输出 DTS 顺序 | POC type0、未来锚点先输出、规则PTS/Duration、递增DTS与Flush已验证；B+Lookahead场景边界已测 |
| 8 | `feat[transform]: add forward 8x8 transform` | 8×8 变换 | 规范逆基定点投影与High Intra8/P 8×8残差候选、信令/共享重建/滤波已接入；320帧三方Oracle |
| 9 | `feat[transform]: add scaling matrix in forward path` | 量化矩阵 | 4×4六列表/DC首项与8×8 intra/inter自定义矩阵PPS/量化/共享重建已验证；flat兼容、矩阵组合320帧及4×4/DC+B126帧三方Oracle |
| 10 | `feat[prediction]: add weighted prediction in encoder` | explicit 权重 | Main/High 单参考 P 已实现；160帧三方 Oracle、权重表解析与实际加权 Skip；B 权重后续接入 |
| 11 | `feat[syntax]: add main and high profile sps writing` | profile_idc 分支 | 已实现并经 ffprobe/FFmpeg Main/High I/P 验证 |
| 12 | `test[oracle]: add main and high decode compatibility suite` | 全 profile 差分 | I/P288、P权重160、High8×8/矩阵320、Main/High B+码控1120帧三方Oracle |
| 13 | `bench[encoder]: add cabac and b frame benchmarks` | 性能记录 | CABAC与真实四帧B闭环各5次、分配与60帧CPU profile已记录performance.md §11；注明并发负载，不作加速承诺 |
| 14 | `docs[status]: mark cabac and b slice modules conformant` | 状态 | 已登记CABAC I/P/High8×8、P权重、非参考spatial B_Direct/B_Skip及重排的具体支持边界 |
| 15 | `chore[release]: close milestone m5c gate and tag v0.5.3` | 证据 | 本地门禁关闭，标签v0.5.3：两尺寸三QP长Oracle/质量、完整verify/race与六目标构建；见 `docs/gates/m5c.md`，远端CI未执行 |

### M5d — GOP 与码率控制（`feat/m5d-rate-control`，10）

| # | Commit | 内容 | 验证 |
|---|---|---|---|
| 1 | `feat[encoder]: add gop structure with idr interval and b placement` | `IBBP`/`IPPP` | 连续1/2/3/16个B、独立GOP/IDR、强制关键帧、规则PTS/DTS、尾部Flush与场景边界已验证 |
| 2 | `feat[encoder]: add lookahead window for scene change detection` | 小窗口分析 | WithLookahead(1..16)窗口分析、未来帧确认切换/抑制闪光、输入深拷贝/背压/失败事务/Flush排空已测试 |
| 3 | `feat[encoder]: add abr rate control` | 目标码率 → QP | 两尺寸各300帧平均偏差+0.475360%/−0.769220%，120帧短回归通过 |
| 4 | `feat[encoder]: add cbr rate control with buffer model` | 固定码率 | 两尺寸各300帧十个窗口达目标；静态filler与分数FPS60帧码率≤1% |
| 5 | `feat[encoder]: add vbv constraint enforcement` | VBV 上下界 | 两尺寸各300步独立充放水与边界核验；120帧High/B2突变压力、上下边界和失败事务通过 |
| 6 | `feat[encoder]: add scene change triggered idr` | 自适应 IDR | 当前帧检测与Lookahead未来帧确认、闪光抑制、实际IDR序列和三方Oracle通过 |
| 7 | `test[encoder]: add bitrate accuracy and vbv window tests` | 长序列 | 720p/1080p×ABR/CBR/VBV各300帧，三方Oracle、完整QP/AU/时钟/缓冲轨迹及ffprobe帧型通过 |
| 8 | `bench[encoder]: add end to end encode benchmark for 720p` | 全参数矩阵 | §11五次基准、§13候选与反馈前后样本/300帧CPU profile、§14完整长测延迟/堆内存均公开；并发负载不作实时承诺 |
| 9 | `docs[performance]: publish encoder quality and bitrate report` | PSNR/SSIM/码率 vs x264 | §12历史五过一失败保留；§14新六项同QP全部通过，实测匹配码率差距、PSNR/SSIM和哈希完整公开 |
| 10 | `chore[release]: close milestone m5d gate and tag v0.6.0` | 总证据 | 本地门禁关闭，标签v0.6.0；见 `docs/gates/m5d.md` 和 `m5cd_results.json`，远端CI未执行 |

---

## M6 — 完整流式处理

**分支：** `feat/m6-streaming` ｜ **目标：** 有界内存 + 可取消 + 任意分块 ｜ **预计提交：** 19

| # | Commit | 内容 | 验证 |
|---|---|---|---|
| 1 | `feat[stream]: add frame reader and writer interfaces` | 四个接口 | 已有具体适配器与pipeline；所有权/EOF/同步消费及错误释放测试通过 |
| 2 | `feat[stream]: add yuv reader for 420 422 and 444` | 8-bit 读帧 | 已完成；已知字节布局、奇数尺寸、所有截断位置、EOF/所有权/资源/I/O错误测试通过 |
| 3 | `feat[stream]: add yuv writer with stride handling` | 写出对齐/非对齐 | 已完成；已知字节、非对齐stride/最小末行、三格式多帧往返、短写/失败事务测试通过 |
| 4 | `feat[stream]: add incremental annexb reader with chunk buffering` | 跨 chunk 起始码/NAL | 已完成；复用有界Scanner，严格前导/尾部策略、独立NAL输出、EOF与I/O错误状态测试通过；AU组装待第7项 |
| 5 | `test[stream]: add chunk size matrix for annexb reader` | 1/2/3/7/4096/整文件 | 与第4项同次验证固定字节向量一致；包含EOF前边界交付、转义零和混合起始码 |
| 6 | `feat[stream]: add incremental avcc reader with configurable length size` | avcC 自动识别 | 已完成；1..4前缀×分块矩阵、逐处截断/限额/I/O错误、从提供的avcC取宽度及参数集快照；不猜测MP4/流格式 |
| 7 | `feat[stream]: add access unit assembler with configurable rules` | 边界规则参数化 | 已完成；图像标识/first_mb模式、跨filler保留VCL摘要、AU字节/数量限额；无AUD多slice、POC0/1/2、分块与5份语料ffprobe计数通过 |
| 8 | `feat[stream]: add annexb writer` | 流式写 | 已完成；完整封装先验证、四字节起始码、NAL/AU/数量限额、短写/错误粘性；固定输出字节与High多slice语料重扫一致 |
| 9 | `feat[stream]: add avcc writer with length prefix` | 长度前缀 | 已完成；1..4大端前缀独立字节解析解、255/65535容量边界、写前封装/限额验证与短写粘性；5份语料×2/3/4宽度×6种chunk逐AU往返一致 |
| 10 | `feat[stream]: add stream pipeline with bounded buffering` | 同步单输入，无额外队列 | 默认16MiB载荷预算、内置YUV读前限界/AU组装前限界，自定义源Send前复核；超限和失败释放测试通过 |
| 11 | `feat[stream]: add context cancellation and backpressure` | Context端点与单帧边界检查 | 阻塞输入/输出取消、慢消费者停产测试通过；普通永久阻塞I/O必须由所有者中断，不遗留后台Read |
| 12 | `feat[stream]: add flush and eof handling in pipeline` | 自动Flush至排空EOF | PCM末帧、B/Lookahead不足尾组和时间戳测试通过；损坏尾部不误报EOF |
| 13 | `feat[stream]: add encode stream and decode stream helpers` | EncodeStream/DecodeStream | 26份语料×Annex-B/AVCC×1/7/4096/整文件chunk对独立Oracle哈希；真实CLI与FFmpeg像素烟测通过 |
| 14 | `test[stream]: add short memory stability test` | tools/streamcheck执行30秒稳定性验收 | 预热5秒、每5秒采样，预先固定RSS/堆/增长率阈值；已有30秒报告通过；提交fedb1d9，见gates/m6.md |
| 15 | `test[stream]: add goroutine leak checks for cancellation` | 确定性阻塞与显式join | 200次输入取消、输出取消释放、goroutine计数与owned管道中断测试通过；CGO=1非缓存全库race通过 |
| 16 | `feat[cmd]: add h264dec command line tool` | 参数/资源预检、文件/标准流、stderr进度 | 真实进程Annex-B/AVCC二进制管道、独立FFmpeg像素对照；同文件/截断/写错/取消测试通过 |
| 17 | `feat[cmd]: add h264enc command line tool` | Profile/QP/GOP/B/码控/FPS/工具选项 | PCM原始字节、High/B/Lookahead、合法码控、错误配置保留输出、文件别名保护与进度测试通过 |
| 18 | `feat[cmd]: add h264probe command line tool` | -syntax逐NAL JSONL，保留默认GOP模式 | 共享语法结构、嵌套VUI/矩阵/SEI、严格封装、限额；Baseline/Main/High与ffprobe元数据/帧数对照通过 |
| 19 | `chore[release]: close milestone m6 gate and tag v0.7.0` | 证据与发布 | 本地功能、常规门禁、全库race、六目标构建及30秒稳定性验收通过；远端CI未执行，尚未创建发布标签 |

---

## M7 — 高级 AVC 特性

**分支：** 每个特性一个短分支（`feat/m7-high10`、`feat/m7-444`…）｜ **预计提交：** 24

| # | Commit | 内容 | 验证 |
|---|---|---|---|
| 1 | `refactor[picture]: parameterize pixel layout by bit depth and chroma format` | New/Pool.Get显式PixelFormat，平面几何与池分桶参数化（8-bit行为不变） | 2026-11-15完整常规门禁通过，见implementation-status.md M7第1项 |
| 2 | `feat[picture]: add 10 bit plane storage and sample accessors` | uint16存储/采样、边缘复制、位深裁剪、格式隔离池 | 10-bit构造测试先失败后通过；边界/极值/奇数stride/存储溢出/池隔离及完整常规门禁通过 |
| 3 | `feat[transform]: add 10 bit transform and quantization depth` | 语法QPY转QP'，共享内核QP'0..63，DC临时量int64，逆变换仍归一化>>6 | 负QPY/色度非线性/QP'52..63/正负1023解析基/溢出测试及完整常规门禁通过；流水线未接入 |
| 4 | `feat[decoder]: add high10 decoding` | 渐进10-bit420全部既有工具、负QP、原生LE与资源/恢复 | 七组动态外部压缩Oracle、三份18帧真实语料、PCM/QP/恢复/裁剪解析解及独立提交完整常规门禁通过 |
| 5 | `feat[encoder]: add high10 encoding` | 原生10-bit压缩、双熵与既有I/P/B/码控/前瞻/矩阵工具、CLI | 32配置×12帧三方像素Oracle、真实CLI与FFmpeg原始LE哈希、独立提交完整常规门禁通过 |
| 6 | `feat[picture]: add 422 plane geometry and chroma handling` | 8/10-bit422几何、全高色度、边缘与池，复用矩形色度预测 | 新布局先红后绿；独立尺寸/极值/池隔离、8×16预测解析解及独立完整常规门禁通过 |
| 7 | `feat[decoder]: add high422 decoding` | 渐进8/10-bit422、双熵I/P/B、矩阵/权重/PCM/全高去块与原生输出 | 两格式×九配置各八帧FFmpeg Oracle、PCM/QP/裁剪/切换/预算解析用例及独立完整常规门禁通过 |
| 8 | `feat[picture]: add 444 chroma support` | 8/10-bit444全尺寸三平面、边缘/池；十四类CABAC与十二列表共享前置 | 几何/极值/六格式池、规范模型/bin轨迹、4096列表组合及独立完整常规门禁通过 |
| 9 | `feat[decoder]: add high444 decoding` | 共同444全尺寸三平面、扩展CABAC与独立色平面组装 | 两格式×九配置各八帧x264/FFmpeg一致；独立平面PCM解析解/单色投影、预算/覆盖/恢复及独立完整常规门禁通过 |
| 10 | `feat[encoder]: add high422 and high444 encoding` | 原生8/10-bit422/444双熵I/P/B、矩阵/权重/码控、PCM与独立平面 | 六格式QP极值/宏块QP/矩阵/权重/B/码控三方Oracle、独立平面单色投影、AU预算/重试、四格式真实CLI及独立完整常规门禁通过 |
| 11 | `feat[syntax]: add field picture and mbaff syntax parsing` | 帧/场/MBAFF SPS组合、map unit几何/裁剪，slice场奇偶与POC条件字段 | 固定规范字节/写前拒绝、真实Main隔行h264probe、独立完整常规门禁通过；像素流水线保持显式拒绝 |
| 12 | `feat[prediction]: add field motion vector prediction` | 帧场MV/ref_idx归一化、MBAFF参考奇偶、定向MVP与P_Skip，共享既有中值内核 | 负奇数/索引边界/可用性/C→D/分区/Skip规范手算及独立完整常规门禁通过 |
| 13 | `feat[decoder]: add mbaff macroblock pair handling` | 双熵宏块对/Skip场标志、混合邻域、场参考/扫描、Direct、逐对去块与420场相位 | 192配置共1152帧外部Oracle、混合PCM/对侧场规范码流、登记语料固定哈希、实际CLI及独立完整常规门禁通过；PAFF仍拒绝 |
| 14 | `feat[encoder]: add interlaced encoding with field order option` | WithInterlaced及-field-order，真实全场MBAFF、场预测/扫描/重建/滤波与pic_timing | 六格式双熵/场序/多slice/B/矩阵/权重/负QP/PCM三方Oracle、跨行双Skip回归、独立完整门禁及实际CLI逐帧场序通过 |
| 15 | `feat[syntax]: add fmo slice group map parsing` | PPS type0–6、动态cycle、map-unit/帧场宏块映射与NextMBAddress | 规范固定字节/七类手算/坏表与分配前限额及独立完整常规门禁通过；像素解码见第16项 |
| 16 | `feat[decoder]: add fmo and aso macroblock ordering` | 组内地址遍历、覆盖所有权、双类邻域隔离和ASO排序 | 七类PCM/I4/P-MVD/skip规范像素、坏覆盖恢复、独立完整门禁及实际CLI逐字节比较通过 |
| 17 | `feat[syntax]: add data partition nal types` | 类型2/3/4前缀、关联快照、条件字段与数据位偏移 | 独立规范bit串、逐bit截断、错误/资源分类及完整独立常规门禁通过；重组见第18项 |
| 18 | `feat[decoder]: add data partition reassembly` | A/B/C按slice_id关联、分类独立reader、AU与探测CLI | PCM/I16/P/混合非零规范像素、坏分区恢复、独立完整门禁及实际CLI两帧逐字节一致 |
| 19 | `feat[decoder]: add sp and si slice support` | 8.6切换核心、QP/QS分域、SP Skip、SI宏块映射与逐块反馈 | 正负DC/AC/色度解析解、真实Annex-B整帧及独立完整常规门禁通过 |
| 20 | `feat[decoder]: add redundant slice handling` | 独立表示、primary优先与最小完整备用、输出和DPB单次提交 | 重叠/ASO/不完整回退/非零MVD/坏覆盖恢复规范像素及独立完整门禁通过 |
| 21 | `feat[syntax]: add hrd parameters parsing and writing` | 完整VUI/HRD、参数集上下文与Level/Profile声明校验 | 固定规范字节、多CPB/尺度/延迟/预算/原子拒绝，旧非法合成声明重建且Oracle像素不变，独立完整门禁通过 |
| 22 | `test[oracle]: add advanced profile corpus suite` | 25份高级码流与清单Oracle来源；M2/M3全部27份High10保工具投影 | 新增76帧原生像素/实际CLI一致、FFmpeg与解析解来源分列、严格High10投影Oracle及55条清单/完整门禁通过 |
| 23 | `docs[status]: publish advanced feature support matrix` | 当前逐项能力、组合限制、原生I/O、Oracle来源与本地门禁 | 矩阵/README/stream同步及相对链接核对；证据详见gates/m7.md与m7_corpus.json |
| 24 | `chore[release]: close milestone m7 gate and tag v0.9.0` | 本地五项退出证据与注释标签v0.9.0，不执行远端发布 | 常规门禁/55条清单/27份High10投影/race/六目标构建全部通过，见gates/m7.md；远端CI未执行 |

---

## M8 — SIMD 与并行优化

v1.0.2 之后的自主性能优化顺序见 [解码性能优化计划](decoder_performance_plan.md)：先整边去块、块级加权和 bS 数据复用，再按实测热点推进插值、重建和 CABAC；该计划当前待实施，不改变下列历史里程碑状态。

**分支：** `perf/m8-*` 若干短分支 ｜ **目标：** 结果不变 + 可复现加速 ｜ **预计提交：** 25

| # | Commit | 内容 | 验证 |
|---|---|---|---|
| 1 | `feat[simd]: add runtime dispatch with cpu feature detection` | `cpuid`/`HWCAP` 检测 | 强制降级路径测试 |
| 2 | `feat[simd]: add kernel registry with generic fallbacks` | 注册表 + 默认实现 | 注册完整性测试 |
| 3 | `test[simd]: add parity harness for generic and simd kernels` | 逐元素等价框架 | 接入即用 |
| 4 | `feat[simd]: add sse2 sad 16x16 kernel` | SSE2 | parity + bench |
| 5 | `feat[simd]: add avx2 sad 16x16 kernel` | AVX2 | parity + bench |
| 6 | `feat[simd]: add arm64 neon sad 16x16 kernel` | NEON | parity + bench |
| 7 | `feat[simd]: add sse2 and avx2 satd kernels` | SATD | parity |
| 8 | `feat[simd]: add neon satd kernel` | NEON | parity |
| 9 | `feat[simd]: add simd motion compensation and subpixel interpolation` | MC/插值 | parity（含奇数尺寸） |
| 10 | `feat[simd]: add simd 4x4 and 8x8 transforms` | 正反变换 | parity |
| 11 | `feat[simd]: add simd quantization and dequantization` | 量化 | parity |
| 12 | `feat[simd]: add simd deblocking` | 边缘滤波 | parity |
| 13 | `feat[simd]: add simd residual add and subtract` | 残差 | parity |
| 14 | `test[simd]: add non aligned stride and odd size parity tests` | 边界矩阵 | 全绿 |
| 15 | `feat[buffer]: add frame and picture pools` | 池化 | 分配计数 |
| 16 | `feat[buffer]: add scratch buffer reuse per slice` | 临时缓冲 | 分配计数 |
| 17 | `perf[decoder]: remove allocations from macroblock hot path` | 零分配 | `AllocsPerRun == 0` |
| 18 | `perf[encoder]: remove allocations from mode decision hot path` | 零分配 | `AllocsPerRun == 0` |
| 19 | `feat[decoder]: add slice level parallel decoding` | slice 并行 | 与串行逐比特一致 + race |
| 20 | `feat[encoder]: add frame pipeline for lookahead and encode` | 流水线 | 输出一致 + race |
| 21 | `bench[bench]: add kernel benchmark suite for all backends` | 全 kernel × 后端 | 报告 |
| 22 | `bench[bench]: add end to end benchmark harness across resolutions` | 240p–4K | 报告 |
| 23 | `docs[performance]: publish kernel and end to end performance report` | 对比表 | — |
| 24 | `docs[performance]: revise targets with measured results` | 目标回填/下调 | — |
| 25 | `chore[release]: close milestone m8 gate and tag v0.9.5` | 证据 | 勾选 |

---

## M9 — 生产化与发布

**分支：** `chore/m9-release` ｜ **计划提交：** 最多 3 个收尾提交；没有实际改动的项直接跳过，不凑数。真实缺陷按 §3.1 独立修复，不捆绑进发布提交。

| # | Commit | 内容 | 验证 |
|---|---|---|---|
| 1 | `test[repo]: complete release regression checks` | 复用现有测试、Fuzz 种子、Oracle、分配断言和构建脚本，只补会影响用户的实际缺口；已有覆盖不重写 | 变更路径定向验证；发布前集中运行一次常规门禁，不按旧任务行重复跑全库 |
| 2 | `docs[repo]: prepare v1 release documentation` | 一次整理 README、已有示例、API 所有权/并发/错误说明、支持边界、许可与 CHANGELOG；链接现有一致性和性能报告，不另造报告体系 | 核对内容与实际 API；有改动的示例可运行；未测和不支持项如实保留 |
| 3 | `chore[release]: tag v1.0.0` | 核对版本和已完成检查，提交发布记录并创建注释标签；push/远端 release 仅在用户要求时执行 | 标签指向最终提交；不把未完成检查写成通过 |

**执行约定：**

- 原 16 行是工作清单，不再逐行开发、逐行提交；已有结果和工具能复用就复用。
- 每日 Oracle/race 工作流、性能 canary、独立分配预算 CI、API 快照测试不作为新增交付要求；不为发布新建这些框架。
- 不默认重跑 M8 的长性能矩阵，不新增性能目标，不重复采样，不另建门禁附件。
- 本地 v1.0.0 按 Roadmap 的 M9 精简范围完成：原生多平台、8 小时长稳和远端 CI 不在本轮启动；完整生产验收另行保留，不能因创建标签而记为通过。
- 本节只简化提交与执行组织，不新增编解码能力，不把现有 Partial/WIP 改称完整支持。

---

## 14. 汇总与门禁映射

### 14.1 提交数量

| 里程碑 | 提交数 | 主分支前缀 | 版本标签 |
|---|---|---|---|
| M0 | 20 | `feat/m0-bootstrap` | v0.1.0 |
| M1 | 32 | `feat/m1-bitstream-foundation` | v0.2.0 |
| M2 | 36 | `feat/m2-decoder-baseline` | v0.3.0 |
| M3 | 30 | `feat/m3-decoder-main-high` | v0.4.0 |
| M4 | 28 | `feat/m4-encoder-baseline` | v0.5.0 |
| M5 (a/b/c/d) | 47 | `feat/m5*` | v0.5.1 / v0.5.2 / v0.5.3 / v0.6.0 |
| M6 | 19 | `feat/m6-streaming` | v0.7.0 |
| M7 | 24 | `feat/m7-*` | v0.9.0 |
| M8 | 25 | `perf/m8-*` | v0.9.5 |
| M9 | 最多 3 个收尾提交 | `chore/m9-release` | v1.0.0 |
| **合计** | **最多 264 个计划提交（另计必要修复）** | — | — |

> M0–M8 数量保留原计划下限规则；M9 按本节精简方案执行，不以提交数为配额。一个提交仍须是一个可独立回滚、可验证的逻辑变更；真实缺陷单独修复，无改动不创建空提交。

### 14.2 提交类型 → 必须通过的门禁

| 提交 type | 必过检查 |
|---|---|
| `feat` | build（CGO=0）、该包单测、相关 parity（若有 SIMD）、ledger 更新 |
| `fix` | 上述 + 回归用例（先红后绿） |
| `refactor` | build、全量测试（证明行为不变） |
| `perf` | 等价性测试 + before/after 基准数据入 PR |
| `test` | 新增测试本身通过，且能捕获目标缺陷 |
| `bench` | 基准可运行，结果入 `docs/performance.md` |
| `docs` | 链接可达、格式正确 |
| `ci` | workflow 语法校验 + 本地等价命令通过 |
| `chore[release]` | 该里程碑 Exit Criteria 全部勾选且有证据 |
| 全部 | `go vet`、`gofmt -l` 为空、subject 正则通过 |

### 14.3 里程碑收尾提交模板

```text
chore[release]: close milestone m2 gate and tag v0.3.0

Exit criteria evidence:
- Baseline I/P pixel exact vs ffmpeg: tests/oracle/baseline (12/12 pass)
- Multi slice frames: testdata/manifest.json tags=multi_slice (3/3 pass)
- P_Skip and MV clipping: decoder/inter_test.go, motion/predict_edge_test.go
- CAVLC nC=-1 paths: cavlc/context_test.go, cavlc/residual_test.go
- Unsupported streams return ErrUnsupported: decoder/unsupported_test.go
- 1080p decode baseline recorded: docs/performance.md (bench-1080p-001)
- Allocation baseline recorded: docs/performance.md (alloc/op table)

State updated:
- implementation-status.md: 14 modules -> acceptable status
- supported-features.md: baseline decoder column -> Conformant

Milestone: M2
```

---

## 15. 反例与钩子

### 15.1 必须拒绝的提交

| 反例 | 问题 | 正确做法 |
|---|---|---|
| `fix bug` | 无 type/module，无信息 | `fix[cavlc]: correct total zeros for 16x16 dc block` |
| `feat[all]: support main profile` | module 无意义，粒度过大 | 按 M3 拆分 12+ 个提交 |
| `update code` | 无动作语义 | 说明具体动作 |
| `feat[decoder]: add b slice and fix idct rounding` | 两件事混在一起 | 拆 `feat` + `fix` |
| 手工改 `tables_gen.go` | 生成物被手改，不可复现 | 改生成器后重新 `go generate` |
| 汇编提交无 parity 测试 | 语义漂移风险 | 同提交加 parity 用例 |
| `wip` 直接进 `main` | 破坏每提交必绿 | 留在本地或 `exp/*` 分支 |
| 修改公共 API 却不写 `Breaking:` | 破坏下游 | footer 声明 + 更新支持文档 |

### 15.2 commit-msg 钩子

```bash
#!/usr/bin/env bash
# .githooks/commit-msg
set -euo pipefail

msg_file="$1"
subject="$(head -n 1 "$msg_file")"

if [ "${#subject}" -gt 72 ]; then
    echo "commit-msg: subject exceeds 72 characters (${#subject})" >&2
    exit 1
fi

if ! printf '%s' "$subject" | grep -Eq \
    '^(feat|fix|refactor|perf|test|bench|docs|build|ci|chore|style|revert)\[[a-z0-9]+(/[a-z0-9]+)?\]: [a-z0-9][^.]*$'; then
    echo "commit-msg: expected 'type[module]: lowercase english subject'" >&2
    echo "  got: $subject" >&2
    exit 1
fi

case "$subject" in
    *" and "*) echo "commit-msg: subject contains 'and', split the commit" >&2; exit 1 ;;
esac
```

安装：`git config core.hooksPath .githooks`

仓库内已提供实现：`.githooks/commit-msg`（上文为规范说明，实际行为以该文件为准；
其中 `and` 检查是**提醒**而不是阻断，避免为了过钩子而写出不自然的英文）。

### 15.3 日常命令

```bash
make verify                                  # fmt + vet + build + test (CGO=0)
CGO_ENABLED=0 go test ./internal/entropy/... # 单包快速循环
CGO_ENABLED=0 go test -run TestCAVLC -v ./internal/entropy/cavlc
CGO_ENABLED=0 go test -bench BenchmarkSAD ./internal/motion -benchmem
CGO_ENABLED=0 GOARCH=arm64 go build ./...    # 交叉编译
git tag -a v0.3.0 -m "milestone m2: baseline decoder pixel exact"
```

---

**执行要求：** 每个 `chore[release]` 提交必须能指向该里程碑全部退出标准的证据；任何"先合入、后补测试"的路径视为违反本文档 §3.1。
