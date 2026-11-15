# 11：接手 agent 派工与交接模板

## 1. 使用方式

把“通用任务前缀”加上一个具体切片发给执行agent，并附当前revision。不要把所有切片一次交给多个agent自由改全仓；共享文件必须指定单一owner，依赖顺序见 [08](08_task_schedule.md)。本次只是文档规划，不已授权接手agent自动发布或覆盖当前用户工作。

## 2. 通用任务前缀，可直接复制

```text
你在 github.com/cry5tallize/goavc 仓库实施指定M8任务。先阅读仓库工作指引及：
docs/m8-development/README.md
01_baseline_and_scope.md、02_contracts.md、08_task_schedule.md
再读本任务对应专题。上述相对文件均在 docs/m8-development/ 内。

目标：性能优化不改变现有支持范围和结果；本任务编号及允许文件见下方切片。
开工先git status --short，保留用户已有改动。当前代码以实际工作区为准，
M7历史基线为1abfa01/v0.9.0，不能据历史记录冒称当前已通过M8。

强约束：Go1.27；常规CGO_ENABLED=0；仅标准库、不加require；汇编只在
internal/simd；共享重建/预测/变换/滤波只保留一份算法；不改QP、搜索范围、
候选集合、位深、权重、场序、B/GOP/IDR、RC策略或slice划分换速度。
实例仍非并发安全，输入/输出/Release/Clone契约不变。未知CPU/边界/宽域
走同语义Generic，不增加新的媒体Unsupported。

代码智能使用可用LSP查定义/引用；改导出符号先查所有引用。复用既有模式，
不引入第二套PicturePool、动态注册框架、无界队列或通用并行任务库。

只改分配的文件；需要共享接口变更先向集成负责人发精确需求，不越界抢改。
编辑过程中不反复跑build/lint/tests/formatter；逻辑变更完成后统一格式化
本次Go文件、跑精确验证。提交前按仓库规则完成常规门禁；性能测量避免与
其他agent长任务同时运行。算法保护必须有规范解析值/独立Oracle，
parity不等于完整规范认证。错误和未执行项如实记录，不删除失败测试。

交付须包含真实调用链接入、有效测试、before/after证据、必要文档和清理。
没有原生arm64执行不能写NEON通过；没有实际consumer路径不能只交汇编stub。
不自动push/tag/release。提交权限按当前用户要求执行；若未授权提交则只留改动。
```

## 3. 切片A：基础设施与接口集成负责人

```text
任务：A00、K01、K02、K03的最小可用基础。
阅读：03_dispatch_and_parity.md、10_performance_and_release.md。

精确责任：internal/simd/cpu*、dispatch*、types与测试后端工具；
internal/testutil/importgraph/graph.go及其规则测试、docs/architecture.md。
允许新增对应generic/测试文件，但第一批只用SAD走通真实链路，不提前为所有
未来kernel制造空槽或占位函数。search/picture的领域修改必须明确移交owner。

先采当前版本串行Generic基线并固定输入SHA、输出及全部参数；定义每槽后端
诊断、purego、测试标签机制和AVX2 OS状态检测。冻结02/05/07中拟新增API，
把真实函数类型、缓存/线程常量及调用者迁移表交给后续agent。
R3只精确增加真实消费者目录，simd测试不得反向import领域包制造环。

验收：真实算法解析值/Generic调度可运行、缺失CPU能力安全选择、purego可编译
运行、依赖检查和实际调用链。不是只测函数指针非nil。
交接：接口清单、已准备的SAD parity入口、任务文件owner表和真实基准原始记录。
```

## 4. 切片B：x86 内核执行者

```text
任务：分配到的K04/K05/K07及K09–K13 x86子任务，不能默认全部同时开工。
阅读：03、04及该kernel现有解析测试。

拥有：明确指定kernel的 *_amd64.go / *_amd64.s 及该kernel的x86专用测试/基准。
不拥有：dispatch/types/CPU探测、*_generic.go、领域包装、其他架构文件。
如果当前任务同时授权Generic迁移或领域接入，应在派工中明确写出具体文件。

遵守冻结签名；SSE2不夹带SSE4/AVX，AVX2不得假设BMI/FMA；非对齐load、
最小行尾len、signed widening、int64宽域、别名与错误保持。SAD16-byte行不能
用32-byte load越读。普通调用必须真的进入矢量快路，宽域Generic回退写明。

验收：同机Generic/SSE2/AVX2 parity、解析向量、边界访问证明/guard测试、
0分配、真实consumer输出不变及5次microbench。不能以孤立汇编bench替代接入。
返回需要集成的函数槽及其参数域，不越权修改共享表。
```

## 5. 切片C：arm64 内核执行者

```text
任务：K06/K08及明确指定的K09–K13 arm64子任务。
阅读：03/04。拥有指定kernel的arm64包装/汇编/专用测试/基准；共享Generic和
函数表由唯一集成负责人管理，不把amd64文件复制成另一套语义。

仅使用arm64基线NEON，不使用可选扩展。byte/uint16和signed coefficient
使用正确的widening、clip、归约；FieldView按逻辑场边界，不用父帧其他行。

必须在真实arm64运行parity、解析向量和端点，并记录CPU/GOHOSTARCH及后端。
只有交叉构建时交付仍可合并的可编译实现和待执行清单，但不得标记原生验收完成，
不得创建假报告或将Generic fallback结果记成NEON。缺runner时完成可达工作后
把具体缺失证据交给验收负责人，不删除硬门禁。
```

## 6. 切片D：内存与所有权负责人

```text
任务：K15；K16的跨模块所有权契约。阅读05及真实DPB/RC/B路径。
拥有：internal/buffer原始存储池、根包frame_pool/Frame释放绑定、picture.Pool。
codec和stream调用者文件须分阶段移交，不能与decoder/encoder负责人同时改。

保留既有picture池；添加有界缓存但不把缓存上限解释为活跃RSS上限。
新增根包NewFrame提案先与集成负责人冻结；Frame头不复用，租约归还一次，
Clone独立、Close后输出仍有效。根包可依赖buffer，buffer只能依赖标准库。

编码工作图池化先列reference/recon/last/bPast/bFuture/RC各角色，释放按唯一
owner进行；DPB保留Motion/referenceIDs/场标记不能当scratch立刻归还。
必须迁移toFrame/Reconstruction/copyQueuedFrame/YUVReader及所有错误路径，
不留下“暂时仍make”的漏网消费者或忽略错误的旧包装。

验收：脏缓冲、六格式桶、同时持有输出、重复Release、Reset/Close、B双锚点、
RC失败/替换/最终失败、元数据长期借用、缓存峰值与race；提供分配前后数据。
```

## 7. 切片E：解码状态与并行负责人

```text
任务：顺序完成K16-decoder、K17、K19；先串行隔离再并行。
阅读05/06；拥有decoder/frame、neighbors、cabac/fmo/redundant等明确文件。
motion.Grid共享变更必须先由指定owner完成或明确移交给你，不能抢改。

必须处理不可变owner-first、Grid独立描述符和完整上界/owner过滤、444私有
fullResidual、slice-local熵与scratch；不能简单go decodeSlice。
首批常规渐进六格式双熵I/P/B完整slice并行；高级工具在Threads>1下保持
原串行兼容。重建全部join后按旧顺序deblock/FillBorders/单一DPB提交。

验收：行中slice/右上下一slice、不同完成顺序、多个错误优先级、temporal
metadata寿命、IDC0/1/2、SPS/IDR恢复、全部55语料与27投影保护网、0分配、
定向及完整race。不能以新增Unsupported缩小并行改造的正确性范围。
```

## 8. 切片F：编码状态与并行负责人

```text
任务：顺序完成K16-encoder、K18、K20及K20m；阅读05/07。
拥有明确分配的encoder/frame/mode_decision/lookahead/b_schedule/rate_control
文件及search执行上下文。与内存负责人交接完成后再修改同一文件。

先提取纯统计，不并发analyzeRD、encodeAtQP或encodeFrameNow。当前编码与
已知确切anchor的未来只读分析在公开调用内重叠，返回前join；B/RC/时钟/
输出/reference仍主线程有序提交。用户QP回调保持原序号同步执行。
MEworker只算SAD，Scorer和strict-<归约按原序号在主线程执行；diamond/
refine每阶段center依赖保持，full候选分有界批。分析和ME共享总Threads预算。

验收：字节/元数据/重建/QP/VBV轨迹一致、真实重叠、失败重试、Flush尾组、
闪光与场景边界、Scorer调用轨迹、低两位与六格式、背压、无worker遗留、
模式决策0分配及同机端点/ME基准。不用多实例乱序编码冒充单实例pipeline。
```

## 9. 切片G：验收与性能报告负责人

```text
任务：K14、K21–K25，最小测量入口在A00即参与。
阅读09/10；拥有Makefile/CI/verify_m8脚本、基准编排及最终文档，
不在验收时修改算法或偷偷关闭慢配置。

逐项记录native SSE2/AVX2/NEON、purego、386、六目标构建、parity、Oracle、
allocation、单多线程、race和实际CLI。基准停止其他长负载后测5次，报告
真实计时边界/完整帧数/输入输出SHA/实际每槽后端，跨机不算ISA倍率。

30FPS未达写固定场景差距与原因，不能换PCM/平坦Skip或下调配置。每个必须
门禁有证据才能标M8完成。未授权不创建标签，不push/release；缺原生runner
或工具就明确列未验收，不用Skip/交叉编译冒充通过。
```

## 10. 标准交接格式

每个执行agent结束时交付以下事实，不能只写“测试通过”：

| 字段 | 必填内容 |
|---|---|
| 任务与基线 | K编号、起始revision、依赖任务版本、现有工作保留情况 |
| 修改 | 真实文件列表、导出符号/调用者迁移、已删除重复实现 |
| 契约 | 输入域、stride单位、别名、边界、错误、所有权和并发预算 |
| 验证 | 精确命令、环境、退出码、实际匹配用例、跳过/未执行原因 |
| 硬件 | 原生CPU/OS/GOHOSTARCH、可用后端及强制方式 |
| 性能 | 同机before/after、5次原始值、统计边界、输出不变证据 |
| 产物 | 提交或未提交状态、结果路径与SHA、小型入库摘要、大临时文件已清理 |
| 下一任务 | 可用接口、不可并发编辑文件、尚缺的必需证据，不写无证据完成声明 |

## 11. 集成负责人验收清单

- 接口与函数表一致、没有领域反向import、没有重复Generic、没有未迁移调用者。
- 真实生产路径已接入；不是只优化没被调用的导出包装。
- 新所有权不会提前释放B锚点/DPB元数据/用户持有输出；错误路径join后清理。
- 单多线程结果与旧串行结果相同，不只“新串行与新并行共同改变后相同”。
- 后端宽域回退和高级串行兼容写清；不能用正常回退掩盖未实现的全部计算。
- 没有新增require、CGO、散落汇编、无界队列、fake fallback、stub或无实际工作worker。
- 完成后更新运行文档与门禁证据；本实施包中的拟定值与落地不同须同步，不让下一agent继续按过期接口工作。
