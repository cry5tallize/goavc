package simd

import (
	"slices"
	"testing"
)

func TestAverageBlock_AliasesAndLocalAllocation(t *testing.T) {
	for _, backend := range guardBackends() {
		previous := kernels
		kernels = backend.table
		for _, shape := range [][4]int{{0, 1, 19, 19}, {1, 0, 19, 19}, {0, 0, 19, 19}, {0, 0, 23, 19}} {
			var got, want [512]byte
			var a [512]byte
			var high, expected, other [512]uint16
			for i := range got {
				got[i] = byte(i * 71)
				high[i] = uint16(i * 131)
				a[i] = byte(i * 23)
				other[i] = uint16(65535 - i*73)
			}
			want, expected = got, high
			d, s, ds, ss := shape[0], shape[1], shape[2], shape[3]
			averageBlockGeneric(want[d:], ds, want[s:], a[:], ss, 7, 5)
			AverageBlock8(got[d:], ds, got[s:], a[:], ss, 7, 5)
			averageBlockGeneric(expected[d:], ds, expected[s:], other[:], ss, 7, 5)
			AverageBlock16(high[d:], ds, high[s:], other[:], ss, 7, 5)
			if got != want || high != expected {
				t.Fatalf("%s别名语义不一致：%v", backend.name, shape)
			}
		}
		if n := testing.AllocsPerRun(100, func() {
			var a, b, d [256]byte
			var u, v, out [256]uint16
			AverageBlock8(d[:], 16, a[:], b[:], 16, 16, 16)
			AverageBlock16(out[:], 16, u[:], v[:], 16, 16, 16)
		}); n != 0 {
			t.Fatalf("%s局部数组分配%g次", backend.name, n)
		}
		AverageBlock8(nil, 0, nil, nil, 0, 0, 0)
		AverageBlock16(nil, 0, nil, nil, 0, 0, 0)
		kernels = previous
	}
}

func TestAverageBlock_UnalignedAndBounds(t *testing.T) {
	for _, backend := range guardBackends() {
		previous := kernels
		kernels = backend.table
		for _, w := range []int{1, 2, 3, 4, 5, 7, 8, 9, 15, 16, 17, 31} {
			for _, h := range []int{1, 5, 16} {
				ss, ds := w+3, w+5
				a, b := make([]byte, (h-1)*ss+w+1), make([]byte, (h-1)*ss+w+1)
				u, v := make([]uint16, len(a)), make([]uint16, len(a))
				d := make([]byte, (h-1)*ds+w+1)
				out := make([]uint16, len(d))
				for i := range a {
					a[i], b[i] = byte(i*71), byte(255-i*23)
					u[i], v[i] = uint16(i*131), uint16(65535-i*73)
				}
				for i := range d {
					d[i], out[i] = 77, 777
				}
				want, expected := slices.Clone(d), slices.Clone(out)
				averageBlockGeneric(want[1:], ds, a[1:], b[1:], ss, w, h)
				averageBlockGeneric(expected[1:], ds, u[1:], v[1:], ss, w, h)
				AverageBlock8(d[1:], ds, a[1:], b[1:], ss, w, h)
				AverageBlock16(out[1:], ds, u[1:], v[1:], ss, w, h)
				if !slices.Equal(d, want) || !slices.Equal(out, expected) {
					t.Fatalf("%s width%d height%d不一致或越界写", backend.name, w, h)
				}
			}
		}
		kernels = previous
	}
}
