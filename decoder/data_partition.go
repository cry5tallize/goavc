package decoder

import (
	"fmt"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/bitstream"
	"github.com/cry5tallize/goavc/nal"
	"github.com/cry5tallize/goavc/syntax"
)

// partitionedSlice 保持 A 的关联上下文与独立的 B/C 读取位置。
// Reader 自身持有去防竞争后的 RBSP；不拼接分类数据，也不复制残差码流。
type partitionedSlice struct {
	context syntax.DataPartitionContext
	readers [2]*bitstream.Reader
	used    [2]bool
}

// residualReader 仅在即将读取残差或 PCM 语法时调用；不能按 slice 类型
// 猜测分区，因为 P/B/SP slice 中的帧内宏块同样从 B 读取 category 3。
func (s *sliceEntry) residualReader(intra bool) (*bitstream.Reader, error) {
	if s.partition == nil {
		return s.data, nil
	}
	index := 1
	if intra {
		index = 0
	}
	r := s.partition.readers[index]
	if r == nil {
		return nil, fmt.Errorf("decoder: missing residual partition %c for slice_id %d: %w", 'B'+index, s.partition.context.SliceID, h264.ErrInvalidData)
	}
	s.partition.used[index] = true
	return r, nil
}

// attachPartition 校验 B/C 的前缀与 A 的上下文后，保留其独立 reader。
// 分区的网络顺序、slice_id 查找与总缓冲预算由访问单元装配层处理。
func attachPartition(s *sliceEntry, u nal.Unit) error {
	if s.partition == nil {
		return fmt.Errorf("decoder: partition B/C without partition A context: %w", h264.ErrInvalidData)
	}
	index := 0
	switch u.Header.Type {
	case nal.TypeSlicePartitionB:
	case nal.TypeSlicePartitionC:
		index = 1
	default:
		return fmt.Errorf("decoder: expected partition B/C NAL, got %d: %w", u.Header.Type, h264.ErrInvalidData)
	}
	if index == 0 && s.partition.readers[1] != nil {
		return fmt.Errorf("decoder: partition B follows C: %w", h264.ErrInvalidData)
	}
	if s.partition.readers[index] != nil {
		return fmt.Errorf("decoder: duplicate partition %c for slice_id %d: %w", 'B'+index, s.partition.context.SliceID, h264.ErrInvalidData)
	}
	rbsp, err := bitstream.EBSPToRBSP(u.Payload)
	if err != nil {
		return fmt.Errorf("decoder: partition %c RBSP: %w", 'B'+index, err)
	}
	hdr, err := syntax.ParseDataPartitionBC(syntax.DataPartitionInput{RBSP: rbsp, BitLength: uint64(len(rbsp)) * 8, Header: u.Header}, s.partition.context)
	if err != nil {
		return err
	}
	r := bitstream.NewReader(rbsp)
	if err := r.SkipBits(uint(hdr.DataBitOffset)); err != nil {
		return err
	}
	s.partition.readers[index] = r
	return nil
}

// validatePartitionTrailing 严格消费 B/C 各自的 rbsp_trailing_bits。
// Spec: 7.4.1，携带的分区必须包含所属 category 的语法元素；只含
// slice_id 和尾位的空 B/C 不合法。A 的尾位仍由 slice_data 遍历核对。
func validatePartitionTrailing(s *sliceEntry) error {
	if s.partition == nil {
		return nil
	}
	for index, r := range s.partition.readers {
		if r == nil {
			continue
		}
		if !s.partition.used[index] {
			return fmt.Errorf("decoder: partition %c has no residual syntax for slice_id %d: %w", 'B'+index, s.partition.context.SliceID, h264.ErrInvalidData)
		}
		remaining := r.BitsRemaining()
		if remaining == 0 || remaining > 8 {
			return fmt.Errorf("decoder: partition %c trailing length %d: %w", 'B'+index, remaining, h264.ErrInvalidData)
		}
		bits, err := r.ReadBits(uint(remaining))
		if err != nil {
			return err
		}
		if bits != uint64(1)<<(remaining-1) {
			return fmt.Errorf("decoder: invalid partition %c trailing bits: %w", 'B'+index, h264.ErrInvalidData)
		}
	}
	return nil
}
