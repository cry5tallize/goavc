package nal

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

// TestAVCCWriter_RoundTrip 验证 1..4 字节前缀的写出/读回。
func TestAVCCWriter_RoundTrip(t *testing.T) {
	units := []Unit{
		{Header: Header{RefIDC: 3, Type: TypeSPS}, Payload: []byte{0x42, 0xC0, 0x0A, 0x80}},
		{Header: Header{RefIDC: 3, Type: TypePPS}, Payload: []byte{0xCE, 0x0F, 0x2C, 0x80}},
		{Header: Header{RefIDC: 0, Type: TypeSEI}, Payload: []byte{0x05, 0x01, 0x7F}},
	}
	for lengthSize := 1; lengthSize <= 4; lengthSize++ {
		data, err := MarshalAVCC(units, lengthSize)
		if err != nil {
			t.Fatalf("lengthSize=%d 写出失败: %v", lengthSize, err)
		}
		got, err := SplitAVCC(data, lengthSize)
		if err != nil {
			t.Fatalf("lengthSize=%d 读回失败: %v", lengthSize, err)
		}
		mustEqualUnits(t, got, units)
	}
}

// TestAVCCWriter_ExactBytes 检查具体前缀字节序。
func TestAVCCWriter_ExactBytes(t *testing.T) {
	u := Unit{Header: Header{RefIDC: 3, Type: TypeSPS}, Payload: []byte{0xAA, 0xBB}}
	data, err := MarshalAVCC([]Unit{u}, 2)
	if err != nil {
		t.Fatalf("写出失败: %v", err)
	}
	want := []byte{0x00, 0x03, 0x67, 0xAA, 0xBB}
	if !bytes.Equal(data, want) {
		t.Fatalf("写出 %x，期望 %x", data, want)
	}
}

// TestAVCCWriter_LengthOverflow 验证长度无法放入前缀时拒绝且不写出。
func TestAVCCWriter_LengthOverflow(t *testing.T) {
	var buf bytes.Buffer
	w, err := NewAVCCWriter(&buf, 1)
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	// NAL 总长度 = 1 (header) + 255 = 256，超过 1 字节前缀上限 255。
	u := Unit{Header: Header{RefIDC: 3, Type: TypeSlice}, Payload: bytes.Repeat([]byte{0xAA}, 255)}
	if err := w.Write(u); err == nil {
		t.Fatal("长度 256 应返回错误")
	}
	if buf.Len() != 0 {
		t.Fatalf("失败写出污染了缓冲区: %x", buf.Bytes())
	}

	// 总长度 255 正好可以放入。
	u.Payload = bytes.Repeat([]byte{0xAA}, 254)
	if err := w.Write(u); err != nil {
		t.Fatalf("长度 255 写出失败: %v", err)
	}
	if buf.Len() != 256 {
		t.Fatalf("缓冲区长度 = %d，期望 256", buf.Len())
	}
}

// TestNewAVCCWriter_InvalidLengthSize 验证前缀宽度范围。
func TestNewAVCCWriter_InvalidLengthSize(t *testing.T) {
	for _, size := range []int{0, 5, -1} {
		if _, err := NewAVCCWriter(io.Discard, size); err == nil {
			t.Fatalf("lengthSize=%d 应返回错误", size)
		}
	}
}

// failingWriter 在第一次写入时返回错误。
type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

// TestAVCCWriter_WriteError 验证底层 I/O 错误被传播。
func TestAVCCWriter_WriteError(t *testing.T) {
	sentinel := errors.New("boom")
	w, err := NewAVCCWriter(failingWriter{err: sentinel}, 2)
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	if err := w.Write(Unit{Header: Header{Type: TypeSPS}}); !errors.Is(err, sentinel) {
		t.Fatalf("错误 = %v，期望 %v", err, sentinel)
	}
}

// shortWriter 永远返回 0, nil，模拟违反 io.Writer 契约的实现。
type shortWriter struct{}

func (shortWriter) Write([]byte) (int, error) { return 0, nil }

// TestWriteAll_ShortWrite 验证短写不会导致死循环。
func TestWriteAll_ShortWrite(t *testing.T) {
	if err := writeAll(shortWriter{}, []byte{0x01}); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("错误 = %v，期望 io.ErrShortWrite", err)
	}
}

// TestAVCCWriter_HeaderOnly 验证只有 header 的 NAL 前缀为 1。
func TestAVCCWriter_HeaderOnly(t *testing.T) {
	data, err := MarshalAVCC([]Unit{{Header: Header{Type: TypeEndOfStream}}}, 4)
	if err != nil {
		t.Fatalf("写出失败: %v", err)
	}
	want := []byte{0x00, 0x00, 0x00, 0x01, 0x0B}
	if !bytes.Equal(data, want) {
		t.Fatalf("写出 %x，期望 %x", data, want)
	}
}

// TestMarshalAVCC_Empty 验证空输入。
func TestMarshalAVCC_Empty(t *testing.T) {
	data, err := MarshalAVCC(nil, 4)
	if err != nil {
		t.Fatalf("写出失败: %v", err)
	}
	if len(data) != 0 {
		t.Fatalf("空输入写出了 %d 字节", len(data))
	}
}
