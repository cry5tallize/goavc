package cabac

import (
	"fmt"
	"testing"

	"github.com/cry5tallize/goavc/bitstream"
)

// 用原逐bin循环对照。最后一位即使归一化失败，也可能返回1；
// 此时幅值前缀、上下文、lastBit以及读位位置均不得改变。
func TestResidualUnary_StateParity(t *testing.T) {
	for _, state := range []uint8{0, 1, 15, 32, 62, 63, 64, 255} {
		for _, mps := range []uint8{0, 1, 2} {
			for rv := uint32(256); rv < 512; rv++ {
				for _, off := range []uint32{0, rv / 2, rv - 1} {
					for remaining := range 25 {
						var data = []byte{0x51, 0xce, 0x07}
						ra, rb := bitstream.NewReader(data), bitstream.NewReader(data)
						_ = ra.SkipBits(uint(24 - remaining))
						_ = rb.SkipBits(uint(24 - remaining))
						a := Decoder{r: ra, rangeValue: rv, offset: off, lastBit: 1}
						b := Decoder{r: rb, rangeValue: rv, offset: off, lastBit: 1}
						ca, cb := Context{state, mps}, Context{state, mps}
						got := a.residualUnary(&ca)
						want := int32(2)
						for want < 15 && b.Err() == nil && b.Bin(&cb) != 0 {
							want++
						}
						pa, ea := ra.PeekBits(uint(ra.BitsRemaining()))
						pb, eb := rb.PeekBits(uint(rb.BitsRemaining()))
						if got != want || ca != cb || a.rangeValue != b.rangeValue || a.offset != b.offset || a.lastBit != b.lastBit || a.terminated != b.terminated || !sameUnaryError(a.err, b.err) || ra.BitsRead() != rb.BitsRead() || pa != pb || !sameUnaryError(ea, eb) {
							t.Fatalf("state=%d mps=%d range=%d offset=%d remaining=%d: level %d != %d or state differs", state, mps, rv, off, remaining, got, want)
						}
					}
				}
			}
		}
	}
}

func sameUnaryError(a, b error) bool {
	if a == b {
		return true
	}
	return a != nil && b != nil && fmt.Sprint(a) == fmt.Sprint(b)
}

func TestResidualUnary_Lifecycle(t *testing.T) {
	for _, tc := range []struct {
		name       string
		terminated bool
		err        error
		context    *Context
	}{
		{name: "nil context"},
		{name: "terminated", terminated: true, context: &Context{State: 32, MPS: 1}},
		{name: "sticky error", err: fmt.Errorf("existing error"), terminated: true, context: &Context{State: 32, MPS: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := bitstream.NewReader([]byte{0x51})
			d := Decoder{r: r, rangeValue: 510, lastBit: 1, terminated: tc.terminated, err: tc.err}
			b := d
			if b.Err() == nil {
				b.Bin(tc.context)
			}
			if got := d.residualUnary(tc.context); got != 2 || !sameUnaryError(d.err, b.err) || d.rangeValue != b.rangeValue || d.offset != b.offset || d.lastBit != b.lastBit || r.BitsRead() != 0 {
				t.Fatalf("lifecycle changed: level=%d err=%v (want %v)", got, d.err, b.err)
			}
			if tc.context != nil && *tc.context != (Context{State: 32, MPS: 1}) {
				t.Fatal("context changed after failure or termination")
			}
		})
	}
}

// 用同一个真实编码的幅值前缀，对照重复 Bin 和候选循环。
// level=2 只解一个 bin；level=15 吃满 13 个，不消费逃逸旁路位。
func BenchmarkResidualUnary_Level_Compare(b *testing.B) {
	for _, level := range []int32{2, 3, 8, 15} {
		for _, candidate := range []bool{false, true} {
			b.Run(fmt.Sprintf("level%d/candidate%t", level, candidate), func(b *testing.B) {
				w := bitstream.NewWriter()
				e, err := NewEncoder(w)
				if err != nil {
					b.Fatal(err)
				}
				initial := Context{State: 32, MPS: 1}
				c := initial
				for k := int32(2); k < 15; k++ {
					if k == level {
						e.Bin(&c, 0)
						break
					}
					e.Bin(&c, 1)
				}
				e.Terminate(1)
				if e.Err() != nil {
					b.Fatal(e.Err())
				}
				r := bitstream.NewReader(w.Bytes())
				d, err := New(r)
				if err != nil {
					b.Fatal(err)
				}
				initialR, initialD := *r, *d
				b.ReportAllocs()
				for b.Loop() {
					*r, *d, c = initialR, initialD, initial
					got := int32(2)
					if candidate {
						got = d.residualUnary(&c)
					} else {
						for got < 15 && d.Err() == nil && d.Bin(&c) != 0 {
							got++
						}
					}
					if got != level || d.Err() != nil {
						b.Fatal(got, d.Err())
					}
				}
			})
		}
	}
}
