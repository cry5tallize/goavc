// Command oracle 是开发期的解码器差分工具：用 FFmpeg 解码 H.264 样本，
// 再把结果与参考 YUV 逐平面逐像素比较。
//
// 用法：
//
//	go run ./tools/oracle -input clip.264 -ref clip.yuv -width 352 -height 288 \
//	    -pixfmt yuv420p
//
// 退出码：
//
//	0  完全一致
//	2  存在像素差异（打印首个差异位置与统计）
//	3  找不到 ffmpeg（视为"环境不具备 Oracle 能力"，不是测试失败）
//	4  ffmpeg 执行失败或参考文件读取失败
//
// 设计约束：本工具只是开发期辅助，绝不能成为库的运行时依赖。
// 运行时依赖 FFmpeg 会直接违反项目的"零外部依赖"约束。
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/goavc/h264/internal/testutil/yuvdiff"
)

// pixFmtInfo 描述像素格式的色度抽样与存储字节数。
type pixFmtInfo struct {
	subW, subH int
	bps        int
}

var pixFmts = map[string]pixFmtInfo{
	"yuv420p":     {2, 2, 1},
	"yuv422p":     {2, 1, 1},
	"yuv444p":     {1, 1, 1},
	"yuv420p10le": {2, 2, 2},
	"yuv422p10le": {2, 1, 2},
	"yuv444p10le": {1, 1, 2},
}

func main() {
	input := flag.String("input", "", "H.264 输入文件（Annex-B 或 AVCC）")
	ref := flag.String("ref", "", "参考 YUV 文件（紧密排列）")
	pixFmt := flag.String("pixfmt", "yuv420p", "参考 YUV 的像素格式")
	width := flag.Int("width", 0, "宽度（像素）")
	height := flag.Int("height", 0, "高度（像素）")
	frames := flag.Int("frames", 0, "最多比较的帧数，0 表示全部")
	ffmpegPath := flag.String("ffmpeg", "ffmpeg", "ffmpeg 可执行文件路径")
	output := flag.String("out", "", "解码输出路径，留空则使用临时文件")
	keep := flag.Bool("keep", false, "保留解码输出用于人工排查")
	flag.Parse()

	if *input == "" || *ref == "" || *width <= 0 || *height <= 0 {
		fmt.Fprintln(os.Stderr, "oracle: -input、-ref、-width、-height 均为必填参数")
		flag.Usage()
		os.Exit(4)
	}
	info, ok := pixFmts[*pixFmt]
	if !ok {
		fmt.Fprintf(os.Stderr, "oracle: 未知像素格式 %q\n", *pixFmt)
		os.Exit(4)
	}

	ffmpeg, err := exec.LookPath(*ffmpegPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "oracle: 未找到 ffmpeg（%v）；该环境下跳过 Oracle 对比\n", err)
		os.Exit(3)
	}

	outPath := *output
	if outPath == "" {
		outPath = filepath.Join(os.TempDir(), "h264-oracle-"+strconv.Itoa(os.Getpid())+".yuv")
	}
	if !*keep && *output == "" {
		defer os.Remove(outPath)
	}

	args := []string{"-v", "error", "-i", *input}
	if *frames > 0 {
		args = append(args, "-frames:v", strconv.Itoa(*frames))
	}
	args = append(args, "-f", "rawvideo", "-pix_fmt", *pixFmt, "-y", outPath)

	cmd := exec.Command(ffmpeg, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "oracle: ffmpeg 执行失败: %v\n%s\n", err, out)
		os.Exit(4)
	}

	refData, err := os.ReadFile(*ref)
	if err != nil {
		fmt.Fprintf(os.Stderr, "oracle: 读取参考 YUV 失败: %v\n", err)
		os.Exit(4)
	}
	gotData, err := os.ReadFile(outPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "oracle: 读取解码输出失败: %v\n", err)
		os.Exit(4)
	}

	layout := yuvdiff.PlanarLayout(*width, *height, info.bps, info.subW, info.subH)
	res, err := yuvdiff.Compare(refData, gotData, layout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "oracle: 比较失败: %v\n", err)
		os.Exit(4)
	}

	if !res.Same() {
		fmt.Fprintf(os.Stderr, "oracle: 像素不一致: %s\n", res)
		os.Exit(2)
	}
	fmt.Printf("oracle: 一致 (%s)\n", res)
}
