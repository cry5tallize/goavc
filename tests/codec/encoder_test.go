// Package codec_test 验证两条公共流水线的互操作；测试位于独立包以保持 R1。
package codec_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/decoder"
	"github.com/cry5tallize/goavc/encoder"
	testpattern "github.com/cry5tallize/goavc/internal/testutil/pattern"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func nonzeroPCM(f *h264.Frame) {
	for comp := range f.Planes {
		for i, v := range f.Planes[comp].Data {
			if v == 0 {
				f.Planes[comp].Data[i] = 1
			}
		}
	}
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
			f.Planes[0].Data[0] = byte(i + 1)
			nonzeroPCM(f)
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

func TestEncoder_MacroblockQPAndPCMDecisions(t *testing.T) {
	for _, baseQP := range []int{0, 26, 51} {
		c := h264.DefaultEncoderConfig()
		c.Width = 96
		c.Height = 64
		c.QP = baseQP
		e, err := encoder.New(c, encoder.WithSliceCount(5), encoder.WithMacroblockQP(func(x, y int) int { return []int{0, 51, 25, 26, 1, 50}[(x+y)%6] }))
		if err != nil {
			t.Fatal(err)
		}
		defer e.Close()
		d, err := decoder.New(h264.DecoderConfig{})
		if err != nil {
			t.Fatal(err)
		}
		defer d.Close()
		f := pattern(c.Width, c.Height)
		var state uint32 = 414
		for comp := range f.Planes {
			for i := range f.Planes[comp].Data {
				state = state*1664525 + 1013904223
				f.Planes[comp].Data[i] = byte(state >> 24)
			}
		}
		if err = e.SendFrame(f); err != nil {
			t.Fatal(err)
		}
		p, _ := e.ReceivePacket()
		r, _ := e.Reconstruction()
		if err = d.SendPacket(p); err != nil {
			t.Fatal(err)
		}
		got, err := d.ReceiveFrame()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(packed(got), packed(r)) {
			t.Fatal("QP/PCM local reconstruction differs")
		}
		ffmpegCheck(t, p.Data, packed(r))
		r.Release()
		got.Release()
	}
}

func TestEncoder_FFprobeMetadata(t *testing.T) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe absent; metadata Oracle skipped")
	}
	for _, pcm := range []bool{false, true} {
		c := h264.DefaultEncoderConfig()
		c.Width = 34
		c.Height = 18
		c.FPSNum = 30000
		c.FPSDen = 1001
		var options []encoder.Option
		if pcm {
			options = append(options, encoder.WithPCM())
		}
		e, err := encoder.New(c, options...)
		if err != nil {
			t.Fatal(err)
		}
		defer e.Close()
		var stream []byte
		for i := 0; i < 3; i++ {
			f := pattern(c.Width, c.Height)
			if pcm {
				nonzeroPCM(f)
			}
			if err = e.SendFrame(f); err != nil {
				t.Fatal(err)
			}
			p, _ := e.ReceivePacket()
			stream = append(stream, p.Data...)
		}
		path := filepath.Join(t.TempDir(), "probe.264")
		if err = os.WriteFile(path, stream, 0600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("ffprobe", "-v", "warning", "-show_streams", "-of", "json", path)
		var log bytes.Buffer
		cmd.Stderr = &log
		raw, err := cmd.Output()
		if err != nil || log.Len() != 0 {
			t.Fatalf("ffprobe %v %s", err, log.String())
		}
		var result struct {
			Streams []struct {
				Profile              string
				Width, Height, Level int
				PixFmt               string `json:"pix_fmt"`
			}
		}
		if err = json.Unmarshal(raw, &result); err != nil {
			t.Fatal(err)
		}
		if len(result.Streams) != 1 {
			t.Fatal(string(raw))
		}
		s := result.Streams[0]
		if s.Profile != "Constrained Baseline" || s.Width != 34 || s.Height != 18 || s.PixFmt != "yuv420p" || s.Level != 20 {
			t.Fatalf("metadata %+v", s)
		}
		// 裸流 r_frame_rate 受 FFmpeg 的旧 x264 build 启发式影响，不能充当
		// VUI 正确性的 Oracle。使用独立 trace_headers 检查实际写出的字段。
		trace, err := exec.Command("ffmpeg", "-v", "info", "-i", path, "-c:v", "copy", "-bsf:v", "trace_headers", "-f", "null", "-").CombinedOutput()
		if err != nil {
			t.Fatalf("trace_headers %v: %s", err, trace)
		}
		for _, field := range []string{"num_units_in_tick", "time_scale", "fixed_frame_rate_flag"} {
			want := map[string]string{"num_units_in_tick": "1001", "time_scale": "60000", "fixed_frame_rate_flag": "1"}[field]
			found := false
			for _, line := range strings.Split(string(trace), "\n") {
				if strings.Contains(line, " "+field+" ") && strings.HasSuffix(strings.TrimSpace(line), "= "+want) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("trace_headers missing %s = %s", field, want)
			}
		}
	}
}

func TestEncoder_DecisionCorpusOracle(t *testing.T) {
	for _, slices := range []int{1, 5} {
		c := h264.DefaultEncoderConfig()
		c.Width = 96
		c.Height = 64
		c.QP = 18
		e, err := encoder.New(c, encoder.WithSliceCount(slices))
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
		for i := 0; i < 60; i++ {
			if err = e.SendFrame(testpattern.Frame(c.Width, c.Height, i)); err != nil {
				t.Fatal(err)
			}
			p, _ := e.ReceivePacket()
			r, _ := e.Reconstruction()
			stream = append(stream, p.Data...)
			want = append(want, packed(r)...)
			if err = d.SendPacket(p); err != nil {
				t.Fatalf("frame %d: %v", i, err)
			}
			got, err := d.ReceiveFrame()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(packed(got), packed(r)) {
				t.Fatalf("slices=%d frame=%d local reconstruction differs", slices, i)
			}
			r.Release()
			got.Release()
		}
		ffmpegCheck(t, stream, want)
	}
}

func TestEncoder_IntraCompatibility(t *testing.T) {
	for _, size := range [][2]int{{16, 16}, {34, 18}, {96, 64}, {176, 144}, {320, 240}} {
		for _, qp := range []int{0, 26, 51} {
			for _, slices := range []int{1, 3} {
				if size[0] == 16 && slices > 1 {
					continue
				}
				t.Run(fmt.Sprintf("%dx%d/qp%d/slices%d", size[0], size[1], qp, slices), func(t *testing.T) {
					c := h264.DefaultEncoderConfig()
					c.Width = size[0]
					c.Height = size[1]
					c.QP = qp
					e, err := encoder.New(c, encoder.WithSliceCount(slices))
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
					for i := 0; i < 2; i++ {
						f := pattern(c.Width, c.Height)
						f.Planes[0].Data[0] = byte(i * 100)
						// QP0 的兼容性矩阵使用合法 PCM 候选域（1..255）；
						// 含0且需要超长前缀的低QP内容在独立边界测试中明确拒绝。
						if qp == 0 {
							nonzeroPCM(f)
						}
						if err = e.SendFrame(f); err != nil {
							t.Fatal(err)
						}
						r, _ := e.Reconstruction()
						p, _ := e.ReceivePacket()
						stream = append(stream, p.Data...)
						want = append(want, packed(r)...)
						if err = d.SendPacket(p); err != nil {
							t.Fatal(err)
						}
						got, err := d.ReceiveFrame()
						if err != nil {
							t.Fatal(err)
						}
						a, b := packed(got), packed(r)
						if !bytes.Equal(a, b) {
							for pos := range a {
								if a[pos] != b[pos] {
									t.Fatalf("local reconstruction pixel %d got %d want %d", pos, a[pos], b[pos])
								}
							}
						}
						r.Release()
						got.Release()
					}
					ffmpegCheck(t, stream, want)
				})
			}
		}
	}
}

func TestEncoder_LargeFramesOracle(t *testing.T) {
	for _, size := range [][3]int{{1280, 720, 30}, {1920, 1080, 15}} {
		for _, pcm := range []bool{false, true} {
			c := h264.DefaultEncoderConfig()
			c.Width = size[0]
			c.Height = size[1]
			c.FPSNum = size[2]
			options := []encoder.Option{encoder.WithSliceCount(64)}
			if pcm {
				options = append(options, encoder.WithPCM())
			}
			e, err := encoder.New(c, options...)
			if err != nil {
				t.Fatal(err)
			}
			if err = e.SendFrame(testpattern.Frame(c.Width, c.Height, 1)); err != nil {
				t.Fatal(err)
			}
			p, _ := e.ReceivePacket()
			r, _ := e.Reconstruction()
			d, err := decoder.New(h264.DecoderConfig{})
			if err != nil {
				t.Fatal(err)
			}
			if err = d.SendPacket(p); err != nil {
				t.Fatal(err)
			}
			got, err := d.ReceiveFrame()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(packed(got), packed(r)) {
				t.Fatal("large frame differs")
			}
			ffmpegCheck(t, p.Data, packed(r))
			got.Release()
			r.Release()
			d.Close()
			e.Close()
		}
	}
}
