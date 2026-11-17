package bitstream

import (
	"fmt"

	h264 "github.com/cry5tallize/goavc"
)

// Reader 从字节切片中按位读取，内部使用 64 bit 位容器（bit reservoir）。
//
// 为什么需要 reservoir：H.264 的语法元素宽度不固定（1..32 bit 很常见），
// 如果每次读取都逐比特移位，热路径会被函数调用和分支淹没。这里一次性把
// 若干字节装入 uint64，读取时只做一次右移；对上层暴露的仍是精确的位语义。
//
// 内存语义：Reader 不复制输入，调用方在 Reader 使用期间不得修改 data。
// Reader 不持有任何堆分配（零值也不可用，必须经 NewReader 构造）。
//
// 越界语义：任何超出输入末尾的读取都返回包装了 h264.ErrInvalidData 的错误，
// 且不推进读取位置。调用方可以据此区分"码流截断"与"语法非法"。
type Reader struct {
	data []byte

	// pos 是下一个尚未装入 reservoir 的字节下标。
	pos int

	// reservoir 的有效位左对齐（最高位是下一个待读位）。
	// nbits 是其中尚未消费的有效位数，范围 0..64。
	reservoir uint64
	nbits     uint

	// consumed 是已经从输入中消费的位数，用于位置计算与 ByteAlign。
	consumed uint64
}

// NewReader 构造一个读取 data 的位读取器。
//
// data 为 nil 或空切片时，任何非零位读取都会返回截断错误。
func NewReader(data []byte) *Reader {
	return &Reader{data: data}
}

// ReadBits 读取 n 个比特并返回其无符号整数值。
//
// n 的范围是 0..64。n == 0 时返回 0 且不改变位置；n > 64 属于调用方错误，
// 同样返回 ErrInvalidData 而不是 panic。读取失败时位置保持不变。
//
// 超过 56 bit 的读取会拆成两段：reservoir 最多容纳 64 bit，而 refill 只在
// 剩余空间足够装入一个完整字节时装载（nbits <= 56）。直接请求 64 bit 时，
// reservoir 可能只有 57..63 位可用；先读高位再读低 56 位可以避免这个缺口。
func (r *Reader) ReadBits(n uint) (uint64, error) {
	if n > 64 {
		return 0, fmt.Errorf("bitstream: read width %d exceeds 64 bits: %w", n, h264.ErrInvalidData)
	}
	if n == 0 {
		return 0, nil
	}
	// 先整体检查，保证失败读取不推进位置；否则第二段失败时第一段已被消费。
	if uint64(n) > r.BitsRemaining() {
		return 0, fmt.Errorf("bitstream: need %d bits at position %d but only %d remain: %w",
			n, r.consumed, r.BitsRemaining(), h264.ErrInvalidData)
	}
	if n <= 56 {
		return r.readBitsSmall(n)
	}

	hi, err := r.readBitsSmall(n - 56)
	if err != nil {
		return 0, err
	}
	lo, err := r.readBitsSmall(56)
	if err != nil {
		return 0, err
	}
	return hi<<56 | lo, nil
}

// readBitsSmall 读取不超过 56 bit 的值；调用方必须保证输入足够。
func (r *Reader) readBitsSmall(n uint) (uint64, error) {
	if n == 0 {
		return 0, nil
	}
	r.refill()
	if uint(r.nbits) < n {
		// 理论上不可达（ReadBits 已做总长度检查），保留为防御性分支。
		return 0, fmt.Errorf("bitstream: need %d bits at position %d but only %d in reservoir: %w",
			n, r.consumed, r.nbits, h264.ErrInvalidData)
	}
	// reservoir 左对齐：最高 n 位就是接下来的 n 个比特。
	v := r.reservoir >> (64 - n)
	r.consume(n)
	return v, nil
}

// PeekBits 预读接下来 n 个比特，但不推进读取位置。
//
// 该函数主要用于需要判断"剩余比特是否只是 rbsp_trailing_bits"的场景。
// 实现方式是复制 Reader 状态后执行一次 ReadBits：data 只读，复制结构体
// 不会影响原 Reader；这比单独维护一套"预读但不消费"的 reservoir 逻辑更
// 不容易出现边界偏差。
func (r *Reader) PeekBits(n uint) (uint64, error) {
	tmp := *r
	return tmp.ReadBits(n)
}

// ReadBit 读取单个比特，返回 0 或 1。
func (r *Reader) ReadBit() (uint8, error) {
	if r.nbits != 0 {
		v := uint8(r.reservoir >> 63)
		r.reservoir <<= 1
		r.nbits--
		r.consumed++
		return v, nil
	}
	return r.readBitRefill()
}

// 冷路径保持ReadBits的尾部原子失败及错误文本；热单比特只消费已有reservoir。
//
//go:noinline
func (r *Reader) readBitRefill() (uint8, error) {
	v, err := r.ReadBits(1)
	if err != nil {
		return 0, err
	}
	return uint8(v), nil
}

// ReadByte 读取下一个 8 bit 并返回一个字节。
//
// 注意：它不要求当前处于字节对齐位置。H.264 中需要 alignment 的语法应显式
// 调用 AlignToByte；本方法只表达"读取 8 个连续比特"。
func (r *Reader) ReadByte() (byte, error) {
	v, err := r.ReadBits(8)
	if err != nil {
		return 0, err
	}
	return byte(v), nil
}

// SkipBits 跳过 n 个比特。
//
// 跳过同样受输入长度约束：如果剩余比特不足，返回截断错误且位置不变。
func (r *Reader) SkipBits(n uint) error {
	if n == 0 {
		return nil
	}
	// 先整体检查，保证"失败不推进位置"的契约；否则分段读取会在中途失败，
	// 让调用方看到一个既没跳过完整区间、又已经移动过的 Reader。
	if uint64(n) > r.BitsRemaining() {
		return fmt.Errorf("bitstream: skip %d bits at position %d but only %d remain: %w",
			n, r.consumed, r.BitsRemaining(), h264.ErrInvalidData)
	}
	// 分段消费，避免一次请求超过 64 bit。
	for n > 0 {
		chunk := min(n, 64)
		if _, err := r.ReadBits(chunk); err != nil {
			return err
		}
		n -= chunk
	}
	return nil
}

// AlignToByte 丢弃当前字节内的剩余比特，使下一次读取从字节边界开始。
//
// 它对应 H.264 的 byte_alignment()：如果已经对齐则是空操作。
func (r *Reader) AlignToByte() error {
	n := (8 - (r.consumed % 8)) % 8
	if n == 0 {
		return nil
	}
	_, err := r.ReadBits(uint(n))
	return err
}

// ByteAligned 报告当前读取位置是否处于字节边界。
func (r *Reader) ByteAligned() bool { return r.consumed%8 == 0 }

// BitsRead 返回已经从输入中消费的比特数。
func (r *Reader) BitsRead() uint64 { return r.consumed }

// BitsRemaining 返回输入中尚未消费的比特数。
func (r *Reader) BitsRemaining() uint64 {
	total := uint64(len(r.data)) * 8
	if r.consumed >= total {
		return 0
	}
	return total - r.consumed
}

// refill 把输入字节装入 reservoir，直到没有空间或输入耗尽。
//
// 不变量：nbits 始终等于 reservoir 中有效位的数量，有效位左对齐。
// 每次最多装入 8 bit，因此 56 是安全阈值（56+8=64）。
func (r *Reader) refill() {
	for r.nbits <= 56 && r.pos < len(r.data) {
		shift := 56 - r.nbits
		r.reservoir |= uint64(r.data[r.pos]) << shift
		r.pos++
		r.nbits += 8
	}
}

// consume 丢弃 reservoir 顶部的 n 个比特。
//
// n 必须已经通过 PeekBits 校验（n <= nbits）。当 n == 64 时，Go 的移位语义
// 会把 reservoir 清零，正好符合"全部消费"的预期。
func (r *Reader) consume(n uint) {
	r.reservoir <<= n
	r.nbits -= n
	r.consumed += uint64(n)
}
