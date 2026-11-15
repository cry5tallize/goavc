package encoder

import (
	"errors"
	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/bitstream"
	"github.com/cry5tallize/goavc/internal/picture"
	"github.com/cry5tallize/goavc/internal/testutil/pattern"
	"github.com/cry5tallize/goavc/syntax"
	"testing"
)

func TestTransform8x8_ActualInterDecision(t *testing.T) {
	cfg := h264.DefaultEncoderConfig()
	cfg.Profile = h264.ProfileHigh
	cfg.Width, cfg.Height = 32, 32
	cfg.QP = 18
	e, err := New(cfg, WithTransform8x8(), WithSearchRange(4))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	f := testFrame(32, 32)
	for comp := range f.Planes {
		for y := 0; y < 32/(1+flag(comp > 0)); y++ {
			for x := 0; x < 32/(1+flag(comp > 0)); x++ {
				f.Planes[comp].Data[y*f.Planes[comp].Stride+x] = byte(30 + (x*17+y*23+comp*29)%160)
			}
		}
	}
	if err = e.SendFrame(f); err != nil {
		t.Fatal(err)
	}
	e.ReceivePacket()
	r, err := e.Reconstruction()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Release()
	r.KeyFrame = false
	for comp := range r.Planes {
		for i, v := range r.Planes[comp].Data {
			r.Planes[comp].Data[i] = v + 12
		}
	}
	if err = e.SendFrame(r); err != nil {
		t.Fatal(err)
	}
	selected := 0
	for _, i := range e.last.info {
		if i.inter && i.transform8 {
			selected++
		}
	}
	if selected == 0 {
		t.Fatal("没有实际选择8×8帧间残差候选")
	}
}

func TestTransform8x8_PCMFallbackClearsCandidateFlags(t *testing.T) {
	src, _ := picture.New(1, 1, h264.YUV420P)
	dst, _ := picture.New(1, 1, h264.YUV420P)
	s := frameState{src: src, pic: dst, info: []mbInfo{{transform8: true, is16: true, inter: true, skip: true}}, nz: make([]uint8, 16)}
	for c := range s.cnz {
		s.cnz[c] = make([]uint8, 16)
	}
	s.writePCMSamples(bitstream.NewWriter(), 0)
	i := s.info[0]
	if !i.pcm || i.transform8 || i.is16 || i.inter || i.skip || i.qp != 0 {
		t.Fatalf("PCM回退残留候选状态：%+v", i)
	}
}

func TestTransform8x8_ActualIntraDecision(t *testing.T) {
	cfg := h264.DefaultEncoderConfig()
	cfg.Profile = h264.ProfileHigh
	cfg.Width, cfg.Height = 32, 32
	cfg.QP = 18
	cfg.GOPSize = 1
	e, err := New(cfg, WithTransform8x8())
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	selected := 0
	for index := 0; index < 30; index++ {
		f := pattern.Frame(32, 32, index)
		if err = e.SendFrame(f); err != nil {
			t.Fatal(err)
		}
		f.Release()
		e.ReceivePacket()
		for _, i := range e.last.info {
			if !i.inter && i.transform8 {
				selected++
			}
		}
	}
	if selected == 0 {
		t.Fatal("未实际选择Intra8×8候选")
	}
}

func TestTransform8x8_ScalingSignalAndValidation(t *testing.T) {
	cfg := h264.DefaultEncoderConfig()
	cfg.Width, cfg.Height = 32, 32
	if _, err := New(cfg, WithTransform8x8()); !errors.Is(err, h264.ErrUnsupported) {
		t.Fatal("Baseline必须拒绝8×8", err)
	}
	cfg.Profile = h264.ProfileMain
	if _, err := New(cfg, WithTransform8x8()); !errors.Is(err, h264.ErrUnsupported) {
		t.Fatal("Main必须拒绝8×8", err)
	}
	cfg.Profile = h264.ProfileHigh
	var a, b [64]uint8
	if _, err := New(cfg, WithScalingMatrix8x8(a, b)); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal("零矩阵项未拒绝", err)
	}
	for i := range a {
		a[i] = uint8(1 + i*3)
		b[i] = uint8(255 - i*2)
	}
	e, err := New(cfg, WithScalingMatrix8x8(a, b))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	rb, err := e.pps.MarshalRBSP(e.sps)
	if err != nil {
		t.Fatal(err)
	}
	pps, err := syntax.ParsePPS(rb, e.sps)
	if err != nil {
		t.Fatal(err)
	}
	m, err := syntax.SelectScalingMatrices(e.sps, pps)
	if err != nil {
		t.Fatal(err)
	}
	if m.Eight[0] != a || m.Eight[1] != b {
		t.Fatal("8×8 PPS矩阵扫描或delta回绕错误")
	}
	for _, matrix := range m.Four {
		for _, value := range matrix {
			if value != 16 {
				t.Fatal("8×8矩阵改变4×4/DC矩阵")
			}
		}
	}
}
