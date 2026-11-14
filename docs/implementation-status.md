# 实现状态（Translation Ledger）

> 本文件逐规范模块记录"实现到什么程度、边界在哪、由什么测试保证"。
> 目的：避免出现"代码看起来完成，但某些合法码流其实不支持"的隐性缺口。
>
> 更新规则：
> - 状态变化必须与代码在同一个 PR 内更新；
> - `Conformant` 必须有 Oracle 或规范解析解测试作为证据；
> - 已知缺口必须具体到触发条件（例如"`mb_adaptive_frame_field_flag=1` 时未处理"），
>   不接受"部分支持"这类模糊描述。

## 状态定义

| 状态 | 含义 |
|---|---|
| `TODO` | 未开始 |
| `WIP` | 已开工，主路径可跑但未验证 |
| `Partial` | 主路径验证通过，存在已登记的缺口 |
| `Conformant` | 通过规定验证（Oracle 逐像素一致） |
| `Unsupported` | 代码显式拒绝并返回 `ErrUnsupported` |

## M0：工程基础

> 本阶段代码与测试已在本地 go1.27.4 windows/amd64 通过（`go build/vet/test`、
> 依赖检查、语料校验、格式检查），但**尚未在 CI 上执行过**。
> 因此下表状态保持 `WIP`；CI 首次全绿后统一改为 `Conformant`，这一步是 M0 门禁的一部分。

| Module | 规范/职责 | Scope | Status | Tests | Known gaps |
|---|---|---|---|---|---|
| 公共错误模型 | — | 5 个哨兵错误 + 分类 | WIP | `errors_test.go` | — |
| `Frame` 类型与几何 | — | 6 种像素格式、平面几何、深拷贝、释放 | WIP | `frame_test.go` | 高位深取值范围校验待 M7 |
| `Frame.Validate` | — | 结构自洽 + 溢出检测 | WIP | `frame_validate_test.go` | 不施加分辨率上限（属策略层） |
| `Packet` | — | AU 载荷、时间戳、深拷贝 | WIP | `packet_test.go` | Side Data 待 M6 |
| `EncoderConfig` / `DecoderConfig` | — | 默认值 + 校验 | WIP | `config_defaults_test.go`、`config_validate_test.go` | 高 bit depth 的 QP 上限 63 待 M7 |
| 依赖规则检查 | 架构 R1/R2/R3/R5 | AST 级 import 图 | WIP | `importgraph/graph_test.go` | R4/R6 靠 review |
| 语料清单校验 | — | 结构 + 枚举 + 哈希 | WIP | `manifest/manifest_test.go` | — |
| YUV 差分 | — | 逐平面逐采样精确比较 | WIP | `yuvdiff/yuvdiff_test.go` | 仅支持紧密布局（约定） |
| Oracle 工具 | — | FFmpeg 解码 + 差分 CLI | WIP | 人工验证 | 尚无真实语料（M1 接入） |

## M1：Bitstream Foundation

> 验证环境：go1.27.4 windows/amd64（本地，2026-11-13）。
> 真实语料：`testdata/baseline`、`testdata/main`（x264，64×48，8-bit 4:2:0）。
> ffprobe Oracle 只在装有 FFmpeg 的环境执行；缺少 ffprobe 时测试显式 skip。

| Module | 规范条款 | Scope | Status | Tests | Known gaps |
|---|---|---|---|---|---|
| `bitstream.Reader` | — | 0..64 bit、Peek/Skip/Align；失败不推进位置 | Conformant | `reader_test.go`、`fuzz_test.go`、`roundtrip_test.go` | — |
| `bitstream.Writer` | — | 0..64 bit、字节对齐、非破坏 `Bytes` | Conformant | `writer_test.go`、`roundtrip_test.go` | — |
| Exp-Golomb | 9.1 / 9.1.1 | ue/se 读写、32 前导零上限、uint32/int32 溢出拒绝 | Conformant | `golomb_test.go`、`golomb_write_test.go` | — |
| RBSP ↔ EBSP | 7.3.1 / 7.4.1 | 插入/移除、非法 00 00 xx 拒绝、尾位校验 | Conformant | `rbsp_test.go`、`rbsp_roundtrip_test.go`（真实 x264 SPS/PPS） | CABAC `cabac_zero_word` 由 M2/M3 的 slice 路径单独处理 |
| NAL Header | 7.3.1 | 单字节头、表 7-1 类型、VCL/参数集分类 | Conformant | `nal_test.go`、`supported_test.go`、语料测试 | — |
| Annex-B 扫描 | B.1 | 3/4 字节起始码、任意分块、前导/尾随零 | Conformant | `annexb_test.go`、`annexb_chunk_test.go`、语料测试 | 前导非零字节按同步恢复策略跳过 |
| AVCC / avcC | ISO/IEC 14496-15 | 1..4 字节长度、8 MiB 上限、avcC 解析与序列化 | Partial | `avcc_test.go`、`avcc_writer_test.go`、`avcconfig_test.go`、`avcconfig_writer_test.go` | 尚无真实 MP4/avcC 语料，目前只有合成记录 |
| Access Unit 组装 | 7.4.1.2.3 | AUD/前缀/后缀、first_mb/frame_num/POC 等判定、显式 hint | Conformant | `accessunit_test.go`、`assembler_test.go`、`corpus_test.go` | 多 slice 且无 AUD 时依赖 SliceInfo hint；suffix SEI 归类保守 |
| SPS 解析与序列化 | 7.3.2.1 | Baseline/Main/High 8-bit、High10渐进420及High422渐进8/10-bit420/422、VUI/ScalingList、裁剪尺寸 | Conformant | 原语法/ffprobe回归及M7 High10/High422外部压缩码流Oracle通过 | 444、隔行、qpprime仍显式拒绝，按M7后续实施项接入 |
| PPS 解析与序列化 | 7.3.2.2 | 核心字段、可选 8×8/scaling 扩展、FMO 拒绝 | Partial | `pps_test.go`、`corpus_test.go` | 无独立 PPS 级 Oracle；依赖 x264 原始字节往返与 manifest 熵模式 |
| VUI | E.1 | 宽高比、overscan、video signal、timing、HRD、bitstream restriction | Partial | `vui_test.go`、`corpus_test.go` | timing/HRD 语义未与独立 Oracle 对比（裸流 ffprobe 帧率不可靠） |
| ScalingList | 7.3.2.1.1.1 | 4×4/8×8、保留原始 delta 序列 | Partial | `scaling_list_test.go`、`sps_write_test.go` | 无自定义矩阵真实语料；应用层（反量化）在 M3 |
| SEI 基础 | 7.3.2.3 | payloadType/Size 扩展、user_data_unregistered | Partial | `sei_test.go`、`corpus_test.go` | 仅框架与 user_data；recovery point 等语义消息在 M2/M3 |
| Slice Header（最小） | 7.3.3 | AU 判定字段、POC type 0/1、field flags、redundant_pic_cnt | Partial | `slice_header_test.go`、`corpus_test.go` | 参考列表/加权预测/去块等字段在 M2 扩展；SP/SI 返回 `ErrUnsupported`（M7） |

## M2：Decoder Baseline

验证环境：Go 1.27.4 windows/amd64、`CGO_ENABLED=0`、FFmpeg/ffprobe 8.0。
五份真实 Baseline 语料合计 21 帧，64×48、176×144、320×240、1920×1080，
含双 slice、重复 IDR、P_Skip 和 1080p crop，Y/U/V 与 FFmpeg 逐像素相等。
独立 Oracle 原始 YUV 哈希保存在 manifest，未安装 FFmpeg 的 CI 仍执行哈希回归。
本地验证不等同于远端 CI 已执行；最终门禁证据见 `docs/gates/m2.md`。

| Module | 规范条款 | Scope | Status | Tests | Known gaps |
|---|---|---|---|---|---|
| `decoder` 状态机 | 公共 API 契约 | Send/Receive、Flush、Reset、Close、参数集按序生效、错误后等待 IDR | Conformant | `decoder_test.go` | 单实例串行；输入须为完整 Annex-B AU，M6 接入 AVCC/增量输入 |
| Macroblock / I_PCM | 7.3.4 / 7.3.5 | I4×4、I16×16、PCM、P16×16/16×8/8×16/8×8 及子分区、Skip | Conformant | PCM 解析解、真实语料 Oracle | FMO/ASO、冗余 slice 在 M7；当前显式拒绝 |
| `entropy/cavlc` | 9.2 | token、level、zeros/run、亮度 DC/AC、nC=-1 色度 DC | Conformant | `decode_test.go`、`tables_test.go`、整帧 Oracle | 4:2:2 色度 DC 在 M7 |
| `prediction` | 8.3 / 8.4.2 | 九种 I4×4、四种 I16×16/色度模式、亚像素 MC、越界复制 | Conformant | 非均匀解析解、`PCMAndEdgeMotionOracle`、真实语料 | 加权预测、B Direct 在 M3 |
| `transform` | 8.5 | 4×4 反量化/IDCT、光栅 DC Hadamard、色度 QP | Conformant | `dc_test.go` 空间基底、极值 IDCT、Oracle | 正变换在 M4；8×8/ScalingMatrix 在 M3 |
| `motion` | 8.4.1 | 分区 MV 预测、slice 邻块隔离、P_Skip 零矢量规则 | Conformant | `mv_test.go`、Oracle | M2 仅 list0 单参考；运动搜索在 M5 |
| `filter` / 调度 | 8.7 | bS、亮度/色度去块、两分量 QP、slice 边界 idc=0/2 | Conformant | `luma_test.go`、`chroma_test.go`、`deblock_test.go`、多 slice Oracle | 无并行/SIMD，M8 优化 |
| `dpb` / POC | 8.2.1 / 8.2.5 | 单参考、frame_num 回绕、type 0/2、非参考帧不覆盖参考状态 | Conformant | `dpb_test.go`、`poc_test.go`、`NonReferenceAndPOC`、Oracle | type 1、多参考/MMCO、显示重排序、缺帧合成在 M3，当前显式拒绝 |
| 资源与坏流 | 架构安全契约 | 配置、NAL 分配前限长、分辨率/帧大小/DPB/slice 上限、覆盖与尾位校验 | Conformant | `ConfigurationAndLimits`、`InvalidSliceCoverage`、NAL 限长、decoder fuzz | MaxFrameSize 限制单帧图像预算，不是整个进程内存上限 |

POC type 2 的无重排路径提前纳入 M2，因为 x264 的 Baseline 无 B 帧样本默认使用
type 2；type 0 使用合成 PCM/P 码流及独立内核测试验证。这里不宣称完整 Baseline
Profile 一致性：高级工具仍按支持矩阵返回 `ErrUnsupported`。

## M3：Decoder Main / High（本地门禁通过，待 CI）

Main 的逐帧 I/P/B、CABAC/CAVLC、多参考、MMCO/长期参考、加权预测和显示重排已接入。
三份 x264 默认 Main 样本（96×64、176×144、320×240，合计 42 帧）包含 B-pyramid、
多参考、隐式加权与 CABAC，全部与 FFmpeg 逐像素一致；另有 CAVLC temporal-direct、
淡出显式 P 权重及三平面不同显式 B 权重的独立语料。全部 27 条语料已登记输入与
独立 Oracle 输出哈希。High 8×8/ScalingMatrix 和 race 已通过验证；完整证据见 `docs/gates/m3.md`。

| Module | 规范条款 | Scope | Status | Tests | Known gaps |
|---|---|---|---|---|---|
| CABAC 算术/初始化 | 9.3.1 / 9.3.3.2 | 460 上下文、64 状态、I/PB0..2、QP 0..51、停止位、PCM restart、zero-word | Conformant | CABAC 解析 trace、全表测试、真实 Oracle、PCM/填充回归 | 4:4:4 上下文 M7 |
| CABAC 残差 | 9.3.3.1.3 | 4×4/8×8 luma、chroma DC/AC、显著性、level 转义 | Conformant | 固定向量、残差 fuzz、Main/High Oracle | 4:4:4 扩展 M7 |
| CABAC 宏块/MV/ref_idx | 9.3.3 | I/P/B、skip、QP/CBP、双列表 MVD、transform8 flag | Conformant | 默认 Main/High B-pyramid、High 多 slice Oracle | 字段与扩展工具 M7 |
| 参考列表与 MMCO | 8.2.4 / 8.2.5 | P/B 默认列表、短期/长期重排、滑窗、MMCO 1..6、图像池所有权 | Conformant | DPB 独立解析向量、两份 MMCO 语料 | 字段参考 M7 |
| POC | 8.2.1 | type 0/1/2、非参考帧、周期、回绕、MMCO5 锚点 | Conformant | POC 向量、type1/MMCO Oracle | 字段 POC M7 |
| B/direct 与去块 | 8.4.1 / 8.7 | 双向预测、spatial/temporal direct、B_Skip、参考身份交换匹配 | Conformant | Main/CAVLC temporal Oracle、B 边界交换单测 | 字段 direct M7 |
| 加权预测 | 8.4.3.2 | 单向显式、双向显式/隐式、三平面权重与偏移 | Conformant | 淡出样本、显式 B 解析样本、权重/距离向量与 Oracle | 8-bit 4:2:0 范围 |
| 显示输出 | 公共 API / VUI | POC 排序、VUI 重排深度、PTS/Duration、Flush、IDR/MMCO5 边界 | Conformant | PTS 0..30000 显示顺序、Again/EOF/重复 Flush、输入复用测试 | 增量字节输入 M6 |
| High / ScalingMatrix | 8.3.2 / 8.5 / 7.4 | 8×8 inverse/prediction、SPS/PPS fallback、默认/自定义矩阵、CAVLC 交织扫描、8×8 去块 | Conformant | 非 DC 组合解析解、矩阵继承单测、七份 High 74 帧 Oracle | 正变换在编码里程碑；字段/高位深 M7 |

全部普通验证使用 Go 1.27.4、CGO_ENABLED=0、FFmpeg 8.0。M2 Baseline 及之前的
Main I/P 回归保持通过。最终清单共 27 条，26 条支持样本 217 帧逐像素/固定哈希回归，
1 条隔行样本明确拒绝。字段不支持及恢复、PTS/Flush、完整 race 均有测试。
正常构建为 CGO=0；race 使用 Go 检测工具链要求的 CGO=1，不引入编解码运行时依赖。
本地标签为 `v0.4.0`，最终逐项证据见 `docs/gates/m3.md`；远端 CI 尚未执行。

## M4：Encoder Baseline（本地门禁通过，待 CI）

| Module | 规范条款 | Scope | Status | Tests | Known gaps |
|---|---|---|---|---|---|
| Annex-B Writer | B.1 | 四字节起始码、EBSP NAL、短写错误 | Conformant | `nal/annexb_writer_test.go` 固定字节与重扫 | — |
| CAVLC 编码 | 9.2 | token、尾随符号、level 转义、zeros/run、4 种块类型 | Conformant | 固定规范码字、14,400 组极端/随机往返、完整 Intra FFmpeg Oracle、fuzz 入口 | 复用现有规范码长/码字表，不生成重复编码表；4:2:2 DC 属 M7 |
| 正向变换与量化 | 8.5（逆路径契约） | 4×4/DC与High8×8自定义矩阵、QP0..51 | Conformant | 正向解析基、flat逐比特兼容、scale32量化解析值、High320帧矩阵组合与126帧4×4/DC+B Oracle | 渐进8-bit4:2:0；QP=0不承诺无损 |
| Encoder 状态与 I_PCM | 7.3.3 / 7.3.5 | 单槽背压、Flush/Reset/Close、独立 IDR、裁剪、1..64 slice、参数集/VUI | Conformant | `encoder/encoder_test.go`、`tests/codec` 自解码与 FFmpeg 逐像素 | 4:2:0 奇数显示尺寸不能用 SPS crop 表达，显式拒绝；无 P/B、GOP 和重排 |
| Intra 编码与本地重建 | 8.3 / 8.5 / 8.7 | I4 九模式、I16/色度各四模式、SAD、MPM、共享预测/反量化/IDCT/相加/滤波、PCM 大小决策 | Conformant | `intra_test.go` 全模式实际决策覆盖；`tests/codec` 27 组尺寸/QP/slice + 120 帧六类图案双解码器 Oracle | 仅渐进式 8-bit 4:2:0，QP=0 仍为有损变换，不等同 PCM |
| CQP / 宏块 QP | 7.3.5 / 表 7-13 | QP 0..51、回调覆盖、delta 回绕、无残差沿用 QP | Conformant | 0↔51/25↔26 等宏块变化与混合 PCM 的双解码器/FFmpeg Oracle | ABR/CBR/VBV、自适应活动度决策在 M5 |
| 参数集与 Level | Annex A / E.1 | Baseline IDR、POC type2、最小 VUI、MaxFS/MBPS/单维上限/保守码率预算 | Conformant | SPS/PPS 结构往返、ffprobe profile/level/尺寸、FFmpeg trace_headers 检查 timing | Level 按最坏 PCM 上限保守选择；非 HRD/VBV 编码器；裸流 ffprobe 帧率猜测不作 VUI Oracle |

普通构建与测试使用 Go 1.27.4、CGO_ENABLED=0。187 帧现场生成的 PCM/Intra/QP/多 slice
语料通过 FFmpeg 与本地重建、自研解码逐像素比较，另 12 帧 720p 测量样本独立哈希一致。
全量 verify、race、两项 fuzz、六目标交叉编译均通过；性能与码率/PSNR 已记录。
完整证据见 `docs/gates/m4.md`，用法见 `docs/encoder.md`。本地标签 `v0.5.0`；远端 CI 尚待执行。

## M5：Encoder Inter / Main / High（进行中）

质量协议已在开工时固定于 `docs/conformance.md`；M5a/b/c/d 各自验证，不能替代整体门禁。

M5a 已完成本地子阶段门禁（`v0.5.1`，远端 CI 待执行）：两种大尺寸各 300 帧、短序列 360 帧、
完整 verify/race、参考帧 fuzz、六目标构建与 P 帧基线已记录。证据见 `docs/gates/m5a.md`。

M5b 本地门禁已通过（`v0.5.2`，远端 CI 待执行）：SATD/RD、全候选对照、活动度QP、
等价剪枝、600帧长Oracle与第一项x264质量比较已记录；Profile边界已补充显式拒绝。
证据见 `docs/gates/m5b.md`。Main/High、B与完整质量/码控由后续M5c/d独立验收。

M5c/d本地门禁已关闭（`v0.5.3` / `v0.6.0`）。
两尺寸×QP18/26/34六项质量及ABR/CBR/VBV六项码控，
每项300帧，共3600帧三方Oracle。固定1.5dB绝对Y差全部通过，最大1.411639dB；
ABR偏差+0.475360%/−0.769220%，CBR十个窗口达目标，VBV逐AU独立充放水不越界。
完整PSNR/SSIM、实测匹配码率效率差距与构建/产物哈希见`docs/performance.md`§14和
`docs/gates/m5cd_results.json`；历史§12的失败项保留，不冒充新实现证据。
最终生产基线常规门禁、非缓存全包race及六目标构建均通过，分别见
`docs/gates/m5c.md`、`docs/gates/m5d.md`。仓库未配置远端，远端CI未执行；
完整Profile保持Partial，当前标量实现不承诺实时，未生成工具仍明确保留边界。

| Module | 规范条款/职责 | Scope | Status | Tests | Known gaps |
|---|---|---|---|---|---|
| `motion/search` | 编码分析；MC 契约 8.4.2 | SAD、穷举、菱形、范围自适应、半/四像素细化 | Conformant | SAD 解析值、已知整数/亚像素平移、边界与局部搜索差值 | 菱形非全局最优承诺；独立子包避免 prediction→motion 的依赖环 |
| P 帧单参考闭环 | 7.3.3 / 8.4 / 8.7 | P16/16×8/8×16/8×8、MV/MVD、CAVLC/CABAC残差、P_Skip、混合PCM、共享MC/重建/滤波 | Conformant | Baseline360+600帧；Main/High CABAC288帧、权重160帧、8×8/矩阵320帧；四种分区实际选中；新长矩阵3600帧 | P宏块仅搜索最近锚点，B启用双锚点；不承诺多参考搜索或与x264相同压缩效率 |
| P 早期 Skip / 搜索策略 | 编码分析 | 规范 Skip MV 的三平面量化残差全零时提前选择；整像素分区选择后做亚像素细化 | Conformant | 全部双解码器Oracle、分区/静态Skip实际选择、搜索解析向量、完整长质量矩阵 | SAD启发式可能选择不同分区，不宣称与旧搜索逐比特相同或全局最优 |
| I/P 状态与参考更新 | 公共 API / POC type0/2 | GOPSize非IDR I刷新、独立IDRInterval、输入KeyFrame/场景变化触发IDR；无B时PTS=DTS | Conformant | I/P40帧；GOP ffprobe、Lookahead和B调度的PTS/参考/边界Oracle | PCM全IDR；启用B时参考标记滑窗保留双锚点 |
| CQP AU 资源门禁 | Annex A / 事务边界 | Auto 几何/帧率校验，实际 AU 上限检查，1080p30 不再因最坏预算在 New 拒绝 | Conformant | `LevelBudgetIsTransactional` 注入超限、已有 Oracle 回归 | 超限返回 ErrResourceLimit 且不发布输出；仍非 HRD/VBV 保证；显式 Level 与 PCM 保留保守初始化检查 |
| SATD | 编码分析 | 4×4 Hadamard、8×8/16×16 聚合、非对齐 stride、帧内候选搜索 | Conformant | 独立常量/脉冲解析解，正负极值，17 模式实际选择与整帧 Oracle | 不参与解码重建 |
| RD 代价与决策 | 编码分析 | Q8 lambda、三平面 SSE、CAVLC 位数、MV 比特搜索代价、Skip/四种 P/I4/I16/PCM；P/B保留1/3与1/6死区候选按J选优 | Conformant | QP全范围、完整未剪枝对照、B两种死区独立最低J；矩阵/B三方Oracle回归及六组固定QP质量矩阵 | CABAC仍使用CAVLC率代理，skip_run按独立宏块估计；不宣称全slice最优或全Profile认证 |
| RD 剪枝与状态回滚 | 候选事务 | 按 D>=0/语法下界提前终止；常量运动窗口支配 P 候选；恢复完整状态 | Conformant | 全部未剪枝候选对照、平坦块完整候选结构相等、插值 halo 拒绝、逐4×4 MV、参考 fuzz 和 Oracle | 非并行；未采用近似像素或降低质量换速度；等价剪枝前后基准见性能记录 |
| 活动度 QP | 编码分析 / mb_qp_delta | AQ strength 0..6、方差分档、QP 0..51 裁剪 | Conformant | 平坦/棋盘解析活动度、实际 QP=23/29、0/26/51 的 36 帧双解码器 Oracle | 默认关闭；与显式宏块 QP 或 PCM 通道冲突时拒绝 |
| Profile 编码约束 | Annex A.2.1/A.2.2 | Baseline/Main 禁止零 PCM 采样与 level_prefix>15，非法候选排除、失败不更新状态 | Conformant | 明确 PCM 零样本拒绝、限长 CAVLC 无部分写入、极低 QP 极端色度拒绝后有效输入恢复 | 极低 QP 且无合法压缩/PCM 候选时 ErrUnsupported；这是配置与内容组合边界，未篡改输入/切换 Profile |
| CABAC 编码基础 | 9.3.3 / 9.3.4 | 位精确算术、E3延迟位、bypass/terminate/PCM restart、I/P 4×4/8×8/DC/AC、B_Direct/B_Skip与零字预算 | Conformant | 规范区间码点、64状态、残差转义、bin密度/密集MPS；Main/High I/P/B和High8×8 Oracle | RD仍用CAVLC比特代理；非Direct B模式未生成 |
| CABAC I/P slice | 7.3.4 / 9.3.3 | Main/High I4/I16/PCM、P 四分区/Skip、MVD、CBP、QP、邻域 CBF、多 slice | Conformant | `tests/codec/cabac_test.go` QP 0/26/51、1/3 slice、宏块 QP 变化、帧号回绕及场景变化，三方像素一致 | 此行仅记录I/P；B与POC0见B编码行；本地门禁完成仍不表示全部Profile工具支持 |
| P 显式加权预测编码 | 7.3.3.2 / 8.4.3.2 | 固定三平面权重、整数最小二乘自动分析、PPS/slice 信令、共享 MC 后加权、P_Skip | Conformant | 权重/偏移解析拟合、插值先于裁剪、实际 Skip、失败事务；Main/High 160帧权重表与三方 Oracle | 单参考 P；搜索使用加权参考代理估计 MV，最终 RD/重建使用原参考插值后加权；B/implicit 待接入 |
| High 8×8 编码闭环 | 8.3.2 / 8.5.13 / 9.3 | 规范逆基定点投影、QP0..51、Intra8九模式与P 8×8残差RD候选、CABAC/代理CAVLC扫描、共享逆变换与滤波 | Conformant | 常量/脉冲/非DC解析基、实际I/P 8×8决策；QP0/18/26/51×1/3slice×flat/custom的320帧三方Oracle | 默认关闭，通过WithTransform8x8启用；RD仍借用CAVLC候选比特代理，不宣称CABAC最优 |
| ScalingMatrix编码 | 7.3.2.2 / 8.5.13 | 4×4六个独立列表、DC首项、8×8 intra/inter光栅矩阵配置、PPS扫描/delta、正量化与共享重建 | Conformant | PPS选择与值拷贝核对、delta回绕/零项拒绝、flat兼容、flat/custom320帧及4×4/DC+B126帧三方Oracle | 默认flat；只生成PPS级矩阵，不生成SPS级矩阵继承信令 |
| GOP / IDR 与场景变化 | 编码器策略 | GOPSize非IDR I刷新、独立IDRInterval、强制IDR、未来窗口确认、B边界调度 | Conformant | GOP ffprobe、Lookahead闪光/切换与B边界Oracle；B深度1/2/3/16实际slice/POC/PTS/DTS | B需规则PTS与恒定Duration；不生成B-pyramid |
| B编码与重排 | 7.3.3 / 8.2.4 / 8.4 | 非参考spatial B_Direct/B_Skip、双锚点零MV、4×4压缩残差、POC type0、规则PTS/DTS、有界输入/尾部排空 | Conformant | Main/High14组1120帧三方Oracle，含B2 ABR/CBR/VBV和High8×8/P权重；实际Direct/Skip、失败事务、溢出与清理测试 | B零MV不做运动搜索；temporal/B-pyramid/B显式权重未生成；B+PCM拒绝 |
| Lookahead窗口 | 输入所有权/帧类型分析 | 1..16帧未来窗口、深拷贝、闪光抑制、持续切换IDR、B边界与尾部排空 | Conformant | `Lookahead_BoundsResetAndFailedSend`、`LookaheadFlashSceneAndDrainOracle`含B0/B2 | 非并行；B组合输入上界MaxBFrames+depth+1，另有锚点与待输出B组；错误保留已接受队列 |
| ABR / CBR / VBV | 编码器码率控制 | 按I/P/B已提交QP局部夹紧、冷启动二分与不可行全QP回退；AU反馈、fullness硬边界、filler、分数帧率余数 | Conformant | 两尺寸×三模式各300帧三方Oracle；ABR±10%、CBR十窗口、VBV300步独立模型；静态分数FPS≤1%、120帧突变/闪光；失败事务/Reset与QP边界 | 验收配置和完整窗口见performance.md§14；未生成HRD信令，不作HRD时序认证或任意内容恒定画质保证 |
| CABAC 零字封装修复 | 7.4.1 / 9.3.4.6 | VCL 末尾00 00 03按已知 CABAC PPS 接受；CAVLC 仍拒绝 | Conformant | `EncapsulatedZeroWordPadding` 先失败后修复，FFmpeg 像素对照；CAVLC 终端转义拒绝 | 通用 EBSP 严格规则保持；不扩大参数集/未知 PPS 的截断容忍 |
| h264probe GOP诊断 | 7.3.3 / 参数集与图片标识 | 有界Annex-B扫描、SPS/PPS/slice头部、帧型/POC/尺寸/slice数、图片/B/IDR断言 | Conformant | B1/2/3/16、独立GOP4/IDR7、多slice、资源与失败断言；12份长码流与ffprobe/实际POC显示位置相符 | 不解码残差，不替代像素Oracle；字段、冗余和ASO图片统计明确拒绝 |

## M6：流式I/O（WIP）

分支`feat/m6-streaming`。原始I/O、NAL/AU适配器、同步有界pipeline、自动Flush、取消/背压与三个CLI已接入；
完整常规门禁、非缓存全库race、六目标构建及30秒稳定性验收通过；远端CI未执行。证据与分项提交见`docs/gates/m6.md`，API见`docs/stream.md`。

| Module | 职责 | Status | 证据 | 缺口 |
|---|---|---|---|---|
| 流接口 | FrameReader/Writer、AccessUnitReader/Writer，独立输出所有权与同步消费 | Conformant | 四接口均有具体实现和编译断言；各适配器所有权/错误契约测试 | 此状态仅接口与同步I/O，不包含pipeline、取消或背压 |
| YUV Reader | 8-bit420/422/444、奇数尺寸、紧密Y/U/V、独立帧与序号时间戳 | Conformant | 3×3已知17/21/27字节解析解、单字节分块、每处截断、EOF/所有权/错误/资源限界测试 | 仅原始I/O，非422/444编码支持；高位深待M7，不中断底层阻塞Read |
| YUV Writer | 8-bit420/422/444，忽略stride填充，最小末行缓冲，全帧验证后同步写出 | Conformant | 已知紧密字节、三格式多帧往返、晚平面错误无输出、短写/底层错误及粘性状态测试 | 原始流无尺寸/格式/时间戳头；不关闭底层Writer，不提供取消/重试传输 |
| Annex-B NAL Reader | 复用有界Scanner，3/4字节起始码、EBSP独立所有权、严格前导/尾部、错误粘性 | Conformant | 固定字节向量×1/2/3/7/4096/整文件，EOF前边界交付、EOF同次数据/包装EOF、超限/错误/所有权测试 | 仅字节封装与header，不组装AU或验证残差；默认8MiB单NAL及64KiB读缓冲，非pipeline的MaxInputBuffer |
| AVCC NAL Reader | 复用长度解析器，1..4前缀及avcC取宽度，参数集快照先于样本交付、分配前NAL限额 | Conformant | 固定前缀×分块矩阵、逐处截断、零长度/坏header、超大长度不读载荷、配置拷贝/版本/类型/限额与I/O测试 | 仅NAL封装；不读取MP4 box、不推断时间戳、不组装AU；avcC/MP4整体兼容性仍Partial |
| AU Reader | SPS/PPS按序解析、AUD/图像标识/可选first_mb规则、POC0/1/2、filler与EOS归属、独立Annex-B Packet | Conformant | 独立比特向量与字节期望、复用源缓冲、AVCC输入、错误/预算/EOF；5份登记语料×6种chunk及本轮ffprobe计数6/4/12/3/6 | 仅渐进8-bit420 I/P/B头部；字段/FMO/冗余/分区等显式Unsupported；不验证残差或恢复时间戳（PTS/DTS/Duration为0）；默认32MiB/4096 NAL，不代替pipeline预算 |
| Annex-B AU Writer | 完整输入封装验证后复用NAL Writer，规范化四字节起始码、独立单NAL/AU/数量限额 | Conformant | 固定字节解析解、重扫与High CABAC多slice语料一致；坏尾部无输出、输入/输出预算、重试校验、每个写入位置短写/I/O失败粘性及所有权 | 仅接受Annex-B Packet；调用方保证单AU分组，未验证slice/RBSP；默认8MiB单NAL/32MiB输入和输出AU/4096 NAL，不序列化时间戳，不提供传输回滚或取消 |
| AVCC AU Writer | Annex-B AU重封装为1..4字节大端长度前缀，写前共享封装验证、前缀容量限制及NAL/AU/数量预算 | Conformant | 独立固定字节解析解、1/2字节255/65535边界与多字节长度；坏尾部/后续NAL超限零写出、所有权/短写/I/O粘性；5份语料×2/3/4宽度×6种chunk逐AU一致 | 输入仅已分组Annex-B，默认4字节前缀/8MiB单NAL/32MiB输入和输出AU/4096 NAL；单NAL还受前缀容量限制，不生成avcC/MP4或时间戳，不验证slice，不提供取消 |
| EncodeStream / DecodeStream | 同步单输入、有界载荷、排空后继续生产、EOF自动Flush、内部codec清理 | Conformant | 全部26份受支持语料×Annex-B/AVCC×1/7/4096/整文件chunk核对独立FFmpeg输出哈希；PCM解析值、B/Lookahead尾组与时钟、限额/截断/错误释放测试 | MaxInputBuffer默认16MiB，仅单个输入；NAL源/codec/Writer内存各自限额，不是RSS总上限 |
| pipeline取消与背压 | Context端点优先，操作前后检查ctx，取消释放帧；同步Writer阻止下个输入 | Conformant | 阻塞输入/输出确定性取消、200次读端取消循环与goroutine计数、慢消费者停产测试 | 非可中断I/O不能被context凭空中断；需Context*接口或由所有者设置deadline/关闭，无后台代读goroutine |
| h264enc / h264dec | 参数预检、文件与二进制管道、Annex-B/AVCC、stderr进度、信号取消、文件别名保护 | Conformant | 实际子进程二进制管道与PCM像素解析值、High/B/Lookahead、码控参数、坏尾/资源/短写/取消/关闭测试；完整常规门禁通过 | 当前codec仅渐进8-bit420；CLI拥有进程标准流，库式CLI入口仍借用传入流；传输失败不回滚文件 |
| h264probe语法树 | JSONL逐NAL展示共享SPS/PPS/VUI/ScalingList/SEI/slice结构，保留默认GOP JSON | Conformant | Baseline/Main/High与ffprobe尺寸/Profile/Level/图片数对照；参数集引用、多slice、严格尾部、限额、AVCC各前缀与输出失败测试 | 不解析宏块/残差，不把头部成功当作像素正确；输出错误前的行只代表局部已解析内容 |
| 短时稳定性 | 同一对pipeline、io.Pipe、10FPS PCM持续传输30秒；RSS/GC后堆/帧积压/退出goroutine | Conformant | tools/streamcheck核对独立PCM常量解析值与预设资源阈值，退出0；取消后goroutine从3回到1；样本见gates/m6_smoke.json | 仅覆盖该输入与30秒窗口，不证明长期无泄漏，不替代M9混合输入验收 |

## M7：高级 AVC 特性（WIP）

当前特性分支`feat/m7-high422`，按实施计划从共享内核到流水线逐项验收、独立提交。
High10渐进420编解码及High422渐进8/10-bit解码已接入，限定工具与证据逐项列于下表；既有8-bit编解码回归保持通过，不据基础内核进度承诺完整Profile。

| 实施项 | 职责 | Status | 证据 | 缺口 |
|---|---|---|---|---|
| 1：共享图像布局参数化 | New/Pool.Get显式接收PixelFormat，平面几何复用公共布局，池按尺寸与格式分桶 | Conformant | 2026-11-15完整常规门禁通过：CGO=0 vet/build/非缓存全包test、depscheck34包、manifest27条、gofmt；既有布局/边缘复制/池复用回归及未支持布局拒绝测试通过 | 本项只证明8-bit行为不变；高位深证据见后续实施项 |
| 2：10-bit平面存储 | uint16采样、Sample/SetSample/Row16、采样单位stride、共享边缘复制与Clip1重建、格式隔离池 | Conformant | TestNew_High10Storage先因ErrUnsupported失败、实现后通过；奇数stride/0与1023/四角边缘/行填充/极端残差/损坏容量与溢出/8与10-bit池隔离测试通过；CGO=0完整常规门禁及8-bit FFmpeg Oracle回归通过 | 仅内部8/10-bit420存储；不表示High10解码/编码或公共Frame LE输入输出已接入 |
| 3：10-bit变换与量化 | ScaleQP/ScaleChromaQP区分语法QPY与有效QP'，4×4/8×8/亮度与420色度DC的正反量化接受QP'0..63，Hadamard临时量使用int64 | Conformant | TestScale*与TestHigh10*的规范手算QP'52..63、负QPY、色度非线性、正负1023矩阵基、正负int32极值/溢出拒绝全部通过；与第2项整合后的同一代码树已通过完整常规门禁和既有8-bit FFmpeg Oracle回归 | 仅共享内核；不代表High10流水线可用；8-bit语法QPY上限仍为51，CABAC初始化QP域不扩展，逆变换归一化仍为>>6 |
| 共享预测前置 | byte/uint16共用泛型帧内/MC/权重算术，默认DC与裁剪按位深；矩形422色度及采样格式相关MC供后续流水线接入 | Conformant | 暂存区独立快照通过CGO=0完整vet/build/非缓存全包test、depscheck34包、manifest27条及gofmt；高位深规范解析值和既有8-bit Oracle回归通过 | 新入口仅为共享核心，尚不单独声明扩展Profile或422/444流水线已支持 |
| 共享滤波前置 | byte/uint16共用弱/强亮度与色度去块，按位深缩放alpha/beta/tc0，保留规范附加量与舍入常量 | Conformant | 独立暂存区快照通过CGO=0完整常规门禁；10-bit阈值、裁剪及正负边界解析向量、旧8-bit Oracle回归通过 | 高位深流水线另行验收；QP实参仍为去掉位深偏移的语法域 |
| 共享运动搜索前置 | 8/10-bit复用整数/菱形/半与四像素搜索控制，uint16原始SAD与共享高位深MC，不降位深 | Conformant | 独立暂存区快照通过CGO=0完整常规门禁；低两位纹理平移、负MV、奇数stride、极值SAD与原8-bit Oracle回归通过 | 编码器High10接入另行验收；Scorer接收原位深SAD |
| 4：High10解码 | 渐进10-bit420 CAVLC/CABAC I/P/B、帧内4/8/16、矩阵、权重、PCM、去块、LE裁剪输出、资源预算与SPS恢复 | Conformant | 七组外部x264压缩序列与三份18帧登记语料逐采样FFmpeg一致；负QP/模64/混合PCM滤波边界/8→10→8与坏IDR恢复/裁剪解析解通过；独立暂存区完整常规门禁通过，manifest30条 | 隔行、422/444、transform bypass仍按现有组合门禁拒绝；不声明完整High10 Profile；高位深编码另验收 |
| 5：High10编码 | 原生LE输入与重建，双熵I/P/B、I4/I8/I16、矩阵/权重/AQ/前瞻/码控，语法QP-12..51、显式PCM及CLI熵选项 | Conformant | 32配置×12帧共384帧FFmpeg/自研解码/本地重建三方逐采样一致；原失败的CAVLC自定义8×8矩阵组合保留并通过（去块按整个8×8块判断非零，保留子流nC）；真实CLI输出与FFmpeg原始LE哈希同为b47b60770a076d3073f7c58298d87642136a67afaadf98ff3987bcd39a9ce439；独立提交完整常规门禁通过 | 仅渐进420；B仍采用既有非参考spatial-direct工具，隔行与422/444另项接入；QP最小值不承诺transform bypass无损 |
| 6：422图像与色度几何 | 8/10-bit422平面按水平2、垂直1采样，完整色度行、边缘扩展、格式隔离池；复用已验收8×16色度预测 | Conformant | 新增TestPicture422_GeometrySamplesAndPoolIsolation先因Unsupported失败后通过，独立48×32/24×32几何、0/255/1023边缘和池复用通过；矩形DC/Plane解析解及独立提交完整常规门禁通过 | 本项仅存储与共享预测；High422编解码分别由第7/10项验收 |
| 422 DC变换前置 | 2×4 Hadamard、八DC扫描、正反向共享内核，422 DC的QP'C+3与规范舍入，原子溢出拒绝 | Conformant | 八个规范空间基、QP分段/缩放、原地运算与正负极值解析测试通过；独立提交完整常规门禁通过 | 量化复用QuantDC并传QP'C+3；逆量化入口传未加3的QP'C；Pipeline在第7/10项接入 |
| 422 CABAC残差前置 | ChromaDC类别接受八项扫描，sig/last增量与幅值上下文按422规则选择，420路径不变 | Conformant | 独立bin/上下文轨迹、四种CBF邻域、截断与原420回归通过；独立提交完整常规门禁通过 | CBF仍为97..100；八DC不新增ctxBlockCat，后续流水线必须提供DC2x4Scan |
| 422 CAVLC残差前置 | 八系数ChromaDC422，隐含nC=-2；独立coeff_token/total_zeros规范列及共享level/run读写 | Conformant | 全部token/zeros码字、固定块比特串、截断与原420回归通过；独立提交完整常规门禁通过 | 调用者使用ChromaDC422且仅前八项，不能用四项或十六项表替代 |
| PPS缩放列表计数修复 | transform_8x8_mode_flag=0时仅六个4×4列表，开启时按色度追加2/6个8×8列表 | Conformant | 独立RBSP字节ce38402c先触发尾位错误、修复后正确解析第二色度偏移-2并精确写回；独立提交完整常规门禁通过 | FMO及其余高级语法仍按各自实施项验收；不以自往返替代规范字节向量 |
| 7：High422解码 | 渐进8/10-bit422双熵I/P/B、4×4/8×8/矩阵/权重、八DC与PCM、全高色度去块、原生LE及逐行裁剪 | Conformant | 两格式×九配置各八帧外部x264/FFmpeg逐采样一致；混合PCM/QP/去块规范Oracle、12×13裁剪解析解、420/422与8/10-bit切换/坏IDR恢复及单帧预算边界；独立提交快照完整常规门禁通过，manifest30条 | 仅渐进帧，444、隔行和transform bypass仍拒绝；422编码由第10项验收；动态生成语料尚未登记静态manifest，不替代M7最终语料门禁 |

本轮完整常规门禁通过不等于每个可选用例均执行：TestEncoder_InterLongOracle保持跳过；
CLI相对路径别名用例及无对应VUI/SEI的语料子用例亦跳过，不计作通过。未重跑race与六目标构建。

## M8–M9

对应里程碑开工时补全模块表，不提前承诺尚未实现的能力。
