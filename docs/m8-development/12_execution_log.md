# M8 实施记录

本页记录实际任务、接口与证据；01–11 是原始设计输入，不代表实施完成。

## 当前交接点

- 分支 `perf/m8-simd`，A00、K01–K05、K07、K09a已逐项提交；K06/K08按用户要求完成实现与交叉编译，原生未验证。SAD与SATD已真实接入，MC当前是唯一Generic块算法，不是SIMD插值。
- amd64 Auto逐槽选择：SAD宽16为AVX2（其他宽SSE2），SATD4×4优选实测更快的SSE2；purego无项目汇编。没有全局“所有槽均AVX2”的声明。
- K14a已补Windows SAD逐行保护页；其余内核矩阵未完成。K09b/c/d、K10–K13、K15–K25及K20m仍待实施，K14收尾待所有内核交付。
- 当前已知成本：SAD间接调用导致搜索window逃逸，P控制场景约875.5k分配/op，须由K18的typed workspace复用解决；MC和SATD的已准备生产调用均0分配。
- 尚未执行完整race/代表性主目标长矩阵或远端CI；不创建标签、不push/release，不签完整M8。以下各节按实施时间保留当时的下一项与原始数据，最新状态以本节和状态矩阵为准。

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

新内部入口为 `simd.MCLuma[T ~byte|~uint16](dst []T, ds int, ref []T, rs,w,h,fx,fy,bitDepth int)` 与 `MCChroma[T](dst []T, ds int, ref []T, rs,w,h,fx,fy int)`；w/h≤16，stride均为采样，ref从精确halo左上角开始，调用者证明实际len。当前只实现唯一Generic，不提前填充SIMD空槽。领域scratch与算法中间缓冲均为调用私有，无新增全局可变数组。极端机器字MV仍走相同内核的单采样路径，保留旧逐采样int回绕/钳位语义；独立宽坐标解析测试在amd64及386执行。

旧MC指纹 `ff62c7838da485c15516101ba5438203a6320500d05f85c6383a458b0209bddd` 在改算法前采集，包括双位深、420/422/444、全部相位、负MV、非对齐/最小len、1/3/7/13/16/17/31矩形及未写padding。改后不变；既有规范解析值、场边缘和固定96帧三方像素/码流/元数据不变。`TestAllocation_MCBlock` 在预备图像/场上下文后运行实际三采样格式、双位深、内部/边缘块且首尾输出必写，AllocsPerRun=0；不能由此推定encoder模式决策已零分配。

最终常规门禁、`CGO_ENABLED=0 go test -tags purego -count=1 ./...`、386实际执行prediction/search/simd、六目标全库build全部退出0；27份High10投影显式用例通过。Fuzz Smoke实际执行decoder 46257次、encoder inter 1317次，均无崩溃；各5秒请求、2 workers，实际结束约6秒。日志 `tmp/m8-baseline/k09a-{verify,purego-all,386,high10-projection,fuzz-decoder,fuzz-encoder}.txt`。首次构建对齐了prediction已有~采样类型约束后重新验证，未删除失败用例。

同机五次MC微基准覆盖interior/border/真实bottom FieldView和双位深；旧源码的固定1x消费者在隔离clean worktree重测，再与最终代码同边界五次比较，旧中位2149919100ns、新中位916330100ns，约2.35倍。保留新首样本1393510500ns等波动，不挑最快值。原始样本、输入SHA及参数见 `docs/gates/m8_mc_samples.json` 和performance§15；原始日志保留于本地tmp，临时工作树已清理。不以此P控制场景替代代表性High/B主目标。

下一项：K07/K08与K09b/c/d仍待实施，K10–K25及K20m未完成；后续SIMD MC必须使用上述唯一Generic与同一边界证明，不能复制一套预测；K18必须解决已登记搜索window逃逸。当前可用SAD测试后端锁和领域parity入口，基准期间不并行跑长测。尚未执行完整race或原生arm64，不签完整M8门禁。

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
