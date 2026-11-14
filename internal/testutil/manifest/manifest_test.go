package manifest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// baseEntry 返回一条字段完整、可通过校验的本地语料条目。
func baseEntry(path, sha string) Entry {
	return Entry{
		Name:       "baseline_352x288_cavlc_ip_001",
		Path:       path,
		Source:     "x264",
		Profile:    "baseline",
		Level:      "3.0",
		Width:      352,
		Height:     288,
		PixFmt:     "yuv420p",
		BitDepth:   8,
		Entropy:    "cavlc",
		SliceTypes: []string{"I", "P"},
		SliceCount: 1,
		RefFrames:  1,
		SHA256:     sha,
		Status:     StatusConformant,
	}
}

// setupCorpus 在临时根目录写入一份语料并返回其哈希。
func setupCorpus(t *testing.T, relPath string, content []byte) (root, sha string) {
	t.Helper()
	root = t.TempDir()
	full := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if err := os.WriteFile(full, content, 0o644); err != nil {
		t.Fatalf("写入语料失败: %v", err)
	}
	sha, err := HashFile(full)
	if err != nil {
		t.Fatalf("计算哈希失败: %v", err)
	}
	return root, sha
}

func TestValidateAcceptsCompleteEntry(t *testing.T) {
	root, sha := setupCorpus(t, "testdata/baseline/clip.264", []byte{0, 0, 0, 1, 0x67})
	f := &File{Version: CurrentVersion, Entries: []Entry{baseEntry("testdata/baseline/clip.264", sha)}}

	if errs := f.Validate(root); len(errs) != 0 {
		t.Fatalf("期望无错误，得到 %v", errs)
	}
}

func TestValidate_OracleAndFrameCount(t *testing.T) {
	root, sha := setupCorpus(t, "a.264", []byte{1})
	for _, oracle := range []string{"", "ffmpeg", "analytic"} {
		t.Run("oracle_"+oracle, func(t *testing.T) {
			for _, frames := range []int{0, 3} {
				e := baseEntry("a.264", sha)
				e.Oracle, e.FrameCount = oracle, frames
				if errs := f2errs(root, e); len(errs) != 0 {
					t.Fatalf("合法 Oracle/frameCount 被拒绝: %v", errs)
				}
			}
		})
	}
	e := baseEntry("a.264", sha)
	e.Oracle, e.FrameCount = "self-roundtrip", -1
	errs := f2errs(root, e)
	for _, want := range []string{"oracle", "frame count"} {
		if !containsAll(errs, want) {
			t.Fatalf("错误集缺少 %q: %v", want, errs)
		}
	}
}

func TestValidate_ExtendedRequires8Bit420CAVLC(t *testing.T) {
	root, sha := setupCorpus(t, "a.264", []byte{1})
	e := baseEntry("a.264", sha)
	e.Profile, e.Oracle = "extended", "analytic"
	if errs := f2errs(root, e); len(errs) != 0 {
		t.Fatalf("合法 Extended 条目被拒绝: %v", errs)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*Entry)
	}{
		{"10bit", func(e *Entry) { e.BitDepth, e.PixFmt = 10, "yuv420p10le" }},
		{"422", func(e *Entry) { e.PixFmt = "yuv422p" }},
		{"444", func(e *Entry) { e.PixFmt = "yuv444p" }},
		{"cabac", func(e *Entry) { e.Entropy = "cabac" }},
		{"unspecified_entropy", func(e *Entry) { e.Entropy = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := e
			tc.mutate(&bad)
			if errs := f2errs(root, bad); !containsAll(errs, "extended") {
				t.Fatalf("不合法 Extended 条目未被明确拒绝: %v", errs)
			}
		})
	}
}

func TestEntry_OptionalOracleMetadataJSON(t *testing.T) {
	if CurrentVersion != 1 {
		t.Fatalf("可选字段不得改变旧 schema 版本: %d", CurrentVersion)
	}
	data, err := json.Marshal(Entry{})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"oracle"`, `"frameCount"`} {
		if strings.Contains(string(data), key) {
			t.Fatalf("未设置的可选字段必须省略: %s", data)
		}
	}
	var e Entry
	if err := json.Unmarshal([]byte(`{"oracle":"analytic","frameCount":3}`), &e); err != nil {
		t.Fatal(err)
	}
	if e.Oracle != "analytic" || e.FrameCount != 3 {
		t.Fatalf("可选字段未保留: %+v", e)
	}
	data, err = json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if string(fields["oracle"]) != `"analytic"` || string(fields["frameCount"]) != "3" {
		t.Fatalf("可选字段 JSON 名称或值不符: %s", data)
	}
}

// 哈希不一致是"语料被替换"的唯一可靠信号，必须报错。
func TestValidateRejectsHashMismatch(t *testing.T) {
	root, sha := setupCorpus(t, "testdata/baseline/clip.264", []byte{1, 2, 3})
	bad := strings.Repeat("0", 64)
	if sha == bad {
		t.Fatal("测试数据构造失败")
	}
	f := &File{Version: CurrentVersion, Entries: []Entry{baseEntry("testdata/baseline/clip.264", bad)}}

	errs := f.Validate(root)
	if len(errs) == 0 {
		t.Fatal("哈希不一致必须报错")
	}
	if !strings.Contains(errs[0].Error(), "mismatch") {
		t.Fatalf("错误信息未指出 mismatch: %v", errs[0])
	}
}

func TestValidateRejectsMissingFile(t *testing.T) {
	root := t.TempDir()
	f := &File{Version: CurrentVersion, Entries: []Entry{baseEntry("testdata/missing.264", "")}}
	if errs := f.Validate(root); len(errs) == 0 {
		t.Fatal("缺失语料必须报错")
	}
}

// 远程语料允许不落地，但必须带哈希，否则无法复现。
func TestValidateRemoteEntryRequiresHash(t *testing.T) {
	f := &File{Version: CurrentVersion, Entries: []Entry{{
		Name:     "remote_001",
		URL:      "https://example.com/clip.264",
		Source:   "ffmpeg-fate",
		Profile:  "high",
		Level:    "4.0",
		Width:    1920,
		Height:   1080,
		PixFmt:   "yuv420p",
		BitDepth: 8,
		Entropy:  "cabac",
		Status:   StatusWIP,
	}}}
	if errs := f.Validate(t.TempDir()); len(errs) == 0 {
		t.Fatal("远程语料缺少哈希必须报错")
	}
}

func TestValidateRejectsDuplicateNames(t *testing.T) {
	root, sha := setupCorpus(t, "a.264", []byte{1})
	e := baseEntry("a.264", sha)
	f := &File{Version: CurrentVersion, Entries: []Entry{e, e}}
	if errs := f.Validate(root); len(errs) == 0 {
		t.Fatal("重复 name 必须报错")
	}
}

func TestValidateRejectsUnknownEnums(t *testing.T) {
	root, sha := setupCorpus(t, "a.264", []byte{1})
	e := baseEntry("a.264", sha)
	e.Profile = "ultra"
	e.PixFmt = "rgb24"
	e.Entropy = "rice"
	e.Status = "done"
	e.BitDepth = 9

	errs := f2errs(root, e)
	for _, want := range []string{"profile", "pixel format", "entropy", "status", "bit depth"} {
		if !containsAll(errs, want) {
			t.Fatalf("错误集缺少 %q: %v", want, errs)
		}
	}
}

// unsupported 条目必须写明原因，否则半年后没人知道为什么要特殊处理它。
func TestValidateUnsupportedRequiresNotes(t *testing.T) {
	root, sha := setupCorpus(t, "a.264", []byte{1})
	e := baseEntry("a.264", sha)
	e.Status = StatusUnsupported
	if errs := f2errs(root, e); len(errs) == 0 {
		t.Fatal("unsupported 条目缺少 notes 必须报错")
	}
	e.Notes = "interlaced coding is scheduled for M7"
	if errs := f2errs(root, e); len(errs) != 0 {
		t.Fatalf("补充 notes 后应通过，得到 %v", errs)
	}
}

func TestValidateRejectsWrongVersion(t *testing.T) {
	f := &File{Version: 99}
	if errs := f.Validate(t.TempDir()); len(errs) == 0 {
		t.Fatal("版本不符必须报错")
	}
}

// Load 必须能读出真实 JSON 文件，并对语法错误报错。
func TestLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")
	data, err := json.Marshal(File{Version: CurrentVersion})
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	f, err := Load(path)
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if f.Version != CurrentVersion {
		t.Fatalf("Version = %d", f.Version)
	}

	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{"), 0o644); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if _, err := Load(bad); err == nil {
		t.Fatal("非法 JSON 应当报错")
	}
}

func f2errs(root string, entries ...Entry) []error {
	f := &File{Version: CurrentVersion, Entries: entries}
	return f.Validate(root)
}

func containsAll(errs []error, substr string) bool {
	for _, err := range errs {
		if strings.Contains(err.Error(), substr) {
			return true
		}
	}
	return false
}
