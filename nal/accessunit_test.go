package nal

import "testing"

func unitOf(typ UnitType, refIDC uint8) Unit {
	return Unit{Header: Header{Type: typ, RefIDC: refIDC}}
}

func sliceInfo() *SliceInfo {
	return &SliceInfo{
		FirstMBInSlice:    5,
		FrameNum:          10,
		PicParameterSetID: 0,
		HasPicOrderCnt:    true,
		PicOrderCntLsb:    20,
		NALRefIDC:         3,
	}
}

func TestDetectBoundary_NALSequenceRules(t *testing.T) {
	slice := unitOf(TypeSlice, 3)
	sps := unitOf(TypeSPS, 3)
	tests := []struct {
		name string
		prev Unit
		curr Unit
		want Boundary
	}{
		{"AUD after slice", slice, unitOf(TypeAUD, 0), BoundaryNew},
		{"AUD after SPS", sps, unitOf(TypeAUD, 0), BoundaryNew},
		{"SPS after slice", slice, unitOf(TypeSPS, 3), BoundaryNew},
		{"PPS after slice", slice, unitOf(TypePPS, 3), BoundaryNew},
		{"SEI after slice", slice, unitOf(TypeSEI, 0), BoundaryNew},
		{"EOS after slice", slice, unitOf(TypeEndOfSequence, 3), BoundarySame},
		{"EOSTREAM after slice", slice, unitOf(TypeEndOfStream, 3), BoundarySame},
		{"filler after slice", slice, unitOf(TypeFiller, 3), BoundarySame},
		{"SPS then slice", sps, slice, BoundarySame},
		{"PPS then slice", unitOf(TypePPS, 3), slice, BoundarySame},
		{"SEI then slice", unitOf(TypeSEI, 0), slice, BoundarySame},
		{"SPS then SPS", sps, unitOf(TypeSPS, 3), BoundarySame},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := DetectBoundary(tc.prev, tc.curr, nil, nil); got != tc.want {
				t.Fatalf("DetectBoundary = %v，期望 %v", got, tc.want)
			}
		})
	}
}

func TestDetectBoundary_SliceFields(t *testing.T) {
	prevUnit := unitOf(TypeSlice, 3)
	currUnit := unitOf(TypeSlice, 3)
	base := sliceInfo()

	tests := []struct {
		name   string
		mutate func(*SliceInfo)
		want   Boundary
	}{
		{"same picture", func(*SliceInfo) {}, BoundarySame},
		{"first mb zero", func(s *SliceInfo) { s.FirstMBInSlice = 0 }, BoundaryNew},
		{"redundant first mb zero", func(s *SliceInfo) {
			s.FirstMBInSlice = 0
			s.RedundantPicCnt = 1
		}, BoundarySame},
		{"frame num differs", func(s *SliceInfo) { s.FrameNum++ }, BoundaryNew},
		{"pps differs", func(s *SliceInfo) { s.PicParameterSetID++ }, BoundaryNew},
		{"field pic differs", func(s *SliceInfo) { s.FieldPicFlag = true }, BoundaryNew},
		{"bottom field differs", func(s *SliceInfo) { s.BottomFieldFlag = true }, BoundaryNew},
		{"idr differs", func(s *SliceInfo) { s.IDR = true }, BoundaryNew},
		{"idr id differs", func(s *SliceInfo) { s.IDRPicID = 3 }, BoundaryNew},
		{"ref idc zero differs", func(s *SliceInfo) { s.NALRefIDC = 0 }, BoundaryNew},
		{"poc differs", func(s *SliceInfo) { s.PicOrderCntLsb++ }, BoundaryNew},
		{"poc presence differs", func(s *SliceInfo) { s.HasPicOrderCnt = false }, BoundaryNew},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			curr := *base
			tc.mutate(&curr)
			if got := DetectBoundary(prevUnit, currUnit, base, &curr); got != tc.want {
				t.Fatalf("DetectBoundary = %v，期望 %v", got, tc.want)
			}
		})
	}
}

func TestDetectBoundary_UnknownWithoutSliceInfo(t *testing.T) {
	prev := unitOf(TypeSlice, 3)
	curr := unitOf(TypeSlice, 3)
	if got := DetectBoundary(prev, curr, nil, nil); got != BoundaryUnknown {
		t.Fatalf("缺少 slice info 时应返回 Unknown，实际 %v", got)
	}
	if got := DetectBoundary(prev, curr, sliceInfo(), nil); got != BoundaryUnknown {
		t.Fatalf("缺少 curr info 时应返回 Unknown，实际 %v", got)
	}
	if got := DetectBoundary(prev, curr, nil, sliceInfo()); got != BoundaryUnknown {
		t.Fatalf("缺少 prev info 时应返回 Unknown，实际 %v", got)
	}
}

func TestDetectBoundary_IDRTransition(t *testing.T) {
	prev := unitOf(TypeSlice, 3)
	curr := unitOf(TypeSliceIDR, 3)
	prevInfo := sliceInfo()
	currInfo := sliceInfo()
	currInfo.IDR = true
	currInfo.FirstMBInSlice = 7 // 即使不是首 slice，IDR 变化也必须开启新 AU
	if got := DetectBoundary(prev, curr, prevInfo, currInfo); got != BoundaryNew {
		t.Fatalf("IDR 切换 = %v，期望 New", got)
	}
}
