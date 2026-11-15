package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cry5tallize/goavc/internal/testutil/manifest"
	"github.com/cry5tallize/goavc/syntax"
)

// 通过真实 CLI 输出与独立 ffprobe 对照；缺少外部工具必须显式跳过。
// 仅验证元数据和图片数，不把头部一致当作 Full Profile 的解码承诺。
func TestSyntax_FFprobeOracle(t *testing.T) {
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe 不可用，显式跳过 CLI 语法树 Oracle 对照")
	}
	root := filepath.Join("..", "..")
	m, err := manifest.Load(filepath.Join(root, "testdata", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	for _, entry := range m.Entries {
		if entry.Status == manifest.StatusUnsupported {
			continue
		}
		if entry.Oracle == manifest.OracleAnalytic {
			t.Run(entry.Name, func(t *testing.T) {
				t.Skip("analytic Oracle：规范解析解由 decoder 的 TestCorpus_ManifestOutputDigests 校验；不声称 ffprobe/FFmpeg 可解")
			})
			continue
		}
		if seen[entry.Profile] {
			continue
		}
		seen[entry.Profile] = true
		t.Run(entry.Name, func(t *testing.T) {
			path := filepath.Join(root, filepath.FromSlash(entry.Path))
			var output bytes.Buffer
			if err := run([]string{"-syntax", "-input", path}, &output, &output); err != nil {
				t.Fatal(err)
			}
			records := syntaxRecords(t, output.Bytes())
			var sps *syntax.SPS
			pictures := 0
			for _, r := range records {
				if r.SPS != nil && sps == nil {
					sps = r.SPS
				}
				if r.SliceHeader != nil && r.SliceHeader.FirstMBInSlice == 0 {
					pictures++
				}
			}
			if sps == nil || pictures == 0 {
				t.Fatal("语料未产生SPS及slice头部")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, ffprobe, "-v", "error", "-f", "h264", "-select_streams", "v:0", "-count_frames", "-show_entries", "stream=profile,level,width,height,pix_fmt,nb_read_frames", "-of", "json", path)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			data, err := cmd.Output()
			if err != nil {
				t.Fatalf("ffprobe执行失败: %v: %s", err, stderr.String())
			}
			var oracle struct {
				Streams []struct {
					Profile string `json:"profile"`
					Level   int    `json:"level"`
					Width   int    `json:"width"`
					Height  int    `json:"height"`
					PixFmt  string `json:"pix_fmt"`
					Frames  string `json:"nb_read_frames"`
				} `json:"streams"`
			}
			if err := json.Unmarshal(data, &oracle); err != nil || len(oracle.Streams) != 1 {
				t.Fatalf("ffprobe返回无效: %s: %v", data, err)
			}
			o := oracle.Streams[0]
			width, ew := sps.DisplayWidth()
			height, eh := sps.DisplayHeight()
			if ew != nil || eh != nil || int(width) != o.Width || int(height) != o.Height || int(sps.LevelIDC) != o.Level || o.PixFmt != entry.PixFmt {
				t.Fatalf("CLI与ffprobe元数据不一致: SPS=%+v ffprobe=%+v", sps, o)
			}
			profile := map[uint8]string{66: "baseline", 77: "main", 88: "extended", 100: "high", 110: "high 10", 122: "high 4:2:2", 244: "high 4:4:4 predictive"}[sps.ProfileIDC]
			if profile == "" || !strings.Contains(strings.ToLower(o.Profile), profile) {
				t.Fatalf("CLI profile=%d，ffprobe=%q", sps.ProfileIDC, o.Profile)
			}
			frames, err := strconv.Atoi(o.Frames)
			if err != nil || pictures != frames {
				t.Fatalf("CLI图片数=%d，ffprobe=%q: %v", pictures, o.Frames, err)
			}
			if entry.FrameCount > 0 && frames != entry.FrameCount {
				t.Fatalf("ffprobe实际解码图片数=%d，manifest frameCount=%d", frames, entry.FrameCount)
			}
		})
	}
	if len(seen) == 0 {
		t.Fatal("语料清单中没有可对照的受支持码流")
	}
}

// 验证 -input 路径模式与 stdin 模式产生相同语法树。
func TestSyntax_FileAndStdinMatch(t *testing.T) {
	data := syntaxAnnexB(richSyntaxUnits(t))
	path := filepath.Join(t.TempDir(), "syntax.264")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	var fileOut, stdinOut bytes.Buffer
	if err := run([]string{"-syntax", "-input", path}, &fileOut, &fileOut); err != nil {
		t.Fatal(err)
	}
	if err := runWithInput([]string{"-syntax", "-input", "-"}, bytes.NewReader(data), &stdinOut, &stdinOut); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(fileOut.Bytes(), stdinOut.Bytes()) {
		t.Fatal("文件与stdin的语法树不同")
	}
}
