package encoder

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/internal/picture"
	"github.com/cry5tallize/goavc/internal/prediction"
	"github.com/cry5tallize/goavc/internal/testutil/pattern"
	"github.com/cry5tallize/goavc/syntax"
)

func TestIntraSelection_AllModes(t *testing.T) {
	// 对每种预测先用解析参考邻域生成唯一零 SAD 图案，再调用实际编码决策函数。
	// 不用模式强制选项，避免只测到写出路径而绕过搜索。
	a := prediction.Intra4x4Samples16{Top: [4]uint16{41, 193, 77, 11}, TopRight: [4]uint16{222, 65, 118, 9}, Left: [4]uint16{231, 32, 161, 93}, TopLeft: 124}
	pic, _ := picture.New(1, 1, h264.YUV420P)
	for mode := 0; mode < 9; mode++ {
		var pattern, chosen [16]uint16
		prediction.Intra4x4High(pattern[:], 4, mode, a, true, true, 8)
		putBlock(&pic.Planes[0], 0, 0, 4, pattern[:])
		got, cost := select4(&chosen, &pic.Planes[0], 0, 0, a, true, true, true)
		if got != mode || cost != 0 {
			t.Fatalf("I4 target %d selected %d cost %d", mode, got, cost)
		}
	}
	b := prediction.IntraBlockSamples16{Top: []uint16{40, 55, 100, 20, 95, 140, 120, 89, 19, 120, 145, 78, 91, 73, 111, 99}, Left: []uint16{140, 99, 60, 88, 145, 18, 65, 174, 35, 13, 18, 219, 175, 99, 160, 185}, TopLeft: 85}
	for mode := 0; mode < 4; mode++ {
		var pattern, chosen [256]uint16
		prediction.Intra16x16High(pattern[:], 16, mode, b, true, true, 8)
		putBlock(&pic.Planes[0], 0, 0, 16, pattern[:])
		got, cost := select16(&chosen, &pic.Planes[0], 0, 0, b, true, true, true)
		if got != mode || cost != 0 {
			t.Fatalf("I16 target %d selected %d cost %d", mode, got, cost)
		}
	}
	// 色度搜索通过实际宏块分析调用，两个分量使用同样边界以保证零 SAD 唯一。
	for mode := 0; mode < 4; mode++ {
		src, _ := picture.New(2, 2, h264.YUV420P)
		recon, _ := picture.New(2, 2, h264.YUV420P)
		s := &frameState{src: src, pic: recon, info: make([]mbInfo, 4)}
		s.info[3].qp = 0
		for c := 1; c < 3; c++ {
			p := &recon.Planes[c]
			for i := 0; i < 8; i++ {
				p.SetSample(8+i, 7, b.Top[i])
				p.SetSample(7, 8+i, b.Left[i])
			}
			p.SetSample(7, 7, b.TopLeft)
			var neighbors blockSamplesScratch
			a, t, l, _ := s.blockSamples(3, c, &neighbors)
			var target [64]uint16
			prediction.Chroma8x8High(target[:], 8, mode, a, t, l, 8)
			putBlock(&src.Planes[c], 8, 8, 8, target[:])
		}
		var r residual
		s.chroma(3, &r)
		if s.info[3].chromaMode != mode {
			t.Fatalf("chroma target %d selected %d", mode, s.info[3].chromaMode)
		}
	}
}

func TestEncoder_DeterminismAndReset(t *testing.T) {
	c := h264.DefaultEncoderConfig()
	c.Width = 34
	c.Height = 18
	e, err := New(c, WithSliceCount(4))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	f := testFrame(c.Width, c.Height)
	if err = e.SendFrame(f); err != nil {
		t.Fatal(err)
	}
	p, _ := e.ReceivePacket()
	original := bytes.Clone(p.Data)
	if e.last.src != nil {
		t.Fatal("source retained after SendFrame")
	}
	if err = e.SendFrame(f); err != nil {
		t.Fatal(err)
	}
	e.ReceivePacket()
	if !bytes.Equal(p.Data, original) {
		t.Fatal("packet overwritten by next SendFrame")
	}
	if err = e.Reset(); err != nil {
		t.Fatal(err)
	}
	if err = e.SendFrame(f); err != nil {
		t.Fatal(err)
	}
	p, _ = e.ReceivePacket()
	if !bytes.Equal(p.Data, original) {
		t.Fatal("reset did not restore deterministic IDR state")
	}
}

func TestEncoder_DecisionCoverage(t *testing.T) {
	c := h264.DefaultEncoderConfig()
	c.Width = 96
	c.Height = 64
	c.QP = 18
	c.GOPSize = 1
	e, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	var counts4 [9]int
	var counts16, chroma [4]int
	pcms := 0
	for frame := 0; frame < 60; frame++ {
		f := pattern.Frame(c.Width, c.Height, frame)
		if err = e.SendFrame(f); err != nil {
			t.Fatal(err)
		}
		e.ReceivePacket()
		for _, info := range e.last.info {
			if info.pcm {
				pcms++
				continue
			}
			chroma[info.chromaMode]++
			if info.is16 {
				counts16[info.mode16]++
			} else {
				for _, mode := range info.modes {
					counts4[mode]++
				}
			}
		}
	}
	t.Logf("I4 %v I16 %v chroma %v PCM %d", counts4, counts16, chroma, pcms)
	for mode, count := range counts4 {
		if count == 0 {
			t.Errorf("I4 mode %d was never selected", mode)
		}
	}
	for mode, count := range counts16 {
		if count == 0 {
			t.Errorf("I16 mode %d was never selected", mode)
		}
	}
	for mode, count := range chroma {
		if count == 0 {
			t.Errorf("chroma mode %d was never selected", mode)
		}
	}
}

func TestEncoder_ParameterRoundTripAndQPValidation(t *testing.T) {
	c := h264.DefaultEncoderConfig()
	c.Width = 34
	c.Height = 18
	c.FPSNum = 30000
	c.FPSDen = 1001
	e, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	rb, err := e.sps.MarshalRBSP()
	if err != nil {
		t.Fatal(err)
	}
	s, err := syntax.ParseSPS(rb)
	if err != nil || !reflect.DeepEqual(s, e.sps) {
		t.Fatalf("SPS %v", err)
	}
	rb, err = e.pps.MarshalRBSP(e.sps)
	if err != nil {
		t.Fatal(err)
	}
	p, err := syntax.ParsePPS(rb, s)
	if err != nil || !reflect.DeepEqual(p, e.pps) {
		t.Fatalf("PPS %v", err)
	}
	if _, err = New(c, WithMacroblockQP(nil)); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal(err)
	}
	e, err = New(c, WithMacroblockQP(func(x, y int) int { return 52 }))
	if err != nil {
		t.Fatal(err)
	}
	if err = e.SendFrame(testFrame(34, 18)); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal(err)
	}
	if e.idr != 0 || e.out != nil || e.recon != nil {
		t.Fatal("failed frame changed encoder state")
	}
}
