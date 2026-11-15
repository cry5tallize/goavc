package cavlc

import (
	"fmt"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
)

// EncodeBlock 写出光栅顺序的残差 level，返回 totalCoeff。
// Spec: 9.2。直接复用规范 VLC 的码长/码字表，不另维护一份反向生成表。
// LumaAC 跳过 DC；ChromaDC/ChromaDC422 使用前四/八项；色度 DC 类别隐含 nC=-1/-2。
func EncodeBlock(w *bitstream.Writer, src *[16]int32, typ BlockType, nC int) (int, error) {
	return encodeBlock(w, src, typ, nC, 28, nil)
}

// EncodeBlockLimited 限制 level_prefix；Baseline/Main 的 Annex A 上限为15。
// 超出是合法高 Profile 工具，不可静默写入受限 Profile，返回 ErrUnsupported。
func EncodeBlockLimited(w *bitstream.Writer, src *[16]int32, typ BlockType, nC, maxPrefix int) (int, error) {
	if maxPrefix < 15 || maxPrefix > 28 {
		return 0, fmt.Errorf("cavlc: invalid prefix limit: %w", h264.ErrInvalidData)
	}
	return encodeBlock(w, src, typ, nC, maxPrefix, nil)
}

func encodeBlock(w *bitstream.Writer, src *[16]int32, typ BlockType, nC, maxPrefix int, scan []int) (int, error) {
	if w == nil || src == nil || typ > ChromaDC422 {
		return 0, fmt.Errorf("cavlc: invalid block: %w", h264.ErrInvalidData)
	}
	if scan == nil {
		scan = defaultBlockScan(typ)
	}
	var levels [16]int32
	var positions [16]int
	total := 0
	for pos, index := range scan {
		if src[index] != 0 {
			levels[total] = src[index]
			positions[total] = pos
			total++
		}
	}
	trailing := 0
	for i := total - 1; i >= 0 && trailing < 3; i-- {
		if levels[i] != 1 && levels[i] != -1 {
			break
		}
		trailing++
	}
	// 唯一可能失败的数据计算是level表示与Profile前缀限额；写任何位之前
	// 用固定槽准备完整level计划，保留原失败原子性而不为每块创建临时Writer。
	var plan [16]levelPlan
	suffix := 0
	if total > 10 && trailing < 3 {
		suffix = 1
	}
	for i := total - 1 - trailing; i >= 0; i-- {
		v := int64(levels[i])
		abs := v
		if abs < 0 {
			abs = -abs
		}
		code := abs*2 - 2
		if v < 0 {
			code++
		}
		first := i == total-1-trailing
		if first && trailing < 3 {
			code -= 2
		}
		prepared, err := prepareLevel(code, suffix, maxPrefix)
		if err != nil {
			return 0, err
		}
		plan[i] = prepared
		if suffix == 0 {
			suffix = 1
		}
		if suffix < 6 && abs > int64(3<<(suffix-1)) {
			suffix++
		}
	}
	symbol := total*4 + trailing
	if typ == ChromaDC {
		w.WriteBits(uint(chromaDCCoeffTokenLen[symbol]), uint64(chromaDCCoeffTokenBits[symbol]))
	} else if typ == ChromaDC422 {
		w.WriteBits(uint(chromaDC422CoeffTokenLen[symbol]), uint64(chromaDC422CoeffTokenBits[symbol]))
	} else {
		table := coeffTokenTableIndex(max(0, nC))
		w.WriteBits(uint(coeffTokenLen[table][symbol]), uint64(coeffTokenBits[table][symbol]))
	}
	for i := 0; i < trailing; i++ {
		var sign uint64
		if levels[total-1-i] < 0 {
			sign = 1
		}
		w.WriteBits(1, sign)
	}
	for i := total - 1 - trailing; i >= 0; i-- {
		plan[i].write(w)
	}
	if total > 0 {
		zeros := positions[total-1] + 1 - total
		if total < len(scan) {
			if typ == ChromaDC {
				w.WriteBits(uint(chromaDCTotalZerosLen[total-1][zeros]), uint64(chromaDCTotalZerosBits[total-1][zeros]))
			} else if typ == ChromaDC422 {
				w.WriteBits(uint(chromaDC422TotalZerosLen[total-1][zeros]), uint64(chromaDC422TotalZerosBits[total-1][zeros]))
			} else {
				w.WriteBits(uint(totalZerosLen[total-1][zeros]), uint64(totalZerosBits[total-1][zeros]))
			}
		}
		for i := total - 1; i > 0 && zeros > 0; i-- {
			run := positions[i] - positions[i-1] - 1
			table := min(zeros, 7) - 1
			w.WriteBits(uint(runLen[table][run]), uint64(runBits[table][run]))
			zeros -= run
		}
	}
	return total, nil
}

type levelPlan struct {
	value        uint64
	prefix, size uint8
}

func (p levelPlan) write(w *bitstream.Writer) {
	w.WriteBits(uint(p.prefix), 0)
	w.WriteBit(1)
	w.WriteBits(uint(p.size), p.value)
}

// prepareLevel 显式枚举规范前缀区间，避免 suffixLength=0 的两个特殊区间
// （prefix=14 和 >=15）被普通除法公式吞掉。
func prepareLevel(code int64, suffix, maxPrefix int) (levelPlan, error) {
	for prefix := 0; prefix <= 28; prefix++ {
		size := suffix
		base := int64(prefix) << suffix
		if prefix == 14 && suffix == 0 {
			size = 4
		}
		if prefix >= 15 {
			size = prefix - 3
			base = int64(15) << suffix
			if suffix == 0 {
				base += 15
			}
			if prefix >= 16 {
				base += (int64(1) << (prefix - 3)) - 4096
			}
		}
		if code < base || code-base >= int64(1)<<size {
			continue
		}
		if prefix > maxPrefix {
			return levelPlan{}, fmt.Errorf("cavlc: level_prefix %d exceeds profile limit %d: %w", prefix, maxPrefix, h264.ErrUnsupported)
		}
		return levelPlan{value: uint64(code - base), prefix: uint8(prefix), size: uint8(size)}, nil
	}
	return levelPlan{}, fmt.Errorf("cavlc: level exceeds bounded escape representation: %w", h264.ErrInvalidData)
}
