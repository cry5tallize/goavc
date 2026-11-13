package transform

import "testing"

func TestDequantLumaDC_ConstantBlock(t *testing.T) {
	// 16 个 DC level 都是 102：Hadamard DC 输出 = 16*102 = 1632，
	// qmul = 10*16<<2 = 640，dc = (1632*640+128)>>8 = 4080。
	var levels [16]int32
	for i := range levels {
		levels[i] = 102
	}
	var out [16]int32
	if err := DequantLumaDC(&out, &levels, 0); err != nil {
		t.Fatal(err)
	}
	if out[0] != 4080 {
		t.Fatalf("out[0] = %d, want 4080", out[0])
	}
	for i, v := range out {
		if i != 0 && v != 0 {
			t.Fatalf("out[%d] = %d, want 0 (only DC basis survives)", i, v)
		}
	}
}

func TestDequantChromaDC_KnownValues(t *testing.T) {
	levels := [4]int32{1, 2, 3, 4}
	var out [4]int32
	if err := DequantChromaDC(&out, &levels, 0); err != nil {
		t.Fatal(err)
	}
	// qmul = 10*16<<2 = 640；sums = {10,-2,-4,0}；>>7 = {50,-10,-20,0}
	want := [4]int32{50, -10, -20, 0}
	if out != want {
		t.Fatalf("out = %v, want %v", out, want)
	}
}
