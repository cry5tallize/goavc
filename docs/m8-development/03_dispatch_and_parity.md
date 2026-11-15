# 03：CPU 分发、构建与 parity 基础设施

对应原计划 K01/K02/K03/K14；相关任务见 [08](08_task_schedule.md)。下列文件和测试名称除显式标明已有者外均拟新增。

## 1. K01：CPU 检测

### 1.1 文件拆分

在 `internal/simd` 中建立：

- `cpu.go`：小型不可变 feature 值类型，纯函数选择策略；不依赖 OS 或 codec。
- `cpu_amd64.go`、`cpu_amd64.s`：CPUID/XGETBV 声明和最小汇编，约束 `amd64 && !purego`。
- `cpu_arm64.go`：本项目支持的 Linux/Windows/macOS arm64 使用架构基线 ASIMD/NEON；约束 `arm64 && !purego`。
- `cpu_generic.go`：`purego || (!amd64 && !arm64)`，不声明或引用汇编。
- `dispatch.go`：构建完整 typed table，init 选择一次。
- 各类 `*_generic.go`：永远可构建，作为唯一标量实现与直接 parity 参照。
- `backend_test.go`、`cpu_test.go`、`parity_test.go`；强制后端工具另放 `goavc_simdtest` 标签文件。

文件名不是平台判断的全部：`.s` 顶部同样加正确 build constraint，避免 `-tags purego` 仍链接汇编。不能只给声明文件加标签。

### 1.2 amd64 检测顺序

1. CPUID(0) 获取最大 basic leaf；访问 leaf1/leaf7 前检查最大 leaf，不假设虚拟机暴露完整叶。
2. leaf1 EDX bit26 表示 SSE2；amd64 通常基线有 SSE2，但选择逻辑仍用可测试 feature 值，不硬编码测试结果。
3. AVX2 必须同时满足：leaf1 ECX XSAVE(bit26)、OSXSAVE(bit27)、AVX(bit28)，且 leaf7/subleaf0 EBX AVX2(bit5)。
4. **仅当 OSXSAVE 已确认时执行 XGETBV(0)**。要求 XCR0 的 XMM/YMM 位均置位，即 `xcr0 & 0x6 == 0x6`。
5. feature 层得出“可执行 AVX2”，而不是“CPUID 中存在 AVX2 字样”。不支持时不执行任何 YMM 指令。
6. CPUID 探测只在初始化执行；每次 SAD/MC 不能调用 CPUID 或 XGETBV。
7. AVX2 kernel 返回前处理上半寄存器状态（通常 VZEROUPPER），不得在 SSE2 fallback 路径执行 AVX 指令。

测试 feature 组合用纯函数和模拟数值，不在真实不支持 CPU 上直接执行 XGETBV/AVX2。构建基线 `GOAMD64=v1` 必须可用；不能依赖 `GOAMD64=v3` 掩盖检测遗漏。

### 1.3 arm64 与其他平台

本阶段只使用 arm64 基线 Advanced SIMD，不使用 SVE、可选 crypto 或扩展指令；三种目标 OS 的 Go arm64 运行前提与基线能力核对后登记。无需为基线 NEON 增加 Linux-only HWCAP 读取，更不能引用不存在的标准库 `internal/cpu` 作为依赖。

若未来采用可选 arm64 扩展，必须单独设计 OS 能力检测；不能把本阶段“架构基线可用”推广到全部未来指令。未知架构、386、purego 均走 Generic。

### 1.4 验收

拟新增 `TestCPU_AVX2RequiresOSState`、`TestCPU_MissingLeavesUseGeneric`、`TestCPU_PureGoHasNoAssemblySelection`。证明所有 feature 组合的选择；native parity 另负责证明指令结果。不能只断言函数表非 nil 或打印后端名。

## 2. K02：typed registry 与调用链接入

“registry”解释为编译期完整函数表，不做动态注册：

1. 先从 SAD 拆出唯一 Generic，实现 Go function type；其余槽按逐项迁移加入，不提前造几十个未用占位接口。
2. 对每槽记录 kernel ID、样本/系数域、实际实现 ID；适合使用固定结构体/定长数组，不用热路径 map。
3. 初始化先绑定全部 Generic，再按 feature 和每槽真实支持能力替换；AVX2 可以与 Generic 的宽域量化槽并存。
4. 上层共享包只调用 simd 包装入口，不能分别在 encoder/decoder 绑定不同实现。
5. 修改实际 `window.consider` 私有 `sad[T]` 调用路径，不只修改 `search.SAD` 导出包装。
6. R3 增加精确目录 `internal/motion/search`、`internal/picture`；更新依赖规则测试与 architecture 白名单。不要将整个 `internal/**` 放行。
7. `simd` 测试不要 import transform/picture/prediction：importgraph 按目录聚合测试 import，不区分构建标签，可能形成反向环。领域集成测试放回各领域包。
8. 保留现有领域 API 签名。迁移导出入口前查引用；不留下旧函数 alias、重复标量算法或未接入的第二套实现。

生产热路径最多一次已绑定函数指针调用或可内联包装，不接受每调用 switch 后端/查字符串。尺寸、位深、边界选择属于必要适配，但应在每块一次，而非每像素判断。

## 3. K03：可复用 parity 测试

### 3.1 组织方式

- simd 包内测试直接得到 Generic 与可用的具体后端函数。真实 CPU 不支持的后端显式 Skip 并计入“未执行”，不回退后仍标成该后端成功。
- 测试输入在循环外生成，用固定种子；失败打印 seed、kernel、backend、stride、offset、位深、QP、相位、首个差异。
- 除整块输出，还比较未写邻域、输入不变、必要元数据或 error sentinel 分类。
- parity 不调用自己写的另一后端计算期望之外，还复用现有规范解析向量；多个后端共同出错不会被互比发现。
- 零分配断言覆盖真实 dispatch 包装，不只直接调用汇编。

### 3.2 通用输入矩阵

| 维度 | 必测值 |
|---|---|
| 样本 | 8-bit：0/1/254/255；10-bit：0/1/2/3/511/512/1022/1023；棋盘、冲激、斜坡、固定随机 |
| 起点 | byte offset 0..31；uint16 offset 0..15 个采样；全部保持 Go 元素对齐但不假设 SIMD 对齐 |
| stride | 紧凑、width+1、width+3、非 16/32 倍数；不同输入不同 stride；FieldView 加倍 stride |
| 宽高 | 1/3/4/7/8/13/15/16/17/31；固定块接口只跑合法大小，通用包装覆盖尾部 |
| 容量 | 最小合法 len，特别是最后一行无 padding；不能拿多分配一页作为默认输入 |
| 别名 | 原先允许的原地；不重叠 dst/src；不承诺未支持的任意部分重叠 |
| 系数 | 正负奇数、边界有效值、int32 极值和会触发溢出的向量；仅在入口声明域内使用 |
| 误差 | 完全相等，无 epsilon，无平均误差，无截断到 8-bit 后比较 |

### 3.3 越界证明不能只靠 canary

- canary 能发现越写，不能发现读取了未使用的 padding。
- Go race 不插桩汇编内存读写，不能证明汇编 race-free。
- 为固定宽度入口审查所有 load/store 的最大地址；用最小 len 测试、相邻区域差异值检查污染。
- K14 增加测试期 guard-page 子进程测试：用标准库 OS 接口建立末端保护页，输入最后合法采样靠近页边；子进程异常不能杀死整个测试协调器。平台专用代码仍放 simd，unsafe 只在这里使用并写理由和检查范围，不引入 CGO。
- guard-page 不能代替头部/跨行/FieldView 语义测试。无法在某平台建保护页时明确跳过并保留静态访问证明，不声称动态边界测试已通过。

### 3.4 后端强制测试机制

生产不提供全局可变 setter。拟新增仅在 `goavc_simdtest` 标签下编译的辅助入口，放在 simd 包中，返回恢复函数和能力错误：

- 设置前使用进程内测试锁；不与 `t.Parallel()`、并发 benchmark 或存活 worker 混用。
- 只允许真实 CPU 支持的后端或 Generic；强制缺失能力返回可识别的“不支持测试后端”，调用方 Skip/报未测，不执行非法指令。
- 测试结束先 join 所有 codec 内部任务，再恢复原表，最后解锁。
- 所有同进程会使用 simd 的后端对照测试统一使用该锁；不能一个测试在切换，另一个测试不经锁 t.Parallel。
- 普通 `go test ./...`、生产二进制均不包含 setter。
- encoder/decoder 允许直接 import simd 的事实只用于测试强制，不改变生产共享内核依赖方向；`tests/codec` 不为测试便利绕过 R3，可通过包内专用比较测试或独立二进制做端点验证。

纯 Go 参照可用独立进程 `-tags purego`，此时不依赖 setter。基准名称 `_Generic` 本身不能强制任何后端。

## 4. Go 汇编工程要求

- 所有 `.s` 在 `internal/simd`；CPU 探测也不例外。
- 使用 Go 工具链支持的汇编和 ABI，声明参数/返回值布局逐一核对；不能凭另一架构偏移照抄。
- 仅在 leaf 且满足约束时用 NOSPLIT；栈上 scratch 大小时核对栈需求，不能为了性能关闭必要的栈增长机制。
- 无 Go 指针藏在整数或全局汇编缓冲，不能跨调用保留 Go 指针。无需汇编分配内存。
- SSE2 路径不引用 SSE4/AVX 指令；AVX2 不额外假定 BMI/FMA；NEON 不夹带 SVE。
- 默认不要求对齐加载；若某专门路径要求更强对齐，由 Go 包装检查并有同语义回退。
- signed widening、算术右移、饱和打包前的 clip 都写明范围依据；不以最终 clip 掩盖中间溢出。
- 包装的 `noescape` 只在确实不逃逸且不保留指针时声明；AllocsPerRun 与逃逸诊断一起核对，不能只添加指令注释“强制零分配”。

## 5. K14 收尾条件

每个已宣称实现的后端/内核必须有：原生运行记录、合法域 parity、别名/尾部/越界证明、allocation=0、有效调用链集成、对应 before/after microbench。未实现或宽域回退的槽清楚列出；不因函数表用了 AVX2 名称便把所有槽填为 AVX2。

命令与平台矩阵统一见 [09](09_validation.md)，测量协议见 [10](10_performance_and_release.md)。
