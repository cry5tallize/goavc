package codeccli

import (
	"context"
	"flag"
	"fmt"
	"io"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/decoder"
	"github.com/cry5tallize/goavc/stream"
)

// Decode 将显式封装的裸 H.264 解码为紧密平面 YUV，不猜测 MP4 或 avcC。
func Decode(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) (err error) {
	f := flag.NewFlagSet("h264dec", flag.ContinueOnError)
	f.SetOutput(stderr)
	var c common
	c.flags(f)
	cfg := h264.DefaultDecoderConfig()
	pixel := f.String("pixel-format", "yuv420p", "输出原生平面格式：yuv420p/422p/444p及对应10le；不做格式转换")
	f.IntVar(&cfg.Threads, "threads", 1, "解码并行度；当前仅支持0或1")
	f.IntVar(&cfg.MaxWidth, "max-width", cfg.MaxWidth, "最大解码宽度")
	f.IntVar(&cfg.MaxHeight, "max-height", cfg.MaxHeight, "最大解码高度")
	f.IntVar(&cfg.MaxFrameSize, "max-frame-size", cfg.MaxFrameSize, "单帧缓冲字节上限；0 按尺寸推导")
	f.IntVar(&cfg.MaxSliceCount, "max-slices", cfg.MaxSliceCount, "单帧最大 slice 数")
	f.IntVar(&cfg.MaxDPBFrames, "max-dpb-frames", cfg.MaxDPBFrames, "最大 DPB 帧数")
	if err = f.Parse(args); err != nil {
		return err
	}
	if err = c.validate(f); err != nil {
		return err
	}
	if cfg.OutputFormat, err = pixelFormat(*pixel); err != nil {
		return err
	}
	cfg.MaxNALSize = c.maxNAL
	// CLI 的显式零上限不隐式变成库默认值，避免掩盖拼写或脚本错误。
	if cfg.MaxSliceCount <= 0 || cfg.MaxDPBFrames <= 0 {
		return fmt.Errorf("解码资源上限必须为正数: %w", h264.ErrInvalidData)
	}
	if err = cfg.Validate(); err != nil {
		return err
	}
	preflight, err := decoder.New(cfg)
	if err != nil {
		return err
	}
	if err = preflight.Close(); err != nil {
		return err
	}
	// 源端预算先收紧，避免 pipeline 接收到 AU 后才发现其已经超额分配。
	c.maxAU = min(c.maxAU, c.maxInput)
	if c.maxAU < 5 {
		return fmt.Errorf("AU 预算不足以容纳起始码与 NAL header: %w", h264.ErrResourceLimit)
	}
	c.maxNAL = min(c.maxNAL, c.maxAU-4)
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
	var nals stream.NALReader
	if c.format == "avcc" {
		nals, err = stream.NewAVCCReader(in, c.lengthSize, c.maxNAL)
	} else {
		nals, err = stream.NewAnnexBReader(in, c.maxNAL)
	}
	if err != nil {
		return err
	}
	src, err := stream.NewAccessUnitReader(nals, stream.AccessUnitConfig{
		MaxAccessUnitSize: c.maxAU, MaxNALUnits: c.maxNALUnits,
	})
	if err != nil {
		return err
	}
	dst, err := stream.NewYUVWriter(countedWriter{out, p})
	if err != nil {
		return err
	}
	return stream.DecodeStream(ctx, src, progressFrameWriter{dst, p}, stream.DecodeConfig{
		Decoder: cfg, MaxInputBuffer: c.maxInput,
	})
}

type progressFrameWriter struct {
	dst stream.FrameWriter
	p   *progress
}

func (w progressFrameWriter) WriteFrame(frame *h264.Frame) error {
	if err := w.dst.WriteFrame(frame); err != nil {
		return err
	}
	w.p.advance()
	return nil
}
