package cabac

import (
	"fmt"

	h264 "github.com/cry5tallize/goavc"
)

// ResidualScan拥有映射的值副本；构造时验证一次，不保留调用方slice。
// 零值不可用；任意调用者scan仍可继续使用原Residual接口及其晚错清零语义。
type ResidualScan struct {
	indices        [64]int
	size, maxIndex int
}

// PrepareResidualScan校验唯一性和64项上限；类别及目标长度在每次解码前检查。
func PrepareResidualScan(scan []int) (out ResidualScan, err error) {
	if len(scan) == 0 || len(scan) > 64 {
		return out, fmt.Errorf("cabac: invalid prepared scan size: %w", h264.ErrInvalidData)
	}
	var seen uint64
	for i, v := range scan {
		if v < 0 || v >= 64 || seen&(uint64(1)<<v) != 0 {
			return ResidualScan{}, fmt.Errorf("cabac: invalid prepared scan: %w", h264.ErrInvalidData)
		}
		seen |= uint64(1) << v
		out.indices[i] = v
		out.maxIndex = max(out.maxIndex, v)
	}
	out.size = len(scan)
	return out, nil
}

// ResidualPrepared使用已验证扫描解码基础上下文域；不改变扫描以外的目标元素。
func (d *Decoder) ResidualPrepared(ctx *[ContextCount]Context, cat int, dst []int32, scan *ResidualScan, field bool) (int, error) {
	if ctx == nil || cat < LumaDC || cat > Luma8x8 {
		return 0, fmt.Errorf("cabac: invalid residual context domain: %w", h264.ErrInvalidData)
	}
	if scan == nil {
		return 0, fmt.Errorf("cabac: nil prepared scan: %w", h264.ErrInvalidData)
	}
	return d.residualPrepared(ctx[:], cat, dst, scan, field)
}

// ResidualPrepared444使用同一算术流程覆盖十四类444残差上下文。
func (d *Decoder) ResidualPrepared444(ctx *[ContextCount444]Context, cat int, dst []int32, scan *ResidualScan, field bool) (int, error) {
	if ctx == nil {
		return 0, fmt.Errorf("cabac: nil residual contexts: %w", h264.ErrInvalidData)
	}
	if scan == nil {
		return 0, fmt.Errorf("cabac: nil prepared scan: %w", h264.ErrInvalidData)
	}
	return d.residualPrepared(ctx[:], cat, dst, scan, field)
}

func (d *Decoder) residualPrepared(ctx []Context, cat int, dst []int32, scan *ResidualScan, field bool) (int, error) {
	if !residualSizeValid(cat, scan.size) {
		return 0, fmt.Errorf("cabac: residual scan size does not match category: %w", h264.ErrInvalidData)
	}
	if scan.maxIndex >= len(dst) {
		return 0, fmt.Errorf("cabac: prepared scan exceeds destination: %w", h264.ErrInvalidData)
	}
	indices := scan.indices[:scan.size]
	for _, i := range indices {
		dst[i] = 0
	}
	return d.decodeResidual(ctx, cat, dst, indices, field)
}
