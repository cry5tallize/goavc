# B01：双向MC的Put/Avg融合

父版本`ba3ea36`。普通8-bit420逐行、双参考、无显式权重且实际为均值时，第一路直接Put
最终平面，第二路插值完成规范Clip/舍入后直接Avg目标，不再创建两份预测块和第三遍合成。
其他位深/格式、field/MBAFF、显式或非均值隐式权重保留原分支。

亮度/色度Put和Avg共用同一插值及边缘适配。新MCBlend8 SSE2以独立stride完成相位组合
和目标均值，两次PAVGB对应两次独立舍入；不平均未裁剪六抽头或未舍入双线性值。
亮度Put的四分之一相位末端也复用此组合核；六抽头本身本阶段仍Go。

HW-M2、Ryzen5700X、Windows/amd64、Go1.27.4、CGO0、GOAMD64v1、GOMAXPROCS16、Threads1。
完整双向MC三平面各100ms×5；第一路每op覆盖目标，不需额外reset，输入固定，均0B/0alloc。

| 完整16×16双向预测 | before ns/op | after ns/op | 耗时变化 |
|---|---:|---:|---:|
| 整数位置 | 434.1 | 355.4 | -18.1% |
| qpel | 5941 | 4732 | -20.4% |
| 逻辑边界 | 5901 | 5243 | -11.2% |

固定原短输入交错五对：IDR34467669→34068653ns/op（-1.2%），
32帧I/P/B905349900→910529400ns/op（+0.6%），视为持平；不确认整体端点收益。
核心三格收益明确且无端点回退，启用上述条件。原始值见[b01_samples.json](b01_samples.json)。
计时仍为New/Send/Receive/Release/Flush/Close，I/O/组AU不计时；IDR500ms、IPB1x，
各侧独立进程count1、奇偶对交替顺序，构建/Oracle与计时分开，外部负载未监测。

原两预测块完整缓冲对照覆盖4/8/16块、全部亮度/色度相位、负MV、边缘及未写区域；
其他格式/10-bit回归及准备后B宏块0分配通过。新组合核直接逐行保护页覆盖1/3/4/7/8/
15/16/17宽、nil第二源和双层舍入。相关parity、两个直接AU FFmpeg像素Oracle、显式权重
编码三方Oracle及CGO0完整gofmt/vet/build/test/deps/manifest门禁通过。
集成purego/386/race/跨平台在B05后集中执行，不计作本项已执行。

复现：`CGO_ENABLED=0 go test ./decoder -run '^$' -bench '^BenchmarkBiMC_16_Auto$' -benchtime=100ms -count=5 -benchmem`。
