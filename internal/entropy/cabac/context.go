package cabac

import (
	"fmt"
	h264 "github.com/goavc/h264"
)

//go:generate go run gen_tables.go

// ContextCount 覆盖 Main/High 8-bit 4:2:0 使用的上下文；4:4:4 扩展属于 M7。
const ContextCount = 460

// InitContexts 实现 Spec: 9.3.1.1。intra 选择 I 表，否则 idc 选择 P/B 的
// 三张表；QP 在初始化公式内裁到 0..51，不改变 slice 的实际 QP。
func InitContexts(intra bool, idc uint32, qp int) ([ContextCount]Context, error) {
	var out [ContextCount]Context
	if idc > 2 && !intra {
		return out, fmt.Errorf("cabac: init_idc %d: %w", idc, h264.ErrInvalidData)
	}
	index := int(idc) + 1
	if intra {
		index = 0
	}
	qp = max(0, min(51, qp))
	for i, mn := range initMN[index] {
		pre := max(1, min(126, ((int(mn[0])*qp)>>4)+int(mn[1])))
		if pre <= 63 {
			out[i] = Context{State: uint8(63 - pre)}
		} else {
			out[i] = Context{State: uint8(pre - 64), MPS: 1}
		}
	}
	return out, nil
}
