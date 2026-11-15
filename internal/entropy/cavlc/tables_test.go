package cavlc

import (
	"testing"

	"github.com/cry5tallize/goavc/bitstream"
)

func writeVLC(t *testing.T, w *bitstream.Writer, lengths, codes []uint8, sym int) {
	t.Helper()
	if err := w.WriteBits(uint(lengths[sym]), uint64(codes[sym])); err != nil {
		t.Fatalf("WriteBits: %v", err)
	}
}

func newReader(t *testing.T, w *bitstream.Writer) *bitstream.Reader {
	t.Helper()
	return bitstream.NewReader(w.Bytes())
}

func TestVLCTables_RoundTrip(t *testing.T) {
	tables := []struct {
		name    string
		lengths []uint8
		codes   []uint8
		vlc     *vlcTable
	}{
		{"coeff0", coeffTokenLen[0][:], coeffTokenBits[0][:], &coeffTokenVLC[0]},
		{"coeff1", coeffTokenLen[1][:], coeffTokenBits[1][:], &coeffTokenVLC[1]},
		{"coeff2", coeffTokenLen[2][:], coeffTokenBits[2][:], &coeffTokenVLC[2]},
		{"coeff3", coeffTokenLen[3][:], coeffTokenBits[3][:], &coeffTokenVLC[3]},
		{"chroma", chromaDCCoeffTokenLen[:], chromaDCCoeffTokenBits[:], &chromaDCToken},
		{"tz0", totalZerosLen[0][:], totalZerosBits[0][:], &totalZerosVLC[0]},
		{"tz14", totalZerosLen[14][:], totalZerosBits[14][:], &totalZerosVLC[14]},
		{"run0", runLen[0][:], runBits[0][:], &runVLC[0]},
		{"run6", runLen[6][:], runBits[6][:], &runVLC[6]},
	}
	for _, tt := range tables {
		t.Run(tt.name, func(t *testing.T) {
			for sym, l := range tt.lengths {
				if l == 0 {
					continue
				}
				w := bitstream.NewWriter()
				writeVLC(t, w, tt.lengths, tt.codes, sym)
				// 追加 16 个 1，保证查表窗口有足够比特。
				if err := w.WriteBits(16, 0xFFFF); err != nil {
					t.Fatal(err)
				}
				r := newReader(t, w)
				got, err := tt.vlc.decode(r)
				if err != nil {
					t.Fatalf("sym %d: decode: %v", sym, err)
				}
				if int(got) != sym {
					t.Fatalf("sym %d: got %d", sym, got)
				}
				if r.BitsRead() != uint64(l) {
					t.Fatalf("sym %d: bits read %d, want %d", sym, r.BitsRead(), l)
				}
			}
		})
	}
}

func TestLevelTable_DirectCodes(t *testing.T) {
	// suffixLength=0 时，前 8 位完整码字直接映射；检查若干已知 code。
	// 8 位窗口的码字长度 = prefix+1+suffix；这里只验证解码结果与长度
	// 和 buildLevelEntry 的构造一致（表是自洽的，码字值由规范决定）。
	for suffix := 0; suffix < 7; suffix++ {
		for i := 0; i < 256; i++ {
			e := levelTable[suffix][i]
			if e.length > 8 {
				t.Fatalf("suffix %d entry %d length %d > 8", suffix, i, e.length)
			}
			if e.code < 100 && e.length == 0 {
				t.Fatalf("suffix %d entry %d: zero length with direct code", suffix, i)
			}
		}
	}
}
