package syntax

import (
	"fmt"

	h264 "github.com/cry5tallize/goavc"
)

// 参数集 writer 在输出第一个比特前调用以下预检，防止最后一个列表非法时
// 留下半个 SPS/PPS。只检查当前语法实际携带的列表，忽略未出现字段的缓存。
func validateSPSScalingLists(s *SPS) error {
	if s == nil {
		return fmt.Errorf("syntax: scaling validation requires SPS: %w", h264.ErrInvalidData)
	}
	if !s.SeqScalingMatrixPresentFlag {
		return nil
	}
	count := 8
	if s.effectiveChromaFormatIDC() == 3 {
		count = 12
	}
	return validateParameterScalingLists(&s.SeqScalingListPresentFlag, &s.SeqScalingList, count)
}

func validatePPSScalingLists(s *SPS, p *PPS) error {
	if s == nil || p == nil {
		return fmt.Errorf("syntax: scaling validation requires SPS/PPS: %w", h264.ErrInvalidData)
	}
	if !p.PicScalingMatrixPresentFlag {
		return nil
	}
	return validateParameterScalingLists(&p.PicScalingListPresentFlag, &p.PicScalingList, scalingListCount(s, p.Transform8x8ModeFlag))
}

func validateParameterScalingLists(present *[12]bool, lists *[12]*ScalingList, count int) error {
	for index := range count {
		if !present[index] {
			continue
		}
		size := 4
		if index >= 6 {
			size = 8
		}
		list := lists[index]
		if list == nil || list.Size != size {
			return fmt.Errorf("syntax: scaling list[%d] requires %dx%d syntax: %w", index, size, size, h264.ErrInvalidData)
		}
		if err := list.validateSyntax(); err != nil {
			return fmt.Errorf("syntax: scaling list[%d]: %w", index, err)
		}
	}
	return nil
}
