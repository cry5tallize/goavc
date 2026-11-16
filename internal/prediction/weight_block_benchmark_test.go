package prediction

import (
	"fmt"
	"github.com/cry5tallize/goavc/internal/simd"
	"testing"
)

type weightBenchmarkCase struct {
	name    string
	weights [2]int32
	offsets [2]int32
	denom   uint32
	both    bool
	inPlace bool
}

var weightBenchmarkCases = [...]weightBenchmarkCase{
	{"single_identity", [2]int32{32, 0}, [2]int32{}, 5, false, false},
	{"single_weighted", [2]int32{-32, 0}, [2]int32{127, 0}, 5, false, false},
	{"single_inplace", [2]int32{-32, 0}, [2]int32{127, 0}, 5, false, true},
	{"single_identity_inplace", [2]int32{32, 0}, [2]int32{}, 5, false, true},
	{"bi_average", [2]int32{1, 1}, [2]int32{}, 0, true, false},
	{"bi_implicit", [2]int32{24, 40}, [2]int32{}, 5, true, false},
	{"bi_explicit", [2]int32{-64, 127}, [2]int32{-128, 127}, 6, true, false},
}

var weightBenchmarkSizes = [...][2]int{{4, 4}, {8, 8}, {16, 16}, {7, 5}}

type weightBenchmarkFixture struct {
	a, b, initial, want [23 * 17]uint16
	u, v, low, expected [23 * 17]byte
	ds                  int
}

// 两源stride=19，目标stride=23；原地场景目标也为19。切片起点偏移1，
// 每行只要求有效width，矩形外哨兵参与预检。10-bit夹具保留独立低两位。
func prepareWeightBenchmark(c weightBenchmarkCase, depth, w, h int) weightBenchmarkFixture {
	f := weightBenchmarkFixture{ds: 23}
	for i := range f.a {
		f.a[i] = uint16(i * 71 % (1 << depth))
		f.b[i] = uint16((i*37 + 3) % (1 << depth))
		if i%17 == 1 {
			f.a[i] = 0
		} else if i%17 == 2 {
			f.a[i] = uint16(1<<depth - 1)
		}
		if i%17 == 3 {
			f.b[i] = uint16(1<<depth - 1)
		}
		f.u[i], f.v[i] = byte(f.a[i]), byte(f.b[i])
		f.initial[i], f.low[i] = 77, 77
	}
	if c.inPlace {
		f.initial, f.low, f.ds = f.a, f.u, 19
	}
	f.want, f.expected = f.initial, f.low
	for y := range h {
		for x := range w {
			i, j := y*f.ds+x+1, y*19+x+1
			if c.both {
				f.want[i] = BiWeightSampleHigh(f.a[j], f.b[j], c.weights[0], c.weights[1], c.offsets[0], c.offsets[1], c.denom, depth)
				f.expected[i] = BiWeightSample(f.u[j], f.v[j], c.weights[0], c.weights[1], c.offsets[0], c.offsets[1], c.denom)
			} else {
				f.want[i] = WeightSampleHigh(f.a[j], c.weights[0], c.offsets[0], c.denom, depth)
				f.expected[i] = WeightSample(f.u[j], c.weights[0], c.offsets[0], c.denom)
			}
		}
	}
	return f
}

func checkWeightBenchmark(t testing.TB, c weightBenchmarkCase, depth, w, h int) weightBenchmarkFixture {
	t.Helper()
	f := prepareWeightBenchmark(c, depth, w, h)
	got, low := f.initial, f.low
	if depth == 8 {
		src := f.u[:]
		if c.inPlace {
			src = low[:]
		}
		if c.both {
			BiWeightBlock8(low[1:], f.ds, src[1:], f.v[1:], 19, w, h, c.weights, c.offsets, c.denom)
		} else {
			WeightBlock8(low[1:], f.ds, src[1:], 19, w, h, c.weights[0], c.offsets[0], c.denom)
		}
		if low != f.expected {
			t.Fatalf("加权夹具结果或未写区错误：%s depth=%d size=%dx%d", c.name, depth, w, h)
		}
	} else {
		src := f.a[:]
		if c.inPlace {
			src = got[:]
		}
		if c.both {
			BiWeightBlock16(got[1:], f.ds, src[1:], f.b[1:], 19, w, h, c.weights, c.offsets, c.denom, depth)
		} else {
			WeightBlock16(got[1:], f.ds, src[1:], 19, w, h, c.weights[0], c.offsets[0], c.denom, depth)
		}
		if got != f.want {
			t.Fatalf("加权夹具结果或未写区错误：%s depth=%d size=%dx%d", c.name, depth, w, h)
		}
	}
	return f
}

func TestWeightBlockBenchmark_Fixtures(t *testing.T) {
	for _, depth := range []int{8, 10} {
		for _, size := range weightBenchmarkSizes {
			for _, c := range weightBenchmarkCases {
				checkWeightBenchmark(t, c, depth, size[0], size[1])
			}
		}
	}
}

var weightBenchmarkSink8 [23 * 17]byte
var weightBenchmarkSink16 [23 * 17]uint16

// 每op处理完整w×h矩形，统一包含目标重置成本，原地场景不会反复加权到饱和。
// 日志分别报告实际均值/加权槽；具体几何和参数不满足准入时继续Generic。
func BenchmarkWeightBlocks_4To16_Auto(b *testing.B) {
	for _, depth := range []int{8, 10} {
		for _, size := range weightBenchmarkSizes {
			for _, c := range weightBenchmarkCases {
				w, h := size[0], size[1]
				b.Run(fmt.Sprintf("%dbit/%dx%d/%s", depth, w, h, c.name), func(b *testing.B) {
					f := checkWeightBenchmark(b, c, depth, w, h)
					b.Logf("均值槽8/16=%s/%s，加权槽8/16=%s/%s；计时含固定目标重置和完整块入口", simd.ActiveBackends().Average8, simd.ActiveBackends().Average16, simd.ActiveBackends().Weight8, simd.ActiveBackends().Weight16)
					b.ReportAllocs()
					if depth == 8 {
						var dst [23 * 17]byte
						src := f.u[:]
						if c.inPlace {
							src = dst[:]
						}
						if c.both {
							for b.Loop() {
								dst = f.low
								BiWeightBlock8(dst[1:], f.ds, src[1:], f.v[1:], 19, w, h, c.weights, c.offsets, c.denom)
							}
						} else {
							for b.Loop() {
								dst = f.low
								WeightBlock8(dst[1:], f.ds, src[1:], 19, w, h, c.weights[0], c.offsets[0], c.denom)
							}
						}
						weightBenchmarkSink8 = dst
					} else {
						var dst [23 * 17]uint16
						src := f.a[:]
						if c.inPlace {
							src = dst[:]
						}
						if c.both {
							for b.Loop() {
								dst = f.initial
								BiWeightBlock16(dst[1:], f.ds, src[1:], f.b[1:], 19, w, h, c.weights, c.offsets, c.denom, depth)
							}
						} else {
							for b.Loop() {
								dst = f.initial
								WeightBlock16(dst[1:], f.ds, src[1:], 19, w, h, c.weights[0], c.offsets[0], c.denom, depth)
							}
						}
						weightBenchmarkSink16 = dst
					}
				})
			}
		}
	}
}
