package syntax

import (
	"bytes"
	"errors"
	"math"
	"reflect"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
)

// 此向量直接按 E.1.2 拼出，不依赖本包的写出函数：
// ue(0)、两个四位零、ue(0)、ue(0)、cbr=1、四个五位零，共 32 位。
func TestHRDFixedBitVector(t *testing.T) {
	data := []byte{0x80, 0x70, 0x00, 0x00}
	r := bitstream.NewReader(data)
	h, err := ParseHRDParameters(r)
	if err != nil {
		t.Fatal(err)
	}
	if r.BitsRead() != 32 || h.CPBCountMinus1 != 0 || len(h.CPB) != 1 || !h.CPB[0].CBRFlag {
		t.Fatalf("HRD 解析错误: %+v，消费 %d 位", h, r.BitsRead())
	}
	if rate, err := h.BitRate(0); err != nil || rate != 64 {
		t.Fatalf("码率 = %d, %v，期望 64", rate, err)
	}
	if size, err := h.CPBSize(0); err != nil || size != 16 {
		t.Fatalf("CPB = %d, %v，期望 16", size, err)
	}
	w := bitstream.NewWriter()
	if err := h.WriteTo(w); err != nil {
		t.Fatal(err)
	}
	if w.BitsWritten() != 32 || !bytes.Equal(w.Bytes(), data) {
		t.Fatalf("HRD 比特 = %x/%d，期望 %x/32", w.Bytes(), w.BitsWritten(), data)
	}
}

func TestHRDTruncatedFixedVector(t *testing.T) {
	data := []byte{0x80, 0x70, 0x00, 0x00}
	for n := range len(data) {
		if _, err := ParseHRDParameters(bitstream.NewReader(data[:n])); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("截断为 %d 字节: %v", n, err)
		}
	}
	// ue(32)=00000100001，必须在按计数分配 CPB 之前拒绝。
	if _, err := ParseHRDParameters(bitstream.NewReader([]byte{0x04, 0x20})); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("cpb_cnt_minus1=32: %v", err)
	}
}

func TestHRDFixedGeneratorExactValues(t *testing.T) {
	cases := []struct {
		name string
		rate uint64
		size uint64
	}{
		{"minimum", 64, 16},
		{"common", 1_000_000, 2_000_000},
		{"scale transition", 274_877_906_944, 68_719_476_736},
		{"maximum", 9_007_199_252_643_840, 2_251_799_813_160_960},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, cbr := range []bool{false, true} {
				h, err := NewFixedHRDParameters(tc.rate, tc.size, cbr)
				if err != nil {
					t.Fatal(err)
				}
				if h.CPBCountMinus1 != 0 || len(h.CPB) != 1 || h.CPB[0].CBRFlag != cbr {
					t.Fatalf("固定 HRD 结构错误: %+v", h)
				}
				if got, err := h.BitRate(0); err != nil || got != tc.rate {
					t.Fatalf("码率 %d，期望 %d: %v", got, tc.rate, err)
				}
				if got, err := h.CPBSize(0); err != nil || got != tc.size {
					t.Fatalf("CPB %d，期望 %d: %v", got, tc.size, err)
				}
				if err := h.Validate(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestHRDFixedGeneratorRejectsRounding(t *testing.T) {
	cases := [][2]uint64{
		{0, 16}, {64, 0}, {63, 16}, {65, 16}, {64, 15}, {64, 17},
		{274_877_907_008, 16}, {64, 68_719_476_752},
		{9_007_199_252_643_904, 16}, {64, 2_251_799_813_160_976},
		{math.MaxUint64, 16}, {64, math.MaxUint64},
	}
	for _, tc := range cases {
		if h, err := NewFixedHRDParameters(tc[0], tc[1], true); h != nil || !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("生成 %d/%d 得到 %+v, %v，期望明确拒绝", tc[0], tc[1], h, err)
		}
	}
}

func TestHRDFixedGeneratorIndependentVector(t *testing.T) {
	h, err := NewFixedHRDParameters(64, 16, true)
	if err != nil {
		t.Fatal(err)
	}
	// E.1.2：最小码率/容量，三个 delay_length_minus1=23，offset_length=24。
	// 1 | 0000 | 0000 | 1 | 1 | 1 | 10111 | 10111 | 10111 | 11000。
	want := []byte{0x80, 0x7b, 0xde, 0xf8}
	w := bitstream.NewWriter()
	if err := h.WriteTo(w); err != nil {
		t.Fatal(err)
	}
	if w.BitsWritten() != 32 || !bytes.Equal(w.Bytes(), want) {
		t.Fatalf("固定 HRD = %x/%d，期望 %x/32", w.Bytes(), w.BitsWritten(), want)
	}
}

func TestHRDDerivedValuesRejectInvalidStructure(t *testing.T) {
	for _, h := range []*HRDParameters{
		nil,
		{CPB: []CPBEntry{{BitRateValueMinus1: math.MaxUint32}}},
		{CPB: []CPBEntry{{CPBSizeValueMinus1: math.MaxUint32}}},
		{BitRateScale: 255, CPB: []CPBEntry{{}}},
		{CPBSizeScale: 255, CPB: []CPBEntry{{}}},
	} {
		if value, err := h.BitRate(0); value != 0 || !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("无效结构派生码率 = %d, %v", value, err)
		}
		if value, err := h.CPBSize(0); value != 0 || !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("无效结构派生容量 = %d, %v", value, err)
		}
	}
}

func TestHRDFixedGeneratorScaleSelection(t *testing.T) {
	for _, tc := range []struct {
		rate, size uint64
		scale      uint8
		minus1     uint32
	}{
		{64, 16, 0, 0},
		{274_877_906_880, 68_719_476_720, 0, math.MaxUint32 - 1},
		{274_877_906_944, 68_719_476_736, 1, 2_147_483_647},
		{9_007_199_252_643_840, 2_251_799_813_160_960, 15, math.MaxUint32 - 1},
	} {
		h, err := NewFixedHRDParameters(tc.rate, tc.size, false)
		if err != nil {
			t.Fatal(err)
		}
		if h.BitRateScale != tc.scale || h.CPBSizeScale != tc.scale ||
			h.CPB[0].BitRateValueMinus1 != tc.minus1 || h.CPB[0].CPBSizeValueMinus1 != tc.minus1 {
			t.Fatalf("精确尺度选择错误: %+v，期望尺度 %d/值 %d", h, tc.scale, tc.minus1)
		}
	}
}

// 表 E.1.2 的非零尺度、多 CPB 和不相等长度字段的独立固定字节向量。
// ue(1)|0011|0100|ue(2)|ue(4)|1|ue(5)|ue(1)|0|11111|00000|01111|00000，49 位。
// 后接 10101，检验解析器不吞掉调用方的后续字段。
func TestHRDAlternativeScheduleFixedVector(t *testing.T) {
	data := []byte{0x46, 0x8c, 0xb3, 0x27, 0xc0, 0xf0, 0x54}
	want := &HRDParameters{
		CPBCountMinus1: 1, BitRateScale: 3, CPBSizeScale: 4,
		CPB: []CPBEntry{
			{BitRateValueMinus1: 2, CPBSizeValueMinus1: 4, CBRFlag: true},
			{BitRateValueMinus1: 5, CPBSizeValueMinus1: 1},
		},
		InitialCPBRemovalDelayLengthMinus1: 31,
		DPBOutputDelayLengthMinus1:         15,
	}
	r := bitstream.NewReader(data)
	got, err := ParseHRDParameters(r)
	if err != nil || !reflect.DeepEqual(got, want) || r.BitsRead() != 49 {
		t.Fatalf("固定向量解析 = %+v/%d, %v", got, r.BitsRead(), err)
	}
	if tail, err := r.ReadBits(5); err != nil || tail != 21 {
		t.Fatalf("HRD 后续字段 = %d, %v", tail, err)
	}
	w := bitstream.NewWriter()
	if err := want.WriteTo(w); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteBits(5, 21); err != nil {
		t.Fatal(err)
	}
	if w.BitsWritten() != 54 || !bytes.Equal(w.Bytes(), data) {
		t.Fatalf("固定向量生成 = %x/%d", w.Bytes(), w.BitsWritten())
	}
	for i, expected := range [][2]uint64{{1536, 1280}, {3072, 512}} {
		rate, rateErr := got.BitRate(i)
		size, sizeErr := got.CPBSize(i)
		if rateErr != nil || sizeErr != nil || rate != expected[0] || size != expected[1] {
			t.Fatalf("调度%d派生值 = %d/%d, %v/%v", i, rate, size, rateErr, sizeErr)
		}
	}
}
