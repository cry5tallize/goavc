// streamcheck执行M6长时门禁；只依赖标准库，报告实际RSS/Go堆与取消回收。
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"sync/atomic"
	"time"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/encoder"
	"github.com/cry5tallize/goavc/internal/testutil/perfstats"
	"github.com/cry5tallize/goavc/stream"
)

type sample struct {
	Seconds                 float64
	RSS, HeapAlloc, HeapSys uint64
	Goroutines              int
	Encoded, Decoded        int64
}
type result struct {
	DurationSeconds            float64
	WarmupSeconds              float64
	RSSGrowthLimitBytes        uint64
	RSSRateLimitBytesPerMinute uint64
	HeapGrowthLimitBytes       uint64
	Samples                    []sample
	Passed                     bool
}

type pacedFrames struct {
	timer *time.Ticker
	index int64
}

func (p *pacedFrames) ReadFrame() (*h264.Frame, error) { return nil, h264.ErrUnsupported }
func (p *pacedFrames) ReadFrameContext(ctx context.Context) (*h264.Frame, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.timer.C:
	}
	f := &h264.Frame{Width: 32, Height: 32, Format: h264.YUV420P, PTS: p.index, Duration: 1}
	p.index++
	for c := range f.Planes {
		w, h := f.Format.PlaneDimensions(c, 32, 32)
		f.Planes[c] = h264.Plane{Stride: w, Data: bytes.Repeat([]byte{byte(80 + c)}, w*h)}
	}
	return f, nil
}

type countedPackets struct {
	dst   stream.AccessUnitWriter
	count *atomic.Int64
}

func (w countedPackets) WriteAccessUnit(p h264.Packet) error {
	err := w.dst.WriteAccessUnit(p)
	if err == nil {
		w.count.Add(1)
	}
	return err
}

type checkedFrames struct{ count *atomic.Int64 }

func (w checkedFrames) WriteFrame(f *h264.Frame) error {
	if f.Width != 32 || f.Height != 32 || f.Format != h264.YUV420P {
		return fmt.Errorf("尺寸/格式改变")
	}
	for c, p := range f.Planes {
		ww, hh := f.Format.PlaneDimensions(c, 32, 32)
		for y := range hh {
			for _, v := range p.Data[y*p.Stride : y*p.Stride+ww] {
				if v != byte(80+c) {
					return fmt.Errorf("PCM像素与独立常量解析值不同")
				}
			}
		}
	}
	w.count.Add(1)
	return nil
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	duration := flag.Duration("duration", time.Hour, "门禁运行时间，正式验收不得少于1h")
	interval := flag.Duration("sample-interval", 10*time.Second, "RSS采样间隔")
	warmup := flag.Duration("warmup", time.Minute, "预热时间，预热后计算增长")
	flag.Parse()
	if *duration <= 0 || *interval <= 0 || *warmup < 0 || *warmup >= *duration || flag.NArg() != 0 {
		return fmt.Errorf("无效时间参数")
	}
	// 阈值在运行前固定：预热后RSS最多增长32MiB，拟合斜率最多1MiB/min；
	// GC后活跃Go堆最多增长8MiB。HeapSys仅记录，不将保留堆误作活跃内存。
	report := result{DurationSeconds: duration.Seconds(), WarmupSeconds: warmup.Seconds(), RSSGrowthLimitBytes: 32 << 20, RSSRateLimitBytesPerMinute: 1 << 20, HeapGrowthLimitBytes: 8 << 20}
	before := runtime.NumGoroutine()
	ctx, cancel := context.WithTimeout(context.Background(), *duration)
	defer cancel()
	pr, pw := io.Pipe()
	// 本工具拥有管道，取消可通过Close真正中断阻塞I/O，等待两个pipeline退出。
	interrupted := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { pr.CloseWithError(ctx.Err()); pw.CloseWithError(ctx.Err()); close(interrupted) })
	defer func() {
		if !stop() {
			<-interrupted
		}
		pr.Close()
		pw.Close()
	}()
	nr, err := stream.NewAnnexBReader(pr, 4096)
	if err != nil {
		return err
	}
	ar, err := stream.NewAccessUnitReader(nr, stream.AccessUnitConfig{MaxAccessUnitSize: 8192})
	if err != nil {
		return err
	}
	aw, err := stream.NewAnnexBWriter(pw, stream.AnnexBWriterConfig{MaxNALSize: 4096, MaxAccessUnitSize: 8192})
	if err != nil {
		return err
	}
	var encoded, decoded atomic.Int64
	done := make(chan error, 2)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	cfg := h264.DefaultEncoderConfig()
	cfg.Width, cfg.Height = 32, 32
	go func() {
		err := stream.DecodeStream(ctx, ar, checkedFrames{&decoded}, stream.DecodeConfig{MaxInputBuffer: 8192})
		if err != nil && !errors.Is(err, ctx.Err()) {
			cancel()
		}
		done <- err
	}()
	go func() {
		err := stream.EncodeStream(ctx, &pacedFrames{timer: ticker}, countedPackets{aw, &encoded}, stream.EncodeConfig{Encoder: cfg, Options: []encoder.Option{encoder.WithPCM()}, MaxInputBuffer: 1536})
		if err != nil && !errors.Is(err, ctx.Err()) {
			cancel()
		}
		done <- err
	}()
	start := time.Now()
	samples := time.NewTicker(*interval)
	defer samples.Stop()
	collect := func() error {
		runtime.GC()
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		rss, err := perfstats.ResidentBytes()
		if err != nil {
			return err
		}
		report.Samples = append(report.Samples, sample{time.Since(start).Seconds(), rss, ms.HeapAlloc, ms.HeapSys, runtime.NumGoroutine(), encoded.Load(), decoded.Load()})
		return nil
	}
	var sampleErr error
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case <-samples.C:
			if err := collect(); err != nil {
				sampleErr = err
				cancel()
				break loop
			}
		}
	}
	var pipelineErr error
	for range 2 {
		err := <-done
		if !errors.Is(err, context.DeadlineExceeded) && pipelineErr == nil {
			pipelineErr = err
		}
	}
	if err := collect(); err != nil && sampleErr == nil {
		sampleErr = err
	}
	<-interrupted
	// 已join的调用者及AfterFunc均已完成；这里不留下监测worker。
	runtime.Gosched()
	var gateErr error
	if sampleErr != nil {
		gateErr = sampleErr
	} else if pipelineErr != nil {
		gateErr = pipelineErr
	} else if time.Since(start) < *duration {
		gateErr = fmt.Errorf("提前停止")
	}
	if runtime.NumGoroutine() > before+1 {
		gateErr = errors.Join(gateErr, fmt.Errorf("取消后goroutine未回收"))
	}
	if decoded.Load() == 0 || encoded.Load()-decoded.Load() > 2 {
		gateErr = errors.Join(gateErr, fmt.Errorf("长流未持续输出或积压"))
	}
	var baseline *sample
	var sumT, sumR, sumTT, sumTR float64
	var n float64
	for i := range report.Samples {
		s := &report.Samples[i]
		if s.Seconds < report.WarmupSeconds {
			continue
		}
		if baseline == nil {
			baseline = s
		}
		if s.RSS > baseline.RSS+report.RSSGrowthLimitBytes || s.HeapAlloc > baseline.HeapAlloc+report.HeapGrowthLimitBytes {
			gateErr = errors.Join(gateErr, fmt.Errorf("预热后内存增长超限于%.1fs", s.Seconds))
			break
		}
		minutes := s.Seconds / 60
		rss := float64(s.RSS)
		sumT += minutes
		sumR += rss
		sumTT += minutes * minutes
		sumTR += minutes * rss
		n++
	}
	if n < 2 {
		gateErr = errors.Join(gateErr, fmt.Errorf("预热后有效采样不足"))
	} else if den := n*sumTT - sumT*sumT; den > 0 && (n*sumTR-sumT*sumR)/den > float64(report.RSSRateLimitBytesPerMinute) {
		gateErr = errors.Join(gateErr, fmt.Errorf("RSS增长率超过1MiB/min"))
	}
	report.Passed = gateErr == nil
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		return err
	}
	return gateErr
}
