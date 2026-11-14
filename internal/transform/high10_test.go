package transform

import (
	"errors"
	"testing"

	h264 "github.com/goavc/h264"
)

func TestScaleQP_SyntaxAndDepthBoundaries(t *testing.T) {
	for _, tt := range []struct{ qp, depth, want int }{
		{0, 8, 0}, {51, 8, 51}, {-12, 10, 0}, {-1, 10, 11},
		{0, 10, 12}, {40, 10, 52}, {51, 10, 63},
	} {
		got, err := ScaleQP(tt.qp, tt.depth)
		if err != nil || got != tt.want {
			t.Fatalf("ScaleQP(%d,%d)=%d,%v want %d", tt.qp, tt.depth, got, err, tt.want)
		}
	}
	for _, tt := range []struct {
		qp, depth int
		want      error
	}{
		{-1, 8, h264.ErrInvalidData}, {52, 8, h264.ErrInvalidData},
		{-13, 10, h264.ErrInvalidData}, {52, 10, h264.ErrInvalidData},
		{0, 7, h264.ErrInvalidData}, {0, 15, h264.ErrInvalidData},
		{0, 9, h264.ErrUnsupported}, {0, 12, h264.ErrUnsupported},
		{0, 14, h264.ErrUnsupported},
	} {
		if _, err := ScaleQP(tt.qp, tt.depth); !errors.Is(err, tt.want) {
			t.Fatalf("ScaleQP(%d,%d): %v want %v", tt.qp, tt.depth, err, tt.want)
		}
		if _, err := ScaleChromaQP(tt.qp, 0, tt.depth); !errors.Is(err, tt.want) {
			t.Fatalf("ScaleChromaQP(%d,0,%d): %v want %v", tt.qp, tt.depth, err, tt.want)
		}
	}
	if MaxQP != 51 || MaxScaledQP != 63 {
		t.Fatal("语法QP和有效QP上限不可混用")
	}
}

func TestScaleChromaQP_NonlinearBeforeDepthOffset(t *testing.T) {
	// 表8-10先映射qPI，再加QpBdOffset；负qPI不查非负表。
	for _, tt := range []struct{ qp, offset, depth, want int }{
		{-12, 0, 10, 0}, {-12, -12, 10, 0}, {-12, 12, 10, 12},
		{-1, 0, 10, 11}, {0, -12, 10, 0}, {29, 0, 10, 41},
		{30, 0, 10, 41}, {31, 0, 10, 42}, {26, 6, 10, 43},
		{39, 0, 10, 47}, {51, 0, 10, 51}, {51, 12, 10, 51},
		{51, -12, 10, 47}, {30, -1, 10, 41}, {51, 12, 8, 39},
	} {
		got, err := ScaleChromaQP(tt.qp, tt.offset, tt.depth)
		if err != nil || got != tt.want {
			t.Fatalf("ScaleChromaQP(%d,%d,%d)=%d,%v want %d", tt.qp, tt.offset, tt.depth, got, err, tt.want)
		}
		if tt.depth == 8 && got != ChromaQP(tt.qp, tt.offset) {
			t.Fatal("旧8-bit色度映射改变")
		}
	}
	for _, offset := range []int{-13, 13, -int(^uint(0)>>1) - 1, int(^uint(0) >> 1)} {
		if _, err := ScaleChromaQP(0, offset, 10); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("offset %d: %v", offset, err)
		}
	}
}

func TestHigh10Dequant_AnalyticQP52To63(t *testing.T) {
	// flat矩阵、(0,0)位置：表8-15乘2^(QP'/6)。8×8 DC为其一半，
	// I16 DC为其四分之一，420 DC为其一半；期望值为手算literal。
	want4 := [...]int32{4096, 4608, 5120, 5632, 6656, 7168, 8192, 9216, 10240, 11264, 13312, 14336}
	for i, want := range want4 {
		qp := 52 + i
		for _, sign := range []int32{-1, 1} {
			var levels4, out4 [16]int32
			var levels8, out8 [64]int32
			var levelsDC, outDC [4]int32
			levels4[0], levels8[0], levelsDC[0] = sign, sign, sign
			if err := Dequant4x4(&out4, &levels4, qp); err != nil || out4[0] != sign*want {
				t.Fatalf("4x4 QP%d: %v %v", qp, out4, err)
			}
			if err := Dequant8x8(&out8, &levels8, qp, nil); err != nil || out8[0] != sign*want/2 {
				t.Fatalf("8x8 QP%d: %v %v", qp, out8, err)
			}
			if err := DequantLumaDC(&out4, &levels4, qp); err != nil {
				t.Fatal(err)
			}
			for _, v := range out4 {
				if v != sign*want/4 {
					t.Fatalf("luma DC QP%d: %v", qp, out4)
				}
			}
			if err := DequantChromaDC(&outDC, &levelsDC, qp); err != nil {
				t.Fatal(err)
			}
			for _, v := range outDC {
				if v != sign*want/2 {
					t.Fatalf("chroma DC QP%d: %v", qp, outDC)
				}
			}
		}
	}
}

func TestHigh10Quant_AnalyticQP63DeadZones(t *testing.T) {
	// QP'=63：4×4乘子9362、shift25，DC shift26；8×8分母458752。
	// 2500*9362/2^25、5000*9362/2^26、5000*64/458752均约0.697，
	// 加1/3为1，加1/6为0；正负对称。
	for _, sign := range []int32{-1, 1} {
		var src4, intra4, inter4 [16]int32
		src4[0] = 2500 * sign
		if err := Quant4x4(&intra4, &src4, 63); err != nil {
			t.Fatal(err)
		}
		if err := QuantInter4x4WithScaling(&inter4, &src4, 63, nil); err != nil {
			t.Fatal(err)
		}
		if intra4[0] != sign || inter4[0] != 0 {
			t.Fatal(intra4, inter4)
		}
		srcDC := [4]int32{5000 * sign}
		var intraDC, interDC [4]int32
		if err := QuantDC(intraDC[:], srcDC[:], 63); err != nil {
			t.Fatal(err)
		}
		if err := QuantInterDCWithScaling(interDC[:], srcDC[:], 63, 16); err != nil {
			t.Fatal(err)
		}
		if intraDC[0] != sign || interDC[0] != 0 {
			t.Fatal(intraDC, interDC)
		}
		var src8, intra8, inter8 [64]int32
		src8[0] = 5000 * sign
		if err := Quant8x8WithScaling(&intra8, &src8, 63, nil); err != nil {
			t.Fatal(err)
		}
		if err := QuantInter8x8WithScaling(&inter8, &src8, 63, nil); err != nil {
			t.Fatal(err)
		}
		if intra8[0] != sign || inter8[0] != 0 {
			t.Fatal(intra8, inter8)
		}
	}
}

func TestHigh10Forward_ExtremeResidualMatrix(t *testing.T) {
	basis := [4][4]int32{{1, 1, 1, 1}, {2, 1, -1, -2}, {1, -1, -1, 1}, {1, -2, 2, -1}}
	for _, sign := range []int32{-1, 1} {
		for pos := range 16 {
			var src, got [16]int32
			src[pos] = sign * 1023
			Forward4x4(&got, &src)
			for y := range 4 {
				for x := range 4 {
					want := sign * 1023 * basis[y][pos/4] * basis[x][pos%4]
					if got[4*y+x] != want {
						t.Fatalf("impulse %d: %v", pos, got)
					}
				}
			}
		}
		var src4, got4 [16]int32
		var src8, got8 [64]int32
		for i := range src4 {
			src4[i] = sign * 1023
		}
		for i := range src8 {
			src8[i] = sign * 1023
		}
		Forward4x4(&got4, &src4)
		Forward8x8(&got8, &src8)
		if got4[0] != sign*16368 || got8[0] != sign*65472 {
			t.Fatal(got4, got8)
		}
		for _, v := range got4[1:] {
			if v != 0 {
				t.Fatal(got4)
			}
		}
		for _, v := range got8[1:] {
			if v != 0 {
				t.Fatal(got8)
			}
		}
		// 10-bit仍按>>6归一化，不能另加两位右移。
		var residual4 [16]int32
		var residual8 [64]int32
		got4 = [16]int32{sign * 65472}
		got8 = [64]int32{sign * 65472}
		IDCT4x4(&residual4, &got4)
		if err := IDCT8x8(&residual8, &got8); err != nil {
			t.Fatal(err)
		}
		for _, v := range residual4 {
			if v != sign*1023 {
				t.Fatal(residual4)
			}
		}
		for _, v := range residual8 {
			if v != sign*1023 {
				t.Fatal(residual8)
			}
		}
	}
}

func TestHigh10DC_Int64TemporaryAndOverflow(t *testing.T) {
	for _, value := range []int32{1 << 30, -1 << 30, 1<<31 - 1, -1 << 31} {
		var levels, out [16]int32
		for i := range levels {
			levels[i] = value
		}
		if err := DequantLumaDC(&out, &levels, 0); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("luma Hadamard wraparound accepted: %v %v", out, err)
		}
		chroma := [4]int32{value, value, value, value}
		var outC [4]int32
		if err := DequantChromaDC(&outC, &chroma, 0); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("chroma Hadamard wraparound accepted: %v %v", outC, err)
		}
		for _, qp := range []int{52, 63} {
			levels = [16]int32{value}
			if err := Dequant4x4(&out, &levels, qp); !errors.Is(err, h264.ErrInvalidData) {
				t.Fatal(qp, err)
			}
			if err := DequantLumaDC(&out, &levels, qp); !errors.Is(err, h264.ErrInvalidData) {
				t.Fatal(qp, err)
			}
			chroma = [4]int32{value}
			if err := DequantChromaDC(&outC, &chroma, qp); !errors.Is(err, h264.ErrInvalidData) {
				t.Fatal(qp, err)
			}
			var levels8, out8 [64]int32
			levels8[0] = value
			if err := Dequant8x8(&out8, &levels8, qp, nil); !errors.Is(err, h264.ErrInvalidData) {
				t.Fatal(qp, err)
			}
		}
	}
	// 中间Hadamard超过int32，但scale=1使最终输出合法，不能过早拒绝。
	var levels, out [16]int32
	for i := range levels {
		levels[i] = 1 << 28
	}
	if err := DequantLumaDCWithScaling(&out, &levels, 0, 1); err != nil || out[0] != 671088640 {
		t.Fatal(out, err)
	}
	chroma := [4]int32{1 << 29, 1 << 29, 1 << 29, 1 << 29}
	var outC [4]int32
	if err := DequantChromaDCWithScaling(&outC, &chroma, 0, 1); err != nil || outC[0] != 671088640 {
		t.Fatal(outC, err)
	}
}

func TestHigh10Quant_AllEffectiveQPsAndScaling(t *testing.T) {
	// 输入取手算的4×4反量化步长；量化正变换系数时对应4级，
	// Hadamard额外缩放一位对应2级，8×8 DC步长减半也对应2级。
	steps := [...]int32{4096, 4608, 5120, 5632, 6656, 7168, 8192, 9216, 10240, 11264, 13312, 14336}
	for i, step := range steps {
		qp := 52 + i
		for _, sign := range []int32{-1, 1} {
			for _, scale := range []uint8{16, 32} {
				var src4, out4 [16]int32
				var src8, out8 [64]int32
				var matrix4 [16]uint8
				var matrix8 [64]uint8
				for j := range matrix4 {
					matrix4[j] = scale
				}
				for j := range matrix8 {
					matrix8[j] = scale
				}
				src4[0], src8[0] = sign*step*int32(scale)/16, sign*step*int32(scale)/16
				if err := Quant4x4WithScaling(&out4, &src4, qp, &matrix4); err != nil || out4[0] != sign*4 {
					t.Fatal(qp, out4, err)
				}
				if err := QuantInter4x4WithScaling(&out4, &src4, qp, &matrix4); err != nil || out4[0] != sign*4 {
					t.Fatal(qp, out4, err)
				}
				if err := QuantDCWithScaling(out4[:], src4[:], qp, scale); err != nil || out4[0] != sign*2 {
					t.Fatal(qp, out4, err)
				}
				if err := QuantInterDCWithScaling(out4[:], src4[:], qp, scale); err != nil || out4[0] != sign*2 {
					t.Fatal(qp, out4, err)
				}
				if err := Quant8x8WithScaling(&out8, &src8, qp, &matrix8); err != nil || out8[0] != sign*2 {
					t.Fatal(qp, out8, err)
				}
				if err := QuantInter8x8WithScaling(&out8, &src8, qp, &matrix8); err != nil || out8[0] != sign*2 {
					t.Fatal(qp, out8, err)
				}
			}
		}
	}
}

func TestHigh10Kernel_InvalidEffectiveQP(t *testing.T) {
	for _, qp := range []int{-1, 64} {
		var src4, out4 [16]int32
		var src8, out8 [64]int32
		var srcC, outC [4]int32
		checks := [...]error{
			Quant4x4(&out4, &src4, qp),
			QuantDC(out4[:], src4[:], qp),
			Quant8x8WithScaling(&out8, &src8, qp, nil),
			Dequant4x4(&out4, &src4, qp),
			Dequant8x8(&out8, &src8, qp, nil),
			DequantLumaDC(&out4, &src4, qp),
			DequantChromaDC(&outC, &srcC, qp),
		}
		for i, err := range checks {
			if !errors.Is(err, h264.ErrInvalidData) {
				t.Fatalf("QP%d check%d: %v", qp, i, err)
			}
		}
	}
}

func TestHigh10Forward8x8_ExtremeImpulseAnalyticBasis(t *testing.T) {
	// 独立展开规范8.5.13在空间采样0的单轴基底与范数，
	// 不是对生产变换做自往返，也不读取生产查找表。
	basis := [8]int64{64, 96, 64, 80, 64, 48, 32, 24}
	norm := [8]int64{32768, 36992, 20480, 36992, 32768, 36992, 20480, 36992}
	for _, sign := range []int32{-1, 1} {
		var src, got [64]int32
		src[0] = sign * 1023
		Forward8x8(&got, &src)
		for y := range 8 {
			for x := range 8 {
				den := norm[x] * norm[y]
				want := sign * int32((1023*(1<<18)*basis[x]*basis[y]+den/2)/den)
				if got[8*y+x] != want {
					t.Fatalf("sign%d (%d,%d): %d want%d", sign, x, y, got[8*y+x], want)
				}
			}
		}
	}
}
