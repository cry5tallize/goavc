package filter

import (
	"fmt"
	"testing"

	"github.com/cry5tallize/goavc/internal/simd"
)

const edgeBenchmarkStride = 29

type edgeBenchmarkCase struct {
	name      string
	strengths [4]uint8
	qp        int
	reject    int // 0全通过，1全拒绝，2奇数像素线拒绝。
}

var edgeBenchmarkCases = [...]edgeBenchmarkCase{
	{"weak_all", [4]uint8{1, 2, 3, 1}, 32, 0},
	{"strong_all", [4]uint8{4, 4, 4, 4}, 32, 0},
	{"mixed", [4]uint8{0, 1, 3, 4}, 32, 0},
	{"reject_all", [4]uint8{1, 2, 3, 4}, 32, 1},
	{"partial", [4]uint8{1, 2, 3, 4}, 32, 2},
	{"zero_strength", [4]uint8{}, 32, 0},
	{"zero_threshold", [4]uint8{1, 2, 3, 4}, 0, 0},
}

// 固定几何同时用于基准与夹具测试；只测一条边，不把Span4称为整边。
// q0的左右/上下均保留p3/q3，非对齐stride可暴露错误的连续行假设。
func edgeBenchmarkFixture(c edgeBenchmarkCase, depth, along, across, lines int) (seed [29 * 24]uint16) {
	for i := range seed {
		seed[i] = 77
	}
	for line := range 4 * lines {
		q0 := 4*edgeBenchmarkStride + 5 + line*along
		for x, v := range [...]int{90, 91, 92, 93, 100, 101, 102, 103} {
			if x >= 4 && (c.reject == 1 || c.reject == 2 && line%2 != 0) {
				v += 80
			}
			seed[q0+(x-4)*across] = uint16(v<<(depth-8) + line%(1<<(depth-8)))
		}
	}
	return
}

// 预检整个缓冲（含未写区），另断言每条像素线是否真的触发滤波。
// 逐段旧入口只作为调度对照，已有规范解析解/Oracle继续承担算术正确性证据。
func checkEdgeBenchmarkFixture(t testing.TB, c edgeBenchmarkCase, depth, along, across, lines int, chroma bool) (seed [29 * 24]uint16) {
	t.Helper()
	seed = edgeBenchmarkFixture(c, depth, along, across, lines)
	got, want := seed, seed
	var low, expected [29 * 24]byte
	for i := range seed {
		low[i], expected[i] = byte(seed[i]), byte(seed[i])
	}
	p := PrepareEdge(c.qp, 0, 0, depth)
	q0 := 4*edgeBenchmarkStride + 5
	if chroma {
		p.Chroma16(got[:], q0, along, across, c.strengths, lines)
		if depth == 8 {
			p.Chroma8(low[:], q0, along, across, c.strengths, lines)
		}
	} else {
		p.Luma16(got[:], q0, along, across, c.strengths)
		if depth == 8 {
			p.Luma8(low[:], q0, along, across, c.strengths)
		}
	}
	for seg, bs := range c.strengths {
		i := q0 + seg*lines*along
		if chroma {
			ChromaEdgeHigh(want[:], i, along, across, int(bs), c.qp, 0, 0, lines, depth)
			if depth == 8 {
				ChromaEdge(expected[:], i, along, across, int(bs), c.qp, 0, 0, lines)
			}
		} else {
			LumaEdgeHigh(want[:], i, along, across, int(bs), c.qp, 0, 0, depth)
			if depth == 8 {
				LumaEdge(expected[:], i, along, across, int(bs), c.qp, 0, 0)
			}
		}
	}
	if got != want || low != expected {
		t.Fatalf("整边夹具与逐段入口不一致：%s depth=%d chroma=%t", c.name, depth, chroma)
	}
	for line := range 4 * lines {
		i := q0 + line*along
		changed := got[i] != seed[i] || got[i-across] != seed[i-across]
		active := c.strengths[line/lines] != 0 && c.qp != 0 && c.reject != 1 && !(c.reject == 2 && line%2 != 0)
		if changed != active {
			t.Fatalf("夹具触发条件错误：%s depth=%d chroma=%t line=%d changed=%t，期望%t", c.name, depth, chroma, line, changed, active)
		}
	}
	return
}

func TestPreparedEdgeBenchmark_Fixtures(t *testing.T) {
	for _, depth := range []int{8, 10} {
		for _, steps := range [][2]int{{29, 1}, {1, 29}} {
			for _, chroma := range []bool{false, true} {
				lines := 4
				if chroma {
					lines = 2
				}
				for _, c := range edgeBenchmarkCases {
					checkEdgeBenchmarkFixture(t, c, depth, steps[0], steps[1], lines, chroma)
				}
			}
		}
	}
}

var edgeBenchmarkSink8 [29 * 24]byte
var edgeBenchmarkSink16 [29 * 24]uint16

func BenchmarkPreparedLumaEdge_16_Auto(b *testing.B)  { benchmarkPreparedEdge(b, false) }
func BenchmarkPreparedChromaEdge_8_Auto(b *testing.B) { benchmarkPreparedEdge(b, true) }

func benchmarkPreparedEdge(b *testing.B, chroma bool) {
	for _, depth := range []int{8, 10} {
		for _, vertical := range []bool{true, false} {
			along, across, direction := edgeBenchmarkStride, 1, "vertical"
			if !vertical {
				along, across, direction = 1, edgeBenchmarkStride, "horizontal"
			}
			lines := 4
			if chroma {
				lines = 2
			}
			for _, c := range edgeBenchmarkCases {
				b.Run(fmt.Sprintf("%dbit/%s/%s", depth, direction, c.name), func(b *testing.B) {
					seed := checkEdgeBenchmarkFixture(b, c, depth, along, across, lines, chroma)
					p := PrepareEdge(c.qp, 0, 0, depth)
					q0 := 4*edgeBenchmarkStride + 5
					b.Logf("filter_luma_edge=%s filter_delta=%s；计时含固定像素重置、整边调度与滤波，不含PrepareEdge", simd.ActiveBackends().FilterLumaEdge, simd.ActiveBackends().FilterDelta)
					b.ReportAllocs()
					if depth == 8 {
						var initial, dst [29 * 24]byte
						for i := range seed {
							initial[i] = byte(seed[i])
						}
						if chroma {
							for b.Loop() {
								dst = initial
								p.Chroma8(dst[:], q0, along, across, c.strengths, lines)
							}
						} else {
							for b.Loop() {
								dst = initial
								p.Luma8(dst[:], q0, along, across, c.strengths)
							}
						}
						edgeBenchmarkSink8 = dst
					} else {
						var dst [29 * 24]uint16
						if chroma {
							for b.Loop() {
								dst = seed
								p.Chroma16(dst[:], q0, along, across, c.strengths, lines)
							}
						} else {
							for b.Loop() {
								dst = seed
								p.Luma16(dst[:], q0, along, across, c.strengths)
							}
						}
						edgeBenchmarkSink16 = dst
					}
				})
			}
		}
	}
}
