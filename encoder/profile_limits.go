package encoder

import (
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/internal/entropy/cavlc"
)

func (s *frameState) pcmAllowed(addr int) bool {
	if s.allowZeroPCM {
		return true
	}
	// Spec: Annex A.2.1/A.2.2，Baseline/Main 的 PCM 采样不可等于0。
	for comp := 0; comp < 3; comp++ {
		n := 16
		if comp > 0 {
			n = 8
		}
		x, y := addr%s.pic.MBWidth*n, addr/s.pic.MBWidth*n
		for j := 0; j < n; j++ {
			for _, v := range s.src.Planes[comp].Row(y + j)[x : x+n] {
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
	return cavlc.EncodeBlockLimited(w, src, kind, nc, limit)
}
