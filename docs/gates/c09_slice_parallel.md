# C09：整宏块行 slice 并行

在同一 `SendPacket` 内并行重建普通渐进、按码流顺序排列的整宏块行 slice，支持 8/10-bit 420、422、共同 444 和 CABAC/CAVLC I/P/B。`DecoderConfig.Threads=1` 强制串行；`0` 按当前 `GOMAXPROCS` 自动选择；正数为上限，实际任务并发度不超过 slice 数、`GOMAXPROCS` 和 64（包含调用者）。CLI 仍默认 `-threads 1`，可显式设为 `-threads 4`。

单 slice、行内切片、ASO、FMO、MBAFF、冗余表示、数据分区、SP/SI 和独立色平面保留原串行路径。不并行去块或跨帧操作；同一个解码器实例仍不得被外部并发调用。

## 所有权与错误

- 派发前验证宏块覆盖，串行准备参考列表。每个 worker 的 Grid 描述符、共同 444 残差 scratch 和 slice 熵读取状态私有；不复制帧像素或整帧元数据 backing。
- 连续宏块区间在读取 `decoded`、运动有效性或邻域数据之前限制访问。整行边界确保左上像素不会落入另一个任务；CABAC `ref_idx` 的 Direct 标志只在索引为正时才读取。
- 每片只写自己的宏块区域；整帧去块、扩边、输出、DPB/POC 提交保持原顺序。所有任务退出后按 slice 顺序选择第一个错误，再走原恢复流程；不会在 worker 写入时释放重建图。
- 新增结果槽受现有 `MaxSliceCount` 限制，额外 Grid 描述符与共同 444 工作区随活跃 worker 数增长。共同 444 每个并行工作区含 4,224 B 残差存储，64 个至多 264 KiB（不含描述符、栈和串行帧工作区）。未改变活跃像素/DPB 资源预算，不宣称池化元数据或零分配。

## 四 slice 端点

Ryzen 7 5700X、Windows/amd64、Go 1.27.4、CGO=0、GOAMD64=v1、GOMAXPROCS=16。固定 C00 的 1080p 300 帧四 slice 输入；同一候选二进制对比 `Threads=1` 与 `2/4`，每格五对独立进程、交替先后。计时含 New/Send/Receive/Release/Flush/Close，不含组 AU 与 FFmpeg 像素摘要核对。每次核对 300 帧及完整像素 SHA-256。

主矩阵使用四片正式测量时冻结的候选；最终控制组二进制另外包含 64 worker 硬上限（对测试的 1/2/4 线程不触发）及可选延迟诊断。两份二进制分别记录 SHA-256，不混合取中位数。

| 输入 | 线程 | 各侧中位 FPS（串行 → 并行） | 逐对耗时降幅中位 | 逐对加速倍率中位 | 并行效率（倍率/线程） |
|---|---:|---:|---:|---:|---:|
| High | 2 | 43.55 → 57.95 | 28.1% | 1.39× | 69.5% |
| High | 4 | 42.26 → 75.15 | 41.7% | 1.72× | 42.9% |
| High10 | 2 | 30.97 → 47.70 | 36.5% | 1.58× | 78.8% |
| High10 | 4 | 30.22 → 50.87 | 40.2% | 1.67× | 41.8% |

四线程相对各自串行基线增加约 0.56/0.55 MB/op、5,319/5,294 allocs/op（整个 300 帧序列）；不是活跃堆或 RSS。独立取中位的 FPS 不用于推导逐对增幅。全部端点样本与输入哈希见 [c09_samples.json](c09_samples.json)。不外推到单 slice 或任意视频。

两份固定四 slice 留出也各测五对，完整像素一致。4 线程的逐对加速倍率中位为高纹理噪声 **2.61×**、低码率重复运动 **1.65×**；这是 60 帧合成负载，不代表自然视频。

独立延迟诊断中，按单进程 300 次 `SendPacket` 计算的 P95（1/2/4 线程）为 High 28.31/23.41/15.68 ms，High10 39.17/29.12/26.63 ms。诊断含逐包计时开销，不并入正式吞吐样本，也不视为重复进程 P95 验收。

### 单 slice 串行对照

父版 `6f67daa` 与最终实现使用相同基准包装、`Threads=1`，两轮分别各五对。配对中位耗时变化：High 首轮 **+10.12%**、唯一补测 **−3.66%**；High10 首轮 **−3.18%**、补测 **−0.34%**。High 首轮的回退未复现，样本波动较大，不宣称单 slice 提速或零开销。所有样本像素一致；单 slice 不启动 worker。

复跑：按输入元数据设置 `GOAVC_M8_INPUT`、`GOAVC_M8_FRAMES=300`、`GOAVC_M8_YUV_SHA256`，冻结 `go test -c ./benchmarks` 二进制，分别设置 `GOAVC_M8_THREADS=1/2/4`，执行 `-test.run=^$ -test.bench=^BenchmarkDecoder_Sequence$ -test.benchtime=1x -test.count=1 -test.benchmem`。`GOAVC_M8_AU_LATENCY=1` 只用于独立延迟诊断，正式吞吐关闭。

## 正确性验证

- 六种像素格式 × CABAC/CAVLC 的两片 I/P/B 输入逐帧匹配 FFmpeg，并核对每个 AU 确实具备并行条件。`Threads=1/2` 的 PTS、Duration、KeyFrame、显示顺序一致；固定语料还验证了 `Threads=0/4/128`。
- Grid 视图拒绝已写入的跨片邻居；整行、行内切分和逆序 ASO 各验证路由及像素。双片截断按串行顺序报错、不输出半帧，同实例重复从 IDR 恢复并验证 Reset 后可重用。FMO、MBAFF、冗余表示、数据分区、SP/SI、独立平面在 `Threads=2` 下通过原规范向量。
- 本机通过 CGO=0 gofmt、vet、build、全库 test、依赖与语料校验，相关包 purego/386，全库 race 和 purego 并行定向 race。实际 `h264dec -threads 1/4` 对照像素哈希相同。未新增汇编，不作为其他平台原生验证、任意 slice 切法并行或完整 K19 验收的证据。
