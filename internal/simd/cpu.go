package simd

// cpuFeatures 表示当前进程可以安全执行的指令，而不是 CPU 宣传的能力。
// 仅包初始化时探测；生产热路径不再次读取 CPUID 或可变环境。
type cpuFeatures struct {
	sse2, avx2, neon bool
}

var nativeCPU = detectCPU()

// x86Features 的依赖显式传入，测试可模拟缺叶/OS状态，不执行非法指令。
func x86Features(cpuid func(uint32, uint32) (uint32, uint32, uint32, uint32), xcr0 func() uint64) cpuFeatures {
	maxLeaf, _, _, _ := cpuid(0, 0)
	if maxLeaf < 1 {
		return cpuFeatures{}
	}
	_, _, ecx, edx := cpuid(1, 0)
	features := cpuFeatures{sse2: edx&(1<<26) != 0}
	const avxState = 1<<26 | 1<<27 | 1<<28 // XSAVE、OSXSAVE、AVX
	if !features.sse2 || maxLeaf < 7 || ecx&avxState != avxState {
		return features
	}
	_, ebx, _, _ := cpuid(7, 0)
	if ebx&(1<<5) == 0 {
		return features
	}
	// 只有确认 OSXSAVE 后才能执行 XGETBV；XMM/YMM 状态必须同时启用。
	features.avx2 = xcr0()&6 == 6
	return features
}
