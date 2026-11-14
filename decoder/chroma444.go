package decoder

import (
	"fmt"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/internal/transform"
)

// fullPlaneCount 对共同编码444处理三平面；独立平面每个slice只处理colour_plane_id。
func (fs *frameState) fullPlaneCount() int {
	if fs.sps.ChromaFormatIDC == 3 && !fs.sps.SeparateColourPlaneFlag {
		return 3
	}
	return 1
}

func (fs *frameState) planeNZ() []uint8 {
	if fs.component == 0 || fs.sps.SeparateColourPlaneFlag {
		return fs.lumaNZ
	}
	return fs.chromaNZ[fs.component-1]
}

func (fs *frameState) planeResidual(res *residual) *planeResidual {
	if fs.component == 0 || fs.sps.SeparateColourPlaneFlag {
		return &res.planeResidual
	}
	return &fs.fullResidual[fs.component-1]
}

// Spec: 8.5.7，共同编码444仍使用色度QP映射；重建内核接收移除位深偏移的语法域。
// 独立平面按单色过程使用QPY，但缩放列表仍由colour_plane_id选择。
func (fs *frameState) componentQP(qp int) int {
	if fs.component == 0 || fs.sps.SeparateColourPlaneFlag {
		return qp
	}
	offset := int(fs.pps.ChromaQPIndexOffset)
	if fs.component == 2 {
		offset = int(fs.pps.SecondChromaQPIndexOffset)
	}
	qpc, _ := transform.ScaleChromaQP(qp, offset, 8+int(fs.sps.BitDepthChromaMinus8))
	return qpc - 6*int(fs.sps.BitDepthChromaMinus8)
}

func (fs *frameState) decodeResiduals(s *sliceEntry, addr, mbx, mby int, i16, lumaAC bool, cbp int, res *residual) error {
	first := fs.component
	defer func() { fs.component = first }()
	if fs.fullResidual != nil {
		*fs.fullResidual = [2]planeResidual{}
	}
	for i := 0; i < fs.fullPlaneCount(); i++ {
		fs.component = first + i
		var err error
		if s.cabac != nil {
			err = fs.decodeCABACPlane(s, addr, mbx, mby, i16, lumaAC, cbp, fs.planeResidual(res))
		} else {
			err = fs.decodeCAVLCLuma(s, addr, mbx, mby, i16, lumaAC, cbp, fs.planeResidual(res))
		}
		if err != nil {
			return fmt.Errorf("plane %d residual: %w", fs.component, err)
		}
	}
	fs.component = first
	if fs.sps.ChromaFormatIDC == 3 {
		return nil
	}
	if s.cabac != nil {
		return fs.decodeCABACChroma(s, addr, mbx, mby, cbp, res)
	}
	return fs.decodeCAVLCChroma(s, addr, mbx, mby, cbp, res)
}

const (
	reconstruct4 = iota
	reconstruct8
	reconstruct16
	reconstructInter
)

// 每个444分量借用同一套预测/逆变换/重建，系数与像素均不复制。
func (fs *frameState) reconstructPlanes(kind, mbx, mby, cbp, qp, mode int, lumaAC bool, res *residual) error {
	first := fs.component
	defer func() { fs.component = first }()
	for i := 0; i < fs.fullPlaneCount(); i++ {
		fs.component = first + i
		qpc := fs.componentQP(qp)
		levels := fs.planeResidual(res)
		var err error
		switch kind {
		case reconstruct4:
			err = fs.reconstructPlane4(mbx, mby, cbp, qpc, levels)
		case reconstruct8:
			err = fs.reconstructPlane8(mbx, mby, cbp, qpc, levels)
		case reconstruct16:
			err = fs.reconstructPlane16(mbx, mby, mode, lumaAC, qpc, levels)
		case reconstructInter:
			err = fs.addPlaneResidual(mbx, mby, cbp, false, qpc, levels)
		default:
			return fmt.Errorf("decoder: invalid reconstruction kind: %w", h264.ErrInvalidData)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (fs *frameState) reconstructIntra4x4(mbx, mby, cbp, qp int, res *residual) error {
	return fs.reconstructPlanes(reconstruct4, mbx, mby, cbp, qp, 0, false, res)
}

func (fs *frameState) reconstructIntra8x8(mbx, mby, cbp, qp int, res *residual) error {
	return fs.reconstructPlanes(reconstruct8, mbx, mby, cbp, qp, 0, false, res)
}

func (fs *frameState) reconstructI16(mbx, mby, mode, chromaMode int, lumaAC bool, qp int, res *residual) error {
	if err := fs.reconstructPlanes(reconstruct16, mbx, mby, 0, qp, mode, lumaAC, res); err != nil {
		return err
	}
	return fs.predictChroma(mbx, mby, chromaMode)
}

func (fs *frameState) addLumaResidual(mbx, mby, cbp int, i16 bool, qp int, res *residual) error {
	if i16 {
		return nil
	}
	return fs.reconstructPlanes(reconstructInter, mbx, mby, cbp, qp, 0, false, res)
}

func (fs *frameState) deblock() error {
	if fs.sps == nil {
		return fs.deblockPlane()
	}
	first := fs.component
	defer func() { fs.component = first }()
	for i := 0; i < fs.fullPlaneCount(); i++ {
		fs.component = first + i
		if err := fs.deblockPlane(); err != nil {
			return err
		}
	}
	return nil
}
