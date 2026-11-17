package picture

import (
	"github.com/cry5tallize/goavc/internal/simd"
	"github.com/cry5tallize/goavc/internal/transform"
)

// Reconstruct4x4执行共享逆变换及预测加法，不改输入系数。
func Reconstruct4x4(p *Plane, x, y int, coeff *[16]int32) {
	if p.BitDepth == 10 {
		if simd.TryIDCTAdd4x4High(p.Data16[p.Index(x, y):], p.Stride, coeff) {
			return
		}
	} else {
		if simd.TryIDCTAdd4x4(p.Data[p.Index(x, y):], p.Stride, coeff) {
			return
		}
	}
	var residual [16]int32
	transform.IDCT4x4(&residual, coeff)
	AddResidual4x4(p, x, y, &residual)
}

// Reconstruct8x8在逆变换错误时不修改像素，错误仍由transform规范入口返回。
func Reconstruct8x8(p *Plane, x, y int, coeff *[64]int32) error {
	if p.BitDepth == 10 {
		if simd.TryIDCTAdd8x8High(p.Data16[p.Index(x, y):], p.Stride, coeff) {
			return nil
		}
	} else {
		if simd.TryIDCTAdd8x8(p.Data[p.Index(x, y):], p.Stride, coeff) {
			return nil
		}
	}
	var residual [64]int32
	if err := transform.IDCT8x8(&residual, coeff); err != nil {
		return err
	}
	AddResidual8x8(p, x, y, &residual)
	return nil
}
