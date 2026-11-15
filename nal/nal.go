package nal

import (
	"fmt"

	h264 "github.com/cry5tallize/goavc"
)

// UnitType 是 NAL unit 的 5 bit nal_unit_type（H.264 表 7-1）。
type UnitType uint8

// NAL unit 类型常量。数值与 H.264 表 7-1 完全一致，不能重排。
const (
	TypeUnspecified       UnitType = 0
	TypeSlice             UnitType = 1  // 非 IDR slice
	TypeSlicePartitionA   UnitType = 2  // slice data partition A
	TypeSlicePartitionB   UnitType = 3  // slice data partition B
	TypeSlicePartitionC   UnitType = 4  // slice data partition C
	TypeSliceIDR          UnitType = 5  // IDR slice
	TypeSEI               UnitType = 6  // supplemental enhancement information
	TypeSPS               UnitType = 7  // sequence parameter set
	TypePPS               UnitType = 8  // picture parameter set
	TypeAUD               UnitType = 9  // access unit delimiter
	TypeEndOfSequence     UnitType = 10 // end of sequence
	TypeEndOfStream       UnitType = 11 // end of stream
	TypeFiller            UnitType = 12 // filler data
	TypeSPSExtension      UnitType = 13 // sequence parameter set extension
	TypePrefix            UnitType = 14 // prefix NAL unit
	TypeSubsetSPS         UnitType = 15 // subset sequence parameter set
	TypeReserved16        UnitType = 16
	TypeReserved17        UnitType = 17
	TypeReserved18        UnitType = 18
	TypeAuxiliarySlice    UnitType = 19 // auxiliary coded picture slice
	TypeSliceExtension    UnitType = 20 // slice extension
	TypeSliceExtension3D  UnitType = 21 // slice extension for depth view
	TypeReserved22        UnitType = 22
	TypeReserved23        UnitType = 23
	TypeUnspecified24     UnitType = 24
	TypeUnspecified25     UnitType = 25
	TypeUnspecified26     UnitType = 26
	TypeUnspecified27     UnitType = 27
	TypeUnspecified28     UnitType = 28
	TypeUnspecified29     UnitType = 29
	TypeUnspecified30     UnitType = 30
	TypeUnspecified31     UnitType = 31
	nalUnitTypeMax        UnitType = 31
	nalUnitTypeTableCount          = 32
)

// unitTypeNames 是表 7-1 的稳定名称，用于日志与测试。
var unitTypeNames = [nalUnitTypeTableCount]string{
	"unspecified",
	"slice",
	"slice partition A",
	"slice partition B",
	"slice partition C",
	"IDR slice",
	"SEI",
	"SPS",
	"PPS",
	"AUD",
	"end of sequence",
	"end of stream",
	"filler data",
	"SPS extension",
	"prefix NAL",
	"subset SPS",
	"reserved 16",
	"reserved 17",
	"reserved 18",
	"auxiliary slice",
	"slice extension",
	"slice extension 3D",
	"reserved 22",
	"reserved 23",
	"unspecified 24",
	"unspecified 25",
	"unspecified 26",
	"unspecified 27",
	"unspecified 28",
	"unspecified 29",
	"unspecified 30",
	"unspecified 31",
}

// String 返回稳定的类型名称。
func (t UnitType) String() string {
	if t > nalUnitTypeMax {
		return "invalid"
	}
	return unitTypeNames[t]
}

// Valid 判断类型是否落在 5 bit 可表示范围内。
func (t UnitType) Valid() bool { return t <= nalUnitTypeMax }

// IsVCL 判断该类型是否属于 VCL（video coding layer）NAL。
//
// VCL NAL 承载图像数据，是 Access Unit 的边界判定对象；非 VCL NAL
// （SPS/PPS/SEI/AUD 等）只作为同一 AU 的前缀或后缀。
func (t UnitType) IsVCL() bool {
	switch t {
	case TypeSlice, TypeSliceIDR, TypeSlicePartitionA, TypeSlicePartitionB,
		TypeSlicePartitionC, TypeAuxiliarySlice, TypeSliceExtension, TypeSliceExtension3D:
		return true
	default:
		return false
	}
}

// IsParameterSet 判断是否为参数集 NAL。
func (t UnitType) IsParameterSet() bool {
	switch t {
	case TypeSPS, TypePPS, TypeSPSExtension, TypeSubsetSPS:
		return true
	default:
		return false
	}
}

// IsSupported 判断核心语法层是否处理该 NAL 类型。
// 支持普通/IDR slice、A/B/C分区、参数集与基础非VCL；SPS扩展、
// prefix/subset SPS、辅助slice和slice扩展仍显式返回ErrUnsupported。
func (t UnitType) IsSupported() bool {
	switch t {
	case TypeSlice, TypeSliceIDR, TypeSlicePartitionA, TypeSlicePartitionB, TypeSlicePartitionC, TypeSEI, TypeSPS, TypePPS,
		TypeAUD, TypeEndOfSequence, TypeEndOfStream, TypeFiller:
		return true
	default:
		return false
	}
}

// ValidateSupported 对未支持的 NAL 类型返回包装 ErrUnsupported 的错误。
func (h Header) ValidateSupported() error {
	if !h.Type.IsSupported() {
		return fmt.Errorf("nal: unit type %d (%s) is not supported: %w",
			h.Type, h.Type, h264.ErrUnsupported)
	}
	return nil
}

// Header 是 NAL unit header（H.264 §7.3.1）：
//
//	forbidden_zero_bit  f(1)
//	nal_ref_idc         u(2)
//	nal_unit_type       u(5)
//
// 单字节头之后才是 payload。Header 不包含 payload 长度信息；
// Annex-B/AVCC 层负责确定边界。
type Header struct {
	ForbiddenZeroBit bool
	RefIDC           uint8
	Type             UnitType
}

// ParseHeader 解析一个 NAL unit header 字节。
//
// forbidden_zero_bit 必须为 0（§7.4.1）；为 1 表示码流损坏或同步丢失，
// 返回包装 ErrInvalidData 的错误。RefIDC 与 Type 由位掩码直接取出，
// 不会越界。
func ParseHeader(b byte) (Header, error) {
	h := Header{
		ForbiddenZeroBit: b&0x80 != 0,
		RefIDC:           (b >> 5) & 0x03,
		Type:             UnitType(b & 0x1F),
	}
	if h.ForbiddenZeroBit {
		return Header{}, fmt.Errorf("nal: forbidden_zero_bit set in header %#02x: %w", b, h264.ErrInvalidData)
	}
	return h, nil
}

// Byte 序列化回单字节头。ForbiddenZeroBit 为 true 时仍写出 0，
// 因为本实现拒绝生成非法头；需要测试异常路径时请直接构造字节。
func (h Header) Byte() byte {
	return h.RefIDC<<5 | byte(h.Type&0x1F)
}

// IsIDR 判断是否为 IDR slice。
func (h Header) IsIDR() bool { return h.Type == TypeSliceIDR }

// IsVCL 判断是否为 VCL NAL。
func (h Header) IsVCL() bool { return h.Type.IsVCL() }

// String 输出便于日志阅读的头信息。
func (h Header) String() string {
	return fmt.Sprintf("nal(%s, ref_idc=%d)", h.Type, h.RefIDC)
}
