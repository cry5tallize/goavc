package bitstream

import (
	"fmt"

	h264 "github.com/goavc/h264"
)

// EBSPToRBSP 把 EBSP 转换为 RBSP：移除防竞争字节（emulation_prevention_three_byte）。
//
// 规则（H.264 §7.4.1）：EBSP 中每出现 00 00 03，03 就是插入的防竞争字节，
// 解码时必须丢弃；如果 00 00 后直接跟 00/01/02，说明码流没有正确插入
// 防竞争字节，属于非法 NAL payload，返回 ErrInvalidData。
//
// 注意零游程的维护：丢弃 03 后必须把连续零计数清零，因为紧随其后的字节
// 才是被转义的原始字节；例如 RBSP 00 00 00 00 会被编码成
// EBSP 00 00 03 00 00，处理 03 后应把下一个 00 当作普通数据重新计数。
//
// 返回值总是新分配的切片，不修改输入。空输入返回 nil。
func EBSPToRBSP(ebsp []byte) ([]byte, error) {
	if len(ebsp) == 0 {
		return nil, nil
	}
	out := make([]byte, 0, len(ebsp))
	zeros := 0
	for i, b := range ebsp {
		if zeros >= 2 {
			switch {
			case b == 0x03:
				// 只有后面还有字节时，03 才可能是有效的防竞争字节。
				// 末尾悬挂的 03 表示码流被截断，不能静默接受。
				if i == len(ebsp)-1 {
					return nil, fmt.Errorf("bitstream: dangling emulation prevention byte at offset %d: %w",
						i, h264.ErrInvalidData)
				}
				zeros = 0
				continue
			case b <= 0x02:
				return nil, fmt.Errorf("bitstream: unescaped 00 00 %02x at offset %d: %w",
					b, i, h264.ErrInvalidData)
			}
		}
		out = append(out, b)
		if b == 0 {
			zeros++
		} else {
			zeros = 0
		}
	}
	return out, nil
}

// ValidateRBSPTrailingBits 校验 RBSP 结尾的 rbsp_trailing_bits()。
//
// 结构（H.264 §7.3.2.10）：一个 rbsp_stop_one_bit（值为 1），后面跟零个或
// 多个 rbsp_alignment_zero_bit，直到字节边界。因此最后一个字节必须非零：
// 如果最后一个字节是 0x00，说明停止位之后又出现了整字节的零，这既不是
// 对齐填充，也不属于该 RBSP 的语法。
//
// 该函数面向 SPS/PPS/SEI 等不包含 cabac_zero_word 的 RBSP。CABAC slice 的
// rbsp_slice_trailing_bits() 允许随后追加零字，解析 slice 时应使用单独的
// 规则（M2/M3 落地），不能直接套用本函数。
func ValidateRBSPTrailingBits(rbsp []byte) error {
	if len(rbsp) == 0 {
		return fmt.Errorf("bitstream: empty rbsp has no trailing bits: %w", h264.ErrInvalidData)
	}
	if rbsp[len(rbsp)-1] == 0x00 {
		return fmt.Errorf("bitstream: rbsp ends with zero byte at offset %d, missing stop bit: %w",
			len(rbsp)-1, h264.ErrInvalidData)
	}
	return nil
}

// WriteRBSPTrailingBits 写入 rbsp_trailing_bits()：一个停止位 1，然后补零对齐。
//
// 调用方应在写完语法元素后调用本函数，再通过 Writer.Bytes 取得 RBSP。
func WriteRBSPTrailingBits(w *Writer) error {
	if err := w.WriteBit(1); err != nil {
		return err
	}
	w.ByteAlign()
	return nil
}

// RBSPToEBSP 把 RBSP 转换为 EBSP：在需要时插入防竞争字节
// emulation_prevention_three_byte（0x03）。
//
// 规则（H.264 §7.4.1）：当 RBSP 中已经出现两个连续 0x00，且下一个字节
// 的值 <= 0x03 时，在下一个字节前插入 0x03。这样 NAL payload 内就不会
// 意外出现 00 00 00 / 00 00 01 / 00 00 02 / 00 00 03 这些会与起始码或
// 其他语法混淆的序列。
//
// 返回值总是新分配的切片，不修改输入。空输入返回 nil。
func RBSPToEBSP(rbsp []byte) []byte {
	if len(rbsp) == 0 {
		return nil
	}
	// 最坏情况每个字节前都插入一个 0x03；预分配 len*2 避免中途扩容。
	out := make([]byte, 0, len(rbsp)*2)
	zeros := 0
	for _, b := range rbsp {
		if zeros >= 2 && b <= 0x03 {
			out = append(out, 0x03)
			// 插入后输出序列以 0x03 结尾，连续零计数必须清零；
			// 否则会把后面的字节误判为"第三个零之后"。
			zeros = 0
		}
		out = append(out, b)
		if b == 0 {
			zeros++
		} else {
			zeros = 0
		}
	}
	return out
}
