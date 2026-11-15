# 13：减少 GC 压力的专项开发实施计划

> **性质：后续实施计划，不是优化完成报告。** 本次只更新实施文档，不修改 Go、汇编、工具或 CI，不重新执行其他 agent 已报告通过的验证，不产生新的性能数据。当前起点是 [12_execution_log.md](12_execution_log.md) 的交接点，而非最初 M7 源码。
>
> 已交付 A00、K01–K08、K09a、K14a；NEON 按用户要求只做实现和交叉编译，原生证据仍未执行。不得重做 CPU 分发、SAD、SATD 或已完成的标量 MC 块化。完整 race 和 M8 最终验收仍未完成。

## 1. 目标、范围与取舍

目标是减少单位有效视频工作的**分配字节、分配对象数、GC 扫描与辅助标记成本**，同时约束缓存与存活集。不是追求最小 GC 次数，更不是关闭 GC。

本计划细化 K15–K18，并给 K19/K20/K20m 提供稳定的 workspace 和所有权。与 [05_memory_and_allocations.md](05_memory_and_allocations.md) 合用：05 定义公共 Frame API 和基础生命周期，本章规定当前代码的优先级、分支级实施步骤、测量及派工。本章不替代上级路线图，也不追加新的编码工具。

必须保持：

- 同输入、同配置，Annex-B、原生像素、PTS/DTS/Duration、QP/VBV 和错误分类不变。保持搜索候选与 Scorer 调用顺序、严格 `<` tie-break、RC 搜索及回调次数/顺序。
- 六格式、奇数显示几何、10-bit 低两位、MBAFF、独立平面、备用表示恢复均保留。
- SendFrame/SendPacket 返回后不保留调用方 Data；收到的 Frame/Packet 在后续调用、Reset、Close 后仍按既有契约有效。
- codec 实例仍非并发安全；先做串行内存改造，不顺手开 worker。
- 不改 GOGC/GOMEMLIMIT，不调用生产热路径 runtime.GC/FreeOSMemory，不使用 finalizer、unsafe 隐藏指针、实验 arena 或新的依赖。
- 不把所有对象塞进 sync.Pool。实例 scratch 采用明确 owner；缓存采用可计费的有界策略。sync.Pool 可被 GC 清空，不适合作为本次确定性复用门禁的基础。

### 1.1 四种成本分开验收

| 成本 | 对应问题 | 本计划策略 |
|---|---|---|
| 分配流量 | 每个候选/MB 产生短命堆对象 | 消除逃逸、固定 workspace、复用 writer 与数组 |
| 大块重分配 | 每帧/每 QP 尝试分配整图及输出平面 | 有界 picture/backing 缓存、候选交换与所有权转移 |
| 活跃指针图 | 队列尾槽、field cache、state 保留旧对象 | 清除真正失效的引用，分离临时/参考元数据 |
| 缓存留存 | 池化后 GC 次数下降，但内存高水位上升 | 按实际 cap 计费、超预算拒收、格式切换淘汰、Close 清理 |

byte/uint16/int32/MV 等无指针 backing 不逐元素扫描，但依然消耗分配/清零/内存带宽；少数指针能钉住整个大 backing。不能据对象小就忽略，也不能把所有像素字节称作 GC 扫描字节。

## 2. 当前证据与待验证假设

### 2.1 已有事实，不在本次重复测量

| 来源 | 已记录事实 | 不能推出什么 |
|---|---|---|
| 12 的 K02 与 performance §15 | SAD 间接调用后真实搜索 window 逃逸；P 控制场景约 335.76 MB、875.5k 次分配/op | 不是最新完整 High/B 场景基线；不能据此计算当前 GC 时间占比 |
| 12 的 A00 | B 基准分配字节中 interResidual flat 66.23% | 不能断言全由一个语句造成，也不能当 K07 后的当前比例 |
| K09a/K07 | 已准备的 MC、SATD 实际调用零分配 | 不等于搜索、模式决策或端点零分配 |
| 源码 search.go | window 含两块固定 [256]T，考虑候选时切片传入函数指针 | 需逃逸诊断和运行计数定位所有中间对象 |
| 源码 inter.go | interResidual 通过局部 quant4/quant8 函数变量传递局部残差/系数地址 | 间接调用可能解释逃逸；必须用当前编译器报告验证 |
| 源码 captureRD/encodeP/encodeIntra | 每候选/MB 创建位写缓冲；正式 trial 经 appendBits 复制位串 | NewWriter 本身是否堆分配由逃逸决定；backing 扩容需运行计数 |
| 源码 picture.Pool | 每尺寸/格式最多 4 张，没有全池字节上限；弹出槽未清 nil | 当前不能将每桶限制视作总内存限制 |
| 源码 toFrame/Reconstruction | 每次分配独立公共 Frame 及 byte 平面 | 当前不是 DPB 像素借用；不能靠暴露 DPB 消除复制 |

MC 控制基准 2.150s → 0.916s 属于已记录阶段结果，不是减少 GC 的 before/after，更不是主场景 30 FPS 证据。

### 2.2 实施时首先验证的假设

按当前证据排序；G00 的新 profile 可以调整 G02 以后的顺序：

1. 将短命搜索 window 的采样数组移到调用者持久 typed workspace，会消除随搜索次数增长的分配；预测：prepared search 的 allocs/op=0，候选/Scorer 轨迹不变。
2. 去掉量化处不必要的间接调用，或给真实逃逸临时量提供固定槽，会显著降低 residual 路径 B/op；预测：编译器不再报告对应局部系数/残差逃逸，prepared interResidual 为0分配。
3. Reset 复用真实试写 writer，会减少随候选/MB 数线性增长的缓冲分配；预测：复杂非 Skip MB 的分配不再随候选数增长，写出位数和码流不变。
4. picture/metadata 及输出 backing 复用会减少随帧数、RC 尝试数增长的大块分配；预测：固定几何的预热后新建次数进入稳定区间，失败/Reset/Close 后借出平衡。
5. 清尾和拆分 retained metadata 会降低仍可达堆，而不一定显著改变 allocs/op；预测：池/队列可达性与生命周期计数改善，GC 后堆不再随重复周期增长。

本任务是规划，不执行假设实验；实施 agent 不得把以上预测抄成测量结论。

## 3. G00：先建立 GC 专项反馈环

### 3.1 固定三类测量边界

1. **冷启动端点**：保留现有 `BenchmarkDecoder_1080p_Generic`，包含 New、三帧、Flush、Receive/Release、Close；不通过把初始化移出计时篡改历史基准。
2. **既有控制基准**：保留固定 `BenchmarkEncoderP_720p_Generic` 的 `-benchtime=1x`；B 基准一个 op 是四帧及 Reset/Flush，单独注明每帧归一化。名称 `_Generic` 不证明实际后端。
3. **新 warm-steady 场景（待实现）**：同一实例、固定配置，先完成真实预热，再连续处理压缩 I/P/B 与排空；接收输出及时 Release/丢弃 Packet。必须跨正常 GOP，不能每轮重建实例，也不能靠 Reset 将稳态工作藏到准备区。

prepared kernel/search/单 MB 门禁与端点分开。prepared 测试可以在外部准备图像/上下文，但重复任务本身的恢复、真实计算、输出校验在门禁内；不能逐次在未计时区重做隐藏分配。

### 3.2 固定输入与工作量

- P 控制、High/CABAC/B2/多 slice/8×8/权重/Lookahead2 各一组；ABR/CBR/VBV 至少有真实多个 QP 尝试的场景。
- decoder 包含 CAVLC、CABAC、B temporal-direct、MBAFF、共同/独立444、错误 primary→备用成功。
- 性能主表优先720p/1080p；六格式与复杂生命周期可用小尺寸定向测量，避免每个用例都做大尺寸长测。
- 所有输入固定 SHA；保留全部配置、输出哈希、后端、Go/OS/CPU、GOMAXPROCS、实际 Threads、环境变量和测量起止条件。
- before/after 使用同硬件、同输入、同配置、同完成帧数。记录 GC 开始/结束周期边界，不能用更少工作量制造 GC 次数降低。

### 3.3 指标与统计口径

| 指标 | 采集方式与边界 |
|---|---|
| B/frame、allocs/frame | 测量窗口 TotalAlloc/Mallocs 差值除以完成帧数，或 benchmark B/op、allocs/op 按真实帧数换算 |
| 自然 GC 周期/千帧 | NumGC 差值归一化；固定窗口计数为0也必须报告，不能除零算倍率 |
| GC CPU 与 assist | runtime/metrics 采样 GC 总 CPU、mark assist、dedicated 等累计值差；先用 metrics.All 验证 Go1.27 实际键与类型 |
| 扫描量 | 记录 /gc/scan/heap:bytes 等当前支持项；这是估计可扫描空间的 gauge，不是“本窗口扫描吞吐”的累计差值 |
| STW | 使用当前 Go 支持的 GC pause histogram；差分桶计数重建窗口分布，P95注明桶上界近似；不能将 GC CPU 当暂停 |
| HeapAlloc/HeapInuse/HeapSys/RSS | 分别列当前、采样最大值和平台来源；HeapSys 不是 RSS，离散采样不称精确峰值 |
| GC 后活跃堆 | 单独的留存实验在指定边界主动 GC 后测；禁止混进自然 GC 或吞吐场景 |
| 应用延迟 | Send/Receive/Flush 工作与输入到显示输出延迟分开；B/lookahead 的调用延迟不是独立显示帧延迟 |
| 业务计数 | QP 尝试/候选/完成帧/包、队列深度、pool hit/miss/拒收/空闲字节、借出高水位 |

GC CPU秒与壁钟秒并非同一维度；报告 CPU秒/千帧及实际并发环境，不随意给多核进程的“百分比”。ReadMemStats/metrics 只在批次边界或低频采样，不能每 MB 读取污染热点。

### 3.4 工具污染隔离

`tools/encodebench` 当前会保留整个 `stream []byte`，维护 originals/reconstructions，生成输入、做质量统计并可启动验证 decoder。其进程级 HeapSys/profile 不是 encoder 独占 GC；计时排除这些工作也不等于 GC 指标排除了它们。

- 保留原工具质量模式；另加明确的**不保留全码流的测量模式**或包内 warm benchmark，Packet 逐个哈希后丢弃，输入预生成/循环复用。
- 实施前确定唯一入口，优先扩展现有工具或 benchmark，不再发明第二套通用性能框架；新增 flags 和报告字段由单一 owner 维护。
- 相同可执行文件内做 before/after 指标差可以用于定位，但如进程仍有 Oracle、I/O生成、后台采样，其分配必须明确标为混合，不能写 codec-only。
- `tools/streamcheck` 当前32×32 PCM、10FPS，collect 会调用 runtime.GC；适合取消/留存回归，不适合证明自然 GC 频次、压缩热路径或主目标吞吐。保留该测试，不修改原阈值来适应缓存。
- `-memprofilerate=1`、逃逸诊断和 CPU profile 分别运行；高精度 allocation profile 不作为吞吐结果。

### 3.5 当前即可使用的定位命令

以下是给实施 agent 的命令，本次未执行。POSIX/Git Bash 示例，PowerShell使用等价环境设置及显式临时路径；检查每条退出码。

```bash
work="$(mktemp -d)"
CGO_ENABLED=0 go test -run '^$' -bench '^BenchmarkEncoderP_720p_Generic$' -benchtime=1x -benchmem -count=5 ./encoder > "$work/p-control.txt"
CGO_ENABLED=0 go test -run '^$' -bench '^BenchmarkEncoderB_720p_Generic$' -benchtime=1x -benchmem -count=5 ./encoder > "$work/b-control.txt"
CGO_ENABLED=0 go test -run '^$' -bench '^BenchmarkDecoder_1080p_Generic$' -benchtime=1x -benchmem -count=5 ./decoder > "$work/decode-cold.txt"
CGO_ENABLED=0 go test -run '^$' -bench '^BenchmarkEncoderP_720p_Generic$' -benchtime=1x -memprofilerate=1 -memprofile "$work/encoder.mem" -o "$work/encoder.test" ./encoder > "$work/profile-run.txt"
go tool pprof -top -alloc_space "$work/encoder.test" "$work/encoder.mem"
go tool pprof -top -alloc_objects "$work/encoder.test" "$work/encoder.mem"
CGO_ENABLED=0 go test -run '^$' -gcflags='github.com/cry5tallize/goavc/internal/motion/search=-m=2' ./internal/motion/search > "$work/search-escape.txt" 2>&1
CGO_ENABLED=0 go test -run '^$' -gcflags='github.com/cry5tallize/goavc/encoder=-m=2' ./encoder > "$work/encoder-escape.txt" 2>&1
```

profile 包括准备/预热等整个进程的采样分配，不直接等同 benchmark 计时窗口；按栈定位后，用 prepared/warm 运行计数验证。日志写临时目录，不把profile二进制提交仓库；报告保留命令、原始样本及 SHA。

## 4. G01：首先消除搜索 window 反复逃逸（K18 前置切片）

### 4.1 文件与接口

落点：`internal/motion/search/search.go`、search 测试、`encoder/mode_decision.go` 及 LSP 找到的 Integer/Refine/Estimate 调用。现有真实 `EstimateWithCost` 在 interCandidateUsingMotion；不要只优化导出 SAD 包装。

采用调用者拥有的 `search.Workspace`，固定持有8位和10位所需搜索存储，字段留在 search 包私有；或者按位深初始化唯一 typed backing，选择以结构大小和运行计数为依据。不得为每次搜索 New workspace。

1. 在 encoder 的持久执行 scratch 中持有 Workspace；串行一个执行 lane 一份，后续 K20m 每个同时在途搜索一份。
2. 工作区提供原始块、候选预测与 window 状态固定槽，候选循环引用已存在的堆 backing，而不是局部大结构切片传入函数指针后每次逃逸。
3. 准备调用重设 ref、reference、几何、radius、best/cost/sad/scorer，复制原始有效块；不用每次整体清零两个256元素数组，只读取已写有效矩形。
4. 搜索结束解除 ref/reference/scorer 借用，避免持久 Workspace 钉住旧图像或闭包捕获对象。
5. Integer→Refine 的原始样本可复用，但每阶段的 best 初始化、中心与候选顺序完全保持；不得据“重复候选”跳过 Scorer。
6. 全部真实搜索入口显式接收 workspace，上层一次迁移；开工先 LSP references，保留原参数语义，不保留暗中每次分配的旧签名 shim。07 中 executor 的尾部参数仍用于后续 K20m，统一冻结为 workspace 与 executor 两种不同职责，不为本任务提前启动 executor。
7. 不修改 simd SAD 分发，不用 `//go:noescape` 包装任意 Go 函数指针，不传整块数组值复制来掩盖数 KB 热路径开销。

### 4.2 必须证明

- prepared Integer/Refine/EstimateWithCost 的 byte/uint16、full/diamond、边界/亚像素路径均0分配；Scorer 顺序、次数、MV/SAD 与当前结果一致。
- 实际 encoder 调用复用该 Workspace；测试不能每次自己建一个全新的局部 Workspace 取代生产生命周期。
- `goavc_simdtest` 的 Generic/SSE2/AVX2 强制路径继续通过；purego 同样0分配。NEON只交叉构建，不新增原生声明。
- P 控制端点 before/after 给五次 B/op/allocs/op；不能将历史97.2k额外分配当硬编码目标，实际剩余数量由当前测量决定。

本切片不依赖整个 K15/K16 大改，可优先交付；后续 slice/ME scratch 接入复用同一 Workspace，不再建第二套。

## 5. G02：消除 residual 与量化调用导致的短命大对象（K18）

落点：`encoder/inter.go:interResidual` 及 profile 命中的 intra/transform8/chroma 对应路径；每个共享领域文件只有一个 owner。

1. 在当前 Go1.27 编译器中定位 `res`、`coeff`、8×8 input 与函数变量 quant4/quant8 的逃逸链。
2. 首选将固定量化策略改为直接具名调用分支，不通过局部函数变量转交局部数组地址；策略在 block 外确定，不为每个系数查接口或改变死区。
3. 若直接调用已让临时量留栈，就不增加池。确需长期 scratch 的数组才移到执行上下文固定槽；禁止每个 residual 一个 Pool.Get/Put。
4. 保持 residual/rdCandidate 的值语义；候选 best 保存的是独立快照，不能统一返回指向同一工作槽的指针。大值复制是否能减少另作同一证据驱动切片，不为“零分配”引入额外复制。
5. 覆盖4×4、8×8、444三平面、422 DC、独立平面和场路径；不能只证明默认8-bit420零分配。
6. 所有Quant*/Transform的合法域、舍入、溢出及已有错误原子性保持；不依赖新 SIMD 量化/变换交付。

验收：prepared interResidual/实际非空RD候选0分配；候选结构、码流与解析/Oracle结果不变；当前 alloc_space/alloc_objects before/after能解释减少来源。若主因不是间接量化调用，保留证据并按实际链修复，不按本假设机械改写所有量化代码。

## 6. G03：真实试写与候选小缓冲复用（K16/K18）

### 6.1 RD 与正式 MB trial

- `captureRD` 每候选 NewWriter，正式 `encodeP/encodeIntra` 另有每 MB trial。每个执行 lane 分别持有 RD writer 与 MB trial writer；不能共用正在被外层消费的同一 writer。
- 每次试写前 Reset，保持位累加器/未满字节状态与错误路径；沿用现有真实语法，不换成近似 rate 模型。
- `appendBits` 当前经 Writer.Bytes 获取副本。若新增 bitstream 的有界追加位接口，必须保持末字节有效位数、非字节对齐和 source 不变；Bytes 继续返回副本，不能悄悄改成借用。
- 优先使用可复用源/目标 writer 直接追加有效位；先 LSP references 再设计导出方法。禁止取私有字段或为此引入 unsafe。
- 残差最坏可写大小与PCM回退判定基于原规则；容量按实测/合法界限准备，不能按常见码量截断或降低质量。
- 错误返回后 scratch 恢复为可复用状态，不留下上一 MB 的 partial byte。

### 6.2 邻域与候选列表

`encoder/neighbors.go:blockSamples` 的上下邻域样本长度有几何上限；将高频临时 slice 换成固定数组/调用者 scratch 时，先查清 top/left 与候选是否同时存活，不能全返回同一个槽。families、分区列表、CABAC restart 缓冲以当前 profile 命中为准，不对全部 append 一概重写。

验收覆盖非空压缩残差、负QP/高位深、试写后PCM fallback、连续MB不同QP、未对齐位数、多slice Reset。prepared 正常模式决策达到0分配；不能只测Skip或直接测Writer.Reset。

## 7. G04：先清除无效引用，再复用容器（K15/K16 前置）

这是留存正确性切片，应与纯 perf 改动分开提交。

| 精确位置 | 开发步骤 | 不能误改的语义 |
|---|---|---|
| `picture.Pool.Get` | 弹出前把原 backing 槽清 nil，再缩短长度；空桶删除 | 借出图仍由唯一 owner 使用，不能因桶删掉释放图 |
| `dpb.remove` | 删除时清最终失效尾槽；释放主/Chroma运动、引用数组、MBField及field派生缓存等所属租约 | Identity可以仍被其他共位数组引用，不得复用身份对象制造ABA；先解除所有使用方再归元数据 |
| `nal.Assembler.takePending/reset` | 复制返回描述符后清掉已用pending槽；正确保留边界检测所需last/lastInfo | takePending不等于完整reset，不能过早清掉仍参与DetectBoundary的最后NAL状态 |
| `Decoder.SendPacket` entries | 复用前清旧sliceEntry指针域，处理本次多图→少slice尾部 | 当前局部entries只影响一次SendPacket存活；未持久化前不称跨调用泄漏 |

decoder out/reorder、AVCC pending 以及 lookahead 已有出队清槽，先核对再改，不重复修已经解决的问题。仍可审视 front-reslice 导致的容量丢失，但那是扩容成本，不是同一种留存缺陷。

测试通过强引用/容器槽与租约计数、后续真实图像重建证明；不依赖 finalizer 或“跑一次 GC 后RSS立即下降”。清空自由列表不保证 Go 立即归还 OS 页。

## 8. G05：有界 picture 与私有缓存（K15a）

延续既有 picture.Pool，不新增同功能池。

- 按 A00 冻结：每实例空闲像素总预算64MiB，每桶4张，精确格式/编码几何分桶。按三个平面实际 backing cap、采样字节、边框和stride计费；不能按显示分辨率估算。
- Get命中扣减空闲计费；Release接收增加；超桶/总预算直接不缓存；空桶删除，必要时确定性淘汰空闲桶，不引入复杂LRU。
- 大于缓存预算的合法帧允许正常分配、处理、输出，归还时不留存；缓存政策不能变成新的ErrResourceLimit。
- `Stats()` 已有三返回值，优先保留并新增独立缓存快照方法；实现时查引用，清楚区分尝试/成功分配、借出、归还、拒收及字节。校验分配失败不制造永久“借出”。
- Trim只清空闲；先释放reference/在途任务再Trim。晚到Release可被仍可达的池安全处理，不访问codec已关闭字段；Close语义不能依赖生产GC。
- 内部像素缓存不清整图，前提是全部有效域与所需halo均已正确写入；公共可见输出另有清零规则。

建议在G00冻结额外缓存政策：**每codec空闲typed metadata合计16MiB，动态byte scratch空闲缓存合计8MiB**；属于本专项新增建议，非当前实现事实。执行中超过预算的合法大工作对象在当次结束后丢弃，不改变支持范围。定长MB scratch按执行lane单独计费。若数据证明政策不适合，修改前公开账本和原因，不静默扩大。

把长期持有在实例字段里的大容量slice也计入复用缓存账本，不能因为“不在pool map中”就绕过上限。活动帧当前确实需要的存储不计为free，但不得把闲置容量永远标active。

## 9. G06：公共 Frame backing 与输入队列复用（K15b）

完全沿用05及A00冻结的根包 `NewFrame(width,height,format)` 契约与64MiB/每桶4帧/最多8非空桶政策。本章不再定义另一套FramePool。

- 根包持有新Frame头和一次性租约；internal/buffer只管理原始byte存储。公共头不能复用，避免旧指针再次Release释放后来新帧。
- callback捕获原始backing，不根据调用方可能改写的Planes归还；租约没有全局借出注册表，忘记Release的帧变不可达时仍能退化为普通GC回收，不能因统计表强引用变泄漏。
- 新公共Frame有效区清零；不能为了省清零新增未初始化公开入口。先报告clear成本，不将池化等同于无写带宽。
- 迁移toFrame、Reconstruction、YUVReader和copyQueuedFrame；保留crop、LE、独立输出。copyQueuedFrame改可失败时迁移lookahead/B所有调用者及回滚。
- lookaheadPrevious可能因为闪光抑制保留旧anchor；bPreviousSource/B输入在最后一次分析前不能Release。以实际角色表释放，不靠“已从队列移除”判断。
- Clone继续独立非池化；不要为省一个Frame头或租约堆对象牺牲公共幂等生命周期。

新增/扩展测试：同时持有多个输出；释放一张后反复触发内部复用；Frame/Clone/重建快照跨Reset/Close；修改Planes后Release归还原backing；重复Release及浅拷贝重复callback只归还一次；不同Frame并发Release；YUV截断/取消/I/O错误回收。浅拷贝不获得独立像素所有权，不测试同一Frame未承诺的并发方法安全。

## 10. G07：encoder图像与RC事务所有权（K15c/K16）

### 10.1 先列owner再替换New

| 角色/分支 | owning责任与结束条件 |
|---|---|
| 源图src | encodeFrameNow借出；普通压缩在候选编码/纯分析全部完成后归还 |
| PCM成功 | state可能为nil，src直接成为committed重建；必须转移，不能统一defer释放 |
| 正在尝试的recon/state/AU | 本次attempt唯一owner；创建层失败时自行回收，因为上层可能只收到nil,nil,error |
| 当前best | 与attempt分离存活；新best替换后才回收旧best，最终成功转移给提交层 |
| committed recon/reference/last.pic | 多字段借用同一owner；不能按字段分别Release |
| bPast/bFuture | 仍待编码B的旧past可晚于未来reference替换；最后一次B使用后才回收 |
| plane/field view | borrower，不归池；ordinary浅拷贝Picture可能复制pool字段，禁止通过视图Release |
| weightedSearchReference | 部分plane新分配、其余可能借用，不是完整owning Picture；单独管理搜索plane scratch |

推荐显式小型owner记录和有限角色集合，不做通用指针图垃圾收集器。不要在每MB增减原子引用计数。独立平面 `channel := *e` 时，新增pool/workspace/owner字段要逐项核对共享与独占；不能浅拷贝正在使用的writer/scratch再复用同一槽。

### 10.2 必须覆盖的失败分支

1. newFrameState已取recon后QP callback拒绝；创建层释放本层资源。
2. encodeIntra/P/CABAC/B/interlaced/separate内部失败；即使上层拿不到state，也无租约丢失。
3. pad失败、AU超限、VBV拒绝、feasible但不优于best：释放当前attempt。
4. 新best胜出：释放旧best，不覆盖其仍需比较的数据；fatal错误同时释放已有best。
5. 所有QP不可行：无attempt遗留，已提交reference/RC/帧号/输出不变。
6. 编码返回best后最终提交校验失败：归还best，旧committed仍有效。
7. separate第二/第三平面失败与field第二套state失败：唯一整图回收一次，已建元数据全部回收。
8. B延迟输出失败保留旧输入/锚点便于重试；只回滚本次未接受资源。

源与重建picture使用实例私有池；当前attempt/best/committed metadata需不同存储。不要仅分配一个全局frameState然后在下一QP覆盖best元数据。

### 10.3 AU backing边界

可复用私有slice/RBSP/EBSP/AU缓冲及filler容量；best AU与当前attempt AU必须分离。按尝试结果交换buffer角色，不重复复制整块best。

发布Packet时二选一：转移独占backing并从复用记录移除，或复制到独立Packet.Data。优先保持现有独占转移可能的路径，不能为了“统一池”多一次完整拷贝。Packet无Release，不能在ReceivePacket后自动回收其Data。所有增长先检查既有AU预算；不为每次尝试预分配最大理论AU。

验收复用既有失败重试/B/lookahead/RC/独立平面/场测试，并增加租约平衡、PCM晋升、保存多份Packet跨后续编码/Reset/Close、主动脏缓存复用。字段清nil不能单独证明所有权正确。

## 11. G08：decoder临时与DPB retained metadata分离（K16/K17）

落点：decoder/frame、redundant、cabac_slice、mbaff；internal/dpb、motion.Grid。该切片与K19的frameState隔离由同一状态owner按阶段交接，不能两组同时改同一结构。

### 11.1 三种生命周期

| 类别 | 字段/资源 | 回收边界 |
|---|---|---|
| 当前表示临时 | decoded/info/NZ/dcCoded/mvd/directBlocks/groupMap/sliceOwners、444 fullResidual、CABAC执行状态 | 所有平面重建/完整性检查/去块完成后；失败表示整体丢弃 |
| 参考保留 | Motion[0/1]、BlockReferences、MBField；独立平面对应Chroma字段 | Store成功转移DPB；MMCO/滑窗/IDR/Reset/格式切换最后解除时归还 |
| 视图/借用 | fieldPics、ref lists、Grid.mappedOwners、SPS/PPS/pic引用 | 结束任务时解绑；不作为owning存储释放 |

1. 不给整个frameState套一个全局sync.Pool。codec内typed scratch持有可复用数组，retained metadata作为独占租约移交DPB。
2. 非参考帧和失败Store由caller归还retained租约；成功Store由DPB唯一负责。DPB field派生引用借用根帧，不重复释放。
3. reference Identity可以由其他参考帧共位数据继续持有；保留其轻量不可变身份，不改成指向完整Reference/picture，也不复用Identity值造成旧引用指向新图。
4. fresh make当前隐含初始化全部改为显式有效域初始化；info.sliceID/intraModes/chromaMode等有-1哨兵，单纯clear不是等价初值。
5. motion.Grid.Reset/BeginSlice处理mappedOwners等借用；当前成功路径已解绑，新增失败与复用路径同样处理。
6. 普通/共同444/独立三平面、B双列表与MBAFF不能共用仍在存活的元数据槽；fullResidual指针不能浅拷贝后并发使用。
7. 当前串行按必要slot复用，不按slice数复制整帧数组。未来复杂度保持 `O(frame metadata + active workers × bounded scratch + DPB retained metadata)`。
8. refs/fieldRefs与CABAC engine在slice准备期就绪，避免首次场切换在MB测量区分配。Lists/Modify内部复用时保留32 active语义、MBAFF场扩展与修改顺序。

验收：prepared真实CAVLC/CABAC/B/MBAFF MB=0分配；temporal-direct跨多次参考淘汰保持正确；primary失败→备用成功与独立平面部分失败租约平衡。原新建路径与复用路径的像素/错误同样可观察，不只做数组长度断言。

## 12. G09：NAL/AU与指针容器容量复用（按profile后置）

本切片需G04清尾完成，不能直接删除AUReader每AU更换assembler的防留存措施。

- 先让assembler在take/reset后正确清理描述符与边界引用，再在AUReader复用有预算的描述符容量。
- 保留ReadNAL、ReadAccessUnit、EBSPToRBSP、Writer.Bytes等公共“独立返回”契约。复用内部destination能力通过明确接口引入，不能把原API悄悄改成借用。
- 自定义NAL源允许复用其返回缓冲，AUReader必须复制需要跨下一次read保留的payload；不能因内置源独立就取消通用保护。
- decoder分区B/C当前为定位slice_id与attach再次做EBSP→RBSP，可在同一调用内复用已验证转换结果，保留完整合法性/尾位/错误边界。
- scanner/NAL组装先验证预算再扩容；大AU→小AU后超缓存cap的backing丢弃或降回合理size class。不要无条件预分配MaxInputBuffer。
- 不引入异步预取，保持stream同步背压与取消边界。decoder直接API允许调用者积压输出，单个Packet也可包含多图；MaxInputBuffer/MaxDPBFrames不是总输出上限。本专项不擅自新增ErrAgain策略或丢帧。

验收：复用NAL源、下一AU首NAL预读、EOF/截断/粘性I/O错误、Annex-B/AVCC独立输出、慢DecodeStream writer阻止继续消费、反复大/小AU不持续留存。已正确清槽的队列不重复修。

## 13. 内存账本与缓存上限验证

每种对象填：owner、borrowers、分配点、提交/转移点、失败释放点、最后使用点、实际cap字节、缓存预算、是否含指针。

整体上界只按配置与调用者行为表达，不承诺固定RSS：

```text
library reachable = 活跃工作图 + RC attempt/best + 活跃输入窗口
                  + DPB像素/参考元数据 + 当前slice/MB scratch
                  + 库尚未交付输出 + 全部空闲缓存
process reachable = library reachable + 调用者仍持有Frame/Packet/Clone
```

- reference/recon/last与B角色按同一owner去重；view不重复计像素。
- slice scratch乘活跃worker，不乘总slice数；本阶段仍Threads1。
- 公共进程缓存64MiB不是每codec64MiB；picture64MiB与metadata/byte缓存分别计费，不能分别达标却报告“全库只占64MiB”。
- 输出Frame/Packet被用户合法长期持有时，内存可能增长；不把这种持有伪报为内部泄漏，也不靠修改输出所有权压低数字。
- 不允许pool的in-use map为了统计强引用全部输出；优先累计计数及内部owner测试。
- Reset释放活跃状态，可保留预算内缓存；Close解除实例私有缓存与owner；公共backing缓存可继续在进程级预算内存在。归池不是OS即时回收。
- 小→大→小、六格式轮换、坏输入恢复至少重复3个完整周期，比较后两个周期的稳态存活与容量；不得仅看首次高水位。

## 14. 任务卡、依赖与派工

所有G编号是本专项切片，不替代K编号，不意味着已实现。

| 切片 | 对应K项 | 前置 | 主要文件owner | 必交证据/建议主题 |
|---|---|---|---|---|
| G00 基线与账本 | A00/K21/K22补充 | 当前交接点 | benchmark/tools与性能文档owner | 当前alloc profile、自然GC/留存分开、固定场景；`bench[bench]: record gc allocation baselines` |
| G01 搜索workspace | K18，供K20m复用 | G00 | search与encoder调用接入owner | prepared双位深真实search=0、端点差值；`perf[motion]: reuse typed search workspace` |
| G02 residual去逃逸 | K18 | G00，可与G01独立设计 | encoder共享残差owner | 当前逃逸链、残差/候选0分配；`perf[encoder]: avoid residual scratch escapes` |
| G03 试写/邻域scratch | K16/K18 | G01/G02接口稳定 | encoder状态owner；bitstream接口独立owner | 位精确/PCM回退/真实模式决策0分配；`perf[encoder]: reuse macroblock trial buffers` |
| G04 清尾与解除持有 | K15/K16前置 | G00 | picture/DPB/NAL各自唯一owner | 可达性/生命周期分支测试；按实际问题单独fix提交 |
| G05 有界缓存 | K15a | G04 | picture/shared memory owner | cap计费/多格式/超预算拒收/Trim；`feat[buffer]: bound reusable storage caches` |
| G06 公共Frame backing | K15b | G05政策、05 API | root/buffer owner，caller顺序迁移 | 多输出/Close后合法/一次归还；`feat[buffer]: reuse public frame backing storage` |
| G07 编码图像/RC | K15c/K16 | G05；队列迁移需G06 | encoder状态owner | PCM/B/RC/separate/field所有权与真实分配；`perf[encoder]: reuse owned coding pictures` |
| G08 解码元数据 | K16/K17 | G04/G05 | decoder状态与DPB owner | retained租约/备用恢复/prepared MB0分配；`perf[decoder]: reuse frame execution metadata` |
| G09 AU容量 | K16/K17/K18补充 | G04，且G00显示值得做 | nal/stream owner | 独立输出/大→小/预算/背压；`perf[stream]: reuse bounded assembly scratch` |
| G10 集成验收 | K17/K18/K22/K23 | 本次交付范围全部完成 | 单一验收owner | 输出/race/GC/内存账本/未执行列表；`docs[performance]: publish gc reduction evidence` |

### 14.1 推荐顺序

1. G00先采集；G01、G02优先，尽早消除约875.5k级别分配控制场景中的高频来源，不等待全部SIMD内核完成。
2. G04/G05可与search工作并行，但共享文件归属明确；G06完成公共backing后再迁移B/lookahead输入。
3. encoder的G03/G07在同一状态owner下顺序集成，避免writer、残差scratch、pool、RC同时由多人改frameState。
4. decoder G08与encoder可独立推进；它先交串行复用，不与K19并行worker同时落地。
5. G09按新profile安排；所有声明属于本次交付范围的G项完成后统一G10。若希望缩小实施范围须先明确授权，不能只做默认420后标专项完成。

### 14.2 可直接复制的首任务提示词

> 只实现GC专项G01，先读13、05、07和12当前交接点。允许改internal/motion/search及encoder/mode_decision真实调用接入，不改SAD/SATD汇编、不改搜索策略、不启用并行。先LSP核对Integer/Refine/Estimate/EstimateWithCost引用，采用caller-owned typed Workspace复用原始块/预测/window状态，结束解绑scorer和reference。补充真实双位深、full/diamond/亚像素、Scorer顺序与prepared0分配证明，保留M8固定指纹；端点固定1x五次before/after，记录B/op与allocs/op。按仓库要求完成定向验证及最终完整常规门禁；中途不反复跑全库门禁，不与其他基准并发。更新实施记录与性能证据，不签完整M8，不补写arm64原生或完整race通过。不得以每次New workspace、关闭GC、修改golden或使用noescape掩盖问题。返回修改范围、精确命令/退出码、原始证据、剩余分配来源和下一任务接口。

其他G任务派工沿用11的模板，必须附本章对应所有权表与精确文件边界。

## 15. G10：验收、性能判定与停止条件

### 15.1 硬门禁

1. 已迁移kernel/包装、prepared search、正常MB重建及真实模式决策的AllocsPerRun=0；CAVLC/CABAC分别覆盖，正常路径不靠Skip/PCM替代。
2. 当前M8固定96帧指纹不变，六格式/场/高级表示现有解析解与Oracle不退化；固定QP、RC、PTS/DTS/QP/VBV轨迹逐项相同。
3. G06/G07/G08新增owner分支的定向测试通过；Reset/Close后库内活跃租约清零或明确转移给仍合法的输出，不能双回收。
4. 各缓存实际空闲cap字节/桶数始终在预算内；大于预算的合法工作仍可完成；尺寸/格式轮换、失败恢复后无持续增长。
5. 当前常规CGO=0完整门禁、受影响purego/386、六目标构建及短Fuzz按09执行。arm64继续仅编译验收，不冒充原生。
6. 共享池/不同输出并发Release/多个codec实例跑定向race；本轮集成完成后由唯一验收owner执行完整race。race是开发期CGO=1例外，不能说CGO=0跑过race，也不能说race证明汇编访存安全。
7. 实际CLI encode/decode走过已迁移的输出/输入路径；帮助与公开生命周期文档同步，不只库内测试。

### 15.2 性能验收而非虚构百分比

- G00先填每场景当前B/frame、allocs/frame和GC指标，再设对应路径的确定性上限；本规划不虚构“减少90% GC”等承诺。
- prepared零分配是硬目标；端点保留公共Frame头/租约、独立Packet.Data、Clone、冷启动及合法尺寸变更等不可混淆成本。
- 每个perf提交报告五次原始样本、中位B/op/allocs/op与ns/op。目标路径分配必须下降；若端点被其他来源掩盖，提供阶段计数和解释，不能假称端点已改善。
- 同工作量的自然GC/assist下降属于期望结果，不保证每个短样本GC次数严格单调；长期无GC样本报告0，不计算无限倍率。
- 若ns/op或P95中位退化超过5%，将其作为复查触发线而非自动容忍范围：在独占环境按预定同协议补一组样本，保留全部结果；稳定退化必须解释取舍并获明确接受，不能只挑最快一次。不能靠加大缓存换GC下降却隐藏留存增长。
- RSS和GC后堆以阶段性平台报告为证，比较稳定窗口而非要求归还池后立刻回落；完整8小时混合输入仍属于M9，不把本次短稳态实验当其通过。
- 仍保留原30FPS目标与当前差距；本专项减少GC不代表剩余SIMD/并行/完整M8已完成。

### 15.3 最终交接格式

按G00–G10逐项写：实现范围、revision、实际接口/缓存政策、before/after原始数据及SHA、输出一致证据、owner平衡、执行命令/退出码、跳过/未执行项、剩余分配Top栈。更新12与performance；只有实际实现状态变化才同步implementation-status/supported-features。不得用本规划将K15–K18从TODO标完成，不创建标签/push/release。
