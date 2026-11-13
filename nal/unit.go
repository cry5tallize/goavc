package nal

// Unit 是一个完整的 NAL unit：头部字段 + payload（EBSP，不含起始码或长度前缀）。
//
// Payload 不包含 header 字节；需要原始 NAL 字节时使用 Bytes。
// 所有权：由解析器返回的 Unit 持有独立的 payload 拷贝，调用方修改源缓冲区
// 不会影响 Unit。
type Unit struct {
	Header  Header
	Payload []byte
}

// Bytes 返回 header + payload 的原始 NAL 字节（不含起始码/长度前缀）。
func (u Unit) Bytes() []byte {
	out := make([]byte, 1+len(u.Payload))
	out[0] = u.Header.Byte()
	copy(out[1:], u.Payload)
	return out
}

// IsVCL 判断该 unit 是否属于 VCL。
func (u Unit) IsVCL() bool { return u.Header.IsVCL() }

// IsIDR 判断该 unit 是否为 IDR slice。
func (u Unit) IsIDR() bool { return u.Header.IsIDR() }
