package codec_test

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/decoder"
	"github.com/cry5tallize/goavc/encoder"
)

func TestEncoder_VBVHighScenePressureOracle(t *testing.T) {
	c := h264.DefaultEncoderConfig()
	c.Width, c.Height, c.Profile = 96, 64, h264.ProfileHigh
	c.GOPSize, c.IDRInterval, c.MaxBFrames = 120, 120, 2
	c.RateControl, c.Bitrate, c.VBVBufferSize = h264.RateControlVBV, 120_000, 24_000
	e, err := encoder.New(c, encoder.WithLookahead(2), encoder.WithSceneChangeThreshold(20), encoder.WithTransform8x8(), encoder.WithWeightedPrediction())
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	d, err := decoder.New(h264.DecoderConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var stream []byte
	recons := map[int64][]byte{}
	var keys []int64
	fullness := int64(c.VBVBufferSize / 2)
	packets := 0
	decoded := 0
	drainDecoder := func() {
		t.Helper()
		for {
			f, err := d.ReceiveFrame()
			if errors.Is(err, h264.ErrAgain) || errors.Is(err, h264.ErrEOF) {
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if f.PTS != int64(decoded) || !bytes.Equal(packed(f), recons[f.PTS]) {
				f.Release()
				t.Fatal("突变压力下Go Decoder显示顺序或像素错误", decoded)
			}
			f.Release()
			decoded++
		}
	}
	drain := func() {
		t.Helper()
		for {
			p, err := e.ReceivePacket()
			if errors.Is(err, h264.ErrAgain) || errors.Is(err, h264.ErrEOF) {
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			packets++
			fullness += int64(len(p.Data)*8 - c.Bitrate/30)
			stats := e.RateControlStats()
			if fullness != stats.VBVFullness || fullness < 0 || fullness > int64(c.VBVBufferSize) {
				t.Fatalf("PTS%d突变压力下VBV错误：%d/%d", p.PTS, fullness, stats.VBVFullness)
			}
			if p.KeyFrame {
				keys = append(keys, p.PTS)
			}
			r, err := e.Reconstruction()
			if err != nil {
				t.Fatal(err)
			}
			recons[p.PTS] = packed(r)
			r.Release()
			if err = d.SendPacket(p); err != nil {
				t.Fatal(err)
			}
			drainDecoder()
			stream = append(stream, p.Data...)
		}
	}
	for i := 0; i < 120; i++ {
		f := movingFrame(c.Width, c.Height, i)
		f.PTS, f.Duration = int64(i), 1
		for comp := range f.Planes {
			w, h := f.Format.PlaneDimensions(comp, f.Width, f.Height)
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					v := 20 + (x+i+y*3+comp*11)%61
					if i >= 40 && i < 80 {
						v = 200 + ((x+i)/4+y/4+comp)%2*40
					}
					if i == 60 { // 单帧闪光不得重置GOP。
						v = 0
					}
					f.Planes[comp].Data[y*f.Planes[comp].Stride+x] = byte(v)
				}
			}
		}
		if err = e.SendFrame(f); err != nil {
			t.Fatalf("输入%d：%v", i, err)
		}
		f.Release()
		drain()
	}
	if err = e.Flush(); err != nil {
		t.Fatal(err)
	}
	drain()
	if err = d.Flush(); err != nil {
		t.Fatal(err)
	}
	drainDecoder()
	if packets != 120 || decoded != 120 || !reflect.DeepEqual(keys, []int64{0, 40, 80}) {
		t.Fatalf("突变/闪光调度错误：包%d IDR%v", packets, keys)
	}
	var want bytes.Buffer
	for i := int64(0); i < 120; i++ {
		want.Write(recons[i])
	}
	ffmpegCheck(t, stream, want.Bytes())
}
