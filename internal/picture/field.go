package picture

import (
	"fmt"

	h264 "github.com/goavc/h264"
)

// FieldView 返回 src 的上场（bottom=false）或下场（bottom=true）逻辑视图。
// 不分配或复制像素；对视图 (x,y) 的写入直接修改 src 的 (x,2*y+parity)。
// src 必须是有效的整帧且 MBHeight 为偶数，六种支持的像素格式均保留原位深。
//
// 视图的生命周期完全依赖 src：src.Release 后视图失效。视图不归池管理，
// Release 不归还原帧；禁止在视图或其平面调用 FillBorders/FillBorder，边界由
// 原帧统一维护。返回的 Border 是原边界的一半，调用者不得再假定为 DefaultBorder。
func FieldView(src *Picture, bottom bool) (Picture, error) {
	if err := src.Validate(); err != nil {
		return Picture{}, err
	}
	if src.MBHeight%2 != 0 {
		return Picture{}, fmt.Errorf("picture: field view requires even macroblock height: %w", h264.ErrInvalidData)
	}
	parity := 0
	if bottom {
		parity = 1
	}
	view := *src
	view.pool = nil
	view.MBHeight /= 2
	view.Height /= 2
	for i := range view.Planes {
		orig := &src.Planes[i]
		if orig.fieldView {
			return Picture{}, fmt.Errorf("picture: cannot take a field of a field view: %w", h264.ErrInvalidData)
		}
		// Validate 已证明原缓冲大小及乘法安全；加倍后的视图也必须可用 int 索引。
		if orig.Stride > int(^uint(0)>>1)/2 {
			return Picture{}, fmt.Errorf("picture: field stride overflows: %w", h264.ErrInvalidData)
		}
		pl := &view.Planes[i]
		pl.Height /= 2
		pl.Border /= 2
		pl.Stride *= 2
		pl.fieldView = true
		// Index 同时把 Border 用在横纵两个方向。纵向步长加倍后，减半 Border
		// 恢复原有效首行，再移动横向起点保持原有效首列。奇数 Border 的余数
		// 仍须补一个原 stride；默认32边界时即 offset=16+parity*orig.Stride。
		offset := (orig.Border-2*pl.Border+parity)*orig.Stride + orig.Border - pl.Border
		if pl.BitDepth == 10 {
			pl.Data16 = orig.Data16[offset:]
		} else {
			pl.Data = orig.Data[offset:]
		}
	}
	return view, nil
}
