# 支持矩阵

> 状态图例：`TODO` 未开始 · `WIP` 进行中 · `Partial` 部分可用（有已知不支持路径）·
> `Conformant` 通过规定验证 · `Unsupported` 明确拒绝并返回 `ErrUnsupported`
>
> 规则：**只有通过 Oracle 或规范解析解测试的模块才能标 `Conformant`**；
> 仅靠自研编解码器互相验证的，最高只能标 `Partial`。
>
> 本文档与每个 PR 同步更新。任何新增的 `Unsupported` 必须写明原因与计划。

## 1. Profile

| Profile | profile_idc | 关键工具 | Decoder | Encoder | 目标里程碑 |
|---|---|---|---|---|---|
| Baseline | 66 | CAVLC、I/P | TODO | TODO | M2 / M4 |
| Main | 77 | CABAC、B、隔行 | TODO | TODO | M3 / M5c |
| High | 100 | 8×8 变换、ScalingMatrix | TODO | TODO | M3 / M5c |
| High 10 | 110 | 10 bit | TODO | TODO | M7 |
| High 4:2:2 | 122 | 4:2:2、10 bit | TODO | TODO | M7 |
| High 4:4:4 Predictive | 244 | 4:4:4、残差预测 | TODO | TODO | M7 |

## 2. 容器与封装

| 特性 | Decoder | Encoder | 备注 |
|---|---|---|---|
| Annex-B（3/4 字节起始码） | TODO | TODO | M1 |
| AVCC（1/2/3/4 字节长度前缀） | TODO | TODO | M1 |
| avcC 配置记录 | TODO | TODO | M1 |
| Access Unit 组装（多 slice） | TODO | TODO | M1 接口 / M6 增量 |
| MP4 demux / mux | Unsupported | Unsupported | 明确不在核心范围内 |
| RTP 打包 | Unsupported | Unsupported | 未来独立适配层 |

## 3. 语法层

| 语法元素 | Spec | Decoder | Encoder | 备注 |
|---|---|---|---|---|
| SPS | 7.3.2.1 | TODO | TODO | M1 |
| PPS | 7.3.2.2 | TODO | TODO | M1 |
| VUI | E.1 | TODO | TODO | M1 解析 / M7 完整 HRD |
| ScalingList | 7.3.2.1.1.1 | TODO | TODO | M1 解析 / M3 应用 |
| SEI（user_data / recovery point） | 7.3.2.3 | TODO | TODO | M1 基础 |
| Slice Header | 7.3.3 | TODO | TODO | M2 / M4 |
| Macroblock Layer | 7.3.4 | TODO | TODO | M2 |
| Slice Data Partitioning | 7.3.5 | TODO | Unsupported | M7 |

## 4. 熵编码

| 特性 | Decoder | Encoder | 备注 |
|---|---|---|---|
| CAVLC：coeff_token / trailing ones | TODO | TODO | M2 / M4 |
| CAVLC：total_zeros / run_before | TODO | TODO | M2 / M4 |
| CAVLC：level prefix / suffix / 转义 | TODO | TODO | M2 / M4 |
| CABAC：算术编解码器 | TODO | TODO | M3 / M5c |
| CABAC：上下文初始化与状态转移 | TODO | TODO | M3 / M5c |
| CABAC：宏块/MV/残差上下文 | TODO | TODO | M3 / M5c |
| CABAC：terminate / cabac_zero_word | TODO | TODO | M3 / M5c |

## 5. 预测

| 特性 | Decoder | Encoder | 备注 |
|---|---|---|---|
| Intra 4×4（9 模式） | TODO | TODO | M2 / M4 |
| Intra 8×8（High） | TODO | TODO | M3 / M5c |
| Intra 16×16（4 模式） | TODO | TODO | M2 / M4 |
| Chroma 8×8（4:2:0 / 4:2:2） | TODO | TODO | M2 |
| Chroma 4:4:4 | TODO | TODO | M7 |
| Inter 16×16 / 16×8 / 8×16 / 8×8 | TODO | TODO | M2 / M5a |
| 亚像素插值（1/2、1/4） | TODO | TODO | M2 / M5a |
| Weighted Prediction（explicit / implicit） | TODO | TODO | M3 / M5c |
| B Direct（temporal / spatial） | TODO | TODO | M3 / M5c |
| 帧场自适应（MBAFF / PAFF） | TODO | TODO | M7 |

## 6. 变换与量化

| 特性 | Decoder | Encoder | 备注 |
|---|---|---|---|
| 4×4 逆/正整数变换 | TODO | TODO | M2 / M4 |
| 4×4 亮度 DC Hadamard | TODO | TODO | M2 / M4 |
| 2×2 色度 DC Hadamard | TODO | TODO | M2 / M4 |
| 8×8 逆/正变换 | TODO | TODO | M3 / M5c |
| 量化 / 反量化 | TODO | TODO | M2 / M4 |
| ScalingMatrix 应用 | TODO | TODO | M3 / M5c |
| 10 bit 变换路径 | TODO | TODO | M7 |

## 7. 滤波

| 特性 | Decoder | Encoder | 备注 |
|---|---|---|---|
| 边界强度 bS 推导 | TODO | TODO | M2 |
| 亮度边缘滤波 | TODO | TODO | M2 |
| 色度边缘滤波 | TODO | TODO | M2 |
| slice 边界 / disable_deblocking | TODO | TODO | M3 |

## 8. 参考帧管理

| 特性 | Decoder | Encoder | 备注 |
|---|---|---|---|
| 单参考帧 | TODO | TODO | M2 |
| 多参考帧 + reordering | TODO | TODO | M3 / M5a |
| POC type 0 / 1 / 2 | TODO | TODO | M2(type0) / M3 |
| 滑动窗口标记 | TODO | TODO | M3 |
| MMCO（含长期参考） | TODO | TODO | M3 |
| 输出重排序与延迟输出 | TODO | TODO | M3 |
| Flush 排空语义 | TODO | TODO | M3 |

## 9. 编码器工具

| 特性 | 状态 | 目标里程碑 |
|---|---|---|
| CQP | TODO | M4 |
| ABR / CBR / VBV | TODO | M5d |
| GOP 结构 / IDR 间隔 | TODO | M5d |
| 场景切换检测 | TODO | M5d |
| Lookahead | TODO | M5d |
| RD 模式决策（J = D + λR） | TODO | M5b |
| 自适应 QP | TODO | M5b |
| SSE2 / AVX2 / NEON | TODO | M8 |

## 10. 明确不支持（第一版）

| 特性 | 原因 |
|---|---|
| MVC / SVC / MVCD / 3D-AVC | 独立扩展项目，不属核心 AVC |
| MP4 / RTP / WebRTC 封装 | 作为独立适配层，核心只提供 AU 接口 |
| RGB 转换 | 编解码链路只在 YUV 域定义，转换放库外 |
| SP / SI Slice 编码 | 仅计划解码支持（M7） |
| FMO / ASO 编码 | 仅计划解码支持（M7），编码收益低、复杂度高 |
