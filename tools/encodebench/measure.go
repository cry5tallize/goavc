package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"runtime"
	"runtime/debug"
	"runtime/metrics"
	"sort"
	"strconv"
	"strings"
	"time"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/encoder"
	"github.com/goavc/h264/internal/testutil/perfstats"
)

// measurementConfig完整保留调用配置；Encoder已应用SetDefaults，不暗中覆盖Threads。
// 与quality分离的入口不创建original/reconstruction映射，也不保留包载荷。
type measurementConfig struct {
	Encoder                               h264.EncoderConfig
	Frames, SliceCount                    int
	PCM, Transform8x8, WeightedPrediction bool
	Lookahead                             int
	CPUProfile                            bool
}

type callTiming struct {
	Calls                           int
	TotalNanos, MeanNanos, P95Nanos int64
}

type heapSample struct {
	HeapAlloc, HeapInuse, HeapSys, Sys, RSS uint64
}

type measurementMemory struct {
	Scope, Sampling, RSSSource string
	Samples                    int
	Before, After, SampledMax  heapSample
	TotalAlloc, Mallocs        uint64
	NaturalNumGC               uint32
	PostCloseGC                heapSample
	PostCloseGCDescription     string
}

// 浮点直方图边界可含无穷，使用字符串保证JSON仍是有效且无损的报告。
type gcHistogram struct {
	Buckets              []string
	Before, After, Delta []uint64
}

type gcMetric struct {
	Name, Kind                  string
	Cumulative                  bool
	BeforeUint64, AfterUint64   uint64
	DeltaUint64                 *uint64 `json:",omitempty"`
	BeforeFloat64, AfterFloat64 float64
	DeltaFloat64                *float64     `json:",omitempty"`
	Histogram                   *gcHistogram `json:",omitempty"`
}

type measurementReport struct {
	Mode, Window, TimingBoundary, P95Definition string
	Config                                      measurementConfig
	Go, GOOS, GOARCH, Revision                  string
	Modified                                    bool
	ModifiedKnown                               bool
	BuildInfoAvailable                          bool
	BuildSettings                               []debug.BuildSetting
	GOMAXPROCS                                  int
	InputFormat, InputBoundary                  string
	InputSHA256, StreamSHA256                   string
	InputBytes, OutputBytes                     int64
	InputFrames, Packets                        int
	OutputEnabled                               bool
	InputPTS, InputDuration                     []int64
	PacketPTS, PacketDTS, PacketDuration        []int64
	QPSequence                                  []int
	FrameBits, VBVFullnessSequence              []int64
	VBVBufferBits                               int64
	Send, Receive, Flush                        callTiming
	CodecCallNanos, WindowNanos                 int64
	Memory                                      measurementMemory
	GCMetrics                                   []gcMetric
	GCUnavailable                               []string
}

// metricWindow只读取本进程实际支持的指标，未提供的指标明确列出，绝不伪造零值。
type metricWindow struct {
	descriptions  []metrics.Description
	before, after []metrics.Sample
}

func newMetricWindow() metricWindow {
	var w metricWindow
	for _, d := range metrics.All() {
		if strings.HasPrefix(d.Name, "/cpu/classes/gc/") || strings.HasPrefix(d.Name, "/gc/scan/") || d.Name == "/gc/pauses:seconds" || d.Name == "/sched/pauses/total/gc:seconds" || d.Name == "/sched/pauses/stopping/gc:seconds" {
			w.descriptions = append(w.descriptions, d)
			w.before = append(w.before, metrics.Sample{Name: d.Name})
			w.after = append(w.after, metrics.Sample{Name: d.Name})
		}
	}
	return w
}

func (w *metricWindow) report() ([]gcMetric, []string) {
	var result []gcMetric
	available := map[string]bool{}
	for i, d := range w.descriptions {
		a, b := w.before[i].Value, w.after[i].Value
		if a.Kind() != b.Kind() || a.Kind() == metrics.KindBad {
			continue
		}
		r := gcMetric{Name: d.Name, Cumulative: d.Cumulative}
		switch a.Kind() {
		case metrics.KindUint64:
			r.Kind = "uint64"
			r.BeforeUint64, r.AfterUint64 = a.Uint64(), b.Uint64()
			if d.Cumulative {
				delta := r.AfterUint64 - r.BeforeUint64
				r.DeltaUint64 = &delta
			}
		case metrics.KindFloat64:
			r.Kind = "float64"
			r.BeforeFloat64, r.AfterFloat64 = a.Float64(), b.Float64()
			if d.Cumulative {
				delta := r.AfterFloat64 - r.BeforeFloat64
				r.DeltaFloat64 = &delta
			}
		case metrics.KindFloat64Histogram:
			r.Kind = "float64 histogram"
			x, y := a.Float64Histogram(), b.Float64Histogram()
			h := &gcHistogram{Before: append([]uint64(nil), x.Counts...), After: append([]uint64(nil), y.Counts...)}
			for _, boundary := range x.Buckets {
				h.Buckets = append(h.Buckets, strconv.FormatFloat(boundary, 'g', -1, 64))
			}
			for j := range x.Counts {
				h.Delta = append(h.Delta, y.Counts[j]-x.Counts[j])
			}
			r.Histogram = h
		default:
			continue
		}
		available[d.Name] = true
		result = append(result, r)
	}
	var unavailable []string
	for _, name := range []string{"/cpu/classes/gc/total:cpu-seconds", "/cpu/classes/gc/mark/assist:cpu-seconds", "/gc/scan/heap:bytes", "/gc/scan/stack:bytes", "/gc/scan/globals:bytes", "/gc/scan/total:bytes", "/gc/pauses:seconds", "/sched/pauses/total/gc:seconds", "/sched/pauses/stopping/gc:seconds"} {
		if !available[name] {
			unavailable = append(unavailable, name)
		}
	}
	return result, unavailable
}

func sampleHeap(ms *runtime.MemStats) (heapSample, error) {
	rss, err := perfstats.ResidentBytes()
	runtime.ReadMemStats(ms)
	return heapSample{HeapAlloc: ms.HeapAlloc, HeapInuse: ms.HeapInuse, HeapSys: ms.HeapSys, Sys: ms.Sys, RSS: rss}, err
}

func (m *measurementMemory) observe(s heapSample) {
	m.Samples++
	m.SampledMax.HeapAlloc = max(m.SampledMax.HeapAlloc, s.HeapAlloc)
	m.SampledMax.HeapInuse = max(m.SampledMax.HeapInuse, s.HeapInuse)
	m.SampledMax.HeapSys = max(m.SampledMax.HeapSys, s.HeapSys)
	m.SampledMax.Sys = max(m.SampledMax.Sys, s.Sys)
	m.SampledMax.RSS = max(m.SampledMax.RSS, s.RSS)
}

func summarizeCalls(samples []time.Duration) callTiming {
	r := callTiming{Calls: len(samples)}
	for _, d := range samples {
		r.TotalNanos += int64(d)
	}
	if len(samples) != 0 {
		sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
		r.MeanNanos = r.TotalNanos / int64(len(samples))
		// nearest-rank：ceil(0.95*N)，不插值；避免乘95溢出。
		r.P95Nanos = int64(samples[len(samples)-len(samples)/20-1])
	}
	return r
}

func runMeasurement(cfg measurementConfig, reader io.Reader, writer io.Writer) (measurementReport, error) {
	if reader == nil {
		return measurementReport{}, fmt.Errorf("measure模式必须提供raw YUV420P输入")
	}
	if cfg.Frames < 1 {
		return measurementReport{}, fmt.Errorf("frames必须大于0")
	}
	cfg.Encoder.SetDefaults()
	if err := cfg.Encoder.Validate(); err != nil {
		return measurementReport{}, err
	}
	if cfg.Encoder.PixelFormat != h264.YUV420P {
		return measurementReport{}, fmt.Errorf("measure仅接受raw YUV420P")
	}
	options := []encoder.Option{encoder.WithSliceCount(cfg.SliceCount)}
	if cfg.PCM {
		options = append(options, encoder.WithPCM())
	}
	if cfg.Transform8x8 {
		options = append(options, encoder.WithTransform8x8())
	}
	if cfg.WeightedPrediction {
		options = append(options, encoder.WithWeightedPrediction())
	}
	if cfg.Lookahead != 0 {
		options = append(options, encoder.WithLookahead(cfg.Lookahead))
	}
	r := measurementReport{
		Mode: "measure", Window: "连续窗口，无显式预热，非warm-steady",
		TimingBoundary: "仅SendFrame/ReceivePacket/Flush调用内的墙钟时间；每次读入及SHA256、包SHA256/写出、RC轨迹采集和内存采样均在调用计时外；Receive含ErrAgain/ErrEOF；encoder.New和Close不计入codec调用时间",
		P95Definition:  "各API的全部调用（包括Receive的ErrAgain/ErrEOF）各为一个样本；升序nearest-rank ceil(0.95*N)，不插值；空样本为0",
		Config:         cfg, Go: runtime.Version(), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, GOMAXPROCS: runtime.GOMAXPROCS(0), Revision: "unknown (vcs.revision unavailable)", OutputEnabled: writer != nil,
		InputFormat: "紧密raw YUV420P", InputBoundary: "恰好Frames帧并检查EOF；SHA256覆盖完整输入与全部Annex-B包，禁止忽略尾随输入",
		Memory: measurementMemory{
			Scope:                  "本进程自然GC窗口；含encoder.New、哈希、输入/输出I/O、报告轨迹及采样开销，并非codec独占；窗口内不主动GC；不含Close和单独留存GC",
			Sampling:               "同步采样：encoder.New前、每次输入完成并排空包后、Flush及最终排空后；最大值是采样最大值，不是连续峰值；RSS与Go堆顺序读取，非原子快照",
			RSSSource:              perfstats.RSSSource,
			PostCloseGCDescription: "自然窗口结束后先Close、释放输入与encoder引用，再且仅再主动GC一次；留存包含报告轨迹及调用者仍持有的reader/writer，不是codec独占",
		},
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		r.BuildInfoAvailable = true
		r.BuildSettings = info.Settings
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				r.Revision = setting.Value
			case "vcs.modified":
				r.Modified = setting.Value == "true"
				r.ModifiedKnown = true
			}
		}
	}
	// 一个输入Frame供所有SendFrame复用；编码器负责其延迟队列的所有权。
	f := &h264.Frame{Width: cfg.Encoder.Width, Height: cfg.Encoder.Height, Format: h264.YUV420P, Duration: 1}
	for comp := range f.Planes {
		w, h := f.Format.PlaneDimensions(comp, f.Width, f.Height)
		f.Planes[comp] = h264.Plane{Data: make([]byte, w*h), Stride: w}
	}
	input, output := sha256.New(), sha256.New()
	gc := newMetricWindow()
	var before, after runtime.MemStats
	var err error
	r.Memory.Before, err = sampleHeap(&before)
	if err != nil {
		return measurementReport{}, err
	}
	r.Memory.observe(r.Memory.Before)
	metrics.Read(gc.before)
	windowStart := time.Now()
	e, err := encoder.New(cfg.Encoder, options...)
	if err != nil {
		return measurementReport{}, err
	}
	defer func() {
		if e != nil {
			e.Close()
		}
	}()
	r.VBVBufferBits = e.RateControlStats().VBVBufferBits
	var sendTimes, receiveTimes, flushTimes []time.Duration
	drain := func() error {
		for {
			start := time.Now()
			p, err := e.ReceivePacket()
			elapsed := time.Since(start)
			receiveTimes = append(receiveTimes, elapsed)
			if errors.Is(err, h264.ErrAgain) || errors.Is(err, h264.ErrEOF) {
				return nil
			}
			if err != nil {
				return err
			}
			output.Write(p.Data)
			if writer != nil {
				if err := writeReconstructionRow(writer, p.Data); err != nil {
					return fmt.Errorf("output packet %d: %w", r.Packets, err)
				}
			}
			r.OutputBytes += int64(len(p.Data))
			r.Packets++
			r.PacketPTS = append(r.PacketPTS, p.PTS)
			r.PacketDTS = append(r.PacketDTS, p.DTS)
			r.PacketDuration = append(r.PacketDuration, p.Duration)
			r.QPSequence = append(r.QPSequence, e.LastQP())
			r.FrameBits = append(r.FrameBits, int64(len(p.Data))*8)
			r.VBVFullnessSequence = append(r.VBVFullnessSequence, e.RateControlStats().VBVFullness)
			// p仅存在于当前迭代；不保留全流或解码/重建副本。
		}
	}
	for i := range cfg.Frames {
		for comp := range f.Planes {
			n, err := io.ReadFull(reader, f.Planes[comp].Data)
			r.InputBytes += int64(n)
			if err != nil {
				return measurementReport{}, fmt.Errorf("input frame %d plane %d: %w", i, comp, err)
			}
		}
		hashFrame(input, f)
		f.PTS = int64(i)
		start := time.Now()
		err := e.SendFrame(f)
		elapsed := time.Since(start)
		sendTimes = append(sendTimes, elapsed)
		if err != nil {
			return measurementReport{}, err
		}
		r.InputFrames++
		r.InputPTS = append(r.InputPTS, f.PTS)
		r.InputDuration = append(r.InputDuration, f.Duration)
		if err := drain(); err != nil {
			return measurementReport{}, err
		}
		sample, err := sampleHeap(&after)
		if err != nil {
			return measurementReport{}, err
		}
		r.Memory.observe(sample)
	}
	var trailing [1]byte
	if n, err := io.ReadFull(reader, trailing[:]); n != 0 || !errors.Is(err, io.EOF) {
		if err == nil {
			err = fmt.Errorf("输入有超出frames的尾随数据")
		}
		return measurementReport{}, fmt.Errorf("input end: %w", err)
	}
	start := time.Now()
	err = e.Flush()
	elapsed := time.Since(start)
	flushTimes = append(flushTimes, elapsed)
	if err != nil {
		return measurementReport{}, err
	}
	if err := drain(); err != nil {
		return measurementReport{}, err
	}
	if r.Packets != cfg.Frames {
		return measurementReport{}, fmt.Errorf("incomplete drain: packets %d frames %d", r.Packets, cfg.Frames)
	}
	r.Memory.After, err = sampleHeap(&after)
	if err != nil {
		return measurementReport{}, err
	}
	r.Memory.observe(r.Memory.After)
	metrics.Read(gc.after)
	r.WindowNanos = time.Since(windowStart).Nanoseconds()
	r.Memory.TotalAlloc = after.TotalAlloc - before.TotalAlloc
	r.Memory.Mallocs = after.Mallocs - before.Mallocs
	r.Memory.NaturalNumGC = after.NumGC - before.NumGC
	r.InputSHA256 = hex.EncodeToString(input.Sum(nil))
	r.StreamSHA256 = hex.EncodeToString(output.Sum(nil))
	r.Send, r.Receive, r.Flush = summarizeCalls(sendTimes), summarizeCalls(receiveTimes), summarizeCalls(flushTimes)
	r.CodecCallNanos = r.Send.TotalNanos + r.Receive.TotalNanos + r.Flush.TotalNanos
	r.GCMetrics, r.GCUnavailable = gc.report()
	if err := e.Close(); err != nil {
		return measurementReport{}, err
	}
	e = nil
	f = nil
	// 只在自然窗口之后主动GC一次；不得用它抹去自然GC/assist成本。
	runtime.GC()
	var retained runtime.MemStats
	r.Memory.PostCloseGC, err = sampleHeap(&retained)
	if err != nil {
		return measurementReport{}, err
	}
	runtime.KeepAlive(reader)
	runtime.KeepAlive(writer)
	return r, nil
}
