// encodebench 生成固定六类 YUV 图案，报告码率、PSNR 与编码延迟。
// 仅为开发期测量工具；不参与编解码运行时，不调用外部编解码库。
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"hash"
	"io"
	"math"
	"os"
	"runtime"
	"sort"
	"time"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/decoder"
	"github.com/goavc/h264/encoder"
	"github.com/goavc/h264/internal/testutil/pattern"
	"github.com/goavc/h264/nal"
)

type report struct {
	GOPSize                                             int
	Width, Height, Frames, QP, FPS, SliceCount          int
	PCM                                                 bool
	Go, InputSHA256, StreamSHA256, ReconstructionSHA256 string
	Bytes                                               int
	BitrateKbps, EncodeFPS, MeanMillis, P95Millis       float64
	// nil 表示该平面完全一致（PSNR=+Inf，JSON 不允许浮点无穷）。
	PSNR                             [3]*float64
	HeapSysBytes                     uint64
	Profile                          string
	MaxBFrames, IDRInterval, Bitrate int
	RateControl                      string
	QPSequence                       []int
	MinVBVFullness, MaxVBVFullness   int64
	PictureCounts                    map[string]int
}

func main() {
	c := h264.DefaultEncoderConfig()
	flag.IntVar(&c.Width, "width", 1280, "显示宽度")
	flag.IntVar(&c.Height, "height", 720, "显示高度")
	flag.IntVar(&c.QP, "qp", 26, "CQP 0..51")
	flag.IntVar(&c.GOPSize, "gop", 1, "IDR 间隔，1 为帧内基线")
	flag.IntVar(&c.FPSNum, "fps", 30, "整数帧率")
	flag.StringVar(&c.Profile, "profile", h264.ProfileBaseline, "baseline/main/high")
	flag.IntVar(&c.MaxBFrames, "bframes", 0, "连续B帧上限0..16")
	flag.IntVar(&c.IDRInterval, "idr", 0, "独立IDR间隔，0跟随GOP")
	flag.IntVar(&c.Bitrate, "bitrate", 0, "目标bit/s")
	flag.IntVar(&c.VBVBufferSize, "vbv-buffer", 0, "VBV缓冲bit")
	rc := flag.String("rc", "cqp", "cqp/abr/cbr/vbv")
	weight := flag.Bool("weighted", false, "P显式自动权重")
	t8 := flag.Bool("transform8", false, "High 8×8候选")
	lookahead := flag.Int("lookahead", 0, "未来帧窗口，0关闭")
	verify := flag.Bool("verify", false, "与自研解码逐帧比较")
	reconPath := flag.String("recon", "", "显示顺序重建YUV输出路径")
	frames := flag.Int("frames", 6, "帧数，图案按六类循环")
	slices := flag.Int("slices", 1, "每帧 slice 数")
	pcm := flag.Bool("pcm", false, "强制 I_PCM")
	output := flag.String("output", "", "可选 Annex-B 输出文件")
	inputPath := flag.String("input", "", "可选紧密 YUV420P 文件；未指定时生成六类图案")
	flag.Parse()
	switch *rc {
	case "cqp":
		c.RateControl = h264.RateControlCQP
	case "abr":
		c.RateControl = h264.RateControlABR
	case "cbr":
		c.RateControl = h264.RateControlCBR
	case "vbv":
		c.RateControl = h264.RateControlVBV
	default:
		fmt.Fprintln(os.Stderr, "invalid rc")
		os.Exit(1)
	}
	var options []encoder.Option
	if *weight {
		options = append(options, encoder.WithWeightedPrediction())
	}
	if *t8 {
		options = append(options, encoder.WithTransform8x8())
	}
	if *lookahead > 0 {
		options = append(options, encoder.WithLookahead(*lookahead))
	}
	var reconWriter io.Writer
	if *reconPath != "" {
		f, err := os.Create(*reconPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer f.Close()
		reconWriter = f
	}
	var input io.Reader
	if *inputPath == "-" {
		input = os.Stdin
	} else if *inputPath != "" {
		f, err := os.Open(*inputPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer f.Close()
		input = f
	}
	result, stream, err := measureConfigured(c, *frames, *slices, *pcm, input, options, reconWriter, *verify)
	if err == nil && *output != "" {
		err = os.WriteFile(*output, stream, 0644)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err = enc.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func measure(c h264.EncoderConfig, count, slices int, pcm bool) (report, []byte, error) {
	return measureInput(c, count, slices, pcm, nil)
}

func measureInput(c h264.EncoderConfig, count, slices int, pcm bool, reader io.Reader) (report, []byte, error) {
	return measureConfigured(c, count, slices, pcm, reader, nil, nil, false)
}

func measureConfigured(c h264.EncoderConfig, count, slices int, pcm bool, reader io.Reader, extra []encoder.Option, reconWriter io.Writer, verify bool) (report, []byte, error) {
	if count < 1 || count > 600 {
		return report{}, nil, fmt.Errorf("frames must be 1..600")
	}
	options := []encoder.Option{encoder.WithSliceCount(slices)}
	options = append(options, extra...)
	if pcm {
		options = append(options, encoder.WithPCM())
	}
	e, err := encoder.New(c, options...)
	if err != nil {
		return report{}, nil, err
	}
	defer e.Close()
	r := report{Width: c.Width, Height: c.Height, Frames: count, QP: c.QP, FPS: c.FPSNum, SliceCount: slices, PCM: pcm, Go: runtime.Version(), GOPSize: c.GOPSize}
	r.Profile = c.Profile
	r.MaxBFrames = c.MaxBFrames
	r.IDRInterval = c.IDRInterval
	r.Bitrate = c.Bitrate
	r.RateControl = c.RateControl.String()
	r.PictureCounts = map[string]int{}
	input, output, recon := sha256.New(), sha256.New(), sha256.New()
	var stream []byte
	var sums [3]uint64
	var samples [3]uint64
	var durations []time.Duration
	var encodeElapsed time.Duration
	originals := map[int64]*h264.Frame{}
	reconstructions := map[int64]*h264.Frame{}
	defer func() {
		for _, f := range originals {
			f.Release()
		}
		for _, f := range reconstructions {
			f.Release()
		}
	}()
	var d *decoder.Decoder
	if verify {
		d, err = decoder.New(h264.DecoderConfig{})
		if err != nil {
			return r, nil, err
		}
		defer d.Close()
	}
	nextRecon, decodedCount, packets := int64(0), 0, 0
	checkDecoded := func() error {
		if d == nil {
			return nil
		}
		for {
			f, err := d.ReceiveFrame()
			if errors.Is(err, h264.ErrAgain) || errors.Is(err, h264.ErrEOF) {
				return nil
			}
			if err != nil {
				return err
			}
			expected := reconstructions[f.PTS]
			if expected == nil {
				f.Release()
				return fmt.Errorf("missing reconstruction PTS %d", f.PTS)
			}
			if f.PTS != int64(decodedCount) {
				f.Release()
				return fmt.Errorf("decoder display order %d expected %d", f.PTS, decodedCount)
			}
			for comp := range f.Planes {
				w, h := f.Format.PlaneDimensions(comp, f.Width, f.Height)
				for y := 0; y < h; y++ {
					for x := 0; x < w; x++ {
						if f.Planes[comp].Data[y*f.Planes[comp].Stride+x] != expected.Planes[comp].Data[y*expected.Planes[comp].Stride+x] {
							f.Release()
							return fmt.Errorf("decoder pixel mismatch frame %d component %d (%d,%d)", decodedCount, comp, x, y)
						}
					}
				}
			}
			f.Release()
			decodedCount++
		}
	}
	consumeDisplay := func() error {
		for {
			f := reconstructions[nextRecon]
			if f == nil || d != nil && int64(decodedCount) <= nextRecon {
				return nil
			}
			hashFrame(recon, f)
			if reconWriter != nil {
				for comp := range f.Planes {
					w, h := f.Format.PlaneDimensions(comp, f.Width, f.Height)
					for y := 0; y < h; y++ {
						if _, err := reconWriter.Write(f.Planes[comp].Data[y*f.Planes[comp].Stride : y*f.Planes[comp].Stride+w]); err != nil {
							return err
						}
					}
				}
			}
			f.Release()
			delete(reconstructions, nextRecon)
			nextRecon++
		}
	}
	drain := func() error {
		for {
			start := time.Now()
			p, err := e.ReceivePacket()
			encodeElapsed += time.Since(start)
			if errors.Is(err, h264.ErrAgain) || errors.Is(err, h264.ErrEOF) {
				return nil
			}
			if err != nil {
				return err
			}
			stream = append(stream, p.Data...)
			output.Write(p.Data)
			if err := countPicture(r.PictureCounts, p.Data); err != nil {
				return err
			}
			packets++
			decoded, err := e.Reconstruction()
			if err != nil {
				return err
			}
			original := originals[p.PTS]
			if original == nil {
				decoded.Release()
				return fmt.Errorf("missing input PTS %d", p.PTS)
			}
			for comp := range decoded.Planes {
				w, h := decoded.Format.PlaneDimensions(comp, decoded.Width, decoded.Height)
				samples[comp] += uint64(w * h)
				for y := 0; y < h; y++ {
					for x := 0; x < w; x++ {
						delta := int(original.Planes[comp].Data[y*original.Planes[comp].Stride+x]) - int(decoded.Planes[comp].Data[y*decoded.Planes[comp].Stride+x])
						sums[comp] += uint64(delta * delta)
					}
				}
			}
			original.Release()
			delete(originals, p.PTS)
			reconstructions[p.PTS] = decoded
			r.QPSequence = append(r.QPSequence, e.LastQP())
			stats := e.RateControlStats()
			if packets == 1 {
				r.MinVBVFullness = stats.VBVFullness
			}
			r.MinVBVFullness = min(r.MinVBVFullness, stats.VBVFullness)
			r.MaxVBVFullness = max(r.MaxVBVFullness, stats.VBVFullness)
			if d != nil {
				if err = d.SendPacket(p); err != nil {
					return err
				}
				if err = checkDecoded(); err != nil {
					return err
				}
			}
			if err = consumeDisplay(); err != nil {
				return err
			}
		}
	}
	for i := 0; i < count; i++ {
		var f *h264.Frame
		if reader == nil {
			f = pattern.Frame(c.Width, c.Height, i)
		} else {
			f = &h264.Frame{Width: c.Width, Height: c.Height, Format: h264.YUV420P, PTS: int64(i), Duration: 1}
			for comp := range f.Planes {
				w, h := f.Format.PlaneDimensions(comp, c.Width, c.Height)
				f.Planes[comp] = h264.Plane{Data: make([]byte, w*h), Stride: w}
				if _, err = io.ReadFull(reader, f.Planes[comp].Data); err != nil {
					return r, nil, fmt.Errorf("input frame %d plane %d: %w", i, comp, err)
				}
			}
		}
		hashFrame(input, f)
		f.PTS = int64(i)
		f.Duration = 1
		originals[f.PTS] = f
		beforeInput := encodeElapsed
		start := time.Now()
		err = e.SendFrame(f)
		encodeElapsed += time.Since(start)
		if err != nil {
			return r, nil, err
		}
		if err = drain(); err != nil {
			return r, nil, err
		}
		durations = append(durations, encodeElapsed-beforeInput)
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)
		r.HeapSysBytes = max(r.HeapSysBytes, mem.HeapSys)
	}
	beforeFlush := encodeElapsed
	start := time.Now()
	if err = e.Flush(); err != nil {
		return r, nil, err
	}
	encodeElapsed += time.Since(start)
	if err = drain(); err != nil {
		return r, nil, err
	}
	durations[len(durations)-1] += encodeElapsed - beforeFlush
	if d != nil {
		if err = d.Flush(); err != nil {
			return r, nil, err
		}
		if err = checkDecoded(); err != nil {
			return r, nil, err
		}
		if err = consumeDisplay(); err != nil {
			return r, nil, err
		}
	}
	if packets != count || nextRecon != int64(count) {
		return r, nil, fmt.Errorf("incomplete drain: packets %d recon %d count %d", packets, nextRecon, count)
	}
	elapsed := encodeElapsed
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	r.InputSHA256 = hex.EncodeToString(input.Sum(nil))
	r.StreamSHA256 = hex.EncodeToString(output.Sum(nil))
	r.ReconstructionSHA256 = hex.EncodeToString(recon.Sum(nil))
	r.Bytes = len(stream)
	r.BitrateKbps = float64(len(stream)*8) * float64(c.FPSNum) / float64(count) / 1000
	r.EncodeFPS = float64(count) / elapsed.Seconds()
	r.MeanMillis = float64(elapsed) / float64(count) / 1e6
	r.P95Millis = float64(durations[(count*95+99)/100-1]) / 1e6
	for comp := range r.PSNR {
		r.PSNR[comp] = psnr(sums[comp], samples[comp])
	}
	return r, stream, nil
}

// 只读取不受CABAC尾部填充影响的first_mb/slice_type前缀；每AU的首slice计一次。
func countPicture(counts map[string]int, data []byte) error {
	scanner := nal.NewAnnexBScanner(bytes.NewReader(data))
	for {
		u, err := scanner.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if u.Header.Type != nal.TypeSlice && u.Header.Type != nal.TypeSliceIDR {
			continue
		}
		payload := u.Payload
		if len(payload) >= 3 && bytes.Equal(payload[len(payload)-3:], []byte{0, 0, 3}) {
			payload = payload[:len(payload)-1]
		}
		rb, err := bitstream.EBSPToRBSP(payload)
		if err != nil {
			return err
		}
		reader := bitstream.NewReader(rb)
		first, err := reader.ReadUE()
		if err != nil {
			return err
		}
		typ, err := reader.ReadUE()
		if err != nil {
			return err
		}
		if first == 0 {
			name := "I"
			switch typ % 5 {
			case 0:
				name = "P"
			case 1:
				name = "B"
			}
			if u.Header.IsIDR() {
				name = "IDR"
			}
			counts[name]++
			return nil
		}
	}
}

func hashFrame(h hash.Hash, f *h264.Frame) {
	for c := 0; c < 3; c++ {
		w, rows := f.Format.PlaneDimensions(c, f.Width, f.Height)
		for y := 0; y < rows; y++ {
			h.Write(f.Planes[c].Data[y*f.Planes[c].Stride : y*f.Planes[c].Stride+w])
		}
	}
}
func psnr(sse, n uint64) *float64 {
	if sse == 0 {
		return nil
	}
	value := 10 * math.Log10(255*255*float64(n)/float64(sse))
	return &value
}
