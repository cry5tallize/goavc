# 10：性能测量、目标复审与发布门禁

对应K21/K22/K23/K24/K25；测量设计从A00开始。现有工具/脚本的实际实现与执行见§8及12，未实现的原设计范围继续保留，不把编排入口或编译成功当作门禁通过。

## 1. 测量原则

- 只有同一硬件、相同输入和编码工具配置、相同Go/环境、相同计时边界，才能计算before/after加速比。
- 性能测量期间停止其他长测、Oracle、race、构建和负载。CPU profile与正式吞吐分开跑，不拿插桩数据作为最终速度。
- 同一配置至少5次原始样本，报告中位数及全部样本；不只选最快一次。方差大先说明干扰，不自动多跑直到出现理想值。
- Generic/SSE2/AVX2在同一amd64上对比；Generic/NEON在同一arm64上对比。不得将不同CPU的AVX2和NEON ns/op当ISA效率排名。
- 每条结果绑定实际代码revision，不把后来的文档提交hash冒充编码构建hash；dirty构建记录diff/来源，不能写clean。
- 热核提升不等于端点提升。保留profile调用占比，解释Amdahl上限和分配/调度开销。

## 2. A00 最小基线采集

当前已有解码和编码基准，先按原语义测：

```bash
CGO_ENABLED=0 go test -run '^$' -bench '^BenchmarkDecoder_1080p_Generic$' -benchmem -count=5 ./decoder
CGO_ENABLED=0 go test -run '^$' -bench '^BenchmarkEncoderP_720p_Generic$' -benchmem -count=5 ./encoder
CGO_ENABLED=0 go test -run '^$' -bench '^BenchmarkEncoderB_720p_Generic$' -benchtime=1x -benchmem -count=5 ./encoder
```

A00时尚无dispatch，当前Generic名称是事实；dispatch落地后同命令必须带purego或强制真实Generic。保留三帧/四帧op含义，不拿ns/op直接当ns/frame。

CPU/分配profile可用标准Go测试参数，不新增第三方分析依赖：

```bash
work="$(mktemp -d)"
CGO_ENABLED=0 go test -run '^$' -bench '^BenchmarkDecoder_1080p_Generic$' -benchtime=5x -o "$work/decoder.test" -cpuprofile "$work/decoder.cpu" -memprofile "$work/decoder.mem" ./decoder
CGO_ENABLED=0 go test -run '^$' -bench '^BenchmarkEncoderB_720p_Generic$' -benchtime=1x -o "$work/encoder.test" -cpuprofile "$work/encoder.cpu" -memprofile "$work/encoder.mem" ./encoder
go tool pprof -top "$work/decoder.test" "$work/decoder.cpu"
go tool pprof -top -alloc_space "$work/decoder.test" "$work/decoder.mem"
```

上述命令在POSIX环境创建独立临时结果目录，profile和因采样保留的测试二进制均写入该目录；PowerShell用同样的显式临时路径。完成后只清理本次目录，不在仓库根留下产物。样本长度不足以稳定采样时按固定配置增加工作量并注明，不猜测热点。

`tools/encodebench` 已有 `-cpu-profile/-input/-frames/-verify/-recon/-profile/-bframes/-gop/-idr/-rc/-transform8/-weighted/-lookahead`，可复用长序列入口。当前无threads/pixel-format等完整M8参数，不能提前在命令中调用不存在的flag。

## 3. K21：kernel microbenchmark

### 3.1 落点与命名

直接后端基准放 `internal/simd/*_benchmark_test.go`，命名遵循 `BenchmarkKernel_Size_Backend`，例如拟新增 `BenchmarkSAD8_16x16_AVX2`。数据、随机数、对齐切片和期望值在计时外准备；防编译器消除结果，输出校验在不扭曲计时的位置完成。

按族至少覆盖：

- SAD8/16：16×16、8×8、分区与7×13尾部。
- SATD4x4与真实tiled batch，8/10-bit残差域。
- MC整数/单轴半像素/j/quarter，内部/边界、luma与420/422/444，byte/uint16。
- Forward4/8、IDCT4/8、DC；普通与宽域fallback。
- Quant/Dequant：典型QP26、域端点、flat/非flat、正负系数；必要缓存准备是否计时明确。
- Deblock：强/弱、水平/垂直、Span1/4、两个位深。
- ResidualAdd/Subtract：4/8×8、clip/不clip、int32宽域。

每一项同时报告ns/op、B/op、allocs/op；kernel与包装必须为0 alloc。fallback占比用单独场景测，不把Generic慢路标成AVX2。

### 3.2 结果表格式

后续填入实测；本次不创建虚构数字或空JSON验收文件。报告表字段：kernel、输入域/尺寸、backend、native CPU、ns/op五样本、中位数、B/op、allocs/op、对同机Generic比、实现/运行状态。

边界路径可能比Generic慢，必须报告；若启用某快路无收益或退化，禁用默认选择前说明测量依据，保留已实现后端直接parity，不掩盖完成范围。

## 4. K22：端点与长序列

### 4.1 工具复用决定

- 扩展既有decoder/encoder基准helper，不创建第二套编解码算法或私有重建实现。
- `tools/encodebench` 继续作为长序列编码测量器；补 `-threads`、六格式输入、field-order以及与现有Option一致的参数，所有flags在文档和测试中同步。
- decoder端长序列测量优先在decoder基准helper中实现，输出结构化运行元数据、实际后端和完整消费帧数；不要把文件I/O和FFmpeg执行计入解码kernel时间。
- 复用仓库已有PowerShell门禁脚本风格，拟新增 `scripts/verify_m8.ps1` 做矩阵编排和原始结果归档；无PowerShell环境提供相同明确Go命令。脚本只调现有工具/基准，不把性能算法藏在脚本里。
- 若encodebench为报告真实后端直接import simd诊断，K22须显式评审并在R3精确增加 `tools/encodebench`，同步规则测试和architecture；不得自动放行tools全目录。也可由codec包内基准提供后端快照，A00冻结一种方式，不能靠名字推断后端。

### 4.2 分辨率和最低场景

| 层级 | 输入与配置 | 用途 |
|---|---|---|
| 快速端点 | 320×240、640×480，至少完整I/P/B组 | 每次迭代定位回归，不承担30FPS门禁 |
| 主解码目标 | 1920×1080、8-bit420；Baseline/CAVLC和High/CABAC+B分别测；slice1/4 | 30FPS目标与通用工具覆盖，不只测PCM |
| 主编码目标 | 1280×720、8-bit420、QP26、High/CABAC、B2、GOP/IDR60、4slice、8×8、P权重、Lookahead2 | 延续M5有代表性的配置，不能更换成平坦Skip证明实时 |
| 控制组 | 原720p帧内、P、平坦Skip、B与CBR flat基准 | 分析收益来源，单独标注专门场景 |
| 扩展 | 1920×1080与3840×2160、8/10-bit、422/444、TFF/BFF、独立平面 | 资源/回退/正确性和吞吐边界，不宣称所有场景实时 |
| 码控 | 720p/1080p ABR/CBR/VBV，固定目标及buffer | 纯性能改动必须保持QP/FrameBits/VBV轨迹 |

主目标输入至少300帧固定序列，另有平移、场景切换和闪光定向输入。4K及重型扩展可明确分为较短诊断序列和完整正确性用例，不混入主目标统计。所有分辨率属于实际编码/解码尺寸，不用扩大输出缓冲伪称4K。

后端维度：同机Generic/Auto/可用指定后端；线程维度1/2/4/auto（受有效任务数裁剪）；至少分别列SIMD串行收益和并行额外收益，不能将两者混成一个倍率。

### 4.3 输入生成与固定

开发期可用FFmpeg testsrc2生成紧凑YUV420P，固定滤镜、rate、帧数、像素格式和FFmpeg版本；生成一次后按SHA复用所有后端与线程。高位深输入必须含低两位独立信息，不能只将8-bit值左移2位。

生成参考H.264时固定x264 profile/entropy/B/ref/线程/场序/矩阵等，记录完整命令与SHA；同一decoder对照使用同一码流。编码对照与x264画质测量不是M8优化前后逐比特等价的替代证据。

### 4.4 计时边界与输出检查

必须分别报告：

1. Cold：New、第一组输入、Flush、完整消费、Close；与旧三帧基准可比。
2. Steady codec-only：实例/输入已准备，计算期间包含真实Send/Receive/Flush及队列排空，不含原始输入文件I/O、哈希、FFmpeg、结果JSON写盘。
3. Stream含I/O：独立场景，含读取/写出/背压，不能与codec-only混比。

FPS=`实际完成且验证的帧数 / 总计时秒数`；漏帧、少编码或只统计成功的最快子区间均无效。

P95分清输入调用耗时、单AU输出耗时、输入到输出可见延迟。B/lookahead有固有等待，不能用Send平均耗时冒称显示延迟。延迟样本按固定帧ID对应，Flush尾组不漏计。

Peak HeapSys、GC后HeapAlloc、RSS分别命名；CPU利用率若无可靠平台测量记未测，不用goroutine数量或runtime统计猜百分比。需要RSS时优先提取复用tools/streamcheck的标准库平台采样逻辑，不能复制不一致的两套实现。

## 5. 证据产物格式

最终小型机器可读摘要建议落 `docs/gates/m8_results.json`；大YUV、profile、二进制和完整raw logs留本地结果目录。不要现在生成空文件假装实现已发生。

每个run必填字段：

- schemaVersion、runID、timestamp、phase、sourceRevision、dirty及代码来源。
- Go/OS/GOARCH/GOHOSTARCH、CGO、GOFLAGS、GOAMD64/GOARM64、GOMAXPROCS、CPU型号/核心、内存、电源/干扰说明。
- requestedBackend、每kernel实际backend及fallback理由、纯Go标签、nativeExecuted。
- 输入name/path或生成命令、SHA、帧数、显示/编码尺寸、format/bitDepth。
- 全部codec参数及Threads请求/有效值、slice数、lookahead/queue容量。
- timingBoundary、warmup、iterations、5次原始耗时、fps/mean/p95定义。
- allocatedBytes/allocs、kernel/MB AllocsPerRun、HeapSys/HeapAlloc/RSS各自来源。
- 输出Packet/Frame数、Annex-B和重建SHA、PTS/DTS/Duration摘要、QP/FrameBits/VBV轨迹哈希。
- parity/Oracle来源与真实退出码、error/skip状态、首差异信息、原始日志路径及SHA。

Resume若实现，必须核对源码、环境、配置、输入/输出完整长度和SHA，且只有验证全部通过才发布完成标记；文件存在不等于通过。没有实现Resume则明确不支持，不能盲目跳过已有文件。

## 6. K23/K24：报告与目标复审

[Roadmap](../Roadmap.md)允许“达到目标，或明确给出差距与原因”，但不是允许暗改目标场景。

- 1080p decode与720p encode分别列固定主场景的实际FPS、30FPS差距、加速比、瓶颈和资源开销。
- 若只有Baseline解码达30、High/B未达，分别写；若只有单slice不达而4slice达，明确依赖实际码流切片数。
- 720p编码高代表性配置若未达30，不能改成PCM/平坦P、关闭B/8×8/权重/lookahead或减小搜索后称达标。
- 性能目标下调须按上级文档流程说明，保留原始30FPS目标、测量与原因；不能删除失败数字或把未测格填0。
- 源码结果改变时不再属于纯性能提交；先作为独立正确性/策略变更讨论，不用新golden吞掉差异。
- 保留历史M2/M5数字和版本边界，新建M8章节，不将旧测量改写为当前HEAD。

## 7. K25：退出核验与发布

逐项建立未来 `docs/gates/m8.md`：

1. 所有宣称后端kernel原生逐元素parity，包含边界/位深/stride/别名/错误域。
2. 无汇编purego及386执行通过；六目标构建和汇编测试二进制交叉编译另列。
3. 固定端点场景达到目标或有量化差距/热点解释，不伪称实时。
4. kernel=0分配，指定MB/模式决策复用路径=0；端点分配边界与例外写清。
5. 串行/并行字节与像素/元数据/RC一致；完整race、定向多核race及强制Generic并行race通过。
6. Generic/SSE2/AVX2/NEON对照报告已写performance，跨机数字不直接作倍率。
7. 汇编路径全在internal/simd，R1/R2/R3/R5通过；需要新增自动规则时明确补上，不能假设现有depscheck会检查汇编落点或第三方依赖。
8. 55条语料、27份High10投影及编码三方Oracle保护网保留；Unsupported与analytic来源不混淆。
9. implementation-status、supported-features、architecture、encoder/stream/CLI说明及Roadmap/Implementation-Plan同步实际范围；本实施包若与落地契约不同也同步说明。
10. 按Roadmap §9.5执行受影响路径的既有Fuzz Smoke，记录真实执行数；本阶段短烟测不冒充M9全量Fuzz。

NEON缺原生runner、必需Oracle缺FFmpeg或任何必需项失败时保留未验收，完成其他可达工作，不签完整M8通过。不得以“交叉编译过”绕过硬门禁。

发布提交主题建议 `chore[release]: close milestone m8 gate`，footer标Milestone:M8并引用证据。经明确发布授权后才创建本地注释标签 `v0.9.5`；不自动push或创建远端release。远端CI未跑继续明确写未执行；本地标签不代表完整Profile认证或M9生产化完成。

## 8. 本轮实际测量入口

`scripts/verify_m8.ps1 -OutputDirectory <新的临时目录> -Phase performance`：先完成构建，再串行运行内核五样本、历史端点控制组、1080p Baseline/CAVLC和High/CABAC+B各slice1/4的300帧×五样本、720p代表High/B2/4slice/8×8/权重/Lookahead2的Generic/Auto各300帧×五样本。Generic端点使用purego二进制，不从旧benchmark名字推断后端。240p/480p/1080p/4K另列六帧诊断，不当长序列实时证据。工具不实现Resume；只在全部命令成功后写完成标记。

`tools/encodebench -measure -input <紧密YUV420P> -threads 1 ...` 保留原质量模式，新增无全流留存的连续窗口：输入Frame复用，包逐个哈希/可选写出后丢弃，恰好读满指定帧数并验证EOF。输入/包哈希、文件I/O、轨迹/内存采样不计入Send/Receive/Flush调用时间；自然GC是包含这些开销和New的**进程窗口**，不冒称codec独占或warm-steady。报告原始TotalAlloc/Mallocs/自然NumGC、实际runtime GC CPU/assist和暂停直方图，以及Go堆/RSS采样最大值。Close后单独一次主动GC只用于留存观察，不能混入自然GC差分。RSS实现从streamcheck提取共用，原PCM留存阈值不变。

`benchmarks/BenchmarkDecoder_Sequence` 从 `GOAVC_M8_INPUT` 读取经既有AUReader预组装的固定码流，要求 `GOAVC_M8_FRAMES` 和独立原生YUV的 `GOAVC_M8_YUV_SHA256`。每op包含New/逐AU Send/Receive/Release/Flush/Close；文件读取、组AU和逐帧有效像素哈希在计时外。AU数、完整显示帧数和外部SHA每op均校验；Generic由purego构建证明，逐槽Auto来源见同源码内核报告，端点不为打印诊断引入新的SIMD白名单。跨流水线基准只放benchmarks，不从decoder引入stream。该数据是cold端点而非warm-steady；六格式/场/备用表示的正确性仍由已有Oracle/规范解析测试保证。

CPU profile在吞吐测量后独立采集；同机其他本任务的构建、race、长Oracle不得与性能样本重叠。未监控外部负载，CPU利用率未测，不用RSS替代HeapSys，也不把进程GC降低推成汇编或并行提速。

