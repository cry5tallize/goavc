package encoder

import (
	"bytes"
	"errors"
	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/internal/motion"
	"github.com/cry5tallize/goavc/internal/picture"
	"github.com/cry5tallize/goavc/internal/testutil/pattern"
	"reflect"
	"testing"
)

func TestModeDecision_MatchesExhaustiveCandidateCosts(t *testing.T) {
	for _, qp := range []int{0, 26, 51} {
		c := h264.DefaultEncoderConfig()
		c.Width = 16
		c.Height = 16
		c.QP = qp
		e, err := New(c)
		if err != nil {
			t.Fatal(err)
		}
		if err = e.SendFrame(pattern.Frame(16, 16, 0)); err != nil {
			t.Fatal(err)
		}
		e.ReceivePacket()
		var reference [3][]byte
		for comp := range reference {
			reference[comp] = bytes.Clone(e.reference.Planes[comp].Data)
		}
		for kind := 0; kind < 4; kind++ {
			src, _ := picture.New(1, 1, h264.YUV420P)
			for comp := 0; comp < 3; comp++ {
				p := &src.Planes[comp]
				for y := 0; y < p.Height; y++ {
					for x := 0; x < p.Width; x++ {
						value := byte(128)
						switch kind {
						case 0:
							value = e.reference.Planes[comp].Pix(x, y)
						case 1:
							value = byte((x + y) % 2 * 255)
						case 2:
							value = byte(x*41 + y*19 + comp*53)
						}
						p.SetPix(x, y, value)
					}
				}
			}
			fresh := func() *frameState {
				s, err := e.newFrameState(src)
				if err != nil {
					t.Fatal(err)
				}
				s.reference = e.reference
				return s
			}
			selected := fresh()
			res, err := selected.analyzeRD(0, qp, 4, true)
			if err != nil {
				t.Fatal(err)
			}
			winner, err := selected.captureRD(0, qp, qp, res)
			if err != nil {
				t.Fatal(err)
			}
			check := func(s *frameState, res residual) {
				candidate, err := s.captureRD(0, qp, qp, res)
				if err != nil {
					if errors.Is(err, h264.ErrUnsupported) {
						return
					}
					t.Fatal(err)
				}
				if candidate.cost < winner.cost {
					t.Fatalf("qp%d pattern%d selected J=%d candidate J=%d info=%+v", qp, kind, winner.cost, candidate.cost, candidate.info)
				}
			}
			s := fresh()
			s.info[0].pcm = true
			s.mv.SetRect(0, 0, 4, 4, motion.MV{}, -1)
			check(s, residual{})
			s = fresh()
			mv := motion.PredictSkip(s.mv, 0, 0, 0)
			s.mc(0, partitions[0][0], mv)
			r := s.interResidual(0)
			if s.info[0].cbp == 0 {
				s.info[0].inter = true
				s.info[0].skip = true
				s.mv.SetRect(0, 0, 4, 4, mv, 0)
				check(s, r)
			}
			for partition := 0; partition < 4; partition++ {
				s = fresh()
				check(s, s.interCandidate(0, partition, 4, true))
			}
			for _, family := range []int{1, 2} {
				s = fresh()
				r = s.analyzeIntra(0, family)
				s.mv.SetRect(0, 0, 4, 4, motion.MV{}, -1)
				check(s, r)
			}
		}
		for comp := range reference {
			if !bytes.Equal(reference[comp], e.reference.Planes[comp].Data) {
				t.Fatal("RD changed reference picture")
			}
		}
		e.Close()
	}
}

func TestModeDecision_HighToolsMatchUnpruned(t *testing.T) {
	for _, qp := range []int{0, 18, 26, 51} {
		cfg := h264.DefaultEncoderConfig()
		cfg.Width, cfg.Height = 32, 32
		cfg.Profile = h264.ProfileHigh
		cfg.QP = qp
		var four [6][16]uint8
		for l := range four {
			for i := range four[l] {
				four[l][i] = uint8(8 + (l*7+i*3)%31)
			}
		}
		e, err := New(cfg, WithTransform8x8(), WithWeightedPrediction(), WithScalingMatrices4x4(four))
		if err != nil {
			t.Fatal(err)
		}
		f := pattern.Frame(32, 32, 1)
		if err = e.SendFrame(f); err != nil {
			t.Fatal(err)
		}
		f.Release()
		e.ReceivePacket()
		for _, kind := range []int{1, 2, 4, 5} {
			input := pattern.Frame(32, 32, kind)
			src, _ := picture.New(2, 2, h264.YUV420P)
			for comp := range src.Planes {
				for y := 0; y < src.Planes[comp].Height; y++ {
					copy(src.Planes[comp].Row(y), input.Planes[comp].Data[y*input.Planes[comp].Stride:])
				}
			}
			input.Release()
			fresh := func() *frameState {
				s, err := e.newFrameState(src)
				if err != nil {
					t.Fatal(err)
				}
				s.reference = e.reference
				return s
			}
			pruned, full := fresh(), fresh()
			for addr := 0; addr < 4; addr++ {
				resA, err := pruned.decideRD(addr, qp, 2, false, true)
				if err != nil {
					t.Fatal(err)
				}
				resB, err := full.decideRD(addr, qp, 2, false, false)
				if err != nil {
					t.Fatal(err)
				}
				a, err := pruned.captureRD(addr, qp, qp, resA)
				if err != nil {
					t.Fatal(err)
				}
				b, err := full.captureRD(addr, qp, qp, resB)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(a, b) {
					t.Fatalf("QP%d图案%d宏块%d剪枝与完整候选不一致：J%d/%d info%+v/%+v", qp, kind, addr, a.cost, b.cost, a.info, b.info)
				}
			}
		}
		e.Close()
	}
}

func TestModeDecision_SceneChangeChoosesIntra(t *testing.T) {
	c := h264.DefaultEncoderConfig()
	c.Width = 64
	c.Height = 64
	e, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err = e.SendFrame(pattern.Frame(64, 64, 0)); err != nil {
		t.Fatal(err)
	}
	e.ReceivePacket()
	f := testFrame(64, 64)
	for comp := range f.Planes {
		for i := range f.Planes[comp].Data {
			f.Planes[comp].Data[i] = 128
		}
	}
	if err = e.SendFrame(f); err != nil {
		t.Fatal(err)
	}
	packet, _ := e.ReceivePacket()
	if packet.KeyFrame {
		t.Fatal("scene macroblock test unexpectedly became IDR")
	}
	intra16 := 0
	for _, info := range e.last.info {
		if !info.inter && info.is16 {
			intra16++
		}
	}
	if intra16 == 0 {
		t.Fatal("scene change never chose I16 inside P picture")
	}
}

func TestModeDecision_FlatPruningMatchesExhaustive(t *testing.T) {
	for _, qp := range []int{18, 26, 34, 51} {
		c := h264.DefaultEncoderConfig()
		c.Width = 16
		c.Height = 16
		c.QP = qp
		e, err := New(c)
		if err != nil {
			t.Fatal(err)
		}
		ref, _ := picture.New(1, 1, h264.YUV420P)
		src, _ := picture.New(1, 1, h264.YUV420P)
		for comp := 0; comp < 3; comp++ {
			for y := 0; y < ref.Planes[comp].Height; y++ {
				for x := 0; x < ref.Planes[comp].Width; x++ {
					ref.Planes[comp].SetPix(x, y, 129)
					src.Planes[comp].SetPix(x, y, 128)
				}
			}
		}
		run := func(early bool) rdCandidate {
			s, err := e.newFrameState(src)
			if err != nil {
				t.Fatal(err)
			}
			s.reference = ref
			r, err := s.decideRD(0, qp, 4, true, early)
			if err != nil {
				t.Fatal(err)
			}
			result, err := s.captureRD(0, qp, qp, r)
			if err != nil {
				t.Fatal(err)
			}
			return result
		}
		if fast, slow := run(true), run(false); fast != slow {
			t.Fatalf("qp%d flat pruning changed winner J=%d/%d", qp, fast.cost, slow.cost)
		}
		e.Close()
	}
}

func TestUniformPrediction_IncludesInterpolationHalo(t *testing.T) {
	c := h264.DefaultEncoderConfig()
	c.Width = 32
	c.Height = 32
	e, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	ref, _ := picture.New(2, 2, h264.YUV420P)
	src, _ := picture.New(2, 2, h264.YUV420P)
	for comp := 0; comp < 3; comp++ {
		for y := 0; y < ref.Planes[comp].Height; y++ {
			for x := 0; x < ref.Planes[comp].Width; x++ {
				ref.Planes[comp].SetPix(x, y, 128)
				src.Planes[comp].SetPix(x, y, 128)
			}
		}
	}
	s, err := e.newFrameState(src)
	if err != nil {
		t.Fatal(err)
	}
	s.reference = ref
	s.mc(0, partitions[0][0], motion.MV{})
	if !s.uniformPrediction(0, 4) {
		t.Fatal("uniform rectangle rejected")
	}
	ref.Planes[0].SetPix(22, 0, 200)
	if s.uniformPrediction(0, 4) {
		t.Fatal("luma interpolation halo ignored")
	}
	ref.Planes[0].SetPix(22, 0, 128)
	ref.Planes[1].SetPix(11, 0, 200)
	if s.uniformPrediction(0, 4) {
		t.Fatal("chroma interpolation halo ignored")
	}
}
