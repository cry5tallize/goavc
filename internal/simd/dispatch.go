package simd

// Backend 是某个已接入内核实际选择的实现，不能用CPU能力代替它。
type Backend uint8

const (
	Generic Backend = iota
	SSE2
	AVX2
	NEON
)

func (b Backend) String() string {
	switch b {
	case Generic:
		return "Generic"
	case SSE2:
		return "SSE2"
	case AVX2:
		return "AVX2"
	case NEON:
		return "NEON"
	default:
		return "unknown"
	}
}

type sad8Func func(a []byte, as int, b []byte, bs, width, height int) int
type sad16Func func(a []uint16, as int, b []uint16, bs, width, height int) int
type satd4Func func([16]int32) int
type forward4Func func([16]int32) [16]int32
type filterDeltaFunc func([4]int32, [4]int32) [4]int32
type filterLuma8EdgeFunc func([]byte, int, int, int, [4]uint8, int, int, [4]int)
type quantMultiplyFunc func([4]uint64, [4]uint64) [4]uint64
type forward8Func func([64]int32) [64]int32
type idct4Func func([16]int32) [16]int32
type idct8Func func([64]int32) ([64]int32, int)
type lumaDCFunc func([16]int32) [16]int32
type chromaDCFunc func([4]int32) [4]int32
type chromaDC422Func func([8]int32) [8]int64
type wideAdd4Func func([4]int64, [4]int64) [4]int64

// Backends 是逐槽诊断快照。仅随真实内核迁移增加字段，不预建未来空槽。
type Backends struct {
	SAD8, SAD16                                        Backend
	SATD4x4                                            Backend
	Forward4x4                                         Backend
	MCLumaCopy                                         Backend
	Residual                                           Backend
	FilterDelta, FilterLumaEdge                        Backend
	QuantMultiply                                      Backend
	Forward8x8, IDCT4x4, IDCT8x8                       Backend
	ForwardLumaDC, ForwardChromaDC, ForwardChromaDC422 Backend
	WideAdd4                                           Backend
}

type kernelTable struct {
	sad8                        sad8Func
	sad16                       sad16Func
	satd4                       satd4Func
	forward4                    forward4Func
	residualVector              residualVector
	filterDelta                 filterDeltaFunc // nil明确选择直接标量算术，非缺失后端回退。
	filterLuma8Edge             filterLuma8EdgeFunc
	quantMultiply               quantMultiplyFunc
	forward8                    forward8Func
	idct4                       idct4Func
	idct8                       idct8Func
	forwardLumaDC               lumaDCFunc
	forwardChromaDC             chromaDCFunc
	forwardChromaDC422          chromaDC422Func
	wideAdd4                    wideAdd4Func
	quantScalar, residualScalar bool
	backends                    Backends
}

func genericTable() kernelTable {
	return kernelTable{
		sad8: sadGeneric[byte], sad16: sadGeneric[uint16], satd4: satd4Generic, forward4: forward4Generic,
		residualVector: residualVectorGeneric,
		quantMultiply:  quantMultiplyGeneric,
		forward8:       forward8Generic, idct4: idct4Generic, idct8: idct8Generic,
		forwardLumaDC: forwardLumaDCGeneric, forwardChromaDC: forwardChromaDCGeneric,
		forwardChromaDC422: forwardChromaDC422Generic,
		wideAdd4:           wideAdd4Generic,
		filterLuma8Edge:    filterLuma8EdgeGeneric,
	}
}

// 生产表初始化后不变；测试强制入口将在专用构建标签内提供。
var kernels = autoKernels(nativeCPU)

func selectKernels(cpu cpuFeatures) kernelTable {
	table := platformKernels(cpu, genericTable())
	table = installResidualKernels(cpu, table)
	table = installFilterKernels(cpu, table)
	table = installQuantKernels(cpu, table)
	table = installTransformKernels(cpu, table)
	table.quantScalar = table.backends.QuantMultiply == Generic
	table.residualScalar = table.backends.Residual == Generic
	return table
}

func autoKernels(cpu cpuFeatures) kernelTable {
	table := selectKernels(cpu)
	// 同机五次测量中4×4 SSE2约7.3ns，AVX2约16ns；按槽选择，
	// 不因CPU有AVX2就让所有内核都使用它。显式测试仍可选择真实AVX2实现。
	if table.backends.SATD4x4 == AVX2 {
		sse := selectKernels(cpuFeatures{sse2: true})
		table.satd4, table.backends.SATD4x4 = sse.satd4, sse.backends.SATD4x4
	}
	// 五次真实块基准显示小批gather/间接调用更慢；默认保留直接Go标量快路。
	// 显式后端测试仍绑定真实SIMD，不以Auto的Generic回退冒充向量通过。
	if cpu.sse2 {
		sse := selectKernels(cpuFeatures{sse2: true})
		table.forward4, table.backends.Forward4x4 = sse.forward4, SSE2
		// 值ABI的小批乘积/宽加法开销高于共享标量；保留真实显式后端，
		// 不把较慢的部分向量化加入默认编码和解码热路径。
		table.forward8, table.backends.Forward8x8 = forward8Generic, Generic
		table.idct4, table.backends.IDCT4x4 = idct4Generic, Generic
		table.idct8, table.backends.IDCT8x8 = idct8Generic, Generic
		table.forwardLumaDC, table.backends.ForwardLumaDC = forwardLumaDCGeneric, Generic
		table.forwardChromaDC, table.backends.ForwardChromaDC = forwardChromaDCGeneric, Generic
		table.forwardChromaDC422, table.backends.ForwardChromaDC422 = forwardChromaDC422Generic, Generic
		table.wideAdd4, table.backends.WideAdd4 = wideAdd4Generic, Generic
		table.quantMultiply, table.backends.QuantMultiply, table.quantScalar = quantMultiplyGeneric, Generic, true
		table.residualVector, table.backends.Residual, table.residualScalar = residualVectorGeneric, Generic, true
		table.filterDelta, table.backends.FilterDelta = nil, Generic
		// 整条亮度边已通过逐元素验证；定向测量胜出且IPB配对样本无退化。
	}
	return table
}

// ActiveBackends 返回逐槽实际选择的后端，不向调用者暴露可变函数表。
func ActiveBackends() Backends { return kernels.backends }

// BindSAD8 为已准备的搜索上下文绑定不可变后端，候选循环只调用一次函数指针。
// 返回函数只读参数，不保留输入引用；生产表初始化后不会改变。
func BindSAD8() func([]byte, int, []byte, int, int, int) int { return kernels.sad8 }

// BindSAD16 是10位采样的对应绑定入口，生命周期和单位与SAD16一致。
func BindSAD16() func([]uint16, int, []uint16, int, int, int) int { return kernels.sad16 }

// SAD8 返回两个合法矩形的原始8位绝对差和，stride以采样计。
// 调用者负责验证尺寸和len；末行只要求width个有效元素，不读取padding。
func SAD8(a []byte, as int, b []byte, bs, width, height int) int {
	return kernels.sad8(a, as, b, bs, width, height)
}

// SAD16 与SAD8同义，输入为0..1023的原生uint16采样，不缩放低两位。
func SAD16(a []uint16, as int, b []uint16, bs, width, height int) int {
	return kernels.sad16(a, as, b, bs, width, height)
}

// SATD4x4 保持Hadamard的(sumAbs+1)>>1，输入为8/10-bit像素差。
// 固定数组按值传递，间接调用不保留领域栈上残差的指针或造成其逃逸。
func SATD4x4(src [16]int32) int { return kernels.satd4(src) }

// Forward4x4 返回共享正变换系数；值参数允许领域入口原地操作且不使局部残差逃逸。
func Forward4x4(src [16]int32) [16]int32 { return kernels.forward4(src) }
