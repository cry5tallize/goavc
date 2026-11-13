// encodebench 生成固定六类 YUV 图案，报告码率、PSNR 与编码延迟。
// 仅为开发期测量工具；不参与编解码运行时，不调用外部编解码库。
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	"github.com/goavc/h264/encoder"
	"github.com/goavc/h264/internal/testutil/pattern"
)

type report struct {
	GOPSize                                             int
	Width, Height, Frames, QP, FPS, SliceCount          int
	PCM                                                 bool
	Go, InputSHA256, StreamSHA256, ReconstructionSHA256 string
	Bytes                                               int
	BitrateKbps, EncodeFPS, MeanMillis, P95Millis       float64
	// nil 表示该平面完全一致（PSNR=+Inf，JSON 不允许浮点无穷）。
	PSNR         [3]*float64
	HeapSysBytes uint64
}

func main() {
	c := h264.DefaultEncoderConfig()
	flag.IntVar(&c.Width, "width", 1280, "显示宽度")
	flag.IntVar(&c.Height, "height", 720, "显示高度")
	flag.IntVar(&c.QP, "qp", 26, "CQP 0..51")
	flag.IntVar(&c.GOPSize, "gop", 1, "IDR 间隔，1 为帧内基线")
	flag.IntVar(&c.FPSNum, "fps", 30, "整数帧率")
	frames := flag.Int("frames", 6, "帧数，图案按六类循环")
	slices := flag.Int("slices", 1, "每帧 slice 数")
	pcm := flag.Bool("pcm", false, "强制 I_PCM")
	output := flag.String("output", "", "可选 Annex-B 输出文件")
	inputPath := flag.String("input", "", "可选紧密 YUV420P 文件；未指定时生成六类图案")
	flag.Parse()
	var input io.Reader
	if *inputPath != "" {
		f, err := os.Open(*inputPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer f.Close()
		input = f
	}
	result, stream, err := measureInput(c, *frames, *slices, *pcm, input)
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
	if count < 1 || count > 600 {
		return report{}, nil, fmt.Errorf("frames must be 1..600")
	}
	options := []encoder.Option{encoder.WithSliceCount(slices)}
	if pcm {
		options = append(options, encoder.WithPCM())
	}
	e, err := encoder.New(c, options...)
	if err != nil {
		return report{}, nil, err
	}
	defer e.Close()
	r := report{Width: c.Width, Height: c.Height, Frames: count, QP: c.QP, FPS: c.FPSNum, SliceCount: slices, PCM: pcm, Go: runtime.Version(), GOPSize: c.GOPSize}
	input, output, recon := sha256.New(), sha256.New(), sha256.New()
	var stream []byte
	var sums [3]uint64
	var samples [3]uint64
	var durations []time.Duration
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
		start := time.Now()
		if err = e.SendFrame(f); err != nil {
			return r, nil, err
		}
		p, err := e.ReceivePacket()
		if err != nil {
			return r, nil, err
		}
		durations = append(durations, time.Since(start))
		stream = append(stream, p.Data...)
		output.Write(p.Data)
		decoded, err := e.Reconstruction()
		if err != nil {
			return r, nil, err
		}
		hashFrame(recon, decoded)
		for comp := 0; comp < 3; comp++ {
			w, h := f.Format.PlaneDimensions(comp, f.Width, f.Height)
			samples[comp] += uint64(w * h)
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					d := int(f.Planes[comp].Data[y*f.Planes[comp].Stride+x]) - int(decoded.Planes[comp].Data[y*decoded.Planes[comp].Stride+x])
					sums[comp] += uint64(d * d)
				}
			}
		}
		decoded.Release()
		f.Release()
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)
		r.HeapSysBytes = max(r.HeapSysBytes, mem.HeapSys)
	}
	var elapsed time.Duration
	for _, d := range durations {
		elapsed += d
	}
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
