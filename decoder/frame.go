package decoder

import (
	"encoding/binary"
	"fmt"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/internal/dpb"
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
	refs  [2][]*dpb.Reference
}

// mbInfo 是去块滤波与后续 MB 预测需要的宏块元数据。
type mbInfo struct {
	intra      bool
	intra16    bool
	pcm        bool
	skip       bool
	direct     bool
	transform8 bool
	allow8     bool
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
	d       *Decoder
	pic     *picture.Picture
	sps     *syntax.SPS
	pps     *syntax.PPS
	poc     int32
	scaling syntax.ScalingMatrices

	mbW, mbH, total  int
	subW, subH       int
	chromaW, chromaH int
	chromaBlocks     int
	decoded          []bool
	info             []mbInfo
	lumaNZ           []uint8
	chromaNZ         [2][]uint8
	dcCoded          [3][]bool
	mvd              []motion.MV
	referenceIDs     [2][]*dpb.Identity
	mv               *motion.Grid
	mv1              *motion.Grid
	mvd1             []motion.MV
	directBlocks     []bool
}

func newFrameState(d *Decoder, sps *syntax.SPS, pps *syntax.PPS, bFrame bool) (*frameState, error) {
	scaling, err := syntax.SelectScalingMatrices(sps, pps)
	if err != nil {
		return nil, err
	}
	mbW := int(sps.PicWidthInMbs())
	mbH := int(sps.FrameHeightInMbs())
	format := h264.YUV420P
	if sps.BitDepthLumaMinus8 == 2 {
		format = h264.YUV420P10LE
	}
	if sps.ChromaFormatIDC == 2 {
		format = h264.YUV422P
		if sps.BitDepthLumaMinus8 == 2 {
			format = h264.YUV422P10LE
		}
	}
	subW, subH := format.ChromaSubsampling()
	chromaW, chromaH := format.PlaneDimensions(1, 16, 16)
	chromaBlocks := chromaW * chromaH / 16
	pic, err := d.pool.Get(mbW, mbH, format)
	if err != nil {
		return nil, err
	}
	total := mbW * mbH
	fs := &frameState{
		scaling: scaling,
		d:       d,
		pic:     pic,
		sps:     sps,
		pps:     pps,
		mbW:     mbW,
		mbH:     mbH,
		total:   total,
		subW:    subW, subH: subH,
		chromaW: chromaW, chromaH: chromaH, chromaBlocks: chromaBlocks,
		decoded: make([]bool, total),
		info:    make([]mbInfo, total),
		lumaNZ:  make([]uint8, total*16),
		mv:      motion.NewGrid(mbW, mbH),
	}
	fs.chromaNZ[0] = make([]uint8, total*chromaBlocks)
	fs.chromaNZ[1] = make([]uint8, total*chromaBlocks)
	fs.referenceIDs[0] = make([]*dpb.Identity, total*16)
	if bFrame {
		fs.mv1 = motion.NewGrid(mbW, mbH)
		fs.referenceIDs[1] = make([]*dpb.Identity, total*16)
		fs.directBlocks = make([]bool, total*16)
		if pps.EntropyCodingModeFlag {
			fs.mvd1 = make([]motion.MV, total*16)
		}
	}
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
	payload := u.Payload
	// Spec: 7.4.1 / 9.3.4.6。封装后的 cabac_zero_word 为 00 00 03，
	// 末尾 03 是合法的防竞争字节；仅在确认 PPS 为 CABAC 后接受此形式。
	terminalWord := len(payload) >= 3 && payload[len(payload)-3] == 0 && payload[len(payload)-2] == 0 && payload[len(payload)-1] == 3
	if terminalWord {
		payload = payload[:len(payload)-1]
	}
	rbsp, err := bitstream.EBSPToRBSP(payload)
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
	if terminalWord && !pps.EntropyCodingModeFlag {
		return sliceEntry{}, fmt.Errorf("decoder: terminal CABAC escape in CAVLC slice: %w", h264.ErrInvalidData)
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
	for i := 1; i < len(slices); i++ {
		if slices[i].hdr.FirstMBInSlice < slices[i-1].hdr.FirstMBInSlice {
			return fmt.Errorf("decoder: arbitrary slice order: %w", h264.ErrUnsupported)
		}
	}
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
		d.outputBoundary(first.hdr.NoOutputOfPriorPicsFlag)
		d.dpb.Reset()
		d.poc.Reset()
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

	top, _, poc, err := d.poc.ComputeFrame(sps, first.hdr, ref)
	if err != nil {
		return err
	}

	bFrame := false
	for _, entry := range slices {
		if entry.hdr.Type == syntax.SliceTypeB {
			bFrame = true
		}
	}
	fs, err := newFrameState(d, sps, pps, bFrame)
	if err != nil {
		return err
	}
	fs.poc = poc
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
		if s.hdr.Type != syntax.SliceTypeI {
			lists := d.dpb.Lists(s.hdr.FrameNum, poc, s.hdr.Type == syntax.SliceTypeB)
			for list := 0; list < 1+boolToInt(s.hdr.Type == syntax.SliceTypeB); list++ {
				n := int(s.hdr.NumRefIdxL0ActiveMinus1) + 1
				if list == 1 {
					n = int(s.hdr.NumRefIdxL1ActiveMinus1) + 1
				}
				var err error
				s.refs[list], err = d.dpb.Modify(lists[list], s.hdr.FrameNum, s.hdr.RefListModifications[list], n)
				if err != nil {
					return err
				}
			}
		}
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
	if fs.mv1 != nil {
		fs.mv1.BeginSlice(0)
	}
	if err := fs.deblock(); err != nil {
		return err
	}
	fs.pic.FillBorders()

	frame, err := d.toFrame(fs.pic, sps, p, idr)
	if err != nil {
		return err
	}
	if ref {
		r := d.dpb.NewReference(fs.pic, first.hdr.FrameNum, poc)
		r.Motion[0] = fs.mv
		r.Motion[1] = fs.mv1
		r.BlockReferences = fs.referenceIDs
		if err := d.dpb.Store(r, idr, first.hdr.LongTermReferenceFlag, first.hdr.AdaptiveRefPicMarking, first.hdr.MemoryManagement); err != nil {
			frame.Release()
			return err
		}
		fs.pic = nil
		for _, m := range first.hdr.MemoryManagement {
			if m.Operation == 5 {
				d.poc.AfterMMCO5(top - poc)
				r.POC = 0
				poc = 0
				d.outputBoundary(false)
			}
		}
	} else {
		fs.release()
	}
	if err := d.queueFrame(frame, poc); err != nil {
		frame.Release()
		return err
	}
	d.waitingForIDR = false
	ok = true
	return nil
}

// checkSupported 在进入宏块解码前拒绝 M3 范围外的工具。
func (d *Decoder) checkSupported(sps *syntax.SPS, pps *syntax.PPS, hdr *syntax.SliceHeader) error {
	if !sps.FrameMbsOnlyFlag || sps.MbAdaptiveFrameFieldFlag || hdr.FieldPicFlag {
		return fmt.Errorf("decoder: field/MBAFF coding: %w", h264.ErrUnsupported)
	}
	if (sps.ChromaFormatIDC != 1 && sps.ChromaFormatIDC != 2) || sps.BitDepthLumaMinus8 != sps.BitDepthChromaMinus8 || (sps.BitDepthLumaMinus8 != 0 && sps.BitDepthLumaMinus8 != 2) {
		return fmt.Errorf("decoder: only 8/10-bit 4:2:0/4:2:2: %w", h264.ErrUnsupported)
	}
	if hdr.Type == syntax.SliceTypeSP || hdr.Type == syntax.SliceTypeSI {
		return fmt.Errorf("decoder: SP/SI slices: %w", h264.ErrUnsupported)
	}
	if hdr.SliceQP < -6*int(sps.BitDepthLumaMinus8) || hdr.SliceQP > 51 {
		return fmt.Errorf("decoder: slice QP %d: %w", hdr.SliceQP, h264.ErrInvalidData)
	}
	if hdr.RedundantPicCnt > 0 {
		return fmt.Errorf("decoder: redundant slices: %w", h264.ErrUnsupported)
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
	if fs.mv1 != nil {
		fs.mv1.BeginSlice(first)
	}
	qscale := s.hdr.SliceQP
	for addr := first; addr < end; addr++ {
		if fs.decoded[addr] {
			return fmt.Errorf("decoder: macroblock %d decoded twice: %w", addr, h264.ErrInvalidData)
		}
		if s.hdr.Type == syntax.SliceTypeP || s.hdr.Type == syntax.SliceTypeB {
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
		Format:   pic.Format,
		PTS:      p.PTS,
		Duration: p.Duration,
		KeyFrame: key,
	}
	for pi := 0; pi < 3; pi++ {
		pw, ph := pic.Format.PlaneDimensions(pi, w, h)
		rowBytes := f.Format.RowBytes(pi, w)
		buf := make([]byte, rowBytes*ph)
		src := &pic.Planes[pi]
		subW, subH := pic.Format.ChromaSubsampling()
		cropX := int(sps.FrameCropLeftOffset) * subW
		cropY := int(sps.FrameCropTopOffset) * subH
		if pi != 0 {
			cropX /= subW
			cropY /= subH
		}
		for y := 0; y < ph; y++ {
			row := buf[y*rowBytes : (y+1)*rowBytes]
			if pic.Format.BitDepth() > 8 {
				for x, value := range src.Row16(y + cropY)[cropX : cropX+pw] {
					binary.LittleEndian.PutUint16(row[x*2:], value)
				}
			} else {
				copy(row, src.Row(y + cropY)[cropX:cropX+pw])
			}
		}
		f.Planes[pi] = h264.Plane{Data: buf, Stride: rowBytes}
	}
	return f, nil
}
