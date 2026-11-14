// Package picture 提供编解码共享的图像工作缓冲。
//
// 与公共 Frame 的区别：
//   - Frame 表示解码器输出给调用方的图像，使用显示尺寸、紧凑布局；
//   - Picture 是内部工作缓冲，使用宏块对齐尺寸并带四周边界扩展区，
//     供运动补偿越界读取、边缘复制和 SIMD 越界读使用。
//
// 布局由公共 PixelFormat 显式指定；构造与池分桶共享相同的格式约束，
// 不以布局参数化冒充扩展 Profile 已接入编解码流水线。
package picture

import (
	"fmt"

	h264 "github.com/goavc/h264"
)

// DefaultBorder 是每个平面四周的扩展像素数。
//
// 需要保证的量：
//   - 亮度 6-tap 插值最多读取 x-2..x+3，即左右各 3 像素；
//   - 色度双线性最多读取 x-1..x+1；
//   - 后续 SIMD 实现可能按 16 像素块越界读。
//
// 32 像素同时满足 16 字节对齐与上述所有范围；代价是每行多 64 字节，
// 对 1080p 亮度平面约增加 6%，可接受。
const DefaultBorder = 32

// Plane 是一个带边界扩展区的平面。
//
// 坐标、Stride、Border均以采样为单位，不是字节。8-bit仅分配Data，
// 10-bit仅分配Data16（本机uint16值，公共Frame的LE序列化由输出层负责）。
// 有效区域与边界区共享缓冲；高位深访问用Sample/SetSample/Row16。
type Plane struct {
	Data     []byte
	Data16   []uint16
	Stride   int
	Width    int
	Height   int
	Border   int
	BitDepth int

	// fieldView 表示借用帧中奇偶行；仅原始帧可以更新边界及管理生命周期。
	fieldView bool
}

// Index 返回图像坐标 (x, y) 在对应采样缓冲中的下标。
//
// 调用方必须保证 -Border <= x < Width+Border、-Border <= y < Height+Border；
// 本函数不做边界检查（热路径调用），越界访问由 Alloc/Validate 保证不会发生。
func (p *Plane) Index(x, y int) int {
	return (y+p.Border)*p.Stride + x + p.Border
}

// Pix 读取8-bit图像坐标处的像素；高位深必须使用Sample。
func (p *Plane) Pix(x, y int) byte {
	return p.Data[p.Index(x, y)]
}

// SetPix 写入8-bit图像坐标处的像素；高位深必须使用SetSample。
func (p *Plane) SetPix(x, y int, v byte) {
	p.Data[p.Index(x, y)] = v
}

// Row 返回8-bit平面第 y 行有效区域的切片（不含左右边界）。
//
// 返回的切片与 Plane 共享底层数组；写入即写入图像。
func (p *Plane) Row(y int) []byte {
	off := p.Index(0, y)
	return p.Data[off : off+p.Width]
}

// FillBorder 用边缘像素填充四周扩展区。
//
// 先填充左右边界，再整行复制到上下边界，这样四个角自然得到最近角像素，
// 与 H.264 的“边缘像素重复”外推规则一致（8.4.2.1 的边缘扩展语义）。
// FieldView 借用的平面不能单独扩展边界；误用会 panic，必须由原图统一维护。
func (p *Plane) FillBorder() {
	if p.fieldView {
		panic("picture: cannot fill borders of a borrowed field view")
	}
	if p.BitDepth == 10 {
		fillBorder(p.Data16, p.Stride, p.Width, p.Height, p.Border)
	} else {
		fillBorder(p.Data, p.Stride, p.Width, p.Height, p.Border)
	}
}

// Picture 是一帧内部图像，格式与位深由 Format 唯一确定。
//
// 尺寸语义：Width/Height 是宏块对齐后的编码尺寸（16 的倍数）；
// 显示尺寸与裁剪由调用方在输出阶段处理。
type Picture struct {
	MBWidth  int
	MBHeight int
	Width    int
	Height   int
	Format   h264.PixelFormat
	Planes   [3]Plane

	pool *Pool
}

// New 按 format 分配编码尺寸为 mbWidth×mbHeight 个宏块的图像。
//
// 返回的 Picture 不归池管理；需要复用请使用 Pool.Get。
func New(mbWidth, mbHeight int, format h264.PixelFormat) (*Picture, error) {
	if err := validateLayout(mbWidth, mbHeight, format); err != nil {
		return nil, err
	}
	p := &Picture{
		MBWidth:  mbWidth,
		MBHeight: mbHeight,
		Width:    mbWidth * 16,
		Height:   mbHeight * 16,
		Format:   format,
	}
	for i := range p.Planes {
		w, h := format.PlaneDimensions(i, p.Width, p.Height)
		p.Planes[i] = allocPlane(w, h, DefaultBorder, format.BitDepth())
	}
	return p, nil
}

// allocPlane 分配一个带边界区的平面，stride 按 64 字节对齐。
//
// 对齐到 64 是为了让 SSE2/AVX2 的 16/32 字节访问天然对齐；Go 的切片本身
// 不保证 64 字节对齐，因此这里只保证行首相对分配起点对齐，而分配起点由
// Go 运行时按 8/16 字节对齐。后续 SIMD 内核必须使用非对齐加载指令，
// 或由 M8 的 buffer 包提供显式对齐分配。
func allocPlane(width, height, border, bitDepth int) Plane {
	// 构造器已限制编码尺寸≤8192；含边界的容量乘法在32位int内仍安全。
	// Stride以采样计数，但每行起点始终保持64字节的相对对齐。
	align := 64
	if bitDepth == 10 {
		align = 32
	}
	stride := (width + 2*border + align - 1) &^ (align - 1)
	p := Plane{Stride: stride, Width: width, Height: height, Border: border, BitDepth: bitDepth}
	if bitDepth == 10 {
		p.Data16 = make([]uint16, stride*(height+2*border))
	} else {
		p.Data = make([]byte, stride*(height+2*border))
	}
	return p
}

// Validate 检查平面几何是否自洽，主要用于测试与池取回后的防御性检查。
func (p *Picture) Validate() error {
	if p == nil {
		return fmt.Errorf("picture: nil picture: %w", h264.ErrInvalidData)
	}
	if err := validateLayout(p.MBWidth, p.MBHeight, p.Format); err != nil {
		return err
	}
	wantW, wantH := p.MBWidth*16, p.MBHeight*16
	if p.Width != wantW || p.Height != wantH {
		return fmt.Errorf("picture: coded size %dx%d, want %dx%d: %w",
			p.Width, p.Height, wantW, wantH, h264.ErrInvalidData)
	}
	for i := range p.Planes {
		pl := &p.Planes[i]
		w, h := p.Format.PlaneDimensions(i, p.Width, p.Height)
		if pl.Width != w || pl.Height != h || pl.BitDepth != p.Format.BitDepth() {
			return fmt.Errorf("picture: plane %d layout does not match %s: %w",
				i, p.Format, h264.ErrInvalidData)
		}
		maxInt := int(^uint(0) >> 1)
		if pl.Border < 0 || pl.Border > (maxInt-pl.Width)/2 || pl.Border > (maxInt-pl.Height)/2 {
			return fmt.Errorf("picture: plane %d border overflows: %w", i, h264.ErrInvalidData)
		}
		rows := pl.Height + 2*pl.Border
		if pl.Stride < pl.Width+2*pl.Border || pl.Stride > maxInt/rows {
			return fmt.Errorf("picture: plane %d invalid stride %d: %w", i, pl.Stride, h264.ErrInvalidData)
		}
		need := pl.Stride * rows
		if pl.fieldView {
			// 场视图切片起点位于原帧首行内部，最后一行不拥有完整 Stride。
			// Index 仅可读取该行的 Width+2*Border 个采样，不能要求行尾填充。
			need = pl.Stride*(rows-1) + pl.Width + 2*pl.Border
		}
		if pl.BitDepth == 10 {
			if len(pl.Data) != 0 || len(pl.Data16) < need {
				return fmt.Errorf("picture: plane %d needs %d uint16 samples: %w", i, need, h264.ErrInvalidData)
			}
		} else if len(pl.Data16) != 0 || len(pl.Data) < need {
			return fmt.Errorf("picture: plane %d needs %d byte samples: %w", i, need, h264.ErrInvalidData)
		}
	}
	return nil
}

// FillBorders 填充三个平面的边界扩展区。
//
// 每帧重建完成后、作为参考帧使用前调用一次。当前帧内部的帧内预测不需要
// 边界区，因此可以推迟到整帧解码结束，避免逐宏块填充的重复工作。
// 借用的 FieldView 不可调用本方法；与 Plane.FillBorder 一样会 panic。
func (p *Picture) FillBorders() {
	for i := range p.Planes {
		p.Planes[i].FillBorder()
	}
}

// Release 把图像归还给分配它的池；非池化图像由 GC 回收。
//
// 幂等：重复调用安全。归还后调用方不得再访问 Plane.Data。
func (p *Picture) Release() {
	if p == nil || p.pool == nil {
		return
	}
	pool := p.pool
	p.pool = nil
	pool.put(p)
}
