package decoder

import (
	"github.com/cry5tallize/goavc/internal/motion"
	"github.com/cry5tallize/goavc/internal/prediction"
)

func (s *sliceEntry) applyPWeights(fs *frameState, mbx, mby, x, y, w, h, ref int) {
	if !s.pps.WeightedPredFlag || s.hdr.PredictionWeights == nil {
		return
	}
	t := s.hdr.PredictionWeights
	if fs.field {
		ref /= 2
	}
	weight := t.Lists[0][ref]
	first, end := 0, 3
	if fs.sps.SeparateColourPlaneFlag {
		first, end = fs.component, fs.component+1
	}
	for c := first; c < end; c++ {
		wi := c
		if fs.sps.SeparateColourPlaneFlag {
			wi = 0
		}
		px, py, pw, ph := (mbx*16 + x), (mby*16 + y), w, h
		denom := t.LumaDenom
		if c > 0 && !fs.sps.SeparateColourPlaneFlag {
			px /= fs.subW
			py /= fs.subH
			pw /= fs.subW
			ph /= fs.subH
			denom = t.ChromaDenom
		}
		pl := &fs.pic.Planes[c]
		if pl.BitDepth == 10 {
			for yy := range ph {
				row := pl.Row16(py + yy)[px : px+pw]
				for i, v := range row {
					row[i] = prediction.WeightSampleHigh(v, weight.Weight[wi], weight.Offset[wi], denom, pl.BitDepth)
				}
			}
		} else {
			for yy := range ph {
				row := pl.Row(py + yy)[px : px+pw]
				for i, v := range row {
					row[i] = prediction.WeightSample(v, weight.Weight[wi], weight.Offset[wi], denom)
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
	first, end := 0, 3
	if fs.sps.SeparateColourPlaneFlag {
		first, end = fs.component, fs.component+1
	}
	for c := first; c < end; c++ {
		x, y, w, h := mbx*16+p.x*4, mby*16+p.y*4, p.w*4, p.h*4
		if c > 0 && !fs.sps.SeparateColourPlaneFlag {
			x /= fs.subW
			y /= fs.subH
			w /= fs.subW
			h /= fs.subH
		}
		var samples [2][256]T
		for l := 0; l < 2; l++ {
			if refs[l] < 0 {
				continue
			}
			r := s.refs[l][refs[l]]
			mv := mvs[l]
			chromaY := int(mv.Y)
			if c > 0 {
				chromaY += fs.chromaFieldOffset(r)
			}
			switch dst := any(samples[l][:]).(type) {
			case []byte:
				if c == 0 || fs.sps.ChromaFormatIDC == 3 {
					prediction.MCBlockLuma(dst, w, &r.Picture.Planes[c], x, y, w, h, int(mv.X), int(mv.Y))
				} else {
					prediction.MCBlockChromaFormat(dst, w, &r.Picture.Planes[c], x, y, w, h, int(mv.X), chromaY, fs.subW, fs.subH)
				}
			case []uint16:
				if c == 0 || fs.sps.ChromaFormatIDC == 3 {
					prediction.MCBlockLumaHigh(dst, w, &r.Picture.Planes[c], x, y, w, h, int(mv.X), int(mv.Y))
				} else {
					prediction.MCBlockChromaFormatHigh(dst, w, &r.Picture.Planes[c], x, y, w, h, int(mv.X), chromaY, fs.subW, fs.subH)
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
			if c > 0 && !fs.sps.SeparateColourPlaneFlag {
				denom = t.ChromaDenom
			}
			for l := 0; l < 2; l++ {
				if refs[l] >= 0 {
					index := int(refs[l])
					if fs.field {
						index /= 2
					}
					v := t.Lists[l][index]
					wi := c
					if fs.sps.SeparateColourPlaneFlag {
						wi = 0
					}
					weights[l], offsets[l] = v.Weight[wi], v.Offset[wi]
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
