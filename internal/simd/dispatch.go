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

// Backends 是逐槽诊断快照。仅随真实内核迁移增加字段，不预建未来空槽。
type Backends struct {
	SAD8, SAD16 Backend
}

type kernelTable struct {
	sad8     sad8Func
	sad16    sad16Func
	backends Backends
}

func genericTable() kernelTable {
	return kernelTable{sad8: sadGeneric[byte], sad16: sadGeneric[uint16]}
}

// 生产表初始化后不变；测试强制入口将在专用构建标签内提供。
var kernels = selectKernels(nativeCPU)

func selectKernels(cpu cpuFeatures) kernelTable { return platformKernels(cpu, genericTable()) }

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
