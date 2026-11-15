package encoder

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/internal/testutil/pattern"
)

// 在纯分析复用前固定真实RC输出；包含场/独立平面、位深低位、权重和AQ。
func TestRCAnalysis_FrozenOutputs(t *testing.T) {
	hash := sha256.New()
	for _, format := range []h264.PixelFormat{h264.YUV420P, h264.YUV420P10LE, h264.YUV422P, h264.YUV422P10LE, h264.YUV444P, h264.YUV444P10LE} {
		for _, field := range []bool{false, true} {
			separates := []bool{false}
			if sub, _ := format.ChromaSubsampling(); sub == 1 {
				separates = append(separates, true)
			}
			for _, separate := range separates {
				for _, rc := range []h264.RateControl{h264.RateControlABR, h264.RateControlCBR, h264.RateControlVBV} {
					cfg := h264.DefaultEncoderConfig()
					cfg.Width, cfg.Height, cfg.Profile, cfg.PixelFormat = 32, 32, h264.ProfileHigh444, format
					cfg.GOPSize, cfg.IDRInterval, cfg.Level = 20, 20, 31
					cfg.RateControl, cfg.Bitrate, cfg.VBVBufferSize = rc, 60_000, 60_000
					opts := []Option{WithWeightedPrediction(), WithAdaptiveQP(3), WithTransform8x8(), WithCABAC(true)}
					if field {
						opts = append(opts, WithInterlaced(true))
					}
					if separate {
						opts = append(opts, WithSeparateColourPlanes())
					}
					e, err := New(cfg, opts...)
					if err != nil {
						t.Fatal(err)
					}
					f, err := h264.NewFrame(32, 32, format)
					if err != nil {
						t.Fatal(err)
					}
					for n := range 3 {
						f.PTS, f.Duration = int64(n), 1
						for c := range f.Planes {
							p := &f.Planes[c]
							w, rows := format.PlaneDimensions(c, 32, 32)
							for y := range rows {
								for x := range w {
									v := uint16(40 + (x*3+y*5+c*17)%120 + n*9)
									if format.BitDepth() > 8 {
										binary.LittleEndian.PutUint16(p.Data[y*p.Stride+x*2:], v*4+uint16((x+y+n)%4))
									} else {
										p.Data[y*p.Stride+x] = byte(v)
									}
								}
							}
						}
						if err = e.SendFrame(f); err != nil {
							t.Fatal(format, field, separate, rc, n, err)
						}
						packet, err := e.ReceivePacket()
						if err != nil {
							t.Fatal(err)
						}
						hash.Write(packet.Data)
						stats := e.RateControlStats()
						_ = binary.Write(hash, binary.LittleEndian, [6]int64{packet.PTS, packet.DTS, packet.Duration, int64(stats.QP), stats.LastFrameBits, stats.VBVFullness})
						r, err := e.Reconstruction()
						if err != nil {
							t.Fatal(err)
						}
						for c := range r.Planes {
							hash.Write(r.Planes[c].Data)
						}
						r.Release()
					}
					f.Release()
					e.Close()
				}
			}
		}
	}
	const want = "23a73ef1835d008854359e4cfa23c28ec3292785fa555de74ab230d244094b16"
	if got := fmt.Sprintf("%x", hash.Sum(nil)); got != want {
		t.Fatalf("RC码流/重建/时钟/QP/VBV改变: got %s want %s", got, want)
	}
}

func TestRCAnalysis_FailedSendMatchesFresh(t *testing.T) {
	cfg := h264.DefaultEncoderConfig()
	cfg.Width, cfg.Height, cfg.Profile = 32, 32, h264.ProfileHigh444
	cfg.PixelFormat, cfg.RateControl, cfg.Bitrate, cfg.Level = h264.YUV444P, h264.RateControlABR, 60_000, 31
	weights := PredictionWeights{Weight: [3]int32{1, 1, 1}, Offset: [3]int32{5, -3, 7}}
	options := []Option{WithPredictionWeights(weights), WithAdaptiveQP(3), WithInterlaced(true), WithSeparateColourPlanes()}
	reused, err := New(cfg, options...)
	if err != nil {
		t.Fatal(err)
	}
	defer reused.Close()
	fresh, err := New(cfg, options...)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	f, err := h264.NewFrame(32, 32, cfg.PixelFormat)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Release()
	for c := range f.Planes {
		for i := range f.Planes[c].Data {
			f.Planes[c].Data[i] = byte(40 + (i+c*13)%100)
		}
	}
	for _, e := range []*Encoder{reused, fresh} {
		if err = e.SendFrame(f); err != nil {
			t.Fatal(err)
		}
		if _, err = e.ReceivePacket(); err != nil {
			t.Fatal(err)
		}
	}
	budget := reused.maxAUBytes
	reused.maxAUBytes = 1
	if err = reused.SendFrame(f); !errors.Is(err, h264.ErrResourceLimit) {
		t.Fatal(err)
	}
	reused.maxAUBytes = budget
	// 失败后更换输入，缓存不得把上一次源分类或搜索参考带入新调用。
	for c := range f.Planes {
		for i := range f.Planes[c].Data {
			f.Planes[c].Data[i] = byte(100 + (i+c*17)%120)
		}
	}
	if err = reused.SendFrame(f); err != nil {
		t.Fatal(err)
	}
	if err = fresh.SendFrame(f); err != nil {
		t.Fatal(err)
	}
	a, err := reused.ReceivePacket()
	if err != nil {
		t.Fatal(err)
	}
	b, err := fresh.ReceivePacket()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Data, b.Data) || reused.RateControlStats() != fresh.RateControlStats() {
		t.Fatal("失败分析缓存污染重试的码流或RC状态")
	}
}

func BenchmarkEncoderRCAnalysis_320x240(b *testing.B) {
	cfg := h264.DefaultEncoderConfig()
	cfg.Width, cfg.Height, cfg.Profile = 320, 240, h264.ProfileHigh
	cfg.GOPSize, cfg.IDRInterval, cfg.Level = 600, 600, h264.LevelAuto
	cfg.RateControl, cfg.Bitrate = h264.RateControlABR, 500_000
	e, err := New(cfg, WithAdaptiveQP(3), WithWeightedPrediction())
	if err != nil {
		b.Fatal(err)
	}
	defer e.Close()
	f := pattern.Frame(320, 240, 1)
	defer f.Release()
	if err = e.SendFrame(f); err != nil {
		b.Fatal(err)
	}
	if _, err = e.ReceivePacket(); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err = e.SendFrame(f); err != nil {
			b.Fatal(err)
		}
		if _, err = e.ReceivePacket(); err != nil {
			b.Fatal(err)
		}
	}
}
