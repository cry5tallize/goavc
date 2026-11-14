package decoder

import (
	"github.com/goavc/h264/internal/prediction"
)

func (fs *frameState) sampleAvailable(addr, comp, x, y, order int) bool {
	other, lx, ly := fs.locateSample(addr, comp, x, y)
	if other == addr {
		return blockOrder(lx/4, ly/4) < order
	}
	return fs.intraNeighbor(addr, other)
}

func mbaffIntra4Samples[T ~uint8 | ~uint16](fs *frameState, mbx, mby, bx, by int, data []T) (intra4Neighborhood[T], bool, bool) {
	var out intra4Neighborhood[T]
	pl := &fs.pic.Planes[fs.component]
	addr := fs.mbAddress(mbx, mby)
	lx, ly := bx*4, by*4
	x, y := mbx*16+lx, mby*16+ly
	order := blockOrder(bx, by)
	top, left := true, true
	for i := range 4 {
		top = top && fs.sampleAvailable(addr, fs.component, lx+i, ly-1, order)
		left = left && fs.sampleAvailable(addr, fs.component, lx-1, ly+i, order)
	}
	if top {
		for i := range 4 {
			out.Top[i] = data[pl.Index(x+i, y-1)]
		}
		for i := range 4 {
			out.TopRight[i] = out.Top[3]
			if fs.sampleAvailable(addr, fs.component, lx+4+i, ly-1, order) {
				out.TopRight[i] = data[pl.Index(x+4+i, y-1)]
			}
		}
	}
	if left {
		for i := range 4 {
			out.Left[i] = data[pl.Index(x-1, y+i)]
		}
	}
	if fs.sampleAvailable(addr, fs.component, lx-1, ly-1, order) {
		out.TopLeft = data[pl.Index(x-1, y-1)]
	}
	return out, top, left
}

func mbaffIntraBlockSamples[T ~uint8 | ~uint16](fs *frameState, mbx, mby, comp int, data []T) (intraBlockNeighborhood[T], bool, bool) {
	var out intraBlockNeighborhood[T]
	pl := &fs.pic.Planes[comp]
	w, h := 16, 16
	if comp > 0 {
		w, h = fs.chromaW, fs.chromaH
	}
	addr := fs.mbAddress(mbx, mby)
	x, y := mbx*w, mby*h
	top, left := true, true
	for i := range w {
		other, _, _ := fs.locateSample(addr, comp, i, -1)
		top = top && fs.intraNeighbor(addr, other)
	}
	for i := range h {
		other, _, _ := fs.locateSample(addr, comp, -1, i)
		left = left && fs.intraNeighbor(addr, other)
	}
	if top {
		for i := range w {
			out.Top[i] = data[pl.Index(x+i, y-1)]
		}
	}
	if left {
		for i := range h {
			out.Left[i] = data[pl.Index(x-1, y+i)]
		}
	}
	other, _, _ := fs.locateSample(addr, comp, -1, -1)
	if fs.intraNeighbor(addr, other) {
		out.TopLeft = data[pl.Index(x-1, y-1)]
	}
	return out, top, left
}

func mbaffIntra8Samples[T ~uint8 | ~uint16](fs *frameState, mbx, mby, bx, by int, data []T) (intra8Neighborhood[T], bool, bool) {
	var out intra8Neighborhood[T]
	pl := &fs.pic.Planes[fs.component]
	addr := fs.mbAddress(mbx, mby)
	lx, ly := bx*8, by*8
	x, y := mbx*16+lx, mby*16+ly
	order := blockOrder(bx*2, by*2)
	top, left := true, true
	for i := range 8 {
		top = top && fs.sampleAvailable(addr, fs.component, lx+i, ly-1, order)
		left = left && fs.sampleAvailable(addr, fs.component, lx-1, ly+i, order)
	}
	out.TopLeftAvailable = fs.sampleAvailable(addr, fs.component, lx-1, ly-1, order)
	if out.TopLeftAvailable {
		out.TopLeft = data[pl.Index(x-1, y-1)]
	}
	if top {
		for i := range 8 {
			out.Top[i] = data[pl.Index(x+i, y-1)]
		}
		for i := 8; i < 16; i++ {
			out.Top[i] = out.Top[7]
			if fs.sampleAvailable(addr, fs.component, lx+i, ly-1, order) {
				out.Top[i] = data[pl.Index(x+i, y-1)]
			}
		}
	}
	if left {
		for i := range 8 {
			out.Left[i] = data[pl.Index(x-1, y+i)]
		}
	}
	return out, top, left
}

func (fs *frameState) mbaffMostProbableMode(addr, bx, by int) int {
	modes := [2]int{-1, -1}
	for side := range 2 {
		other, x, y := fs.blockNeighbor(addr, 0, bx, by, side == 0)
		if other == addr {
			modes[side] = int(fs.info[addr].intraModes[y*4+x])
		} else if fs.intraNeighbor(addr, other) {
			modes[side] = fs.neighborIntraMode(other, y*4+x)
		}
	}
	return prediction.MostProbableIntra4x4(modes[0], modes[1])
}

func (fs *frameState) mbaffNC(addr, comp, bx, by, width, blocks int, grid []uint8) int {
	var values [2]int
	var available [2]bool
	for side := range 2 {
		other, x, y := fs.blockNeighbor(addr, comp, bx, by, side == 0)
		available[side] = other == addr || fs.neighborAvailable(addr, other)
		if available[side] {
			values[side] = int(grid[other*blocks+y*width+x])
		}
	}
	return nC(values[0], values[1], available[0], available[1])
}

func (fs *frameState) locateMotionQuery(addr, bx, by int) (other, index int) {
	x, y := (bx-addr%fs.mbW*4)*4, (by-addr/fs.mbW*4)*4
	if x < 0 {
		x = -1
	}
	if y < 0 {
		y = -1
	}
	other, x, y = fs.locateSample(addr, 0, x, y)
	if other < 0 || other != addr && !fs.neighborAvailable(addr, other) {
		return -1, 0
	}
	return other, (other/fs.mbW*4+y/4)*fs.mv.BlockW + other%fs.mbW*4 + x/4
}

func (s *sliceEntry) readMBAFFCBP(fs *frameState, addr int) (int, error) {
	c := s.cabac
	cbp := 0
	for block := range 4 {
		x, y := block%2*8, block/2*8
		values := [2]int{1, 1}
		for side := range 2 {
			nx, ny := x, y-1
			if side == 0 {
				nx, ny = x-1, y
			}
			other, lx, ly := fs.locateSample(addr, 0, nx, ny)
			if other == addr {
				values[side] = (cbp >> (ly/8*2 + lx/8)) & 1
			} else if fs.neighborAvailable(addr, other) {
				values[side] = (fs.info[other].cbp >> (ly/8*2 + lx/8)) & 1
			}
		}
		cbp |= c.bin(73+boolToInt(values[0] == 0)+2*boolToInt(values[1] == 0)) << block
	}
	if fs.sps.ChromaFormatIDC == 3 {
		return cbp, c.d.Err()
	}
	var chroma [2]int
	for side := range 2 {
		other := fs.neighborMB(addr, side == 0)
		if fs.neighborAvailable(addr, other) {
			chroma[side] = (fs.info[other].cbp >> 4) & 3
		}
	}
	if c.bin(77+boolToInt(chroma[0] > 0)+2*boolToInt(chroma[1] > 0)) != 0 {
		cbp |= (1 + c.bin(81+boolToInt(chroma[0] == 2)+2*boolToInt(chroma[1] == 2))) << 4
	}
	return cbp, c.d.Err()
}
