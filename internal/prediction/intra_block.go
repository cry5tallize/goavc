package prediction

import (
	"fmt"

	h264 "github.com/cry5tallize/goavc"
)

// 16×16 与色度 8×8/8×16 的模式编号（规范表 8-3 / 8-4）。
const (
	Intra16x16Vertical   = 0
	Intra16x16Horizontal = 1
	Intra16x16DC         = 2
	Intra16x16Plane      = 3
)

// IntraBlockSamples 是 16×16 或色度 8×8/8×16 预测所需的邻域样本。
//
// Top/Left 至少覆盖块宽/高；Top[-1] 与 Left[-1] 由 TopLeft 给出。
type IntraBlockSamples = intraBlockSamples[byte]

// IntraBlockSamples16 是 16×16 或色度 8×8/8×16 预测的高位深邻域。
type IntraBlockSamples16 = intraBlockSamples[uint16]

type intraBlockSamples[T sample] struct {
	Top     []T
	Left    []T
	TopLeft T
}

// Intra16x16 计算 16×16 帧内预测。
// 保留具名跨包边界：导入泛型shape的保守逃逸会把调用方邻域切片的局部数组移到堆上。
//
//go:noinline
func Intra16x16(dst []byte, stride, mode int, s IntraBlockSamples, topAvail, leftAvail bool) error {
	return intra16x16(dst, stride, mode, s, topAvail, leftAvail, 8)
}

// Intra16x16High 保留高位深 Plane 计算与位深相关的 Clip1。
func Intra16x16High(dst []uint16, stride, mode int, s IntraBlockSamples16, topAvail, leftAvail bool, bitDepth int) error {
	if err := checkBitDepth(bitDepth); err != nil {
		return err
	}
	return intra16x16(dst, stride, mode, s, topAvail, leftAvail, bitDepth)
}

func intra16x16[T sample](dst []T, stride, mode int, s intraBlockSamples[T], topAvail, leftAvail bool, bitDepth int) error {
	if len(s.Top) < 16 || len(s.Left) < 16 {
		return fmt.Errorf("prediction: intra16x16 needs 16 top and left samples")
	}
	return intraBlock(dst, stride, 16, 16, mode, s, topAvail, leftAvail, bitDepth)
}

// Chroma8x8 计算 8×8 色度帧内预测。
//
// 注意 chroma 的模式编号与 16×16 不同（规范表 8-4）：
// 0=DC、1=水平、2=垂直、3=Plane。这里先做映射再复用 intraBlock。
func Chroma8x8(dst []byte, stride, mode int, s IntraBlockSamples, topAvail, leftAvail bool) error {
	return chroma8x8(dst, stride, mode, s, topAvail, leftAvail, 8)
}

// Chroma8x8High 使用四象限 DC 和 4:2:0 Plane 预测。
func Chroma8x8High(dst []uint16, stride, mode int, s IntraBlockSamples16, topAvail, leftAvail bool, bitDepth int) error {
	if err := checkBitDepth(bitDepth); err != nil {
		return err
	}
	return chroma8x8(dst, stride, mode, s, topAvail, leftAvail, bitDepth)
}

// Chroma422 计算 4:2:2 的 8×16 色度帧内预测（Spec: 8.3.4）。
// Top 至少 8 个样本，Left 至少 16 个样本；跨度以采样计。
// 与Intra16x16保持同样的具名跨包边界，避免导入shape使调用方局部邻域逃逸。
//
//go:noinline
func Chroma422(dst []byte, stride, mode int, s IntraBlockSamples, topAvail, leftAvail bool) error {
	return chromaBlock(dst, stride, 16, mode, s, topAvail, leftAvail, 8)
}

// Chroma422High 与 Chroma422 使用同一内核；bitDepth 决定 DC 中点和 Clip1C。
func Chroma422High(dst []uint16, stride, mode int, s IntraBlockSamples16, topAvail, leftAvail bool, bitDepth int) error {
	if err := checkBitDepth(bitDepth); err != nil {
		return err
	}
	return chromaBlock(dst, stride, 16, mode, s, topAvail, leftAvail, bitDepth)
}

func chroma8x8[T sample](dst []T, stride, mode int, s intraBlockSamples[T], topAvail, leftAvail bool, bitDepth int) error {
	return chromaBlock(dst, stride, 8, mode, s, topAvail, leftAvail, bitDepth)
}

func chromaBlock[T sample](dst []T, stride, height, mode int, s intraBlockSamples[T], topAvail, leftAvail bool, bitDepth int) error {
	if len(s.Top) < 8 || len(s.Left) < height {
		return fmt.Errorf("prediction: chroma8x%d needs 8 top and %d left samples: %w", height, height, h264.ErrInvalidData)
	}
	var internal int
	switch mode {
	case 0:
		internal = Intra16x16DC
	case 1:
		internal = Intra16x16Horizontal
	case 2:
		internal = Intra16x16Vertical
	case 3:
		internal = Intra16x16Plane
	default:
		return fmt.Errorf("prediction: invalid chroma mode %d: %w", mode, h264.ErrInvalidData)
	}
	return intraBlock(dst, stride, 8, height, internal, s, topAvail, leftAvail, bitDepth)
}

// intraBlock 共享 16×16 亮度和 8×8/8×16 色度预测，尺寸决定 DC 分区与 Plane 系数。
func intraBlock[T sample](dst []T, stride, width, height, mode int, s intraBlockSamples[T], topAvail, leftAvail bool, bitDepth int) error {
	if stride < width || len(dst) < (height-1)*stride+width {
		return fmt.Errorf("prediction: intra block destination too small: %w", h264.ErrInvalidData)
	}
	switch mode {
	case Intra16x16Vertical:
		if !topAvail {
			return fmt.Errorf("prediction: intra block vertical without top: %w", h264.ErrInvalidData)
		}
		for y := 0; y < height; y++ {
			copy(dst[y*stride:y*stride+width], s.Top[:width])
		}
	case Intra16x16Horizontal:
		if !leftAvail {
			return fmt.Errorf("prediction: intra block horizontal without left: %w", h264.ErrInvalidData)
		}
		for y := 0; y < height; y++ {
			row := dst[y*stride : y*stride+width]
			for x := range row {
				row[x] = s.Left[y]
			}
		}
	case Intra16x16DC:
		if width == 8 {
			intraChromaDC(dst, stride, height, s, topAvail, leftAvail, bitDepth)
			break
		}
		var dc int
		switch {
		case topAvail && leftAvail:
			sum := 0
			for i := 0; i < width; i++ {
				sum += int(s.Top[i]) + int(s.Left[i])
			}
			dc = (sum + width) >> (log2(width) + 1)
		case topAvail:
			sum := 0
			for i := 0; i < width; i++ {
				sum += int(s.Top[i])
			}
			dc = (sum + width/2) / width
		case leftAvail:
			sum := 0
			for i := 0; i < height; i++ {
				sum += int(s.Left[i])
			}
			dc = (sum + height/2) / height
		default:
			dc = 1 << (bitDepth - 1)
		}
		for y := 0; y < height; y++ {
			row := dst[y*stride : y*stride+width]
			for x := range row {
				row[x] = T(dc)
			}
		}
	case Intra16x16Plane:
		if !topAvail || !leftAvail {
			return fmt.Errorf("prediction: plane mode requires top and left: %w", h264.ErrInvalidData)
		}
		intraPlane(dst, stride, width, height, s, bitDepth)
	default:
		return fmt.Errorf("prediction: invalid intra block mode %d: %w", mode, h264.ErrInvalidData)
	}
	return nil
}

// intraChromaDC 按 4×4 分区计算 DC（Spec: 8.3.4.1）。除左上区外，
// 首行右区只取上方、其余行左区只取左方，其余区平均对应的上方与左方。
func intraChromaDC[T sample](dst []T, stride, height int, s intraBlockSamples[T], topAvail, leftAvail bool, bitDepth int) {
	var top [2]int
	if topAvail {
		for x := 0; x < 8; x++ {
			top[x/4] += int(s.Top[x])
		}
	}
	for by := 0; by < height; by += 4 {
		left := 0
		if leftAvail {
			for y := by; y < by+4; y++ {
				left += int(s.Left[y])
			}
		}
		for bx := 0; bx < 2; bx++ {
			dc := 1 << (bitDepth - 1)
			switch {
			case topAvail && leftAvail:
				switch {
				case by == 0 && bx == 1:
					dc = (top[bx] + 2) >> 2
				case by != 0 && bx == 0:
					dc = (left + 2) >> 2
				default:
					dc = (top[bx] + left + 4) >> 3
				}
			case topAvail:
				dc = (top[bx] + 2) >> 2
			case leftAvail:
				dc = (left + 2) >> 2
			}
			for y := by; y < by+4; y++ {
				row := dst[y*stride+bx*4 : y*stride+bx*4+4]
				for x := range row {
					row[x] = T(dc)
				}
			}
		}
	}
}

// intraPlane 的两个轴分别归一化，8×16 的水平使用 17/32、垂直使用 5/64。
// Top[-1]/Left[-1] 均为 TopLeft（Spec: 8.3.3.4、8.3.4.4）。
func intraPlane[T sample](dst []T, stride, width, height int, s intraBlockSamples[T], bitDepth int) {
	b := intraPlaneSlope(s.Top[:width], s.TopLeft)
	c := intraPlaneSlope(s.Left[:height], s.TopLeft)
	a := 16 * (int(s.Top[width-1]) + int(s.Left[height-1]))
	for y := 0; y < height; y++ {
		row := dst[y*stride : y*stride+width]
		for x := range row {
			v := (a + b*(x-(width/2-1)) + c*(y-(height/2-1)) + 16) >> 5
			row[x] = clipSample[T](v, bitDepth)
		}
	}
}

func intraPlaneSlope[T sample](edge []T, topLeft T) int {
	half := len(edge) / 2
	sum := 0
	for i := 0; i < half; i++ {
		lo := int(topLeft)
		if i != half-1 {
			lo = int(edge[half-2-i])
		}
		sum += (i + 1) * (int(edge[half+i]) - lo)
	}
	if len(edge) == 16 {
		return (5*sum + 32) >> 6
	}
	return (17*sum + 16) >> 5
}

// sample 的实例直接读取原始采样缓冲，8-bit 路径不分配转换数组。
type sample interface {
	~uint8 | ~uint16
}

func clipSample[T sample](v, bitDepth int) T {
	if v < 0 {
		return 0
	}
	max := (1 << bitDepth) - 1
	if v > max {
		return T(max)
	}
	return T(v)
}

func checkBitDepth(bitDepth int) error {
	if bitDepth < 8 || bitDepth > 16 {
		return fmt.Errorf("prediction: invalid bit depth %d: %w", bitDepth, h264.ErrInvalidData)
	}
	return nil
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
