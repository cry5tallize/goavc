package simd

import (
	"fmt"
	"math"
	"testing"
)

func TestGuardPage_Residual(t *testing.T) {
	if !guardChild(t) {
		return
	}
	previous := kernels
	defer func() { kernels = previous }()
	for _, backend := range guardBackends() {
		kernels = backend.table
		t.Run(backend.name, func(t *testing.T) {
			for _, w := range []int{1, 3, 4, 7, 8, 13, 15, 16, 17, 31} {
				for _, h := range []int{1, 3, 8, 17} {
					for _, element := range []int{1, 2} {
						t.Run(fmt.Sprintf("%dx%d/u%d", w, h, element*8), func(t *testing.T) {
							a, as := guardedRows(t, w, h, element)
							p, ps := guardedRows(t, w, h, element)
							r, rs := guardedRows(t, w, h, 4)
							out, os := guardedRows(t, w, h, 4)
							residual, result := guardInt32(r), guardInt32(out)
							for y := range h {
								for x := range w {
									residual[y*rs+x] = []int32{math.MinInt32, -1, 2, math.MaxInt32}[x%4]
								}
							}
							if element == 1 {
								for y := range h {
									for x := range w {
										a[y*as+x], p[y*ps+x] = 100, 103
									}
								}
								Subtract8(result, os, p, ps, a, as, w, h)
								AddResidual8(a, as, residual, rs, w, h)
								for y := range h {
									for x := range w {
										want := []byte{0, 99, 102, 255}[x%4]
										if a[y*as+x] != want || result[y*os+x] != 3 || p[y*ps+x] != 103 {
											t.Fatalf("残差u8 (%d,%d)=%d/%d，差=%d", x, y, a[y*as+x], want, result[y*os+x])
										}
									}
								}
							} else {
								a16, p16 := guardUint16(a), guardUint16(p)
								for y := range h {
									for x := range w {
										a16[y*as+x], p16[y*ps+x] = 512, 515
									}
								}
								Subtract16(result, os, p16, ps, a16, as, w, h)
								AddResidual16(a16, as, residual, rs, w, h)
								for y := range h {
									for x := range w {
										want := []uint16{0, 511, 514, 1023}[x%4]
										if a16[y*as+x] != want || result[y*os+x] != 3 || p16[y*ps+x] != 515 {
											t.Fatalf("残差u16 (%d,%d)=%d/%d，差=%d", x, y, a16[y*as+x], want, result[y*os+x])
										}
									}
								}
							}
						})
					}
				}
			}
		})
	}
}

func TestGuardPage_MC(t *testing.T) {
	if !guardChild(t) {
		return
	}
	previous := kernels
	defer func() { kernels = previous }()
	for _, backend := range guardBackends() {
		kernels = backend.table
		t.Run(backend.name, func(t *testing.T) {
			for _, w := range []int{1, 3, 7, 13, 16} {
				for _, h := range []int{1, 3, 16} {
					for phase := range 16 {
						t.Run(fmt.Sprintf("%dx%d/phase%d", w, h, phase), func(t *testing.T) {
							fx, fy := phase%4, phase/4
							rw, rh := w, h
							if fx != 0 {
								rw += 5
							}
							if fy != 0 {
								rh += 5
							}
							for _, element := range []int{1, 2} {
								t.Run(fmt.Sprintf("u%d", element*8), func(t *testing.T) {
									a, as := guardedRows(t, rw, rh, element)
									b, bs := guardedRows(t, w, h, element)
									if element == 1 {
										for y := range rh {
											for x := range rw {
												a[y*as+x] = 73
											}
										}
										MCLuma(b, bs, a, as, w, h, fx, fy, 8)
										for y := range h {
											for x := range w {
												if b[y*bs+x] != 73 {
													t.Fatalf("亮度MC常量改变 (%d,%d)=%d", x, y, b[y*bs+x])
												}
											}
										}
									} else {
										a16, b16 := guardUint16(a), guardUint16(b)
										for y := range rh {
											for x := range rw {
												a16[y*as+x] = 515
											}
										}
										MCLuma(b16, bs, a16, as, w, h, fx, fy, 10)
										for y := range h {
											for x := range w {
												if b16[y*bs+x] != 515 {
													t.Fatalf("亮度MC低两位改变 (%d,%d)=%d", x, y, b16[y*bs+x])
												}
											}
										}
									}
								})
							}
						})
					}
					for phase := range 64 {
						t.Run(fmt.Sprintf("%dx%d/chroma%d", w, h, phase), func(t *testing.T) {
							fx, fy := phase%8, phase/8
							rw, rh := w, h
							if fx != 0 {
								rw++
							}
							if fy != 0 {
								rh++
							}
							for _, element := range []int{1, 2} {
								t.Run(fmt.Sprintf("u%d", element*8), func(t *testing.T) {
									a, as := guardedRows(t, rw, rh, element)
									b, bs := guardedRows(t, w, h, element)
									if element == 1 {
										for y := range rh {
											for x := range rw {
												a[y*as+x] = 73
											}
										}
										MCChroma(b, bs, a, as, w, h, fx, fy)
										for y := range h {
											for x := range w {
												if b[y*bs+x] != 73 {
													t.Fatalf("色度MC常量改变 (%d,%d)=%d", x, y, b[y*bs+x])
												}
											}
										}
									} else {
										a16, b16 := guardUint16(a), guardUint16(b)
										for y := range rh {
											for x := range rw {
												a16[y*as+x] = 515
											}
										}
										MCChroma(b16, bs, a16, as, w, h, fx, fy)
										for y := range h {
											for x := range w {
												if b16[y*bs+x] != 515 {
													t.Fatalf("色度MC低两位改变 (%d,%d)=%d", x, y, b16[y*bs+x])
												}
											}
										}
									}
								})
							}
						})
					}
				}
			}
		})
	}
}
