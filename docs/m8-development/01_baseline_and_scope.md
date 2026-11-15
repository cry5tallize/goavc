# 01：代码基线、范围与优先级

## 1. 当前事实，不是实施承诺

| 位置 | 已核对的实际状态 | M8 必须怎样处理 |
|---|---|---|
| `internal/simd/doc.go` | 只有包契约，尚无 CPU 检测、函数表或汇编 | 从可运行的 Generic 开始，不把注释中的文件当已有实现 |
| `internal/buffer/doc.go` | 只有池化目标说明 | 新实现前先确定原始缓冲层不反向依赖根包或流水线 |
| `internal/picture/pool.go` | 已有按宏块宽高、PixelFormat 分桶的互斥池，Get/Release/Stats/Trim，默认每桶 4 张 | 扩充现有实现，不能新建第二套 PicturePool；现有每桶上限不等于全池字节上限 |
| `internal/picture/picture.go` | 8-bit `[]byte` / 10-bit `[]uint16`；Stride 以采样计；行跨度相对对齐，起始地址不保证 32/64 字节对齐 | 默认使用非对齐加载；不能把 stride 对齐当基址对齐 |
| `internal/picture/field.go` | 场视图借用同一张图，行跨度加倍 | SIMD 不可用另一场的像素充作垂直 halo；不得独立 Release/FillBorder |
| `frame.go` | `onRelease` 私有，当前生产包没有注入者；Clone 独立分配、Release 清空切片并调用一次钩子 | 单有 Release 不代表公共输出已池化；不能从 decoder/stream 直接设置私有字段 |
| `config.go` | EncoderConfig、DecoderConfig 已有 Threads；0 自动、1 串行的契约基础 | 复用字段，不再添加 Workers/Parallel 等同义字段 |
| `encoder/encoder.go:New`、`decoder/decoder.go:New` | `Threads > 1` 仍显式 ErrUnsupported | 并行任务完成后迁移构造检查、契约测试与 CLI 帮助，不能只删除拒绝条件 |
| `cmd/internal/codeccli/encode.go`、`decode.go` | `-threads` 默认 1，帮助仍写仅支持 0/1 | 保留默认 1 的兼容行为，完成后更新帮助和端点测试 |
| `decoder/frame.go` | 每帧创建 frameState、多组 MB/NZ/MV/referenceIDs 元数据 | 先分离 frame-wide 数据与 slice-local scratch；部分数组会被参考帧长期借用 |
| `encoder/frame.go`、`encoder/encoder.go` | 源图与候选重建用 picture.New；模式决策可反复尝试，码控有重试 | 池化必须覆盖失败候选、成功引用、重建快照与 B 双锚点寿命 |
| `encoder/lookahead.go` | 有界深拷贝、串行场景差异分析；没有 worker | 只并行纯分析，不让 worker 修改参考/码控/输出槽 |
| `encoder/b_schedule.go` | 未来锚点先编码，B 帧后输出，具有 PTS/DTS 和事务恢复 | 不将显示顺序输入当编码顺序；不能跨参考依赖并发 encodeFrameNow |
| `stream/pipeline.go` | 同步拉取和排空，慢 Writer 直接背压，无预取 goroutine | M8 编码器内部并行不改变 Stream I/O 所有权，不新增外层异步读 |
| `.github/workflows/ci.yml` | OS 标签矩阵，不能据此证明某个 job 实际是 arm64 | M8 原生 NEON 证据必须打印 GOHOSTARCH/CPU，并在真实 arm64 跑测试 |

上述代码引用定位来自本次源码读取及 gopls；行号会漂移，接手时按符号定位。M8 只能优化已经支持的语义，不扩大 Profile 认证范围。

## 2. 历史证据如何使用

### 2.1 Decoder

[性能文档](../performance.md) §5 的 M2 基线为三帧 1080p Baseline/CAVLC、Generic，中位 21.54 FPS，包含 New/Flush/消费/Close。这是旧代码、短序列、特定工具的冷启动端点，不是 M7 当前速度，不代表 CABAC、B、MBAFF 或高位深。

M8-A 必须保留这一基准以观察长期变化，同时新增长序列稳态指标；不能用不同 op 含义直接计算“提升百分比”。

### 2.2 Encoder

历史 M5 300 帧 profile 指向 `eec92c7`，不是 M7 HEAD。其 flat 热点含 `prediction.pixClamped` 41.10%、`search.SAD` 2.45%；`MCBlockLuma` cumulative 59.16%。累计比例包含子调用，不能相加。

该证据说明应首先调查逐采样钳位和重复插值，而非假定“写完 SAD 汇编就能实时”。当前版本更换了位深和共享实现，符号及热点比例必须重新采样。

§14 的 720p High/CABAC/B2/4slice/8×8/权重/Lookahead2 长测吞吐远低于 30 FPS，并含同机并行验证负载。它不能作为独占性能基线，也不能拿较快的平坦 P-Skip 或 PCM 替代该场景宣称达标。M8 对 30 FPS 的可行性尚无当前测量证据。

### 2.3 优先级规则

1. A00 锁定当前输入、参数和哈希，K21/K22 的最小测量能力提前落地。
2. 先 SAD 16×16 把 dispatch/parity/ABI 路径走通。
3. 随即做 MC interior/border 分离与六抽头复用；这是历史证据指向的首要优化机会，不等待所有 SATD 后端完成。
4. 其余按当前 profile 排序；变换、量化、去块、残差和 SATD 均保留原计划交付项。
5. 内存工作可与接口冻结后的汇编任务并行，后端切换及公共入口由唯一集成负责人修改。
6. 并行前先完成切片隔离和所有权模型，禁止一边全帧池化一边改 worker 生命周期。

## 3. 开工时必须重新固化的快照

输出到本地临时结果目录，最终仅将小型汇总和复现命令入库：

- 完整代码 revision、工作区是否有改动、Go 版本与 CGO/GOFLAGS/GOAMD64/GOARM64/GOMAXPROCS。
- CPU 型号、核心/线程、OS、内存、当前电源模式、是否独占运行。
- 55 条 manifest 的实际内容及 SHA-256；原 M2/M3 27 份 High10 投影用例继续作为保护网。
- 每条基准的输入 SHA、帧数、格式、QP、Profile、熵、B/GOP/IDR、slice、权重、矩阵、前瞻和码控。
- 编码 Packet.Data/PTS/DTS/Duration/KeyFrame、显示顺序重建；解码原生有效像素、时间戳和帧数。
- 对现有输出建立比较器，而不是把生成的新输出更新为新 golden。
- 每个拟优化入口的分配 profile、CPU profile 和真实调用占比。

若基线用例当前失败，保存失败事实并作为独立修复任务；不得为了推进 M8 禁用用例或覆盖 M7 证据。正确性修复与性能提交分离。

## 4. 非目标与禁止捷径

- 不加 CGO、C/C++、x/sys/cpu、外部编解码库或 `go:linkname` 引用 runtime 内部 CPU 状态。
- 不改搜索半径、采样位深、QP、RDO 候选、权重顺序、B 策略、slice 划分或 HRD 信令来提高 FPS。
- 不把 10-bit 右移为 8-bit 再做 SAD/MC；低两位纹理是必测输入。
- 不将 H.264 Forward8x8 换成另一个“更标准/更快”的算法，现有编码策略的逐比特结果是本阶段契约。
- 不汇编化 CABAC/CAVLC；若 profile 指向熵路径，只做可证明的表/分支/缓冲优化，单独提交并保留编码结果。
- 不实现 PAFF、transform bypass 或其他 M7 当前 Unsupported 能力。
- 不用 `sync.Pool` 命中概率证明硬内存上限或零分配。
- 不把 race 通过解释为汇编无越界；不把交叉构建解释为原生 parity 通过。

## 5. 高风险先决项

| 风险 | 阻断条件 | 对策 |
|---|---|---|
| AVX2 只检查 CPUID、忽略 OS 状态 | 任何 YMM 指令可在 OS 未启用状态时执行 | 检查 XSAVE/OSXSAVE/AVX 与 XCR0 后再启用 AVX2 |
| 非对齐行末或 FieldView 越读 | 依靠 slice cap 或父缓冲“通常够大” | 基于实际 len 和采样 halo 证明；尾部标量/局部固定 scratch |
| 窄位 SIMD 对 int32 系数饱和 | 只测自然视频小系数 | 保留 int64 域、溢出与有符号舍入；宽域不能证明则走 Generic |
| slice 邻域先读别的 worker 的 metadata 再判断 sliceID | 像素不跨 slice 但仍发生数据竞争 | 先读不可变 owner 表，再决定是否可读取可变数组 |
| 池复用 DPB 仍引用的 MV/NZ/场标记 | 当前帧结束即清空所有 frameState 数组 | 分离借出元数据与临时 scratch；参考帧逐项释放 |
| Frame 头复用导致旧指针再次 Release 新帧 | 将同一个 *Frame 放回池再交给新消费者 | 公共 Frame 头不复用，只池化 backing storage |
| 前瞻投机结果使用了错误的场景锚点 | 未来分析触发 IDR 或码控提前提交 | 结果绑定 frame ID、anchor ID、epoch；主线程按旧语义消费 |
| 并行候选改了相等代价 tie-break | 以先完成者为胜者 | 保留原候选序号，按 cost/原序号归约 |

具体约束见 [总契约](02_contracts.md)。
