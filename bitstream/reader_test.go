package bitstream

import (
	"errors"
	"testing"

	h264 "github.com/goavc/h264"
)

// TestReader_ReadBits_Sequential 验证跨字节边界的连续读取。
func TestReader_ReadBits_Sequential(t *testing.T) {
	// 10110010 01011101 11111111
	r := NewReader([]byte{0xB2, 0x5D, 0xFF})

	cases := []struct {
		n    uint
		want uint64
	}{
		{1, 0b1},
		{3, 0b011},
		{4, 0b0010},
		{8, 0x5D},
		{4, 0xF},
		{4, 0xF},
	}
	for i, c := range cases {
		got, err := r.ReadBits(c.n)
		if err != nil {
			t.Fatalf("第 %d 次读取 %d bit 失败: %v", i, c.n, err)
		}
		if got != c.want {
			t.Fatalf("第 %d 次读取 %d bit = %#x，期望 %#x", i, c.n, got, c.want)
		}
	}
	if !r.ByteAligned() {
		t.Fatalf("读完 24 bit 后应处于字节边界，实际 %d bit", r.BitsRead())
	}
	if r.BitsRemaining() != 0 {
		t.Fatalf("剩余比特 = %d，期望 0", r.BitsRemaining())
	}
}

// TestReader_ReadBits_CrossesByteBoundary 覆盖单次读取跨越 8 bit 边界的情形。
func TestReader_ReadBits_CrossesByteBoundary(t *testing.T) {
	// 00000000 11111111
	r := NewReader([]byte{0x00, 0xFF})
	got, err := r.ReadBits(4)
	if err != nil || got != 0 {
		t.Fatalf("前 4 bit = %#x, err = %v", got, err)
	}
	got, err = r.ReadBits(8)
	if err != nil {
		t.Fatalf("跨字节读取失败: %v", err)
	}
	if got != 0x0F {
		t.Fatalf("跨字节 8 bit = %#x，期望 0x0f", got)
	}
	got, err = r.ReadBits(4)
	if err != nil || got != 0x0F {
		t.Fatalf("后 4 bit = %#x, err = %v", got, err)
	}
}

// TestReader_ReadBits_64 验证一次读取整个 reservoir。
func TestReader_ReadBits_64(t *testing.T) {
	data := []byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF}
	r := NewReader(data)
	got, err := r.ReadBits(64)
	if err != nil {
		t.Fatalf("读取 64 bit 失败: %v", err)
	}
	if got != 0x0123456789ABCDEF {
		t.Fatalf("64 bit = %#x", got)
	}
	if r.BitsRemaining() != 0 {
		t.Fatalf("剩余 %d bit，期望 0", r.BitsRemaining())
	}
}

// TestReader_TruncatedDoesNotAdvance 验证截断读取不会移动位置。
func TestReader_TruncatedDoesNotAdvance(t *testing.T) {
	r := NewReader([]byte{0xFF})
	if _, err := r.ReadBits(9); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("越界读取错误 = %v，期望 ErrInvalidData", err)
	}
	if r.BitsRead() != 0 {
		t.Fatalf("失败读取后位置 = %d，期望 0", r.BitsRead())
	}
	if got, err := r.ReadBits(8); err != nil || got != 0xFF {
		t.Fatalf("恢复读取 = %#x, err = %v", got, err)
	}
}

// TestReader_InvalidWidth 验证 n > 64 被显式拒绝。
func TestReader_InvalidWidth(t *testing.T) {
	r := NewReader(make([]byte, 16))
	if _, err := r.ReadBits(65); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("n=65 错误 = %v，期望 ErrInvalidData", err)
	}
}

// TestReader_PeekDoesNotAdvance 验证 PeekBits 不消费比特。
func TestReader_PeekDoesNotAdvance(t *testing.T) {
	r := NewReader([]byte{0b10110010})
	peek, err := r.PeekBits(5)
	if err != nil || peek != 0b10110 {
		t.Fatalf("PeekBits(5) = %#x, err = %v", peek, err)
	}
	if r.BitsRead() != 0 {
		t.Fatalf("Peek 后位置 = %d，期望 0", r.BitsRead())
	}
	got, err := r.ReadBits(5)
	if err != nil || got != peek {
		t.Fatalf("Peek 后 ReadBits = %#x, err = %v", got, err)
	}
	if again, err := r.PeekBits(3); err != nil || again != 0b010 {
		t.Fatalf("继续 PeekBits(3) = %#x, err = %v", again, err)
	}
}

// TestReader_AlignToByte 验证字节对齐会丢弃当前字节的剩余位。
func TestReader_AlignToByte(t *testing.T) {
	r := NewReader([]byte{0xFF, 0x80})
	if _, err := r.ReadBits(3); err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if err := r.AlignToByte(); err != nil {
		t.Fatalf("对齐失败: %v", err)
	}
	if !r.ByteAligned() || r.BitsRead() != 8 {
		t.Fatalf("对齐后位置 = %d，aligned = %v", r.BitsRead(), r.ByteAligned())
	}
	// 已对齐时不应再消耗比特。
	if err := r.AlignToByte(); err != nil {
		t.Fatalf("重复对齐失败: %v", err)
	}
	if r.BitsRead() != 8 {
		t.Fatalf("重复对齐后位置 = %d，期望 8", r.BitsRead())
	}
	got, err := r.ReadBit()
	if err != nil || got != 1 {
		t.Fatalf("对齐后读取 = %d, err = %v", got, err)
	}
}

// TestReader_ReadByteUnaligned 验证 ReadByte 读取连续 8 bit，而非要求对齐。
func TestReader_ReadByteUnaligned(t *testing.T) {
	r := NewReader([]byte{0b10110010, 0b01011101})
	if _, err := r.ReadBits(3); err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	got, err := r.ReadByte()
	if err != nil {
		t.Fatalf("ReadByte 失败: %v", err)
	}
	// 跳过 101 后，剩下的 8 bit 是 10010 010。
	if got != 0b10010010 {
		t.Fatalf("ReadByte = %#x，期望 0x92", got)
	}
}

// TestReader_SkipBits 验证跳过失败的原子性。
func TestReader_SkipBits(t *testing.T) {
	r := NewReader([]byte{0xFF})
	if err := r.SkipBits(9); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("越界跳过错误 = %v，期望 ErrInvalidData", err)
	}
	if r.BitsRead() != 0 {
		t.Fatalf("失败跳过后位置 = %d，期望 0", r.BitsRead())
	}
	if err := r.SkipBits(8); err != nil || r.BitsRead() != 8 {
		t.Fatalf("跳过 8 bit 后位置 = %d, err = %v", r.BitsRead(), err)
	}
	if err := r.SkipBits(1); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("末尾越界跳过错误 = %v，期望 ErrInvalidData", err)
	}
	if err := r.SkipBits(0); err != nil {
		t.Fatalf("跳过 0 bit 失败: %v", err)
	}
}

// TestReader_AllZero 覆盖 reservoir 全零与边界填充。
func TestReader_AllZero(t *testing.T) {
	r := NewReader(make([]byte, 16))
	got, err := r.ReadBits(64)
	if err != nil || got != 0 {
		t.Fatalf("全零读取 = %#x, err = %v", got, err)
	}
	if r.BitsRemaining() != 64 {
		t.Fatalf("剩余 = %d，期望 64", r.BitsRemaining())
	}
}

// TestReader_EmptyInput 验证空输入的错误语义。
func TestReader_EmptyInput(t *testing.T) {
	r := NewReader(nil)
	if got, err := r.ReadBits(0); err != nil || got != 0 {
		t.Fatalf("读取 0 bit = %#x, err = %v", got, err)
	}
	if _, err := r.ReadBit(); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("空输入错误 = %v，期望 ErrInvalidData", err)
	}
	if r.BitsRead() != 0 || r.BitsRemaining() != 0 {
		t.Fatalf("空输入位置 = %d/%d", r.BitsRead(), r.BitsRemaining())
	}
}
