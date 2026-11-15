package codec_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/decoder"
	"github.com/cry5tallize/goavc/encoder"
)

// TestEncoder_InterLongOracle 是显式开发期长门禁，不拖慢普通单元测试。
// H264_LONG_TEST=1 go test -run TestEncoder_InterLongOracle -v ./tests/codec。
func TestEncoder_InterLongOracle(t *testing.T) {
	if os.Getenv("H264_LONG_TEST") != "1" {
		t.Skip("set H264_LONG_TEST=1 for 300-frame 720p/1080p Oracle")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Fatal("long gate requires FFmpeg", err)
	}
	for _, size := range [][2]int{{1280, 720}, {1920, 1080}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			c := h264.DefaultEncoderConfig()
			c.Width = size[0]
			c.Height = size[1]
			c.GOPSize = 30
			e, err := encoder.New(c, encoder.WithSliceCount(4))
			if err != nil {
				t.Fatal(err)
			}
			defer e.Close()
			d, err := decoder.New(h264.DecoderConfig{})
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			gen := exec.Command("ffmpeg", "-v", "warning", "-f", "lavfi", "-i", fmt.Sprintf("testsrc2=size=%dx%d:rate=30", c.Width, c.Height), "-frames:v", "300", "-pix_fmt", "yuv420p", "-f", "rawvideo", "pipe:1")
			var genLog bytes.Buffer
			gen.Stderr = &genLog
			reader, err := gen.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err = gen.Start(); err != nil {
				t.Fatal(err)
			}
			finished := false
			defer func() {
				if !finished {
					gen.Process.Kill()
					gen.Wait()
				}
			}()
			inputHash, reconHash := sha256.New(), sha256.New()
			var stream bytes.Buffer
			var elapsed time.Duration
			f := pattern(c.Width, c.Height)
			for i := 0; i < 300; i++ {
				for comp := range f.Planes {
					if _, err = io.ReadFull(reader, f.Planes[comp].Data); err != nil {
						t.Fatal(i, comp, err)
					}
					inputHash.Write(f.Planes[comp].Data)
				}
				f.PTS = int64(i) * 1001
				f.Duration = 1001
				start := time.Now()
				if err = e.SendFrame(f); err != nil {
					t.Fatal(i, err)
				}
				p, err := e.ReceivePacket()
				if err != nil {
					t.Fatal(err)
				}
				elapsed += time.Since(start)
				stream.Write(p.Data)
				r, err := e.Reconstruction()
				if err != nil {
					t.Fatal(err)
				}
				pixels := packed(r)
				reconHash.Write(pixels)
				if err = d.SendPacket(p); err != nil {
					t.Fatal(i, err)
				}
				got, err := d.ReceiveFrame()
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(packed(got), pixels) || got.PTS != f.PTS {
					t.Fatalf("frame %d local/decoder mismatch", i)
				}
				r.Release()
				got.Release()
				if (i+1)%50 == 0 {
					t.Logf("frames=%d encode_seconds=%.3f bytes=%d", i+1, elapsed.Seconds(), stream.Len())
				}
			}
			if err = gen.Wait(); err != nil || genLog.Len() != 0 {
				t.Fatalf("generator %v %s", err, genLog.String())
			}
			finished = true
			if err = e.Flush(); err != nil {
				t.Fatal(err)
			}
			if _, err = e.ReceivePacket(); !errors.Is(err, h264.ErrEOF) {
				t.Fatal(err)
			}
			if err = d.Flush(); err != nil {
				t.Fatal(err)
			}
			if _, err = d.ReceiveFrame(); !errors.Is(err, h264.ErrEOF) {
				t.Fatal(err)
			}
			oracleHash := sha256.New()
			cmd := exec.Command("ffmpeg", "-v", "warning", "-xerror", "-probesize", "32M", "-i", "pipe:0", "-fps_mode", "passthrough", "-pix_fmt", "yuv420p", "-f", "rawvideo", "pipe:1")
			cmd.Stdin = bytes.NewReader(stream.Bytes())
			cmd.Stdout = oracleHash
			var log bytes.Buffer
			cmd.Stderr = &log
			if err = cmd.Run(); err != nil || log.Len() != 0 {
				t.Fatalf("Oracle %v: %s", err, log.String())
			}
			if !bytes.Equal(oracleHash.Sum(nil), reconHash.Sum(nil)) {
				t.Fatal("FFmpeg reconstruction SHA-256 differs")
			}
			t.Logf("input_sha256=%s output_sha256=%x recon_sha256=%s bytes=%d encode_fps=%.4f", hex.EncodeToString(inputHash.Sum(nil)), sha256.Sum256(stream.Bytes()), hex.EncodeToString(reconHash.Sum(nil)), stream.Len(), 300/elapsed.Seconds())
		})
	}
}
