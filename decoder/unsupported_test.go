package decoder

import (
	"errors"
	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/nal"
	"os"
	"testing"
)

func TestUnsupported_FieldsDoNotPolluteRecovery(t *testing.T) {
	field, err := os.ReadFile("../testdata/main/main_64x64_yuv420p_cabac_interlaced_013.264")
	if err != nil {
		t.Fatal(err)
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
