// Package yuvdiff 提供 raw YUV 的逐平面逐像素比较，是解码器 Oracle 差分测试的核心。
//
// 为什么不用 RGB 或 PSNR 作为判据：H.264 的编解码链路在 YUV 域定义，
// RGB 转换会引入额外的舍入误差，让"实现有偏差"和"转换有偏差"混在一起。
// 本项目要求与参考实现在 YUV 平面上一字节不差，因此判据必须是精确比较。
//
// 用法约束：输入必须是**紧密排列**的平面数据（stride == 行字节数）。
// 带 padding 的帧请先按行裁剪成紧密布局再比较。
package yuvdiff

import (
	"fmt"
	"math"
)

// PlaneLayout 描述单个平面的几何信息。
type PlaneLayout struct {
	Width int
	Height int
	// BytesPerSample 是存储字节数（8 bit 为 1，10/12 bit 为 2）。
	BytesPerSample int
}

// Layout 描述整帧的平面顺序（固定为 Y、U、V）与各自几何。
type Layout struct {
	Planes []PlaneLayout
}

// PlanarLayout 按像素格式参数构造紧密排列布局。
//
// subW/subH 是色度的水平/垂直抽样因子（4:2:0 → 2,2；4:2:2 → 2,1；4:4:4 → 1,1）。
// 宽高使用向上取整，保证裁剪后的奇数尺寸也能正确比较。
func PlanarLayout(width, height, bytesPerSample, subW, subH int) Layout {
	chroma := func(n, sub int) int {
		if sub <= 1 {
			return n
		}
		return (n + sub - 1) / sub
	}
	return Layout{Planes: []PlaneLayout{
		{Width: width, Height: height, BytesPerSample: bytesPerSample},
		{Width: chroma(width, subW), Height: chroma(height, subH), BytesPerSample: bytesPerSample},
		{Width: chroma(width, subW), Height: chroma(height, subH), BytesPerSample: bytesPerSample},
	}}
}

// Mismatch 描述第一个不一致的采样点。
type Mismatch struct {
	Plane int
	X     int
	Y     int
	A     int // 参考值
	B     int // 实际值
}

// Result 是比较结果。
type Result struct {
	Samples  int // 参与比较的采样总数
	Diffs    int // 不一致的采样数
	MaxDelta int // 最大绝对差值（用于判断是舍入偏差还是严重错误）
	First    *Mismatch
}

// Same 表示两个缓冲区逐采样一致。
func (r Result) Same() bool { return r.Diffs == 0 }

// String 输出稳定的单行摘要，便于 CI 日志解析。
func (r Result) String() string {
	s := fmt.Sprintf("samples=%d diffs=%d maxDelta=%d", r.Samples, r.Diffs, r.MaxDelta)
	if r.First != nil {
		s += fmt.Sprintf(" first=plane%d(%d,%d) ref=%d got=%d",
			r.First.Plane, r.First.X, r.First.Y, r.First.A, r.First.B)
	}
	return s
}

// Compare 逐平面逐采样比较两份紧密排列的 YUV 数据。
//
// 返回错误的情形只有三种：布局非法、缓冲区长度与布局不符。
// 内容不一致不算错误，通过 Result.Diffs 表达——这样调用方可以一次性
// 打印完整统计，而不是遇到第一个差异就丢失上下文。
func Compare(ref, got []byte, layout Layout) (Result, error) {
	total, err := layout.Size()
	if err != nil {
		return Result{}, err
	}
	if len(ref) != total {
		return Result{}, fmt.Errorf("yuvdiff: reference length %d does not match layout size %d", len(ref), total)
	}
	if len(got) != total {
		return Result{}, fmt.Errorf("yuvdiff: decoded length %d does not match layout size %d", len(got), total)
	}

	var res Result
	offset := 0
	for pi, pl := range layout.Planes {
		rowBytes := pl.Width * pl.BytesPerSample
		for y := 0; y < pl.Height; y++ {
			row := offset + y*rowBytes
			for x := 0; x < pl.Width; x++ {
				o := row + x*pl.BytesPerSample
				var a, b int
				if pl.BytesPerSample == 1 {
					a, b = int(ref[o]), int(got[o])
				} else {
					// 小端 16 bit：高字节在后。10 bit 有效位在低位。
					a = int(ref[o]) | int(ref[o+1])<<8
					b = int(got[o]) | int(got[o+1])<<8
				}
				res.Samples++
				if a == b {
					continue
				}
				res.Diffs++
				d := a - b
				if d < 0 {
					d = -d
				}
				if d > res.MaxDelta {
					res.MaxDelta = d
				}
				if res.First == nil {
					res.First = &Mismatch{Plane: pi, X: x, Y: y, A: a, B: b}
				}
			}
		}
		offset += rowBytes * pl.Height
	}
	return res, nil
}

// Size 计算布局对应的总字节数，并检测溢出。
//
// 溢出检测是必需的：布局参数来自命令行或码流，超大尺寸会让
//「长度校验」因为回绕而失效，随后比较循环越界读取。
func (l Layout) Size() (int, error) {
	if len(l.Planes) == 0 {
		return 0, fmt.Errorf("yuvdiff: empty layout")
	}
	total := 0
	for i, pl := range l.Planes {
		if pl.Width <= 0 || pl.Height <= 0 {
			return 0, fmt.Errorf("yuvdiff: plane %d has invalid size %dx%d", i, pl.Width, pl.Height)
		}
		if pl.BytesPerSample != 1 && pl.BytesPerSample != 2 {
			return 0, fmt.Errorf("yuvdiff: plane %d has invalid bytes per sample %d", i, pl.BytesPerSample)
		}
		rowBytes, err := mul(pl.Width, pl.BytesPerSample)
		if err != nil {
			return 0, err
		}
		planeBytes, err := mul(rowBytes, pl.Height)
		if err != nil {
			return 0, err
		}
		if planeBytes > math.MaxInt-total {
			return 0, fmt.Errorf("yuvdiff: total size overflows int")
		}
		total += planeBytes
	}
	return total, nil
}

func mul(a, b int) (int, error) {
	if a < 0 || b < 0 {
		return 0, fmt.Errorf("yuvdiff: negative size")
	}
	if a != 0 && b > math.MaxInt/a {
		return 0, fmt.Errorf("yuvdiff: size multiplication overflows int")
	}
	return a * b, nil
}
