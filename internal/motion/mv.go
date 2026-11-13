// Package motion 提供运动矢量网格与 MV 预测。
//
// 运动估计（搜索）只属于编码器；本包实现的是解码器与编码器本地重建
// 共同需要的 MV 存储、邻块读取与中值预测（规范 8.4.1.3）。解码器在
// 每个宏块重建时写入网格，后续宏块与去块滤波从网格读取。
package motion

import (
	"fmt"

	h264 "github.com/goavc/h264"
)

// MaxAbsMV 是允许的 MV 分量绝对值上限（四分之一像素）。
//
// 规范由 level 限制 MV 范围，最大约 2048 像素；这里放宽到 8192 像素
// 以覆盖任何合法码流，同时为坐标运算留出安全余量。超过上限视为
// 非法输入，而不是让 int 溢出后在别处崩溃。
const MaxAbsMV = 8192 * 4

// MV 是一个运动矢量，单位是 1/4 像素。
type MV struct {
	X int32
	Y int32
}

// IsZero 判断两个分量是否都为零。
func (m MV) IsZero() bool { return m.X == 0 && m.Y == 0 }

// Validate 检查 MV 是否在允许范围内。
func (m MV) Validate() error {
	if m.X < -MaxAbsMV || m.X > MaxAbsMV || m.Y < -MaxAbsMV || m.Y > MaxAbsMV {
		return fmt.Errorf("motion: mv (%d,%d) exceeds limit %d: %w", m.X, m.Y, MaxAbsMV, h264.ErrInvalidData)
	}
	return nil
}

// Grid 存储整帧按 4×4 块划分的 MV 与参考索引。
//
// 坐标：bx/by 是从图像左上角开始的 4×4 块坐标（不是宏块内坐标）。
// Ref < 0 表示该块没有可用的 list0 预测（帧内块或尚未解码）。
type Grid struct {
	MBWidth    int
	MBHeight   int
	BlockW     int
	BlockH     int
	MV         []MV
	Ref        []int8
	valid      []bool
	sliceStart int
}

// NewGrid 分配一张与图像尺寸匹配的网格。
func NewGrid(mbWidth, mbHeight int) *Grid {
	bw, bh := mbWidth*4, mbHeight*4
	g := &Grid{
		MBWidth:  mbWidth,
		MBHeight: mbHeight,
		BlockW:   bw,
		BlockH:   bh,
		MV:       make([]MV, bw*bh),
		Ref:      make([]int8, bw*bh),
		valid:    make([]bool, bw*bh),
	}
	g.Reset()
	return g
}

func (g *Grid) index(bx, by int) int { return by*g.BlockW + bx }

// Available 判断块坐标是否在图像内且已有参考索引。
func (g *Grid) Available(bx, by int) bool {
	if g == nil || bx < 0 || by < 0 || bx >= g.BlockW || by >= g.BlockH {
		return false
	}
	return (by/4)*g.MBWidth+bx/4 >= g.sliceStart && g.valid[g.index(bx, by)]
}

// BeginSlice 限制预测邻块到当前 slice；调用 BeginSlice(0) 可恢复整帧
// 视图供去块使用。M2 的 slice 按宏块地址递增且不重叠。
func (g *Grid) BeginSlice(firstMB int) { g.sliceStart = firstMB }

// Get 返回块的 MV 与参考索引；不可用时 ok=false。
func (g *Grid) Get(bx, by int) (MV, int8, bool) {
	if !g.Available(bx, by) {
		return MV{}, 0, false
	}
	i := g.index(bx, by)
	return g.MV[i], g.Ref[i], true
}

// SetRect 把 bx/by 起、宽高为 w/h（4×4 块数）的区域写为同一个 MV/ref。
func (g *Grid) SetRect(bx, by, w, h int, mv MV, ref int8) {
	for y := by; y < by+h; y++ {
		if y < 0 || y >= g.BlockH {
			continue
		}
		for x := bx; x < bx+w; x++ {
			if x < 0 || x >= g.BlockW {
				continue
			}
			i := g.index(x, y)
			g.MV[i] = mv
			g.Ref[i] = ref
			g.valid[i] = true
		}
	}
}

// Reset 清空网格，供新帧复用。
func (g *Grid) Reset() {
	g.sliceStart = 0
	for i := range g.MV {
		g.MV[i] = MV{}
		g.Ref[i] = -1
		g.valid[i] = false
	}
}

// Predict 计算普通 P 宏块分区的 MV 预测（8.4.1.3）。
//
// bx/by 是分区左上角 4×4 块坐标，partW 是分区宽度（1/2/4 个 4×4 块）。
// ref 是当前分区的参考索引（M2 恒为 0）。返回的 MV 未裁剪到图像边界，
// 边界裁剪在运动补偿阶段完成。
func Predict(g *Grid, bx, by, partW int, ref int8) MV {
	a, aRef, aOK := g.Get(bx-1, by)
	b, bRef, bOK := g.Get(bx, by-1)
	c, cRef, cOK := diagonal(g, bx, by, partW)

	match := 0
	if aOK && aRef == ref {
		match++
	}
	if bOK && bRef == ref {
		match++
	}
	if cOK && cRef == ref {
		match++
	}
	switch {
	case match > 1:
		return median(a, b, c)
	case match == 1:
		if aOK && aRef == ref {
			return a
		}
		if bOK && bRef == ref {
			return b
		}
		return c
	case !bOK && !cOK && aOK:
		return a
	default:
		return median(a, b, c)
	}
}

// Predict16x8 返回 16×8 分区的 MV 预测。
//
// 上分区若上方块参考一致则直接取 B；下分区若左侧块参考一致则取 A；
// 否则退回通用中值预测（与 FFmpeg 的 pred_16x8_motion 一致）。
func Predict16x8(g *Grid, bx, by int, lower bool, ref int8) MV {
	if !lower {
		if b, bRef, ok := g.Get(bx, by-1); ok && bRef == ref {
			return b
		}
	} else {
		if a, aRef, ok := g.Get(bx-1, by); ok && aRef == ref {
			return a
		}
	}
	return Predict(g, bx, by, 4, ref)
}

// Predict8x16 返回 8×16 分区的 MV 预测。
//
// 左分区优先取左侧 A；右分区优先取右上 C（含左上回退）。这与
// FFmpeg 的 pred_8x16_motion 对齐。
func Predict8x16(g *Grid, bx, by int, right bool, ref int8) MV {
	if !right {
		if a, aRef, ok := g.Get(bx-1, by); ok && aRef == ref {
			return a
		}
	} else {
		if c, cRef, ok := diagonal(g, bx, by, 2); ok && cRef == ref {
			return c
		}
	}
	return Predict(g, bx, by, 2, ref)
}

// PredictSkip 计算 P_Skip 的 MV（8.4.1.1）。
//
// 与普通预测的差异：只要左或上邻块的 MV 为零且参考一致，就直接取零，
// 不再做中值；这是跳过宏块最常见的静态场景，也是规范明确规定的捷径。
func PredictSkip(g *Grid, bx, by int, ref int8) MV {
	a, aRef, aOK := g.Get(bx-1, by)
	b, bRef, bOK := g.Get(bx, by-1)
	c, cRef, cOK := diagonal(g, bx, by, 4)
	// Spec: 8.4.1.1。左或上邻块不可用时，P_Skip 必须取零矢量。
	if !aOK || !bOK {
		return MV{}
	}
	if aOK && aRef == ref && a.IsZero() {
		return MV{}
	}
	if bOK && bRef == ref && b.IsZero() {
		return MV{}
	}
	match := 0
	if cOK && cRef == ref {
		match++
	}
	if bOK && bRef == ref {
		match++
	}
	if aOK && aRef == ref {
		match++
	}
	switch {
	case match > 1:
		return median(a, b, c)
	case match == 1:
		if aOK && aRef == ref {
			return a
		}
		if bOK && bRef == ref {
			return b
		}
		return c
	case !bOK && !cOK && aOK:
		return a
	default:
		return median(a, b, c)
	}
}

// diagonal 读取右上邻块 C；右上不可用时退回左上 D。
//
// 规范 8.4.1.3：C 位于分区右上角；若超出图像右边界，则用分区
// 左上方的 D 代替；D 也不可用时 C 视为不可用。
func diagonal(g *Grid, bx, by, partW int) (MV, int8, bool) {
	if c, r, ok := g.Get(bx+partW, by-1); ok {
		return c, r, true
	}
	if d, r, ok := g.Get(bx-1, by-1); ok {
		return d, r, true
	}
	return MV{}, 0, false
}

func median(a, b, c MV) MV {
	return MV{X: mid(a.X, b.X, c.X), Y: mid(a.Y, b.Y, c.Y)}
}

func mid(a, b, c int32) int32 {
	if a > b {
		a, b = b, a
	}
	if b > c {
		b = c
	}
	if a > b {
		b = a
	}
	return b
}
