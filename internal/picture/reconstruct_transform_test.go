package picture

import (
	"fmt"
	"testing"

	"github.com/cry5tallize/goavc/internal/transform"
)

func TestReconstruct_TransformAndAddParity(t *testing.T) {
	for _, depth := range []int{8, 10} {
		for _, wide := range []bool{false, true} {
			var a, b [23 * 17]byte
			var u, v [23 * 17]uint16
			for i := range a {
				a[i], b[i] = byte(i*71), byte(i*71)
				u[i], v[i] = uint16(i*131), uint16(i*131)
			}
			got, want := Plane{Data: a[:], Data16: u[:], Stride: 23, BitDepth: depth}, Plane{Data: b[:], Data16: v[:], Stride: 23, BitDepth: depth}
			var four, res4 [16]int32
			var eight, res8 [64]int32
			for i := range eight {
				eight[i] = int32(i*31 - 300)
				if wide {
					eight[i] = int32(1<<31 - 1)
					if i%3 == 0 {
						eight[i] = -1 << 31
					}
				}
				if i < 16 {
					four[i] = eight[i]
				}
			}
			transform.IDCT4x4(&res4, &four)
			original4, original8 := four, eight
			AddResidual4x4(&want, 1, 1, &res4)
			Reconstruct4x4(&got, 1, 1, &four)
			if a != b || u != v {
				t.Fatalf("4x4 depth%d wide%t不符", depth, wide)
			}
			if err := transform.IDCT8x8(&res8, &eight); err != nil {
				t.Fatal(err)
			}
			AddResidual8x8(&want, 1, 1, &res8)
			if err := Reconstruct8x8(&got, 1, 1, &eight); err != nil {
				t.Fatal(err)
			}
			if a != b || u != v {
				t.Fatalf("8x8 depth%d wide%t不符", depth, wide)
			}
			if four != original4 || eight != original8 {
				t.Fatal("融合重建修改输入系数")
			}
		}
	}
}

func TestReconstruct_LocalAllocationAndFailurePrefix(t *testing.T) {
	if n := testing.AllocsPerRun(100, func() {
		var low [23 * 17]byte
		var high [23 * 17]uint16
		var four [16]int32
		var eight [64]int32
		four[3], eight[7] = 31, -37
		p := Plane{Data: low[:], Stride: 23, BitDepth: 8}
		Reconstruct4x4(&p, 1, 1, &four)
		_ = Reconstruct8x8(&p, 1, 1, &eight)
		p = Plane{Data16: high[:], Stride: 23, BitDepth: 10}
		Reconstruct4x4(&p, 1, 1, &four)
		_ = Reconstruct8x8(&p, 1, 1, &eight)
	}); n != 0 {
		t.Fatalf("融合包装局部数组分配%g次", n)
	}
	for _, size := range []int{4, 8} {
		var got, want [60]byte
		for i := range got {
			got[i], want[i] = 77, 77
		}
		length, stride := 19, 7
		if size == 8 {
			length, stride = 60, 9
		}
		a, b := Plane{Data: got[:length], Stride: stride, BitDepth: 8}, Plane{Data: want[:length], Stride: stride, BitDepth: 8}
		var four, out4 [16]int32
		var eight, out8 [64]int32
		four[1] = 64
		eight[1] = 64
		panicked := func(fn func()) (v any) { defer func() { v = recover() }(); fn(); return }
		old := panicked(func() {
			if size == 4 {
				transform.IDCT4x4(&out4, &four)
				AddResidual4x4(&b, 0, 0, &out4)
			} else {
				if err := transform.IDCT8x8(&out8, &eight); err != nil {
					t.Fatal(err)
				}
				AddResidual8x8(&b, 0, 0, &out8)
			}
		})
		fresh := panicked(func() {
			if size == 4 {
				Reconstruct4x4(&a, 0, 0, &four)
			} else {
				_ = Reconstruct8x8(&a, 0, 0, &eight)
			}
		})
		if old == nil || fresh == nil || got != want {
			t.Fatalf("size%d原矩形错误写入前缀改变", size)
		}
	}
}

func BenchmarkReconstruct_4To8_Auto(b *testing.B) {
	for _, size := range []int{4, 8} {
		for _, depth := range []int{8, 10} {
			for _, wide := range []bool{false, true} {
				b.Run(fmt.Sprintf("%dx%d/%dbit/wide%t", size, size, depth, wide), func(b *testing.B) {
					var low, seed [23 * 17]byte
					var high, hs [23 * 17]uint16
					for i := range seed {
						seed[i] = byte(i * 71)
						hs[i] = uint16(i * 131 % 1024)
					}
					p := Plane{Data: low[:], Data16: high[:], Stride: 23, BitDepth: depth}
					var four [16]int32
					var eight [64]int32
					for i := range eight {
						eight[i] = int32(i*31 - 300)
						if wide {
							eight[i] = int32(1<<31 - 1)
							if i%3 == 0 {
								eight[i] = -1 << 31
							}
						}
						if i < 16 {
							four[i] = eight[i]
						}
					}
					b.ReportAllocs()
					for b.Loop() {
						low = seed
						high = hs
						if size == 4 {
							Reconstruct4x4(&p, 1, 1, &four)
						} else {
							if err := Reconstruct8x8(&p, 1, 1, &eight); err != nil {
								b.Fatal(err)
							}
						}
					}
				})
			}
		}
	}
}
