package decoder

import (
	"fmt"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/internal/dpb"
	"github.com/cry5tallize/goavc/internal/filter"
	"github.com/cry5tallize/goavc/internal/motion"
	"github.com/cry5tallize/goavc/internal/transform"
)

// deblock 对整个帧做去块滤波（8.7）。
//
// 顺序与规范一致：按宏块光栅顺序，先左边界（若存在）再内部垂直边，
// 然后上边界与内部水平边；每条边按 4x4 段计算独立 bS。这样后续
// 边界看到的是已经过滤过的像素，与参考实现一致。
func (fs *frameState) deblockPlane() error {
	for mby := 0; mby < fs.mbH; mby++ {
		for mbx := 0; mbx < fs.mbW; mbx++ {
			if err := fs.deblockMB(mbx, mby); err != nil {
				return err
			}
		}
	}
	return nil
}

func (fs *frameState) deblockMB(mbx, mby int) error {
	info := &fs.info[mby*fs.mbW+mbx]
	if info.deblockIDC == 1 {
		return nil
	}
	for dir := range 2 {
		vertical := dir == 0
		sub := fs.subH
		if vertical {
			sub = fs.subW
		}
		for edge := 0; edge < 16; edge += 4 {
			boundary := edge == 0
			if boundary && (vertical && (mbx == 0 || fs.skipSliceBoundary(mbx, mby, mbx-1, mby)) ||
				!vertical && (mby == 0 || fs.skipSliceBoundary(mbx, mby, mbx, mby-1))) {
				continue
			}
			luma := !info.transform8 || edge%8 == 0
			chroma := (fs.sps == nil || fs.sps.ChromaFormatIDC != 3) && edge%(4*sub) == 0
			if !luma && !chroma {
				continue
			}
			// bS只依赖元数据，跨分量复用不会改变相邻边的像素读写顺序。
			strengths := fs.edgeStrengths(mbx, mby, edge, vertical)
			if strengths == [4]uint8{} {
				continue
			}
			if luma {
				fs.filterLumaEdge(mbx, mby, edge, vertical, strengths)
			}
			if chroma {
				fs.filterChromaEdge(mbx, mby, edge, vertical, strengths)
			}
		}
	}
	return nil
}

// skipSliceBoundary 在 disable_deblocking_filter_idc==2 时跳过跨 slice 边。
func (fs *frameState) skipSliceBoundary(mbx, mby, nbx, nby int) bool {
	info := &fs.info[mby*fs.mbW+mbx]
	if info.deblockIDC != 2 {
		return false
	}
	other := &fs.info[nby*fs.mbW+nbx]
	return info.sliceID != other.sliceID
}

// edgeStrengths 每条边只推导一次四段bS。帧内/PCM的强度与非零系数、MV无关；
// 此入口也供MBAFF的纯帧邻域复用，混合帧/场边仍由mbaff_deblock.go独立定位。
func (fs *frameState) edgeStrengths(mbx, mby, edge int, vertical bool) (strengths [4]uint8) {
	addr := mby*fs.mbW + mbx
	intra := fs.info[addr].intra || fs.info[addr].pcm
	if edge == 0 {
		other := addr - fs.mbW
		if vertical {
			other = addr - 1
		}
		intra = intra || fs.info[other].intra || fs.info[other].pcm
	}
	if intra {
		bs := uint8(3)
		if edge == 0 {
			bs = 4
		}
		return [4]uint8{bs, bs, bs, bs}
	}
	for seg := range 4 {
		px, py := mbx*4+edge/4-1, mby*4+seg
		qx, qy := px+1, py
		if !vertical {
			px, py = mbx*4+seg, mby*4+edge/4-1
			qx, qy = px, py+1
		}
		strengths[seg] = uint8(fs.bSAtBlocks(px, py, qx, qy, edge == 0))
	}
	return
}

func (fs *frameState) filterLumaEdge(mbx, mby, edge int, vertical bool, strengths [4]uint8) {
	pl := &fs.pic.Planes[fs.component]
	info := &fs.info[mby*fs.mbW+mbx]
	qp := fs.edgeQP(mbx, mby, edge == 0, vertical)
	params := filter.PrepareEdge(qp, int(info.alphaDiv2), int(info.betaDiv2), pl.BitDepth)
	x, y, along, across := mbx*16+edge, mby*16, pl.Stride, 1
	if !vertical {
		x, y, along, across = mbx*16, mby*16+edge, 1, pl.Stride
	}
	if pl.BitDepth == 10 {
		params.Luma16(pl.Data16, pl.Index(x, y), along, across, strengths)
	} else {
		params.Luma8(pl.Data, pl.Index(x, y), along, across, strengths)
	}
}

func (fs *frameState) filterChromaEdge(mbx, mby, edge int, vertical bool, strengths [4]uint8) {
	info := &fs.info[mby*fs.mbW+mbx]
	for comp := range 2 {
		pl := &fs.pic.Planes[comp+1]
		qp := fs.edgeChromaQP(mbx, mby, edge == 0, vertical, comp)
		params := filter.PrepareEdge(qp, int(info.alphaDiv2), int(info.betaDiv2), pl.BitDepth)
		x, y, along, across, lines := mbx*fs.chromaW, mby*fs.chromaH, pl.Stride, 1, 4/fs.subH
		if vertical {
			x += edge / fs.subW
		} else {
			y += edge / fs.subH
			along, across, lines = 1, pl.Stride, 4/fs.subW
		}
		if pl.BitDepth == 10 {
			params.Chroma16(pl.Data16, pl.Index(x, y), along, across, strengths, lines)
		} else if fs.subW == 2 && fs.subH == 2 && !fs.mbaff && !fs.field {
			params.Chroma4208(pl.Data, pl.Index(x, y), along, across, strengths)
		} else {
			params.Chroma8(pl.Data, pl.Index(x, y), along, across, strengths, lines)
		}
	}
}

// edgeChromaQP 返回色度边界使用的 QP。
//
// 注意不能先用亮度 QP 求平均再查色度表：ChromaQP 是非线性映射，
// 与“两个色度 QP 再平均”可能差 1。libavcodec 的做法是先分别映射，
// 再在 MB 边界对色度 QP 求平均。
func (fs *frameState) edgeChromaQP(mbx, mby int, boundary, left bool, comp int) int {
	off := int(fs.pps.ChromaQPIndexOffset)
	if comp == 1 {
		off = int(fs.pps.SecondChromaQPIndexOffset)
	}
	chromaQP := func(qpy int) int {
		if fs.sps == nil || fs.sps.BitDepthChromaMinus8 == 0 {
			return transform.ChromaQP(qpy, off)
		}
		// QPY及PPS偏移已由语法层校验；滤波表需要移除位深偏移。
		qpc, _ := transform.ScaleChromaQP(qpy, off, 8+int(fs.sps.BitDepthChromaMinus8))
		return qpc - 6*int(fs.sps.BitDepthChromaMinus8)
	}
	cur := chromaQP(fs.info[mby*fs.mbW+mbx].qp)
	if !boundary {
		return cur
	}
	var otherQP int
	if left {
		otherQP = fs.info[mby*fs.mbW+mbx-1].qp
	} else {
		otherQP = fs.info[(mby-1)*fs.mbW+mbx].qp
	}
	return (cur + chromaQP(otherQP) + 1) >> 1
}

// edgeQP 返回边界使用的 QP。MB 边界取两侧宏块 QP 的平均，内部边取当前 MB。
func (fs *frameState) edgeQP(mbx, mby int, boundary, left bool) int {
	cur := fs.info[mby*fs.mbW+mbx].qp
	if fs.sps != nil && fs.sps.ChromaFormatIDC == 3 {
		cur = fs.componentQP(cur)
	}
	if !boundary {
		return cur
	}
	var other int
	if left {
		other = fs.info[mby*fs.mbW+mbx-1].qp
	} else {
		other = fs.info[(mby-1)*fs.mbW+mbx].qp
	}
	if fs.sps != nil && fs.sps.ChromaFormatIDC == 3 {
		other = fs.componentQP(other)
	}
	return (cur + other + 1) >> 1
}

// bSAtBlocks 计算相邻两个 4x4 块的边界强度（8.7.2.1）。
//
// p 块在左/上，q 块在右/下；boundary 表示这是宏块边界（不是 4x4 内部边）。
// 规则：
//   - 任一侧帧内：边界 4，内部 3；
//   - 任一侧有非零系数：2；
//   - 参考或 MV 不同（|Δmv|>=4 个 1/4 像素）：1；否则 0。
func (fs *frameState) bSAtBlocks(pbx, pby, qbx, qby int, boundary bool) int {
	pAddr := (pby/4)*fs.mbW + pbx/4
	qAddr := (qby/4)*fs.mbW + qbx/4
	pInfo := &fs.info[pAddr]
	qInfo := &fs.info[qAddr]
	if pInfo.intra || qInfo.intra || pInfo.pcm || qInfo.pcm {
		if boundary {
			return 4
		}
		return 3
	}
	if fs.lumaNonzero(pAddr, pbx%4, pby%4) || fs.lumaNonzero(qAddr, qbx%4, qby%4) {
		return 2
	}
	if fs.mv1 != nil {
		return fs.bMotionStrength(pbx, pby, qbx, qby)
	}
	pmv, pref, pok := fs.mv.Get(pbx, pby)
	qmv, qref, qok := fs.mv.Get(qbx, qby)
	sameReference := pref == qref
	if fs.referenceIDs[0] != nil {
		sameReference = fs.referenceIDs[0][pby*fs.mv.BlockW+pbx] == fs.referenceIDs[0][qby*fs.mv.BlockW+qbx]
	}
	if !pok || !qok || !sameReference {
		return 1
	}
	if absInt(int(pmv.X-qmv.X)) >= 4 || absInt(int(pmv.Y-qmv.Y)) >= 4 {
		return 1
	}
	return 0
}

func (fs *frameState) lumaNonzero(addr, bx, by int) bool {
	grid := fs.lumaNZ
	if !fs.info[addr].transform8 {
		return grid[addr*16+by*4+bx] != 0
	}
	base := addr*16 + (by/2)*8 + (bx/2)*2
	return grid[base] != 0 || grid[base+1] != 0 || grid[base+4] != 0 || grid[base+5] != 0
}

// B 边界允许两侧参考列表互换；若两方向恰好引用同一图像，任一成对
// 匹配即可，不能只比较 list0 或把不同 slice 的 ref_idx 视为图像身份。
func (fs *frameState) bMotionStrength(px, py, qx, qy int) int {
	return fs.bMotionStrengthLimit(px, py, qx, qy, 4)
}

func (fs *frameState) bMotionStrengthLimit(px, py, qx, qy, verticalLimit int) int {
	pi, qi := py*fs.mv.BlockW+px, qy*fs.mv.BlockW+qx
	pIDs := [2]*dpb.Identity{fs.referenceIDs[0][pi], fs.referenceIDs[1][pi]}
	qIDs := [2]*dpb.Identity{fs.referenceIDs[0][qi], fs.referenceIDs[1][qi]}
	pMV0, _, _ := fs.mv.Get(px, py)
	pMV1, _, _ := fs.mv1.Get(px, py)
	qMV0, _, _ := fs.mv.Get(qx, qy)
	qMV1, _, _ := fs.mv1.Get(qx, qy)
	pm, qm := [2]motion.MV{pMV0, pMV1}, [2]motion.MV{qMV0, qMV1}
	for _, swap := range []int{0, 1} {
		match := true
		for l := 0; l < 2; l++ {
			other := l ^ swap
			if pIDs[l] != qIDs[other] {
				match = false
				break
			}
			if pIDs[l] != nil && (absInt(int(pm[l].X-qm[other].X)) >= 4 || absInt(int(pm[l].Y-qm[other].Y)) >= verticalLimit) {
				match = false
				break
			}
		}
		if match {
			return 0
		}
	}
	return 1
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// errInvalidDeblock 保留给未来的并行去块实现做防御性校验。
var errInvalidDeblock = fmt.Errorf("decoder: invalid deblock state: %w", h264.ErrInvalidData)
