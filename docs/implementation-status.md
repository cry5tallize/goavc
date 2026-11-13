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

## M3：Decoder Main / High（进行中）

当前完成首批 CABAC 基础与 I/P 接入，未完成 M3 退出门禁。Go 1.27.4、
CGO_ENABLED=0、FFmpeg 8.0 下，三份新增 Main 样本共 12 帧（64×48 全 I、
176×144 I/P init_idc=1、320×240 双 slice I/P init_idc=2）通过独立像素 Oracle，
输入与 Oracle YUV 哈希已登记在 manifest。M2 五份 Baseline 回归继续通过。

| Module | 规范条款 | Scope | Status | Tests | Known gaps |
|---|---|---|---|---|---|
| CABAC 算术引擎 | 9.3.1 / 9.3.3.2 | range/offset、MPS/LPS、bypass、terminate、截断粘性错误 | Conformant | `cabac/decoder_test.go` 固定解析 trace、64 状态×双 MPS 转移；真实 Oracle | 编码器在 M5；无 SIMD |
| CABAC 上下文初始化 | 表 9-12..9-24、9-44/45 | I/PB0/PB1/PB2，ctxIdx 0..459；可复现 CSV→Go 生成 | Conformant | 全部上下文×QP 0..51 的初始化检查、生成复现 | 4:4:4 扩展上下文在 M7 |
| CABAC 残差 | 9.3.3.1.3 | 4×4 亮度 DC/AC、色度 DC/AC、显著性与 level 转义 | Conformant | `cabac/residual_test.go` 固定向量、残差 fuzz、三份 Main Oracle | 8×8 ctxBlockCat=5 待接入 |
| CABAC 宏块与 MVD | 9.3.3 / 7.3.5 | I4×4/I16×16/PCM、P 各分区、Skip、QP delta、CBP、MVD 上下文 | Conformant | 三份 Main Oracle；`TestCABAC_PCMRestartOracle` 验证 PCM 后引擎重启 | B 宏块、ref_idx 在后续批次接入 |
| Main 解码流水线 | M3 范围 | CABAC 单参考、无重排 I/P、4×4 变换、多 slice | Partial | `TestCABAC_IntraPixelOracle`、固定 Oracle 哈希、M2 回归 | B、双向/direct、多参考/MMCO、加权预测、POC type 1、显示重排尚未实现，显式拒绝 |
| High 解码流水线 | M3 范围 | 8×8 / ScalingMatrix | TODO | 尚无新增 High 验证 | 8×8 逆变换、I8×8 预测、缩放矩阵选择与应用待实现 |

后续按 `docs/Implementation-Plan.md` M3 的参考列表/MMCO、B 帧与重排、
High 8×8/ScalingMatrix、完整 Oracle 与 race 门禁继续推进；当前不创建 v0.4.0 标签。

## M4–M9

后续里程碑的模块表在对应里程碑开工时补全；开工前这里只保留占位，
避免出现"提前承诺、实际未实现"的记录失真。

<!--
模板：

| Module | 规范条款 | Scope | Status | Tests | Known gaps |
|---|---|---|---|---|---|
| `cavlc.DecodeResidualBlock` | 9.2 | 4×4 亮度 AC/DC、色度 DC | Partial | cavlc/residual_test.go | 未覆盖 nC=-1 的色度 DC（见 #123） |
-->
