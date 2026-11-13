package prediction

import (
	"fmt"

	h264 "github.com/goavc/h264"
)

// 16×16 与色度 8×8 的模式编号（规范表 8-3 / 8-4）。
const (
	Intra16x16Vertical   = 0
	Intra16x16Horizontal = 1
	Intra16x16DC         = 2
	Intra16x16Plane      = 3
)

// IntraBlockSamples 是 16×16 或 8×8 预测所需的邻域样本。
//
// Top/Left 长度分别为 16 或 8；Top[-1] 与 Left[-1] 由 TopLeft 给出。
type IntraBlockSamples struct {
	Top     []byte
	Left    []byte
	TopLeft byte
}

// Intra16x16 计算 16×16 帧内预测。
func Intra16x16(dst []byte, stride, mode int, s IntraBlockSamples, topAvail, leftAvail bool) error {
	if len(s.Top) < 16 || len(s.Left) < 16 {
		return fmt.Errorf("prediction: intra16x16 needs 16 top and left samples")
	}
	return intraBlock(dst, stride, 16, mode, s, topAvail, leftAvail, true)
}

// Chroma8x8 计算 8×8 色度帧内预测（模式 0..3）。
func Chroma8x8(dst []byte, stride, mode int, s IntraBlockSamples, topAvail, leftAvail bool) error {
	if len(s.Top) < 8 || len(s.Left) < 8 {
		return fmt.Errorf("prediction: chroma8x8 needs 8 top and left samples")
	}
	return intraBlock(dst, stride, 8, mode, s, topAvail, leftAvail, false)
}

// intraBlock 是 16×16/8×8 的共享实现。
//
// 两者除尺寸与 Plane 系数外公式相同；luma16 仅用于选择 5/17 的 Plane
// 归一化系数。
func intraBlock(dst []byte, stride, n, mode int, s IntraBlockSamples, topAvail, leftAvail, luma16 bool) error {
	if len(dst) < (n-1)*stride+n {
		return fmt.Errorf("prediction: intra block destination too small: %w", h264.ErrInvalidData)
	}
	switch mode {
	case Intra16x16Vertical:
		if !topAvail {
			return fmt.Errorf("prediction: intra block vertical without top: %w", h264.ErrInvalidData)
		}
		for y := 0; y < n; y++ {
			copy(dst[y*stride:y*stride+n], s.Top[:n])
		}
	case Intra16x16Horizontal:
		if !leftAvail {
			return fmt.Errorf("prediction: intra block horizontal without left: %w", h264.ErrInvalidData)
		}
		for y := 0; y < n; y++ {
			row := dst[y*stride : y*stride+n]
			for x := range row {
				row[x] = s.Left[y]
			}
		}
	case Intra16x16DC:
		var dc int
		switch {
		case topAvail && leftAvail:
			sum := 0
			for i := 0; i < n; i++ {
				sum += int(s.Top[i]) + int(s.Left[i])
			}
			dc = (sum + n) >> (log2(n) + 1)
		case topAvail:
			sum := 0
			for i := 0; i < n; i++ {
				sum += int(s.Top[i])
			}
			dc = (sum + n/2) / n
		case leftAvail:
			sum := 0
			for i := 0; i < n; i++ {
				sum += int(s.Left[i])
			}
			dc = (sum + n/2) / n
		default:
			dc = 128
		}
		for y := 0; y < n; y++ {
			row := dst[y*stride : y*stride+n]
			for x := range row {
				row[x] = byte(dc)
			}
		}
	case Intra16x16Plane:
		if !topAvail || !leftAvail {
			return fmt.Errorf("prediction: plane mode requires top and left: %w", h264.ErrInvalidData)
		}
		intraPlane(dst, stride, n, s, luma16)
	default:
		return fmt.Errorf("prediction: invalid intra block mode %d: %w", mode, h264.ErrInvalidData)
	}
	return nil
}

// intraPlane 实现 16×16/8×8 Plane 预测（8.3.3.4）。
//
// 两份公式仅在求和范围与 5/17 系数上不同：
//
//	16×16: b = (5H+32)>>6
//	 8×8 : b = (17H+16)>>5
//
// top[-1]/left[-1] 都取左上角样本。
func intraPlane(dst []byte, stride, n int, s IntraBlockSamples, luma16 bool) {
	half := n / 2
	var hSum, vSum int
	for i := 0; i < half; i++ {
		w := i + 1
		topHi := int(s.Top[half+i])
		leftHi := int(s.Left[half+i])
		var topLo, leftLo int
		if i == half-1 {
			// 规范中的 top[-1]/left[-1] 就是左上角样本。
			topLo, leftLo = int(s.TopLeft), int(s.TopLeft)
		} else {
			topLo, leftLo = int(s.Top[half-2-i]), int(s.Left[half-2-i])
		}
		hSum += w * (topHi - topLo)
		vSum += w * (leftHi - leftLo)
	}
	var b, c int
	if luma16 {
		b = (5*hSum + 32) >> 6
		c = (5*vSum + 32) >> 6
	} else {
		b = (17*hSum + 16) >> 5
		c = (17*vSum + 16) >> 5
	}
	a := 16 * (int(s.Top[n-1]) + int(s.Left[n-1]))
	for y := 0; y < n; y++ {
		row := dst[y*stride : y*stride+n]
		for x := 0; x < n; x++ {
			v := (a + b*(x-(half-1)) + c*(y-(half-1)) + 16) >> 5
			row[x] = clip8(v)
		}
	}
}

func clip8(v int) byte {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v)
}

func log2(n int) int {
	switch n {
	case 8:
		return 3
	case 16:
		return 4
	default:
		return 0
	}
}
