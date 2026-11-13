package syntax

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/internal/testutil/manifest"
	"github.com/goavc/h264/nal"
)

// corpusEntry 是某个 manifest 条目解析出的参数集与 NAL 序列。
type corpusEntry struct {
	name  string
	units []nal.Unit
	sps   *SPS
	pps   *PPS
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
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(e.Path)))
		if err != nil {
			t.Fatalf("%s: 读取失败: %v", e.Name, err)
		}
		units, err := nal.SplitAnnexB(data)
		if err != nil {
			t.Fatalf("%s: 切分失败: %v", e.Name, err)
		}
		ce := corpusEntry{name: e.Name, units: units}
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
			sliceCount := 0
			for _, u := range ce.units {
				var info *nal.SliceInfo
				if u.Header.IsVCL() {
					rbsp, err := bitstream.EBSPToRBSP(u.Payload)
					if err != nil {
						t.Fatalf("slice EBSP→RBSP 失败: %v", err)
					}
					h, err := ParseSliceHeaderEx(rbsp, ce.sps, ppsByID, u.Header.IsIDR(), u.Header.RefIDC)
					if err != nil {
						t.Fatalf("slice header 解析失败: %v", err)
					}
					sliceCount++
					info = &nal.SliceInfo{
						FirstMBInSlice:    h.FirstMBInSlice,
						FrameNum:          h.FrameNum,
						PicParameterSetID: h.PicParameterSetID,
						FieldPicFlag:      h.FieldPicFlag,
						BottomFieldFlag:   h.BottomFieldFlag,
						IDR:               h.IDR,
						IDRPicID:          h.IDRPicID,
						HasPicOrderCnt:    h.HasPicOrderCntLsb,
						PicOrderCntLsb:    h.PicOrderCntLsb,
						RedundantPicCnt:   h.RedundantPicCnt,
						NALRefIDC:         u.Header.RefIDC,
					}
				}
				if au := asm.Push(u, info); au != nil {
					aus = append(aus, au)
				}
			}
			if au := asm.Flush(); au != nil {
				aus = append(aus, au)
			}

			if sliceCount == 0 {
				t.Fatal("语料没有 VCL NAL")
			}
			if len(aus) != 3 {
				t.Fatalf("组装出 %d 个 AU，期望 3", len(aus))
			}
			for i, au := range aus {
				if au.VCLCount() != 1 {
					t.Fatalf("AU %d 有 %d 个 VCL，期望 1", i, au.VCLCount())
				}
			}
			if !aus[0].IsIDR() {
				t.Fatal("第一个 AU 应为 IDR")
			}
			for i := 1; i < len(aus); i++ {
				if aus[i].IsIDR() {
					t.Fatalf("AU %d 不应为 IDR", i)
				}
			}
		})
	}
}
