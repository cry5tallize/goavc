package transform

import (
	"errors"
	h264 "github.com/goavc/h264"
	"testing"
)

func TestIDCT8x8_AnalyticBases(t *testing.T) {
	var in, out [64]int32
	in[0] = 64
	if err := IDCT8x8(&out, &in); err != nil {
		t.Fatal(err)
	}
	for _, v := range out {
		if v != 1 {
			t.Fatal("DC basis", out)
		}
	}
	in = [64]int32{}
	in[1] = 64
	if err := IDCT8x8(&out, &in); err != nil {
		t.Fatal(err)
	}
	// 单个水平频率的未归一化行是 96,80,48,24,-24,-48,-80,-96。
	want := [8]int32{2, 1, 1, 0, 0, -1, -1, -1}
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			if out[y*8+x] != want[x] {
				t.Fatal(out)
			}
		}
	}
}

func TestDequant8x8_ScalingAndInverse(t *testing.T) {
	var levels, coeff, out [64]int32
	levels[0] = 64
	var matrix [64]uint8
	for i := range matrix {
		matrix[i] = 32
	}
	if err := Dequant8x8(&coeff, &levels, 0, &matrix); err != nil {
		t.Fatal(err)
	}
	if coeff[0] != 640 {
		t.Fatal(coeff[0])
	}
	if err := IDCT8x8(&out, &coeff); err != nil {
		t.Fatal(err)
	}
	for _, v := range out {
		if v != 10 {
			t.Fatal(out)
		}
	}
	levels[0] = 1<<31 - 1
	if err := Dequant8x8(&coeff, &levels, 51, &matrix); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal(err)
	}
	var flat [16]uint8
	for i := range flat {
		flat[i] = 16
	}
	input := [16]int32{1, -3, 5, -7, 9, -11, 13, -15, 17, -19, 21, -23, 25, -27, 29, -31}
	for qp := 0; qp <= 51; qp++ {
		var a, b [16]int32
		if err := Dequant4x4(&a, &input, qp); err != nil {
			t.Fatal(err)
		}
		if err := Dequant4x4WithScaling(&b, &input, qp, &flat); err != nil {
			t.Fatal(err)
		}
		if a != b {
			t.Fatal("flat scaling differs", qp)
		}
	}
}

func TestZigzag8x8_Permutation(t *testing.T) {
	var seen [64]bool
	for _, i := range Zigzag8x8 {
		if i < 0 || i >= 64 || seen[i] {
			t.Fatal("invalid scan")
		}
		seen[i] = true
	}
	if Zigzag8x8[0] != 0 || Zigzag8x8[1] != 1 || Zigzag8x8[2] != 8 || Zigzag8x8[63] != 63 {
		t.Fatal("scan anchors")
	}
}

func TestForward8x8_AnalyticImpulse(t *testing.T) {
	var src, got [64]int32
	src[0] = 1
	Forward8x8(&got, &src)
	basis := [8]int32{1, 1, 1, 1, 1, 1, -1, 2}
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			if want := basis[y] * basis[x]; got[y*8+x] != want {
				t.Fatalf("impulse coefficient (%d,%d)=%d want %d", x, y, got[y*8+x], want)
			}
		}
	}
}

func TestQuant8x8WithScaling_ParityAndBounds(t *testing.T) {
	var src, flat, dequant [64]int32
	var scaling [64]uint8
	for i := range src {
		src[i] = int32((i*17)%101 - 50)
		scaling[i] = 16
	}
	for qp := 0; qp <= 51; qp++ {
		if err := Quant8x8WithScaling(&flat, &src, qp, nil); err != nil {
			t.Fatal(err)
		}
		if err := Quant8x8WithScaling(&dequant, &src, qp, &scaling); err != nil {
			t.Fatal(err)
		}
		if flat != dequant {
			t.Fatalf("flat matrix mismatch at QP %d", qp)
		}
		var restored [64]int32
		if err := Dequant8x8(&restored, &flat, qp, nil); err != nil {
			t.Fatal(err)
		}
		for i, level := range flat {
			if level != 0 && (restored[i] == 0 || (restored[i] < 0) != (src[i] < 0)) {
				t.Fatalf("QP%d coefficient%d sign/scale mismatch: src=%d level=%d dequant=%d", qp, i, src[i], level, restored[i])
			}
		}
	}
	scaling[23] = 0
	if err := Quant8x8WithScaling(&flat, &src, 26, &scaling); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal("zero scaling matrix entry accepted", err)
	}
}

func TestDequant8x8_NonDCScalingAnalyticOutput(t *testing.T) {
	var levels, coeff, out [64]int32
	levels[1] = 64
	var matrix [64]uint8
	for i := range matrix {
		matrix[i] = 16
	}
	matrix[1] = 32
	if err := Dequant8x8(&coeff, &levels, 0, &matrix); err != nil {
		t.Fatal(err)
	}
	// 位置 (0,1) 使用 levelScale=19：64*19*32/64=608，其余系数为0。
	for i, v := range coeff {
		want := int32(0)
		if i == 1 {
			want = 608
		}
		if v != want {
			t.Fatalf("coefficient %d: %d want %d", i, v, want)
		}
	}
	if err := IDCT8x8(&out, &coeff); err != nil {
		t.Fatal(err)
	}
	want := [8]int32{14, 12, 7, 4, -4, -7, -12, -14}
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			if out[y*8+x] != want[x] {
				t.Fatalf("residual (%d,%d)=%d want %d", x, y, out[y*8+x], want[x])
			}
		}
	}
}
