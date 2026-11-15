package decoder

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
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
	gets, puts, _ := d.pool.Stats()
	if gets != puts {
		t.Fatalf("Reset leaked DPB pictures: gets=%d puts=%d", gets, puts)
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
	for _, cfg := range []h264.DecoderConfig{{OutputFormat: h264.PixelFormat(255)}, {Threads: 2}} {
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
		func(s *syntax.SPS, p *syntax.PPS, h *syntax.SliceHeader) { s.ChromaFormatIDC = 0 },
		func(s *syntax.SPS, p *syntax.PPS, h *syntax.SliceHeader) {
			s.BitDepthLumaMinus8 = 1
			s.BitDepthChromaMinus8 = 1
		},
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

func TestDecoder_ClearSliceEntries(t *testing.T) {
	entries := []sliceEntry{{
		unit: nal.Unit{Payload: []byte{1, 2, 3}},
		rbsp: []byte{4, 5, 6},
	}}
	clearSliceEntries(entries)
	if !reflect.DeepEqual(entries[0], sliceEntry{}) {
		t.Fatalf("slice entry after clear = %#v, want zero value", entries[0])
	}
}

// 污染所有可复用分量后，经公共解码入口比较完整原生像素，而非只检查切片长度。
func TestDecoder_TemporaryDirtyReuse(t *testing.T) {
	for _, path := range []string{
		"../testdata/main/main_64x48_yuv420p_cabac_ipb_001.264",
		"../testdata/high10/high10_48x32_yuv420p10le_cabac_ipb_002.264",
		"../testdata/advanced/high422_64x64_yuv422p_cavlc_ipb_progressive_001.264",
		"../testdata/advanced/high422_64x64_yuv422p10le_cabac_ipb_progressive_003.264",
		"../testdata/advanced/high444_64x64_yuv444p_cavlc_ipb_progressive_005.264",
		"../testdata/advanced/high444_64x64_yuv444p10le_cabac_ipb_progressive_007.264",
		"../testdata/advanced/extended_64x64_yuv420p_cavlc_fmo6_007.264",
	} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			want := decodeFile(t, path)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			d := mustDecoder(t, h264.DecoderConfig{})
			for range 2 {
				poisonDecoderTemporary(t, d)
				if err := d.SendPacket(h264.Packet{Data: data}); err != nil {
					t.Fatal(err)
				}
				if err := d.Flush(); err != nil {
					t.Fatal(err)
				}
				var got []byte
				for {
					f, err := d.ReceiveFrame()
					if errors.Is(err, h264.ErrEOF) {
						break
					}
					if err != nil {
						t.Fatal(err)
					}
					for c, pl := range f.Planes {
						_, rows := f.Format.PlaneDimensions(c, f.Width, f.Height)
						rowBytes := f.Format.RowBytes(c, f.Width)
						for y := range rows {
							got = append(got, pl.Data[y*pl.Stride:y*pl.Stride+rowBytes]...)
						}
					}
					f.Release()
				}
				if !bytes.Equal(got, want) {
					t.Fatalf("dirty scratch changed native pixels: got %d bytes, want %d", len(got), len(want))
				}
				if err := d.Reset(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func poisonDecoderTemporary(t *testing.T, d *Decoder) {
	t.Helper()
	sps := &syntax.SPS{ProfileIDC: 244, ChromaFormatIDC: 3, FrameMbsOnlyFlag: true, PicWidthInMbsMinus1: 7, PicHeightInMapUnitsMinus1: 7}
	pps := &syntax.PPS{EntropyCodingModeFlag: true}
	fs, err := newFrameState(d, sps, pps, true)
	if err != nil {
		t.Fatal(err)
	}
	fs.temporary.sliceOwners = make([]int, fs.total)
	for i := range fs.temporary.sliceOwners {
		fs.temporary.sliceOwners[i] = 77
	}
	for i := range fs.decoded {
		fs.decoded[i] = true
		fs.info[i] = mbInfo{intra: true, transform8: true, sliceID: 77, qp: 51}
	}
	for i := range fs.lumaNZ {
		fs.lumaNZ[i] = 16
		fs.chromaNZ[0][i], fs.chromaNZ[1][i] = 16, 16
		fs.mvd[i].X, fs.mvd[i].Y = 127, -127
		fs.mvd1[i] = fs.mvd[i]
		fs.directBlocks[i] = true
	}
	for c := range fs.dcCoded {
		for i := range fs.dcCoded[c] {
			fs.dcCoded[c][i] = true
		}
	}
	for c := range fs.fullResidual {
		for i := range fs.fullResidual[c].luma {
			for j := range fs.fullResidual[c].luma[i] {
				fs.fullResidual[c].luma[i][j] = 12345
			}
		}
		for i := range fs.fullResidual[c].lumaDC {
			fs.fullResidual[c].lumaDC[i] = 12345
		}
		for i := range fs.fullResidual[c].luma8 {
			for j := range fs.fullResidual[c].luma8[i] {
				fs.fullResidual[c].luma8[i][j] = 12345
			}
		}
	}
	fs.release()
}

func TestDecoder_TemporaryDirtyFailureRecovery(t *testing.T) {
	d := mustDecoder(t, h264.DecoderConfig{})
	poisonDecoderTemporary(t, d)
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
	entry, err := d.parseSlice(idr)
	if err != nil {
		t.Fatal(err)
	}
	entry.hdr.FirstMBInSlice = 1 // 第0个MB缺失，dirty info不能掩盖覆盖错误。
	if err := d.decodeFrame([]sliceEntry{entry}, h264.Packet{}); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("missing macroblock: %v", err)
	}
	entry, err = d.parseSlice(idr)
	if err != nil {
		t.Fatal(err)
	}
	entry.data = bitstream.NewReader(nil) // 已建slot后在宏块重建内部失败。
	if err := d.decodeFrame([]sliceEntry{entry}, h264.Packet{}); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("truncated macroblock: %v", err)
	}
	for i := range d.temporary {
		if d.temporary[i].inUse {
			t.Fatalf("failure retained temporary slot %d", i)
		}
	}
	want := decodeFile(t, "../testdata/baseline/baseline_64x48_yuv420p_cavlc_ipp_001.264")
	if err := d.SendPacket(h264.Packet{Data: annex(idr)}); err != nil {
		t.Fatal(err)
	}
	f, err := d.ReceiveFrame()
	if err != nil {
		t.Fatal(err)
	}
	var got []byte
	for c, pl := range f.Planes {
		_, rows := f.Format.PlaneDimensions(c, f.Width, f.Height)
		rowBytes := f.Format.RowBytes(c, f.Width)
		for y := range rows {
			got = append(got, pl.Data[y*pl.Stride:y*pl.Stride+rowBytes]...)
		}
	}
	f.Release()
	if len(want) < len(got) || len(got) == 0 || !bytes.Equal(got, want[:len(got)]) {
		t.Fatal("recovery IDR pixels differ after dirty failure")
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if d.idleTemporaryBytes() != 0 {
		t.Fatal("Close retained temporary storage")
	}
}

func TestDecoder_TemporaryWarmInitializationAllocations(t *testing.T) {
	d := mustDecoder(t, h264.DecoderConfig{})
	sps := &syntax.SPS{ChromaFormatIDC: 3}
	pps := &syntax.PPS{EntropyCodingModeFlag: true}
	init := func() {
		fs := frameState{d: d, sps: sps, pps: pps, total: 64, chromaBlocks: 16}
		fs.acquireTemporary(true)
		fs.releaseTemporary()
	}
	init()
	if n := testing.AllocsPerRun(20, init); n != 0 {
		t.Fatalf("warm temporary initialization allocated %g times", n)
	}
}

func TestDecoder_TemporarySlotsAndBudget(t *testing.T) {
	d := mustDecoder(t, h264.DecoderConfig{})
	sps := &syntax.SPS{ChromaFormatIDC: 3, SeparateColourPlaneFlag: true}
	pps := &syntax.PPS{EntropyCodingModeFlag: true}
	var states [3]frameState
	for i := range states {
		states[i] = frameState{d: d, sps: sps, pps: pps, total: 16384, chromaBlocks: 16}
		states[i].acquireTemporary(true)
		states[i].info[0].sliceID = i
	}
	for i := range states {
		if states[i].info[0].sliceID != i {
			t.Fatal("independent planes alias temporary storage")
		}
	}
	// 三个合法平面的活动工作集可以超过预算；只允许淘汰已归还存储。
	for i := range states {
		states[i].releaseTemporary()
		if d.idleTemporaryBytes() > decoderTemporaryBudget {
			t.Fatal("idle temporary backing exceeds budget")
		}
		for j := i + 1; j < len(states); j++ {
			if states[j].info[0].sliceID != j {
				t.Fatal("returning a plane discarded an active plane")
			}
		}
	}
	if d.idleTemporaryBytes() == 0 {
		t.Fatal("bounded storage was not retained")
	}
	// 用很短的有效域和超预算的实际cap防止账本误按len计费。
	d.temporary = [3]frameTemporary{}
	d.temporary[0].decoded = make([]bool, 1, decoderTemporaryBudget+1)
	d.trimTemporary()
	if d.idleTemporaryBytes() != 0 {
		t.Fatal("oversized backing was charged by length instead of capacity")
	}
}
