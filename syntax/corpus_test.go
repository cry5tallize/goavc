package syntax

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cry5tallize/goavc/bitstream"
	"github.com/cry5tallize/goavc/internal/testutil/manifest"
	"github.com/cry5tallize/goavc/nal"
)

// corpusEntry 是某个 manifest 条目解析出的参数集与 NAL 序列。
type corpusEntry struct {
	name              string
	maxSlicesPerFrame int
	frameCount        int
	units             []nal.Unit
	sps               *SPS
	pps               *PPS
}

// loadCorpus 读取并解析测试语料。
func loadCorpus(t *testing.T) []corpusEntry {
	t.Helper()
	root := ".."
	m, err := manifest.Load(filepath.Join(root, "testdata", "manifest.json"))
	if err != nil {
		t.Fatalf("加载 manifest 失败: %v", err)
	}
	var out []corpusEntry
	for _, e := range m.Entries {
		if e.Status == manifest.StatusUnsupported {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(e.Path)))
		if err != nil {
			t.Fatalf("%s: 读取失败: %v", e.Name, err)
		}
		units, err := nal.SplitAnnexB(data)
		if err != nil {
			t.Fatalf("%s: 切分失败: %v", e.Name, err)
		}
		ce := corpusEntry{name: e.Name, units: units, maxSlicesPerFrame: e.SliceCount, frameCount: e.FrameCount}
		for _, u := range units {
			switch u.Header.Type {
			case nal.TypeSPS:
				if ce.sps == nil {
					ce.sps = parseSPSUnit(t, u)
				}
			case nal.TypePPS:
				if ce.pps == nil {
					if ce.sps == nil {
						t.Fatalf("%s: PPS 出现在 SPS 之前", e.Name)
					}
					ce.pps = parsePPSUnit(t, u, ce.sps)
				}
			}
		}
		if ce.sps == nil || ce.pps == nil {
			t.Fatalf("%s: 缺少 SPS/PPS", e.Name)
		}
		out = append(out, ce)
	}
	return out
}

func parseSPSUnit(t *testing.T, u nal.Unit) *SPS {
	t.Helper()
	rbsp, err := bitstream.EBSPToRBSP(u.Payload)
	if err != nil {
		t.Fatalf("SPS EBSP→RBSP 失败: %v", err)
	}
	sps, err := ParseSPS(rbsp)
	if err != nil {
		t.Fatalf("解析 SPS 失败: %v", err)
	}
	return sps
}

func parsePPSUnit(t *testing.T, u nal.Unit, sps *SPS) *PPS {
	t.Helper()
	rbsp, err := bitstream.EBSPToRBSP(u.Payload)
	if err != nil {
		t.Fatalf("PPS EBSP→RBSP 失败: %v", err)
	}
	pps, err := ParsePPS(rbsp, sps)
	if err != nil {
		t.Fatalf("解析 PPS 失败: %v", err)
	}
	return pps
}

// TestCorpus_ParameterSetRoundTrip 验证语料中所有 SPS/PPS 的逐字节往返。
func TestCorpus_ParameterSetRoundTrip(t *testing.T) {
	entries := loadCorpus(t)
	if len(entries) == 0 {
		t.Skip("语料为空")
	}
	for _, ce := range entries {
		t.Run(ce.name, func(t *testing.T) {
			spsFound, ppsFound := false, false
			for _, u := range ce.units {
				switch u.Header.Type {
				case nal.TypeSPS:
					spsFound = true
					original, err := bitstream.EBSPToRBSP(u.Payload)
					if err != nil {
						t.Fatalf("SPS EBSP→RBSP 失败: %v", err)
					}
					got, err := ce.sps.MarshalRBSP()
					if err != nil {
						t.Fatalf("SPS 序列化失败: %v", err)
					}
					if !bytes.Equal(got, original) {
						t.Fatalf("SPS 往返不一致:\n got %x\nwant %x", got, original)
					}
				case nal.TypePPS:
					ppsFound = true
					original, err := bitstream.EBSPToRBSP(u.Payload)
					if err != nil {
						t.Fatalf("PPS EBSP→RBSP 失败: %v", err)
					}
					got, err := ce.pps.MarshalRBSP(ce.sps)
					if err != nil {
						t.Fatalf("PPS 序列化失败: %v", err)
					}
					if !bytes.Equal(got, original) {
						t.Fatalf("PPS 往返不一致:\n got %x\nwant %x", got, original)
					}
				}
			}
			if !spsFound || !ppsFound {
				t.Fatal("语料缺少 SPS 或 PPS")
			}
		})
	}
}

// TestCorpus_VUIRoundTrip 单独验证每条 SPS 中的 VUI。
func TestCorpus_VUIRoundTrip(t *testing.T) {
	for _, ce := range loadCorpus(t) {
		t.Run(ce.name, func(t *testing.T) {
			if !ce.sps.VUIParametersPresentFlag || ce.sps.VUI == nil {
				t.Skip("SPS 不含 VUI")
			}
			w := bitstream.NewWriter()
			if err := ce.sps.VUI.WriteTo(w); err != nil {
				t.Fatalf("VUI 序列化失败: %v", err)
			}
			if err := bitstream.WriteRBSPTrailingBits(w); err != nil {
				t.Fatalf("写尾位失败: %v", err)
			}
			r := bitstream.NewReader(w.Bytes())
			parsed, err := ParseVUI(r)
			if err != nil {
				t.Fatalf("VUI 解析失败: %v", err)
			}
			if !reflect.DeepEqual(parsed, ce.sps.VUI) {
				t.Fatalf("VUI 往返不一致:\n got %+v\nwant %+v", parsed, ce.sps.VUI)
			}
		})
	}
}

// TestCorpus_SEIRoundTrip 验证 SEI 消息框架的逐字节往返。
func TestCorpus_SEIRoundTrip(t *testing.T) {
	for _, ce := range loadCorpus(t) {
		t.Run(ce.name, func(t *testing.T) {
			found := false
			for _, u := range ce.units {
				if u.Header.Type != nal.TypeSEI {
					continue
				}
				found = true
				rbsp, err := bitstream.EBSPToRBSP(u.Payload)
				if err != nil {
					t.Fatalf("SEI EBSP→RBSP 失败: %v", err)
				}
				sei, err := ParseSEI(rbsp)
				if err != nil {
					t.Fatalf("SEI 解析失败: %v", err)
				}
				got, err := sei.MarshalRBSP()
				if err != nil {
					t.Fatalf("SEI 序列化失败: %v", err)
				}
				if !bytes.Equal(got, rbsp) {
					t.Fatalf("SEI 往返不一致:\n got %x\nwant %x", got, rbsp)
				}
			}
			if !found {
				t.Skip("语料不含 SEI")
			}
		})
	}
}

// TestCorpus_SliceHeaderAndAUAssembly 用真实 slice header 驱动 AU 组装。
func TestCorpus_SliceHeaderAndAUAssembly(t *testing.T) {
	for _, ce := range loadCorpus(t) {
		t.Run(ce.name, func(t *testing.T) {
			ppsByID := map[uint32]*PPS{ce.pps.PicParameterSetID: ce.pps}
			asm := nal.NewAssembler()
			var aus []*nal.AccessUnit
			var countsByPicture [][128]int
			var counts [128]int
			var lastInfo *nal.SliceInfo
			type partitionEntry struct {
				context DataPartitionContext
				info    *nal.SliceInfo
				seen    uint8
			}
			partitions := make(map[uint32]*partitionEntry)
			sliceCount := 0
			afterEnd := false
			for _, u := range ce.units {
				var info *nal.SliceInfo
				var partition *DataPartitionA
				hint := nal.BoundaryUnknown
				if u.Header.IsVCL() {
					rbsp, err := bitstream.EBSPToRBSP(u.Payload)
					if err != nil {
						t.Fatalf("slice EBSP→RBSP 失败: %v", err)
					}
					in := DataPartitionInput{RBSP: rbsp, BitLength: uint64(len(rbsp)) * 8, Header: u.Header}
					var h *SliceHeader
					switch u.Header.Type {
					case nal.TypeSlice, nal.TypeSliceIDR:
						h, err = ParseSliceHeaderEx(rbsp, ce.sps, ppsByID, u.Header.IsIDR(), u.Header.RefIDC)
					case nal.TypeSlicePartitionA:
						partition, err = ParseDataPartitionA(in, ce.sps, ppsByID)
						if err == nil {
							h = partition.SliceHeader
						}
					case nal.TypeSlicePartitionB, nal.TypeSlicePartitionC:
						id, err := bitstream.NewReader(rbsp).ReadUE()
						if err != nil {
							t.Fatalf("分区 B/C slice_id 解析失败: %v", err)
						}
						entry := partitions[id]
						if entry == nil {
							t.Fatalf("分区 B/C slice_id=%d 缺少同图像的分区 A", id)
						}
						if _, err := ParseDataPartitionBC(in, entry.context); err != nil {
							t.Fatalf("分区 B/C 前缀解析失败: %v", err)
						}
						flag := uint8(1)
						if u.Header.Type == nal.TypeSlicePartitionC {
							flag = 2
						}
						if entry.seen&flag != 0 || (flag == 1 && entry.seen&2 != 0) {
							t.Fatalf("slice_id=%d 存在重复或逆序的分区 B/C", id)
						}
						entry.seen |= flag
						info = entry.info
					default:
						t.Fatalf("未解析的 VCL NAL 类型: %v", u.Header.Type)
					}
					if err != nil {
						t.Fatalf("slice header 解析失败: %v", err)
					}
					// B/C 只有分区前缀，不是额外的 slice，也不能开启新图像。
					hint = nal.BoundarySame
					if h != nil {
						sliceCount++
						info = &nal.SliceInfo{
							FirstMBInSlice:            h.FirstMBInSlice,
							FrameNum:                  h.FrameNum,
							PicParameterSetID:         h.PicParameterSetID,
							FieldPicFlag:              h.FieldPicFlag,
							BottomFieldFlag:           h.BottomFieldFlag,
							IDR:                       h.IDR,
							IDRPicID:                  h.IDRPicID,
							HasPicOrderCnt:            h.HasPicOrderCntLsb,
							PicOrderCntLsb:            h.PicOrderCntLsb,
							HasDeltaPicOrderCntBottom: h.HasDeltaPicOrderCntBottom,
							DeltaPicOrderCntBottom:    h.DeltaPicOrderCntBottom,
							HasDeltaPicOrderCnt:       h.HasDeltaPicOrderCnt,
							DeltaPicOrderCnt:          h.DeltaPicOrderCnt,
							RedundantPicCnt:           h.RedundantPicCnt,
							NALRefIDC:                 u.Header.RefIDC,
						}
						// ASO/FMO 的首址 0 和 redundant_pic_cnt 的变化都不是图像边界。
						if lastInfo != nil {
							hint = nal.DetectPictureBoundary(lastInfo, info)
						}
					}
				} else if lastInfo != nil {
					// 保留跨 filler 的图像标识；其他前缀 NAL 属于下一个 AU。
					switch u.Header.Type {
					case nal.TypeEndOfSequence, nal.TypeEndOfStream, nal.TypeFiller:
					default:
						hint = nal.BoundaryNew
					}
				}
				if afterEnd {
					hint = nal.BoundaryNew
				}
				if au := asm.PushHinted(u, info, hint); au != nil {
					aus = append(aus, au)
					if au.HasVCL() {
						countsByPicture = append(countsByPicture, counts)
						counts = [128]int{}
					}
					lastInfo = nil
					clear(partitions)
				}
				if partition != nil {
					id := partition.Context.SliceID
					if previous := partitions[id]; previous != nil && previous.context.RedundantPicCnt == partition.Context.RedundantPicCnt {
						t.Fatalf("同图像存在重复的分区 A slice_id=%d", id)
					}
					partitions[id] = &partitionEntry{context: partition.Context, info: info}
				}
				if info != nil {
					lastInfo = info
				}
				if info != nil && u.Header.Type != nal.TypeSlicePartitionB && u.Header.Type != nal.TypeSlicePartitionC {
					counts[info.RedundantPicCnt]++
				}
				afterEnd = u.Header.Type == nal.TypeEndOfSequence || u.Header.Type == nal.TypeEndOfStream
				if afterEnd {
					lastInfo = nil
					clear(partitions)
				}
			}
			if au := asm.Flush(); au != nil {
				aus = append(aus, au)
				if au.HasVCL() {
					countsByPicture = append(countsByPicture, counts)
				}
			}

			if sliceCount == 0 {
				t.Fatal("语料没有 VCL NAL")
			}
			frameCount, assembledSlices := 0, 0
			for i, au := range aus {
				if !au.HasVCL() {
					continue
				}
				frameCount++
				if frameCount == 1 && !au.IsIDR() {
					t.Fatal("第一个图像 AU 应为 IDR")
				}
				slices := 0
				for count, n := range countsByPicture[frameCount-1] {
					if n > ce.maxSlicesPerFrame {
						t.Fatalf("AU %d 表示cnt%d有%d个slice，最大%d", i, count, n, ce.maxSlicesPerFrame)
					}
					slices += n
				}
				if slices < 1 {
					t.Fatalf("AU %d 缺少slice header", i)
				}
				assembledSlices += slices
			}
			if assembledSlices != sliceCount {
				t.Fatalf("组装后有 %d 个 slice header，解析得到 %d 个", assembledSlices, sliceCount)
			}
			if ce.frameCount != 0 && frameCount != ce.frameCount {
				t.Fatalf("组装出 %d 个图像 AU，期望 %d", frameCount, ce.frameCount)
			}
		})
	}
}
