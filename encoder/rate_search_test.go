package encoder

import (
	"errors"
	"testing"

	h264 "github.com/goavc/h264"
)

func TestQPSearch_LocalBracketAndColdFallback(t *testing.T) {
	for _, known := range []bool{false, true} {
		for seed := 0; seed <= 51; seed++ {
			for boundary := 0; boundary <= 52; boundary++ {
				s := newQPSearch(seed, known)
				seen := [52]bool{}
				for range 7 {
					qp, more := s.next()
					if !more {
						break
					}
					if qp < 0 || qp > 51 || seen[qp] {
						t.Fatalf("搜索越界或重复：seed%d boundary%d QP%d", seed, boundary, qp)
					}
					seen[qp] = true
					s.observe(qp < boundary)
				}
				if boundary <= 51 && !seen[boundary] {
					// 有界快速阶段允许尚未夹紧远处目标，但不能错误地宣布无剩余QP。
					if _, more := s.next(); !more {
						t.Fatalf("跳过了可行边界QP%d：seed%d", boundary, seed)
					}
				}
			}
		}
	}
	// 稳态目标在QP39，已有反馈39：只需39/38两个相邻候选夹紧，
	// 而冷启动仍覆盖完整区间，不直接信任调用方提供的未知seed。
	warm := newQPSearch(39, true)
	for _, want := range []int{39, 38} {
		qp, more := warm.next()
		if !more || qp != want {
			t.Fatalf("稳态探测%v/%d，期望%d", more, qp, want)
		}
		warm.observe(qp < 39)
	}
	if _, more := warm.next(); more {
		t.Fatal("相邻QP已夹紧后仍重复编码")
	}
	cold := newQPSearch(51, false)
	if qp, more := cold.next(); !more || qp != 25 {
		t.Fatal("新帧型未从完整区间开始", qp, more)
	}
}

func TestRateSearch_FeedbackIsTransactionalAndReset(t *testing.T) {
	c := h264.DefaultEncoderConfig()
	c.Width, c.Height = 16, 16
	c.RateControl, c.Bitrate = h264.RateControlABR, 30_000
	e, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	f := testFrame(16, 16)
	defer f.Release()
	if err = e.SendFrame(f); err != nil {
		t.Fatal(err)
	}
	e.ReceivePacket()
	if !e.rateQPKnown[0] || e.rateQPKnown[1] || e.rateQPKnown[2] {
		t.Fatal("帧型反馈没有隔离", e.rateQPKnown)
	}
	before, known := e.rateQP, e.rateQPKnown
	e.maxAUBytes = 1
	if err = e.SendFrame(f); !errors.Is(err, h264.ErrResourceLimit) {
		t.Fatal("未触发失败事务", err)
	}
	if e.rateQP != before || e.rateQPKnown != known {
		t.Fatal("失败QP尝试污染后续反馈")
	}
	if err = e.Reset(); err != nil {
		t.Fatal(err)
	}
	if e.rateQP != [3]int{} || e.rateQPKnown != [3]bool{} {
		t.Fatal("Reset遗留码控历史")
	}
}
