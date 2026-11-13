package bitstream

import (
	"bytes"
	"math"
	"testing"
)

// TestWriteUE_KnownCodewords 对照规范表 9-1 检查写出的码字。
func TestWriteUE_KnownCodewords(t *testing.T) {
	cases := []struct {
		value uint32
		bits  string
	}{
		{0, "1"},
		{1, "010"},
		{2, "011"},
		{3, "00100"},
		{4, "00101"},
		{5, "00110"},
		{6, "00111"},
		{7, "0001000"},
		{8, "0001001"},
	}
	for _, c := range cases {
		w := NewWriter()
		if err := w.WriteUE(c.value); err != nil {
			t.Fatalf("WriteUE(%d) 失败: %v", c.value, err)
		}
		want := packBits(t, c.bits)
		if got := w.Bytes(); !bytes.Equal(got, want) {
			t.Fatalf("WriteUE(%d) = %08b，期望 %08b", c.value, got, want)
		}
		if w.BitsWritten() != uint64(len(c.bits)) {
			t.Fatalf("WriteUE(%d) 写 %d bit，期望 %d", c.value, w.BitsWritten(), len(c.bits))
		}
	}
}

// TestWriteSE_KnownCodewords 检查 se(v) 的 codeNum 映射。
func TestWriteSE_KnownCodewords(t *testing.T) {
	cases := []struct {
		value int32
		bits  string
	}{
		{0, "1"},
		{1, "010"},
		{-1, "011"},
		{2, "00100"},
		{-2, "00101"},
		{3, "00110"},
		{-3, "00111"},
	}
	for _, c := range cases {
		w := NewWriter()
		if err := w.WriteSE(c.value); err != nil {
			t.Fatalf("WriteSE(%d) 失败: %v", c.value, err)
		}
		want := packBits(t, c.bits)
		if got := w.Bytes(); !bytes.Equal(got, want) {
			t.Fatalf("WriteSE(%d) = %08b，期望 %08b", c.value, got, want)
		}
	}
}

// TestGolomb_RoundTrip 验证写读对称，覆盖边界值。
func TestGolomb_RoundTrip(t *testing.T) {
	ueValues := []uint32{0, 1, 2, 3, 7, 8, 255, 256, 65535, 1 << 20, math.MaxUint32}
	for _, value := range ueValues {
		w := NewWriter()
		if err := w.WriteUE(value); err != nil {
			t.Fatalf("WriteUE(%d) 失败: %v", value, err)
		}
		r := NewReader(w.Bytes())
		got, err := r.ReadUE()
		if err != nil {
			t.Fatalf("ReadUE(%d) 失败: %v", value, err)
		}
		if got != value {
			t.Fatalf("ue round-trip %d -> %d", value, got)
		}
		if r.BitsRead() != w.BitsWritten() {
			t.Fatalf("ue %d: 写 %d bit，读 %d bit", value, w.BitsWritten(), r.BitsRead())
		}
	}

	seValues := []int32{0, 1, -1, 2, -2, 32767, -32768, math.MaxInt32, -math.MaxInt32}
	for _, value := range seValues {
		w := NewWriter()
		if err := w.WriteSE(value); err != nil {
			t.Fatalf("WriteSE(%d) 失败: %v", value, err)
		}
		r := NewReader(w.Bytes())
		got, err := r.ReadSE()
		if err != nil {
			t.Fatalf("ReadSE(%d) 失败: %v", value, err)
		}
		if got != value {
			t.Fatalf("se round-trip %d -> %d", value, got)
		}
	}
}

// TestWriteUE_MaxUint32 验证最大值的码字长度与内容。
func TestWriteUE_MaxUint32(t *testing.T) {
	w := NewWriter()
	if err := w.WriteUE(math.MaxUint32); err != nil {
		t.Fatalf("WriteUE(MaxUint32) 失败: %v", err)
	}
	if w.BitsWritten() != 65 {
		t.Fatalf("写 %d bit，期望 65", w.BitsWritten())
	}
	want := packBits(t, "00000000000000000000000000000000"+"1"+
		"00000000000000000000000000000000")
	if got := w.Bytes(); !bytes.Equal(got, want) {
		t.Fatalf("MaxUint32 码字 = %x，期望 %x", got, want)
	}
}

// TestWriteSE_RejectsMinInt32 验证不可表示的 se(v) 值被拒绝且不写入比特。
func TestWriteSE_RejectsMinInt32(t *testing.T) {
	w := NewWriter()
	if err := w.WriteSE(math.MinInt32); err == nil {
		t.Fatal("MinInt32 应返回错误")
	}
	if w.BitsWritten() != 0 || len(w.Bytes()) != 0 {
		t.Fatalf("失败写入污染缓冲: bits=%d bytes=%x", w.BitsWritten(), w.Bytes())
	}
}
