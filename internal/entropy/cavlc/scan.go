package cavlc

import (
	"fmt"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/bitstream"
	"github.com/cry5tallize/goavc/internal/transform"
)

func defaultBlockScan(typ BlockType) []int {
	switch typ {
	case LumaDC:
		return transform.LumaDCScan[:]
	case LumaAC:
		return transform.Zigzag4x4[1:]
	case ChromaDC:
		return transform.DC2x2Scan[:]
	case ChromaDC422:
		return transform.DC2x4Scan[:]
	default:
		return transform.Zigzag4x4[:]
	}
}

func validateBlockScan(typ BlockType, scan []int) error {
	if typ > ChromaDC422 || len(scan) != len(defaultBlockScan(typ)) {
		return fmt.Errorf("cavlc: scan does not match block category: %w", h264.ErrInvalidData)
	}
	limit := 16
	if typ == ChromaDC {
		limit = 4
	} else if typ == ChromaDC422 {
		limit = 8
	}
	seen := uint16(0)
	for _, index := range scan {
		if index < 0 || index >= limit || typ == LumaAC && index == 0 || seen&(uint16(1)<<index) != 0 {
			return fmt.Errorf("cavlc: invalid or duplicate scan index: %w", h264.ErrInvalidData)
		}
		seen |= uint16(1) << index
	}
	return nil
}

// DecodeBlockScan 按调用方提供的规范帧/场扫描直接写入光栅系数，不经过临时重排。
// scan必须为块类别的完整无重复映射，AC排除DC；错误扫描不消费输入或修改dst。
func DecodeBlockScan(r *bitstream.Reader, dst *[16]int32, typ BlockType, nC int, scan []int) (int, error) {
	if err := validateBlockScan(typ, scan); err != nil {
		return 0, err
	}
	return decodeBlock(r, dst, typ, nC, scan)
}

// EncodeBlockScan 对显式扫描复用同一token/level/zeros/run实现，level_prefix上限28。
func EncodeBlockScan(w *bitstream.Writer, src *[16]int32, typ BlockType, nC int, scan []int) (int, error) {
	return EncodeBlockLimitedScan(w, src, typ, nC, 28, scan)
}

// EncodeBlockLimitedScan 同时保留Profile前缀预算和帧/场扫描；失败不写出部分块。
func EncodeBlockLimitedScan(w *bitstream.Writer, src *[16]int32, typ BlockType, nC, maxPrefix int, scan []int) (int, error) {
	if maxPrefix < 15 || maxPrefix > 28 {
		return 0, fmt.Errorf("cavlc: invalid prefix limit: %w", h264.ErrInvalidData)
	}
	if err := validateBlockScan(typ, scan); err != nil {
		return 0, err
	}
	return encodeBlock(w, src, typ, nC, maxPrefix, scan)
}
