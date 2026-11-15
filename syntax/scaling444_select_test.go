package syntax

import (
	"errors"
	"fmt"
	"testing"

	h264 "github.com/cry5tallize/goavc"
)

// Spec: 6.4 的帧扫描列表，输入按规范手工列出，期望是逐行递增的光栅。
// 不引用实现中的 Zigzag 表，避免选择器与测试共享同一扫描错误。
var scaling444Scan4 = []int32{
	1, 2, 5, 9, 6, 3, 4, 7, 10, 13, 14, 11, 8, 12, 15, 16,
}
var scaling444Scan8 = []int32{
	1, 2, 9, 17, 10, 3, 4, 11,
	18, 25, 33, 26, 19, 12, 5, 6,
	13, 20, 27, 34, 41, 49, 42, 35,
	28, 21, 14, 7, 8, 15, 22, 29,
	36, 43, 50, 57, 58, 51, 44, 37,
	30, 23, 16, 24, 31, 38, 45, 52,
	59, 60, 53, 46, 39, 32, 40, 47,
	54, 61, 62, 55, 48, 56, 63, 64,
}
var scaling444Raster4 = [16]uint8{
	1, 2, 3, 4,
	5, 6, 7, 8,
	9, 10, 11, 12,
	13, 14, 15, 16,
}
var scaling444Raster8 = [64]uint8{
	1, 2, 3, 4, 5, 6, 7, 8,
	9, 10, 11, 12, 13, 14, 15, 16,
	17, 18, 19, 20, 21, 22, 23, 24,
	25, 26, 27, 28, 29, 30, 31, 32,
	33, 34, 35, 36, 37, 38, 39, 40,
	41, 42, 43, 44, 45, 46, 47, 48,
	49, 50, 51, 52, 53, 54, 55, 56,
	57, 58, 59, 60, 61, 62, 63, 64,
}

// Spec: 表 7-3/7-4 的缺省矩阵，独立 literal，不读取被测实现的默认表。
var scaling444Default4 = [2][16]uint8{
	{
		6, 13, 20, 28,
		13, 20, 28, 32,
		20, 28, 32, 37,
		28, 32, 37, 42,
	},
	{
		10, 14, 20, 24,
		14, 20, 24, 27,
		20, 24, 27, 30,
		24, 27, 30, 34,
	},
}
var scaling444Default8 = [2][64]uint8{
	{
		6, 10, 13, 16, 18, 23, 25, 27,
		10, 11, 16, 18, 23, 25, 27, 29,
		13, 16, 18, 23, 25, 27, 29, 31,
		16, 18, 23, 25, 27, 29, 31, 33,
		18, 23, 25, 27, 29, 31, 33, 36,
		23, 25, 27, 29, 31, 33, 36, 38,
		25, 27, 29, 31, 33, 36, 38, 40,
		27, 29, 31, 33, 36, 38, 40, 42,
	},
	{
		9, 13, 15, 17, 19, 21, 22, 24,
		13, 13, 17, 19, 21, 22, 24, 25,
		15, 17, 19, 21, 22, 24, 25, 27,
		17, 19, 21, 22, 24, 25, 27, 28,
		19, 21, 22, 24, 25, 27, 28, 30,
		21, 22, 24, 25, 27, 28, 30, 32,
		22, 24, 25, 27, 28, 30, 32, 33,
		24, 25, 27, 28, 30, 32, 33, 35,
	},
}

func scaling444List(size int, offset uint8) *ScalingList {
	scan := scaling444Scan4
	if size == 8 {
		scan = scaling444Scan8
	}
	list := &ScalingList{Size: size, Values: make([]int32, len(scan))}
	for i, v := range scan {
		list.Values[i] = v + int32(offset)
	}
	return list
}

func scaling444Want4(offset uint8) [16]uint8 {
	want := scaling444Raster4
	for i := range want {
		want[i] += offset
	}
	return want
}

func scaling444Want8(offset uint8) [64]uint8 {
	want := scaling444Raster8
	for i := range want {
		want[i] += offset
	}
	return want
}

func TestScaling444_AllListsHaveIndependentRasterOrder(t *testing.T) {
	for _, usePPS := range []bool{false, true} {
		t.Run(fmt.Sprintf("pps=%v", usePPS), func(t *testing.T) {
			s := &SPS{ProfileIDC: 244, ChromaFormatIDC: 3, SeqScalingMatrixPresentFlag: true}
			p := &PPS{PicScalingMatrixPresentFlag: usePPS, Transform8x8ModeFlag: true}
			var want ScalingMatrices
			for i := range 6 {
				offset4, offset8 := uint8(i*20), uint8(i*24)
				s.SeqScalingListPresentFlag[i], s.SeqScalingListPresentFlag[6+i] = true, true
				s.SeqScalingList[i], s.SeqScalingList[6+i] = scaling444List(4, offset4), scaling444List(8, offset8)
				if usePPS {
					offset4 += 8
					offset8 += 8
					p.PicScalingListPresentFlag[i], p.PicScalingListPresentFlag[6+i] = true, true
					p.PicScalingList[i], p.PicScalingList[6+i] = scaling444List(4, offset4), scaling444List(8, offset8)
				}
				want.Four[i], want.Eight[i] = scaling444Want4(offset4), scaling444Want8(offset8)
			}
			got, err := SelectScalingMatrices(s, p)
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("独立 Y/Cb/Cr 列表或扫描顺序错误\ngot=%v\nwant=%v", got, want)
			}
		})
	}
}

func TestScaling444_MatrixPresenceCombinations(t *testing.T) {
	for _, seqPresent := range []bool{false, true} {
		for _, picPresent := range []bool{false, true} {
			for _, transform8 := range []bool{false, true} {
				for _, separate := range []bool{false, true} {
					name := fmt.Sprintf("seq=%v/pic=%v/8x8=%v/separate=%v", seqPresent, picPresent, transform8, separate)
					t.Run(name, func(t *testing.T) {
						s := &SPS{ProfileIDC: 244, ChromaFormatIDC: 3, SeparateColourPlaneFlag: separate, SeqScalingMatrixPresentFlag: seqPresent}
						p := &PPS{PicScalingMatrixPresentFlag: picPresent, Transform8x8ModeFlag: transform8}
						var want ScalingMatrices
						for i := range 6 {
							for j := range want.Four[i] {
								want.Four[i][j] = 16
							}
							for j := range want.Eight[i] {
								want.Eight[i][j] = 16
							}
							if seqPresent || picPresent {
								want.Four[i] = scaling444Default4[i/3]
							}
							if seqPresent || (picPresent && transform8) {
								want.Eight[i] = scaling444Default8[i%2]
							}
						}
						got, err := SelectScalingMatrices(s, p)
						if err != nil {
							t.Fatal(err)
						}
						if got != want {
							t.Fatalf("顶层矩阵缺省错误\ngot=%v\nwant=%v", got, want)
						}
					})
				}
			}
		}
	}
}

func TestScaling444_SPSFallbackAUsesPreviousPlane(t *testing.T) {
	s := &SPS{ProfileIDC: 244, ChromaFormatIDC: 3, SeqScalingMatrixPresentFlag: true}
	for _, i := range []int{0, 2, 4} {
		s.SeqScalingListPresentFlag[i] = true
		s.SeqScalingList[i] = scaling444List(4, uint8(i*20))
	}
	for _, i := range []int{0, 3} {
		s.SeqScalingListPresentFlag[6+i] = true
		s.SeqScalingList[6+i] = scaling444List(8, uint8(i*24))
	}
	want := ScalingMatrices{
		Four: [6][16]uint8{
			scaling444Want4(0), scaling444Want4(0), scaling444Want4(40),
			scaling444Default4[1], scaling444Want4(80), scaling444Want4(80),
		},
		Eight: [6][64]uint8{
			scaling444Want8(0), scaling444Default8[1],
			scaling444Want8(0), scaling444Want8(72),
			scaling444Want8(0), scaling444Want8(72),
		},
	}
	got, err := SelectScalingMatrices(s, &PPS{})
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("fallback A 未按 4x4 的 i-1 / 8x8 的 i-2 继承\ngot=%v\nwant=%v", got, want)
	}
}

func TestScaling444_PPSFallbackBUsesSPSAnchorsAndPPSChroma(t *testing.T) {
	for _, seqPresent := range []bool{false, true} {
		t.Run(fmt.Sprintf("seq=%v", seqPresent), func(t *testing.T) {
			s := &SPS{ProfileIDC: 244, ChromaFormatIDC: 3, SeqScalingMatrixPresentFlag: seqPresent}
			for i := range 6 {
				s.SeqScalingListPresentFlag[i], s.SeqScalingListPresentFlag[6+i] = true, true
				s.SeqScalingList[i] = scaling444List(4, uint8(i*20))
				s.SeqScalingList[6+i] = scaling444List(8, uint8(i*24))
			}
			p := &PPS{PicScalingMatrixPresentFlag: true, Transform8x8ModeFlag: true}
			p.PicScalingListPresentFlag[1], p.PicScalingListPresentFlag[4] = true, true
			p.PicScalingList[1], p.PicScalingList[4] = scaling444List(4, 150), scaling444List(4, 170)
			p.PicScalingListPresentFlag[7], p.PicScalingListPresentFlag[8] = true, true
			p.PicScalingList[7], p.PicScalingList[8] = scaling444List(8, 150), scaling444List(8, 170)
			want := ScalingMatrices{
				Four: [6][16]uint8{
					scaling444Default4[0], scaling444Want4(150), scaling444Want4(150),
					scaling444Default4[1], scaling444Want4(170), scaling444Want4(170),
				},
				Eight: [6][64]uint8{
					scaling444Default8[0], scaling444Want8(150),
					scaling444Want8(170), scaling444Want8(150),
					scaling444Want8(170), scaling444Want8(150),
				},
			}
			if seqPresent {
				want.Four[0], want.Four[3] = scaling444Want4(0), scaling444Want4(60)
				want.Eight[0] = scaling444Want8(0)
			}
			got, err := SelectScalingMatrices(s, p)
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("fallback B 未区分 SPS 锚点与 PPS 继承\ngot=%v\nwant=%v", got, want)
			}
		})
	}
}

func TestScaling444_UseDefaultOverridesEachExplicitList(t *testing.T) {
	for _, usePPS := range []bool{false, true} {
		for index := range 12 {
			t.Run(fmt.Sprintf("pps=%v/list=%d", usePPS, index), func(t *testing.T) {
				s := &SPS{ProfileIDC: 244, ChromaFormatIDC: 3, SeqScalingMatrixPresentFlag: true}
				p := &PPS{PicScalingMatrixPresentFlag: usePPS, Transform8x8ModeFlag: true}
				for i := range 12 {
					size := 4
					if i >= 6 {
						size = 8
					}
					s.SeqScalingListPresentFlag[i] = true
					s.SeqScalingList[i] = scaling444List(size, 90)
					if usePPS {
						p.PicScalingListPresentFlag[i] = true
						p.PicScalingList[i] = scaling444List(size, 120)
					}
				}
				size := 4
				if index >= 6 {
					size = 8
				}
				list := &ScalingList{Size: size, Deltas: []int32{-8}, Values: make([]int32, size*size)}
				// 解析器在首 delta=-8 后保留 8；不能把它当作平坦有效矩阵。
				for i := range list.Values {
					list.Values[i] = 8
				}
				if usePPS {
					p.PicScalingList[index] = list
				} else {
					s.SeqScalingList[index] = list
				}
				got, err := SelectScalingMatrices(s, p)
				if err != nil {
					t.Fatal(err)
				}
				offset := uint8(90)
				if usePPS {
					offset = 120
				}
				var want ScalingMatrices
				for i := range 6 {
					want.Four[i], want.Eight[i] = scaling444Want4(offset), scaling444Want8(offset)
				}
				if index < 6 {
					want.Four[index] = scaling444Default4[index/3]
				} else {
					want.Eight[index-6] = scaling444Default8[(index-6)%2]
				}
				if got != want {
					t.Fatalf("useDefault 选择或相邻列表受污染\ngot=%v\nwant=%v", got, want)
				}
			})
		}
	}
}

func TestScaling444_RejectsInvalidExplicitLists(t *testing.T) {
	cases := []struct {
		name string
		edit func(*ScalingList) *ScalingList
	}{
		{"missing", func(*ScalingList) *ScalingList { return nil }},
		{"zero-size", func(l *ScalingList) *ScalingList { l.Size = 0; return l }},
		{"other-size", func(l *ScalingList) *ScalingList { l.Size = 7; return l }},
		{"wrong-size", func(l *ScalingList) *ScalingList { l.Size = 12 - l.Size; return l }},
		{"short", func(l *ScalingList) *ScalingList { l.Values = l.Values[:len(l.Values)-1]; return l }},
		{"long", func(l *ScalingList) *ScalingList { l.Values = append(l.Values, 1); return l }},
		{"zero", func(l *ScalingList) *ScalingList { l.Values[len(l.Values)-1] = 0; return l }},
		{"negative", func(l *ScalingList) *ScalingList { l.Values[0] = -1; return l }},
		{"overflow", func(l *ScalingList) *ScalingList { l.Values[len(l.Values)/2] = 256; return l }},
		{"default-wrong-size", func(l *ScalingList) *ScalingList { l.Size = 0; l.Deltas = []int32{-8}; return l }},
		{"default-missing-values", func(l *ScalingList) *ScalingList { l.Values = nil; l.Deltas = []int32{-8}; return l }},
	}
	for _, usePPS := range []bool{false, true} {
		for index := range 12 {
			for _, tc := range cases {
				t.Run(fmt.Sprintf("pps=%v/list=%d/%s", usePPS, index, tc.name), func(t *testing.T) {
					s := &SPS{ProfileIDC: 244, ChromaFormatIDC: 3, SeqScalingMatrixPresentFlag: !usePPS}
					p := &PPS{PicScalingMatrixPresentFlag: usePPS, Transform8x8ModeFlag: true}
					size := 4
					if index >= 6 {
						size = 8
					}
					list := tc.edit(scaling444List(size, 0))
					if usePPS {
						p.PicScalingListPresentFlag[index], p.PicScalingList[index] = true, list
					} else {
						s.SeqScalingListPresentFlag[index], s.SeqScalingList[index] = true, list
					}
					if _, err := SelectScalingMatrices(s, p); !errors.Is(err, h264.ErrInvalidData) {
						t.Fatalf("非法列表应返回 ErrInvalidData，got %v", err)
					}
				})
			}
		}
	}
	for _, pair := range []struct {
		s *SPS
		p *PPS
	}{{nil, &PPS{}}, {&SPS{}, nil}, {nil, nil}} {
		if _, err := SelectScalingMatrices(pair.s, pair.p); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("缺少参数集应返回 ErrInvalidData，got %v", err)
		}
	}
}

func TestScaling444_PPSWithoutTransform8x8OnlyOverridesSixLists(t *testing.T) {
	for _, seqPresent := range []bool{false, true} {
		s := &SPS{ProfileIDC: 244, ChromaFormatIDC: 3, SeqScalingMatrixPresentFlag: seqPresent}
		p := &PPS{PicScalingMatrixPresentFlag: true}
		for i := range 6 {
			s.SeqScalingListPresentFlag[6+i] = true
			s.SeqScalingList[6+i] = scaling444List(8, uint8(i*24))
			// 不在当前 PPS 语法中的字段即使残留非法值也不能被读取。
			p.PicScalingListPresentFlag[6+i] = true
		}
		got, err := SelectScalingMatrices(s, p)
		if err != nil {
			t.Fatal(err)
		}
		for i := range 6 {
			want := scaling444Want8(uint8(i * 24))
			if !seqPresent {
				for j := range want {
					want[j] = 16
				}
			}
			if got.Eight[i] != want {
				t.Fatalf("seq=%v，未启用 8x8 的 PPS 修改了列表 %d", seqPresent, i)
			}
		}
	}
}

func TestScaling444_Non444RetainsTwoListsAndIgnoresUnusedEntries(t *testing.T) {
	for _, chroma := range []uint32{0, 1, 2} {
		for _, usePPS := range []bool{false, true} {
			t.Run(fmt.Sprintf("chroma=%d/pps=%v", chroma, usePPS), func(t *testing.T) {
				s := &SPS{ProfileIDC: 122, ChromaFormatIDC: chroma, SeqScalingMatrixPresentFlag: true}
				p := &PPS{PicScalingMatrixPresentFlag: usePPS, Transform8x8ModeFlag: true}
				for i := range 2 {
					s.SeqScalingListPresentFlag[6+i] = true
					s.SeqScalingList[6+i] = scaling444List(8, uint8(i*24))
					p.PicScalingListPresentFlag[6+i] = true
					p.PicScalingList[6+i] = scaling444List(8, uint8(80+i*24))
				}
				for i := 8; i < 12; i++ {
					s.SeqScalingListPresentFlag[i], p.PicScalingListPresentFlag[i] = true, true
				}
				got, err := SelectScalingMatrices(s, p)
				if err != nil {
					t.Fatal(err)
				}
				for i := range 2 {
					offset := uint8(i * 24)
					if usePPS {
						offset += 80
					}
					if got.Eight[i] != scaling444Want8(offset) {
						t.Fatalf("旧格式有效 8x8 列表 %d 改变", i)
					}
				}
				for i := 2; i < 6; i++ {
					if got.Eight[i] != [64]uint8{} {
						t.Fatalf("旧格式不应初始化额外 8x8 列表 %d", i)
					}
				}
			})
		}
	}
}
