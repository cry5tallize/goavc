package transform

import (
	"errors"
	"testing"

	h264 "github.com/goavc/h264"
)

func TestSwitchingLuma_QPYAndQSYDomains(t *testing.T) {
	// 手算DC基底：QP0的反量化步长10，primary先乘A00/64=1/4；
	// QS12的量化步长10，最终反量化步长40，IDCT再除64。
	// 因而同一level=64，primary(QP0)输出10，switching/SI输出40。
	var pred, dst [16]uint8
	var levels [16]int32
	levels[0] = 64
	for _, tc := range []struct {
		name string
		mode SwitchingMode
		qp   int
		want uint8
	}{
		{"primary_qp0", SPPrimary, 0, 10},
		{"primary_qp6", SPPrimary, 6, 20},
		{"primary_qp12", SPPrimary, 12, 40},
		{"switching_qp0", SPSwitching, 0, 40},
		{"switching_qp51", SPSwitching, 51, 40},
		{"si_qp0", SI, 0, 40},
		{"si_qp51", SI, 51, 40},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ReconstructSwitchingLuma4x4(&dst, &pred, &levels, tc.qp, 12, tc.mode); err != nil {
				t.Fatal(err)
			}
			for i, v := range dst {
				if v != tc.want {
					t.Fatalf("sample[%d]=%d, want %d", i, v, tc.want)
				}
			}
		})
	}
}

func TestSwitchingLuma_SkipStillQuantizesPrediction(t *testing.T) {
	// Spec: 8.6的P_Skip仅令码流level为零，不绕过QSY。
	// 常量20变换DC=320；QS51：floor((320*9362+2^22)/2^23)=0。
	// 普通P的零残差复制会错误输出20。
	var pred, dst [16]uint8
	var levels [16]int32
	for i := range pred {
		pred[i] = 20
	}
	for _, mode := range []SwitchingMode{SPPrimary, SPSwitching, SI} {
		if err := ReconstructSwitchingLuma4x4(&dst, &pred, &levels, 0, 51, mode); err != nil {
			t.Fatal(err)
		}
		if dst != [16]uint8{} {
			t.Fatalf("mode %d copied unquantized prediction: %v", mode, dst)
		}
	}
}

func TestSwitchingLuma_IndependentOddBasis(t *testing.T) {
	// 常量128的预测DC在QS0量化为819，反量化8190。
	// level(0,1)=64反量化832；水平逆基底为{832,416,-416,-832}。
	// 每项加8190和最终舍入32，再>>6，得到以下非对称的解析向量。
	var pred, dst [16]uint8
	for i := range pred {
		pred[i] = 128
	}
	var levels [16]int32
	levels[1] = 64
	if err := ReconstructSwitchingLuma4x4(&dst, &pred, &levels, 0, 0, SPSwitching); err != nil {
		t.Fatal(err)
	}
	want := [16]uint8{141, 134, 121, 115, 141, 134, 121, 115, 141, 134, 121, 115, 141, 134, 121, 115}
	if dst != want {
		t.Fatalf("got %v, want %v", dst, want)
	}
	levels[1] = -64
	if err := ReconstructSwitchingLuma4x4(&pred, &pred, &levels, 0, 0, SI); err != nil {
		t.Fatal(err)
	}
	want = [16]uint8{115, 121, 134, 141, 115, 121, 134, 141, 115, 121, 134, 141, 115, 121, 134, 141}
	if pred != want {
		t.Fatalf("SI/alias got %v, want %v", pred, want)
	}
}

func TestSwitchingLuma_NegativePredictionCoefficients(t *testing.T) {
	// 每行{0,1,2,3}的预测变换只有首行{24,-28,0,-4}非零。
	// QS0的量化结果{10,-7,0,-1}，反量化{100,-91,0,-13}。
	// 逆变换水平中间量（含DC舍入32）为{34,99,165,230}。
	var pred, dst [16]uint8
	var levels [16]int32
	for y := range 4 {
		for x := range 4 {
			pred[y*4+x] = uint8(x)
		}
	}
	for _, mode := range []SwitchingMode{SPPrimary, SPSwitching, SI} {
		if err := ReconstructSwitchingLuma4x4(&dst, &pred, &levels, 0, 0, mode); err != nil {
			t.Fatal(err)
		}
		if dst != pred {
			t.Fatalf("mode %d got %v, want %v", mode, dst, pred)
		}
	}
}

func TestSwitchingLuma_SignedPrimaryResidual(t *testing.T) {
	// Spec: 8-418/420。预测脉冲使(0,1)系数为+2/-2；残差±1先
	// 有符号右移为+4/-5，合成为6/-7；QP0量化乘子8066得到1/-2。
	// DC量化为820，(1,1)为±1；按规范逆变换手算最终采样，不能将
	// 符号移到第一次右移之外，也不能只验证私有量化助手的返回值。
	for _, tc := range []struct {
		impulse int
		level   int32
		want    [16]uint8
	}{
		{0, 1, [16]uint8{129, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128}},
		{3, -1, [16]uint8{127, 128, 128, 129, 128, 128, 128, 129, 128, 128, 128, 128, 128, 128, 128, 128}},
	} {
		var pred, got [16]uint8
		for i := range pred {
			pred[i] = 128
		}
		pred[tc.impulse] = 129
		levels := [16]int32{1: tc.level}
		if err := ReconstructSwitchingLuma4x4(&got, &pred, &levels, 0, 0, SPPrimary); err != nil || got != tc.want {
			t.Fatalf("primary residual %d: %v, %v; want %v", tc.level, got, err, tc.want)
		}
	}
}

func TestSwitchingLuma_ClipAndRejectOverflowAtomically(t *testing.T) {
	var pred, dst [16]uint8
	var levels [16]int32
	for _, tc := range []struct {
		level int32
		want  uint8
	}{{64, 255}, {-64, 0}, {1, 56}} {
		levels[0] = tc.level
		if err := ReconstructSwitchingLuma4x4(&dst, &pred, &levels, 0, 51, SI); err != nil {
			t.Fatal(err)
		}
		for _, v := range dst {
			if v != tc.want {
				t.Fatalf("level %d: %v, want %d", tc.level, dst, tc.want)
			}
		}
	}
	for i := range dst {
		dst[i] = 71
		pred[i] = 128
	}
	saved := dst
	levels[0] = 1<<31 - 1
	for _, mode := range []SwitchingMode{SPPrimary, SPSwitching, SI} {
		if err := ReconstructSwitchingLuma4x4(&dst, &pred, &levels, 51, 51, mode); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("overflow mode %d: %v", mode, err)
		}
		if dst != saved {
			t.Fatal("overflow changed destination")
		}
	}
	levels = [16]int32{}
	for _, tc := range []struct {
		qp, qs int
		mode   SwitchingMode
	}{{-1, 0, SPPrimary}, {52, 0, SPPrimary}, {0, -1, SI}, {0, 52, SI}, {0, 0, 3}} {
		if err := ReconstructSwitchingLuma4x4(&dst, &pred, &levels, tc.qp, tc.qs, tc.mode); !errors.Is(err, h264.ErrInvalidData) || dst != saved {
			t.Fatalf("invalid %v: %v", tc, err)
		}
	}
}

func TestSwitchingChroma420_DCQuantizationDomains(t *testing.T) {
	// Hadamard域DC的量化比普通4×4多一位，逆量化比普通DC少一位。
	// qpC=0、dc=64：primary先得到64*10/2=320，再在qsC=12量化为16，
	// 逆Hadamard/DC缩放得到16*40/2=320，最终每像素5。
	// switching/SI按8-441仅复制逆Hadamard，level64的常量输出为1，而非20。
	var pred, dst [64]uint8
	var dc [4]int32
	var ac [4][16]int32
	dc[0] = 64
	for _, tc := range []struct {
		mode SwitchingMode
		want uint8
	}{{SPPrimary, 5}, {SPSwitching, 1}, {SI, 1}} {
		if err := ReconstructSwitchingChroma420(&dst, &pred, &dc, &ac, 0, 12, 0, tc.mode); err != nil {
			t.Fatal(err)
		}
		for i, v := range dst {
			if v != tc.want {
				t.Fatalf("mode %d sample %d: %d, want %d", tc.mode, i, v, tc.want)
			}
		}
	}
}

func TestSwitchingChroma420_IndependentHadamardBasis(t *testing.T) {
	// Hadamard({64,32,16,8})={120,40,72,24}。
	// primary乘10/2=5，输出{9,3,6,2}；switching/SI仅复制f，输出{2,1,1,0}。
	var pred, dst [64]uint8
	dc := [4]int32{64, 32, 16, 8}
	var ac [4][16]int32
	for _, mode := range []SwitchingMode{SPPrimary, SPSwitching, SI} {
		if err := ReconstructSwitchingChroma420(&dst, &pred, &dc, &ac, 0, 0, 0, mode); err != nil {
			t.Fatal(err)
		}
		wantBlocks := [4]uint8{9, 3, 6, 2}
		if mode != SPPrimary {
			wantBlocks = [4]uint8{2, 1, 1, 0}
		}
		for y := range 8 {
			for x := range 8 {
				want := wantBlocks[(y/4)*2+x/4]
				if got := dst[y*8+x]; got != want {
					t.Fatalf("mode %d (%d,%d): %d, want %d", mode, x, y, got, want)
				}
			}
		}
	}
}

func TestSwitchingChroma420_DCAndACAreDifferentDomains(t *testing.T) {
	// 常量128的4个预测DC均2048，Hadamard DC=8192，QS0量化为1638。
	// switching按8-441给每个块DC=1638，不做primary的LevelScale/32。
	// 右下块AC(0,1)=64反量化为832，水平基底为{832,416,-416,-832}。
	var pred, dst [64]uint8
	for i := range pred {
		pred[i] = 128
	}
	var dc [4]int32
	var ac [4][16]int32
	ac[3][1] = 64
	if err := ReconstructSwitchingChroma420(&dst, &pred, &dc, &ac, 0, 0, 0, SPSwitching); err != nil {
		t.Fatal(err)
	}
	for y := range 8 {
		for x := range 8 {
			want := uint8(26)
			if x >= 4 && y >= 4 {
				want = [4]uint8{39, 32, 19, 13}[x-4]
			}
			if got := dst[y*8+x]; got != want {
				t.Fatalf("(%d,%d): %d, want %d", x, y, got, want)
			}
		}
	}
	// 零残差也必须对预测DC作Hadamard与量化；同时验证原地重建。
	ac[3][1] = 0
	if err := ReconstructSwitchingChroma420(&pred, &pred, &dc, &ac, 0, 0, 0, SI); err != nil {
		t.Fatal(err)
	}
	for i, v := range pred {
		if v != 26 {
			t.Fatalf("alias sample %d: %d, want 26", i, v)
		}
	}
}

func TestSwitchingChroma420_SeparateQPCAndQSCMapping(t *testing.T) {
	// primary先用QPC反量化：QPY30->29，31->30。
	// 同一dc64在QSC12量化后分别重建144、160。
	var pred, dst [64]uint8
	dc := [4]int32{64, 0, 0, 0}
	var ac [4][16]int32
	for _, tc := range []struct {
		qp, qs, offset int
		want           uint8
	}{
		{30, 12, 0, 144},
		{31, 12, 0, 160},
		{0, 0, -12, 5},
		{51, 51, 12, 255},
	} {
		if err := ReconstructSwitchingChroma420(&dst, &pred, &dc, &ac, tc.qp, tc.qs, tc.offset, SPPrimary); err != nil {
			t.Fatal(err)
		}
		for _, v := range dst {
			if v != tc.want {
				t.Fatalf("%v: got %v, want %d", tc, dst, tc.want)
			}
		}
	}
	// switching的预测常量128给Hadamard DC8192。QSY30、offset0映射29，
	// 量化为57；offset1映射30，量化为51。再加dc40后分别为97、91，
	// (dc+32)>>6得到2、1，因此QSC不能从已映射QPC派生。
	for i := range pred {
		pred[i] = 128
	}
	dc[0] = 40
	for _, tc := range []struct {
		offset int
		want   uint8
	}{{0, 2}, {1, 1}} {
		for _, mode := range []SwitchingMode{SPSwitching, SI} {
			if err := ReconstructSwitchingChroma420(&dst, &pred, &dc, &ac, 0, 30, tc.offset, mode); err != nil {
				t.Fatal(err)
			}
			for _, v := range dst {
				if v != tc.want {
					t.Fatalf("offset %d mode %d: got %v, want %d", tc.offset, mode, dst, tc.want)
				}
			}
		}
	}
}

func TestSwitchingChroma_SignBeforeArithmeticShift(t *testing.T) {
	// Spec: 8.6的色度量化公式中Sign在右移内，亮度在右移外。
	// DC例：-(1*13107+32768)>>16=-1，不能错写为-((... )>>16)=0。
	for _, tc := range []struct {
		value, want int32
	}{{0, 0}, {1, 0}, {-1, -1}, {3, 1}, {-3, -2}} {
		if got := switchingQuantize(tc.value, 13107, 16, true); got != tc.want {
			t.Fatalf("chroma DC %d: got %d, want %d", tc.value, got, tc.want)
		}
	}
	for _, tc := range []struct {
		value, want int32
	}{{0, 0}, {4, 1}, {-4, -2}} {
		if got := switchingQuantize(tc.value, 8066, 15, true); got != tc.want {
			t.Fatalf("chroma AC %d: got %d, want %d", tc.value, got, tc.want)
		}
	}
}

func TestSwitchingChroma420_RejectInvalidAtomically(t *testing.T) {
	var pred, dst [64]uint8
	for i := range dst {
		dst[i] = 71
		pred[i] = 128
	}
	saved := dst
	var dc [4]int32
	var ac [4][16]int32
	for _, offset := range []int{-13, 13} {
		if err := ReconstructSwitchingChroma420(&dst, &pred, &dc, &ac, 0, 0, offset, SI); !errors.Is(err, h264.ErrInvalidData) || dst != saved {
			t.Fatalf("offset %d: %v", offset, err)
		}
	}
	ac[3][0] = 1
	if err := ReconstructSwitchingChroma420(&dst, &pred, &dc, &ac, 0, 0, 0, SI); !errors.Is(err, h264.ErrInvalidData) || dst != saved {
		t.Fatalf("AC carrying DC: %v", err)
	}
	ac[3][0] = 0
	ac[3][15] = 1<<31 - 1
	for _, mode := range []SwitchingMode{SPPrimary, SPSwitching, SI} {
		if err := ReconstructSwitchingChroma420(&dst, &pred, &dc, &ac, 51, 51, 0, mode); !errors.Is(err, h264.ErrInvalidData) || dst != saved {
			t.Fatalf("late AC overflow mode %d: %v", mode, err)
		}
	}
	ac = [4][16]int32{}
	dc[0] = 1<<31 - 1
	for _, mode := range []SwitchingMode{SPPrimary, SPSwitching, SI} {
		if err := ReconstructSwitchingChroma420(&dst, &pred, &dc, &ac, 51, 51, 0, mode); !errors.Is(err, h264.ErrInvalidData) || dst != saved {
			t.Fatalf("DC overflow mode %d: %v", mode, err)
		}
	}
}

func TestSwitchingChroma420_NormativeDCMatrixIndices(t *testing.T) {
	// 8-427预测矩阵用块光栅排列；8-428/440与8-431/441的[j*2+i]
	// 输入/输出索引与矩阵行列不同。常量块{10,20,30,40}在primary路径
	// 的解析DC为{630,1920,1280,2570}，对应采样{10,30,20,40}。
	// switching/SI不乘LevelScale/32，DC为{126,384,256,514}。
	var pred, dst [64]uint8
	var dc [4]int32
	var ac [4][16]int32
	values := [4]uint8{10, 20, 30, 40}
	for y := range 8 {
		for x := range 8 {
			pred[y*8+x] = values[(y/4)*2+x/4]
		}
	}
	for _, mode := range []SwitchingMode{SPPrimary, SPSwitching, SI} {
		if err := ReconstructSwitchingChroma420(&dst, &pred, &dc, &ac, 0, 0, 0, mode); err != nil {
			t.Fatal(err)
		}
		want := [4]uint8{10, 30, 20, 40}
		if mode != SPPrimary {
			want = [4]uint8{2, 6, 4, 8}
		}
		for y := range 8 {
			for x := range 8 {
				if got := dst[y*8+x]; got != want[(y/4)*2+x/4] {
					t.Fatalf("mode %d (%d,%d): %d, want %d", mode, x, y, got, want[(y/4)*2+x/4])
				}
			}
		}
	}
}
