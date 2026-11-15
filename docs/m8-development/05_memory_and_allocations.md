# 05：池化、所有权与热路径分配

对应 K15/K16/K17/K18。目标是减少可避免的分配和复制，不将公开所有权变成隐含借用；本章的 API/文件均为待实施设计。

> 当前实际交付与冻结政策以 [12_execution_log.md](12_execution_log.md) 为准。针对当前搜索 window 逃逸、residual 分配、RC/PCM 所有权及 GC 证据的进一步拆解见 [13_gc_reduction_plan.md](13_gc_reduction_plan.md)。该专项细化本章，不新增第二套池；G00/G01/G02可先于完整池化实施。

## 1. 先画清现有存活关系

### 1.1 Decoder

- `newFrameStateForPicture` 从实例私有 `d.pool` 借出工作图像。
- 成功参考帧经 `dpb.Reference` 保留 picture、Motion、BlockReferences、场标记；不只保留像素。
- 非参考帧复制输出后归池；失败表示、失败图像必须回收。
- `decoder/frame.go:toFrame` 当前创建独立 Frame 和各平面 byte 存储；10-bit 显式转 LE，不暴露 DPB 视图。
- DPB 淘汰/Reset 才能归还仍借出的运动和引用元数据。

### 1.2 Encoder

- 源图、重建图、不同 QP 的试编码候选当前主要用 picture.New，依靠 GC。
- `reference`、`recon`、`last.pic` 可指向同一个重建图；B 的 `bPast/bFuture` 可延长旧参考寿命。
- `planeReconstruction` 及 separate-plane 借用描述符不能被当独立 owning picture 释放。
- 最佳 RC 候选与其他尝试在搜索结束前可能同时存活；拒绝/替换候选均要回收。
- `copyQueuedFrame` 为 B/Lookahead 拷贝有效平面；不能池化后提前返还给下一帧。

任何改动前，先将这些路径列成 owner/borrower 表和定向测试。不能“发现指针不在 reference 就 Release”。

## 2. K15a：扩充既有 picture.Pool

目标文件：`internal/picture/pool.go`、`picture.go`、现有 pool/layout/field 测试；新增测试可放 `pool_lifecycle_test.go`。

1. 继续使用当前按 MBWidth/MBHeight/精确 PixelFormat 的 key；禁止8/10-bit或420/422/444混桶。
2. 保留现有 NewPool/Get/Release/Stats/Trim 语义与调用方式，不新增同功能第二个池。
3. 增加全池空闲字节预算，初始建议每实例64MiB、每桶最多4张；64MiB 是缓存政策，不是总解码内存上限。超过预算的归还对象直接不缓存，大帧仍可合法分配和使用。
4. 缓存字节包含实际 Data/Data16 backing capacity、边缘和对齐填充，不用显示尺寸估算。32位乘法先检查；统计可用uint64。
5. 空桶及时移除，或限制空桶元数据数量；反复切换尺寸不能让 map 只增不减。采用简单确定性淘汰策略，不实现通用复杂 LRU。
6. Get 不清零像素的现状保持，前提是后续路径完整写覆盖；失败图像不发布。复用元数据需要按逻辑有效域重置，不从陈旧内容推导可用性。
7. Pool 的锁只保护 free 列表和计数，不围住重建/滤波/拷贝；不要每 MB 加池锁。
8. Picture.Release 不是引用计数，不能由多个角色分别释放同一别名，也不保证同一个对象并发 Release；owner 在唯一释放点调用一次。FieldView 的 pool=nil 必须保持。
9. Trim 仍只释放空闲存储，不擅自使借出对象失效；codec Close 先 join/释放引用再 Trim。

## 3. K15b：公共 Frame backing pool 与构造 API

### 3.1 为什么需要根包入口

`Frame.onRelease` 保持根包私有；`frame_pool.go` 在根包内部绑定租约，decoder、encoder、stream 不直接注入 callback。因此不能只在 internal/buffer 写一个 FramePool 后声称外部输出已回池；那会遇到私有字段、依赖环或任意 callback setter 问题。

根包 `frame_pool.go` 已提供：

`NewFrame(width, height int, format PixelFormat) (*Frame, error)`

它是公共分配 API，不替代 Clone，也不是零拷贝参考帧接口；Frame 头每次新建，只有 backing storage 进入有界进程缓存。

### 3.2 固定语义

- 正尺寸、六格式，平面几何复用现有 PixelFormat 方法；尺寸超过既有8192上限为 ErrResourceLimit，非正尺寸为 ErrInvalidData，未知格式为 ErrUnsupported；分配前验证每项乘法。
- 返回紧凑 byte 平面；10-bit 为公共 LE 存储，不复用内部 Data16 视图。
- 每次获取返回**新的 *Frame 头**，PTS/Duration/KeyFrame 为零值，像素有效区域清零。清零避免跨调用方泄露旧缓存数据；报告该成本，不声称靠池化消除了完整写入。
- 仅 backing storage 可复用，不能把已 Release 的同一个 Frame 头重新交给另一消费者。否则旧指针第二次 Release 可释放新帧，破坏幂等。
- callback 捕获原始租约和原始 backing，不依赖调用方后来改写的 Planes；租约持有独立一次性释放保护，防止 callback 因浅拷贝而重复返还。
- owning Frame 不应按值复制后分别当独立 owner；文档要求使用指针或 Clone。一次性释放保护不赋予浅拷贝继续读取像素的权利。
- Release 清空当前 Frame，并恰好归还租约一次；nil/重复释放安全。不同 Frame 的 Release 可由不同调用者并发发生，同一 Frame 不承诺并发方法安全。
- Clone 继续独立非池化分配；原帧Release或codecClose不影响Clone。
- 新帧在创建它的codec Reset/Close后仍由调用者合法持有，直到Release；callback不能访问已关闭codec的可变字段。

### 3.3 依赖与缓存政策

`internal/buffer` 仅管理原始 byte 存储，依赖标准库；根包 import buffer，buffer 不 import 根包，避免环。Frame 几何与 onRelease 绑定放根包，Picture 池保留在 picture 包，不将两者强行抽成万能对象池。

公共 Frame backing pool 已落地为进程级 64 MiB 空闲缓存、每个几何/格式 key 最多4个槽、最多8个非空 key；超过缓存容量直接丢弃归还存储。精确 key 不允许跨格式混用。总字节与桶数是缓存限制，不限制用户合法持有的活跃输出，也不保证进程 RSS ≤64MiB。

新 Frame 头/租约/callback 可能仍有少量分配，这不违反 kernel 0 分配目标；不能把 Frame 获取/释放整个 API 断言为0。优先消除大像素 backing 分配，不为消除一个小 Frame 头引入危险的对象复用协议。

### 3.4 迁移消费者

- `decoder/frame.go:toFrame`：通过构造入口取得输出，按SPS裁剪并逐行复制/LE打包；拷贝失败时Release。
- `encoder/encoder.go:Reconstruction`：获取独立输出快照，保持现有时序/错误/元数据契约，不借用e.recon。
- `encoder/lookahead.go:copyQueuedFrame`：获取内部拥有的紧凑Frame，复制有效字节，错误传递沿所有调用者迁移；SendFrame 返回后不读外部Frame。
- `stream/yuv_reader.go:ReadFrame`：获取帧后完整读取，EOF/截断/I/O失败均Release；后续读不覆盖仍持有的旧输出。
- 更新 Clone/Release 中与当前深拷贝实现不一致的注释，说明“独立生命周期”而不是误称当前输出会被DPB直接覆盖。

不能只改默认420路径；六格式、奇数几何和10-bit低两位全部保持。返回错误签名如有变化，先查引用后迁移每一个调用者，不保留忽略错误的兼容包装。

## 4. K15c：Encoder 工作图像与 RC 候选

新增实例私有 picture.Pool，复用现有池实现。不要使用全局默认池掩盖不同编码器的所有权。

### 4.1 最小所有权模型

- 所有 owning picture 由编码器主流程管理；字段 reference/recon/last/bPast/bFuture 只是借用角色，不独立持有可释放回调。
- 推荐用小型固定存活集合/显式 ownership record 管理同一 picture 的多个角色；在角色变化后以指针去重，只释放不再被任何角色或在途任务引用的对象。
- 如果采用引用计数，应只在 owning lease 上增减，FieldView/分量视图不携带可释放计数；不能让每个普通 Picture 指针复制暗含一次retain。
- 源图在当前试编码和只读分析结束后立即归池；选中重建在最后一个角色消失后归池。

### 4.2 RC 路径逐项落实

在 `rate_control.go` 中对以下路径显式释放候选：encode错误、padding失败、AU预算不合格、VBV不合格、最佳候选被替换、搜索最终失败。成功候选转移到提交记录；defer不能在转移后仍释放它。

不并行RC尝试；保持 `qpSearch`、穷举补偿、`preferRateQP` 与反馈历史。failed attempt 不更新参考、帧号、RC、输出槽或 B 队列位置。

Reset/Close：先结束当前公开调用内所有任务，再释放每个唯一存活图像、队列Frame与scratch，最后Trim。重建快照已经复制为公共Frame，不受影响。

## 5. K16：slice-local scratch 与元数据分离

### 5.1 Decoder

将 `frameState` 拆成只读帧描述、帧级可分区数组、slice执行上下文三类，先做纯串行等价重构再开并行：

- slice执行：当前component/activeMB/field/parity、CABAC状态、临时残差、MC patch、变换中间数组。
- 帧级可分区：decoded/info/NZ/MVD/direct标记，按owner确定独占写范围。
- 参考帧保留：Motion、BlockReferences/referenceIDs、MBField及独立色平面对应数据。不能在frame结束时和scratch一起清空归池。

`fullResidual` 是指针，共同444当前每帧一份；不能靠浅拷贝frameState获得worker私有性。每个活跃worker准备一份固定scratch，不为每slice复制整帧数组。

DPB保留元数据采用明确lease/独占转移；参考淘汰时释放，临时scratch在join后立即复用。时间/内存复杂度目标为 `O(frame metadata + workers × bounded scratch)`，不是 `O(slices × frame metadata)`。

### 5.2 Encoder

`rdCandidate` 已有固定像素/MV/NZ快照，尽量复用其值语义；不要为每个候选转成interface或指针对象。CAVLC真实试写缓冲、宏块residual、MC/量化scratch、families/候选小列表均按固定上限复用。

源AQ活动度可按帧缓存纯统计，RC每次QP仍按同样裁剪逻辑算最终QP；用户宏块QP回调继续主调用内串行、原顺序执行，不进worker。

共享 buffer 包不认识 codec 私有 mbInfo/residual 类型；这些typed scratch留在各codec包，避免为了“统一池”暴露内部类型或引入依赖环。

### 5.3 本轮精简实施边界

本轮不为尚未通过收益准入的并行预建任务系统。decoder 先复用表示级临时元数据，Motion/reference IDs/MBField 仍按原契约转交 DPB；临时存储只能在完整重建、覆盖检查与去块完成后回收。encoder 的 attempt、best、committed 及场/平面通道各自独占元数据；释放旧元数据不等于释放仍作为参考的像素。

两侧 typed 元数据空闲缓存各以 16 MiB、最多三个槽为上限，按实际 capacity 计费；活跃合法工作不受空闲预算拒绝。Reset 可保留有界缓存，Close 解除实例缓存。不复用公共对象头，不使用全局在用注册表，不把清除引用冒充 retained backing 已池化。


## 6. K17/K18：零分配门禁边界

### 必须为0

- 已迁移的每个simd生产包装和Generic/后端kernel。
- 已准备好frame/slice上下文后的正常单MB重建（CAVLC与CABAC分别覆盖真实路径）。
- 已准备好源/参考/候选scratch后的正常模式决策，包括实际候选试写而不是只测空Skip。
- 固定容量scratch的复用、MC patch构造、变换/量化和滤波。

### 不强行宣称为0

首次New/首次尺寸初始化、合法SPS切换、公共Frame头与租约、归调用者独立拥有的Packet.Data、Clone、Reconstruction快照接口、输出队列增长和错误字符串。这些要有 B/op/allocs/op 报告及有界策略，但不能牺牲所有权换0。

AllocsPerRun 在准备与预热之后测量，迭代内必须恢复正确MB上下文或执行有效重复任务，校验输出仍正确。不能测未写像素的no-op、总是Skip的平坦帧，或把make移到每次未计时的setup后宣称真实路径零分配。编译器逃逸诊断用于定位，不替代运行计数。

## 7. 所有权验收用例

至少覆盖：

1. 两张同时持有的输出互不覆盖；释放其中一张后复用不影响另一张。
2. Release重复、nil、Clone独立、Reset/Close后输出仍有效；callback恰好返还一次。
3. 六格式/尺寸桶隔离；旧缓冲用非零脏数据填充，不能泄露到新Frame有效区域。
4. 截断YUV、decode错误、备用表示失败、RC拒绝/替换/整次失败均无租约遗留或双归还。
5. B未来参考替换时，旧bPast仍可用于B重建；该图不能提前回池。
6. Frame输出保留时DPB可继续淘汰，像素不变；referenceIDs/MV在后续temporal-direct仍正确。
7. 多次格式切换后缓存字节/桶数不持续增长；上限统计包含实际capacity。
8. FieldView从不归池、不单独扩边；worker停止后才能释放原图。
9. race检测不同输出并发Release、多个独立实例，不测试未承诺的同实例并发Send。

现有 `picture/pool_test.go`、field/high_bit_depth/layout测试、encoder失败重试/B/lookahead测试和stream所有权测试全部保留，不通过删掉error/释放断言实现池化。
