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

func avccVectors() [][]byte {
	// 手写长度前缀，独立期望；覆盖1..4字节，并含合法header-only EOS。
	return [][]byte{
		{3, 0x65, 0xab, 0x80, 1, 0x0b},
		{0, 3, 0x65, 0xab, 0x80, 0, 1, 0x0b},
		{0, 0, 3, 0x65, 0xab, 0x80, 0, 0, 1, 0x0b},
		{0, 0, 0, 3, 0x65, 0xab, 0x80, 0, 0, 0, 1, 0x0b},
	}
}

func TestAVCCReader_KnownLengthAndChunkMatrix(t *testing.T) {
	want := []nal.Unit{
		{Header: nal.Header{RefIDC: 3, Type: nal.TypeSliceIDR}, Payload: []byte{0xab, 0x80}},
		{Header: nal.Header{Type: nal.TypeEndOfStream}, Payload: []byte{}},
	}
	for i, vector := range avccVectors() {
		for _, chunk := range []int{1, 2, 3, 7, 4096, len(vector)} {
			raw := bytes.Clone(vector)
			r, err := NewAVCCReader(annexChunkReader{bytes.NewReader(raw), chunk}, i+1, 3)
			if err != nil {
				t.Fatal(err)
			}
			var got []nal.Unit
			for range 2 {
				u, err := r.ReadNAL()
				if err != nil {
					t.Fatal(i+1, chunk, err)
				}
				got = append(got, u)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatal("AVCC固定字节向量不符", i+1, chunk, got)
			}
			raw[i+2] = 0xff
			if got[0].Payload[0] != 0xab {
				t.Fatal("输出引用了输入缓冲")
			}
			for range 2 {
				if _, err := r.ReadNAL(); !errors.Is(err, io.EOF) {
					t.Fatal("重复EOF错误", err)
				}
			}
		}
	}
}

func TestAVCCReader_TruncationAndStickyErrors(t *testing.T) {
	for i, raw := range avccVectors() {
		for cut := 0; cut < i+1+3; cut++ {
			r, _ := NewAVCCReader(iotest.OneByteReader(bytes.NewReader(raw[:cut])), i+1, 3)
			_, err := r.ReadNAL()
			want := h264.ErrInvalidData
			if cut == 0 {
				want = io.EOF
			}
			if !errors.Is(err, want) || cut != 0 && errors.Is(err, io.EOF) {
				t.Fatal("前缀/载荷截断分类错误", i+1, cut, err)
			}
			if _, again := r.ReadNAL(); again != err {
				t.Fatal("截断后继续消费了输入")
			}
		}
	}
	for _, raw := range [][]byte{{0}, {1, 0x80}} {
		r, _ := NewAVCCReader(bytes.NewReader(raw), 1, 3)
		if _, err := r.ReadNAL(); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatal("零长度或坏header未拒绝", err)
		}
	}
	src := bytes.NewReader([]byte{255, 255, 255, 255, 0x65, 0x80})
	r, _ := NewAVCCReader(src, 4, 0)
	_, err := r.ReadNAL()
	if !errors.Is(err, h264.ErrResourceLimit) || src.Len() != 2 {
		t.Fatal("未在读取/分配超大载荷前拒绝", err)
	}
	if _, again := r.ReadNAL(); again != err || src.Len() != 2 {
		t.Fatal("超限后继续读流")
	}
}

func avcConfigVector() []byte {
	return []byte{1, 66, 0, 30, 255, 225, 0, 4, 0x67, 0x42, 0xc0, 0x1e, 1, 0, 2, 0x68, 0xce}
}

func TestAVCCReader_ConfigWidthAndOwnership(t *testing.T) {
	for _, size := range []int{1, 2, 4} {
		config := avcConfigVector()
		config[4] = 0xfc | byte(size-1)
		data := avccVectors()[size-1]
		src := bytes.NewReader(data)
		r, err := NewAVCCReaderFromConfig(src, config, 4)
		if err != nil {
			t.Fatal(err)
		}
		clear(config)
		for _, want := range []struct {
			typ     nal.UnitType
			payload []byte
		}{{nal.TypeSPS, []byte{0x42, 0xc0, 0x1e}}, {nal.TypePPS, []byte{0xce}}} {
			u, err := r.ReadNAL()
			if err != nil || u.Header.Type != want.typ || !bytes.Equal(u.Payload, want.payload) || src.Len() != len(data) {
				t.Fatal("配置未快照复制或提前消费样本", size, u, err)
			}
		}
		if u, err := r.ReadNAL(); err != nil || u.Header.Type != nal.TypeSliceIDR {
			t.Fatal("未采用avcC的长度宽度", size, u, err)
		}
	}
}

func TestAVCCReader_InvalidConfiguration(t *testing.T) {
	for _, size := range []int{0, -1, 5} {
		if _, err := NewAVCCReader(bytes.NewReader(nil), size, 0); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatal("前缀宽度未校验", size, err)
		}
	}
	if _, err := NewAVCCReader(nil, 4, 0); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal(err)
	}
	if _, err := NewAVCCReader(bytes.NewReader(nil), 4, -1); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal(err)
	}
	config := avcConfigVector()
	for cut := 0; cut < len(config); cut++ {
		if _, err := NewAVCCReaderFromConfig(bytes.NewReader(nil), config[:cut], 4); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatal("截断avcC未拒绝", cut, err)
		}
	}
	if _, err := NewAVCCReaderFromConfig(bytes.NewReader(nil), config, 3); !errors.Is(err, h264.ErrResourceLimit) {
		t.Fatal("配置参数集未遵守单NAL限额", err)
	}
	config[8] = 0x65
	if _, err := NewAVCCReaderFromConfig(bytes.NewReader(nil), config, 4); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal("avcC的SPS槽接受了slice", err)
	}
	config = avcConfigVector()
	config[0] = 2
	if _, err := NewAVCCReaderFromConfig(bytes.NewReader(nil), config, 4); !errors.Is(err, h264.ErrUnsupported) {
		t.Fatal("不支持的avcC版本被吞掉", err)
	}
}

func TestAVCCReader_IOErrorsAndWrappedEOF(t *testing.T) {
	src := bytes.NewReader([]byte{1, 0x0b})
	wrapped := annexReadFunc(func(p []byte) (int, error) {
		n, err := src.Read(p)
		if err != nil {
			err = fmt.Errorf("测试来源: %w", err)
		}
		return n, err
	})
	for _, input := range []io.Reader{wrapped, iotest.DataErrReader(bytes.NewReader([]byte{1, 0x0b}))} {
		r, _ := NewAVCCReader(input, 1, 1)
		if u, err := r.ReadNAL(); err != nil || u.Header.Type != nal.TypeEndOfStream {
			t.Fatal("EOF丢掉了完整NAL", err)
		}
		if _, err := r.ReadNAL(); !errors.Is(err, io.EOF) {
			t.Fatal(err)
		}
	}
	for _, input := range []io.Reader{iotest.ErrReader(io.ErrClosedPipe), io.MultiReader(bytes.NewReader([]byte{2, 0x65}), iotest.ErrReader(io.ErrClosedPipe))} {
		r, _ := NewAVCCReader(input, 1, 4)
		if _, err := r.ReadNAL(); !errors.Is(err, io.ErrClosedPipe) {
			t.Fatal("前缀/载荷底层I/O错误未保留", err)
		}
	}
}
