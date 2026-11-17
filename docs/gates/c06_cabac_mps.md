# C06：CABAC MPS 热分支评估（未采纳）

基线C04 `1136482`。本次评估一个窄切片：合法MPS分支已证明offset<range，若减去LPS
后range仍>=256，直接返回，不再调用renormalize。任意Context的State/MPS校验、ready、
终止和粘性错误仍保留；其他分支保持原算术。没有改Reader位置契约或新增预读。

**结论：不接入生产。** High10补测慢6.3%，留出方向不一致，未通过约3%以上长端点
稳定改善的采纳门槛。生产`decoder.go`与父版本完全一致；候选只保留在
`mps_path_test.go`用于今后状态对照，不把测试中的候选称作已启用优化。
C06的可信上下文/残差循环结构专化尚未完整实施，本项只关闭这个MPS候选的评估。

## 性能证据

Ryzen7 5700X、Windows/amd64、Go1.27.4、CGO0、GOAMD64v1、GOMAXPROCS16、Threads1。
独立进程交错五对，奇偶轮交换先后，IDR500ms/High10及留出Sequence1x。
计时包含完整实例生命周期，I/O、组AU和独立FFmpeg像素哈希在计时外。
High10沿用B05单slice原300帧文件，三个60帧合成留出沿用C00身份；不冒充自然实拍。
正式计时没有自己的构建、Oracle、profile或race并行；外部负载未监测。

| 输入/轮次 | before→candidate ns/op中位 | 两侧中位变化 | 逐对变化中位 |
|---|---:|---:|---:|
| IDR首轮 | 39840443→34476794 | -13.5% | -10.9% |
| High10首轮 | 8818602200→8886794300 | +0.8% | +0.1% |
| IDR唯一补测 | 35094638→35841207 | +2.1% | +2.1% |
| High10唯一补测 | 9614031000→10222903400 | +6.3% | +3.3% |
| desktop-static | 803672500→838956700 | +4.4% | +2.3% |
| texture-noise | 12376100900→12157947900 | -1.8% | -4.3% |
| lowrate-skip | 1001407700→919708200 | -8.2% | -4.2% |

首轮IDR明显收益未在补测重复；不反复采样到好数字。全部原始耗时/B/op/allocs、
二进制SHA及输入参数见[c06_samples.json](c06_samples.json)。

200ms×5诊断核：归一化MPS 8.128→11.51ns，0B/0alloc；prepared残差15/16/64项
483.6→490.1、562→480、2224→1873ns，均64B/1alloc（每op复制Reader引起）。
这些顺序微基准不是独占成本profile，不能用个别残差核收益推翻长端点门禁。
B04固定4/15/16启用、8/64通用的选择保持不变；没有删scan/上下文校验。

## 回归与边界

候选逐状态对照生产Bin：全部256..511区间、64状态、两种MPS、三种offset及空/零/全一尾部，
比较bin、context、range/offset、lastBit、终止、错误、逻辑位数和后续Peek。
另覆盖混合Bin/Bypass/Terminate的全部0..24剩余位，非法/空Context、错误后调用及终止后调用。
原逐位trace、全部截断残差类别、帧/场与PCM重启Oracle继续保留。

候选没有改变生产输出；测量版High10和留出每次完整像素哈希均通过。
最终接受树恢复父版本算术，不把未采纳候选的计时当作当前默认版本速度。
最终接受树通过CGO0完整gofmt/vet/build/非缓存全库test、36包依赖及55条manifest，
bitstream/CABAC/decoder的purego/386、bitstream/CABAC/DPB/decoder的CGO1 race。
两个短AU独立FFmpeg及加权编码三方Oracle通过；未新增汇编，未重跑六目标编译。
算术/残差fuzz各5秒、parallel2，211073/197168次exec通过，测试的是最终生产入口，
不是候选专项fuzz；候选正确性由上述差分测试证明。不把短烟测称为长期稳定性。
验证日志哈希见[c06_validation.json](c06_validation.json)，不据此提升Profile状态。

复现CGO0：`go test ./internal/entropy/cabac -run 'TestDecoder_MPS' -count=1`。
候选仅测试可调用，`BenchmarkBin_NormalizedMPS_Compare`可同二进制对照；
真实端点测量用当时冻结的before/after二进制，最终生产树不会自动切换候选。
