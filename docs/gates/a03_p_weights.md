# A03：P 预测逐分量恒等跳过与块级加权

2026-11-17，父提交 `414059d`。`applyPWeights` 在选择各分量的denom和独立平面索引后，
仅跳过 `weight=1<<denom && offset=0` 的分量；其余复用共享 `WeightBlock8/16` 原地处理。
场ref索引除2、420/422/444几何、独立平面使用亮度参数和10-bit offset缩放保持原规则。
未修改单样本或块加权公式，没有SIMD或Profile支持范围变化。

`TestPWeights_ComponentIdentityAndSampleParity` 逐像素对照原逐样本路径，覆盖六格式、
场/帧ref、独立444三平面、denom0/2/5/7和四种分量权重组合，核对完整缓冲及未写区。
CGO=0的gofmt/vet/build/全库test、36包依赖和55条语料门禁通过；
两个High直接AU FFmpeg像素Oracle及显式加权编码三方Oracle通过。

HW-M2/Ryzen5700X、Windows amd64、Go1.27.4、CGO0、GOAMD64v1、GOMAXPROCS16、
codec Threads1。微基准各100ms×5，包含三平面固定重置和真实`applyPWeights`，
不含图像/权重准备，均0B/op、0allocs/op。代表性中位数（ns/op）：

| 分量权重 | 8-bit before→after | 10-bit before→after |
|---|---:|---:|
| 全恒等 | 1323→440.7 | 2005→673.5 |
| 仅亮度非恒等 | 1332→801.1 | 1918→996.1 |
| 仅色度非恒等 | 1312→639.6 | 1957→867.5 |
| 全部非恒等 | 1310→971.8 | 1862→1180 |

冻结父版本及修改后两个二进制，交错顺序各五对，沿用performance §17/18同一输入与计时边界。
32帧I/P/B中位1102663000→1047244000ns/op，下降5.0%。IDR控制组首次+3.8%，
按计划只补测一次，43128483→42888177ns/op（-0.6%），未出现可重复回退，不确认IDR收益。
全部样本和分配数据见[a03_samples.json](a03_samples.json)，初测与补测均保留。
本任务的构建/Oracle/常规测试未与正式计时重叠，外部负载未监测。

复现：`CGO_ENABLED=0 go test ./decoder -run '^$' -bench '^BenchmarkPWeights_16_Auto$' -benchtime=100ms -count=5 -benchmem`。
端点仍包含New/Send/Receive/Release/Flush/Close，不含I/O或组AU；IDR500ms、I/P/B1x，
每对奇数before先、偶数after先，各侧独立进程count1。300帧主矩阵留到A07；
本轮受影响五包purego/386/race及六目标build/测试编译已在[A06集成验证](a06_deblocking_batch.md)通过；
不扩大Unix/arm64原生证据，300帧仍留A07。
