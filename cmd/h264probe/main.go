// h264probe读取Annex-B参数集与slice header，供GOP门禁使用；不解码像素，
// 头部探测成功不能替代Decoder或独立Oracle对残差的验证。
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/nal"
	"github.com/goavc/h264/syntax"
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
	f := flag.NewFlagSet("h264probe", flag.ContinueOnError)
	f.SetOutput(stderr)
	input := f.String("input", "", "Annex-B输入路径")
	limit := f.Int("max-frames", 1<<20, "最大图片数")
	frames := f.Int("expect-frames", -1, "断言图片总数，-1关闭")
	b := f.Int("expect-max-b", -1, "断言连续B图片最大数，-1关闭")
	idr := f.Int("expect-idr-interval", -1, "断言相邻IDR图片的编码顺序间隔，-1关闭")
	if err := f.Parse(args); err != nil {
		return err
	}
	if *input == "" || *limit < 1 || *limit > 1<<24 || *frames < -1 || *b < -1 || *idr < -1 || *idr == 0 || f.NArg() != 0 {
		return fmt.Errorf("h264probe: 输入或资源上限无效: %w", h264.ErrInvalidData)
	}
	in, err := os.Open(*input)
	if err != nil {
		return err
	}
	defer in.Close()
	r, err := probe(in, *limit)
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
	r := report{Counts: map[string]int{}}
	sps := map[uint32]*syntax.SPS{}
	pps := map[uint32]*syntax.PPS{}
	scanner := nal.NewAnnexBScannerWithLimit(input, 64<<20)
	consecutive := 0
	var lastMB uint32
	for {
		u, err := scanner.Next()
		if errors.Is(err, io.EOF) {
			return r, nil
		}
		if err != nil {
			return r, err
		}
		if u.Header.Type != nal.TypeSPS && u.Header.Type != nal.TypePPS && !u.IsVCL() {
			continue
		}
		if u.IsVCL() && u.Header.Type != nal.TypeSlice && !u.IsIDR() {
			return r, fmt.Errorf("h264probe: 不支持数据分区: %w", h264.ErrUnsupported)
		}
		payload := u.Payload
		// 仅已解析CABAC PPS的slice允许规范终端zero-word转义；参数集仍严格校验。
		terminal := u.IsVCL() && len(payload) >= 3 && payload[len(payload)-3] == 0 && payload[len(payload)-2] == 0 && payload[len(payload)-1] == 3
		if terminal {
			payload = payload[:len(payload)-1]
		}
		rbsp, err := bitstream.EBSPToRBSP(payload)
		if err != nil {
			return r, err
		}
		switch u.Header.Type {
		case nal.TypeSPS:
			s, err := syntax.ParseSPS(rbsp)
			if err != nil {
				return r, err
			}
			sps[s.SeqParameterSetID] = s
		case nal.TypePPS:
			bits := bitstream.NewReader(rbsp)
			if _, err = bits.ReadUE(); err != nil {
				return r, err
			}
			id, err := bits.ReadUE()
			if err != nil || sps[id] == nil {
				return r, fmt.Errorf("h264probe: PPS缺少有效SPS: %w", h264.ErrInvalidData)
			}
			p, err := syntax.ParsePPS(rbsp, sps[id])
			if err != nil {
				return r, err
			}
			pps[p.PicParameterSetID] = p
		default:
			bits := bitstream.NewReader(rbsp)
			for range 2 {
				if _, err = bits.ReadUE(); err != nil {
					return r, err
				}
			}
			id, err := bits.ReadUE()
			p := pps[id]
			if err != nil || p == nil || terminal && !p.EntropyCodingModeFlag {
				return r, fmt.Errorf("h264probe: slice缺少有效PPS或终端转义非法: %w", h264.ErrInvalidData)
			}
			s := sps[p.SeqParameterSetID]
			h, err := syntax.ParseSliceHeaderEx(rbsp, s, pps, u.IsIDR(), u.Header.RefIDC)
			if err != nil {
				return r, err
			}
			if !s.FrameMbsOnlyFlag || h.RedundantPicCnt != 0 {
				return r, fmt.Errorf("h264probe: 字段/冗余slice尚未提供图片统计: %w", h264.ErrUnsupported)
			}
			w, ew := s.DisplayWidth()
			height, eh := s.DisplayHeight()
			if ew != nil {
				return r, ew
			}
			if eh != nil {
				return r, eh
			}
			if w > h264.MaxFrameWidth || height > h264.MaxFrameHeight || s.PicWidthInMbs() > h264.MaxFrameWidth/16 || s.FrameHeightInMbs() > h264.MaxFrameHeight/16 {
				return r, fmt.Errorf("h264probe: 显示尺寸无效或超限: %w", h264.ErrResourceLimit)
			}
			if uint64(h.FirstMBInSlice) >= s.PicWidthInMbs()*s.FrameHeightInMbs() || u.IsIDR() && u.Header.RefIDC == 0 {
				return r, fmt.Errorf("h264probe: slice地址或IDR参考标志无效: %w", h264.ErrInvalidData)
			}
			kind := h.Type.String()
			if u.IsIDR() {
				kind = "IDR"
			}
			info := pictureInfo{Type: kind, FrameNum: h.FrameNum, POCLsb: h.PicOrderCntLsb, PPS: id, Reference: u.Header.RefIDC != 0, Profile: s.ProfileIDC, Width: w, Height: height}
			if h.FirstMBInSlice == 0 {
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
			} else if len(r.Pictures) == 0 {
				return r, fmt.Errorf("h264probe: 不支持首slice非零的ASO流: %w", h264.ErrUnsupported)
			} else {
				if h.FirstMBInSlice == lastMB {
					return r, fmt.Errorf("h264probe: 重复slice地址: %w", h264.ErrInvalidData)
				}
				if h.FirstMBInSlice < lastMB {
					return r, fmt.Errorf("h264probe: 非递增slice地址尚不支持: %w", h264.ErrUnsupported)
				}
				previous := r.Pictures[len(r.Pictures)-1]
				previous.Slices = 0
				if previous != info {
					return r, fmt.Errorf("h264probe: 多slice图片标识不一致: %w", h264.ErrInvalidData)
				}
			}
			r.Pictures[len(r.Pictures)-1].Slices++
			lastMB = h.FirstMBInSlice
		}
	}
}
