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

> 本阶段代码与测试已就绪，但**尚未在 CI 上执行过**（当前开发环境没有可用的 Go 工具链）。
> 因此下表状态全部为 `WIP`；CI 首次全绿后统一改为 `Conformant`，这一步是 M0 门禁的一部分。

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

| Module | 规范条款 | Scope | Status | Tests | Known gaps |
|---|---|---|---|---|---|
| `bitstream.Reader` | — | TODO | TODO | — | — |
| `bitstream.Writer` | — | TODO | TODO | — | — |
| Exp-Golomb | 9.1 | TODO | TODO | — | — |
| RBSP ↔ EBSP | 7.3.1 / 7.4.1 | TODO | TODO | — | — |
| NAL Header | 7.3.1 | TODO | TODO | — | — |
| Annex-B 扫描 | B.1 | TODO | TODO | — | — |
| AVCC / avcC | ISO/IEC 14496-15 | TODO | TODO | — | — |
| Access Unit 组装 | 7.4.1.2.3 | TODO | TODO | — | — |
| SPS / PPS 解析与序列化 | 7.3.2.1 / 7.3.2.2 | TODO | TODO | — | — |
| VUI | E.1 | TODO | TODO | — | — |
| ScalingList | 7.3.2.1.1.1 | TODO | TODO | — | — |
| SEI 基础 | 7.3.2.3 | TODO | TODO | — | — |

## M2–M9

后续里程碑的模块表在对应里程碑开工时补全；开工前这里只保留占位，
避免出现"提前承诺、实际未实现"的记录失真。

<!--
模板：

| Module | 规范条款 | Scope | Status | Tests | Known gaps |
|---|---|---|---|---|---|
| `cavlc.DecodeResidualBlock` | 9.2 | 4×4 亮度 AC/DC、色度 DC | Partial | cavlc/residual_test.go | 未覆盖 nC=-1 的色度 DC（见 #123） |
-->
