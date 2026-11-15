package simd

import (
	"math"
	"slices"
	"testing"
)

func TestResidualParityAndAllocation(t *testing.T) {
	tables := []struct {
		name  string
		table kernelTable
	}{{"Generic", genericTable()}, {"Auto", kernels}}
	if nativeCPU.sse2 {
		tables = append(tables, struct {
			name  string
			table kernelTable
		}{"SSE2", selectKernels(cpuFeatures{sse2: true})})
	}
	if nativeCPU.avx2 {
		tables = append(tables, struct {
			name  string
			table kernelTable
		}{"AVX2", selectKernels(cpuFeatures{sse2: true, avx2: true})})
	}
	if nativeCPU.neon {
		tables = append(tables, struct {
			name  string
			table kernelTable
		}{"NEON", selectKernels(cpuFeatures{neon: true})})
	}
	for _, tc := range tables {
		t.Run(tc.name, func(t *testing.T) {
			for _, w := range []int{1, 3, 4, 7, 8, 13, 16} {
				const h = 5
				stride := w + 3
				n := (h-1)*stride + w
				a8, b8 := make([]byte, n), make([]byte, n)
				a16, b16 := make([]uint16, n), make([]uint16, n)
				r := make([]int32, w*h)
				for i := range n {
					a8[i] = byte(i * 17)
					b8[i] = byte(i * 31)
					a16[i] = uint16(i * 37 % 1024)
					b16[i] = uint16(i * 79 % 1024)
				}
				for i := range r {
					r[i] = int32(i*53 - 700)
					if i%7 == 0 {
						r[i] = math.MaxInt32
					}
					if i%11 == 0 {
						r[i] = math.MinInt32
					}
				}
				want8, want16 := slices.Clone(a8), slices.Clone(a16)
				residualAddVector(want8, stride, r, w, w, h, 255, residualVectorGeneric)
				residualAddVector(want16, stride, r, w, w, h, 1023, residualVectorGeneric)
				residualAddVector(a8, stride, r, w, w, h, 255, tc.table.residualVector)
				residualAddVector(a16, stride, r, w, w, h, 1023, tc.table.residualVector)
				if !slices.Equal(a8, want8) || !slices.Equal(a16, want16) {
					t.Fatalf("残差相加不匹配，宽度=%d", w)
				}
				out, want := make([]int32, n), make([]int32, n)
				for i := range n {
					out[i] = -9999
					want[i] = -9999
				}
				residualSubVector(want, stride, a8, stride, b8, stride, w, h, residualVectorGeneric)
				residualSubVector(out, stride, a8, stride, b8, stride, w, h, tc.table.residualVector)
				if !slices.Equal(out, want) {
					t.Fatalf("8位残差相减不匹配，宽度=%d", w)
				}
				residualSubVector(want, stride, a16, stride, b16, stride, w, h, residualVectorGeneric)
				residualSubVector(out, stride, a16, stride, b16, stride, w, h, tc.table.residualVector)
				if !slices.Equal(out, want) {
					t.Fatalf("10位残差相减不匹配，宽度=%d", w)
				}
				if alloc := testing.AllocsPerRun(20, func() {
					residualAddVector(a8, stride, r, w, w, h, 255, tc.table.residualVector)
					residualAddVector(a16, stride, r, w, w, h, 1023, tc.table.residualVector)
					residualSubVector(out, stride, a8, stride, b8, stride, w, h, tc.table.residualVector)
					residualSubVector(out, stride, a16, stride, b16, stride, w, h, tc.table.residualVector)
				}); alloc != 0 {
					t.Fatalf("残差内核分配=%v", alloc)
				}
			}
		})
	}
}

func TestResidualAnalytic(t *testing.T) {
	a := []uint16{0, 1, 1022, 1023}
	p := []uint16{1023, 2, 1, 0}
	var d [4]int32
	Subtract16(d[:], 4, a, 4, p, 4, 4, 1)
	if d != [4]int32{-1023, -1, 1021, 1023} {
		t.Fatalf("10位相减错误：%v", d)
	}
	r := []int32{math.MaxInt32, math.MinInt32, 1, -1023}
	AddResidual16(a, 4, r, 4, 4, 1)
	if !slices.Equal(a, []uint16{1023, 0, 1023, 0}) {
		t.Fatalf("10位裁剪错误：%v", a)
	}
}

func TestResidualStackAllocation(t *testing.T) {
	if alloc := testing.AllocsPerRun(100, func() {
		var a, b [64]byte
		var a16, b16 [64]uint16
		var r [64]int32
		a[0], b[0], a16[0], b16[0] = 3, 1, 1023, 2
		Subtract8(r[:], 8, a[:], 8, b[:], 8, 8, 8)
		AddResidual8(a[:], 8, r[:], 8, 8, 8)
		Subtract16(r[:], 8, a16[:], 8, b16[:], 8, 8, 8)
		AddResidual16(b16[:], 8, r[:], 8, 8, 8)
		if a[0] != 5 || b16[0] != 1023 {
			panic("残差计算错误")
		}
	}); alloc != 0 {
		t.Fatalf("栈局部残差运算分配=%v", alloc)
	}
}

func BenchmarkResidual(b *testing.B) {
	var a, p [64]byte
	var r [64]int32
	b.Run("Add8x8", func(b *testing.B) {
		for b.Loop() {
			AddResidual8(a[:], 8, r[:], 8, 8, 8)
		}
	})
	b.Run("Subtract8x8", func(b *testing.B) {
		for b.Loop() {
			Subtract8(r[:], 8, a[:], 8, p[:], 8, 8, 8)
		}
	})
}
