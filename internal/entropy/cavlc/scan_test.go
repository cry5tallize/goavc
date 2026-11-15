package cavlc

import (
	"errors"
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/bitstream"
)

func TestBlockScan_FieldSpatialCoefficient(t *testing.T) {
	// 表9-5/9-7固定比特01|0|011：一个正trailing one，前面一个零。
	// 场4×4扫描位置1是(0,1)，帧扫描位置1是(1,0)。
	scan := []int{0, 4, 1, 8, 12, 5, 9, 13, 2, 6, 10, 14, 3, 7, 11, 15}
	r := bitstream.NewReader([]byte{0x4c})
	var dst [16]int32
	count, err := DecodeBlockScan(r, &dst, Luma4x4, 0, scan)
	if err != nil || count != 1 || dst[4] != 1 || dst[1] != 0 || r.BitsRead() != 6 {
		t.Fatalf("field coefficient %v count%d bits%d err%v", dst, count, r.BitsRead(), err)
	}
	w := bitstream.NewWriter()
	count, err = EncodeBlockLimitedScan(w, &dst, Luma4x4, 0, 15, scan)
	if err != nil || count != 1 || w.BitsWritten() != 6 || w.Bytes()[0] != 0x4c {
		t.Fatalf("field code %x bits%d err%v", w.Bytes(), w.BitsWritten(), err)
	}
}

func TestBlockScan_InvalidMappingIsTransactional(t *testing.T) {
	for _, scan := range [][]int{nil, {0}, {0, 0, 1, 8, 12, 5, 9, 13, 2, 6, 10, 14, 3, 7, 11, 15}, {0, 4, 1, 8, 12, 5, 9, 13, 2, 6, 10, 14, 3, 7, 11, 16}} {
		r := bitstream.NewReader([]byte{0x4c})
		dst := [16]int32{9}
		if _, err := DecodeBlockScan(r, &dst, Luma4x4, 0, scan); !errors.Is(err, h264.ErrInvalidData) || r.BitsRead() != 0 || dst[0] != 9 {
			t.Fatalf("invalid scan consumed input: %v", err)
		}
		w := bitstream.NewWriter()
		w.WriteBits(3, 5)
		if _, err := EncodeBlockLimitedScan(w, &dst, Luma4x4, 0, 15, scan); !errors.Is(err, h264.ErrInvalidData) || w.BitsWritten() != 3 {
			t.Fatalf("invalid scan wrote bits: %v", err)
		}
	}
}
