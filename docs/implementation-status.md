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
| `Frame` 类型与几何 | — | 6种8/10-bit格式、平面几何、深拷贝、释放与采样范围 | WIP | `frame_test.go`及M7格式/低位Oracle | M0 CI状态保留；高位深能力已由M7验收 |
| `Frame.Validate` | — | 结构自洽 + 溢出检测 | WIP | `frame_validate_test.go` | 不施加分辨率上限（属策略层） |
| `Packet` | — | AU 载荷、时间戳、深拷贝 | WIP | `packet_test.go` | Side Data 待 M6 |
| `EncoderConfig` / `DecoderConfig` | — | 默认值、资源预算及格式/QPY校验 | WIP | 原配置测试及M7格式/负QPY组合回归 | 公共QPY与有效QP'分域见M7；M0 CI状态保留 |
| 依赖规则检查 | 架构 R1/R2/R3/R5 | AST 级 import 图 | WIP | `importgraph/graph_test.go` | R4/R6 靠 review |
| 语料清单校验 | — | 结构 + 枚举 + 哈希 | WIP | `manifest/manifest_test.go` | — |
| YUV 差分 | — | 逐平面逐采样精确比较 | WIP | `yuvdiff/yuvdiff_test.go` | 仅支持紧密布局（约定） |
| Oracle 工具 | — | FFmpeg开发期解码与原生YUV差分 | WIP | 当前55条登记语料及M7外部/解析解来源 | M0 CI状态保留；实际证据见M7，不将解析解冒充FFmpeg互操作 |

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
| SPS 解析与序列化 | 7.3.2.1 | Baseline/Main/High、High10/422/444的8/10-bit、独立色平面、帧/场与MBAFF语法、VUI/ScalingList与裁剪 | Conformant | 原语法/ffprobe及扩展Profile像素Oracle、场/MBAFF固定字节与真实隔行语法探测通过 | qpprime仍拒绝；MBAFF像素重建见第13项，PAFF场图像及单色解码仍拒绝 |
| PPS 解析与序列化 | 7.3.2.2 | 核心字段、4/8×8缩放扩展、FMO 0–6、引用与Profile组合校验 | Conformant | 固定规范字节、完整矩阵/七类映射、x264字节回写与真实像素Oracle；M7独立完整门禁 | 语法API不代表编码器生成全部工具；FMO像素见第16项 |
| VUI / HRD | E.1/E.2、Annex A | 完整条件语法、NAL/VCL CPB替代调度、profile/level声明与显式资源预算 | Conformant | M7第21项固定非字节对齐VUI/HRD字节、尺度/计数/32项/顺序/截断、level/profile倍率与DPB边界；完整独立门禁通过 | 参数集验证不代替实际AU码率、SEI时序与MV范围的运行时证明 |
| ScalingList / ScalingMatrix | 7.3.2.1.1.1、7.4.2.1/2 | SPS/PPS 4×4/8×8、444十二列表、fallback A/B与写前原子校验 | Conformant | 原4096 presence组合×SPS/PPS来源、444/独立平面Oracle；尾部delta/缓存分裂/最终列表失败无部分输出、真实反量化固定数值；完整独立门禁通过 | 语法支持不放开Profile禁止的PPS扩展，不表示transform bypass或单色流水线已接入 |
| SEI 基础 | 7.3.2.3 | payloadType/Size 扩展、user_data_unregistered | Partial | `sei_test.go`、`corpus_test.go` | 仅框架与 user_data；recovery point 等语义消息在 M2/M3 |
| Slice Header | 7.3.3 | 完整I/P/B/SP/SI、POC0/1/2、帧场、权重/参考标记、FMO cycle与冗余计数 | Conformant | 固定规范bit串/边界、真实Oracle语料、27份High10保工具投影及完整门禁 | PAFF仅语法可解析，像素拒绝；头部成功不证明残差完整 |

## M2：Decoder Baseline

验证环境：Go 1.27.4 windows/amd64、`CGO_ENABLED=0`、FFmpeg/ffprobe 8.0。
五份真实 Baseline 语料合计 21 帧，64×48、176×144、320×240、1920×1080，
含双 slice、重复 IDR、P_Skip 和 1080p crop，Y/U/V 与 FFmpeg 逐像素相等。
独立 Oracle 原始 YUV 哈希保存在 manifest，未安装 FFmpeg 的 CI 仍执行哈希回归。
本地验证不等同于远端 CI 已执行；最终门禁证据见 `docs/gates/m2.md`。

| Module | 规范条款 | Scope | Status | Tests | Known gaps |
|---|---|---|---|---|---|
| `decoder` 状态机 | 公共 API 契约 | Send/Receive、Flush、Reset、Close、参数集按序生效、错误后等待 IDR | Conformant | `decoder_test.go` | 单实例串行；输入须为完整 Annex-B AU，M6 接入 AVCC/增量输入 |
| Macroblock / I_PCM | 7.3.4 / 7.3.5 | I4/I8/I16/PCM、P/B分区与Skip、FMO遍历及分类残差 | Conformant | 规范解析像素、真实外部语料、M7六格式及跨slice隔离 | 高级组合和明确拒绝边界见M7第13–20项 |
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
| h264probe GOP诊断 | 7.3.3 / 图片标识 | 参数集、POC、slice计数与图片/B/IDR断言；ASO/FMO/冗余/分区统计 | Conformant | 原长码流/ffprobe回归与M7规范头测试、实际分区CLI通过 | 不解码残差；非逐行图片的GOP统计仍Unsupported，-syntax可展示MBAFF头 |

## M6：流式I/O（WIP）

分支`feat/m6-streaming`。原始I/O、NAL/AU适配器、同步有界pipeline、自动Flush、取消/背压与三个CLI已接入；
完整常规门禁、非缓存全库race、六目标构建及30秒稳定性验收通过；远端CI未执行。证据与分项提交见`docs/gates/m6.md`，API见`docs/stream.md`。

| Module | 职责 | Status | 证据 | 缺口 |
|---|---|---|---|---|
| 流接口 | FrameReader/Writer、AccessUnitReader/Writer，独立输出所有权与同步消费 | Conformant | 四接口均有具体实现和编译断言；各适配器所有权/错误契约测试 | 此状态仅接口与同步I/O，不包含pipeline、取消或背压 |
| YUV Reader | 8/10-bit420/422/444、LE与字节stride、奇数尺寸、独立帧与序号时间戳 | Conformant | 原字节/错误/资源边界及M7六格式原生LE实际CLI像素通过 | 不中断底层阻塞Read，不推断编码器可表达的裁剪几何 |
| YUV Writer | 8-bit420/422/444，忽略stride填充，最小末行缓冲，全帧验证后同步写出 | Conformant | 已知紧密字节、三格式多帧往返、晚平面错误无输出、短写/底层错误及粘性状态测试 | 原始流无尺寸/格式/时间戳头；不关闭底层Writer，不提供取消/重试传输 |
| Annex-B NAL Reader | 复用有界Scanner，3/4字节起始码、EBSP独立所有权、严格前导/尾部、错误粘性 | Conformant | 固定字节向量×1/2/3/7/4096/整文件，EOF前边界交付、EOF同次数据/包装EOF、超限/错误/所有权测试 | 仅字节封装与header，不组装AU或验证残差；默认8MiB单NAL及64KiB读缓冲，非pipeline的MaxInputBuffer |
| AVCC NAL Reader | 复用长度解析器，1..4前缀及avcC取宽度，参数集快照先于样本交付、分配前NAL限额 | Conformant | 固定前缀×分块矩阵、逐处截断、零长度/坏header、超大长度不读载荷、配置拷贝/版本/类型/限额与I/O测试 | 仅NAL封装；不读取MP4 box、不推断时间戳、不组装AU；avcC/MP4整体兼容性仍Partial |
| AU Reader | 参数集按序、图片标识与可选first_mb边界、MBAFF/FMO/ASO/冗余/分区头关联 | Conformant | 原分块/预算/所有权回归、M7六格式/场/规范高级工具实际CLI | 不验证残差或恢复时间戳；PAFF仍Unsupported；默认32MiB/4096 NAL，不替代pipeline预算 |
| Annex-B AU Writer | 完整输入封装验证后复用NAL Writer，规范化四字节起始码、独立单NAL/AU/数量限额 | Conformant | 固定字节解析解、重扫与High CABAC多slice语料一致；坏尾部无输出、输入/输出预算、重试校验、每个写入位置短写/I/O失败粘性及所有权 | 仅接受Annex-B Packet；调用方保证单AU分组，未验证slice/RBSP；默认8MiB单NAL/32MiB输入和输出AU/4096 NAL，不序列化时间戳，不提供传输回滚或取消 |
| AVCC AU Writer | Annex-B AU重封装为1..4字节大端长度前缀，写前共享封装验证、前缀容量限制及NAL/AU/数量预算 | Conformant | 独立固定字节解析解、1/2字节255/65535边界与多字节长度；坏尾部/后续NAL超限零写出、所有权/短写/I/O粘性；5份语料×2/3/4宽度×6种chunk逐AU一致 | 输入仅已分组Annex-B，默认4字节前缀/8MiB单NAL/32MiB输入和输出AU/4096 NAL；单NAL还受前缀容量限制，不生成avcC/MP4或时间戳，不验证slice，不提供取消 |
| EncodeStream / DecodeStream | 同步单输入、有界载荷、排空后继续生产、EOF自动Flush、内部codec清理 | Conformant | 全部26份受支持语料×Annex-B/AVCC×1/7/4096/整文件chunk核对独立FFmpeg输出哈希；PCM解析值、B/Lookahead尾组与时钟、限额/截断/错误释放测试 | MaxInputBuffer默认16MiB，仅单个输入；NAL源/codec/Writer内存各自限额，不是RSS总上限 |
| pipeline取消与背压 | Context端点优先，操作前后检查ctx，取消释放帧；同步Writer阻止下个输入 | Conformant | 阻塞输入/输出确定性取消、200次读端取消循环与goroutine计数、慢消费者停产测试 | 非可中断I/O不能被context凭空中断；需Context*接口或由所有者设置deadline/关闭，无后台代读goroutine |
| h264enc / h264dec | 参数预检、文件/二进制管道、Annex-B/AVCC、stderr进度、信号取消、文件别名保护及-field-order | Conformant | 原M6测试；M7原生8/10-bit六格式与真实MBAFF CLI通过，TFF/BFF各六帧的FFmpeg像素和ffprobe逐帧场序一致 | codec范围见M7各项；PAFF仍拒绝；CLI拥有进程标准流，库式入口借用传入流；传输失败不回滚文件 |
| h264probe语法树 | JSONL逐NAL展示共享SPS/PPS/VUI/ScalingList/SEI/slice结构，保留默认GOP JSON | Conformant | Baseline/Main/High与ffprobe尺寸/Profile/Level/图片数对照；参数集引用、多slice、严格尾部、限额、AVCC各前缀与输出失败测试 | 不解析宏块/残差，不把头部成功当作像素正确；输出错误前的行只代表局部已解析内容 |
| 短时稳定性 | 同一对pipeline、io.Pipe、10FPS PCM持续传输30秒；RSS/GC后堆/帧积压/退出goroutine | Conformant | tools/streamcheck核对独立PCM常量解析值与预设资源阈值，退出0；取消后goroutine从3回到1；样本见gates/m6_smoke.json | 仅覆盖该输入与30秒窗口，不证明长期无泄漏，不替代M9混合输入验收 |

## M7：高级 AVC 特性（本地验收通过，v0.9.0仅本地标签）

当前特性分支`feat/m7-high422`，按实施计划从共享内核到流水线逐项验收、独立提交。
六格式8/10-bit编解码、MBAFF/TFF-BFF及限定FMO/ASO/分区/SP/SI/冗余已接入，VUI/HRD与完整矩阵已验收。表内前置条目描述其独立提交时的证明范围；当前有效组合以支持矩阵为准，不据此宣称完整Profile认证。

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
| 8：444图像与色度共享内核 | 8/10-bit444三平面16×16存储、边缘复制、裁剪重建与格式隔离池；444 CABAC十四类/1024模型及十二列表fallback | Conformant | 全平面几何/行共享/极值与六格式池隔离；ITU Amd.2表9-24d/e固定模型分段值、CBF/bin轨迹与范围；全部4096种列表presence组合×三种SPS/PPS来源；独立快照完整常规门禁通过 | 本项仅共享内核，不提前放开High444流水线；独立平面按colour_plane_id选择矩阵，QP仍用Y过程 |
| 9：High444解码 | 共同444三平面全尺寸残差、十四类CABAC、逐分量矩阵/QP、亮度插值与共享bS；独立平面覆盖/运动/权重头隔离 | Conformant | 两格式×九配置各八帧x264/FFmpeg逐采样一致；独立平面PCM的13×11裁剪/LE固定采样、乱序平面/缺失/重复/恢复与预算边界；权重固定字节521e；独立提交完整CGO=0门禁（35包依赖、30条语料）及真实h264dec双帧10-bit像素比较通过 | 独立平面原始封装被FFmpeg8明确拒绝，使用规范固定采样与保持slice_data/对齐的单色投影Oracle，不宣称FFmpeg支持原始封装；隔行/transform bypass及编码另项验收，静态高级语料门禁仍待完成 |
| 10：High422/High444编码 | 8/10-bit422/444双熵压缩与PCM、I/P/既有spatial-direct B、逐分量矩阵/QP、权重/AQ/前瞻/码控及独立平面 | Conformant | 六格式双熵QP极值、4/8×8矩阵、实际权重表、宏块QP、B与四种码控、PCM及三平面独立集合的三方像素Oracle；独立平面按保持slice_data对齐的单色投影逐平面FFmpeg比较；AU预算失败/重试及亮度bS/CBF规范回归；独立完整CGO=0门禁通过，四种422/444格式真实CLI各两帧与FFmpeg一致 | 仅渐进帧，B限既有非参考spatial-direct；不生成隔行与transform bypass；FFmpeg8不支持独立平面原始封装，投影不冒充原始封装互操作性；静态高级语料/race/六目标最终门禁另行完成 |
| 11：场图像与MBAFF语法 | SPS的frame_mbs_only/MBAFF/direct组合校验，map unit与完整帧高度/裁剪；slice场奇偶及POC条件字段 | Conformant | 固定SPS字节4d001ef668ea90/4d001ef669ea90、顶/底场与帧slice规范字节/精确payload边界；非法Baseline/flag组合写前拒绝；真实Main64×64 MBAFF语料h264probe -syntax及独立完整门禁通过 | 语法证据不单独代表像素支持；MBAFF像素接入见第13项，PAFF仍拒绝 |
| 12：场运动矢量预测 | 显式帧/顶场/底场单位，帧场MV/ref_idx归一化，MBAFF场列表奇偶拆分，分区定向MVP与P_Skip | Conformant | 负奇数/参考边界/场奇偶/可用与帧内区别/C→D/定向/Skip规范向量及独立完整门禁；第13项接入后由真实帧场混合Oracle覆盖 | PAFF场列表不能用MBAFF拆分代替，PAFF流水线仍拒绝 |
| 13：MBAFF宏块对解码 | 双熵宏块对遍历/Skip场标志推导，帧场混合物理邻域、原生场视图/扫描/MV与参考，B Direct、逐对去块及420对侧场色度相位 | Conformant | 六格式×双熵×TFF/BFF×四类I/P/B/8×8矩阵×全场/真实混合共192配置、1152帧外部x264/FFmpeg逐采样一致；混合PCM与对侧场规范码流；原Main隔行四帧固定YUV哈希及manifest状态迁移；独立完整CGO=0门禁（35包依赖/30条语料）与真实六帧CLI二进制比较通过 | 只接受MBAFF帧图片，PAFF及非MBAFF隔行序列仍ErrUnsupported；FMO/ASO/分区/SP/SI/冗余及transform bypass另项验收；未据此关闭M7总门禁 |
| 14：顶/底场优先隔行编码 | WithInterlaced与CLI field-order，真实全field-coded MBAFF宏块对、原生场预测/扫描/重建/滤波及逐帧pic_timing | Conformant | 六格式/双熵/TFF-BFF/1与3slice、I/P/B/8×8矩阵/权重、负QP与显式PCM三方像素通过，独立平面按单色投影比较；跨pair-row无同slice邻pair的双Skip推导回归；完整CGO=0独立门禁及TFF/BFF实际CLI各六帧像素/逐帧场序通过 | 固定选择真实场宏块，不做自适应帧/场RDO，不生成PAFF；高度须能由2*SubHeightC裁剪单位表达，Baseline拒绝；B仍限既有非参考spatial-direct工具 |
| 15：FMO 0–6语法与映射 | PPS参数读写、动态slice_group_change_cycle位宽、map-unit/帧场宏块映射及NextMBAddress | Conformant | 固定规范PPS字节、七类型手算映射、双方向/矩形覆盖/退化尺寸/动态cycle与payload边界、坏表及分配前资源限制；独立暂存区完整CGO=0门禁通过（35包依赖/30条语料） | 本项仅语法和地址映射；FMO/ASO像素解码仍由第16项接入，不宣称完整Baseline/Extended支持 |
| 16：FMO/ASO宏块顺序解码 | 按组首址排序、NextMbAddress遍历、逐MB覆盖所有权、CAVLC上下文/MVP隔离与统一去块 | Conformant | 七种映射的非均匀PCM逐采样解析解、I4跨slice邻居不可用、普通逆序ASO、P非零MVD及跨组skip_run、cycle不一致/重叠/缺失/越界拒绝及IDR恢复；独立完整CGO=0门禁及真实h264dec 64×64三平面6144字节比较通过 | FMO限Baseline/Extended CAVLC，非法Main/High或CABAC组合返回ErrInvalidData；不生成FMO/ASO编码；PAFF仍拒绝，非PCM高级外部静态语料另验收 |
| 17：数据分区NAL语法 | 类型2的完整slice_header与slice_id、类型3/4的条件前缀、关联上下文快照及精确数据位偏移 | Conformant | 手写规范bit串、每bit截断、NAL参考属性、colour_plane/redundant上下文、CABAC非法组合和尺寸溢出分类；SP/SI条件头一并覆盖；独立完整CGO=0门禁通过（35包依赖/30条语料） | 仅前缀语法，不验证残差或重组；像素与AU分组由第18项接入，SP/SI像素见第19项 |
| 18：A/B/C分区重组解码 | 按slice_id关联ASO的A/B/C、独立PCM/帧内与帧间残差读端、各分区尾位与实际category校验；AU/探测CLI接入 | Conformant | PCM、I16非零DC、P非零MVD/DC、混合I/P及仅A的Skip规范像素；缺失/重复/孤立/错ID/RefIDC/截断/空category和恢复；独立完整CGO=0门禁及实际h264dec两帧12288字节独立采样一致 | 分区仅Extended/CAVLC，B在对应C前且A必须先到；不提供丢失残差的错误隐藏；h264probe只验证前缀，不能替代残差/像素验证 |
| 19：SP/SI切片解码 | 规范8.6 primary/secondary/SI系数域合成，QP/QS及色度域分离，SP Skip切换量化，SI特殊与普通I宏块分派 | Conformant | 正负DC/AC空间基、QS≠QP、色度DC Hadamard域、分量偏移、SI逐块重建反馈、SP Skip与真实Annex-B整帧解析解；修正测试CBP码字31→32后完整独立CGO=0门禁通过 | 仅Extended逐行8-bit420/CAVLC/flat4×4，合法但未接入的位深/采样/隔行/矩阵组合明确拒绝，不生成SP/SI编码；规范像素证据不冒充外部播放器互操作 |
| 20：冗余slice解码 | 独立redundant_pic_cnt表示重建，优先primary/最小完整备用；失败归还图像，成功后仅一次DPB提交与输出 | Conformant | primary/backup重叠、备用独立ASO、低计数不完整回退、不同计数不拼图、非零MVD压缩备用、错误/下降cnt/覆盖恢复及多图片身份规范像素；独立完整CGO=0门禁通过 | 不输出多份冗余图；较低表示完整后丢弃其余残差，仅保留已解析头部的语法/资源检查；不执行丢失宏块的像素错误隐藏 |
| 21：VUI/HRD参数解析生成及校验 | 安全派生bitrate/CPB、替代调度、延迟位宽、完整SPS写前/解析后Annex A/E校验和资源错误分类 | Conformant | 独立固定字节、双HRD/多CPB/非零尺度、全部已支持profile/level边界与原子拒绝；隔行Auto排除非法level；旧weighted-b VUI声明16→15由生成器重建，FFmpeg输出哈希仍41e461eb…f864236；独立完整CGO=0门禁通过 | 不把VUI/HRD声明等同实际码流HRD时序合规；编码流水线仍未自动生成buffering_period/完整HRD调度，API可读写并严格校验参数 |
| ScalingMatrix与组合级约束 | 列表全范围预检、解析缓存与真实矩阵一致；已知Profile的SPS/PPS/slice/分区组合限制 | Conformant | 十二列表/既有4096 fallback组合、解析矩阵到反量化固定数值、写前无污染、Profile工具矩阵与错误分类；旧Baseline B/场/CABAC及High冗余等非法夹具迁为合法上下文，完整独立CGO=0门禁通过 | 规范禁止组合返回ErrInvalidData；合法但未实现的9/12/14-bit、单色、PAFF或transform bypass仍ErrUnsupported，不以拒绝路径宣称完整Profile支持 |
| 22：高级Profile Oracle语料套件 | 25份新增高级码流/76帧、原生位深/显示顺序哈希、Oracle来源与帧数清单、跨里程碑High10投影 | Conformant | 55条清单；八份422/444外部x264与一份真实HRD、十六份规范工具向量全部实际CLI匹配；11份新增可用FFmpeg像素证据，14份明确解析解；原M2/M3全部27份保留预测/残差/参考工具投影至High10（PCM扩至10位）与严格FFmpeg逐采样一致；完整独立CGO=0门禁通过，详见gates/m7_corpus.json | FFmpeg对FMO/ASO/分区/secondary SP/SI存在拒绝或差异；不篡改期望，不计作外部Oracle通过；独立平面互操作仍按单色投影与规范证据限定 |
| 23：逐项支持矩阵与门禁文档 | 公开Profile/工具/组合限制、Oracle来源、原生I/O与发布边界 | Conformant | supported-features、README、stream与gates/m7.md同步；相对文档链接核对，功能证据指向独立测试/固定哈希，不把历史前置快照当当前拒绝范围 | 本地证据不冒充远端CI或完整Profile认证；最终发布检查见第24项 |
| 24：本地退出标准与版本标签 | Roadmap五项证据、本地常规/race/六目标门禁和注释标签v0.9.0 | Conformant（本地范围） | CGO=0完整门禁、55条清单、27份High10投影、全库race24包成功/11包无测试及六目标构建通过；记录见gates/m7.md | 远端CI与对外发布未执行；Profile认证、M9长稳和未支持工具不计作通过 |

本轮完整常规门禁通过不等于每个可选用例均执行：TestEncoder_InterLongOracle保持跳过；
CLI相对路径别名及无对应VUI/SEI字段的子用例不计作通过；analytic不计作外部Oracle通过。最终全库race与六目标构建均已通过，完整发布边界见gates/m7.md。

## M8：SIMD 与并行优化

基线与接口冻结见 `m8-development/12_execution_log.md`；本地逐任务推进，未通过完整M8门禁，远端CI未执行。

A07已完成原300帧八格与统一v1.0.2基线的五对测量，High Auto单/四slice为31.17/31.93FPS；
报告、实际消费链和原始数据见[gates/a07_decoder_block_results.md](gates/a07_decoder_block_results.md)。
A01局部数组包装分配已由[C01](gates/c01_luma_edge_dispatch.md)关闭；encoder亮度/均值/权重整块入口尚未迁移，完整门禁不因端点性能而升级。

| Module | Scope | Status | Tests | Known gaps |
|---|---|---|---|---|
| CABAC MPS热分支评估（C06） | 省略已归一化MPS调用的测试候选；生产Bin不变 | WIP | 全区间/状态/截断尾部和生命周期差分、IDR/High10及留出五对，见gates/c06_cabac_mps.md | High10补测+6.3%未通过门禁，不采纳；可信上下文/残差循环专化尚未完整实施 |
| 参考身份令牌实验（C05） | 每图字典及uint32身份索引的隔离模型；生产仍指针布局 | WIP | 身份对照、合成留存及真实 DPB 驻留探针，见gates/c05_dpb_residency.md | 固定 High/High10 实测驻留下理想进程活跃堆降幅低于 5%，本轮不迁移；未证明自然GC收益 |
| 高位深分数MC SSE2（B05b） | 10-bit亮度全qpel及uint16色度双线性，4/8/12/16宽完整块，编解码共享 | Partial | 全相位/宽域/halo保护页/真实帧场块/解析向量/0分配/FFmpeg；本轮purego/386/race/六目标通过，见gates/b05_high_depth_mc.md | 其他形状/ISA仍Generic，无10-bit Put/Avg；用户确认B05按本机范围完成，NEON未原生验证，不改变完整M8或Profile状态 |
| 16宽块加权AVX2（B05a） | 8/10-bit单/双向，16宽且至少两行；其他形状保留SSE2/Generic | Partial | 独立int64解析式、完整uint16/原地/非对齐/保护页/局部0分配及Oracle，见gates/b05_avx2_weight.md | 不是所有加权形状均AVX2；单行收益未确认不启用；NEON未扩展 |
| CABAC缓存/固定扫描（B04） | ReadBit复用已有reservoir、立即提交逻辑位数；固定4/15/16项扫描一次验证 | Partial | 旧引擎逐状态trace、混合读位、十四类帧场/截断、FFmpeg与编码三方Oracle，见gates/b04_cabac_cache.md | 8/64项及未知scan保持通用入口；短端点收益未确认，不扩大Profile或并行支持 |
| CPU检测（K01） | amd64安全CPUID/XGETBV、arm64基线NEON能力、purego/其他架构回退 | Partial | `simd/cpu_test.go` 的1024组合与 `cpu_purego_test.go` | CPU检测不等于任何向量kernel已实现；arm64仅交叉构建时不记原生执行 |
| 内核表（K02） | SAD/SATD、4/8×8正逆变换与DC、精确乘积、去块候选和残差的类型化分发及逐槽诊断 | Partial | 显式Generic/SSE2/AVX2真实consumer；MC用静态noescape平台包装保留scratch零分配 | amd64按实测默认选择SSE2 Forward4/SATD及Generic部分向量化槽；MC仍只加速整数宽16复制，不冒充插值SIMD |
| parity框架（K03） | 固定种子矩形/stride/offset/最小len、输入不变、强制后端与生产包装0分配 | Partial | x86原生、purego及386全库测试；K14保护页扩展至全部现有内核类别 | NEON仅三系统交叉构建/测试编译，未原生验证；Unix保护页辅助已交付但本机未执行 |
| SSE2 SAD（K04） | byte/原生10-bit矩形，16/8/4窄加载与标量尾部，64位累加 | Partial | 直接后端parity、独立最大值、强制搜索双位深、M7固定指纹及K14保护页 | NEON与Unix动态执行证据不能由本机x86替代 |
| AVX2 SAD（K05） | width16的8/10-bit真实AVX2，其他宽度复用SSE2 | Partial | AVX2直接parity、强制搜索、M7指纹及K14保护页 | 逐槽AVX2诊断表示此快路覆盖域，非16宽实际为SSE2 |
| NEON SAD（K06） | width16的8/10-bit基线NEON，其他矩形复用Generic | WIP | 原生parity/bench测试入口已交付；三种arm64交叉vet/测试构建 | 用户指定只做编译验收；未原生执行，不提供虚构NEON性能或正确性通过声明 |
| MC块化（K09a） | 唯一共享标量、interior/边界patch、相位一次分解、j中间行复用 | Partial | 旧MC指纹、原六格式/场/权重解析值、M7码流指纹、MC0分配与stride/phase/bit-depth回归 | SSE2/AVX2仅新增整数宽16复制快路；SIMD插值仍待K09b/c/d，不把复制快路标为完整AVX2/NEON MC |
| 双向MC Put/Avg（B01） | decoder普通8-bit420逐行双参考均值融合；共享相位组合MCBlend8 SSE2 | Partial | 两预测块对照、全相位/负MV/边界/未写区、局部0分配及新核保护页、FFmpeg像素通过，见gates/b01_put_average.md | 显式/非均值权重及其他几何/位深仍原路；六抽头和双线性本阶段仍Go，端点收益未确认 |
| MC整数复制快路（K09b前置） | SSE2/AVX2宽16、fx=fy=0、逐行16字节无对齐要求，静态noescape包装，其他尺寸/相位唯一Generic回退 | Partial | 全相位边界保护页、预测解析值、purego/386全库测试及prediction/search/encoder现有0分配保护网 | 仍缺SSE2/AVX2/NEON插值与uint16快路，不因K14完成而扩大K09能力 |
| x86 SATD（K07） | SSE2/AVX2的4×4 Hadamard，真实帧内候选接入，内部值参数 | Partial | 8/10-bit常量/冲激解析值、5000组parity、实际候选、局部残差0分配、M7指纹 | 默认实测优选SSE2；arm64尚未原生验证；矩形仍按原4×4 tiled |
| NEON SATD（K08） | 基线ASIMD 4×4 Hadamard、同值参数与测试入口 | WIP | arm64交叉vet/构建，原生测试入口已交付 | 用户指定编译验收；未原生运行，不把Generic或跨编译当NEON通过 |
| 汇编边界矩阵（K14） | 逐行保护页/子进程、最小len、奇数尺寸/stride、只读源与未写区，覆盖SAD/SATD/变换/量化/去块/残差及现有MC | Partial | Windows原生Generic/Auto/SSE2/AVX2、purego及386保护页执行；六目标汇编vet/build/测试编译 | Unix mmap辅助已交付但本机未运行；arm64遵循用户编译验收范围，无NEON动态越界/性能通过声明；值ABI内部栈访问另有静态范围证明 |
| x86正逆变换（K10） | 4/8×8及亮度/420/422 DC共享语义，SSE2/AVX2真实部分向量化；保留int64中间域、原地及IDCT8首错写入序 | Partial | 显式后端解析基底/极值/原地/错误与局部0分配；独立420混合符号回归；编码旧指纹及默认路径三方Oracle | 8×8投影乘积及逆/DC宽加法为部分SIMD，其余算术仍Go；实测小批调用较慢，amd64默认仅Forward4选SSE2，其余变换槽选Generic |
| NEON正逆变换（K10） | 基线ASIMD Forward4、8×8精确投影乘积、逆变换及DC宽加法接入同一共享入口 | WIP | windows/linux/darwin arm64全库build、汇编vet及带测试标签的simd/transform测试编译 | 用户指定编译验收；原生parity、保护页、分配、Oracle和性能未执行，不声明NEON原生正确性通过 |
| 量化/反量化（K11） | 4/8×8与DC按块准备参数，精确32×32→64乘积，原整数除法/死区/符号与晚期失败前缀；指针块接口避免数组复制 | Partial | 本机Generic/Auto/SSE2/AVX2全QP'0..63、64位乘积、宽域回退、局部0分配、原地保护页及首错前缀/未写后缀回归通过 | SIMD仅负责准确乘积，除法/舍入/错误仍共享Go；amd64默认按实测选择直接Generic，显式后端仍真实SIMD；NEON原生未验 |
| 去块（K12） | 唯一共享亮度/色度算法，强弱候选部分向量化，保持逐线顺序和条件邻域访问 | Partial | 本机Generic/Auto/SSE2/AVX2强弱、方向/Span1/2/4与最小邻域保护页通过；领域解析测试保留 | 不是整条边全SIMD，不改变bS/阈值/slice权限；amd64默认候选加法保留Generic，实测SIMD较慢；NEON原生未验 |
| 去块逐边参数复用 | decoder普通边四段bS跨亮色度复用，QP/阈值按分量逐边准备；旧单段入口保留 | Partial | 六格式原调度像素回归、B参考交换、准备边负QP/偏移/stride/零分配；1080p High IDR及32帧I/P/B直接AU逐像素FFmpeg一致；五样本见performance §17 | 混合MBAFF几何保留原路径；A06进一步bS批量候选因重复端点退化未采纳，见gates/a06_deblocking_batch.md；并行/Profile状态不变，短端点不替代300帧主矩阵 |
| 去块Generic直接算术 | 默认/纯Go去块直接计算强弱候选，显式SIMD继续使用原候选后端；阈值/裁剪/邻域流程共享 | Partial | 直接标量与原值ABI候选双位深/方向/Span等价；最终High IDR和32帧I/P/B直接AU逐像素FFmpeg一致；三阶段五样本见performance §17.2 | 未改CABAC、IDCT、重建或并发模型；NEON原生性能未测 |
| 8-bit亮度类整边（A01/C01） | decoder完整16线亮度类边使用SSE2，含满足几何的444分量及MBAFF纯帧MB | Partial | 差分、保护页、Oracle通过；C01闭包局部数组两方向及短邻域回退0alloc，见gates/c01_luma_edge_dispatch.md | encoder亮度仍分段；A01短端点A00来源归因已撤回 |
| 8-bit 420色度整边（A02） | 逐行解码和编码本地重建共用八线SSE2；独立FilterChromaEdge槽及Generic回退 | Partial | 两方向/混合bS/严格阈值/tc0+1/饱和裁剪解析值与全缓冲差分；新内核保护页、包装层栈缓冲0分配；High直接AU及编码三方Oracle通过；五样本见performance §21 | 10-bit、422/444、场/MBAFF保留原入口；原生仅Windows/amd64，其他平台构建证据另列；Profile与并发状态不变 |
| P权重分量快路（A03） | decoder逐分量恒等跳过，非恒等复用共享块入口 | Partial | 六格式、场ref、独立平面和单样本公式对照、加权Oracle；阶段数据见gates/a03_p_weights.md | encoder自己的逐样本权重循环未迁移；不扩展Profile功能范围 |
| 整块均值（A04） | decoder B块均值使用Average8/16 SSE2槽，保留精确原地和部分重叠顺序 | Partial | 奇数尾部/stride/最小len/完整uint16域差分、直接保护页、栈缓冲0分配及FFmpeg像素Oracle通过；五样本见gates/a04_average_blocks.md | width<4及不适合SIMD的几何用Generic；encoder仍逐样本均值；受影响purego/386/race及六目标构建/测试编译通过，Unix/NEON原生未验 |
| 整块单/双向加权（A05） | decoder P非恒等与B块非均值权重进入Weight8/16 SSE2槽，4样本批量32位乘加/移位/Clip1 | Partial | 规范参数、全uint16域、负数舍入/offset缩放解析值及直接保护页；旧宽域/部分重叠回退、局部0分配、FFmpeg像素Oracle通过，见gates/a05_weight_blocks.md | 奇数宽、其他参数和不合适别名继续原int64；encoder仍逐样本权重；NEON原生未验，Profile状态不变 |
| IDCT＋Add融合（B03） | 编解码重建共用4×4/有界8×8 Generic融合，int64中间值，列结果直接Add/Clip | Partial | 原组合路径、极值/双位深/错误前缀/系数不变/局部0分配及Oracle通过，见gates/b03_idct_add.md | 8×8宽域和显式其他后端保留原路；不合并反量化，不新增ISA；端点收益未确认 |
| 残差加减（K13） | 8/10-bit共享Clip1和Subtract，picture与编码真实残差循环迁移，R3精确放行picture | Partial | 本机四路径parity、int32极值、低两位、奇数stride/最小len逐行保护页及局部0分配通过 | gather/scatter与不足4采样尾部为Go，值ABI仅向量化加减；amd64默认直接标量避免gather开销，显式后端保留真实SIMD；NEON原生未验 |
| 分数MC SIMD（B02/K09b-d子集） | 8-bit亮度六抽头/qpel与色度双线性SSE2，4/8/12/16宽全相位，Put/Avg共用 | Partial | 未裁剪双轴int32、全相位Go对照、逻辑边界/FieldView/halo保护页及0分配、FFmpeg Oracle通过，见gates/b02_fractional_mc.md | 10-bit和其他ISA仍原Go，其他宽度回退；不是完整K09全部平台覆盖 |
| 搜索去逃逸（G01/K18前置） | caller-owned双位深Workspace，真实编码器跨帧/场/Reset复用，结束解除借用 | Partial | 32组改造前Scorer轨迹/结果，64组prepared零分配，强制x86/purego/386/M8指纹 | 该行仅搜索；完整决策保护网见G03b，K16仍是串行租约复用，未开并行 |
| 残差去逃逸（G02/K18切片） | 具名调用唯一共享量化器、共同444亮度重建入口解除递归逃逸 | Partial | 384组改造前系数/CBP/重建指纹；六格式/场/矩阵/两死区/4与8变换的真实残差和四种帧间分区0分配 | 该行仅残差，完整决策见G03b；不新增量化策略或SIMD量化 |
| 帧内邻域（G03a/K18切片） | 调用者栈上固定邻域槽；相同可用性预检避免枚举不可用模式的错误分配 | Partial | 1152个旧I4/I16/I8候选指纹、六格式/场/矩阵/QP的prepared帧内候选0分配、邻域并存/清零解析值 | 该行仅单候选，完整决策见G03b；不将局部零分配扩大为整个库 |
| 真实试写（G03b/K18收尾） | 串行lane的RD/正式MB独立Writer复用；有效位追加；CAVLC固定level预检 | Partial | 原192个非空决策指纹；新增六格式/场/独立444/4与8变换、flat/custom、最小QPY/26/51完整决策0分配；错误后复用 | 极端QP允许合法PCM/全零胜者，每个矩阵family仍强制覆盖非空压缩；不是整个codec零分配，完整M8未验收 |
| 引用清尾（G04） | picture pool、DPB retained metadata、NAL pending、decoder slice entries 的失效引用清理 | Partial | `TestPool_GetClearsReleasedSlotAndEmptyBucket`、`TestReferences_RemoveClearsRetainedState`、`TestAssembler_ClearConsumedUnitSlots`、`TestDecoder_ClearSliceEntries`及四包定向测试 | 仅解除已确认的容器留存；有界缓存、公共Frame backing、编码/解码完整所有权及自然GC/稳态指标仍待完成 |
| 有界 picture 缓存（G05） | 既有 `picture.Pool` 的实际 backing 字节预算、每桶上限、格式隔离和 Trim 账本 | Partial | `TestPool_BoundsFreeStorageAndCacheStats`、既有池复用/并发/格式隔离测试、picture race | 默认每 Pool 空闲预算 64 MiB，不限制活跃图像或 RSS；公共 Frame backing、encoder/decoder 所有权和全库 GC 证据仍待完成 |
| 公共 Frame backing（G06） | `NewFrame`、新的 Frame 头、原始 backing 有界池、清零和输入/输出调用者迁移 | Partial | 根包 NewFrame 生命周期/错误/浅拷贝租约测试，decoder/encoder/stream 定向回归及 High10 值域保护 | 公共 Frame 头/租约仍允许少量分配；G07/G08 完整所有权、长期 GC/RSS、CLI 全路径和 M8 最终门禁仍待完成 |
| encoder 图像/RC 所有权（G07/K15收尾） | Encoder 私有池与普通/场/独立平面候选唯一所有权；创建层失败、RC best fatal、拒绝/替换、Reset/Close 回收 | Partial | 低QP拒绝租约回归、后续场/平面QP拒绝与成功重试、既有RC/B和固定码流保护网 | 本轮补齐创建层失败与独立平面池接入；自然GC/RSS、完整长期所有权及M8门禁仍未验收 |
| decoder retained metadata（G08） | frameState 失败状态清尾、DPB Picture Reset 归还、输出/重排队列生命周期保护 | Partial | decoder生命周期/恢复、池账本；本轮集成全库race通过 | DPB retained backing未池化，临时复用见K16；自然GC/RSS及长期集成门禁仍未完成 |
| AU 容量与粘性失败（G09） | AUReader 失败状态清零、NAL/AU 输入输出容量边界和 Packet.Data 独立所有权 | Partial | AU错误/重复读取、writer容量；本轮集成全库race通过 | 未新增封装算法；自然GC/RSS及长期集成门禁仍未完成 |
| 串行元数据复用（K16精简） | decoder临时与DPB retained分离；encoder候选/场/平面独占租约；各实例空闲16MiB/三槽 | Partial | 六格式脏缓存像素、错误恢复、RC best保持与租约/预算测试；原端点五次before/after | 不是完整slice执行状态隔离；未池化DPB retained backing，未开启内部并行，GC/RSS与长稳未测 |
| 正常P/B宏块去分配（K17切片） | 栈上固定分区backing，保留全部分区/子类型与读取顺序 | Partial | prepared CAVLC/CABAC、8/10位、P/B非零残差、空间/时间direct的像素/MV/身份/读端与0分配 | 不含每slice初始化、MBAFF首次场引用等完整K17配置门禁；不是整个decoder零分配 |
| slice并行（K19收益准入） | 本轮暂不实施worker，保留原并行设计 | TODO | 实际h264probe：现有1080p基准每帧1slice，320×240控制语料每帧2slice | 不证明代表性多slice收益；owner-first/私有执行上下文与调度均未交付，Threads>1仍拒绝 |
| RC纯分析复用（K20精简） | 单次RC调用九槽缓存AQ分类/权重/加权搜索参考；场/平面槽分离 | Partial | 改动前144帧RC码流/像素/时钟/QP/VBV SHA不变，固定权重失败换源重试对照，真实ABR前后五样本 | 无worker/跨帧pipeline，CQP不建缓存；时间未加速，GC/RSS未测，不是完整K20 |
| ME候选并行（K20m范围决定） | 本轮暂不新增批执行器或候选worker | TODO | 现有串行搜索workspace/Scorer轨迹和零分配保护网保留 | 默认diamond/refine粒度小；尚无full-search代表性并行端点证据，不代表证明full-search无收益 |
| K15–K20m阶段集成验证 | 各代码项独立常规门禁；受影响purego/386、六目标build、短fuzz与全库race | Partial | 12执行记录含命令/结果/日志SHA，codec生产算法cf1dd8feea | 仅已授权精简切片，不签完整M8；本轮剩余测试与性能证据另列 |
| K21已实现内核基准 | 501个实际case，覆盖双位深/边界/尾部/矩阵/宽域及真实可用后端 | Partial | 2505个正式100ms样本全部0B/0alloc；完整原始值和SHA见gates/m8_kernel_samples.json | 源码e299d8a125；NEON未原生运行，MC插值仍Generic，不伪称完整后端交付 |
| M8剩余功能验证 | 非缓存全库、显式双尺寸300帧Oracle、全库purego/386/测试标签、CLI、race | Partial | 全库race及定向purego race、55语料实际CLI、六格式×四码控24组FFmpeg像素和六目标汇编编译通过 | 保留analytic/FFmpeg来源和NEON跳过；早期PS中文JSON事件损伤已登记，不把可解析子事件当完整计数 |
| 本轮测试与性能收尾 | 按用户要求，以已有通过测试和完成的性能样本结束，不再扩展矩阵 | Partial | 功能验证通过；1080p八组各五样本及720p Generic五样本完成，数字见performance §16.2 | Auto编码未完成样本排除，不宣称30FPS或新增未实现能力；本轮收尾工作已结束 |

## M9

v1.0.2性能迭代按七个独立实现提交完成：Direct像素任务合并、块级加权、零/DC重建、
帧内邻域去逃逸、重复网格Reset清理、CABAC归一化、整帧网格访问。
各项完整常规门禁及相关High直接AU FFmpeg像素比较通过；六格式、逐4×4运动元数据、
负权重/舍入/裁剪、错误与位消费契约保留。各阶段五样本、失败诊断与最终验证见performance §18。
Profile状态、公共API和并发模型不变；后续A01/A02已实现限定8-bit亮/色度整边SSE2，见performance §20/21；DPB retained池化及扫描表预验证仍留作后续候选。

**v1.0.0 本地收尾完成。** 按用户要求复用现有实现、测试和工具，整理 README、既有 API/流式用法、支持边界、一致性报告和 CHANGELOG；不新增编解码算法、公共 API 或发布框架。范围调整已同步到 Roadmap v1.2，完整 Profile 状态保持原结论。

本轮执行 `H264_LONG_TEST=0 bash scripts/verify.sh`，退出 0：CGO=0 的 vet、build、test、gofmt 检查、36 包依赖规则和 55 条语料校验全部通过；多数包复用 Go 测试缓存，tests/codec 实际运行 50.946 秒。实际启动 h264enc/h264dec/h264probe 完成 32×32 High/YUV420P 单帧文件流程，图片数为 1，常量 128 的输出采样与输入一致，临时文件已清理。

原 race、purego/386、六目标构建、独立 Oracle 和性能记录沿用 M8 已完成结果，本轮不重跑。8 小时混合长稳、原生 arm64、远端 CI 未执行；内部并行未实现；本地版本不宣称完整生产认证。发布使用一个逻辑提交及注释标签 `v1.0.0`，不 push、不创建远端 release；既有 `scripts/verify_m8.ps1` 未提交改动保留，不纳入本次发布提交。
