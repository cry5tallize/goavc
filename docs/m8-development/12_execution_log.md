# M8 实施记录

本页记录实际任务、接口与证据；01–11 是原始设计输入，不代表实施完成。

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
