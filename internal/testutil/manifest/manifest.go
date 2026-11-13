// Package manifest 定义测试语料清单（testdata/manifest.json）的结构与校验规则。
//
// 为什么语料需要清单：差分测试的可信度完全取决于"输入是哪一份、哈希是多少、
// 期望输出是什么"。仅靠文件名无法防止语料被替换或截断，而一次静默的语料
// 变更足以让一整轮 Oracle 对比得出错误结论。
//
// 清单同时承担"支持边界登记"的职责：每条语料必须声明 profile、熵编码模式、
// 状态（conformant / unsupported / wip）。CI 会据此断言 unsupported 条目
// 必须返回 ErrUnsupported，避免"看起来支持、实际不支持"。
package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

// Status 是语料的验证状态。
const (
	StatusConformant  = "conformant"  // 已通过逐像素 Oracle 对比
	StatusUnsupported = "unsupported" // 已知不支持，必须返回 ErrUnsupported
	StatusWIP         = "wip"         // 已入库但尚未验证
)

// Entry 描述一条测试语料。
type Entry struct {
	// Name 是语料唯一标识，必须与文件名（去扩展名）一致，便于脚本反查。
	Name string `json:"name"`

	// Path 是相对模块根的路径；远程语料可留空并填写 URL。
	Path string `json:"path"`
	// URL 仅用于"体积过大不入库"的语料，CI 拉取后必须校验 SHA256。
	URL string `json:"url,omitempty"`

	// Source 记录语料来源：x264 / ffmpeg-fate / jm / synthetic / corrupt。
	Source string `json:"source"`

	Profile  string `json:"profile"`
	Level    string `json:"level"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	PixFmt   string `json:"pixFmt"`
	BitDepth int    `json:"bitDepth"`
	Entropy  string `json:"entropy"`

	SliceTypes []string `json:"sliceTypes"`
	SliceCount int      `json:"sliceCount"`
	RefFrames  int      `json:"refFrames"`
	Tags       []string `json:"tags,omitempty"`

	// SHA256 是输入码流的哈希（小写十六进制）。
	SHA256 string `json:"sha256"`

	// ExpectedYUV 是参考解码输出的路径，ExpectedYUVSHA256 是其哈希。
	ExpectedYUV       string `json:"expectedYUV,omitempty"`
	ExpectedYUVSHA256 string `json:"expectedYUVSha256,omitempty"`

	Status string `json:"status"`
	Notes  string `json:"notes,omitempty"`
}

// File 是 manifest.json 的顶层结构。
type File struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
}

// CurrentVersion 是清单格式版本。结构不兼容变更时必须递增。
const CurrentVersion = 1

var (
	namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	shaPattern  = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// 允许的取值集合。这里的字符串与 FFmpeg 的参数保持一致，
// 让差分脚本可以对同一份清单直接调用 ffmpeg。
var (
	validProfiles = map[string]bool{
		"baseline": true, "main": true, "high": true,
		"high10": true, "high422": true, "high444": true,
	}
	validPixFmts = map[string]bool{
		"yuv420p": true, "yuv422p": true, "yuv444p": true,
		"yuv420p10le": true, "yuv422p10le": true, "yuv444p10le": true,
	}
	validEntropy = map[string]bool{"cavlc": true, "cabac": true}
	validStatus  = map[string]bool{StatusConformant: true, StatusUnsupported: true, StatusWIP: true}
)

// Load 读取并解析清单文件。只做 JSON 结构解析，不做语义校验。
func Load(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse manifest %s: %w", path, err)
	}
	return &f, nil
}

// Validate 执行全部语义校验，返回所有问题而不是遇到第一个就返回。
//
// 一次性列出全部问题，是为了让"补语料"这件事可以在一次 PR 里做完，
// 而不是修一个错再跑一遍。
//
// root 是模块根目录，Path / ExpectedYUV 都相对于它解析。
func (f *File) Validate(root string) []error {
	var errs []error
	report := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}

	if f.Version != CurrentVersion {
		report("manifest: unsupported version %d (want %d)", f.Version, CurrentVersion)
	}

	seen := make(map[string]bool, len(f.Entries))
	for i := range f.Entries {
		e := &f.Entries[i]
		label := fmt.Sprintf("entry %d (%s)", i, e.Name)

		if e.Name == "" {
			report("%s: name is required", label)
		} else {
			if !namePattern.MatchString(e.Name) {
				report("%s: name must match %s", label, namePattern)
			}
			if seen[e.Name] {
				report("%s: duplicate name", label)
			}
			seen[e.Name] = true
		}

		if !validProfiles[e.Profile] {
			report("%s: unknown profile %q", label, e.Profile)
		}
		if !validPixFmts[e.PixFmt] {
			report("%s: unknown pixel format %q", label, e.PixFmt)
		}
		if e.BitDepth != 8 && e.BitDepth != 10 {
			report("%s: bit depth %d must be 8 or 10", label, e.BitDepth)
		}
		if e.Entropy != "" && !validEntropy[e.Entropy] {
			report("%s: unknown entropy mode %q", label, e.Entropy)
		}
		if e.Width <= 0 || e.Height <= 0 {
			report("%s: invalid dimensions %dx%d", label, e.Width, e.Height)
		}
		if !validStatus[e.Status] {
			report("%s: unknown status %q", label, e.Status)
		}
		if e.Source == "" {
			report("%s: source is required", label)
		}

		if e.SHA256 != "" && !shaPattern.MatchString(e.SHA256) {
			report("%s: sha256 must be 64 lowercase hex characters", label)
		}
		if e.ExpectedYUVSHA256 != "" && !shaPattern.MatchString(e.ExpectedYUVSHA256) {
			report("%s: expectedYUVSha256 must be 64 lowercase hex characters", label)
		}

		// 路径与哈希：本地语料必须存在且哈希一致；远程语料允许缺文件，
		// 但仍必须声明哈希，否则无法保证拉取到的是同一份数据。
		if e.Path == "" {
			if e.URL == "" {
				report("%s: path or url is required", label)
			}
			if e.SHA256 == "" {
				report("%s: remote entries must declare sha256", label)
			}
		} else {
			full := filepath.Join(root, filepath.FromSlash(e.Path))
			if _, err := os.Stat(full); err != nil {
				report("%s: input file %s: %v", label, e.Path, err)
			} else if e.SHA256 != "" {
				got, err := HashFile(full)
				if err != nil {
					report("%s: hashing %s: %v", label, e.Path, err)
				} else if got != e.SHA256 {
					report("%s: sha256 mismatch for %s: got %s want %s", label, e.Path, got, e.SHA256)
				}
			}
		}

		if e.Status == StatusConformant && e.SHA256 == "" {
			report("%s: conformant entries must declare sha256", label)
		}
		if e.Status == StatusUnsupported && e.Notes == "" {
			report("%s: unsupported entries must explain the gap in notes", label)
		}

		if e.ExpectedYUV != "" {
			full := filepath.Join(root, filepath.FromSlash(e.ExpectedYUV))
			if _, err := os.Stat(full); err != nil {
				report("%s: expected yuv %s: %v", label, e.ExpectedYUV, err)
			} else if e.ExpectedYUVSHA256 != "" {
				got, err := HashFile(full)
				if err != nil {
					report("%s: hashing %s: %v", label, e.ExpectedYUV, err)
				} else if got != e.ExpectedYUVSHA256 {
					report("%s: expected yuv sha256 mismatch", label)
				}
			}
		}
	}
	return errs
}

// HashFile 计算文件的 SHA-256（小写十六进制）。
//
// 用流式读取而不是 os.ReadFile：语料可能是几百 MB 的 YUV 参考输出，
// 一次性读入内存没有意义。
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
