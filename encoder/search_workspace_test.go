package encoder

import (
	"testing"

	h264 "github.com/goavc/h264"
)

func TestWorkspace_EncoderReusesSearchLane(t *testing.T) {
	for _, field := range []bool{false, true} {
		cfg := h264.DefaultEncoderConfig()
		cfg.Width, cfg.Height, cfg.Profile = 32, 32, h264.ProfileHigh
		options := []Option{WithSearchRange(2)}
		if field {
			options = append(options, WithInterlaced(true))
		}
		e, err := New(cfg, options...)
		if err != nil {
			t.Fatal(err)
		}
		f := testFrame(32, 32)
		defer f.Release()
		lane := e.searchWorkspace
		for round := range 2 {
			for range 2 {
				if err := e.SendFrame(f); err != nil {
					t.Fatal(err)
				}
				if _, err := e.ReceivePacket(); err != nil {
					t.Fatal(err)
				}
				if e.searchWorkspace != lane {
					t.Fatal("跨帧更换了搜索工作区")
				}
				if field {
					for _, s := range e.last.fields {
						if s.searchWorkspace != lane {
							t.Fatal("串行场通道未借用同一执行lane")
						}
					}
				} else if e.last.searchWorkspace != lane {
					t.Fatal("真实帧状态未接入持久工作区")
				}
			}
			if err := e.Reset(); err != nil || e.searchWorkspace != lane {
				t.Fatalf("Reset %d: %v", round, err)
			}
		}
		if err := e.Close(); err != nil || e.searchWorkspace != nil {
			t.Fatalf("Close未释放私有工作区: %v", err)
		}
	}
}
