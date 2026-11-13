package h264

import "testing"

// 默认值必须与文档一致，否则 CLI、库、测试三方会各自假设不同的默认行为。
func TestEncoderConfigSetDefaults(t *testing.T) {
	c := EncoderConfig{Width: 320, Height: 240}
	c.SetDefaults()

	if got, want := c.Profile, ProfileBaseline; got != want {
		t.Fatalf("Profile = %q，期望 %q", got, want)
	}
	if got, want := c.FPSNum, defaultFPSNum; got != want {
		t.Fatalf("FPSNum = %d，期望 %d", got, want)
	}
	if got, want := c.FPSDen, defaultFPSDen; got != want {
		t.Fatalf("FPSDen = %d，期望 %d", got, want)
	}
	if got, want := c.GOPSize, defaultGOPSize; got != want {
		t.Fatalf("GOPSize = %d，期望 %d", got, want)
	}
	if got, want := c.IDRInterval, c.GOPSize; got != want {
		t.Fatalf("IDRInterval = %d，期望跟随 GOPSize %d", got, want)
	}
	if c.QP != 0 {
		t.Fatalf("QP = %d，SetDefaults 不得改动 QP", c.QP)
	}
}

// 只填了分数分母时，分子保持调用方设定，分母补 1。
func TestEncoderConfigSetDefaultsPartialFPS(t *testing.T) {
	c := EncoderConfig{FPSNum: 24}
	c.SetDefaults()
	if c.FPSNum != 24 || c.FPSDen != 1 {
		t.Fatalf("fps = %d/%d，期望 24/1", c.FPSNum, c.FPSDen)
	}
}

// 显式设置的值不得被默认值覆盖。
func TestEncoderConfigSetDefaultsKeepsExplicit(t *testing.T) {
	c := EncoderConfig{
		Width:   1920,
		Height:  1080,
		Profile: ProfileHigh,
		FPSNum:  60000,
		FPSDen:  1001,
		GOPSize: 12,
		QP:      30,
	}
	c.SetDefaults()
	if c.FPSNum != 60000 || c.FPSDen != 1001 {
		t.Fatalf("fps 被覆盖为 %d/%d", c.FPSNum, c.FPSDen)
	}
	if c.GOPSize != 12 {
		t.Fatalf("GOPSize 被覆盖为 %d", c.GOPSize)
	}
	if c.QP != 30 {
		t.Fatalf("QP 被覆盖为 %d", c.QP)
	}
	if c.IDRInterval != 12 {
		t.Fatalf("IDRInterval = %d，期望 12", c.IDRInterval)
	}
}

func TestDefaultEncoderConfig(t *testing.T) {
	c := DefaultEncoderConfig()
	if err := c.Validate(); err != nil {
		// 模板本身没有尺寸，Validate 应当仅因尺寸失败。
		if !isInvalidData(err) {
			t.Fatalf("模板校验错误类型不对: %v", err)
		}
	}
	c.Width, c.Height = 640, 480
	if err := c.Validate(); err != nil {
		t.Fatalf("默认模板 + 尺寸后应当合法: %v", err)
	}
}

// 解码器上限必须全部补成具体数值：留 0 等于无限制，是不可接受的默认。
func TestDecoderConfigSetDefaults(t *testing.T) {
	var c DecoderConfig
	c.SetDefaults()

	if c.MaxWidth != MaxFrameWidth || c.MaxHeight != MaxFrameHeight {
		t.Fatalf("分辨率上限 = %dx%d，期望 %dx%d", c.MaxWidth, c.MaxHeight, MaxFrameWidth, MaxFrameHeight)
	}
	if c.MaxNALSize != defaultMaxNALSize {
		t.Fatalf("MaxNALSize = %d，期望 %d", c.MaxNALSize, defaultMaxNALSize)
	}
	if c.MaxSliceCount != defaultMaxSliceCount {
		t.Fatalf("MaxSliceCount = %d，期望 %d", c.MaxSliceCount, defaultMaxSliceCount)
	}
	if c.MaxDPBFrames != defaultMaxDPBFrames {
		t.Fatalf("MaxDPBFrames = %d，期望 %d", c.MaxDPBFrames, defaultMaxDPBFrames)
	}
	if c.OutputFormat != YUV420P {
		t.Fatalf("OutputFormat = %v，期望 yuv420p", c.OutputFormat)
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("默认解码器配置应当合法: %v", err)
	}
}

func TestDefaultDecoderConfig(t *testing.T) {
	c := DefaultDecoderConfig()
	if err := c.Validate(); err != nil {
		t.Fatalf("默认解码器配置应当合法: %v", err)
	}
}
