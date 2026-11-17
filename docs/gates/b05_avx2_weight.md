# B05a：16宽单/双向加权AVX2

基线为B04 `bce30ee`。本项只增加整块加权AVX2，不改权重、offset、QP、模式决策
或输出精度。8/10-bit存储按槽选择：CPU及OS允许AVX2、宽16且至少两行时执行AVX2；
4/8/12宽、单行继续SSE2；奇数宽、非法几何、部分重叠或宽域参数继续原int64标量。
未强制要求AVX2，purego及其他平台不变。

Spec:8.4.3.2。每批八采样，32位乘加/舍入/算术移位/offset/Clip1在同一汇编调用完成。
uint16先减32768、bias补偿32768×权重和，覆盖完整0..65535输入，不丢低两位。
规范权重[-128,128]的分子绝对值<2^25。两128-bit lane在pack前显式拼接，
仅读取八个byte/uint16，返回前VZEROUPPER，内循环及常量准备均使用VEX寄存器指令。
无新增unsafe；静态noescape调用不保留slice引用。

## 微基准与负结果

HW-M2、Ryzen5700X、Windows/amd64、Go1.27.4、CGO0、GOAMD64v1、GOMAXPROCS16。
同一二进制显式选择SSE2/AVX2；宽16高16、stride23，参数准备及完整准入检查、
lane拼接、VZEROUPPER均计时。输入准备在计时外，不原地修改源，因此无需逐op重置。
200ms×5，全部0B/0alloc。原始全部样本见[b05_avx2_samples.json](b05_avx2_samples.json)。

| 16×16 | SSE2→AVX2 ns/op中位 | 耗时下降 |
|---|---:|---:|
| byte单向 | 69.75→48.44 | 30.6% |
| byte双向 | 63.90→47.76 | 25.3% |
| uint16单向 | 67.94→49.14 | 27.7% |
| uint16双向 | 67.35→50.90 | 24.4% |

另测高1/2/4/8/16：单行19.03/18.82ns持平，不启用；至少两行已测代表形状获益。
完整准入矩阵另覆盖16×1/2/4和4×4、8×8、12×7的单/双向、两个存储类型。
两行byte单向初测21.19→20.61ns收益未确认，唯一补测21.04→19.60ns（-6.8%）后保留；
窄块和单行两种“后端”实际均SSE2，控制组差异不解释为AVX2加速。
不将ISA名称理解为全部加权尺寸均执行AVX2，也不将AVX2核收益外推为所有视频吞吐。

初始候选解析值正确但AVX/旧SSE寄存器指令混用，微基准慢约5倍。仅修复store仍慢，
再将常量MOVD也改VEX后消除回退；初始与中间失败样本完整保留在JSON，不计入最终收益。
没有为了保留AVX2而降低SSE2基线或放宽正确性断言。

## 原端点与持续吞吐

相同冻结二进制交错五次，短IDR34492480→34053506ns/op（-1.3%，持平），
32帧I/P/B首轮846724100→871514500ns/op（+2.9%）；唯一同配置补测
888082100→881235400ns/op（-0.8%，持平）。不确认端点收益，没有可重复回退。
原300帧High单slice8938428300→8740488900ns/op（-2.2%），四slice
8735372500→8721190000ns/op（-0.2%），均视为持平，不声称稳定吞吐提高。
输入及全部参数同performance.md的原主矩阵；输入SHA、二进制SHA、B/op/allocs及
全部轮次见JSON。短IDR500ms/IPB1x，长序列1x，count1独立进程，五次顺序交错。

## 验证

独立int64规范解析式覆盖两个位深、单/双向、denom0..7、负/零/极值权重及offset、
完整uint16输入、非对齐stride/地址、输入不变、未写区、原地P/B及局部数组0分配。
新AVX2入口有逐行保护页测试，窄块继续受既有SSE2保护网保护。
两个短直接AU及显式加权编码三方Oracle、CGO0完整门禁、全库purego/386/测试标签、
相关九包race、六目标构建/汇编vet/测试编译均通过。未新增NEON实现或原生执行证据。

复现：`go test ./internal/simd -run '^TestWeightBlockAVX2|^TestGuardPage_WeightBlockAVX2' -count=1`；
`go test ./internal/simd -run '^$' -bench '^BenchmarkWeightWide_16_Backend$' -benchtime=200ms -count=5 -benchmem`，
均预设CGO_ENABLED=0。强制全局后端仅用于串行测试，不与并发用例混用。
