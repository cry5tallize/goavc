# A04：8/10-bit 整块均值 SSE2

2026-11-17，父提交 `8108a02`。共享BiWeightBlock的均值分支接入独立Average8/Average16槽。
8-bit使用PAVGB，uint16使用PAVGW，精确保持`(a+b+1)>>1`；16-bit完整输入域均正确，
不丢10-bit低两位。向量和8/4字节或标量尾部均不读取行外padding。
width≥4的已验证完整矩形采用SSE2，较小/不适合的几何保留Generic。

新准入检查实际len、stride和溢出；精确原地允许，部分重叠保留原逐像素顺序。
`unsafe`仅比较真实矩形的地址范围，不创建或解引用新指针。旧入口的参数、缓冲和别名语义不变。
显式权重offset仍先缩放后平均，均值判定保持原条件；非均值继续旧int64公式。

HW-M2、Ryzen5700X、Windows amd64、Go1.27.4、CGO0、GOAMD64v1、GOMAXPROCS16、Threads1。
复用A00块夹具，含固定目标重置、偏移1、非对齐stride和最小末行；各100ms×5，均0B/0alloc。
中位数ns/op如下，完整原值见[a04_samples.json](a04_samples.json)：

| 块 | 8-bit before→after | 10-bit before→after |
|---|---:|---:|
| 4×4 | 36.25→26.66 | 39.07→32.98 |
| 8×8 | 81.21→30.20 | 81.33→39.10 |
| 16×16 | 211.6→51.35 | 237.0→59.16 |
| 7×5，含尾部 | 54.95→35.11 | 59.93→38.80 |

冻结父/后版本，沿用performance §17/18输入及cold直接AU计时，交错顺序各五对；
IDR37384773→36552033ns/op（-2.2%），I/P/B1024056500→1017634000ns/op（-0.6%）。
端点视为持平，不宣称确认加速；核心各已测形状下降15.6%–75.7%，没有端点回退，故启用Auto。
IDR仍500ms、I/P/B1x，各侧独立进程count1；构建/测试/Oracle不与正式计时重叠，外部负载未监测。

无标签与实际Generic/Auto/SSE2/AVX2表测试核对完整输出、未写区、部分重叠、不同stride及
1..31宽/奇数尾部，局部栈数组0分配。`TestGuardPage_AverageBlockSSE2` 直接执行新8/16-bit
汇编，逐行保护页及65535极值通过。共享块对单样本公式、两份直接AU的FFmpeg像素、
显式加权编码三方Oracle和CGO0完整gofmt/vet/build/test/deps/manifest门禁通过。
受影响五包purego/386/race及六目标build/测试编译已在[A06集成验证](a06_deblocking_batch.md)通过，
300帧主矩阵仍在A07，Unix/arm64原生未验。

复现微基准：`CGO_ENABLED=0 go test ./internal/prediction -run '^$' -bench '^BenchmarkWeightBlocks_4To16_Auto$/(8bit|10bit)/(4x4|8x8|16x16|7x5)/bi_average$' -benchtime=100ms -count=5 -benchmem`。
定向验证：`CGO_ENABLED=0 go test ./internal/simd ./internal/prediction -run 'Test(AverageBlock|GuardPage_AverageBlock|WeightBlock)' -count=1`。
