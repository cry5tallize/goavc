# M8 开发实施包：SIMD、内存复用与确定性并行

> **后续开发已开始：** 本页及01–11最初记录规划交付，以下“本次只写文档”等措辞属于该历史任务。当前实现、实际接口、逐项提交和验证证据以 [12_execution_log.md](12_execution_log.md) 及源码为准；不要据初始待实施描述重复实现已交付内核。arm64按用户明确要求仅做实现/交叉编译验收，原生运行仍标未执行。

> 文档性质：待实施设计，不是完成报告。本次只新增本目录的 Markdown，不新增 Go、汇编、脚本或 CI 代码，不更新 M8 支持状态，不创建提交或标签。
>
> 代码基线：M7 本地验收提交 `1abfa01`、本地注释标签 `v0.9.0`。开工前重新检查工作区，后续变更以实际源码为准。M7 远端 CI 未执行，不因制定 M8 计划而补记为通过。

## 1. 给接手 agent 的第一条指令

先读本文件，再读 [执行任务表](08_task_schedule.md) 和自己任务对应的专题文档。只实现分配的任务，不从“优化一下”扩展到改编码策略。先证明标量行为与数据范围，再编写汇编；先证明所有权，再池化；先证明依赖，再开 goroutine。

没有被验证的目标写“未测量”；没有原生 arm64 运行证据的 NEON 写“未验收”，不得用交叉编译冒充正确性。M8 的核心是**相同输入、相同配置、相同输出**，不是通过减小搜索范围、减少候选或调整 QP 换 FPS。

## 2. 文档导航与阅读顺序

| 文件 | 内容 | 必须阅读的人 |
|---|---|---|
| [01_baseline_and_scope.md](01_baseline_and_scope.md) | 真实起点、热点证据、现有配置缺口、范围与风险 | 全部 agent |
| [02_contracts.md](02_contracts.md) | 依赖方向、数据域、SIMD 接口、Threads、错误与所有权总契约 | 全部实现 agent |
| [03_dispatch_and_parity.md](03_dispatch_and_parity.md) | CPU 检测、构建标签、静态分发、强制后端与等价测试 | SIMD 基础设施与架构 agent |
| [04_kernel_implementation.md](04_kernel_implementation.md) | SAD/SATD/MC/变换/量化/滤波/残差逐类实施步骤 | 内核 agent |
| [05_memory_and_allocations.md](05_memory_and_allocations.md) | 现有 picture.Pool 的复用、公共帧池 API、scratch、生命周期和零分配范围 | 内存、编码、解码 agent |
| [06_decoder_parallelism.md](06_decoder_parallelism.md) | slice 隔离、邻域竞态、滤波屏障、DPB 串行提交和高级工具路由 | 解码并行 agent |
| [07_encoder_parallelism.md](07_encoder_parallelism.md) | 前瞻分析与编码重叠、ME 候选批处理、确定性、资源及生命周期 | 编码并行 agent |
| [08_task_schedule.md](08_task_schedule.md) | 原计划 25 项的映射、子任务、前置条件、提交粒度及文件归属 | 集成负责人及全部 agent |
| [09_validation.md](09_validation.md) | 原生/纯 Go/386/race/Oracle/分配/CLI 验证矩阵与命令 | 实现与验收 agent |
| [10_performance_and_release.md](10_performance_and_release.md) | 基准设计、原始证据格式、性能目标复审、M8 门禁与发布约束 | 性能与发布负责人 |
| [11_agent_handoff.md](11_agent_handoff.md) | 可直接复制的任务提示词、交接格式、并行编辑规则和完成定义 | 派工人及接手 agent |
| [12_execution_log.md](12_execution_log.md) | 已实施任务、冻结接口、实际证据与当前交接点 | 全部接手 agent，先于原始待实施描述核对 |
| [13_gc_reduction_plan.md](13_gc_reduction_plan.md) | 当前源码的 GC 专项：搜索/残差去逃逸、试写复用、有界池、RC/DPB 所有权、G00–G10 派工与验收 | 内存与去分配 agent；优先从 G00/G01 开始 |

本目录是实施层补充，不替代上级门禁。冲突优先级：仓库工作指引 → [Roadmap](../Roadmap.md) → [architecture](../architecture.md) → [Implementation-Plan](../Implementation-Plan.md) → 本目录。发现冲突先登记并由集成负责人裁决，不通过悄悄缩小测试或修改目标消除冲突。

## 3. 推荐开发顺序

1. **M8-A：锁定基线与契约。** 重测当前版本，不把 M2/M5 的历史数字当 M7 当前速度；冻结参数、输入 SHA-256、输出和公共语义。
2. **M8-B：CPU 分发与证明基础。** 先做安全检测、纯 Go 路由、类型化内核表、parity 和后端可观测性，再接第一条 SAD。
3. **M8-C：热核优化。** SAD 验证基础设施后优先做 MC 边界分离与插值，再按当前 profile 安排变换、量化、滤波、残差、SATD。所有原计划项保留，不把排序调整当取消。
4. **M8-D：内存与标量去分配。** 与已经冻结接口的内核任务并行；复用已有图像池，分离工作元数据和 DPB 保留元数据。
5. **M8-E：确定性并行。** 在串行零分配及所有权稳定后，分别做解码 slice、编码前瞻流水线和独立 ME 候选批处理；不并行提交参考帧或码控。
6. **M8-F：整体验收。** 原生后端矩阵、完整 Oracle/race/纯 Go/386、端点测量、目标差距说明和发布记录。未执行的必需门禁保持未完成。

每个逻辑改动独立提交。25 是上级计划的逻辑项数，不限制真实提交数量；各架构与大型重构应拆成更小的可二分提交。

## 4. 必须交付的结果

- amd64 SSE2、AVX2 与 arm64 NEON 的安全分发及纯 Go 回退；不会在不支持的 CPU 上执行指令。
- 现有编码器与解码器共用的热点内核确实接入调用链，不能只有未使用的汇编符号。
- 8/10-bit、420/422/444、场视图以及边界路径结果不变；没有 SIMD 专用实现的组合走同语义 Generic，不因此返回新的 ErrUnsupported。
- kernel 零堆分配；已列入门禁的 MB 重建/模式决策复用路径零堆分配。公共帧头、独立 Packet、冷启动和外部长期持有对象的分配单独计量，不虚称整个库零分配。
- slice 并行、编码前瞻流水线、ME 候选批处理有真实工作、有确定性与背压证明；公开实例仍非并发安全。
- 同机前后基准和端点 FPS/P95/内存/分配/输出哈希；30 FPS 未达到时公开差距及热点，不改变测试配置掩盖差距。
- [实施状态](../implementation-status.md)、[支持矩阵](../supported-features.md)、[性能报告](../performance.md)、架构/CLI 文档及未来 `docs/gates/m8.md` 同步，不把本实施包当通过证据。

## 5. 本次明确不做

不实现 M8，不跑 Go 全门禁或重新测量吞吐，不改 M7 验收结论，不改算法质量策略，不新增外部依赖，不新增 M7 拒绝能力，不执行 push/release。实施 agent 的验证要求见专题文档；本次文档交付只验证引用和覆盖关系。
