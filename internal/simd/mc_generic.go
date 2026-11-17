package simd

// MCLuma 计算最多16×16的内部亮度块（Spec: 8.4.2.2.1）。ref从所需halo
// 左上角开始；fx/fy为0..3。非零相位对应轴有左/上2、右/下3采样halo。
// 调用者证明len与逻辑图像/场边界；本函数不钳位，也不认识picture对象。
// dst/ref不重叠，stride以采样计。8/10位共用唯一标量算法。
func MCLuma[T ~byte | ~uint16](dst []T, ds int, ref []T, rs, w, h, fx, fy, bitDepth int) {
	if fx == 0 && fy == 0 && w == 16 {
		if d, ok := any(dst).([]byte); ok && copy16Platform(d, ds, any(ref).([]byte), rs, h) {
			return
		}
	}
	mcLumaGeneric(dst, ds, ref, rs, w, h, fx, fy, bitDepth)
}

func mcLumaGeneric[T ~byte | ~uint16](dst []T, ds int, ref []T, rs, w, h, fx, fy, bitDepth int) {
	mcLumaMode(dst, ds, ref, rs, w, h, fx, fy, bitDepth, false)
}

// MCLumaAverage8先完成规范插值/Clip，再和既有目标做向上舍入均值。
func MCLumaAverage8(dst []byte, ds int, ref []byte, rs, w, h, fx, fy int) {
	mcLumaMode(dst, ds, ref, rs, w, h, fx, fy, 8, true)
}

func mcLumaMode[T ~byte | ~uint16](dst []T, ds int, ref []T, rs, w, h, fx, fy, bitDepth int, average bool) {
	left, top := 0, 0
	if fx != 0 {
		left = 2
	}
	if fy != 0 {
		top = 2
	}
	if fx == 0 && fy == 0 {
		if average {
			mcCombine(dst, ds, ref, rs, nil, 0, w, h, true)
			return
		}
		for y := range h {
			copy(dst[y*ds:][:w], ref[y*rs:][:w])
		}
		return
	}
	// j只用于至少一个轴恰为半像素的双轴相位；奇/奇相位不能误用j。
	needJ := fx != 0 && fy != 0 && (fx == 2 || fy == 2)
	needH := fx != 0 && (fy == 0 || fx == 2 || fy&1 != 0)
	needV := fy != 0 && (fx == 0 || fx&1 != 0)
	var rawH [21 * 16]int32
	var horizontal [17 * 16]T
	var vertical, diagonal [16 * 16]T
	accelerated := false
	if bitDepth == 8 {
		if source, ok := any(ref).([]byte); ok {
			accelerated = luma8PartsNative(source, rs, w, h, fx, fy, &rawH, any(horizontal[:]).([]byte), any(vertical[:]).([]byte), any(diagonal[:]).([]byte))
		}
	}
	if !accelerated && fx != 0 {
		start, rows := top, h
		if fy == 3 {
			rows++
		}
		if needJ {
			start, rows = 0, h+5
		}
		for y := range rows {
			row := ref[(y+start)*rs:]
			for x := range w {
				rawH[y*16+x] = sixTap(int32(row[x]), int32(row[x+1]), int32(row[x+2]), int32(row[x+3]), int32(row[x+4]), int32(row[x+5]))
			}
		}
		if needH {
			rows := h
			if fy == 3 {
				rows++
			}
			for y := range rows {
				for x := range w {
					horizontal[y*16+x] = clipPixel[T]((rawH[(y+top-start)*16+x]+16)>>5, bitDepth)
				}
			}
		}
		if needJ {
			for y := range h {
				for x := range w {
					i := y*16 + x
					v := sixTap(rawH[i], rawH[i+16], rawH[i+32], rawH[i+48], rawH[i+64], rawH[i+80])
					diagonal[i] = clipPixel[T]((v+512)>>10, bitDepth)
				}
			}
		}
	}
	if !accelerated && needV {
		xOffset := left
		if fx == 3 {
			xOffset++
		}
		for y := range h {
			for x := range w {
				i := y*rs + x + xOffset
				v := sixTap(int32(ref[i]), int32(ref[i+rs]), int32(ref[i+2*rs]), int32(ref[i+3*rs]), int32(ref[i+4*rs]), int32(ref[i+5*rs]))
				vertical[y*16+x] = clipPixel[T]((v+16)>>5, bitDepth)
			}
		}
	}
	// 只在块入口选择组合及寻址。最终循环不再分相位，不改变四分之一相位图。
	var a, b []T
	as, bs := 16, 16
	origin := top*rs + left
	switch fx + 4*fy {
	case 1:
		a, as, b = ref[origin:], rs, horizontal[:]
	case 2:
		a = horizontal[:]
	case 3:
		a, as, b = ref[origin+1:], rs, horizontal[:]
	case 4:
		a, as, b = ref[origin:], rs, vertical[:]
	case 8:
		a = vertical[:]
	case 12:
		a, as, b = ref[origin+rs:], rs, vertical[:]
	case 5, 7:
		a, b = horizontal[:], vertical[:]
	case 13, 15:
		a, b = horizontal[16:], vertical[:]
	case 6:
		a, b = horizontal[:], diagonal[:]
	case 10:
		a = diagonal[:]
	case 14:
		a, b = horizontal[16:], diagonal[:]
	case 9, 11:
		a, b = vertical[:], diagonal[:]
	}
	if b == nil {
		if average {
			mcCombine(dst, ds, a, as, nil, 0, w, h, true)
			return
		}
		for y := range h {
			copy(dst[y*ds:][:w], a[y*as:][:w])
		}
		return
	}
	if _, ok := any(dst).([]byte); ok {
		mcCombine(dst, ds, a, as, b, bs, w, h, average)
		return
	}
	for y := range h {
		out, first, second := dst[y*ds:][:w], a[y*as:][:w], b[y*bs:][:w]
		for x := range w {
			out[x] = T((int(first[x]) + int(second[x]) + 1) >> 1)
		}
	}
}

// 中间绝对值<=52*1023，第二遍<=52*52*1023，int32域可精确容纳。
func sixTap(a, b, c, d, e, f int32) int32 { return a - 5*b + 20*c + 20*d - 5*e + f }

func clipPixel[T ~byte | ~uint16](v int32, bitDepth int) T {
	limit := int32((1 << bitDepth) - 1)
	return T(min(max(v, 0), limit))
}

// MCChroma 计算最多16×16双线性块。fx/fy是0..7的本平面1/8相位，
// 非零轴才要求右/下一个采样。422垂直相位由领域层转换，444不能调用此算法。
func MCChroma[T ~byte | ~uint16](dst []T, ds int, ref []T, rs, w, h, fx, fy int) {
	mcChromaMode(dst, ds, ref, rs, w, h, fx, fy, false)
}

// MCChromaAverage8保留双线性插值的舍入，再对目标取均值，不平均未舍入中间值。
func MCChromaAverage8(dst []byte, ds int, ref []byte, rs, w, h, fx, fy int) {
	mcChromaMode(dst, ds, ref, rs, w, h, fx, fy, true)
}

func chromaBlend2[T ~byte | ~uint16](a, b T, f int) T { return T(((8-f)*int(a) + f*int(b) + 4) >> 3) }
func chromaBlend4[T ~byte | ~uint16](u, v, s, t T, a, b, c, d int) T {
	return T((a*int(u) + b*int(v) + c*int(s) + d*int(t) + 32) >> 6)
}

func mcChromaMode[T ~byte | ~uint16](dst []T, ds int, ref []T, rs, w, h, fx, fy int, average bool) {
	if d, ok := any(dst).([]byte); ok {
		if chroma8Native(d, ds, any(ref).([]byte), rs, w, h, fx, fy, average) {
			return
		}
	}
	if fx == 0 && fy == 0 {
		if average {
			mcCombine(dst, ds, ref, rs, nil, 0, w, h, true)
			return
		}
		for y := range h {
			copy(dst[y*ds:][:w], ref[y*rs:][:w])
		}
		return
	}
	if fx == 0 {
		if average {
			for y := range h {
				for x := range w {
					v := chromaBlend2(ref[y*rs+x], ref[(y+1)*rs+x], fy)
					dst[y*ds+x] = T((int(dst[y*ds+x]) + int(v) + 1) >> 1)
				}
			}
			return
		}
		for y := range h {
			for x := range w {
				dst[y*ds+x] = chromaBlend2(ref[y*rs+x], ref[(y+1)*rs+x], fy)
			}
		}
		return
	}
	if fy == 0 {
		if average {
			for y := range h {
				for x := range w {
					v := chromaBlend2(ref[y*rs+x], ref[y*rs+x+1], fx)
					dst[y*ds+x] = T((int(dst[y*ds+x]) + int(v) + 1) >> 1)
				}
			}
			return
		}
		for y := range h {
			for x := range w {
				dst[y*ds+x] = chromaBlend2(ref[y*rs+x], ref[y*rs+x+1], fx)
			}
		}
		return
	}
	a, b, c, d := (8-fx)*(8-fy), fx*(8-fy), (8-fx)*fy, fx*fy
	if average {
		for y := range h {
			for x := range w {
				i := y*rs + x
				v := chromaBlend4(ref[i], ref[i+1], ref[i+rs], ref[i+rs+1], a, b, c, d)
				dst[y*ds+x] = T((int(dst[y*ds+x]) + int(v) + 1) >> 1)
			}
		}
		return
	}
	for y := range h {
		for x := range w {
			i := y*rs + x
			dst[y*ds+x] = chromaBlend4(ref[i], ref[i+1], ref[i+rs], ref[i+rs+1], a, b, c, d)
		}
	}
}
