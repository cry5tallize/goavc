# B03：共享4×4/8×8 IDCT＋Add融合

父版本`d0998c9`。decoder和encoder重建调用相同picture.Reconstruct4x4/8x8，
默认Generic逆变换使用唯一共享的int64行/列算术，列结果直接Add预测及Clip1，省去整块
残差数组往返。旧IDCT4/8入口仍可用；显式非Generic后端及不适合的缓冲保留旧路径。

4×4覆盖完整int32系数。8×8只对|coeff|≤2^24的域融合：单遍保守增益<8，两遍/64之后
残差安全落在int32内；宽域仍走原IDCT8x8溢出检查及Add。没有缩窄中间精度，也不清零输入。
反量化及其错误先完成，decoder原零/DC分派保留；完整矩形不满足时沿用原逐行错误前缀。
本项没有把反量化合并，也没有新增ISA槽。

HW-M2、Windows/amd64、Go1.27.4、CGO0、GOAMD64v1、GOMAXPROCS16、Threads1。
固定预测目标重置＋完整IDCT/Add，各100ms×5，stride23、偏移1，均0B/0alloc。

| 块/系数域 | 8-bit before→after ns/op | 10-bit before→after ns/op |
|---|---:|---:|
| 4×4普通 | 83.31→58.43 | 79.20→56.80 |
| 4×4极值 | 79.77→58.84 | 78.84→58.65 |
| 8×8普通 | 526.9→466.2 | 527.4→447.4 |
| 8×8宽域回退 | 526.3→523.8 | 527.8→510.5 |

4×4快25.6%–29.9%，8×8普通域快11.5%–15.2%；宽域回退不宣称收益。
原短输入交错五对IDR37523036→37403164ns/op（-0.3%），32帧I/P/B972464300→
970430000ns/op（-0.2%），持平，不把局部收益外推为已确认吞吐改善。
全部原始值见[b03_samples.json](b03_samples.json)。每对独立进程count1，IDR500ms/IPB1x，
奇偶对交换顺序，完整实例生命周期计时，I/O/组AU除外；外部负载未监测。

原变换＋Add对照、int32极值/正负舍入/双位深/完整缓冲、输入系数不变、局部数组0分配
及短缓冲错误写入前缀通过；既有规范解析向量、dequant错误/零DC回归保留。
两个直接AU FFmpeg像素、编码三方Oracle、CGO0完整gofmt/vet/build/全库test/deps/manifest
门禁通过。purego/386/race及六目标集成在B05收尾，不将其预记为通过。

复现：`CGO_ENABLED=0 go test ./internal/picture -run '^$' -bench '^BenchmarkReconstruct_4To8_Auto$' -benchtime=100ms -count=5 -benchmem`。
