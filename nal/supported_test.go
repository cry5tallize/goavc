package nal

import (
	"errors"
	"testing"

	h264 "github.com/goavc/h264"
)

// TestUnitType_IsSupported 验证 M1 支持的 NAL 类型集合。
func TestUnitType_IsSupported(t *testing.T) {
	supported := []UnitType{
		TypeSlice, TypeSliceIDR, TypeSEI, TypeSPS, TypePPS,
		TypeAUD, TypeEndOfSequence, TypeEndOfStream, TypeFiller,
	}
	for _, typ := range supported {
		if !typ.IsSupported() {
			t.Fatalf("%s 应为支持类型", typ)
		}
	}
	unsupported := []UnitType{
		TypeUnspecified, TypeSlicePartitionA, TypeSlicePartitionB, TypeSlicePartitionC,
		TypeSPSExtension, TypePrefix, TypeSubsetSPS,
		TypeAuxiliarySlice, TypeSliceExtension, TypeSliceExtension3D,
		TypeReserved16, TypeReserved22, TypeUnspecified31,
	}
	for _, typ := range unsupported {
		if typ.IsSupported() {
			t.Fatalf("%s 不应为支持类型", typ)
		}
	}
}

// TestHeader_ValidateSupported 验证显式错误分类。
func TestHeader_ValidateSupported(t *testing.T) {
	if err := (Header{Type: TypeSPS}).ValidateSupported(); err != nil {
		t.Fatalf("SPS 应支持: %v", err)
	}
	err := (Header{Type: TypeSlicePartitionA}).ValidateSupported()
	if !errors.Is(err, h264.ErrUnsupported) {
		t.Fatalf("错误 = %v，期望 ErrUnsupported", err)
	}
}
