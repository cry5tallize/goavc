package codec_test

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/decoder"
	"github.com/goavc/h264/encoder"
)

type m8Snapshot struct {
	Name                            string
	Input, Stream, Metadata, Pixels string
	Packets, Frames                 int
}

// 指纹在 M7 的原始串行算法上采集；只证明优化前后不变，规范正确性仍由
// chromaEncoderOracle 的解析向量及 FFmpeg 保护。本测试没有更新 golden 的开关。
func TestM8Baseline_SerialOutput(t *testing.T) {
	var got []m8Snapshot
	for _, format := range chromaEncoderFormats {
		for _, cabac := range []bool{false, true} {
			got = append(got, m8Capture(t, format, cabac))
		}
	}
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("M8 baseline snapshot: %s", data)
	wantData, err := os.ReadFile("testdata/m8_baseline.json")
	if err != nil {
		t.Fatal(err)
	}
	var want []m8Snapshot
	if err := json.Unmarshal(wantData, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("M7 输出指纹变化；禁止以新输出覆盖基线\ngot: %s\nwant: %s", data, wantData)
	}
}

func TestM8Baseline_WithoutFFmpeg(t *testing.T) {
	// Oracle缺失只跳过外部比较，不能连已固定的码流/元数据/像素回归一起跳过。
	t.Setenv("PATH", t.TempDir())
	TestM8Baseline_SerialOutput(t)
}

func m8Capture(t *testing.T, format h264.PixelFormat, cabac bool) m8Snapshot {
	t.Helper()
	cfg := chromaEncoderConfig(format)
	cfg.Threads, cfg.MaxBFrames = 1, 2
	e, err := encoder.New(cfg, encoder.WithCABAC(cabac), encoder.WithSliceCount(3),
		encoder.WithSearchRange(2), encoder.WithTransform8x8(),
		encoder.WithWeightedPrediction(), encoder.WithLookahead(2))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	d, err := decoder.New(h264.DecoderConfig{Threads: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	input, stream, metadata, pixels := sha256.New(), sha256.New(), sha256.New(), sha256.New()
	meta := json.NewEncoder(metadata)
	recons := make(map[int64][]byte)
	var display, encoded []byte
	snapshot := m8Snapshot{Name: fmt.Sprintf("%s/cabac=%t", format, cabac)}
	drainDecoder := func() {
		for {
			f, err := d.ReceiveFrame()
			if errors.Is(err, h264.ErrAgain) || errors.Is(err, h264.ErrEOF) {
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := chromaEncoderPacked(t, f, cfg)
			want, ok := recons[f.PTS]
			if !ok || f.PTS != int64(snapshot.Frames)*100 {
				t.Fatalf("帧%d PTS=%d 没有对应的本地重建", snapshot.Frames, f.PTS)
			}
			chromaEncoderEqual(t, "M8 baseline decoder", got, want, format.BytesPerSample())
			pixels.Write(got)
			display = append(display, got...)
			if err := meta.Encode(struct {
				PTS, Duration int64
				KeyFrame      bool
			}{f.PTS, f.Duration, f.KeyFrame}); err != nil {
				t.Fatal(err)
			}
			snapshot.Frames++
			f.Release()
		}
	}
	drainEncoder := func() {
		for {
			p, err := e.ReceivePacket()
			if errors.Is(err, h264.ErrAgain) || errors.Is(err, h264.ErrEOF) {
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			r, err := e.Reconstruction()
			if err != nil {
				t.Fatal(err)
			}
			if r.PTS != p.PTS || recons[p.PTS] != nil {
				t.Fatalf("包 PTS=%d 重建 PTS=%d 重复或不同步", p.PTS, r.PTS)
			}
			recons[p.PTS] = chromaEncoderPacked(t, r, cfg)
			r.Release()
			stream.Write(p.Data)
			encoded = append(encoded, p.Data...)
			if err := meta.Encode(struct {
				PTS, DTS, Duration int64
				KeyFrame           bool
				Bytes              int
				Stats              encoder.RateControlStats
			}{p.PTS, p.DTS, p.Duration, p.KeyFrame, len(p.Data), e.RateControlStats()}); err != nil {
				t.Fatal(err)
			}
			snapshot.Packets++
			if err := d.SendPacket(p); err != nil {
				t.Fatal(err)
			}
			drainDecoder()
		}
	}
	const count = 8
	for i := range count {
		f := chromaEncoderFrame(cfg, i)
		input.Write(chromaEncoderPacked(t, f, cfg))
		if err := e.SendFrame(f); err != nil {
			f.Release()
			t.Fatal(err)
		}
		f.Release()
		drainEncoder()
	}
	if err := e.Flush(); err != nil {
		t.Fatal(err)
	}
	drainEncoder()
	if err := d.Flush(); err != nil {
		t.Fatal(err)
	}
	drainDecoder()
	if snapshot.Packets != count || snapshot.Frames != count {
		t.Fatalf("排空后包=%d 帧=%d，期望%d", snapshot.Packets, snapshot.Frames, count)
	}
	snapshot.Input, snapshot.Stream = fmt.Sprintf("%x", input.Sum(nil)), fmt.Sprintf("%x", stream.Sum(nil))
	snapshot.Metadata, snapshot.Pixels = fmt.Sprintf("%x", metadata.Sum(nil)), fmt.Sprintf("%x", pixels.Sum(nil))
	t.Run(snapshot.Name+"/FFmpeg", func(t *testing.T) {
		chromaEncoderFFmpegCheck(t, encoded, display, format)
	})
	return snapshot
}
