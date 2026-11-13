package cabac

import (
	"fmt"
	h264 "github.com/goavc/h264"
)

// 残差类别对应 Spec: 表 9-42 的 ctxBlockCat（4:2:0、4×4 路径）。
const (
	LumaDC = iota
	LumaAC
	Luma4x4
	ChromaDC
	ChromaAC
)

var significantBase = [5]int{105, 120, 134, 149, 152}
var lastBase = [5]int{166, 181, 195, 210, 213}
var levelBase = [5]int{227, 237, 247, 257, 266}

// Residual 解码 coded_block_flag=1 之后的显著性、末系数、幅值与符号。
// scan 指定有效系数到目标数组的映射，AC 使用跳过 DC 的 15 项扫描表。
// Spec: 9.3.3.1.3，先顺序读取显著性，再按逆扫描顺序读取幅值。
func (d *Decoder) Residual(ctx *[ContextCount]Context, cat int, dst []int32, scan []int) (int, error) {
	if cat < 0 || cat >= 5 || ctx == nil || len(scan) < 1 || len(scan) > 16 {
		return 0, fmt.Errorf("cabac: invalid residual category or size: %w", h264.ErrInvalidData)
	}
	seen := uint64(0)
	for _, i := range scan {
		if i < 0 || i >= len(dst) || i >= 64 || seen&(uint64(1)<<i) != 0 {
			return 0, fmt.Errorf("cabac: invalid residual scan: %w", h264.ErrInvalidData)
		}
		seen |= uint64(1) << i
		dst[i] = 0
	}
	var positions [16]int
	count := 0
	for i := 0; i < len(scan); i++ {
		if i == len(scan)-1 {
			positions[count] = i
			count++
			break
		}
		if d.Bin(&ctx[significantBase[cat]+i]) == 0 {
			if d.Err() != nil {
				return 0, d.Err()
			}
			continue
		}
		positions[count] = i
		count++
		if d.Bin(&ctx[lastBase[cat]+i]) != 0 {
			break
		}
	}
	ones, greater := 0, 0
	for n := count - 1; n >= 0; n-- {
		inc := 0
		if greater == 0 {
			inc = min(4, ones+1)
		}
		level := int32(1)
		if d.Bin(&ctx[levelBase[cat]+inc]) != 0 {
			inc = 5 + min(4, greater)
			level = 2
			for level < 15 && d.Err() == nil && d.Bin(&ctx[levelBase[cat]+inc]) != 0 {
				level++
			}
			if level == 15 {
				k := 0
				for d.Err() == nil && d.Bypass() != 0 {
					k++
					if k > 23 {
						return 0, fmt.Errorf("cabac: coefficient escape exceeds 23 bits: %w", h264.ErrInvalidData)
					}
				}
				suffix := int32(0)
				for j := 0; j < k; j++ {
					suffix = suffix*2 + int32(d.Bypass())
				}
				level = 14 + (1 << uint(k)) + suffix
			}
			greater++
		} else {
			ones++
		}
		if d.Bypass() != 0 {
			level = -level
		}
		dst[scan[positions[n]]] = level
		if d.Err() != nil {
			return 0, d.Err()
		}
	}
	return count, d.Err()
}
