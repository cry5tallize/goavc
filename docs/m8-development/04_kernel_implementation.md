# 04：逐类内核实施说明

所有任务遵守 [总契约](02_contracts.md) 和 [parity 基础](03_dispatch_and_parity.md)。下文“新增文件”均为建议拆分名，后续可按同一 kernel 的 Go 包装/Generic/架构汇编分文件；单文件不堆成整套编码器。

## 1. K04/K05/K06：SAD 16×16 与通用矩形

### 真实入口

`internal/motion/search/search.go`：导出 byte `SAD`，私有 `sad[T byte|uint16]`，`window.consider` 的直接参考路径和 MC scratch 路径。`EstimateWithCost` 的 Scorer 接受原始 SAD，再结合 MV 比特排序。

### 实施步骤

1. 将唯一 SAD 标量循环迁入 simd 的 byte/uint16 入口；领域包装保留旧签名。
2. 在搜索进入 sample 类型分支的位置绑定相应内核，覆盖私有调用链，不在每像素做 interface/type switch。
3. 首先实现 SSE2 8-bit 16×16：无符号 byte SAD，使用足够宽的累加槽。宽度不满足固定入口时按完整块/尾部处理。
4. AVX2 不允许一次从 16-byte 行读取 32 byte。可以把两行拼入寄存器或使用窄 load；后一行 stride 和末行 len 必须真实可读。
5. NEON 用无符号绝对差与逐级 widening reduction，不让 16-bit lane 聚合整个 10-bit 16×16。
6. 补 10-bit uint16 内核，不使用 byte SAD 指令直接解释 LE 数据；先 unsigned 比较/差值再 widening。最大 261,888。
7. 通用矩形包装覆盖 4×4、8×8、8×16、16×8、16×16 和 7×13 等有效奇数矩形；固定汇编入口只接受自己的尺寸。
8. 不改 Scorer 调用时机、SAD 标度、候选顺序和严格 `<` tie-break。

### 验证

复用 `TestSAD_ExtremesAndUnalignedStride`、`TestSearch_CostCallbackPreservesReportedSAD`、High10 搜索低两位与边界用例；新增每后端独立 parity、最小末行长度与 AllocsPerRun=0。加入真实运动搜索的位深/负 MV/边界集成对照，不只测试孤立 SAD。

建议提交：`feat[simd]: add sse2 sad kernels`、`feat[simd]: add avx2 sad kernels`、`feat[simd]: add neon sad kernels`。

## 2. K07/K08：SATD

### 真实入口与陷阱

`internal/transform/satd.go:SATD4x4` 被 `encoder/intra.go:blockSATD` 用于真实帧内候选，I4/I8/I16 与色度均可能经过它。矩形 byte `transform.SATD` 当前主要为测试入口。运动搜索不使用 SATD。

当前 SATD4x4 注释仍写 8-bit 差值，但真实 High10 帧内调用会产生 -1023..1023；K07 先用规范解析值补足该真实输入域的文档与测试，不把它降成 byte。

### 实施步骤

1. 迁移 4×4 Hadamard；保留 row/column 布局和 `(sumAbs+1)>>1`。
2. 输入按 signed int32 或已证明安全的 widening lane 处理。10-bit 变换系数与绝对值和不能在 16-bit 累加器中溢出。
3. 8×8/16×16 SATD 明确是独立 4×4 tiled 累加，不是新的大尺寸 Hadamard。
4. 同时验证单个 4×4 调用开销；如果汇编调用比可内联 Generic 慢，应尝试固定批处理接口，保持 blockSATD 的原候选和差值域。
5. SSE2/AVX2 和 NEON 分别提交，不以 byte 矩形包装微基准替代真实 blockSATD 接入。

### 验证

复用 `TestSATD_AnalyticBasisAndStride` 和 `TestIntraSelection_AllModes`；增加正负 1023 冲激/常量/低两位纹理，完整帧内候选选择和编码 Packet 哈希不变。SATD 无“全 int32 输入有效”的承诺，只测声明的像素差域。

## 3. K09：MC 与亚像素，优先级最高的算法保持型优化

### 真实入口

- `internal/prediction/inter_luma.go`：`LumaSample` / `LumaSampleHigh`、`MCBlockLuma` / `MCBlockLumaHigh`，泛型 `samplePlane`、`pixClamped`、`hSample/vSample/jSample`。
- `internal/prediction/inter_chroma.go`：`ChromaSampleFormat[High]`、`MCBlockChromaFormat[High]`；420/422 双线性，444 复用亮度六抽头。
- `internal/prediction/weight.go`：单向/双向加权。

### 3.1 先做共享标量重构，再做汇编

拆成 K09a：无输出变化的块级 MC 重构；K09b/c/d：SSE2/AVX2/NEON。每一步均可独立回退。

1. 在每块入口一次完成 MV 整数/相位分解，特别保持负数的 floor/remainder 语义，不能用向零截断除法替换现有算术右移。
2. 根据当前相位计算**真实需要的 halo**。纯整数 copy 不应强行要求 6-tap halo；双半像素最宽需要 x/y 各 -2..+3。
3. 全部 halo 在逻辑图像/场内：直接用未钳位的 interior kernel。
4. 边缘：用唯一 Generic 钳位逻辑或在固定栈上 scratch 构造扩边 patch，再调用同一 interior 算术；不要保留领域包一份完整 MC、simd 又复制一份。
5. 16×16 亮度最宽 patch 为 21×21 个采样；包含额外对齐/中间行时明确实际大小。w/h 大于当前小块上限时分块或走通用 Generic，不能让固定 scratch 越界。
6. 水平中间值按块/行重用，不为每个输出像素重复计算 36 次带 clamp 的访问；scratch 为任务私有，不能放包级可变数组。
7. 整数 copy、水平半像素、垂直半像素、双半像素、四分之一组合分别实现，入口一次分相位。

### 3.2 必须保留的算术

- 六抽头系数 `[1,-5,20,20,-5,1]`。
- 单轴半像素 `(+16)>>5` 后 clip。
- 双半像素 j 的第一遍中间值**不 clip**，第二遍 `(+512)>>10` 后才 clip。
- 四分之一像素按原规范相位图组合 `(a+b+1)>>1`，不能把任意两张相邻平面平均当成全部 16 相位。
- uint16 10-bit 的六抽头中间值用 signed 足够宽 lane；不能 unsigned 饱和减法。
- 420 色度 8×8 相位，422 色度 8×4 相位；MV 始终以亮度 1/4 像素计。444 不是双线性。
- 真实加权预测为“原参考插值 → 权重 → clip”，不是先对参考帧加权再插值。`weightedSearchReference` 只是编码搜索启发式，不能替代重建参考。
- High offset 的位深缩放、双向先缩放再平均顺序保持；若融合到 SIMD，要单独证明与共享 WeightSample/BiWeightSample 一致。
- FieldView 使用它自己的起点、加倍 stride、逻辑高度；不能用父帧预扩边代替场边缘钳位。

### 3.3 验证

原解析向量：`TestHigh10LumaAllPhasesAnalyticVector`、`TestHigh10LumaSignedIntermediateAndClip`、`TestHigh10ChromaAllPhasesAnalyticPolynomial`、`TestChromaFormat_422AllPhasesAnalytic`、`TestChromaFormat_444SixTapOddStride`、`TestFieldView_MCFieldEdgesAllFormats`。

新增矩阵：所有亮度 16 相位，420 64 相位，422 32 相位；正负 MV；四角/边/内部；1/3/7/13 等尾部；不对齐 stride；0/255/1023 与 overshoot；双向权重；TFF/BFF。先 Generic 规范解析值，再后端 parity，最后全语料与编码三方 Oracle。

性能同时报告 interior、border、FieldView 和真实分区占比；只展示整数 copy 的加速不代表亚像素 MC 完成。

## 4. K10：4×4/8×8 正逆变换

### 入口

`forward.go:Forward4x4/ForwardLumaDC/ForwardChromaDC`，`eight.go:Forward8x8/IDCT8x8`，`idct.go:IDCT4x4/IDCT4x4DC`，`chroma422.go:ForwardChromaDC422`。

### 实施步骤

1. 首先冻结现有精确语义。Forward8x8 是当前整数正交投影，包含 int64 乘积、固定范数除法和恢复符号；不能换常见前向蝶形后说“规范等价”。这会改变编码码流。
2. 4×4 正变换使用残差域 -1023..1023 的范围证明决定 lane 宽度；保留原地。
3. IDCT4×4 输入可接近 int32 极值，现有 int64 temporary、DC+32、水平后垂直、最后 >>6 必须保持。现有 API **无 error 返回值**，不擅自新增错误协议。
4. IDCT8×8 同样 int64，输出越 int32 返回 ErrInvalidData；不可用 16-bit 饱和变换截断恶意输入。
5. 可以采用范围受限的 SIMD 快路与全域 Generic 慢路，但每个范围条件必须显式证明并测试刚好越界值；普通输入确实执行 SIMD，不接受全部调用都落回 Generic 的空实现。
6. DC 快路、420/422 Hadamard、场扫描不因迁移而删除。帧/场差异在扫描与像素寻址，不创造第二套变换。
7. Forward8x8 的常量分母可预推导精确算法；禁止浮点倒数近似、改变负数 rounding 或漏掉校正。SSE2/NEON 不具备所需宽乘法时先做准确部分向量化，公开其有效覆盖，不虚标全程 SIMD。

### 验证

基向量、正负冲激、全常量、极值 checkerboard、原地；复用 `TestIDCT4x4_MixedOddCoefficientsRespectPassOrder`、`TestIDCT4x4_LargeDCDoesNotWrap`、`TestForward8x8_OrthogonalSpatialBases`、`TestHigh10Forward8x8_ExtremeImpulseAnalyticBasis`、场扫描与422 DC用例。保持 SP/SI 的专用系数域重建，不用普通重建链替代。

## 5. K11：量化与反量化

### 入口与准备层

`forward.go` 的 Quant4x4/QuantDC 与 intra/inter variants，`eight.go` 的 Quant8x8/Dequant8x8/Dequant4x4WithScaling，`dc.go`、`chroma422.go` 的 DC。

1. 由 transform 层从 QP'、矩阵、块类别准备乘子/位移/分母；可在参数集、QP 或矩阵变化时缓存，不能每 coefficient 动态解析 ScalingList。
2. 缓存键包含位深、分量、4/8×8、DC 类型、QP'、矩阵内容/不可变版本、死区；不能只按 QP 缓存后复用到另一 PPS。
3. Quant4 的 `(mul*16+matrix/2)/matrix`、intra 1/3/inter 1/6、abs 后量化再恢复符号都保持。
4. Quant8 的整数 `mul/divisor` 在分子中的顺序不变；不得用乘倒数的近似除法产生不同 level。精确 reciprocal 必须有全有效域证明和最后校正。
5. Dequant 中 int64 乘积、+32 和算术 shift、int32 溢出错误保留；超过 SIMD 快路范围时回 Generic 算术，不吞错。
6. 422 DC 正量化由调用者传 QP'C+3；逆量化入口内部加3，不能重复。420 DC、LumaDC 的归一化和 rounding 各自保留。
7. 原子性逐入口记录：Quant4/422 DC 已有预检或整块提交；Quant8、部分 Dequant/IDCT8 可能晚期失败后已部分写 dst。若统一补强必须独立重构提交，并更新共用 Generic 和测试，不伪称旧实现全原子。
8. 不将需要 error 的校验推到只有某些调用者会执行的旁路。编码重建中忽略 error 的既有路径需证明上游输入域，不能为 SIMD 快路再扩大该假设。

### 验证

QP' 0..63 每值；语法负 QP 通过 ScaleQP 接入；matrix nil/flat16/非平坦/1/255/晚期0；int32 极值、溢出、src==dst；DC 420/422/Luma；死区刚好边界的正负系数。复用 High10Quant/Dequant、InterQuantization、ChromaDC422 QP offset 解析测试。三方像素验证之外，**编码 level 和 Packet 字节也必须与基线相等**。

## 6. K12：去块滤波

### 入口

`internal/filter/luma.go:LumaEdge/LumaEdgeHigh/LumaEdgeSpan/LumaEdgeHighSpan`，`chroma.go:ChromaEdge/ChromaEdgeHigh`。上层 `decoder/deblock.go`、`mbaff_deblock.go`、`encoder/deblock.go` 继续决定 bS、QP、参考关系和 slice 边界权限。

### 实施步骤

1. 表索引和阈值推导保留共享 Go 层；SIMD 处理合法边缘 lane 的样本运算，不在汇编重新实现 slice/DPB 规则。
2. 按垂直/水平组织 load，必要时局部 transpose；不能将列跨度当行跨度。
3. bS0 不改，1..3 弱滤波，4 强滤波；保持当前公共入口对无效边缘的处理，不另造错误接口。
4. 弱滤波先保存原 p/q 邻域，不能先写 p1 后拿修改值计算 p0 delta。
5. 10-bit alpha/beta/tc0 按位深缩放，但色度 `tc=(tc0<<shift)+1` 的 +1 不缩放；亮度弱滤波附加量和强滤波 `(alpha>>2)+2` 的 +2 不缩放。
6. Span=1/2/4 等真实分段均可正确处理；MBAFF 混合边缘不强行凑四线或跨场 gather。
7. 444 使用亮度型滤波；420/422 色度线数和垂直几何保持。
8. 只向量化同一条边上可证明独立的 lane，不改变跨边/跨 MB 顺序。首版整帧 deblock 仍串行，在重建 barrier 后执行。

### 验证

阈值差刚好小于/等于/大于 alpha/beta、正负 delta、强弱切换、tc0=0、边缘最小容量、水平/垂直、Span1、场视图、0/1023 clip。复用 `TestLumaEdge_WeakUsesOriginalNeighbors`、High10Threshold/Strong/Clip 解析向量和编码/解码跨 slice deblock 用例。

## 7. K13：残差相加与相减

现有共享相加在 `internal/picture/reconstruct.go:AddResidual4x4/AddResidual8x8`；编码各模块还会在生成残差时做源样本减预测。先用 LSP 查真实减法调用点，再确定哪些循环可合并到原始数组 Subtract，不假设已有导出 `Subtract`。

1. picture 适配层仅算 Index/stride/位深，调用 simd；R3 必须先放行实际目录。
2. 预测样本与 int32 残差先扩到足够宽再加，clip 到255/1023；int32 最大正值加1023不能在 int32 中回绕。
3. 源减预测返回 signed int32；byte/uint16 减法前 widening，不能 unsigned wrap 或饱和减法。
4. 支持4/8×8和真实调用尺寸，FieldView stride 不变。dst/src 允许范围按旧契约，不强行支持未证明的部分重叠。
5. 删除实际被迁移的重复算术，但不将 SP/SI 系数域组合拆成普通 AddResidual。
6. 各后端至少交付有实际生产调用的矢量快路；宽残差边界可以 Generic 回退，必须写明实际后端覆盖。

验证现有 ClipAndStride/High10/8×8/444 全平面用例，增加 int32 极端残差、低两位、未写邻域和场奇偶行不变断言。

## 8. 每类任务完成定义

必须同时交付：唯一 Generic、至少声明目标上的真实后端运算、领域调用链接入、独立规范向量保护、逐元素 parity、边界/别名/错误域、0 分配、同环境前后基准。单独增加 `.s` 或只让直接 microbench 调用汇编不算完成。

SSE2/AVX2/NEON 全矩阵的“已实现 / Generic 宽域回退 / 未实现 / 未原生执行”分别记录；未实现不能填成性能报告里的1.00倍已通过。按 Roadmap 完成要求补齐后再进入最终门禁。
