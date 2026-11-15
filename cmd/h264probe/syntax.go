package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/bitstream"
	"github.com/cry5tallize/goavc/nal"
	"github.com/cry5tallize/goavc/stream"
	"github.com/cry5tallize/goavc/syntax"
)

// syntaxRecord 每行对应一个 NAL；直接保留共享语法结构中的 VUI、缩放矩阵和头部字段。
// 不输出 slice data，也不因残差未经解码就声称整个码流有效。
type syntaxRecord struct {
	Index       int                     `json:"index"`
	Bytes       int                     `json:"bytes"`
	Header      nal.Header              `json:"header"`
	Type        string                  `json:"type"`
	SPS         *syntax.SPS             `json:"sps,omitempty"`
	PPS         *syntax.PPS             `json:"pps,omitempty"`
	SEI         *syntax.SEI             `json:"sei,omitempty"`
	SliceHeader *syntax.SliceHeader     `json:"slice_header,omitempty"`
	PartitionA  *syntax.DataPartitionA  `json:"partition_a,omitempty"`
	PartitionBC *syntax.DataPartitionBC `json:"partition_bc,omitempty"`
}

type unitReader interface {
	Next() (nal.Unit, error)
}

type streamUnits struct{ stream.NALReader }

func (r streamUnits) Next() (nal.Unit, error) { return r.ReadNAL() }

func newUnitReader(input io.Reader, format string, lengthSize, maxNALSize int) (unitReader, error) {
	switch format {
	case "annexb":
		r, err := stream.NewAnnexBReader(input, maxNALSize)
		if err != nil {
			return nil, err
		}
		return streamUnits{r}, nil
	case "avcc":
		r, err := stream.NewAVCCReader(input, lengthSize, maxNALSize)
		if err != nil {
			return nil, err
		}
		return streamUnits{r}, nil
	default:
		return nil, fmt.Errorf("h264probe: 未知输入格式 %q: %w", format, h264.ErrInvalidData)
	}
}

// limitedUnits 在进入语法解析前同时限制单 NAL 大小和 NAL 数量；不会缓存整条流。
type limitedUnits struct {
	unitReader
	count, limit int
}

func (r *limitedUnits) Next() (nal.Unit, error) {
	u, err := r.unitReader.Next()
	if err != nil {
		return nal.Unit{}, err
	}
	if r.count >= r.limit {
		return nal.Unit{}, fmt.Errorf("h264probe: NAL数量超限: %w", h264.ErrResourceLimit)
	}
	r.count++
	return u, nil
}

type syntaxState struct {
	sps        map[uint32]*syntax.SPS
	pps        map[uint32]*syntax.PPS
	last       *nal.SliceInfo
	partitions map[uint32]*probePartition
}

func newSyntaxState() *syntaxState {
	return &syntaxState{sps: make(map[uint32]*syntax.SPS), pps: make(map[uint32]*syntax.PPS)}
}

type probePartition struct {
	context syntax.DataPartitionContext
	b, c    bool
}

func probeSliceInfo(h *syntax.SliceHeader, ref uint8) *nal.SliceInfo {
	return &nal.SliceInfo{FirstMBInSlice: h.FirstMBInSlice, FrameNum: h.FrameNum,
		PicParameterSetID: h.PicParameterSetID, FieldPicFlag: h.FieldPicFlag,
		BottomFieldFlag: h.BottomFieldFlag, IDR: h.IDR, IDRPicID: h.IDRPicID,
		HasPicOrderCnt: h.HasPicOrderCntLsb, PicOrderCntLsb: h.PicOrderCntLsb,
		HasDeltaPicOrderCntBottom: h.HasDeltaPicOrderCntBottom, DeltaPicOrderCntBottom: h.DeltaPicOrderCntBottom,
		HasDeltaPicOrderCnt: h.HasDeltaPicOrderCnt, DeltaPicOrderCnt: h.DeltaPicOrderCnt,
		RedundantPicCnt: h.RedundantPicCnt, NALRefIDC: ref}
}

func (s *syntaxState) parse(u nal.Unit) (syntaxRecord, error) {
	r := syntaxRecord{Header: u.Header, Type: u.Header.Type.String(), Bytes: 1 + len(u.Payload)}
	if err := u.Header.ValidateSupported(); err != nil {
		return r, err
	}
	payload := u.Payload
	// CABAC 的终端 00 00 03 只能在后续已知 PPS 确认熵编码模式后获准。
	// 其他 NAL 仍严格使用公共 EBSP 校验，不能泛化为删除末尾的 03。
	terminal := u.IsVCL() && len(payload) >= 3 && payload[len(payload)-3] == 0 && payload[len(payload)-2] == 0 && payload[len(payload)-1] == 3
	if terminal {
		payload = payload[:len(payload)-1]
	}
	rbsp, err := bitstream.EBSPToRBSP(payload)
	if err != nil {
		return r, err
	}
	switch u.Header.Type {
	case nal.TypeAUD, nal.TypeEndOfSequence, nal.TypeEndOfStream:
		s.last, s.partitions = nil, nil
	case nal.TypeSlicePartitionB, nal.TypeSlicePartitionC:
		if terminal {
			return r, fmt.Errorf("h264probe: 分区终端转义非法: %w", h264.ErrInvalidData)
		}
		id, readErr := bitstream.NewReader(rbsp).ReadUE()
		if readErr != nil {
			return r, readErr
		}
		entry := s.partitions[id]
		if entry == nil {
			return r, fmt.Errorf("h264probe: 孤立分区B/C: %w", h264.ErrInvalidData)
		}
		isB := u.Header.Type == nal.TypeSlicePartitionB
		if isB && (entry.b || entry.c) || !isB && entry.c {
			return r, fmt.Errorf("h264probe: 重复或逆序分区: %w", h264.ErrInvalidData)
		}
		r.PartitionBC, err = syntax.ParseDataPartitionBC(syntax.DataPartitionInput{RBSP: rbsp, BitLength: uint64(len(rbsp)) * 8, Header: u.Header}, entry.context)
		if err == nil {
			if isB {
				entry.b = true
			} else {
				entry.c = true
			}
		}
	case nal.TypeSPS:
		r.SPS, err = syntax.ParseSPS(rbsp)
		if err == nil {
			err = checkSPSSize(r.SPS)
		}
		if err == nil {
			s.sps[r.SPS.SeqParameterSetID] = r.SPS
		}
	case nal.TypePPS:
		// 只读取引用 ID 来选择上下文；完整语法和 ID 上限仍由共享解析器校验。
		bits := bitstream.NewReader(rbsp)
		if _, err = bits.ReadUE(); err != nil {
			return r, err
		}
		id, readErr := bits.ReadUE()
		if readErr != nil {
			return r, readErr
		}
		if s.sps[id] == nil {
			return r, fmt.Errorf("h264probe: PPS缺少有效SPS: %w", h264.ErrInvalidData)
		}
		r.PPS, err = syntax.ParsePPS(rbsp, s.sps[id])
		if err == nil {
			s.pps[r.PPS.PicParameterSetID] = r.PPS
		}
	case nal.TypeSEI:
		r.SEI, err = syntax.ParseSEI(rbsp)
	case nal.TypeSlice, nal.TypeSliceIDR, nal.TypeSlicePartitionA:
		bits := bitstream.NewReader(rbsp)
		for range 2 {
			if _, err = bits.ReadUE(); err != nil {
				return r, err
			}
		}
		id, readErr := bits.ReadUE()
		if readErr != nil {
			return r, readErr
		}
		p := s.pps[id]
		if p == nil || terminal && !p.EntropyCodingModeFlag {
			return r, fmt.Errorf("h264probe: slice缺少有效PPS或终端转义非法: %w", h264.ErrInvalidData)
		}
		parameter := s.sps[p.SeqParameterSetID]
		if parameter == nil {
			return r, fmt.Errorf("h264probe: slice缺少有效SPS: %w", h264.ErrInvalidData)
		}
		if u.Header.Type == nal.TypeSlicePartitionA {
			r.PartitionA, err = syntax.ParseDataPartitionA(syntax.DataPartitionInput{RBSP: rbsp, BitLength: uint64(len(rbsp)) * 8, Header: u.Header}, parameter, s.pps)
			if err == nil {
				r.SliceHeader = r.PartitionA.SliceHeader
			}
		} else {
			r.SliceHeader, err = syntax.ParseSliceHeaderEx(rbsp, parameter, s.pps, u.IsIDR(), u.Header.RefIDC)
		}
		if err == nil {
			mbCount := parameter.PicWidthInMbs() * parameter.FrameHeightInMbs()
			if parameter.MbAdaptiveFrameFieldFlag || r.SliceHeader.FieldPicFlag {
				mbCount /= 2
			}
			if uint64(r.SliceHeader.FirstMBInSlice) >= mbCount || u.IsIDR() && u.Header.RefIDC == 0 {
				err = fmt.Errorf("h264probe: slice地址或IDR参考标志无效: %w", h264.ErrInvalidData)
			}
			if err == nil {
				info := probeSliceInfo(r.SliceHeader, u.Header.RefIDC)
				if s.last == nil || nal.DetectPictureBoundary(s.last, info) == nal.BoundaryNew {
					s.partitions = nil
				}
				if r.PartitionA != nil {
					if s.partitions == nil {
						s.partitions = make(map[uint32]*probePartition)
					}
					id := r.PartitionA.Context.SliceID
					if old := s.partitions[id]; old != nil && old.context.RedundantPicCnt == r.SliceHeader.RedundantPicCnt {
						return r, fmt.Errorf("h264probe: 重复分区A: %w", h264.ErrInvalidData)
					}
					s.partitions[id] = &probePartition{context: r.PartitionA.Context}
				}
				s.last = info
			}
		}
	}
	return r, err
}

func checkSPSSize(s *syntax.SPS) error {
	w, err := s.DisplayWidth()
	if err != nil {
		return err
	}
	h, err := s.DisplayHeight()
	if err != nil {
		return err
	}
	if w > h264.MaxFrameWidth || h > h264.MaxFrameHeight || s.PicWidthInMbs() > h264.MaxFrameWidth/16 || s.FrameHeightInMbs() > h264.MaxFrameHeight/16 {
		return fmt.Errorf("h264probe: 显示尺寸无效或超限: %w", h264.ErrResourceLimit)
	}
	return nil
}

// probeSyntax 输出 JSON Lines；每次仅保留一个有界 NAL 与至多 32/256 个参数集。
// 后续错误会返回非零状态；此前已经输出的行仍然是有效的局部语法记录。
func probeSyntax(input unitReader, out io.Writer) error {
	state := newSyntaxState()
	enc := json.NewEncoder(out)
	for index := 0; ; index++ {
		u, err := input.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("h264probe: NAL %d: %w", index, err)
		}
		record, err := state.parse(u)
		if err != nil {
			return fmt.Errorf("h264probe: NAL %d (%s): %w", index, u.Header.Type, err)
		}
		record.Index = index
		if err := enc.Encode(record); err != nil {
			return err
		}
	}
}
