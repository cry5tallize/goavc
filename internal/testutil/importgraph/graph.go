package importgraph

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Package 描述模块内的一个 Go 包（按目录聚合，包含 _test.go）。
type Package struct {
	// Path 是完整 import 路径，例如 github.com/goavc/h264/decoder。
	Path string
	// Dir 是包所在目录的绝对路径。
	Dir string
	// Imports 是去重后的 import 列表（含标准库与外部模块）。
	Imports []string
}

// Graph 是一个模块的包依赖图。
type Graph struct {
	// ModulePath 来自 go.mod 的 module 指令。
	ModulePath string
	// Root 是模块根目录的绝对路径。
	Root string
	// Packages 以 import 路径为键。
	Packages map[string]*Package
}

// Violation 描述一处依赖规则违规。
type Violation struct {
	// Rule 是规则编号（R1/R2/R3/R5），与 docs/architecture.md 对应。
	Rule string
	// From / To 是模块内相对路径，便于阅读。
	From string
	To   string
	// Reason 说明为什么这条依赖被禁止。
	Reason string
}

// String 输出稳定的单行描述，便于 CI 日志与 diff。
func (v Violation) String() string {
	return fmt.Sprintf("%s: %s -> %s: %s", v.Rule, v.From, v.To, v.Reason)
}

// simdImporters 是允许 import internal/simd 的模块内目录。
//
// 白名单而不是黑名单：新增一个包时默认"不允许直接触碰 SIMD"，
// 想用就必须显式修改这张表，从而强制经过一次依赖评审（R3）。
var simdImporters = map[string]bool{
	"internal/prediction": true,
	"internal/transform":  true,
	"internal/motion":     true,
	"internal/filter":     true,
	"internal/buffer":     true,
	"internal/simd":       true, // 自身与内部测试
	"decoder":             true,
	"encoder":             true,
}

// Load 解析 root 目录下的整个模块。
//
// 只做 AST 级 import 提取，不要求包能编译通过。这样在 M0/M1 阶段
//（大量包还只有 doc.go）也能执行依赖检查，把违规挡在更早的位置。
//
// 跳过规则：目录名以 "." 或 "_" 开头、以及 testdata / vendor ——
// 这些目录里的 Go 文件不参与构建，解析它们只会制造误报。
func Load(root string) (*Graph, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	mod, err := modulePath(abs)
	if err != nil {
		return nil, err
	}
	g := &Graph{ModulePath: mod, Root: abs, Packages: map[string]*Package{}}
	fset := token.NewFileSet()

	err = filepath.WalkDir(abs, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if path == abs {
				return nil
			}
			name := d.Name()
			if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") ||
				name == "testdata" || name == "vendor" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		dir := filepath.Dir(path)
		rel, err := filepath.Rel(abs, dir)
		if err != nil {
			return err
		}
		pkgPath := mod
		if rel != "." {
			pkgPath = mod + "/" + filepath.ToSlash(rel)
		}

		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		pkg := g.Packages[pkgPath]
		if pkg == nil {
			pkg = &Package{Path: pkgPath, Dir: dir}
			g.Packages[pkgPath] = pkg
		}
		for _, spec := range f.Imports {
			imp, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				continue
			}
			if !pkg.hasImport(imp) {
				pkg.Imports = append(pkg.Imports, imp)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return g, nil
}

func (p *Package) hasImport(path string) bool {
	for _, imp := range p.Imports {
		if imp == path {
			return true
		}
	}
	return false
}

// Rel 把完整 import 路径转成模块内相对路径（模块根返回 "."）。
func (g *Graph) Rel(path string) string {
	if path == g.ModulePath {
		return "."
	}
	if strings.HasPrefix(path, g.ModulePath+"/") {
		return strings.TrimPrefix(path, g.ModulePath+"/")
	}
	return path
}

// isModulePkg 判断 path 是否属于本模块（标准库与外部依赖会被排除）。
func (g *Graph) isModulePkg(path string) bool {
	return path == g.ModulePath || strings.HasPrefix(path, g.ModulePath+"/")
}

// isDir 判断 path 是否精确等于模块内某个目录对应的包路径。
func (g *Graph) isDir(path, rel string) bool {
	return path == g.ModulePath+"/"+rel
}

// isInternal 判断是否为 internal 下的共享内核包。
func (g *Graph) isInternal(rel string) bool {
	return strings.HasPrefix(rel, "internal/")
}

// Check 执行 R1/R2/R3/R5 四条可自动化的依赖规则，并返回全部违规。
//
// 未在此实现的 R4（公共 API 不暴露内部类型）与 R6（单文件不巨型）
// 属于 API 快照与代码评审范畴，由 tests + review 保证。
func (g *Graph) Check() []Violation {
	var out []Violation
	for _, pkgPath := range g.sortedPackages() {
		pkg := g.Packages[pkgPath]
		from := g.Rel(pkgPath)
		for _, imp := range pkg.Imports {
			if !g.isModulePkg(imp) {
				continue
			}
			to := g.Rel(imp)

			// R1：编码器与解码器不得相互依赖。
			if (from == "decoder" && to == "encoder") || (from == "encoder" && to == "decoder") {
				out = append(out, Violation{
					Rule:   "R1",
					From:   from,
					To:     to,
					Reason: "encoder and decoder must not depend on each other",
				})
			}

			// R2：共享内核不得反向依赖上层流水线。
			if g.isInternal(from) && (to == "decoder" || to == "encoder" || to == "stream") {
				out = append(out, Violation{
					Rule:   "R2",
					From:   from,
					To:     to,
					Reason: "shared internal packages must not import codec pipelines",
				})
			}

			// R3：SIMD 只能经白名单内的底层包使用。
			if to == "internal/simd" && !simdImporters[from] {
				out = append(out, Violation{
					Rule:   "R3",
					From:   from,
					To:     to,
					Reason: "package is not allowed to use simd kernels directly",
				})
			}
		}
	}

	// R5：循环依赖。go build 也会报，但在骨架阶段提前发现更省时间。
	out = append(out, g.cycleViolations()...)

	sort.Slice(out, func(i, j int) bool {
		if out[i].Rule != out[j].Rule {
			return out[i].Rule < out[j].Rule
		}
		if out[i].From != out[j].From {
			return out[i].From < out[j].From
		}
		return out[i].To < out[j].To
	})
	return out
}

func (g *Graph) sortedPackages() []string {
	paths := make([]string, 0, len(g.Packages))
	for p := range g.Packages {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

// cycleViolations 用三色标记 DFS 找出模块内的 import 环。
//
// 每发现一条回边就记录一条违规，并把环路径写进 Reason，
// 这样 CI 报错时能直接看出"谁引用了谁"。
func (g *Graph) cycleViolations() []Violation {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int, len(g.Packages))
	var stack []string
	var out []Violation

	var visit func(path string)
	visit = func(path string) {
		color[path] = gray
		stack = append(stack, path)
		pkg := g.Packages[path]
		imports := append([]string(nil), pkg.Imports...)
		sort.Strings(imports)
		for _, imp := range imports {
			if _, ok := g.Packages[imp]; !ok {
				continue
			}
			switch color[imp] {
			case gray:
				start := 0
				for i, s := range stack {
					if s == imp {
						start = i
						break
					}
				}
				cycle := append(append([]string(nil), stack[start:]...), imp)
				names := make([]string, 0, len(cycle))
				for _, c := range cycle {
					names = append(names, g.Rel(c))
				}
				out = append(out, Violation{
					Rule:   "R5",
					From:   g.Rel(path),
					To:     g.Rel(imp),
					Reason: "import cycle: " + strings.Join(names, " -> "),
				})
			case white:
				visit(imp)
			}
		}
		stack = stack[:len(stack)-1]
		color[path] = black
	}

	for _, p := range g.sortedPackages() {
		if color[p] == white {
			visit(p)
		}
	}
	return out
}

// modulePath 从 go.mod 读取 module 指令。
func modulePath(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("read go.mod: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "module ") {
			continue
		}
		path := strings.TrimSpace(strings.TrimPrefix(line, "module "))
		if path == "" {
			return "", fmt.Errorf("go.mod: empty module path")
		}
		return path, nil
	}
	return "", fmt.Errorf("go.mod: module directive not found")
}
