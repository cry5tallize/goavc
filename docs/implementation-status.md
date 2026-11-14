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
| SPS 解析与序列化 | 7.3.2.1 | Baseline/Main/High 8-bit 4:2:0、VUI/ScalingList、裁剪尺寸 | Conformant | `sps_test.go`、`sps_write_test.go`、`corpus_test.go`、`ffprobe_oracle_test.go` | High10/422/444、隔行、qpprime 显式返回 `ErrUnsupported`（M7） |
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
证据见 `docs/gates/m5b.md`。M5 整体的 Main/High 工具、B 帧与全参数质量/性能门禁仍未完成。

M5c/d已通过Main/High I/P288帧、P权重160帧、High8×8/矩阵320帧、非参考spatial B+重排/码控1120帧三方Oracle；Lookahead/B场景边界及ABR/CBR/VBV窗口已验证。完整720p/1080p长序列、x264等QP/等码率和性能矩阵门禁仍待执行；不据短序列关闭里程碑。证据见 `docs/gates/m5cd.md`。

| Module | 规范条款/职责 | Scope | Status | Tests | Known gaps |
|---|---|---|---|---|---|
| `motion/search` | 编码分析；MC 契约 8.4.2 | SAD、穷举、菱形、范围自适应、半/四像素细化 | Conformant | SAD 解析值、已知整数/亚像素平移、边界与局部搜索差值 | 菱形非全局最优承诺；独立子包避免 prediction→motion 的依赖环 |
| P 帧单参考闭环 | 7.3.3 / 8.4 / 8.7 | P16/16×8/8×16/8×8、MV/MVD、CAVLC/CABAC残差、P_Skip、混合PCM、共享MC/重建/滤波 | Conformant | Baseline360+600帧；Main/High CABAC288帧、权重160帧、8×8/矩阵320帧；四种分区实际选中 | M5总门禁未过；B/重排未接入 |
| P 早期 Skip / 搜索策略 | 编码分析 | 规范 Skip MV 的三平面量化残差全零时提前选择；整像素分区选择后做亚像素细化 | Conformant | 全部双解码器 Oracle、分区/静态 Skip 实际选择、搜索解析向量 | SAD 启发式可能选择不同分区，不宣称与旧搜索逐比特相同；质量待 M5 整体门禁 |
| I/P 状态与参考更新 | 公共 API / POC type2 | GOPSize 周期非 IDR I 刷新、独立 IDRInterval、输入 KeyFrame/场景阈值触发 IDR、无重排 PTS=DTS | Conformant | 40 帧序列三方像素；`IndependentGOPAndSceneChangeIDR` + ffprobe I/P/key_frame 检查 | 无 B/重排；场景阈值直比参考帧；PCM 选项仍为全 IDR |
| CQP AU 资源门禁 | Annex A / 事务边界 | Auto 几何/帧率校验，实际 AU 上限检查，1080p30 不再因最坏预算在 New 拒绝 | Conformant | `LevelBudgetIsTransactional` 注入超限、已有 Oracle 回归 | 超限返回 ErrResourceLimit 且不发布输出；仍非 HRD/VBV 保证；显式 Level 与 PCM 保留保守初始化检查 |
| SATD | 编码分析 | 4×4 Hadamard、8×8/16×16 聚合、非对齐 stride、帧内候选搜索 | Conformant | 独立常量/脉冲解析解，正负极值，17 模式实际选择与整帧 Oracle | 不参与解码重建 |
| RD 代价与决策 | 编码分析 | Q8 lambda、三平面 SSE、实际 CAVLC 位数、MV 比特搜索代价、Skip/四种 P/I4/I16/PCM 全比较 | Conformant | QP 全范围单调性、位数精确、穷举候选最低 J 对照、参考只读、场景变化选择帧内块及完整 Oracle | skip_run 按独立宏块开销估计，不宣称全 slice 全局最优；x264 质量门禁待执行 |
| RD 剪枝与状态回滚 | 候选事务 | 按 D>=0/语法下界提前终止；常量运动窗口支配 P 候选；恢复完整状态 | Conformant | 全部未剪枝候选对照、平坦块完整候选结构相等、插值 halo 拒绝、逐4×4 MV、参考 fuzz 和 Oracle | 非并行；未采用近似像素或降低质量换速度；等价剪枝前后基准见性能记录 |
| 活动度 QP | 编码分析 / mb_qp_delta | AQ strength 0..6、方差分档、QP 0..51 裁剪 | Conformant | 平坦/棋盘解析活动度、实际 QP=23/29、0/26/51 的 36 帧双解码器 Oracle | 默认关闭；与显式宏块 QP 或 PCM 通道冲突时拒绝 |
| Profile 编码约束 | Annex A.2.1/A.2.2 | Baseline/Main 禁止零 PCM 采样与 level_prefix>15，非法候选排除、失败不更新状态 | Conformant | 明确 PCM 零样本拒绝、限长 CAVLC 无部分写入、极低 QP 极端色度拒绝后有效输入恢复 | 极低 QP 且无合法压缩/PCM 候选时 ErrUnsupported；这是配置与内容组合边界，未篡改输入/切换 Profile |
| CABAC 编码基础 | 9.3.3 / 9.3.4 | 位精确算术、E3延迟位、bypass/terminate/PCM restart、I/P 4×4/8×8/DC/AC与零字预算 | Partial | 规范区间码点、64状态、残差转义、bin密度与密集MPS读回；Main/High I/P及High8×8 Oracle | CABAC RD位数仍用CAVLC代理；B slice未完成 |
| CABAC I/P slice | 7.3.4 / 9.3.3 | Main/High I4/I16/PCM、P 四分区/Skip、MVD、CBP、QP、邻域 CBF、多 slice | Conformant | `tests/codec/cabac_test.go` QP 0/26/51、1/3 slice、宏块 QP 变化、帧号回绕及场景变化，三方像素一致 | 单参考、POC type2，无 B；Main/High 整体门禁尚未完成 |
| P 显式加权预测编码 | 7.3.3.2 / 8.4.3.2 | 固定三平面权重、整数最小二乘自动分析、PPS/slice 信令、共享 MC 后加权、P_Skip | Conformant | 权重/偏移解析拟合、插值先于裁剪、实际 Skip、失败事务；Main/High 160帧权重表与三方 Oracle | 单参考 P；搜索使用加权参考代理估计 MV，最终 RD/重建使用原参考插值后加权；B/implicit 待接入 |
| High 8×8 编码闭环 | 8.3.2 / 8.5.13 / 9.3 | 规范逆基定点投影、QP0..51、Intra8九模式与P 8×8残差RD候选、CABAC/代理CAVLC扫描、共享逆变换与滤波 | Conformant | 常量/脉冲/非DC解析基、实际I/P 8×8决策；QP0/18/26/51×1/3slice×flat/custom的320帧三方Oracle | 默认关闭，通过WithTransform8x8启用；RD仍借用CAVLC候选比特代理，不宣称CABAC最优 |
| ScalingMatrix编码 | 7.3.2.2 / 8.5.13 | 4×4六个独立列表、DC首项、8×8 intra/inter光栅矩阵配置、PPS扫描/delta、正量化与共享重建 | Conformant | PPS选择与值拷贝核对、delta回绕/零项拒绝、flat兼容、flat/custom320帧及4×4/DC+B126帧三方Oracle | 默认flat；只生成PPS级矩阵，不生成SPS级矩阵继承信令 |
| GOP / IDR 与场景变化 | 编码器策略 | GOPSize非IDR I刷新、独立IDRInterval、强制IDR、未来窗口确认、B边界调度 | Conformant | GOP ffprobe、Lookahead闪光/切换与B边界Oracle；B深度1/2/3/16实际slice/POC/PTS/DTS | B需规则PTS与恒定Duration；不生成B-pyramid |
| B编码与重排 | 7.3.3 / 8.2.4 / 8.4 | 非参考spatial B_Direct/B_Skip、双锚点零MV、4×4压缩残差、POC type0、规则PTS/DTS、有界输入/尾部排空 | Conformant | Main/High14组1120帧三方Oracle，含B2 ABR/CBR/VBV和High8×8/P权重；实际Direct/Skip、失败事务、溢出与清理测试 | B零MV不做运动搜索；temporal/B-pyramid/B显式权重未生成；B+PCM拒绝 |
| Lookahead窗口 | 输入所有权/帧类型分析 | 1..16帧未来窗口、深拷贝、闪光抑制、持续切换IDR、B边界与尾部排空 | Conformant | `Lookahead_BoundsResetAndFailedSend`、`LookaheadFlashSceneAndDrainOracle`含B0/B2 | 非并行；B组合输入上界MaxBFrames+depth+1，另有锚点与待输出B组；错误保留已接受队列 |
| ABR / CBR / VBV | 编码器码率控制 | 帧QP候选、AU反馈、CBR fullness、VBV硬边界、filler NAL、分数帧率预算余数 | Partial | ABR120/CBR2×30/VBV60帧；静态CBR/VBV各60帧分数FPS码率≤1%且Oracle；填充字节/资源门禁解析解 | HRD参数信令未生成；大尺寸长序列与x264质量门禁仍待执行 |
| CABAC 零字封装修复 | 7.4.1 / 9.3.4.6 | VCL 末尾00 00 03按已知 CABAC PPS 接受；CAVLC 仍拒绝 | Conformant | `EncapsulatedZeroWordPadding` 先失败后修复，FFmpeg 像素对照；CAVLC 终端转义拒绝 | 通用 EBSP 严格规则保持；不扩大参数集/未知 PPS 的截断容忍 |

## M6–M9

后续里程碑的模块表在对应里程碑开工时补全；开工前这里只保留占位，
避免出现"提前承诺、实际未实现"的记录失真。

<!--
模板：

| Module | 规范条款 | Scope | Status | Tests | Known gaps |
|---|---|---|---|---|---|
| `cavlc.DecodeResidualBlock` | 9.2 | 4×4 亮度 AC/DC、色度 DC | Partial | cavlc/residual_test.go | 未覆盖 nC=-1 的色度 DC（见 #123） |
-->
