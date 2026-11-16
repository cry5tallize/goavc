package decoder

import (
	"github.com/cry5tallize/goavc/internal/dpb"
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
		// Spec: 8.4.3.2。逐分量判定，独立平面使用亮度denom；
		// 恒等亮度不代表色度也恒等，offset仍为码流8-bit标度。
		if int64(weight.Weight[wi]) == int64(1)<<denom && weight.Offset[wi] == 0 {
			continue
		}
		pl := &fs.pic.Planes[c]
		if pl.BitDepth == 10 {
			block := pl.Data16[pl.Index(px, py):]
			prediction.WeightBlock16(block, pl.Stride, block, pl.Stride, pw, ph, weight.Weight[wi], weight.Offset[wi], denom, pl.BitDepth)
		} else {
			block := pl.Data[pl.Index(px, py):]
			prediction.WeightBlock8(block, pl.Stride, block, pl.Stride, pw, ph, weight.Weight[wi], weight.Offset[wi], denom)
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
	explicit := s.pps.WeightedBipredIDC == 1 && s.hdr.PredictionWeights != nil
	both := refs[0] >= 0 && refs[1] >= 0
	list := 0
	if refs[0] < 0 {
		list = 1
	}
	implicit := [2]int32{1, 1}
	if s.pps.WeightedBipredIDC == 2 && both {
		a, b := s.refs[0][refs[0]], s.refs[1][refs[1]]
		implicit[0], implicit[1] = prediction.ImplicitWeights(fs.poc, a.POC, b.POC, a.LongTerm, b.LongTerm)
	}
	for c := first; c < end; c++ {
		x, y, w, h := mbx*16+p.x*4, mby*16+p.y*4, p.w*4, p.h*4
		if c > 0 && !fs.sps.SeparateColourPlaneFlag {
			x /= fs.subW
			y /= fs.subH
			w /= fs.subW
			h /= fs.subH
		}
		pl := &fs.pic.Planes[c]
		if !both && !explicit {
			// 单参考无权重时直接写最终平面，不清零/填充中转预测块。
			if pl.BitDepth == 10 {
				mcBPlane(fs, pl.Data16[pl.Index(x, y):], pl.Stride, s.refs[list][refs[list]], c, x, y, w, h, mvs[list])
			} else {
				mcBPlane(fs, pl.Data[pl.Index(x, y):], pl.Stride, s.refs[list][refs[list]], c, x, y, w, h, mvs[list])
			}
			continue
		}
		var samples [2][256]T
		for l := range 2 {
			if refs[l] >= 0 {
				mcBPlane(fs, samples[l][:], w, s.refs[l][refs[l]], c, x, y, w, h, mvs[l])
			}
		}
		denom := uint32(0)
		weights, offsets := [2]int32{1, 1}, [2]int32{}
		if explicit {
			t := s.hdr.PredictionWeights
			denom = t.LumaDenom
			if c > 0 && !fs.sps.SeparateColourPlaneFlag {
				denom = t.ChromaDenom
			}
			for l := range 2 {
				if refs[l] < 0 {
					continue
				}
				index := int(refs[l])
				if fs.field {
					index /= 2
				}
				wi := c
				if fs.sps.SeparateColourPlaneFlag {
					wi = 0
				}
				weights[l], offsets[l] = t.Lists[l][index].Weight[wi], t.Lists[l][index].Offset[wi]
			}
		} else if s.pps.WeightedBipredIDC == 2 && both {
			weights, denom = implicit, 5
		}
		switch values := any(&samples).(type) {
		case *[2][256]byte:
			if both {
				prediction.BiWeightBlock8(pl.Data[pl.Index(x, y):], pl.Stride, values[0][:], values[1][:], w, w, h, weights, offsets, denom)
			} else {
				prediction.WeightBlock8(pl.Data[pl.Index(x, y):], pl.Stride, values[list][:], w, w, h, weights[list], offsets[list], denom)
			}
		case *[2][256]uint16:
			if both {
				prediction.BiWeightBlock16(pl.Data16[pl.Index(x, y):], pl.Stride, values[0][:], values[1][:], w, w, h, weights, offsets, denom, pl.BitDepth)
			} else {
				prediction.WeightBlock16(pl.Data16[pl.Index(x, y):], pl.Stride, values[list][:], w, w, h, weights[list], offsets[list], denom, pl.BitDepth)
			}
		}
	}
}

// mcBPlane 只分派存储和采样格式，所有插值仍调用唯一共享MC内核。
func mcBPlane[T ~uint8 | ~uint16](fs *frameState, dst []T, stride int, r *dpb.Reference, c, x, y, w, h int, mv motion.MV) {
	chromaY := int(mv.Y)
	if c > 0 {
		chromaY += fs.chromaFieldOffset(r)
	}
	switch d := any(dst).(type) {
	case []byte:
		if c == 0 || fs.sps.ChromaFormatIDC == 3 {
			prediction.MCBlockLuma(d, stride, &r.Picture.Planes[c], x, y, w, h, int(mv.X), int(mv.Y))
		} else {
			prediction.MCBlockChromaFormat(d, stride, &r.Picture.Planes[c], x, y, w, h, int(mv.X), chromaY, fs.subW, fs.subH)
		}
	case []uint16:
		if c == 0 || fs.sps.ChromaFormatIDC == 3 {
			prediction.MCBlockLumaHigh(d, stride, &r.Picture.Planes[c], x, y, w, h, int(mv.X), int(mv.Y))
		} else {
			prediction.MCBlockChromaFormatHigh(d, stride, &r.Picture.Planes[c], x, y, w, h, int(mv.X), chromaY, fs.subW, fs.subH)
		}
	}
}
