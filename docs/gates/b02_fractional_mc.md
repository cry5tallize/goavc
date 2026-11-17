# B02：8-bit亮度qpel与色度双线性SSE2

父版本`4d67c60`。MCLumaFractional8/MCChroma8新增真实SSE2槽，decoder和encoder的
共享MC入口均可到达。4/8/12/16宽的8-bit块覆盖全部16亮度及64色度相位，其他宽度和
10-bit仍Go；边界/FieldView由原逻辑patch适配，仅读取所需halo。

水平六抽头一次处理整块，原值[-2550,10710]以int32保存，对角第二遍使用未裁剪原值，
+512后>>10再Clip1。水平/垂直半像素+16后>>5，四分之一相位复用B01组合核。
色度权重和64，8-bit加权和+32≤16352，16-bit乘加精确；非零轴才访问额外采样。
每个独立水平/垂直/对角内核一次完成完整块，不在每行或每像素跨汇编边界。

HW-M2、Go1.27.4 Windows/amd64、CGO0、GOAMD64v1、GOMAXPROCS16、Threads1。
复用BenchmarkMCBlocks固定图片及单样本Go参考预检，各100ms×5，均0B/0alloc。
代表性中位ns/op（亮度16×16、色度8×8）：

| 场景 | 亮度before→after | 色度before→after |
|---|---:|---:|
| 水平半像素内部 | 952.9→233.8 | 107.2→45.02 |
| 垂直半像素边界 | 1148→519.6 | 173.2→127.2 |
| 双轴内部 | 1876→367.7 | 153.6→44.87 |
| 四分之一内部 | 1739→321.3 | 185.6→53.26 |
| 四分之一边界 | 2157→700.7 | 251.6→139.5 |

各格快26.6%–81.5%。短IDR配对35379206→34665147ns/op（-2.0%，持平）。
首轮I/P/B两个中位数+17.0%，但冻结before在同轮由0.879至1.295秒变化，五对中三对更快；
按计划只补测一次，同配置913673900→910270900ns/op（-0.4%），未重复回退，不确认端点收益。
核心收益明确，启用已验证形状。首轮与补测全部保留在[b02_samples.json](b02_samples.json)。
端点始终复用原High输入、完整实例生命周期，I/O/组AU不计时；IDR500ms/IPB1x，独立进程
交错五对，奇偶对交换先后，构建/Oracle和计时分开，外部负载未监测。

全相位Go对照覆盖Put/Avg、最小len、非对齐stride及完整缓冲；逐行保护页覆盖4/8/12/16
新向量宽及原奇数回退、全部相位、双位深和halo边界。预测逻辑边缘/负MV/FieldView原回归、
局部数组0分配及准备B宏块0分配通过。两个直接AU FFmpeg像素Oracle、显式权重编码三方Oracle
和CGO0完整gofmt/vet/build/全库test/deps/manifest门禁通过。
purego/386/race及跨平台集成在B05收尾，10-bit插值不是本项已实现覆盖。

复现：`CGO_ENABLED=0 go test ./internal/prediction -run '^$' -bench '^BenchmarkMCBlocks$/(Luma|Chroma)/yuv420p$/(HalfHorizontalInterior|HalfVerticalBorder|HalfXY-JLumaInterior|QuarterInterior|QuarterBorder)/' -benchtime=100ms -count=5 -benchmem`。
