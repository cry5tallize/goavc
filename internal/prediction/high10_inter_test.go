package prediction

import (
	"testing"

	"github.com/goavc/h264/internal/picture"
)

func TestHigh10LumaAllPhasesAnalyticVector(t *testing.T) {
	p := picture.Plane{Width: 6, Height: 6, Stride: 6, BitDepth: 10, Data16: []uint16{
		101, 1000, 42, 711, 15, 989,
		888, 63, 912, 3, 777, 222,
		17, 901, 513, 1001, 8, 610,
		1003, 14, 777, 321, 999, 2,
		666, 1019, 0, 888, 123, 456,
		91, 11, 1021, 204, 57, 1000,
	}}
	// 在 (2,2) 解析六抽头：h0=824,h1=559,v0=697,v1=716；
	// 双半像素的未裁剪二维卷积为 779656，(779656+512)>>10=761。
	want := [4][4]uint16{
		{513, 669, 824, 913},
		{605, 761, 793, 770},
		{697, 729, 761, 739},
		{737, 628, 660, 638},
	}
	for fy := range 4 {
		for fx := range 4 {
			if got := LumaSampleHigh(&p, 2, 2, fx, fy); got != want[fy][fx] {
				t.Fatalf("phase (%d,%d): got %d want %d", fx, fy, got, want[fy][fx])
			}
			// 负运动矢量必须采用向负无穷取整，而不是向零截断。
			if got := LumaSampleHigh(&p, 3, 3, fx-4, fy-4); got != want[fy][fx] {
				t.Fatalf("negative phase (%d,%d): got %d want %d", fx, fy, got, want[fy][fx])
			}
			var dst [3]uint16
			dst[2] = 65535
			MCBlockLumaHigh(dst[:], 3, &p, 2, 2, 1, 1, fx, fy)
			if dst[0] != want[fy][fx] || dst[1] != 0 || dst[2] != 65535 {
				t.Fatalf("MC phase (%d,%d): %v", fx, fy, dst)
			}
		}
	}
}

func TestHigh10LumaSignedIntermediateAndClip(t *testing.T) {
	for _, c := range []struct {
		profile [6]int
		want    uint16
	}{{[6]int{0, 1, 0, 0, 1, 0}, 100}, {[6]int{0, 0, 1, 1, 0, 0}, 1023}} {
		p := picture.Plane{Width: 6, Height: 6, Stride: 8, Border: 1, BitDepth: 10, Data16: make([]uint16, 64)}
		for y := range 6 {
			for x := range 6 {
				p.SetSample(x, y, uint16(1023*c.profile[x]*c.profile[y]))
			}
		}
		// 第一个向量两遍卷积分别为 -10230 与 +102300。任何中间裁剪
		// 都会把正确结果 100 变成 0；第二个向量验证最终 Clip1023。
		if got := LumaSampleHigh(&p, 2, 2, 2, 2); got != c.want {
			t.Fatalf("profile=%v: got %d want %d", c.profile, got, c.want)
		}
	}
	for _, c := range []struct {
		row  [6]uint16
		want uint16
	}{{[6]uint16{0, 1023, 0, 0, 1023, 0}, 0}, {[6]uint16{0, 0, 1023, 1023, 0, 0}, 1023}} {
		p := picture.Plane{Width: 6, Height: 1, Stride: 6, BitDepth: 10, Data16: c.row[:]}
		if got := LumaSampleHigh(&p, 2, 0, 2, 0); got != c.want {
			t.Fatalf("half-pel clipping: got %d want %d", got, c.want)
		}
	}
}

func TestHigh10MotionBlockStridesAndEdges(t *testing.T) {
	p := picture.Plane{Width: 8, Height: 8, Stride: 12, Border: 2, BitDepth: 10, Data16: make([]uint16, 144)}
	for y := range 8 {
		for x := range 8 {
			p.SetSample(x, y, uint16(513+8*x+16*y))
		}
	}
	var dst [3 * 7]uint16
	MCBlockLumaHigh(dst[:], 7, &p, 2, 2, 4, 3, 1, 3)
	// 线性场的四分之一像素解析值为 513+8*(x+1/4)+16*(y+3/4)。
	for y := range 3 {
		for x := range 7 {
			want := uint16(0)
			if x < 4 {
				want = uint16(575 + 8*x + 16*y)
			}
			if dst[7*y+x] != want {
				t.Fatalf("luma block (%d,%d): got %d want %d", x, y, dst[7*y+x], want)
			}
		}
	}
	for _, c := range []struct {
		x, y, mx, my int
		want         uint16
	}{
		{0, 0, -40, -40, 513}, {7, 7, 40, 40, 681},
	} {
		if got := LumaSampleHigh(&p, c.x, c.y, c.mx, c.my); got != c.want {
			t.Fatalf("luma edge %+v: got %d", c, got)
		}
		if got := ChromaSampleHigh(&p, c.x, c.y, c.mx*2, c.my*2); got != c.want {
			t.Fatalf("chroma edge %+v: got %d", c, got)
		}
	}
}

func TestHigh10ChromaAllPhasesAnalyticPolynomial(t *testing.T) {
	p := picture.Plane{Width: 8, Height: 8, Stride: 10, Border: 1, BitDepth: 10, Data16: make([]uint16, 100)}
	for y := range 8 {
		for x := range 8 {
			p.SetSample(x, y, uint16(513+9*x+17*y+x*y))
		}
	}
	for fy := range 8 {
		for fx := range 8 {
			// 双线性场可直接在分数坐标代入，不重写生产的四权重公式。
			want := uint16((588*64 + 12*fx*8 + 19*fy*8 + fx*fy + 32) >> 6)
			if got := ChromaSampleHigh(&p, 2, 3, fx, fy); got != want {
				t.Fatalf("phase (%d,%d): got %d want %d", fx, fy, got, want)
			}
			if got := ChromaSampleHigh(&p, 3, 4, fx-8, fy-8); got != want {
				t.Fatalf("negative phase (%d,%d): got %d want %d", fx, fy, got, want)
			}
		}
	}
	var dst [3 * 7]uint16
	MCBlockChromaHigh(dst[:], 7, &p, 2, 3, 4, 3, 3, 5)
	for y := range 3 {
		for x := range 7 {
			want := uint16(0)
			if x < 4 {
				// 以八分之一像素为坐标，直接代入原双线性多项式。
				x8, y8 := 8*(2+x)+3, 8*(3+y)+5
				want = uint16((513*64 + 9*x8*8 + 17*y8*8 + x8*y8 + 32) >> 6)
			}
			if dst[7*y+x] != want {
				t.Fatalf("chroma block (%d,%d): got %d want %d", x, y, dst[7*y+x], want)
			}
		}
	}
}

func TestHigh10WeightedPredictionAnalyticVectors(t *testing.T) {
	for _, c := range []struct {
		s    uint16
		w, o int32
		d    uint32
		want uint16
	}{
		{513, 3, -5, 1, 750}, {777, -3, 127, 2, 0}, {900, 3, 17, 1, 1023},
		{513, 1, -1, 0, 509}, {513, 1, 0, 1, 257}, {511, -1, 127, 0, 0},
	} {
		if got := WeightSampleHigh(c.s, c.w, c.o, c.d, 10); got != c.want {
			t.Fatalf("unidirectional %+v: got %d", c, got)
		}
	}
	for _, c := range []struct {
		a, b           uint16
		w0, w1, o0, o1 int32
		d              uint32
		want           uint16
	}{
		{513, 1000, -7, 39, -3, 4, 5, 555},
		{5, 8, 1, 1, 0, 1, 0, 9}, {5, 8, 1, 1, 0, -1, 0, 5},
		{1023, 1023, 1, 1, 1, 0, 0, 1023}, {1, 1, 1, 1, -1, 0, 0, 0},
	} {
		if got := BiWeightSampleHigh(c.a, c.b, c.w0, c.w1, c.o0, c.o1, c.d, 10); got != c.want {
			t.Fatalf("bidirectional %+v: got %d", c, got)
		}
	}
	for _, c := range []struct {
		poc  int32
		long bool
		want uint16
	}{{2, false, 635}, {4, false, 757}, {2, true, 757}, {100, false, 757}} {
		w0, w1 := ImplicitWeights(c.poc, 0, 8, c.long, false)
		if got := BiWeightSampleHigh(513, 1000, w0, w1, 0, 0, 5, 10); got != c.want {
			t.Fatalf("implicit %+v (%d,%d): got %d", c, w0, w1, got)
		}
	}
}
