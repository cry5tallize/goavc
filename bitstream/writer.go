package bitstream

import "fmt"

// Writer 按位写出比特流，支持 0..64 位的单次写入与字节对齐。
//
// 实现使用一个字节累加器而不是 64 bit reservoir：写侧每次最多把一个
// 64 位值拆成 8 个字节，循环开销可以忽略，而字节累加器让 ByteAlign 与
// Bytes 的语义更直接（不存在"半个 reservoir 是否已输出"的歧义）。
//
// Writer 不做 I/O，只维护内存缓冲；容量按需增长。零值不可用，
// 必须通过 NewWriter 构造。
//
// 错误语义：写侧的非法宽度或超出宽度的值属于调用方编程错误，
// 返回普通 error（不包装 h264.ErrInvalidData）。写入失败时缓冲区不变。
type Writer struct {
	buf []byte

	// cur 是尚未写满的当前字节，低位补零；nbits 是已填充的高位数（0..7）。
	cur   byte
	nbits uint
}

// NewWriter 创建一个空 Writer。
func NewWriter() *Writer { return &Writer{} }

// WriteBits 写入 n 个比特，value 的高 n 位被忽略、低 n 位按 MSB-first 写出。
//
// n 的范围是 0..64。n == 0 时只接受 value == 0，用于让调用方显式表达
// "写入零位"；n < 64 时 value 必须能放入 n 位，否则返回错误。
func (w *Writer) WriteBits(n uint, value uint64) error {
	if n > 64 {
		return fmt.Errorf("bitstream: write width %d exceeds 64 bits", n)
	}
	if n == 0 {
		if value != 0 {
			return fmt.Errorf("bitstream: zero-width write with non-zero value %#x", value)
		}
		return nil
	}
	if n < 64 && value>>n != 0 {
		return fmt.Errorf("bitstream: value %#x does not fit in %d bits", value, n)
	}

	// 逐段填入当前字节，保证 n == 64 与跨字节写入都走同一条路径。
	remaining := n
	for remaining > 0 {
		space := 8 - w.nbits
		chunk := min(remaining, space)
		shift := remaining - chunk
		part := byte((value >> shift) & ((uint64(1) << chunk) - 1))
		w.cur |= part << (space - chunk)
		w.nbits += chunk
		remaining -= chunk
		if w.nbits == 8 {
			w.buf = append(w.buf, w.cur)
			w.cur = 0
			w.nbits = 0
		}
	}
	return nil
}

// WriteBit 写入单个比特；bit 只能是 0 或 1。
func (w *Writer) WriteBit(bit uint8) error {
	if bit > 1 {
		return fmt.Errorf("bitstream: invalid bit value %d", bit)
	}
	return w.WriteBits(1, uint64(bit))
}

// WriteByte 写入一个字节（8 个连续比特）。
func (w *Writer) WriteByte(b byte) error {
	return w.WriteBits(8, uint64(b))
}

// ByteAlign 用零比特补齐当前字节，使后续写入从字节边界开始。
func (w *Writer) ByteAlign() {
	if w.nbits == 0 {
		return
	}
	// cur 中的有效位始终左对齐，低位已经是零，直接提交即可。
	w.buf = append(w.buf, w.cur)
	w.cur = 0
	w.nbits = 0
}

// Bytes 返回已写出的字节序列（副本）。
//
// 如果当前处于非字节对齐状态，返回值会把最后一个未满字节用零补齐。
// 该操作不改变 Writer 状态：调用 Bytes 后仍可继续写入剩余比特。
func (w *Writer) Bytes() []byte {
	out := make([]byte, len(w.buf), len(w.buf)+1)
	copy(out, w.buf)
	if w.nbits != 0 {
		// cur 中的有效位左对齐，低位为零，正是"用零补齐"的字节视图。
		out = append(out, w.cur)
	}
	return out
}

// BitsWritten 返回已经写入的比特总数。
func (w *Writer) BitsWritten() uint64 {
	return uint64(len(w.buf))*8 + uint64(w.nbits)
}

// Reset 清空缓冲，回到初始状态，但保留已分配的容量供复用。
func (w *Writer) Reset() {
	w.buf = w.buf[:0]
	w.cur = 0
	w.nbits = 0
}
