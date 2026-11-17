package cabac

import (
	"fmt"
	h264 "github.com/cry5tallize/goavc"
)

// 残差类别对应 Spec: 表 9-42 的 ctxBlockCat；422 DC 仍为 ChromaDC。
const (
	LumaDC = iota
	LumaAC
	Luma4x4
	ChromaDC
	ChromaAC
	Luma8x8
	CbDC
	CbAC
	Cb4x4
	Cb8x8
	CrDC
	CrAC
	Cr4x4
	Cr8x8
)

// Spec: 表 9-39/9-40；第一行逐帧，第二行场编码。幅值不区分帧场。
var significantBase = [2][14]int{
	{105, 120, 134, 149, 152, 402, 484, 499, 513, 660, 528, 543, 557, 718},
	{277, 292, 306, 321, 324, 436, 776, 791, 805, 675, 820, 835, 849, 733},
}
var lastBase = [2][14]int{
	{166, 181, 195, 210, 213, 417, 572, 587, 601, 690, 616, 631, 645, 748},
	{338, 353, 367, 382, 385, 451, 864, 879, 893, 699, 908, 923, 937, 757},
}
var levelBase = [14]int{227, 237, 247, 257, 266, 426, 952, 962, 972, 708, 982, 992, 1002, 766}

// CodedBlockFlagBase 返回表9-39/9-40的CBF基址；邻域增量由调用方另加0..3。
// 8×8块的CBF仅在ChromaArrayType=3时编码，非444调用方仍须按语法跳过。
func CodedBlockFlagBase(cat int) (int, error) {
	if cat < LumaDC || cat > Cr8x8 {
		return 0, fmt.Errorf("cabac: invalid coded block category: %w", h264.ErrInvalidData)
	}
	return [14]int{85, 89, 93, 97, 101, 1012, 460, 464, 468, 1016, 472, 476, 480, 1020}[cat], nil
}

// Spec: 表 9-43，逐帧 8×8 的扫描位置到上下文增量映射。
var significant8 = [63]int{0, 1, 2, 3, 4, 5, 5, 4, 4, 3, 3, 4, 4, 4, 5, 5, 4, 4, 4, 4, 3, 3, 6, 7, 7, 7, 8, 9, 10, 9, 8, 7, 7, 6, 11, 12, 13, 11, 6, 7, 8, 9, 14, 10, 9, 8, 6, 11, 12, 13, 11, 6, 9, 14, 10, 9, 11, 12, 13, 11, 14, 10, 12}

// Spec: 表9-43第三列，场编码8×8；末位增量仍与帧编码共用last8。
var significant8Field = [63]int{0, 1, 1, 2, 2, 3, 3, 4, 5, 6, 7, 7, 7, 8, 4, 5, 6, 9, 10, 10, 8, 11, 12, 11, 9, 9, 10, 10, 8, 11, 12, 11, 9, 9, 10, 10, 8, 11, 12, 11, 9, 9, 10, 10, 8, 13, 13, 9, 9, 10, 10, 8, 13, 13, 9, 9, 10, 10, 14, 14, 14, 14, 14}
var last8 = [63]int{0, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 3, 3, 3, 3, 3, 3, 3, 3, 4, 4, 4, 4, 4, 4, 4, 4, 5, 5, 5, 5, 6, 6, 6, 6, 7, 7, 7, 7, 8, 8, 8}

// residualSizeValid 保留420/422色度DC长度，并为444色度提供亮度同构类别。
func residualSizeValid(cat, size int) bool {
	if cat < LumaDC || cat > Cr8x8 {
		return false
	}
	return size == [14]int{16, 15, 16, 4, 15, 64, 16, 15, 16, 64, 16, 15, 16, 64}[cat] || cat == ChromaDC && size == 8
}

// residualPositionContext 来自 Spec: 9.3.3.1.3；422 DC 的显著性和末位
// 共用 min(levelListIdx/2,2)，第八项由剩余位置推导，不编码 sig/last。
func residualPositionContext(cat, size, pos int, field bool) (int, int) {
	if cat == Luma8x8 || cat == Cb8x8 || cat == Cr8x8 {
		if field {
			return significant8Field[pos], last8[pos]
		}
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
	if ctx == nil || cat < LumaDC || cat > Luma8x8 {
		return 0, fmt.Errorf("cabac: invalid residual context domain: %w", h264.ErrInvalidData)
	}
	return d.residual(ctx[:], cat, dst, scan, false)
}

// ResidualField 使用旧460模型域及场扫描上下文；scan由调用方提供场扫描次序。
func (d *Decoder) ResidualField(ctx *[ContextCount]Context, cat int, dst []int32, scan []int) (int, error) {
	if ctx == nil || cat < LumaDC || cat > Luma8x8 {
		return 0, fmt.Errorf("cabac: invalid residual context domain: %w", h264.ErrInvalidData)
	}
	return d.residual(ctx[:], cat, dst, scan, true)
}

// Residual444 解码全部14类残差；field选择上下文映射，不替换调用方的scan。
func (d *Decoder) Residual444(ctx *[ContextCount444]Context, cat int, dst []int32, scan []int, field bool) (int, error) {
	if ctx == nil {
		return 0, fmt.Errorf("cabac: nil residual contexts: %w", h264.ErrInvalidData)
	}
	return d.residual(ctx[:], cat, dst, scan, field)
}

// residual借用数组切片，共享算术路径，不分配或复制上下文。
func (d *Decoder) residual(ctx []Context, cat int, dst []int32, scan []int, field bool) (int, error) {
	if !residualSizeValid(cat, len(scan)) {
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
	return d.decodeResidual(ctx, cat, dst, scan, field)
}

// 校验和清零之后唯一的算术解码流程，prepared与通用入口共用。
func (d *Decoder) decodeResidual(ctx []Context, cat int, dst []int32, scan []int, field bool) (int, error) {
	mode := 0
	if field {
		mode = 1
	}
	var positions [64]int
	count := 0
	for i := 0; i < len(scan); i++ {
		if i == len(scan)-1 {
			positions[count] = i
			count++
			break
		}
		sig, last := residualPositionContext(cat, len(scan), i, field)
		if d.Bin(&ctx[significantBase[mode][cat]+sig]) == 0 {
			if d.Err() != nil {
				return 0, d.Err()
			}
			continue
		}
		positions[count] = i
		count++
		if d.Bin(&ctx[lastBase[mode][cat]+last]) != 0 {
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
