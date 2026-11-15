package encoder

import "github.com/goavc/h264/internal/prediction"

func (s *frameState) available(addr, other int) bool {
	return other >= s.info[addr].start && other < addr
}

// blockSamplesScratch由调用栈持有。返回的Top/Left仅在该槽下次填充前有效；
// 同时存活的邻域必须传入不同槽，不能用frameState单一共享槽替代。
type blockSamplesScratch struct {
	top, left [16]uint16
}

func (s *frameState) blockSamples(addr, c int, scratch *blockSamplesScratch) (prediction.IntraBlockSamples16, bool, bool, bool) {
	w, h := s.pic.Format.PlaneDimensions(c, 16, 16)
	mw := s.pic.MBWidth
	x, y := addr%mw*w, addr/mw*h
	top, left := s.available(addr, addr-mw), addr%mw > 0 && s.available(addr, addr-1)
	tl := x > 0 && s.available(addr, addr-mw-1)
	a := prediction.IntraBlockSamples16{Top: scratch.top[:w], Left: scratch.left[:h]}
	// 不可用邻域原先由make给出零值；复用后也不得泄漏上一宏块的采样。
	clear(a.Top)
	clear(a.Left)
	p := &s.pic.Planes[c]
	if top {
		for i := range w {
			a.Top[i] = p.Sample(x+i, y-1)
		}
	}
	if left {
		for i := range h {
			a.Left[i] = p.Sample(x-1, y+i)
		}
	}
	if tl {
		a.TopLeft = p.Sample(x-1, y-1)
	}
	return a, top, left, tl
}

func (s *frameState) samples4(addr, bx, by int) (prediction.Intra4x4Samples16, bool, bool, bool) {
	mw := s.pic.MBWidth
	mbx, mby := addr%mw, addr/mw
	x, y := mbx*16+bx*4, mby*16+by*4
	top := by > 0 || s.available(addr, addr-mw)
	left := bx > 0 || (mbx > 0 && s.available(addr, addr-1))
	tl := top && left
	if bx == 0 && by == 0 {
		tl = mbx > 0 && s.available(addr, addr-mw-1)
	}
	var a prediction.Intra4x4Samples16
	p := &s.pic.Planes[0]
	if top {
		tr := false
		if by == 0 {
			tr = bx < 3 || (mbx+1 < mw && s.available(addr, addr-mw+1))
		} else if bx < 3 {
			tr = order(bx+1, by-1) < order(bx, by)
		}
		for i := 0; i < 4; i++ {
			a.Top[i] = p.Sample(x+i, y-1)
			a.TopRight[i] = p.Sample(x+3, y-1)
			if tr {
				a.TopRight[i] = p.Sample(x+4+i, y-1)
			}
		}
	}
	if left {
		for i := 0; i < 4; i++ {
			a.Left[i] = p.Sample(x-1, y+i)
		}
	}
	if tl {
		a.TopLeft = p.Sample(x-1, y-1)
	}
	return a, top, left, tl
}

func (s *frameState) probable(addr, r int) int {
	mw := s.pic.MBWidth
	bx, by := r%4, r/4
	left, top := -1, -1
	if bx > 0 {
		left = int(s.info[addr].modes[r-1])
	} else if addr%mw > 0 && s.available(addr, addr-1) {
		left = int(s.info[addr-1].modes[by*4+3])
		if s.info[addr-1].inter {
			left = 2
		}
	}
	if by > 0 {
		top = int(s.info[addr].modes[r-4])
	} else if s.available(addr, addr-mw) {
		top = int(s.info[addr-mw].modes[12+bx])
		if s.info[addr-mw].inter {
			top = 2
		}
	}
	return prediction.MostProbableIntra4x4(left, top)
}

func (s *frameState) nc(addr, r, c int) int {
	w, h := 4, 4
	grid := s.nz
	if c > 0 {
		pw, ph := s.pic.Format.PlaneDimensions(c, 16, 16)
		w, h = pw/4, ph/4
		grid = s.cnz[c-1]
	}
	mw := s.pic.MBWidth
	bx, by := r%w, r/w
	a, b := -1, -1
	if bx > 0 {
		a = int(grid[addr*16+r-1])
	} else if addr%mw > 0 && s.available(addr, addr-1) {
		a = int(grid[(addr-1)*16+by*w+w-1])
	}
	if by > 0 {
		b = int(grid[addr*16+r-w])
	} else if s.available(addr, addr-mw) {
		b = int(grid[(addr-mw)*16+(h-1)*w+bx])
	}
	if a < 0 {
		return max(0, b)
	}
	if b < 0 {
		return a
	}
	return (a + b + 1) >> 1
}
