# B05 本机范围收尾与B01–B05集成

## 验收范围

2026-11-17，用户明确确认：“本机完成了就是完成了，arm64没有，但也能算完成”。
本次B05按Windows/amd64可执行范围关闭：AVX2加权、高位深MC、实测分派和本轮集成验证。
无原生arm64 runner，不新增未经原生验证的NEON默认内核；已有NEON实现保持编译交付，
**未原生验证、无NEON性能数字**。六目标交叉编译不替代原生执行。
此范围确认不关闭完整M8、Profile认证或A01局部数组1alloc/op的既有缺口。

实现、数值范围与定向性能分列为[B05a AVX2加权](b05_avx2_weight.md)、
[B05b高位深MC](b05_high_depth_mc.md)。全部样本、二进制SHA和输入身份见
[b05_samples.json](b05_samples.json)，不相加不同阶段的百分比。

## 可复现性与归因

父基线B04 `bce30ee`，固定三个二进制：before=B04、avx2=B04仅加权改动、
after=加权及高位深MC。avx2在独立父版本工作树构建，未混入高位深MC。
8-bit输入五次交错三方测量，按奇偶轮逆转顺序；High10为avx2/after五对。
各进程count1，短IDR500ms/IPB1x，长序列1x，长测每op300帧，codec Threads1。
完整New/Send/Receive/Release/Flush/Close计时；文件读取、AU组装及独立像素哈希核对不计时。
测量期间没有本次构建、Oracle、profile或race；外部负载未监测。

原8-bit High单/四slice输入完全复用A07/M8的300帧主输入，不重新生成。
新增High10单/四slice仍为testsrc2 1920×1080@30、300帧、veryfast、QP26、GOP60、
B2、ref2、CABAC、8×8、weightp1、b-adapt0、b-pyramid none、scenecut0、threads1。
独立FFmpeg输出原生yuv420p10le，逐份长度1,866,240,000字节，并固定SHA；
新增本地长输入不纳入静态语料清单，不冒充登记语料或原8-bit历史基线。

## 端点实测结论

| 固定场景 | B04→仅AVX2→最终 ns/op中位 | 最终耗时变化 |
|---|---:|---:|
| 短IDR（1帧） | 34492480→34053506→34339081 | -0.4%，持平 |
| 短I/P/B（32帧）首轮 | 846724100→871514500→897758900 | +6.0%，保留 |
| 短I/P/B唯一补测 | 888082100→881235400→891356700 | +0.4%，持平 |
| High单slice（300帧） | 8938428300→8740488900→8775545100 | -1.8%，持平 |
| High四slice（300帧） | 8735372500→8721190000→8709679300 | -0.3%，持平 |

首轮短I/P/B变化没有在唯一补测重复，不宣称吞吐提高或掩盖初测回退。
最终High单/四slice为34.19/34.44FPS，只描述原固定输入，不代表任意内容实时保证。
高位深对比独立于8-bit：High10单slice仅AVX2→最终12367237400→12167316300ns/op
（-1.6%，24.66FPS），四slice12375855700→12008998200（-3.0%，24.98FPS）。
收益均不超过约3%，端点总体仍按未确认稳定收益记录；10-bit此输入未达到30FPS。
内核明显改善与端点持平可同时成立，不用核加速百分比外推整体吞吐。

## 工程验证结果

CGO_ENABLED=0：gofmt、全库vet/build/非缓存test、36包依赖检查、55条manifest通过。
全库purego、386和goavc_simdtest本轮重新执行通过，不使用A轮旧证据替代。
六目标Windows/Linux/macOS×amd64/arm64均build、SIMD汇编vet及带测试标签的测试二进制
交叉编译通过；原生动态执行仅Windows amd64/386与purego。

相关race按工具链例外CGO_ENABLED=1执行：bitstream、CABAC、SIMD、prediction、picture、
transform、filter、decoder、encoder九包均通过；之后恢复CGO0。没有新增内部并行。
新汇编访问边界由逐行保护页与parity另行验证，不能用race代替。

两个短直接AU FFmpeg像素Oracle和显式P加权编码三方Oracle通过；全库测试包含原生
High10/422/444动态Oracle、MBAFF六格式/场序、独立平面投影及既有规范解析向量。
独立平面投影不是原始封装互操作认证；存在可选长测跳过，不宣称所有可选门禁均通过。

decoder及B重排fuzz各5秒、parallel2，通过，日志分别6和1414次exec；
B04算术/残差5秒烟测证据继续有效（295027/303863次exec），均不是长期稳定性证明。
本次不重跑8小时混合输入长稳、不发布、不推送、不打标签。

## 复现入口

常规验证：仓库AGENTS.md列出的CGO0命令；集成额外使用
`go test -tags=purego -count=1 ./...`、`GOARCH=386 go test -count=1 ./...`、
`go test -tags=goavc_simdtest -count=1 ./...`及上述九包race。
端点继续使用`BenchmarkDecoder_DirectAU_Auto`和`BenchmarkDecoder_Sequence`，
输入环境变量与计时定义分别见`benchmarks/decode_au_test.go`、`benchmarks/decoder_test.go`。
本地日志与复用脚本位于`tmp/b05/`，持久化数值以JSON和本页为准。
