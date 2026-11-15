package cavlc

import (
	"github.com/cry5tallize/goavc/bitstream"
	"math/rand"
	"testing"
)

func TestEncodeBlock_ExtremeDistributions(t *testing.T) {
	rng := rand.New(rand.NewSource(417))
	for typ := Luma4x4; typ <= ChromaDC; typ++ {
		for _, nc := range []int{-1, 0, 1, 2, 3, 4, 7, 8, 16} {
			for n := 0; n < 400; n++ {
				var src [16]int32
				for i := range src {
					if rng.Intn(3) == 0 {
						src[i] = int32(rng.Intn(7) - 3)
					}
				}
				switch n {
				case 0:
					clear(src[:])
				case 1:
					clear(src[:])
					src[15] = 1
				case 2:
					for i := range src {
						src[i] = 32767
					}
				case 3:
					for i := range src {
						src[i] = -32768
					}
				case 4:
					clear(src[:])
					src[0] = -1
					src[15] = 1000000
				}
				if typ == ChromaDC {
					clear(src[4:])
				}
				if typ == LumaAC {
					src[0] = 0
				}
				w := bitstream.NewWriter()
				want := 0
				for _, v := range src {
					if v != 0 {
						want++
					}
				}
				count, err := EncodeBlock(w, &src, typ, nc)
				if err != nil || count != want {
					t.Fatalf("type %d n %d: count=%d want=%d %v", typ, n, count, want, err)
				}
				bits := w.BitsWritten()
				bitstream.WriteRBSPTrailingBits(w)
				r := bitstream.NewReader(w.Bytes())
				var got [16]int32
				count, err = DecodeBlock(r, &got, typ, nc)
				if err != nil || count != want || got != src || r.BitsRead() != bits {
					t.Fatalf("type=%d nc=%d n=%d got=%v want=%v count=%d/%d err=%v", typ, nc, n, got, src, count, want, err)
				}
			}
		}
	}
}

func TestEncodeBlock_AnalyticBits(t *testing.T) {
	// Spec: tables 9-5/9-7。nC=0，全零为 1；唯一 DC +1 为 token 01 + sign 0 + zeros 1。
	for _, tc := range []struct {
		v    int32
		bits string
	}{{0, "1"}, {1, "0101"}, {-1, "0111"}} {
		var src [16]int32
		src[0] = tc.v
		w := bitstream.NewWriter()
		if _, err := EncodeBlock(w, &src, Luma4x4, 0); err != nil {
			t.Fatal(err)
		}
		var got string
		r := bitstream.NewReader(w.Bytes())
		for range w.BitsWritten() {
			v, _ := r.ReadBits(1)
			got += string('0' + byte(v))
		}
		if got != tc.bits {
			t.Fatalf("%d got %s want %s", tc.v, got, tc.bits)
		}
	}
}

func FuzzEncodeBlock_Symmetry(f *testing.F) {
	f.Add([]byte{0, 1, 255, 0, 99}, uint8(0), uint8(0))
	f.Fuzz(func(t *testing.T, data []byte, kind, nc uint8) {
		typ := BlockType(kind % 4)
		var src [16]int32
		for i := 0; i < 16 && i*2+1 < len(data); i++ {
			src[i] = int32(int16(uint16(data[i*2])<<8 | uint16(data[i*2+1])))
		}
		if typ == ChromaDC {
			clear(src[4:])
		}
		if typ == LumaAC {
			src[0] = 0
		}
		w := bitstream.NewWriter()
		_, err := EncodeBlock(w, &src, typ, int(nc%17))
		if err != nil {
			t.Fatal(err)
		}
		bitstream.WriteRBSPTrailingBits(w)
		var got [16]int32
		if _, err = DecodeBlock(bitstream.NewReader(w.Bytes()), &got, typ, int(nc%17)); err != nil || got != src {
			t.Fatalf("%v got %v want %v", err, got, src)
		}
	})
}
