package nal

import "testing"

func TestDetectPictureBoundary_IdentityConditions(t *testing.T) {
	base := SliceInfo{FrameNum: 2, HasPicOrderCnt: true, PicOrderCntLsb: 4, NALRefIDC: 3}
	for _, tc := range []struct {
		name   string
		change func(*SliceInfo)
		want   Boundary
	}{
		{"first mb alone", func(s *SliceInfo) { s.FirstMBInSlice = 0 }, BoundarySame},
		{"frame num", func(s *SliceInfo) { s.FrameNum++ }, BoundaryNew},
		{"pps", func(s *SliceInfo) { s.PicParameterSetID++ }, BoundaryNew},
		{"poc lsb", func(s *SliceInfo) { s.PicOrderCntLsb++ }, BoundaryNew},
		{"poc absent", func(s *SliceInfo) { s.HasPicOrderCnt = false }, BoundaryNew},
		{"bottom delta", func(s *SliceInfo) { s.HasDeltaPicOrderCntBottom = true; s.DeltaPicOrderCntBottom = 1 }, BoundaryNew},
		{"type1 delta", func(s *SliceInfo) { s.HasDeltaPicOrderCnt = true; s.DeltaPicOrderCnt = [2]int32{1, -1} }, BoundaryNew},
		{"ref priority", func(s *SliceInfo) { s.NALRefIDC = 1 }, BoundarySame},
		{"nonref", func(s *SliceInfo) { s.NALRefIDC = 0 }, BoundaryNew},
		{"idr", func(s *SliceInfo) { s.IDR = true }, BoundaryNew},
		{"inactive idr id", func(s *SliceInfo) { s.IDRPicID = 9 }, BoundarySame},
		{"inactive bottom flag", func(s *SliceInfo) { s.BottomFieldFlag = true }, BoundarySame},
		{"field", func(s *SliceInfo) { s.FieldPicFlag = true }, BoundaryNew},
	} {
		t.Run(tc.name, func(t *testing.T) {
			curr := base
			tc.change(&curr)
			if got := DetectPictureBoundary(&base, &curr); got != tc.want {
				t.Fatalf("边界=%v，期望%v", got, tc.want)
			}
		})
	}
	for _, base := range []SliceInfo{
		{IDR: true}, {FieldPicFlag: true},
		{HasDeltaPicOrderCntBottom: true}, {HasDeltaPicOrderCnt: true},
	} {
		curr := base
		switch {
		case base.IDR:
			curr.IDRPicID++
		case base.FieldPicFlag:
			curr.BottomFieldFlag = true
		case base.HasDeltaPicOrderCntBottom:
			curr.DeltaPicOrderCntBottom--
		default:
			curr.DeltaPicOrderCnt[1]++
		}
		if DetectPictureBoundary(&base, &curr) != BoundaryNew {
			t.Fatal("活动标识变化未开启新图像", base, curr)
		}
	}
	if DetectPictureBoundary(nil, &base) != BoundaryUnknown {
		t.Fatal("缺失标识未报告Unknown")
	}
}
