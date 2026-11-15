package syntax

import (
	"bytes"
	"errors"
	"math"
	"strings"
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/bitstream"
)

// 仅用基础位写入器构造故意非法的语法，不绕过公开 API 的被测验证逻辑。
func rawHRDForTest(t *testing.T, h *HRDParameters) []byte {
	t.Helper()
	w := bitstream.NewWriter()
	if err := w.WriteUE(h.CPBCountMinus1); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteBits(4, uint64(h.BitRateScale)); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteBits(4, uint64(h.CPBSizeScale)); err != nil {
		t.Fatal(err)
	}
	for _, e := range h.CPB {
		if err := w.WriteUE(e.BitRateValueMinus1); err != nil {
			t.Fatal(err)
		}
		if err := w.WriteUE(e.CPBSizeValueMinus1); err != nil {
			t.Fatal(err)
		}
		var cbr uint8
		if e.CBRFlag {
			cbr = 1
		}
		if err := w.WriteBit(cbr); err != nil {
			t.Fatal(err)
		}
	}
	for _, length := range []uint8{
		h.InitialCPBRemovalDelayLengthMinus1, h.CPBRemovalDelayLengthMinus1,
		h.DPBOutputDelayLengthMinus1, h.TimeOffsetLength,
	} {
		if err := w.WriteBits(5, uint64(length)); err != nil {
			t.Fatal(err)
		}
	}
	return w.Bytes()
}

func hrdBitsForTest(t *testing.T, bits string) []byte {
	t.Helper()
	bits = strings.Join(strings.Fields(bits), "")
	data := make([]byte, (len(bits)+7)/8)
	for i, b := range bits {
		switch b {
		case '1':
			data[i/8] |= 1 << (7 - uint(i%8))
		case '0':
		default:
			t.Fatalf("非法测试比特 %q", b)
		}
	}
	return data
}

func TestHRDValidationBoundaries(t *testing.T) {
	cases := []struct {
		name string
		edit func(*HRDParameters)
	}{
		{"count limit", func(h *HRDParameters) { h.CPBCountMinus1 = 32 }},
		{"count overflow", func(h *HRDParameters) { h.CPBCountMinus1 = math.MaxUint32 }},
		{"no entries", func(h *HRDParameters) { h.CPB = nil }},
		{"count mismatch", func(h *HRDParameters) { h.CPBCountMinus1 = 1 }},
		{"rate scale", func(h *HRDParameters) { h.BitRateScale = 16 }},
		{"size scale", func(h *HRDParameters) { h.CPBSizeScale = 16 }},
		{"initial length", func(h *HRDParameters) { h.InitialCPBRemovalDelayLengthMinus1 = 32 }},
		{"cpb length", func(h *HRDParameters) { h.CPBRemovalDelayLengthMinus1 = 32 }},
		{"dpb length", func(h *HRDParameters) { h.DPBOutputDelayLengthMinus1 = 32 }},
		{"offset length", func(h *HRDParameters) { h.TimeOffsetLength = 32 }},
		{"rate value", func(h *HRDParameters) { h.CPB[0].BitRateValueMinus1 = math.MaxUint32 }},
		{"size value", func(h *HRDParameters) { h.CPB[0].CPBSizeValueMinus1 = math.MaxUint32 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := &HRDParameters{CPB: []CPBEntry{{}}}
			tc.edit(h)
			if err := h.Validate(); !errors.Is(err, h264.ErrInvalidData) {
				t.Fatalf("Validate: %v", err)
			}
			w := bitstream.NewWriter()
			if err := w.WriteBits(3, 5); err != nil {
				t.Fatal(err)
			}
			before := w.Bytes()
			if err := h.WriteTo(w); !errors.Is(err, h264.ErrInvalidData) {
				t.Fatalf("WriteTo: %v", err)
			}
			if w.BitsWritten() != 3 || !bytes.Equal(w.Bytes(), before) {
				t.Fatal("无效 HRD 在预验证前修改了写入器")
			}
		})
	}
	var h *HRDParameters
	if err := h.Validate(); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("nil HRD: %v", err)
	}
	if _, err := ParseHRDParameters(nil); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("nil reader: %v", err)
	}
	h = &HRDParameters{CPB: []CPBEntry{{}}}
	if err := h.WriteTo(nil); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("nil writer: %v", err)
	}
	for _, index := range []int{-1, 1, math.MaxInt} {
		if _, err := h.BitRate(index); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("BitRate(%d): %v", index, err)
		}
		if _, err := h.CPBSize(index); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("CPBSize(%d): %v", index, err)
		}
	}
}

func TestHRDParseRejectsForbiddenValuesAndOrdering(t *testing.T) {
	cases := []struct {
		name string
		cpb  []CPBEntry
	}{
		{"maximum rate minus1", []CPBEntry{{BitRateValueMinus1: math.MaxUint32}}},
		{"maximum size minus1", []CPBEntry{{CPBSizeValueMinus1: math.MaxUint32}}},
		{"equal rates", []CPBEntry{{}, {}}},
		{"decreasing rates", []CPBEntry{{BitRateValueMinus1: 1}, {}}},
		{"increasing sizes", []CPBEntry{{}, {BitRateValueMinus1: 1, CPBSizeValueMinus1: 1}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := &HRDParameters{CPBCountMinus1: uint32(len(tc.cpb) - 1), CPB: tc.cpb}
			if _, err := ParseHRDParameters(bitstream.NewReader(rawHRDForTest(t, h))); !errors.Is(err, h264.ErrInvalidData) {
				t.Fatalf("解析非法 HRD: %v", err)
			}
			if err := h.WriteTo(bitstream.NewWriter()); !errors.Is(err, h264.ErrInvalidData) {
				t.Fatalf("写出非法 HRD: %v", err)
			}
		})
	}
}

func TestHRDMaximumCountAndValues(t *testing.T) {
	h := &HRDParameters{
		CPBCountMinus1: 31, BitRateScale: 15, CPBSizeScale: 15,
		CPB:                                make([]CPBEntry, 32),
		InitialCPBRemovalDelayLengthMinus1: 31,
		CPBRemovalDelayLengthMinus1:        31,
		DPBOutputDelayLengthMinus1:         31,
		TimeOffsetLength:                   31,
	}
	for i := range h.CPB {
		h.CPB[i] = CPBEntry{BitRateValueMinus1: uint32(i), CPBSizeValueMinus1: 31}
	}
	if err := h.Validate(); err != nil {
		t.Fatal(err)
	}
	if parsed, err := ParseHRDParameters(bitstream.NewReader(rawHRDForTest(t, h))); err != nil || len(parsed.CPB) != 32 {
		t.Fatalf("最大计数解析: %+v, %v", parsed, err)
	}
	// E.2.2 的最大合法 minus1 是 2^32-2，不是 uint32 最大值。
	h.CPBCountMinus1 = 0
	h.CPB = []CPBEntry{{BitRateValueMinus1: math.MaxUint32 - 1, CPBSizeValueMinus1: math.MaxUint32 - 1}}
	parsed, err := ParseHRDParameters(bitstream.NewReader(rawHRDForTest(t, h)))
	if err != nil {
		t.Fatal(err)
	}
	if rate, err := parsed.BitRate(0); err != nil || rate != 9_007_199_252_643_840 {
		t.Fatalf("最大码率 = %d, %v", rate, err)
	}
	if size, err := parsed.CPBSize(0); err != nil || size != 2_251_799_813_160_960 {
		t.Fatalf("最大容量 = %d, %v", size, err)
	}
}

func TestHRDVUIIndependentVectors(t *testing.T) {
	// 前五个 present_flag=0，NAL HRD=1，接独立的 32 位 HRD，
	// 最后为 VCL HRD、low_delay、pic_struct、restriction 四个零。
	data := hrdBitsForTest(t, "00000 1 10000000 01110000 00000000 00000000 0 0 0 0")
	r := bitstream.NewReader(data)
	v, err := ParseVUI(r)
	if err != nil {
		t.Fatal(err)
	}
	if r.BitsRead() != 42 || !v.NALHRDParametersPresentFlag || v.NALHRD == nil ||
		v.VCLHRDParametersPresentFlag || v.LowDelayHRDFlag || !v.NALHRD.CPB[0].CBRFlag {
		t.Fatalf("独立 VUI 向量解析错误: %+v，消费 %d 位", v, r.BitsRead())
	}
	w := bitstream.NewWriter()
	if err := v.WriteTo(w); err != nil || w.BitsWritten() != 42 || !bytes.Equal(w.Bytes(), data) {
		t.Fatalf("独立 VUI 向量写出: %x/%d, %v", w.Bytes(), w.BitsWritten(), err)
	}
	// 扩展 SAR 的 0/0 明确表示未指定，不是非法的分母零。
	zeroSAR := hrdBitsForTest(t, "1 11111111 0000000000000000 0000000000000000 00000000")
	if v, err := ParseVUI(bitstream.NewReader(zeroSAR)); err != nil || v.SARWidth != 0 || v.SARHeight != 0 {
		t.Fatalf("未指定 SAR: %+v, %v", v, err)
	}
	// E.2.1 表 E-2 的保留 video_format=7 应保留而非误报截断或损坏。
	reserved := hrdBitsForTest(t, "0 0 1 111 0 0 0 0 0 0 0 0")
	if v, err := ParseVUI(bitstream.NewReader(reserved)); err != nil || v.VideoFormat != 7 {
		t.Fatalf("保留 video_format: %+v, %v", v, err)
	}
}

func TestHRDParseRejectsMaximumCountBeforeAllocation(t *testing.T) {
	h := &HRDParameters{CPBCountMinus1: math.MaxUint32}
	if _, err := ParseHRDParameters(bitstream.NewReader(rawHRDForTest(t, h))); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("uint32 最大 CPB 计数未被拒绝: %v", err)
	}
}
