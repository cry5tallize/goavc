package bitstream

import (
	"bytes"
	"testing"
)

// TestWriter_WriteBits 覆盖跨字节与整字节写入。
func TestWriter_WriteBits(t *testing.T) {
	w := NewWriter()
	if err := w.WriteBits(3, 0b101); err != nil {
		t.Fatalf("写入 3 bit 失败: %v", err)
	}
	if err := w.WriteBits(5, 0b10011); err != nil {
		t.Fatalf("写入 5 bit 失败: %v", err)
	}
	if got := w.Bytes(); !bytes.Equal(got, []byte{0b10110011}) {
		t.Fatalf("写出 %08b，期望 10110011", got)
	}
	if w.BitsWritten() != 8 {
		t.Fatalf("BitsWritten = %d，期望 8", w.BitsWritten())
	}
}

// TestWriter_WriteBits64 验证一次写入 64 位。
func TestWriter_WriteBits64(t *testing.T) {
	w := NewWriter()
	if err := w.WriteBits(64, 0x0123456789ABCDEF); err != nil {
		t.Fatalf("写入 64 bit 失败: %v", err)
	}
	want := []byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF}
	if got := w.Bytes(); !bytes.Equal(got, want) {
		t.Fatalf("写出 %x，期望 %x", got, want)
	}
	if w.BitsWritten() != 64 {
		t.Fatalf("BitsWritten = %d，期望 64", w.BitsWritten())
	}
}

// TestWriter_ByteAlign 验证零填充与后续字节写入。
func TestWriter_ByteAlign(t *testing.T) {
	w := NewWriter()
	if err := w.WriteBits(3, 0b101); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if w.BitsWritten() != 3 {
		t.Fatalf("对齐前 BitsWritten = %d，期望 3", w.BitsWritten())
	}
	w.ByteAlign()
	if got := w.Bytes(); !bytes.Equal(got, []byte{0b10100000}) {
		t.Fatalf("对齐后 %08b，期望 10100000", got)
	}
	if w.BitsWritten() != 8 {
		t.Fatalf("对齐后 BitsWritten = %d，期望 8", w.BitsWritten())
	}
	if err := w.WriteByte(0xFF); err != nil {
		t.Fatalf("写入字节失败: %v", err)
	}
	if got := w.Bytes(); !bytes.Equal(got, []byte{0b10100000, 0xFF}) {
		t.Fatalf("结果 %x", got)
	}
	// 已对齐时 ByteAlign 不应追加字节。
	w.ByteAlign()
	if got := len(w.Bytes()); got != 2 {
		t.Fatalf("重复对齐后长度 = %d，期望 2", got)
	}
}

// TestWriter_BytesDoesNotFlush 验证 Bytes 返回补齐视图但不改变状态。
func TestWriter_BytesDoesNotFlush(t *testing.T) {
	w := NewWriter()
	if err := w.WriteBits(4, 0xA); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if got := w.Bytes(); !bytes.Equal(got, []byte{0xA0}) {
		t.Fatalf("半字节视图 = %x，期望 a0", got)
	}
	// 如果 Bytes 错误地提交了半字节，这里会得到两个字节 ab 00。
	if err := w.WriteBits(4, 0xB); err != nil {
		t.Fatalf("继续写入失败: %v", err)
	}
	if got := w.Bytes(); !bytes.Equal(got, []byte{0xAB}) {
		t.Fatalf("继续写入后 = %x，期望 ab", got)
	}
}

// TestWriter_InvalidInput 验证非法宽度与越界值不会污染缓冲。
func TestWriter_InvalidInput(t *testing.T) {
	w := NewWriter()
	if err := w.WriteBits(65, 0); err == nil {
		t.Fatal("宽度 65 应返回错误")
	}
	if err := w.WriteBits(3, 0b1000); err == nil {
		t.Fatal("值 8 无法放入 3 bit，应返回错误")
	}
	if err := w.WriteBits(0, 1); err == nil {
		t.Fatal("零宽写入非零值应返回错误")
	}
	if err := w.WriteBit(2); err == nil {
		t.Fatal("非法比特值应返回错误")
	}
	if w.BitsWritten() != 0 || len(w.Bytes()) != 0 {
		t.Fatalf("失败写入污染了缓冲: bits=%d bytes=%x", w.BitsWritten(), w.Bytes())
	}
	if err := w.WriteBits(0, 0); err != nil {
		t.Fatalf("零宽写入 0 失败: %v", err)
	}
}

// TestWriter_Reset 验证重置后可复用且不残留旧数据。
func TestWriter_Reset(t *testing.T) {
	w := NewWriter()
	if err := w.WriteByte(0xAA); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	w.Reset()
	if w.BitsWritten() != 0 || len(w.Bytes()) != 0 {
		t.Fatalf("重置后仍有数据: bits=%d bytes=%x", w.BitsWritten(), w.Bytes())
	}
	if err := w.WriteByte(0x55); err != nil {
		t.Fatalf("重置后写入失败: %v", err)
	}
	if got := w.Bytes(); !bytes.Equal(got, []byte{0x55}) {
		t.Fatalf("重置后写出 %x，期望 55", got)
	}
}
