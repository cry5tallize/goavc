package syntax_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/internal/testutil/manifest"
	"github.com/goavc/h264/nal"
	"github.com/goavc/h264/syntax"
)

// ffprobeStream 是 ffprobe -show_entries stream 的 JSON 子集。
type ffprobeStream struct {
	Profile string `json:"profile"`
	Level   int    `json:"level"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	PixFmt  string `json:"pix_fmt"`
}

type ffprobeOutput struct {
	Streams []ffprobeStream `json:"streams"`
}

// TestFFprobe_SPSMatchesStreamMetadata 用 ffprobe 作为独立 Oracle，核对
// SPS/PPS 解析出的 profile、level、分辨率、像素格式与熵编码模式。
//
// ffprobe 不存在时整条测试跳过——CI 无 FFmpeg 环境不得伪装成通过。
func TestFFprobe_SPSMatchesStreamMetadata(t *testing.T) {
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe 不可用，跳过 NAL 级 Oracle 对比")
	}

	root := filepath.Join("..")
	manifestPath := filepath.Join(root, "testdata", "manifest.json")
	m, err := manifest.Load(manifestPath)
	if err != nil {
		t.Fatalf("加载语料清单失败: %v", err)
	}
	if len(m.Entries) == 0 {
		t.Skip("语料清单为空")
	}

	for _, entry := range m.Entries {
		t.Run(entry.Name, func(t *testing.T) {
			path := filepath.Join(root, filepath.FromSlash(entry.Path))
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("读取语料失败: %v", err)
			}
			sps, pps := parseCorpusParameterSets(t, data)
			if sps == nil || pps == nil {
				t.Fatal("语料中缺少 SPS/PPS")
			}

			width, err := sps.DisplayWidth()
			if err != nil {
				t.Fatalf("DisplayWidth 失败: %v", err)
			}
			height, err := sps.DisplayHeight()
			if err != nil {
				t.Fatalf("DisplayHeight 失败: %v", err)
			}
			if int(width) != entry.Width || int(height) != entry.Height {
				t.Fatalf("SPS 尺寸 %dx%d 与 manifest %dx%d 不符", width, height, entry.Width, entry.Height)
			}
			if profileName(sps.ProfileIDC) != entry.Profile {
				t.Fatalf("SPS profile %q 与 manifest %q 不符", profileName(sps.ProfileIDC), entry.Profile)
			}
			entropy := "cavlc"
			if pps.EntropyCodingModeFlag {
				entropy = "cabac"
			}
			if entropy != entry.Entropy {
				t.Fatalf("PPS entropy %q 与 manifest %q 不符", entropy, entry.Entropy)
			}

			stream := probeStream(t, ffprobe, path)
			if stream.Width != int(width) || stream.Height != int(height) {
				t.Fatalf("ffprobe 尺寸 %dx%d 与 SPS %dx%d 不符",
					stream.Width, stream.Height, width, height)
			}
			if got := ffprobeProfile(stream.Profile); got != entry.Profile {
				t.Fatalf("ffprobe profile %q (%s) 与 manifest %q 不符",
					stream.Profile, got, entry.Profile)
			}
			if stream.Level != int(sps.LevelIDC) {
				t.Fatalf("ffprobe level %d 与 SPS %d 不符", stream.Level, sps.LevelIDC)
			}
			if stream.PixFmt != entry.PixFmt {
				t.Fatalf("ffprobe pix_fmt %q 与 manifest %q 不符", stream.PixFmt, entry.PixFmt)
			}
		})
	}
}

// parseCorpusParameterSets 从 Annex-B 语料中解析 SPS/PPS。
func parseCorpusParameterSets(t *testing.T, data []byte) (*syntax.SPS, *syntax.PPS) {
	t.Helper()
	units, err := nal.SplitAnnexB(data)
	if err != nil {
		t.Fatalf("切分 Annex-B 失败: %v", err)
	}
	var sps *syntax.SPS
	for _, u := range units {
		if u.Header.Type != nal.TypeSPS {
			continue
		}
		rbsp, err := bitstream.EBSPToRBSP(u.Payload)
		if err != nil {
			t.Fatalf("SPS EBSP→RBSP 失败: %v", err)
		}
		sps, err = syntax.ParseSPS(rbsp)
		if err != nil {
			t.Fatalf("解析 SPS 失败: %v", err)
		}
		break
	}
	if sps == nil {
		return nil, nil
	}
	var pps *syntax.PPS
	for _, u := range units {
		if u.Header.Type != nal.TypePPS {
			continue
		}
		rbsp, err := bitstream.EBSPToRBSP(u.Payload)
		if err != nil {
			t.Fatalf("PPS EBSP→RBSP 失败: %v", err)
		}
		pps, err = syntax.ParsePPS(rbsp, sps)
		if err != nil {
			t.Fatalf("解析 PPS 失败: %v", err)
		}
		break
	}
	return sps, pps
}

func probeStream(t *testing.T, ffprobe, path string) ffprobeStream {
	t.Helper()
	cmd := exec.Command(ffprobe,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=profile,level,width,height,pix_fmt",
		"-of", "json",
		path,
	)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("ffprobe 执行失败: %v", err)
	}
	var parsed ffprobeOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("解析 ffprobe JSON 失败: %v\n%s", err, out)
	}
	if len(parsed.Streams) == 0 {
		t.Fatalf("ffprobe 没有返回视频流: %s", out)
	}
	return parsed.Streams[0]
}

// ffprobeProfile 把 ffprobe 的展示名映射回 manifest 的 profile 取值。
func ffprobeProfile(name string) string {
	switch name {
	case "Constrained Baseline", "Baseline":
		return "baseline"
	case "Main":
		return "main"
	case "High":
		return "high"
	case "High 10":
		return "high10"
	case "High 4:2:2":
		return "high422"
	case "High 4:4:4 Predictive":
		return "high444"
	default:
		return name
	}
}

func profileName(profileIDC uint8) string {
	switch profileIDC {
	case 66:
		return "baseline"
	case 77:
		return "main"
	case 100:
		return "high"
	case 110:
		return "high10"
	case 122:
		return "high422"
	case 244:
		return "high444"
	default:
		return fmt.Sprintf("profile_%d", profileIDC)
	}
}
