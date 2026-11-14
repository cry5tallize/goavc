# M6 流式编解码与命令行

`stream` 连接原始帧、完整 AU 与同步编解码器；不实现 MP4、RTP、网络重试或 RGB 转换。
运行时仅标准库、`CGO_ENABLED=0`。完整 Profile 的支持边界仍见[支持矩阵](supported-features.md)。

## 编解码入口

```go
src, err := stream.NewYUVReader(input, 640, 360, h264.YUV420P)
if err != nil {
    return err
}
dst, err := stream.NewAnnexBWriter(output, stream.AnnexBWriterConfig{})
if err != nil {
    return err
}
cfg := h264.DefaultEncoderConfig()
cfg.Width, cfg.Height = 640, 360
err = stream.EncodeStream(ctx, src, dst, stream.EncodeConfig{
    Encoder: cfg,
    MaxInputBuffer: 16 << 20,
})
if err != io.EOF {
    return err
}
```

解码输入先用 `NewAnnexBReader` 或 `NewAVCCReader` 切分 NAL，再用
`NewAccessUnitReader` 组装完整 Annex-B AU，输出接 `NewYUVWriter`：

```go
nals, err := stream.NewAnnexBReader(input, 8 << 20)
if err != nil {
    return err
}
src, err := stream.NewAccessUnitReader(nals, stream.AccessUnitConfig{
    MaxAccessUnitSize: 16 << 20,
})
if err != nil {
    return err
}
dst, err := stream.NewYUVWriter(output)
if err != nil {
    return err
}
err = stream.DecodeStream(ctx, src, dst, stream.DecodeConfig{
    Decoder: h264.DefaultDecoderConfig(),
    MaxInputBuffer: 16 << 20,
})
if err != io.EOF {
    return err
}
```

- 每个入口拥有并关闭自己的 codec；任何退出都清理 DPB、Lookahead、待输出帧。
- 源正常 EOF 后自动 Flush，持续 Receive 到排空，**最终返回 `io.EOF`**。
  错误退出不 Flush，不把半条流当作完整成功输出；损坏数据即使同时包装 EOF 也不吞掉。
- 每次成功读取的输入帧都由 pipeline `Release()`；解码输出在 Writer 返回后释放，
  Writer 失败或取消也释放。Writer 必须同步消费，不得保留帧或 Packet 引用。
- 输出先排空，再读下一输入；不启动内部 goroutine、没有额外预取队列。
  慢消费者会直接阻止后续生产，codec 本身的 B/Lookahead 窗口仍有界。
- 不关闭调用方 Reader/Writer，不承诺 I/O 失败后的传输回滚，也不隐式调用目标的 Flush/Close。

## 资源预算

`MaxInputBuffer=0` 使用 16 MiB（默认单 NAL 8 MiB 的两倍）；负数无效。

| 层 | 限制 | 检查点 |
|---|---|---|
| 编码 pipeline | 单帧三平面 `len(Data)` 总和，含 stride 填充 | 内置 YUVReader 固定布局在读取前；自定义源在 Send 前复核，超限帧仍释放 |
| 解码 pipeline | 一个完整 Annex-B AU 的字节数 | 内置 AUReader 在组装复制前收紧 AU 预算；自定义源在 Send 前复核 |
| NAL 源 | 每个 NAL 大小，Annex-B 另有固定 64 KiB 读缓冲 | Reader 构造时设置；AVCC 在长度字段后、载荷分配前检查 |
| AU 源/目标 | AU 大小、NAL 数、输出规范化后长度 | 使用各适配器配置；CLI 会将源 AU/NAL 预算收紧到输入限额 |
| Codec | 分辨率、单图像、DPB、B 与 Lookahead 窗口 | 原有 codec 配置/选项 |

这是分层载荷预算，**不是进程 RSS 总上限**。调用方源自行分配的内存、Writer 缓存、
codec 的参考图像与未来窗口不计入单个输入预算。使用自定义源时，须在源的分配前
实施对应限额；pipeline 不可能追回第三方源在返回前已经分配的内存。

## 取消与阻塞 I/O

每次源、目标和 codec 操作前后检查 `ctx`；取消优先返回 `ctx.Err()`，不再送下一帧。
当前单帧 codec 运算不在中途抢占，取消边界最多为正在执行的一帧操作。

可能无限阻塞的端点应额外实现 `ContextFrameReader`、`ContextFrameWriter`、
`ContextAccessUnitReader` 或 `ContextAccessUnitWriter`，pipeline 优先调用对应
`*Context(ctx, ...)` 方法。端点必须保证取消时返回且没有遗留后台 I/O。

普通 `io.Reader/io.Writer` 无法被 context 自动中断。安全选择是由 I/O 所有者在取消时
设置网络 deadline 或关闭自己拥有的管道/连接，并等待中断回调结束。不要在后台创建
一个永远卡在 Read 的 goroutine 后只让外层提前返回。库不擅自关闭借用资源；
对于不支持取消且可能永久阻塞的自定义端点，不承诺及时返回。

独立 `h264enc/h264dec` 拥有其打开的文件及进程标准流；首次取消信号会关闭这些资源
来中断管道 I/O。内部可调用的 CLI 函数仍将传入的标准流视为借用资源。

## CLI

```bash
CGO_ENABLED=0 go build -o h264enc ./cmd/h264enc
CGO_ENABLED=0 go build -o h264dec ./cmd/h264dec
CGO_ENABLED=0 go build -o h264probe ./cmd/h264probe

h264enc -input input.yuv -output out.264 -width 640 -height 360 \
  -profile high -qp 26 -gop 60 -idr 120 -bframes 2 -lookahead 2 -8x8 -weighted
h264dec -input out.264 -output decoded.yuv
h264probe -input out.264 -expect-frames 300 -expect-max-b 2
h264probe -syntax -input out.264 > syntax.jsonl
```

- 三个命令都显式选择 `-format annexb|avcc`，AVCC 用 `-length-size 1..4`，不猜测容器。
- 输入 `-input -` 使用 stdin；编解码输出 `-output -` 使用 stdout。
  进度与错误只写 stderr，`-progress 0` 关闭进度。使用二进制安全的 POSIX shell 或
  `cmd.exe` 管道；不要让旧版 PowerShell 文本管道改写 YUV/H.264 字节。
- 编码 `-fps` 支持 `30` 或 `30000/1001`；原始帧 PTS 为序号、Duration 为 1，
  GOP/B/DTS 在该帧时间基调度。`-rate cqp|abr|cbr|vbv` 与 bitrate/VBV 参数原样交 codec 校验。
- 配置先校验，再创建/截断输出；禁止输入输出为同一个普通文件，包括硬链接和路径别名。
  文件输出会覆盖原内容，传输或编码失败可能留下部分文件，不提供文件事务回滚。
- 原始 YUV Reader/Writer 支持 8-bit 420/422/444，不扩大当前 codec 的渐进 8-bit 420 支持。
  `-pcm` 的 Baseline/Main 输入采样必须在 1..255，零采样按 Annex A 显式拒绝。
- `h264probe -syntax` 每个 NAL 输出一行 JSON，打印共享解析器的 NAL 头、SPS/PPS、
  VUI/ScalingList、SEI 和 slice header。保留单个有界 NAL 与有界参数集，不缓存整条流。
  不解析宏块/残差；后续错误使进程非零退出，此前输出只是已成功解析的局部记录。
  语法模式不接受 `-expect-*` GOP 断言；默认模式仍输出原 GOP JSON。

## 验证与短时稳定性

`stream/pipeline_test.go` 将登记的全部受支持语料按 Annex-B/AVCC 与
1/7/4096/整文件 chunk 解码，核对独立 FFmpeg 显示顺序输出哈希；同时覆盖 PCM
规范常量、B/Lookahead 末组 Flush、资源限额、尾部截断、慢消费者、取消及释放。
CLI 测试包括真正子进程二进制管道、文件别名保护与 ffprobe 元数据对照。

M6稳定性验收入口（运行30秒，本地结果见[门禁记录](gates/m6.md)）：

```bash
CGO_ENABLED=0 go run ./tools/streamcheck -duration 30s -warmup 5s -sample-interval 5s > m6_smoke.json
```

同一对长存活 pipeline 通过 `io.Pipe` 连续传输 32×32 PCM、10 FPS；每个输出帧
检查独立常量解析值。阈值在运行前固定：预热后 RSS 增长不超过 32 MiB，
线性拟合增长率不超过 1 MiB/min，GC 后活跃堆增长不超过 8 MiB。
逐样本记录真实 RSS、HeapAlloc、HeapSys、帧计数与 goroutine；HeapSys 不冒充 RSS。
取消时关闭工具拥有的管道并 join 两个调用者，检测帧积压与退出后的 goroutine。
本项仅覆盖该输入的30秒运行窗口，不证明长期无泄漏，也不是所有内容的吞吐承诺。

按用户要求，M6以此短时测试作为稳定性退出标准，不再设置额外的长测等待条件。
已有[m6_smoke.json](gates/m6_smoke.json)报告通过；M9生产化混合输入验收范围不变。
功能按逻辑分项提交，发布仍须遵守其余门禁；远端CI尚未执行，未创建M6版本标签。
