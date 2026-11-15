# 08：任务顺序、依赖与提交卡

## 1. 编号与派工原则

- A00 为必要前置，不替代原计划项。
- K01–K25 与 [Implementation-Plan 的 M8 表](../Implementation-Plan.md) 一一对应。
- K09、K10、K11、K12、K13可按架构与语义重构拆成多提交，但不得丢掉任一项。
- K20m 为Roadmap明确列出的ME候选并行，挂在K20执行包内单独提交，不能因原25项没有单列而遗漏。
- K21/K22的最小测量框架在A00提前建立，最终完整矩阵仍要在相应收尾项交付。不是到第21项才第一次测性能。
- 本表的完成列是**所需证据**，不是当前完成状态。已交付项与arm64编译验收范围以 [12_execution_log.md](12_execution_log.md) 为准；K15–K18的GC专项切片与优先级见 [13_gc_reduction_plan.md](13_gc_reduction_plan.md)，不得据最初计划重复实现已交付任务。

## 2. 最小执行依赖表

| 任务 | 前置 | 输出/实施落点 | 完成证据 | 建议提交主题 |
|---|---|---|---|---|
| A00 基线与契约冻结 | M7基线可定位 | 固定输入/配置/哈希、当前CPU/alloc profile；冻结02/05/07中的拟新增接口与缓存/线程常量 | 同一HEAD、Generic串行完整输出与测量原始日志；未测项明确列出 | `docs[performance]: record m8 baseline contracts`；必要基准另作bench提交 |
| K01 CPU检测 | A00 | simd/cpu各平台文件，安全CPUID/XGETBV，purego与其他架构回退 | 能力组合选择、GOAMD64=v1、缺叶/缺OS状态；详03 | `feat[simd]: add safe cpu feature detection` |
| K02 内核表 | K01、A00接口冻结 | typed table、唯一Generic迁移起点、每槽实际后端；R3精确放行search/picture | 真实共享入口可运行、depscheck通过，无反向环；详02/03 | `feat[simd]: add typed dispatch with generic fallbacks` |
| K03 parity框架 | K02 | simd包内后端对照、固定seed、直接Generic、测试标签强制机制 | 真实算法向量和错误/边界比较，不只检查槽非nil；详03 | `test[simd]: add backend parity harness` |
| K04 SSE2 SAD | K03 | sad_generic、sad_amd64包装/汇编，搜索真实私有SAD接入 | byte/10-bit范围、尾部、真实搜索、0分配和bench；详04§1 | `feat[simd]: add sse2 sad kernels` |
| K05 AVX2 SAD | K04 | 安全AVX2实际计算，不32-byte越读16-byte行 | 原生AVX2 parity、OS能力约束、bench；详04§1 | `feat[simd]: add avx2 sad kernels` |
| K06 NEON SAD | K03 | sad_arm64，uint16 widening/reduction | 原生arm64 parity与bench；详04§1 | `feat[simd]: add neon sad kernels` |
| K07 x86 SATD | K03 | 实际SATD4x4生产入口及必要批处理；SSE2/AVX2 | 8/10-bit解析向量、帧内选择/码流不变；详04§2 | `feat[simd]: add x86 satd kernels` |
| K08 NEON SATD | K03 | arm64同语义SATD | 原生arm64、tiled语义及集成；详04§2 | `feat[simd]: add neon satd kernels` |
| K09 MC/插值 | K03、SAD端到端基础走通 | a唯一Generic块化/边界分离；b SSE2；c AVX2；d NEON；权重/场适配 | 全相位/负MV/三采样格式/双位深/FieldView/三方Oracle；详04§3 | 先`refactor[prediction]: isolate block interpolation kernels`，再逐架构feat |
| K10 4/8×8变换 | K03 | 保留现有Forward8整数策略与int64逆变换，逐后端接入 | 基向量、原地、负shift、宽域/错误、编码字节一致；详04§4 | `feat[simd]: add exact transform kernels`，按后端拆 |
| K11 量化/反量化 | K03、K10契约冻结 | 参数准备/缓存、原量化策略、精确算术与宽域回退 | QP'0..63/矩阵/DC/死区/溢出/原子性分列；详04§5 | `feat[simd]: add exact quantization kernels`，预检重构另提交 |
| K12 去块 | K03 | luma/chroma及Span、两方向和三后端 | 阈值/原邻域/位深常量/Span1/跨slice；详04§6 | `feat[simd]: add deblocking kernels` |
| K13 残差加减 | K02 R3、K03 | picture共享相加与真实减法生产循环迁移 | int32极值clip/低两位/stride/场；详04§7 | `feat[simd]: add residual arithmetic kernels` |
| K14 全边界矩阵 | K04–K13 | guard-page、最小len、奇数尺寸、非对齐、所有后端清单 | 原生覆盖矩阵，无padding掩盖、无假后端；详03§3/5 | `test[simd]: cover boundary and stride parity` |
| K15 frame/picture池 | A00所有权/API冻结 | a扩充既有picture.Pool；b根包Frame构造与backing池；c encoder/RC池化；迁移全部输出 | 释放/借用/缓存预算/错误/Close/多输出/六格式；详05§1–4 | `feat[buffer]: add bounded frame storage reuse`；picture与codec迁移分提交 |
| K16 slice scratch | K15核心租约稳定 | frame-wide与task-local拆分、typed scratch、DPB元数据转移 | 串行行为不变、共同444私有scratch、无O(slice×frame)；详05§5 | `refactor[decoder]: isolate slice execution state` 等，按codec拆 |
| K17 decoder去分配 | K16、热点profile | 正常MB重建、熵与MC临时分配清除 | 已准备上下文的真实CAVLC/CABAC MB路径AllocsPerRun=0；详05§6 | `perf[decoder]: remove macroblock hot path allocations` |
| K18 encoder去分配 | K16、热点profile | 模式决策、真实试写、候选与MC scratch复用 | 非空/非纯Skip的真实候选决策=0分配且输出不变；详05§6 | `perf[encoder]: remove mode decision allocations` |
| K19 slice并行 | K16/K17、K15释放稳定 | a串行隔离；b owner-first/Grid；c worker；d barrier/错误/高级兼容 | 六格式双熵常规slice并行、先进工具Threads>1仍正确、determinism/race；详06 | `feat[decoder]: add deterministic slice parallelism`，隔离先提交 |
| K20 编码流水线 | K15/K16/K18 | a纯分析提取；b调用内真实重叠；c共享并发预算；d生命周期/CLI | 码流/时钟/RC/闪光/尾组/背压不变、无后台任务；详07§1–4/6–7 | `feat[encoder]: overlap lookahead analysis with encoding` |
| K20m ME候选并行 | K04/K09/K18、K20并发接口冻结 | 纯SAD任务、固定候选批、主线程Scorer/归约 | 候选/Scorer顺序、tie-break、full/diamond/refine一致和bench；详07§5 | `perf[motion]: parallelize independent search candidates` |
| K21 全kernel基准 | A00已有最小框架，收尾依赖K14 | 所有kernel×位深×后端×典型/边界微基准 | 5次原始样本、实际执行后端/0分配；详10 | `bench[bench]: cover all kernel backends` |
| K22 端点基准 | A00已有最小框架，收尾依赖K19/K20/K20m | 240p–4K、冷热分离、单/多线程、格式/工具矩阵 | 完整帧数/参数/哈希、FPS/P95/内存/alloc/Oracle；详10 | `bench[bench]: add reproducible endpoint matrix` |
| K23 性能报告 | K21/K22、09验证完成 | docs/performance、原始结果小清单、后端与瓶颈覆盖 | 可复现同机before/after，无跨机器加速比；详10 | `docs[performance]: publish m8 benchmark results` |
| K24 目标复审 | K23 | 原30FPS目标与差距/原因/适用配置写实 | 公开达标或未达标；不得更换场景、下调隐藏门禁；详10 | `docs[performance]: review measured m8 targets` |
| K25 门禁收尾 | 全部必需项、原生矩阵与race | Roadmap勾选、实施状态/矩阵/架构/CLI/门禁页同步；经授权本地v0.9.5 | 逐门禁证据，未执行项不算通过；详10§发布 | `chore[release]: close milestone m8 gate` |

## 3. 可并行的执行波次

### Wave A：唯一集成负责人

完成A00、K01/K02/K03最小可用、R3精确更新和函数签名冻结。不要让各架构agent同时发明自己的函数类型或复制Generic。

K15的Frame API/租约和picture预算设计也在此冻结，但先不与多个codec调用者混合修改。

### Wave B：按独立文件集派工

可同时开展：

- x86后端组：K04/K05，随后按热点接K09/K10等架构文件。
- arm64后端组：K06/K08及对应K09–K13 arm64文件。
- 共享标量/适配组：MC块化、变换/量化/滤波领域适配；这些共享Go文件只能一个owner。
- 内存组：K15各租约与缓存实现；先把picture/backing池做好，再接codec生命周期。

不能把两个agent分别分配“全部x86”和“全部MC”且都允许改mc_generic/dispatch。按文件后缀拆分只解决部分冲突；共享签名/表修改由集成负责人执行。

### Wave C：codec独立改造

K16/K17解码状态和K16/K18编码状态可独立，但内存组不同时改同一个frame.go/encoder.go。各codec串行结构和所有权稳定后再接K19/K20/K20m。

解码并行与编码并行可同时进行；它们若都需要改motion.Grid，先由共享owner完成范围/owner契约，再分别接入，不能互相覆盖。

### Wave D：单一验收负责人

合并后只运行一次最终完整矩阵，停止其他基准/长测负载后测性能。各任务完成时运行自己的定向验证；代码提交前按仓库规则跑完整常规门禁。不要多个agent同时跑全库race或吞吐基准污染结果。

## 4. 文件归属约束

| 文件集合 | 唯一owner规则 |
|---|---|
| `internal/simd/dispatch.go`、cpu、types、测试强制工具 | 基础设施负责人；后端agent提交所需槽说明，负责人集成 |
| `*_generic.go`、领域Go包装 | 每个kernel族一个共享owner，迁移时删除对应旧循环 |
| `*_amd64.s`及对应amd64包装 | x86负责人；同一文件不要多人追加不同kernel，按kernel拆文件 |
| `*_arm64.s`及对应arm64包装 | arm64负责人，同上 |
| importgraph规则/架构文档 | 集成负责人；任何放行须说明实际消费者 |
| 根包frame相关文件、buffer原始池 | 公共内存负责人；先冻结API再分配caller迁移 |
| decoder/frame/neighbors/redundant及motion.Grid接入 | 解码状态负责人；并行负责人在前一任务结束后接手同一文件 |
| encoder/encoder/frame/lookahead/b_schedule/rate_control | 编码状态负责人；池化、去分配、并行三个阶段顺序交接 |
| Makefile/CI/验证脚本、性能最终报告 | 验收负责人，不让每个后端agent各自改一份命令 |

## 5. 单任务执行卡必填项

派工时从本表复制一行并展开：

1. 当前基线revision与未提交改动说明。
2. 目标任务及专题章节；允许修改的精确文件、禁止碰的共享文件。
3. 已冻结的输入输出签名、单位、别名、错误和生命周期契约。
4. 本任务中依次完成的重构/实际实现/调用链接入/清理。
5. consumer-visible测试、原生硬件要求、allocation和before/after命令。
6. 完成后要更新的状态/支持/性能/架构文档，哪些只应标未测。
7. 返回提交、文件清单、实际命令与退出码、原始产物、已知缺口和下一任务所需接口。

不得接受“实现已写，测试后补”“只有交叉构建，NEON已完成”“纯往返所以Conformant”等交接。
