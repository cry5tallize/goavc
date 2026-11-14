package encoder

import (
	"errors"
	h264 "github.com/goavc/h264"
	"testing"
)

func TestLookahead_BoundsResetAndFailedSend(t *testing.T) {
	cfg := h264.DefaultEncoderConfig()
	cfg.Width, cfg.Height = 32, 32
	for _, depth := range []int{0, 17} {
		if _, err := New(cfg, WithLookahead(depth)); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatal("窗口边界未拒绝", err)
		}
	}
	e, err := New(cfg, WithLookahead(1))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err = e.SendFrame(testFrame(32, 32)); err != nil {
		t.Fatal(err)
	}
	first := e.pendingFrames[0]
	budget := e.maxAUBytes
	e.maxAUBytes = 1
	if err = e.SendFrame(testFrame(32, 32)); !errors.Is(err, h264.ErrResourceLimit) {
		t.Fatal("预算失败未拒绝", err)
	}
	if len(e.pendingFrames) != 1 || e.pendingFrames[0] != first || e.reference != nil || e.out != nil {
		t.Fatal("失败Send污染窗口或编码状态")
	}
	e.maxAUBytes = budget
	if err = e.SendFrame(testFrame(32, 32)); err != nil {
		t.Fatal(err)
	}
	if err = e.SendFrame(testFrame(32, 32)); !errors.Is(err, h264.ErrAgain) {
		t.Fatal("输出满时必须背压", err)
	}
	if len(e.pendingFrames) > e.lookahead {
		t.Fatal("窗口越界")
	}
	if err = e.Reset(); err != nil {
		t.Fatal(err)
	}
	if len(e.pendingFrames) != 0 || e.lookaheadPrevious != nil || e.reference != nil || e.out != nil {
		t.Fatal("Reset未释放窗口")
	}
	if err = e.SendFrame(testFrame(32, 32)); err != nil {
		t.Fatal(err)
	}
	if err = e.Close(); err != nil {
		t.Fatal(err)
	}
	if len(e.pendingFrames) != 0 || e.lookaheadPrevious != nil {
		t.Fatal("Close未释放窗口")
	}
}
