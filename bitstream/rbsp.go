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
