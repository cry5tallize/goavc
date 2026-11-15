// h264probe 提供 GOP 统计与逐 NAL 语法树；仅解析头部，不解码像素或残差，
// 探测成功不能替代 Decoder 或独立 Oracle 对完整码流的验证。
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/nal"
)

type pictureInfo struct {
	Type             string
	FrameNum, POCLsb uint32
	PPS              uint32
	Reference        bool
	Slices           int
	Profile          uint8
	Width, Height    uint64
}

type report struct {
	Pictures        []pictureInfo
	Counts          map[string]int
	IDRIndices      []int
	MaxConsecutiveB int
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, out, stderr io.Writer) error {
	return runWithInput(args, os.Stdin, out, stderr)
}

func runWithInput(args []string, stdin io.Reader, out, stderr io.Writer) error {
	f := flag.NewFlagSet("h264probe", flag.ContinueOnError)
	f.SetOutput(stderr)
	input := f.String("input", "", "输入路径；-表示stdin")
	format := f.String("format", "annexb", "输入格式：annexb或avcc")
	lengthSize := f.Int("length-size", 4, "AVCC长度前缀字节数，1..4")
	syntaxMode := f.Bool("syntax", false, "逐NAL输出JSON Lines语法树，不解码残差；错误前可能已有输出")
	maxNALSize := f.Int("max-nal-size", 64<<20, "单NAL字节上限，包含头部；最大64MiB")
	maxNALs := f.Int("max-nals", 1<<20, "NAL数量上限")
	limit := f.Int("max-frames", 1<<20, "GOP统计最大图片数")
	frames := f.Int("expect-frames", -1, "断言图片总数，-1关闭")
	b := f.Int("expect-max-b", -1, "断言连续B图片最大数，-1关闭")
	idr := f.Int("expect-idr-interval", -1, "断言相邻IDR图片的编码顺序间隔，-1关闭")
	if err := f.Parse(args); err != nil {
		return err
	}
	if *input == "" || *limit < 1 || *limit > 1<<24 || *frames < -1 || *b < -1 || *idr < -1 || *idr == 0 || f.NArg() != 0 || (*format != "annexb" && *format != "avcc") || *lengthSize < 1 || *lengthSize > 4 || *maxNALSize < 1 || *maxNALSize > 64<<20 || *maxNALs < 1 || *maxNALs > 1<<24 || *syntaxMode && (*frames >= 0 || *b >= 0 || *idr >= 0) {
		return fmt.Errorf("h264probe: 输入或资源上限无效: %w", h264.ErrInvalidData)
	}
	var in io.Reader = stdin
	if *input != "-" {
		file, err := os.Open(*input)
		if err != nil {
			return err
		}
		defer file.Close()
		in = file
	}
	reader, err := newUnitReader(in, *format, *lengthSize, *maxNALSize)
	if err != nil {
		return err
	}
	units := &limitedUnits{unitReader: reader, limit: *maxNALs}
	if *syntaxMode {
		return probeSyntax(units, out)
	}
	r, err := probeUnits(units, *limit)
	if err != nil {
		return err
	}
	if *frames >= 0 && len(r.Pictures) != *frames || *b >= 0 && r.MaxConsecutiveB != *b {
		return fmt.Errorf("h264probe: GOP断言失败，图片%d、最大连续B%d", len(r.Pictures), r.MaxConsecutiveB)
	}
	if *idr >= 0 {
		if len(r.IDRIndices) == 0 || r.IDRIndices[0] != 0 {
			return fmt.Errorf("h264probe: 缺少首个IDR")
		}
		for i := 1; i < len(r.IDRIndices); i++ {
			if r.IDRIndices[i]-r.IDRIndices[i-1] != *idr {
				return fmt.Errorf("h264probe: IDR间隔断言失败，位置%v", r.IDRIndices)
			}
		}
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func probe(input io.Reader, maxFrames int) (report, error) {
	return probeUnits(&limitedUnits{unitReader: nal.NewAnnexBScannerWithLimit(input, 64<<20), limit: 1 << 20}, maxFrames)
}

func probeUnits(scanner unitReader, maxFrames int) (report, error) {
	r := report{Counts: map[string]int{}}
	state := newSyntaxState()
	consecutive := 0
	var previousInfo *nal.SliceInfo
	type sliceAddress struct{ count, first uint32 }
	seen := make(map[sliceAddress]bool)
	var lastCount uint32
	for {
		u, err := scanner.Next()
		if errors.Is(err, io.EOF) {
			return r, nil
		}
		if err != nil {
			return r, err
		}
		if u.Header.Type != nal.TypeSPS && u.Header.Type != nal.TypePPS && u.Header.Type != nal.TypeAUD && u.Header.Type != nal.TypeEndOfSequence && u.Header.Type != nal.TypeEndOfStream && !u.IsVCL() {
			continue
		}
		record, err := state.parse(u)
		if err != nil {
			return r, err
		}
		if u.Header.Type == nal.TypeAUD || u.Header.Type == nal.TypeEndOfSequence || u.Header.Type == nal.TypeEndOfStream {
			previousInfo = nil
		}
		if record.SliceHeader != nil {
			h := record.SliceHeader
			id := h.PicParameterSetID
			p := state.pps[id]
			s := state.sps[p.SeqParameterSetID]
			if !s.FrameMbsOnlyFlag {
				return r, fmt.Errorf("h264probe: 字段slice尚未提供图片统计: %w", h264.ErrUnsupported)
			}
			w, ew := s.DisplayWidth()
			height, eh := s.DisplayHeight()
			if ew != nil {
				return r, ew
			}
			if eh != nil {
				return r, eh
			}
			kind := h.Type.String()
			if u.IsIDR() {
				kind = "IDR"
			}
			info := pictureInfo{Type: kind, FrameNum: h.FrameNum, POCLsb: h.PicOrderCntLsb, PPS: id, Reference: u.Header.RefIDC != 0, Profile: s.ProfileIDC, Width: w, Height: height}
			infoSummary := probeSliceInfo(h, u.Header.RefIDC)
			if previousInfo == nil || nal.DetectPictureBoundary(previousInfo, infoSummary) == nal.BoundaryNew {
				clear(seen)
				lastCount = h.RedundantPicCnt
				if len(r.Pictures) >= maxFrames {
					return r, fmt.Errorf("h264probe: 图片数超限: %w", h264.ErrResourceLimit)
				}
				if kind == "IDR" {
					r.IDRIndices = append(r.IDRIndices, len(r.Pictures))
				}
				r.Pictures = append(r.Pictures, info)
				r.Counts[kind]++
				consecutive++
				if kind != "B" {
					consecutive = 0
				}
				r.MaxConsecutiveB = max(r.MaxConsecutiveB, consecutive)
			} else {
				if h.RedundantPicCnt < lastCount {
					return r, fmt.Errorf("h264probe: 冗余计数递减: %w", h264.ErrInvalidData)
				}
				lastCount = h.RedundantPicCnt
			}
			r.Pictures[len(r.Pictures)-1].Slices++
			address := sliceAddress{h.RedundantPicCnt, h.FirstMBInSlice}
			if seen[address] {
				return r, fmt.Errorf("h264probe: 重复slice地址: %w", h264.ErrInvalidData)
			}
			seen[address] = true
			previousInfo = infoSummary
		}
	}
}
