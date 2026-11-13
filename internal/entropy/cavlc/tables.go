// Package cavlc 实现 H.264 Baseline 的 CAVLC 熵解码（规范 9.2）。
//
// 本包只负责把比特流还原成量化后的 level；反量化、逆变换和预测由
// transform/decoder 包完成。这样 CAVLC 的表与算法可以独立用固定比特
// 序列测试，不需要构造完整宏块。
//
// 表数据来自规范 9.2.1/9.2.2 的 VLC 表。为了方便查表，符号统一编码为
// 4*totalCoeff + trailingOnes（与 libavcodec 的 coeff_token 符号一致）；
// 这只是内部表示，不改变码字本身。
package cavlc

import (
	"fmt"
	"math/bits"
)

// coeffTokenLen/Bits 是表 9-5 至 9-7：按 nC 分段的 coeff_token VLC。
//
// 四张表分别对应 nC 0..1、2..3、4..7、>=8。数组按 17 组 × 4 个
// trailingOnes 展开，索引为 4*totalCoeff+trailingOnes。
var coeffTokenLen = [4][68]uint8{
	{
		1, 0, 0, 0,
		6, 2, 0, 0, 8, 6, 3, 0, 9, 8, 7, 5, 10, 9, 8, 6,
		11, 10, 9, 7, 13, 11, 10, 8, 13, 13, 11, 9, 13, 13, 13, 10,
		14, 14, 13, 11, 14, 14, 14, 13, 15, 15, 14, 14, 15, 15, 15, 14,
		16, 15, 15, 15, 16, 16, 16, 15, 16, 16, 16, 16, 16, 16, 16, 16,
	},
	{
		2, 0, 0, 0,
		6, 2, 0, 0, 6, 5, 3, 0, 7, 6, 6, 4, 8, 6, 6, 4,
		8, 7, 7, 5, 9, 8, 8, 6, 11, 9, 9, 6, 11, 11, 11, 7,
		12, 11, 11, 9, 12, 12, 12, 11, 12, 12, 12, 11, 13, 13, 13, 12,
		13, 13, 13, 13, 13, 14, 13, 13, 14, 14, 14, 13, 14, 14, 14, 14,
	},
	{
		4, 0, 0, 0,
		6, 4, 0, 0, 6, 5, 4, 0, 6, 5, 5, 4, 7, 5, 5, 4,
		7, 5, 5, 4, 7, 6, 6, 4, 7, 6, 6, 4, 8, 7, 7, 5,
		8, 8, 7, 6, 9, 8, 8, 7, 9, 9, 8, 8, 9, 9, 9, 8,
		10, 9, 9, 9, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10,
	},
	{
		6, 0, 0, 0,
		6, 6, 0, 0, 6, 6, 6, 0, 6, 6, 6, 6, 6, 6, 6, 6,
		6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6,
		6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6,
		6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6,
	},
}

var coeffTokenBits = [4][68]uint8{
	{
		1, 0, 0, 0,
		5, 1, 0, 0, 7, 4, 1, 0, 7, 6, 5, 3, 7, 6, 5, 3,
		7, 6, 5, 4, 15, 6, 5, 4, 11, 14, 5, 4, 8, 10, 13, 4,
		15, 14, 9, 4, 11, 10, 13, 12, 15, 14, 9, 12, 11, 10, 13, 8,
		15, 1, 9, 12, 11, 14, 13, 8, 7, 10, 9, 12, 4, 6, 5, 8,
	},
	{
		3, 0, 0, 0,
		11, 2, 0, 0, 7, 7, 3, 0, 7, 10, 9, 5, 7, 6, 5, 4,
		4, 6, 5, 6, 7, 6, 5, 8, 15, 6, 5, 4, 11, 14, 13, 4,
		15, 10, 9, 4, 11, 14, 13, 12, 8, 10, 9, 8, 15, 14, 13, 12,
		11, 10, 9, 12, 7, 11, 6, 8, 9, 8, 10, 1, 7, 6, 5, 4,
	},
	{
		15, 0, 0, 0,
		15, 14, 0, 0, 11, 15, 13, 0, 8, 12, 14, 12, 15, 10, 11, 11,
		11, 8, 9, 10, 9, 14, 13, 9, 8, 10, 9, 8, 15, 14, 13, 13,
		11, 14, 10, 12, 15, 10, 13, 12, 11, 14, 9, 12, 8, 10, 13, 8,
		13, 7, 9, 12, 9, 12, 11, 10, 5, 8, 7, 6, 1, 4, 3, 2,
	},
	{
		3, 0, 0, 0,
		0, 1, 0, 0, 4, 5, 6, 0, 8, 9, 10, 11, 12, 13, 14, 15,
		16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31,
		32, 33, 34, 35, 36, 37, 38, 39, 40, 41, 42, 43, 44, 45, 46, 47,
		48, 49, 50, 51, 52, 53, 54, 55, 56, 57, 58, 59, 60, 61, 62, 63,
	},
}

// chromaDCCoeffTokenLen/Bits 是色度 DC 的 coeff_token 表（规范表 9-5）。
// 符号同样按 4*totalCoeff+trailingOnes 排列，共 5 组。
var chromaDCCoeffTokenLen = [20]uint8{
	2, 0, 0, 0,
	6, 1, 0, 0,
	6, 6, 3, 0,
	6, 7, 7, 6,
	6, 8, 8, 7,
}

var chromaDCCoeffTokenBits = [20]uint8{
	1, 0, 0, 0,
	7, 1, 0, 0,
	4, 6, 1, 0,
	3, 3, 2, 5,
	2, 3, 2, 0,
}

// totalZerosLen/Bits 是表 9-7：行号 = totalCoeff-1，列号 = total_zeros。
// 最后一行（totalCoeff=16）在规范中没有码字，保持全零。
var totalZerosLen = [16][16]uint8{
	{1, 3, 3, 4, 4, 5, 5, 6, 6, 7, 7, 8, 8, 9, 9, 9},
	{3, 3, 3, 3, 3, 4, 4, 4, 4, 5, 5, 6, 6, 6, 6},
	{4, 3, 3, 3, 4, 4, 3, 3, 4, 5, 5, 6, 5, 6},
	{5, 3, 4, 4, 3, 3, 3, 4, 3, 4, 5, 5, 5},
	{4, 4, 4, 3, 3, 3, 3, 3, 4, 5, 4, 5},
	{6, 5, 3, 3, 3, 3, 3, 3, 4, 3, 6},
	{6, 5, 3, 3, 3, 2, 3, 4, 3, 6},
	{6, 4, 5, 3, 2, 2, 3, 3, 6},
	{6, 6, 4, 2, 2, 3, 2, 5},
	{5, 5, 3, 2, 2, 2, 4},
	{4, 4, 3, 3, 1, 3},
	{4, 4, 2, 1, 3},
	{3, 3, 1, 2},
	{2, 2, 1},
	{1, 1},
}

var totalZerosBits = [16][16]uint8{
	{1, 3, 2, 3, 2, 3, 2, 3, 2, 3, 2, 3, 2, 3, 2, 1},
	{7, 6, 5, 4, 3, 5, 4, 3, 2, 3, 2, 3, 2, 1, 0},
	{5, 7, 6, 5, 4, 3, 4, 3, 2, 3, 2, 1, 1, 0},
	{3, 7, 5, 4, 6, 5, 4, 3, 3, 2, 2, 1, 0},
	{5, 4, 3, 7, 6, 5, 4, 3, 2, 1, 1, 0},
	{1, 1, 7, 6, 5, 4, 3, 2, 1, 1, 0},
	{1, 1, 5, 4, 3, 3, 2, 1, 1, 0},
	{1, 1, 1, 3, 3, 2, 2, 1, 0},
	{1, 0, 1, 3, 2, 1, 1, 1},
	{1, 0, 1, 3, 2, 1, 1},
	{0, 1, 1, 2, 1, 3},
	{0, 1, 1, 1, 1},
	{0, 1, 1, 1},
	{0, 1, 1},
	{0, 1},
}

// chromaDCTotalZerosLen/Bits 是色度 DC 的 total_zeros 表（表 9-9）。
// 行号 = totalCoeff-1（totalCoeff 1..3），列号 = total_zeros。
var chromaDCTotalZerosLen = [3][4]uint8{
	{1, 2, 3, 3},
	{1, 2, 2, 0},
	{1, 1, 0, 0},
}

var chromaDCTotalZerosBits = [3][4]uint8{
	{1, 1, 1, 0},
	{1, 1, 0, 0},
	{1, 0, 0, 0},
}

// runLen/Bits 是表 9-10：行号 = min(zerosLeft,7)-1，
// 前 6 行用于 zerosLeft 1..6，最后一行用于 zerosLeft >= 7。
var runLen = [7][16]uint8{
	{1, 1},
	{1, 2, 2},
	{2, 2, 2, 2},
	{2, 2, 2, 3, 3},
	{2, 2, 3, 3, 3, 3},
	{2, 3, 3, 3, 3, 3, 3},
	{3, 3, 3, 3, 3, 3, 3, 4, 5, 6, 7, 8, 9, 10, 11},
}

var runBits = [7][16]uint8{
	{1, 0},
	{1, 1, 0},
	{3, 2, 1, 0},
	{3, 2, 1, 1, 0},
	{3, 2, 3, 2, 1, 0},
	{3, 0, 1, 3, 2, 5, 4},
	{7, 6, 5, 4, 3, 2, 1, 1, 1, 1, 1, 1, 1, 1, 1},
}

// vlcEntry 是单层查表项。length=0 表示该前缀不对应任何码字。
type vlcEntry struct {
	symbol uint8
	length uint8
}

// vlcTable 把长度 <= maxBits 的码字展开成 2^maxBits 的直接查表。
type vlcTable struct {
	maxBits uint8
	entries []vlcEntry
}

// buildVLC 由长度/码字表构造直接查表。
//
// 码字按 MSB-first 存放（与比特流顺序一致）。所有码字必须是前缀码；
// 构造时检测冲突并返回错误，避免表本身写错却静默解出错误符号。
func buildVLC(lengths []uint8, codes []uint8) (vlcTable, error) {
	if len(lengths) != len(codes) {
		return vlcTable{}, fmt.Errorf("cavlc: vlc table length mismatch %d vs %d", len(lengths), len(codes))
	}
	maxBits := uint8(0)
	for _, l := range lengths {
		if l > maxBits {
			maxBits = l
		}
	}
	if maxBits == 0 || maxBits > 16 {
		return vlcTable{}, fmt.Errorf("cavlc: invalid vlc max bits %d", maxBits)
	}
	t := vlcTable{maxBits: maxBits, entries: make([]vlcEntry, 1<<maxBits)}
	for sym, l := range lengths {
		if l == 0 {
			continue
		}
		if l > maxBits || int(codes[sym]) >= 1<<l {
			return vlcTable{}, fmt.Errorf("cavlc: invalid code for symbol %d (len %d code %d)", sym, l, codes[sym])
		}
		shift := uint(maxBits - l)
		base := int(codes[sym]) << shift
		fill := 1 << shift
		for j := 0; j < fill; j++ {
			e := t.entries[base+j]
			if e.length != 0 {
				return vlcTable{}, fmt.Errorf("cavlc: prefix collision at symbol %d", sym)
			}
			t.entries[base+j] = vlcEntry{symbol: uint8(sym), length: l}
		}
	}
	return t, nil
}

// levelEntry 是 level VLC 的查表项。
// code >= 100 表示转义：code-100 是已消费的 level_prefix 初始值（8 表示
// 还需要继续读前导零）。这与 libavcodec 的 cavlc_level_tab 编码一致。
type levelEntry struct {
	code   int32
	length uint8
}

// levelTable 按 suffixLength 0..6 存储 8-bit 查表。
var levelTable [7][256]levelEntry

func init() {
	for suffix := 0; suffix < 7; suffix++ {
		for i := 0; i < 256; i++ {
			levelTable[suffix][i] = buildLevelEntry(i, suffix)
		}
	}
}

// buildLevelEntry 复现规范 9.2.2.1 的“先读 8 位、再决定是否转义”策略。
//
// 若完整码字落在 8 位窗口内，直接给出带符号 level 与码长；
// 否则给出转义标记与前缀长度，由调用方继续读取。
func buildLevelEntry(i, suffix int) levelEntry {
	prefix := 8
	if i > 0 {
		prefix = 8 - (bits.Len(uint(2*i)) - 1)
	}
	if prefix+1+suffix <= 8 {
		logi := bits.Len(uint(i)) - 1
		if logi < suffix {
			return levelEntry{code: 108, length: 8}
		}
		levelCode := (prefix << uint(suffix)) +
			(i >> uint(logi-suffix)) - (1 << uint(suffix))
		mask := -(levelCode & 1)
		signed := (((2 + levelCode) >> 1) ^ mask) - mask
		return levelEntry{code: int32(signed), length: uint8(prefix + 1 + suffix)}
	}
	if prefix+1 <= 8 {
		return levelEntry{code: int32(prefix + 100), length: uint8(prefix + 1)}
	}
	return levelEntry{code: 108, length: 8}
}
