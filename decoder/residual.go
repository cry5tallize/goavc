package decoder

import (
	"fmt"

	"github.com/cry5tallize/goavc/internal/dpb"
	"github.com/cry5tallize/goavc/internal/entropy/cavlc"
	"github.com/cry5tallize/goavc/internal/motion"
	"github.com/cry5tallize/goavc/internal/picture"
	"github.com/cry5tallize/goavc/internal/prediction"
	"github.com/cry5tallize/goavc/internal/transform"
)

// decodeCAVLCLuma 同时服务亮度与444全尺寸色度，nC按当前平面独立维护。
func (fs *frameState) decodeCAVLCLuma(s *sliceEntry, addr, mbx, mby int, i16, lumaAC bool, cbp int, res *planeResidual) error {
	r := s.data
	grid := fs.planeNZ()
	if i16 {
		bx, by := 0, 0
		nC := fs.lumaNC(mbx, mby, bx, by)
		if _, err := fs.decodeBlock(r, &res.lumaDC, cavlc.LumaDC, nC); err != nil {
			return fmt.Errorf("luma dc: %w", err)
		}
		if lumaAC {
			for i8 := 0; i8 < 4; i8++ {
				for i4 := 0; i4 < 4; i4++ {
					raster := blockRaster(i4 + 4*i8)
					nC := fs.lumaNC(mbx, mby, raster%4, raster/4)
					n, err := fs.decodeBlock(r, &res.luma[raster], cavlc.LumaAC, nC)
					if err != nil {
						return fmt.Errorf("luma ac block %d: %w", raster, err)
					}
					grid[addr*16+raster] = uint8(n)
				}
			}
		}
	} else {
		for i8 := 0; i8 < 4; i8++ {
			if cbp&(1<<i8) == 0 {
				continue
			}
			for i4 := 0; i4 < 4; i4++ {
				raster := blockRaster(i4 + 4*i8)
				nC := fs.lumaNC(mbx, mby, raster%4, raster/4)
				target := &res.luma[raster]
				var stream [16]int32
				if fs.info[addr].transform8 {
					target = &stream
				}
				n, err := fs.decodeBlock(r, target, cavlc.Luma4x4, nC)
				if err != nil {
					return fmt.Errorf("luma block %d: %w", raster, err)
				}
				grid[addr*16+raster] = uint8(n)
				if fs.info[addr].transform8 {
					for k := 0; k < 16; k++ {
						res.luma8[i8][fs.scan8()[4*k+i4]] = stream[fs.scan4()[k]]
					}
				}
			}
		}
	}
	return nil
}

func (fs *frameState) decodeCAVLCChroma(s *sliceEntry, addr, mbx, mby, cbp int, res *residual) error {
	r := s.data

	chromaCBP := (cbp >> 4) & 3
	if chromaCBP == 0 {
		return nil
	}
	for comp := 0; comp < 2; comp++ {
		dcType := cavlc.ChromaDC
		if fs.chromaBlocks == 8 {
			dcType = cavlc.ChromaDC422
		}
		var tmp [16]int32
		_, err := cavlc.DecodeBlock(r, &tmp, dcType, 0)
		if err != nil {
			return fmt.Errorf("chroma dc %d: %w", comp, err)
		}
		copy(res.chromaDC[comp][:], tmp[:fs.chromaBlocks])
	}
	if chromaCBP != 2 {
		return nil
	}
	for comp := 0; comp < 2; comp++ {
		for b := 0; b < fs.chromaBlocks; b++ {
			nC := fs.chromaNC(comp, mbx, mby, b%2, b/2)
			n, err := fs.decodeBlock(r, &res.chroma[comp][b], cavlc.LumaAC, nC)
			if err != nil {
				return fmt.Errorf("chroma ac comp %d block %d: %w", comp, b, err)
			}
			fs.chromaNZ[comp][addr*fs.chromaBlocks+b] = uint8(n)
		}
	}
	return nil
}

// reconstructIntra4x4 按规范块序预测并重建，右上样本不得提前可用。
func (fs *frameState) reconstructPlane4(mbx, mby, cbp, qp int, res *planeResidual) error {
	plane := &fs.pic.Planes[fs.component]
	info := &fs.info[fs.mbAddress(mbx, mby)]
	for n := 0; n < 16; n++ {
		raster := blockRaster(n)
		bx, by := raster%4, raster/4
		mode := int(info.intraModes[raster])
		x, y := mbx*16+bx*4, mby*16+by*4
		if err := fs.predictIntra4(mbx, mby, bx, by, x, y, mode); err != nil {
			return err
		}
		i8 := (by/2)*2 + bx/2
		if cbp&(1<<i8) != 0 {
			if err := addResidual4x4(plane, x, y, &res.luma[raster], qp, nil, fs.matrix4(true, 0)); err != nil {
				return err
			}
		}
	}
	return nil
}

// reconstructI16 预测 16x16，并加入 DC/AC 残差。
func (fs *frameState) reconstructPlane16(mbx, mby, predMode int, lumaAC bool, qp int, res *planeResidual) error {
	plane := &fs.pic.Planes[fs.component]
	qp, err := transform.ScaleQP(qp, 8+int(fs.sps.BitDepthLumaMinus8))
	if err != nil {
		return err
	}
	if err := fs.predictIntraBlock(mbx, mby, 0, predMode); err != nil {
		return err
	}
	var dc [16]int32
	if err := transform.DequantLumaDCWithScaling(&dc, &res.lumaDC, qp, dcScale(fs.matrix4(true, 0))); err != nil {
		return err
	}
	for raster := 0; raster < 16; raster++ {
		var coeff [16]int32
		if lumaAC {
			if err := transform.Dequant4x4WithScaling(&coeff, &res.luma[raster], qp, fs.matrix4(true, 0)); err != nil {
				return err
			}
		}
		coeff[0] = dc[raster]
		bx, by := raster%4, raster/4
		if err := addResidual4x4(plane, mbx*16+bx*4, mby*16+by*4, &coeff, qp, &coeff, nil); err != nil {
			return err
		}
	}
	return nil
}

// predictChroma 按采样格式预测两个色度平面。
func (fs *frameState) predictChroma(mbx, mby, mode int) error {
	if fs.sps.ChromaFormatIDC == 3 {
		return nil
	}
	for comp := 0; comp < 2; comp++ {
		if err := fs.predictIntraBlock(mbx, mby, comp+1, mode); err != nil {
			return err
		}
	}
	return nil
}

// addLumaResidual 把非 I16x16 的亮度残差加到已预测像素上。
func (fs *frameState) addPlaneResidual(mbx, mby, cbp int, i16 bool, qp int, res *planeResidual) error {
	if i16 {
		return nil
	}
	if fs.info[fs.mbAddress(mbx, mby)].transform8 {
		for block := 0; block < 4; block++ {
			if cbp&(1<<block) != 0 {
				if err := fs.addLuma8Block(mbx, mby, block, qp, &res.luma8[block], false); err != nil {
					return err
				}
			}
		}
		return nil
	}
	plane := &fs.pic.Planes[fs.component]
	for raster := 0; raster < 16; raster++ {
		bx, by := raster%4, raster/4
		i8 := (by/2)*2 + bx/2
		if cbp&(1<<i8) == 0 {
			continue
		}
		if err := addResidual4x4(plane, mbx*16+bx*4, mby*16+by*4, &res.luma[raster], qp, nil, fs.matrix4(false, 0)); err != nil {
			return err
		}
	}
	return nil
}

// addChromaResidual 反量化色度 DC/AC 并加到预测像素。
func (fs *frameState) addChromaResidual(mbx, mby, chromaCBP, qp int, res *residual) error {
	if fs.sps.ChromaFormatIDC == 3 {
		return nil
	}
	if chromaCBP == 0 {
		return nil
	}
	offsets := [2]int{int(fs.pps.ChromaQPIndexOffset), int(fs.pps.SecondChromaQPIndexOffset)}
	for comp := 0; comp < 2; comp++ {
		qpc, err := transform.ScaleChromaQP(qp, offsets[comp], 8+int(fs.sps.BitDepthChromaMinus8))
		if err != nil {
			return err
		}
		matrix := fs.matrix4(fs.info[fs.mbAddress(mbx, mby)].intra, comp+1)
		var dc [8]int32
		if fs.chromaBlocks == 8 {
			if err := transform.DequantChromaDC422WithScaling(&dc, &res.chromaDC[comp], qpc, dcScale(matrix)); err != nil {
				return err
			}
		} else {
			levels := [4]int32(res.chromaDC[comp][:4])
			var dc420 [4]int32
			if err := transform.DequantChromaDCWithScaling(&dc420, &levels, qpc, dcScale(matrix)); err != nil {
				return err
			}
			copy(dc[:], dc420[:])
		}
		pl := &fs.pic.Planes[comp+1]
		for b := 0; b < fs.chromaBlocks; b++ {
			var coeff [16]int32
			if chromaCBP == 2 {
				if err := transform.Dequant4x4WithScaling(&coeff, &res.chroma[comp][b], qpc, matrix); err != nil {
					return err
				}
			}
			coeff[0] = dc[b]
			bx, by := b%2, b/2
			if err := addResidual4x4(pl, mbx*fs.chromaW+bx*4, mby*fs.chromaH+by*4, &coeff, qpc, &coeff, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

// addResidual4x4 对 coeff 做反量化/IDCT 并加到像素上。
//
// alreadyDequant 为 nil 表示使用 levels 本身做反量化；I16x16 的 DC 已经从
// 亮度 DC 变换写入 coeff[0]，因此传入同一数组且不再重复反量化 DC。
func addResidual4x4(pl *picture.Plane, x, y int, levels *[16]int32, qp int, alreadyDequant *[16]int32, scaling *[16]uint8) error {
	var dequant [16]int32
	src := levels
	if alreadyDequant == nil {
		var err error
		qp, err = transform.ScaleQP(qp, pl.BitDepth)
		if err != nil {
			return err
		}
		if err := transform.Dequant4x4WithScaling(&dequant, levels, qp, scaling); err != nil {
			return err
		}
		src = &dequant
	}
	var out [16]int32
	transform.IDCT4x4(&out, src)
	picture.AddResidual4x4(pl, x, y, &out)
	return nil
}

// mcPartition 做一次亮度+色度运动补偿并写入当前图像。
func (fs *frameState) mcPartition(r *dpb.Reference, mbx, mby, xOff, yOff, w, h int, mv motion.MV) {
	ref := r.Picture
	lx, ly := mbx*16+xOff, mby*16+yOff
	comp := 0
	if fs.sps.SeparateColourPlaneFlag {
		comp = fs.component
	}
	pl := &fs.pic.Planes[comp]
	if pl.BitDepth == 10 {
		prediction.MCBlockLumaHigh(pl.Data16[pl.Index(lx, ly):], pl.Stride, &ref.Planes[comp], lx, ly, w, h, int(mv.X), int(mv.Y))
	} else {
		prediction.MCBlockLuma(pl.Data[pl.Index(lx, ly):], pl.Stride, &ref.Planes[comp], lx, ly, w, h, int(mv.X), int(mv.Y))
	}
	if fs.sps.SeparateColourPlaneFlag {
		return
	}
	cx, cy := mbx*fs.chromaW+xOff/fs.subW, mby*fs.chromaH+yOff/fs.subH
	chromaY := int(mv.Y) + fs.chromaFieldOffset(r)
	for comp := 0; comp < 2; comp++ {
		if fs.sps.ChromaFormatIDC == 3 {
			cpl := &fs.pic.Planes[comp+1]
			if cpl.BitDepth == 10 {
				prediction.MCBlockLumaHigh(cpl.Data16[cpl.Index(cx, cy):], cpl.Stride, &ref.Planes[comp+1], cx, cy, w, h, int(mv.X), int(mv.Y))
			} else {
				prediction.MCBlockLuma(cpl.Data[cpl.Index(cx, cy):], cpl.Stride, &ref.Planes[comp+1], cx, cy, w, h, int(mv.X), int(mv.Y))
			}
			continue
		}
		cpl := &fs.pic.Planes[comp+1]
		if cpl.BitDepth == 10 {
			prediction.MCBlockChromaFormatHigh(cpl.Data16[cpl.Index(cx, cy):], cpl.Stride, &ref.Planes[comp+1], cx, cy, w/fs.subW, h/fs.subH, int(mv.X), chromaY, fs.subW, fs.subH)
		} else {
			prediction.MCBlockChromaFormat(cpl.Data[cpl.Index(cx, cy):], cpl.Stride, &ref.Planes[comp+1], cx, cy, w/fs.subW, h/fs.subH, int(mv.X), chromaY, fs.subW, fs.subH)
		}
	}
}
