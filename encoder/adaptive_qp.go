package encoder

import (
	"fmt"
	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/internal/picture"
)

// WithAdaptiveQP 按亮度活动度对基准 QP 加减 strength（0..6，0 关闭）。
// 平坦块减 QP 保留细节，高活动块加 QP；不与显式宏块回调或 PCM 通道共用。
func WithAdaptiveQP(strength int) Option {
	return func(e *Encoder) error {
		if strength < 0 || strength > 6 {
			return fmt.Errorf("encoder: AQ strength %d: %w", strength, h264.ErrInvalidData)
		}
		e.adaptiveQP = strength
		return nil
	}
}

func activityQP(p *picture.Plane, x, y, qp, strength int) int {
	if strength == 0 {
		return qp
	}
	return activityAdjustedQP(qp, strength, activityClass(p, x, y), p.BitDepth)
}

// activityClass保持原8-bit归一化和两个严格阈值；仅与源采样有关。
func activityClass(p *picture.Plane, x, y int) int8 {
	var sum, squares int64
	for j := 0; j < 16; j++ {
		for i := 0; i < 16; i++ {
			v := int64(p.Sample(x+i, y+j)) >> (p.BitDepth - 8)
			sum += v
			squares += v * v
		}
	}
	variance := (squares*256 - sum*sum) / (256 * 256)
	if variance < 64 {
		return -1
	}
	if variance > 1024 {
		return 1
	}
	return 0
}

func activityAdjustedQP(qp, strength int, class int8, bitDepth int) int {
	return min(51, max(-6*(bitDepth-8), qp+int(class)*strength))
}
