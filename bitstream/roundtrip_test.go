package bitstream

import (
	"math"
	"testing"
)

// maskBits 返回低 n 位全 1 的掩码；n == 64 时返回全 1。
func maskBits(n uint) uint64 {
	if n >= 64 {
		return ^uint64(0)
	}
	return (uint64(1) << n) - 1
}

// patternValues 生成覆盖全 0、全 1、交替位与若干边界值的测试向量。
func patternValues(n uint) []uint64 {
	mask := maskBits(n)
	values := []uint64{
		0,
		1 & mask,
		mask,
		0xAAAAAAAAAAAAAAAA & mask,
		0x5555555555555555 & mask,
		0x8000000000000000 & mask,
		0x0123456789ABCDEF & mask,
		0xFEDCBA9876543210 & mask,
	}
	// 确定性伪随机值：使用简单的 LCG，避免测试依赖 math/rand 的实现细节。
	x := uint64(0x9E3779B97F4A7C15)
	for i := 0; i < 16; i++ {
		x = x*6364136223846793005 + 1442695040888963407
		values = append(values, x&mask)
	}
	return values
}

// TestReaderWriter_RoundTripMatrix 覆盖 1..64 bit 宽度、8 种起始偏移与多种位模式。
//
// 起始偏移是关键：它让每次写入都跨字节边界，能暴露 reservoir/累加器的
// 对齐错误；只在字节边界上测试会漏掉绝大多数真实语法场景。
func TestReaderWriter_RoundTripMatrix(t *testing.T) {
	for width := uint(1); width <= 64; width++ {
		for offset := uint(0); offset < 8; offset++ {
			for _, value := range patternValues(width) {
				w := NewWriter()
				if offset > 0 {
					if err := w.WriteBits(offset, maskBits(offset)); err != nil {
						t.Fatalf("写前缀失败: %v", err)
					}
				}
				if err := w.WriteBits(width, value); err != nil {
					t.Fatalf("width=%d offset=%d value=%#x: 写入失败: %v", width, offset, value, err)
				}
				if err := w.WriteBits(8, 0xA5); err != nil {
					t.Fatalf("写后缀失败: %v", err)
				}

				r := NewReader(w.Bytes())
				if offset > 0 {
					prefix, err := r.ReadBits(offset)
					if err != nil || prefix != maskBits(offset) {
						t.Fatalf("width=%d offset=%d: 前缀=%#x err=%v", width, offset, prefix, err)
					}
				}
				got, err := r.ReadBits(width)
				if err != nil {
					t.Fatalf("width=%d offset=%d: 读取失败: %v", width, offset, err)
				}
				if got != value {
					t.Fatalf("width=%d offset=%d: 写入 %#x，读回 %#x", width, offset, value, got)
				}
				suffix, err := r.ReadBits(8)
				if err != nil || suffix != 0xA5 {
					t.Fatalf("width=%d offset=%d: 后缀=%#x err=%v", width, offset, suffix, err)
				}
				checkOnlyZeroPadding(t, r)
			}
		}
	}
}

// TestReaderWriter_SequenceRoundTrip 验证任意宽度序列的连续读写。
func TestReaderWriter_SequenceRoundTrip(t *testing.T) {
	type op struct {
		width uint
		value uint64
	}
	ops := []op{
		{1, 1}, {2, 0}, {3, 5}, {7, 0x55}, {8, 0xFF}, {13, 0x1234},
		{32, 0xDEADBEEF}, {64, 0x0123456789ABCDEF}, {5, 0x1F}, {1, 0},
		{17, 0x1FFFF}, {9, 0x100}, {4, 0xA}, {11, 0x7FF}, {6, 0x2A},
	}
	w := NewWriter()
	for i, o := range ops {
		if err := w.WriteBits(o.width, o.value); err != nil {
			t.Fatalf("第 %d 步写入失败: %v", i, err)
		}
	}
	r := NewReader(w.Bytes())
	for i, o := range ops {
		got, err := r.ReadBits(o.width)
		if err != nil {
			t.Fatalf("第 %d 步读取失败: %v", i, err)
		}
		if got != o.value {
			t.Fatalf("第 %d 步: 写入 %#x，读回 %#x", i, o.value, got)
		}
	}
	checkOnlyZeroPadding(t, r)
}

// checkOnlyZeroPadding 断言剩余比特只可能是最后一个字节的零填充。
//
// Writer.Bytes 会把未满的尾字节补零，因此非 8 倍数的总长度在读回后
// 会留下 0..7 个零比特；这些比特不是数据，但也不是错误。
func checkOnlyZeroPadding(t *testing.T, r *Reader) {
	t.Helper()
	rem := r.BitsRemaining()
	if rem == 0 {
		return
	}
	if rem >= 8 {
		t.Fatalf("剩余 %d bit，超过一个字节的填充", rem)
	}
	pad, err := r.PeekBits(uint(rem))
	if err != nil {
		t.Fatalf("读取 %d bit 填充失败: %v", rem, err)
	}
	if pad != 0 {
		t.Fatalf("剩余 %d bit 填充 = %#x，期望全零", rem, pad)
	}
}

// TestReaderWriter_Extremes 明确覆盖 64 位边界的两个极值。
func TestReaderWriter_Extremes(t *testing.T) {
	for _, value := range []uint64{0, math.MaxUint64} {
		w := NewWriter()
		if err := w.WriteBits(64, value); err != nil {
			t.Fatalf("写入 %#x 失败: %v", value, err)
		}
		r := NewReader(w.Bytes())
		got, err := r.ReadBits(64)
		if err != nil || got != value {
			t.Fatalf("64 bit round-trip %#x -> %#x, err=%v", value, got, err)
		}
	}
}
