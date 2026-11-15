package syntax

import (
	"errors"
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/bitstream"
)

func TestFMOTruncationBeforeParameterAllocation(t *testing.T) {
	for _, tc := range []struct {
		name string
		data byte
	}{
		{"run_length", 0x80}, // map_type=0只占1位，剩余7位不足8个ue(v)。
		{"rectangle", 0x60},  // map_type=2占3位，剩余5位不足7对ue(v)。
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &PPS{NumSliceGroupsMinus1: 7}
			err := p.parseFMO(bitstream.NewReader([]byte{tc.data}), fmoTestSPS())
			if !errors.Is(err, h264.ErrInvalidData) || p.RunLengthMinus1 != nil || p.TopLeft != nil || p.BottomRight != nil {
				t.Fatalf("已确定截断时不能先分配FMO参数数组: %+v / %v", p, err)
			}
		})
	}
}
