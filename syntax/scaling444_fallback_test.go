package syntax

import (
	"fmt"
	"testing"
)

func TestScaling444_PPSMissingListsInheritOnlySPSAnchors(t *testing.T) {
	s := &SPS{ProfileIDC: 244, ChromaFormatIDC: 3, SeqScalingMatrixPresentFlag: true}
	for i := range 6 {
		s.SeqScalingListPresentFlag[i], s.SeqScalingListPresentFlag[6+i] = true, true
		s.SeqScalingList[i] = scaling444List(4, uint8(i*20))
		s.SeqScalingList[6+i] = scaling444List(8, uint8(i*24))
	}
	p := &PPS{PicScalingMatrixPresentFlag: true, Transform8x8ModeFlag: true}
	want := ScalingMatrices{
		Four: [6][16]uint8{
			scaling444Want4(0), scaling444Want4(0), scaling444Want4(0),
			scaling444Want4(60), scaling444Want4(60), scaling444Want4(60),
		},
		Eight: [6][64]uint8{
			scaling444Want8(0), scaling444Want8(24),
			scaling444Want8(0), scaling444Want8(24),
			scaling444Want8(0), scaling444Want8(24),
		},
	}
	got, err := SelectScalingMatrices(s, p)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("PPS 缺省不应逐项保留 SPS 色度矩阵\ngot=%v\nwant=%v", got, want)
	}
}

func TestScaling444_SPSFallbackAKeepsIntraAndInterIndependent(t *testing.T) {
	s := &SPS{ProfileIDC: 244, ChromaFormatIDC: 3, SeqScalingMatrixPresentFlag: true}
	s.SeqScalingListPresentFlag[7] = true
	s.SeqScalingList[7] = scaling444List(8, 70)
	got, err := SelectScalingMatrices(s, &PPS{})
	if err != nil {
		t.Fatal(err)
	}
	want := [6][64]uint8{
		scaling444Default8[0], scaling444Want8(70),
		scaling444Default8[0], scaling444Want8(70),
		scaling444Default8[0], scaling444Want8(70),
	}
	if got.Eight != want {
		t.Fatalf("SPS 缺省的 intra/inter 链不能交叉\ngot=%v\nwant=%v", got.Eight, want)
	}
}

func TestScaling444_Non444DefaultCombinationsMatchLegacyMatrices(t *testing.T) {
	for _, chroma := range []uint32{1, 2} {
		for _, seqPresent := range []bool{false, true} {
			for _, picPresent := range []bool{false, true} {
				t.Run(fmt.Sprintf("chroma=%d/seq=%v/pic=%v", chroma, seqPresent, picPresent), func(t *testing.T) {
					s := &SPS{ProfileIDC: 122, ChromaFormatIDC: chroma, SeqScalingMatrixPresentFlag: seqPresent}
					p := &PPS{PicScalingMatrixPresentFlag: picPresent, Transform8x8ModeFlag: true}
					var want ScalingMatrices
					for i := range 6 {
						for j := range want.Four[i] {
							want.Four[i][j] = 16
						}
						if seqPresent || picPresent {
							want.Four[i] = scaling444Default4[i/3]
						}
					}
					for i := range 2 {
						for j := range want.Eight[i] {
							want.Eight[i][j] = 16
						}
						if seqPresent || picPresent {
							want.Eight[i] = scaling444Default8[i]
						}
					}
					got, err := SelectScalingMatrices(s, p)
					if err != nil {
						t.Fatal(err)
					}
					if got != want {
						t.Fatalf("旧 420/422 默认矩阵改变\ngot=%v\nwant=%v", got, want)
					}
				})
			}
		}
	}
}

func TestScaling444_ExplicitValuesAcceptBothEndpoints(t *testing.T) {
	for _, usePPS := range []bool{false, true} {
		s := &SPS{ProfileIDC: 244, ChromaFormatIDC: 3, SeqScalingMatrixPresentFlag: !usePPS}
		p := &PPS{PicScalingMatrixPresentFlag: usePPS, Transform8x8ModeFlag: true}
		for i := range 12 {
			size := 4
			if i >= 6 {
				size = 8
			}
			list := scaling444List(size, 0)
			list.Values[0], list.Values[len(list.Values)-1] = 1, 255
			if usePPS {
				p.PicScalingListPresentFlag[i], p.PicScalingList[i] = true, list
			} else {
				s.SeqScalingListPresentFlag[i], s.SeqScalingList[i] = true, list
			}
		}
		got, err := SelectScalingMatrices(s, p)
		if err != nil {
			t.Fatal(err)
		}
		for i := range 6 {
			want4, want8 := scaling444Want4(0), scaling444Want8(0)
			want4[15], want8[63] = 255, 255
			if got.Four[i] != want4 || got.Eight[i] != want8 {
				t.Fatalf("pps=%v，列表 %d 的合法边界值未保留", usePPS, i)
			}
		}
	}
}

func TestScaling444_AllTwelveListPresenceMasks(t *testing.T) {
	// H.264表7-2逐项抄录的依赖索引，-1表示默认矩阵或SPS锚点。
	// 不按被测实现的4×4/8×8循环推导父索引，穷举全部4096种缺省组合。
	parent := [12]int{-1, 0, 1, -1, 3, 4, -1, -1, 6, 7, 8, 9}
	for mode := range 3 {
		for mask := range 1 << 12 {
			s := &SPS{ProfileIDC: 244, ChromaFormatIDC: 3, SeqScalingMatrixPresentFlag: mode != 1}
			p := &PPS{PicScalingMatrixPresentFlag: mode != 0, Transform8x8ModeFlag: true}
			var want ScalingMatrices
			for index, previous := range parent {
				size := 4
				if index >= 6 {
					size = 8
				}
				present := mask&(1<<index) != 0
				list := scaling444List(size, uint8(5*index))
				if mode == 0 {
					s.SeqScalingListPresentFlag[index], s.SeqScalingList[index] = present, list
				} else {
					// SPS所有列表独立，PPS的非锚点缺省不得直接保留它们。
					s.SeqScalingListPresentFlag[index], s.SeqScalingList[index] = true, scaling444List(size, uint8(80+5*index))
					p.PicScalingListPresentFlag[index], p.PicScalingList[index] = present, list
				}
				if index < 6 {
					switch {
					case present:
						want.Four[index] = scaling444Want4(uint8(5 * index))
					case previous >= 0:
						want.Four[index] = want.Four[previous]
					case mode == 2:
						want.Four[index] = scaling444Want4(uint8(80 + 5*index))
					default:
						want.Four[index] = scaling444Default4[index/3]
					}
				} else {
					switch {
					case present:
						want.Eight[index-6] = scaling444Want8(uint8(5 * index))
					case previous >= 0:
						want.Eight[index-6] = want.Eight[previous-6]
					case mode == 2:
						want.Eight[index-6] = scaling444Want8(uint8(80 + 5*index))
					default:
						want.Eight[index-6] = scaling444Default8[index%2]
					}
				}
			}
			got, err := SelectScalingMatrices(s, p)
			if err != nil || got != want {
				t.Fatalf("mode=%d mask=%03x got=%v want=%v err=%v", mode, mask, got, want, err)
			}
		}
	}
}
