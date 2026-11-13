package decoder

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/internal/testutil/manifest"
)

// ffmpegPath 允许通过环境变量覆盖 FFmpeg 位置；找不到时测试 skip。
func ffmpegPath(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("FFMPEG"); p != "" {
		return p
	}
	p, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed; skipping pixel oracle")
	}
	return p
}

func decodeFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	d, err := New(h264.DecoderConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.SendPacket(h264.Packet{Data: data}); err != nil {
		t.Fatalf("SendPacket: %v", err)
	}
	if err := d.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	var out []byte
	for {
		f, err := d.ReceiveFrame()
		if errors.Is(err, h264.ErrEOF) {
			break
		}
		if err != nil {
			t.Fatalf("ReceiveFrame: %v", err)
		}
		for pi := 0; pi < 3; pi++ {
			pl := f.Planes[pi]
			_, rows := f.Format.PlaneDimensions(pi, f.Width, f.Height)
			rowBytes := f.Format.RowBytes(pi, f.Width)
			for y := 0; y < rows; y++ {
				out = append(out, pl.Data[y*pl.Stride:y*pl.Stride+rowBytes]...)
			}
		}
		f.Release()
	}
	return out
}

func ffmpegRaw(t *testing.T, ffmpeg, path string) []byte {
	t.Helper()
	args := []string{"-v", "error", "-i", path, "-f", "rawvideo", "-pix_fmt", "yuv420p", "-"}
	cmd := exec.Command(ffmpeg, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("ffmpeg: %v\n%s", err, stderr.String())
	}
	return out
}

func TestCorpus_BaselinePixelExact(t *testing.T) {
	ffmpeg := ffmpegPath(t)
	m, err := manifest.Load("../testdata/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range m.Entries {
		if e.Status != manifest.StatusConformant {
			continue
		}
		path := filepath.Join("..", filepath.FromSlash(e.Path))
		t.Run(e.Name, func(t *testing.T) {
			got := decodeFile(t, path)
			want := ffmpegRaw(t, ffmpeg, path)
			if len(got) != len(want) {
				t.Fatalf("decoded %d bytes, ffmpeg %d bytes", len(got), len(want))
			}
			for i := range want {
				if got[i] != want[i] {
					w, h := e.Width, e.Height
					frameSize := w * h * 3 / 2
					frame, offset := i/frameSize, i%frameSize
					plane, x, y := "Y", 0, 0
					switch {
					case offset < w*h:
						x, y = offset%w, offset/w
					case offset < w*h+w*h/4:
						j := offset - w*h
						plane, x, y = "U", j%(w/2), j/(w/2)
					default:
						j := offset - w*h - w*h/4
						plane, x, y = "V", j%(w/2), j/(w/2)
					}
					t.Fatalf("first mismatch at byte %d (frame %d %s %d,%d): got %d want %d",
						i, frame, plane, x, y, got[i], want[i])
				}
			}
		})
	}
}

// 固化独立 Oracle 的原始 YUV 哈希，使无 FFmpeg 的 CI 也能检查真实解码
// 像素与显示顺序；实时 Oracle 测试继续逐字节比较并给出首个差异坐标。
func TestCorpus_ManifestOutputDigests(t *testing.T) {
	m, err := manifest.Load("../testdata/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if problems := m.Validate(".."); len(problems) > 0 {
		t.Fatal(problems)
	}
	registered := map[string]bool{}
	for _, e := range m.Entries {
		registered[filepath.Clean(filepath.Join("..", e.Path))] = true
		t.Run(e.Name, func(t *testing.T) {
			path := filepath.Join("..", e.Path)
			switch e.Status {
			case manifest.StatusUnsupported:
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				d := mustDecoder(t, h264.DecoderConfig{})
				if err := d.SendPacket(h264.Packet{Data: data}); !errors.Is(err, h264.ErrUnsupported) {
					t.Fatal(err)
				}
			case manifest.StatusConformant:
				if e.ExpectedYUVSHA256 == "" {
					t.Fatal("missing independent Oracle digest")
				}
				got := decodeFile(t, path)
				sum := sha256.Sum256(got)
				if hex.EncodeToString(sum[:]) != e.ExpectedYUVSHA256 {
					t.Fatalf("decoded YUV hash %x, want %s", sum, e.ExpectedYUVSHA256)
				}
			}
		})
	}
	files, err := filepath.Glob("../testdata/baseline/*.264")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range files {
		if !registered[filepath.Clean(p)] {
			t.Fatalf("unregistered corpus %s", p)
		}
	}
}
