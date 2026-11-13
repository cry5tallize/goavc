# 支持矩阵

> **M4 进展：** `encoder.New` 默认执行串行 Baseline CQP 的 I4/I16/色度压缩，
> 每帧独立 IDR；支持 I_PCM 选项、1..64 slice、偶数显示尺寸裁剪和宏块 QP 覆盖。
> 两条路径及混合 PCM 已通过 FFmpeg/自研解码/本地重建逐像素验证。
> 性能记录与最终门禁尚待收尾；Main/High、P/B、ABR/CBR 明确拒绝。

> 状态图例：`TODO` 未开始 · `WIP` 进行中 · `Partial` 部分可用（有已知不支持路径）·
> `Conformant` 通过规定验证 · `Unsupported` 明确拒绝并返回 `ErrUnsupported`
>
> 规则：**只有通过 Oracle 或规范解析解测试的模块才能标 `Conformant`**；
> 仅靠自研编解码器互相验证的，最高只能标 `Partial`。
>
> 本文档与每个 PR 同步更新。任何新增的 `Unsupported` 必须写明原因与计划。
>
> **M2 状态说明：** Baseline 的无重排、单参考 CAVLC I/P 解码已通过整帧 Oracle。
> `Conformant` 仅针对表内注明的范围；Baseline Profile 整体保留 `Partial`，因为
> FMO/ASO、冗余 slice 等高级工具仍未实现。编码流水线从 M4 开始。
>
> **M3 进展：** CABAC/CAVLC 多参考 I/P/B、MMCO 与长期参考已验证；
> High 8×8/ScalingMatrix、Main 渐进帧 I/P/B、direct、加权预测和重排均已验证。
> Profile 整体保留 Partial，范围外的字段/高位深工具仍明确拒绝；本地门禁证据见 gates/m3.md。

## 1. Profile

| Profile | profile_idc | 关键工具 | Decoder | Encoder | 目标里程碑 |
|---|---|---|---|---|---|
| Baseline | 66 | CAVLC、I/P | Partial | Partial | M4 Encoder 仅独立 IDR I_PCM/I4/I16；P 在 M5a；高级工具 M7 |
| Main | 77 | CABAC、B、隔行 | Partial | TODO | M3 渐进帧 I/P/B、MMCO、权重与显示重排已验证；隔行 M7 |
| High | 100 | 8×8 变换、ScalingMatrix | Partial | TODO | M3 渐进式 8-bit 4:2:0 已验证；字段/扩展 M7；Encoder M5c |
| High 10 | 110 | 10 bit | TODO | TODO | M7 |
| High 4:2:2 | 122 | 4:2:2、10 bit | TODO | TODO | M7 |
| High 4:4:4 Predictive | 244 | 4:4:4、残差预测 | TODO | TODO | M7 |

## 2. 容器与封装

| 特性 | Decoder | Encoder | 备注 |
|---|---|---|---|
| Annex-B（3/4 字节起始码） | Conformant | Conformant | M4 编码输出四字节起始码、完整 AU，PCM/Intra 双解码器 Oracle |
| AVCC（1/2/3/4 字节长度前缀） | Partial | Partial | M1 读写已验证；尚无真实 MP4 语料 |
| avcC 配置记录 | Partial | Partial | M1 解析/序列化；尚无真实 avcC 语料 |
| Access Unit 组装（多 slice） | Conformant | Conformant | M4 1..64 slice 参数化，跨 slice 预测/熵隔离，滤波 idc=0；M6 增量 I/O |
| MP4 demux / mux | Unsupported | Unsupported | 明确不在核心范围内 |
| RTP 打包 | Unsupported | Unsupported | 未来独立适配层 |

## 3. 语法层

| 语法元素 | Spec | Decoder | Encoder | 备注 |
|---|---|---|---|---|
| SPS | 7.3.2.1 | Partial | Partial | M1 解析/序列化 + ffprobe；流水线 M2/M4 |
| PPS | 7.3.2.2 | Partial | Partial | M1 解析/序列化；独立 PPS Oracle 待补 |
| VUI | E.1 | Partial | Partial | M1 结构往返；timing/HRD 语义待 Oracle |
| ScalingList | 7.3.2.1.1.1 | Conformant | Partial | M3 4×4/8×8、SPS/PPS fallback、JVT 与实际应用 Oracle |
| SEI（user_data / recovery point） | 7.3.2.3 | Partial | Partial | M1 框架与 user_data；其余语义消息 M2/M3 |
| Slice Header | 7.3.3 | Conformant | Partial | M3 I/P/B、重排、权重和 MMCO 参数保留并消费；范围外工具明确拒绝 |
| Macroblock Layer | 7.3.4 | Conformant | Partial | M4 I_PCM/I4/I16 的 CBP/QP/MPM/残差已验证；P/B 编码在 M5 |
| Slice Data Partitioning | 7.3.5 | TODO | Unsupported | M7 |

## 4. 熵编码

| 特性 | Decoder | Encoder | 备注 |
|---|---|---|---|
| CAVLC：coeff_token / trailing ones | Conformant | Conformant | M4 规范码字、极端系数、完整 Intra Oracle |
| CAVLC：total_zeros / run_before | Conformant | Conformant | M4 亮度 DC/AC、色度 DC、长游程与整帧 Oracle |
| CAVLC：level prefix / suffix / 转义 | Conformant | Conformant | M4 suffix 0/1..6、百万 level 转义、14,400 组往返及 Oracle |
| CABAC：算术编解码器 | Conformant | TODO | M3 解码 MPS/LPS/bypass/terminate 解析解与 Oracle；编码 M5 |
| CABAC：上下文初始化与状态转移 | Conformant | TODO | M3 4:2:0 使用的 460 个上下文、64 状态已验证；4:4:4 M7 |
| CABAC：宏块/MV/残差上下文 | Conformant | TODO | M3 多参考 I/P/B、双列表、ref_idx、4×4/8×8 Oracle |
| CABAC：terminate / cabac_zero_word | Conformant | TODO | M3 停止位、PCM 重启、Annex-B zero-word 与 Oracle 已验证 |

## 5. 预测

| 特性 | Decoder | Encoder | 备注 |
|---|---|---|---|
| Intra 4×4（9 模式） | Conformant | Conformant | M4 全模式 SAD 实际选中，MPM/边界与 60 帧六类图案 Oracle |
| Intra 8×8（High） | Conformant | TODO | M3 滤波邻域、九方向解析向量及七份 High Oracle |
| Intra 16×16（4 模式） | Conformant | Conformant | M4 全模式实际选中、共享预测与 DC/AC 重建 Oracle |
| Chroma 8×8（4:2:0） | Conformant | Conformant | M4 全模式实际选中、双平面 SAD、共享 DC/AC/滤波 Oracle |
| Chroma 4:2:2 | Unsupported | TODO | 当前 SPS 显式拒绝；M7 实现 |
| Chroma 4:4:4 | TODO | TODO | M7 |
| Inter 16×16 / 16×8 / 8×16 / 8×8 | Conformant | TODO | M2 Oracle/解析解已验证；编码流水线按原计划推进 |
| 亚像素插值（1/2、1/4） | Conformant | TODO | M2 Oracle/解析解已验证；编码流水线按原计划推进 |
| Weighted Prediction（explicit / implicit） | Conformant | TODO | M3 P/B 三平面显式与 POC 隐式权重解析解及 Oracle |
| B Direct（temporal / spatial） | Conformant | TODO | M3 CABAC/CAVLC、B_Skip 与两类 direct Oracle |
| 帧场自适应（MBAFF / PAFF） | Unsupported | TODO | M2 显式拒绝；M3/M7 按路线图实现 |

## 6. 变换与量化

| 特性 | Decoder | Encoder | 备注 |
|---|---|---|---|
| 4×4 逆/正整数变换 | Conformant | Conformant | M4 正向独立矩阵基底与完整 Oracle；编码重建共享逆内核 |
| 4×4 亮度 DC Hadamard | Conformant | Conformant | M4 常量/基底解析解及 I16 Oracle |
| 2×2 色度 DC Hadamard | Conformant | Conformant | M4 常量解析解及完整 Intra Oracle |
| 8×8 逆/正变换 | Partial | TODO | M3 逆变换 Conformant；正变换属于编码阶段 |
| 量化 / 反量化 | Conformant | Conformant | M4 平坦矩阵 QP 0..51；编码重建共享反量化；非平坦正向 M5c |
| ScalingMatrix 应用 | Conformant | TODO | M3 4×4/8×8/DC，默认、SPS/PPS、自定义及 JVT Oracle |
| 10 bit 变换路径 | TODO | TODO | M7 |

## 7. 滤波

| 特性 | Decoder | Encoder | 备注 |
|---|---|---|---|
| 边界强度 bS 推导 | Conformant | Conformant | M4 intra 内部=3、宏块边界=4，混合 PCM Oracle |
| 亮度边缘滤波 | Conformant | Conformant | M4 编解码共享内核，三方像素一致 |
| 色度边缘滤波 | Conformant | Conformant | M4 非线性色度 QP 先映射再平均，共享内核 |
| slice 边界 / disable_deblocking | Conformant | Partial | M4 PCM 通道 idc=1；Intra idc=0 跨 slice 滤波；暂无用户自定义 offsets/idc |

## 8. 参考帧管理

| 特性 | Decoder | Encoder | 备注 |
|---|---|---|---|
| 单参考帧 | Conformant | TODO | M2 Oracle/解析解已验证；编码流水线按原计划推进 |
| 多参考帧 + reordering | Conformant | TODO | M3 P/B 列表、短期/长期重排通过解析解与 Oracle |
| POC type 0 / 1 / 2 | Conformant | TODO | M3 三种渐进帧推导、MMCO5 与 B 显示重排已验证 |
| 滑动窗口标记 | Conformant | TODO | M3 PicNum 回绕排序、淘汰与所有权单测/Oracle |
| MMCO（含长期参考） | Conformant | TODO | M3 操作 1..6、长期参考、重置及专设 Oracle 语料；字段 M7 |
| 输出重排序与延迟输出 | Conformant | TODO | M3 VUI/POC 缓冲、PTS/Duration 与 Flush 已验证 |
| Flush 排空语义 | Conformant | Conformant | M4 单槽输出、重复 Flush、排空 EOF、Reset/Close/背压测试 |

## 9. 编码器工具

| 特性 | 状态 | 目标里程碑 |
|---|---|---|
| CQP | Conformant | M4 固定 QP 与宏块 QP 回调，QP=0 仍为有损；无残差沿用前 QP |
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
