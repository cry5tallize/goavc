package stream

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
	"testing"
	"testing/iotest"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/nal"
)

type annexChunkReader struct {
	src   *bytes.Reader
	chunk int
}

func (r annexChunkReader) Read(p []byte) (int, error) { return r.src.Read(p[:min(len(p), r.chunk)]) }

func TestAnnexBReader_KnownChunkMatrixAndOwnership(t *testing.T) {
	// 固定Annex B字节向量，独立期望；不通过自研writer生成参考。
	raw := []byte{0, 0, 0, 0, 1, 0x67, 0x42, 0, 0, 3, 1, 0x80,
		0, 0, 1, 0x68, 0xce, 0x80, 0, 0, 0, 1, 0x65, 0xab, 0x80,
		0, 0, 0, 0, 1, 0x0b, 0, 0}
	want := []nal.Unit{
		{Header: nal.Header{RefIDC: 3, Type: nal.TypeSPS}, Payload: []byte{0x42, 0, 0, 3, 1, 0x80}},
		{Header: nal.Header{RefIDC: 3, Type: nal.TypePPS}, Payload: []byte{0xce, 0x80}},
		{Header: nal.Header{RefIDC: 3, Type: nal.TypeSliceIDR}, Payload: []byte{0xab, 0x80}},
		{Header: nal.Header{Type: nal.TypeEndOfStream}, Payload: []byte{}},
	}
	for _, chunk := range []int{1, 2, 3, 7, 4096, len(raw)} {
		r, err := NewAnnexBReader(annexChunkReader{bytes.NewReader(raw), chunk}, 7)
		if err != nil {
			t.Fatal(err)
		}
		var got []nal.Unit
		for {
			u, err := r.ReadNAL()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatalf("chunk%d: %v", chunk, err)
			}
			got = append(got, u)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("chunk%d固定NAL向量不符：%+v", chunk, got)
		}
		got[1].Payload[0] = 0xff
		if got[0].Payload[0] != 0x42 || got[2].Payload[0] != 0xab || raw[16] != 0xce {
			t.Fatal("NAL之间或输入/输出共享了缓冲")
		}
		if _, err := r.ReadNAL(); !errors.Is(err, io.EOF) {
			t.Fatal("重复EOF不稳定", err)
		}
	}
}

type annexReadFunc func([]byte) (int, error)

func (f annexReadFunc) Read(p []byte) (int, error) { return f(p) }

func TestAnnexBReader_DataWithEOFAndWrappedEOF(t *testing.T) {
	raw := []byte{0, 0, 1, 0x65, 0x80}
	src := bytes.NewReader(raw)
	wrapped := annexReadFunc(func(p []byte) (int, error) {
		n, err := src.Read(p)
		if err != nil {
			err = fmt.Errorf("测试来源: %w", err)
		}
		return n, err
	})
	for _, input := range []io.Reader{iotest.DataErrReader(bytes.NewReader(raw)), wrapped} {
		r, _ := NewAnnexBReader(input, 2)
		if u, err := r.ReadNAL(); err != nil || u.Header.Type != nal.TypeSliceIDR || !bytes.Equal(u.Payload, []byte{0x80}) {
			t.Fatal("EOF丢失最后一个NAL", u, err)
		}
		if _, err := r.ReadNAL(); !errors.Is(err, io.EOF) {
			t.Fatal("最后一个NAL后未正常结束", err)
		}
	}
}

func TestAnnexBReader_ReturnsAtBoundaryBeforeEOF(t *testing.T) {
	available := []byte{0, 0, 1, 0x65, 0x80, 0, 0, 1}
	ended, calls := false, 0
	src := annexReadFunc(func(p []byte) (int, error) {
		calls++
		if len(available) == 0 && !ended {
			return 0, io.ErrClosedPipe // 检测实现是否越过已提供的NAL边界继续等待。
		}
		n := copy(p, available)
		available = available[n:]
		if ended && len(available) == 0 {
			return n, io.EOF
		}
		return n, nil
	})
	r, _ := NewAnnexBReader(src, 8)
	if u, err := r.ReadNAL(); err != nil || u.Header.Type != nal.TypeSliceIDR || calls != 1 {
		t.Fatal("未在下一起始码处立即交付前NAL", err, calls)
	}
	available, ended = []byte{0x68, 0xce, 0x80}, true
	if u, err := r.ReadNAL(); err != nil || u.Header.Type != nal.TypePPS || !bytes.Equal(u.Payload, []byte{0xce, 0x80}) {
		t.Fatal("后续chunk未正确续接已消费的起始码", u, err)
	}
}

func TestAnnexBReader_StrictTailAndMalformedInput(t *testing.T) {
	for _, data := range [][]byte{{0}, {0, 0}, {0, 0, 1}, {0, 0, 1, 0, 0}} {
		r, _ := NewAnnexBReader(iotest.OneByteReader(bytes.NewReader(data)), 0)
		_, err := r.ReadNAL()
		if !errors.Is(err, h264.ErrInvalidData) || !errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
			t.Fatal("残缺封装被当作正常EOF", data, err)
		}
		if _, again := r.ReadNAL(); again != err {
			t.Fatal("错误后未保持终止状态")
		}
	}
	for _, data := range [][]byte{{9, 0, 0, 1, 0x65, 0x80}, {0, 0, 1, 0x80}, {0, 0, 1, 0, 0, 1, 0x65}} {
		r, _ := NewAnnexBReader(bytes.NewReader(data), 8)
		if _, err := r.ReadNAL(); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatal("垃圾/非法header/空NAL未拒绝", data, err)
		}
	}
	// 整段预读也必须先返回此前完整NAL，下一次才报告残缺尾部。
	r, _ := NewAnnexBReader(bytes.NewReader([]byte{0, 0, 1, 0x65, 0x80, 0, 0, 1}), 8)
	if u, err := r.ReadNAL(); err != nil || u.Header.Type != nal.TypeSliceIDR {
		t.Fatal("坏尾部吞掉了此前完整NAL", err)
	}
	if _, err := r.ReadNAL(); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal("尾部空起始码未拒绝", err)
	}
}

func TestAnnexBReader_LimitsAndIOErrors(t *testing.T) {
	if _, err := NewAnnexBReader(nil, 0); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal(err)
	}
	if _, err := NewAnnexBReader(bytes.NewReader(nil), -1); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal(err)
	}
	empty, _ := NewAnnexBReader(bytes.NewReader(nil), 0)
	if _, err := empty.ReadNAL(); !errors.Is(err, io.EOF) {
		t.Fatal("空流不是EOF", err)
	}
	data := append([]byte{0, 0, 1, 0x65}, bytes.Repeat([]byte{0x55}, 1<<20)...)
	src := bytes.NewReader(data)
	r, _ := NewAnnexBReader(src, 8)
	_, err := r.ReadNAL()
	if !errors.Is(err, h264.ErrResourceLimit) || src.Len() == 0 {
		t.Fatal("未在读完整个超大NAL之前拒绝", err)
	}
	left := src.Len()
	if _, again := r.ReadNAL(); again != err || src.Len() != left {
		t.Fatal("超限后仍在消费输入")
	}
	for _, src := range []io.Reader{iotest.ErrReader(io.ErrClosedPipe), io.MultiReader(bytes.NewReader([]byte{0, 0, 1, 0x65}), iotest.ErrReader(io.ErrClosedPipe))} {
		r, _ := NewAnnexBReader(src, 8)
		if _, err := r.ReadNAL(); !errors.Is(err, io.ErrClosedPipe) {
			t.Fatal("底层I/O错误被误报为EOF", err)
		}
	}
}
