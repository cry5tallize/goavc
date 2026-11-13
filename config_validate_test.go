package h264

import (
	"errors"
	"testing"
)

// 测试辅助：按错误分类断言，而不是按错误文本断言。
// 文本会随文案调整而变化，分类是契约的一部分。
func isInvalidData(err error) bool { return errors.Is(err, ErrInvalidData) }
func isUnsupported(err error) bool { return errors.Is(err, ErrUnsupported) }

func validEncoderConfig() EncoderConfig {
	c := DefaultEncoderConfig()
	c.Width, c.Height = 640, 480
	return c
}

func TestEncoderConfigValidate(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(*EncoderConfig)
		wantInvalid bool
		wantUnsup   bool
	}{
		{"default is valid", func(c *EncoderConfig) {}, false, false},

		{"width below minimum", func(c *EncoderConfig) { c.Width = MinFrameWidth - 1 }, true, false},
		{"width above maximum", func(c *EncoderConfig) { c.Width = MaxFrameWidth + 1 }, true, false},
		{"height below minimum", func(c *EncoderConfig) { c.Height = MinFrameHeight - 1 }, true, false},
		{"height above maximum", func(c *EncoderConfig) { c.Height = MaxFrameHeight + 1 }, true, false},

		{"unknown pixel format", func(c *EncoderConfig) { c.PixelFormat = PixelFormat(99) }, false, true},
		{"unknown profile", func(c *EncoderConfig) { c.Profile = "super" }, false, true},
		{"high10 needs 10 bit", func(c *EncoderConfig) { c.Profile = ProfileHigh10 }, false, true},
		{"high needs 420", func(c *EncoderConfig) {
			c.Profile = ProfileHigh
			c.PixelFormat = YUV444P
		}, false, true},
		{"high422 rejects 444", func(c *EncoderConfig) {
			c.Profile = ProfileHigh422
			c.PixelFormat = YUV444P
		}, false, true},

		{"unknown level", func(c *EncoderConfig) { c.Level = 15 }, true, false},
		{"level auto", func(c *EncoderConfig) { c.Level = LevelAuto }, false, false},
		{"level 4.1", func(c *EncoderConfig) { c.Level = 41 }, false, false},

		{"zero fps numerator", func(c *EncoderConfig) { c.FPSNum = 0 }, true, false},
		{"zero fps denominator", func(c *EncoderConfig) { c.FPSDen = 0 }, true, false},

		{"qp below minimum", func(c *EncoderConfig) { c.QP = -1 }, true, false},
		{"qp above maximum", func(c *EncoderConfig) { c.QP = MaxQP + 1 }, true, false},
		{"qp at minimum", func(c *EncoderConfig) { c.QP = MinQP }, false, false},
		{"qp at maximum", func(c *EncoderConfig) { c.QP = MaxQP }, false, false},

		{"zero gop size", func(c *EncoderConfig) { c.GOPSize = 0 }, true, false},
		{"negative idr interval", func(c *EncoderConfig) { c.IDRInterval = -1 }, true, false},
		{"b frames not below gop", func(c *EncoderConfig) { c.GOPSize = 4; c.MaxBFrames = 4 }, true, false},
		{"negative b frames", func(c *EncoderConfig) { c.MaxBFrames = -1 }, true, false},
		{"too many b frames", func(c *EncoderConfig) { c.GOPSize = 100; c.MaxBFrames = maxBFrames + 1 }, true, false},
		{"baseline refuses b frames", func(c *EncoderConfig) { c.MaxBFrames = 2 }, false, true},
		{"high accepts b frames", func(c *EncoderConfig) {
			c.Profile = ProfileHigh
			c.MaxBFrames = 2
		}, false, false},

		{"unknown rate control", func(c *EncoderConfig) { c.RateControl = RateControl(9) }, true, false},
		{"abr without bitrate", func(c *EncoderConfig) { c.RateControl = RateControlABR }, true, false},
		{"abr with bitrate", func(c *EncoderConfig) {
			c.RateControl = RateControlABR
			c.Bitrate = 2_000_000
		}, false, false},
		{"negative bitrate", func(c *EncoderConfig) { c.Bitrate = -1 }, true, false},
		{"max bitrate below target", func(c *EncoderConfig) {
			c.RateControl = RateControlCBR
			c.Bitrate = 2_000_000
			c.MaxBitrate = 1_000_000
		}, true, false},
		{"negative threads", func(c *EncoderConfig) { c.Threads = -1 }, true, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := validEncoderConfig()
			tc.mutate(&c)
			err := c.Validate()
			switch {
			case tc.wantInvalid:
				if !isInvalidData(err) {
					t.Fatalf("期望 ErrInvalidData，得到 %v", err)
				}
			case tc.wantUnsup:
				if !isUnsupported(err) {
					t.Fatalf("期望 ErrUnsupported，得到 %v", err)
				}
			default:
				if err != nil {
					t.Fatalf("期望合法，得到 %v", err)
				}
			}
		})
	}
}

// 配置层不得把资源上限错误与结构错误混用：调用方需要靠分类决定是
// "修正参数" 还是 "提高上限后重试"。
func TestDecoderConfigValidate(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(*DecoderConfig)
		wantInvalid bool
		wantUnsup   bool
	}{
		{"default is valid", func(c *DecoderConfig) {}, false, false},
		{"max width below minimum", func(c *DecoderConfig) { c.MaxWidth = 8 }, true, false},
		{"max width above maximum", func(c *DecoderConfig) { c.MaxWidth = MaxFrameWidth + 1 }, true, false},
		{"max height below minimum", func(c *DecoderConfig) { c.MaxHeight = 8 }, true, false},
		{"negative frame size", func(c *DecoderConfig) { c.MaxFrameSize = -1 }, true, false},
		{"negative nal size", func(c *DecoderConfig) { c.MaxNALSize = -1 }, true, false},
		{"negative slice count", func(c *DecoderConfig) { c.MaxSliceCount = -1 }, true, false},
		{"negative dpb frames", func(c *DecoderConfig) { c.MaxDPBFrames = -1 }, true, false},
		{"negative threads", func(c *DecoderConfig) { c.Threads = -1 }, true, false},
		{"unknown output format", func(c *DecoderConfig) { c.OutputFormat = PixelFormat(42) }, false, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := DefaultDecoderConfig()
			tc.mutate(&c)
			err := c.Validate()
			switch {
			case tc.wantInvalid:
				if !isInvalidData(err) {
					t.Fatalf("期望 ErrInvalidData，得到 %v", err)
				}
			case tc.wantUnsup:
				if !isUnsupported(err) {
					t.Fatalf("期望 ErrUnsupported，得到 %v", err)
				}
			default:
				if err != nil {
					t.Fatalf("期望合法，得到 %v", err)
				}
			}
		})
	}
}

func TestRateControlStringAndValid(t *testing.T) {
	tests := []struct {
		rc    RateControl
		name  string
		valid bool
	}{
		{RateControlCQP, "cqp", true},
		{RateControlABR, "abr", true},
		{RateControlCBR, "cbr", true},
		{RateControlVBV, "vbv", true},
		{RateControl(200), "unknown", false},
	}
	for _, tc := range tests {
		if got := tc.rc.String(); got != tc.name {
			t.Fatalf("RateControl(%d).String() = %q，期望 %q", uint8(tc.rc), got, tc.name)
		}
		if got := tc.rc.Valid(); got != tc.valid {
			t.Fatalf("RateControl(%d).Valid() = %v，期望 %v", uint8(tc.rc), got, tc.valid)
		}
	}
}
