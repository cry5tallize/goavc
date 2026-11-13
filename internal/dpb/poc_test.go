package dpb

import "testing"

func TestPOCType0_Sequence(t *testing.T) {
	p := NewPOCState(16)
	tests := []struct {
		idr  bool
		lsb  int32
		want int32
	}{
		{true, 0, 0}, // IDR
		{false, 4, 4},
		{false, 8, 8},
		{false, 12, 12},
		{false, 0, 16}, // 回绕
		{false, 4, 20},
	}
	for _, tt := range tests {
		if got := p.Compute(tt.idr, tt.lsb); got != tt.want {
			t.Fatalf("lsb %d: poc = %d, want %d", tt.lsb, got, tt.want)
		}
	}
}

func TestPOCType0_Rewind(t *testing.T) {
	p := NewPOCState(16)
	p.Compute(true, 2)
	// lsb 从 2 跳到 12，差值 10 > 8，msb 减 16。
	if got := p.Compute(false, 12); got != -4 {
		t.Fatalf("rewind poc = %d, want -4", got)
	}
}

func TestPOCType0_Reset(t *testing.T) {
	p := NewPOCState(16)
	p.Compute(true, 10)
	p.Reset()
	if got := p.Compute(false, 3); got != 3 {
		t.Fatalf("after reset poc = %d, want 3", got)
	}
}
