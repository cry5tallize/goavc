package nal

import (
	"errors"
	"testing"

	h264 "github.com/cry5tallize/goavc"
)

// TestParseHeader_RoundTrip 遍历全部 5 bit 类型与 2 bit ref_idc。
func TestParseHeader_RoundTrip(t *testing.T) {
	for refIDC := uint8(0); refIDC < 4; refIDC++ {
		for typ := UnitType(0); typ <= nalUnitTypeMax; typ++ {
			b := refIDC<<5 | byte(typ)
			h, err := ParseHeader(b)
			if err != nil {
				t.Fatalf("ParseHeader(%#02x) 失败: %v", b, err)
			}
			if h.ForbiddenZeroBit {
				t.Fatalf("%#02x: forbidden_zero_bit 应为 false", b)
			}
			if h.RefIDC != refIDC || h.Type != typ {
				t.Fatalf("%#02x: 解析为 ref_idc=%d type=%d", b, h.RefIDC, h.Type)
			}
			if got := h.Byte(); got != b {
				t.Fatalf("%#02x: Byte() = %#02x", b, got)
			}
		}
	}
}

// TestParseHeader_ForbiddenBit 验证非法头被拒绝。
func TestParseHeader_ForbiddenBit(t *testing.T) {
	for _, b := range []byte{0x80, 0xA1, 0xFF} {
		if _, err := ParseHeader(b); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("ParseHeader(%#02x) 错误 = %v，期望 ErrInvalidData", b, err)
		}
	}
}

// TestUnitType_String 覆盖表 7-1 的名称与越界值。
func TestUnitType_String(t *testing.T) {
	cases := map[UnitType]string{
		TypeUnspecified:      "unspecified",
		TypeSlice:            "slice",
		TypeSlicePartitionA:  "slice partition A",
		TypeSlicePartitionB:  "slice partition B",
		TypeSlicePartitionC:  "slice partition C",
		TypeSliceIDR:         "IDR slice",
		TypeSEI:              "SEI",
		TypeSPS:              "SPS",
		TypePPS:              "PPS",
		TypeAUD:              "AUD",
		TypeEndOfSequence:    "end of sequence",
		TypeEndOfStream:      "end of stream",
		TypeFiller:           "filler data",
		TypeSPSExtension:     "SPS extension",
		TypePrefix:           "prefix NAL",
		TypeSubsetSPS:        "subset SPS",
		TypeAuxiliarySlice:   "auxiliary slice",
		TypeSliceExtension:   "slice extension",
		TypeSliceExtension3D: "slice extension 3D",
		UnitType(32):         "invalid",
	}
	for typ, want := range cases {
		if got := typ.String(); got != want {
			t.Fatalf("UnitType(%d).String() = %q，期望 %q", typ, got, want)
		}
	}
	for typ := UnitType(0); typ <= nalUnitTypeMax; typ++ {
		if !typ.Valid() {
			t.Fatalf("类型 %d 应合法", typ)
		}
	}
	if UnitType(32).Valid() {
		t.Fatal("类型 32 应非法")
	}
}

// TestUnitType_Classification 验证 VCL 与参数集分类。
func TestUnitType_Classification(t *testing.T) {
	vcl := []UnitType{
		TypeSlice, TypeSliceIDR, TypeSlicePartitionA, TypeSlicePartitionB,
		TypeSlicePartitionC, TypeAuxiliarySlice, TypeSliceExtension, TypeSliceExtension3D,
	}
	for _, typ := range vcl {
		if !typ.IsVCL() {
			t.Fatalf("%s 应为 VCL", typ)
		}
	}
	nonVCL := []UnitType{TypeUnspecified, TypeSEI, TypeSPS, TypePPS, TypeAUD, TypeEndOfStream, TypeFiller}
	for _, typ := range nonVCL {
		if typ.IsVCL() {
			t.Fatalf("%s 不应为 VCL", typ)
		}
	}

	for _, typ := range []UnitType{TypeSPS, TypePPS, TypeSPSExtension, TypeSubsetSPS} {
		if !typ.IsParameterSet() {
			t.Fatalf("%s 应为参数集", typ)
		}
	}
	for _, typ := range []UnitType{TypeSlice, TypeSEI, TypeAUD} {
		if typ.IsParameterSet() {
			t.Fatalf("%s 不应为参数集", typ)
		}
	}
}

// TestHeader_IsIDR 验证 IDR 判定。
func TestHeader_IsIDR(t *testing.T) {
	if !(Header{RefIDC: 3, Type: TypeSliceIDR}).IsIDR() {
		t.Fatal("type 5 应为 IDR")
	}
	if (Header{RefIDC: 3, Type: TypeSlice}).IsIDR() {
		t.Fatal("type 1 不应为 IDR")
	}
}
