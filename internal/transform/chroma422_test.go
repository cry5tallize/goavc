package transform

import (
	"errors"
	"testing"

	h264 "github.com/goavc/h264"
)

func TestChromaDC422_KnownSpatialBases(t *testing.T) {
	// Spec: 8.5.11.1 的 H4、H2 矩阵直接给出各频率基底，不从被测核生成期望。
	basis := [8][8]int32{
		{1, 1, 1, 1, 1, 1, 1, 1},
		{1, -1, 1, -1, 1, -1, 1, -1},
		{1, 1, 1, 1, -1, -1, -1, -1},
		{1, -1, 1, -1, -1, 1, -1, 1},
		{1, 1, -1, -1, -1, -1, 1, 1},
		{1, -1, -1, 1, -1, 1, 1, -1},
		{1, 1, -1, -1, 1, 1, -1, -1},
		{1, -1, -1, 1, 1, -1, -1, 1},
	}
	for frequency, spatial := range basis {
		for _, sign := range []int32{1, -1} {
			var impulse, want, forward, inverse [8]int32
			impulse[frequency] = sign * 16
			for i := range spatial {
				spatial[i] *= sign
				want[i] = spatial[i] * 56
			}
			// QP'C=0 => qPDC=3，LevelScale=14*16；16*224/64=56。
			if err := DequantChromaDC422(&inverse, &impulse, 0); err != nil || inverse != want {
				t.Fatalf("inverse basis %d sign %d: %v want %v: %v", frequency, sign, inverse, want, err)
			}
			if err := ForwardChromaDC422(&forward, &spatial); err != nil {
				t.Fatal(err)
			}
			want = [8]int32{}
			want[frequency] = sign * 8
			if forward != want {
				t.Fatalf("forward basis %d sign %d: %v want %v", frequency, sign, forward, want)
			}
		}
	}
	if DC2x4Scan != [8]int{0, 2, 1, 4, 6, 3, 5, 7} {
		t.Fatal(DC2x4Scan)
	}
}

func TestChromaDC422_ScalingAndQPOffset(t *testing.T) {
	for _, tc := range []struct {
		qp    int
		scale uint8
		level int32
		want  int32
	}{
		{0, 16, 1, 4}, {0, 16, -1, -3}, // 低段正负半值均向正方向舍入。
		{0, 1, 1, 0}, {0, 1, -1, 0},
		{2, 16, 1, 5}, {3, 16, 1, 5},
		{32, 16, 1, 144}, {33, 16, 1, 160}, // qPDC=36 的分段边界。
		{39, 16, -1, -320}, {51, 16, 1, 1280},
		{63, 255, -1, -81600},
	} {
		levels := [8]int32{tc.level}
		var got [8]int32
		if err := DequantChromaDC422WithScaling(&got, &levels, tc.qp, tc.scale); err != nil {
			t.Fatal(tc, err)
		}
		for _, v := range got {
			if v != tc.want {
				t.Fatalf("%+v: %v", tc, got)
			}
		}
	}
	// 编码量化复用既有 DC API：先映射 QPC，再加三，且前向变换不除二。
	for _, bitDepth := range []int{8, 10} {
		qp, err := ScaleChromaQP(51, 12, bitDepth)
		if err != nil {
			t.Fatal(err)
		}
		if qp != 39+6*(bitDepth-8) {
			t.Fatal(qp)
		}
		var levels [8]int32
		input := [8]int32{65536, -65536}
		if err := QuantDCWithScaling(levels[:], input[:], qp+3, 16); err != nil {
			t.Fatal(err)
		}
		// qPDC=42/54，MF=13107，shift=23/25，死区1/3。
		want := int32(102)
		if bitDepth == 10 {
			want = 25
		}
		if levels[0] != want || levels[1] != -want {
			t.Fatalf("depth %d: %v want %d", bitDepth, levels, want)
		}
	}
}

func TestChromaDC422_ExtremesAliasAndRejection(t *testing.T) {
	// 单个空间脉冲的所有频率幅度相等，直接覆盖 int32 正负极值而非往返。
	for _, value := range []int32{1<<31 - 1, -1 << 31} {
		src := [8]int32{value}
		var got [8]int32
		if err := ForwardChromaDC422(&got, &src); err != nil {
			t.Fatal(value, err)
		}
		for _, v := range got {
			if v != value {
				t.Fatal(value, got)
			}
		}
		if err := ForwardChromaDC422(&src, &src); err != nil || src != got {
			t.Fatal(src, got, err)
		}
		bad := [8]int32{value, value}
		sentinel := [8]int32{7, 7, 7, 7, 7, 7, 7, 7}
		out := sentinel
		if err := ForwardChromaDC422(&out, &bad); !errors.Is(err, h264.ErrInvalidData) || out != sentinel {
			t.Fatal(out, err)
		}
		out = sentinel
		if err := DequantChromaDC422WithScaling(&out, &bad, 63, 255); !errors.Is(err, h264.ErrInvalidData) || out != sentinel {
			t.Fatal(out, err)
		}
	}
	levels := [8]int32{1, 2, 3, 4, 5, 6, 7, 8}
	// H4*levels*H2={36,-4;-16,0;0,0;-8,0}，qPDC=3 时乘 3.5。
	want := [8]int32{126, -14, -56, 0, 0, 0, -28, 0}
	if err := DequantChromaDC422(&levels, &levels, 0); err != nil || levels != want {
		t.Fatal(levels, want, err)
	}
	for _, tc := range []struct {
		qp    int
		scale uint8
	}{{-1, 16}, {64, 16}, {0, 0}} {
		out := want
		if err := DequantChromaDC422WithScaling(&out, &levels, tc.qp, tc.scale); !errors.Is(err, h264.ErrInvalidData) || out != want {
			t.Fatal(tc, out, err)
		}
	}
	if err := ForwardChromaDC422(nil, &levels); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal(err)
	}
	if err := DequantChromaDC422(&levels, nil, 0); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal(err)
	}
}
