package simd

import "testing"

func TestIDCT4x4_Generic_AliasAndExtremes(t *testing.T) {
	cases := [][16]int32{
		{}, {1}, {-1}, {1, -2, 3, -4, 1023, -1023},
	}
	for _, src := range cases {
		want := src
		got := src
		IDCT4x4(&want, &src)
		IDCT4x4(&got, &got)
		if got != want {
			t.Fatalf("alias mismatch: got %v want %v", got, want)
		}
	}
}

func TestIDCT8x8_Generic_AliasAndExtremes(t *testing.T) {
	var src [64]int32
	for i := range src {
		src[i] = int32((i*7919)%2047 - 1023)
	}
	want, got := src, src
	if !IDCT8x8(&want, &src) || !IDCT8x8(&got, &got) {
		t.Fatal("valid extreme block reported overflow")
	}
	if got != want {
		t.Fatalf("alias mismatch: got %v want %v", got, want)
	}
}

func TestIDCT8x8_Generic_WritesOnlyBlock(t *testing.T) {
	var src, got [64]int32
	for i := range src {
		src[i] = int32(i - 32)
	}
	if !IDCT8x8(&got, &src) {
		t.Fatal("valid block reported overflow")
	}
}

func TestIDCTValueKernels_PreserveAliasAndPartialOrder(t *testing.T) {
	var src [64]int32
	for i := range src {
		src[i] = int32(i*7919 - 250000)
	}
	want, written := idct8Generic(src)
	got := src
	ok := IDCT8x8(&got, &got)
	if ok != (written == 64) {
		t.Fatalf("success=%v written=%d", ok, written)
	}
	for n := 0; n < written; n++ {
		x, y := n/8, n%8
		if got[y*8+x] != want[y*8+x] {
			t.Fatalf("column-major output %d: got %d want %d", n, got[y*8+x], want[y*8+x])
		}
	}
}

func TestTransformValueKernels_ZeroAllocs(t *testing.T) {
	var src4 [16]int32
	var src8 [64]int32
	for i := range src4 {
		src4[i] = int32(i - 8)
	}
	for i := range src8 {
		src8[i] = int32(i - 32)
	}
	if n := testing.AllocsPerRun(100, func() {
		_ = idct4Generic(src4)
		_ = forward8Generic(src8)
		_, _ = idct8Generic(src8)
	}); n != 0 {
		t.Fatalf("transform kernels allocated: %v", n)
	}
}
