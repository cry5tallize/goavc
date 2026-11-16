//go:build amd64 && !purego

package simd

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"testing"
)

func TestFilterChroma8EdgeSSE2_Parity(t *testing.T) {
	if !nativeCPU.sse2 {
		t.Skip("当前CPU不支持SSE2")
	}
	rng := rand.New(rand.NewPCG(0xa02, 0x420))
	for _, g := range []struct{ q, along, across, size int }{
		{2, 4, 1, 32}, {2, 19, 1, 137}, {7, 21, 1, 156},
		{16, 1, 8, 32}, {38, 1, 19, 65}, {47, 1, 21, 76},
	} {
		t.Run(fmt.Sprint(g), func(t *testing.T) {
			for n := range 2048 {
				alpha, beta := rng.IntN(256), rng.IntN(256)
				tc := [4]int{0, rng.IntN(4), rng.IntN(8), rng.IntN(26)}
				var strengths [4]uint8
				for i := range strengths {
					strengths[i] = uint8(rng.IntN(6))
				}
				got := make([]byte, g.size)
				for i := range got {
					got[i] = byte(rng.IntN(256))
				}
				for line := range 8 {
					base := rng.IntN(256)
					for x := -2; x <= 1; x++ {
						v := byte(rng.IntN(256))
						if n%2 == 0 {
							v = byte(min(255, max(0, base+rng.IntN(25)-12)))
						}
						got[g.q+line*g.along+x*g.across] = v
					}
				}
				want := bytes.Clone(got)
				filterChroma8EdgeGeneric(want, g.q, g.along, g.across, strengths, alpha, beta, tc)
				filterChroma8EdgeSSE2(got, g.q, g.along, g.across, strengths, alpha, beta, tc)
				if !bytes.Equal(got, want) {
					t.Fatalf("n=%d bS=%v alpha=%d beta=%d tc0=%v\n got=%v\nwant=%v", n, strengths, alpha, beta, tc, got, want)
				}
			}
		})
	}
}

func TestFilterChroma8EdgeSSE2_ThresholdAndAnalytic(t *testing.T) {
	if !nativeCPU.sse2 {
		t.Skip("当前CPU不支持SSE2")
	}
	for _, vertical := range []bool{true, false} {
		q, along, across := 2, 8, 1
		if !vertical {
			q, along, across = 16, 1, 8
		}
		// Spec: 8.7.2.4。tc0=0仍允许delta=1；强滤波从两侧原值独立计算。
		var data [64]byte
		for line := range 8 {
			i := q + line*along
			data[i-2*across], data[i-across], data[i], data[i+across] = 100, 100, 110, 110
		}
		filterChroma8EdgeSSE2(data[:], q, along, across, [4]uint8{1, 2, 3, 4}, 20, 10, [4]int{0, 0, 2, 4})
		for line := range 8 {
			i, seg := q+line*along, line/2
			if data[i-across] != [4]byte{101, 103, 104, 103}[seg] || data[i] != [4]byte{109, 107, 106, 108}[seg] {
				t.Fatalf("解析解不符：vertical=%t line=%d p0=%d q0=%d", vertical, line, data[i-across], data[i])
			}
		}
		for _, bs := range []uint8{0, 1, 2, 3, 4} {
			for condition := range 3 {
				for _, diff := range []byte{9, 10, 11} {
					var input [64]byte
					for i := range input {
						input[i] = 100
					}
					for line := range 8 {
						i := q + line*along
						switch condition {
						case 0:
							input[i], input[i+across] = 100+diff, 100+diff
						case 1:
							input[i-2*across] = 100 + diff
						case 2:
							input[i+across] = 100 + diff
						}
					}
					want := input
					strengths := [4]uint8{bs, bs, bs, bs}
					filterChroma8EdgeGeneric(want[:], q, along, across, strengths, 10, 10, [4]int{})
					filterChroma8EdgeSSE2(input[:], q, along, across, strengths, 10, 10, [4]int{})
					if input != want {
						t.Fatalf("严格阈值不符：vertical=%t bs=%d condition=%d diff=%d", vertical, bs, condition, diff)
					}
				}
			}
		}
		for _, row := range [][4]byte{{254, 0, 0, 0}, {1, 255, 255, 255}, {95, 100, 100, 100}, {100, 100, 100, 105}} {
			var input [64]byte
			for line := range 8 {
				for x, v := range row {
					input[q+line*along+(x-2)*across] = v
				}
			}
			want := input
			filterChroma8EdgeGeneric(want[:], q, along, across, [4]uint8{1, 2, 3, 4}, 255, 255, [4]int{0, 0, 13, 25})
			filterChroma8EdgeSSE2(input[:], q, along, across, [4]uint8{1, 2, 3, 4}, 255, 255, [4]int{0, 0, 13, 25})
			if input != want {
				t.Fatalf("饱和裁剪或负数算术移位不符：vertical=%t row=%v", vertical, row)
			}
		}
	}
	filterChroma8EdgeSSE2(nil, -1, 0, 0, [4]uint8{4, 3, 2, 1}, 0, 20, [4]int{})
	filterChroma8EdgeSSE2(nil, -1, 0, 0, [4]uint8{4, 3, 2, 1}, 20, 0, [4]int{})
	filterChroma8EdgeSSE2(nil, -1, 0, 0, [4]uint8{}, 20, 20, [4]int{})
}

func TestGuardPage_Chroma8EdgeSSE2(t *testing.T) {
	if !nativeCPU.sse2 {
		t.Skip("当前CPU不支持SSE2")
	}
	if !guardChild(t) {
		return
	}
	for _, vertical := range []bool{true, false} {
		for _, strengths := range [][4]uint8{{1, 2, 3, 1}, {4, 4, 4, 4}, {0, 4, 1, 0}, {0, 0, 0, 0}} {
			t.Run(fmt.Sprintf("vertical%t/bs%v", vertical, strengths), func(t *testing.T) {
				w, h := 8, 4
				if vertical {
					w, h = 4, 8
				}
				raw, stride := guardedRows(t, w, h, 1)
				q, along, across := 2*stride, 1, stride
				if vertical {
					q, along, across = 2, stride, 1
				}
				var want [8][4]byte
				for line := range 8 {
					for x, v := range [4]byte{98, 100, 109, 111} {
						raw[q+line*along+(x-2)*across] = v
						want[line][x] = v
					}
					bs := strengths[line/2]
					if bs != 0 {
						filterChroma(want[line][:], 2, 4, 1, int(bs), 20, 10, 3, 1, 8, nil)
					}
				}
				filterChroma8EdgeSSE2(raw, q, along, across, strengths, 20, 10, [4]int{0, 2, 2, 2})
				for line := range 8 {
					for x := range 4 {
						if raw[q+line*along+(x-2)*across] != want[line][x] {
							t.Fatalf("line=%d x=%d结果不符", line, x)
						}
					}
				}
			})
		}
	}
}
