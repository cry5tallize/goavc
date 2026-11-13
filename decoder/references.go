package decoder

import (
	"fmt"
	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/internal/dpb"
)

func (fs *frameState) setReferenceIdentity(list, bx, by, w, h int, id *dpb.Identity) {
	for y := by; y < by+h; y++ {
		for x := bx; x < bx+w; x++ {
			fs.referenceIDs[list][y*fs.mv.BlockW+x] = id
		}
	}
}

// refIndexNeighbor 允许 ref_idx 上下文读取当前宏块中已解析的参考索引，
// 但不会把这些尚未解出 MVD 的分区标记为运动矢量可用。
func (fs *frameState) refIndexNeighbor(addr, bx, by int) int {
	if bx < 0 || by < 0 || bx >= fs.mv.BlockW || by >= fs.mv.BlockH {
		return -1
	}
	other := (by/4)*fs.mbW + bx/4
	if other != addr && !fs.neighborAvailable(addr, other) {
		return -1
	}
	return int(fs.mv.Ref[by*fs.mv.BlockW+bx])
}

func (s *sliceEntry) readReferenceIndex(fs *frameState, addr, bx, by int) (int8, error) {
	n := len(s.refs[0])
	if n < 1 {
		return -1, fmt.Errorf("decoder: empty list0: %w", h264.ErrInvalidData)
	}
	if n == 1 {
		return 0, nil
	}
	index := 0
	if s.cabac == nil {
		if n == 2 {
			b, err := s.data.ReadBits(1)
			if err != nil {
				return -1, err
			}
			index = 1 - int(b)
		} else {
			v, err := s.data.ReadUE()
			if err != nil {
				return -1, err
			}
			index = int(v)
		}
	} else {
		ctx := boolToInt(fs.refIndexNeighbor(addr, bx-1, by) > 0) + 2*boolToInt(fs.refIndexNeighbor(addr, bx, by-1) > 0)
		for s.cabac.d.Err() == nil && s.cabac.bin(54+ctx) != 0 {
			index++
			if index >= n {
				return -1, fmt.Errorf("decoder: CABAC ref_idx exceeds active count: %w", h264.ErrInvalidData)
			}
			ctx = (ctx >> 2) + 4
		}
		if err := s.cabac.d.Err(); err != nil {
			return -1, err
		}
	}
	if index < 0 || index >= n {
		return -1, fmt.Errorf("decoder: ref_idx %d exceeds %d: %w", index, n, h264.ErrInvalidData)
	}
	return int8(index), nil
}
