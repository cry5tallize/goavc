# Baseline 帧内编码 API

`encoder.New(cfg, options...)` 创建非并发安全的编码器。M4 范围为串行、渐进式
8-bit YUV420、偶数显示尺寸；支持 CQP/ABR/CBR/VBV 帧级 QP 搜索，Main/High 使用
CABAC I/P及非参考B_Direct/B_Skip，Baseline使用CAVLC。B启用双锚点与POC重排；Profile整体Partial。

```go
cfg := h264.DefaultEncoderConfig()
cfg.Width, cfg.Height = 640, 360
cfg.QP = 26
enc, err := encoder.New(cfg, encoder.WithSliceCount(2))
if err != nil {
    return err
}
defer enc.Close()

// frame 是通过 Validate 的 YUV420P 帧，尺寸须与 cfg 一致。
if err := enc.SendFrame(frame); err != nil {
    return err
}
packet, err := enc.ReceivePacket()
if err != nil {
    return err
}
// packet.Data 是完整 Annex-B AU；其所有权归调用方。
```

接口语义：

- SendFrame 同步消费输入；成功返回后可修改或释放 frame。输出槽只有一个，尚未取走
  packet 时 SendFrame 返回 `ErrAgain` 且不消费输入。成功 ReceivePacket 后应继续送帧。
- WithLookahead(depth)启用后，SendFrame深拷贝输入，初次输出延迟depth帧；需循环Receive直到Again。Flush后继续Receive直到EOF，不能假定每个Send立即产出一个包。Reconstruction始终对应最近成功编码的帧，排空时在每次成功Receive后获取。
- 暂无输出时 ReceivePacket 返回 `ErrAgain`。Flush（或 SendFrame(nil)）结束输入，
  最后一个包仍可取走，之后返回 `ErrEOF`。Flush 幂等。
- Reset 丢弃待取包和重建，保留配置，从 IDR 编号 0 重新开始。Close 幂等，关闭后
  不能 Reset。用 errors.Is 判断哨兵错误。
- Reconstruction 返回最近成功编码帧的独立重建快照，不消耗 packet；保留 PTS/Duration。
  未编码返回 `ErrAgain`，关闭后返回 `ErrEOF`。返回帧使用完毕调用 Release。
- IDR AU 包含 SPS/PPS；P AU 只包含 slice，PTS=DTS，Duration 保留。
  KeyFrame 与实际 IDR 一致。首帧、IDRInterval 边界或输入 KeyFrame=true 时写 IDR。
  GOPSize触发非IDR I刷新、IDRInterval独立触发IDR；PCM通道仍全帧IDR。
- Main/High设置MaxBFrames=1..16后，先输出未来参考锚点，再输出中间非参考B帧。B采用双向零MV spatial-direct与量化残差，残差全零用B_Skip；不做B运动搜索。PTS必须等间隔，Duration固定（非正值按1个时间戳单位调度），否则显式拒绝。DTS从首PTS减MaxBFrames×步长开始递增。末尾Flush继续Receive排空；每次成功Receive后获取对应Reconstruction。B与Lookahead可并用，但不支持PCM。

选项：

| 选项 | 行为 |
|---|---|
| 默认 | SAD 选择 I4/I16 与色度模式；4×4 变换、CAVLC、共享本地重建与去块；复杂块允许 I_PCM 大小决策 |
| WithPCM() | 整帧无损 I_PCM，关闭滤波；用于传输与独立像素验证，QP 不影响输出 |
| WithSliceCount(n) | 1..64，且不超过宏块总数；连续光栅地址等分；预测和熵上下文在 slice 边界隔离 |
| WithMacroblockQP(fn) | fn(mbX,mbY) 返回 0..51；同步回调，不得重入实例；不与 WithPCM 共用 |
| WithSearchRange(n) | 运动搜索半径 1..64 整数像素，默认16；按小图像自适应收缩 |
| WithFullSearch() | 用穷举代替默认菱形；两者都通过共享 MC 细化半/四像素 |
| WithAdaptiveQP(n) | 按亮度活动度对基准 QP 加减 0..6，裁剪到0..51；默认关闭，不与显式 QP 回调或 PCM 共用 |
| WithWeightedPrediction() | 自动分析 Main/High P slice 的三平面显式权重；默认关闭，Baseline/PCM 拒绝 |
| WithPredictionWeights(weights) | 固定单参考 P 的三平面显式权重，与自动分析互斥；denom 0..7，显式 weight/offset -128..127 |
| WithTransform8x8() | High增加Intra8与P 8×8残差候选，按RD与原4×4比较；默认关闭，Baseline/Main拒绝 |
| WithScalingMatrix8x8(intra, inter) | 两个光栅顺序[64]uint8矩阵，项为1..255；按值复制、隐式开启8×8，4×4/DC列表显式flat=16 |
| WithScalingMatrices4x4(matrices) | High六个光栅[16]uint8列表：intra Y/Cb/Cr、inter Y/Cb/Cr，项1..255、按值复制；DC取对应首项，可独立于8×8配置 |
| WithLookahead(depth) | 1..16帧未来窗口确认场景变化并抑制单帧闪光，默认20%阈值；B启用时增加未来锚点后预读，避免跨场景参考；有界深拷贝与Flush排空，PCM拒绝 |
| WithSceneChangeThreshold(n) | 全平面平均绝对采样变化达到 1..100% 时触发 IDR；直接比较当前输入与已重建参考，不做 lookahead |

QP=0 仍为有损变换，不等同 PCM。无残差的 I4 不携带 mb_qp_delta，滤波沿用前一宏块 QP。
CBR/VBV使用规范filler NAL补足低复杂度画面的码量，填充进入AU上限与VBV约束；分数FPS预算保留余数。filler不改变像素，不代表已生成HRD参数或时序SEI。
加权预测在共享 MC 完成插值后应用，P_Skip 也使用同一权重；不是先对参考裁剪后再插值。自动权重仅在实际裁剪后的全平面 SSE 优于恒等预测时启用拟合结果；运动搜索使用加权参考副本作为启发式，最终残差/重建总是按规范顺序计算。暂不支持 B 显式/隐式权重编码。
Baseline/Main 的 Annex A 限制 PCM 采样为1..255、CAVLC level_prefix≤15。
强制 PCM 遇到0样本返回 ErrUnsupported；普通压缩允许0样本，但极低QP与极端色度邻接
可能没有当前实现可表示的合法候选，此时同样明确拒绝，并保留前一参考帧和编号。
调用方可提高QP或修正输入后重试。测试的 PCM 合法输入域与此边界分别验证。
M5b 用 SATD 搜索预测，再按三平面重建 SSE + 定点 lambda×实际 CAVLC 位数比较
Skip、P 的四种分区、I4、I16 和 PCM。运动搜索同时计入 MVD 比特代价；合法零残差 Skip
和语法位数下界允许安全提前终止。候选试探不会修改先前参考帧。
QP 回调所有返回值先校验，失败不发布半帧、不递增 IDR；调用方可修正输入后重试。

New显式拒绝Baseline CABAC/B、非YUV420P、B+PCM、Threads>1、显式宏块QP与码控并用、无法满足的Level和奇数显示尺寸。B初始化额外检查三图像DPB预算。ABR/CBR/VBV目标窗口实证见[M5c/d进展](gates/m5cd.md)。LevelAuto按最坏PCM/EBSP预算保守选择，可能明显高于平均码率需要的Level。
M5a 对最坏预算超出最高 Level 的 Auto 配置，改为选择满足几何/帧率的最高级别，
并在发布 AU 前检查实际大小。1080p30 可初始化；内容超限返回 ErrResourceLimit，
保持原参考帧与编号，调用方可降低帧率/调整质量或 Reset。
这不是 HRD/VBV 码控保证；显式 Level 与 PCM 通道仍执行保守初始化预算。

运行时不依赖 FFmpeg；独立 Oracle、质量与分配记录见 [M4 门禁](gates/m4.md) 和
[性能基线](performance.md#6-m4-帧内编码基线)。AVCC 与增量流式 I/O 在 M6 接入。
