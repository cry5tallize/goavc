package decoder

import (
	"fmt"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/internal/motion"
	"github.com/goavc/h264/internal/picture"
	"github.com/goavc/h264/nal"
	"github.com/goavc/h264/syntax"
)

// sliceEntry 是解析后的一个 slice：header 与定位好的 slice data reader。
type sliceEntry struct {
	unit  nal.Unit
	hdr   *syntax.SliceHeader
	sps   *syntax.SPS
	pps   *syntax.PPS
	rbsp  []byte
	data  *bitstream.Reader
	cabac *cabacSyntax
}

// mbInfo 是去块滤波与后续 MB 预测需要的宏块元数据。
type mbInfo struct {
	intra      bool
	intra16    bool
	pcm        bool
	skip       bool
	qp         int
	cbp        int
	sliceID    int
	deblockIDC uint8
	alphaDiv2  int8
	betaDiv2   int8
	// intraModes 是 16 个 4x4 块的帧内模式（光栅顺序）；
	// 非 Intra4x4 宏块保持 -1，供 MPM 判定“邻块无模式”。
	intraModes [16]int8
	chromaMode int8
}

// frameState 是一帧解码期间的工作状态。
type frameState struct {
	d   *Decoder
	pic *picture.Picture
	sps *syntax.SPS
	pps *syntax.PPS

	mbW, mbH, total int
	decoded         []bool
	info            []mbInfo
	lumaNZ          []uint8
	chromaNZ        [2][]uint8
	dcCoded         [3][]bool
	mvd             []motion.MV
	mv              *motion.Grid
}

func newFrameState(d *Decoder, sps *syntax.SPS, pps *syntax.PPS) (*frameState, error) {
	mbW := int(sps.PicWidthInMbs())
	mbH := int(sps.FrameHeightInMbs())
	pic, err := d.pool.Get(mbW, mbH)
	if err != nil {
		return nil, err
	}
	total := mbW * mbH
	fs := &frameState{
		d:       d,
		pic:     pic,
		sps:     sps,
		pps:     pps,
		mbW:     mbW,
		mbH:     mbH,
		total:   total,
		decoded: make([]bool, total),
		info:    make([]mbInfo, total),
		lumaNZ:  make([]uint8, total*16),
		mv:      motion.NewGrid(mbW, mbH),
	}
	fs.chromaNZ[0] = make([]uint8, total*4)
	fs.chromaNZ[1] = make([]uint8, total*4)
	if pps.EntropyCodingModeFlag {
		for i := range fs.dcCoded {
			fs.dcCoded[i] = make([]bool, total)
		}
		fs.mvd = make([]motion.MV, total*16)
	}
	for i := range fs.info {
		fs.info[i].intraModes = [16]int8{-1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1}
		fs.info[i].chromaMode = -1
	}
	fs.mv.Reset()
	return fs, nil
}

// release 把图像归还缓冲池；仅用于解码失败路径。
func (fs *frameState) release() {
	if fs != nil && fs.pic != nil {
		fs.pic.Release()
		fs.pic = nil
	}
}

// parseSlice 为单个 NAL 解析参数集引用和完整头部，不分配图像缓冲。
func (d *Decoder) parseSlice(u nal.Unit) (sliceEntry, error) {
	if u.Header.IsIDR() && u.Header.RefIDC == 0 {
		return sliceEntry{}, fmt.Errorf("decoder: non-reference IDR: %w", h264.ErrInvalidData)
	}
	rbsp, err := bitstream.EBSPToRBSP(u.Payload)
	if err != nil {
		return sliceEntry{}, err
	}
	id, err := peekSlicePPSID(rbsp)
	if err != nil {
		return sliceEntry{}, err
	}
	pps := d.pps[id]
	if pps == nil {
		return sliceEntry{}, fmt.Errorf("decoder: unknown pps %d: %w", id, h264.ErrInvalidData)
	}
	sps := d.sps[pps.SeqParameterSetID]
	if sps == nil {
		return sliceEntry{}, fmt.Errorf("decoder: unknown sps: %w", h264.ErrInvalidData)
	}
	hdr, err := syntax.ParseSliceHeaderEx(rbsp, sps, d.pps, u.Header.IsIDR(), u.Header.RefIDC)
	if err != nil {
		return sliceEntry{}, err
	}
	if err := d.checkSupported(sps, pps, hdr); err != nil {
		return sliceEntry{}, err
	}
	r := bitstream.NewReader(rbsp)
	if err := r.SkipBits(uint(hdr.HeaderBits)); err != nil {
		return sliceEntry{}, err
	}
	return sliceEntry{unit: u, hdr: hdr, sps: sps, pps: pps, rbsp: rbsp, data: r}, nil
}

func samePicture(a, b sliceEntry) bool {
	if a.hdr.IDR != b.hdr.IDR || a.hdr.FrameNum != b.hdr.FrameNum ||
		a.hdr.PicParameterSetID != b.hdr.PicParameterSetID ||
		(a.unit.Header.RefIDC == 0) != (b.unit.Header.RefIDC == 0) ||
		(a.hdr.IDR && a.hdr.IDRPicID != b.hdr.IDRPicID) ||
		a.hdr.DeltaPicOrderCntBottom != b.hdr.DeltaPicOrderCntBottom {
		return false
	}
	if a.hdr.HasPicOrderCntLsb != b.hdr.HasPicOrderCntLsb ||
		a.hdr.PicOrderCntLsb != b.hdr.PicOrderCntLsb {
		return false
	}
	return true
}

// decodeFrame 解码一个访问单元并输出一帧。
func (d *Decoder) decodeFrame(slices []sliceEntry, p h264.Packet) error {
	if len(slices) == 0 {
		return nil
	}
	first := slices[0]
	sps, pps := first.sps, first.pps
	if err := d.checkSupported(sps, pps, first.hdr); err != nil {
		return err
	}
	if err := d.activateSPS(sps); err != nil {
		return err
	}
	idr := first.hdr.IDR
	ref := first.unit.Header.RefIDC != 0
	if idr {
		if first.hdr.NoOutputOfPriorPicsFlag {
			for _, f := range d.out {
				f.Release()
			}
			d.out = nil
		}
		d.dpb.Reset()
		d.poc.Reset()
		d.frameNumOffset = 0
		d.hasOutput = false
	}
	if !idr && sps.GapsInFrameNumValueAllowedFlag && first.hdr.FrameNum != (d.dpb.ReferenceFrameNum()+1)%d.maxFrameNum {
		return fmt.Errorf("decoder: non-existing reference pictures are not supported: %w", h264.ErrUnsupported)
	}
	// 非参考帧不推进参考 frame_num；下一参考帧可以与其同号。
	tracker := d.dpb
	if !ref {
		copy := *d.dpb
		tracker = &copy
	}
	if err := tracker.BeginFrame(idr, first.hdr.FrameNum); err != nil {
		return err
	}
	if !idr && d.dpb.Reference() == nil {
		return fmt.Errorf("decoder: non-IDR picture without reference: %w", h264.ErrInvalidData)
	}

	poc := int32(0)
	if sps.PicOrderCntType == 0 {
		previous := *d.poc
		poc = d.poc.Compute(idr, int32(first.hdr.PicOrderCntLsb))
		if !ref {
			*d.poc = previous
		}
		if first.hdr.DeltaPicOrderCntBottom < 0 {
			poc += first.hdr.DeltaPicOrderCntBottom
		}
	} else if sps.PicOrderCntType == 2 {
		// Spec: 8.2.1.3。x264 的无 B 帧 Baseline 通常使用 type 2。
		offset := d.frameNumOffset
		if !idr && first.hdr.FrameNum < d.dpb.ReferenceFrameNum() {
			offset += int64(d.maxFrameNum)
		}
		value := 2 * (offset + int64(first.hdr.FrameNum))
		if !ref {
			value--
		}
		if value > 1<<31-1 {
			return fmt.Errorf("decoder: POC overflow: %w", h264.ErrResourceLimit)
		}
		poc = int32(value)
		if ref {
			d.frameNumOffset = offset
		}
	} else {
		return fmt.Errorf("decoder: poc type %d is not supported in M2: %w",
			sps.PicOrderCntType, h264.ErrUnsupported)
	}
	if d.hasOutput && poc <= d.lastOutputPOC {
		return fmt.Errorf("decoder: display reordering requires M3: %w", h264.ErrUnsupported)
	}

	fs, err := newFrameState(d, sps, pps)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			fs.release()
		}
	}()

	if len(slices) > d.cfg.MaxSliceCount {
		return fmt.Errorf("decoder: slice count %d exceeds limit %d: %w",
			len(slices), d.cfg.MaxSliceCount, h264.ErrResourceLimit)
	}

	// 校验 slice 覆盖范围：first_mb 必须递增且首 slice 从 0 开始。
	lastEnd := 0
	for si := range slices {
		s := &slices[si]
		if s.hdr.FirstMBInSlice >= uint32(fs.total) {
			return fmt.Errorf("decoder: first_mb_in_slice %d exceeds picture: %w",
				s.hdr.FirstMBInSlice, h264.ErrInvalidData)
		}
		if int(s.hdr.FirstMBInSlice) < lastEnd {
			return fmt.Errorf("decoder: overlapping slices at first_mb %d: %w",
				s.hdr.FirstMBInSlice, h264.ErrInvalidData)
		}
		if si == 0 && s.hdr.FirstMBInSlice != 0 {
			return fmt.Errorf("decoder: first slice starts at mb %d: %w",
				s.hdr.FirstMBInSlice, h264.ErrInvalidData)
		}
		end := fs.total
		if si+1 < len(slices) {
			end = int(slices[si+1].hdr.FirstMBInSlice)
		}
		if end <= int(s.hdr.FirstMBInSlice) || end > fs.total {
			return fmt.Errorf("decoder: invalid slice interval: %w", h264.ErrInvalidData)
		}
		if err := decodeSlice(fs, s, si, int(s.hdr.FirstMBInSlice), end); err != nil {
			return err
		}
		lastEnd = end
	}
	for i, okMB := range fs.decoded {
		if !okMB {
			return fmt.Errorf("decoder: macroblock %d was not covered by any slice: %w",
				i, h264.ErrInvalidData)
		}
	}

	fs.mv.BeginSlice(0)
	if err := fs.deblock(); err != nil {
		return err
	}
	fs.pic.FillBorders()

	frame, err := d.toFrame(fs.pic, sps, p, idr)
	if err != nil {
		return err
	}
	d.out = append(d.out, frame)
	d.lastOutputPOC, d.hasOutput = poc, true
	if ref {
		d.dpb.PutReference(fs.pic, first.hdr.FrameNum, poc)
	} else {
		fs.release()
	}
	d.waitingForIDR = false
	ok = true
	return nil
}

// checkSupported 在进入宏块解码前拒绝 M2 范围外的工具。
func (d *Decoder) checkSupported(sps *syntax.SPS, pps *syntax.PPS, hdr *syntax.SliceHeader) error {
	if hdr.SliceQP < 0 || hdr.SliceQP > 51 {
		return fmt.Errorf("decoder: slice QP %d: %w", hdr.SliceQP, h264.ErrInvalidData)
	}
	if hdr.LongTermReferenceFlag || hdr.RedundantPicCnt > 0 || sps.MaxNumRefFrames > 1 {
		return fmt.Errorf("decoder: long term, redundant or multiple references: %w", h264.ErrUnsupported)
	}
	if pps.Transform8x8ModeFlag {
		return fmt.Errorf("decoder: 8x8 transform is not supported in M2: %w", h264.ErrUnsupported)
	}
	if sps.SeqScalingMatrixPresentFlag || pps.PicScalingMatrixPresentFlag {
		return fmt.Errorf("decoder: scaling matrices are not supported in M2: %w", h264.ErrUnsupported)
	}
	if hdr.Type == syntax.SliceTypeB {
		return fmt.Errorf("decoder: B slices are not supported in M2: %w", h264.ErrUnsupported)
	}
	if hdr.HasPredWeightTable {
		return fmt.Errorf("decoder: weighted prediction is not supported in M2: %w", h264.ErrUnsupported)
	}
	if hdr.AdaptiveRefPicMarking || hdr.MMCOCount > 0 {
		return fmt.Errorf("decoder: MMCO is not supported in M2: %w", h264.ErrUnsupported)
	}
	if hdr.RefPicListReorderingL0 || hdr.RefPicListReorderingL1 {
		return fmt.Errorf("decoder: reference list reordering is not supported in M2: %w", h264.ErrUnsupported)
	}
	if hdr.NumRefIdxL0ActiveMinus1 > 0 {
		return fmt.Errorf("decoder: multiple reference frames are not supported in M2: %w", h264.ErrUnsupported)
	}
	if sps.PicOrderCntType == 1 {
		return fmt.Errorf("decoder: poc type 1 is not supported in M2: %w", h264.ErrUnsupported)
	}
	return nil
}

// decodeSlice 解码一个 slice 覆盖的宏块区间 [first, end)。
//
// qscale 在 slice 内跨宏块保持，mb_qp_delta 逐 MB 累积；P_Skip 沿用
// 当前值而不是重置为 slice QP。
func decodeSlice(fs *frameState, s *sliceEntry, sliceID int, first, end int) error {
	if s.pps.EntropyCodingModeFlag {
		return decodeCABACSlice(fs, s, sliceID, first, end)
	}
	fs.mv.BeginSlice(first)
	qscale := s.hdr.SliceQP
	for addr := first; addr < end; addr++ {
		if fs.decoded[addr] {
			return fmt.Errorf("decoder: macroblock %d decoded twice: %w", addr, h264.ErrInvalidData)
		}
		if s.hdr.Type == syntax.SliceTypeP {
			run, err := s.data.ReadUE()
			if err != nil {
				return fmt.Errorf("decoder: mb_skip_run at mb %d: %w", addr, err)
			}
			if run > uint32(end-addr) {
				return fmt.Errorf("decoder: mb_skip_run %d overruns slice: %w", run, h264.ErrInvalidData)
			}
			for i := uint32(0); i < run; i++ {
				if err := decodeSkipMacroblock(fs, addr+int(i), s, qscale, sliceID); err != nil {
					return err
				}
				fs.decoded[addr+int(i)] = true
			}
			addr += int(run)
			if addr >= end {
				break
			}
		}
		if err := decodeMacroblock(fs, addr, s, sliceID, &qscale); err != nil {
			return fmt.Errorf("decoder: mb %d: %w", addr, err)
		}
		fs.decoded[addr] = true
	}
	// CAVLC slice_data 后必须恰好是 rbsp_trailing_bits。
	remaining := s.data.BitsRemaining()
	if remaining == 0 || remaining > 8 {
		return fmt.Errorf("decoder: slice trailing length %d: %w", remaining, h264.ErrInvalidData)
	}
	bits, err := s.data.ReadBits(uint(remaining))
	if err != nil {
		return err
	}
	if bits != uint64(1)<<(remaining-1) {
		return fmt.Errorf("decoder: invalid slice trailing bits: %w", h264.ErrInvalidData)
	}
	return nil
}

// peekSlicePPSID 读取 slice header 前三个 ue(v) 中的 pic_parameter_set_id。
func peekSlicePPSID(rbsp []byte) (uint32, error) {
	r := bitstream.NewReader(rbsp)
	if _, err := r.ReadUE(); err != nil {
		return 0, err
	}
	if _, err := r.ReadUE(); err != nil {
		return 0, err
	}
	return r.ReadUE()
}

// toFrame 把内部图像按 SPS 裁剪窗口拷贝成公共 Frame。
func (d *Decoder) toFrame(pic *picture.Picture, sps *syntax.SPS, p h264.Packet, key bool) (*h264.Frame, error) {
	dw, err := sps.DisplayWidth()
	if err != nil {
		return nil, err
	}
	dh, err := sps.DisplayHeight()
	if err != nil {
		return nil, err
	}
	w, h := int(dw), int(dh)
	if w <= 0 || h <= 0 || w > pic.Width || h > pic.Height {
		return nil, fmt.Errorf("decoder: invalid display size %dx%d: %w", w, h, h264.ErrInvalidData)
	}
	f := &h264.Frame{
		Width:    w,
		Height:   h,
		Format:   h264.YUV420P,
		PTS:      p.PTS,
		Duration: p.Duration,
		KeyFrame: key,
	}
	for pi := 0; pi < 3; pi++ {
		var pw, ph int
		if pi == 0 {
			pw, ph = w, h
		} else {
			pw, ph = (w+1)/2, (h+1)/2
		}
		rowBytes := pw
		buf := make([]byte, rowBytes*ph)
		src := &pic.Planes[pi]
		cropX, cropY := int(sps.FrameCropLeftOffset), int(sps.FrameCropTopOffset)
		if pi == 0 {
			cropX *= 2
			cropY *= 2
		}
		for y := 0; y < ph; y++ {
			copy(buf[y*rowBytes:(y+1)*rowBytes], src.Row(y + cropY)[cropX:cropX+pw])
		}
		f.Planes[pi] = h264.Plane{Data: buf, Stride: rowBytes}
	}
	return f, nil
}
