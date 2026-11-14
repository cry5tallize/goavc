# 架构

> 本文档定义分层、依赖方向、内存所有权与错误模型。任何破坏这些约定的改动，
> 都必须在 PR 中显式讨论并同步更新本文档，否则视为架构违规。

## 1. 分层

```text
Public API      encoder.go decoder.go frame.go packet.go config.go errors.go
    │
    ├── Encoder        encoder/       （M4 起）
    ├── Decoder        decoder/       （M2 起）
    └── Stream         stream/        （M1 接口，M6 完整）
         │
         ▼
    Codec Pipeline
         │
         ├── Syntax     syntax/        SPS / PPS / Slice / SEI / VUI / ScalingList
         ├── NAL        nal/           Annex-B / AVCC / avcC / Access Unit
         ├── Entropy    internal/entropy/{cavlc,cabac}
         ├── DPB        internal/dpb
         └── Shared Codec Core
                 ├── picture/    图像存储（宏块对齐、边缘扩展区）
                 ├── prediction/ 帧内/帧间预测
                 ├── transform/  整数变换与量化
                 ├── motion/     运动估计（仅编码器使用）
                 ├── filter/     去块滤波
                 └── simd/       平台内核 + 标量回退
```

数据流方向只有一个：上层调用下层，下层不认识上层。

## 2. 依赖规则

| 编号 | 规则 | 检查方式 |
|---|---|---|
| R1 | `encoder` 与 `decoder` 不得相互 import | `tools/depscheck`（自动） |
| R2 | `internal/*` 不得 import `decoder` / `encoder` / `stream` | `tools/depscheck`（自动） |
| R3 | `internal/simd` 只能被白名单内的底层包使用 | `tools/depscheck`（自动） |
| R4 | 公共 API 不暴露内部包类型 | 公共 API 快照测试（M9）+ review |
| R5 | 禁止 import 环 | `tools/depscheck`（自动） |
| R6 | 禁止单文件巨型化（建议 ≤ 800 行有效代码） | review |

`tools/depscheck` 在 CI 中以 `go run ./tools/depscheck` 执行，任何一条违规即阻断合并。

R3 的白名单是**显式**的：

```text
internal/prediction  internal/transform  internal/motion
internal/filter      internal/buffer     internal/simd
decoder              encoder
```

新增一个包想直接使用 SIMD 内核，必须修改白名单——这次修改本身就是一次依赖评审，
而不是一次隐式的耦合。

## 3. 为什么编解码必须共享内核

编码器需要"本地重建"：它必须用与解码器**完全相同**的反量化、逆变换、预测、
去块滤波逻辑，构造出解码器将来会得到的参考帧。一旦两侧实现有差异
（哪怕只是舍入方式不同），编码器的参考帧就会与解码器的参考帧逐渐漂移，
表现为"码流越长，画面越偏"。

因此以下模块只允许存在一份实现：

| 模块 | 编码器用途 | 解码器用途 |
|---|---|---|
| transform 反向路径 | 本地重建 | 残差重建 |
| prediction | 计算预测 | 计算预测 |
| filter | 滤波参考帧 | 滤波输出帧 |
| picture | 工作缓冲 | 工作缓冲 |

"两份实现互相验证"在本项目中是被禁止的：它们会一起错。

## 4. 尺寸与裁剪

- `Frame.Width/Height` 是**显示尺寸**，可以是奇数（例如 641×361）。
- 内部 `picture` 缓冲使用**编码尺寸**：宽高按 16 对齐，并用边缘复制填充。
- 解码输出前按 SPS 的裁剪窗口裁到显示尺寸；编码输入时按 16 对齐补齐。
- 因此 `PixelFormat.PlaneDimensions` 一律使用向上取整，不假设偶数。

这样上层永远只看到真实画面，不需要自己处理 Crop；内部算法又始终面对对齐尺寸，
可以安全使用 SIMD 的越界读。

## 5. 内存所有权

| 场景 | 契约 |
|---|---|
| `SendFrame(*Frame)` | 返回后调用方可复用/释放输入；默认调用内消费，WithLookahead使用有界内部深拷贝，不保留调用方Frame/Data引用 |
| `ReceiveFrame() (*Frame, error)` | 返回帧归调用方；用完必须 `Release()`，否则缓冲无法回池 |
| `SendPacket(Packet)` | 解码器不在返回后持有 `Data` 引用（需要保留则内部拷贝） |
| `ReceivePacket() (Packet, error)` | `Data` 归调用方所有 |
| `Encoder.Reconstruction()` | 最近成功编码帧的独立重建快照，不消耗 Packet；调用方用完 Release |
| `stream.FrameReader.ReadFrame()` | 成功帧归调用方，后续读取不覆盖；YUVReader每帧独立缓冲，用完Release |
| `stream.FrameWriter.WriteFrame()` | 同步消费且不保留输入引用，不转移Release责任；底层I/O对象关闭权仍属调用方 |
| `stream.AnnexBReader.ReadNAL()` | 返回的EBSP独立归调用方，后续读取不覆盖；不组装AU或推断时间戳，底层Reader仍由调用方关闭 |
| `stream.AVCCReader.ReadNAL()` | 独立EBSP；FromConfig先交付构造时复制的SPS/PPS/扩展再读样本，不保留原avcC引用；不组装AU或推断时间戳 |
| `stream.AUReader.ReadAccessUnit()` | 独立Annex-B Packet；复制挂起NAL以允许源复用缓冲，后续读取不覆盖；裸流PTS/DTS/Duration均0，KeyFrame仅IDR |
| `stream.AnnexBWriter.WriteAccessUnit()` | 同步消费Annex-B Packet，完整封装和预算验证后才写出；不保留Data，不写时间戳，不关闭底层Writer；传输失败粘性 |
| `stream.AVCCWriter.WriteAccessUnit()` | 同步消费Annex-B Packet并写AVCC长度前缀；全部NAL的长度容量/封装/预算验证后才写出，不保留Data，不写avcC或时间戳，不关闭底层Writer；传输失败粘性 |
| `stream.EncodeStream()` | 拥有内部Encoder；读到的输入帧总会Release；逐次同步消费输出，正常EOF自动Flush；不关闭调用方I/O |
| `stream.DecodeStream()` | 拥有内部Decoder；输出Writer返回后Release帧，错误/取消也释放；正常EOF排空显示队列；不关闭调用方I/O |
| `Frame.Clone()` | 独立内存，不参与池管理，调用方负责释放（GC 回收即可） |
| `Frame.Release()` | 幂等；调用后帧失效，`Validate` 返回 `ErrInvalidData` |

不允许"池化缓冲被库外长期持有"：需要长期持有就 `Clone`。

## 6. 错误模型

```text
ErrAgain          当前无输出，属于流控信号，不是错误状态
ErrEOF            (= io.EOF) 流结束且输出已排空
ErrInvalidData    输入非法/截断，致命，需 Reset 后等待 IDR
ErrUnsupported    合法但未实现，致命，必须显式返回而不是降级处理
ErrResourceLimit  超出配置上限，致命
```

规则：

1. 一律用 `errors.Is` 判断；内部用 `%w` 附加上下文。
2. `ErrUnsupported` 与 `ErrInvalidData` 必须严格区分：前者是兼容性边界，
   后者是输入问题，两者的恢复策略不同。
3. 绝不"静默降级"：未支持的工具不能退化成近似实现，否则产出的码流
   在别的解码器上会变成花屏。

## 7. 并发模型

- `Encoder` / `Decoder` 实例**非并发安全**：同一实例不得被多个 goroutine 同时调用。
- 多路视频请使用多个独立实例（这是项目支持的主要并行方式）。
- 内部实现的并行（slice 并行、帧流水线）必须在 M8 完成依赖分析之后进行：
  宏块预测依赖、slice 边界滤波强度、DPB 访问都存在数据依赖，
  未经分析直接并行会产生难复现的错误。
- 并行版本的输出必须与串行版本逐比特一致，并跑 `-race`。
- M6同步pipeline不创建worker或预取队列；慢消费者直接背压。取消在单帧操作边界检查；
  阻塞端点须实现Context*方法，或由I/O所有者关闭/设置deadline以中断，不能用遗留后台Read伪取消。
  MaxInputBuffer是单个输入载荷预算，NAL/AU源、codec DPB/Lookahead和Writer缓存另有各自限额，见`docs/stream.md`。

## 8. 平台抽象

```text
上层业务代码
     │
     ▼
internal/simd  dispatch（包初始化时选择一次）
     │
     ├── generic.go  纯 Go 标量实现（永远存在）
     ├── amd64.s     SSE2 / AVX2
     └── arm64.s     NEON
```

- 汇编文件只允许出现在 `internal/simd`。
- 每个 kernel 必须有对应的标量实现与等价性测试（含非对齐 stride、奇数尺寸、极值输入）。
- 运行时分派结果在包初始化时确定，热路径不允许反复查表。
