package decoder

import "github.com/goavc/h264/internal/prediction"

// 预测入口只分派采样存储；邻域规则和预测算术均各自只有一份实现。
func (fs *frameState) predictIntra4(mbx, mby, bx, by, x, y, mode int) error {
	pl := &fs.pic.Planes[0]
	if pl.BitDepth == 10 {
		s, top, left := intra4Samples(fs, mbx, mby, bx, by, pl.Data16)
		return prediction.Intra4x4High(pl.Data16[pl.Index(x, y):], pl.Stride, mode, prediction.Intra4x4Samples16(s), top, left, pl.BitDepth)
	}
	s, top, left := intra4Samples(fs, mbx, mby, bx, by, pl.Data)
	return prediction.Intra4x4(pl.Data[pl.Index(x, y):], pl.Stride, mode, prediction.Intra4x4Samples(s), top, left)
}

func (fs *frameState) predictIntraBlock(mbx, mby, component, mode int) error {
	pl := &fs.pic.Planes[component]
	size := 16
	if component != 0 {
		size = 8
	}
	x, y := mbx*size, mby*size
	if pl.BitDepth == 10 {
		s, top, left := intraBlockSamples(fs, mbx, mby, component, pl.Data16)
		samples := prediction.IntraBlockSamples16{Top: s.Top[:size], Left: s.Left[:size], TopLeft: s.TopLeft}
		if component == 0 {
			return prediction.Intra16x16High(pl.Data16[pl.Index(x, y):], pl.Stride, mode, samples, top, left, pl.BitDepth)
		}
		return prediction.Chroma8x8High(pl.Data16[pl.Index(x, y):], pl.Stride, mode, samples, top, left, pl.BitDepth)
	}
	s, top, left := intraBlockSamples(fs, mbx, mby, component, pl.Data)
	samples := prediction.IntraBlockSamples{Top: s.Top[:size], Left: s.Left[:size], TopLeft: s.TopLeft}
	if component == 0 {
		return prediction.Intra16x16(pl.Data[pl.Index(x, y):], pl.Stride, mode, samples, top, left)
	}
	return prediction.Chroma8x8(pl.Data[pl.Index(x, y):], pl.Stride, mode, samples, top, left)
}

func (fs *frameState) predictIntra8(mbx, mby, bx, by, x, y, mode int) error {
	pl := &fs.pic.Planes[0]
	if pl.BitDepth == 10 {
		s, top, left := intra8Samples(fs, mbx, mby, bx, by, pl.Data16)
		return prediction.Intra8x8High(pl.Data16[pl.Index(x, y):], pl.Stride, mode, prediction.Intra8x8Samples16(s), top, left, pl.BitDepth)
	}
	s, top, left := intra8Samples(fs, mbx, mby, bx, by, pl.Data)
	return prediction.Intra8x8(pl.Data[pl.Index(x, y):], pl.Stride, mode, prediction.Intra8x8Samples(s), top, left)
}
