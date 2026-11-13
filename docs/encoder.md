# Baseline 帧内编码 API

`encoder.New(cfg, options...)` 创建非并发安全的编码器。M4 范围为串行、渐进式
8-bit YUV420、CQP、偶数显示尺寸；M5a 默认使用 IDR/P 单参考，GOPSize=1 可选择 M4
全帧内路径。Profile 整体仍为 Partial，Main/High/B 与其他码控尚待实现。

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
- 暂无输出时 ReceivePacket 返回 `ErrAgain`。Flush（或 SendFrame(nil)）结束输入，
  最后一个包仍可取走，之后返回 `ErrEOF`。Flush 幂等。
- Reset 丢弃待取包和重建，保留配置，从 IDR 编号 0 重新开始。Close 幂等，关闭后
  不能 Reset。用 errors.Is 判断哨兵错误。
- Reconstruction 返回最近成功编码帧的独立重建快照，不消耗 packet；保留 PTS/Duration。
  未编码返回 `ErrAgain`，关闭后返回 `ErrEOF`。返回帧使用完毕调用 Release。
- IDR AU 包含 SPS/PPS；P AU 只包含 slice，PTS=DTS，Duration 保留。
  KeyFrame 与实际 IDR 一致。首帧、IDRInterval 边界或输入 KeyFrame=true 时写 IDR。
  暂要求 GOPSize=IDRInterval，尚无 B 或延迟/重排；PCM 通道仍全帧 IDR。

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

QP=0 仍为有损变换，不等同 PCM。无残差的 I4 不携带 mb_qp_delta，滤波沿用前一宏块 QP。
M5b 用 SATD 搜索预测，再按三平面重建 SSE + 定点 lambda×实际 CAVLC 位数比较
Skip、P 的四种分区、I4、I16 和 PCM。运动搜索同时计入 MVD 比特代价；合法零残差 Skip
和语法位数下界允许安全提前终止。候选试探不会修改先前参考帧。
QP 回调所有返回值先校验，失败不发布半帧、不递增 IDR；调用方可修正输入后重试。

New 显式拒绝 Main/High、非 YUV420P、非 CQP、Threads>1、无法满足的 Level 和奇数
显示尺寸。LevelAuto 按最坏 PCM/EBSP 预算保守选择，可能明显高于平均码率需要的 Level。
M5a 对最坏预算超出最高 Level 的 Auto 配置，改为选择满足几何/帧率的最高级别，
并在发布 AU 前检查实际大小。1080p30 可初始化；内容超限返回 ErrResourceLimit，
保持原参考帧与编号，调用方可降低帧率/调整质量或 Reset。
这不是 HRD/VBV 码控保证；显式 Level 与 PCM 通道仍执行保守初始化预算。

运行时不依赖 FFmpeg；独立 Oracle、质量与分配记录见 [M4 门禁](gates/m4.md) 和
[性能基线](performance.md#6-m4-帧内编码基线)。AVCC 与增量流式 I/O 在 M6 接入。
