package prediction

import (
	"testing"

	"github.com/goavc/h264/internal/picture"
)

func TestChromaFormat_VerticalMotionAnd420Compatibility(t *testing.T) {
	p := picture.Plane{Width: 3, Height: 3, Stride: 5, BitDepth: 10,
		Data:   []byte{0, 8, 16, 255, 255, 40, 48, 56, 255, 255, 120, 128, 136, 255, 255},
		Data16: []uint16{0, 32, 64, 65535, 65535, 160, 192, 224, 65535, 65535, 480, 512, 544, 65535, 65535}}
	for _, c := range []struct {
		subH     int
		want     byte
		wantHigh uint16
	}{{2, 5, 20}, {1, 10, 40}} {
		var dst [2 * 5]byte
		var high [2 * 5]uint16
		MCBlockChromaFormat(dst[:], 5, &p, 0, 0, 2, 2, 0, 1, 2, c.subH)
		MCBlockChromaFormatHigh(high[:], 5, &p, 0, 0, 2, 2, 0, 1, 2, c.subH)
		if dst[0] != c.want || high[0] != c.wantHigh {
			t.Fatalf("subH=%d: got %d/%d want %d/%d", c.subH, dst[0], high[0], c.want, c.wantHigh)
		}
		for _, i := range []int{2, 3, 4, 7, 8, 9} {
			if dst[i] != 0 || high[i] != 0 {
				t.Fatalf("MC touched padding at %d", i)
			}
		}
	}
	// 4:2:0 旧入口的负 MV 与新入口共用舍入；解析值为 7+30=37，高位深为 148。
	if got := ChromaSample(&p, 1, 1, -1, -2); got != 37 {
		t.Fatalf("legacy 420: got %d want 37", got)
	}
	if got := ChromaSampleFormat(&p, 1, 1, -1, -2, 2, 2); got != 37 {
		t.Fatalf("format 420: got %d want 37", got)
	}
	if got := ChromaSampleHigh(&p, 1, 1, -1, -2); got != 148 {
		t.Fatalf("legacy high 420: got %d want 148", got)
	}
	if got := ChromaSampleFormatHigh(&p, 1, 1, -1, -2, 2, 2); got != 148 {
		t.Fatalf("format high 420: got %d want 148", got)
	}
	// 4:2:2 的负垂直 1/4 像素在 40 与 120 之间取 3/4：100。
	if got := ChromaSampleFormat(&p, 0, 2, 0, -1, 2, 1); got != 100 {
		t.Fatalf("negative 422: got %d want 100", got)
	}
}

func TestChromaFormat_444SixTapOddStride(t *testing.T) {
	p := picture.Plane{Width: 6, Height: 6, Stride: 9, BitDepth: 8, Data: make([]byte, 6*9)}
	for y := range 6 {
		p.Data[y*9+2] = 200
	}
	// 六抽头的中心脉冲响应为 20*200/32=125，双线性会错误得到 100。
	// 其余三个相位按规范与整像素平均，垂直恒定场使所有垂直相位相同。
	want := [4]byte{200, 163, 125, 63}
	for fy := range 4 {
		for fx := range 4 {
			if got := ChromaSampleFormat(&p, 2, 2, fx, fy, 1, 1); got != want[fx] {
				t.Fatalf("444 phase (%d,%d): got %d want %d", fx, fy, got, want[fx])
			}
			if got := ChromaSampleFormat(&p, 3, 3, fx-4, fy-4, 1, 1); got != want[fx] {
				t.Fatalf("444 negative phase (%d,%d): got %d want %d", fx, fy, got, want[fx])
			}
			var dst [3 * 5]byte
			MCBlockChromaFormat(dst[:], 5, &p, 2, 1, 1, 3, fx, fy, 1, 1)
			for y := range 3 {
				for x := range 5 {
					v := byte(0)
					if x == 0 {
						v = want[fx]
					}
					if got := dst[y*5+x]; got != v {
						t.Fatalf("444 MC (%d,%d): got %d want %d", x, y, got, v)
					}
				}
			}
		}
	}
}

func TestChromaFormat_444HighSignedIntermediate(t *testing.T) {
	for _, c := range []struct {
		profile [6]int
		want    uint16
	}{
		{[6]int{0, 1, 0, 0, 1, 0}, 100},
		{[6]int{0, 0, 1, 1, 0, 0}, 1023},
	} {
		p := picture.Plane{Width: 6, Height: 6, Stride: 9, BitDepth: 10, Data16: make([]uint16, 6*9)}
		for y := range 6 {
			for x := range 6 {
				p.Data16[y*9+x] = uint16(1023 * c.profile[x] * c.profile[y])
			}
		}
		// 第一个解析二维卷积为 102300，未裁剪负中间值后得到 100；
		// 第二个卷积超过 10-bit 上限，最终 Clip1C 为 1023。
		if got := ChromaSampleFormatHigh(&p, 2, 2, 2, 2, 1, 1); got != c.want {
			t.Fatalf("444 high profile %v: got %d want %d", c.profile, got, c.want)
		}
		var dst [7]uint16
		MCBlockChromaFormatHigh(dst[:], 7, &p, 2, 2, 1, 1, 2, 2, 1, 1)
		for i, got := range dst {
			want := uint16(0)
			if i == 0 {
				want = c.want
			}
			if got != want {
				t.Fatalf("444 high MC index %d: got %d want %d", i, got, want)
			}
		}
	}
}

func TestChromaFormat_422AllPhasesAnalytic(t *testing.T) {
	p := picture.Plane{Width: 5, Height: 5, Stride: 7, BitDepth: 10, Data: make([]byte, 5*7), Data16: make([]uint16, 5*7)}
	for y := range 5 {
		for x := range 5 {
			p.Data[y*7+x] = byte(100 + 8*x + 16*y)
			p.Data16[y*7+x] = uint16(400 + 32*x + 64*y)
		}
	}
	for fy := range 4 {
		for fx := range 8 {
			want := byte(124 + fx + 4*fy)
			wantHigh := uint16(496 + 4*fx + 16*fy)
			if got := ChromaSampleFormat(&p, 1, 1, fx, fy, 2, 1); got != want {
				t.Fatalf("422 phase (%d,%d): got %d want %d", fx, fy, got, want)
			}
			if got := ChromaSampleFormatHigh(&p, 1, 1, fx, fy, 2, 1); got != wantHigh {
				t.Fatalf("422 high phase (%d,%d): got %d want %d", fx, fy, got, wantHigh)
			}
			if got := ChromaSampleFormat(&p, 2, 2, fx-8, fy-4, 2, 1); got != want {
				t.Fatalf("422 negative phase (%d,%d): got %d want %d", fx, fy, got, want)
			}
			if got := ChromaSampleFormatHigh(&p, 2, 2, fx-8, fy-4, 2, 1); got != wantHigh {
				t.Fatalf("422 negative high phase (%d,%d): got %d want %d", fx, fy, got, wantHigh)
			}
			var dst [2 * 5]byte
			var high [2 * 5]uint16
			MCBlockChromaFormat(dst[:], 5, &p, 1, 1, 2, 2, fx, fy, 2, 1)
			MCBlockChromaFormatHigh(high[:], 5, &p, 1, 1, 2, 2, fx, fy, 2, 1)
			for y := range 2 {
				for x := range 5 {
					v, vh := byte(0), uint16(0)
					if x < 2 {
						v = want + byte(8*x+16*y)
						vh = wantHigh + uint16(32*x+64*y)
					}
					if dst[y*5+x] != v || high[y*5+x] != vh {
						t.Fatalf("422 MC phase (%d,%d) pixel (%d,%d): got %d/%d want %d/%d", fx, fy, x, y, dst[y*5+x], high[y*5+x], v, vh)
					}
				}
			}
		}
	}
}
