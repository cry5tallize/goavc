// Package picture 提供编解码共享的图像工作缓冲。
//
// 与公共 Frame 的区别：
//   - Frame 表示解码器输出给调用方的图像，使用显示尺寸、紧凑布局；
//   - Picture 是内部工作缓冲，使用宏块对齐尺寸并带四周边界扩展区，
//     供运动补偿越界读取、边缘复制和 SIMD 越界读使用。
//
// 本包只实现 M2 需要的 8-bit 4:2:0 布局。其他色度格式属于后续里程碑，
// 在这里显式返回错误，避免调用方误用未验证的内存布局。
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
// 坐标约定：x/y 是图像坐标（0 表示第一个有效像素），边界区坐标为负或
// 大于等于 Width/Height。Index 负责把图像坐标翻译到 Data 下标，因此
// 所有访问都必须经过 Pix/SetPix/Row，不能直接按图像坐标切片 Data。
type Plane struct {
	Data   []byte
	Stride int
	Width  int
	Height int
	Border int
}

// Index 返回图像坐标 (x, y) 在 Data 中的下标。
//
// 调用方必须保证 -Border <= x < Width+Border、-Border <= y < Height+Border；
// 本函数不做边界检查（热路径调用），越界访问由 Alloc/Validate 保证不会发生。
func (p *Plane) Index(x, y int) int {
	return (y+p.Border)*p.Stride + x + p.Border
}

// Pix 读取图像坐标处的像素。
func (p *Plane) Pix(x, y int) byte {
	return p.Data[p.Index(x, y)]
}

// SetPix 写入图像坐标处的像素。
func (p *Plane) SetPix(x, y int, v byte) {
	p.Data[p.Index(x, y)] = v
}

// Row 返回第 y 行有效区域的切片（不含左右边界）。
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
func (p *Plane) FillBorder() {
	b := p.Border
	if p.Width <= 0 || p.Height <= 0 {
		return
	}
	// 左右边界：每一行（含上下边界行）都复制该行最左/最右有效像素。
	for y := -b; y < p.Height+b; y++ {
		var left, right byte
		if y < 0 {
			left = p.Pix(0, 0)
			right = p.Pix(p.Width-1, 0)
		} else if y >= p.Height {
			left = p.Pix(0, p.Height-1)
			right = p.Pix(p.Width-1, p.Height-1)
		} else {
			left = p.Pix(0, y)
			right = p.Pix(p.Width-1, y)
		}
		rowStart := p.Index(-b, y)
		for x := 0; x < b; x++ {
			p.Data[rowStart+x] = left
		}
		rowEnd := p.Index(p.Width, y)
		for x := 0; x < b; x++ {
			p.Data[rowEnd+x] = right
		}
	}
	// 上下边界：把第 0 行/最后一行的完整行（含左右角）复制上去。
	rowLen := p.Width + 2*b
	first := p.Index(-b, 0)
	last := p.Index(-b, p.Height-1)
	for y := 0; y < b; y++ {
		copy(p.Data[p.Index(-b, -1-y):p.Index(-b, -1-y)+rowLen], p.Data[first:first+rowLen])
		copy(p.Data[p.Index(-b, p.Height+y):p.Index(-b, p.Height+y)+rowLen], p.Data[last:last+rowLen])
	}
}

// Picture 是一帧内部图像，包含 4:2:0 的三个平面。
//
// 尺寸语义：Width/Height 是宏块对齐后的编码尺寸（16 的倍数）；
// 显示尺寸与裁剪由调用方在输出阶段处理。
type Picture struct {
	MBWidth  int
	MBHeight int
	Width    int
	Height   int
	Planes   [3]Plane

	pool *Pool
}

// New 分配一张编码尺寸为 mbWidth×mbHeight 个宏块的 4:2:0 图像。
//
// 返回的 Picture 不归池管理；需要复用请使用 Pool.Get。
func New(mbWidth, mbHeight int) (*Picture, error) {
	if mbWidth <= 0 || mbHeight <= 0 {
		return nil, fmt.Errorf("picture: invalid macroblock dimensions %dx%d: %w",
			mbWidth, mbHeight, h264.ErrInvalidData)
	}
	// 上限与公共配置保持一致，防止由码流字段推导出巨大分配。
	if mbWidth > h264.MaxFrameWidth/16 || mbHeight > h264.MaxFrameHeight/16 {
		return nil, fmt.Errorf("picture: dimensions %dx%d MB exceed limit: %w",
			mbWidth, mbHeight, h264.ErrResourceLimit)
	}
	p := &Picture{
		MBWidth:  mbWidth,
		MBHeight: mbHeight,
		Width:    mbWidth * 16,
		Height:   mbHeight * 16,
	}
	// 4:2:0：色度宽高各减半；宏块对齐后仍是整数。
	cw, ch := mbWidth*8, mbHeight*8
	p.Planes[0] = allocPlane(p.Width, p.Height, DefaultBorder)
	p.Planes[1] = allocPlane(cw, ch, DefaultBorder)
	p.Planes[2] = allocPlane(cw, ch, DefaultBorder)
	return p, nil
}

// allocPlane 分配一个带边界区的平面，stride 按 64 字节对齐。
//
// 对齐到 64 是为了让 SSE2/AVX2 的 16/32 字节访问天然对齐；Go 的切片本身
// 不保证 64 字节对齐，因此这里只保证行首相对分配起点对齐，而分配起点由
// Go 运行时按 8/16 字节对齐。后续 SIMD 内核必须使用非对齐加载指令，
// 或由 M8 的 buffer 包提供显式对齐分配。
func allocPlane(width, height, border int) Plane {
	stride := width + 2*border
	stride = (stride + 63) &^ 63
	rows := height + 2*border
	return Plane{
		Data:   make([]byte, stride*rows),
		Stride: stride,
		Width:  width,
		Height: height,
		Border: border,
	}
}

// Validate 检查平面几何是否自洽，主要用于测试与池取回后的防御性检查。
func (p *Picture) Validate() error {
	if p == nil {
		return fmt.Errorf("picture: nil picture: %w", h264.ErrInvalidData)
	}
	if p.MBWidth <= 0 || p.MBHeight <= 0 {
		return fmt.Errorf("picture: invalid macroblock dimensions: %w", h264.ErrInvalidData)
	}
	wantW, wantH := p.MBWidth*16, p.MBHeight*16
	if p.Width != wantW || p.Height != wantH {
		return fmt.Errorf("picture: coded size %dx%d, want %dx%d: %w",
			p.Width, p.Height, wantW, wantH, h264.ErrInvalidData)
	}
	dims := [3][2]int{{p.Width, p.Height}, {p.MBWidth * 8, p.MBHeight * 8}, {p.MBWidth * 8, p.MBHeight * 8}}
	for i := range p.Planes {
		pl := &p.Planes[i]
		if pl.Width != dims[i][0] || pl.Height != dims[i][1] {
			return fmt.Errorf("picture: plane %d size %dx%d, want %dx%d: %w",
				i, pl.Width, pl.Height, dims[i][0], dims[i][1], h264.ErrInvalidData)
		}
		if pl.Border < 0 || pl.Stride < pl.Width+2*pl.Border {
			return fmt.Errorf("picture: plane %d stride %d too small: %w",
				i, pl.Stride, h264.ErrInvalidData)
		}
		need := pl.Stride * (pl.Height + 2*pl.Border)
		if len(pl.Data) < need {
			return fmt.Errorf("picture: plane %d buffer %d bytes, need %d: %w",
				i, len(pl.Data), need, h264.ErrInvalidData)
		}
	}
	return nil
}

// FillBorders 填充三个平面的边界扩展区。
//
// 每帧重建完成后、作为参考帧使用前调用一次。当前帧内部的帧内预测不需要
// 边界区，因此可以推迟到整帧解码结束，避免逐宏块填充的重复工作。
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
