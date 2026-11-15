package decoder

import (
	"fmt"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/internal/dpb"
	"github.com/cry5tallize/goavc/internal/motion"
)

// MB元数据始终按完整帧光栅索引；MBAFF只改变语法遍历和像素视图。
func (fs *frameState) rasterMB(scan int) int {
	if !fs.mbaff {
		return scan
	}
	return scan/(2*fs.mbW)*(2*fs.mbW) + scan/2%fs.mbW + (scan&1)*fs.mbW
}

func (fs *frameState) fieldAt(addr int) bool {
	return addr >= 0 && addr < len(fs.fieldFlags) && fs.fieldFlags[addr]
}

func (fs *frameState) mbAddress(mbx, mby int) int {
	if fs.field {
		mby = 2*mby + fs.parity
	}
	return mby*fs.mbW + mbx
}

func (fs *frameState) mbCoordinates(addr int) (int, int) {
	x, y := addr%fs.mbW, addr/fs.mbW
	if fs.fieldAt(addr) {
		y /= 2
	}
	return x, y
}

func (fs *frameState) motionY(mby int) int {
	if fs.field {
		return (2*mby + fs.parity) * 4
	}
	return mby * 4
}

func (fs *frameState) selectMB(addr int) {
	fs.activeMB = addr
	fs.field = fs.fieldAt(addr)
	fs.parity = (addr / fs.mbW) & 1
	fs.pic = fs.framePic
	fs.poc = fs.framePOC
	if fs.field {
		fs.pic = &fs.fieldPics[fs.parity]
		fs.poc = fs.fieldPOC[fs.parity]
	}
}

func (fs *frameState) restoreFrame() {
	fs.pic = fs.framePic
	fs.poc = fs.framePOC
	fs.field = false
}

func (fs *frameState) setPairField(scan int, field bool) {
	top := fs.rasterMB(scan &^ 1)
	fs.fieldFlags[top], fs.fieldFlags[top+fs.mbW] = field, field
}

func (fs *frameState) pairNeighbor(scan int, left bool) int {
	top := fs.rasterMB(scan &^ 1)
	if left {
		if top%fs.mbW == 0 {
			return -1
		}
		return top - 1
	}
	return top - 2*fs.mbW
}

func (fs *frameState) inferPairField(scan int) bool {
	addr := fs.rasterMB(scan &^ 1)
	for _, left := range []bool{true, false} {
		other := fs.pairNeighbor(scan, left)
		if fs.neighborAvailable(addr, other) {
			return fs.fieldAt(other)
		}
	}
	return false
}

func (s *sliceEntry) readPairField(fs *frameState, scan int) error {
	var value bool
	if s.cabac == nil {
		v, err := s.data.ReadBits(1)
		if err != nil {
			return err
		}
		value = v != 0
	} else {
		inc := 0
		addr := fs.rasterMB(scan &^ 1)
		for _, left := range []bool{true, false} {
			other := fs.pairNeighbor(scan, left)
			if fs.neighborAvailable(addr, other) && fs.fieldAt(other) {
				inc++
			}
		}
		value = s.cabac.bin(70+inc) != 0
		if err := s.cabac.d.Err(); err != nil {
			return err
		}
	}
	fs.setPairField(scan, value)
	return nil
}

// locateSample把当前MB局部的帧/场样点映射到完整帧光栅MB及其局部采样。
// 位于另一场的块不是简单的addr-mbW；所有邻块消费者共用这一物理映射。
func (fs *frameState) locateSample(addr, comp, x, y int) (other, lx, ly int) {
	w, h := 16, 16
	if comp > 0 {
		w, h = fs.chromaW, fs.chromaH
	}
	x += addr % fs.mbW * w
	row := addr / fs.mbW
	if fs.fieldAt(addr) {
		y = row/2*(2*h) + (row & 1) + 2*y
	} else {
		y += row * h
	}
	return fs.locatePhysical(comp, x, y)
}

func (fs *frameState) locatePhysical(comp, x, y int) (other, lx, ly int) {
	w, h := 16, 16
	if comp > 0 {
		w, h = fs.chromaW, fs.chromaH
	}
	if x < 0 || y < 0 || x >= fs.mbW*w || y >= fs.mbH*h {
		return -1, 0, 0
	}
	pairY := y / (2 * h)
	top := pairY*(2*fs.mbW) + x/w
	if fs.fieldAt(top) {
		return top + (y&1)*fs.mbW, x % w, (y % (2 * h)) / 2
	}
	return y/h*fs.mbW + x/w, x % w, y % h
}

func (fs *frameState) neighborMB(addr int, left bool) int {
	x, y := 0, -1
	if left {
		x, y = -1, 0
	}
	other, _, _ := fs.locateSample(addr, 0, x, y)
	return other
}

func (fs *frameState) blockNeighbor(addr, comp, bx, by int, left bool) (int, int, int) {
	x, y := bx*4, by*4-1
	if left {
		x, y = bx*4-1, by*4
	}
	other, x, y := fs.locateSample(addr, comp, x, y)
	return other, x / 4, y / 4
}

func (fs *frameState) structure(addr int) motion.FieldStructure {
	if !fs.fieldAt(addr) {
		return motion.FrameMotion
	}
	if addr/fs.mbW&1 == 0 {
		return motion.TopFieldMotion
	}
	return motion.BottomFieldMotion
}

func (fs *frameState) motionNeighbor(grid *motion.Grid, addr, x, y int) motion.FieldNeighbor {
	other, bx, by := fs.locateSample(addr, 0, x, y)
	if other < 0 || other != addr && !fs.neighborAvailable(addr, other) {
		return motion.FieldNeighbor{Ref: -1}
	}
	mv, ref, ok := grid.Get(other%fs.mbW*4+bx/4, other/fs.mbW*4+by/4)
	return motion.FieldNeighbor{MV: mv, Ref: ref, Structure: fs.structure(other), Available: ok}
}

func (fs *frameState) fieldNeighbors(grid *motion.Grid, addr, bx, by, w int) motion.FieldNeighbors {
	x, y := (bx-addr%fs.mbW*4)*4, (by-addr/fs.mbW*4)*4
	return motion.FieldNeighbors{A: fs.motionNeighbor(grid, addr, x-1, y), B: fs.motionNeighbor(grid, addr, x, y-1), C: fs.motionNeighbor(grid, addr, x+w*4, y-1), D: fs.motionNeighbor(grid, addr, x-1, y-1)}
}

func (fs *frameState) fieldPrediction(grid *motion.Grid, addr, bx, by, w int, ref int8, partition motion.FieldPartition, skip bool) motion.MV {
	n := fs.fieldNeighbors(grid, addr, bx, by, w)
	if skip {
		return motion.PredictFieldSkip(fs.structure(addr), n)
	}
	return motion.PredictField(fs.structure(addr), ref, partition, n)
}

func (s *sliceEntry) selectFieldReferences(fs *frameState) error {
	if !fs.mbaff {
		return nil
	}
	if !fs.field {
		s.refs = s.frameRefs
		return nil
	}
	parity := fs.parity
	for list := range 2 {
		if s.fieldRefs[parity][list] == nil && len(s.frameRefs[list]) > 0 {
			refs := make([]*dpb.Reference, 0, 2*len(s.frameRefs[list]))
			for _, r := range s.frameRefs[list] {
				for i := range 2 {
					field, err := r.FieldReference((parity ^ i) != 0)
					if err != nil {
						return err
					}
					refs = append(refs, field)
				}
			}
			s.fieldRefs[parity][list] = refs
		}
		s.refs[list] = s.fieldRefs[parity][list]
	}
	return nil
}

func (fs *frameState) decodeMBAFFMB(s *sliceEntry, scan, sliceID int, qp *int, skip bool) error {
	addr := fs.rasterMB(scan)
	if fs.decoded[addr] {
		return fmt.Errorf("decoder: MBAFF macroblock %d repeated: %w", scan, h264.ErrInvalidData)
	}
	fs.selectMB(addr)
	fs.info[addr].sliceID = sliceID
	if err := s.selectFieldReferences(fs); err != nil {
		return err
	}
	var err error
	if skip {
		err = decodeSkipMacroblock(fs, addr, s, *qp, sliceID)
	} else {
		err = decodeMacroblock(fs, addr, s, sliceID, qp)
	}
	if err != nil {
		return fmt.Errorf("decoder: MBAFF mb %d: %w", scan, err)
	}
	if s.cabac != nil && (skip || fs.info[addr].cbp == 0 && !fs.info[addr].intra16) {
		s.cabac.lastDelta = false
	}
	fs.decoded[addr] = true
	return nil
}

// Spec: 8.4.1.4，420场间色度位置不同；亮度MV不变，色度纵向另加±2。
func (fs *frameState) chromaFieldOffset(ref *dpb.Reference) int {
	if !fs.field || fs.subH != 2 || ref.FrameReference == nil {
		return 0
	}
	return 2 * (fs.parity - boolToInt(ref.BottomField))
}
