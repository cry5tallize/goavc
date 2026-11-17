package decoder

import (
	"github.com/cry5tallize/goavc/internal/entropy/cabac"
	"github.com/cry5tallize/goavc/internal/transform"
)

// 规范固定表只在初始化校验一次，描述符拥有副本；未知/自定义scan继续通用入口。
func mustCABACScan(scan []int) cabac.ResidualScan {
	p, err := cabac.PrepareResidualScan(scan)
	if err != nil {
		panic(err)
	}
	return p
}

var preparedCABACScans = [7]cabac.ResidualScan{
	mustCABACScan(transform.Zigzag4x4[:]), mustCABACScan(transform.FieldScan4x4[:]),
	mustCABACScan(transform.Zigzag4x4[1:]), mustCABACScan(transform.FieldScan4x4[1:]),
	mustCABACScan(transform.LumaDCScan[:]), mustCABACScan(transform.FieldLumaDCScan[:]),
	mustCABACScan(transform.DC2x2Scan[:]),
}

func fixedCABACScan(scan []int) *cabac.ResidualScan {
	if len(scan) == 0 {
		return nil
	}
	p := &scan[0]
	switch len(scan) {
	case 16:
		switch p {
		case &transform.Zigzag4x4[0]:
			return &preparedCABACScans[0]
		case &transform.FieldScan4x4[0]:
			return &preparedCABACScans[1]
		case &transform.LumaDCScan[0]:
			return &preparedCABACScans[4]
		case &transform.FieldLumaDCScan[0]:
			return &preparedCABACScans[5]
		}
	case 15:
		if p == &transform.Zigzag4x4[1] {
			return &preparedCABACScans[2]
		}
		if p == &transform.FieldScan4x4[1] {
			return &preparedCABACScans[3]
		}
	case 4:
		if p == &transform.DC2x2Scan[0] {
			return &preparedCABACScans[6]
		}
	}
	return nil
}
