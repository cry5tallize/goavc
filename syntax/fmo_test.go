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

func fmoTestSPS() *SPS {
	return &SPS{FrameMbsOnlyFlag: true, PicWidthInMbsMinus1: 3, PicHeightInMapUnitsMinus1: 2, PicOrderCntType: 2}
}

// 每张表直接按规范手写，不能以被测映射算法产生期望值。
func TestFMOLiteralMaps(t *testing.T) {
	cases := []struct {
		name  string
		pps   PPS
		cycle uint32
		want  []uint8
	}{
		{"single", PPS{}, 0, []uint8{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}},
		{"interleaved", PPS{NumSliceGroupsMinus1: 2, RunLengthMinus1: []uint32{0, 1, 2}}, 0, []uint8{0, 1, 1, 2, 2, 2, 0, 1, 1, 2, 2, 2}},
		{"dispersed", PPS{NumSliceGroupsMinus1: 2, SliceGroupMapType: 1}, 0, []uint8{0, 1, 2, 0, 1, 2, 0, 1, 0, 1, 2, 0}},
		{"foreground", PPS{NumSliceGroupsMinus1: 2, SliceGroupMapType: 2, TopLeft: []uint32{1, 0}, BottomRight: []uint32{6, 10}}, 0, []uint8{1, 0, 0, 2, 1, 0, 0, 2, 1, 1, 1, 2}},
		{"box-out-left", PPS{NumSliceGroupsMinus1: 1, SliceGroupMapType: 3}, 5, []uint8{1, 0, 0, 0, 1, 0, 0, 1, 1, 1, 1, 1}},
		{"box-out-right", PPS{NumSliceGroupsMinus1: 1, SliceGroupMapType: 3, SliceGroupChangeDirectionFlag: true}, 5, []uint8{1, 1, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1}},
		{"raster-forward", PPS{NumSliceGroupsMinus1: 1, SliceGroupMapType: 4}, 5, []uint8{0, 0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1}},
		{"raster-reverse", PPS{NumSliceGroupsMinus1: 1, SliceGroupMapType: 4, SliceGroupChangeDirectionFlag: true}, 5, []uint8{1, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0}},
		{"wipe-forward", PPS{NumSliceGroupsMinus1: 1, SliceGroupMapType: 5}, 5, []uint8{0, 0, 1, 1, 0, 0, 1, 1, 0, 1, 1, 1}},
		{"wipe-reverse", PPS{NumSliceGroupsMinus1: 1, SliceGroupMapType: 5, SliceGroupChangeDirectionFlag: true}, 5, []uint8{1, 1, 1, 0, 1, 1, 0, 0, 1, 1, 0, 0}},
		{"explicit", PPS{NumSliceGroupsMinus1: 2, SliceGroupMapType: 6, PicSizeInMapUnitsMinus1: 11, SliceGroupID: []uint8{2, 1, 0, 1, 0, 2, 1, 2, 2, 0, 2, 1}}, 0, []uint8{2, 1, 0, 1, 0, 2, 1, 2, 2, 0, 2, 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := fmoTestSPS()
			got, err := tc.pps.MapUnitToSliceGroupMap(s, tc.cycle)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("map=%v err=%v want=%v", got, err, tc.want)
			}
			data, err := tc.pps.MarshalRBSP(s)
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := ParsePPS(data, s)
			if err != nil {
				t.Fatal(err)
			}
			again, err := parsed.MapUnitToSliceGroupMap(s, tc.cycle)
			if err != nil || !reflect.DeepEqual(again, tc.want) {
				t.Fatalf("parsed map=%v err=%v", again, err)
			}
			roundtrip, err := parsed.MarshalRBSP(s)
			if err != nil || !bytes.Equal(data, roundtrip) {
				t.Fatalf("PPS roundtrip=%x err=%v", roundtrip, err)
			}
		})
	}
}

func TestFMODynamicBoundaries(t *testing.T) {
	for typ := uint32(3); typ <= 5; typ++ {
		for _, direction := range []bool{false, true} {
			p := &PPS{NumSliceGroupsMinus1: 1, SliceGroupMapType: typ, SliceGroupChangeDirectionFlag: direction, SliceGroupChangeRateMinus1: 4}
			s := fmoTestSPS()
			width, max, err := p.SliceGroupChangeCycleBits(s)
			if err != nil || width != 2 || max != 3 {
				t.Fatalf("type=%d width=%d max=%d err=%v", typ, width, max, err)
			}
			for _, tc := range []struct {
				cycle uint32
				want  uint8
			}{{0, 1}, {3, 0}} {
				m, err := p.MapUnitToSliceGroupMap(s, tc.cycle)
				if err != nil {
					t.Fatal(err)
				}
				for _, v := range m {
					if v != tc.want {
						t.Fatalf("type=%d dir=%v cycle=%d map=%v", typ, direction, tc.cycle, m)
					}
				}
			}
			if _, err := p.MapUnitToSliceGroupMap(s, 4); !errors.Is(err, h264.ErrInvalidData) {
				t.Fatalf("cycle error=%v", err)
			}
		}
	}
	// 15/2+1=8.5，位宽必须为4，不能先整除后得到3。
	s := fmoTestSPS()
	s.PicWidthInMbsMinus1 = 4
	p := &PPS{NumSliceGroupsMinus1: 1, SliceGroupMapType: 4, SliceGroupChangeRateMinus1: 1}
	width, max, err := p.SliceGroupChangeCycleBits(s)
	if err != nil || width != 4 || max != 8 {
		t.Fatalf("width=%d max=%d err=%v", width, max, err)
	}
	// 最后一轮超出图片的余数被裁剪，不得溢出或越界。
	m, err := p.MapUnitToSliceGroupMap(s, 8)
	if err != nil || !bytes.Equal(m, make([]byte, 15)) {
		t.Fatalf("last cycle map=%v err=%v", m, err)
	}
}

func TestFMOBoxOutDegeneratePictures(t *testing.T) {
	for _, dim := range [][2]uint32{{1, 1}, {1, 7}, {7, 1}, {3, 3}, {4, 4}} {
		for _, direction := range []bool{false, true} {
			s := &SPS{FrameMbsOnlyFlag: true, PicWidthInMbsMinus1: dim[0] - 1, PicHeightInMapUnitsMinus1: dim[1] - 1}
			p := &PPS{NumSliceGroupsMinus1: 1, SliceGroupMapType: 3, SliceGroupChangeDirectionFlag: direction}
			for cycle := range dim[0]*dim[1] + 1 {
				m, err := p.MapUnitToSliceGroupMap(s, cycle)
				if err != nil {
					t.Fatal(err)
				}
				zeros := uint32(0)
				for _, v := range m {
					if v == 0 {
						zeros++
					}
				}
				if zeros != cycle {
					t.Fatalf("dim=%v direction=%v cycle=%d map=%v", dim, direction, cycle, m)
				}
			}
		}
	}
}

func TestFMOMacroblockAddressSpaces(t *testing.T) {
	p := &PPS{NumSliceGroupsMinus1: 2, SliceGroupMapType: 6, PicSizeInMapUnitsMinus1: 5, SliceGroupID: []uint8{0, 1, 2, 2, 0, 1}}
	s := &SPS{PicWidthInMbsMinus1: 2, PicHeightInMapUnitsMinus1: 1}
	cases := []struct {
		name                string
		frame, field, mbaff bool
		want                []uint8
	}{
		{"progressive", true, false, false, []uint8{0, 1, 2, 2, 0, 1}},
		{"field", false, true, false, []uint8{0, 1, 2, 2, 0, 1}},
		{"mbaff-field", false, true, true, []uint8{0, 1, 2, 2, 0, 1}},
		{"interlaced-frame", false, false, false, []uint8{0, 1, 2, 0, 1, 2, 2, 0, 1, 2, 0, 1}},
		{"mbaff-frame", false, false, true, []uint8{0, 0, 1, 1, 2, 2, 2, 2, 0, 0, 1, 1}},
	}
	for _, tc := range cases {
		s.FrameMbsOnlyFlag, s.MbAdaptiveFrameFieldFlag = tc.frame, tc.mbaff
		got, err := p.MbToSliceGroupMap(s, tc.field, 0)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s map=%v err=%v", tc.name, got, err)
		}
	}
	s.FrameMbsOnlyFlag, s.MbAdaptiveFrameFieldFlag = true, false
	if _, err := p.MbToSliceGroupMap(s, true, 0); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("field error=%v", err)
	}
	m := []uint8{0, 1, 0, 2, 1, 2}
	for _, tc := range [][2]uint32{{0, 2}, {1, 4}, {2, 6}, {3, 5}, {4, 6}, {5, 6}} {
		next, err := NextMBAddress(m, tc[0])
		if err != nil || next != tc[1] {
			t.Fatalf("next(%d)=%d err=%v", tc[0], next, err)
		}
	}
	for _, tc := range []struct {
		m       []uint8
		current uint32
	}{{nil, 0}, {m, 6}, {m, math.MaxUint32}} {
		if _, err := NextMBAddress(tc.m, tc.current); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("address error=%v", err)
		}
	}
	if _, err := NextMBAddress(make([]uint8, 262145), 0); !errors.Is(err, h264.ErrResourceLimit) {
		t.Fatalf("map resource error=%v", err)
	}
}

func TestFMOMalformedAndResourceLimits(t *testing.T) {
	s := fmoTestSPS()
	bad := []PPS{
		{NumSliceGroupsMinus1: 8},
		{NumSliceGroupsMinus1: math.MaxUint32},
		{NumSliceGroupsMinus1: 1, SliceGroupMapType: 7},
		{NumSliceGroupsMinus1: 1, RunLengthMinus1: []uint32{0}},
		{NumSliceGroupsMinus1: 1, RunLengthMinus1: []uint32{12, 0}},
		{NumSliceGroupsMinus1: 1, RunLengthMinus1: []uint32{math.MaxUint32, 0}},
		{NumSliceGroupsMinus1: 1, SliceGroupMapType: 2},
		{NumSliceGroupsMinus1: 1, SliceGroupMapType: 2, TopLeft: []uint32{3}, BottomRight: []uint32{4}},
		{NumSliceGroupsMinus1: 1, SliceGroupMapType: 2, TopLeft: []uint32{5}, BottomRight: []uint32{4}},
		{NumSliceGroupsMinus1: 1, SliceGroupMapType: 2, TopLeft: []uint32{0}, BottomRight: []uint32{12}},
		{NumSliceGroupsMinus1: 1, SliceGroupMapType: 6, PicSizeInMapUnitsMinus1: math.MaxUint32},
		{NumSliceGroupsMinus1: 1, SliceGroupMapType: 6, PicSizeInMapUnitsMinus1: 11, SliceGroupID: []uint8{0}},
		{NumSliceGroupsMinus1: 1, SliceGroupMapType: 6, PicSizeInMapUnitsMinus1: 11, SliceGroupID: []uint8{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2}},
	}
	for typ := uint32(3); typ <= 5; typ++ {
		bad = append(bad, PPS{NumSliceGroupsMinus1: 2, SliceGroupMapType: typ}, PPS{NumSliceGroupsMinus1: 1, SliceGroupMapType: typ, SliceGroupChangeRateMinus1: 12}, PPS{NumSliceGroupsMinus1: 1, SliceGroupMapType: typ, SliceGroupChangeRateMinus1: math.MaxUint32})
	}
	for i := range bad {
		if _, err := bad[i].MarshalRBSP(s); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("bad PPS %d write=%v", i, err)
		}
		if _, err := bad[i].MapUnitToSliceGroupMap(s, 0); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("bad PPS %d map=%v", i, err)
		}
	}
	p := &PPS{NumSliceGroupsMinus1: 1, SliceGroupMapType: 1}
	for _, dim := range []SPS{
		{FrameMbsOnlyFlag: true, PicWidthInMbsMinus1: 512},
		{FrameMbsOnlyFlag: true, PicHeightInMapUnitsMinus1: 512},
		{PicHeightInMapUnitsMinus1: 256},
		{FrameMbsOnlyFlag: true, PicWidthInMbsMinus1: math.MaxUint32, PicHeightInMapUnitsMinus1: math.MaxUint32},
	} {
		if _, err := p.MapUnitToSliceGroupMap(&dim, 0); !errors.Is(err, h264.ErrResourceLimit) {
			t.Fatalf("dimensions=%+v err=%v", dim, err)
		}
		if _, err := p.MarshalRBSP(&dim); !errors.Is(err, h264.ErrResourceLimit) {
			t.Fatalf("write dimensions err=%v", err)
		}
	}
	contradictory := &SPS{FrameMbsOnlyFlag: true, MbAdaptiveFrameFieldFlag: true}
	if _, err := p.MapUnitToSliceGroupMap(contradictory, 0); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("contradictory frame flags=%v", err)
	}
	// 精确硬上限必须允许，且每个输出元素为一个 MB。
	for _, dim := range []SPS{{FrameMbsOnlyFlag: true, PicWidthInMbsMinus1: 511, PicHeightInMapUnitsMinus1: 511}, {PicWidthInMbsMinus1: 511, PicHeightInMapUnitsMinus1: 255, MbAdaptiveFrameFieldFlag: true}} {
		m, err := p.MbToSliceGroupMap(&dim, false, 0)
		if err != nil || len(m) != 262144 {
			t.Fatalf("limit len=%d err=%v", len(m), err)
		}
	}
	if _, err := p.MapUnitToSliceGroupMap(nil, 0); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("nil SPS=%v", err)
	}
	if _, err := p.MapUnitToSliceGroupMap(s, 1); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("static cycle=%v", err)
	}
}

// 手写 PPS 前缀，绕过写出校验来独立覆盖解析畸形输入。
func fmoRawPrefix(t *testing.T, groups, typ uint32) *bitstream.Writer {
	t.Helper()
	w := bitstream.NewWriter()
	writeUEOrFail(t, w, 0)
	writeUEOrFail(t, w, 0)
	writeBitOrFail(t, w, 0)
	writeBitOrFail(t, w, 0)
	writeUEOrFail(t, w, groups)
	writeUEOrFail(t, w, typ)
	return w
}

func TestFMOParseMalformed(t *testing.T) {
	cases := []struct {
		name        string
		groups, typ uint32
		tail        []uint32
	}{
		{"groups", 8, 0, nil}, {"groups-overflow", math.MaxUint32, 0, nil}, {"type", 1, 7, nil},
		{"run", 1, 0, []uint32{12, 0}}, {"rectangle", 1, 2, []uint32{3, 4}},
		{"dynamic-three-groups", 2, 3, nil}, {"dynamic-four-groups", 3, 4, nil}, {"dynamic-five-groups", 4, 5, nil},
		{"explicit-overflow", 1, 6, []uint32{math.MaxUint32}}, {"explicit-size", 1, 6, []uint32{10}}, {"explicit-truncated", 1, 6, []uint32{11}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := fmoRawPrefix(t, tc.groups, tc.typ)
			for _, v := range tc.tail {
				writeUEOrFail(t, w, v)
			}
			if _, err := ParsePPS(w.Bytes(), fmoTestSPS()); !errors.Is(err, h264.ErrInvalidData) {
				t.Fatalf("parse=%v", err)
			}
		})
	}
	for typ := uint32(3); typ <= 5; typ++ {
		w := fmoRawPrefix(t, 1, typ)
		writeBitOrFail(t, w, 0)
		writeUEOrFail(t, w, 12)
		if _, err := ParsePPS(w.Bytes(), fmoTestSPS()); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("rate type=%d err=%v", typ, err)
		}
	}
	w := fmoRawPrefix(t, 2, 6)
	writeUEOrFail(t, w, 11)
	for range 12 {
		writeBitsOrFail(t, w, 2, 3)
	}
	if _, err := ParsePPS(w.Bytes(), fmoTestSPS()); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("group ID=%v", err)
	}
	// SPS上下文超限须先于显式表读取/分配返回资源错误；坏表长度由上面的独立用例覆盖。
	w = fmoRawPrefix(t, 1, 6)
	writeUEOrFail(t, w, 11)
	s := fmoTestSPS()
	s.PicWidthInMbsMinus1 = math.MaxUint32
	if _, err := ParsePPS(w.Bytes(), s); !errors.Is(err, h264.ErrResourceLimit) {
		t.Fatalf("dimensions=%v", err)
	}
}
