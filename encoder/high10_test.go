package encoder

import (
	"encoding/binary"
	"errors"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/internal/picture"
	"github.com/goavc/h264/internal/prediction"
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
