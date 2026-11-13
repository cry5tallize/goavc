// Package codec_test 验证两条公共流水线的互操作；测试位于独立包以保持 R1。
package codec_test

import (
	"bytes"
	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/decoder"
	"github.com/goavc/h264/encoder"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func pattern(w, h int) *h264.Frame {
	f := &h264.Frame{Width: w, Height: h, Format: h264.YUV420P}
	for c := range f.Planes {
		cw, ch := f.Format.PlaneDimensions(c, w, h)
		f.Planes[c] = h264.Plane{Data: make([]byte, cw*ch), Stride: cw}
		for y := 0; y < ch; y++ {
			for x := 0; x < cw; x++ {
				f.Planes[c].Data[y*cw+x] = byte(c*51 + x*3 + y*7)
			}
		}
	}
	return f
}
func packed(f *h264.Frame) []byte {
	var b []byte
	for c := range f.Planes {
		w, h := f.Format.PlaneDimensions(c, f.Width, f.Height)
		for y := 0; y < h; y++ {
			b = append(b, f.Planes[c].Data[y*f.Planes[c].Stride:y*f.Planes[c].Stride+w]...)
		}
	}
	return b
}

func TestEncoder_PCMCompatibility(t *testing.T) {
	for _, slices := range []int{1, 3} {
		c := h264.DefaultEncoderConfig()
		c.Width = 34
		c.Height = 18
		e, err := encoder.New(c, encoder.WithPCM(), encoder.WithSliceCount(slices))
		if err != nil {
			t.Fatal(err)
		}
		defer e.Close()
		d, err := decoder.New(h264.DecoderConfig{})
		if err != nil {
			t.Fatal(err)
		}
		defer d.Close()
		var stream, want []byte
		for i := 0; i < 3; i++ {
			f := pattern(34, 18)
			f.PTS = int64(i * 100)
			f.Planes[0].Data[0] = byte(i)
			if err = e.SendFrame(f); err != nil {
				t.Fatal(err)
			}
			r, err := e.Reconstruction()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(packed(f), packed(r)) {
				t.Fatal("PCM reconstruction differs")
			}
			p, err := e.ReceivePacket()
			if err != nil {
				t.Fatal(err)
			}
			stream = append(stream, p.Data...)
			want = append(want, packed(r)...)
			r.Release()
			if err = d.SendPacket(p); err != nil {
				t.Fatal(err)
			}
			got, err := d.ReceiveFrame()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(packed(got), packed(f)) || got.PTS != f.PTS {
				t.Fatal("PCM decoder differs")
			}
			got.Release()
		}
		ffmpegCheck(t, stream, want)
	}
}

func ffmpegCheck(t *testing.T, stream, want []byte) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("FFmpeg absent; independent Oracle skipped")
	}
	path := filepath.Join(t.TempDir(), "encoded.264")
	if err := os.WriteFile(path, stream, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("ffmpeg", "-v", "warning", "-xerror", "-i", path, "-f", "rawvideo", "-pix_fmt", "yuv420p", "pipe:1")
	var log bytes.Buffer
	cmd.Stderr = &log
	got, err := cmd.Output()
	if err != nil || log.Len() != 0 {
		t.Fatalf("ffmpeg %v: %s", err, log.String())
	}
	if !bytes.Equal(got, want) {
		for i := range got {
			if i >= len(want) || got[i] != want[i] {
				t.Fatalf("ffmpeg pixel %d got %d want %d (lengths %d/%d)", i, got[i], want[i], len(got), len(want))
			}
		}
		t.Fatalf("ffmpeg lengths %d/%d", len(got), len(want))
	}
}
