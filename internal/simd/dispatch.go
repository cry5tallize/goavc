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

type mcluma8Func func(dst []byte, ds int, ref []byte, rs, height int)
type mcluma16Func func(dst []uint16, ds int, ref []uint16, rs, height int)
type sad8Func func(a []byte, as int, b []byte, bs, width, height int) int
type sad16Func func(a []uint16, as int, b []uint16, bs, width, height int) int
type satd4Func func([16]int32) int
type forward4Func func([16]int32) [16]int32
// Backends 是逐槽诊断快照。仅随真实内核迁移增加字段，不预建未来空槽。
type Backends struct {
    SAD8, SAD16 Backend
    SATD4x4     Backend
    Forward4x4  Backend
    MCLumaCopy  Backend
}

type kernelTable struct {
    mcluma8  mcluma8Func
    mcluma16 mcluma16Func
    sad8     sad8Func
    sad16    sad16Func
    satd4    satd4Func
    forward4 forward4Func
    backends Backends
}

func genericTable() kernelTable {
	return kernelTable{sad8: sadGeneric[byte], sad16: sadGeneric[uint16], satd4: satd4Generic, forward4: forward4Generic}
}

// 生产表初始化后不变；测试强制入口将在专用构建标签内提供。
var kernels = autoKernels(nativeCPU)

func selectKernels(cpu cpuFeatures) kernelTable { return platformKernels(cpu, genericTable()) }

func autoKernels(cpu cpuFeatures) kernelTable {
	table := selectKernels(cpu)
	// 同机五次测量中4×4 SSE2约7.3ns，AVX2约16ns；按槽选择，
	// 不因CPU有AVX2就让所有内核都使用它。显式测试仍可选择真实AVX2实现。
	if table.backends.SATD4x4 == AVX2 {
		sse := selectKernels(cpuFeatures{sse2: true})
		table.satd4, table.backends.SATD4x4 = sse.satd4, sse.backends.SATD4x4
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
