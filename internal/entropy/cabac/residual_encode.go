package cabac

import (
	"fmt"
	h264 "github.com/goavc/h264"
	"math/bits"
)

// Residual 写出 coded_block_flag=1 后的显著性、末位置、绝对值与符号。
// 编解码共享类别基址与规范扫描上下文表；全零块必须由调用方写 CBF=0。
func (e *Encoder) Residual(ctx *[ContextCount]Context, cat int, src []int32, scan []int) (int, error) {
	if ctx == nil || !residualSizeValid(cat, len(scan)) {
		return 0, fmt.Errorf("cabac: invalid residual encoding category: %w", h264.ErrInvalidData)
	}
	var positions [64]int
	count := 0
	seen := uint64(0)
	for pos, index := range scan {
		if index < 0 || index >= len(src) || index >= 64 || seen&(uint64(1)<<index) != 0 {
			return 0, fmt.Errorf("cabac: invalid residual encoding scan: %w", h264.ErrInvalidData)
		}
		seen |= uint64(1) << index
		v := int64(src[index])
		if v < -(1<<24) || v > 1<<24 {
			return 0, fmt.Errorf("cabac: residual escape exceeds bound: %w", h264.ErrInvalidData)
		}
		if v != 0 {
			positions[count] = pos
			count++
		}
	}
	if count == 0 {
		return 0, fmt.Errorf("cabac: empty coded residual: %w", h264.ErrInvalidData)
	}
	lastPos := positions[count-1]
	for pos := 0; pos < len(scan)-1; pos++ {
		sig, last := residualPositionContext(cat, len(scan), pos)
		if src[scan[pos]] == 0 {
			e.Bin(&ctx[significantBase[cat]+sig], 0)
			continue
		}
		e.Bin(&ctx[significantBase[cat]+sig], 1)
		if pos == lastPos {
			e.Bin(&ctx[lastBase[cat]+last], 1)
			break
		}
		e.Bin(&ctx[lastBase[cat]+last], 0)
	}
	ones, greater := 0, 0
	for n := count - 1; n >= 0; n-- {
		level := int64(src[scan[positions[n]]])
		sign := uint8(0)
		if level < 0 {
			level = -level
			sign = 1
		}
		inc := 0
		if greater == 0 {
			inc = min(4, ones+1)
		}
		if level == 1 {
			e.Bin(&ctx[levelBase[cat]+inc], 0)
			ones++
		} else {
			e.Bin(&ctx[levelBase[cat]+inc], 1)
			inc = residualGreaterContext(cat, len(scan), greater)
			for magnitude := int64(2); magnitude < 15; magnitude++ {
				if magnitude < level {
					e.Bin(&ctx[levelBase[cat]+inc], 1)
				} else {
					e.Bin(&ctx[levelBase[cat]+inc], 0)
					break
				}
			}
			if level >= 15 {
				code := uint64(level - 14)
				k := bits.Len64(code) - 1
				for i := 0; i < k; i++ {
					e.Bypass(1)
				}
				e.Bypass(0)
				suffix := code - (1 << k)
				for j := k - 1; j >= 0; j-- {
					e.Bypass(uint8(suffix >> j & 1))
				}
			}
			greater++
		}
		e.Bypass(sign)
	}
	return count, e.Err()
}
