package encoder

import (
	"fmt"
	h264 "github.com/goavc/h264"
)

// WithScalingMatrices4x4 为High配置六个光栅矩阵：intra Y/Cb/Cr、inter Y/Cb/Cr。
// 每项必须为1..255；按值复制。DC路径使用对应矩阵的首项，不单独引入DC矩阵。
func WithScalingMatrices4x4(matrices [6][16]uint8) Option {
	return func(e *Encoder) error {
		if e.cfg.Profile != h264.ProfileHigh && e.cfg.Profile != h264.ProfileHigh10 {
			return fmt.Errorf("encoder: custom scaling matrices require High: %w", h264.ErrUnsupported)
		}
		for _, matrix := range matrices {
			for _, value := range matrix {
				if value == 0 {
					return fmt.Errorf("encoder: zero 4x4 scaling entry: %w", h264.ErrInvalidData)
				}
			}
		}
		copy := matrices
		e.scaling4 = &copy
		return nil
	}
}

func (s *frameState) matrix4(intra bool, comp int) *[16]uint8 {
	if s.scaling4 == nil {
		return nil
	}
	if !intra {
		comp += 3
	}
	return &s.scaling4[comp]
}

func scalingDC(matrix *[16]uint8) uint8 {
	if matrix == nil {
		return 16
	}
	return matrix[0]
}
