package encoder

import "github.com/goavc/h264/internal/prediction"

func (s *intraState) available(addr, other int) bool {
	return other >= s.info[addr].start && other < addr
}

func (s *intraState) blockSamples(addr, c int) (prediction.IntraBlockSamples, bool, bool, bool) {
	n := 16
	if c > 0 {
		n = 8
	}
	mw := s.pic.MBWidth
	x, y := addr%mw*n, addr/mw*n
	top, left := s.available(addr, addr-mw), addr%mw > 0 && s.available(addr, addr-1)
	tl := x > 0 && s.available(addr, addr-mw-1)
	a := prediction.IntraBlockSamples{Top: make([]byte, n), Left: make([]byte, n)}
	p := &s.pic.Planes[c]
	if top {
		for i := 0; i < n; i++ {
			a.Top[i] = p.Pix(x+i, y-1)
		}
	}
	if left {
		for i := 0; i < n; i++ {
			a.Left[i] = p.Pix(x-1, y+i)
		}
	}
	if tl {
		a.TopLeft = p.Pix(x-1, y-1)
	}
	return a, top, left, tl
}

func (s *intraState) samples4(addr, bx, by int) (prediction.Intra4x4Samples, bool, bool, bool) {
	mw := s.pic.MBWidth
	mbx, mby := addr%mw, addr/mw
	x, y := mbx*16+bx*4, mby*16+by*4
	top := by > 0 || s.available(addr, addr-mw)
	left := bx > 0 || (mbx > 0 && s.available(addr, addr-1))
	tl := top && left
	if bx == 0 && by == 0 {
		tl = mbx > 0 && s.available(addr, addr-mw-1)
	}
	var a prediction.Intra4x4Samples
	p := &s.pic.Planes[0]
	if top {
		tr := false
		if by == 0 {
			tr = bx < 3 || (mbx+1 < mw && s.available(addr, addr-mw+1))
		} else if bx < 3 {
			tr = order(bx+1, by-1) < order(bx, by)
		}
		for i := 0; i < 4; i++ {
			a.Top[i] = p.Pix(x+i, y-1)
			a.TopRight[i] = p.Pix(x+3, y-1)
			if tr {
				a.TopRight[i] = p.Pix(x+4+i, y-1)
			}
		}
	}
	if left {
		for i := 0; i < 4; i++ {
			a.Left[i] = p.Pix(x-1, y+i)
		}
	}
	if tl {
		a.TopLeft = p.Pix(x-1, y-1)
	}
	return a, top, left, tl
}

func (s *intraState) probable(addr, r int) int {
	mw := s.pic.MBWidth
	bx, by := r%4, r/4
	left, top := -1, -1
	if bx > 0 {
		left = int(s.info[addr].modes[r-1])
	} else if addr%mw > 0 && s.available(addr, addr-1) {
		left = int(s.info[addr-1].modes[by*4+3])
	}
	if by > 0 {
		top = int(s.info[addr].modes[r-4])
	} else if s.available(addr, addr-mw) {
		top = int(s.info[addr-mw].modes[12+bx])
	}
	return prediction.MostProbableIntra4x4(left, top)
}

func (s *intraState) nc(addr, r, c int) int {
	n := 4
	grid := s.nz
	if c > 0 {
		n = 2
		grid = s.cnz[c-1]
	}
	mw := s.pic.MBWidth
	bx, by := r%n, r/n
	a, b := -1, -1
	if bx > 0 {
		a = int(grid[addr*n*n+r-1])
	} else if addr%mw > 0 && s.available(addr, addr-1) {
		a = int(grid[(addr-1)*n*n+by*n+n-1])
	}
	if by > 0 {
		b = int(grid[addr*n*n+r-n])
	} else if s.available(addr, addr-mw) {
		b = int(grid[(addr-mw)*n*n+(n-1)*n+bx])
	}
	if a < 0 {
		return max(0, b)
	}
	if b < 0 {
		return a
	}
	return (a + b + 1) >> 1
}
