# A07：解码端点与实际覆盖

报告对象为 `ff46b51`，生产算法与 `4bd8638` 相同；A06未采纳。
统一生产基线为 `v1.0.2`（`0e29cc3`），原A00数值保留在performance §19。
本次使用A00冻结二进制重新配对采样，避免把历史运行条件差异当作优化收益。
主矩阵和短端点原始样本、输入身份及分配数据见[a07_endpoint_samples.json](a07_endpoint_samples.json)。

## High Auto在原300帧输入上超过30FPS

各侧五样本中位数，ns/op均处理300显示帧；Generic使用purego：

| 输入/后端 | 统一基线ns/op | 当前ns/op | 基线→当前FPS | 耗时变化 |
|---|---:|---:|---:|---:|
| Baseline slice1 Generic | 5,078,615,500 | 5,044,467,400 | 59.07→59.47 | -0.7%，未确认收益 |
| Baseline slice1 Auto | 4,923,308,400 | 4,651,256,100 | 60.93→64.50 | -5.5% |
| Baseline slice4 Generic | 4,991,853,200 | 4,931,530,200 | 60.10→60.83 | -1.2%，未确认收益 |
| Baseline slice4 Auto | 4,946,390,100 | 4,664,224,800 | 60.65→64.32 | -5.7% |
| High slice1 Generic | 10,963,669,600 | 10,109,944,200 | 27.36→29.67 | -7.8% |
| High slice1 Auto | 11,021,750,200 | 9,624,806,700 | 27.22→31.17 | -12.7% |
| High slice4 Generic | 10,956,211,400 | 10,273,761,300 | 27.38→29.20 | -6.2% |
| High slice4 Auto | 11,375,700,600 | 9,395,844,500 | 26.37→31.93 | -17.4% |

80个正式op共24000帧全部通过独立有效像素核对。新版本High Auto单/四slice均在该固定
300帧合成序列超过30FPS，purego仍为29.67/29.20FPS；P95延迟及一般内容的长期实时能力未测。
Baseline Generic小幅变化视为持平，不据此额外采样或改分派。

短端点的整体效果也采用本次统一基线，不拼接各提交的不同运行时段：

| 短输入 | 基线ns/op | 当前ns/op | 基线→当前FPS | 耗时变化 |
|---|---:|---:|---:|---:|
| IDR，1帧/op | 46,712,942 | 36,967,347 | 21.41→27.05 | -20.9% |
| I/P/B，32帧/op | 1,137,168,100 | 988,045,900 | 28.14→32.39 | -13.1% |

达到计划短IDR≥15%、短I/P/B≥10%的耗时探索目标，300帧High也确认相同收益方向。

## 工作量与计时保持原含义

- HW-M2：Ryzen7 5700X、Windows11 10.0.26200 amd64、Go1.27.4、CGO0、GOAMD64v1、
  GOMAXPROCS16、codec Threads1；GOGC/GOMEMLIMIT/GOFLAGS未设置，采用Go默认值。
- 基线冻结二进制为 `tmp/a00/decoder-{auto,generic}.test.exe`，来源为A00的文档提交
  `1b14647`和基准补充，生产实现仍为`0e29cc3`；Go buildinfo核对版本、平台和CGO0，
  Generic明确为purego。当前二进制由`ff46b51`重新构建，生产源码未修改。
- 四份输入仍为 `tmp/m8-closeout/endpoints-au/decode-1080p-{baseline,high}-s{1,4}.264`。
  显示1920×1080、编码1920×1088、300帧，Baseline/CAVLC B0及High/CABAC B2，各slice1/4；
  QP26、keyint/min60、scenecut0、ref2、x264 threads1，其余参数和输入身份沿用§19及JSON。
- `BenchmarkDecoder_Sequence` 每op包含New、完整Send/Receive/Release、Flush和Close。
  文件I/O、组AU及逐帧原生有效像素SHA核对不计时；每op检查300AU、300显示帧和独立FFmpeg参考。
- 每格五对、各侧独立进程count1；奇数对基线先、偶数对当前先。两Profile×两slice×
  Generic/Auto，共80op、24000帧。原A00同进程count5有不同的进程级缓存预热，不混合两批样本。
- 两个短端点仍为§17/18同一high-idr/high-ipb文件，1/32帧每op；计时含实例完整生命周期，
  I/O/组AU不计时。IDR500ms、I/P/B1x，各五对。先主矩阵、后短端点，profile在全部正式计时之后。
- 构建、Oracle、fuzz和正式计时分开；未监测外部负载或CPU时钟。所有FPS由有效帧数/中位耗时计算，
  百分比为两侧五样本中位数之比，不是各阶段收益之和。

## 实际消费链与SSE2准入

下表是调用链和准入审计，结合真实槽诊断及原有parity/保护页证据，不是指令命中率统计。

| 项 | 当前实际消费者 | 可向量化范围 | 回退及缺口 |
|---|---|---|---|
| A01亮度类整边 | decoder的Prepared Luma8 | 8-bit、16线、完整p2/q2、合法水平/垂直步长；444亮度类分量及MBAFF纯帧MB可到达 | 10-bit和混合/场逐线边保持原段路径；encoder亮度仍调用分段LumaEdge，未接整边槽 |
| A02色度整边 | decoder与encoder本地重建 | 8-bit420逐行、8线、完整p1/q1及合法步长 | 10-bit、422/444、field/MBAFF不进入此槽 |
| A03 P权重快路 | decoder applyPWeights | 按正确分量/denom判断恒等跳过，非恒等调用共享WeightBlock | 六格式、field ref及独立平面索引保留；encoder自己的逐样本权重循环未迁移 |
| A04均值 | decoder B块BiWeightBlock均值分支 | 8/16-bit存储，width≥4、完整矩形、独立缓冲或精确原地；尾部精确处理 | 小块/不适合几何/部分重叠保持原顺序；encoder仍逐样本均值 |
| A05单/双向加权 | decoder P非恒等与B块权重 | width≥4且4倍数、denom0..7、weight[-128,128]、offset[-128,127]、输出8/10-bit；完整uint16输入域精确 | 其他宽度/参数/位深/部分重叠继续int64；encoder逐样本权重不使用新块槽 |
| A06 bS批量 | 未采纳 | 两候选正确性通过，第二候选单边快7.9%–25.1% | I/P/B两轮慢5.2%/4.5%，生产bS保持原实现 |

当前Auto新增槽FilterLumaEdge、FilterChromaEdge、Average8/16、Weight8/16为SSE2；
FilterDelta、IDCT4/8、Forward8、逆量化乘积和残差仍按原实测选择Generic。
SAD及宽16整数复制仍可选AVX2；分数像素MC仍Generic，未新增AVX2或NEON内核。
槽诊断表示选择能力，上述调用链与准入决定实际执行范围。

## 新热点仍是去块与CABAC残差，不再是逐像素权重

正式计时结束后单独采样当前Auto：IDR为3.52秒CPU，I/P/B为3.80秒CPU，含benchmark预热。
下面均为累计采样比例，父子节点重叠，不能相加，也不作为FPS样本：

| 节点 | IDR | I/P/B |
|---|---:|---:|
| 完整去块链 | 30.68% | 22.11% |
| 亮度整边SSE2 | 17.90% | 2.63% |
| 色度整边SSE2 | 2.27% | 0.26% |
| edgeStrengths | 1.14% | 16.84% |
| CABAC residual | 21.02% | 16.05% |
| CABAC Bin，含renormalize | 17.33% | 12.37% |
| CABAC renormalize | 10.23% | 9.47% |
| BiWeightBlock8 | — | 3.42% |

decodeCABACSlice累计为51.14%/64.21%，其中包含预测、重建，不能解释为纯算术熵解码成本。
I/P/B的亮度/色度MC共享标量入口分别为2.63%/2.11%，本短输入尚不足以把插值列为首要投入。
完整profile和符号表保存在 `tmp/a07/`，关键数值随JSON归档。

后续先补齐A01具名分派/局部数组门禁；下一轮性能探索优先B04的CABAC残差、读位缓存与
固定扫描批量，先拆独占成本再实施。bS仍值得研究，但不重新启用A06已退化配置；
分数像素SIMD的优先级由代表性运动负载决定。

## A01零分配证据需要收窄

A07使用Go1.27.4额外实测AllocsPerRun100，数组在closure内部创建：

- `PrepareEdge(32,0,0,8).Luma8`：512字节局部数组、q0=4、lineStep32/across1、
  bS{1,2,3,4}，实测**1 alloc/op**。
- `Chroma4208`：256字节局部数组、q0=4、lineStep24/across1、同bS，实测**0 alloc/op**。

逃逸分析确认Prepared Luma8的data经函数值分派保守逃逸；新色度具名平台包装不逃逸。
A01已有固定目标复用基准0B/0alloc及直接汇编的栈数组0分配仍有效，但不证明
“调用包装层并在每次closure内新建目标数组”也0分配。实际帧平面已经在堆上，
端点复用既有堆平面；局部数组的保守逃逸只在调用方新建该数组时体现为新分配。
探针和日志在 `tmp/a07/allocation_probe_test.go`、`allocation-probe.txt`、`escape.txt`，
临时测试已删除，生产代码未改。A01局部栈缓冲零分配门禁仍需后续修正具名分派并验证。

## 修正A01短端点的来源归因

历史A01 JSON的generic_before中位数为51627950/1281560700ns/op，
与A00归档的short-auto中位45738950/1048345500ns/op不一致。现存材料不能证明
这两组before是“A00直接复用”，因此撤回该来源归因和据此单独归因A01的端点百分比。
历史JSON原值保留；当前整轮收益只使用本次统一v1.0.2基线的交错配对结果。

## 验证范围

主矩阵每个op都校验独立FFmpeg原生像素参考，短输入另外执行当前版本FFmpeg逐像素Oracle。
复用A06最终接受源码的五包purego/386/race、六目标build/SIMD测试编译、六格式/场/独立平面、
错误/所有权、共享内核parity/保护页及编码三方Oracle证据；A07没有生产代码变化，不重复整套门禁。

本次另跑 `CGO_ENABLED=0 go test ./decoder -run '^$' -fuzz '^FuzzDecoder_NoPanic$' -fuzztime=5s -parallel=2`：
4个初始seed，日志报告6次exec、0个new interesting，6.028s后PASS。仅为有界烟测，不能说明
长期稳定性或新增SIMD路径的随机覆盖充分。CABAC专用fuzz留在B04，未执行Unix/arm64原生或8小时长稳。
Profile功能状态不提升，完整M8/M9门禁不因这次端点报告而自动关闭。

正式命令、全部原始数值与输入SHA见JSON；本地编排在 `tmp/a07/run.ps1`。
