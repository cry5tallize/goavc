package encoder

import (
	"testing"

	h264 "github.com/cry5tallize/goavc"
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
		trials := e.trialWriters
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
				if e.trialWriters != trials {
					t.Fatal("跨帧更换试写缓冲")
				}
				if field {
					for _, s := range e.last.fields {
						if s.searchWorkspace != lane || s.trialWriters != trials {
							t.Fatal("串行场通道未借用同一执行lane")
						}
					}
				} else if e.last.searchWorkspace != lane || e.last.trialWriters != trials {
					t.Fatal("真实帧状态未接入持久工作区")
				}
			}
			if err := e.Reset(); err != nil || e.searchWorkspace != lane || e.trialWriters != trials || trials.rd.BitsWritten() != 0 || trials.mb.BitsWritten() != 0 {
				t.Fatalf("Reset %d: %v", round, err)
			}
		}
		if err := e.Close(); err != nil || e.searchWorkspace != nil || e.trialWriters != nil {
			t.Fatalf("Close未释放私有工作区: %v", err)
		}
	}
}
