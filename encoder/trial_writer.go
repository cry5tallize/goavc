package encoder

import "github.com/cry5tallize/goavc/bitstream"

// 两个Writer属于同一个串行lane，跨帧/QP尝试复用。RD候选与正式MB试写
// 使用不同槽，不会覆盖仍被外层消费的位串；不借用或复用最终Packet.Data。
type trialWriters struct {
	rd, mb bitstream.Writer
}

func newTrialWriters() *trialWriters {
	return &trialWriters{rd: *bitstream.NewWriter(), mb: *bitstream.NewWriter()}
}
