//go:build goavc_simdtest

package prediction_test

import (
	"slices"
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/internal/picture"
	"github.com/cry5tallize/goavc/internal/prediction"
	"github.com/cry5tallize/goavc/internal/simd"
)

// 真实块入口包含逻辑边界patch及FieldView适配；不能用单采样宽1的标量回退
// 代替4/8/16宽SIMD消费者验证。切换期间无worker且本测试不并行。
func TestHighDepthBackend_FrameFieldAndBorder(t *testing.T) {
	if simd.ActiveBackends().MCLumaFractional16 != simd.SSE2 {
		t.Skip("当前平台未启用高位深SSE2")
	}
	for _, format := range []h264.PixelFormat{h264.YUV420P10LE, h264.YUV422P10LE, h264.YUV444P10LE} {
		p, err := picture.New(3, 4, format)
		if err != nil {
			t.Fatal(err)
		}
		for c := range p.Planes {
			pl := &p.Planes[c]
			for y := range pl.Height {
				for x := range pl.Width {
					pl.SetSample(x, y, uint16((x*71+y*137+c*331)%1024))
				}
			}
		}
		// 不填充物理边缘，证明领域层按逻辑边界准备halo而不是依赖池padding。
		for _, parity := range []int{-1, 0, 1} {
			view := *p
			if parity >= 0 {
				view, err = picture.FieldView(p, parity == 1)
				if err != nil {
					t.Fatal(err)
				}
			}
			for _, w := range []int{4, 8, 16} {
				for _, pos := range [][2]int{{-2, -1}, {8, 7}, {45, 29}} {
					for fy := range 4 {
						for fx := range 4 {
							var got, want [3][23 * 7]uint16
							run := func(dst *[3][23 * 7]uint16) {
								for c := range dst {
									for i := range dst[c] {
										dst[c][i] = 777
									}
									if c == 0 {
										prediction.MCBlockLumaHigh(dst[c][1:], 23, &view.Planes[c], pos[0], pos[1], w, 7, fx-4, fy-4)
									} else {
										subW, subH := format.ChromaSubsampling()
										prediction.MCBlockChromaFormatHigh(dst[c][1:], 23, &view.Planes[c], pos[0], pos[1], w, 7, fx-4, fy-4, subW, subH)
									}
								}
							}
							restore, err := simd.ForceBackendForTesting(simd.Generic)
							if err != nil {
								t.Fatal(err)
							}
							run(&want)
							restore()
							run(&got)
							for c := range got {
								if !slices.Equal(got[c][:], want[c][:]) {
									t.Fatalf("%s parity%d w%d pos%v mv%d,%d plane%d不符", format, parity, w, pos, fx-4, fy-4, c)
								}
							}
						}
					}
				}
			}
		}
	}
}
