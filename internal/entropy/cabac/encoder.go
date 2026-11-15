package cabac

import (
	"fmt"
	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/bitstream"
)

// Encoder 维护 Spec: 9.3.3.2 解码区间的编码侧对偶。low 为 10-bit 归一化
// 区间下界；pending 保存中间半区（E3）尚不能确定的位，不预写再回改字节。
type Encoder struct {
	w                 *bitstream.Writer
	low, rangeValue   uint32
	pending           uint64
	first, terminated bool
	bins              uint64
	err               error
}

func NewEncoder(w *bitstream.Writer) (*Encoder, error) {
	if w == nil || w.BitsWritten()%8 != 0 {
		return nil, fmt.Errorf("cabac: encoder requires aligned writer: %w", h264.ErrInvalidData)
	}
	return &Encoder{w: w, rangeValue: 510, first: true}, nil
}
func (e *Encoder) Err() error       { return e.err }
func (e *Encoder) BinCount() uint64 { return e.bins }

func (e *Encoder) ready(bin uint8) bool {
	if e.err != nil {
		return false
	}
	if bin > 1 || e.terminated {
		e.err = fmt.Errorf("cabac: invalid bin or terminated encoder: %w", h264.ErrInvalidData)
		return false
	}
	e.bins++
	return true
}

func (e *Encoder) Bin(c *Context, bin uint8) {
	if !e.ready(bin) {
		return
	}
	if c == nil || c.State > 63 || c.MPS > 1 {
		e.err = fmt.Errorf("cabac: invalid encoding context: %w", h264.ErrInvalidData)
		return
	}
	lps := uint32(rangeLPS[c.State][(e.rangeValue>>6)&3])
	e.rangeValue -= lps
	if bin != c.MPS {
		e.low += e.rangeValue
		e.rangeValue = lps
		if c.State == 0 {
			c.MPS ^= 1
		}
		c.State = transLPS[c.State]
	} else {
		c.State = nextMPS(c.State)
	}
	e.renormalize()
}

func (e *Encoder) Bypass(bin uint8) {
	if !e.ready(bin) {
		return
	}
	e.low *= 2
	if bin != 0 {
		e.low += e.rangeValue
	}
	switch {
	case e.low >= 1024:
		e.putBit(1)
		e.low -= 1024
	case e.low < 512:
		e.putBit(0)
	default:
		e.pending++
		e.low -= 512
	}
}

// Terminate(1) 选择终止宽度 2 区间内的奇数码点，最后消费位因此为停止位 1。
// 先消解 E3 pending，再补齐该码点低九位；字节余位只填零，不另添加停止位。
// PCM 后应使用同一 Writer 新建引擎，保留外部 Context 数组。
func (e *Encoder) Terminate(bin uint8) {
	if !e.ready(bin) {
		return
	}
	e.rangeValue -= 2
	if bin == 0 {
		e.renormalize()
		return
	}
	e.low += e.rangeValue
	point := e.low | 1
	e.putBit(uint8(point >> 9))
	if e.err == nil {
		e.err = e.w.WriteBits(9, uint64(point&511))
	}
	e.w.ByteAlign()
	e.terminated = true
}

func (e *Encoder) renormalize() {
	for e.rangeValue < 256 && e.err == nil {
		switch {
		case e.low < 256:
			e.putBit(0)
		case e.low >= 512:
			e.putBit(1)
			e.low -= 512
		default:
			e.pending++
			e.low -= 256
		}
		e.low *= 2
		e.rangeValue *= 2
	}
}

func (e *Encoder) putBit(bin uint8) {
	if e.err != nil {
		return
	}
	// 初始区间 [0,510) 位于 10-bit 空间的下半区，首个 0 是固定先导位，
	// 不属于规范初始化要读取的九位 codIOffset。
	if e.first {
		e.first = false
		if bin != 0 {
			e.err = fmt.Errorf("cabac: initial arithmetic carry: %w", h264.ErrInvalidData)
			return
		}
	} else {
		e.err = e.w.WriteBit(bin)
	}
	for e.pending > 0 && e.err == nil {
		e.err = e.w.WriteBit(1 - bin)
		e.pending--
	}
}
