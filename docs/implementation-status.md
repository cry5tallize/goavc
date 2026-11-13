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

## M2–M9

后续里程碑的模块表在对应里程碑开工时补全；开工前这里只保留占位，
避免出现"提前承诺、实际未实现"的记录失真。

<!--
模板：

| Module | 规范条款 | Scope | Status | Tests | Known gaps |
|---|---|---|---|---|---|
| `cavlc.DecodeResidualBlock` | 9.2 | 4×4 亮度 AC/DC、色度 DC | Partial | cavlc/residual_test.go | 未覆盖 nC=-1 的色度 DC（见 #123） |
-->
