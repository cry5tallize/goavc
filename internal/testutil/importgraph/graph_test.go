package importgraph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile 在 root 下写入一个文件，自动创建父目录。
// 用逐文件调用的方式而不是 map 字面量，是为了让测试意图在 diff 里一眼可见。
func writeFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写入 %s 失败: %v", name, err)
	}
}

// newModule 建立一个带 go.mod 的临时模块。
func newModule(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/m\n\ngo 1.24\n")
	return root
}

func rulesOf(violations []Violation) map[string]int {
	out := map[string]int{}
	for _, v := range violations {
		out[v.Rule]++
	}
	return out
}

func TestLoadReadsModulePathAndImports(t *testing.T) {
	root := newModule(t)
	writeFile(t, root, "main.go", "package m\n\nimport _ \"fmt\"\n")
	writeFile(t, root, "decoder/d.go", "package decoder\n\nimport _ \"example.com/m/encoder\"\n")
	writeFile(t, root, "decoder/d_test.go", "package decoder\n\nimport _ \"testing\"\n")
	writeFile(t, root, "encoder/e.go", "package encoder\n")

	g, err := Load(root)
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if g.ModulePath != "example.com/m" {
		t.Fatalf("ModulePath = %q", g.ModulePath)
	}
	if len(g.Packages) != 3 {
		t.Fatalf("包数量 = %d，期望 3", len(g.Packages))
	}
	if got := g.Rel("example.com/m/decoder"); got != "decoder" {
		t.Fatalf("Rel 结果错误: %q", got)
	}
	if got := g.Rel(g.ModulePath); got != "." {
		t.Fatalf("模块根的 Rel = %q，期望 \".\"", got)
	}

	pkg := g.Packages["example.com/m/decoder"]
	if pkg == nil {
		t.Fatal("缺少 decoder 包")
	}
	found := false
	for _, imp := range pkg.Imports {
		if imp == "example.com/m/encoder" {
			found = true
		}
	}
	if !found {
		t.Fatal("未提取到 encoder import")
	}
}

// testdata / vendor / 点目录 / 下划线目录都不参与构建，解析它们只会误报。
func TestLoadSkipsNonBuildDirs(t *testing.T) {
	root := newModule(t)
	writeFile(t, root, "a/a.go", "package a\n")
	writeFile(t, root, "testdata/sample.go", "package sample\n\nimport _ \"example.com/m/decoder\"\n")
	writeFile(t, root, "vendor/x/x.go", "package x\n")
	writeFile(t, root, ".hidden/h.go", "package h\n")
	writeFile(t, root, "_build/b.go", "package b\n")

	g, err := Load(root)
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	for path := range g.Packages {
		for _, bad := range []string{"testdata", "vendor", "hidden", "_build"} {
			if strings.Contains(path, bad) {
				t.Fatalf("不应加载 %q", path)
			}
		}
	}
	if len(g.Packages) != 1 {
		t.Fatalf("包数量 = %d，期望 1", len(g.Packages))
	}
}

func TestLoadWithoutGoModFails(t *testing.T) {
	if _, err := Load(t.TempDir()); err == nil {
		t.Fatal("缺少 go.mod 时应当报错")
	}
}

// R1：编码器与解码器不得相互依赖。这是整个架构最重要的一条，
// 一旦破例，共享内核的边界就会在后续迭代中被慢慢侵蚀。
func TestCheckR1(t *testing.T) {
	root := newModule(t)
	writeFile(t, root, "decoder/d.go", "package decoder\n\nimport _ \"example.com/m/encoder\"\n")
	writeFile(t, root, "encoder/e.go", "package encoder\n\nimport _ \"example.com/m/decoder\"\n")

	g, err := Load(root)
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	counts := rulesOf(g.Check())
	if counts["R1"] != 2 {
		t.Fatalf("R1 违规数 = %d，期望 2", counts["R1"])
	}
}

// R2：internal 下的共享内核不得反向依赖上层流水线。
func TestCheckR2(t *testing.T) {
	root := newModule(t)
	writeFile(t, root, "internal/picture/p.go",
		"package picture\n\nimport (\n\t_ \"example.com/m/decoder\"\n\t_ \"example.com/m/stream\"\n)\n")
	writeFile(t, root, "decoder/d.go", "package decoder\n")
	writeFile(t, root, "stream/s.go", "package stream\n")

	g, err := Load(root)
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	counts := rulesOf(g.Check())
	if counts["R2"] != 2 {
		t.Fatalf("R2 违规数 = %d，期望 2", counts["R2"])
	}
}

// R3：SIMD 只能被白名单内的底层包使用，syntax 这类语法层不允许直接调汇编。
func TestCheckR3(t *testing.T) {
	root := newModule(t)
	writeFile(t, root, "syntax/s.go", "package syntax\n\nimport _ \"example.com/m/internal/simd\"\n")
	writeFile(t, root, "internal/simd/s.go", "package simd\n")
	writeFile(t, root, "internal/transform/t.go", "package transform\n\nimport _ \"example.com/m/internal/simd\"\n")

	g, err := Load(root)
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	violations := g.Check()
	if got := rulesOf(violations)["R3"]; got != 1 {
		t.Fatalf("R3 违规数 = %d，期望 1: %v", got, violations)
	}
	for _, v := range violations {
		if v.Rule == "R3" && v.From != "syntax" {
			t.Fatalf("R3 定位错误: %v", v)
		}
	}
}

func TestCheckR3_ExactSearchConsumer(t *testing.T) {
	for _, dir := range []string{"internal/motion/search", "internal/motion/search/helper", "internal/picture", "internal/picture/helper"} {
		t.Run(dir, func(t *testing.T) {
			root := newModule(t)
			writeFile(t, root, "internal/simd/s.go", "package simd\n")
			writeFile(t, root, dir+"/s.go", "package consumer\nimport _ \"example.com/m/internal/simd\"\n")
			g, err := Load(root)
			if err != nil {
				t.Fatal(err)
			}
			want := 1
			if dir == "internal/motion/search" || dir == "internal/picture" {
				want = 0
			}
			if got := rulesOf(g.Check())["R3"]; got != want {
				t.Fatalf("目录%s R3违规=%d，期望%d", dir, got, want)
			}
		})
	}
}

// R5：import 环必须被检测出来，且 Reason 里带完整环路径。
func TestCheckR5Cycle(t *testing.T) {
	root := newModule(t)
	writeFile(t, root, "a/a.go", "package a\n\nimport _ \"example.com/m/b\"\n")
	writeFile(t, root, "b/b.go", "package b\n\nimport _ \"example.com/m/a\"\n")

	g, err := Load(root)
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	violations := g.Check()
	if rulesOf(violations)["R5"] == 0 {
		t.Fatal("未检测到 import 环")
	}
	hasPath := false
	for _, v := range violations {
		if v.Rule == "R5" && strings.Contains(v.Reason, "->") {
			hasPath = true
		}
	}
	if !hasPath {
		t.Fatal("环路径未写入违规原因")
	}
}

// 外部测试包（package foo_test）import 自身目录是合法的，
// 依赖图按目录聚合时不得把它当成 foo -> foo 的 import 环。
func TestCheckExternalTestSelfImportNotCycle(t *testing.T) {
	root := newModule(t)
	writeFile(t, root, "syntax/s.go", "package syntax\n")
	writeFile(t, root, "syntax/s_external_test.go",
		"package syntax_test\n\nimport _ \"example.com/m/syntax\"\n")

	g, err := Load(root)
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	for _, v := range g.Check() {
		if v.Rule == "R5" {
			t.Fatalf("外部测试包自引用被误判为环: %v", v)
		}
	}
}

func TestCleanModuleHasNoViolations(t *testing.T) {
	root := newModule(t)
	writeFile(t, root, "internal/simd/s.go", "package simd\n")
	writeFile(t, root, "internal/prediction/p.go", "package prediction\n\nimport _ \"example.com/m/internal/simd\"\n")
	writeFile(t, root, "decoder/d.go", "package decoder\n\nimport _ \"example.com/m/internal/prediction\"\n")
	writeFile(t, root, "encoder/e.go", "package encoder\n\nimport _ \"example.com/m/internal/prediction\"\n")

	g, err := Load(root)
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if v := g.Check(); len(v) != 0 {
		t.Fatalf("期望无违规，得到 %v", v)
	}
}
