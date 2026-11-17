package decoder

import (
	"fmt"
	"unsafe"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/bitstream"
	"github.com/cry5tallize/goavc/internal/dpb"
	"github.com/cry5tallize/goavc/internal/motion"
	"github.com/cry5tallize/goavc/internal/picture"
	"github.com/cry5tallize/goavc/nal"
	"github.com/cry5tallize/goavc/syntax"
)

// sliceEntry 是解析后的一个 slice：header 与定位好的 slice data reader。
type sliceEntry struct {
	unit           nal.Unit
	hdr            *syntax.SliceHeader
	sps            *syntax.SPS
	pps            *syntax.PPS
	rbsp           []byte
	data           *bitstream.Reader
	cabac          *cabacSyntax
	refs           [2][]*dpb.Reference
	frameRefs      [2][]*dpb.Reference
	fieldRefs      [2][2][]*dpb.Reference
	firstMB, endMB int
	partition      *partitionedSlice
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
	// 去块专用派生值；原 lumaNZ 仍保存熵上下文需要的系数计数。
	deblockNZ      uint16
	deblockNZReady bool
	deblockUniform bool // 整宏块 Direct 已发布两列表一致的运动与身份。
}

// frameState 是一帧解码期间的工作状态。
type frameState struct {
	d       *Decoder
	pic     *picture.Picture
	sps     *syntax.SPS
	pps     *syntax.PPS
	poc     int32
	scaling syntax.ScalingMatrices

	temporary *frameTemporary

	mbW, mbH, total  int
	subW, subH       int
	chromaW, chromaH int
	chromaBlocks     int
	component        int               // 444共享全尺寸内核当前处理的平面；其余路径保持0。
	fullResidual     *[2]planeResidual // 仅共同编码444分配，每帧复用，420/422不增加宏块清零量。
	framePic         *picture.Picture
	fieldPics        *[2]picture.Picture
	mbaff, field     bool
	parity           int
	activeMB         int
	fieldFlags       []bool
	framePOC         int32
	fieldPOC         [2]int32
	decoded          []bool
	groupMap         []uint8 // FMO按语法扫描地址索引；普通单组不分配。
	sliceOwners      []int   // 物理MB的slice身份，供非连续组的运动邻域使用。
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

const decoderTemporaryBudget = 16 << 20

// frameTemporary只含重建、覆盖验证和去块结束前使用的无指针元素数组。
// Motion、referenceIDs和MBField会交给DPB，绝不能放进此缓存。
type frameTemporary struct {
	inUse        bool
	decoded      []bool
	info         []mbInfo
	lumaNZ       []uint8
	chromaNZ     [2][]uint8
	dcCoded      [3][]bool
	mvd          [2][]motion.MV
	directBlocks []bool
	sliceOwners  []int
	fullResidual *[2]planeResidual
}

func (t *frameTemporary) bytes() int {
	// unsafe.Sizeof仅作编译期类型大小记账；不做指针转换或内存访问。
	n := cap(t.decoded)*int(unsafe.Sizeof(bool(false))) + cap(t.info)*int(unsafe.Sizeof(mbInfo{})) + cap(t.lumaNZ)
	for _, nz := range t.chromaNZ {
		n += cap(nz)
	}
	for _, dc := range t.dcCoded {
		n += cap(dc) * int(unsafe.Sizeof(bool(false)))
	}
	for _, mvd := range t.mvd {
		n += cap(mvd) * int(unsafe.Sizeof(motion.MV{}))
	}
	n += cap(t.directBlocks)*int(unsafe.Sizeof(bool(false))) + cap(t.sliceOwners)*int(unsafe.Sizeof(int(0)))
	if t.fullResidual != nil {
		n += int(unsafe.Sizeof([2]planeResidual{}))
	}
	return n
}

func (d *Decoder) idleTemporaryBytes() int {
	n := 0
	for i := range d.temporary {
		if !d.temporary[i].inUse {
			n += d.temporary[i].bytes()
		}
	}
	return n
}

func (d *Decoder) trimTemporary() {
	n := d.idleTemporaryBytes()
	for i := range d.temporary {
		t := &d.temporary[i]
		if n <= decoderTemporaryBudget {
			break
		}
		if !t.inUse {
			n -= t.bytes()
			*t = frameTemporary{}
		}
	}
}

func (fs *frameState) acquireTemporary(bFrame bool) {
	// 串行表示最多同时持有三个独立平面，每平面独占一个slot。
	for i := range fs.d.temporary {
		t := &fs.d.temporary[i]
		// 优先已有的大容量slot，避免预算淘汰留下的空slot触发无谓分配。
		if !t.inUse && (fs.temporary == nil || cap(t.info) > cap(fs.temporary.info)) {
			fs.temporary = t
		}
	}
	if fs.temporary == nil {
		panic("decoder: more than three active temporary planes")
	}
	t := fs.temporary
	t.inUse = true
	n := fs.total
	if cap(t.decoded) < n {
		t.decoded = make([]bool, n)
	} else {
		t.decoded = t.decoded[:n]
		clear(t.decoded)
	}
	if cap(t.info) < n {
		t.info = make([]mbInfo, n)
	} else {
		t.info = t.info[:n]
	}
	for i := range t.info {
		t.info[i] = mbInfo{sliceID: -1, intraModes: [16]int8{-1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1}, chromaMode: -1}
	}
	if cap(t.lumaNZ) < n*16 {
		t.lumaNZ = make([]uint8, n*16)
	} else {
		t.lumaNZ = t.lumaNZ[:n*16]
		clear(t.lumaNZ)
	}
	fs.decoded, fs.info, fs.lumaNZ = t.decoded, t.info, t.lumaNZ
	if !fs.sps.SeparateColourPlaneFlag {
		for c := range t.chromaNZ {
			if cap(t.chromaNZ[c]) < n*fs.chromaBlocks {
				t.chromaNZ[c] = make([]uint8, n*fs.chromaBlocks)
			} else {
				t.chromaNZ[c] = t.chromaNZ[c][:n*fs.chromaBlocks]
				clear(t.chromaNZ[c])
			}
		}
		fs.chromaNZ = t.chromaNZ
	}
	if fs.sps.ChromaFormatIDC == 3 && !fs.sps.SeparateColourPlaneFlag {
		if t.fullResidual == nil {
			t.fullResidual = new([2]planeResidual)
		} else {
			*t.fullResidual = [2]planeResidual{}
		}
		fs.fullResidual = t.fullResidual
	}
	if bFrame {
		if cap(t.directBlocks) < n*16 {
			t.directBlocks = make([]bool, n*16)
		} else {
			t.directBlocks = t.directBlocks[:n*16]
			clear(t.directBlocks)
		}
		fs.directBlocks = t.directBlocks
	}
	if fs.pps.EntropyCodingModeFlag {
		for c := range t.dcCoded {
			if cap(t.dcCoded[c]) < n {
				t.dcCoded[c] = make([]bool, n)
			} else {
				t.dcCoded[c] = t.dcCoded[c][:n]
				clear(t.dcCoded[c])
			}
		}
		fs.dcCoded = t.dcCoded
		for list := range 1 + boolToInt(bFrame) {
			if cap(t.mvd[list]) < n*16 {
				t.mvd[list] = make([]motion.MV, n*16)
			} else {
				t.mvd[list] = t.mvd[list][:n*16]
				clear(t.mvd[list])
			}
		}
		fs.mvd = t.mvd[0]
		if bFrame {
			fs.mvd1 = t.mvd[1]
		}
	}
}

// releaseTemporary在归还slot之前解除所有临时别名与只读元数据引用。
// 成功路径在覆盖验证、deblock和FillBorders之后调用，不影响DPB retained数组。
func (fs *frameState) releaseTemporary() {
	if fs == nil || fs.temporary == nil {
		return
	}
	d, t := fs.d, fs.temporary
	if fs.sliceOwners != nil {
		// 包括失败路径，先解除Grid对可复用sliceOwners的借用。
		if fs.mv != nil {
			fs.mv.BeginSlice(0)
		}
		if fs.mv1 != nil {
			fs.mv1.BeginSlice(0)
		}
	}
	fs.temporary = nil
	fs.d, fs.sps, fs.pps = nil, nil, nil
	fs.decoded, fs.info, fs.lumaNZ = nil, nil, nil
	fs.chromaNZ = [2][]uint8{}
	fs.dcCoded = [3][]bool{}
	fs.mvd, fs.mvd1, fs.directBlocks = nil, nil, nil
	fs.fullResidual, fs.groupMap, fs.sliceOwners = nil, nil, nil
	t.inUse = false
	d.trimTemporary()
}

func newFrameState(d *Decoder, sps *syntax.SPS, pps *syntax.PPS, bFrame bool) (*frameState, error) {
	return newFrameStateForPicture(d, sps, pps, bFrame, nil)
}

func newFrameStateForPicture(d *Decoder, sps *syntax.SPS, pps *syntax.PPS, bFrame bool, pic *picture.Picture) (*frameState, error) {
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
	if sps.ChromaFormatIDC == 3 {
		format = h264.YUV444P
		if sps.BitDepthLumaMinus8 == 2 {
			format = h264.YUV444P10LE
		}
	}
	subW, subH := format.ChromaSubsampling()
	chromaW, chromaH := format.PlaneDimensions(1, 16, 16)
	chromaBlocks := chromaW * chromaH / 16
	ownedPicture := pic == nil
	if pic == nil {
		pic, err = d.pool.Get(mbW, mbH, format)
		if err != nil {
			return nil, err
		}
	}
	total := mbW * mbH
	fs := &frameState{
		scaling:  scaling,
		d:        d,
		pic:      pic,
		framePic: pic,
		mbaff:    sps.MbAdaptiveFrameFieldFlag,
		sps:      sps,
		pps:      pps,
		mbW:      mbW,
		mbH:      mbH,
		total:    total,
		subW:     subW, subH: subH,
		chromaW: chromaW, chromaH: chromaH, chromaBlocks: chromaBlocks,
		mv: motion.NewGrid(mbW, mbH),
	}
	if fs.mbaff {
		fs.fieldPics = new([2]picture.Picture)
		fs.fieldFlags = make([]bool, total)
		for i := range 2 {
			fs.fieldPics[i], err = picture.FieldView(pic, i != 0)
			if err != nil {
				if ownedPicture {
					pic.Release()
				}
				return nil, err
			}
		}
	}
	fs.acquireTemporary(bFrame)
	fs.referenceIDs[0] = make([]*dpb.Identity, total*16)
	if bFrame {
		fs.mv1 = motion.NewGrid(mbW, mbH)
		fs.referenceIDs[1] = make([]*dpb.Identity, total*16)
	}
	// NewGrid已完成初始化；中间仅准备独立临时数组，没有改写运动网格。
	return fs, nil
}

// release归还尚未交给DPB的图像与临时存储，可重复调用。
func (fs *frameState) release() {
	if fs == nil {
		return
	}
	fs.releaseTemporary()
	if fs.framePic != nil {
		fs.framePic.Release()
	}
	fs.framePic = nil
	fs.pic = nil
	fs.fieldPics = nil
	fs.fieldFlags = nil
	fs.referenceIDs = [2][]*dpb.Identity{}
	fs.mv, fs.mv1 = nil, nil
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
	var hdr *syntax.SliceHeader
	var partition *partitionedSlice
	offset := uint64(0)
	if u.Header.Type == nal.TypeSlicePartitionA {
		if sps.ProfileIDC != 88 || pps.EntropyCodingModeFlag {
			return sliceEntry{}, fmt.Errorf("decoder: partitions require Extended CAVLC: %w", h264.ErrInvalidData)
		}
		a, err := syntax.ParseDataPartitionA(syntax.DataPartitionInput{RBSP: rbsp, BitLength: uint64(len(rbsp)) * 8, Header: u.Header}, sps, d.pps)
		if err != nil {
			return sliceEntry{}, err
		}
		hdr, offset = a.SliceHeader, a.DataBitOffset
		partition = &partitionedSlice{context: a.Context}
	} else {
		hdr, err = syntax.ParseSliceHeaderEx(rbsp, sps, d.pps, u.Header.IsIDR(), u.Header.RefIDC)
		if err != nil {
			return sliceEntry{}, err
		}
		offset = hdr.HeaderBits
	}
	if err := d.checkSupported(sps, pps, hdr); err != nil {
		return sliceEntry{}, err
	}
	r := bitstream.NewReader(rbsp)
	if err := r.SkipBits(uint(offset)); err != nil {
		return sliceEntry{}, err
	}
	return sliceEntry{unit: u, hdr: hdr, sps: sps, pps: pps, rbsp: rbsp, data: r, partition: partition}, nil
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
	if len(slices) > d.cfg.MaxSliceCount {
		return fmt.Errorf("decoder: slice count exceeds limit: %w", h264.ErrResourceLimit)
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

	top, bottom, poc, err := d.poc.ComputeFrame(sps, first.hdr, ref)
	if err != nil {
		return err
	}

	states, planeCount, selected, err := d.reconstructRepresentations(slices, top, bottom, poc)
	if err != nil {
		return err
	}
	first = selected
	fs := states[0]
	ok := false
	defer func() {
		if !ok {
			fs.release()
		}
	}()

	frame, err := d.toFrame(fs.pic, sps, p, idr)
	if err != nil {
		return err
	}
	if ref {
		r := d.dpb.NewReference(fs.pic, first.hdr.FrameNum, poc)
		r.Motion[0] = fs.mv
		r.Motion[1] = fs.mv1
		r.BlockReferences = fs.referenceIDs
		r.FieldPOC, r.MBField = [2]int32{top, bottom}, fs.fieldFlags
		for comp := 1; comp < planeCount; comp++ {
			r.ChromaMotion[comp-1] = [2]*motion.Grid{states[comp].mv, states[comp].mv1}
			r.ChromaBlockReferences[comp-1] = states[comp].referenceIDs
			r.ChromaMBField[comp-1] = states[comp].fieldFlags
		}
		if err := d.dpb.Store(r, idr, first.hdr.LongTermReferenceFlag, first.hdr.AdaptiveRefPicMarking, first.hdr.MemoryManagement); err != nil {
			frame.Release()
			return err
		}
		fs.pic = nil
		fs.framePic = nil
		for _, m := range first.hdr.MemoryManagement {
			if m.Operation == 5 {
				d.poc.AfterMMCO5(top - poc)
				r.FieldPOC[0] -= poc
				r.FieldPOC[1] -= poc
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
	if hdr.FieldPicFlag || !sps.FrameMbsOnlyFlag && !sps.MbAdaptiveFrameFieldFlag {
		return fmt.Errorf("decoder: PAFF field pictures: %w", h264.ErrUnsupported)
	}
	if pps.NumSliceGroupsMinus1 != 0 && (pps.EntropyCodingModeFlag || (sps.ProfileIDC != 66 && sps.ProfileIDC != 88)) {
		return fmt.Errorf("decoder: FMO requires Baseline/Extended CAVLC: %w", h264.ErrInvalidData)
	}
	if sps.ChromaFormatIDC < 1 || sps.ChromaFormatIDC > 3 || sps.BitDepthLumaMinus8 != sps.BitDepthChromaMinus8 || (sps.BitDepthLumaMinus8 != 0 && sps.BitDepthLumaMinus8 != 2) {
		return fmt.Errorf("decoder: only 8/10-bit 4:2:0/4:2:2/4:4:4: %w", h264.ErrUnsupported)
	}
	if err := checkSwitchingSupported(sps, pps, hdr); err != nil {
		return err
	}
	if hdr.SliceQP < -6*int(sps.BitDepthLumaMinus8) || hdr.SliceQP > 51 {
		return fmt.Errorf("decoder: slice QP %d: %w", hdr.SliceQP, h264.ErrInvalidData)
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
	if fs.mbaff {
		if err := decodeMBAFFCAVLC(fs, s, sliceID, first, end); err != nil {
			return err
		}
	} else {
		if err := decodeMappedCAVLC(fs, s, sliceID, first, end); err != nil {
			return err
		}
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
	return validatePartitionTrailing(s)
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
	f, err := h264.NewFrame(w, h, pic.Format)
	if err != nil {
		return nil, fmt.Errorf("decoder: allocate output frame: %w", err)
	}
	f.PTS = p.PTS
	f.Duration = p.Duration
	f.KeyFrame = key
	for pi := 0; pi < 3; pi++ {
		pw, ph := pic.Format.PlaneDimensions(pi, w, h)
		rowBytes := f.Format.RowBytes(pi, w)
		src := &pic.Planes[pi]
		subW, subH := pic.Format.ChromaSubsampling()
		cropX := int(sps.FrameCropLeftOffset) * subW
		cropY := int(sps.FrameCropTopOffset) * subH
		if pi != 0 {
			cropX /= subW
			cropY /= subH
		}
		high := pic.Format.BitDepth() > 8
		for y := 0; y < ph; y++ {
			row := f.Planes[pi].Data[y*rowBytes : (y+1)*rowBytes]
			if high {
				picture.PackRow16LE(row, src.Row16(y + cropY)[cropX:cropX+pw])
			} else {
				copy(row, src.Row(y + cropY)[cropX:cropX+pw])
			}
		}
	}
	return f, nil
}
