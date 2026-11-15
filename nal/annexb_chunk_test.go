package nal

import (
	"bytes"
	"errors"
	"io"
	"testing"

	h264 "github.com/cry5tallize/goavc"
)

// chunkReader 把数据按固定上限分块返回，用于模拟网络/文件读取的任意切分。
type chunkReader struct {
	data  []byte
	chunk int
	off   int
}

func (r *chunkReader) Read(p []byte) (int, error) {
	if r.off >= len(r.data) {
		return 0, io.EOF
	}
	n := min(len(p), r.chunk, len(r.data)-r.off)
	copy(p, r.data[r.off:r.off+n])
	r.off += n
	return n, nil
}

// collectAnnexB 用给定分块大小扫描全部 NAL。
func collectAnnexB(t *testing.T, data []byte, chunk int) ([]Unit, error) {
	t.Helper()
	scanner := NewAnnexBScanner(&chunkReader{data: data, chunk: chunk})
	var units []Unit
	for {
		u, err := scanner.Next()
		if err == io.EOF {
			return units, nil
		}
		if err != nil {
			return nil, err
		}
		units = append(units, u)
	}
}

func mustEqualUnits(t *testing.T, got, want []Unit) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("得到 %d 个 NAL，期望 %d", len(got), len(want))
	}
	for i := range got {
		if got[i].Header != want[i].Header || !bytes.Equal(got[i].Payload, want[i].Payload) {
			t.Fatalf("unit %d 不一致:\n got %+v %x\nwant %+v %x",
				i, got[i].Header, got[i].Payload, want[i].Header, want[i].Payload)
		}
	}
}

// TestAnnexBScanner_ChunkMatrix 验证 1/2/3/7/4096/整文件分块得到相同结果。
func TestAnnexBScanner_ChunkMatrix(t *testing.T) {
	units := []Unit{
		{Header: Header{RefIDC: 3, Type: TypeSPS}, Payload: []byte{0x42, 0xC0, 0x0A, 0x00, 0x00, 0x03, 0x01, 0x80}},
		{Header: Header{RefIDC: 3, Type: TypePPS}, Payload: []byte{0xCE, 0x0F, 0x2C, 0x80}},
		{Header: Header{RefIDC: 0, Type: TypeSEI}, Payload: []byte{0x05, 0x01, 0x00, 0x00, 0x03, 0x7F}},
		{Header: Header{RefIDC: 3, Type: TypeSliceIDR}, Payload: []byte{0x88, 0x84, 0x11, 0x00, 0x00, 0x03, 0x01, 0xFF}},
		{Header: Header{RefIDC: 0, Type: TypeEndOfStream}},
	}

	var stream []byte
	stream = append(stream, 0x00, 0x00, 0x00, 0x00) // 前导零
	stream = append(stream, 0x00, 0x00, 0x00, 0x01)
	stream = append(stream, units[0].Bytes()...)
	stream = append(stream, 0x00, 0x00, 0x01) // 3 字节起始码
	stream = append(stream, units[1].Bytes()...)
	stream = append(stream, 0x00, 0x00, 0x00, 0x01)
	stream = append(stream, units[2].Bytes()...)
	stream = append(stream, 0x00, 0x00, 0x00, 0x00, 0x00) // trailing zeros
	stream = append(stream, 0x00, 0x00, 0x00, 0x01)
	stream = append(stream, units[3].Bytes()...)
	stream = append(stream, 0x00, 0x00, 0x01)
	stream = append(stream, units[4].Bytes()...)
	stream = append(stream, 0x00, 0x00, 0x00) // 文件末尾零

	reference, err := SplitAnnexB(stream)
	if err != nil {
		t.Fatalf("参考扫描失败: %v", err)
	}
	mustEqualUnits(t, reference, units)

	for _, chunk := range []int{1, 2, 3, 7, 4096, len(stream) + 17} {
		got, err := collectAnnexB(t, stream, chunk)
		if err != nil {
			t.Fatalf("chunk=%d 扫描失败: %v", chunk, err)
		}
		mustEqualUnits(t, got, units)
	}
}

// TestAnnexBScanner_ChunkMatrixMalformed 验证分块不影响错误判定。
func TestAnnexBScanner_ChunkMatrixMalformed(t *testing.T) {
	// 第二个 NAL 的 forbidden_zero_bit 被置位。
	stream := []byte{
		0x00, 0x00, 0x01, 0x67, 0x00, 0x00, 0x00, 0x01,
		0x80, 0x00,
	}
	for _, chunk := range []int{1, 2, 3, 7} {
		_, err := collectAnnexB(t, stream, chunk)
		if !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("chunk=%d 错误 = %v，期望 ErrInvalidData", chunk, err)
		}
	}
}

// TestAnnexBScanner_EmptyChunks 验证空输入在所有分块下都返回 EOF。
func TestAnnexBScanner_EmptyChunks(t *testing.T) {
	for _, chunk := range []int{1, 7, 4096} {
		units, err := collectAnnexB(t, nil, chunk)
		if err != nil || len(units) != 0 {
			t.Fatalf("chunk=%d: units=%d err=%v", chunk, len(units), err)
		}
	}
}
