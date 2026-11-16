# A02：逐行 8-bit 420 色度整边 SSE2

2026-11-17，父基线为 A01 `a0f3c0c14834f0dbfe2703fdfaded21fa54eab43`。
**结论：水平和垂直整边采用 SSE2 Auto。** 活跃色度边微基准耗时下降约40%–61%；
交错配对测量中，合成整帧去块下降10.2%–14.1%，短 IDR 下降5.7%，短 I/P/B 基本持平。
原始 before、最终 after、配对和初测样本完整保存在
[a02_chroma_edge_samples.json](a02_chroma_edge_samples.json)，不以 A00 的旧端点值代替本项 before。

## 实现与调用范围

- 从 H.264 8.7.2.4 和仓库已有标量公式自主推导，一次调用处理四段、八条色度线。
  八个16-bit lane 完成严格阈值比较、强弱候选、`tc0+1`、带符号移位和像素裁剪。
  弱滤波分子范围为[-1271,1279]，强滤波加权和为[2,1022]，16-bit 有符号运算精确。
- 四组 bS 各覆盖两条线，bS=0和阈值拒绝线不写回。所有候选使用原始 p1/p0/q0/q1；
  只修改 p0/q0。垂直每行精确读取4字节、水平每行读取8字节，不增加 padding 要求。
- 新增独立 `FilterChromaEdge` 后端槽，`FilterDelta` 继续 Generic，亮度槽保持 A01 SSE2。
  `EdgeParameters.Chroma4208` 检查实际 len、完整邻域和步长；除法先于乘法，避免检查溢出。
  短缓冲及未接入几何使用原公式的逐段精确回退，旧 `Chroma8`/`Chroma16` 入口保持原契约。
- decoder 明确限定 `subW=subH=2`、8-bit、非 field/MBAFF；encoder 本地重建按相同条件调用
  同一共享内核。宏块光栅、先垂直后水平、分量和边顺序不变；逐侧色度 QP 映射后平均不变。
  10-bit、422/444、场/MBAFF继续原路径，Profile和并发能力不变。
- 平台包装使用具名调用保留 `noescape` 信息，避免函数值分派令局部数组逃逸；Generic回退
  仍调用共享标量算术。未引入 `unsafe` 或运行时依赖。

## 同输入、五样本测量

环境为 HW-M2、Ryzen 7 5700X、Windows 11 10.0.26200 amd64、Go1.27.4，
CGO=0、GOAMD64=v1、GOMAXPROCS=16、codec Threads=1。构建、Oracle、race和正式计时分开；
本任务未并跑其他测量任务，外部负载和CPU时钟未监测。

before 二进制在生产修改前编译：A01加一个仅转调旧 `Chroma8` 的 `Chroma4208` 包装，
基准提前调用该包装。after 使用真实新入口。两侧夹具、有效像素、每 op 重置和计时边界一致。
初测遇到 asmdecl 对数组命名直接加载的 vet 报错；改为通过数组地址加载并复用强度掩码，
重新验证、编译和测量最终版本。初测及其配对值保留在 JSON 的 `pilot`，不混入最终结论。

### 色度整边耗时下降，零阈值没有 SIMD 工作

复用 `BenchmarkPreparedChromaEdge_8_Auto`；stride=29、q0=121，
每 op 包含696字节固定目标重置及一次完整边处理，PrepareEdge和夹具核对不计时。
8/10-bit × 两方向 × 七场景，共28格，各五样本；均为 **0 B/op、0 allocs/op**。
10-bit作为原路径控制组保留在 JSON，下表只报告8-bit，单位 ns/op，中位数：

| 场景 | 垂直 before → after | 变化 | 水平 before → after | 变化 |
|---|---:|---:|---:|---:|
| 弱全通过 | 90.76 → 38.10 | -58.0% | 92.53 → 37.33 | -59.7% |
| 强全通过 | 90.53 → 37.44 | -58.6% | 91.77 → 36.26 | -60.5% |
| 混合 bS | 74.79 → 37.00 | -50.5% | 76.13 → 38.52 | -49.4% |
| 全拒绝 | 47.06 → 28.29 | -39.9% | 47.47 → 28.52 | -39.9% |
| 部分线通过 | 66.58 → 34.97 | -47.5% | 71.50 → 35.38 | -50.5% |
| 全零 bS | 16.49 → 15.16 | -8.1% | 16.17 → 15.24 | -5.8% |
| 零阈值 | 14.52 → 14.37 | -1.0% | 14.30 → 14.35 | +0.3% |

全零bS及零阈值直接返回，不执行像素内核；零阈值的小幅差异视为持平。

### 端点采用交错配对判断，保留批间漂移

首次 before 与最终 after 的顺序批测，短 IDR 为38.950→43.825ms，I/P/B为
1094.570→1343.893ms；合成 intra/inter 去块为15.146→15.286ms、8.948→11.087ms。
冻结的同一个 before 二进制在后续配对中，I/P/B中位数已变为1322.296ms，
比原 before 慢约20.8%。这表明批间运行条件变化明显；原因未监测，不能把上述批间差归因于 A02。

因此对最终候选进行一次交错配对补测：每格五对，奇数对 before→after、偶数对反向，
每侧为独立进程、`count=1`；IDR仍500ms、I/P/B仍1x、合成整帧仍300ms。
下表各侧五样本独立取中位数，变化为中位数之比：

| 配对场景 | before ns/op | after ns/op | 变化 |
|---|---:|---:|---:|
| 真实去块链，合成 intra 帧 | 18,745,395 | 16,110,150 | -14.1% |
| 真实去块链，合成 inter 帧 | 12,137,459 | 10,897,408 | -10.2% |
| High IDR，1帧/op，直接 AU | 47,675,850 | 44,952,885 | -5.7% |
| High I/P/B，32帧/op，直接 AU | 1,322,296,300 | 1,321,616,500 | -0.1% |

I/P/B收益未确认，配对未出现可重复回退；整边两方向和合成完整去块链收益明确，故启用两方向 Auto。
独立进程1x的冷池分配不与同进程count=5混合：端点完整 B/op/allocs/op 均在 JSON。
整帧配对均0allocs/op；其中一格实测3B/op，其余0B/op，保留原值。
包装层的局部栈缓冲独立断言0分配，逃逸分析为 `data does not escape`。

合成帧复用既有 `BenchmarkDeblock_1080p_Auto` 的 intra=true/false 两格，编码尺寸1920×1088；
包含三平面固定重置、QP/bS及实际 `deblockPlane`，不含解码。短端点沿用
[performance.md §17/18](../performance.md) 的同一份 high-idr/high-ipb 文件和生成配置；
包含 New/Send/Receive/Release/Flush/Close，不含文件I/O和组AU。输入身份及原始命令见 JSON。
300帧主矩阵按计划留到A07，这些短端点不改变持续实时结论。

## 正确性与工程验收

- 新 SSE2 内核：两方向、bS0..4/混合、严格阈值临界、tc0=0、负数舍入、0/255裁剪、
  非对齐地址/stride和最小len的解析值、确定性随机及全缓冲标量差分通过。
  `TestGuardPage_Chroma8EdgeSSE2` 直接执行新内核，逐行邻域紧贴保护页，强/弱/混合及全零均通过。
- `TestChroma420Edge_FallbackAndAllocations` 核对短邻域、未接入步长/位深和溢出检查，
  数组在 AllocsPerRun closure 内创建；可派发及回退路径均0分配。
  `goavc_simdtest` 只切换新色度槽，核对亮度和FilterDelta槽不变、恢复正确及整边输出一致。
- 最终两个直接AU输入与FFmpeg8.0逐像素一致。编码的 DecisionCorpus、InterSequence、
  BFrameReorder、CABACSequence、Transform8x8、WeightedPrediction Oracle通过，
  本地重建/自研解码/FFmpeg像素一致；全库常规测试也包含六格式与场/MBAFF回归。
- CGO=0：gofmt检查无输出，vet、build、全库test、36包依赖检查和55条语料清单校验通过；
  未受影响包复用有效缓存。受影响simd/filter/decoder/encoder的purego和386回归通过。
- purego+测试标签首次发现A01强制SSE2用例在后端不可用时直接失败，已修正该用例和基准为
  明确跳过，filter复测通过；simd/decoder/encoder先前通过结果仍有效。跳过不计作SSE2执行证据。
- 受影响四包race通过，仅此开发期检查启用CGO=1。Windows/Linux/macOS × amd64/arm64
  六组合全仓build及SIMD测试二进制编译通过；原生执行证据为Windows amd64/386。
  未新增Unix/arm64原生执行、全库race、fuzz或长稳证据。

常规门禁使用仓库既有命令。定向复现如下，运行性能命令前结束其他构建与验证任务：

```powershell
$env:CGO_ENABLED='0'; $env:GOAMD64='v1'; $env:GOMAXPROCS='16'
go test -tags goavc_simdtest ./internal/simd ./internal/filter -count=1
go test ./decoder -run '^TestDeblock_PreparedScheduleParity$' -count=1
go test ./internal/filter -run '^$' -bench '^BenchmarkPreparedChromaEdge_8_Auto$' -benchtime=150ms -count=5 -benchmem
go test ./decoder -run '^$' -bench '^BenchmarkDeblock_1080p_Auto$' -benchtime=300ms -count=5 -benchmem
```

本地原日志与采样编排保存在 `tmp/a02/`；可提交的完整数值证据为上述JSON。
本项完成后下一项为A03，不继续修改CABAC、运动预测、bS计算或其他SIMD内核。
