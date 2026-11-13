package syntax

import (
	"fmt"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
)

// readFlag 读取一个 u(1) 布尔标志。
func readFlag(r *bitstream.Reader, name string) (bool, error) {
	b, err := r.ReadBit()
	if err != nil {
		return false, fmt.Errorf("syntax: read %s: %w", name, err)
	}
	return b != 0, nil
}

// writeFlag 写出一个 u(1) 布尔标志。
func writeFlag(w *bitstream.Writer, v bool) error {
	if v {
		return w.WriteBit(1)
	}
	return w.WriteBit(0)
}

// readUE 读取 ue(v) 并附加上下文名称。
func readUE(r *bitstream.Reader, name string) (uint32, error) {
	v, err := r.ReadUE()
	if err != nil {
		return 0, fmt.Errorf("syntax: read %s: %w", name, err)
	}
	return v, nil
}

// readSE 读取 se(v) 并附加上下文名称。
func readSE(r *bitstream.Reader, name string) (int32, error) {
	v, err := r.ReadSE()
	if err != nil {
		return 0, fmt.Errorf("syntax: read %s: %w", name, err)
	}
	return v, nil
}

// writeUE 写出 ue(v) 并附加上下文名称。
func writeUE(w *bitstream.Writer, v uint32, name string) error {
	if err := w.WriteUE(v); err != nil {
		return fmt.Errorf("syntax: write %s: %w", name, err)
	}
	return nil
}

// writeSE 写出 se(v) 并附加上下文名称。
func writeSE(w *bitstream.Writer, v int32, name string) error {
	if err := w.WriteSE(v); err != nil {
		return fmt.Errorf("syntax: write %s: %w", name, err)
	}
	return nil
}

// readBits 读取 n 个固定宽度比特并附加上下文名称。
func readBits(r *bitstream.Reader, n uint, name string) (uint64, error) {
	v, err := r.ReadBits(n)
	if err != nil {
		return 0, fmt.Errorf("syntax: read %s: %w", name, err)
	}
	return v, nil
}

// writeBits 写出 n 个固定宽度比特并附加上下文名称。
func writeBits(w *bitstream.Writer, n uint, v uint64, name string) error {
	if err := w.WriteBits(n, v); err != nil {
		return fmt.Errorf("syntax: write %s: %w", name, err)
	}
	return nil
}

// consumeRBSPTrailingBits 校验并消费 rbsp_trailing_bits()。
//
// 与 bitstream.ValidateRBSPTrailingBits 相比，这里还要求尾位恰好从当前
// 位置开始：剩余 0 bit（没有停止位）或超过 8 bit（停止位后还有整字节数据）
// 都视为非法。这样可以发现解析器读少了字段或码流尾部有多余字节。
func consumeRBSPTrailingBits(r *bitstream.Reader) error {
	remaining := r.BitsRemaining()
	if remaining == 0 || remaining > 8 {
		return fmt.Errorf("syntax: rbsp trailing bits occupy %d bits, expected 1..8: %w",
			remaining, h264.ErrInvalidData)
	}
	bits, err := r.PeekBits(uint(remaining))
	if err != nil {
		return err
	}
	stop := uint64(1) << (remaining - 1)
	if bits != stop {
		return fmt.Errorf("syntax: invalid rbsp trailing bits %0*b: %w",
			remaining, bits, h264.ErrInvalidData)
	}
	return r.SkipBits(uint(remaining))
}

// moreRBSPData 判断当前读取位置之后是否还有语法元素（而非只剩尾位）。
//
// 判据：RBSP 的尾位最多 8 bit（一个停止位 1 + 零填充）。如果剩余超过
// 8 bit，必然还有数据；如果剩余不超过 8 bit，只有"剩余值恰好是
// 1<<(n-1)"时才表示停止位就是剩余序列的第一位、后面全是零填充。
// 若 1 不在最高位（例如剩余 0100），说明它前面还有数据位。
//
// 该函数面向 SPS/PPS/SEI 等不含 cabac_zero_word 的 RBSP。
func moreRBSPData(r *bitstream.Reader) (bool, error) {
	remaining := r.BitsRemaining()
	if remaining == 0 {
		return false, nil
	}
	if remaining > 8 {
		return true, nil
	}
	bits, err := r.PeekBits(uint(remaining))
	if err != nil {
		return false, fmt.Errorf("syntax: inspect rbsp tail: %w", err)
	}
	stop := uint64(1) << (remaining - 1)
	return bits != stop, nil
}

// requireMoreRBSP 在 more_rbsp_data() 为真时返回 nil，否则返回 ErrInvalidData。
// 用于必须存在可选扩展字段的语法分支（例如 PPS 的 transform_8x8 扩展）。
func requireMoreRBSP(r *bitstream.Reader, syntaxName string) error {
	more, err := moreRBSPData(r)
	if err != nil {
		return err
	}
	if !more {
		return fmt.Errorf("syntax: %s missing rbsp data: %w", syntaxName, h264.ErrInvalidData)
	}
	return nil
}
