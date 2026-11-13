package encoder

import (
	"fmt"
	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/internal/picture"
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
	var sum, squares int64
	for j := 0; j < 16; j++ {
		for i := 0; i < 16; i++ {
			v := int64(p.Pix(x+i, y+j))
			sum += v
			squares += v * v
		}
	}
	variance := (squares*256 - sum*sum) / (256 * 256)
	delta := 0
	if variance < 64 {
		delta = -strength
	} else if variance > 1024 {
		delta = strength
	}
	return min(51, max(0, qp+delta))
}
