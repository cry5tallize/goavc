package codeccli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/encoder"
	"github.com/cry5tallize/goavc/stream"
)

// Encode 编码紧密平面 YUV；调用者提供的标准流始终为借用资源。
// 所有配置（包括 encoder.Option）均在打开输出之前检查。
func Encode(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) (err error) {
	f := flag.NewFlagSet("h264enc", flag.ContinueOnError)
	f.SetOutput(stderr)
	var c common
	c.flags(f)
	cfg := h264.DefaultEncoderConfig()
	f.IntVar(&cfg.Width, "width", 0, "YUV 显示宽度，必须为偶数")
	f.IntVar(&cfg.Height, "height", 0, "YUV 显示高度，必须为偶数")
	pixel := f.String("pixel-format", "yuv420p", "输入平面格式，如yuv420p、yuv420p10le；组合由编码器校验")
	f.StringVar(&cfg.Profile, "profile", cfg.Profile, "baseline、main、high、high10、high422或high444；支持边界见文档")
	f.IntVar(&cfg.Level, "level", cfg.Level, "level_idc，0 自动")
	f.IntVar(&cfg.QP, "qp", cfg.QP, "语法QPY，8-bit为0..51，10-bit为-12..51")
	f.IntVar(&cfg.GOPSize, "gop", cfg.GOPSize, "GOP 帧数")
	f.IntVar(&cfg.IDRInterval, "idr", 0, "独立 IDR 间隔；0 跟随 GOP")
	f.IntVar(&cfg.MaxBFrames, "bframes", 0, "连续非参考 B 帧上限；Baseline 必须为0")
	rate := f.String("rate", "cqp", "码率策略：cqp、abr、cbr、vbv")
	f.IntVar(&cfg.Bitrate, "bitrate", 0, "目标 bit/s")
	f.IntVar(&cfg.MaxBitrate, "max-bitrate", 0, "峰值 bit/s")
	f.IntVar(&cfg.VBVBufferSize, "vbv-buffer-size", 0, "VBV 容量，单位 bit；0 自动")
	fps := f.String("fps", "30/1", "有理帧率，如 30 或 30000/1001")
	f.IntVar(&cfg.Threads, "threads", 1, "编码并行度；当前仅支持0或1")
	slices := f.Int("slices", 1, "每帧切片数，1..64 且不大于宏块数")
	lookahead := f.Int("lookahead", 0, "前瞻深度，0 关闭，1..16 开启")
	transform := f.Bool("8x8", false, "启用 High Profile 8x8 变换候选")
	weighted := f.Bool("weighted", false, "启用 Main/High 自动加权 P 预测")
	pcm := f.Bool("pcm", false, "使用无损 I_PCM 互操作通道")
	entropy := f.String("entropy", "auto", "熵编码：auto、cavlc或cabac；Baseline不允许CABAC")
	fieldOrder := f.String("field-order", "progressive", "扫描顺序：progressive、tff或bff；隔行输入为逐行交织的完整帧")
	if err = f.Parse(args); err != nil {
		return err
	}
	if err = c.validate(f); err != nil {
		return err
	}
	if cfg.PixelFormat, err = pixelFormat(*pixel); err != nil {
		return err
	}
	if cfg.FPSNum, cfg.FPSDen, err = parseFPS(*fps); err != nil {
		return err
	}
	switch *rate {
	case "cqp":
		cfg.RateControl = h264.RateControlCQP
	case "abr":
		cfg.RateControl = h264.RateControlABR
	case "cbr":
		cfg.RateControl = h264.RateControlCBR
	case "vbv":
		cfg.RateControl = h264.RateControlVBV
	default:
		return fmt.Errorf("无效码率策略 %q: %w", *rate, h264.ErrInvalidData)
	}
	if err = cfg.Validate(); err != nil {
		return err
	}
	if *lookahead < 0 || *lookahead > 16 {
		return fmt.Errorf("前瞻窗口必须为0..16: %w", h264.ErrInvalidData)
	}
	options := []encoder.Option{encoder.WithSliceCount(*slices)}
	switch *fieldOrder {
	case "progressive":
	case "tff", "bff":
		options = append(options, encoder.WithInterlaced(*fieldOrder == "tff"))
	default:
		return fmt.Errorf("无效场顺序 %q: %w", *fieldOrder, h264.ErrInvalidData)
	}
	switch *entropy {
	case "auto":
	case "cavlc":
		options = append(options, encoder.WithCABAC(false))
	case "cabac":
		options = append(options, encoder.WithCABAC(true))
	default:
		return fmt.Errorf("无效熵编码 %q: %w", *entropy, h264.ErrInvalidData)
	}
	if *lookahead > 0 {
		options = append(options, encoder.WithLookahead(*lookahead))
	}
	if *transform {
		options = append(options, encoder.WithTransform8x8())
	}
	if *weighted {
		options = append(options, encoder.WithWeightedPrediction())
	}
	if *pcm {
		options = append(options, encoder.WithPCM())
	}
	// 构造器是可用特性及 Option 组合校验的唯一来源，不复制其规则。
	preflight, err := encoder.New(cfg, options...)
	if err != nil {
		return err
	}
	if err = preflight.Close(); err != nil {
		return err
	}
	// 固定 YUV 布局在打开输出前即可判定，超小预算不能先截断已有文件。
	frameBytes := 0
	for comp := range 3 {
		_, rows := cfg.PixelFormat.PlaneDimensions(comp, cfg.Width, cfg.Height)
		frameBytes += cfg.PixelFormat.RowBytes(comp, cfg.Width) * rows
	}
	if frameBytes > c.maxInput {
		return fmt.Errorf("YUV 单帧 %d 字节超过输入预算 %d: %w", frameBytes, c.maxInput, h264.ErrResourceLimit)
	}
	if stdin == nil || stdout == nil || stderr == nil {
		return fmt.Errorf("标准流不能为 nil: %w", h264.ErrInvalidData)
	}
	p := newProgress(stderr, c.progress)
	owned := newOwnedIO(ctx)
	defer finish(ctx, owned, p, &err)
	in, out, err := owned.open(ctx, c, stdin, stdout)
	if err != nil {
		return err
	}
	src, err := stream.NewYUVReader(in, cfg.Width, cfg.Height, cfg.PixelFormat)
	if err != nil {
		return err
	}
	dst, err := accessUnitWriter(countedWriter{out, p}, c)
	if err != nil {
		return err
	}
	return stream.EncodeStream(ctx, src, progressAUWriter{dst, p}, stream.EncodeConfig{
		Encoder: cfg, Options: options, MaxInputBuffer: c.maxInput,
	})
}

func parseFPS(value string) (int, int, error) {
	parts := strings.Split(value, "/")
	if len(parts) < 1 || len(parts) > 2 {
		return 0, 0, fmt.Errorf("无效帧率 %q: %w", value, h264.ErrInvalidData)
	}
	n, err := strconv.Atoi(parts[0])
	d := 1
	if err == nil && len(parts) == 2 {
		d, err = strconv.Atoi(parts[1])
	}
	if err != nil || n <= 0 || d <= 0 {
		return 0, 0, fmt.Errorf("无效帧率 %q: %w", value, h264.ErrInvalidData)
	}
	return n, d, nil
}

func accessUnitWriter(dst io.Writer, c common) (stream.AccessUnitWriter, error) {
	if c.format == "avcc" {
		return stream.NewAVCCWriter(dst, stream.AVCCWriterConfig{
			LengthSize: c.lengthSize, MaxNALSize: c.maxNAL, MaxAccessUnitSize: c.maxAU, MaxNALUnits: c.maxNALUnits,
		})
	}
	return stream.NewAnnexBWriter(dst, stream.AnnexBWriterConfig{
		MaxNALSize: c.maxNAL, MaxAccessUnitSize: c.maxAU, MaxNALUnits: c.maxNALUnits,
	})
}

type progressAUWriter struct {
	dst stream.AccessUnitWriter
	p   *progress
}

func (w progressAUWriter) WriteAccessUnit(packet h264.Packet) error {
	if err := w.dst.WriteAccessUnit(packet); err != nil {
		return err
	}
	w.p.advance()
	return nil
}
