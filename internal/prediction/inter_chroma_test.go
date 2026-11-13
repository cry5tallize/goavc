package prediction

import "testing"

func TestChromaSample_LinearRamp(t *testing.T) {
	p := rampPicture(t)
	// 半色度像素（相位 4/8）在线性斜坡上等于 x+1。
	// fullX = cx*8 + 4 -> ix=cx, fx=4。
	if got := ChromaSample(&p.Planes[1], 3, 2, 4, 0); got != 4 {
		t.Fatalf("chroma half = %d, want 4", got)
	}
	if got := ChromaSample(&p.Planes[1], 3, 2, 0, 0); got != 3 {
		t.Fatalf("chroma full = %d, want 3", got)
	}
}

func TestMCBlockChroma_FullPel(t *testing.T) {
	p := rampPicture(t)
	dst := make([]byte, 4*4)
	MCBlockChroma(dst, 4, &p.Planes[1], 2, 2, 4, 4, 0, 0)
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			if got, want := dst[y*4+x], byte(2+x); got != want {
				t.Fatalf("chroma (%d,%d) = %d, want %d", x, y, got, want)
			}
		}
	}
}
