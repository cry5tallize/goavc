package syntax

import (
	"fmt"
	"math/bits"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
)

// fmoDimensions 在分配前限制编码尺寸为 8192×8192，不能用裁剪后尺寸绕过。
func fmoDimensions(s *SPS) (width, height, size uint32, err error) {
	if s == nil {
		return 0, 0, 0, fmt.Errorf("syntax: FMO requires an SPS: %w", h264.ErrInvalidData)
	}
	w, h := uint64(s.PicWidthInMbsMinus1)+1, uint64(s.PicHeightInMapUnitsMinus1)+1
	factor := uint64(2)
	if s.FrameMbsOnlyFlag {
		factor = 1
	}
	if s.FrameMbsOnlyFlag && s.MbAdaptiveFrameFieldFlag {
		return 0, 0, 0, fmt.Errorf("syntax: frame_mbs_only_flag forbids MBAFF: %w", h264.ErrInvalidData)
	}
	if w > 8192/16 || h > 8192/(16*factor) {
		return 0, 0, 0, fmt.Errorf("syntax: FMO coded dimensions exceed 8192x8192: %w", h264.ErrResourceLimit)
	}
	return uint32(w), uint32(h), uint32(w * h), nil
}

func (p *PPS) validateFMO(s *SPS) error {
	if p == nil || p.NumSliceGroupsMinus1 > 7 {
		return fmt.Errorf("syntax: num_slice_groups_minus1 exceeds 7: %w", h264.ErrInvalidData)
	}
	if p.NumSliceGroupsMinus1 == 0 {
		return nil
	}
	width, _, size, err := fmoDimensions(s)
	if err != nil {
		return err
	}
	groups := p.NumSliceGroupsMinus1 + 1
	switch p.SliceGroupMapType {
	case 0:
		if len(p.RunLengthMinus1) != int(groups) {
			return fmt.Errorf("syntax: FMO run length count: %w", h264.ErrInvalidData)
		}
		for _, run := range p.RunLengthMinus1 {
			if run >= size {
				return fmt.Errorf("syntax: FMO run length exceeds picture: %w", h264.ErrInvalidData)
			}
		}
	case 1:
	case 2:
		if len(p.TopLeft) != int(groups-1) || len(p.BottomRight) != int(groups-1) {
			return fmt.Errorf("syntax: FMO rectangle count: %w", h264.ErrInvalidData)
		}
		for i, tl := range p.TopLeft {
			br := p.BottomRight[i]
			if tl > br || br >= size || tl%width > br%width {
				return fmt.Errorf("syntax: invalid FMO rectangle %d: %w", i, h264.ErrInvalidData)
			}
		}
	case 3, 4, 5:
		if groups != 2 || p.SliceGroupChangeRateMinus1 >= size {
			return fmt.Errorf("syntax: invalid dynamic FMO groups/rate: %w", h264.ErrInvalidData)
		}
	case 6:
		if uint64(p.PicSizeInMapUnitsMinus1)+1 != uint64(size) || len(p.SliceGroupID) != int(size) {
			return fmt.Errorf("syntax: explicit FMO map size differs from SPS: %w", h264.ErrInvalidData)
		}
		for _, group := range p.SliceGroupID {
			if uint32(group) >= groups {
				return fmt.Errorf("syntax: invalid FMO slice_group_id: %w", h264.ErrInvalidData)
			}
		}
	default:
		return fmt.Errorf("syntax: slice_group_map_type exceeds 6: %w", h264.ErrInvalidData)
	}
	return nil
}

// SliceGroupChangeCycleBits 返回动态 FMO cycle 的位宽及合法最大值。
// 最大 cycle 为 Ceil(PicSizeInMapUnits / SliceGroupChangeRate)，位宽为
// Ceil(Log2(PicSizeInMapUnits / SliceGroupChangeRate + 1))；除法按实数解释。
// 非动态 map type 不携带 cycle，返回 (0, 0, nil)。
func (p *PPS) SliceGroupChangeCycleBits(s *SPS) (width uint, maxCycle uint32, err error) {
	if err = p.validateFMO(s); err != nil {
		return 0, 0, err
	}
	if p.NumSliceGroupsMinus1 == 0 || p.SliceGroupMapType < 3 || p.SliceGroupMapType > 5 {
		return 0, 0, nil
	}
	_, _, size, err := fmoDimensions(s)
	if err != nil {
		return 0, 0, err
	}
	rate := p.SliceGroupChangeRateMinus1 + 1
	maxCycle = (size + rate - 1) / rate
	return uint(bits.Len32(maxCycle)), maxCycle, nil
}

func (p *PPS) parseFMO(r *bitstream.Reader, s *SPS) error {
	if p.NumSliceGroupsMinus1 > 7 {
		return fmt.Errorf("syntax: num_slice_groups_minus1 exceeds 7: %w", h264.ErrInvalidData)
	}
	if p.NumSliceGroupsMinus1 == 0 {
		return nil
	}
	_, _, size, err := fmoDimensions(s)
	if err != nil {
		return err
	}
	if p.SliceGroupMapType, err = readUE(r, "slice_group_map_type"); err != nil {
		return err
	}
	groups := p.NumSliceGroupsMinus1 + 1
	switch p.SliceGroupMapType {
	case 0:
		if r.BitsRemaining() < uint64(groups) {
			return fmt.Errorf("syntax: truncated FMO run lengths: %w", h264.ErrInvalidData)
		}
		p.RunLengthMinus1 = make([]uint32, groups)
		for i := range p.RunLengthMinus1 {
			if p.RunLengthMinus1[i], err = readUE(r, "run_length_minus1"); err != nil {
				return err
			}
		}
	case 1:
	case 2:
		if r.BitsRemaining() < 2*uint64(groups-1) {
			return fmt.Errorf("syntax: truncated FMO rectangles: %w", h264.ErrInvalidData)
		}
		p.TopLeft, p.BottomRight = make([]uint32, groups-1), make([]uint32, groups-1)
		for i := range p.TopLeft {
			if p.TopLeft[i], err = readUE(r, "top_left"); err != nil {
				return err
			}
			if p.BottomRight[i], err = readUE(r, "bottom_right"); err != nil {
				return err
			}
		}
	case 3, 4, 5:
		if groups != 2 {
			return fmt.Errorf("syntax: dynamic FMO requires two groups: %w", h264.ErrInvalidData)
		}
		if p.SliceGroupChangeDirectionFlag, err = readFlag(r, "slice_group_change_direction_flag"); err != nil {
			return err
		}
		if p.SliceGroupChangeRateMinus1, err = readUE(r, "slice_group_change_rate_minus1"); err != nil {
			return err
		}
	case 6:
		if p.PicSizeInMapUnitsMinus1, err = readUE(r, "pic_size_in_map_units_minus1"); err != nil {
			return err
		}
		width := uint(bits.Len32(groups - 1))
		// 尺寸、乘积和输入长度全部先检查，截断输入不能触发大数组分配。
		if uint64(p.PicSizeInMapUnitsMinus1)+1 != uint64(size) || uint64(size)*uint64(width) > r.BitsRemaining() {
			return fmt.Errorf("syntax: invalid/truncated explicit FMO map: %w", h264.ErrInvalidData)
		}
		p.SliceGroupID = make([]uint8, size)
		for i := range p.SliceGroupID {
			v, err := readBits(r, width, "slice_group_id")
			if err != nil {
				return err
			}
			if v >= uint64(groups) {
				return fmt.Errorf("syntax: slice_group_id exceeds group count: %w", h264.ErrInvalidData)
			}
			p.SliceGroupID[i] = uint8(v)
		}
	default:
		return fmt.Errorf("syntax: slice_group_map_type exceeds 6: %w", h264.ErrInvalidData)
	}
	return p.validateFMO(s)
}

func (p *PPS) writeFMO(w *bitstream.Writer) error {
	if p.NumSliceGroupsMinus1 == 0 {
		return nil
	}
	if err := writeUE(w, p.SliceGroupMapType, "slice_group_map_type"); err != nil {
		return err
	}
	switch p.SliceGroupMapType {
	case 0:
		for _, run := range p.RunLengthMinus1 {
			if err := writeUE(w, run, "run_length_minus1"); err != nil {
				return err
			}
		}
	case 2:
		for i, tl := range p.TopLeft {
			if err := writeUE(w, tl, "top_left"); err != nil {
				return err
			}
			if err := writeUE(w, p.BottomRight[i], "bottom_right"); err != nil {
				return err
			}
		}
	case 3, 4, 5:
		if err := writeFlag(w, p.SliceGroupChangeDirectionFlag); err != nil {
			return err
		}
		return writeUE(w, p.SliceGroupChangeRateMinus1, "slice_group_change_rate_minus1")
	case 6:
		if err := writeUE(w, p.PicSizeInMapUnitsMinus1, "pic_size_in_map_units_minus1"); err != nil {
			return err
		}
		width := uint(bits.Len32(p.NumSliceGroupsMinus1))
		for _, group := range p.SliceGroupID {
			if err := writeBits(w, width, uint64(group), "slice_group_id"); err != nil {
				return err
			}
		}
	}
	return nil
}
