package encoder

import (
	"fmt"
	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/internal/motion"
	"github.com/goavc/h264/internal/motion/search"
	"github.com/goavc/h264/internal/picture"
)

func (e *Encoder) newFrameState(src *picture.Picture) (*frameState, error) {
	if e.searchWorkspace == nil {
		e.searchWorkspace = new(search.Workspace)
	}
	if e.trialWriters == nil {
		e.trialWriters = newTrialWriters()
	}
	pic := e.planeReconstruction
	if pic == nil {
		var err error
		pic, err = picture.New(src.MBWidth, src.MBHeight, src.Format)
		if err != nil {
			return nil, err
		}
	}
	total := src.MBWidth * src.MBHeight
	s := &frameState{src: src, pic: pic, info: make([]mbInfo, total), nz: make([]uint8, total*16), mv: motion.NewGrid(src.MBWidth, src.MBHeight)}
	s.searchWorkspace = e.searchWorkspace
	s.trialWriters = e.trialWriters
	s.allowZeroPCM = e.cfg.Profile != h264.ProfileBaseline && e.cfg.Profile != h264.ProfileMain
	s.separate, s.plane = e.separateColourPlane, e.colourPlaneID
	s.independent = e.separateColourPlane
	s.allow8 = e.transform8
	s.scaling8 = e.scaling8
	s.scaling4 = e.scaling4
	if !e.encodingB && e.reference != nil && (e.autoWeights || e.fixedWeights != nil) {
		weights := e.fixedWeights
		if e.autoWeights {
			w := estimatePredictionWeights(src, e.reference, s.planeCount())
			weights = &w
		}
		s.weights = weights
		// 该副本只用于整数/亚像素搜索启发式。真正 MC 必须先在原参考上插值，
		// 再逐采样加权；裁剪与插值不可交换（Spec: 8.4.3.2）。
		s.searchReference = weightedSearchReference(e.reference, weights, s.planeCount())
	}
	s.maxLevelPrefix = 15
	if e.cabac || s.allowZeroPCM {
		s.maxLevelPrefix = 28
	}
	for c := range s.dcCoded {
		s.dcCoded[c] = make([]bool, total)
	}
	s.mvd = make([]motion.MV, total*16)
	for c := range s.cnz {
		s.cnz[c] = make([]uint8, total*16)
	}
	// 所有回调值在写入之前校验；失败不得更新参考帧、帧号和输出槽。
	for addr := range s.info {
		qp := e.frameQP
		if e.qpAt != nil {
			qp = e.qpAt(addr%src.MBWidth, addr/src.MBWidth)
		}
		if qp < -6*(src.Planes[0].BitDepth-8) || qp > 51 {
			return nil, fmt.Errorf("encoder: macroblock qp %d: %w", qp, h264.ErrInvalidData)
		}
		qp = activityQP(&src.Planes[0], addr%src.MBWidth*16, addr/src.MBWidth*16, qp, e.adaptiveQP)
		s.info[addr].qp = qp
	}
	return s, nil
}
