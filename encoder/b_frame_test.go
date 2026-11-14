package encoder

import (
	"errors"
	"math"
	"testing"

	h264 "github.com/goavc/h264"
)

func TestBEncoder_ActualDirectAndSkip(t *testing.T) {
	for _, offset := range []byte{0, 17} {
		cfg := h264.DefaultEncoderConfig()
		cfg.Width, cfg.Height = 32, 32
		cfg.Profile = h264.ProfileMain
		cfg.MaxBFrames = 2
		e, err := New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		for frame := 0; frame < 4; frame++ {
			f := testFrame(32, 32)
			f.PTS = int64(frame)
			f.Duration = 1
			value := byte(100)
			if frame == 1 || frame == 2 {
				value += offset
			}
			for comp := range f.Planes {
				for i := range f.Planes[comp].Data {
					f.Planes[comp].Data[i] = value
				}
			}
			if err = e.SendFrame(f); err != nil {
				t.Fatal(err)
			}
			f.Release()
			for {
				p, err := e.ReceivePacket()
				if errors.Is(err, h264.ErrAgain) {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				if p.PTS == 1 || p.PTS == 2 {
					for addr, i := range e.last.info {
						if !i.inter || i.skip != (offset == 0) {
							t.Fatalf("偏移%d MB%d不符合Direct/Skip决策：%+v", offset, addr, i)
						}
					}
				}
			}
		}
		e.Close()
	}
}

func TestBEncoder_FailedSendRetryAndCleanup(t *testing.T) {
	cfg := h264.DefaultEncoderConfig()
	cfg.Width, cfg.Height = 32, 32
	cfg.Profile = h264.ProfileMain
	cfg.MaxBFrames = 2
	e, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	f := testFrame(32, 32)
	f.PTS = 0
	f.Duration = 100
	budget := e.maxAUBytes
	e.maxAUBytes = 1
	if err = e.SendFrame(f); !errors.Is(err, h264.ErrResourceLimit) {
		t.Fatal(err)
	}
	if e.bClockSet || e.reference != nil || e.out != nil || e.bNextIndex != 0 || len(e.bInputs) != 0 {
		t.Fatal("首帧失败污染调度状态")
	}
	e.maxAUBytes = budget
	if err = e.SendFrame(f); err != nil {
		t.Fatal(err)
	}
	e.ReceivePacket()
	for i := 1; i <= 2; i++ {
		f.PTS = int64(i) * 100
		if err = e.SendFrame(f); err != nil {
			t.Fatal(err)
		}
	}
	ref, stats := e.reference, e.RateControlStats()
	e.maxAUBytes = 1
	f.PTS = 300
	if err = e.SendFrame(f); !errors.Is(err, h264.ErrResourceLimit) {
		t.Fatal(err)
	}
	if e.reference != ref || e.RateControlStats() != stats || len(e.bInputs) != 2 || len(e.bReady) != 0 || e.bNextIndex != 3 {
		t.Fatal("未来锚点失败污染已接受队列或参考")
	}
	e.maxAUBytes = budget
	if err = e.SendFrame(f); err != nil {
		t.Fatal(err)
	}
	if err = e.SendFrame(f); !errors.Is(err, h264.ErrAgain) {
		t.Fatal("输出满时没有背压", err)
	}
	if err = e.Reset(); err != nil {
		t.Fatal(err)
	}
	if len(e.bInputs) != 0 || len(e.bReady) != 0 || e.bPast != nil || e.bFuture != nil || e.bClockSet {
		t.Fatal("Reset未清理B调度")
	}
	f.PTS = math.MinInt64
	if err = e.SendFrame(f); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal("DTS下溢未拒绝", err)
	}
	f.PTS = 0
	f.Duration = math.MaxInt64
	if err = e.SendFrame(f); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal("延迟乘法溢出未拒绝", err)
	}
	if e.bClockSet {
		t.Fatal("非法时间戳改变调度状态")
	}
}

func TestSliceQPDelta_DoesNotWrap(t *testing.T) {
	e := Encoder{cfg: h264.EncoderConfig{QP: 0}, frameQP: 51}
	if e.sliceQPDelta() != 51 {
		t.Fatal("slice_qp_delta不能按mb_qp_delta回绕")
	}
	e.cfg.QP, e.frameQP = 51, 0
	if e.sliceQPDelta() != -51 {
		t.Fatal("负slice_qp_delta错误")
	}
}

func TestBEncoder_FailedDelayedPacketIsRetryable(t *testing.T) {
	cfg := h264.DefaultEncoderConfig()
	cfg.Width, cfg.Height = 32, 32
	cfg.Profile = h264.ProfileMain
	cfg.MaxBFrames = 2
	e, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	for i := 0; i < 4; i++ {
		f := testFrame(32, 32)
		f.PTS = int64(i)
		f.Duration = 1
		if err = e.SendFrame(f); err != nil {
			t.Fatal(err)
		}
		f.Release()
		if i == 0 || i == 3 {
			if _, err = e.ReceivePacket(); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(e.bReady) != 2 {
		t.Fatal("缺待输出B组")
	}
	ref, stats, clock := e.reference, e.RateControlStats(), e.bClock
	budget := e.maxAUBytes
	e.maxAUBytes = 1
	if _, err = e.ReceivePacket(); !errors.Is(err, h264.ErrResourceLimit) {
		t.Fatal(err)
	}
	if len(e.bReady) != 2 || e.reference != ref || e.RateControlStats() != stats || e.bClock != clock || e.encodingB {
		t.Fatal("延迟B失败污染状态")
	}
	e.maxAUBytes = budget
	if p, err := e.ReceivePacket(); err != nil || p.PTS != 1 {
		t.Fatal("B重试失败", p, err)
	}
	if err = e.Close(); err != nil {
		t.Fatal(err)
	}
	if len(e.bReady) != 0 || e.bPast != nil || e.bFuture != nil {
		t.Fatal("Close未清理B组")
	}
}
