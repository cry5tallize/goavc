package syntax

import (
	"fmt"

	h264 "github.com/goavc/h264"
)

// MapUnitToSliceGroupMap 按 §8.2.2 生成 map-unit 光栅地址到 slice group 的映射。
// 逐行帧的 map unit 是一个 MB；隔行 SPS 的 map unit 是一对垂直 MB，
// 场图像中则是该场的一个 MB。cycle 为 slice header 中的动态 cycle，
// 静态类型及单组 PPS 必须传 0。返回数组归调用方所有，不引用 PPS 存储。
func (p *PPS) MapUnitToSliceGroupMap(s *SPS, cycle uint32) ([]uint8, error) {
	if err := p.validateFMO(s); err != nil {
		return nil, err
	}
	width, height, size, err := fmoDimensions(s)
	if err != nil {
		return nil, err
	}
	maxCycle := uint32(0)
	if p.NumSliceGroupsMinus1 > 0 && p.SliceGroupMapType >= 3 && p.SliceGroupMapType <= 5 {
		rate := p.SliceGroupChangeRateMinus1 + 1
		maxCycle = (size + rate - 1) / rate
	}
	if cycle > maxCycle {
		return nil, fmt.Errorf("syntax: slice_group_change_cycle exceeds %d: %w", maxCycle, h264.ErrInvalidData)
	}
	m := make([]uint8, size)
	if p.NumSliceGroupsMinus1 == 0 {
		return m, nil
	}
	groups := p.NumSliceGroupsMinus1 + 1
	switch p.SliceGroupMapType {
	case 0:
		for i := uint32(0); i < size; {
			for group, run := range p.RunLengthMinus1 {
				end := i + run + 1
				if end > size {
					end = size
				}
				for ; i < end; i++ {
					m[i] = uint8(group)
				}
			}
		}
	case 1:
		for i := range m {
			x, y := uint32(i)%width, uint32(i)/width
			m[i] = uint8((x + y*groups/2) % groups)
		}
	case 2:
		for i := range m {
			m[i] = uint8(groups - 1)
		}
		// 较小组号覆盖较大的矩形，背景组永远最后。
		for group := int(groups) - 2; group >= 0; group-- {
			tl, br := p.TopLeft[group], p.BottomRight[group]
			for y := tl / width; y <= br/width; y++ {
				for x := tl % width; x <= br%width; x++ {
					m[y*width+x] = uint8(group)
				}
			}
		}
	case 3, 4, 5:
		units0 := uint64(cycle) * (uint64(p.SliceGroupChangeRateMinus1) + 1)
		if units0 > uint64(size) {
			units0 = uint64(size)
		}
		direction := uint32(0)
		if p.SliceGroupChangeDirectionFlag {
			direction = 1
		}
		if p.SliceGroupMapType == 3 {
			boxOutMap(m, int(width), int(height), int(units0), int(direction))
			break
		}
		upperLeft := uint32(units0)
		if direction != 0 {
			upperLeft = size - upperLeft
		}
		for i := range size {
			address := i
			if p.SliceGroupMapType == 5 {
				// Wipe 按列扫描，但结果仍按 map-unit 光栅地址索引。
				address = (i%height)*width + i/height
			}
			m[address] = uint8(direction)
			if i >= upperLeft {
				m[address] = uint8(1 - direction)
			}
		}
	case 6:
		copy(m, p.SliceGroupID)
	}
	return m, nil
}

// boxOutMap 逐步扩张四个边界；方向和转弯条件逐项对应 §8.2.2.4。
func boxOutMap(m []uint8, width, height, units0, direction int) {
	for i := range m {
		m[i] = 1
	}
	x, y := (width-direction)/2, (height-direction)/2
	left, top, right, bottom := x, y, x, y
	xDir, yDir := direction-1, direction
	for k := 0; k < units0; {
		if m[y*width+x] == 1 {
			m[y*width+x] = 0
			k++
		}
		switch {
		case xDir == -1 && x == left:
			if left > 0 {
				left--
			}
			x, xDir, yDir = left, 0, 2*direction-1
		case xDir == 1 && x == right:
			if right < width-1 {
				right++
			}
			x, xDir, yDir = right, 0, 1-2*direction
		case yDir == -1 && y == top:
			if top > 0 {
				top--
			}
			y, xDir, yDir = top, 1-2*direction, 0
		case yDir == 1 && y == bottom:
			if bottom < height-1 {
				bottom++
			}
			y, xDir, yDir = bottom, 2*direction-1, 0
		default:
			x, y = x+xDir, y+yDir
		}
	}
}

// MbToSliceGroupMap 返回以宏块地址（非像素坐标）为索引的映射。
// 逐行帧/场图像为光栅 MB 地址；MBAFF 帧为 MB-pair 扫描地址（每对两个
// 连续地址）；非 MBAFF 隔行帧为帧光栅 MB 地址。fieldPic 表示场图像。
func (p *PPS) MbToSliceGroupMap(s *SPS, fieldPic bool, cycle uint32) ([]uint8, error) {
	if s == nil || (s.FrameMbsOnlyFlag && fieldPic) {
		return nil, fmt.Errorf("syntax: invalid FMO field picture: %w", h264.ErrInvalidData)
	}
	units, err := p.MapUnitToSliceGroupMap(s, cycle)
	if err != nil {
		return nil, err
	}
	if s.FrameMbsOnlyFlag || fieldPic {
		return units, nil
	}
	width := s.PicWidthInMbsMinus1 + 1
	m := make([]uint8, len(units)*2)
	for i := range m {
		address := uint32(i) / 2
		if !s.MbAdaptiveFrameFieldFlag {
			address = (uint32(i)/(2*width))*width + uint32(i)%width
		}
		m[i] = units[address]
	}
	return m, nil
}

// NextMBAddress 返回同组的下一个宏块地址（§8.2.2.8）。mbMap 必须是
// MbToSliceGroupMap 的结果；到组尾时返回 uint32(len(mbMap))，不越界索引。
// 当前地址或空图返回 ErrInvalidData；超出 8192×8192 图像上限返回 ErrResourceLimit。
func NextMBAddress(mbMap []uint8, current uint32) (uint32, error) {
	if len(mbMap) > (8192/16)*(8192/16) {
		return 0, fmt.Errorf("syntax: FMO macroblock map exceeds picture resource limit: %w", h264.ErrResourceLimit)
	}
	if len(mbMap) == 0 || uint64(current) >= uint64(len(mbMap)) {
		return 0, fmt.Errorf("syntax: invalid FMO macroblock address: %w", h264.ErrInvalidData)
	}
	group := mbMap[current]
	for next := current + 1; next < uint32(len(mbMap)); next++ {
		if mbMap[next] == group {
			return next, nil
		}
	}
	return uint32(len(mbMap)), nil
}
