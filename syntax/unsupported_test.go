package syntax

import (
	"errors"
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/bitstream"
)

// SP/SI 类型本身合法；不完整的头必须报告截断，而不是工具未实现。
func TestSwitchSliceTruncatedPrefix(t *testing.T) {
	sps := testSPSForSlice()
	pps := testPPSForSlice()
	for _, code := range []uint32{3, 4, 8, 9} {
		w := bitstream.NewWriter()
		writeUEOrFail(t, w, 0)
		writeUEOrFail(t, w, code)
		h, err := ParseSliceHeader(w.Bytes(), sps, ppsMap(pps), false)
		if !errors.Is(err, h264.ErrInvalidData) || errors.Is(err, h264.ErrUnsupported) || h != nil {
			t.Fatalf("slice_type=%d 截断错误 = %v，期望 ErrInvalidData", code, err)
		}
	}
}

// TestUnsupportedQPPPrime 验证 transform bypass 在 M1 显式拒绝。
func TestUnsupportedQPPPrime(t *testing.T) {
	rbsp := encodeTestSPS(t, testSPS{
		profile:         244,
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
