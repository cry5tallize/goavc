package decoder

import (
	"fmt"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/internal/entropy/cabac"
	"github.com/goavc/h264/internal/motion"
	"github.com/goavc/h264/internal/picture"
	"github.com/goavc/h264/syntax"
)

// 8.5.2.1 的 CBP 映射表：码流中的 coded_block_pattern 不是直接位图。
var intraCBPMap = [48]uint8{
	47, 31, 15, 0, 23, 27, 29, 30, 7, 11, 13, 14, 39, 43, 45, 46,
	16, 3, 5, 10, 12, 19, 21, 26, 28, 35, 37, 42, 44, 1, 2, 4,
	8, 17, 18, 20, 24, 6, 9, 22, 25, 32, 33, 34, 36, 40, 38, 41,
}

var interCBPMap = [48]uint8{
	0, 16, 1, 2, 4, 8, 32, 3, 5, 10, 12, 15, 47, 7, 11, 13,
	14, 6, 9, 31, 35, 37, 42, 44, 33, 34, 36, 40, 39, 43, 45, 46,
	17, 18, 20, 24, 19, 21, 26, 28, 23, 27, 29, 30, 22, 25, 38, 41,
}

// planeResidual 保存全尺寸平面的变换系数；444复用亮度内核，不复制系数。
type planeResidual struct {
	luma   [16][16]int32
	lumaDC [16]int32
	luma8  [4][64]int32
}

// residual 保存两种熵模式共用的宏块 level（尚未反量化）。
type residual struct {
	planeResidual
	chroma   [2][8][16]int32
	chromaDC [2][8]int32
}

// blockRaster 把比特流中的 4x4 块序号（8x8 分组）映射为光栅下标。
//
// 规范按 i8x8 外层、i4x4 内层解析与重建；存储使用光栅下标。
func blockRaster(n int) int {
	i8 := n / 4
	i4 := n % 4
	x := (i8&1)*2 + (i4 & 1)
	y := (i8>>1)*2 + (i4 >> 1)
	return y*4 + x
}

func decodeSkipMacroblock(fs *frameState, addr int, s *sliceEntry, qp int, sliceID int) error {
	mbx, mby := addr%fs.mbW, addr/fs.mbW
	if s.hdr.Type == syntax.SliceTypeB {
		info := &fs.info[addr]
		*info = mbInfo{skip: true, direct: true, qp: qp, sliceID: sliceID, deblockIDC: uint8(s.hdr.DisableDeblockingFilterIDC), alphaDiv2: int8(s.hdr.SliceAlphaC0OffsetDiv2), betaDiv2: int8(s.hdr.SliceBetaOffsetDiv2), chromaMode: -1}
		for i := range info.intraModes {
			info.intraModes[i] = -1
		}
		return decodeBMacroblock(fs, addr, mbx, mby, 0, s, &qp, true)
	}
	if len(s.refs[0]) == 0 {
		return fmt.Errorf("decoder: P_Skip without reference: %w", h264.ErrInvalidData)
	}
	ref := s.refs[0][0].Picture
	mv := motion.PredictSkip(fs.mv, mbx*4, mby*4, 0)
	if err := mv.Validate(); err != nil {
		return err
	}
	fs.mv.SetRect(mbx*4, mby*4, 4, 4, mv, 0)
	fs.setReferenceIdentity(0, mbx*4, mby*4, 4, 4, s.refs[0][0].Identity)
	fs.mcPartition(ref, mbx, mby, 0, 0, 16, 16, mv)
	s.applyPWeights(fs, mbx, mby, 0, 0, 16, 16, 0)
	info := &fs.info[addr]
	*info = mbInfo{skip: true, qp: qp, sliceID: sliceID}
	info.intraModes = [16]int8{-1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1}
	info.chromaMode = -1
	info.deblockIDC = uint8(s.hdr.DisableDeblockingFilterIDC)
	info.alphaDiv2 = int8(s.hdr.SliceAlphaC0OffsetDiv2)
	info.betaDiv2 = int8(s.hdr.SliceBetaOffsetDiv2)
	return nil
}

func decodeMacroblock(fs *frameState, addr int, s *sliceEntry, sliceID int, qscale *int) error {
	mbx, mby := addr%fs.mbW, addr/fs.mbW
	info := &fs.info[addr]
	info.sliceID = sliceID
	info.qp = *qscale
	info.deblockIDC = uint8(s.hdr.DisableDeblockingFilterIDC)
	info.alphaDiv2 = int8(s.hdr.SliceAlphaC0OffsetDiv2)
	info.betaDiv2 = int8(s.hdr.SliceBetaOffsetDiv2)

	mbType, err := s.readMBType(fs, addr)
	if err != nil {
		return fmt.Errorf("mb_type: %w", err)
	}
	if s.hdr.Type == syntax.SliceTypeP {
		if mbType <= 4 {
			return decodeInterMacroblock(fs, addr, mbx, mby, int(mbType), s, qscale)
		}
		mbType -= 5
	}
	if s.hdr.Type == syntax.SliceTypeB {
		if mbType <= 22 {
			return decodeBMacroblock(fs, addr, mbx, mby, int(mbType), s, qscale, false)
		}
		mbType -= 23
	}
	if mbType > 25 {
		return fmt.Errorf("intra mb_type %d exceeds 25: %w", mbType, h264.ErrInvalidData)
	}
	return decodeIntraMacroblock(fs, addr, mbx, mby, int(mbType), s, qscale)
}

// decodeIntraMacroblock 处理 I 宏块（I4x4 / I8x8 / I16x16 / I_PCM）。
func decodeIntraMacroblock(fs *frameState, addr, mbx, mby, mbType int, s *sliceEntry, qscale *int) error {
	info := &fs.info[addr]
	info.intra = true
	fs.mv.SetRect(mbx*4, mby*4, 4, 4, motion.MV{}, -1)
	if fs.mv1 != nil {
		fs.mv1.SetRect(mbx*4, mby*4, 4, 4, motion.MV{}, -1)
	}

	if mbType == 25 {
		return decodePCMMacroblock(fs, addr, mbx, mby, s)
	}
	if mbType == 0 {
		// IntraNxN：PPS 允许时先读取 8×8 标志，再读取对应数量的模式。
		if fs.pps.Transform8x8ModeFlag {
			v, err := s.readTransformFlag(fs, addr)
			if err != nil {
				return err
			}
			info.transform8 = v
		}
		var modes [16]int8
		count := 16
		if info.transform8 {
			count = 4
		}
		for n := 0; n < count; n++ {
			index := n
			if info.transform8 {
				index = n * 4
			}
			raster := blockRaster(index)
			pred := fs.mostProbableMode(mbx, mby, raster)
			mode, err := s.readIntraMode(pred)
			if err != nil {
				return err
			}
			if mode < 0 || mode > 8 {
				return fmt.Errorf("intra4x4 mode %d out of range: %w", mode, h264.ErrInvalidData)
			}
			// Spec: 8.3.1。缓存规范模式 0..8；边界 DC 的内部替代编号
			// 只用于像素预测，不能参与后续 MPM 推导。
			modes[raster] = int8(mode)
			// 立即写回缓存：后续块的 MPM 需要已解析的邻块模式，
			// 若等整个循环结束再赋值，所有 pred 都会退化为 DC。
			info.intraModes[raster] = int8(mode)
			if info.transform8 {
				for _, offset := range []int{0, 1, 4, 5} {
					modes[raster+offset] = int8(mode)
					info.intraModes[raster+offset] = int8(mode)
				}
			}
		}
		chromaMode, err := s.readChromaMode(fs, addr)
		if err != nil {
			return fmt.Errorf("intra_chroma_pred_mode: %w", err)
		}
		if chromaMode > 3 {
			return fmt.Errorf("intra_chroma_pred_mode %d out of range: %w", chromaMode, h264.ErrInvalidData)
		}
		info.intraModes = modes
		info.chromaMode = int8(chromaMode)
		cbp, err := s.readCBP(fs, addr, true)
		if err != nil {
			return err
		}
		info.cbp = cbp
		var res residual
		if cbp != 0 {
			if err := s.updateQP(qscale); err != nil {
				return err
			}
			info.qp = *qscale
		}
		if err := fs.decodeResiduals(s, addr, mbx, mby, false, false, cbp, &res); err != nil {
			return err
		}
		if info.transform8 {
			if err := fs.reconstructIntra8x8(mbx, mby, cbp, *qscale, &res); err != nil {
				return err
			}
		} else {
			if err := fs.reconstructIntra4x4(mbx, mby, cbp, *qscale, &res); err != nil {
				return err
			}
		}
		// I4x4/I8x8的色度按采样格式使用8x8或8x16预测，与亮度模式无关。
		if err := fs.predictChroma(mbx, mby, int(chromaMode)); err != nil {
			return err
		}
		return fs.addChromaResidual(mbx, mby, (cbp>>4)&3, *qscale, &res)
	}

	// Intra16x16：mb_type 1..24。
	predMode, lumaAC, chromaCBP := intra16Info(mbType)
	chromaMode, err := s.readChromaMode(fs, addr)
	if err != nil {
		return fmt.Errorf("intra_chroma_pred_mode: %w", err)
	}
	if chromaMode > 3 {
		return fmt.Errorf("intra_chroma_pred_mode %d out of range: %w", chromaMode, h264.ErrInvalidData)
	}
	info.intra16 = true
	info.chromaMode = int8(chromaMode)
	cbp := chromaCBP << 4
	if lumaAC {
		cbp |= 0x0F
	}
	info.cbp = cbp
	if err := s.updateQP(qscale); err != nil {
		return err
	}
	info.qp = *qscale
	var res residual
	if err := fs.decodeResiduals(s, addr, mbx, mby, true, lumaAC, cbp, &res); err != nil {
		return err
	}
	if err := fs.reconstructI16(mbx, mby, predMode, int(chromaMode), lumaAC, *qscale, &res); err != nil {
		return err
	}
	return fs.addChromaResidual(mbx, mby, chromaCBP, *qscale, &res)
}

// intra16Info 把 mb_type 1..24 映射为 (pred_mode, luma_ac, chroma_cbp)。
func intra16Info(mbType int) (predMode int, lumaAC bool, chromaCBP int) {
	idx := mbType - 1
	pred := idx % 4
	group := idx / 4
	return pred, group >= 3, group % 3
}

func decodePCMMacroblock(fs *frameState, addr, mbx, mby int, s *sliceEntry) error {
	for !s.data.ByteAligned() {
		v, err := s.data.ReadBits(1)
		if err != nil {
			return err
		}
		if v != 0 {
			return fmt.Errorf("decoder: nonzero PCM alignment: %w", h264.ErrInvalidData)
		}
	}
	planes := [3]*picture.Plane{&fs.pic.Planes[0], &fs.pic.Planes[1], &fs.pic.Planes[2]}
	dims := [3][2]int{{16, 16}, {fs.chromaW, fs.chromaH}, {fs.chromaW, fs.chromaH}}
	origins := [3][2]int{{mbx * 16, mby * 16}, {mbx * fs.chromaW, mby * fs.chromaH}, {mbx * fs.chromaW, mby * fs.chromaH}}
	firstPlane, planeEnd := 0, 3
	if fs.sps.SeparateColourPlaneFlag {
		firstPlane, planeEnd = fs.component, fs.component+1
	}
	for pi := firstPlane; pi < planeEnd; pi++ {
		for y := 0; y < dims[pi][1]; y++ {
			for x := 0; x < dims[pi][0]; x++ {
				v, err := readBits(s.data, uint(8+fs.sps.BitDepthLumaMinus8))
				if err != nil {
					return fmt.Errorf("i_pcm plane %d: %w", pi, err)
				}
				planes[pi].SetSample(origins[pi][0]+x, origins[pi][1]+y, uint16(v))
			}
		}
	}
	info := &fs.info[addr]
	info.intra = true
	info.pcm = true
	info.qp = -6 * int(fs.sps.BitDepthLumaMinus8) // I_PCM的去块QP'为0，元数据保留语法域。
	info.cbp = 47
	base := addr * 16
	for i := 0; i < 16; i++ {
		fs.lumaNZ[base+i] = 16
	}
	cb := addr * fs.chromaBlocks
	for i := 0; !fs.sps.SeparateColourPlaneFlag && i < fs.chromaBlocks; i++ {
		fs.chromaNZ[0][cb+i] = 16
		fs.chromaNZ[1][cb+i] = 16
	}
	fs.mv.SetRect(mbx*4, mby*4, 4, 4, motion.MV{}, -1)
	if s.cabac != nil {
		for i := range fs.dcCoded {
			fs.dcCoded[i][addr] = true
		}
		engine, err := cabac.New(s.data)
		if err != nil {
			return err
		}
		s.cabac.d = engine
		s.cabac.lastDelta = false
	}
	return nil
}

func updateQScale(r *bitstream.Reader, qscale *int, depthOffset int) error {
	d, err := r.ReadSE()
	if err != nil {
		return fmt.Errorf("mb_qp_delta: %w", err)
	}
	return applyQPDelta(qscale, int(d), depthOffset)
}

// applyQPDelta 保留语法QPY：高位深的回绕区间为[-QpBdOffset,51]。
func applyQPDelta(qp *int, delta, depthOffset int) error {
	if delta < -(26+depthOffset/2) || delta > 25+depthOffset/2 {
		return fmt.Errorf("decoder: mb_qp_delta %d: %w", delta, h264.ErrInvalidData)
	}
	*qp = (*qp+delta+52+2*depthOffset)%(52+depthOffset) - depthOffset
	return nil
}

func readBit(r *bitstream.Reader) (uint64, error) {
	v, err := r.ReadBits(1)
	if err != nil {
		return 0, fmt.Errorf("read bit: %w", err)
	}
	return v, nil
}

func readBits(r *bitstream.Reader, n uint) (uint64, error) {
	v, err := r.ReadBits(n)
	if err != nil {
		return 0, fmt.Errorf("read %d bits: %w", n, err)
	}
	return v, nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
