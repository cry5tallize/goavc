package decoder

import (
	"fmt"
	"github.com/goavc/h264/internal/entropy/cavlc"
	"github.com/goavc/h264/internal/motion"
	"github.com/goavc/h264/internal/picture"
	"github.com/goavc/h264/internal/prediction"
	"github.com/goavc/h264/internal/transform"
)

// decodeResiduals 按语法顺序解码一个宏块的亮度/色度 level。
func (fs *frameState) decodeResiduals(s *sliceEntry, addr, mbx, mby int, i16, lumaAC bool, cbp int, res *residual) error {
	if s.cabac != nil {
		return fs.decodeCABACResidual(s, addr, mbx, mby, i16, lumaAC, cbp, res)
	}
	r := s.data
	if i16 {
		bx, by := 0, 0
		nC := fs.lumaNC(mbx, mby, bx, by)
		if _, err := cavlc.DecodeBlock(r, &res.lumaDC, cavlc.LumaDC, nC); err != nil {
			return fmt.Errorf("luma dc: %w", err)
		}
		if lumaAC {
			for i8 := 0; i8 < 4; i8++ {
				for i4 := 0; i4 < 4; i4++ {
					raster := blockRaster(i4 + 4*i8)
					nC := fs.lumaNC(mbx, mby, raster%4, raster/4)
					n, err := cavlc.DecodeBlock(r, &res.luma[raster], cavlc.LumaAC, nC)
					if err != nil {
						return fmt.Errorf("luma ac block %d: %w", raster, err)
					}
					fs.lumaNZ[addr*16+raster] = uint8(n)
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
				n, err := cavlc.DecodeBlock(r, &res.luma[raster], cavlc.Luma4x4, nC)
				if err != nil {
					return fmt.Errorf("luma block %d: %w", raster, err)
				}
				fs.lumaNZ[addr*16+raster] = uint8(n)
			}
		}
	}

	chromaCBP := (cbp >> 4) & 3
	if chromaCBP == 0 {
		return nil
	}
	for comp := 0; comp < 2; comp++ {
		var tmp [16]int32
		_, err := cavlc.DecodeBlock(r, &tmp, cavlc.ChromaDC, 0)
		if err != nil {
			return fmt.Errorf("chroma dc %d: %w", comp, err)
		}
		copy(res.chromaDC[comp][:], tmp[:4])
	}
	if chromaCBP != 2 {
		return nil
	}
	for comp := 0; comp < 2; comp++ {
		for b := 0; b < 4; b++ {
			nC := fs.chromaNC(comp, mbx, mby, b%2, b/2)
			n, err := cavlc.DecodeBlock(r, &res.chroma[comp][b], cavlc.LumaAC, nC)
			if err != nil {
				return fmt.Errorf("chroma ac comp %d block %d: %w", comp, b, err)
			}
			fs.chromaNZ[comp][addr*4+b] = uint8(n)
		}
	}
	return nil
}

// reconstructIntra4x4 按规范块序预测并重建，右上样本不得提前可用。
func (fs *frameState) reconstructIntra4x4(mbx, mby, cbp, qp int, res *residual) error {
	plane := &fs.pic.Planes[0]
	info := &fs.info[mby*fs.mbW+mbx]
	for n := 0; n < 16; n++ {
		raster := blockRaster(n)
		bx, by := raster%4, raster/4
		mode := int(info.intraModes[raster])
		samples, topAvail, leftAvail := fs.intra4Samples(mbx, mby, bx, by)
		x, y := mbx*16+bx*4, mby*16+by*4
		dst := plane.Data[plane.Index(x, y):]
		if err := prediction.Intra4x4(dst, plane.Stride, mode, samples, topAvail, leftAvail); err != nil {
			return err
		}
		i8 := (by/2)*2 + bx/2
		if cbp&(1<<i8) != 0 {
			if err := addResidual4x4(plane, x, y, &res.luma[raster], qp, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

// reconstructI16 预测 16x16，并加入 DC/AC 残差。
func (fs *frameState) reconstructI16(mbx, mby, predMode, chromaMode int, lumaAC bool, qp int, res *residual) error {
	plane := &fs.pic.Planes[0]
	samples, topAvail, leftAvail := fs.intra16Samples(mbx, mby)
	x, y := mbx*16, mby*16
	dst := plane.Data[plane.Index(x, y):]
	if err := prediction.Intra16x16(dst, plane.Stride, predMode, samples, topAvail, leftAvail); err != nil {
		return err
	}
	var dc [16]int32
	if err := transform.DequantLumaDC(&dc, &res.lumaDC, qp); err != nil {
		return err
	}
	for raster := 0; raster < 16; raster++ {
		var coeff [16]int32
		if lumaAC {
			if err := transform.Dequant4x4(&coeff, &res.luma[raster], qp); err != nil {
				return err
			}
		}
		coeff[0] = dc[raster]
		bx, by := raster%4, raster/4
		if err := addResidual4x4(plane, mbx*16+bx*4, mby*16+by*4, &coeff, qp, &coeff); err != nil {
			return err
		}
	}
	// 色度预测。
	for comp := 0; comp < 2; comp++ {
		pl := &fs.pic.Planes[comp+1]
		cs, ct, cl := fs.chromaSamples(mbx, mby, comp)
		cx, cy := mbx*8, mby*8
		cdst := pl.Data[pl.Index(cx, cy):]
		if err := prediction.Chroma8x8(cdst, pl.Stride, chromaMode, cs, ct, cl); err != nil {
			return err
		}
	}
	return nil
}

// predictChroma 对两个色度分量做 8x8 帧内预测。
func (fs *frameState) predictChroma(mbx, mby, mode int) error {
	for comp := 0; comp < 2; comp++ {
		pl := &fs.pic.Planes[comp+1]
		cs, ct, cl := fs.chromaSamples(mbx, mby, comp)
		dst := pl.Data[pl.Index(mbx*8, mby*8):]
		if err := prediction.Chroma8x8(dst, pl.Stride, mode, cs, ct, cl); err != nil {
			return err
		}
	}
	return nil
}

// addLumaResidual 把非 I16x16 的亮度残差加到已预测像素上。
func (fs *frameState) addLumaResidual(mbx, mby, cbp int, i16 bool, qp int, res *residual) error {
	if i16 {
		return nil
	}
	plane := &fs.pic.Planes[0]
	for raster := 0; raster < 16; raster++ {
		bx, by := raster%4, raster/4
		i8 := (by/2)*2 + bx/2
		if cbp&(1<<i8) == 0 {
			continue
		}
		if err := addResidual4x4(plane, mbx*16+bx*4, mby*16+by*4, &res.luma[raster], qp, nil); err != nil {
			return err
		}
	}
	return nil
}

// addChromaResidual 反量化色度 DC/AC 并加到预测像素。
func (fs *frameState) addChromaResidual(mbx, mby, chromaCBP, qp int, res *residual) error {
	if chromaCBP == 0 {
		return nil
	}
	offsets := [2]int{int(fs.pps.ChromaQPIndexOffset), int(fs.pps.SecondChromaQPIndexOffset)}
	for comp := 0; comp < 2; comp++ {
		qpc := transform.ChromaQP(qp, offsets[comp])
		var dc [4]int32
		if err := transform.DequantChromaDC(&dc, &res.chromaDC[comp], qpc); err != nil {
			return err
		}
		pl := &fs.pic.Planes[comp+1]
		for b := 0; b < 4; b++ {
			var coeff [16]int32
			if chromaCBP == 2 {
				if err := transform.Dequant4x4(&coeff, &res.chroma[comp][b], qpc); err != nil {
					return err
				}
			}
			coeff[0] = dc[b]
			bx, by := b%2, b/2
			if err := addResidual4x4(pl, mbx*8+bx*4, mby*8+by*4, &coeff, qpc, &coeff); err != nil {
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
func addResidual4x4(pl *picture.Plane, x, y int, levels *[16]int32, qp int, alreadyDequant *[16]int32) error {
	var dequant [16]int32
	src := levels
	if alreadyDequant == nil {
		if err := transform.Dequant4x4(&dequant, levels, qp); err != nil {
			return err
		}
		src = &dequant
	}
	var out [16]int32
	transform.IDCT4x4(&out, src)
	for j := 0; j < 4; j++ {
		for i := 0; i < 4; i++ {
			px := int(pl.Pix(x+i, y+j)) + int(out[j*4+i])
			pl.SetPix(x+i, y+j, clip8(px))
		}
	}
	return nil
}

// mcPartition 做一次亮度+色度运动补偿并写入当前图像。
func (fs *frameState) mcPartition(ref *picture.Picture, mbx, mby, xOff, yOff, w, h int, mv motion.MV) {
	lx, ly := mbx*16+xOff, mby*16+yOff
	pl := &fs.pic.Planes[0]
	dst := pl.Data[pl.Index(lx, ly):]
	prediction.MCBlockLuma(dst, pl.Stride, &ref.Planes[0], lx, ly, w, h, int(mv.X), int(mv.Y))
	cx, cy := mbx*8+xOff/2, mby*8+yOff/2
	for comp := 0; comp < 2; comp++ {
		cpl := &fs.pic.Planes[comp+1]
		cdst := cpl.Data[cpl.Index(cx, cy):]
		prediction.MCBlockChroma(cdst, cpl.Stride, &ref.Planes[comp+1], cx, cy, w/2, h/2, int(mv.X), int(mv.Y))
	}
}
