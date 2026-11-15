package decoder

import (
	"bytes"
	"errors"
	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/bitstream"
	"github.com/cry5tallize/goavc/nal"
	"github.com/cry5tallize/goavc/syntax"
	"os"
	"path/filepath"
	"testing"
)

func TestCABAC_IntraPixelOracle(t *testing.T) {
	for _, path := range []string{"../testdata/main/main_64x48_yuv420p_cabac_i_002.264", "../testdata/main/main_176x144_yuv420p_cabac_ip_003.264", "../testdata/main/main_320x240_yuv420p_cabac_ip_multislice_004.264"} {
		t.Run(path, func(t *testing.T) {
			got := decodeFile(t, path)
			want := ffmpegRaw(t, ffmpegPath(t), path)
			if len(got) != len(want) {
				t.Fatalf("decoded %d bytes, want %d", len(got), len(want))
			}
			for i := range got {
				if got[i] != want[i] {
					t.Fatalf("first difference byte %d: got %d want %d", i, got[i], want[i])
				}
			}
		})
	}
}

func TestCABAC_TruncationRecoversAtIDR(t *testing.T) {
	data, err := os.ReadFile("../testdata/main/main_64x48_yuv420p_cabac_i_002.264")
	if err != nil {
		t.Fatal(err)
	}
	units, err := nal.SplitAnnexB(data)
	if err != nil {
		t.Fatal(err)
	}
	d := mustDecoder(t, h264.DecoderConfig{})
	for _, u := range units {
		if u.Header.Type == nal.TypeSPS || u.Header.Type == nal.TypePPS {
			if err := d.SendPacket(h264.Packet{Data: annex(u)}); err != nil {
				t.Fatal(err)
			}
		}
		if u.Header.IsIDR() {
			u.Payload = u.Payload[:len(u.Payload)/2]
			if err := d.SendPacket(h264.Packet{Data: annex(u)}); !errors.Is(err, h264.ErrInvalidData) {
				t.Fatal(err)
			}
			break
		}
	}
	if !d.waitingForIDR {
		t.Fatal("corruption did not invalidate reference state")
	}
	if err := d.SendPacket(h264.Packet{Data: data}); err != nil {
		t.Fatal(err)
	}
	if d.waitingForIDR || len(d.out)+len(d.pending) != 3 {
		t.Fatalf("IDR recovery: waiting=%v frames=%d", d.waitingForIDR, len(d.out))
	}
}

func TestCABAC_PCMRestartOracle(t *testing.T) {
	sps := &syntax.SPS{ProfileIDC: 77, LevelIDC: 10, ChromaFormatIDC: 1, MaxNumRefFrames: 1, FrameMbsOnlyFlag: true, Direct8x8InferenceFlag: true}
	pps := &syntax.PPS{EntropyCodingModeFlag: true, DeblockingFilterControlPresentFlag: true}
	srb, err := sps.MarshalRBSP()
	if err != nil {
		t.Fatal(err)
	}
	prb, err := pps.MarshalRBSP(sps)
	if err != nil {
		t.Fatal(err)
	}
	unit := func(typ nal.UnitType, rbsp []byte) nal.Unit {
		return nal.Unit{Header: nal.Header{Type: typ, RefIDC: 3}, Payload: bitstream.RBSPToEBSP(rbsp)}
	}
	w := bitstream.NewWriter()
	w.WriteUE(0)
	w.WriteUE(2)
	w.WriteUE(0)
	w.WriteBits(4, 0)
	w.WriteUE(0)
	w.WriteBits(4, 0)
	w.WriteBits(2, 0)
	w.WriteSE(0)
	w.WriteUE(1)
	for w.BitsWritten()%8 != 0 {
		w.WriteBits(1, 1)
	}
	// QP=26 的 I ctx3 为 state46/MPS0。range510 的 LPS=22；
	// offset509 后读1111 -> range352/offset351，停止位1，terminate=1 选择 PCM。
	w.WriteBits(9, 509)
	w.WriteBits(4, 15)
	w.ByteAlign()
	want := make([]byte, 384)
	for i := range want {
		want[i] = byte(16 + i%200)
		w.WriteBits(8, uint64(want[i]))
	}
	// PCM 后重启算术引擎；offset509 立即解出 end_of_slice_flag=1。
	w.WriteBits(9, 509)
	w.ByteAlign()
	data := annex(unit(nal.TypeSPS, srb), unit(nal.TypePPS, prb), unit(nal.TypeSliceIDR, w.Bytes()))
	path := filepath.Join(t.TempDir(), "pcm.264")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	got := decodeFile(t, path)
	if !bytes.Equal(got, want) {
		t.Fatal("PCM samples differ")
	}
	if ref := ffmpegRaw(t, ffmpegPath(t), path); !bytes.Equal(got, ref) {
		t.Fatal("PCM differs from FFmpeg")
	}
}

func TestCABAC_ZeroWordPadding(t *testing.T) {
	data, err := os.ReadFile("../testdata/main/main_96x64_yuv420p_cabac_default_008.264")
	if err != nil {
		t.Fatal(err)
	}
	units, err := nal.SplitAnnexB(data)
	if err != nil {
		t.Fatal(err)
	}
	var padded []byte
	for _, u := range units {
		padded = append(padded, annex(u)...)
		if u.Header.IsVCL() {
			padded = append(padded, 0, 0, 0, 0)
		}
	}
	path := filepath.Join(t.TempDir(), "padded.264")
	if err := os.WriteFile(path, padded, 0600); err != nil {
		t.Fatal(err)
	}
	got := decodeFile(t, path)
	base := decodeFile(t, "../testdata/main/main_96x64_yuv420p_cabac_default_008.264")
	if !bytes.Equal(got, base) {
		t.Fatal("CABAC zero words changed output")
	}
	if want := ffmpegRaw(t, ffmpegPath(t), path); !bytes.Equal(got, want) {
		t.Fatal("zero-word stream differs from FFmpeg")
	}
}

func TestCABAC_EncapsulatedZeroWordPadding(t *testing.T) {
	original := "../testdata/main/main_96x64_yuv420p_cabac_default_008.264"
	data, err := os.ReadFile(original)
	if err != nil {
		t.Fatal(err)
	}
	units, err := nal.SplitAnnexB(data)
	if err != nil {
		t.Fatal(err)
	}
	var padded []byte
	for _, unit := range units {
		if unit.Header.IsVCL() {
			unit.Payload = append(unit.Payload, 0, 0, 3, 0, 0, 3)
		}
		padded = append(padded, annex(unit)...)
	}
	path := filepath.Join(t.TempDir(), "encapsulated_zero_words.264")
	if err = os.WriteFile(path, padded, 0600); err != nil {
		t.Fatal(err)
	}
	got := decodeFile(t, path)
	base := decodeFile(t, original)
	if !bytes.Equal(got, base) {
		t.Fatal("encapsulated CABAC padding changed pixels")
	}
	if want := ffmpegRaw(t, ffmpegPath(t), path); !bytes.Equal(got, want) {
		t.Fatal("encapsulated padding differs from FFmpeg")
	}
}

func TestCABAC_TerminalEscapeRejectedForCAVLC(t *testing.T) {
	data, err := os.ReadFile("../testdata/baseline/baseline_64x48_yuv420p_cavlc_ipp_001.264")
	if err != nil {
		t.Fatal(err)
	}
	units, err := nal.SplitAnnexB(data)
	if err != nil {
		t.Fatal(err)
	}
	var malformed []byte
	for _, unit := range units {
		if unit.Header.IsVCL() {
			unit.Payload = append(unit.Payload, 0, 0, 3)
		}
		malformed = append(malformed, annex(unit)...)
	}
	d, err := New(h264.DecoderConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err = d.SendPacket(h264.Packet{Data: malformed}); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal(err)
	}
}
