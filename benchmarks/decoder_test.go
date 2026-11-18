package benchmarks_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/decoder"
	"github.com/cry5tallize/goavc/stream"
)

// BenchmarkDecoder_Sequence测完整固定序列，输入组AU与像素哈希不计时。
// 每op独立实例，包含New/Send/Receive/Release/Flush/Close，是cold端点而非稳态。
// 外部FFmpeg有效像素SHA为必填，不允许漏帧或无验证的吞吐样本。
func BenchmarkDecoder_Sequence(b *testing.B) {
	path := os.Getenv("GOAVC_M8_INPUT")
	if path == "" {
		b.Skip("需GOAVC_M8_INPUT/GOAVC_M8_FRAMES/GOAVC_M8_YUV_SHA256")
	}
	frames, err := strconv.Atoi(os.Getenv("GOAVC_M8_FRAMES"))
	if err != nil || frames < 1 {
		b.Fatal("GOAVC_M8_FRAMES须为正整数")
	}
	threads := 1
	if value := os.Getenv("GOAVC_M8_THREADS"); value != "" {
		threads, err = strconv.Atoi(value)
		if err != nil || threads < 0 {
			b.Fatal("GOAVC_M8_THREADS 须为非负整数")
		}
	}
	want, err := hex.DecodeString(os.Getenv("GOAVC_M8_YUV_SHA256"))
	if err != nil || len(want) != sha256.Size {
		b.Fatal("需独立FFmpeg原生有效像素SHA-256")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		b.Fatal(err)
	}
	nr, err := stream.NewAnnexBReader(bytes.NewReader(data), 0)
	if err != nil {
		b.Fatal(err)
	}
	ar, err := stream.NewAccessUnitReader(nr, stream.AccessUnitConfig{})
	if err != nil {
		b.Fatal(err)
	}
	var packets []h264.Packet
	for {
		p, err := ar.ReadAccessUnit()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			b.Fatal(err)
		}
		packets = append(packets, p)
	}
	if len(packets) != frames {
		b.Fatalf("AU数%d，需%d", len(packets), frames)
	}
	b.Logf("input=%s sha256=%x frames=%d AUs=%d threads=%d", path, sha256.Sum256(data), frames, len(packets), threads)
	b.ReportAllocs()
	hash := sha256.New()
	// 独立诊断 SendPacket 的墙钟长尾，不混入正式吞吐比较。
	var latency []time.Duration
	if os.Getenv("GOAVC_M8_AU_LATENCY") == "1" {
		latency = make([]time.Duration, 0, frames)
	}
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		hash.Reset()
		b.StartTimer()
		d, err := decoder.New(h264.DecoderConfig{Threads: threads})
		if err != nil {
			b.Fatal(err)
		}
		count := 0
		drain := func() error {
			for {
				f, err := d.ReceiveFrame()
				if errors.Is(err, h264.ErrAgain) || errors.Is(err, h264.ErrEOF) {
					return nil
				}
				if err != nil {
					return err
				}
				b.StopTimer()
				for c := range f.Planes {
					w, h := f.Format.PlaneDimensions(c, f.Width, f.Height)
					if f.Format.BitDepth() > 8 {
						w *= 2
					}
					for y := range h {
						_, _ = hash.Write(f.Planes[c].Data[y*f.Planes[c].Stride : y*f.Planes[c].Stride+w])
					}
				}
				b.StartTimer()
				f.Release()
				count++
			}
		}
		for _, p := range packets {
			if latency != nil {
				start := time.Now()
				err = d.SendPacket(p)
				latency = append(latency, time.Since(start))
			} else {
				err = d.SendPacket(p)
			}
			if err == nil {
				err = drain()
			}
			if err != nil {
				break
			}
		}
		if err == nil {
			err = d.Flush()
		}
		if err == nil {
			err = drain()
		}
		d.Close()
		b.StopTimer()
		if err != nil {
			b.Fatal(err)
		}
		if count != frames || !bytes.Equal(hash.Sum(nil), want) {
			b.Fatal(fmt.Sprintf("输出%d/%d帧，SHA=%x，期望%x", count, frames, hash.Sum(nil), want))
		}
		if latency != nil {
			slices.Sort(latency)
			b.Logf("SendPacket p95=%s max=%s n=%d", latency[(95*len(latency)+99)/100-1], latency[len(latency)-1], len(latency))
			latency = latency[:0]
		}
		b.StartTimer()
	}
	b.ReportMetric(float64(b.N*frames)/b.Elapsed().Seconds(), "frames/s")
	b.ReportMetric(float64(frames), "frames/op")
}
