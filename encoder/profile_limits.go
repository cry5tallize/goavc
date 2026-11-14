package encoder

import (
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/internal/entropy/cavlc"
	"github.com/goavc/h264/internal/transform"
)

func (s *frameState) pcmAllowed(addr int) bool {
	if s.allowZeroPCM {
		return true
	}
	// Spec: Annex A.2.1/A.2.2，Baseline/Main 的 PCM 采样不可等于0。
	for comp := range s.planeCount() {
		mw, mh := s.pic.Format.PlaneDimensions(comp, 16, 16)
		x, y := addr%s.pic.MBWidth*mw, addr/s.pic.MBWidth*mh
		for j := range mh {
			for i := range mw {
				v := s.src.Planes[comp].Sample(x+i, y+j)
				if v == 0 {
					return false
				}
			}
		}
	}
	return true
}

func (s *frameState) encodeResidualBlock(w *bitstream.Writer, src *[16]int32, kind cavlc.BlockType, nc int) (int, error) {
	limit := s.maxLevelPrefix
	if limit == 0 {
		limit = 28
	}
	if s.field {
		scan := s.scan4()
		switch kind {
		case cavlc.LumaDC:
			scan = s.scanDC()
		case cavlc.LumaAC:
			scan = scan[1:]
		case cavlc.ChromaDC:
			scan = transform.DC2x2Scan[:]
		case cavlc.ChromaDC422:
			scan = transform.DC2x4Scan[:]
		}
		return cavlc.EncodeBlockLimitedScan(w, src, kind, nc, limit, scan)
	}
	return cavlc.EncodeBlockLimited(w, src, kind, nc, limit)
}
