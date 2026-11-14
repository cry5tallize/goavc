package nal

// DetectPictureBoundary按Spec: 7.4.1.2.4比较两个VCL的图像标识。
// 与DetectBoundary的first_mb辅助规则不同，首宏块地址本身不证明新图像，
// 因而多slice乱序或同标识的first_mb=0不会被强行拆开。
// 调用方负责AUD/非VCL归属，并将最近VCL摘要跨filler保留。
func DetectPictureBoundary(prev, curr *SliceInfo) Boundary {
	if prev == nil || curr == nil {
		return BoundaryUnknown
	}
	if prev.FrameNum != curr.FrameNum ||
		prev.PicParameterSetID != curr.PicParameterSetID ||
		prev.FieldPicFlag != curr.FieldPicFlag ||
		(prev.FieldPicFlag && curr.FieldPicFlag && prev.BottomFieldFlag != curr.BottomFieldFlag) ||
		(prev.NALRefIDC == 0) != (curr.NALRefIDC == 0) ||
		prev.IDR != curr.IDR ||
		(prev.IDR && curr.IDR && prev.IDRPicID != curr.IDRPicID) ||
		prev.HasPicOrderCnt != curr.HasPicOrderCnt ||
		(prev.HasPicOrderCnt && prev.PicOrderCntLsb != curr.PicOrderCntLsb) ||
		prev.HasDeltaPicOrderCntBottom != curr.HasDeltaPicOrderCntBottom ||
		(prev.HasDeltaPicOrderCntBottom && prev.DeltaPicOrderCntBottom != curr.DeltaPicOrderCntBottom) ||
		prev.HasDeltaPicOrderCnt != curr.HasDeltaPicOrderCnt ||
		(prev.HasDeltaPicOrderCnt && prev.DeltaPicOrderCnt != curr.DeltaPicOrderCnt) {
		return BoundaryNew
	}
	return BoundarySame
}
