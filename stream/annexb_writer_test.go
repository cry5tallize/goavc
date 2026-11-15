package stream

import (
	"bytes"
	"errors"
	"io"
	"os"
	"reflect"
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/nal"
)

func TestAnnexBWriter_KnownBytesAndRescan(t *testing.T) {
	// 独立Annex B向量：前导/尾零、混合起始码、转义零及header-only EOS。
	raw := []byte{0, 0, 0, 0, 1, 0x67, 0x42, 0x80,
		0, 0, 1, 0x68, 0xce, 0x80,
		0, 0, 1, 0x65, 0xab, 0, 0, 3, 1, 0x80,
		0, 0, 0, 1, 0x0b, 0, 0}
	want := []byte{0, 0, 0, 1, 0x67, 0x42, 0x80,
		0, 0, 0, 1, 0x68, 0xce, 0x80,
		0, 0, 0, 1, 0x65, 0xab, 0, 0, 3, 1, 0x80,
		0, 0, 0, 1, 0x0b}
	wantUnits := []nal.Unit{
		{Header: nal.Header{RefIDC: 3, Type: nal.TypeSPS}, Payload: []byte{0x42, 0x80}},
		{Header: nal.Header{RefIDC: 3, Type: nal.TypePPS}, Payload: []byte{0xce, 0x80}},
		{Header: nal.Header{RefIDC: 3, Type: nal.TypeSliceIDR}, Payload: []byte{0xab, 0, 0, 3, 1, 0x80}},
		{Header: nal.Header{Type: nal.TypeEndOfStream}, Payload: []byte{}},
	}
	var out bytes.Buffer
	w, err := NewAnnexBWriter(&out, AnnexBWriterConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatal("构造时输出了数据")
	}
	for range 2 {
		if err := w.WriteAccessUnit(h264.Packet{Data: raw, PTS: 100, DTS: 80, Duration: 20, KeyFrame: true}); err != nil {
			t.Fatal(err)
		}
	}
	clear(raw)
	if !bytes.Equal(out.Bytes(), bytes.Repeat(want, 2)) {
		t.Fatalf("固定输出字节不符: %x", out.Bytes())
	}
	got, err := nal.SplitAnnexB(out.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, append(wantUnits, wantUnits...)) {
		t.Fatal("重扫NAL不符", got)
	}
}

func TestAnnexBWriter_ValidateBeforeOutputAndRetry(t *testing.T) {
	valid := []byte{0, 0, 1, 0x65, 0x80}
	for _, raw := range [][]byte{
		nil, {1, 0x65, 0x80}, {0, 0}, {0, 0, 0, 1},
		{0, 0, 1, 0x67, 0x80}, // 参数集不能假装完整AU。
		{0, 0, 1, 0x65, 0x80, 0, 0, 1, 0x81},
		{0, 0, 1, 0x65, 0x80, 0, 0, 0, 1},
		{0, 0, 1, 0x65, 0x80, 0, 0, 1, 0, 0, 1, 0x0b},
	} {
		var out bytes.Buffer
		w, _ := NewAnnexBWriter(&out, AnnexBWriterConfig{})
		if err := w.WriteAccessUnit(h264.Packet{Data: raw}); !errors.Is(err, h264.ErrInvalidData) || out.Len() != 0 {
			t.Fatal("坏尾部前已写出或错误分类不符", raw, err)
		}
		if err := w.WriteAccessUnit(h264.Packet{Data: valid}); err != nil || !bytes.Equal(out.Bytes(), []byte{0, 0, 0, 1, 0x65, 0x80}) {
			t.Fatal("校验错误后无法重试", err)
		}
	}
}

func TestAnnexBWriter_ResourceBudgets(t *testing.T) {
	// 输入12字节，规范化14字节；单NAL大小为3。
	raw := []byte{0, 0, 1, 0x65, 0xaa, 0x80, 0, 0, 1, 0x61, 0xbb, 0x80}
	for _, cfg := range []AnnexBWriterConfig{
		{MaxAccessUnitSize: 11}, {MaxAccessUnitSize: 12}, {MaxAccessUnitSize: 13},
		{MaxNALSize: 2}, {MaxNALUnits: 1},
	} {
		var out bytes.Buffer
		w, _ := NewAnnexBWriter(&out, cfg)
		if err := w.WriteAccessUnit(h264.Packet{Data: raw}); !errors.Is(err, h264.ErrResourceLimit) || out.Len() != 0 {
			t.Fatal("资源超限前已写出", cfg, err)
		}
		if err := w.WriteAccessUnit(h264.Packet{Data: []byte{0, 0, 1, 0x65, 0x80}}); err != nil {
			t.Fatal("限额失败污染Writer", err)
		}
	}
	var out bytes.Buffer
	w, _ := NewAnnexBWriter(&out, AnnexBWriterConfig{MaxAccessUnitSize: 14, MaxNALSize: 3, MaxNALUnits: 2})
	if err := w.WriteAccessUnit(h264.Packet{Data: raw}); err != nil || out.Len() != 14 {
		t.Fatal("恰好预算被拒绝", err)
	}
	for _, cfg := range []AnnexBWriterConfig{{MaxNALSize: -1}, {MaxAccessUnitSize: -1}, {MaxNALUnits: -1}} {
		if _, err := NewAnnexBWriter(io.Discard, cfg); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatal(err)
		}
	}
	if _, err := NewAnnexBWriter(nil, AnnexBWriterConfig{}); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal(err)
	}
}

type annexWriteFunc func([]byte) (int, error)

func (f annexWriteFunc) Write(p []byte) (int, error) { return f(p) }

func TestAnnexBWriter_IOFailureIsSticky(t *testing.T) {
	raw := auAnnex([]byte{0x65, 0xaa, 0x80}, []byte{0x61, 0xbb, 0x80})
	// 每个起始码/header与payload的写调用分别失败，覆盖AU中途的位置。
	for failAt := 1; failAt <= 4; failAt++ {
		for _, want := range []error{io.ErrShortWrite, io.ErrClosedPipe} {
			calls := 0
			sink := annexWriteFunc(func(p []byte) (int, error) {
				calls++
				if calls == failAt {
					if want == io.ErrShortWrite {
						return len(p) - 1, nil
					}
					return 1, want
				}
				return len(p), nil
			})
			w, _ := NewAnnexBWriter(sink, AnnexBWriterConfig{})
			err := w.WriteAccessUnit(h264.Packet{Data: raw})
			if !errors.Is(err, want) || calls != failAt {
				t.Fatal("I/O错误被吞掉或重试", err, calls)
			}
			if again := w.WriteAccessUnit(h264.Packet{Data: raw}); again != err || calls != failAt {
				t.Fatal("错误后继续写出", again, calls)
			}
		}
	}
	for _, count := range []int{-1, 100} {
		w, _ := NewAnnexBWriter(annexWriteFunc(func([]byte) (int, error) { return count, nil }), AnnexBWriterConfig{})
		if err := w.WriteAccessUnit(h264.Packet{Data: raw}); !errors.Is(err, io.ErrShortWrite) {
			t.Fatal("底层非法数量未拒绝", err)
		}
	}
}

func TestAnnexBWriter_AUReaderPackets(t *testing.T) {
	params := auParameters()
	raw := auAnnex(params[0], params[1], auSlice("1", "0000", "0000", true), auSlice("1", "0001", "0010", false))
	src, _ := NewAnnexBReader(bytes.NewReader(raw), 0)
	packets := auPackets(t, src, AccessUnitConfig{})
	var out bytes.Buffer
	w, _ := NewAnnexBWriter(&out, AnnexBWriterConfig{})
	for _, p := range packets {
		if err := w.WriteAccessUnit(p); err != nil {
			t.Fatal(err)
		}
		clear(p.Data)
	}
	if !bytes.Equal(out.Bytes(), raw) {
		t.Fatal("AU Reader→Writer丢失NAL或保留输入引用")
	}
}

func TestAnnexBWriter_RegisteredMultisliceCorpus(t *testing.T) {
	raw, err := os.ReadFile("../testdata/high/high_192x128_yuv420p_cabac_multislice_007.264")
	if err != nil {
		t.Fatal(err)
	}
	src, _ := NewAnnexBReader(annexChunkReader{bytes.NewReader(raw), 7}, 0)
	packets := auPackets(t, src, AccessUnitConfig{})
	if len(packets) != 12 {
		t.Fatal("语料AU计数不符", len(packets))
	}
	var out bytes.Buffer
	w, _ := NewAnnexBWriter(&out, AnnexBWriterConfig{})
	for _, p := range packets {
		if err := w.WriteAccessUnit(p); err != nil {
			t.Fatal(err)
		}
	}
	before, err := nal.SplitAnnexB(raw)
	if err != nil {
		t.Fatal(err)
	}
	after, err := nal.SplitAnnexB(out.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("High CABAC多slice语料重扫发生变化")
	}
}
