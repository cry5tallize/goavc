# 07：编码器前瞻流水线与 ME 候选并行

对应 K20，并补齐 Roadmap 中未在原25项单列的 ME 候选并行（K20m）。本阶段不把有参考依赖的多帧 encodeFrameNow 并发执行；重建、熵写出、RC/VBV及参考提交保持单一有序主流程。

## 1. 真实依赖与禁止并行点

已有输入路径为 `SendFrame → queueLookahead/queueBInput/encodeFrameNow`；`ReceivePacket` 也可能触发延迟编码，Flush只设置结束输入，后续接收排空尾组。

以下函数会修改编码状态，不能直接扔进worker：

- `mode_decision.go:decideRD/analyzeRD`：重置info/NZ、写MV/重建像素、试写CAVLC、捕获和恢复候选。
- `intra.go:analyzeIntra`：块内预测与重建有扫描依赖。
- `rate_control.go:encodePlaneAtQP/encodeRateControlled`：临时修改frameQP、选择QP、保留候选，搜索顺序依赖先前观察。
- `encoder.go:encodeFrameNow`：frame_num/POC/GOP/RC/输出/reference提交。
- 用户宏块QP回调：继续在公开调用内按原顺序同步调用，不能改为worker并发回调。

## 2. K20a：先分离纯分析与决策

建议新增 `analysis.go`、`analysis_cache.go`、`analysis_parallel.go`；现有lookahead/B/AQ/weight文件只保留调度与语义适配。不要把所有功能挤进encoder.go。

### 2.1 场景差异

从 `framesDiffer` 提取纯统计：固定两帧 → `difference/samples`，使用int64；阈值比较仍为原严格 `difference*100 > samples*maxSample*threshold`。

- 统计包含Y/U/V全部有效采样，忽略外部stride padding；10-bit原始LE值不降位深。
- 结果缓存键为源frame ID、比较anchor ID、配置epoch；阈值若不在统计中则不必入key，但不能缓存布尔后复用到另一阈值。
- 不用抽样/直方图替代逐采样差异，不改变闪光抑制和场景/GOP边界。
- 同一纯统计可按固定平面/行带分解，固定序号整数归并。检查最大尺寸的int64总和与乘100仍安全。

### 2.2 AQ

`activityQP` 当前先将高位深样本右移到8-bit，再算16×16统计；这是**既有AQ策略**，不能与SAD的原位深契约混淆，也不能在M8“顺手改为原生10-bit AQ”。

提取sum/squares或方差分类缓存；保持 `variance=(squares*256-sum*sum)/(256*256)`、阈值64/1024和最终QP裁剪顺序。缓存纯统计/分类，不缓存最终QP；每次RC尝试仍结合其基准QP和strength算结果。编码尺寸边缘复制、MBAFF场采样方式必须与当前newFrameState一致；只在能证明一致的布局使用预计算。

### 2.3 权重分析

`weighted_prediction.go:estimatePredictionWeights`：采样格点统计 `n/sumX/sumY/sumXX/sumXY`，之后对**完整平面**算identitySSE/weightedSSE。

- 不改变stepX/stepY的采样格点；行带边界需要从同一个全局格点取样，不从每带首行重新起步。
- 只对冻结的当前源和已提交参考计算，不能拿未来未知重建参考预测权重。
- 权重、offset、整数除法和严格 `<` 验收不变；分平面/行带后固定归并。
- weightedSearchReference可按不相交区域构建，但真实MC继续在原参考插值后加权。

先以Threads=1接入这些提取结果，保证码流与旧策略完全相同，再做worker。

## 3. K20b：真正的前瞻分析—编码重叠

### 3.1 保持公开方法内 fork/join

采用保守的调用内流水线，而非跨公开调用存活的后台编码器：

1. 输入已经按现有规则消费，需延迟使用的Frame先复制到有界内部队列。
2. 主流程按原调度确定本次要编码的head/锚点，完成必需的当前场景判断。
3. 此时能确定下一分析需要的不可变帧对/源统计；向有限worker派发这些未来分析。
4. 调用goroutine同时编码当前已确定帧；只读未来源分析与当前参考编码不写同一存储。
5. 当前AU结束或失败时join本批分析，按frame/anchor/epoch接收或丢弃结果，再执行原有队列释放和提交。
6. 返回SendFrame/ReceivePacket之前没有在途任务。只读分析结果可随内部Frame保留到下一调用，但worker和临时借用不能跨调用。

这满足真实的分析与编码重叠，不改变同步API和stream外层背压。禁止启动一个空worker再把全部计算仍放主线程，当作流水线完成。

### 3.2 已知安全的工作选择

- 非B的lookahead：当前head的场景/闪光判断完成后，下一分析锚点由原分支已知。若当前帧是被抑制的闪光，anchor保持旧场景；否则预期成功后anchor为当前head。可在编码当前帧期间计算下一head及必要未来帧相对该anchor的差异。
- AQ开启时：对已排队、将被编码的未来源做纯活动统计；不依赖未来重建参考，失败时缓存可保留或按epoch失效。
- B+lookahead：锚点选择、bSceneAt、flash逻辑仍主流程有序执行；只派发已知确切Frame/anchor的纯查询或源AQ任务。无法提前确定参考或场景锚点的查询在当前阶段fork/join，不投机全窗口所有帧对。
- 没开lookahead时：没有未来输入可安全预取，不能新增输入延迟。可使用当前帧的行带分析与K20m候选并行，不宣称跨帧流水收益。

不要一次预计算窗口内所有两两帧对；这增加大量无用工作。每个任务必须对应现有决策确实会消费的统计，投机只限当前成功后紧邻分析，受队列容量约束。

### 3.3 所有权与失败

- 为每个内部Frame分配单调ID、reset epoch；缓存用ID而不是可回池裸地址作身份。
- 在分析任务join前，不Release旧anchor、当前head或未来源；将现有Release点移动到join之后，语义仍只释放一次。
- 当前编码失败：原参考/RC/输出/队列不推进，分析结果不得触发未来IDR。缓存仅为同一输入/anchor可重用的纯统计；错误重试仍按旧流程。
- Frame/anchor不再存活时清除缓存引用，不能让结果map永久保留整个原始视频。
- 重建参考不进入无界future；内部队列仍受现有lookahead≤16、B≤16等预算限制，worker队列额外容量固定。

## 4. K20c：单实例并发预算

Threads是包含主计算线程的总预算。当前编码由调用goroutine执行时，最多Threads-1个分析/候选worker。不能“前瞻Threads个 + ME Threads个 + 主线程1个”。

优先采用一次公开调用内共享的小型typed work team，不构建仓库通用任务框架：

- 主线程拥有队列、编码状态和所有commit。
- worker任务仅携带不可变输入视图、输出slot编号、固定统计/候选类型；不捕获Encoder作为可变上下文，不分配每任务closure。
- 队列有界；worker不等待其他worker的子任务、不调用编码器公开方法。
- 分析以有限行带为粒度，ME以候选批为粒度。不能为每个像素或每个候选启动goroutine。
- 分析与ME同时有任务时共享预算；先保证当前编码所需任务可完成，不让主线程等ME、worker又等未来编码形成循环。
- Threads=1完全不创建worker；小任务串行，阈值由同机测量确定，不以“配置多线程”强迫更慢调度。

如果不用共享队列，可采用阶段间join再重用同一worker预算；但实际前瞻与编码重叠阶段仍需限制嵌套ME，不能两个调度器各自满额。

## 5. K20m：ME 候选批处理，必须保留搜索路径

### 5.1 现有次序，不能误判为全部独立

`internal/motion/search/search.go` 实际行为：

- 整像素先consider零向量，再consider裁剪后的predictor。
- full=true时按dy外层、dx内层穷举。
- diamond每轮先固定center，再依序左/右/上/下4点；归约后决定下一轮center。
- Refine每个step=2、1先固定center，再按dy/dx顺序处理3×3候选；每阶段结束才选下一center。
- `Scorer func(MV,int) uint64` 可由调用者提供，不保证并发安全或无副作用；当前是依序同步调用。
- consider仅在rank严格更小时替换best，相等保留先出现者；范围外候选不调用Scorer。

### 5.2 实施步骤

1. 将纯“某MV的原始SAD”从consider中抽出，输入src/ref/几何/MV冻结，worker私有候选MC scratch。
2. 主线程生成一个**当前中心已确定的候选批**，保留原序号和重复候选，不擅自去重改变Scorer调用轨迹。
3. worker只计算SAD，不执行Scorer、不更新best/MV grid。
4. 主线程按原序号调用Scorer并做原strict-<归约，保留其调用顺序、次数和原始SAD返回值。
5. full search可按32/64个候选批处理，固定大小结果slot，不能一次为最大半径分配数万闭包。
6. diamond每轮4点、refine每阶段9点在center固定后可作为小批；只有测量确有收益才并行。若调度成本更高则保留串行，不改变已确定的下一轮查询集合。
7. 不把整个diamond搜索空间改成full search，不减少搜索半径，不把不同MB/分区尚未确定的MVP提前固定。
8. 保留无执行器时的串行行为；为内部搜索显式增加执行上下文并一次迁移全部调用者，不新增第二套搜索算法、全局worker或忽略执行器的兼容shim。
9. `WithFullSearch` 现有选项是大批候选的实际生产路径；测试和测量必须覆盖它。默认diamond不应为微小批次产生退化。

### 5.3 接入与内存

search包不能import encoder。采用search定义的typed批描述和两种样本执行方法，不传pipeline类型，不将Scorer放进任务。建议在A00冻结以下具体形状：

- `CandidateBatch[T byte|uint16]`：包含只读`Original []T`（行距16）、`Reference *picture.Plane`、`X/Y/W/H/Radius`、`Count`以及固定`[64]MV`、`[64]bool`有效标记、`[64]int`结果。Count只能为0..64；无效候选不调Scorer。
- `BatchExecutor`：`Evaluate8(*CandidateBatch[byte])`、`Evaluate16(*CandidateBatch[uint16])`；方法同步返回，内部可用受预算约束的worker，返回时全部SAD结果就绪。不保留批指针，不允许后台完成。
- search提供供执行器调用的单候选纯计算入口，接收batch、候选序号和worker私有`[256]byte/uint16`预测scratch；它只写该序号SAD，不触碰其他slot或best。
- `EstimateWithCost`尾部增加`executor BatchExecutor`参数，nil表示串行；内部整数/细化阶段传递同一上下文。已有Integer/Refine/Estimate保持原串行接口，内部明确传nil；所有EstimateWithCost调用一次迁移，不保留旧签名别名。
- 每个并行搜索上下文持有预分配typed workspace，原始块和batch在准备阶段拥有固定存储，Original只借用它；不能从每次局部window取slice传给interface后导致每次搜索逃逸分配。传给接口的是已存在的workspace指针，不把大batch按值装箱。

当前LSP查到的真实迁移位置为`encoder/mode_decision.go`与`internal/motion/search/search_test.go`中的调用；开工时重新查引用。新方法命名可按包风格调整，Count/单位/同步完成/Scorer主线程语义不可改变。用AllocsPerRun核对接口与workspace确实复用，不在候选循环创建goroutine/channel/slice。

## 6. 生命周期与流控

- 保留e.out单槽背压，ErrAgain不消费输入；不可为了提高流水吞吐扩成无界Packet队列。
- Flush不漏尾部、不重复编码，B尾组仍按原顺序；ReceivePacket触发的延迟编码也必须使用同一分析/join路径。
- Reset/Close发生时没有跨调用worker；清空队列、缓存、frame IDs/epoch、B时钟和RC状态。已返回的Packet.Data和Frame快照仍独立有效。
- 当前调用内失败必须先join再释放，不能用epoch过滤结果来代替等待。
- Stream仍同步读/写和按帧边界取消；不在codec内部从stream端点预取，不关闭调用方I/O。

## 7. 验收矩阵

1. Threads=1/2/0/多于任务数；同一输入的Packet字节、PTS/DTS/Duration/KeyFrame、本地重建、最终QP/FrameBits/VBV轨迹完全相等。
2. CQP/ABR/CBR/VBV；GOP与IDR独立边界；B0/1/2/3/16；Lookahead0/1/2/16；闪光、持续切换、平移纹理、平坦与噪声。
3. 六格式，10-bit低两位、负QP、非紧凑stride、444奇数尺寸；TFF/BFF与独立平面即使串行兼容也必须结果不变。
4. 宏块QP回调顺序/次数与非并发调用不变；不要把回调测试改成只比较最后一张QP map。
5. 搜索零向量/predictor平局、等cost多候选、边界裁剪、重复候选；Scorer记录调用序列以捕获并发/顺序变化。
6. 故意延迟不同任务，结果与错误顺序不受完成次序影响；未来分析确实与当前编码重叠，可用测试同步点证明因果，不能靠sleep或只检查worker数量。
7. FailedSend、FailedDelayedPacket可重试；原始调用者Frame返回后立即改写/Release仍正确；Reset再编码等于新建实例。
8. stream慢Writer前不继续读取，取消/错误不残留worker；不把既有 `CancellationHasNoWorkers` 测试直接删除，应按“stream不持久启动worker、codec任务在返回前join”的实际契约更新有意义断言。
9. AllocsPerRun覆盖已准备的模式决策与候选批复用；端点单独报告调度初始化开销。

性能测量分别报告：纯分析阶段、真实重叠、ME穷举与默认diamond、端点总耗时。若分析只占总时间很小，明确Amdahl限制，不以一个阶段加速承诺30FPS。

## 8. 本轮精简交付：仅RC调用内纯分析复用

本轮不实现worker或跨帧流水线，只复用同一次RC调用中不随QP变化的AQ分类、自动权重拟合及加权搜索参考。`frameAnalysis`固定九槽，对应三个独立平面各自整帧/顶场/底场；浅拷贝channel只借用同一上下文。AQ按真实采样布局分类后每次结合QP裁剪，MBAFF权重仍先整帧拟合再应用到场，真实MC保持原参考插值后加权。

缓存只在非CQP且启用相关工具时创建，正常与失败返回均解除；不跨AU缓存源/参考，不需要指针身份map或epoch。权重值按值交给候选，避免选中的state意外持有整套缓存。CQP、用户QP回调、场景/闪光、B调度和QP尝试顺序保持原实现。未做scene两两帧对缓存，因为本轮没有其命中率/收益证据。完整K20流水线与K20m执行器均不因本项标记完成。

## 9. 本轮K20m范围决定

本轮不新增CandidateBatch/BatchExecutor、不迁移search签名、不启动候选worker。当前默认diamond每轮四点、refine每阶段九点，下一中心依赖前一归约；SAD已有SIMD且Scorer必须原序串行执行。当前B直接使用零MV双向预测，不提供额外运动搜索并行量。上述结构不等于证明full-search并行无收益；只是当前缺少大半径full-search代表负载、调度成本与端点收益证据。

后续若实际full-search成为重要瓶颈，沿§5的纯SAD固定批、私有MC scratch、原序Scorer与总Threads预算重新准入。不得先铺通用执行器、修改候选策略或新增B搜索来制造工作量；本轮保留串行零分配与原Scorer轨迹保护网，K20m实现状态仍TODO。

