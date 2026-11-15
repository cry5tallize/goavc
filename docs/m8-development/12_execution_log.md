# M8 实施记录

本页记录实际任务、接口与证据；01–11 是原始设计输入，不代表实施完成。

## 当前交接点

- 分支 `perf/m8-simd`，A00、K01–K05、K07、K09a已逐项提交；K06/K08按用户要求完成实现与交叉编译，原生未验证。SAD与SATD已真实接入，MC当前是唯一Generic块算法，不是SIMD插值。
- amd64 Auto逐槽选择：SAD宽16为AVX2（其他宽SSE2），SATD4×4优选实测更快的SSE2；purego无项目汇编。没有全局“所有槽均AVX2”的声明。
- SIMD后续K10a/b：Forward4x4已接入SSE2与NEON；SSE2原生解析/parity/零分配通过，NEON按用户要求仅完成三种arm64编译验证，原生未验。8×8/逆变换和其余内核仍待实现；内存/GC工作由其他agent负责，本轮没有改动其文件。
- K14a已补Windows SAD逐行保护页；其余内核矩阵未完成。K09b/c/d、K10–K13、K15–K25及K20m仍待实施，K14收尾待所有内核交付。
- G01/G02/G03a/G03b（K16/K18切片）已消除搜索/残差/邻域/试写反复分配。prepared候选和六格式QP26/flat的真实非空I/P/B决策为0分配；P控制中位4945520B/op、42次/op，主要剩余图像/元数据/独立输出。完整配置/所有权/资源集成门禁仍待补齐，不签完整K18。
- G03b生产代码完整race通过（tests/codec 291.726s），后续B fuzz测试修复另补新种子race；本轮decoder/P/B短fuzz已执行，原B失败已独立复现/修复，不隐去失败。代表性主目标长矩阵、自然GC指标及远端CI未执行；不创建标签、不push/release，不签完整M8。以下各节按实施时间保留当时记录，最新状态以本节和状态矩阵为准。

## A00：基线与契约

- 原始算法 revision：`1abfa01dcd94fc580990f25c8c8d8811aabb3c72`；开工分支 `perf/m8-simd`。原有未跟踪实施文档原样单独提交为 `37ca7f1`，未混入算法改动。
- 环境：2026-11-15，Go 1.27.4，Windows/amd64，AMD Ryzen 7 5700X，8 核 16 逻辑处理器，高性能电源方案。常规命令 `CGO_ENABLED=0`、`GOAMD64=v1`，默认 `GOMAXPROCS=16`；每实例串行。测量未同时运行其他本次任务的构建、长测或基准；未监控外部负载。
- 55 条语料的 manifest SHA-256：`128c96e409d9a528ab54458b22cb2428cebca3b8d4bf775742fd1df10c7c2fa9`。
- 原三项 benchmark 原样各运行五次；计时语义不变，详 `docs/gates/m8_baseline.json`。历史 benchmark 的 Threads 请求为 0，当前实现有效串行；后续固定比较须明确串行，不能把自动并行混入 ISA 倍率。
- `TestM8Baseline_SerialOutput` 固定六格式×双熵共 12 配置、每配置 8 帧的输入、Annex-B、包/帧元数据及显示像素 SHA。QP26、B2、GOP5、IDR13、3slice、搜索半径2、8×8、自动P权重、Lookahead2、Threads1；420为34×18、422为34×19、444为35×19。复用 M7 非紧凑 stride 图案，10-bit 含独立低两位。初次运行先对 FFmpeg/自研解码/本地重建逐采样验证，再从未改算法的结果固定 JSON；初次因尚无 golden 文件而失败是采集步骤，不能写成已通过回归。后续测试不提供更新 golden 开关。
- 当前 profile：编码 B 基准 `pixClamped` flat 26.26%、MC luma cumulative 38.27%，SAD flat 3.07%；分配字节中 `interResidual` flat 66.23%。解码短样本 `pixClamped` flat 11.43%、lumaEdge flat 10%；样本较短，比例仅用于排序。原始 profile 和日志在本地 `tmp/m8-baseline/`，不作为正式吞吐数据。

### 冻结接口与政策

1. 第一批只迁移 SAD：`SAD8(a []byte, as int, b []byte, bs, width, height int) int` 和 `SAD16(a []uint16, as int, b []uint16, bs, width, height int) int`。stride 均为采样数，容量按 len；byte 域0..255，uint16有效域0..1023；只读有效矩形，允许最小末行和不对齐起点。上层继续负责现有几何验证。函数表与后端诊断逐槽记录，未来内核实现时才加槽。
2. SAD 的唯一 Generic 迁入 simd，真实 `window.consider` 的整像素/MC候选两路都迁移；不只改导出包装。Scorer 时机、原始 SAD、候选顺序和严格小于比较不变。
3. 拟新增根包 `NewFrame(width, height int, format PixelFormat) (*Frame, error)` 按05执行；正尺寸且每维≤8192，六格式，分配前检查乘法。公共头不复用，租约一次归还，Clone独立，Close后输出仍有效。原始池只能依赖标准库。
4. picture 池保留现有实现：空闲存储总预算64MiB、每桶4张；Frame backing进程缓存64MiB、每桶4帧、最多8个非空桶；均不是活跃内存或RSS上限。公共Frame有效字节清零。
5. Threads 总计算预算包含调用goroutine，上限64；0在New时取GOMAXPROCS再裁剪，1不创建worker。slice scratch按活跃worker分配，禁止复制每slice整帧状态；实际scratch字节数须由K16实现按结构大小登记后才能放行并行，当前未测。
6. ME按07§5的 `CandidateBatch[T]` / `BatchExecutor` 形状落地，批上限64；Evaluate同步完成，不保留批指针，worker只算SAD，Scorer与原序归约留主线程。执行上下文显式迁移全部调用者，不新增兼容shim。
7. R3在真实消费者接入时分别放行 `internal/motion/search`、`internal/picture`；不提前放行整个目录族。正式后端诊断测量暂不增加tools直接依赖simd，先由包内基准报告。

### 文件归属与推进

按11的A–G切片逐任务执行：CPU/dispatch/types与规则由集成者唯一维护；每个kernel的Generic/领域适配和架构文件分开交付；codec的池化、scratch、去分配、并行依次接手同一文件。当前未派发并行编辑任务。K01/K02/K03分别提交，首条SAD基础完成后优先K09a的块化，再按当前profile接架构实现；K06/K08及其余NEON原生验收保持独立记录。

## 验证状态

精确命令、退出码和剩余项随任务追加。没有原生arm64 runner，不能将交叉构建记为NEON通过；远端CI未执行，不自动push/tag/release。未完成K01–K25/K20m，不签M8门禁。

A00：`CGO_ENABLED=0 go test -count=1 -run '^TestM8Baseline_SerialOutput$' ./tests/codec` 退出0，12配置共96帧独立FFmpeg比较执行。完整常规门禁依次执行 `gofmt -l .`（无输出）、`go vet ./...`、`go build ./...`、`go test -count=1 ./...`、`go run ./tools/depscheck`、`go run ./tools/manifest -file testdata/manifest.json` 和 `git diff --check`，均退出0。完整日志 `tmp/m8-baseline/a00-verify.txt`；依赖检查35包、清单55条。沙箱首次拒绝写模块版本缓存，获准后在沙箱外重跑通过。长门禁 `H264_LONG_TEST=1`、race及原生arm64未执行，不能由本次常规门禁推定通过。

### K01：安全 CPU 检测

从A00提交 `2a45f23` 开始，新增cpu平台文件及测试；运行时只在包初始化探测，缺leaf1/7、XSAVE/OSXSAVE/AVX或XMM/YMM状态均不启用AVX2。模拟1024种组合，断言缺能力时不调用XGETBV。native windows/amd64 检测SSE2/AVX2可执行；没有由检测结果冒称kernel已实现。

`CGO_ENABLED=0 go test -count=1 -run '^TestCPU_' -v ./internal/simd`、`go test -tags purego -count=1 ./internal/simd` 均退出0；`go list -tags purego -f '{{.SFiles}}' ./internal/simd` 输出 `[]`。完整常规门禁退出0（`tmp/m8-baseline/k01-verify.txt`）；`GOARCH=386 go test -count=1 ./internal/simd` 在本机实际执行退出0；windows/linux/darwin × amd64/arm64 的simd测试二进制 `go test -c` 全部编译成功。arm64未原生执行。首次vet发现uint64返回区的部分宽度写法，已改为合并寄存器后一次MOVQ返回，并重新通过完整门禁。

### K02：类型化 SAD 分发与真实接入

从 `4620a76` 开始；新增SAD8/16两槽及逐槽 `ActiveBackends()` 诊断，当前均为Generic。共享包的 `BindSAD8/16()` 返回只读内核函数供搜索上下文一次绑定，候选循环仅一次函数指针调用。删除search原SAD标量循环，整像素与MC候选均调用同一simd内核。R3只加入当前真实消费者 `internal/motion/search`；`internal/picture` 等其真实接入时再放行，规则测试拒绝搜索子目录和picture的提前使用。架构原文中“宏块对齐允许越界读”已纠正为逐入口证明len/halo/场边界。

`CGO_ENABLED=0 go test -count=1 ./internal/simd ./internal/motion/search ./internal/testutil/importgraph`、固定M7指纹、完整常规门禁及 `go test -tags purego -count=1 ./internal/simd ./internal/motion/search` 全部退出0，日志 `tmp/m8-baseline/k02-verify.txt`。12组指纹未改变，SAD16的低两位和261888最大值解析验证通过。

真实P消费者五次ns/op为2291247300、2390645600、2216798200、2243312300、2191737400，中位2243312300；B/op约335.76MB，allocs/op约875.5k。对照A00中位2173442000ns、293.50MB、778.3k，分发结构阶段尚无加速，临时window经间接调用逃逸，增加约97.2k分配。K18必须将搜索/模式决策typed workspace复用一并解决，不能将该阶段记为性能完成。原始日志 `tmp/m8-baseline/k02-encoder-p-bench.txt`；后续比较保持相同串行配置，不覆盖A00。

### K03：可复用 parity 与测量基础

从 `5b9fa20` 开始；固定seed `0x8a7c`，矩形宽高各10种、byte起点0..31/uint16起点0..15、三组不等stride、最小末行len，比较完整输入不变并验证独立解析最大SAD。生产SAD包装在预备输入后 `AllocsPerRun=0`。专用 `goavc_simdtest` 标签提供进程内锁、能力错误及幂等恢复；普通生产构建不含setter，真实搜索用同一锁强制后端。当前只有Generic真实实现，SSE2/AVX2/NEON明确Skip为未执行，不假装回退后通过。

`CGO_ENABLED=0 go test -tags goavc_simdtest -count=1 -v ./internal/simd ./internal/motion/search`、完整常规门禁、`go test -tags purego -count=1 ./internal/simd ./internal/motion/search` 全部退出0；日志 `k03-parity.txt`、`k03-verify.txt` 位于 `tmp/m8-baseline/`。随后独占执行 `go test -run '^$' -bench '^BenchmarkSAD(8|16)_16x16_' -benchmem -count=5 ./internal/simd`，Generic和Auto各五次，均0 B/op、0 allocs/op；原始日志 `k03-sad-bench.txt`。后端专用原生parity及保护页在后续任务补齐，当前不能签K14。

### K04：SSE2 SAD

从 `2a99d40` 开始；8位每行16/8/4/1字节，10位每行8/4/1采样的实际SSE2计算，非对齐加载且不超出width。10位unsigned饱和差两方向合并为绝对差，每对≤2046，立即扩为64位后累加；未使用SSE4或AVX。私有搜索通过已绑定函数真正进入该路径；其他架构及purego保持唯一Generic。

本机SSE2直接后端及强制搜索（含10位低两位、负MV、全幅Scorer）通过；12配置M7码流/元数据/像素指纹不变，FFmpeg逐采样通过。完整常规门禁、purego目标包通过，`go tool objdump -s 'simd.sad(8|16)SSE2'` 得到实际汇编并核对仅SSE2/基线整数指令。首轮构建修正Go汇编使用的PMADDWL名称后重新验证。最小len矩阵和每行最大访问范围已核对；K14保护页尚未执行，不把canary/parity等同动态越读证明。

同机五次微基准及原始日志SHA见 `docs/gates/m8_sad_samples.json`，kernel均0 B/op、0 allocs/op。真实P消费者实际后端SSE2（沿用旧函数名 `_Generic`，不能据名字称其为标量）五次ns/op为2102411500、2101213600、2104698700、2089643800、2099128300，中位2101213600；对照K02中位2243312300。分发造成的额外临时分配仍未解决，不声称整体模式决策零分配。日志在 `tmp/m8-baseline/k04-{parity,verify,sad-bench,encoder-p-bench}.txt`；原生NEON和完整race未执行。

### K05：AVX2 SAD

从 `311a449` 开始；宽16的byte路径将两条独立16字节行拼成YMM，奇数末行只加载16字节并清零高半部；uint16每行16采样恰为32字节。unsigned差值扩为qword累加，退出VZEROUPPER，不依赖BMI/FMA。其他宽度明确走SSE2，包括7×13等尾部场景；逐槽AVX2诊断表示绑定该混合包装，不能把非16宽的实际计算记为AVX2。

`go test -tags goavc_simdtest -count=1 -v ./internal/simd ./internal/motion/search` 在原生amd64执行Generic/SSE2/AVX2直接与真实搜索路径，只有NEON跳过；固定96帧指纹和FFmpeg不变。完整常规门禁及purego目标包退出0，反汇编文件 `tmp/m8-baseline/k05-avx2-disassembly.txt`；全部常规Go命令CGO=0、GOAMD64=v1。

各后端五次微基准原始样本追加至 `docs/gates/m8_sad_samples.json`，均0分配。真实P消费者实际Auto=AVX2/SSE2，五次ns/op为2106986600、2080791300、2129799700、2072790900、2151368700，中位2106986600，较K04端点无明显额外收益，不能用microbench倍率承诺端点提速。临时window分配问题仍在K18。原始日志 `tmp/m8-baseline/k05-{parity,verify,sad-bench,encoder-p-bench}.txt`。

### arm64 验收范围调整

用户明确确认没有arm64机器，要求“做了能过编译就行”。本次arm64工作按实现与六目标交叉构建交付，不等待原生runner；原实施门禁中的NEON原生parity/Oracle/性能仍记未执行，不能据交叉构建宣称这些证据存在。此调整不取消其他平台、正确性、所有权及并行门禁。

### 独立修复：缺 FFmpeg 仍执行基线回归

从 `7caf104` 开始；外部Oracle比较移到子测试，缺工具只跳过该子项，父测试仍校验全部12组固定指纹。新增清空PATH的定向回归，确认缺FFmpeg时父测试通过且各Oracle子项显式跳过；有工具的正常测试继续执行实际比较。`go test -count=1 -run '^TestM8Baseline_' -v ./tests/codec` 和完整常规门禁均退出0，日志 `tmp/m8-baseline/baseline-{no-ffmpeg,fix-verify}.txt`。此项单独fix提交，不与向量功能混合。

### K06：NEON SAD 编译交付

从 `360277b` 开始；arm64基线ASIMD，宽16正常生产调用进入真实向量计算。byte每行一个16字节向量，以unsigned max-min求差并扩宽归约后转64位累加；uint16每行两组8采样，分别扩宽到32位归约后转64位累加。其他宽度绑定包装中的唯一Generic，不能将这些fallback记为NEON运算。加入直接后端parity/基准及测试限定强制入口，留待原生环境执行；不含可选扩展，不复制另一份Generic。

按用户明确范围完成编译验收：`CGO_ENABLED=0 GOARCH=arm64 GOOS={windows,linux,darwin} go vet ./internal/simd`、含 `goavc_simdtest` 的 `go test -c` 均退出0。随后windows/linux/darwin × amd64/arm64六目标全库 `go build ./...` 与simd后端测试二进制全部构建成功；native完整常规门禁、purego目标包也退出0（`tmp/m8-baseline/k06-verify.txt`）。没有运行任何arm64二进制，没有NEON五次基准/Oracle/parity数据，支持状态保留WIP。完整M8未完成。

### K09a：块级标量 MC 与边界分离

从 `da8bbbc` 开始，按profile优先于K07/K08推进此共享重构。公共亮度/色度点采样及块入口签名不变；LSP已核对decoder/encoder/search与全部测试调用。唯一六抽头、相位组合、双线性算术迁入 `simd/mc_generic.go`，删除原领域重复循环。领域层只做采样格式/负MV floor分解、逻辑场边界patch和16×16分块；大于16及最小末行len继续支持。整数相位不要求六抽头halo，内部块借用原有效缓冲，边缘固定21×21 scratch；j的水平中间行只算一次且不提前裁剪，块入口只选一次最终组合。独立weighted预测仍在原插值后执行，未改变QP/搜索/候选。

新内部入口仍为 `simd.MCLuma[T ~byte|~uint16](dst []T, ds int, ref []T, rs,w,h,fx,fy,bitDepth int)` 与 `MCChroma[T](dst []T, ds int, ref []T, rs,w,h,fx,fy int)`；w/h≤16，stride均为采样，ref从精确halo左上角开始，调用者证明实际len。新增仅对`byte`、`w=16`、`fx=fy=0`的真实SSE2/AVX2逐行16字节复制；其余相位/宽度/位深和非amd64组合仍走唯一Generic。`mc_generic_test.go`覆盖luma非对齐stride和chroma全部1/8相位、8/10-bit有效域与padding不写；`internal/simd`、`internal/prediction`及purego测试通过。该快路不是完整SIMD插值实现。

旧MC指纹 `ff62c7838da485c15516101ba5438203a6320500d05f85c6383a458b0209bddd` 在改算法前采集，包括双位深、420/422/444、全部相位、负MV、非对齐/最小len、1/3/7/13/16/17/31矩形及未写padding。改后不变；既有规范解析值、场边缘和固定96帧三方像素/码流/元数据不变。`TestAllocation_MCBlock` 在预备图像/场上下文后运行实际三采样格式、双位深、内部/边缘块且首尾输出必写，AllocsPerRun=0；不能由此推定encoder模式决策已零分配。

最终常规门禁、`CGO_ENABLED=0 go test -tags purego -count=1 ./...`、386实际执行prediction/search/simd、六目标全库build全部退出0；27份High10投影显式用例通过。Fuzz Smoke实际执行decoder 46257次、encoder inter 1317次，均无崩溃；各5秒请求、2 workers，实际结束约6秒。日志 `tmp/m8-baseline/k09a-{verify,purego-all,386,high10-projection,fuzz-decoder,fuzz-encoder}.txt`。首次构建对齐了prediction已有~采样类型约束后重新验证，未删除失败用例。

同机五次MC微基准覆盖interior/border/真实bottom FieldView和双位深；旧源码的固定1x消费者在隔离clean worktree重测，再与最终代码同边界五次比较，旧中位2149919100ns、新中位916330100ns，约2.35倍。保留新首样本1393510500ns等波动，不挑最快值。原始样本、输入SHA及参数见 `docs/gates/m8_mc_samples.json` 和performance§15；原始日志保留于本地tmp，临时工作树已清理。不以此P控制场景替代代表性High/B主目标。

下一项：K09b/c/d仍待完成；当前仅交付MC整数宽16复制快路，K10–K25及K20m未完成。后续SIMD插值必须使用上述唯一Generic与同一边界证明，不能复制一套预测；K18必须解决已登记搜索window逃逸。尚未执行完整race或原生arm64，不签完整M8门禁。

### K07：x86 SATD

从 `4d3238c` 开始，真实 `encoder.blockSATD → transform.SATD4x4 → simd` 接入；唯一Generic迁入simd，保留领域指针签名。内部按值传16个int32（64字节），避免栈上局部残差因间接调用而逃逸；02中的拟定指针参数已同步为实际值参数。范围为真实8/10位残差±1023，两轴Hadamard没有中间舍入，最后仍 `(sumAbs+1)>>1`；矩形继续4×4 tiled，不改候选和代价标度。

SSE2以32位蝶形/转置/绝对值归约，不夹SSE4/AVX；AVX2以双行YMM蝶形、VPBLENDD/VPABSD实际计算，退出VZEROUPPER，不额外假设BMI/FMA。固定输入值64字节，所有加载最大偏移63，不依赖padding或外部指针。vet对数组参数偏移的首轮检查已修正为先取完整数组地址再按明确偏移加载，随后完整门禁通过。

独立常量/冲激解析值含±1023、低两位±3，固定seed5000组逐后端parity；强制Generic/SSE2/AVX2分别执行真实8/10位帧内候选，NEON SATD未交付时不把Generic槽记成NEON通过。生产包装及领域循环内局部残差的AllocsPerRun均0，M7固定96帧码流/像素/元数据不变。完整常规门禁、purego目标包、386实际执行simd/transform及六平台simd测试二进制构建均退出0；日志 `tmp/m8-baseline/k07-{verify,purego,consumer-parity}.txt`。

同机五次初测SSE2约7.262ns、AVX2约16.02ns、Generic约21.61ns；因此生产初始化按槽优选SSE2 SATD，SAD仍为AVX2。显式测试仍可选择真正AVX2 SATD。选择前后的全部微基准/固定1x P样本见 `docs/gates/m8_satd_samples.json`，均0 kernel分配，不删除较慢数字。初始Auto AVX2端点中位936393400ns，改为SSE2后878243300ns；不据此保证其他CPU或代表性High/B主场景收益。完整race、NEON原生仍未执行。

### K08：NEON SATD 编译交付

从 `317a0cb` 开始，按K07同一64字节值参数实现ASIMD两轴32位Hadamard/转置/绝对值归约；输入合法域±1023，归约不进入16位累加器，最后按旧int32归约及算术右移返回。不使用可选扩展；固定加载0..63字节，无外部指针。ARM分发SATD槽已绑定真实NEON，Generic及矩形tiled语义仍唯一，原生parity/基准和真实候选测试入口均可用。

遵照用户“能过编译就行”的arm64范围：三种arm64 GOOS的 `go vet ./internal/simd`、全库 `go build ./...`、带 `goavc_simdtest` 的simd测试二进制全部交叉构建成功；本机完整常规门禁和purego simd/transform通过，退出0，日志 `tmp/m8-baseline/k08-verify.txt`。未执行NEON原生parity/Oracle/分配/性能，不生成虚构数据，状态WIP；无发布或标签操作。

### K14a：逐行 SAD 保护页

从 `59d3e45` 开始；Windows子进程在pin的无指针Go byte backing内部设置PAGE_NOACCESS，保证每一行（不只最后一行）的最后有效采样紧邻保护页。恢复原页属性后才unpin，不把Go指针藏入整数再转换回来；uint16视图限定偶数起点/len、同对象内。保护页不覆盖对象头或相邻对象，纯标准库/CGO=0，unsafe理由与边界在测试源码中说明。子进程异常由父测试报告，不杀死整个协调器。

宽1/3/4/7/8/13/15/16/17/31、高1/3/16/17、byte/uint16共80几何；本机Generic/Auto/SSE2/AVX2四路径共320调用实际通过，解析期望分别为w*h*255/1023。最小len覆盖所有末行，无依赖padding的通过条件；原随机输入不变/非对齐stride矩阵继续保留。`go test -count=1 -run '^TestGuardPage_SAD$' -v ./internal/simd`、完整常规门禁、purego和386实际执行均退出0；六目标simd测试二进制编译通过。日志 `tmp/m8-baseline/k14a-{guard,verify}.txt`。其他OS动态页明确Skip为未执行，NEON不作原生声明；这不是完整K14或汇编race证明。

### G00（部分）/G01：调用者拥有的搜索 Workspace

起点 `4ec4ccc`。恢复时已有用户修改的05/08/README及未跟踪13专项计划，原样保留，不混入代码提交。按13优先级先修搜索逃逸，没有重做分发/SAD/SATD/MC。G00本次仅补当前P/B/冷解码各五样本、P独立allocation profile和搜索逃逸诊断；自然GC/assist/scan/pause/RSS/留存、warm-steady及RC测量尚未交付，不能称完整G00。

`gopls references search.go:91:6 / :129:6 / :156:6 / :162:6`核对全部入口引用。Integer/Refine/Estimate追加必需`*Workspace`参数；EstimateWithCost在Scorer后追加同参数，移除旧隐式分配签名，真实interCandidateUsingMotion和所有测试同步迁移。Workspace零值可用，非并发/重入，一个lane一份；两个私有typed窗口含1536字节采样槽加状态。Encoder构造一次、Reset复用、Close解绑；场/独立平面channel浅拷贝只借用同一指针。newFrameState兼容包内手工构造的Encoder，不会每次建立workspace。

准备时只覆盖有效原始矩形，Estimate两阶段共用原始块与绑定；细化前仍重新初始化best/cost/SAD。Scorer同步参数传递，不存入持久窗口；每次结束（包括Scorer panic展开）解除reference/ref借用。候选顺序、重复候选、原始SAD和strict-<不变。改造前捕获32组8/10-bit、frame/FieldView、full/diamond、边界/内部、相等/非相等Scorer的序列指纹、MV、SAD，改造后逐项一致。fixture无更新开关。初次采集测试误写不存在的格式常量，修正为既有YUV420P10LE后才采集有效基线，编译失败不算通过。

TestAllocation_PreparedSearch覆盖双位深×真实场/帧×边界/内部×full/diamond×四入口，共64用例，均0分配；Scorer在每次测量调用内部建立。强制Generic/SSE2/AVX2重复上述测试和原解析向量；NEON本机不可用明确Skip。实际Encoder跨帧/Reset及MBAFF两场复用同一lane，Close解除私有指针。完整96帧基线不改golden。

同机固定1x五次P中位：873234900 → 864596000 ns/op，335761024 → 293497608 B/op，875520 → 778286 allocs/op。B一个op含四帧：1958292700 → 1979717200 ns/op，777294336 → 735043024 B/op，2117812 → 2020629 allocs/op；保留B耗时约1.1%的波动，不称普遍吞吐提升。全部样本、计时边界、配置、源revision、输入SHA及日志SHA见`docs/gates/m8_search_workspace_samples.json`。

P独立memprofilerate=1覆盖整个进程（含预热，不能与计时区B/op混算）：搜索两阶段原各20250KiB和32400对象已从top栈消失，Scorer调用者的32400闭包对象亦消失。剩余主要为interResidual、analyzeIntra、subsampledChromaResidual、Writer.WriteBits和blockSamples。search的-m=2不再报告局部window搬堆或生产Scorer逃逸；测试框架闭包准备分配单列。没有测自然GC，不能给GC次数/暂停下降百分比。

验证全部退出0：`go test -count=1 ./internal/motion/search ./encoder`；`go test -tags goavc_simdtest -count=1 -run '^(TestParity_SearchBackends|TestWorkspace_)' -v ./internal/motion/search ./encoder`；`go test -tags purego -count=1 ./internal/motion/search ./encoder`；实际`GOARCH=386 go test -count=1 ./internal/motion/search ./encoder`；Windows/Linux/macOS × amd64/arm64全库build；完整常规门禁gofmt/vet/build/test/depscheck/55条manifest/diff-check。普通命令均CGO_ENABLED=0，日志`tmp/m8-baseline/g01-{targeted,parity,purego,386,verify}.txt`。未执行NEON原生、完整race、长矩阵、远端CI；没有push/tag/release。下一项优先G02残差逃逸，之后G03试写/邻域，再有界池和所有权；剩余SIMD任务仍保留。

### G02：残差量化与共同444重建去逃逸

起点`11b902e`，保留原有05/08/README/13文档修改。本任务只改encoder/inter.go、chroma.go及相关验证，不动transform量化公式、死区/候选策略、共享SIMD分发或线程。先用encoder的-m=2与G01的独立memprofilerate=1逐行归因，确认局部res经量化函数变量逃逸，420/422色度coeff/had亦有反复分配；原analyzeIntra经色度调用也保守逃逸。改为直接具名调用原4/8/DC量化器，不增加池、不隐藏指针、不改变返回残差的独立值语义。

首次零分配测试发现共同444的重建仍有2次分配，失败日志`g02-targeted.txt`原样保留。中间-m=2把reconstructChroma444的局部plane经reconstructInter递归图保守逃逸定位出来；提取唯一reconstructInterLuma，普通入口和共同444均调用它，保持4/8块顺序与同一共享反量化/逆变换。中间分配诊断日志也保留，未降低测试要求。

在算法修改前以新测试在11b902e采集384组完整系数/CBP/重建采样SHA：`bb4e1a6cc1f3fc24cbd8f4567634d6765add07174b8d105f5c6f065da39f84e0`，修改后逐项聚合不变，无更新开关。六原生格式、frame/真实bottom-field、flat/自定义矩阵、QP最小值/26/51、4/8变换、两死区、共同/独立444均覆盖。TestAllocation_InterResidual含每次预测/CBP恢复和真实重建为0分配；TestAllocation_InterCandidate在同矩阵运行四个实际分区、真实搜索/MC/残差/重建也为0分配。RD capture/整个模式决策仍因Writer/邻域分配而未满足完整K18。

五次prepared微基准12场景原420为7072B/11次、422为7616B/19次、444为19584B/3次，均降为0；耗时全部中位降低，见performance。相同固定1x端点：P中位864596000→847895700ns、293497608→13371880B、778286→342528次；B四帧/op中位1979717200→1894669700ns、735043024→47529072B、2020629→951160次。完整样本、配置、输入SHA、源码状态和日志SHA见`docs/gates/m8_residual_samples.json`。profile单独测量，含预热不能与计时窗口混算；原残差相关flat栈消失，剩余writer/blockSamples/picture/metadata。没有自然GC/assist/暂停数据，不宣称其下降百分比，也不签30FPS。

定向真实候选/指纹、goavc_simdtest强制Generic/SSE2/AVX2、purego encoder、386 encoder实际运行、六OS/架构全库build、完整常规gofmt/vet/build/test/depscheck/55条manifest/diff-check均退出0；日志`tmp/m8-baseline/g02-{targeted-complete,parity,purego,386,verify}.txt`。NEON本机不可用明确Skip，交叉构建不替代原生。全库race在本次集成后单独运行，实际结果另行追加。下一项G03试写/邻域复用，剩余SIMD/池化/并行仍待交付。

### G03a：帧内邻域固定槽与正常候选可用性预检

起点`bb3c7d8`，保留用户05/08/README/13工作。blockSamples追加调用者私有`*blockSamplesScratch`，实际analyzeIntra/chroma及包内测试一次迁移；槽为Top/Left各16个uint16，返回slice借用到下一次填充前，不在frameState放单一共享槽。同存活邻域使用独立槽，不可用部分清零保持原make的零值，仍按实际采样几何返回8/16长度和FieldView采样stride。

改造前在bb3c7d8捕获1152个I4/I16/I8候选完整系数、CBP、预测模式和重建采样SHA `8e5b5258d07d30ef0509e47f4306e30a51f8447e743f85a54beba064618da41b`，改造后不变。384组六格式/场/矩阵/QP/共同独立444配置每组跑三个family，真实非空候选的AllocsPerRun均0。邻域独立解析测试覆盖六格式×frame/真实场×slice起点0/1/2/3×三平面，检查可用性/有效长度、精确采样、不同时覆盖和复用清零。

首次固定数组后仍有8次分配；临时尝试具名色度预测未改善，已撤回该无效改动。独立memprofilerate=1定位到原I16/色度枚举不可用方向时构造的fmt.Errorf；将相同top/left/tl写前判断移到调用前，跳过原本不可用的模式，未删去任何合法候选、未改顺序/公式，错误分配清零。中间8次/4次失败日志与profile原样保留；没有修改断言掩盖问题。

五次prepared I4/I16/I8中位分别14079→13450、9193→8148、19333→18982ns，640B/20次、640B/20次、368B/14次均降为0。固定1x P中位847895700→870585400ns（+2.7%）、13371880→11029600B、342528→212910次；B四帧/op中位1894669700→1907183200ns（+0.7%）、47529072→40983320B、951160→577199次。保留端点耗时回退，不虚称FPS提高；原始样本、配置引用、边界和SHA见`docs/gates/m8_intra_workspace_samples.json`。仍有captureRD及正式MB试写/列表分配，未完成G03/K18。

定向指纹/分配/邻域、强制Generic/SSE2/AVX2、purego/386 encoder实际执行、六目标全库build、完整常规门禁与encoder race均退出0，日志`tmp/m8-baseline/g03a-{targeted-success,parity,purego,386,verify,race}.txt`。普通CGO=0，race显式CGO=1。之前G02代码的完整`go test -race -count=1 ./...`也退出0（tests/codec 305.297s），完整日志`g02-race.txt`与SHA已登记；该结果只对应G02代码，不能扩大为未来并行实现通过。NEON原生、自然GC、主目标长矩阵及远端CI仍未执行。

### G03b：RD/正式MB试写、有效位追加与CAVLC写前计划

起点`9a126d2`。先通过gopls核对Writer.Bytes和旧appendBits引用（首次行号不正确已修正，未把失败当通过）。真实调用者只有encoder/intra、inter的正式MB追加；保留公共Bytes独立副本契约。新增`(*bitstream.Writer).Append(src *Writer) error`复制全部有效位、不补尾位、不改源，拒绝自身/nil源/浅拷贝共享backing且错误前不写出，无unsafe。对齐与非对齐路径均支持；512种独立位解析值、源继续写不覆盖目标、别名和Reset后别名拒绝、容量足够时0分配通过。

Encoder每串行lane新增一个trialWriters，RD和正式MB各有独立Writer。newFrameState接入、场/独立平面channel借用同一指针；候选前Reset，实例Reset保留容量并清位状态，Close解除指针。旧逐MB NewWriter/Bytes追加包装删除，两个family动态append列表改为相同1/2/可选3顺序的整数循环。最终slice/Packet仍独立，不借用用户输出。

首轮完整决策零分配测试失败，普通I/P/B仍有31/175/36次分配（无8变换）。profile定位到CAVLC每块为事务保证新建临时Writer，而非剩余残差或模式对象。将原level区间计算提取为唯一prepareLevel，固定[16]levelPlan先验证所有level/前缀限额，再按旧序提交token/sign/level/zeros/run。没有第二套量化/熵公式、不改合法域和ErrUnsupported/ErrInvalidData分类，保留整块失败原子性。五类CAVLC块在首/尾非法level和非字节对齐目标下均无部分写出；RD错误后复用与全新Writer逐位一致。原失败及高精度profile日志保留。

算法改动前在9a126d2采集192个六格式×frame/真实场×4/8开关×I/P/B（含共同/独立444）的真实决策SHA `9b37334a5878629110a85858983c82d323913c16a40080ca44da76f4ee644115`，涵盖完整系数/CBP/像素/模式/MV/QP与rate/distortion/cost。所有胜者均为非Skip、非PCM、非空压缩残差。改造后指纹和96帧码流/元数据/三方像素不变，AllocsPerRun=0。此完整决策分配场景为QP26/flat，矩阵/低QP/码控及K16所有权/资源集成门禁仍待补齐，不将本切片签为完整K18。

五次微基准I/P/B中位60982→59236、329115→307734、30444→28795ns，64/316/46次降为报告取整0次。同一基准源码计时包含首个Writer容量增长，P改造后摊销2B/op，不能凭取整称零分配；确定性证据来自预热后的真实任务AllocsPerRun。固定1x端点P中位870585400→805327100ns、11029600→4945520B、212910→42次；B四帧/op中位1907183200→1852161900ns、40983320→26583624B、577199→221次。全部样本、边界、配置引用及SHA见`docs/gates/m8_trial_workspace_samples.json`。

独立memprofilerate=1 profile仍包含输入/预热/关闭，不能与计时窗口B/op混算。原window/residual/blockSamples/CAVLC事务缓冲反复分配已消失，剩余为picture.allocPlane、newFrameState/Grid、slice位串扩容及独立输出；保留profile以供G04/G05/G07。没有测自然GC、assist、pause、RSS，不能将分配降幅称作GC耗时/RSS降幅。

bitstream/CAVLC/encoder定向、goavc_simdtest强制Generic/SSE2/AVX2、purego/386实际执行、六目标全库build、完整常规门禁均退出0；日志`tmp/m8-baseline/g03b-{kernels,packages,targeted-final,parity,purego,386,verify}.txt`。本次集成代码的全库race及fuzz smoke结果在后文追加；普通命令CGO=0，race按仓库例外CGO=1。原用户05/08/README/13文档工作保留不提交。剩余G00自然GC、G04清尾/G05有界池、G06公共Frame、G07/G08所有权/元数据、其余SIMD和并行仍未完成。

G03b补充验证：完整`CGO_ENABLED=1 go test -race -count=1 ./...`退出0，tests/codec 291.726s（`g03b-race.txt`）；随后普通CGO=0 fuzz smoke：decoder 5s/12854exec、P inter 5s/1634exec均通过。B重排5s首次在797exec发现合法AU预算错误，原失败输入/log完整保留，处理见下一段；不把失败次数写成通过。

### 独立修复：B fuzz 的 Level 预算假设

最小输入`[]byte("0\x00!1 \xfb \x8b0")`在旧9a126d2对照树同样复现`access unit 1006 exceeds level budget 1000: ErrResourceLimit`，不是G03b回归。该fuzzer原本要求所有随机低QP/自定义矩阵输入完成十帧重排，却使用会合法拒绝超限AU的Auto Level。修正仅测试配置为显式level_idc=31，保留原样随机输入/QP/工具和所有像素、DTS、所有权断言，不改生产Auto策略/限额、性能基准或96帧golden。最小输入移入第四条f.Add，始终作为普通回归种子执行；原Go fuzz文件副本保留于`tmp/m8-baseline/g03b-b-fuzz-failure.seed`，不新增伪视频manifest条目。

旧代码加相同测试修复后四种子全部通过，证明修复独立于优化；改造后B fuzz重跑5s/835exec通过。最初误用不存在的Level常量/类型导致两次测试编译失败，已按实际`EncoderConfig.Level int`修正为31，未记为通过。日志为`g03b-b-fuzz-before-repro.txt`、`g03b-b-fuzz-harness-before-valid.txt`、`g03b-fuzz-b.txt`（原失败）、`g03b-fuzz-b-pass.txt`（有效重跑）。该修复与性能变更分开提交。

临时对照树在一次后置verify期间尚在tmp，被depscheck正常扫描为70个包并触发4处R3失败；未放宽白名单。对照树已恢复/确认干净后移除。随后`g03b-verify-clean.txt`因PowerShell把Git的CRLF提示当终止异常而中止；仅规范本次文档为LF后，最终完整门禁`g03b-verify-success.txt`退出0，35包依赖及55条manifest通过。新种子`g03b-b-seeds-race.txt`退出0。执行Go门禁前必须先移除仓库内的对照工作树。独立fuzzer修复提交为`dcce7ed`。

已核对并逐文件清理本次及前轮M8生成的41个测试二进制，共202867376字节；只删除已列出的tmp/m8-baseline内文件，不递归清空tmp，不删除profile/样本/失败证据。删除清单`g03b-cleanup.txt`，当前只有主工作树。profile后续可用保存的mem文本符号或按revision重新构建二进制分析；不声称旧exe仍在。没有push/tag/release。

### G04：清除无效引用与容器尾槽

起点为 `b0d9615` 的 G03b/B fuzz 修复交接点；保留工作区中既有的 05/08/README/13 文档修改。本任务只处理已经确认的容器留存，不改变编码策略、DPB 语义或输出所有权。

- `internal/picture.Pool.Get` 弹出空闲图像前清零 backing slice 的已消费槽；最后一张图取出后删除空桶，避免池 map 和底层数组继续持有已借出图像。
- `internal/dpb.remove` 在释放图像后解除 `Motion`、`BlockReferences`、独立色平面运动/引用、MBAFF 场标记和派生场缓存；若删除当前 `d.ref`，同步清除该指针；压缩 reference 列表后清零尾槽。`Identity` 保留为其他共位引用可借用的轻量身份，不提前清空。
- `nal.Assembler.takePending/reset` 在复制独立 AU 后清掉已消费的 `Unit` 槽，并保留 `takePending` 到调用方完成边界判定所需的 `last/lastInfo` 语义；直接 reset 也解除 pending payload。
- `decoder.SendPacket` 完成一次 `decodeFrame` 后清空本次 `entries` 的全部 slice/header/RBSP/分区引用，再复用切片容量；不跨公开调用保留输入数据。

新增/扩展行为测试：`TestPool_GetClearsReleasedSlotAndEmptyBucket`、`TestReferences_RemoveClearsRetainedState`、`TestAssembler_ClearConsumedUnitSlots`、`TestDecoder_ClearSliceEntries`。测试检查池租约平衡、DPB 图像与 retained metadata 解除、NAL 独立 AU 不受影响，以及 decoder slice 容器清零。

定向命令 `CGO_ENABLED=0 go test -count=1 ./internal/picture ./internal/dpb ./nal ./decoder` 退出 0。该任务没有新增性能样本；自然 GC、scan/assist/pause、RSS 和稳态留存实验仍未执行。G05 有界缓存、G06 公共 Frame backing、G07/G08 所有权和 K19/K20/K20m 并行仍未完成。

### G05：有界 picture 空闲缓存

起点为 G04 提交 `6a0cba0673`。本切片只扩充既有 `internal/picture.Pool`，不新增第二套图像池，不改变 `Get/Release/Stats/Trim` 的既有调用契约，也不把缓存预算解释为活跃内存或 RSS 上限。

- 每个 Pool 的空闲 backing 总预算固定为 64 MiB；原有每个尺寸/格式桶的 `maxFree` 上限保留为 4（`NewPool` 的 `<=0` 默认值仍为 4）。
- 账本按三个平面的实际 backing capacity 计费：8-bit `cap(Data)`，10-bit `cap(Data16)*2`；不按显示尺寸估算，不跨 `PixelFormat` 或编码几何混桶。
- `Get` 命中时扣除实际字节并清除空闲槽；`Release` 超过单桶或全池预算时仍完成合法归还，但不缓存；`Trim` 同步清空字节账本。新增 `CacheStats` 仅报告空闲 backing 字节、非空桶数和图像数，保留原 `Stats` 三返回值。
- 大于缓存预算的合法图像不会返回新的资源错误；该缓存只限制空闲留存，借出图像和当前工作图像不计入空闲预算。

新增 `TestPool_BoundsFreeStorageAndCacheStats`，验证实际 capacity 计费、命中扣账、超预算拒收和后续重新分配；既有多格式/尺寸隔离、复用、并发测试继续保留。`CGO_ENABLED=0 go test -count=1 ./internal/picture` 与 `CGO_ENABLED=1 go test -race -count=1 ./internal/picture` 均退出 0。

本切片没有新增吞吐或 GC 样本；64 MiB 是空闲缓存预算，不是活跃内存/RSS承诺。G06 公共 Frame backing、G07 encoder 图像/RC 所有权、G08 decoder retained metadata、G09 AU 容量和 G10 集成验收仍待完成。

### G06：公共 Frame backing 与输入输出迁移

起点为 G05 提交 `d81de411a8`。本切片实现根包 `NewFrame` 和标准库 `internal/buffer` 原始 backing 池，并迁移公共 Frame 的真实输出/输入路径；不改变 Frame/Packet 的公开所有权语义。

- `NewFrame(width,height,format)` 先校验正尺寸、8192 上限、六种格式和每个平面乘法；返回新的 Frame 头、紧凑 byte 平面和清零有效区域。10-bit 继续使用公共 LE byte 存储。
- `internal/buffer.Pool` 只管理三平面原始 `byte` backing；Frame 池按几何/格式 key 分桶，空闲总预算 64 MiB、每 key 4 个槽、最多 8 个非空 key。超预算合法归还直接丢弃，不返回新的资源错误。
- `Frame` 头不复用。租约 callback 捕获原始 key/backing，并用 `sync.Once` 防止浅拷贝的多个 Release 重复归还；修改当前 Frame 的 `Planes` 不改变原始租约。Clone 仍为独立非池化分配。
- `decoder.toFrame`、`encoder.Reconstruction`、`encoder.copyQueuedFrame`、B 输入队列和 `stream.YUVReader.ReadFrame` 均迁移到 `NewFrame`；拷贝/读取失败会 Release 已取得的 Frame，错误签名沿调用者传递。

新增 `TestNewFrame_BoundedPooledLifecycle`、`TestNewFrame_RejectsInvalidGeometryAndFormat`、`TestNewFrame_ShallowCopyReturnsBackingOnce`。迁移后的首轮测试发现 YUVReader 丢失原有 High10 值域检查，`TestYUVIO_High10KnownLayout` 复现“非法 10-bit sample accepted”；恢复 `Frame.Validate` 后原失败用例通过，未放宽 0..1023 约束。

验证：`CGO_ENABLED=0 go test -count=1 . ./stream ./decoder ./encoder`、`CGO_ENABLED=1 go test -race -count=1 . ./stream ./decoder ./encoder ./internal/buffer`、以及提交前完整 `gofmt/vet/build/test/depscheck/manifest/diff-check` 均退出 0；常规门禁为 CGO=0，race 按仓库例外使用 CGO=1。当前未把公共 Frame 头、租约和清零成本记为 kernel 零分配，也未宣称 RSS/GC 已下降。G07 encoder 图像/RC 所有权、G08 decoder retained metadata、G09 AU 容量和并行仍待完成。

### G07：encoder 工作图像与 RC 候选所有权

G07 在 `Encoder` 内增加实例私有 `picture.Pool`，源图、候选重建图和最终 reconstruction 均按明确 owner 管理；不改变 QP 搜索顺序、tie-break、码流或公开 Encoder 非并发契约。

- `encodeFrameNow` 从实例池获取源 `Picture`；RC 返回候选后，源图只在候选重建不同于源图时归还，AU 超 level budget 或编码失败均归还源图和候选。
- `encodeRateControlled` 对快速搜索和穷举中的 Unsupported、ResourceLimit、padding、AU 超限、VBV 拒绝候选立即释放；新最优候选替换旧最优候选前释放旧 `frameState.pic`，最终候选所有权转移给 `e.recon/e.last`。
- `frameState.ownsPic` 区分新分配的重建图与 MBAFF 借用的 FieldView；连续非 B 帧及 B 调度完成后会归还不再被 `recon/reference/bPast/bFuture/last` 引用的旧图，`Reset`/`Close` 再按地址去重清理 retained 图像，避免同一池图像在重新借出后重复 Release。

新增 `TestEncoder_RateControlCandidatesReturnPictures`，断言 ABR 候选搜索成功后只有最终 reconstruction 借出，Reset 后 `gets == puts`；扩展 `TestEncoder_LevelBudgetIsTransactional` 与 B 输入失败测试，断言拒绝路径不遗留 Picture。`CGO_ENABLED=0 go test -count=1 . ./stream ./decoder ./encoder`、`CGO_ENABLED=1 go test -race -count=1 ./encoder` 和提交前完整 `gofmt/vet/build/test/depscheck/manifest/diff-check` 均退出 0；race 仍只覆盖 encoder 包，非全库 race。

当前 G07 未把自然 GC/RSS 或候选分配 before/after 记为性能证据；编码错误发生在底层语法函数已创建状态之前的极少数路径仍由后续完整错误矩阵覆盖。G08 decoder retained metadata、G09 AU 容量和 G10 集成验收仍待完成。

### G08：decoder 失败状态与 DPB 所有权

G08 保留现有 DPB/输出顺序和公共 Frame 生命周期，补强失败路径的状态清尾：`frameState.release` 归还唯一 `framePic` 后清空 SPS/PPS、FieldView、残差、邻域、运动和 reference-ID 切片；`Reset` 继续先释放输出/重排队列，再重置 DPB，确保 DPB retained Picture 回到 decoder 私有池。

新增/扩展 decoder 生命周期测试：损坏输入进入等待 IDR 后不输出失败 P 图；IDR 输出 Release 幂等且 Clone 独立；`Reset` 后断言 `d.pool.Stats()` 的 `gets == puts`，不保留 DPB Picture 或旧参数集。该切片未改变 `toFrame`、DPB Store、POC 或 reorder 行为。

验证：`CGO_ENABLED=0 go test -count=1 . ./stream ./decoder ./encoder`、`CGO_ENABLED=1 go test -race -count=1 ./decoder` 和提交前完整 `gofmt/vet/build/test/depscheck/manifest/diff-check` 均退出 0；常规门禁为 CGO=0，race 按仓库例外使用 CGO=1，未执行全库 race。未把元数据清空解释为完整自然 GC/RSS 证据；G09 AU 容量和 G10 集成验收仍待完成。

### G09：AU 容量与粘性失败状态

现有 `AUReader`、Annex-B/AVCC Writer、pipeline 和 NAL Reader 已在分配/封装前校验单 NAL、NAL 数量、输入 AU 与输出 AU 容量，并保持 Packet.Data 独立归调用方。本切片只补齐 `AUReader.fail` 的失败状态清零：丢弃 assembler、重置 `size/hasVCL/closed`、SPS/PPS 和最后 slice 元数据；粘性错误仍阻止再次读取，不改变合法封装字节。

扩展 `TestAUReader_ErrorsAndBudgets`，验证超限/语法/IO 失败不泄露部分 Packet，失败后容量与 assembler pending 均为零，重复读取返回同一错误且不再次访问源。`CGO_ENABLED=0 go test -count=1 . ./stream ./decoder ./encoder`、`CGO_ENABLED=1 go test -race -count=1 ./stream` 和提交前完整 `gofmt/vet/build/test/depscheck/manifest/diff-check` 均退出 0；常规门禁为 CGO=0，race 按仓库例外使用 CGO=1。G10 集成验收仍待完成。

### K10a：SSE2 4×4正变换

用户将后续内存/GC工作交给其他agent，本任务只处理SIMD。起点HEAD为`1e4effe`，保留已有05/08/README/13文档及picture/pool.go未提交改动；不修改池化/所有权代码。用gopls核对Forward4x4全部调用者，原领域签名保持不变，将唯一标量两轴算术迁入simd，增加固定`[16]int32 → [16]int32`值参数槽，避免局部残差经间接调用逃逸。领域只适配，无重复算法。Auto在amd64选择SSE2该槽；即使SAD/SATD显式AVX2，此槽仍如实诊断SSE2；其他架构/purego为Generic，不把回退记为向量通过。

SSE2使用32位加减/倍增、两次4×4转置，输入/输出ABI数组各64字节，只访问0..63，MOVOU不要求对齐。有效残差±1023，单轴绝对值<=6138、双轴<=36828，不饱和窄化。全int32域还保持原Go模运算。独立矩阵正负冲激含低两位/极值、seed0x1044的5000组合法/宽域逐元素parity通过；原地与领域局部输入AllocsPerRun=0。没有触碰IDCT宽域/溢出契约、8×8投影或量化策略。

同机五次直接内核中位Generic14.95ns、SSE2 10.44ns；真实transform入口中位15.53ns与15.13ns，均0分配。额外函数指针包装中位20.09/21.00ns，包含不同复制/调用开销，完整保留，不以直接内核数字替代实际consumer。固定1x P端点中位887134400ns→937174000ns（约+5.6%），触发复查，复查中位890776600ns；样本范围明显波动，不能声称稳定端点加速。所有样本及SHA见`docs/gates/m8_forward4_samples.json`。当前数据不作为30FPS或K23/K24证据。

CGO=0的simd/transform定向、goavc_simdtest强制真实后端、purego simd/transform/encoder、实际386同包、六目标全库build和完整常规门禁全部退出0（`k10a-{tagged,purego,386,verify}.txt`）。原编码器/六格式/M8固定码流、Oracle、候选零分配保护网继续通过。新增consumer benchmark在同一测试标签编译运行；AVX2/NEON Forward专用路径明确未实现，不算这些后端通过。受影响包race/短fuzz实际结果另补。不宣称完整K10或M8；剩余SIMD MC、其他正逆变换、AVX2/NEON、量化、滤波和残差仍待推进。

K10a补充：CGO=0帧间编码fuzz smoke 5s/1432exec退出0；CGO=1 simd/transform/encoder race退出0（k10a-fuzz.txt、k10a-race.txt）。完整全库race仍留给最终集成验收。

### K10b：NEON 4×4正变换编译交付

起点`eb882a1`，只增加forward_arm64声明/汇编/测试和ARM逐槽分发；沿用K10a唯一Generic与领域适配，不改内存GC工作。基线ASIMD四条S4向量完成两轴32位加减/倍增及转置，输入/输出各固定64字节；与SSE2同一C基底、顺序与int32模运算，没有窄化、可选扩展或新的输入域。NEON槽已绑定实际计算，原生解析值/5000组parity/benchmark入口复用checkForward；不会把Generic结果标为NEON。

按用户arm64只需可编译的范围，CGO=0 windows/linux/darwin×arm64分别go vet internal/simd、go build ./...、带goavc_simdtest的simd测试二进制编译。无原生arm64，未执行NEON parity/分配/Oracle/性能，状态WIP。宿主purego simd/transform及完整常规门禁另验证；完整K10仍缺AVX2、逆变换、8×8等，MC/量化/滤波/残差也仍待实现。不签M8、不push/tag/release。内存agent与用户原有文件原样保留。

### K10c前置：Forward8x8共享 Generic 迁移

将 `internal/transform/eight.go` 中唯一的 Forward8x8 正变换实现迁入 `internal/simd/forward8_generic.go`，领域层保留原签名适配；没有新增伪造的 SIMD backend 槽。该迁移保持 `[64]int32` 原地契约、int64 中间域、基底/舍入和输出顺序不变。`CGO_ENABLED=0 go test -count=1 ./internal/simd ./internal/transform ./encoder ./decoder` 通过；AVX2/NEON Forward8x8、逆变换及 K10 完整边界矩阵仍未交付。

### K10d：4×4/8×8逆变换共享 Generic 迁移

将 `IDCT4x4`、`IDCT4x4DC` 和 `IDCT8x8` 的唯一算术实现迁入 `internal/simd/idct4_generic.go`、`idct8_generic.go`，`internal/transform` 仅保留错误/公共 API 适配。保持原地别名、算术右移顺序、int64 临时域、8×8 溢出分类和不裁剪残差语义。受影响 transform/encoder/decoder 测试、purego 测试、全库 vet/build/test、depscheck 均通过；真实 AVX2/NEON 逆变换后端、量化/去块/残差和 K14 完整边界矩阵仍未完成。

### K10–K14：精确算术后端、共享消费者与全边界矩阵

本轮限定SIMD，不扩展K09插值、池化、并行或Profile支持。保持现有8×8整数正交投影，不改用另一种前向蝶形；正逆变换、量化、重建和滤波仍由encoder/decoder共享一份领域实现。状态表继续使用Partial/WIP，不把本地任务交付当作完整M8退出门禁。

- K10：4×4新增真实AVX2行蝶形；8×8投影以显式SSE2/AVX2/NEON精确乘积批次执行，宽域回退原int64乘法；逆变换和亮度/420/422 DC以64位向量加法部分计算。保留负数算术shift、int64临时域、原地别名、422整块提交与IDCT8晚期溢出的原列优先写入序。除法、归一化、检查和部分蝶形仍为共享Go，不宣称全程SIMD。
- K11：领域按QP'、矩阵和块类型准备乘子；普通块用直接Go指针入口，间接SIMD调用只传固定值批次，避免局部系数逃逸和整块返回复制。精确32×32→64乘积之后仍执行原整数除法、intra/inter死区和符号恢复；DC宽域保持原int64。Quant8及普通Dequant保留首个错误之前已写的前缀，原地失败时未写后缀不变，不虚构整块原子性。
- K12：唯一共享亮度/色度滤波算法迁入simd，SSE2/AVX2/NEON仅计算当前line的强弱候选加法；bS、阈值、条件邻域读取、Span1/2/4和逐线顺序不变，不把整条边gather后并行提交。
- K13：picture重建与encoder真实Subtract接入共享8/10-bit Clip1/减法入口；使用int64累加覆盖int32极端残差，保持10-bit低两位、独立stride、padding和尾部。标量路径不为禁用的向量槽做gather，行切片只在入口验证边界，热循环不反复计算行偏移。R3只精确新增picture消费者。
- K14：Windows逐行保护页扩展至现有SAD、SATD、正逆变换、量化/DC、滤波、残差和MC；MC覆盖亮度16相位与色度64相位、8/10-bit和最小len。补Unix mmap辅助；本机只执行Windows动态页，其他系统不冒充运行通过。值ABI的栈参数由静态访问范围约束：四通道int32加法输入/输出各16字节，八通道int32或四通道int64/uint64各32字节，Forward4输入/输出各64字节，AVX2行蝶形输入64/输出128字节；均无对齐要求。外部保护页不等价于保护ABI内部栈槽。

#### 已定位并修复的缺陷

1. SSE2的420 DC包装把水平/垂直系数1、2交换：相邻和差批次应按`[p0+p2,p4+p6,p0-p2,p4-p6]`提交。原生直接parity暴露失败；`TestForwardChromaDC_AnalyticMixedSigns`用独立解析值`[1023,-1023,511,-511] → [0,3068,0,1024]`、反号、低两位及int32模运算固定回归，Generic/Auto/SSE2/AVX2通过。
2. MC函数表中的slice间接调用使边界patch、单采样输出逃逸，即使purego的槽为空也影响静态逃逸，传导为MC每次1次、搜索约17次及编码候选更多堆分配。编译器`-m=2`直接证明调用链；删除未实现的uint16空槽和动态复制槽，改初始化后端选择加静态noescape汇编包装。现有prediction/search/encoder零分配测试在purego、386与强制x86后端全部恢复，不放宽断言。
3. 量化块值返回和小批gather使普通量化/残差明显慢于原标量。改为直接指针块接口和标量快路，保留真实显式后端；首错与原地后缀用`TestDequantization_FirstFailurePreservesPrefix`、`TestQuant8x8_LateFailurePreservesAliasedSuffix`锁定。
4. 汇编vet拒绝把数组符号直接当不同宽度的FP标量或不同偏移字段引用；改为先取得准确ABI数组首址，再做有界非对齐加载/存储，不关闭vet。交叉构建发现缺失`forward8NEON`适配，补接真实NEON乘积批次，非Generic假绑定。

#### 已执行的功能验证

日志均位于`tmp/m8-k10-k14/`，保留初次失败日志，不以失败/跳过算通过。

| 验证 | 实际结果 | 原始日志 |
|---|---|---|
| 强制Generic/SSE2/AVX2内核、transform解析值及真实encoder候选 | 通过；NEON在本机明确Skip | `tagged-final.txt` |
| 默认路径六格式、双熵、QP边界、矩阵/B/码控/独立444及8×8三方Oracle | FFmpeg=Go Decoder=本地重建通过；独立色平面沿用既有投影Oracle边界 | `oracle-final.txt` |
| M7固定编码Packet/metadata/像素指纹 | 六格式×双熵的96帧保持不变，未更新golden | `oracle-final.txt` |
| purego全库测试 | 修复MC逃逸后全部通过 | `purego-final.txt` |
| 原生GOARCH=386全库测试 | 修复MC逃逸后全部通过，含保护页 | `386-final.txt` |
| Windows/Linux/macOS × amd64/arm64 | 全库build、simd汇编vet、带测试标签的simd/transform测试二进制编译通过；非宿主不执行 | `cross-final.txt`中已成功的windows/amd64及`cross-rest.txt`五目标 |
| 既有decoder fuzz smoke | 5s预算，1280次执行，通过 | `fuzz-decoder.txt` |
| 既有encoder inter fuzz smoke | 5s预算，329次执行，通过 | `fuzz-encoder.txt` |
| 常规完整门禁 | CGO=0的vet/build/test、gofmt、35包R1/R2/R3/R5及55条manifest全部通过 | `verify-final.txt` |
| 完整全库race | CGO=1、GOMAXPROCS=2、`go test -race -count=1 -timeout 45m ./...`通过 | `race-final.txt` |

arm64沿用用户指定的编译验收范围，未运行NEON原生parity/guard-page/分配/Oracle/性能；Unix动态页同样未执行。没有push、tag或release，不以本轮结果声明完整M8或30FPS。


#### 最终性能与默认分派

完整race结束后单独执行最终代码基准，环境为Go 1.27.4、Windows/amd64、Ryzen 7 5700X，benchmark默认并发参数为16。命令：

`CGO_ENABLED=0 go test -run '^$' -bench '^(BenchmarkTransformBlocks|BenchmarkQuant4x4_4x4|BenchmarkFilter_4Lines|BenchmarkResidual_8x8|BenchmarkDequantization_8x8)$' -benchtime=50ms -count=5 -benchmem ./internal/simd`

下表为同环境五次中位数，单位ns/op；每项五个原始样本均保留在`bench-final.txt`，全部0 B/op、0 allocs/op。标量比较器为同次运行的`ScalarBefore`实现，并非旧revision整端到端测速。

| 实际块入口 | 标量比较器 | Generic | Auto | 显式SSE2 | 显式AVX2 |
|---|---:|---:|---:|---:|---:|
| 4×4正变换 | — | 20.93 | 17.74 | 17.98 | 32.12 |
| 8×8正变换 | — | 1173 | 997.6 | 12113 | 12959 |
| 4×4逆变换 | — | 37.61 | 30.45 | 198.3 | 211.3 |
| 8×8逆变换 | — | 475.1 | 427.5 | 1013 | 1089 |
| 亮度DC正变换 | — | 36.59 | 33.23 | 184.4 | 206.5 |
| 420 DC正变换 | — | 8.287 | 7.895 | 18.74 | 42.24 |
| 422 DC正变换 | — | 14.32 | 14.17 | 57.64 | 62.42 |
| 4×4量化 | 24.76 | 21.46 | 21.74 | 211.6 | 233.1 |
| 8×8反量化 | 96.14 | 77.3 | 77.47 | 836.7 | 883.1 |
| 8×8残差加法/uint16 | 95.63 | 82.67 | 100.1 | 248.5 | 284.1 |
| 8×8残差减法/uint16 | 38.13 | 71.04 | 76.94 | 216.2 | 241.3 |
| 4条line弱滤波 | — | 121.4 | 115.8 | 135 | 146.3 |
| 4条line强滤波 | — | 106.2 | 102.3 | 115.4 | 125.5 |

amd64默认Forward4与SATD选择SSE2；8×8投影、逆变换/DC、量化、去块候选与残差保留Generic直接路径。显式SSE2/AVX2测试仍调用真实SIMD，不能将这些默认Generic结果作为SIMD加速证据。Auto和显式Generic对同一Generic槽的差异反映顺序测量波动，不是另一种算法；尤其残差减法仍慢于内联标量比较器，未计为加速收益。没有新增整端到端吞吐、RSS或GC性能声明。

初始小批量路径的明显回退与修正过程保留在`arithmetic-bench.txt`、`arithmetic-bench-fixed.txt`、`transform-bench.txt`和`blocks-bench-final.txt`；最终代码以`bench-final.txt`为准。其SHA-256为`7f34263a89210899a0d17ff73bc4ba828dc25f8b2619852c6eeb0cbeb50799bb`。其余验证日志摘要见`verification-sha256.txt`，race摘要另存`race-final-sha256.txt`。本轮生成的交叉测试二进制和单独汇编对象已清理，保留文本证据。

### K15 收尾：候选创建层失败回收

本轮从 `e34d882` 开始，按用户授权采用可行性评估后的范围，并严格按 K15→K16→K17→K18→K19→K20→K20m 顺序独立提交。K16 仅做有证据的串行临时存储复用；K19 先作多 slice 收益准入；K20 仅消除重复纯分析，不启用流水线；K20m 暂不实施，不将裁剪项标成实现完成。

K15 继续使用既有 Picture/Frame 池，不新增池或改变公共所有权。现有低 QP 拒绝后重试测试加入租约断言，修复前实际失败 `gets=2 puts=1`。普通 I/P/CABAC/B 创建层增加失败清理，成功才转移候选；RC fatal 退出清理此前最佳候选；场通道只回收自己拥有的完整 recon；独立平面改用实例池，整图成功转移，借用视图不独立归还。

新增组合测试通过公开 SendFrame 在后续场/平面拒绝 QP，检查失败不发布、重试成功只保留一个图像 owner、Reset 后池账本平衡。定向测试、`CGO_ENABLED=0 bash scripts/verify.sh` 完整门禁（35包依赖、55条manifest）及四类失败/重试用例的 `CGO_ENABLED=1 go test -race -count=1` 均退出0；日志 `tmp/m8-k15-k20/k15-verify.txt`。尚未测自然 GC/RSS，不把所有权修复解释为吞吐提升。


### K16 精简：串行元数据独占复用

起点 `45d74432b0`。decoder 的三个表示级 temporary slot 复用 decoded/info/NZ/DC/MVD/direct/FMO owners/共同444残差；完整覆盖检查、去块和扩边后归还，失败遍历所有已建平面，清除Grid对owner表的借用。Motion/reference IDs/MBField仍按原契约交DPB，不冒充retained metadata已池化。encoder 的 typed storage 独占 info/NZ/DC/MVD/Grid；attempt/best/committed及场通道不能共享同一租约，旧e.last仅归还元数据，不归还仍借用的像素。独立平面非首通道结束后回收，Reset保留有界空闲缓存，Close清空。

两侧分别按实际cap计费，空闲上限16MiB/最多三槽；合法活跃工作可超过该缓存预算。只以unsafe.Sizeof作编译期类型尺寸记账，不转换指针、不访问原始内存。未引入worker、公共API或通用对象池。

新增decoder六格式/FMO脏缓存完整像素、失败恢复、热初始化0分配和预算测试；encoder六格式/场/独立平面、RC best/attempt保持及活跃运动消费者测试。定向测试通过。五次原端点before/after中位：decoder三帧18710576→17384256 B、27851→27837次；encoder B四帧15991752→12854392 B、215→194次。时间与原始样本SHA见performance的K16节，不宣称GC/RSS或完整K16原始并行隔离门禁通过。

K16提交前 `CGO_ENABLED=0 bash scripts/verify.sh` 完整门禁退出0（35包依赖、55条manifest，包含既有三方Oracle与固定码流保护网）；新增decoder Temporary与encoder元数据消费者用例的 `CGO_ENABLED=1 go test -race -count=1` 退出0。日志 `tmp/m8-k15-k20/k16-verify.txt`。不是全库race或原生arm64验收。


### K17：正常 P/B 宏块分区去分配

起点 `cf87f09d87`。P/B 分区改为固定四组、总共十六个分区的栈 backing，组只持有不重叠的slice视图；不改候选、参考索引/MVD读取顺序、MC或残差算法。首轮B虽然固定数组仍有2次分配，编译器 `-m=2` 证明通过动态append组的指针字段赋值使partBacking/directBacking逃逸；改成静态初始化组视图、按规范上限索引填充后归零，不把数组搬到全局池。报告 `tmp/m8-k15-k20/k17-escape.txt` 保留。

新增 prepared 正常P/B宏块测试覆盖CAVLC/CABAC、8/10位、全部P/B分区与子类型、混合direct和空间/时间direct。实际解码非零残差并验证像素、MV、参考身份、NZ及读端终点，AllocsPerRun=0。混合temporal-direct首组的零MVD后继须继承非零MVP（Spec:8.4.1.3），首版测试错误期望零MV已按手算邻域修正，未修改生产预测或删减向量。

端点沿用K16后三帧冷基准作before。第一次after时间较早样本慢超过5%，按政策保留原样并复查；同环境从只读历史 `cf87f09` 导出的临时源码重新测量，旧中位122040300ns、新复查115726700ns，说明早期104716600ns不能直接当稳定对照。临时源码已清理。旧复查17384288B/27839次，新复查17275776B/25811次；不将本项解释为30FPS或自然GC/RSS达标。全部样本在 `tmp/m8-k15-k20/k17-{decoder-after,decoder-review,baseline-review}.txt`，比较边界及限制见performance。

本项只签已测P/B正常宏块去分配；没有把每slice初始化、MBAFF首次场引用、全部配置的完整K17或整个decoder零分配记为通过。

K17提交前CGO=0完整常规门禁与CGO=1的上述P/B/非法分区定向race均退出0；日志 `tmp/m8-k15-k20/k17-verify.txt`。after/review/baseline-review样本SHA-256依次为 `5adfc107434b858d25cfa7ac85606f8dab94f4563fb671126c1b2948b0d41eb4`、`3747741c085e8512a4f4f84e42694dd5752a2836a8724518f801e31e40c58172`、`e60e79d2084445b2d91a280255edfd0cb069c17473383796c340353946a29fd3`。

