package nal

import (
	"bytes"
	"errors"
	"io"
	"testing"

	h264 "github.com/cry5tallize/goavc"
)

// avccStream 用指定前缀宽度拼接 AVCC 数据。
func avccStream(lengthSize int, units ...Unit) []byte {
	var out []byte
	for _, u := range units {
		raw := u.Bytes()
		length := len(raw)
		switch lengthSize {
		case 1:
			out = append(out, byte(length))
		case 2:
			out = append(out, byte(length>>8), byte(length))
		case 3:
			out = append(out, byte(length>>16), byte(length>>8), byte(length))
		case 4:
			out = append(out, byte(length>>24), byte(length>>16), byte(length>>8), byte(length))
		}
		out = append(out, raw...)
	}
	return out
}

// TestSplitAVCC_LengthSizes 覆盖 1..4 字节长度前缀。
func TestSplitAVCC_LengthSizes(t *testing.T) {
	units := []Unit{
		{Header: Header{RefIDC: 3, Type: TypeSPS}, Payload: []byte{0x42, 0xC0, 0x0A, 0x80}},
		{Header: Header{RefIDC: 3, Type: TypePPS}, Payload: []byte{0xCE, 0x0F, 0x2C, 0x80}},
		{Header: Header{RefIDC: 3, Type: TypeSliceIDR}, Payload: []byte{0x88, 0x84, 0x11, 0xFF}},
	}
	for lengthSize := 1; lengthSize <= 4; lengthSize++ {
		got, err := SplitAVCC(avccStream(lengthSize, units...), lengthSize)
		if err != nil {
			t.Fatalf("lengthSize=%d 解析失败: %v", lengthSize, err)
		}
		mustEqualUnits(t, got, units)
	}
}

// TestAVCCReader_ChunkMatrix 验证任意 Read 分块（含前缀被切开）得到相同 NAL。
func TestAVCCReader_ChunkMatrix(t *testing.T) {
	units := []Unit{
		{Header: Header{RefIDC: 3, Type: TypeSPS}, Payload: []byte{0x42, 0xC0, 0x0A, 0x80}},
		{Header: Header{RefIDC: 3, Type: TypePPS}, Payload: []byte{0xCE, 0x0F, 0x2C, 0x80}},
		{Header: Header{RefIDC: 3, Type: TypeSliceIDR}, Payload: []byte{0x88, 0x84, 0x11, 0xFF}},
	}
	stream := avccStream(3, units...)
	for _, chunk := range []int{1, 2, 3, 4, 7, 4096, len(stream) + 1} {
		reader, err := NewAVCCReader(&chunkReader{data: stream, chunk: chunk}, 3)
		if err != nil {
			t.Fatalf("chunk=%d 构造失败: %v", chunk, err)
		}
		var got []Unit
		for {
			u, err := reader.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("chunk=%d 读取失败: %v", chunk, err)
			}
			got = append(got, u)
		}
		mustEqualUnits(t, got, units)
	}
}

// TestAVCCReader_EOFAndTruncation 验证空流、半截前缀与半截 payload。
func TestAVCCReader_EOFAndTruncation(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		r, err := NewAVCCReader(bytes.NewReader(nil), 2)
		if err != nil {
			t.Fatalf("构造失败: %v", err)
		}
		if _, err := r.Next(); err != io.EOF {
			t.Fatalf("错误 = %v，期望 io.EOF", err)
		}
	})
	t.Run("partial prefix", func(t *testing.T) {
		r, err := NewAVCCReader(bytes.NewReader([]byte{0x00}), 2)
		if err != nil {
			t.Fatalf("构造失败: %v", err)
		}
		if _, err := r.Next(); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("错误 = %v，期望 ErrInvalidData", err)
		}
	})
	t.Run("partial payload", func(t *testing.T) {
		// 长度 5，但只有 2 个 payload 字节。
		data := []byte{0x00, 0x05, 0x67, 0x42}
		r, err := NewAVCCReader(bytes.NewReader(data), 2)
		if err != nil {
			t.Fatalf("构造失败: %v", err)
		}
		if _, err := r.Next(); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("错误 = %v，期望 ErrInvalidData", err)
		}
	})
}

// TestAVCCReader_ZeroLength 验证零长度 NAL 被拒绝。
func TestAVCCReader_ZeroLength(t *testing.T) {
	r, err := NewAVCCReader(bytes.NewReader([]byte{0x00, 0x00}), 2)
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	if _, err := r.Next(); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("错误 = %v，期望 ErrInvalidData", err)
	}
}

// TestAVCCReader_MaxNALSize 验证长度上限。
func TestAVCCReader_MaxNALSize(t *testing.T) {
	data := []byte{0x00, 0x64} // 长度 100
	r, err := NewAVCCReader(bytes.NewReader(data), 2)
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	r.MaxNALSize = 10
	if _, err := r.Next(); !errors.Is(err, h264.ErrResourceLimit) {
		t.Fatalf("错误 = %v，期望 ErrResourceLimit", err)
	}
}

// TestNewAVCCReader_InvalidLengthSize 验证前缀宽度范围。
func TestNewAVCCReader_InvalidLengthSize(t *testing.T) {
	for _, size := range []int{0, 5, -1} {
		if _, err := NewAVCCReader(bytes.NewReader(nil), size); err == nil {
			t.Fatalf("lengthSize=%d 应返回错误", size)
		}
	}
}

// TestAVCCReader_StickyError 验证错误后继续调用返回同一错误。
func TestAVCCReader_StickyError(t *testing.T) {
	r, err := NewAVCCReader(bytes.NewReader([]byte{0x00, 0x00}), 2)
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	_, err1 := r.Next()
	_, err2 := r.Next()
	if !errors.Is(err1, h264.ErrInvalidData) || !errors.Is(err2, h264.ErrInvalidData) {
		t.Fatalf("粘性错误: %v / %v", err1, err2)
	}
}

// TestAVCCReader_ForbiddenBit 验证非法 header。
func TestAVCCReader_ForbiddenBit(t *testing.T) {
	data := []byte{0x00, 0x01, 0x80}
	r, err := NewAVCCReader(bytes.NewReader(data), 2)
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	if _, err := r.Next(); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("错误 = %v，期望 ErrInvalidData", err)
	}
}

// TestAVCCReader_HeaderOnly 验证长度为 1 的 NAL。
func TestAVCCReader_HeaderOnly(t *testing.T) {
	data := []byte{0x00, 0x01, 0x0B} // end of stream
	units, err := SplitAVCC(data, 2)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(units) != 1 || units[0].Header.Type != TypeEndOfStream || len(units[0].Payload) != 0 {
		t.Fatalf("结果 = %+v", units)
	}
}

// TestSplitAVCC_CopiesPayload 验证返回的 payload 不引用输入。
func TestSplitAVCC_CopiesPayload(t *testing.T) {
	data := avccStream(2, Unit{Header: Header{RefIDC: 3, Type: TypeSPS}, Payload: []byte{0xAA, 0xBB}})
	units, err := SplitAVCC(data, 2)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	for i := range data {
		data[i] = 0xFF
	}
	if !bytes.Equal(units[0].Payload, []byte{0xAA, 0xBB}) {
		t.Fatalf("payload 被输入修改: %x", units[0].Payload)
	}
}
