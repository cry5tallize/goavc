package simd

import "testing"

func TestCPU_AVX2RequiresOSState(t *testing.T) {
	t.Logf("当前进程可用能力：%+v", nativeCPU)
	for _, maxLeaf := range []uint32{0, 1, 6, 7} {
		for mask := uint32(0); mask < 32; mask++ {
			for state := uint64(0); state < 8; state++ {
				xgetCalls := 0
				cpuid := func(leaf, subleaf uint32) (a, b, c, d uint32) {
					if leaf > maxLeaf || subleaf != 0 {
						t.Fatalf("越过CPUID叶：max=%d leaf=%d subleaf=%d", maxLeaf, leaf, subleaf)
					}
					switch leaf {
					case 0:
						return maxLeaf, 0, 0, 0
					case 1:
						return 0, 0, (mask & 7) << 26, ((mask >> 3) & 1) << 26
					case 7:
						return 0, ((mask >> 4) & 1) << 5, 0, 0
					}
					t.Fatalf("意外的CPUID叶%d", leaf)
					return
				}
				got := x86Features(cpuid, func() uint64 { xgetCalls++; return state })
				canReadXCR := maxLeaf >= 7 && mask == 31
				want := cpuFeatures{sse2: maxLeaf >= 1 && mask&8 != 0, avx2: canReadXCR && state&6 == 6}
				if got != want || (xgetCalls == 1) != canReadXCR || xgetCalls > 1 {
					t.Fatalf("max=%d flags=%05b XCR0=%d got=%+v want=%+v XGETBV次数=%d", maxLeaf, mask, state, got, want, xgetCalls)
				}
			}
		}
	}
}
