package cabac

import (
	"fmt"
	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
)

// Context 保存规范中的 pStateIdx 和 valMPS，独立于算术引擎的区间状态。
type Context struct{ State, MPS uint8 }

// Decoder 按 Spec: 9.3.3.2 执行位精确算术解码。错误是粘性的：截断后
// 不能继续消费位流；终止后只能通过 New 在 PCM 后的正确位置重启。
type Decoder struct {
	r          *bitstream.Reader
	rangeValue uint32
	offset     uint32
	err        error
	terminated bool
	lastBit    uint8
}

func New(r *bitstream.Reader) (*Decoder, error) {
	if r == nil {
		return nil, fmt.Errorf("cabac: nil reader: %w", h264.ErrInvalidData)
	}
	v, err := r.ReadBits(9)
	if err != nil {
		return nil, err
	}
	if v >= 510 {
		return nil, fmt.Errorf("cabac: initial offset %d: %w", v, h264.ErrInvalidData)
	}
	return &Decoder{r: r, rangeValue: 510, offset: uint32(v), lastBit: uint8(v & 1)}, nil
}

func (d *Decoder) Err() error { return d.err }

// Bin 解码普通上下文 bin；使用显式区间而非带预取的低位寄存器，
// 便于把每次消耗的规范比特位置与独立 Oracle trace 对齐。
func (d *Decoder) Bin(c *Context) uint8 {
	if !d.ready() {
		return 0
	}
	if c == nil || c.State > 63 || c.MPS > 1 {
		d.err = fmt.Errorf("cabac: invalid context: %w", h264.ErrInvalidData)
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
	d.renormalize()
	return bin
}

func (d *Decoder) Bypass() uint8 {
	if !d.ready() {
		return 0
	}
	d.offset = d.offset*2 + uint32(d.readBit())
	if d.offset >= d.rangeValue {
		d.offset -= d.rangeValue
		return 1
	}
	return 0
}

func (d *Decoder) Terminate() uint8 {
	if !d.ready() {
		return 0
	}
	d.rangeValue -= 2
	if d.offset >= d.rangeValue {
		d.terminated = true
		if d.lastBit != 1 {
			d.err = fmt.Errorf("cabac: missing termination stop bit: %w", h264.ErrInvalidData)
		}
		return 1
	}
	d.renormalize()
	return 0
}

func (d *Decoder) ready() bool {
	if d.err != nil {
		return false
	}
	if d.terminated {
		d.err = fmt.Errorf("cabac: decoding after termination: %w", h264.ErrInvalidData)
		return false
	}
	return true
}

func (d *Decoder) readBit() uint8 {
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

func (d *Decoder) renormalize() {
	for d.rangeValue < 256 && d.err == nil {
		d.rangeValue *= 2
		d.offset = d.offset*2 + uint32(d.readBit())
	}
	if d.err == nil && d.offset >= d.rangeValue {
		d.err = fmt.Errorf("cabac: offset exceeds range: %w", h264.ErrInvalidData)
	}
}
