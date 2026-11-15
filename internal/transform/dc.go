package transform

import (
	"fmt"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/internal/simd"
)

// DequantLumaDC 返回每个 4×4 亮度块的 DC，输入输出均为光栅顺序。
//
// 与普通 4×4 反量化不同，DC 块使用固定位置 (0,0) 的 levelScale，并且
// 反变换后的归一化是 >>8（Hadamard 增益 16，与 4×4 IDCT 的 >>6 合并）。
// 公式与 FFmpeg 的 ff_h264_luma_dc_dequant_idct 一致：
//
//	qmul = levelScale[qP%6][0] * 16 << (qP/6+2)
//	dc   = (hadamard(levels) * qmul + 128) >> 8
//
// qp是已转换的QP'，范围0..MaxScaledQP；Hadamard中间量使用int64。
func DequantLumaDC(dst *[16]int32, levels *[16]int32, qp int) error {
	return DequantLumaDCWithScaling(dst, levels, qp, 16)
}

func DequantLumaDCWithScaling(dst *[16]int32, levels *[16]int32, qp int, scale uint8) error {
	if scale == 0 {
		return fmt.Errorf("transform: zero luma DC scale: %w", h264.ErrInvalidData)
	}
	if qp < 0 || qp > MaxScaledQP {
		return fmt.Errorf("transform: qp %d out of range 0..%d: %w", qp, MaxScaledQP, h264.ErrInvalidData)
	}
	qmul := dequantMul(levelScale4x4[qp%6][0], qp, scale)
	out, written := simd.DequantLumaDC(*levels, qmul)
	for n := 0; n < written; n++ {
		i, j := n/4, n%4
		dst[j*4+i] = out[j*4+i]
	}
	if written != 16 {
		return fmt.Errorf("transform: luma dc coefficient overflows int32: %w", h264.ErrInvalidData)
	}
	return nil
}

// DequantChromaDC 把 2×2 色度 DC level 反量化并做 2×2 逆 Hadamard。
//
// levels 按 2×2 光栅顺序（0,1,2,3）给出；输出同样按光栅顺序，
// 对应四个色度 4×4 块的 DC 位置。
//
// FFmpeg 的色度 DC 路径使用 >>7 且不额外加舍入常数：
//
//	qmul = levelScale[qP%6][0] * 16 << (qP/6+2)
//	dc   = (sum * qmul) >> 7
//
// 注意这里没有 +64；这是规范 8.5.11 与 8.5.10 的差异之一，
// 多一个舍入常数就会让 P 帧逐像素对比出现系统性偏差。
// qp是已转换的QP'，范围0..MaxScaledQP；Hadamard中间量使用int64。
func DequantChromaDC(dst *[4]int32, levels *[4]int32, qp int) error {
	return DequantChromaDCWithScaling(dst, levels, qp, 16)
}

func DequantChromaDCWithScaling(dst *[4]int32, levels *[4]int32, qp int, scale uint8) error {
	if scale == 0 {
		return fmt.Errorf("transform: zero chroma DC scale: %w", h264.ErrInvalidData)
	}
	if qp < 0 || qp > MaxScaledQP {
		return fmt.Errorf("transform: qp %d out of range 0..%d: %w", qp, MaxScaledQP, h264.ErrInvalidData)
	}
	qmul := dequantMul(levelScale4x4[qp%6][0], qp, scale)
	out, written := simd.DequantChromaDC(*levels, qmul)
	copy(dst[:written], out[:written])
	if written != len(out) {
		return fmt.Errorf("transform: chroma dc coefficient overflows int32: %w", h264.ErrInvalidData)
	}
	return nil
}
