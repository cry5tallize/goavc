package cabac

import (
	"encoding/csv"
	"errors"
	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/bitstream"
	"os"
	"strconv"
	"testing"
)

func TestDecoder_AnalyticDecisionTrace(t *testing.T) {
	w := bitstream.NewWriter()
	w.WriteBits(9, 300)
	w.WriteBits(7, 0b0001000)
	r := bitstream.NewReader(w.Bytes())
	d, err := New(r)
	if err != nil {
		t.Fatal(err)
	}
	c := Context{}
	// Spec: 表 9-44 的 state=0,q=3 对应 LPS=240。
	// offset300 进入 LPS：range240/offset30，读0归一化到480/60并翻转MPS。
	if b := d.Bin(&c); b != 1 || c != (Context{0, 1}) || d.rangeValue != 480 || d.offset != 60 || r.BitsRead() != 10 {
		t.Fatalf("LPS: %d %+v %+v", b, c, d)
	}
	if b := d.Bin(&c); b != 1 || c != (Context{1, 1}) || d.rangeValue != 480 || d.offset != 120 {
		t.Fatalf("MPS: %d %+v %+v", b, c, d)
	}
	if b := d.Bypass(); b != 0 || d.offset != 240 {
		t.Fatal(b, d.offset)
	}
	if b := d.Bypass(); b != 1 || d.offset != 1 {
		t.Fatal(b, d.offset)
	}
	if b := d.Terminate(); b != 0 || d.rangeValue != 478 || d.Err() != nil {
		t.Fatal(b, d.Err())
	}
}

func TestDecoder_TerminationAndTruncation(t *testing.T) {
	bad := bitstream.NewWriter()
	bad.WriteBits(9, 508)
	bad.ByteAlign()
	invalid, err := New(bitstream.NewReader(bad.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	invalid.Terminate()
	if !errors.Is(invalid.Err(), h264.ErrInvalidData) {
		t.Fatal("accepted a zero stop bit")
	}
	w := bitstream.NewWriter()
	w.WriteBits(9, 509)
	w.ByteAlign()
	d, err := New(bitstream.NewReader(w.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if d.Terminate() != 1 || d.Err() != nil {
		t.Fatal(d.Err())
	}
	d.Bypass()
	if !errors.Is(d.Err(), h264.ErrInvalidData) {
		t.Fatal(d.Err())
	}
	for _, data := range [][]byte{nil, {0}, {255, 128}, {255, 255}} {
		if _, err := New(bitstream.NewReader(data)); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("%x: %v", data, err)
		}
	}
	d, _ = New(bitstream.NewReader([]byte{0, 0}))
	for i := 0; i < 8; i++ {
		d.Bypass()
	}
	if !errors.Is(d.Err(), h264.ErrInvalidData) {
		t.Fatal("missing truncation error")
	}
	before := d.r.BitsRead()
	d.Bin(&Context{})
	if d.r.BitsRead() != before {
		t.Fatal("consumed bits after failure")
	}
}

func TestContexts_AllInitializationEntries(t *testing.T) {
	f, err := os.Open("context_init.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	for mode := 0; mode < 4; mode++ {
		for qp := 0; qp <= 51; qp++ {
			idc := uint32(max(0, mode-1))
			ctx, err := InitContexts(mode == 0, idc, qp)
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < ContextCount; i++ {
				m, _ := strconv.Atoi(rows[i+1][1+mode*2])
				n, _ := strconv.Atoi(rows[i+1][2+mode*2])
				product := m * qp
				scaled := product / 16
				if product < 0 && product%16 != 0 {
					scaled--
				}
				pre := scaled + n
				if pre < 1 {
					pre = 1
				}
				if pre > 126 {
					pre = 126
				}
				want := Context{}
				if pre < 64 {
					want.State = uint8(63 - pre)
				} else {
					want.State = uint8(pre - 64)
					want.MPS = 1
				}
				if ctx[i] != want {
					t.Fatalf("mode=%d qp=%d ctx=%d got=%+v want=%+v", mode, qp, i, ctx[i], want)
				}
			}
		}
	}
	if _, err := InitContexts(false, 3, 26); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal(err)
	}
}

func TestDecoder_AllStateTransitions(t *testing.T) {
	// Spec: 表 9-45 独立固定的 LPS 转移序列；检查两个 MPS 值与翻转点。
	wantLPS := [64]uint8{0, 0, 1, 2, 2, 4, 4, 5, 6, 7, 8, 9, 9, 11, 11, 12, 13, 13, 15, 15, 16, 16, 18, 18, 19, 19, 21, 21, 22, 22, 23, 24, 24, 25, 26, 26, 27, 27, 28, 29, 29, 30, 30, 30, 31, 32, 32, 33, 33, 33, 34, 34, 35, 35, 35, 36, 36, 36, 37, 37, 37, 38, 38, 63}
	for state := 0; state < 64; state++ {
		for mps := 0; mps < 2; mps++ {
			for _, lps := range []bool{false, true} {
				data := []byte{0, 0, 0}
				if lps {
					data = []byte{254, 128, 0}
				}
				d, err := New(bitstream.NewReader(data))
				if err != nil {
					t.Fatal(err)
				}
				c := Context{uint8(state), uint8(mps)}
				got := d.Bin(&c)
				wantState := uint8(min(62, state+1))
				if state == 63 {
					wantState = 63
				}
				wantMPS, wantBin := uint8(mps), uint8(mps)
				if lps {
					wantState = wantLPS[state]
					wantBin ^= 1
					if state == 0 {
						wantMPS ^= 1
					}
				}
				if d.Err() != nil || got != wantBin || c != (Context{wantState, wantMPS}) {
					t.Fatalf("state=%d mps=%d lps=%v: bin=%d ctx=%+v err=%v", state, mps, lps, got, c, d.Err())
				}
			}
		}
	}
}

func FuzzDecoder_Arithmetic(f *testing.F) {
	f.Add([]byte{0, 0, 0}, uint8(0), uint8(0))
	f.Add([]byte{254, 128, 0}, uint8(63), uint8(1))
	f.Fuzz(func(t *testing.T, data []byte, state, mps uint8) {
		d, err := New(bitstream.NewReader(data))
		if err != nil {
			return
		}
		c := Context{state % 64, mps % 2}
		for i := 0; i < 1024 && d.Err() == nil; i++ {
			switch i % 3 {
			case 0:
				d.Bin(&c)
			case 1:
				d.Bypass()
			case 2:
				if d.Terminate() == 1 {
					return
				}
			}
			if d.Err() == nil && (d.rangeValue < 256 || d.rangeValue > 510 || d.offset >= d.rangeValue) {
				t.Fatal("arithmetic invariant")
			}
		}
	})
}
