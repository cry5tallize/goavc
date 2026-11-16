# A05：单/双向整块加权 SSE2

2026-11-17，父提交 `160a0a3`。共享WeightBlock/BiWeightBlock的非均值路径接入独立
Weight8/Weight16槽；均值继续使用A04。4样本一批的乘加、算术移位、offset和Clip1在
一次汇编调用内完成。准入为width≥4且为4的倍数、完整矩形及可安全向量化别名、
denom0..7、weight[-128,128]、offset[-128,127]、输出位深8/10；其余继续原int64公式。
独立块、精确原地、部分重叠、旧参数域和未写区域语义均保留。

## uint16不靠额外样本范围前提

8-bit零扩展后交错两源，PMADDWL得到4个32-bit和。uint16先翻转符号位成为`s-32768`，
把`32768*(w0+w1)`并入bias，完整0..65535输入域仍精确，不要求调用者新增范围扫描。
最宽分子绝对值≤16777088，offset/舍入仍在int32内。单向denom0不加round；
双向先按位深缩放offset再平均。移位和offset之后先饱和到int16，再Clip1到8/10-bit，
等价于原标量裁剪，不丢原生10-bit低位。每批只读4/8字节，末行不要求padding。

## 8/16宽实际块明显更快，奇数形状保持标量

环境同A04：HW-M2、Go1.27.4 Windows/amd64、CGO0、GOAMD64v1、GOMAXPROCS16、Threads1。
复用A00全部56格，每格100ms×5，含固定目标重置，均0B/0alloc；输入准备/核对不计时。
16×16代表性中位数ns/op：

| 模式 | 8-bit before→after | 10-bit before→after |
|---|---:|---:|
| 单向真实权重 | 356.5→81.68 | 327.3→102.8 |
| 双向显式负权重 | 427.7→77.42 | 409.6→95.04 |

初测发现奇数7×5在进入Try函数后才拒绝，额外调用使部分格变慢；已在调用层提前检查形状，
不让非向量形状跨新边界。初测负结果保留在JSON的pilot。最终7×5仍为原标量，不确认其收益；
均值作为控制组，不把其波动算作A05收益。实际4/8/16宽单/双向、恒等和原地场景均有实测收益。

冻结父/后版本交错五对，IDR42240500→41941285ns/op（-0.7%，持平），
32帧I/P/B1176028400→1113779500ns/op（-5.3%），据此启用已测形状Auto。
同一performance §17/18输入及New/Send/Receive/Release/Flush/Close边界，I/O/组AU在计时外；
IDR500ms、I/P/B1x，每侧独立进程count1，奇偶对交替先后。构建/测试/Oracle不与计时重叠，
外部负载未监测。原始值与分配数据完整见[a05_samples.json](a05_samples.json)。

## 验证

- 新汇编直接测试覆盖正负/极值权重、offset和denom0..7，解析int64公式、0/255/1023/
  32768/65535、非对齐stride、最小len、完整缓冲和未写区；单向nil第二源不访问。
- `TestGuardPage_WeightBlockSSE2`直接执行新单/双向8/16-bit内核，4/8/12/16宽逐行保护页通过。
  Try准入测试证明合格块实际进入SSE2、奇数/宽域/部分重叠被精确拒绝。
- 共享入口对原单样本公式、原地/部分重叠、其他位深及超规范参数的int64回退通过；
  局部数组实际单/双向加权0分配。SSE2和purego的相关包测试通过。
- CGO0完整gofmt/vet/build/全库test、36包deps、55条manifest，两个High直接AU FFmpeg
  像素Oracle和显式加权编码三方Oracle均通过。受影响五包purego/386/race及六目标build/测试编译
  已在[A06集成验证](a06_deblocking_batch.md)通过；300帧留A07，Unix/arm64原生未验。

微基准复现：`CGO_ENABLED=0 go test ./internal/prediction -run '^$' -bench '^BenchmarkWeightBlocks_4To16_Auto$' -benchtime=100ms -count=5 -benchmem`。
定向验证：`CGO_ENABLED=0 go test ./internal/simd ./internal/prediction -run 'Test(WeightBlock|GuardPage_WeightBlock)' -count=1`。
