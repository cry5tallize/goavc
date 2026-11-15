package encoder

import (
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/internal/picture"
)

func TestBResidual_AdaptiveDeadZoneRetainsOriginalCandidate(t *testing.T) {
	selected := [2]int{}
	for _, qp := range []int{0, 18, 26, 34, 51} {
		cfg := h264.DefaultEncoderConfig()
		cfg.Width, cfg.Height, cfg.QP = 16, 16, qp
		cfg.Profile = h264.ProfileHigh
		e, err := New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		for pattern := 0; pattern < 16; pattern++ {
			src, _ := picture.New(1, 1, h264.YUV420P)
			for comp := range src.Planes {
				p := &src.Planes[comp]
				for y := 0; y < p.Height; y++ {
					for x := 0; x < p.Width; x++ {
						p.SetPix(x, y, byte(128+(x*17+y*29+comp*13)%(pattern+1)-pattern/2))
					}
				}
			}
			fresh := func() *frameState {
				s, err := e.newFrameState(src)
				if err != nil {
					t.Fatal(err)
				}
				s.reference = src // 只需标记帧间语法，预测已固定为128。
				s.info[0].inter = true
				for i := range s.info[0].modes {
					s.info[0].modes[i] = -1
				}
				for comp := range s.pic.Planes {
					p := &s.pic.Planes[comp]
					for y := 0; y < p.Height; y++ {
						for x := 0; x < p.Width; x++ {
							p.SetPix(x, y, 128)
						}
					}
				}
				return s
			}
			adaptive := fresh()
			res, err := adaptive.bResidualRD(0, qp)
			if err != nil {
				t.Fatal(err)
			}
			winner, err := adaptive.captureRD(0, qp, qp, res)
			if err != nil {
				t.Fatal(err)
			}
			selected[flag(winner.info.interDeadZone)]++
			// 分别从同一预测独立生成两种候选，验证全局选优而非强制用低码量者。
			for _, deadZone := range []bool{false, true} {
				s := fresh()
				s.info[0].interDeadZone = deadZone
				r := s.interResidual(0)
				s.info[0].skip = s.info[0].cbp == 0
				if !s.info[0].skip {
					s.reconstructInter(0, &r)
				}
				c, err := s.captureRD(0, qp, qp, r)
				if err != nil {
					t.Fatal(err)
				}
				if winner.cost > c.cost {
					t.Fatalf("QP%d图案%d丢失较低RD候选：%d > %d", qp, pattern, winner.cost, c.cost)
				}
			}
		}
		e.Close()
	}
	if selected[0] == 0 || selected[1] == 0 {
		t.Fatal("未实际覆盖两种死区获胜", selected)
	}
}
