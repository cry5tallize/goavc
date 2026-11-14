package decoder

import (
	"errors"
	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/nal"
	"github.com/goavc/h264/syntax"
	"os"
	"testing"
)

func TestUnsupported_PAFFDoesNotPolluteRecovery(t *testing.T) {
	field, err := os.ReadFile("../testdata/main/main_64x64_yuv420p_cabac_interlaced_013.264")
	if err != nil {
		t.Fatal(err)
	}
	// MBAFF已进入支持路径；此恢复契约仅覆盖仍未接入的PAFF序列。
	units, err := nal.SplitAnnexB(field)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range units {
		if u.Header.Type != nal.TypeSPS {
			continue
		}
		rb, err := bitstream.EBSPToRBSP(u.Payload)
		if err != nil {
			t.Fatal(err)
		}
		s, err := syntax.ParseSPS(rb)
		if err != nil {
			t.Fatal(err)
		}
		s.MbAdaptiveFrameFieldFlag = false
		rb, err = s.MarshalRBSP()
		if err != nil {
			t.Fatal(err)
		}
		u.Payload = bitstream.RBSPToEBSP(rb)
		field = annex(u)
		break
	}
	good, err := os.ReadFile("../testdata/main/main_96x64_yuv420p_cabac_default_008.264")
	if err != nil {
		t.Fatal(err)
	}
	d := mustDecoder(t, h264.DecoderConfig{})
	if err := d.SendPacket(h264.Packet{Data: field}); !errors.Is(err, h264.ErrUnsupported) {
		t.Fatalf("field stream: %v", err)
	}
	if !d.waitingForIDR || len(d.pending) != 0 {
		t.Fatal("unsupported input did not invalidate decode state")
	}
	if err := d.SendPacket(h264.Packet{Data: good}); err != nil {
		t.Fatal(err)
	}
	if err := d.Flush(); err != nil {
		t.Fatal(err)
	}
	count := 0
	for {
		f, err := d.ReceiveFrame()
		if errors.Is(err, h264.ErrEOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Validate(); err != nil {
			t.Fatal(err)
		}
		f.Release()
		count++
	}
	if count != 16 || d.waitingForIDR {
		t.Fatalf("recovery frames=%d waiting=%v", count, d.waitingForIDR)
	}
}

func TestUnsupported_ArbitrarySliceOrder(t *testing.T) {
	d := mustDecoder(t, h264.DecoderConfig{})
	var idr nal.Unit
	for _, u := range baselineUnits(t) {
		if u.Header.Type == nal.TypeSPS || u.Header.Type == nal.TypePPS {
			if err := d.SendPacket(h264.Packet{Data: annex(u)}); err != nil {
				t.Fatal(err)
			}
		}
		if u.Header.IsIDR() {
			idr = u
		}
	}
	a, err := d.parseSlice(idr)
	if err != nil {
		t.Fatal(err)
	}
	b := a
	ha, hb := *a.hdr, *a.hdr
	ha.FirstMBInSlice = 1
	hb.FirstMBInSlice = 0
	a.hdr, b.hdr = &ha, &hb
	if err := d.decodeFrame([]sliceEntry{a, b}, h264.Packet{}); !errors.Is(err, h264.ErrUnsupported) {
		t.Fatal(err)
	}
}
