package encoder

import (
	"fmt"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/internal/picture"
	"github.com/goavc/h264/internal/prediction"
)

// PredictionWeights 描述单参考 P slice 的显式权重，分量顺序为 Y、Cb、Cr。
// Spec: 7.4.3.2，分母指数 0..7；显式 weight/offset 为 -128..127。
// denom=7 的默认 weight=128 只能使用隐含值，不能与同组非默认色度权重混写。
type PredictionWeights struct {
	LumaDenom, ChromaDenom uint32
	Weight, Offset         [3]int32
}

// WithWeightedPrediction 自动按当前帧与最近重建参考的线性关系分析权重。
// 默认关闭；仅支持 Main/High P slice，不与固定权重选项共用。
func WithWeightedPrediction() Option {
	return func(e *Encoder) error {
		if e.fixedWeights != nil {
			return fmt.Errorf("encoder: automatic and fixed prediction weights conflict: %w", h264.ErrInvalidData)
		}
		e.autoWeights = true
		return nil
	}
}

// WithPredictionWeights 为所有 P slice 固定同一组显式权重；参数按值复制。
// 适用于调用方已知的淡入淡出曲线与确定性的互操作测试。
func WithPredictionWeights(weights PredictionWeights) Option {
	return func(e *Encoder) error {
		if e.autoWeights {
			return fmt.Errorf("encoder: automatic and fixed prediction weights conflict: %w", h264.ErrInvalidData)
		}
		if err := weights.validate(); err != nil {
			return err
		}
		copy := weights
		e.fixedWeights = &copy
		return nil
	}
}

func (p PredictionWeights) denom(comp int) uint32 {
	if comp == 0 {
		return p.LumaDenom
	}
	return p.ChromaDenom
}

func (p PredictionWeights) defaultComponent(comp int) bool {
	return p.Weight[comp] == 1<<p.denom(comp) && p.Offset[comp] == 0
}

func (p PredictionWeights) validate() error {
	if p.LumaDenom > 7 || p.ChromaDenom > 7 {
		return fmt.Errorf("encoder: prediction weight denominator exceeds 7: %w", h264.ErrInvalidData)
	}
	for comp := range p.Weight {
		implicit := p.defaultComponent(comp)
		if comp > 0 {
			implicit = p.defaultComponent(1) && p.defaultComponent(2)
		}
		if p.Offset[comp] < -128 || p.Offset[comp] > 127 || (!implicit && (p.Weight[comp] < -128 || p.Weight[comp] > 127)) {
			return fmt.Errorf("encoder: prediction weight component %d is not representable: %w", comp, h264.ErrInvalidData)
		}
	}
	return nil
}

func (p *PredictionWeights) writeTable(w *bitstream.Writer) {
	// 即使本帧全为帧内宏块，P slice 的权重表仍不能省略。
	w.WriteUE(p.LumaDenom)
	w.WriteUE(p.ChromaDenom)
	luma := !p.defaultComponent(0)
	w.WriteBit(uint8(flag(luma)))
	if luma {
		w.WriteSE(p.Weight[0])
		w.WriteSE(p.Offset[0])
	}
	chroma := !p.defaultComponent(1) || !p.defaultComponent(2)
	w.WriteBit(uint8(flag(chroma)))
	if chroma {
		for comp := 1; comp < 3; comp++ {
			w.WriteSE(p.Weight[comp])
			w.WriteSE(p.Offset[comp])
		}
	}
}

func (s *frameState) applyPredictionWeights(x, y, width, height int) {
	if s.weights == nil {
		return
	}
	for comp := 0; comp < 3; comp++ {
		px, py, w, h := x, y, width, height
		if comp > 0 {
			px, py, w, h = x/2, y/2, width/2, height/2
		}
		for row := 0; row < h; row++ {
			pl := &s.pic.Planes[comp]
			for i := range w {
				v := prediction.WeightSampleHigh(pl.Sample(px+i, py+row), s.weights.Weight[comp], s.weights.Offset[comp], s.weights.denom(comp), pl.BitDepth)
				pl.SetSample(px+i, py+row, v)
			}
		}
	}
}

func weightedSearchReference(reference *picture.Picture, weights *PredictionWeights) (*picture.Picture, error) {
	out, err := picture.New(reference.MBWidth, reference.MBHeight, reference.Format)
	if err != nil {
		return nil, err
	}
	for comp := range out.Planes {
		for y := 0; y < out.Planes[comp].Height; y++ {
			for x := range out.Planes[comp].Width {
				value := reference.Planes[comp].Sample(x, y)
				out.Planes[comp].SetSample(x, y, prediction.WeightSampleHigh(value, weights.Weight[comp], weights.Offset[comp], weights.denom(comp), out.Planes[comp].BitDepth))
			}
		}
	}
	return out, nil
}

// estimatePredictionWeights 用整数最小二乘拟合 gain/offset，再以实际裁剪与
// 舍入后的 SSE 检验收益。每平面独立；平坦参考只拟合 offset，避免方差为零。
// 拟合网格每平面最多 128×128 点，covariance*64 也不越过 int64；
// 验收 SSE 仍检查完整平面，不因采样漏掉裁剪带来的劣化。
func estimatePredictionWeights(src, reference *picture.Picture) PredictionWeights {
	p := PredictionWeights{LumaDenom: 6, ChromaDenom: 6, Weight: [3]int32{64, 64, 64}}
	for comp := range src.Planes {
		var n, sumX, sumY, sumXX, sumXY int64
		stepX, stepY := (src.Planes[comp].Width+127)/128, (src.Planes[comp].Height+127)/128
		for y := 0; y < src.Planes[comp].Height; y += stepY {
			for x := 0; x < src.Planes[comp].Width; x += stepX {
				value := src.Planes[comp].Sample(x, y)
				a, b := int64(reference.Planes[comp].Sample(x, y)), int64(value)
				n++
				sumX += a
				sumY += b
				sumXX += a * a
				sumXY += a * b
			}
		}
		weight := int64(64)
		variance := n*sumXX - sumX*sumX
		if variance > 0 {
			covariance := n*sumXY - sumX*sumY
			weight = max(int64(-128), min(int64(127), covariance*64/variance))
		}
		offset := max(int64(-128), min(int64(127), (sumY-(sumX*weight+32)/64)/(n<<(src.Planes[comp].BitDepth-8))))
		var identitySSE, weightedSSE int64
		for y := 0; y < src.Planes[comp].Height; y++ {
			for x := range src.Planes[comp].Width {
				value := src.Planes[comp].Sample(x, y)
				r := reference.Planes[comp].Sample(x, y)
				a := int64(value) - int64(r)
				b := int64(value) - int64(prediction.WeightSampleHigh(r, int32(weight), int32(offset), 6, src.Planes[comp].BitDepth))
				identitySSE += a * a
				weightedSSE += b * b
			}
		}
		if weightedSSE < identitySSE {
			p.Weight[comp], p.Offset[comp] = int32(weight), int32(offset)
		}
	}
	return p
}
