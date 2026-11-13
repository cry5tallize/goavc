package decoder

import (
	"fmt"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/internal/dpb"
	"github.com/goavc/h264/internal/filter"
	"github.com/goavc/h264/internal/motion"
	"github.com/goavc/h264/internal/transform"
)

// deblock 对整个帧做去块滤波（8.7）。
//
// 顺序与规范一致：按宏块光栅顺序，先左边界（若存在）再内部垂直边，
// 然后上边界与内部水平边；每条边按 4x4 段计算独立 bS。这样后续
// 边界看到的是已经过滤过的像素，与参考实现一致。
func (fs *frameState) deblock() error {
	for mby := 0; mby < fs.mbH; mby++ {
		for mbx := 0; mbx < fs.mbW; mbx++ {
			info := &fs.info[mby*fs.mbW+mbx]
			if info.deblockIDC == 1 {
				continue
			}
			// 垂直边。
			for _, edgeX := range []int{0, 4, 8, 12} {
				boundary := edgeX == 0
				if boundary {
					if mbx == 0 || fs.skipSliceBoundary(mbx, mby, mbx-1, mby) {
						continue
					}
				}
				if err := fs.filterLumaVertical(mbx, mby, edgeX, boundary); err != nil {
					return err
				}
				if edgeX == 0 || edgeX == 8 {
					if err := fs.filterChromaVertical(mbx, mby, edgeX, boundary); err != nil {
						return err
					}
				}
			}
			// 水平边。
			for _, edgeY := range []int{0, 4, 8, 12} {
				boundary := edgeY == 0
				if boundary {
					if mby == 0 || fs.skipSliceBoundary(mbx, mby, mbx, mby-1) {
						continue
					}
				}
				if err := fs.filterLumaHorizontal(mbx, mby, edgeY, boundary); err != nil {
					return err
				}
				if edgeY == 0 || edgeY == 8 {
					if err := fs.filterChromaHorizontal(mbx, mby, edgeY, boundary); err != nil {
						return err
					}
				}
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

func (fs *frameState) filterLumaVertical(mbx, mby, edgeX int, boundary bool) error {
	pl := &fs.pic.Planes[0]
	baseX := mbx * 16
	for r := 0; r < 4; r++ {
		pbx, pby := mbx*4+edgeX/4-1, mby*4+r
		qbx, qby := pbx+1, pby
		bS := fs.bSAtBlocks(pbx, pby, qbx, qby, boundary)
		if bS <= 0 {
			continue
		}
		qp := fs.edgeQP(mbx, mby, boundary, edgeX == 0)
		info := &fs.info[mby*fs.mbW+mbx]
		x := baseX + edgeX
		y := mby*16 + r*4
		filter.LumaEdge(pl.Data, pl.Index(x, y), pl.Stride, 1, bS, qp,
			int(info.alphaDiv2), int(info.betaDiv2))
	}
	return nil
}

func (fs *frameState) filterLumaHorizontal(mbx, mby, edgeY int, boundary bool) error {
	pl := &fs.pic.Planes[0]
	for c := 0; c < 4; c++ {
		pbx, pby := mbx*4+c, mby*4+edgeY/4-1
		qbx, qby := pbx, pby+1
		bS := fs.bSAtBlocks(pbx, pby, qbx, qby, boundary)
		if bS <= 0 {
			continue
		}
		qp := fs.edgeQP(mbx, mby, boundary, false)
		info := &fs.info[mby*fs.mbW+mbx]
		x := mbx*16 + c*4
		y := mby*16 + edgeY
		filter.LumaEdge(pl.Data, pl.Index(x, y), 1, pl.Stride, bS, qp,
			int(info.alphaDiv2), int(info.betaDiv2))
	}
	return nil
}

func (fs *frameState) filterChromaVertical(mbx, mby, lumaEdgeX int, boundary bool) error {
	chromaX := lumaEdgeX / 2
	info := &fs.info[mby*fs.mbW+mbx]
	for r := 0; r < 4; r++ {
		pbx, pby := mbx*4+lumaEdgeX/4-1, mby*4+r
		qbx, qby := pbx+1, pby
		bS := fs.bSAtBlocks(pbx, pby, qbx, qby, boundary)
		if bS <= 0 {
			continue
		}
		for comp := 0; comp < 2; comp++ {
			qpc := fs.edgeChromaQP(mbx, mby, boundary, boundary, comp)
			pl := &fs.pic.Planes[comp+1]
			x := mbx*8 + chromaX
			y := mby*8 + r*2
			filter.ChromaEdge(pl.Data, pl.Index(x, y), pl.Stride, 1, bS, qpc,
				int(info.alphaDiv2), int(info.betaDiv2), 2)
		}
	}
	return nil
}

func (fs *frameState) filterChromaHorizontal(mbx, mby, lumaEdgeY int, boundary bool) error {
	chromaY := lumaEdgeY / 2
	// 水平边界的另一侧在上方，不是左侧；这里必须传 false。
	info := &fs.info[mby*fs.mbW+mbx]
	for c := 0; c < 4; c++ {
		pbx, pby := mbx*4+c, mby*4+lumaEdgeY/4-1
		qbx, qby := pbx, pby+1
		bS := fs.bSAtBlocks(pbx, pby, qbx, qby, boundary)
		if bS <= 0 {
			continue
		}
		for comp := 0; comp < 2; comp++ {
			qpc := fs.edgeChromaQP(mbx, mby, boundary, false, comp)
			pl := &fs.pic.Planes[comp+1]
			x := mbx*8 + c*2
			y := mby*8 + chromaY
			filter.ChromaEdge(pl.Data, pl.Index(x, y), 1, pl.Stride, bS, qpc,
				int(info.alphaDiv2), int(info.betaDiv2), 2)
		}
	}
	return nil
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
	cur := transform.ChromaQP(fs.info[mby*fs.mbW+mbx].qp, off)
	if !boundary {
		return cur
	}
	var otherQP int
	if left {
		otherQP = fs.info[mby*fs.mbW+mbx-1].qp
	} else {
		otherQP = fs.info[(mby-1)*fs.mbW+mbx].qp
	}
	return (cur + transform.ChromaQP(otherQP, off) + 1) >> 1
}

// edgeQP 返回边界使用的 QP。MB 边界取两侧宏块 QP 的平均，内部边取当前 MB。
func (fs *frameState) edgeQP(mbx, mby int, boundary, left bool) int {
	cur := fs.info[mby*fs.mbW+mbx].qp
	if !boundary {
		return cur
	}
	var other int
	if left {
		other = fs.info[mby*fs.mbW+mbx-1].qp
	} else {
		other = fs.info[(mby-1)*fs.mbW+mbx].qp
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
	pNZ := fs.lumaNZ[pAddr*16+(pby%4)*4+pbx%4]
	qNZ := fs.lumaNZ[qAddr*16+(qby%4)*4+qbx%4]
	if pNZ != 0 || qNZ != 0 {
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

// B 边界允许两侧参考列表互换；若两方向恰好引用同一图像，任一成对
// 匹配即可，不能只比较 list0 或把不同 slice 的 ref_idx 视为图像身份。
func (fs *frameState) bMotionStrength(px, py, qx, qy int) int {
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
			if pIDs[l] != nil && (absInt(int(pm[l].X-qm[other].X)) >= 4 || absInt(int(pm[l].Y-qm[other].Y)) >= 4) {
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
