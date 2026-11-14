# 支持矩阵

> **M5 开工：** 先推进 M5a。SAD、full/diamond 与亚像素搜索已有解析向量验证，
> 连续 P 帧单参考、四种分区与 Skip 已通过 360 帧双解码器 Oracle；默认 GOPSize=30，
> GOPSize=1 保留 M4 全帧内通道。M5 整体门禁未通过，质量阈值见 conformance.md。

> **M4 进展：** `encoder.New` 默认执行串行 Baseline CQP 的 I4/I16/色度压缩，
> 每帧独立 IDR；支持 I_PCM 选项、1..64 slice、偶数显示尺寸裁剪和宏块 QP 覆盖。
> 两条路径及混合 PCM 已通过 FFmpeg/自研解码/本地重建逐像素验证。
> 本地门禁及性能记录已完成，证据见 gates/m4.md；远端 CI 尚待执行。
> M5c 已接入Main/High CABAC I/P及非参考spatial B_Direct/B_Skip和重排。M5d已接入ABR/CBR/VBV，
> 但没有 HRD filler NAL，仍未通过 M5 总门禁。LevelAuto 新增实际 AU 预算门禁，
> 1080p@30 已通过 300 帧三方像素验证。M5a 本地门禁见 gates/m5a.md；M5 整体未完成。

> 状态图例：`TODO` 未开始 · `WIP` 进行中 · `Partial` 部分可用（有已知不支持路径）·
> `Conformant` 通过规定验证 · `Unsupported` 明确拒绝并返回 `ErrUnsupported`
>
> 规则：**只有通过 Oracle 或规范解析解测试的模块才能标 `Conformant`**；
> 仅靠自研编解码器互相验证的，最高只能标 `Partial`。
>
> **Profile 边界：** Baseline/Main PCM 仅1..255，CAVLC level_prefix≤15。
> 极低QP的内容若无合法候选则 ErrUnsupported；不能以 FFmpeg 可解替代 Annex A 约束。
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
| Main | 77 | CABAC、B、隔行 | Partial | Partial | CABAC I/P/P显式权重、spatial B_Direct/B_Skip非参考与重排已验证；隔行M7 |
| High | 100 | 8×8 变换、ScalingMatrix | Partial | Partial | CABAC I4/I8/I16/P 4×4/8×8、自定义8×8矩阵及spatial B_Direct/B_Skip已验证；字段/扩展M7 |
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
| CABAC：算术编解码器 | Conformant | Conformant | 编码区间解析解、64状态与 Main/High I/P 整帧 FFmpeg Oracle |
| CABAC：上下文初始化与状态转移 | Conformant | Conformant | I/PB0..2、QP 0..51 共用初始化表；编码 slice 使用 idc=0 |
| CABAC：宏块/MV/残差上下文 | Conformant | Partial | I/P四分区与4×4/8×8/DC/AC、非参考B_Direct/B_Skip残差Oracle；B非Direct分区未生成 |
| CABAC：terminate / cabac_zero_word | Conformant | Conformant | 停止位/PCM重启、逐slice bin密度预算、终端03封装、阈值解析解与5000个MPS密集流读回；现有整帧Oracle回归通过 |

## 5. 预测

| 特性 | Decoder | Encoder | 备注 |
|---|---|---|---|
| Intra 4×4（9 模式） | Conformant | Conformant | M4 全模式 SAD 实际选中，MPM/边界与 60 帧六类图案 Oracle |
| Intra 8×8（High） | Conformant | Conformant | 共享九模式预测、规范邻域、实际Intra8决策、High320帧三方Oracle；编码需WithTransform8x8 |
| Intra 16×16（4 模式） | Conformant | Conformant | M4 全模式实际选中、共享预测与 DC/AC 重建 Oracle |
| Chroma 8×8（4:2:0） | Conformant | Conformant | M4 全模式实际选中、双平面 SAD、共享 DC/AC/滤波 Oracle |
| Chroma 4:2:2 | Unsupported | TODO | 当前 SPS 显式拒绝；M7 实现 |
| Chroma 4:4:4 | TODO | TODO | M7 |
| Inter 16×16 / 16×8 / 8×16 / 8×8 | Conformant | Conformant | M5a 四种分区实际选中；单参考 P，360+600 帧双解码器 Oracle |
| 亚像素插值（1/2、1/4） | Conformant | Conformant | M5a 共享 MC、解析亚像素平移与完整 P 帧 Oracle |
| Weighted Prediction（explicit / implicit） | Conformant | Partial | 编码 Main/High 单参考 P 显式权重已验证：自动分析/固定三平面权重、负权重、裁剪、隐含默认值、多 slice 与160帧三方 Oracle；B/implicit 编码待实现 |
| B Direct（temporal / spatial） | Conformant | Partial | 编码spatial-direct零MV双参考、B_Skip/B_Direct残差实际选中，1120帧三方Oracle；temporal与非Direct运动搜索未生成 |
| 帧场自适应（MBAFF / PAFF） | Unsupported | TODO | M2 显式拒绝；M3/M7 按路线图实现 |

## 6. 变换与量化

| 特性 | Decoder | Encoder | 备注 |
|---|---|---|---|
| 4×4 逆/正整数变换 | Conformant | Conformant | M4 正向独立矩阵基底与完整 Oracle；编码重建共享逆内核 |
| 4×4 亮度 DC Hadamard | Conformant | Conformant | M4 常量/基底解析解及 I16 Oracle |
| 2×2 色度 DC Hadamard | Conformant | Conformant | M4 常量解析解及完整 Intra Oracle |
| 8×8 逆/正变换 | Conformant | Conformant | 规范逆基定点分析、解析空间基；High Intra8/P 8×8候选/信令/共享逆变换/去块，320帧三方Oracle |
| 量化 / 反量化 | Conformant | Conformant | M4 平坦矩阵 QP 0..51；编码重建共享反量化；非平坦正向 M5c |
| ScalingMatrix 应用 | Conformant | Partial | 编码器8×8 intra/inter矩阵PPS/量化/重建与320帧Oracle；4×4/DC编码仅flat=16，自定义4×4/DC属后续完整矩阵范围 |
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
| 单参考帧 | Conformant | Conformant | M5a 已滤波参考、IDR 边界、P_Skip、16 帧号回绕与长序列 Oracle |
| 多参考帧 + reordering | Conformant | TODO | M3 P/B 列表、短期/长期重排通过解析解与 Oracle |
| POC type 0 / 1 / 2 | Conformant | Partial | 编码I/P type2；启用B为type0、16-bit POC LSB、IDR锚点与显示重排已验证；type1编码未生成 |
| 滑动窗口标记 | Conformant | TODO | M3 PicNum 回绕排序、淘汰与所有权单测/Oracle |
| MMCO（含长期参考） | Conformant | TODO | M3 操作 1..6、长期参考、重置及专设 Oracle 语料；字段 M7 |
| 输出重排序与延迟输出 | Conformant | Conformant | 编码未来参考锚点先于B输出、规则PTS/Duration和递增DTS、Flush/Reset/Close/输入深拷贝，1120帧三方Oracle |
| Flush 排空语义 | Conformant | Conformant | M4 单槽输出、重复 Flush、排空 EOF、Reset/Close/背压测试 |

## 9. 编码器工具

| 特性 | 状态 | 目标里程碑 |
|---|---|---|
| CQP | Conformant | M4 固定 QP 与宏块 QP 回调，QP=0 仍为有损；无残差沿用前 QP |
| ABR / CBR / VBV | Partial | M5d | ABR 120帧平均、CBR 2×30帧窗口均在 ±10%；VBV 60帧候选 AU fullness 上下界搜索；无 filler NAL/HRD，完整 M5 门禁未完成 |
| GOP 结构 / IDR 间隔 | Partial | M5d | GOPSize/IDRInterval、连续1..16非参考B、强制IDR与场景边界；B需规则PTS与恒定Duration，完整长序列质量门禁未完成 |
| 场景切换检测 | Conformant | M5d | 直接阈值比较或WithLookahead未来窗口确认/闪光抑制；实际IDR序列、PTS/DTS与三方Oracle |
| Lookahead | Conformant | M5d | WithLookahead(1..16)有界深拷贝，未来确认场景/抑制闪光；B调度避免跨切换边界；背压/Flush/Reset/Close/失败事务已测 |
| RD 模式决策（J = D + λR） | Conformant | M5b 定点 lambda、SSE/实际 CAVLC 比特、Skip/P/I4/I16/PCM 候选；整段质量门禁仍待执行 |
| 自适应 QP | Conformant | M5b 活动度 QP 选项 0..6、QP 0..51 裁剪，默认关闭，显式回调/PCM 冲突拒绝 |
| SSE2 / AVX2 / NEON | TODO | M8 |

## 10. 明确不支持（第一版）

| 特性 | 原因 |
|---|---|
| MVC / SVC / MVCD / 3D-AVC | 独立扩展项目，不属核心 AVC |
| MP4 / RTP / WebRTC 封装 | 作为独立适配层，核心只提供 AU 接口 |
| RGB 转换 | 编解码链路只在 YUV 域定义，转换放库外 |
| SP / SI Slice 编码 | 仅计划解码支持（M7） |
| FMO / ASO 编码 | 仅计划解码支持（M7），编码收益低、复杂度高 |
