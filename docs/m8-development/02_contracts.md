# 02：架构与接口契约

> 本文中的“拟新增”符号、类型、文件和测试均是后续实施目标，当前不存在。实现前以 gopls 定义/引用核对所有调用点。修改导出符号必须先查引用，不能只文本替换已打开文件。

## 1. 依赖方向和 Generic 的唯一归属

采用以下固定分层，禁止 runtime 注册插件或每次调用按字符串查表：

- codec/syntax 上层：参数和资源验证、QP/矩阵/参考/滤波边界推导、错误包装。
- `internal/prediction` / `transform` / `filter` / `motion/search` / `picture`：保留原语义入口和格式/边界适配。
- `internal/simd`：独立于流水线和 picture 对象的原始数组内核、唯一 Generic 计算实现、静态类型化后端表与汇编。
- `internal/buffer`：仅标准库的原始存储复用层，不认识 `h264.Frame`、`picture.Picture`、SPS、DPB 或 codec；根包因此可安全依赖它。

迁移一个算术内核时，把其 Generic 算术移入 simd，上层只保留验证、表选择和适配；删除被替代的重复循环。没有迁移的内核继续保留原实现，不为填满“注册表”复制一套。simd 不反向 import 上述共享包；尤其不能让 simd import transform 再由 transform import simd。

`internal/testutil/importgraph/graph.go` 的 R3 白名单当前包括 `internal/motion`，**不包括** `internal/motion/search` 或 `internal/picture`。本设计由这两个实际消费者直接调用 simd，因此 K02 必须显式增加这两项并更新依赖测试、架构说明。不得以包名前缀匹配放宽整个 internal，也不新增 motion 中转层制造绕行。`benchmarks` 不直接 import simd；直接后端基准放在 simd 包内。

## 2. 拟定类型化表

不导出汇编符号；simd 对外只有内部包级语义入口，表和后端实现不暴露给生产上层。函数槽按实际内核定义，未实现的槽绑定 Generic；禁止 nil 槽、interface{} 参数、map[string]func、反射或运行期注册。

第一批接口按以下契约落地，不强迫所有类别使用一个万能 Kernel：

| 拟定入口族 | 参数/返回值 | 契约 |
|---|---|---|
| `SAD8` / `SAD16` | 两个 slice、各自采样 stride、w/h → int | 原始位深绝对差和；16-bit 存储入口目前支持 10-bit 有效值；只读有效区域 |
| `SATD4x4` | `*[16]int32` → int | 严格复制当前 Hadamard 与 `(sum+1)>>1`；不改成重建变换 |
| `Forward4x4` / `Forward8x8` | 固定数组 dst/src | 支持原地；输入残差域保持当前约束；不能改舍入和正变换策略 |
| `IDCT4x4` | 固定数组 dst/src，无返回值 | int64中间量和现有归一化保持，不新增error协议 |
| `IDCT8x8` | 固定数组 dst/src → 溢出状态 | int64中间量，保持int32输出溢出判定，上层包装为ErrInvalidData |
| Quant/Dequant 计算族 | 固定数组、预验证乘子/位移参数 → 状态 | 参数表在上层一次准备，simd 不依赖 transform 表；保留 signed/rounding/overflow 行为 |
| MC8/MC16 interior 族 | 原始采样缓冲、stride、offset、w/h、相位、固定 scratch | 不认识 picture.Plane；调用方先证明全部 halo 可读；整数与各相位分开绑定 |
| Filter8/Filter16 族 | 指向完整边缘邻域的数组、步长、有效 lane 数、阈值 | bS/QP/跨 slice 权限由上层计算；保留原地读写顺序 |
| ResidualAdd8/16、Subtract8/16 | 数组/stride、残差 int32、w/h、位深 | clip 上限随位深，先扩展再加减，尾部不访问无效 lane |

最终 Go 名称可按现有包风格微调，但参数单位、归属、别名和错误边界不能由各后端独立决定。K02 将实际签名补写到本表对应的代码 godoc，并让 K03 用同一函数类型运行所有后端。

对于错误路径：汇编返回小整数/布尔状态或 badIndex；Go 包装负责错误值，不在汇编创建 error。**不要假定所有现有变换/量化都失败原子**：当前实现有逐项写出行为。K02/K10/K11 先记录各入口有效/无效输入的既有契约；若需要更强的写前验证，单独提交共享验证重构并让所有后端一致，不只让某后端回滚。成功结果和 errors.Is 分类必须保持。

## 3. 内存、安全与整数域

### 3.1 单位

- 公共 Frame：Stride 是字节；10-bit 是显式 LE 打包。
- picture.Plane：Stride/Width/Height/Border 是采样数；Data16 是本机 uint16 数值，不是可直接别名的 LE byte 视图。
- simd 的 `8/16` 表示存储 lane 类型；不得将 uint16 stride 又乘 2 后当采样 stride。汇编计算字节地址时才乘元素大小。
- 输入容量基于 len，不基于 cap；公共平面合法最小长度为 `(height-1)*stride+rowBytes`，末行可以没有完整行尾。
- 允许不对齐基址、任意有效 stride、奇数宽高；矢量实现只能处理已证明完整的批次，其余标量尾部。

### 3.2 数值

- byte 样本 0..255；当前 10-bit 有效样本 0..1023。
- 16×16 SAD 上限分别为 65,280 和 261,888，不能用 16-bit 单个累加器承接 10-bit 总和。
- Forward4/8 的当前残差输入范围为 -1023..1023；有符号 shift、负数舍入逐步保持。
- IDCT、Dequant 的输入可来自不可信 int32 系数；必须保持 int64 中间域及 int32 输出溢出检查，不能依据“正常视频系数很小”静默饱和。
- 8-bit 语法 QPY 0..51；10-bit QPY -12..51；共享有效 QP' 0..63。CABAC 初始化和滤波 QP 域不随量化入口一起扩大。
- 自定义 scaling 项合法值 1..255；nil matrix 的含义、光栅布局、分量列表与场扫描均不改变。
- 用 int 做索引与小块 SAD；总字节数/帧计数等乘法在分配前检查溢出。386 必须可运行纯 Go。

### 3.3 允许别名

变换/量化原先允许 dst==src 的入口继续允许。MC、SAD、Subtract 的部分重叠只在现有契约允许时支持；默认不新增模糊部分重叠语义。滤波必须保持其原地更新语义。Reconstruction 和解码输出是独立快照，不与内部参考图片借用同一可变存储。

### 3.4 越界证明

不能引用“宏块对齐可以越界读”作为通行证。对每个汇编入口写清最小 load/store 范围、halo、尾部、地址对齐要求。缓存行内越读即使不崩溃仍然不合格。FieldView 的物理相邻行不是同一场相邻采样行；边界必须先按场坐标钳位。

## 4. 后端与可观测性

- 生产初始化一次：平台可用能力 → 每个函数槽选择实现 → 整个进程不变。
- 后端不是单个“AVX2 已开启”布尔值；不同 kernel 可分别为 Generic/SSE2/AVX2/NEON。报告需要每槽实际后端，AVX2 不支持的位深和宽域不记作 AVX2。
- 生产不添加可随时切换的全局 setter，不读取环境变量改变热路径，不在每 MB 重新检测 CPU。
- `purego` 强制全部 Generic 并排除所有汇编声明/汇编文件；其他 GOARCH 自动 Generic。
- 仅测试标签 `goavc_simdtest` 可提供后端选择辅助；所有 worker 停止、测试串行且表未被读取时才允许切换。详见 [03](03_dispatch_and_parity.md)。

## 5. Threads 与并发预算

复用现有配置；不改变实例外部非并发安全的契约。

- Threads=1：禁止内部计算 worker，作为确定性参照。
- Threads=0：在 New 时取 GOMAXPROCS，之后按可独立工作单元数裁剪；运行期 GOMAXPROCS 变化不重建 worker。
- Threads>1：是单实例计算并发上限，包含调用 goroutine 正在执行的计算；不能在 decoder slice 和 encoder ME/analysis 层层各创建 Threads 个 worker。
- 初始内部最大 worker 上限建议固定为 64；有效并行度 `min(请求或自动值,64,可用独立任务数)`。这是调度上限，不是新的错误；显式负数仍 ErrInvalidData。64 的选择及每 worker 内存上限在 A00 固化，修改须同时更新文档和测试。
- 解码一帧可能只有一个 slice，或某高级组合只能串行。Threads>1 仍接受并按该组合串行执行，这是执行策略，不得将原本可解的流变为 Unsupported，也不能宣称该组合已并行加速。
- 编码未开 lookahead 且当前没有安全候选批次时同样使用串行；不能为“用满线程”改变输出节奏或候选集合。
- 外部并发调用 Send/Receive/Reset/Close 仍不支持；内部 worker 不授权用户并发使用实例。

调度队列容量和结果槽固定有界；为 worker 额外分配的内存逐项计入预算。MaxFrameSize 保持当前语义，不能悄悄解释为整个进程所有工作内存；若现有资源字段不能约束新放大，先形成独立 API/策略补充并由集成负责人冻结，不以无界队列代替设计。

## 6. 生命周期、错误与确定性

- SendFrame 返回后不得保留调用方 Frame/Data；需要并行分析的输入必须已经内部复制。
- SendPacket 返回前，所有使用本次输入或临时 RBSP 的 worker 完成；不让失败清理与 worker 继续写同一图像竞态。
- 解码每张图只有一个 DPB/POC/输出提交点；编码每个 AU 只有一个参考、RC/VBV、frame_num、时钟和输出槽提交点。
- 并行错误选择遵循串行任务顺序，而不是先完成的任务。可以停止派发更后任务，但必须完成所有可能优先报错的前序任务；取消不能改变错误种类和上下文。
- 所有成功结果按原序号合并。相同 cost 保留原候选顺序，不使用完成时间、map 遍历或浮点并行归约。
- Reset/Close/错误清理：先停止派发、等待在途任务并使结果失效，再释放引用与缓冲。epoch 只能过滤旧结果，不能代替 join 或所有权释放。
- Stream 仍按帧边界检查取消，不从普通 io.Reader 后台预取，不承诺中断不可取消的底层 I/O。

## 7. 公共 API 变更约束

Threads 已存在，无需新增字段。为公共输出池化，推荐新增且仅新增一个根包构造函数 `NewFrame(width, height int, format PixelFormat) (*Frame, error)`，详细设计见 [05](05_memory_and_allocations.md)。这是**待实施 API 提案**，不是已有方法。它返回拥有独立生命周期的新 Frame 头，像素存储可池化；不暴露 internal 类型，不提供任意设置 onRelease 的接口。

A00/内存任务集成负责人必须先确认其与现有公共 API 和资源规则一致，再实施；若采用替代 API，必须解决私有钩子访问、根包依赖环、幂等 Release 和缓存预算四个问题，不能留“后续处理”。本次仅制定文档，不增加该 API。
