package decoder

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cry5tallize/goavc/internal/testutil/manifest"
)

// 高级外部语料直接比较原生位深与色度采样，绝不先转成 8-bit 4:2:0。
// analytic 条目只具有规范解析解证据，由 TestCorpus_ManifestOutputDigests
// 在无外部工具时也执行解码哈希校验；这里显式跳过，不声称 FFmpeg 可解。
func TestCorpus_AdvancedFFmpegPixelExact(t *testing.T) {
	m, err := manifest.Load("../testdata/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if problems := m.Validate(".."); len(problems) != 0 {
		t.Fatal(problems)
	}
	entries := 0
	for _, e := range m.Entries {
		if e.Status != manifest.StatusConformant || !strings.HasPrefix(filepath.ToSlash(e.Path), "testdata/advanced/") {
			continue
		}
		entries++
		t.Run(e.Name, func(t *testing.T) {
			if e.Oracle == manifest.OracleAnalytic {
				t.Skip("analytic Oracle：像素证据来自规范解析解，由 TestCorpus_ManifestOutputDigests 校验原生解码哈希；不声称 FFmpeg 可解")
			}
			ffmpeg := ffmpegPath(t)
			path := filepath.Join("..", filepath.FromSlash(e.Path))
			got := decodeCorpusFile(t, path, &e)
			// 裸 HRD 码流可能被猜出不同帧率；passthrough 禁止补帧或丢帧。
			cmd := exec.Command(ffmpeg, "-v", "error", "-i", path, "-fps_mode", "passthrough", "-f", "rawvideo", "-pix_fmt", e.PixFmt, "-")
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			want, err := cmd.Output()
			if err != nil {
				t.Fatalf("ffmpeg native %s Oracle: %v\n%s", e.PixFmt, err, stderr.String())
			}
			if len(got) != len(want) {
				t.Fatalf("native %s decoded %d bytes, ffmpeg %d bytes", e.PixFmt, len(got), len(want))
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("native %s first mismatch at byte %d: got %d want %d", e.PixFmt, i, got[i], want[i])
				}
			}
		})
	}
	if entries == 0 {
		t.Fatal("manifest 中没有 conformant 高级语料，无法执行高级 Oracle 门禁")
	}
}
