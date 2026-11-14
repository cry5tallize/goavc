package cabac

import (
	"fmt"
	h264 "github.com/goavc/h264"
)

// 残差类别对应 Spec: 表 9-42 的 ctxBlockCat；422 DC 仍为 ChromaDC，不能新增类别编号。
const (
	LumaDC = iota
	LumaAC
	Luma4x4
	ChromaDC
	ChromaAC
	Luma8x8
)

var significantBase = [6]int{105, 120, 134, 149, 152, 402}
var lastBase = [6]int{166, 181, 195, 210, 213, 417}
var levelBase = [6]int{227, 237, 247, 257, 266, 426}

// Spec: 表 9-43，逐帧 8×8 的扫描位置到上下文增量映射。
var significant8 = [63]int{0, 1, 2, 3, 4, 5, 5, 4, 4, 3, 3, 4, 4, 4, 5, 5, 4, 4, 4, 4, 3, 3, 6, 7, 7, 7, 8, 9, 10, 9, 8, 7, 7, 6, 11, 12, 13, 11, 6, 7, 8, 9, 14, 10, 9, 8, 6, 11, 12, 13, 11, 6, 9, 14, 10, 9, 11, 12, 13, 11, 14, 10, 12}
var last8 = [63]int{0, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 3, 3, 3, 3, 3, 3, 3, 3, 4, 4, 4, 4, 4, 4, 4, 4, 5, 5, 5, 5, 6, 6, 6, 6, 7, 7, 7, 7, 8, 8, 8}

// residualSizeValid 保持 420 的四项输入，并为同一 ctxBlockCat=3 接受八项 DC。
// coded_block_flag 仍用 85+4*ChromaDC+condA+2*condB，即 97..100。
func residualSizeValid(cat, size int) bool {
	if cat < 0 || cat >= 6 {
		return false
	}
	return size == [6]int{16, 15, 16, 4, 15, 64}[cat] || cat == ChromaDC && size == 8
}

// residualPositionContext 来自 Spec: 9.3.3.1.3；422 DC 的显著性和末位
// 共用 min(levelListIdx/2,2)，第八项由剩余位置推导，不编码 sig/last。
func residualPositionContext(cat, size, pos int) (int, int) {
	if cat == Luma8x8 {
		return significant8[pos], last8[pos]
	}
	if cat == ChromaDC && size == 8 {
		inc := min(pos/2, 2)
		return inc, inc
	}
	return pos, pos
}

// residualGreaterContext 的 422 DC 饱和值为 3，其余块为 4。
// 因而 422 不得访问 ctxIdx=266（该位置已属于 ChromaAC）。
func residualGreaterContext(cat, size, greater int) int {
	if cat == ChromaDC && size == 8 {
		return 5 + min(3, greater)
	}
	return 5 + min(4, greater)
}

// Residual 解码 coded_block_flag=1 之后的显著性、末系数、幅值与符号。
// scan 指定有效系数到目标数组的映射，AC 使用跳过 DC 的 15 项扫描表。
// Spec: 9.3.3.1.3，先顺序读取显著性，再按逆扫描顺序读取幅值。
// ChromaDC 的 scan 可为 DC2x2Scan 或 DC2x4Scan；CBF=0 由调用方清零并跳过。
func (d *Decoder) Residual(ctx *[ContextCount]Context, cat int, dst []int32, scan []int) (int, error) {
	if ctx == nil || !residualSizeValid(cat, len(scan)) {
		return 0, fmt.Errorf("cabac: residual scan size does not match category: %w", h264.ErrInvalidData)
	}
	seen := uint64(0)
	for _, i := range scan {
		if i < 0 || i >= len(dst) || i >= 64 || seen&(uint64(1)<<i) != 0 {
			return 0, fmt.Errorf("cabac: invalid residual scan: %w", h264.ErrInvalidData)
		}
		seen |= uint64(1) << i
		dst[i] = 0
	}
	var positions [64]int
	count := 0
	for i := 0; i < len(scan); i++ {
		if i == len(scan)-1 {
			positions[count] = i
			count++
			break
		}
		sig, last := residualPositionContext(cat, len(scan), i)
		if d.Bin(&ctx[significantBase[cat]+sig]) == 0 {
			if d.Err() != nil {
				return 0, d.Err()
			}
			continue
		}
		positions[count] = i
		count++
		if d.Bin(&ctx[lastBase[cat]+last]) != 0 {
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
			inc = residualGreaterContext(cat, len(scan), greater)
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
