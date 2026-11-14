package decoder

import (
	"errors"
	"fmt"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/syntax"
)

// reconstructRepresentation只重建一个redundant_pic_cnt表示，不发布帧、不更新
// DPB/POC。失败仅归还本表示的图像，不能把不同表示的宏块拼成一张图。
func (d *Decoder) reconstructRepresentation(entries []sliceEntry, top, bottom, poc int32) (states [3]*frameState, planeCount int, err error) {
	groupMap, err := prepareSliceOrder(entries)
	if err != nil {
		return states, 0, err
	}
	first := entries[0]
	sps, pps := first.sps, first.pps
	bFrame := false
	for _, entry := range entries {
		if entry.hdr.Type == syntax.SliceTypeB {
			bFrame = true
		}
	}
	fs, err := newFrameState(d, sps, pps, bFrame)
	if err != nil {
		return states, 0, err
	}
	success := false
	defer func() {
		if !success {
			fs.release()
		}
	}()
	fs.poc, fs.framePOC, fs.fieldPOC = poc, poc, [2]int32{top, bottom}
	states[0], planeCount = fs, 1
	if sps.SeparateColourPlaneFlag {
		planeCount = 3
		for comp := 1; comp < planeCount; comp++ {
			states[comp], err = newFrameStateForPicture(d, sps, pps, bFrame, fs.pic)
			if err != nil {
				return states, 0, err
			}
			states[comp].component, states[comp].poc = comp, poc
			states[comp].framePOC, states[comp].fieldPOC = poc, [2]int32{top, bottom}
		}
	}
	if err := assignSliceOwnership(entries, states, planeCount, groupMap); err != nil {
		return states, 0, err
	}
	for si := range entries {
		s := &entries[si]
		fs := states[s.hdr.ColourPlaneID]
		if s.hdr.Type != syntax.SliceTypeI && s.hdr.Type != syntax.SliceTypeSI {
			lists := d.dpb.Lists(s.hdr.FrameNum, poc, s.hdr.Type == syntax.SliceTypeB)
			for list := 0; list < 1+boolToInt(s.hdr.Type == syntax.SliceTypeB); list++ {
				n := int(s.hdr.NumRefIdxL0ActiveMinus1) + 1
				if list == 1 {
					n = int(s.hdr.NumRefIdxL1ActiveMinus1) + 1
				}
				s.refs[list], err = d.dpb.Modify(lists[list], s.hdr.FrameNum, s.hdr.RefListModifications[list], n)
				if err != nil {
					return states, 0, err
				}
			}
		}
		s.frameRefs = s.refs
		if err := decodeSlice(fs, s, si, s.firstMB, s.endMB); err != nil {
			return states, 0, err
		}
	}
	for comp := 0; comp < planeCount; comp++ {
		state := states[comp]
		state.restoreFrame()
		for i, decoded := range state.decoded {
			if !decoded {
				return states, 0, fmt.Errorf("decoder: plane %d macroblock %d missing: %w", comp, i, h264.ErrInvalidData)
			}
		}
		state.mv.BeginSlice(0)
		if state.mv1 != nil {
			state.mv1.BeginSlice(0)
		}
		if err := state.deblock(); err != nil {
			return states, 0, err
		}
	}
	fs.pic.FillBorders()
	success = true
	return states, planeCount, nil
}

func (d *Decoder) reconstructRepresentations(entries []sliceEntry, top, bottom, poc int32) (states [3]*frameState, planeCount int, selected sliceEntry, err error) {
	// 7.4.1.2.3：primary在先，备用表示按cnt升序。ASO只重排同表示内slice。
	for i := 1; i < len(entries); i++ {
		if entries[i].hdr.RedundantPicCnt < entries[i-1].hdr.RedundantPicCnt {
			return states, 0, selected, fmt.Errorf("decoder: decreasing redundant_pic_cnt: %w", h264.ErrInvalidData)
		}
	}
	var firstError error
	for start := 0; start < len(entries); {
		end := start + 1
		for end < len(entries) && entries[end].hdr.RedundantPicCnt == entries[start].hdr.RedundantPicCnt {
			end++
		}
		states, planeCount, err = d.reconstructRepresentation(entries[start:end], top, bottom, poc)
		if err == nil {
			return states, planeCount, entries[start], nil
		}
		if !errors.Is(err, h264.ErrInvalidData) {
			return states, 0, selected, err
		}
		if firstError == nil {
			firstError = err
		}
		start = end
	}
	return states, 0, selected, firstError
}
