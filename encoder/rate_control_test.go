package encoder

import (
	"testing"

	h264 "github.com/goavc/h264"
)

func TestEncoder_VBVBufferBoundaries(t *testing.T) {
	e := &Encoder{
		cfg:           h264.EncoderConfig{RateControl: h264.RateControlVBV, Bitrate: 30_000, FPSNum: 30, FPSDen: 1},
		vbvBufferBits: 100_000,
		vbvFullness:   50_000,
	}
	if ok, _ := e.vbvAccept(6_375); !ok {
		t.Fatal("frame at exactly the upper boundary rejected")
	}
	if ok, _ := e.vbvAccept(6_376); ok {
		t.Fatal("frame above the upper boundary accepted")
	}
	e.vbvFullness = 1_000
	if ok, _ := e.vbvAccept(0); !ok {
		t.Fatal("frame at exactly the lower boundary rejected")
	}
	e.vbvFullness = 999
	if ok, _ := e.vbvAccept(0); ok {
		t.Fatal("frame below the lower boundary accepted")
	}
}
