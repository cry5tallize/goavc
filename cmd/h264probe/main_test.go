package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/encoder"
)

func probeFixture(t *testing.T) []byte {
	return probeConfiguredFixture(t, 2, 4, 7, 12)
}

func probeConfiguredFixture(t *testing.T, depth, gop, idr, frames int) []byte {
	t.Helper()
	c := h264.DefaultEncoderConfig()
	c.Width, c.Height, c.Profile = 32, 32, h264.ProfileHigh
	c.GOPSize, c.IDRInterval, c.MaxBFrames = gop, idr, depth
	e, err := encoder.New(c, encoder.WithSliceCount(2), encoder.WithTransform8x8())
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	var stream []byte
	drain := func() {
		for {
			p, err := e.ReceivePacket()
			if errors.Is(err, h264.ErrAgain) || errors.Is(err, h264.ErrEOF) {
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			stream = append(stream, p.Data...)
		}
	}
	for i := 0; i < frames; i++ {
		f := &h264.Frame{Width: 32, Height: 32, Format: h264.YUV420P, PTS: int64(i), Duration: 1}
		for comp := range f.Planes {
			w, h := f.Format.PlaneDimensions(comp, 32, 32)
			f.Planes[comp] = h264.Plane{Data: bytes.Repeat([]byte{128}, w*h), Stride: w}
		}
		if err := e.SendFrame(f); err != nil {
			t.Fatal(err)
		}
		f.Release()
		drain()
	}
	if err := e.Flush(); err != nil {
		t.Fatal(err)
	}
	drain()
	return stream
}

func TestProbe_BDepthVariations(t *testing.T) {
	for _, depth := range []int{1, 2, 3, 16} {
		r, err := probe(bytes.NewReader(probeConfiguredFixture(t, depth, 40, 67, 40)), 40)
		if err != nil || r.MaxConsecutiveB != depth || len(r.Pictures) != 40 {
			t.Fatalf("连续B%d未反映在探测结果中：%+v %v", depth, r, err)
		}
	}
}

func TestProbe_GOPHeaderCountsAndLimits(t *testing.T) {
	stream := probeFixture(t)
	r, err := probe(bytes.NewReader(stream), 12)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Pictures) != 12 || len(r.IDRIndices) != 2 || r.IDRIndices[0] != 0 || r.IDRIndices[1] != 7 || r.Counts["I"] != 2 || r.MaxConsecutiveB != 2 {
		t.Fatalf("GOP探测未反映独立4/7周期和B2：%+v", r)
	}
	for _, p := range r.Pictures {
		if p.Width != 32 || p.Height != 32 || p.Profile != 100 || p.Slices != 2 {
			t.Fatal("参数集或多slice统计错误", p)
		}
	}
	if _, err := probe(bytes.NewReader(stream), 5); !errors.Is(err, h264.ErrResourceLimit) {
		t.Fatal("图片资源门禁未生效", err)
	}
	// first_mb=0、slice_type=P、pps_id=0，没有参数集时必须明确失败。
	if _, err := probe(bytes.NewReader([]byte{0, 0, 1, 0x61, 0xe0}), 5); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal("未知PPS被接受", err)
	}
}

func TestRun_GOPAssertionsFailClosed(t *testing.T) {
	file := filepath.Join(t.TempDir(), "gop.264")
	if err := os.WriteFile(file, probeFixture(t), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"-input", file, "-expect-frames", "12", "-expect-max-b", "2", "-expect-idr-interval", "7"}
	if err := run(args, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, wrong := range [][]string{{"-expect-frames", "11"}, {"-expect-max-b", "1"}, {"-expect-idr-interval", "6"}} {
		if err := run(append([]string{"-input", file}, wrong...), io.Discard, io.Discard); err == nil {
			t.Fatal("错误的GOP期望未导致失败", wrong)
		}
	}
}
