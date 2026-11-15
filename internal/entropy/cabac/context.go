package cabac

import (
	"fmt"
	h264 "github.com/cry5tallize/goavc"
)

//go:generate go run gen_tables.go

// ContextCount 覆盖 Main/High/High422 的上下文，不承担 444 模型初始化开销。
const ContextCount = 460

// ContextCount444 覆盖 High 4:4:4 Predictive 的全部上下文（包括8×8 CBF）。
const ContextCount444 = 1024

// InitContexts 实现 Spec: 9.3.1.1。intra 选择 I 表，否则 idc 选择 P/B 的
// 三张表；QP 在初始化公式内裁到 0..51，不改变 slice 的实际 QP。
func InitContexts(intra bool, idc uint32, qp int) ([ContextCount]Context, error) {
	var out [ContextCount]Context
	if idc > 2 && !intra {
		return out, fmt.Errorf("cabac: init_idc %d: %w", idc, h264.ErrInvalidData)
	}
	initContexts(out[:], intra, idc, qp)
	return out, nil
}

// InitContexts444 实现相同的初始化公式，但为全部 1024 个模型建立状态。
func InitContexts444(intra bool, idc uint32, qp int) ([ContextCount444]Context, error) {
	var out [ContextCount444]Context
	if idc > 2 && !intra {
		return out, fmt.Errorf("cabac: init_idc %d: %w", idc, h264.ErrInvalidData)
	}
	initContexts(out[:], intra, idc, qp)
	return out, nil
}

// out 借用调用方数组；旧接口只循环460项，既不分配也不复制扩展模型。
func initContexts(out []Context, intra bool, idc uint32, qp int) {
	index := 0
	if !intra {
		index = int(idc) + 1
	}
	qp = max(0, min(51, qp))
	for i := range out {
		mn := initMN[index][i]
		pre := max(1, min(126, ((int(mn[0])*qp)>>4)+int(mn[1])))
		if pre <= 63 {
			out[i] = Context{State: uint8(63 - pre)}
		} else {
			out[i] = Context{State: uint8(pre - 64), MPS: 1}
		}
	}
}
