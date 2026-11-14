package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/nal"
	"github.com/goavc/h264/syntax"
)

// advancedMetadata 与 manifest 的已有字段同名；generated 只表示已构造，
// 不表示经过 FFmpeg 或本项目解码器验证。期望来自 PCM 源样本或独立空间解析解。
type advancedMetadata struct {
	Name              string   `json:"name"`
	Path              string   `json:"path"`
	Source            string   `json:"source"`
	Profile           string   `json:"profile"`
	ProfileIDC        uint8    `json:"profileIDC"`
	Level             string   `json:"level"`
	LevelIDC          uint8    `json:"levelIDC"`
	Width             int      `json:"width"`
	Height            int      `json:"height"`
	PixFmt            string   `json:"pixFmt"`
	BitDepth          int      `json:"bitDepth"`
	Entropy           string   `json:"entropy"`
	SliceTypes        []string `json:"sliceTypes"`
	SliceCount        int      `json:"sliceCount"`
	RefFrames         int      `json:"refFrames"`
	FrameCount        int      `json:"frameCount"`
	Tags              []string `json:"tags"`
	SHA256            string   `json:"sha256"`
	ExpectedYUVPath   string   `json:"expectedYUVPath"`
	ExpectedYUVSha256 string   `json:"expectedYUVSha256"`
	ExpectedYUVBytes  int      `json:"expectedYUVBytes"`
	Oracle            string   `json:"oracle"`
	Status            string   `json:"status"`
	Notes             string   `json:"notes"`
	GenerationCommand string   `json:"generationCommand"`
}

type advancedBits struct{ w *bitstream.Writer }

func newAdvancedBits() advancedBits          { return advancedBits{w: bitstream.NewWriter()} }
func (b advancedBits) ue(v uint32)           { must(b.w.WriteUE(v)) }
func (b advancedBits) se(v int32)            { must(b.w.WriteSE(v)) }
func (b advancedBits) bits(n uint, v uint64) { must(b.w.WriteBits(n, v)) }
func (b advancedBits) rbsp() []byte {
	must(bitstream.WriteRBSPTrailingBits(b.w))
	return b.w.Bytes()
}

type advancedStream struct {
	sps  *syntax.SPS
	pps  *syntax.PPS
	data []byte
}

func advancedParameters(mbs, level uint32) *syntax.SPS {
	return &syntax.SPS{ProfileIDC: 88, LevelIDC: uint8(level), ChromaFormatIDC: 1,
		MaxNumRefFrames: 1, PicOrderCntType: 2, PicWidthInMbsMinus1: mbs - 1,
		PicHeightInMapUnitsMinus1: mbs - 1, FrameMbsOnlyFlag: true, Direct8x8InferenceFlag: true,
		VUIParametersPresentFlag: true, VUI: &syntax.VUI{BitstreamRestrictionFlag: true,
			MotionVectorsOverPicBoundariesFlag: true, Log2MaxMVLengthHorizontal: 15,
			Log2MaxMVLengthVertical: 15, MaxNumReorderFrames: 0, MaxDecFrameBuffering: 1}}
}

func newAdvancedStream(s *syntax.SPS, p *syntax.PPS) *advancedStream {
	p.DeblockingFilterControlPresentFlag = true
	a := &advancedStream{sps: s, pps: p}
	srb, err := s.MarshalRBSP()
	must(err)
	prb, err := p.MarshalRBSP(s)
	must(err)
	a.unit(nal.TypeSPS, srb)
	a.unit(nal.TypePPS, prb)
	return a
}

func (a *advancedStream) unit(kind nal.UnitType, rbsp []byte) {
	a.data = append(a.data, 0, 0, 0, 1)
	a.data = append(a.data, nal.Header{Type: kind, RefIDC: 3}.Byte())
	a.data = append(a.data, bitstream.RBSPToEBSP(rbsp)...)
}

func advancedSample(c, mb, x, y int) byte {
	return byte(17 + (mb*29+c*53+x*7+y*11)%221)
}

func advancedRedundantSample(c, mb, x, y, bias int) byte {
	return byte(19 + (mb*23+c*47+x*5+y*9+bias)%211)
}

// 原始 PCM 的源采样与平面拼接，不经过任何编解码或变换内核。
func advancedRaw(width, height int, sample func(c, x, y int) byte) []byte {
	raw := make([]byte, 0, width*height*3/2)
	for c := range 3 {
		w, h := width, height
		if c != 0 {
			w, h = w/2, h/2
		}
		for y := range h {
			for x := range w {
				raw = append(raw, sample(c, x, y))
			}
		}
	}
	return raw
}

func advancedPCMRaw() []byte {
	return advancedRaw(64, 64, func(c, x, y int) byte {
		n := 16
		if c != 0 {
			n = 8
		}
		return advancedSample(c, y/n*4+x/n, x%n, y%n)
	})
}

func advancedPCM(b advancedBits, mb int, sample func(c, mb, x, y int) byte) {
	b.w.ByteAlign()
	for c := range 3 {
		n := 16
		if c != 0 {
			n = 8
		}
		for y := range n {
			for x := range n {
				b.bits(8, uint64(sample(c, mb, x, y)))
			}
		}
	}
}

type advancedFMOMap struct {
	pps    syntax.PPS
	groups [][]int
	cycle  uint32
}

// 地址逐项手列，不能调用 decoder 的 map/NextMbAddress 推导自己的期望。
func advancedFMOMaps() []advancedFMOMap {
	return []advancedFMOMap{
		{pps: syntax.PPS{NumSliceGroupsMinus1: 1, SliceGroupMapType: 0, RunLengthMinus1: []uint32{1, 0}}, groups: [][]int{{0, 1, 3, 4, 6, 7, 9, 10, 12, 13, 15}, {2, 5, 8, 11, 14}}},
		{pps: syntax.PPS{NumSliceGroupsMinus1: 1, SliceGroupMapType: 1}, groups: [][]int{{0, 2, 5, 7, 8, 10, 13, 15}, {1, 3, 4, 6, 9, 11, 12, 14}}},
		{pps: syntax.PPS{NumSliceGroupsMinus1: 1, SliceGroupMapType: 2, TopLeft: []uint32{0}, BottomRight: []uint32{5}}, groups: [][]int{{0, 1, 4, 5}, {2, 3, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}}},
		{pps: syntax.PPS{NumSliceGroupsMinus1: 1, SliceGroupMapType: 3}, cycle: 1, groups: [][]int{{10}, {0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 11, 12, 13, 14, 15}}},
		{pps: syntax.PPS{NumSliceGroupsMinus1: 1, SliceGroupMapType: 4}, cycle: 5, groups: [][]int{{0, 1, 2, 3, 4}, {5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}}},
		{pps: syntax.PPS{NumSliceGroupsMinus1: 1, SliceGroupMapType: 5}, cycle: 5, groups: [][]int{{0, 1, 4, 8, 12}, {2, 3, 5, 6, 7, 9, 10, 11, 13, 14, 15}}},
		{pps: syntax.PPS{NumSliceGroupsMinus1: 2, SliceGroupMapType: 6, PicSizeInMapUnitsMinus1: 15, SliceGroupID: []uint8{0, 2, 1, 0, 1, 0, 2, 1, 2, 1, 0, 2, 0, 2, 1, 0}}, groups: [][]int{{0, 3, 5, 10, 12, 15}, {2, 4, 7, 9, 14}, {1, 6, 8, 11, 13}}},
	}
}

func advancedPCMSlice(a *advancedStream, addresses []int, cycle uint32) {
	b := newAdvancedBits()
	b.ue(uint32(addresses[0]))
	b.ue(2)
	b.ue(0)
	b.bits(4, 0)
	b.ue(0)
	b.bits(2, 0)
	b.se(0)
	b.ue(1)
	if a.pps.NumSliceGroupsMinus1 > 0 && a.pps.SliceGroupMapType >= 3 && a.pps.SliceGroupMapType <= 5 {
		b.bits(5, uint64(cycle)) // ceil(log2(16/1+1))=5。
	}
	for _, mb := range addresses {
		b.ue(25)
		advancedPCM(b, mb, advancedSample)
	}
	a.unit(nal.TypeSliceIDR, b.rbsp())
}

func advancedPartition() (*advancedStream, []byte) {
	a := newAdvancedStream(advancedParameters(4, 20), &syntax.PPS{})
	addresses := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	advancedPCMSlice(a, addresses, 0)
	b, intra, inter := newAdvancedBits(), newAdvancedBits(), newAdvancedBits()
	b.ue(0)
	b.ue(0) // P slice，可同时带 I 和 P 宏块。
	b.ue(0)
	b.bits(4, 1)
	b.bits(1, 0)
	b.bits(1, 0)
	b.bits(1, 0)
	b.se(0) // QPY26。
	b.ue(1)
	b.ue(0) // slice_id，B/C 独立携带同一标识。
	intra.ue(0)
	inter.ue(0)
	b.ue(0)
	b.ue(8) // P 中的 I16x16 DC，CBP=0。
	b.ue(0)
	b.se(0)
	intra.bits(4, 5) // coeff_token=01，正号=0，total_zeros=0 -> 1。
	b.ue(0)
	b.ue(0) // P_L0_16x16。
	b.se(8) // 整像素右移：亮度2、色度1，无插值舍入。
	b.se(0)
	b.ue(2) // CBP=1。
	b.se(0)
	inter.bits(4, 5) // 左上4x4 DC=+1。
	inter.bits(3, 7) // 其余三个4x4为空，nC<=1 的 token 都为1。
	b.ue(14)
	a.unit(nal.TypeSlicePartitionA, b.rbsp())
	a.unit(nal.TypeSlicePartitionB, intra.rbsp())
	a.unit(nal.TypeSlicePartitionC, inter.rbsp())
	raw := advancedPCMRaw()
	raw = append(raw, advancedRaw(64, 64, func(c, x, y int) byte {
		n := 16
		if c != 0 {
			n = 8
		}
		mb := y/n*4 + x/n
		if mb == 0 {
			if c == 0 {
				return 129
			} // I16 DC：13*16/4=52；(52+32)>>6=1。
			return 128
		}
		sx := x
		if mb == 1 {
			if c == 0 {
				sx += 2
			} else {
				sx++
			}
		}
		v := int(advancedSample(c, y/n*4+sx/n, sx%n, y%n))
		if mb == 1 && c == 0 && x%n < 4 && y%n < 4 {
			v += 3 // 普通4x4 DC：13*16=208；(208+32)>>6=3。
		}
		if v > 255 {
			v = 255
		}
		return byte(v)
	})...)
	return a, raw
}

// 仅写固定的单 DC 系数 ±64，不实现通用 CAVLC 或完整变换算法。
// TotalCoeff=1/TrailingOnes=0：亮度 token=000101，色度 token=000111；
// 首 levelCode=124/125，prefix=15、suffixLength=12，suffix=94/95；total_zeros=0 为1。
func advancedSwitchingDC(b advancedBits, chroma, negative bool) {
	token := uint64(5)
	if chroma {
		token = 7
	}
	b.bits(6, token)
	b.bits(15, 0)
	b.bits(1, 1)
	suffix := uint64(94)
	if negative {
		suffix++
	}
	b.bits(12, suffix)
	b.bits(1, 1)
}

func advancedSwitching(si, secondary, negative bool) (*advancedStream, []byte) {
	a := newAdvancedStream(advancedParameters(1, 10), &syntax.PPS{})
	var raw []byte
	if !si {
		b := newAdvancedBits()
		b.ue(0)
		b.ue(2)
		b.ue(0)
		b.bits(4, 0)
		b.ue(0)
		b.bits(2, 0)
		b.se(0)
		b.ue(1)
		b.ue(25)
		advancedPCM(b, 0, func(c, mb, x, y int) byte {
			if c == 0 {
				return 128
			}
			return 0
		})
		a.unit(nal.TypeSliceIDR, b.rbsp())
		raw = advancedRaw(16, 16, func(c, x, y int) byte {
			if c == 0 {
				return 128
			}
			return 0
		})
	}
	b := newAdvancedBits()
	b.ue(0)
	kind := nal.TypeSlice
	if si {
		b.ue(4)
		b.ue(0)
		b.bits(4, 0)
		b.ue(0)
		b.bits(2, 0)
		kind = nal.TypeSliceIDR
	} else {
		b.ue(3)
		b.ue(0)
		b.bits(4, 1)
		b.bits(3, 0) // num_ref_idx_active_override、重排、参考标记均关闭。
	}
	b.se(-26) // QPY=0。
	if !si {
		b.bits(1, uint64(boolInt(secondary)))
	}
	b.se(-14) // QSY=12，严格不同于 QPY。
	b.ue(1)
	if !si {
		b.ue(0)
	} // mb_skip_run。
	b.ue(0)
	if si {
		b.bits(16, 65535) // 16 个 Intra4x4 MPM=DC。
		b.ue(0)
		b.ue(33) // intra CBP=17。
	} else {
		b.se(0)
		b.se(0)
		b.ue(32) // inter CBP=17。
	}
	b.se(0)
	advancedSwitchingDC(b, false, negative)
	b.bits(3, 7) // 剩余三个亮度4x4为空，nC依次为1、1、0。
	advancedSwitchingDC(b, true, false)
	advancedSwitchingDC(b, true, false)
	a.unit(kind, b.rbsp())
	// 单 DC 的空间基是常量1。primary 在 QP0 下的增量=64*10/64=10；
	// secondary/SI 在 QS12 下为64*40/64=40。预测128的DC在QS12中恰好可表示。
	// 色度 DC primary：(64*10/2+32)>>6=5；secondary：(64+32)>>6=1；
	// SI 还加入128的变换域预测：((8192+10)/20+64+32)>>6=7。
	delta, chroma := 10, byte(5)
	if secondary || si {
		delta, chroma = 40, 1
	}
	if si {
		chroma = 7
	}
	if negative {
		delta = -delta
	}
	raw = append(raw, advancedRaw(16, 16, func(c, x, y int) byte {
		if c != 0 {
			return chroma
		}
		if si || x < 4 && y < 4 {
			return byte(128 + delta)
		}
		return 128
	})...)
	return a, raw
}

func advancedRedundant() (*advancedStream, []byte) {
	s := advancedParameters(4, 20)
	s.PicOrderCntType = 0
	a := newAdvancedStream(s, &syntax.PPS{RedundantPicCntPresentFlag: true})
	writeSlice := func(idr bool, frame, poc uint64, count uint32, first, length, bias int) {
		b := newAdvancedBits()
		b.ue(uint32(first))
		b.ue(2)
		b.ue(0)
		b.bits(4, frame)
		if idr {
			b.ue(0)
		}
		b.bits(4, poc)
		b.ue(count)
		if idr {
			b.bits(2, 0)
		} else {
			b.bits(1, 0)
		}
		b.se(0)
		b.ue(1)
		for mb := first; mb < first+length; mb++ {
			b.ue(25)
			advancedPCM(b, mb, func(c, mb, x, y int) byte {
				return advancedRedundantSample(c, mb, x, y, bias)
			})
		}
		kind := nal.TypeSlice
		if idr {
			kind = nal.TypeSliceIDR
		}
		a.unit(kind, b.rbsp())
	}
	writeSlice(true, 0, 0, 0, 0, 16, 0)
	writeSlice(false, 1, 2, 0, 0, 8, 31)
	writeSlice(false, 1, 2, 0, 8, 8, 31)
	writeSlice(false, 1, 2, 1, 0, 16, 77)
	var raw []byte
	for _, bias := range []int{0, 31} {
		raw = append(raw, advancedRaw(64, 64, func(c, x, y int) byte {
			n := 16
			if c != 0 {
				n = 8
			}
			return advancedRedundantSample(c, y/n*4+x/n, x%n, y%n, bias)
		})...)
	}
	return a, raw
}

func advancedSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func generateAdvanced(outputDir string) {
	must(os.MkdirAll(outputDir, 0755))
	var entries []advancedMetadata
	emit := func(name string, a *advancedStream, raw []byte, maxSlices int, types, tags []string, notes string) {
		width := int(a.sps.PicWidthInMbsMinus1+1) * 16
		height := int(a.sps.PicHeightInMapUnitsMinus1+1) * 16
		path := filepath.Join(outputDir, name+".264")
		yuvPath := filepath.Join(outputDir, name+".yuv")
		must(os.WriteFile(path, a.data, 0644))
		must(os.WriteFile(yuvPath, raw, 0644))
		entry := advancedMetadata{Name: name, Path: filepath.ToSlash(path), Source: "goavc-normative-fixtures",
			Profile: "extended", ProfileIDC: a.sps.ProfileIDC, Level: fmt.Sprintf("%d.%d", a.sps.LevelIDC/10, a.sps.LevelIDC%10),
			LevelIDC: a.sps.LevelIDC, Width: width, Height: height, PixFmt: "yuv420p", BitDepth: 8, Entropy: "cavlc",
			SliceTypes: types, SliceCount: maxSlices, RefFrames: 1, FrameCount: len(raw) / (width * height * 3 / 2), Tags: append([]string{"m7_advanced"}, tags...),
			SHA256: advancedSHA256(a.data), ExpectedYUVPath: filepath.ToSlash(yuvPath), ExpectedYUVSha256: advancedSHA256(raw),
			ExpectedYUVBytes: len(raw), Oracle: "independent-analytic", Status: "generated",
			Notes:             notes + " Independent PCM/closed-form expectation; not an FFmpeg Oracle result.",
			GenerationCommand: "go run ./tools/fixtures -kind advanced -output-dir " + filepath.ToSlash(outputDir)}
		metadata, err := json.MarshalIndent(entry, "", "  ")
		must(err)
		must(os.WriteFile(filepath.Join(outputDir, name+".json"), append(metadata, '\n'), 0644))
		entries = append(entries, entry)
		fmt.Printf("generated %s (%d bytes, %d frames; analytic YUV %s)\n", path, len(a.data), entry.FrameCount, entry.ExpectedYUVSha256)
	}
	for i, v := range advancedFMOMaps() {
		a := newAdvancedStream(advancedParameters(4, 20), &v.pps)
		for group := len(v.groups) - 1; group >= 0; group-- {
			advancedPCMSlice(a, v.groups[group], v.cycle)
		}
		emit(fmt.Sprintf("extended_64x64_yuv420p_cavlc_fmo%d_%03d", i, i+1), a, advancedPCMRaw(), len(v.groups), []string{"I"},
			[]string{"fmo", fmt.Sprintf("map%d", i), "pcm", "aso"}, "Nonuniform PCM; hand-listed map addresses; reverse slice-group arrival; deblocking disabled.")
	}
	a := newAdvancedStream(advancedParameters(4, 20), &syntax.PPS{})
	for mb := 15; mb >= 0; mb-- {
		advancedPCMSlice(a, []int{mb}, 0)
	}
	emit("extended_64x64_yuv420p_cavlc_aso_008", a, advancedPCMRaw(), 16, []string{"I"}, []string{"aso", "pcm"},
		"Sixteen one-MB slices in descending raster order; no FMO; nonuniform PCM; deblocking disabled.")
	a, raw := advancedPartition()
	emit("extended_64x64_yuv420p_cavlc_partitionabc_009", a, raw, 1, []string{"I", "P"}, []string{"data_partitioning", "partition_a", "partition_b", "partition_c", "nonzero_dc"},
		"PCM IDR then mixed intra/inter P partition A+B+C; both residual partitions have DC=+1; integer motion +2/+1 samples; QP26; deblocking disabled.")
	index := 10
	for mode := range 3 {
		for _, negative := range []bool{false, true} {
			si, secondary := mode == 2, mode == 1
			label, sliceType := "sp_primary", "SP"
			if secondary {
				label = "sp_secondary"
			}
			if si {
				label, sliceType = "si", "SI"
			}
			sign := "positive"
			if negative {
				sign = "negative"
			}
			a, raw := advancedSwitching(si, secondary, negative)
			types := []string{"I", sliceType}
			if si {
				types = []string{sliceType}
			}
			emit(fmt.Sprintf("extended_16x16_yuv420p_cavlc_%s_%s_%03d", label, sign, index), a, raw, 1, types,
				[]string{"switching", label, "nonzero_dc", "qs_ne_qp", sign},
				"Signed luma DC level +/-64, chroma DC +64; QPY0/QSY12; closed-form constant DC spatial basis; deblocking disabled.")
			index++
		}
	}
	a, raw = advancedRedundant()
	emit("extended_64x64_yuv420p_cavlc_redundant_016", a, raw, 2, []string{"I"}, []string{"redundant", "primary", "backup", "pcm"},
		"PCM IDR followed by two primary slices (count0/bias31) and complete different backup (count1/bias77); only the primary contributes the second output frame.")
	metadata, err := json.MarshalIndent(struct {
		Version int                `json:"version"`
		Entries []advancedMetadata `json:"entries"`
	}{Version: 1, Entries: entries}, "", "  ")
	must(err)
	must(os.WriteFile(filepath.Join(outputDir, "metadata.json"), append(metadata, '\n'), 0644))
}
