package nal

// SliceInfo 是 Access Unit 边界判定所需的 slice header 摘要。
//
// 它刻意不依赖 syntax 包：nal 层只消费这些字段，syntax 解析器在 M2 之后
// 负责从 slice header 中填充。字段缺失时用 Has* 标志表达"未知"，
// 避免把合法的零值误判为"未提供"。
type SliceInfo struct {
	FirstMBInSlice    uint32
	FrameNum          uint32
	PicParameterSetID uint32

	FieldPicFlag    bool
	BottomFieldFlag bool

	IDR      bool
	IDRPicID uint32

	// HasPicOrderCnt 表示 PicOrderCntLsb 有效（pic_order_cnt_type == 0）。
	HasPicOrderCnt bool
	PicOrderCntLsb uint32

	// RedundantPicCnt 大于 0 的 slice 属于当前 AU 的冗余编码，不开启新 AU。
	RedundantPicCnt uint32

	NALRefIDC uint8
}

// Boundary 是相邻两个 NAL unit 之间的 Access Unit 边界判定结果。
type Boundary int

const (
	// BoundaryUnknown 表示缺少 slice 信息，无法仅凭 NAL 序列判断。
	// 调用方应把它当作"同一 AU"保守处理，或提供更完整的 SliceInfo。
	BoundaryUnknown Boundary = iota

	// BoundarySame 表示当前 NAL 属于当前 Access Unit。
	BoundarySame

	// BoundaryNew 表示当前 NAL 开始一个新的 Access Unit。
	BoundaryNew
)

// DetectBoundary 根据 H.264 §7.4.1.2.3 判断 curr 是否开始一个新 AU。
//
// 判定顺序：
//  1. AUD 永远开始新 AU；
//  2. 非 VCL 跟在 VCL 之后：end of sequence/stream、filler 属于当前 AU，
//     其余（SPS/PPS/SEI 等）视为下一个 AU 的前缀；
//  3. VCL 跟在非 VCL 之后：属于当前 AU（前缀 + 首个 slice）；
//  4. VCL 跟在 VCL 之后：使用 SliceInfo 比较 first_mb_in_slice、frame_num、
//     PPS id、field flags、IDR id、POC、nal_ref_idc 是否为零等字段。
//
// 缺少任意一侧的 SliceInfo 时返回 BoundaryUnknown。M1 的 assembler 会把
// Unknown 当作 Same；调用方需要精确边界时应提供 hint（例如解析 slice header
// 后填充 SliceInfo）。
func DetectBoundary(prev, curr Unit, prevInfo, currInfo *SliceInfo) Boundary {
	if curr.Header.Type == TypeAUD {
		return BoundaryNew
	}
	currVCL := curr.Header.IsVCL()
	prevVCL := prev.Header.IsVCL()

	if !currVCL {
		if !prevVCL {
			return BoundarySame
		}
		switch curr.Header.Type {
		case TypeEndOfSequence, TypeEndOfStream, TypeFiller:
			return BoundarySame
		default:
			// SPS/PPS/SEI 等非 VCL 出现在 VCL 之后，按规范属于下一个 AU 的前缀。
			return BoundaryNew
		}
	}
	if !prevVCL {
		// 非 VCL 前缀（SPS/PPS/SEI/AUD 之后）的第一个 VCL 属于同一个 AU。
		return BoundarySame
	}

	if prevInfo == nil || currInfo == nil {
		return BoundaryUnknown
	}

	// 冗余 slice 即使 first_mb_in_slice == 0 也属于当前 AU。
	if currInfo.RedundantPicCnt > 0 {
		return BoundarySame
	}
	// 新 picture 的第一个 slice 必定 first_mb_in_slice == 0。
	if currInfo.FirstMBInSlice == 0 {
		return BoundaryNew
	}

	if currInfo.FrameNum != prevInfo.FrameNum ||
		currInfo.PicParameterSetID != prevInfo.PicParameterSetID ||
		currInfo.FieldPicFlag != prevInfo.FieldPicFlag ||
		currInfo.BottomFieldFlag != prevInfo.BottomFieldFlag ||
		currInfo.IDR != prevInfo.IDR ||
		currInfo.IDRPicID != prevInfo.IDRPicID ||
		(currInfo.NALRefIDC == 0) != (prevInfo.NALRefIDC == 0) {
		return BoundaryNew
	}
	if currInfo.HasPicOrderCnt != prevInfo.HasPicOrderCnt {
		return BoundaryNew
	}
	if currInfo.HasPicOrderCnt && currInfo.PicOrderCntLsb != prevInfo.PicOrderCntLsb {
		return BoundaryNew
	}
	return BoundarySame
}
