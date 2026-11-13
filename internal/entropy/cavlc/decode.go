package cavlc

import (
	"fmt"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/internal/transform"
)

// BlockType 区分 CAVLC 残差块的语法上下文。
//
// 选择上下文会影响两件事：coeff_token VLC 的选择与最大系数个数。
// LumaDC 与 Luma4x4 使用同一套 coeff_token 表，只是 nC 由调用方按
// 亮度 DC 的邻块规则计算；色度 DC 则使用独立的 5 组表。
type BlockType uint8

const (
	// Luma4x4 是普通亮度 4×4 块（16 个系数）。
	Luma4x4 BlockType = iota
	// LumaDC 是 I16×16 的亮度 DC 块（16 个系数）。
	LumaDC
	// LumaAC 是 I16×16 的亮度 AC 块（15 个系数，跳过 DC）。
	LumaAC
	// ChromaDC 是色度 DC 块（4:2:0 下 2×2，共 4 个系数）。
	ChromaDC
)

// 预构造的直接查表。
var (
	coeffTokenVLC [4]vlcTable
	chromaDCToken vlcTable
	totalZerosVLC [15]vlcTable
	chromaDCZeros [3]vlcTable
	runVLC        [7]vlcTable
)

func init() {
	mustBuild := func(name string, lengths []uint8, codes []uint8) vlcTable {
		t, err := buildVLC(lengths, codes)
		if err != nil {
			panic("cavlc: build " + name + ": " + err.Error())
		}
		return t
	}
	for i := 0; i < 4; i++ {
		coeffTokenVLC[i] = mustBuild("coeff_token", coeffTokenLen[i][:], coeffTokenBits[i][:])
	}
	chromaDCToken = mustBuild("chroma_dc_coeff_token", chromaDCCoeffTokenLen[:], chromaDCCoeffTokenBits[:])
	for i := 0; i < 15; i++ {
		totalZerosVLC[i] = mustBuild("total_zeros", totalZerosLen[i][:], totalZerosBits[i][:])
	}
	for i := 0; i < 3; i++ {
		chromaDCZeros[i] = mustBuild("chroma_dc_total_zeros", chromaDCTotalZerosLen[i][:], chromaDCTotalZerosBits[i][:])
	}
	for i := 0; i < 7; i++ {
		runVLC[i] = mustBuild("run_before", runLen[i][:], runBits[i][:])
	}
}

// coeffTokenTableIndex 把 nC 映射到 4 张 coeff_token 表之一（规范 9.2.1）。
//
// nC 可能来自邻块预测（0..32），规范规定 >=8 统一使用第 4 张表。
func coeffTokenTableIndex(nC int) int {
	switch {
	case nC < 2:
		return 0
	case nC < 4:
		return 1
	case nC < 8:
		return 2
	default:
		return 3
	}
}

// DecodeBlock 解码一个残差块，把 level 按扫描顺序写入 dst 的光栅位置。
//
// dst 长度至少 16；ChromaDC 只使用前 4 项。函数首先清零 dst，
// 因此调用方不需要预先清理。返回值是 totalCoeff（非零系数个数），
// 可直接用于 deblocking 的 non-zero count 与 nC 预测。
//
// nC 对 ChromaDC 无意义；对 LumaDC/Luma4x4/LumaAC，调用方必须按
// 规范 9.2.1 的邻块规则计算后传入。传入 -1 表示“无邻块可用”，
// 内部会按 nC=0 处理（与规范一致）。
func DecodeBlock(r *bitstream.Reader, dst *[16]int32, typ BlockType, nC int) (int, error) {
	if r == nil {
		return 0, fmt.Errorf("cavlc: nil bit reader")
	}
	for i := range dst {
		dst[i] = 0
	}

	var symbol uint8
	var err error
	if typ == ChromaDC {
		symbol, err = chromaDCToken.decode(r)
	} else {
		if nC < 0 {
			nC = 0
		}
		if nC > 16 {
			nC = 16
		}
		symbol, err = coeffTokenVLC[coeffTokenTableIndex(nC)].decode(r)
	}
	if err != nil {
		return 0, fmt.Errorf("cavlc: coeff_token: %w", err)
	}
	totalCoeff := int(symbol) >> 2
	trailingOnes := int(symbol) & 3

	maxCoeff := 16
	scan := transform.Zigzag4x4[:]
	switch typ {
	case LumaAC:
		maxCoeff = 15
		scan = transform.Zigzag4x4[1:]
	case ChromaDC:
		maxCoeff = 4
		scan = transform.DC2x2Scan[:]
	}
	if totalCoeff > maxCoeff {
		return 0, fmt.Errorf("cavlc: total_coeff %d exceeds max %d: %w",
			totalCoeff, maxCoeff, h264.ErrInvalidData)
	}
	if totalCoeff == 0 {
		return 0, nil
	}

	var levels [16]int32
	// 尾随 1 的符号：1 表示 -1，0 表示 +1（规范 9.2.2）。
	for i := 0; i < trailingOnes; i++ {
		b, err := readBit(r)
		if err != nil {
			return 0, fmt.Errorf("cavlc: trailing_ones sign: %w", err)
		}
		if b != 0 {
			levels[i] = -1
		} else {
			levels[i] = 1
		}
	}
	if trailingOnes < totalCoeff {
		suffixLength := 0
		if totalCoeff > 10 && trailingOnes < 3 {
			suffixLength = 1
		}
		if levels[trailingOnes], suffixLength, err = decodeFirstLevel(r, trailingOnes, suffixLength); err != nil {
			return 0, err
		}
		for i := trailingOnes + 1; i < totalCoeff; i++ {
			levels[i], suffixLength, err = decodeLevel(r, suffixLength)
			if err != nil {
				return 0, fmt.Errorf("cavlc: level[%d]: %w", i, err)
			}
		}
	}

	// total_zeros。
	zerosLeft := 0
	if totalCoeff != maxCoeff {
		switch typ {
		case ChromaDC:
			if totalCoeff > 3 {
				return 0, fmt.Errorf("cavlc: chroma dc total_coeff %d out of range: %w",
					totalCoeff, h264.ErrInvalidData)
			}
			z, err := chromaDCZeros[totalCoeff-1].decode(r)
			if err != nil {
				return 0, fmt.Errorf("cavlc: chroma_dc_total_zeros: %w", err)
			}
			zerosLeft = int(z)
		default:
			z, err := totalZerosVLC[totalCoeff-1].decode(r)
			if err != nil {
				return 0, fmt.Errorf("cavlc: total_zeros: %w", err)
			}
			zerosLeft = int(z)
		}
	}

	// run_before：从扫描末尾向前放置系数。
	pos := zerosLeft + totalCoeff - 1
	if pos < 0 || pos >= maxCoeff {
		return 0, fmt.Errorf("cavlc: invalid coefficient position %d: %w", pos, h264.ErrInvalidData)
	}
	dst[scan[pos]] = levels[0]
	i := 1
	for ; i < totalCoeff && zerosLeft > 0; i++ {
		var run int
		if zerosLeft < 7 {
			z, err := runVLC[zerosLeft-1].decode(r)
			if err != nil {
				return 0, fmt.Errorf("cavlc: run_before: %w", err)
			}
			run = int(z)
		} else {
			z, err := runVLC[6].decode(r)
			if err != nil {
				return 0, fmt.Errorf("cavlc: run_before: %w", err)
			}
			run = int(z)
		}
		if run > zerosLeft {
			return 0, fmt.Errorf("cavlc: run_before %d exceeds zeros_left %d: %w",
				run, zerosLeft, h264.ErrInvalidData)
		}
		zerosLeft -= run
		pos -= 1 + run
		if pos < 0 {
			return 0, fmt.Errorf("cavlc: coefficient position underflow: %w", h264.ErrInvalidData)
		}
		dst[scan[pos]] = levels[i]
	}
	for ; i < totalCoeff; i++ {
		pos--
		if pos < 0 {
			return 0, fmt.Errorf("cavlc: coefficient position underflow: %w", h264.ErrInvalidData)
		}
		dst[scan[pos]] = levels[i]
	}
	return totalCoeff, nil
}

// decodeFirstLevel 解码第一个非尾随 level（规范 9.2.2.1）。
//
// 初始 suffixLength 为 0 或 1；返回更新后的 suffixLength。
// 转义分支的位宽与前缀处理必须与规范逐项一致，否则后续 level 的
// suffix 状态会错位，表现为整帧花屏但难以定位。
func decodeFirstLevel(r *bitstream.Reader, trailingOnes, suffixLength int) (int32, int, error) {
	entry, err := peekLevel(r, suffixLength)
	if err != nil {
		return 0, 0, err
	}
	if entry.code < 100 {
		levelCode := entry.code
		if err := skipLevel(r, entry.length); err != nil {
			return 0, 0, err
		}
		if trailingOnes < 3 {
			if levelCode < 0 {
				levelCode--
			} else {
				levelCode++
			}
		}
		if uint32(levelCode+3) > 6 {
			return levelCode, 2, nil
		}
		return levelCode, 1, nil
	}
	prefix := int(entry.code - 100)
	if err := skipLevel(r, entry.length); err != nil {
		return 0, 0, err
	}
	if prefix == 8 {
		ext, err := readLevelPrefixExtension(r)
		if err != nil {
			return 0, 0, err
		}
		prefix += ext
	}
	var levelCode int32
	switch {
	case prefix < 14:
		if suffixLength != 0 {
			b, err := readBit(r)
			if err != nil {
				return 0, 0, err
			}
			levelCode = int32(prefix<<1) + int32(b)
		} else {
			levelCode = int32(prefix)
		}
	case prefix == 14:
		if suffixLength != 0 {
			b, err := readBit(r)
			if err != nil {
				return 0, 0, err
			}
			levelCode = int32(prefix<<1) + int32(b)
		} else {
			v, err := readBits(r, 4)
			if err != nil {
				return 0, 0, err
			}
			levelCode = int32(prefix) + int32(v)
		}
	default:
		levelCode = 30
		if prefix >= 16 {
			if prefix > 28 {
				return 0, 0, fmt.Errorf("cavlc: level prefix %d exceeds 28: %w", prefix, h264.ErrInvalidData)
			}
			levelCode += (1 << uint(prefix-3)) - 4096
		}
		v, err := readBits(r, uint(prefix-3))
		if err != nil {
			return 0, 0, err
		}
		levelCode += int32(v)
	}
	if trailingOnes < 3 {
		levelCode += 2
	}
	return signedLevel(levelCode), 2, nil
}

// decodeLevel 解码后续 level，并更新 suffixLength。
func decodeLevel(r *bitstream.Reader, suffixLength int) (int32, int, error) {
	entry, err := peekLevel(r, suffixLength)
	if err != nil {
		return 0, 0, err
	}
	var levelCode int32
	if entry.code < 100 {
		levelCode = entry.code
		if err := skipLevel(r, entry.length); err != nil {
			return 0, 0, err
		}
	} else {
		prefix := int(entry.code - 100)
		if err := skipLevel(r, entry.length); err != nil {
			return 0, 0, err
		}
		if prefix == 8 {
			ext, err := readLevelPrefixExtension(r)
			if err != nil {
				return 0, 0, err
			}
			prefix += ext
		}
		if prefix < 15 {
			v, err := readBits(r, uint(suffixLength))
			if err != nil {
				return 0, 0, err
			}
			levelCode = int32(prefix)<<uint(suffixLength) + int32(v)
		} else {
			levelCode = int32(15) << uint(suffixLength)
			if prefix >= 16 {
				if prefix > 28 {
					return 0, 0, fmt.Errorf("cavlc: level prefix %d exceeds 28: %w", prefix, h264.ErrInvalidData)
				}
				levelCode += (1 << uint(prefix-3)) - 4096
			}
			v, err := readBits(r, uint(prefix-3))
			if err != nil {
				return 0, 0, err
			}
			levelCode += int32(v)
		}
		levelCode = signedLevel(levelCode)
	}

	suffixLimit := [...]uint32{0, 3, 6, 12, 24, 48, 0xFFFFFFFF}
	if suffixLength < 0 || suffixLength > 6 {
		return 0, 0, fmt.Errorf("cavlc: suffix length %d out of range: %w", suffixLength, h264.ErrInvalidData)
	}
	limit := suffixLimit[suffixLength]
	if uint32(levelCode)+limit > 2*limit && suffixLength < 6 {
		suffixLength++
	}
	return levelCode, suffixLength, nil
}

// signedLevel 把规范中的 levelCode 映射为带符号 level。
//
// 偶数 code 表示正数，奇数表示负数：
// 0→1, 1→-1, 2→2, 3→-2 ...
func signedLevel(code int32) int32 {
	mask := -(code & 1)
	return (((2 + code) >> 1) ^ mask) - mask
}

// peekLevel 读取 8 位（不足时右补零）并查 level 表。
func peekLevel(r *bitstream.Reader, suffixLength int) (levelEntry, error) {
	v, n, err := peekPadded8(r)
	if err != nil {
		return levelEntry{}, err
	}
	e := levelTable[suffixLength][v]
	if e.length == 0 || uint8(n) < e.length {
		return levelEntry{}, fmt.Errorf("cavlc: truncated level code: %w", h264.ErrInvalidData)
	}
	return e, nil
}

// skipLevel 跳过已查表得到的码字长度。
func skipLevel(r *bitstream.Reader, length uint8) error {
	if uint64(length) > r.BitsRemaining() {
		return fmt.Errorf("cavlc: truncated level suffix: %w", h264.ErrInvalidData)
	}
	return r.SkipBits(uint(length))
}

// readLevelPrefixExtension 读取转义后的额外前导零。
//
// 规范上限 15；这里放宽到 31 以便先返回“前缀过大”的明确错误，
// 而不是在无界循环里等待输入耗尽。
func readLevelPrefixExtension(r *bitstream.Reader) (int, error) {
	for zeros := 0; zeros < 32; zeros++ {
		b, err := readBit(r)
		if err != nil {
			return 0, err
		}
		if b != 0 {
			return zeros, nil
		}
	}
	return 0, fmt.Errorf("cavlc: level prefix longer than 31 zeros: %w", h264.ErrInvalidData)
}

// peekPadded8 返回下一个字节的 8 位值（MSB 对齐）与真实可用位数。
//
// 码流末尾可能只剩 1..7 位；CAVLC 的 VLC 解码允许先以零填充查表，
// 但最终码长必须不超过真实剩余位数，否则视为截断。
func peekPadded8(r *bitstream.Reader) (int, uint, error) {
	rem := r.BitsRemaining()
	if rem == 0 {
		return 0, 0, fmt.Errorf("cavlc: unexpected end of data: %w", h264.ErrInvalidData)
	}
	n := uint(8)
	if rem < 8 {
		n = uint(rem)
	}
	v, err := r.PeekBits(n)
	if err != nil {
		return 0, 0, err
	}
	return int(v) << (8 - n), n, nil
}

func readBit(r *bitstream.Reader) (uint64, error) {
	v, err := r.ReadBits(1)
	if err != nil {
		return 0, fmt.Errorf("cavlc: read bit: %w", err)
	}
	return v, nil
}

func readBits(r *bitstream.Reader, n uint) (uint64, error) {
	v, err := r.ReadBits(n)
	if err != nil {
		return 0, fmt.Errorf("cavlc: read %d bits: %w", n, err)
	}
	return v, nil
}

// decode 从直接查表解出一个符号。
func (t *vlcTable) decode(r *bitstream.Reader) (uint8, error) {
	rem := r.BitsRemaining()
	if rem == 0 {
		return 0, fmt.Errorf("cavlc: unexpected end of data: %w", h264.ErrInvalidData)
	}
	n := uint(t.maxBits)
	if rem < uint64(n) {
		n = uint(rem)
	}
	v, err := r.PeekBits(n)
	if err != nil {
		return 0, err
	}
	idx := int(v) << (uint(t.maxBits) - n)
	e := t.entries[idx]
	if e.length == 0 || uint8(n) < e.length {
		return 0, fmt.Errorf("cavlc: invalid vlc code (prefix %0*b): %w", t.maxBits, idx, h264.ErrInvalidData)
	}
	if err := r.SkipBits(uint(e.length)); err != nil {
		return 0, err
	}
	return e.symbol, nil
}
