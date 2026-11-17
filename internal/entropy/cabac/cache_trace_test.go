package cabac

import (
	"fmt"
	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/bitstream"
	"testing"
)

func traceOldBit(d *Decoder) uint8 {
	if d.err != nil {
		return 0
	}
	v, err := d.r.ReadBits(1)
	if err != nil {
		d.err = err
	}
	d.lastBit = uint8(v)
	return uint8(v)
}
func traceOldNormalize(d *Decoder) {
	for d.rangeValue < 256 && d.err == nil {
		d.rangeValue *= 2
		d.offset = d.offset*2 + uint32(traceOldBit(d))
	}
	if d.err == nil && d.offset >= d.rangeValue {
		d.err = fmt.Errorf("cabac: offset exceeds range: %w", h264.ErrInvalidData)
	}
}
func traceOldStep(d *Decoder, c *Context, kind int) uint8 {
	if !d.ready() {
		return 0
	}
	if kind == 1 {
		d.offset = d.offset*2 + uint32(traceOldBit(d))
		if d.offset >= d.rangeValue {
			d.offset -= d.rangeValue
			return 1
		}
		return 0
	}
	if kind == 2 {
		d.rangeValue -= 2
		if d.offset >= d.rangeValue {
			d.terminated = true
			if d.lastBit != 1 {
				d.err = fmt.Errorf("cabac: missing termination stop bit: %w", h264.ErrInvalidData)
			}
			return 1
		}
		traceOldNormalize(d)
		return 0
	}
	lps := uint32(rangeLPS[c.State][(d.rangeValue>>6)&3])
	d.rangeValue -= lps
	bin := c.MPS
	if d.offset >= d.rangeValue {
		d.offset -= d.rangeValue
		d.rangeValue = lps
		bin ^= 1
		if c.State == 0 {
			c.MPS ^= 1
		}
		c.State = transLPS[c.State]
	} else {
		c.State = nextMPS(c.State)
	}
	traceOldNormalize(d)
	return bin
}

func TestDecoder_CachedBitOldEngineTrace(t *testing.T) {
	for state := range 64 {
		for mps := range 2 {
			for _, rv := range []uint32{256, 510, 511} {
				for _, off := range []uint32{0, rv / 2, rv - 1} {
					for remaining := 0; remaining <= 24; remaining++ {
						data := []byte{0x5a, 0xc3, 0xf0}
						a, b := Decoder{r: bitstream.NewReader(data), rangeValue: rv, offset: off, lastBit: 1}, Decoder{r: bitstream.NewReader(data), rangeValue: rv, offset: off, lastBit: 1}
						_ = a.r.SkipBits(uint(24 - remaining))
						_ = b.r.SkipBits(uint(24 - remaining))
						ca, cb := Context{State: uint8(state), MPS: uint8(mps)}, Context{State: uint8(state), MPS: uint8(mps)}
						for step := range 8 {
							kind := step % 3
							var actual uint8
							if kind == 0 {
								actual = a.Bin(&ca)
							} else if kind == 1 {
								actual = a.Bypass()
							} else {
								actual = a.Terminate()
							}
							expected := traceOldStep(&b, &cb, kind)
							if actual != expected || ca != cb || a.rangeValue != b.rangeValue || a.offset != b.offset || a.lastBit != b.lastBit || a.terminated != b.terminated || fmt.Sprint(a.err) != fmt.Sprint(b.err) || a.r.BitsRead() != b.r.BitsRead() || a.r.BitsRemaining() != b.r.BitsRemaining() {
								t.Fatalf("state%d mps%d rv%d off%d remain%d step%d状态/消费改变", state, mps, rv, off, remaining, step)
							}
							pa, ea := a.r.PeekBits(min(13, uint(a.r.BitsRemaining())))
							pb, eb := b.r.PeekBits(min(13, uint(b.r.BitsRemaining())))
							if pa != pb || fmt.Sprint(ea) != fmt.Sprint(eb) {
								t.Fatal("后续可见Reader位改变")
							}
						}
					}
				}
			}
		}
	}
}
