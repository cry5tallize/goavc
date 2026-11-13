# Baseline 帧内编码 API

`encoder.New(cfg, options...)` 创建非并发安全的编码器。M4 范围为串行、渐进式
8-bit YUV420、CQP、偶数显示尺寸，每帧输出独立 IDR Annex-B AU。Profile 整体仍为 Partial。

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
- 每 AU 包含 SPS/PPS + IDR slice，PTS=DTS，KeyFrame=true，Duration 保留。
  M4 没有 P/B、延迟或重排，GOPSize/IDRInterval 为后续配置，当前每帧都是 IDR。

选项：

| 选项 | 行为 |
|---|---|
| 默认 | SAD 选择 I4/I16 与色度模式；4×4 变换、CAVLC、共享本地重建与去块；复杂块允许 I_PCM 大小决策 |
| WithPCM() | 整帧无损 I_PCM，关闭滤波；用于传输与独立像素验证，QP 不影响输出 |
| WithSliceCount(n) | 1..64，且不超过宏块总数；连续光栅地址等分；预测和熵上下文在 slice 边界隔离 |
| WithMacroblockQP(fn) | fn(mbX,mbY) 返回 0..51；同步回调，不得重入实例；不与 WithPCM 共用 |

QP=0 仍为有损变换，不等同 PCM。无残差的 I4 不携带 mb_qp_delta，滤波沿用前一宏块 QP。
QP 回调所有返回值先校验，失败不发布半帧、不递增 IDR；调用方可修正输入后重试。

New 显式拒绝 Main/High、非 YUV420P、非 CQP、Threads>1、无法满足的 Level 和奇数
显示尺寸。LevelAuto 按最坏 PCM/EBSP 预算保守选择，可能明显高于平均码率需要的 Level。
例如 1080p 30 FPS 目前超出保守预算，1080p 15 FPS 已验证；这与编码速度是两个限制。

运行时不依赖 FFmpeg；独立 Oracle、质量与分配记录见 [M4 门禁](gates/m4.md) 和
[性能基线](performance.md#6-m4-帧内编码基线)。AVCC 与增量流式 I/O 在 M6 接入。
