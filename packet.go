package h264

// Packet 表示一个压缩访问单元（Access Unit），而不是单个 NAL Unit。
//
// 为什么以 AU 为单位：
//   - Decoder 的输入单位本来就是 AU：一个 AU 内的多个 slice NAL 属于同一帧，
//     拆开喂入会让 DPB/POC 逻辑不得不自行缓存半个帧。
//   - Encoder 的输出单位同样是 AU：B 帧重排后，一个 AU 的 DTS 与 PTS 可能不同，
//     只有 AU 粒度才能正确表达 DTS/PTS 关系。
//
// 载荷格式：Data 可以是 Annex-B（含起始码）也可以是 AVCC（长度前缀），
// 由上层配置决定；Decoder 会按配置解析，不做自动猜测。
//
// 所有权：SendPacket 成功后 Decoder 不会在调用返回后继续持有 Data 引用
// （如需保留会拷贝到内部缓冲）。因此调用方可以在 SendPacket 返回后复用 Data。
type Packet struct {
	Data []byte

	// PTS 是显示时间戳，DTS 是解码时间戳。无 B 帧时二者通常相等。
	PTS int64
	DTS int64

	// Duration 是该 AU 的显示时长，单位与时间基一致（由调用方约定）。
	Duration int64

	// KeyFrame 表示该 AU 是随机访问点（IDR 或可独立解码的 I 帧）。
	// Decoder 可以据此在数据丢失后更快恢复，但不得据此跳过 PPS/SPS 解析。
	KeyFrame bool
}

// Valid 判断载荷是否非空。空载荷 AU 在 Annex-B 中没有意义，
// 但 flush 语义用长度为零的 Packet 表示"输入结束"是允许的，
// 因此校验放在上层而不是这里。
func (p Packet) Valid() bool { return len(p.Data) > 0 }

// Clone 返回载荷的深拷贝，时间戳与标志原样保留。
//
// 典型用途：把 Packet 交给异步管道后，调用方仍想复用自己收到的缓冲区。
func (p Packet) Clone() Packet {
	out := p
	if p.Data != nil {
		out.Data = make([]byte, len(p.Data))
		copy(out.Data, p.Data)
	}
	return out
}
