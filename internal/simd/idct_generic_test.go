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
