package encoder

import (
	"encoding/binary"
	"errors"
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/internal/picture"
	"github.com/cry5tallize/goavc/internal/prediction"
)

func TestHigh10ModeSearch_PreservesAllIntraModes(t *testing.T) {
	pic, err := picture.New(1, 1, h264.YUV420P10LE)
	if err != nil {
		t.Fatal(err)
	}
	a := prediction.Intra4x4Samples16{Top: [4]uint16{165, 773, 309, 45}, TopRight: [4]uint16{889, 261, 473, 37}, Left: [4]uint16{925, 129, 645, 373}, TopLeft: 497}
	for mode := range 9 {
		var target, chosen [16]uint16
		if err := prediction.Intra4x4High(target[:], 4, mode, a, true, true, 10); err != nil {
			t.Fatal(err)
		}
		putBlock(&pic.Planes[0], 0, 0, 4, target[:])
		if got, cost := select4(&chosen, &pic.Planes[0], 0, 0, a, true, true, true); got != mode || cost != 0 {
			t.Fatalf("High10 I4模式%d被选为%d，代价%d", mode, got, cost)
		}
	}
	b := prediction.IntraBlockSamples16{Top: []uint16{161, 221, 401, 81, 381, 561, 481, 357, 77, 481, 581, 313, 365, 293, 445, 397}, Left: []uint16{561, 397, 241, 353, 581, 73, 261, 697, 141, 53, 73, 877, 701, 397, 641, 741}, TopLeft: 341}
	for mode := range 4 {
		var target, chosen [256]uint16
		if err := prediction.Intra16x16High(target[:], 16, mode, b, true, true, 10); err != nil {
			t.Fatal(err)
		}
		putBlock(&pic.Planes[0], 0, 0, 16, target[:])
		if got, cost := select16(&chosen, &pic.Planes[0], 0, 0, b, true, true, true); got != mode || cost != 0 {
			t.Fatalf("High10 I16模式%d被选为%d，代价%d", mode, got, cost)
		}
	}
}

func TestHigh10QP_DomainAndEntropyOptions(t *testing.T) {
	base := h264.DefaultEncoderConfig()
	base.Width, base.Height = 16, 16
	if _, err := New(base, WithCABAC(true)); !errors.Is(err, h264.ErrUnsupported) {
		t.Fatalf("Baseline必须拒绝CABAC：%v", err)
	}
	base.Profile, base.PixelFormat = h264.ProfileHigh10, h264.YUV420P10LE
	for _, enabled := range []bool{false, true} {
		for _, qp := range []int{-12, -1, 0, 51} {
			base.QP = qp
			e, err := New(base, WithCABAC(enabled))
			if err != nil {
				t.Fatal(err)
			}
			if e.pps.EntropyCodingModeFlag != enabled || e.sps.ProfileIDC != 110 || e.sps.BitDepthLumaMinus8 != 2 {
				t.Fatal("公开熵选项或High10参数集未生效")
			}
			pic, err := picture.New(1, 1, h264.YUV420P10LE)
			if err != nil {
				t.Fatal(err)
			}
			s, err := e.newFrameState(pic)
			if err != nil {
				t.Fatal(err)
			}
			if s.lumaQP(qp) != qp+12 || s.qpDelta(51, -12) != -1 || s.qpDelta(-12, 51) != 1 {
				t.Fatal("语法QP与QP'或模64差分混淆")
			}
			e.Close()
		}
	}
	base.QP = -12
	e, err := New(base, WithMacroblockQP(func(int, int) int { return -13 }))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	f := &h264.Frame{Width: 16, Height: 16, Format: h264.YUV420P10LE}
	for c := range f.Planes {
		w, h := f.Format.PlaneDimensions(c, 16, 16)
		f.Planes[c] = h264.Plane{Stride: w * 2, Data: make([]byte, w*h*2)}
		for i := 0; i < len(f.Planes[c].Data); i += 2 {
			binary.LittleEndian.PutUint16(f.Planes[c].Data[i:], 513)
		}
	}
	defer f.Release()
	if err = e.SendFrame(f); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("越界宏块QP错误分类：%v", err)
	}
	if _, err = e.ReceivePacket(); !errors.Is(err, h264.ErrAgain) {
		t.Fatal("QP校验失败仍发布输出")
	}
}

func TestChroma444Residual_ChromaOnlyCBP(t *testing.T) {
	for _, format := range []h264.PixelFormat{h264.YUV444P, h264.YUV444P10LE} {
		for _, eight := range []bool{false, true} {
			cfg := h264.DefaultEncoderConfig()
			cfg.Width, cfg.Height, cfg.Profile, cfg.PixelFormat, cfg.QP = 16, 16, h264.ProfileHigh444, format, 0
			e, err := New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			src, err := picture.New(1, 1, format)
			if err != nil {
				t.Fatal(err)
			}
			s, err := e.newFrameState(src)
			if err != nil {
				t.Fatal(err)
			}
			mid := uint16(1 << (format.BitDepth() - 1))
			for c := range 3 {
				for y := range 16 {
					for x := range 16 {
						src.Planes[c].SetSample(x, y, mid)
						s.pic.Planes[c].SetSample(x, y, mid)
					}
				}
			}
			src.Planes[1].SetSample(2, 3, mid+63)
			src.Planes[2].SetSample(13, 14, mid-63)
			s.info[0].transform8 = eight
			res := s.interResidual(0)
			if s.info[0].cbp == 0 || s.info[0].cbp & ^15 != 0 {
				t.Fatalf("%s/8x8%t：仅色度残差未并入共享CBP：%d", format, eight, s.info[0].cbp)
			}
			for block := range 4 {
				if nonzero(res.luma8[block][:]) {
					t.Fatal("色度分析污染亮度8x8系数")
				}
			}
			for block := range 16 {
				if nonzero(res.luma[block][:]) {
					t.Fatal("色度分析污染亮度4x4系数")
				}
			}
			s.reconstructInter(0, &res)
			if s.pic.Planes[1].Sample(2, 3) == mid || s.pic.Planes[2].Sample(13, 14) == mid {
				t.Fatal("共享重建内核未应用444色度残差")
			}
			e.Close()
		}
	}
}

func TestSeparateColourPlanes_AccessUnitBudget(t *testing.T) {
	for _, cabac := range []bool{false, true} {
		cfg := h264.DefaultEncoderConfig()
		cfg.Width, cfg.Height, cfg.Profile, cfg.PixelFormat = 16, 16, h264.ProfileHigh444, h264.YUV444P10LE
		e, err := New(cfg, WithSeparateColourPlanes(), WithCABAC(cabac))
		if err != nil {
			t.Fatal(err)
		}
		src, err := picture.New(1, 1, cfg.PixelFormat)
		if err != nil {
			t.Fatal(err)
		}
		budget := e.maxAUBytes
		e.maxAUBytes = 1
		if _, _, err = e.encodeAtQP(src, true, true, cfg.QP); !errors.Is(err, h264.ErrResourceLimit) {
			t.Fatalf("独立平面超AU预算未拒绝：%v", err)
		}
		if e.reference != nil || e.out != nil || e.frameNum != 0 {
			t.Fatal("失败的平面集合提交了图像状态")
		}
		e.maxAUBytes = budget
		data, state, err := e.encodeAtQP(src, true, true, cfg.QP)
		if err != nil || len(data) == 0 || state == nil || state.pic == nil {
			t.Fatalf("AU预算拒绝后无法完整重试：%v", err)
		}
		e.Close()
	}
}

func TestChroma444QP_JointAndIndependent(t *testing.T) {
	for _, format := range []h264.PixelFormat{h264.YUV444P, h264.YUV444P10LE} {
		for _, independent := range []bool{false, true} {
			cfg := h264.DefaultEncoderConfig()
			cfg.Width, cfg.Height, cfg.Profile, cfg.PixelFormat = 16, 16, h264.ProfileHigh444, format
			var options []Option
			if independent {
				options = append(options, WithSeparateColourPlanes())
			}
			e, err := New(cfg, options...)
			if err != nil {
				t.Fatal(err)
			}
			src, err := picture.New(1, 1, format)
			if err != nil {
				t.Fatal(err)
			}
			s, err := e.newFrameState(src)
			if err != nil {
				t.Fatal(err)
			}
			offset := 6 * (format.BitDepth() - 8)
			if got := s.lumaQP(51); got != 51+offset {
				t.Fatalf("亮度QP'=%d", got)
			}
			want := 39 + offset
			if independent {
				want = 51 + offset
			}
			if got := s.chromaQP(51); got != want {
				t.Fatalf("独立%t：色度QP'=%d，期望%d", independent, got, want)
			}
			// 共同444也用亮度内核处理临时色度视图，不能因此丢失QPC映射。
			s.plane, s.separate = 1, true
			if got := s.lumaQP(51); got != want {
				t.Fatalf("独立%t：色度视图QP'=%d，期望%d", independent, got, want)
			}
			e.Close()
		}
	}
}

func TestChroma444CABAC_EightCBFNeighborAvailability(t *testing.T) {
	cfg := h264.DefaultEncoderConfig()
	cfg.Width, cfg.Height, cfg.Profile, cfg.PixelFormat = 32, 16, h264.ProfileHigh444, h264.YUV444P
	e, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	src, err := picture.New(2, 1, cfg.PixelFormat)
	if err != nil {
		t.Fatal(err)
	}
	s, err := e.newFrameState(src)
	if err != nil {
		t.Fatal(err)
	}
	s.info[1].transform8 = true
	s.nz[3], s.cnz[0][3], s.cnz[1][3] = 1, 1, 1
	for comp := -1; comp < 2; comp++ {
		a, b := s.cbf(1, 0, 0, comp, false, true)
		if a || !b {
			t.Fatalf("分量%d：4x4邻块被误作8x8或不可用帧内默认值丢失：%t/%t", comp, a, b)
		}
		a, _ = s.cbf(1, 0, 0, comp, false, false)
		if !a {
			t.Fatalf("分量%d：4x4自身的CBF不能受8x8规则影响", comp)
		}
	}
	s.info[0].transform8 = true
	for comp := -1; comp < 2; comp++ {
		a, _ := s.cbf(1, 0, 0, comp, false, true)
		if !a {
			t.Fatalf("分量%d：可用8x8邻块非零丢失", comp)
		}
	}
	// bS仅按共同亮度NZ推导，不能把共同444的色度系数并入。
	s.info[0].inter, s.info[1].inter = true, true
	clear(s.nz)
	if got := s.strength(3, 0, 4, 0, true); got != 0 {
		t.Fatalf("仅色度非零却得到bS=%d", got)
	}
	s.nz[3] = 1
	if got := s.strength(3, 0, 4, 0, true); got != 2 {
		t.Fatalf("亮度非零未得到bS=2：%d", got)
	}
}
