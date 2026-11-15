package cabac

import (
	"bytes"
	"errors"
	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/bitstream"
	"math/rand"
	"testing"
)

func TestEncoder_AnalyticIntervals(t *testing.T) {
	// 初始 R=510。只终止取 [508,510) 的奇数509；state0 的 MPS 区间
	// 为 [0,270)，终止取269；LPS 路径重归一化后取最高奇数码点。
	for _, tc := range []struct {
		bin   int
		bytes []byte
	}{{-1, []byte{0xfe, 0x80}}, {0, []byte{0x86, 0x80}}, {1, []byte{0xfe, 0xc0}}} {
		w := bitstream.NewWriter()
		e, err := NewEncoder(w)
		if err != nil {
			t.Fatal(err)
		}
		c := Context{}
		if tc.bin >= 0 {
			e.Bin(&c, uint8(tc.bin))
		}
		e.Terminate(1)
		if e.Err() != nil || !bytes.Equal(w.Bytes(), tc.bytes) {
			t.Fatal(tc.bin, w.Bytes(), tc.bytes, e.Err())
		}
	}
}

func TestEncoder_AllStatesAndMixedBins(t *testing.T) {
	rng := rand.New(rand.NewSource(93))
	type event struct{ kind, index, bin uint8 }
	for state := 0; state < 64; state++ {
		for mps := 0; mps < 2; mps++ {
			for length := 0; length < 130; length += 7 {
				var initial [16]Context
				for i := range initial {
					initial[i] = Context{State: uint8((state + i) % 64), MPS: uint8((mps + i) % 2)}
				}
				encoded, decoded := initial, initial
				w := bitstream.NewWriter()
				e, err := NewEncoder(w)
				if err != nil {
					t.Fatal(err)
				}
				var events []event
				for n := 0; n < length; n++ {
					v := event{kind: uint8(rng.Intn(3)), index: uint8(rng.Intn(16)), bin: uint8(rng.Intn(2))}
					if v.kind == 2 {
						v.bin = 0
					}
					events = append(events, v)
					switch v.kind {
					case 0:
						e.Bin(&encoded[v.index], v.bin)
					case 1:
						e.Bypass(v.bin)
					case 2:
						e.Terminate(0)
					}
				}
				e.Terminate(1)
				if e.Err() != nil {
					t.Fatal(state, mps, length, e.Err())
				}
				r := bitstream.NewReader(w.Bytes())
				d, err := New(r)
				if err != nil {
					t.Fatal(state, mps, length, err, w.Bytes())
				}
				for n, v := range events {
					var got uint8
					switch v.kind {
					case 0:
						got = d.Bin(&decoded[v.index])
					case 1:
						got = d.Bypass()
					case 2:
						got = d.Terminate()
					}
					if got != v.bin || d.Err() != nil {
						t.Fatalf("state%d mps%d length%d event%d got%d want%d err%v bytes%x", state, mps, length, n, got, v.bin, d.Err(), w.Bytes())
					}
				}
				if d.Terminate() != 1 || d.Err() != nil || encoded != decoded {
					t.Fatal(state, mps, length, d.Err())
				}
			}
		}
	}
}

func TestEncoder_InvalidAndStickyError(t *testing.T) {
	if _, err := NewEncoder(nil); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal(err)
	}
	w := bitstream.NewWriter()
	w.WriteBit(1)
	if _, err := NewEncoder(w); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal(err)
	}
	w = bitstream.NewWriter()
	e, _ := NewEncoder(w)
	e.Bin(nil, 0)
	if !errors.Is(e.Err(), h264.ErrInvalidData) {
		t.Fatal(e.Err())
	}
	e.Terminate(1)
	if w.BitsWritten() != 0 {
		t.Fatal("error wrote output")
	}
}
