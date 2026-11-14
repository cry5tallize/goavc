# 编码 API

`encoder.New(cfg, options...)`创建非并发安全的串行编码器，默认渐进。M7支持8/10-bit
YUV420/422/444真实压缩与可选MBAFF场编码。渐进420显示宽高须为偶数，422仅宽须为偶数，444宽高可为奇数；隔行另受下述裁剪单位限制。
支持CQP/ABR/CBR/VBV与双锚点B重排，默认Baseline用CAVLC，其余已接入Profile用CABAC；Profile整体仍按支持矩阵限定。

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
- Main/High/High10设置MaxBFrames=1..16后，先输出未来参考锚点，再输出中间非参考B帧。B采用双向零MV spatial-direct与量化残差，残差全零用B_Skip；不做B运动搜索。PTS必须等间隔，Duration固定（非正值按1个时间戳单位调度），否则显式拒绝。DTS从首PTS减MaxBFrames×步长开始递增。末尾Flush继续Receive排空；每次成功Receive后获取对应Reconstruction。B与Lookahead可并用，但不支持PCM。

选项：

| 选项 | 行为 |
|---|---|
| 默认 | SAD选择I4/I16与色度模式；4×4变换、按Profile默认熵编码、共享本地重建与去块；复杂块允许I_PCM大小决策 |
| WithPCM() | 整帧无损 I_PCM，关闭滤波；用于传输与独立像素验证，QP 不影响输出 |
| WithCABAC(enabled) | 显式选择CABAC/CAVLC，保持默认行为；Baseline禁止CABAC，其他已支持Profile可选双熵 |
| WithSliceCount(n) | 1..64，且不超过宏块总数；连续光栅地址等分；预测和熵上下文在 slice 边界隔离 |
| WithMacroblockQP(fn) | fn(mbX,mbY)返回语法QPY：8-bit为0..51、10-bit为-12..51；同步回调，不得重入，不与PCM共用 |
| WithSearchRange(n) | 运动搜索半径 1..64 整数像素，默认16；按小图像自适应收缩 |
| WithFullSearch() | 用穷举代替默认菱形；两者都通过共享 MC 细化半/四像素 |
| WithAdaptiveQP(n) | 按亮度活动度对基准QP加减0..6，裁剪到该位深的语法域；默认关闭，不与显式QP回调或PCM共用 |
| WithWeightedPrediction() | 自动分析Main/High/High10 P slice的三平面显式权重；默认关闭，Baseline/PCM拒绝 |
| WithPredictionWeights(weights) | 固定单参考 P 的三平面显式权重，与自动分析互斥；denom 0..7，显式 weight/offset -128..127 |
| WithTransform8x8() | High/High10增加Intra8与P 8×8残差候选，按RD与4×4比较；默认关闭，Baseline/Main拒绝 |
| WithScalingMatrix8x8(intra, inter) | 两个光栅顺序[64]uint8矩阵，项为1..255；按值复制、隐式开启8×8，4×4/DC列表显式flat=16 |
| WithScalingMatrices4x4(matrices) | High/High10六个光栅[16]uint8列表：intra Y/Cb/Cr、inter Y/Cb/Cr，项1..255、按值复制；DC取对应首项，可独立于8×8配置 |
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

New显式拒绝Baseline CABAC/B、未接入的Profile/格式组合、B+PCM、Threads>1、显式宏块QP与码控并用、无法满足的Level和420奇数显示尺寸。B初始化额外检查三图像DPB预算。ABR/CBR/VBV目标窗口实证见[M5c/d进展](gates/m5cd.md)。LevelAuto按最坏PCM/EBSP预算保守选择，可能高于平均码率需要的Level。
M5a 对最坏预算超出最高 Level 的 Auto 配置，改为选择满足几何/帧率的最高级别，
并在发布 AU 前检查实际大小。1080p30 可初始化；内容超限返回 ErrResourceLimit，
保持原参考帧与编号，调用方可降低帧率/调整质量或 Reset。
这不是 HRD/VBV 码控保证；显式 Level 与 PCM 通道仍执行保守初始化预算。

运行时不依赖 FFmpeg；独立 Oracle、质量与分配记录见 [M4 门禁](gates/m4.md) 和
[性能基线](performance.md#6-m4-帧内编码基线)。AVCC重封装、增量I/O、pipeline与CLI用法见[M6流式文档](stream.md)。

## High10 原生采样与熵选择

设置`cfg.Profile=h264.ProfileHigh10`、`cfg.PixelFormat=h264.YUV420P10LE`；QP是语法QPY，
范围-12..51，内部有效QP'加12，不能把63作为公开语法QP。输入、Reconstruction及Decoder输出均为
逐采样16-bit LE字节，Stride以字节计，有效值0..1023；行填充不作为图像采样校验或写出。
SendFrame返回后的输入可修改/释放，前瞻与B队列仍保持独立所有权。

默认执行真实压缩，不以全PCM或降位深代替High10。`WithPCM()`是独立显式选项；
QP=-12且未启用transform bypass时仍不承诺无损。`WithCABAC(false)`选择CAVLC，
自定义8×8矩阵的CAVLC子流计数仅用于nC，去块按整个变换块判断非零。

CLI示例：`h264enc -width 32 -height 32 -profile high10 -pixel-format yuv420p10le -qp -1 -entropy cavlc -input in.yuv -output out.264`。
`-entropy auto|cavlc|cabac`分别沿用默认或显式选择；所有格式/预算预检仍在打开输出文件前完成。
第4/5项的逐像素与完整常规门禁证据见[实现台账](implementation-status.md)。

## High422 / High444 与独立色平面

422使用`ProfileHigh422`及`YUV422P`/`YUV422P10LE`，444使用`ProfileHigh444`及
`YUV444P`/`YUV444P10LE`。10-bit仍使用上面的LE/字节stride和语法QPY契约；
共享预测、变换、重建和滤波内核不降采样、不丢低两位。

`WithScalingMatrices8x8Chroma`按intraY/interY/intraCb/interCb/intraCr/interCr接收六个光栅矩阵；
`WithScalingMatrices4x4`的顺序仍为intraY/Cb/Cr、interY/Cb/Cr。共同444的色度QP使用规范非线性映射，
三平面去块共享亮度bS，不能以逐色度非零统计代替。

`WithSeparateColourPlanes()`仅用于444，每个AU包含三个`colour_plane_id`的完整slice集合。
各分量独立预测、熵状态和运动，使用各自分量矩阵及Y量化过程；AU限额按三集合总和计费，
失败不发布部分AU。FFmpeg8明确不支持该原始封装：测试采用固定PCM规范向量及不重编码
slice_data的单色投影Oracle，不据此承诺外部播放器可解独立平面。普通共同444已直接通过FFmpeg。

422/444延续既有B工具边界：只生成非参考spatial-direct B，不生成transform bypass。隔行能力见下节。
四种格式实际`h264enc`/`h264dec`文件路径均已按原生格式与FFmpeg逐采样核对。

## MBAFF 顶/底场优先

`encoder.WithInterlaced(true)`选择顶场优先，`false`选择底场优先；不传选项仍为渐进。
输入仍是完整帧的逐行交织YUV，不是把两个场前后串接。编码器使用原生奇偶场视图和
真实field-coded宏块对，SPS声明MBAFF，每帧`pic_timing`的`pic_struct=3/4`表达场显示顺序。
宏块地址始终按顶/底成对编码，BFF不交换输入行。

该策略固定选择场宏块，不做自适应帧/场RDO，也不生成PAFF。Baseline显式拒绝；
高度必须是`2*SubHeightC`的倍数（420为4，422/444为2），编码高度按32对齐。
slice不拆宏块对；QP回调坐标的`y=2*场MB行+奇偶`，量化参数仍使用原位深语法QPY。
双熵、既有B/8×8/矩阵/权重、显式PCM与独立色平面沿用各自已登记边界。

CLI使用`-field-order progressive|tff|bff`，例如：
`h264enc -width 64 -height 64 -profile high -field-order bff -entropy cabac -gop 4 -bframes 2 -slices 3 -input in.yuv -output out.264`。
TFF/BFF实际CLI均已与FFmpeg逐采样比较，并由ffprobe逐帧核验`interlaced_frame/top_field_first`。
