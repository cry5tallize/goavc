package encoder

import (
	"errors"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/internal/motion"
	"github.com/goavc/h264/internal/picture"
)

func TestPredictionWeights_Validation(t *testing.T) {
	identity := PredictionWeights{LumaDenom: 7, ChromaDenom: 7, Weight: [3]int32{128, 128, 128}}
	if err := identity.validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*PredictionWeights){
		func(p *PredictionWeights) { p.LumaDenom = 8 },
		func(p *PredictionWeights) { p.Offset[0] = 128 },
		func(p *PredictionWeights) { p.Offset[1] = 1 },
		func(p *PredictionWeights) { p.Weight[2] = -129 },
	} {
		p := identity
		mutate(&p)
		if err := p.validate(); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("非法权重被接受：%+v %v", p, err)
		}
	}
	cfg := h264.DefaultEncoderConfig()
	cfg.Width, cfg.Height = 32, 32
	if _, err := New(cfg, WithWeightedPrediction()); !errors.Is(err, h264.ErrUnsupported) {
		t.Fatal("Baseline 必须拒绝显式权重", err)
	}
	cfg.Profile = h264.ProfileMain
	for _, options := range [][]Option{
		{WithWeightedPrediction(), WithPredictionWeights(identity)},
		{WithPredictionWeights(identity), WithWeightedPrediction()},
	} {
		if _, err := New(cfg, options...); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatal("冲突选项未拒绝", err)
		}
	}
}

func TestPredictionWeights_FitAnalyticGainOffset(t *testing.T) {
	ref, _ := picture.New(2, 2, h264.YUV420P)
	src, _ := picture.New(2, 2, h264.YUV420P)
	for comp := range ref.Planes {
		for y := 0; y < ref.Planes[comp].Height; y++ {
			for x := range ref.Planes[comp].Row(y) {
				v := byte(20 + (x+y*3)%80*2)
				ref.Planes[comp].Row(y)[x] = v
				src.Planes[comp].Row(y)[x] = v/2 + 11
			}
		}
	}
	p := estimatePredictionWeights(src, ref, 3)
	if p.Weight != [3]int32{32, 32, 32} || p.Offset != [3]int32{11, 11, 11} {
		t.Fatalf("拟合解析值错误：%+v", p)
	}
	p = estimatePredictionWeights(ref, ref, 3)
	if p.Weight != [3]int32{64, 64, 64} || p.Offset != [3]int32{} {
		t.Fatalf("恒等帧不应启用非恒等权重：%+v", p)
	}
}

func TestPredictionWeights_InterpolationBeforeClipping(t *testing.T) {
	ref, _ := picture.New(1, 1, h264.YUV420P)
	dst, _ := picture.New(1, 1, h264.YUV420P)
	for comp := range ref.Planes {
		for y := 0; y < ref.Planes[comp].Height; y++ {
			for x := range ref.Planes[comp].Row(y) {
				ref.Planes[comp].Row(y)[x] = byte((x % 2) * 255)
			}
		}
	}
	p := PredictionWeights{Weight: [3]int32{1, 1, 1}, Offset: [3]int32{-100, -100, -100}}
	s := frameState{pic: dst, reference: ref, weights: &p}
	s.mc(0, partitions[0][0], motion.MV{X: 2})
	// 色度 X=2 是 1/4 采样：0/255 插值为64，再减100裁剪到0。
	// 若先减100裁剪再插值，则得到39，与规范顺序不同。
	if got := dst.Planes[1].Pix(0, 0); got != 0 {
		t.Fatalf("插值必须先于加权裁剪：%d", got)
	}
}

func TestPredictionWeights_WeightedSkipAndFailedAU(t *testing.T) {
	cfg := h264.DefaultEncoderConfig()
	cfg.Width, cfg.Height, cfg.Profile = 32, 32, h264.ProfileMain
	weights := PredictionWeights{Weight: [3]int32{1, 1, 1}, Offset: [3]int32{7, -9, 11}}
	e, err := New(cfg, WithPredictionWeights(weights))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	f := testFrame(32, 32)
	for comp := range f.Planes {
		for i := range f.Planes[comp].Data {
			f.Planes[comp].Data[i] = 100
		}
	}
	if err = e.SendFrame(f); err != nil {
		t.Fatal(err)
	}
	if _, err = e.ReceivePacket(); err != nil {
		t.Fatal(err)
	}
	r, err := e.Reconstruction()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Release()
	r.KeyFrame = false
	for comp := range r.Planes {
		for i, value := range r.Planes[comp].Data {
			r.Planes[comp].Data[i] = byte(int(value) + int(weights.Offset[comp]))
		}
	}
	if err = e.SendFrame(r); err != nil {
		t.Fatal(err)
	}
	for addr, info := range e.last.info {
		if !info.skip {
			t.Fatalf("宏块%d 未选择实际加权 P_Skip：%+v", addr, info)
		}
	}
	if _, err = e.ReceivePacket(); err != nil {
		t.Fatal(err)
	}
	ref, frameNum, stats, last := e.reference, e.frameNum, e.RateControlStats(), e.last
	budget := e.maxAUBytes
	e.maxAUBytes = 1
	if err = e.SendFrame(r); !errors.Is(err, h264.ErrResourceLimit) {
		t.Fatal("必须拒绝超限 AU", err)
	}
	if e.reference != ref || e.frameNum != frameNum || e.last != last || e.out != nil || e.RateControlStats() != stats {
		t.Fatal("加权候选失败污染已提交状态")
	}
	e.maxAUBytes = budget
	if err = e.SendFrame(r); err != nil {
		t.Fatal("失败后重试合法帧", err)
	}
}
