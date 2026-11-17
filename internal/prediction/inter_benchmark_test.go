package prediction

import (
	"fmt"
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/internal/picture"
	"github.com/cry5tallize/goavc/internal/simd"
)

var mcBenchmarkByteSink [256]byte
var mcBenchmarkHighSink [256]uint16

// 历史名称保留；实际为Auto分派，槽诊断见BenchmarkMCBlocks。
func BenchmarkMCLuma_16x16_Generic(b *testing.B) {
	for _, format := range []h264.PixelFormat{h264.YUV420P, h264.YUV420P10LE} {
		for _, scenario := range []string{"interior", "border", "field"} {
			b.Run(format.String()+"/"+scenario, func(b *testing.B) {
				pic, err := picture.New(4, 8, format)
				if err != nil {
					b.Fatal(err)
				}
				defer pic.Release()
				for y := range pic.Height {
					for x := range pic.Width {
						pic.Planes[0].SetSample(x, y, uint16((x*19+y*31)%((1<<format.BitDepth())-1)))
					}
				}
				ref := &pic.Planes[0]
				x, y := 20, 21
				if scenario == "border" {
					x, y = -2, -3
				}
				if scenario == "field" {
					field, err := picture.FieldView(pic, true)
					if err != nil {
						b.Fatal(err)
					}
					ref = &field.Planes[0]
				}
				var dst8 [256]byte
				var dst16 [256]uint16
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if format.BitDepth() == 8 {
						MCBlockLuma(dst8[:], 16, ref, x, y, 16, 16, 2, 2)
					} else {
						MCBlockLumaHigh(dst16[:], 16, ref, x, y, 16, 16, 2, 2)
					}
				}
				b.StopTimer()
				mcBenchmarkByteSink, mcBenchmarkHighSink = dst8, dst16
			})
		}
	}
}

// 名称记录真实槽；边界通过逻辑patch后仍可使用相同插值内核。
// 图片、边界位置和单采样参考均在计时外准备，不把边界patch伪称直接ISA。
func BenchmarkMCBlocks(b *testing.B) {
	formats := []h264.PixelFormat{h264.YUV420P, h264.YUV420P10LE, h264.YUV422P, h264.YUV422P10LE, h264.YUV444P, h264.YUV444P10LE}
	for _, format := range formats {
		for _, chroma := range []bool{false, true} {
			// 亮度与色度格式无关，不重复422/444的同一亮度基准。
			if !chroma && format != h264.YUV420P && format != h264.YUV420P10LE {
				continue
			}
			for _, scenario := range []struct {
				name     string
				mvx, mvy int
				border   bool
			}{
				{"IntegerInterior", 0, 0, false},
				{"IntegerBorder", 0, 0, true},
				{"HalfHorizontalInterior", 2, 0, false},
				{"HalfVerticalBorder", 0, 2, true},
				{"HalfXY-JLumaInterior", 2, 2, false},
				{"HalfXY-JLumaBorder", 2, 2, true},
				{"QuarterInterior", 1, 3, false},
				{"QuarterBorder", 3, 1, true},
			} {
				planeName, actual := "Luma", simd.Generic
				subW, subH := format.ChromaSubsampling()
				if chroma {
					planeName = "Chroma"
				}
				if format.BitDepth() == 8 && scenario.mvx == 0 && scenario.mvy == 0 && (!chroma || subW == 1 && subH == 1) {
					actual = simd.ActiveBackends().MCLumaCopy
				}
				if format.BitDepth() == 8 && (scenario.mvx != 0 || scenario.mvy != 0) {
					if !chroma || subW == 1 && subH == 1 {
						actual = simd.ActiveBackends().MCLumaFractional8
					} else {
						actual = simd.ActiveBackends().MCChroma8
					}
				}
				name := fmt.Sprintf("%s/%s/%s/Auto-%s", planeName, format, scenario.name, actual)
				b.Run(name, func(b *testing.B) {
					pic, err := picture.New(4, 4, format)
					if err != nil {
						b.Fatal(err)
					}
					defer pic.Release()
					plane := 0
					if chroma {
						plane = 1
					}
					ref := &pic.Planes[plane]
					limit := 1<<format.BitDepth() - 1
					for y := range ref.Height {
						for x := range ref.Width {
							ref.SetSample(x, y, uint16((x*19+y*31+x*y*7)&limit))
						}
					}
					x, y := 7, 8
					if scenario.border {
						x, y = -2, -3
					}
					w, h := 16, 16
					if chroma {
						w, h = 16/subW, 16/subH
					}
					var dst8 [256]byte
					var dst16 [256]uint16
					if format.BitDepth() == 8 {
						if chroma {
							MCBlockChromaFormat(dst8[:], 16, ref, x, y, w, h, scenario.mvx, scenario.mvy, subW, subH)
						} else {
							MCBlockLuma(dst8[:], 16, ref, x, y, w, h, scenario.mvx, scenario.mvy)
						}
					} else if chroma {
						MCBlockChromaFormatHigh(dst16[:], 16, ref, x, y, w, h, scenario.mvx, scenario.mvy, subW, subH)
					} else {
						MCBlockLumaHigh(dst16[:], 16, ref, x, y, w, h, scenario.mvx, scenario.mvy)
					}
					for row := range h {
						for col := range w {
							var got, want uint16
							if format.BitDepth() == 8 {
								got = uint16(dst8[row*16+col])
								if chroma {
									want = uint16(ChromaSampleFormat(ref, x+col, y+row, scenario.mvx, scenario.mvy, subW, subH))
								} else {
									want = uint16(LumaSample(ref, x+col, y+row, scenario.mvx, scenario.mvy))
								}
							} else {
								got = dst16[row*16+col]
								if chroma {
									want = ChromaSampleFormatHigh(ref, x+col, y+row, scenario.mvx, scenario.mvy, subW, subH)
								} else {
									want = LumaSampleHigh(ref, x+col, y+row, scenario.mvx, scenario.mvy)
								}
							}
							if got != want {
								b.Fatalf("MC(%d,%d)=%d，期望%d", col, row, got, want)
							}
						}
					}
					b.ReportAllocs()
					if format.BitDepth() == 8 {
						if chroma {
							for b.Loop() {
								MCBlockChromaFormat(mcBenchmarkByteSink[:], 16, ref, x, y, w, h, scenario.mvx, scenario.mvy, subW, subH)
							}
						} else {
							for b.Loop() {
								MCBlockLuma(mcBenchmarkByteSink[:], 16, ref, x, y, w, h, scenario.mvx, scenario.mvy)
							}
						}
					} else if chroma {
						for b.Loop() {
							MCBlockChromaFormatHigh(mcBenchmarkHighSink[:], 16, ref, x, y, w, h, scenario.mvx, scenario.mvy, subW, subH)
						}
					} else {
						for b.Loop() {
							MCBlockLumaHigh(mcBenchmarkHighSink[:], 16, ref, x, y, w, h, scenario.mvx, scenario.mvy)
						}
					}
				})
			}
		}
	}
}
