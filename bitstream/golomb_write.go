package bitstream

import (
	"fmt"
	"math/bits"
)

// WriteUE 写出无符号 Exp-Golomb 码字（ue(v)）。
//
// 写法与 ReadUE 对称（H.264 §9.1）：
//
//	codeNum+1 的二进制长度 numBits
//	先写 numBits-1 个 0，再写 codeNum+1 的 numBits 位
//
// 使用 uint64 计算 codeNum+1，保证 value == MaxUint32 时不会回绕。
func (w *Writer) WriteUE(value uint32) error {
	n := uint64(value) + 1
	numBits := uint(bits.Len64(n))
	if err := w.WriteBits(numBits-1, 0); err != nil {
		return err
	}
	return w.WriteBits(numBits, n)
}

// WriteSE 写出有符号 Exp-Golomb 码字（se(v)）。
//
// 映射关系（H.264 §9.1.1）：
//
//	se > 0 → codeNum = 2*se - 1
//	se < 0 → codeNum = -2*se
//	se = 0 → codeNum = 0
//
// MinInt32 的 -2*se 等于 2^32，无法用 uint32 codeNum 表示；该值也永远不会
// 由 ReadSE 产生（ReadSE 的取值范围是 [-MaxInt32, MaxInt32]）。
// 遇到它时返回错误且不写入任何比特。
func (w *Writer) WriteSE(value int32) error {
	var codeNum uint64
	switch {
	case value > 0:
		codeNum = uint64(value)*2 - 1
	case value < 0:
		codeNum = uint64(-int64(value)) * 2
	default:
		codeNum = 0
	}
	if codeNum > uint64(^uint32(0)) {
		return fmt.Errorf("bitstream: se(v) value %d exceeds uint32 codeNum range", value)
	}
	return w.WriteUE(uint32(codeNum))
}
