# 09：验证矩阵、命令与证据规则

> 本章是M8执行手册；`purego`、`goavc_simdtest`、强制后端、保护页和分配保护网已落地，实际执行证据以12为准。K19/K20并行流水线/K20m本轮未实施，不能运行零匹配的TestParallel后宣称通过。脚本入口为 `scripts/verify_m8.ps1`，既不支持Resume，也不自动发布。

## 1. 分层验证，避免重复和空跑

1. 每个逻辑任务完成后，先运行精确包/用例，失败即修复；不要每编辑几行就跑全库。
2. 算法任务必须有规范解析向量或独立Oracle；Generic对SIMD仅证明后端等价，不能单独证明原算法正确。
3. 每个代码提交前按仓库要求执行完整常规门禁；多个agent不同时运行全库门禁和基准。
4. 最终验收负责人在集成版本统一运行native后端、purego、386、Oracle、分配和race矩阵；记录每次跳过和原因。
5. 只改文档的本次任务不跑Go全门禁；只核对文档引用、范围与任务覆盖。

## 2. 必须存在的验证维度

| 维度 | 最小覆盖 | 不可替代它的证据 |
|---|---|---|
| Generic正确性 | 现有规范解析向量、全语料哈希、编码三方Oracle | 自研编解码往返 |
| SSE2 | 原生amd64强制SSE2、GOAMD64=v1构建、反汇编检查无AVX/SSE4依赖 | AVX2机器Auto选到AVX2 |
| AVX2 | 原生可用AVX2、OSXSAVE/XCR0选择测试、直接后端parity | CPUID打印AVX2字符串 |
| NEON | 原生arm64运行逐元素parity和端点，报告CPU/GOHOSTARCH | amd64交叉build arm64 |
| purego | 全包测试，所有实际槽Generic，汇编文件未纳入构建 | 默认Auto恰好没调用某个汇编 |
| 386 | 原生可执行环境的GOARCH=386全测试，检查索引/溢出/字宽 | 只go build |
| 汇编边界 | 静态最大访问范围、最小len、非对齐/尾部、guard-page子进程 | canary或-race单独通过 |
| 单/多线程 | consumer输出、错误优先级、RC/时钟/背压一致 | 检查worker数或调度条件 |
| 分配 | 已准备真实kernel/MB/模式决策AllocsPerRun=0，端点另报 | 只直接调用汇编、空Skip或初始化前移 |
| 所有权 | 多输出同时持有、B锚点/DPB元数据/失败/Close/Release | GC最终回收或只无panic |

普通8-bit420是性能主场景，8/10-bit六格式与MBAFF/高级工具是行为不回归保护网，不能因没进并行快路删掉测试。

## 3. 当前可用的基准与Oracle保护网

现有名称可直接定位：

- `BenchmarkDecoder_1080p_Generic`：三帧含构造/排空/Close，输入I/O不计时。
- `BenchmarkEncoder_720p_Generic`、`BenchmarkEncoderP_720p_Generic`、`BenchmarkEncoderPFlat_720p_Generic`、`BenchmarkEncoderCABAC_720p_Generic`、`BenchmarkEncoderB_720p_Generic`。
- `BenchmarkEncoderCBRFlatSequence_720p_Generic`：一个op含12帧与排空，不能直接当每帧ns。
- `TestCorpus_M2M3High10ProjectionOracle`：原27份M2/M3保工具投影。
- `decoder/advanced_corpus_test.go`、`corpus_oracle_test.go`：原生像素和Oracle来源边界。
- `tests/codec`：编码三方像素、权重/矩阵/码控/B/lookahead/隔行保护网。
- `TestEncoder_InterLongOracle` 为显式长门禁，需 `H264_LONG_TEST=1`；普通测试跳过不算执行过。

引入dispatch后，旧名的 `_Generic` 不再天然代表标量。保留历史基准对照时用purego或测试限定后端；新增Auto基准另命名，不能悄悄测了Auto还把结果记为Generic。

## 4. 环境与完整常规门禁

以下命令从仓库根目录运行。PowerShell每条原生命令检查LASTEXITCODE；gofmt有输出即失败，不能只看其退出码。

```powershell
$env:CGO_ENABLED = '0'
go version
if ($LASTEXITCODE -ne 0) { throw 'go version failed' }
$unformatted = @(gofmt -l .)
if ($LASTEXITCODE -ne 0 -or $unformatted.Count -gt 0) { throw "gofmt failed: $unformatted" }
go vet ./...
if ($LASTEXITCODE -ne 0) { throw 'vet failed' }
go build ./...
if ($LASTEXITCODE -ne 0) { throw 'build failed' }
go test -count=1 ./...
if ($LASTEXITCODE -ne 0) { throw 'test failed' }
go run ./tools/depscheck
if ($LASTEXITCODE -ne 0) { throw 'deps failed' }
go run ./tools/manifest -file testdata/manifest.json
if ($LASTEXITCODE -ne 0) { throw 'manifest failed' }
```

已有POSIX入口为 `make verify` 或 `bash scripts/verify.sh`；如果采用这些入口，不再无意义重跑同一套命令。最终非缓存测试可选择上面的显式命令方案。只对修改过的Go文件做gofmt -w，不能用make fmt重写用户已有工作。

## 5. 拟新增定向测试命名及执行

每类至少有一个实际匹配用例；名称可调整，但命令必须随真实符号同步：

```bash
CGO_ENABLED=0 go test -count=1 -run '^TestCPU_' -v ./internal/simd
CGO_ENABLED=0 go test -count=1 -run '^TestParity_' -v ./internal/simd
CGO_ENABLED=0 go test -count=1 -run '^TestAllocation_' -v ./internal/simd ./decoder ./encoder
CGO_ENABLED=0 go test -count=1 -run '^TestParallel_' -v ./decoder ./encoder
CGO_ENABLED=0 go test -count=1 -run '^TestPool_' -v . ./internal/picture ./internal/buffer
CGO_ENABLED=0 go test -count=1 -run TestCorpus_M2M3High10ProjectionOracle -v ./decoder
```

上述TestCPU/TestParity等前缀由对应任务实现；运行日志必须显示实际用例，不把`[no tests to run]`计成功。

Go1.27支持的构建标签方案落地后：

```bash
CGO_ENABLED=0 go test -tags purego -count=1 ./...
CGO_ENABLED=0 go test -tags goavc_simdtest -count=1 ./internal/simd ./decoder ./encoder
```

后端矩阵测试在各包内部串行选择真实可用后端，功能用例覆盖真实consumer；严禁以setter锁测试代替算法测试。强制不可用后端显示Skip并记录未测，不能静默回Generic后算通过。

可用 `go list -tags purego -f '{{.ImportPath}} {{.SFiles}}' ./internal/simd` 核对该构建没有汇编源；另检查声明/平台包装也被正确排除。该检查是构建证据，不替代全包运行。

## 6. 386 与六目标构建

在支持执行32位程序的runner执行：

```bash
CGO_ENABLED=0 GOARCH=386 go test -count=1 ./...
CGO_ENABLED=0 GOARCH=386 go test -tags purego -count=1 ./...
```

若host不能执行目标程序，明确记为构建通过/原生运行未执行；使用原生Linux或Windows合适runner补齐。不要使用`go test -exec true`制造假通过。386无race要求，Go竞态检测器平台支持另计。

保留 `make cross` 六组合。M8验收还要编译包含汇编测试的二进制：对 `internal/simd` 可在独立临时目录逐目标 `go test -c -o <明确且不重复的输出路径>`；其他包分别编译，不能多个包共享一个-o文件。这里输出路径需要验收脚本按实际GOOS/GOARCH生成，不在文档填一个伪固定文件。

## 7. race 与确定性

仅开发期race启用CGO，项目源码仍不导入C。标准环境：

```bash
CGO_ENABLED=1 go test -race -count=1 ./decoder ./encoder ./stream ./internal/picture
CGO_ENABLED=1 go test -race -tags purego -count=1 ./decoder ./encoder ./stream
```

最终完整一次：

```bash
CGO_ENABLED=1 GOMAXPROCS=2 go test -race -count=1 -timeout 45m ./...
```

PowerShell运行前暂存CGO_ENABLED/GOMAXPROCS旧值，结束后恢复；不要让后续常规构建误用CGO=1。超时参数是当前参考，不是允许忽略挂起；新并行用例还需在实际多核配置下定向race，不能仅GOMAXPROCS=1。

至少在Threads=1/2/auto、不同任务完成顺序下比较完整Packet序列、显示Frame、错误与状态；测`GOMAXPROCS`并不等于配置Threads。小型调度测试用同步屏障控制顺序，不用sleep制造“可能的竞态”。

## 8. CLI 实际表面验证

并行功能必须用实际h264enc/h264dec验证，不只库内测试。现有CLI采用`-input/-output/-threads`等参数，最终以 `-h` 和 `common.flags` 的实际实现为准。

可复用已存在的双slice语料，分别执行库/CLI单多线程并比较输出文件：

```bash
work="$(mktemp -d)"
CGO_ENABLED=0 go run ./cmd/h264dec -input testdata/baseline/baseline_320x240_yuv420p_cavlc_ip_multislice_003.264 -output "$work/serial.yuv" -threads 1
CGO_ENABLED=0 go run ./cmd/h264dec -input testdata/baseline/baseline_320x240_yuv420p_cavlc_ip_multislice_003.264 -output "$work/parallel.yuv" -threads 4
cmp "$work/serial.yuv" "$work/parallel.yuv"
sha256sum "$work/serial.yuv" "$work/parallel.yuv"
```

上述POSIX命令创建独立临时目录；PowerShell使用临时目录及Get-FileHash等价执行。验收后只清理本次创建的目录。补CABAC/B/10-bit/422/444/MBAFF实际命令，输出原始字节长度、帧数、SHA-256；同哈希之外用逐平面比较器定位失败。

编码CLI使用现场生成并登记SHA的输入；固定全部参数，先threads1再threads4，比较Annex-B字节和FFmpeg解码。必须覆盖B+lookahead排空、CQP与至少一个VBV路径；默认CLI线程值仍1。帮助文本不再写“仅0/1”，但实例非并发安全、有效并行受工作量限制要保留。

## 9. Oracle 与跳过边界

- 55条manifest均做当前支持状态对应验证；Unsupported条目验证错误和恢复，不要求输出像素。
- `oracle=analytic` 保持解析解来源；FMO/ASO/分区/secondary SP/SI的FFmpeg限制见M7证据，不能在M8改成FFmpeg通过。
- 独立色平面沿用原始封装/单色投影的证据边界；不能误称FFmpeg已支持原始封装。
- 高位深以原生LE采样/有效显示区域比较，保持显示顺序，不经RGB或8-bit转换。
- 编码要求FFmpeg=Go Decoder=本地重建，并且M8优化前后Packet字节一致；三方像素相等不能替代码流确定性。
- `tools/oracle` 原始退出码0一致、2像素差异、3缺FFmpeg、4工具/环境错误。若用go run，外层进程可能只返回1并打印exit status N；要保留工具精确码，先构建到临时路径再执行二进制。
- 裸HRD等需要 `-fps_mode passthrough` 的场景单独用FFmpeg明确输出；当前oracle工具未统一设置该参数，不能让默认补帧污染帧数和哈希。
- 需要外部Oracle的验收缺FFmpeg时记未执行，不把Skip当通过；不为了全绿删除工具检查。

## 10. 测试设计约束

新增测试只验证可观察的像素/码流/错误/状态/所有权/分配或确定性，不测试源码字符串、字段恰好某个默认值、长度变大、仅“不panic”。参数默认变化造成的旧实现快照测试应更新为行为契约，不再钉住“Threads2必须Unsupported”。

随机种子固定，尺寸受限，独立结果和资源清理明确；全套运行安全，不依赖用户本地文件或不可访问网络。大型Oracle/性能数据生成到临时目录，大文件不入库，结束清理；已登记语料遵守manifest来源和SHA规则。

最终报告分别列：已通过、失败、明确跳过、环境未执行。只有本章全部必需维度有实际证据后，才讨论K25门禁收尾。

本机剩余测试集中执行：`powershell -NoProfile -ExecutionPolicy Bypass -File scripts/verify_m8.ps1 -OutputDirectory <新的临时目录> -Phase validation`。包括非缓存普通门禁与显式两尺寸300帧长Oracle、全库purego/386/测试标签、六目标汇编vet/测试编译、全库及定向purego race、CABAC/B短fuzz、30秒既有PCM留存回归、55条语料真实CLI哈希与六格式×四种码控的实际CLI/FFmpeg像素比较。各日志保留原始跳过项，不将命令成功等同每个子测试都执行。


## 11. 既有Fuzz Smoke保护

Roadmap §9.5要求每PR保留短时Fuzz Smoke。M8不是重做M9全量模糊测试，但不得只跑种子测试后声称做过模糊输入探索。当前已存在的入口可分别执行，每次只选一个fuzz target：

```bash
CGO_ENABLED=0 go test -run '^$' -fuzz '^FuzzDecoder_NoPanic$' -fuzztime=5s -parallel=2 ./decoder
CGO_ENABLED=0 go test -run '^$' -fuzz '^FuzzEncoder_InterRoundTrip$' -fuzztime=5s -parallel=2 ./tests/codec
CGO_ENABLED=0 go test -run '^$' -fuzz '^FuzzDecoder_Residual$' -fuzztime=5s -parallel=2 ./internal/entropy/cabac
```

内存/调度或内核改变涉及其他已有fuzz入口时选对应目标，不一概扩大成本。记录真实执行数和发现的崩溃；崩溃输入经最小化后作为回归种子登记，禁止删除失败输入或通过改随机种子掩盖。最终不把本短烟测描述为M9完整Fuzz验收。
