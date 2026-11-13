package transform

import (
	"errors"
	"testing"

	h264 "github.com/goavc/h264"
)

func TestLevelScaleTable(t *testing.T) {
	// 与规范表 8-15 逐项对照（抽查每个 qP%6 的四个角）。
	want := [6][4]int32{
		{10, 13, 13, 11},
		{16, 18, 18, 14},
		{13, 14, 14, 10},
		{11, 13, 13, 9},
		{18, 21, 21, 16},
		{14, 16, 16, 12},
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
		// qmul = 10 << (0+2) = 40; (1*40+32)>>6 = 1
		{"qp0 pos0", 0, 1, 0, 1},
		// qmul = 10 << (6/6+2) = 80; (1*80+32)>>6 = 1
		{"qp6 pos0", 6, 1, 0, 1},
		// qp24: qmul = 10 << 6 = 640; (640+32)>>6 = 10
		{"qp24 pos0", 24, 1, 0, 10},
		// qp24 pos1: scale 13 -> 13<<6 = 832; (832+32)>>6 = 13
		{"qp24 pos1", 24, 1, 1, 13},
		// qp51 pos0: qP%6=3，scale=11；qmul = 11 << (51/6+2) = 11<<10;
		// (11264+32)>>6 = 176
		{"qp51 pos0", 51, 1, 0, 176},
		// 负系数使用算术右移：(-40+32)>>6 = -1
		{"negative", 0, -1, 0, -1},
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
