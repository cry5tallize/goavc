package bitstream

import (
	"fmt"
	"math"

	h264 "github.com/cry5tallize/goavc"
)

// maxLeadingZeroBits 是 H.264 §9.1 对 ue(v) 前导零个数的硬上限。
//
// 规范只允许最多 32 个前导零。这个上限同时是拒绝服务防护：
// 如果没有它，一段全零输入会让解码器一直循环到输入耗尽，
// 而每次循环都伴随一次错误路径构造。
const maxLeadingZeroBits = 32

// ReadUE 读取一个无符号 Exp-Golomb 码字（ue(v)），返回 codeNum。
//
// 码字结构（H.264 §9.1）：
//
//	leadingZeroBits 个 0 + 一个 1 + leadingZeroBits 位后缀
//	codeNum = 2^leadingZeroBits - 1 + 后缀值
//
// 返回值使用 uint32：H.264 中所有 ue(v) 语法元素都能放入 32 位。
// 若码字算出的 codeNum 超出 uint32（32 个前导零且后缀非零），
// 返回 ErrInvalidData；这类码字在合法语法中不会出现，宁可拒绝也不截断。
func (r *Reader) ReadUE() (uint32, error) {
	zeros := uint(0)
	for {
		bit, err := r.ReadBit()
		if err != nil {
			return 0, err
		}
		if bit == 1 {
			break
		}
		zeros++
		if zeros > maxLeadingZeroBits {
			return 0, fmt.Errorf("bitstream: ue(v) prefix exceeds %d zero bits at position %d: %w",
				maxLeadingZeroBits, r.BitsRead(), h264.ErrInvalidData)
		}
	}

	if zeros == 0 {
		return 0, nil
	}

	suffix, err := r.ReadBits(zeros)
	if err != nil {
		return 0, err
	}
	// 用 uint64 计算，避免 1<<zeros-1 在 zeros==32 时回绕。
	value := (uint64(1) << zeros) - 1 + suffix
	if value > math.MaxUint32 {
		return 0, fmt.Errorf("bitstream: ue(v) value %d exceeds uint32 at position %d: %w",
			value, r.BitsRead(), h264.ErrInvalidData)
	}
	return uint32(value), nil
}

// ReadSE 读取一个有符号 Exp-Golomb 码字（se(v)）。
//
// se(v) 先把 codeNum 映射为有符号值（H.264 §9.1.1）：
//
//	codeNum 0 → 0
//	codeNum 1 → +1
//	codeNum 2 → -1
//	codeNum 3 → +2
//	codeNum 4 → -2
//
// 返回值使用 int32，覆盖 H.264 中 se(v) 的实际取值范围（例如
// offset_for_non_ref_pic、mb_qp_delta）。超出 int32 的码字返回 ErrInvalidData。
func (r *Reader) ReadSE() (int32, error) {
	codeNum, err := r.ReadUE()
	if err != nil {
		return 0, err
	}
	if codeNum&1 == 1 {
		v := (int64(codeNum) + 1) / 2
		if v > math.MaxInt32 {
			return 0, fmt.Errorf("bitstream: se(v) value %d exceeds int32 at position %d: %w",
				v, r.BitsRead(), h264.ErrInvalidData)
		}
		return int32(v), nil
	}
	v := -int64(codeNum) / 2
	if v < math.MinInt32 {
		return 0, fmt.Errorf("bitstream: se(v) value %d exceeds int32 at position %d: %w",
			v, r.BitsRead(), h264.ErrInvalidData)
	}
	return int32(v), nil
}
