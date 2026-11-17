package cabac

import (
	"errors"
	"fmt"
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/bitstream"
)

// C06未采纳候选，只存在于测试二进制。合法MPS已证明offset<range，且
// range>=256时省去renormalize调用；任意Context及生命周期检查保持不变。
// High10端点未通过性能门禁，生产Bin仍使用原流程，不以局部微基准启用。
func (d *Decoder) binMPSCandidate(c *Context) uint8 {
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
	if d.offset < d.rangeValue {
		c.State = nextMPS(c.State)
		if d.rangeValue >= 256 {
			return bin
		}
		d.renormalize()
		return bin
	}
	d.offset -= d.rangeValue
	d.rangeValue = lps
	if c.State == 0 {
		c.MPS ^= 1
	}
	c.State = transLPS[c.State]
	d.renormalize()
	return bin ^ 1
}

// 遍历全部规范归一化区间，含MPS仍需移位的边界；逐状态对照未改动的生产引擎。
// Context的公开任意值校验继续由Bin执行，而非假定调用者总是可信。
func TestDecoder_MPSNormalizedRangeTrace(t *testing.T) {
	for rv := uint32(256); rv <= 511; rv++ {
		for state := range 64 {
			for mps := range 2 {
				for _, off := range []uint32{0, rv / 2, rv - 1} {
					for _, tail := range [][]byte{nil, {0}, {255}, {0x5a, 0xc3}} {
						a := Decoder{r: bitstream.NewReader(tail), rangeValue: rv, offset: off, lastBit: 1}
						b := Decoder{r: bitstream.NewReader(tail), rangeValue: rv, offset: off, lastBit: 1}
						ca, cb := Context{uint8(state), uint8(mps)}, Context{uint8(state), uint8(mps)}
						got, want := a.binMPSCandidate(&ca), b.Bin(&cb)
						if got != want || ca != cb || a.rangeValue != b.rangeValue || a.offset != b.offset || a.lastBit != b.lastBit || a.terminated != b.terminated || fmt.Sprint(a.err) != fmt.Sprint(b.err) || a.r.BitsRead() != b.r.BitsRead() {
							t.Fatalf("range%d state%d mps%d off%d tail%x状态改变", rv, state, mps, off, tail)
						}
						pa, ea := a.r.PeekBits(min(8, uint(a.r.BitsRemaining())))
						pb, eb := b.r.PeekBits(min(8, uint(b.r.BitsRemaining())))
						if pa != pb || fmt.Sprint(ea) != fmt.Sprint(eb) {
							t.Fatal("后续可见位改变")
						}
					}
				}
			}
		}
	}
}

func TestDecoder_MPSFastPathKeepsContextAndLifecycleChecks(t *testing.T) {
	for _, c := range []*Context{nil, {State: 64}, {State: 255, MPS: 255}, {State: 62, MPS: 2}} {
		r := bitstream.NewReader([]byte{0x5a})
		d := Decoder{r: r, rangeValue: 510, offset: 0, lastBit: 1}
		if d.binMPSCandidate(c) != 0 || !errors.Is(d.Err(), h264.ErrInvalidData) || r.BitsRead() != 0 || d.rangeValue != 510 || d.offset != 0 || d.lastBit != 1 {
			t.Fatal("非法上下文绕过校验或改变算术状态")
		}
	}
	for _, terminated := range []bool{false, true} {
		r := bitstream.NewReader([]byte{0x5a})
		d := Decoder{r: r, rangeValue: 510, offset: 0, lastBit: 1, terminated: terminated}
		if !terminated {
			d.err = fmt.Errorf("test: %w", h264.ErrInvalidData)
		}
		c := Context{State: 62, MPS: 1}
		if d.binMPSCandidate(&c) != 0 || !errors.Is(d.Err(), h264.ErrInvalidData) || c != (Context{State: 62, MPS: 1}) || r.BitsRead() != 0 || d.rangeValue != 510 {
			t.Fatal("粘性错误或终止状态被MPS快路绕过")
		}
	}
	// 合法MPS无位可读仍可完成；需要归一化时则保持截断错误和已移位前缀。
	for _, state := range []uint8{0, 62, 63} {
		a := Decoder{r: bitstream.NewReader(nil), rangeValue: 510, offset: 0, lastBit: 1}
		b := Decoder{r: bitstream.NewReader(nil), rangeValue: 510, offset: 0, lastBit: 1}
		ca, cb := Context{State: state, MPS: 1}, Context{State: state, MPS: 1}
		if a.binMPSCandidate(&ca) != b.Bin(&cb) || fmt.Sprint(a.err) != fmt.Sprint(b.err) || a.rangeValue != b.rangeValue || a.lastBit != b.lastBit {
			t.Fatal("空尾MPS状态改变")
		}
	}
}

func BenchmarkBin_NormalizedMPS_Engine(b *testing.B) {
	// 无移位MPS成本隔离，不代表真实残差混合；每op恢复同一规范有效引擎状态。
	r := bitstream.NewReader(nil)
	var out uint8
	b.ReportAllocs()
	for b.Loop() {
		d := Decoder{r: r, rangeValue: 510, offset: 0, lastBit: 1}
		c := Context{State: 62, MPS: 1}
		out = d.Bin(&c)
		if d.Err() != nil {
			b.Fatal(d.Err())
		}
	}
	if out != 1 {
		b.Fatal("MPS结果不符")
	}
}

func BenchmarkBin_NormalizedMPS_Compare(b *testing.B) {
	for _, candidate := range []bool{false, true} {
		b.Run(fmt.Sprintf("candidate%t", candidate), func(b *testing.B) {
			r := bitstream.NewReader(nil)
			b.ReportAllocs()
			for b.Loop() {
				d := Decoder{r: r, rangeValue: 510, lastBit: 1}
				c := Context{State: 62, MPS: 1}
				var got uint8
				if candidate {
					got = d.binMPSCandidate(&c)
				} else {
					got = d.Bin(&c)
				}
				if got != 1 || d.Err() != nil {
					b.Fatal("MPS基准失败")
				}
			}
		})
	}
}

func TestDecoder_MPSCandidateMixedOperations(t *testing.T) {
	for remaining := 0; remaining <= 24; remaining++ {
		for state := range 64 {
			a := Decoder{r: bitstream.NewReader([]byte{0x5a, 0xc3, 0xf0}), rangeValue: 510, offset: 250, lastBit: 1}
			b := Decoder{r: bitstream.NewReader([]byte{0x5a, 0xc3, 0xf0}), rangeValue: 510, offset: 250, lastBit: 1}
			_ = a.r.SkipBits(uint(24 - remaining))
			_ = b.r.SkipBits(uint(24 - remaining))
			ca, cb := Context{State: uint8(state), MPS: 1}, Context{State: uint8(state), MPS: 1}
			for step := range 32 {
				var got, want uint8
				switch step % 3 {
				case 0:
					got, want = a.binMPSCandidate(&ca), b.Bin(&cb)
				case 1:
					got, want = a.Bypass(), b.Bypass()
				case 2:
					got, want = a.Terminate(), b.Terminate()
				}
				if got != want || ca != cb || a.rangeValue != b.rangeValue || a.offset != b.offset || a.lastBit != b.lastBit || a.terminated != b.terminated || fmt.Sprint(a.err) != fmt.Sprint(b.err) || a.r.BitsRead() != b.r.BitsRead() {
					t.Fatalf("remain%d state%d step%d不符", remaining, state, step)
				}
			}
		}
	}
}
