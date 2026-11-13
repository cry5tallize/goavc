package prediction

import (
	"testing"

	"github.com/goavc/h264/internal/picture"
)

func rampPicture(t *testing.T) *picture.Picture {
	t.Helper()
	p, err := picture.New(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	// 亮度 p(x,y)=x，色度 p(x,y)=x；用于验证线性插值的解析值。
	for y := 0; y < p.Planes[0].Height; y++ {
		for x := 0; x < p.Planes[0].Width; x++ {
			p.Planes[0].SetPix(x, y, byte(x))
		}
	}
	for pi := 1; pi < 3; pi++ {
		for y := 0; y < p.Planes[pi].Height; y++ {
			for x := 0; x < p.Planes[pi].Width; x++ {
				p.Planes[pi].SetPix(x, y, byte(x))
			}
		}
	}
	return p
}
func TestLumaSample_FullPel(t *testing.T) {
	p := rampPicture(t)
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			if got, want := LumaSample(&p.Planes[0], x, y, 0, 0), byte(x); got != want {
				t.Fatalf("full pel (%d,%d) = %d, want %d", x, y, got, want)
			}
		}
	}
}

func TestLumaSample_LinearRamp(t *testing.T) {
	p := rampPicture(t)
	// 水平半像素：h(x)=x+1（线性斜坡的 6-tap 解析结果）。
	if got := LumaSample(&p.Planes[0], 5, 3, 2, 0); got != 6 {
		t.Fatalf("h(5) = %d, want 6", got)
	}
	// 四分之一像素 (1,0) = (p(5)+h(5)+1)>>1 = (5+6+1)>>1 = 6。
	if got := LumaSample(&p.Planes[0], 5, 3, 1, 0); got != 6 {
		t.Fatalf("q(5,1) = %d, want 6", got)
	}
	// 垂直方向是常量，半像素垂直插值应等于原值。
	if got := LumaSample(&p.Planes[0], 5, 3, 0, 2); got != 5 {
		t.Fatalf("v(5) = %d, want 5", got)
	}
}

func TestLumaSample_ConstantAllPhases(t *testing.T) {
	p, err := picture.New(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	for pi := range p.Planes {
		pl := &p.Planes[pi]
		for y := 0; y < pl.Height; y++ {
			for x := 0; x < pl.Width; x++ {
				pl.SetPix(x, y, 100)
			}
		}
	}
	for dy := 0; dy < 4; dy++ {
		for dx := 0; dx < 4; dx++ {
			if got := LumaSample(&p.Planes[0], 4, 4, dx, dy); got != 100 {
				t.Fatalf("phase (%d,%d) = %d, want 100", dx, dy, got)
			}
		}
	}
	for dy := 0; dy < 8; dy++ {
		for dx := 0; dx < 8; dx++ {
			if got := ChromaSample(&p.Planes[1], 4, 4, dx, dy); got != 100 {
				t.Fatalf("chroma phase (%d,%d) = %d, want 100", dx, dy, got)
			}
		}
	}
}

func TestLumaSample_EdgeClamp(t *testing.T) {
	p := rampPicture(t)
	// 负方向的大 MV 与右下越界都必须夹到帧内，不能 panic。
	_ = LumaSample(&p.Planes[0], 0, 0, -100, -100)
	_ = LumaSample(&p.Planes[0], 15, 15, 100, 100)
	// 左上角半像素相位需要读取 -2..+3 邻域，全部由边缘复制补齐。
	_ = LumaSample(&p.Planes[0], 0, 0, 2, 2)
}

func TestMCBlockLuma_DoubleHalf(t *testing.T) {
	p := rampPicture(t)
	dst := make([]byte, 8*8)
	// (2,2) 是双半像素：线性水平斜坡上 j(x)=x+1，与行无关。
	MCBlockLuma(dst, 8, &p.Planes[0], 4, 4, 8, 8, 2, 2)
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			if got, want := dst[y*8+x], byte(4+x+1); got != want {
				t.Fatalf("dst[%d][%d] = %d, want %d", y, x, got, want)
			}
		}
	}
}
