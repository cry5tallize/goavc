package decoder

import (
	"encoding/binary"
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/internal/picture"
	"github.com/cry5tallize/goavc/syntax"
)

func TestOutputPack_CroppedPlanesAreOwned(t *testing.T) {
	sps := &syntax.SPS{FrameMbsOnlyFlag: true, ChromaFormatIDC: 1,
		FrameCroppingFlag: true, FrameCropLeftOffset: 1, FrameCropTopOffset: 1}
	pic, err := picture.New(1, 1, h264.YUV420P10LE)
	if err != nil {
		t.Fatal(err)
	}
	defer pic.Release()
	for c := range pic.Planes {
		p := &pic.Planes[c]
		for y := range p.Height {
			for x := range p.Width {
				p.Row16(y)[x] = uint16(c*1777 + x*997 + y*307)
			}
		}
	}
	f, err := (&Decoder{}).toFrame(pic, sps, h264.Packet{}, false)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Release()
	if f.Width != 14 || f.Height != 14 {
		t.Fatalf("意外裁剪尺寸 %d×%d", f.Width, f.Height)
	}
	for c, p := range f.Planes {
		nx, ny := f.Format.PlaneDimensions(c, f.Width, f.Height)
		off := 1
		if c == 0 {
			off = 2
		}
		for y := range ny {
			for x := range nx {
				got := binary.LittleEndian.Uint16(p.Data[y*p.Stride+2*x:])
				if got != pic.Planes[c].Row16(y + off)[x+off] {
					t.Fatalf("平面 %d (%d,%d) 裁剪/字节序错误", c, x, y)
				}
			}
		}
		sample := &pic.Planes[c].Row16(off)[off]
		old := *sample
		p.Data[0] ^= 255
		if *sample != old {
			t.Fatal("输出修改污染了内部图像")
		}
	}
}

func BenchmarkOutputPack_1080p_High10(b *testing.B) {
	sps := &syntax.SPS{FrameMbsOnlyFlag: true, ChromaFormatIDC: 1,
		PicWidthInMbsMinus1: 119, PicHeightInMapUnitsMinus1: 67,
		FrameCroppingFlag: true, FrameCropTopOffset: 1, FrameCropBottomOffset: 3}
	pic, err := picture.New(120, 68, h264.YUV420P10LE)
	if err != nil {
		b.Fatal(err)
	}
	defer pic.Release()
	d := &Decoder{}
	b.ReportAllocs()
	for b.Loop() {
		f, err := d.toFrame(pic, sps, h264.Packet{}, false)
		if err != nil {
			b.Fatal(err)
		}
		f.Release()
	}
}
