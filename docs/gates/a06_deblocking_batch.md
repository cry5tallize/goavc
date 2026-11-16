# A06：bS批量与局部复用评估——未采纳

2026-11-17，生产父版本 `4bd8638`。**两轮端点配对确认回退，撤回候选实现。**
当前生产bS、运动网格接口和帧/场路径保持A05；本提交仅保留回归、基准及完整负结果。
下一步A07应对比实际已采纳的A03–A05，不把本项微基准收益计入生产。

## 两个候选回答了什么

1. 第一候选按整边复制四段的两列表MV/ref/可用性快照，复用MB地址和8×8非零映射。
   稀疏残差B格反而慢10.6%–23.2%，因此停止使用整边预取配置。
2. 第二候选只为非零系数未命中的段读取运动，预计算p/q线性下标、复用两侧四段非零值；
   新GetLinear保持valid、sliceStart、mapped owners及不可用块零返回语义，场/MBAFF继续原路径。
   单边基准覆盖准备成本，P/B、残差和transform8八格全部更快，但真实短端点重复变慢。

两候选均对照原bSAtBlocks/调度，通过参考交换、相同/空参考、MV阈值、变换8非零映射、
mapped/slice权限、完整像素及FFmpeg Oracle检查；没有通过改输出获得更快数字。
第二候选在本地保留为 `tmp/a06/rejected_candidate.patch`，未留下生产开关、未使用的新API或缓存。

## 单边更快并未变成端点收益

HW-M2/Ryzen5700X、Windows amd64、Go1.27.4、CGO0、GOAMD64v1、GOMAXPROCS16、Threads1。
before是生产修改前冻结的A05二进制；候选二是下面的after。两侧固定相同1080p元数据，
包含每条边四段准备、普通/8×8非零判断和实际身份/MV匹配，各100ms×5，中位ns/op：

| 整边bS | 垂直 before→候选 | 水平 before→候选 |
|---|---:|---:|
| P，零残差 | 70.07→52.47 | 69.13→53.93 |
| B，零残差 | 134.8→110.9 | 142.9→119.7 |
| B，混合非零 | 113.7→97.23 | 118.3→103.2 |
| B，transform8 | 91.74→70.92 | 80.76→74.37 |

既有真实去块链合成基准含三平面固定重置，intra为13325412→13382638ns/op（+0.4%），
inter为10934020→8646129ns/op（-20.9%）。这不是解码吞吐。

直接AU复用performance §17/18的同一份High IDR和32帧I/P/B，包含
New/Send/Receive/Release/Flush/Close，I/O/组AU不计时。奇数对before先、偶数对candidate先，
各侧独立进程count1，IDR500ms、I/P/B1x，每轮五对：

| 场景 | before ns/op | 候选 ns/op | 变化 |
|---|---:|---:|---:|
| IDR | 37,424,467 | 37,305,693 | -0.3%，持平 |
| I/P/B首轮 | 968,998,100 | 1,019,137,300 | +5.2% |
| I/P/B唯一一次补测 | 933,177,600 | 974,759,600 | +4.5% |

变化由两侧五样本中位数之比计算；首次五对均变慢，补测五对中四对变慢。
核心收益不能抵消已确认的端点退化，因此本项不进入默认路径。
全部样本、B/op/allocs/op、首候选负结果及补测保存在[a06_samples.json](a06_samples.json)；
其中after明确是已撤回候选，不是当前生产源码。

单独3x CPU采样中，父/候选总CPU为4.00/4.04秒，累计去块为0.94/0.92秒；
这份短采样不足以解释端点反差，不能替代正式配对结果。profile和top报告在 `tmp/a06/`。
本任务构建、Oracle、profile和正式计时分开，外部负载/CPU时钟未监测。

## 已交付的回归与后续复用

`deblock_batch_test.go` 保留P/B零/混合残差、transform8、参考交换/空参考、
slice/mapped权限和B帧整帧像素对照；与既有六格式原调度回归共同保留后续候选的验收工作量。
基准新增 `BenchmarkEdgeStrengths_1080p_Auto` 八格，复用原
`BenchmarkDeblock_1080p_Auto` 两格；实际Go标量链，不标成SIMD。

```powershell
$env:CGO_ENABLED='0'; $env:GOAMD64='v1'; $env:GOMAXPROCS='16'
go test ./decoder -run '^TestDeblock_' -count=1
go test ./decoder -run '^$' -bench '^Benchmark(EdgeStrengths|Deblock)_1080p_Auto$' -benchtime=100ms -count=5 -benchmem
```

恢复A05生产源码后，CGO0完整gofmt/vet/build/全库test、36包依赖、55条语料清单、
两份直接AU FFmpeg像素Oracle和显式加权编码三方Oracle通过。
300帧主矩阵仍留到A07，不宣称本次已完成长期吞吐或持续实时验收。

## A03–A05最终集成验证通过

实际保留的生产改动是A03 P权重快路、A04均值SSE2、A05单/双向加权SSE2。
以下均已完成，原日志在 `tmp/a06/`：

- `CGO_ENABLED=0 go test -tags 'purego,goavc_simdtest' ./internal/simd ./internal/prediction ./internal/motion ./decoder ./encoder -count=1` 通过；不可用SSE2的显式测试按原规则跳过，不计作原生执行。
- 相同五包在Windows/386、CGO0下非缓存执行通过。
- Windows/Linux/macOS × amd64/arm64六组合全仓build及SIMD测试二进制编译通过；Unix/arm64不计作原生验证。
- `CGO_ENABLED=1 go test -race ./internal/simd ./internal/prediction ./internal/motion ./decoder ./encoder -count=1` 通过，随后恢复常规CGO0；不是全库race。
- 新均值/加权汇编的逐行保护页、标量/解析式、别名/宽域回退及局部栈数组0分配检查通过。

未运行300帧主矩阵、Unix/arm64原生执行、额外fuzz或长期稳定性测试；这些不算本项通过记录。
