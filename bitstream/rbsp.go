package bitstream

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
