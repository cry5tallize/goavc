package syntax

import (
	"errors"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
)

// TestUnsupportedSliceTypes 验证 SP/SI slice 在 M1 显式拒绝。
func TestUnsupportedSliceTypes(t *testing.T) {
	sps := testSPSForSlice()
	pps := testPPSForSlice()
	for _, code := range []uint32{3, 4, 8, 9} {
		w := bitstream.NewWriter()
		writeUEOrFail(t, w, 0)
		writeUEOrFail(t, w, code)
		_, err := ParseSliceHeader(w.Bytes(), sps, ppsMap(pps), false)
		if !errors.Is(err, h264.ErrUnsupported) {
			t.Fatalf("slice_type=%d 错误 = %v，期望 ErrUnsupported", code, err)
		}
	}
}

// TestUnsupportedQPPPrime 验证 transform bypass 在 M1 显式拒绝。
func TestUnsupportedQPPPrime(t *testing.T) {
	rbsp := encodeTestSPS(t, testSPS{
		profile:         100,
		chroma:          1,
		qpprime:         true,
		frameMbsOnly:    true,
		widthMbsMinus1:  3,
		heightMapMinus1: 2,
	})
	if _, err := ParseSPS(rbsp); !errors.Is(err, h264.ErrUnsupported) {
		t.Fatalf("错误 = %v，期望 ErrUnsupported", err)
	}
}
