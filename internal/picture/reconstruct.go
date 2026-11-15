package picture

import "github.com/cry5tallize/goavc/internal/simd"

// AddResidual4x4 将共享逆变换得到的残差加到预测块并执行 Clip1。
// 调用方必须保证 4×4 区域位于编码图像内；int64 避免极端残差加法回绕。
// 按平面位深选择采样类型，8-bit热路径不做逐像素位深分支或额外转换。
func AddResidual4x4(p *Plane, x, y int, residual *[16]int32) {
	if p.BitDepth == 10 {
		simd.AddResidual16(p.Data16[p.Index(x, y):], p.Stride, residual[:], 4, 4, 4)
	} else {
		simd.AddResidual8(p.Data[p.Index(x, y):], p.Stride, residual[:], 4, 4, 4)
	}
}

// AddResidual8x8 与4×4共享Clip1算法，不拆块或复制变换输出。
func AddResidual8x8(p *Plane, x, y int, residual *[64]int32) {
	if p.BitDepth == 10 {
		simd.AddResidual16(p.Data16[p.Index(x, y):], p.Stride, residual[:], 8, 8, 8)
	} else {
		simd.AddResidual8(p.Data[p.Index(x, y):], p.Stride, residual[:], 8, 8, 8)
	}
}
