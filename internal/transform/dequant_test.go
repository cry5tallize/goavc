package transform

import (
	"errors"
	"testing"

	h264 "github.com/goavc/h264"
)

func TestLevelScaleTable(t *testing.T) {
	// 与规范表 8-15 逐项对照（抽查每个 qP%6 的四个角）。
	want := [6][4]int32{
		{10, 13, 13, 16},
		{11, 14, 14, 18},
		{13, 16, 16, 20},
		{14, 18, 18, 23},
		{16, 20, 20, 25},
		{18, 23, 23, 29},
	}
	for q := 0; q < 6; q++ {
		got := [4]int32{
			levelScale4x4[q][0], levelScale4x4[q][3],
			levelScale4x4[q][12], levelScale4x4[q][15],
		}
		if got != want[q] {
			t.Errorf("levelScale[%d] corners = %v, want %v", q, got, want[q])
		}
	}
}

func TestDequant4x4_KnownValues(t *testing.T) {
	tests := []struct {
		name   string
		qp     int
		level  int32
		pos    int
		expect int32
	}{
		// qmul = 10*16 << (0+2) = 640; (640+32)>>6 = 10
		{"qp0 pos0", 0, 1, 0, 10},
		// qp6: qP%6=0 -> scale=10，shift=qP/6+2=3；
		// qmul = 10*16<<3 = 1280; (1280+32)>>6 = 20
		{"qp6 pos0", 6, 1, 0, 20},
		// 位置 (1,1) 的 scale 为 16，而不是旧实现中的 11：
		// qmul = 16*16<<2 = 1024; (1024+32)>>6 = 16
		{"qp0 pos5", 0, 1, 5, 16},
		// qp24: qmul = 10*16 << 6 = 10240; (10240+32)>>6 = 160
		{"qp24 pos0", 24, 1, 0, 160},
		// qp24 pos1: scale 13 -> 13*16<<6 = 13312; (13312+32)>>6 = 208
		{"qp24 pos1", 24, 1, 1, 208},
		// qp51 pos0: qP%6=3，scale=14；qmul = 14*16<<10 = 229376;
		// (229376+32)>>6 = 3584
		{"qp51 pos0", 51, 1, 0, 3584},
		// 负系数使用算术右移：(-640+32)>>6 = -10
		{"negative", 0, -1, 0, -10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var levels, out [16]int32
			levels[tt.pos] = tt.level
			if err := Dequant4x4(&out, &levels, tt.qp); err != nil {
				t.Fatalf("Dequant4x4: %v", err)
			}
			if out[tt.pos] != tt.expect {
				t.Fatalf("out[%d] = %d, want %d", tt.pos, out[tt.pos], tt.expect)
			}
			for i, v := range out {
				if i != tt.pos && v != 0 {
					t.Fatalf("out[%d] = %d, want 0", i, v)
				}
			}
		})
	}
}

func TestDequant4x4_QPOutOfRange(t *testing.T) {
	var levels, out [16]int32
	for _, qp := range []int{-1, MaxQP + 1} {
		if err := Dequant4x4(&out, &levels, qp); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("qp=%d err = %v, want ErrInvalidData", qp, err)
		}
	}
}

func TestChromaQP(t *testing.T) {
	// QPY + offset 在 30 之后进入压缩段；务必与表 8-10 一致。
	tests := []struct {
		qp, off, want int
	}{
		{0, 0, 0},
		{29, 0, 29},
		{30, 0, 29},
		{31, 0, 30},
		{39, 0, 35},
		{51, 0, 39},
		{51, 12, 39}, // 上限裁剪
		{0, -12, 0},  // 下限裁剪
		{26, 6, 31},  // 32 -> 31
		{30, -1, 29}, // 29
		{36, 0, 34},  // 34
		{42, 0, 37},  // 37
	}
	for _, tt := range tests {
		if got := ChromaQP(tt.qp, tt.off); got != tt.want {
			t.Errorf("ChromaQP(%d,%d) = %d, want %d", tt.qp, tt.off, got, tt.want)
		}
	}
}
