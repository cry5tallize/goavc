package encoder

import (
	"fmt"
	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/internal/motion"
	"github.com/goavc/h264/internal/picture"
)

func (e *Encoder) newFrameState(src *picture.Picture) (*frameState, error) {
	pic, err := picture.New(src.MBWidth, src.MBHeight)
	if err != nil {
		return nil, err
	}
	total := src.MBWidth * src.MBHeight
	s := &frameState{src: src, pic: pic, info: make([]mbInfo, total), nz: make([]uint8, total*16), mv: motion.NewGrid(src.MBWidth, src.MBHeight)}
	s.allowZeroPCM = e.cfg.Profile == h264.ProfileHigh
	s.maxLevelPrefix = 15
	if e.cabac || e.cfg.Profile == h264.ProfileHigh {
		s.maxLevelPrefix = 28
	}
	for c := range s.dcCoded {
		s.dcCoded[c] = make([]bool, total)
	}
	s.mvd = make([]motion.MV, total*16)
	for c := range s.cnz {
		s.cnz[c] = make([]uint8, total*4)
	}
	// 所有回调值在写入之前校验；失败不得更新参考帧、帧号和输出槽。
	for addr := range s.info {
		qp := e.cfg.QP
		if e.qpAt != nil {
			qp = e.qpAt(addr%src.MBWidth, addr/src.MBWidth)
		}
		if qp < 0 || qp > 51 {
			return nil, fmt.Errorf("encoder: macroblock qp %d: %w", qp, h264.ErrInvalidData)
		}
		qp = activityQP(&src.Planes[0], addr%src.MBWidth*16, addr/src.MBWidth*16, qp, e.adaptiveQP)
		s.info[addr].qp = qp
	}
	return s, nil
}
