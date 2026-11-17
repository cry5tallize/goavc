# B01–B05 性能优化交接

日期：2026-11-17。仓库 `E:\code\goavc`，分支 `perf/decoder-block-kernels`，HEAD `31d0308`。
用户要求每项完成后单独提交；不推送、不打标签。当前停在交接点，没有正在运行的B04测量进程。

> 接续更新：B04已独立提交`bce30ee`，B05a AVX2加权已提交`29f25d8`；B05开发及本轮集成已完成。B05按用户后续明确确认的本机范围验收，
> 没有arm64 runner不阻断本次完成；NEON仍未原生验证。以下为接手时历史快照，
> 当前实现、性能与本轮集成证据以`docs/gates/b05_results.md`及对应分项报告为准。

## 当前进度

| 项目 | 状态 | 提交与结果 |
|---|---|---|
| B01 Put/Avg融合 | 已提交 | `4d67c60`；普通8-bit420逐行双参考均值融合，完整MC核心快11.2%–20.4%，短端点持平 |
| B02 分数像素MC | 已提交 | `d0998c9`；8-bit 4/8/12/16宽亮度/色度全相位SSE2，关键格快26.6%–81.5%；端点补测持平 |
| B03 IDCT＋Add融合 | 已提交 | `31d0308`；共享4×4/有界8×8 Generic融合，普通核快11.5%–29.9%，短端点持平 |
| B04 CABAC缓存/扫描 | 实现与验证完成，未提交 | 最终短I/P/B补测已完成并归档，持平；还需最终审阅、补性能/状态文档并提交 |
| B05 AVX2/NEON/高位深 | 未开始 | 没有B05实现或性能数据，后续接手者负责 |

B01–B03报告和完整样本分别在 `docs/gates/b01_*`、`b02_*`、`b03_*`。
总体方向与门禁见 `docs/decoder_performance_plan.md`；先遵守 `AGENTS.md`。

## B04工作树：不要丢弃

生产文件：

- `bitstream/reader.go`：ReadBit直接消费已有reservoir；空缓存冷路径仍调用ReadBits，保留尾部原子失败与错误文本。
- `internal/entropy/cabac/decoder.go`：readBit/单比特renormalize改用ReadBit；逻辑消费位数每次立即更新，没有延后提交的私有游标。
- `internal/entropy/cabac/residual.go`：通用校验/清零与唯一算术解码流程分离，原通用API的非法scan清零前缀仍保留。
- 新增 `internal/entropy/cabac/prepared_scan.go`：拥有映射副本的ResidualScan，在构造时校验，支持460/1024上下文域。
- `decoder/cabac_prepared_scan.go`、`decoder/mbaff_scan.go`：固定表按身份选择prepared路径。
  **当前只启用4/15/16项；8与64项继续通用校验。** 未知/自定义scan也走原API。

新增测试：

- `bitstream/reader_bit_cache_test.go`：ReadBit与ReadBits(1)混合Read/Peek/Skip/Align的值、错误和位置对照。
- `internal/entropy/cabac/cache_trace_test.go`：冻结旧逐位引擎，逐状态比较bin、context、range/offset、lastBit、终止、错误、消费位数及后续Peek。
- `internal/entropy/cabac/prepared_scan_test.go`：prepared/通用路径、截断、14类别与帧/场对照及基准。

## B04性能结论：唯一补测已结束，不必再跑

完整数据：`docs/gates/b04_samples.json`；概要：`tmp/b04/summary.txt`。

- 最终IDR：34928512→34576587ns/op（-1.0%，持平）。
- 最终I/P/B首轮：833243800→867562700ns/op（+4.1%）。
- 刚才中断时的唯一补测已在后台完成：**881802600→882011900ns/op（+0.02%，持平）**。
  五对日志为 `tmp/b04/repeat-pair-ipb-{1..5}-{before,after}.txt`，已写入JSON的repeat。
- 原始完整残差基准15/16项快4.0%/7.2%，64项持平。这个基准每op重建Reader，
  **两侧均64B/1alloc**，不是新增的扫描分配；不要报告成0alloc。
- 辅助同版本比较改为复用Reader/Decoder，0B/0alloc，200ms×5；见
  `tmp/b04/scan-compare-reused.txt`及JSON的auxiliary_scan_comparison。
  4/15/16项中位分别284.5→241.7、463.7→366.8、537.2→516ns/op；
  8项397.1→388.1，收益未确认；64项921.3→1158，变慢，故未接入8/64项默认prepared路径。
  最初带Reader分配的辅助比较也保留为 `scan-compare-allocating.txt`，不能与复用Reader结果混算。

建议保留当前4/15/16选择；端点没有重复退化，补齐报告后完成B04独立提交。
不要恢复早期“所有固定scan均prepared”的配置，JSON的pilot保留了该阶段数据。

## 已有验证与尚未执行项

B04当前生产源码已通过CGO0的gofmt/vet/build/全库test、36包deps、55条manifest，
两份直接AU FFmpeg像素Oracle和显式加权编码三方Oracle；日志在 `tmp/b04/`。
后续只增加/调整测试时也已运行相关bitstream/CABAC定向测试，均通过。

CABAC算术/残差fuzz各5秒、parallel2均通过：日志分别报告295027/303863次exec；
这是烟测，不是长期稳定性证据。对应文件为 `fuzz-arithmetic.txt`、`fuzz-residual.txt`。

**B01–B04本轮集成purego、386、相关race与六目标构建/测试编译尚未执行，原计划B05后集中运行。**
不要把A轮旧证据当作这些新实现的集成通过。每项当前已有正常CGO0门禁和Oracle。

## 接手顺序

1. `git status --short`核对上述B04工作树，审阅差异及已有日志；不重复已有效的正式采样。
2. 补 `docs/gates/b04_*.md`、performance、实现状态、支持矩阵与计划进度；核对样本/引用后，
   将B04代码、测试、JSON与文档独立提交，例如 `perf[cabac]: reuse bit cache and fixed residual scans`。
3. 推进B05：可先评估原16宽块加权的AVX2，以及B02仍缺的高位深MC路径，保持SSE2/Go回退、
   独立正确性与同环境前后样本；每个实际采用的逻辑改动独立提交。
   当前已验证平台为Windows/amd64 Ryzen5700X；NEON需要原生arm64执行证据，交叉编译不能替代。
4. 完成本轮集成验证并同步实际覆盖；原A01亮度包装层局部数组1alloc/op缺口仍存在，未在本轮修复。

本地复用工具：`tmp/perf_followup.ps1`负责measure/endpoints/repeat/verify，
`tmp/perf_archive.cjs`归档样本；正式计时不可与构建、Oracle、profile或race并跑。

原有 `docs/performance.md` 整文件换行状态与 `scripts/verify_m8.ps1` BOM改动保留，后者从未执行。
不要整文件暂存换行噪声；`tmp/stage_performance.cjs`仅支持自己的末尾追加段，修改历史段落需另行审查最小补丁。
此前A07探针副本已改名为 `tmp/a07/allocation_probe_test.go.txt`，避免被`go test ./...`当作额外包。
