package decoder

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/nal"
	"github.com/goavc/h264/syntax"
)

func mustDecoder(t *testing.T, cfg h264.DecoderConfig) *Decoder {
	t.Helper()
	d, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func baselineUnits(t testing.TB) []nal.Unit {
	t.Helper()
	data, err := os.ReadFile("../testdata/baseline/baseline_64x48_yuv420p_cavlc_ipp_001.264")
	if err != nil {
		t.Fatal(err)
	}
	u, err := nal.SplitAnnexB(data)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func annex(units ...nal.Unit) []byte {
	var out []byte
	for _, u := range units {
		out = append(out, 0, 0, 0, 1)
		out = append(out, u.Bytes()...)
	}
	return out
}

func TestDecoder_LifecycleAndRecovery(t *testing.T) {
	d := mustDecoder(t, h264.DecoderConfig{})
	if _, err := d.ReceiveFrame(); !errors.Is(err, h264.ErrAgain) {
		t.Fatal(err)
	}
	var params, idr, p []nal.Unit
	for _, u := range baselineUnits(t) {
		switch u.Header.Type {
		case nal.TypeSPS, nal.TypePPS:
			params = append(params, u)
		case nal.TypeSliceIDR:
			idr = append(idr, u)
		case nal.TypeSlice:
			p = append(p, u)
		}
	}
	if err := d.SendPacket(h264.Packet{Data: annex(params...)}); err != nil {
		t.Fatal(err)
	}
	if err := d.SendPacket(h264.Packet{Data: []byte{0, 0, 1, 0x65, 0x80}}); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("corruption: %v", err)
	}
	if !d.waitingForIDR {
		t.Fatal("must wait for IDR")
	}
	if err := d.SendPacket(h264.Packet{Data: annex(p...)}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ReceiveFrame(); !errors.Is(err, h264.ErrAgain) {
		t.Fatal("P picture should be discarded", err)
	}
	packet := h264.Packet{Data: annex(idr...), PTS: 123, Duration: 7}
	if err := d.SendPacket(packet); err != nil {
		t.Fatal(err)
	}
	// Send 返回后调用方可覆写输入；输出和参考帧必须独立。
	clear(packet.Data)
	f, err := d.ReceiveFrame()
	if err != nil {
		t.Fatal(err)
	}
	if f.PTS != 123 || f.Duration != 7 || !f.KeyFrame || f.Width != 64 || f.Height != 48 {
		t.Fatalf("frame metadata: %+v", f)
	}
	if err := f.Validate(); err != nil {
		t.Fatal(err)
	}
	clone := f.Clone()
	f.Release()
	f.Release()
	if err := clone.Validate(); err != nil {
		t.Fatal(err)
	}
	clone.Release()
	if err := d.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := d.Flush(); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ReceiveFrame(); !errors.Is(err, h264.ErrEOF) {
		t.Fatal(err)
	}
	if err := d.SendPacket(h264.Packet{Data: annex(idr...)}); !errors.Is(err, h264.ErrEOF) {
		t.Fatal(err)
	}
	if err := d.Reset(); err != nil {
		t.Fatal(err)
	}
	if err := d.SendPacket(h264.Packet{Data: annex(idr...)}); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal("Reset retained parameter sets", err)
	}
	d.Close()
	d.Close()
	if _, err := d.ReceiveFrame(); !errors.Is(err, h264.ErrEOF) {
		t.Fatal(err)
	}
	if err := d.Reset(); !errors.Is(err, h264.ErrEOF) {
		t.Fatal(err)
	}
}

func TestDecoder_ConfigurationAndLimits(t *testing.T) {
	for _, cfg := range []h264.DecoderConfig{{MaxWidth: -1}, {MaxHeight: -1}, {MaxNALSize: -1}, {MaxSliceCount: -1}, {MaxDPBFrames: -1}, {MaxFrameSize: -1}, {Threads: -1}} {
		if _, err := New(cfg); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("config %+v: %v", cfg, err)
		}
	}
	for _, cfg := range []h264.DecoderConfig{{OutputFormat: h264.YUV422P}, {Threads: 2}} {
		if _, err := New(cfg); !errors.Is(err, h264.ErrUnsupported) {
			t.Fatal(err)
		}
	}
	data := annex(baselineUnits(t)...)
	for _, cfg := range []h264.DecoderConfig{{MaxWidth: 32}, {MaxHeight: 32}, {MaxFrameSize: 100}, {MaxNALSize: 16}} {
		d := mustDecoder(t, cfg)
		if err := d.SendPacket(h264.Packet{Data: data}); !errors.Is(err, h264.ErrResourceLimit) {
			t.Fatalf("limit %+v: %v", cfg, err)
		}
	}
	data, err := os.ReadFile("../testdata/baseline/baseline_320x240_yuv420p_cavlc_ip_multislice_003.264")
	if err != nil {
		t.Fatal(err)
	}
	d := mustDecoder(t, h264.DecoderConfig{MaxSliceCount: 1})
	if err := d.SendPacket(h264.Packet{Data: data}); !errors.Is(err, h264.ErrResourceLimit) {
		t.Fatal(err)
	}
}

func TestDecoder_UnsupportedTools(t *testing.T) {
	s := &syntax.SPS{MaxNumRefFrames: 1, FrameMbsOnlyFlag: true, ChromaFormatIDC: 1}
	p := &syntax.PPS{}
	h := &syntax.SliceHeader{Type: syntax.SliceTypeI}
	for _, mutate := range []func(*syntax.SPS, *syntax.PPS, *syntax.SliceHeader){
		func(s *syntax.SPS, p *syntax.PPS, h *syntax.SliceHeader) { s.FrameMbsOnlyFlag = false },
		func(s *syntax.SPS, p *syntax.PPS, h *syntax.SliceHeader) { s.MbAdaptiveFrameFieldFlag = true },
		func(s *syntax.SPS, p *syntax.PPS, h *syntax.SliceHeader) { s.ChromaFormatIDC = 2 },
		func(s *syntax.SPS, p *syntax.PPS, h *syntax.SliceHeader) {
			s.BitDepthLumaMinus8 = 1
			s.BitDepthChromaMinus8 = 1
		},
		func(s *syntax.SPS, p *syntax.PPS, h *syntax.SliceHeader) { h.RedundantPicCnt = 1 },
		func(s *syntax.SPS, p *syntax.PPS, h *syntax.SliceHeader) { h.Type = syntax.SliceTypeSP },
	} {
		ss, pp, hh := *s, *p, *h
		mutate(&ss, &pp, &hh)
		if err := (&Decoder{}).checkSupported(&ss, &pp, &hh); !errors.Is(err, h264.ErrUnsupported) {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile("../testdata/main/main_64x48_yuv420p_cabac_ipb_001.264")
	if err != nil {
		t.Fatal(err)
	}
	d := mustDecoder(t, h264.DecoderConfig{})
	if err := d.SendPacket(h264.Packet{Data: data}); err != nil {
		t.Fatal(err)
	}
}

// pcmStream 提供规范直接可解的 I_PCM→P16x16 向量，包含 POC type 0、
// 非零左/上 crop 和大幅越界 MV；既有解析解，也可交给 FFmpeg 独立验证。
func pcmStream(t testing.TB, mvx, mvy int32, crop bool, nonRef ...bool) []byte {
	t.Helper()
	s := &syntax.SPS{ProfileIDC: 66, LevelIDC: 10, ChromaFormatIDC: 1, MaxNumRefFrames: 1, FrameMbsOnlyFlag: true, Direct8x8InferenceFlag: true}
	if crop {
		s.FrameCroppingFlag = true
		s.FrameCropLeftOffset = 1
		s.FrameCropTopOffset = 1
	}
	p := &syntax.PPS{DeblockingFilterControlPresentFlag: true}
	srb, err := s.MarshalRBSP()
	if err != nil {
		t.Fatal(err)
	}
	prb, err := p.MarshalRBSP(s)
	if err != nil {
		t.Fatal(err)
	}
	unit := func(typ nal.UnitType, rbsp []byte) nal.Unit {
		return nal.Unit{Header: nal.Header{Type: typ, RefIDC: 3}, Payload: bitstream.RBSPToEBSP(rbsp)}
	}
	units := []nal.Unit{unit(nal.TypeSPS, srb), unit(nal.TypePPS, prb)}
	for frame := 0; frame < 2; frame++ {
		w := bitstream.NewWriter()
		w.WriteUE(0)
		if frame == 0 {
			w.WriteUE(2)
		} else {
			w.WriteUE(0)
		}
		w.WriteUE(0)
		w.WriteBits(4, uint64(frame))
		if frame == 0 {
			w.WriteUE(0)
		}
		poc := frame * 2
		if frame == 1 && len(nonRef) > 1 && nonRef[1] {
			poc = 4
		}
		w.WriteBits(4, uint64(poc))
		isNonRef := frame == 1 && len(nonRef) > 0 && nonRef[0]
		if frame == 0 {
			w.WriteBits(2, 0)
		} else {
			w.WriteBits(1, 0)
			w.WriteBits(1, 0)
			if !isNonRef {
				w.WriteBits(1, 0)
			}
		}
		w.WriteSE(0)
		w.WriteUE(1)
		if frame == 0 {
			w.WriteUE(25)
			w.ByteAlign()
			for pi := 0; pi < 3; pi++ {
				size := 16
				if pi > 0 {
					size = 8
				}
				for y := 0; y < size; y++ {
					for x := 0; x < size; x++ {
						w.WriteBits(8, uint64(20+pi*40+x+y*2))
					}
				}
			}
		} else {
			w.WriteUE(0)
			w.WriteUE(0)
			w.WriteSE(mvx)
			w.WriteSE(mvy)
			w.WriteUE(0)
		}
		bitstream.WriteRBSPTrailingBits(w)
		typ := nal.TypeSlice
		if frame == 0 {
			typ = nal.TypeSliceIDR
		}
		u := unit(typ, w.Bytes())
		if isNonRef {
			u.Header.RefIDC = 0
		}
		units = append(units, u)
	}
	return annex(units...)
}

func TestDecoder_NonReferenceAndPOC(t *testing.T) {
	d := mustDecoder(t, h264.DecoderConfig{})
	if err := d.SendPacket(h264.Packet{Data: pcmStream(t, 0, 0, false, true)}); err != nil {
		t.Fatal(err)
	}
	if d.dpb.ReferenceFrameNum() != 0 || d.poc.Prev() != 0 {
		t.Fatal("non-reference frame changed reference state")
	}
	units, err := nal.SplitAnnexB(pcmStream(t, 0, 0, false, false, true))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SendPacket(h264.Packet{Data: annex(units[len(units)-1])}); err != nil {
		t.Fatal(err)
	}
	if d.dpb.ReferenceFrameNum() != 1 || d.dpb.ReferencePOC() != 4 {
		t.Fatal("type 0 POC/reference not updated")
	}
}

func TestDecoder_InvalidSliceCoverage(t *testing.T) {
	for _, first := range []uint32{0, 999999} {
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
		h := *a.hdr
		h.FirstMBInSlice = first
		b.hdr = &h
		if err := d.decodeFrame([]sliceEntry{a, b}, h264.Packet{}); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatal(err)
		}
	}
}

func TestDecoder_PCMAndEdgeMotionOracle(t *testing.T) {
	for _, mv := range [][2]int32{{-400, -400}, {400, 400}, {-1, 3}, {0, 0}} {
		data := pcmStream(t, mv[0], mv[1], true)
		path := filepath.Join(t.TempDir(), "edge.264")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		got := decodeFile(t, path)
		// 裁剪后的第一帧首样本来自原图坐标 (2,2)。
		if len(got) != 14*14*3 || got[0] != 26 {
			t.Fatalf("PCM crop len=%d first=%d", len(got), got[0])
		}
		ff := ffmpegPath(t)
		want := ffmpegRaw(t, ff, path)
		if !bytes.Equal(got, want) {
			t.Fatalf("edge MV %v differs from FFmpeg", mv)
		}
	}
}

func FuzzDecoder_NoPanic(f *testing.F) {
	if data, err := os.ReadFile("../testdata/high/high_64x48_yuv420p_cabac_default_006.264"); err == nil {
		f.Add(data)
	} else {
		f.Fatal(err)
	}
	if data, err := os.ReadFile("../testdata/main/main_64x48_yuv420p_cabac_i_002.264"); err == nil {
		f.Add(data)
	} else {
		f.Fatal(err)
	}
	f.Add(pcmStream(f, -400, 400, false))
	f.Add([]byte{0, 0, 1, 0x65, 0x80})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 65536 {
			t.Skip()
		}
		d, err := New(h264.DecoderConfig{MaxWidth: 64, MaxHeight: 64, MaxNALSize: 8192, MaxSliceCount: 4})
		if err != nil {
			t.Fatal(err)
		}
		defer d.Close()
		_ = d.SendPacket(h264.Packet{Data: data})
		_ = d.Flush()
		for {
			frame, err := d.ReceiveFrame()
			if err != nil {
				break
			}
			if err := frame.Validate(); err != nil {
				t.Fatal(err)
			}
			frame.Release()
		}
	})
}
