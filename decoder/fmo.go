package decoder

import (
	"cmp"
	"fmt"
	"slices"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/syntax"
)

// prepareSliceOrder只重排NAL描述符，不复制RBSP。Spec: 8.2.2.8；组内必须
// 沿递增MB地址解码，其他组的首址不能截断本组slice。
func prepareSliceOrder(entries []sliceEntry) ([]uint8, error) {
	slices.SortFunc(entries, func(a, b sliceEntry) int {
		if c := cmp.Compare(a.hdr.ColourPlaneID, b.hdr.ColourPlaneID); c != 0 {
			return c
		}
		return cmp.Compare(a.hdr.FirstMBInSlice, b.hdr.FirstMBInSlice)
	})
	first := &entries[0]
	total := int(first.sps.PicWidthInMbs() * first.sps.FrameHeightInMbs())
	factor := 1
	if first.sps.MbAdaptiveFrameFieldFlag {
		factor = 2
	}
	var groupMap []uint8
	if first.pps.NumSliceGroupsMinus1 != 0 {
		var err error
		groupMap, err = first.pps.MbToSliceGroupMap(first.sps, false, first.hdr.SliceGroupChangeCycle)
		if err != nil {
			return nil, err
		}
	}
	var next [3][8]int
	for plane := range next {
		for group := range next[plane] {
			next[plane][group] = total
		}
	}
	for i := len(entries) - 1; i >= 0; i-- {
		s := &entries[i]
		plane := int(s.hdr.ColourPlaneID)
		if plane >= 3 || (plane != 0 && !first.sps.SeparateColourPlaneFlag) || !samePicture(*first, *s) || s.hdr.SliceGroupChangeCycle != first.hdr.SliceGroupChangeCycle {
			return nil, fmt.Errorf("decoder: inconsistent picture/plane/FMO cycle: %w", h264.ErrInvalidData)
		}
		address := uint64(s.hdr.FirstMBInSlice) * uint64(factor)
		if address >= uint64(total) {
			return nil, fmt.Errorf("decoder: first macroblock exceeds picture: %w", h264.ErrInvalidData)
		}
		s.firstMB = int(address)
		group := uint8(0)
		if groupMap != nil {
			group = groupMap[s.firstMB]
		}
		s.endMB = next[plane][group]
		if s.firstMB >= s.endMB {
			return nil, fmt.Errorf("decoder: duplicate slice first macroblock: %w", h264.ErrInvalidData)
		}
		next[plane][group] = s.firstMB
	}
	return groupMap, nil
}

func assignSliceOwnership(entries []sliceEntry, states [3]*frameState, planeCount int, groupMap []uint8) error {
	for plane := 0; plane < planeCount; plane++ {
		fs := states[plane]
		fs.groupMap = groupMap
		if groupMap != nil {
			fs.sliceOwners = make([]int, fs.total)
		}
	}
	for i := range entries {
		s := &entries[i]
		fs := states[s.hdr.ColourPlaneID]
		for scan := s.firstMB; scan < s.endMB; scan = fs.nextMB(scan) {
			addr := fs.rasterMB(scan)
			if fs.info[addr].sliceID != -1 {
				return fmt.Errorf("decoder: overlapping slice ownership: %w", h264.ErrInvalidData)
			}
			fs.info[addr].sliceID = i
			if fs.sliceOwners != nil {
				fs.sliceOwners[addr] = i
			}
		}
	}
	for plane := 0; plane < planeCount; plane++ {
		for _, info := range states[plane].info {
			if info.sliceID < 0 {
				return fmt.Errorf("decoder: missing macroblock coverage: %w", h264.ErrInvalidData)
			}
		}
	}
	return nil
}

// 单组无额外存储；多组最多各扫描一次图片，工作量受八组上限约束。
func (fs *frameState) nextMB(addr int) int {
	next := addr + 1
	if fs.groupMap != nil {
		group := fs.groupMap[addr]
		for next < fs.total && fs.groupMap[next] != group {
			next++
		}
	}
	return next
}

func decodeMappedCAVLC(fs *frameState, s *sliceEntry, sliceID, first, end int) error {
	fs.mv.BeginSlice(first)
	if fs.mv1 != nil {
		fs.mv1.BeginSlice(first)
	}
	if fs.sliceOwners != nil {
		fs.mv.BeginMappedSlice(fs.sliceOwners, sliceID)
		if fs.mv1 != nil {
			fs.mv1.BeginMappedSlice(fs.sliceOwners, sliceID)
		}
	}
	qp := s.hdr.SliceQP
	for addr := first; addr < end; {
		if s.hdr.Type == syntax.SliceTypeP || s.hdr.Type == syntax.SliceTypeB || s.hdr.Type == syntax.SliceTypeSP {
			run, err := s.data.ReadUE()
			if err != nil {
				return fmt.Errorf("decoder: mb_skip_run: %w", err)
			}
			for i := uint32(0); i < run; i++ {
				if addr >= end || fs.decoded[addr] || fs.info[addr].sliceID != sliceID {
					return fmt.Errorf("decoder: skip run exceeds slice group: %w", h264.ErrInvalidData)
				}
				if err := decodeSkipMacroblock(fs, addr, s, qp, sliceID); err != nil {
					return err
				}
				fs.decoded[addr] = true
				addr = fs.nextMB(addr)
			}
			if addr >= end {
				break
			}
		}
		if fs.decoded[addr] || fs.info[addr].sliceID != sliceID {
			return fmt.Errorf("decoder: macroblock ownership mismatch: %w", h264.ErrInvalidData)
		}
		if err := decodeMacroblock(fs, addr, s, sliceID, &qp); err != nil {
			return fmt.Errorf("decoder: mb %d: %w", addr, err)
		}
		fs.decoded[addr] = true
		addr = fs.nextMB(addr)
	}
	return nil
}
