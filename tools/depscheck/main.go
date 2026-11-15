// Command depscheck 校验模块内的依赖方向是否符合 docs/architecture.md。
//
// 用法：
//
//	go run ./tools/depscheck           # 以当前目录为模块根
//	go run ./tools/depscheck -root .
//
// 退出码：0 表示无违规，1 表示存在违规（CI 据此阻断合并）。
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/cry5tallize/goavc/internal/testutil/importgraph"
)

func main() {
	root := flag.String("root", ".", "模块根目录（包含 go.mod）")
	flag.Parse()

	g, err := importgraph.Load(*root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "depscheck: 加载失败: %v\n", err)
		os.Exit(1)
	}

	violations := g.Check()
	for _, v := range violations {
		fmt.Fprintln(os.Stderr, v.String())
	}
	if len(violations) > 0 {
		fmt.Fprintf(os.Stderr, "depscheck: %d 个包中发现 %d 处依赖违规\n", len(g.Packages), len(violations))
		os.Exit(1)
	}
	fmt.Printf("depscheck: %d 个包，R1/R2/R3/R5 全部通过\n", len(g.Packages))
}
