package transform

import "testing"

func TestIDCT4x4_DCOnly(t *testing.T) {
	for _, tc := range []struct {
		dc, want int32
	}{
		{0, 0},
		{31, 0},
		{32, 1},
		{64, 1},
		{-64, -1},
		{-32, 0},
		{4096, 64},
	} {
		var src, dst [16]int32
		src[0] = tc.dc
		IDCT4x4(&dst, &src)
		for i, v := range dst {
			if v != tc.want {
				t.Fatalf("dc=%d out[%d] = %d, want %d", tc.dc, i, v, tc.want)
			}
		}
		if got := IDCT4x4DC(tc.dc); got != tc.want {
			t.Fatalf("IDCT4x4DC(%d) = %d, want %d", tc.dc, got, tc.want)
		}
	}
}

func TestIDCT4x4_Impulse(t *testing.T) {
	// 只有 (row0,col1) 非零。按规范蝶形逐行展开：
	// DC 舍入后 b = [32,64,0,0,...]，两遍变换得到每行 [1,1,0,-1]。
	var src, dst [16]int32
	src[1] = 64
	IDCT4x4(&dst, &src)
	wantRow := [4]int32{1, 1, 0, -1}
	for row := 0; row < 4; row++ {
		for col := 0; col < 4; col++ {
			if got, want := dst[row*4+col], wantRow[col]; got != want {
				t.Fatalf("row %d col %d = %d, want %d (dst=%v)", row, col, got, want, dst)
			}
		}
	}
}

func TestIDCT4x4_ConstantDCBasis(t *testing.T) {
	// DC 基函数在所有输出位置相同：任意 DC 值都应产生常量残差。
	for _, dc := range []int32{-1000, -33, -32, 0, 33, 1000, 100000} {
		var src, dst [16]int32
		src[0] = dc
		IDCT4x4(&dst, &src)
		want := (dc + 32) >> 6
		for i, v := range dst {
			if v != want {
				t.Fatalf("dc=%d dst[%d] = %d, want %d", dc, i, v, want)
			}
		}
	}
}
