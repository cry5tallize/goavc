package filter

import (
	"slices"
	"testing"
)

func TestPreparedEdge_SegmentParity(t *testing.T) {
	// QP覆盖10-bit负语法域、表端点和零阈值；两种方向使用非对齐stride。
	for _, depth := range []int{8, 10} {
		for qp := -12; qp <= 51; qp++ {
			for _, offsets := range [][2]int{{0, 0}, {-6, 6}, {6, -6}} {
				for _, steps := range [][2]int{{19, 1}, {1, 19}} {
					for _, strengths := range [][4]uint8{{0, 1, 2, 3}, {4, 4, 3, 0}} {
						params := PrepareEdge(qp, offsets[0], offsets[1], depth)
						var initial [19 * 21]uint16
						for i := range initial {
							initial[i] = uint16((96+(i%19)/4+(i/19)/4*2)<<(depth-8) + i%(1<<(depth-8)))
						}
						for _, chroma := range []bool{false, true} {
							for _, lines := range []int{2, 4} {
								if !chroma && lines != 4 {
									continue
								}
								a, b := initial, initial
								var u, v [19 * 21]byte
								for i := range u {
									u[i], v[i] = byte(initial[i]), byte(initial[i])
								}
								q, along, across := 4*19+4, steps[0], steps[1]
								if chroma {
									params.Chroma16(a[:], q, along, across, strengths, lines)
									if depth == 8 {
										params.Chroma8(u[:], q, along, across, strengths, lines)
									}
								} else {
									params.Luma16(a[:], q, along, across, strengths)
									if depth == 8 {
										params.Luma8(u[:], q, along, across, strengths)
									}
								}
								for seg, bs := range strengths {
									i := q + seg*lines*along
									if chroma {
										ChromaEdgeHigh(b[:], i, along, across, int(bs), qp, offsets[0], offsets[1], lines, depth)
										if depth == 8 {
											ChromaEdge(v[:], i, along, across, int(bs), qp, offsets[0], offsets[1], lines)
										}
									} else {
										LumaEdgeHigh(b[:], i, along, across, int(bs), qp, offsets[0], offsets[1], depth)
										if depth == 8 {
											LumaEdge(v[:], i, along, across, int(bs), qp, offsets[0], offsets[1])
										}
									}
								}
								if a != b || u != v {
									t.Fatalf("准备边与逐段不一致：depth=%d QP=%d offsets=%v steps=%v bS=%v chroma=%v lines=%d", depth, qp, offsets, steps, strengths, chroma, lines)
								}
							}
						}
					}
				}
			}
		}
	}
}

func TestPreparedEdge_InactiveAndAllocations(t *testing.T) {
	var data [320]byte
	for i := range data {
		data[i] = byte(90 + i%19)
	}
	want := slices.Clone(data[:])
	for _, p := range []EdgeParameters{{}, PrepareEdge(-1000, 0, 0, 8), PrepareEdge(1000, 0, 0, 8), PrepareEdge(0, 0, 0, 8)} {
		p.Luma8(data[:], 4, 19, 1, [4]uint8{1, 2, 3, 4})
		p.Chroma8(data[:], 4, 19, 1, [4]uint8{4, 3, 2, 1}, 2)
	}
	if !slices.Equal(data[:], want) {
		t.Fatal("无效索引或零阈值修改了像素")
	}
	if n := testing.AllocsPerRun(100, func() {
		p := PrepareEdge(30, 0, 0, 8)
		p.Luma8(data[:], 4, 19, 1, [4]uint8{1, 2, 3, 4})
		p.Chroma8(data[:], 4, 19, 1, [4]uint8{4, 3, 2, 1}, 2)
	}); n != 0 {
		t.Fatalf("准备边发生分配：%v", n)
	}
}
