package decoder

import (
	"github.com/goavc/h264/internal/motion"
	"github.com/goavc/h264/internal/prediction"
)

func (s *sliceEntry) applyPWeights(fs *frameState, mbx, mby, x, y, w, h, ref int) {
	if !s.pps.WeightedPredFlag || s.hdr.PredictionWeights == nil {
		return
	}
	t := s.hdr.PredictionWeights
	weight := t.Lists[0][ref]
	for c := 0; c < 3; c++ {
		px, py, pw, ph := (mbx*16 + x), (mby*16 + y), w, h
		denom := t.LumaDenom
		if c > 0 {
			px /= 2
			py /= 2
			pw /= 2
			ph /= 2
			denom = t.ChromaDenom
		}
		pl := &fs.pic.Planes[c]
		if pl.BitDepth == 10 {
			for yy := range ph {
				row := pl.Row16(py + yy)[px : px+pw]
				for i, v := range row {
					row[i] = prediction.WeightSampleHigh(v, weight.Weight[c], weight.Offset[c], denom, pl.BitDepth)
				}
			}
		} else {
			for yy := range ph {
				row := pl.Row(py + yy)[px : px+pw]
				for i, v := range row {
					row[i] = prediction.WeightSample(v, weight.Weight[c], weight.Offset[c], denom)
				}
			}
		}
	}
}

// mcBPartition 将各方向运动补偿结果独立插值，再做均值或显式/隐式加权。
// 工作数组固定在栈上；不创建第二套插值、反变换或图像重建内核。
func (fs *frameState) mcBPartition(s *sliceEntry, mbx, mby int, p pPartition, refs [2]int8, mvs [2]motion.MV) {
	if fs.pic.Planes[0].BitDepth == 10 {
		mcBPartitionSamples[uint16](fs, s, mbx, mby, p, refs, mvs)
	} else {
		mcBPartitionSamples[byte](fs, s, mbx, mby, p, refs, mvs)
	}
}

func mcBPartitionSamples[T ~uint8 | ~uint16](fs *frameState, s *sliceEntry, mbx, mby int, p pPartition, refs [2]int8, mvs [2]motion.MV) {
	for c := 0; c < 3; c++ {
		x, y, w, h := mbx*16+p.x*4, mby*16+p.y*4, p.w*4, p.h*4
		if c > 0 {
			x /= 2
			y /= 2
			w /= 2
			h /= 2
		}
		var samples [2][256]T
		for l := 0; l < 2; l++ {
			if refs[l] < 0 {
				continue
			}
			r := s.refs[l][refs[l]]
			mv := mvs[l]
			switch dst := any(samples[l][:]).(type) {
			case []byte:
				if c == 0 {
					prediction.MCBlockLuma(dst, w, &r.Picture.Planes[c], x, y, w, h, int(mv.X), int(mv.Y))
				} else {
					prediction.MCBlockChroma(dst, w, &r.Picture.Planes[c], x, y, w, h, int(mv.X), int(mv.Y))
				}
			case []uint16:
				if c == 0 {
					prediction.MCBlockLumaHigh(dst, w, &r.Picture.Planes[c], x, y, w, h, int(mv.X), int(mv.Y))
				} else {
					prediction.MCBlockChromaHigh(dst, w, &r.Picture.Planes[c], x, y, w, h, int(mv.X), int(mv.Y))
				}
			}
		}
		denom := uint32(0)
		weights := [2]int32{1, 1}
		offsets := [2]int32{}
		explicit := s.pps.WeightedBipredIDC == 1 && s.hdr.PredictionWeights != nil
		if explicit {
			t := s.hdr.PredictionWeights
			denom = t.LumaDenom
			if c > 0 {
				denom = t.ChromaDenom
			}
			for l := 0; l < 2; l++ {
				if refs[l] >= 0 {
					v := t.Lists[l][refs[l]]
					weights[l], offsets[l] = v.Weight[c], v.Offset[c]
				}
			}
		}
		if s.pps.WeightedBipredIDC == 2 && refs[0] >= 0 && refs[1] >= 0 {
			a, b := s.refs[0][refs[0]], s.refs[1][refs[1]]
			weights[0], weights[1] = prediction.ImplicitWeights(fs.poc, a.POC, b.POC, a.LongTerm, b.LongTerm)
			denom = 5
		}
		pl := &fs.pic.Planes[c]
		l := 0
		if refs[0] < 0 {
			l = 1
		}
		both := refs[0] >= 0 && refs[1] >= 0
		switch values := any(&samples).(type) {
		case *[2][256]byte:
			for yy := range h {
				row := pl.Row(y + yy)[x : x+w]
				for xx := range w {
					i := yy*w + xx
					if both {
						row[xx] = prediction.BiWeightSample(values[0][i], values[1][i], weights[0], weights[1], offsets[0], offsets[1], denom)
					} else if explicit {
						row[xx] = prediction.WeightSample(values[l][i], weights[l], offsets[l], denom)
					} else {
						row[xx] = values[l][i]
					}
				}
			}
		case *[2][256]uint16:
			for yy := range h {
				row := pl.Row16(y + yy)[x : x+w]
				for xx := range w {
					i := yy*w + xx
					if both {
						row[xx] = prediction.BiWeightSampleHigh(values[0][i], values[1][i], weights[0], weights[1], offsets[0], offsets[1], denom, pl.BitDepth)
					} else if explicit {
						row[xx] = prediction.WeightSampleHigh(values[l][i], weights[l], offsets[l], denom, pl.BitDepth)
					} else {
						row[xx] = values[l][i]
					}
				}
			}
		}
	}
}
