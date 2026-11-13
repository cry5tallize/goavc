// Command manifest 校验测试语料清单。
//
// 用法：
//
//	go run ./tools/manifest -file testdata/manifest.json
//	go run ./tools/manifest -file testdata/manifest.json -root .
//
// 校验内容：字段完整性、枚举取值、名称唯一性、文件存在性、SHA-256 一致性。
// 任何一条不通过就以非零退出码结束，CI 据此阻断合并。
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/goavc/h264/internal/testutil/manifest"
)

func main() {
	file := flag.String("file", "testdata/manifest.json", "manifest 文件路径")
	root := flag.String("root", ".", "语料根目录（manifest 中的相对路径以它为基准）")
	flag.Parse()

	f, err := manifest.Load(*file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "manifest: %v\n", err)
		os.Exit(1)
	}

	errs := f.Validate(*root)
	for _, err := range errs {
		fmt.Fprintln(os.Stderr, err)
	}
	if len(errs) > 0 {
		fmt.Fprintf(os.Stderr, "manifest: %d 条语料中发现 %d 个问题\n", len(f.Entries), len(errs))
		os.Exit(1)
	}
	fmt.Printf("manifest: %d 条语料校验通过\n", len(f.Entries))
}
