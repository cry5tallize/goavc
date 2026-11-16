package cabac

import (
	"fmt"
	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/bitstream"
	"testing"
)

func renormalizeReference(d *Decoder) {
	for d.rangeValue < 256 && d.err == nil {
		d.rangeValue *= 2
		d.offset = d.offset*2 + uint32(d.readBit())
	}
	if d.err == nil && d.offset >= d.rangeValue {
		d.err = fmt.Errorf("cabac: offset exceeds range: %w", h264.ErrInvalidData)
	}
}

func TestRenormalize_BatchedBitExact(t *testing.T) {
	for _, data := range [][]byte{{0, 0}, {255, 254}, {0x5a, 0xc3}} {
		for consumed := uint(0); consumed <= 16; consumed++ {
			for r := uint32(1); r < 512; r++ {
				for _, off := range []uint32{0, r - 1, r, 511} {
					a, b := &Decoder{r: bitstream.NewReader(data), rangeValue: r, offset: off, lastBit: 1}, &Decoder{r: bitstream.NewReader(data), rangeValue: r, offset: off, lastBit: 1}
					if err := a.r.SkipBits(consumed); err != nil {
						t.Fatal(err)
					}
					if err := b.r.SkipBits(consumed); err != nil {
						t.Fatal(err)
					}
					a.renormalize()
					renormalizeReference(b)
					if a.rangeValue != b.rangeValue || a.offset != b.offset || a.lastBit != b.lastBit || a.r.BitsRead() != b.r.BitsRead() || fmt.Sprint(a.err) != fmt.Sprint(b.err) {
						t.Fatalf("批量归一化改变状态/消费/错误：range=%d offset=%d consumed=%d got=%+v want=%+v", r, off, consumed, a, b)
					}
				}
			}
		}
	}
}

func BenchmarkRenormalize_Bits_Generic(b *testing.B) {
	for _, r := range []uint32{128, 2} {
		for _, reference := range []bool{false, true} {
			b.Run(fmt.Sprintf("range%d/reference%v", r, reference), func(b *testing.B) {
				initial := *bitstream.NewReader([]byte{0, 0, 0, 0})
				b.ReportAllocs()
				for b.Loop() {
					reader := initial
					d := Decoder{r: &reader, rangeValue: r}
					if reference {
						renormalizeReference(&d)
					} else {
						d.renormalize()
					}
					if d.rangeValue != 256 || d.err != nil {
						b.Fatal("归一化基准状态错误")
					}
				}
			})
		}
	}
}
