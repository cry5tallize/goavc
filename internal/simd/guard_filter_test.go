package simd

import (
	"fmt"
	"testing"
)

func TestGuardPage_Filter(t *testing.T) {
	if !guardChild(t) {
		return
	}
	saved := kernels
	defer func() { kernels = saved }()
	for _, backend := range guardBackends() {
		t.Run(backend.name, func(t *testing.T) {
			for _, lines := range []int{1, 2, 4} {
				for _, vertical := range []bool{false, true} {
					for _, strong := range []bool{false, true} {
						for _, chroma := range []bool{false, true} {
							for _, element := range []int{1, 2} {
								t.Run(fmt.Sprintf("span%d/vertical%t/strong%t/chroma%t/u%d", lines, vertical, strong, chroma, element*8), func(t *testing.T) {
									w, h := lines, 8
									if vertical {
										w, h = 8, lines
									}
									raw, stride := guardedRows(t, w, h, element)
									q0, lineStep, acrossStep := 4*stride, 1, stride
									plainQ0, plainLine, plainAcross := 4*w, 1, w
									if vertical {
										q0, lineStep, acrossStep = 4, stride, 1
										plainQ0, plainLine, plainAcross = 4, w, 1
									}
									bs := 2
									if strong {
										bs = 4
									}
									depth := 8
									scale := 1
									if element == 2 {
										depth, scale = 10, 4
									}
									kernels = genericTable()
									if element == 1 {
										want := make([]byte, w*h)
										for y := range h {
											for x := range w {
												n := y
												if vertical {
													n = x
												}
												v := byte((100 + n*2) * scale)
												raw[y*stride+x], want[y*w+x] = v, v
											}
										}
										if chroma {
											FilterChroma8(want, plainQ0, plainLine, plainAcross, bs, 40*scale, 20*scale, 2*scale, lines, depth)
										} else {
											FilterLuma8(want, plainQ0, plainLine, plainAcross, bs, 40*scale, 20*scale, 2*scale, lines, depth)
										}
										kernels = backend.table
										if chroma {
											FilterChroma8(raw, q0, lineStep, acrossStep, bs, 40*scale, 20*scale, 2*scale, lines, depth)
										} else {
											FilterLuma8(raw, q0, lineStep, acrossStep, bs, 40*scale, 20*scale, 2*scale, lines, depth)
										}
										for y := range h {
											for x := range w {
												if raw[y*stride+x] != want[y*w+x] {
													t.Fatalf("滤波u8 (%d,%d)=%d，期望%d", x, y, raw[y*stride+x], want[y*w+x])
												}
											}
										}
									} else {
										data := guardUint16(raw)
										want := make([]uint16, w*h)
										for y := range h {
											for x := range w {
												n := y
												if vertical {
													n = x
												}
												v := uint16((100+n*2)*scale + 3)
												data[y*stride+x], want[y*w+x] = v, v
											}
										}
										if chroma {
											FilterChroma16(want, plainQ0, plainLine, plainAcross, bs, 40*scale, 20*scale, 2*scale, lines, depth)
										} else {
											FilterLuma16(want, plainQ0, plainLine, plainAcross, bs, 40*scale, 20*scale, 2*scale, lines, depth)
										}
										kernels = backend.table
										if chroma {
											FilterChroma16(data, q0, lineStep, acrossStep, bs, 40*scale, 20*scale, 2*scale, lines, depth)
										} else {
											FilterLuma16(data, q0, lineStep, acrossStep, bs, 40*scale, 20*scale, 2*scale, lines, depth)
										}
										for y := range h {
											for x := range w {
												if data[y*stride+x] != want[y*w+x] {
													t.Fatalf("滤波u16 (%d,%d)=%d，期望%d", x, y, data[y*stride+x], want[y*w+x])
												}
											}
										}
									}
								})
							}
						}
					}
				}
			}
		})
	}
}
