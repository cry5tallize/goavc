package stream

import (
	"fmt"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/nal"
	"github.com/goavc/h264/syntax"
)

// inspect只转换需要解析的EBSP；SEI/filler等载荷归语法/解码层验证。
func (r *AUReader) inspect(u nal.Unit) (*nal.SliceInfo, error) {
	if u.Header.ForbiddenZeroBit || u.Header.RefIDC > 3 || !u.Header.Type.Valid() ||
		(u.IsIDR() && u.Header.RefIDC == 0) {
		return nil, fmt.Errorf("NAL header无效: %w", h264.ErrInvalidData)
	}
	if err := u.Header.ValidateSupported(); err != nil {
		return nil, err
	}
	if u.Header.Type != nal.TypeSPS && u.Header.Type != nal.TypePPS && !u.IsVCL() {
		return nil, nil
	}
	payload := u.Payload
	// CABAC zero-word末端可含终端防竞争字节；只有解析PPS后才能接受。
	terminal := u.IsVCL() && len(payload) >= 3 && payload[len(payload)-3] == 0 && payload[len(payload)-2] == 0 && payload[len(payload)-1] == 3
	if terminal {
		payload = payload[:len(payload)-1]
	}
	rbsp, err := bitstream.EBSPToRBSP(payload)
	if err != nil {
		return nil, err
	}
	switch u.Header.Type {
	case nal.TypeSPS:
		s, err := syntax.ParseSPS(rbsp)
		if err != nil {
			return nil, err
		}
		if !s.FrameMbsOnlyFlag && !s.MbAdaptiveFrameFieldFlag {
			return nil, fmt.Errorf("PAFF场图像访问单元尚未支持: %w", h264.ErrUnsupported)
		}
		if s.PicWidthInMbs() > h264.MaxFrameWidth/16 || s.FrameHeightInMbs() > h264.MaxFrameHeight/16 {
			return nil, fmt.Errorf("编码尺寸超限: %w", h264.ErrResourceLimit)
		}
		if _, err := s.DisplayWidth(); err != nil {
			return nil, err
		}
		if _, err := s.DisplayHeight(); err != nil {
			return nil, err
		}
		r.sps[s.SeqParameterSetID] = s
		return nil, nil
	case nal.TypePPS:
		bits := bitstream.NewReader(rbsp)
		if _, err := bits.ReadUE(); err != nil {
			return nil, err
		}
		id, err := bits.ReadUE()
		if err != nil {
			return nil, err
		}
		if r.sps[id] == nil {
			return nil, fmt.Errorf("PPS引用未知SPS: %w", h264.ErrInvalidData)
		}
		p, err := syntax.ParsePPS(rbsp, r.sps[id])
		if err != nil {
			return nil, err
		}
		r.pps[p.PicParameterSetID] = p
		return nil, nil
	}
	bits := bitstream.NewReader(rbsp)
	for range 2 {
		if _, err := bits.ReadUE(); err != nil {
			return nil, err
		}
	}
	id, err := bits.ReadUE()
	if err != nil {
		return nil, err
	}
	p := r.pps[id]
	if p == nil || terminal && !p.EntropyCodingModeFlag {
		return nil, fmt.Errorf("未知PPS或CAVLC终端转义: %w", h264.ErrInvalidData)
	}
	s := r.sps[p.SeqParameterSetID]
	h, err := syntax.ParseSliceHeaderEx(rbsp, s, r.pps, u.IsIDR(), u.Header.RefIDC)
	if err != nil {
		return nil, err
	}
	if h.FieldPicFlag {
		return nil, fmt.Errorf("PAFF场图像访问单元尚未支持: %w", h264.ErrUnsupported)
	}
	if h.RedundantPicCnt != 0 {
		return nil, fmt.Errorf("冗余slice尚未支持: %w", h264.ErrUnsupported)
	}
	mbCount := s.PicWidthInMbs() * s.FrameHeightInMbs()
	if s.MbAdaptiveFrameFieldFlag {
		mbCount /= 2
	}
	if uint64(h.FirstMBInSlice) >= mbCount {
		return nil, fmt.Errorf("首宏块地址超出图像: %w", h264.ErrInvalidData)
	}
	return &nal.SliceInfo{FirstMBInSlice: h.FirstMBInSlice, FrameNum: h.FrameNum,
		PicParameterSetID: h.PicParameterSetID, FieldPicFlag: h.FieldPicFlag,
		BottomFieldFlag: h.BottomFieldFlag, IDR: h.IDR, IDRPicID: h.IDRPicID,
		HasPicOrderCnt: h.HasPicOrderCntLsb, PicOrderCntLsb: h.PicOrderCntLsb,
		HasDeltaPicOrderCntBottom: h.HasDeltaPicOrderCntBottom, DeltaPicOrderCntBottom: h.DeltaPicOrderCntBottom,
		HasDeltaPicOrderCnt: h.HasDeltaPicOrderCnt, DeltaPicOrderCnt: h.DeltaPicOrderCnt,
		RedundantPicCnt: h.RedundantPicCnt, NALRefIDC: u.Header.RefIDC}, nil
}
