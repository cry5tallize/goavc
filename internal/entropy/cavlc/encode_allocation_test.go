package cavlc

import (
	"bytes"
	"errors"
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/bitstream"
)

func TestCAVLC_EncodeAtomicLateLevel(t *testing.T) {
	for _, typ := range []BlockType{Luma4x4, LumaAC, LumaDC, ChromaDC, ChromaDC422} {
		scan := defaultBlockScan(typ)
		for _, at := range []int{0, len(scan) - 1} {
			for _, tc := range []struct {
				value int32
				limit int
				want  error
			}{{1 << 17, 15, h264.ErrUnsupported}, {-1 << 31, 28, h264.ErrInvalidData}} {
				var src [16]int32
				for i, p := range scan {
					src[p] = int32(3*i + 7)
				}
				src[scan[at]] = tc.value
				w := bitstream.NewWriter()
				w.WriteBits(3, 5)
				before := w.Bytes()
				n, err := EncodeBlockLimited(w, &src, typ, 2, tc.limit)
				if n != 0 || !errors.Is(err, tc.want) || w.BitsWritten() != 3 || !bytes.Equal(w.Bytes(), before) {
					t.Fatalf("typ=%d at=%d limit=%d: n=%d err=%v bits=%d", typ, at, tc.limit, n, err, w.BitsWritten())
				}
			}
		}
	}
}

func TestAllocation_CAVLCEncode(t *testing.T) {
	var src [16]int32
	for i := range src {
		src[i] = int32(7*i - 51)
	}
	w := bitstream.NewWriter()
	allocs := testing.AllocsPerRun(50, func() {
		w.Reset()
		w.WriteBits(3, 5)
		if _, err := EncodeBlock(w, &src, Luma4x4, 2); err != nil {
			panic(err)
		}
	})
	if allocs != 0 || w.BitsWritten() <= 3 {
		t.Fatalf("allocs=%g bits=%d", allocs, w.BitsWritten())
	}
}
