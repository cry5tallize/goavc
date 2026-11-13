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

func TestReorder_PTSAndFlush(t *testing.T) {
	data, err := os.ReadFile("../testdata/main/main_96x64_yuv420p_cabac_default_008.264")
	if err != nil {
		t.Fatal(err)
	}
	units, err := nal.SplitAnnexB(data)
	if err != nil {
		t.Fatal(err)
	}
	d := mustDecoder(t, h264.DecoderConfig{})
	var timestamps []int64
	count := 0
	drain := func(final bool) {
		for {
			f, err := d.ReceiveFrame()
			if errors.Is(err, h264.ErrAgain) {
				if final {
					t.Fatal("Again after Flush")
				}
				break
			}
			if errors.Is(err, h264.ErrEOF) {
				if !final {
					t.Fatal("EOF before Flush")
				}
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if f.Duration != 2000 {
				t.Fatal("duration lost")
			}
			timestamps = append(timestamps, f.PTS)
			f.Release()
		}
	}
	for _, u := range units {
		if u.Header.Type == nal.TypeSPS || u.Header.Type == nal.TypePPS {
			if err := d.SendPacket(h264.Packet{Data: annex(u)}); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if !u.Header.IsVCL() {
			continue
		}
		entry, err := d.parseSlice(u)
		if err != nil {
			t.Fatal(err)
		}
		packet := h264.Packet{Data: annex(u), PTS: int64(entry.hdr.PicOrderCntLsb) * 1000, Duration: 2000}
		if err := d.SendPacket(packet); err != nil {
			t.Fatal(err)
		}
		clear(packet.Data)
		count++
		drain(false)
		if count == 1 && len(timestamps) != 0 {
			t.Fatal("reorder buffer emitted the first frame prematurely")
		}
	}
	if count != 16 || len(d.pending) == 0 || len(timestamps) >= count {
		t.Fatal("stream did not exercise delayed output")
	}
	if err := d.Flush(); err != nil {
		t.Fatal(err)
	}
	drain(true)
	if err := d.Flush(); err != nil {
		t.Fatal(err)
	}
	drain(true)
	if len(timestamps) != count {
		t.Fatalf("got %d frames want %d", len(timestamps), count)
	}
	for i, pts := range timestamps {
		if pts != int64(i*2000) {
			t.Fatalf("display frame %d PTS %d", i, pts)
		}
	}
}

func TestWeights_FadeUsesNonDefaultValues(t *testing.T) {
	data, err := os.ReadFile("../testdata/main/main_160x96_yuv420p_cabac_weighted_011.264")
	if err != nil {
		t.Fatal(err)
	}
	units, err := nal.SplitAnnexB(data)
	if err != nil {
		t.Fatal(err)
	}
	sps := map[uint32]*syntax.SPS{}
	pps := map[uint32]*syntax.PPS{}
	found := false
	for _, u := range units {
		rbsp, err := bitstream.EBSPToRBSP(u.Payload)
		if err != nil {
			t.Fatal(err)
		}
		switch u.Header.Type {
		case nal.TypeSPS:
			s, err := syntax.ParseSPS(rbsp)
			if err != nil {
				t.Fatal(err)
			}
			sps[s.SeqParameterSetID] = s
		case nal.TypePPS:
			_, id, err := peekPPSIDs(rbsp)
			if err != nil {
				t.Fatal(err)
			}
			p, err := syntax.ParsePPS(rbsp, sps[id])
			if err != nil {
				t.Fatal(err)
			}
			pps[p.PicParameterSetID] = p
		case nal.TypeSlice, nal.TypeSliceIDR:
			id, err := peekSlicePPSID(rbsp)
			if err != nil {
				t.Fatal(err)
			}
			h, err := syntax.ParseSliceHeaderEx(rbsp, sps[pps[id].SeqParameterSetID], pps, u.Header.IsIDR(), u.Header.RefIDC)
			if err != nil {
				t.Fatal(err)
			}
			if h.PredictionWeights != nil {
				w := h.PredictionWeights
				for _, v := range w.Lists[0] {
					if v.Weight[0] != 1<<w.LumaDenom || v.Offset[0] != 0 {
						found = true
					}
				}
			}
		}
	}
	if !found {
		t.Fatal("fade corpus has only identity weights")
	}
}
