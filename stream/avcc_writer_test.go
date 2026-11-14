package stream

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/nal"
)

func TestAVCCWriter_KnownLengthPrefixesAndOwnership(t *testing.T) {
	// 独立大端长度解析解：NAL长为3/3/7/1，包含header，不含前缀。
	// 混合Annex-B起始码和前导/尾零不进入AVCC载荷，转义零原样保留。
	raw := []byte{0, 0, 0, 0, 1, 0x67, 0x42, 0x80,
		0, 0, 1, 0x68, 0xce, 0x80,
		0, 0, 1, 0x65, 0xab, 0, 0, 3, 1, 0x80,
		0, 0, 0, 1, 0x0b, 0, 0}
	wantUnits := []nal.Unit{
		{Header: nal.Header{RefIDC: 3, Type: nal.TypeSPS}, Payload: []byte{0x42, 0x80}},
		{Header: nal.Header{RefIDC: 3, Type: nal.TypePPS}, Payload: []byte{0xce, 0x80}},
		{Header: nal.Header{RefIDC: 3, Type: nal.TypeSliceIDR}, Payload: []byte{0xab, 0, 0, 3, 1, 0x80}},
		{Header: nal.Header{Type: nal.TypeEndOfStream}, Payload: []byte{}},
	}
	for _, tc := range []struct {
		width int
		want  []byte
	}{
		{1, []byte{3, 0x67, 0x42, 0x80, 3, 0x68, 0xce, 0x80, 7, 0x65, 0xab, 0, 0, 3, 1, 0x80, 1, 0x0b}},
		{2, []byte{0, 3, 0x67, 0x42, 0x80, 0, 3, 0x68, 0xce, 0x80, 0, 7, 0x65, 0xab, 0, 0, 3, 1, 0x80, 0, 1, 0x0b}},
		{3, []byte{0, 0, 3, 0x67, 0x42, 0x80, 0, 0, 3, 0x68, 0xce, 0x80, 0, 0, 7, 0x65, 0xab, 0, 0, 3, 1, 0x80, 0, 0, 1, 0x0b}},
		{4, []byte{0, 0, 0, 3, 0x67, 0x42, 0x80, 0, 0, 0, 3, 0x68, 0xce, 0x80, 0, 0, 0, 7, 0x65, 0xab, 0, 0, 3, 1, 0x80, 0, 0, 0, 1, 0x0b}},
	} {
		t.Run(fmt.Sprint(tc.width), func(t *testing.T) {
			var out bytes.Buffer
			w, err := NewAVCCWriter(&out, AVCCWriterConfig{LengthSize: tc.width})
			if err != nil {
				t.Fatal(err)
			}
			if out.Len() != 0 {
				t.Fatal("构造时写出了数据")
			}
			input := append([]byte(nil), raw...)
			for range 2 {
				if err := w.WriteAccessUnit(h264.Packet{Data: input, PTS: 100, DTS: 80, Duration: 20, KeyFrame: true}); err != nil {
					t.Fatal(err)
				}
			}
			if !bytes.Equal(input, raw) {
				t.Fatal("Writer修改输入")
			}
			clear(input)
			want := bytes.Repeat(tc.want, 2)
			if !bytes.Equal(out.Bytes(), want) {
				t.Fatalf("已知字节不符: %x", out.Bytes())
			}
			for _, chunk := range []int{1, 2, 3, 7, 4096, len(want)} {
				r, _ := NewAVCCReader(annexChunkReader{bytes.NewReader(out.Bytes()), chunk}, tc.width, 0)
				for i, unit := range append(wantUnits, wantUnits...) {
					got, err := r.ReadNAL()
					if err != nil || !reflect.DeepEqual(got, unit) {
						t.Fatalf("chunk%d NAL%d: %+v %v", chunk, i, got, err)
					}
				}
				if _, err := r.ReadNAL(); err != io.EOF {
					t.Fatal("未排空到EOF", err)
				}
			}
		})
	}
	var out bytes.Buffer
	w, _ := NewAVCCWriter(&out, AVCCWriterConfig{})
	if err := w.WriteAccessUnit(h264.Packet{Data: []byte{0, 0, 1, 0x65, 0x80}}); err != nil || !bytes.Equal(out.Bytes(), []byte{0, 0, 0, 2, 0x65, 0x80}) {
		t.Fatal("默认宽度不是4", err)
	}
}

func TestAVCCWriter_LengthCapacityBeforeAnyOutput(t *testing.T) {
	for _, tc := range []struct {
		width, length int
		prefix        []byte
	}{
		{1, 255, []byte{0xff}}, {2, 65535, []byte{0xff, 0xff}},
		{3, 65537, []byte{1, 0, 1}}, {4, 65537, []byte{0, 1, 0, 1}},
	} {
		t.Run(fmt.Sprint(tc.width), func(t *testing.T) {
			big := append([]byte{0x61}, bytes.Repeat([]byte{0x55}, tc.length-1)...)
			first := []byte{0x65, 0x80}
			var out bytes.Buffer
			w, _ := NewAVCCWriter(&out, AVCCWriterConfig{LengthSize: tc.width})
			if err := w.WriteAccessUnit(h264.Packet{Data: auAnnex(first, big)}); err != nil {
				t.Fatal(err)
			}
			offset := tc.width + len(first)
			if !bytes.Equal(out.Bytes()[offset:offset+tc.width], tc.prefix) || !bytes.Equal(out.Bytes()[offset+tc.width:], big) {
				t.Fatal("多字节长度或NAL载荷错误")
			}
			if tc.width > 2 {
				return
			}
			out.Reset()
			big = append(big, 0x55)
			if err := w.WriteAccessUnit(h264.Packet{Data: auAnnex(first, big)}); !errors.Is(err, h264.ErrResourceLimit) || out.Len() != 0 {
				t.Fatal("后续NAL长度溢出前已写出首NAL", err)
			}
			if err := w.WriteAccessUnit(h264.Packet{Data: auAnnex(first)}); err != nil {
				t.Fatal("前缀容量错误污染了Writer", err)
			}
		})
	}
}

func TestAVCCWriter_ValidationAndBudgets(t *testing.T) {
	valid := []byte{0, 0, 1, 0x65, 0x80}
	for _, raw := range [][]byte{
		nil, {0, 0}, {1, 0x65, 0x80}, {2, 0x65, 0x80}, // 不猜测AVCC输入。
		{0, 0, 1}, {0, 0, 1, 0x67, 0x80},
		{0, 0, 1, 0x65, 0x80, 0, 0, 1, 0x81},
		{0, 0, 1, 0x65, 0x80, 0, 0, 0, 1},
	} {
		var out bytes.Buffer
		w, _ := NewAVCCWriter(&out, AVCCWriterConfig{})
		if err := w.WriteAccessUnit(h264.Packet{Data: raw}); !errors.Is(err, h264.ErrInvalidData) || out.Len() != 0 {
			t.Fatal("坏封装已提前写出", raw, err)
		}
		if err := w.WriteAccessUnit(h264.Packet{Data: valid}); err != nil {
			t.Fatal("结构错误后不能重试", err)
		}
	}
	// 输入12字节；4字节前缀输出14字节，输入和输出都必须受限。
	raw := []byte{0, 0, 1, 0x65, 0xaa, 0x80, 0, 0, 1, 0x61, 0xbb, 0x80}
	for _, cfg := range []AVCCWriterConfig{
		{MaxAccessUnitSize: 11}, {MaxAccessUnitSize: 12}, {MaxAccessUnitSize: 13},
		{MaxNALSize: 2}, {MaxNALUnits: 1},
	} {
		var out bytes.Buffer
		w, _ := NewAVCCWriter(&out, cfg)
		if err := w.WriteAccessUnit(h264.Packet{Data: raw}); !errors.Is(err, h264.ErrResourceLimit) || out.Len() != 0 {
			t.Fatal("资源限额前已写出", cfg, err)
		}
		if err := w.WriteAccessUnit(h264.Packet{Data: valid}); err != nil {
			t.Fatal("资源错误后不能重试", err)
		}
	}
	for _, tc := range []struct{ width, outputSize int }{{1, 8}, {2, 10}, {3, 12}, {4, 14}} {
		var out bytes.Buffer
		w, _ := NewAVCCWriter(&out, AVCCWriterConfig{LengthSize: tc.width, MaxNALSize: 3, MaxAccessUnitSize: max(len(raw), tc.outputSize), MaxNALUnits: 2})
		if err := w.WriteAccessUnit(h264.Packet{Data: raw}); err != nil || out.Len() != tc.outputSize {
			t.Fatal("恰好预算被拒绝", tc.width, err)
		}
	}
	for _, cfg := range []AVCCWriterConfig{{LengthSize: -1}, {LengthSize: 5}, {MaxNALSize: -1}, {MaxAccessUnitSize: -1}, {MaxNALUnits: -1}} {
		if _, err := NewAVCCWriter(io.Discard, cfg); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatal(err)
		}
	}
	if _, err := NewAVCCWriter(nil, AVCCWriterConfig{}); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal(err)
	}
}

func TestAVCCWriter_IOFailureIsSticky(t *testing.T) {
	raw := auAnnex([]byte{0x65, 0xaa, 0x80}, []byte{0x61, 0xbb, 0x80})
	for width := 1; width <= 4; width++ {
		for failAt := 1; failAt <= 4; failAt++ {
			for _, want := range []error{io.ErrShortWrite, io.ErrClosedPipe} {
				calls := 0
				sink := annexWriteFunc(func(p []byte) (int, error) {
					calls++
					if calls == failAt {
						if want == io.ErrShortWrite {
							return len(p) - 1, nil
						}
						return len(p), want
					}
					return len(p), nil
				})
				w, _ := NewAVCCWriter(sink, AVCCWriterConfig{LengthSize: width})
				err := w.WriteAccessUnit(h264.Packet{Data: raw})
				if !errors.Is(err, want) || calls != failAt {
					t.Fatal("短写或底层错误被吞掉/重试", width, failAt, err)
				}
				if again := w.WriteAccessUnit(h264.Packet{Data: raw}); again != err || calls != failAt {
					t.Fatal("失败后仍在写出", again)
				}
			}
		}
	}
	for _, count := range []int{-1, 100} {
		w, _ := NewAVCCWriter(annexWriteFunc(func([]byte) (int, error) { return count, nil }), AVCCWriterConfig{})
		if err := w.WriteAccessUnit(h264.Packet{Data: raw}); !errors.Is(err, io.ErrShortWrite) {
			t.Fatal("非法Write数量未被拒绝", err)
		}
	}
}

func TestAVCCWriter_AURoundTripAllWidths(t *testing.T) {
	params := auParameters()
	raw := auAnnex(params[0], params[1], auSlice("1", "0000", "0000", true), auSlice("1", "0001", "0010", false))
	src, _ := NewAnnexBReader(bytes.NewReader(raw), 0)
	packets := auPackets(t, src, AccessUnitConfig{})
	if len(packets) != 2 {
		t.Fatal("原始AU数量错误")
	}
	for width := 1; width <= 4; width++ {
		var out bytes.Buffer
		w, _ := NewAVCCWriter(&out, AVCCWriterConfig{LengthSize: width})
		for _, p := range packets {
			if err := w.WriteAccessUnit(p); err != nil {
				t.Fatal(err)
			}
		}
		r, _ := NewAVCCReader(bytes.NewReader(out.Bytes()), width, 0)
		if got := auPackets(t, r, AccessUnitConfig{}); !reflect.DeepEqual(got, packets) {
			t.Fatal("AVCC往返AU有变化", width)
		}
	}
}

func TestAVCCWriter_RegisteredCorpusRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		path   string
		frames int
	}{
		{"baseline/baseline_320x240_yuv420p_cavlc_ip_multislice_003.264", 6},
		{"main/main_320x240_yuv420p_cabac_ip_multislice_004.264", 4},
		{"high/high_192x128_yuv420p_cabac_multislice_007.264", 12},
		{"main/main_64x48_yuv420p_cabac_ipb_001.264", 3},
		{"main/main_16x16_yuv420p_cavlc_poc1_007.264", 6},
	} {
		t.Run(tc.path, func(t *testing.T) {
			raw, err := os.ReadFile("../testdata/" + tc.path)
			if err != nil {
				t.Fatal(err)
			}
			src, _ := NewAnnexBReader(bytes.NewReader(raw), 0)
			packets := auPackets(t, src, AccessUnitConfig{})
			if len(packets) != tc.frames {
				t.Fatal("登记语料AU数量变化", len(packets))
			}
			for width := 2; width <= 4; width++ {
				var out bytes.Buffer
				w, _ := NewAVCCWriter(&out, AVCCWriterConfig{LengthSize: width})
				for _, p := range packets {
					if err := w.WriteAccessUnit(p); err != nil {
						t.Fatal(err)
					}
				}
				for _, chunk := range []int{1, 2, 3, 7, 4096, out.Len()} {
					r, _ := NewAVCCReader(annexChunkReader{bytes.NewReader(out.Bytes()), chunk}, width, 0)
					if got := auPackets(t, r, AccessUnitConfig{}); !reflect.DeepEqual(got, packets) {
						t.Fatal("真实AU分块往返变化", width, chunk)
					}
				}
			}
		})
	}
}
