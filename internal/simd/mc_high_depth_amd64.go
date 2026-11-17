//go:build amd64 && !purego

package simd

// 10-bit第一遍范围[-10230,42966]，必须用int32而非8-bit路径的int16。
// 完整uint16输入域两遍保守绝对值<2^28，仍保持旧入口最终Clip1023语义。
// 每次加载四个uint16，输出仅写四个；不保留源及scratch引用。
//
//go:noescape
func lumaHorizontal16SSE2(src []uint16, rs, w, rows int, raw *[336]int32, horizontal []uint16, clipStart, clipRows int)

//go:noescape
func lumaVertical16SSE2(src []uint16, rs, w, h int, out []uint16)

//go:noescape
func lumaDiagonal16SSE2(raw *[336]int32, w, h int, out []uint16)

// chroma16SSE2保留完整uint16输入域，32位累加后无符号压回16位。
//
//go:noescape
func chroma16SSE2(dst []uint16, ds int, src []uint16, rs, w, h, fx, fy int)

func chroma16Native(dst []uint16, ds int, src []uint16, rs, w, h, fx, fy int) bool {
	if kernels.backends.MCChroma16 != SSE2 || w < 4 || w > 16 || w%4 != 0 || h <= 0 || h > 16 || fx < 0 || fx > 7 || fy < 0 || fy > 7 || fx == 0 && fy == 0 {
		return false
	}
	chroma16SSE2(dst, ds, src, rs, w, h, fx, fy)
	return true
}

func luma16PartsNative(src []uint16, rs, w, h, fx, fy int, raw *[336]int32, horizontal, vertical, diagonal []uint16) bool {
	if kernels.backends.MCLumaFractional16 != SSE2 || w < 4 || w > 16 || w%4 != 0 || h <= 0 || h > 16 || fx < 0 || fx > 3 || fy < 0 || fy > 3 {
		return false
	}
	needJ := fx != 0 && fy != 0 && (fx == 2 || fy == 2)
	needH := fx != 0 && (fy == 0 || fx == 2 || fy&1 != 0)
	needV := fy != 0 && (fx == 0 || fx&1 != 0)
	left, top := 0, 0
	if fx != 0 {
		left = 2
	}
	if fy != 0 {
		top = 2
	}
	if fx != 0 {
		start, rows := top, h
		if fy == 3 {
			rows++
		}
		if needJ {
			start, rows = 0, h+5
		}
		clipRows := 0
		if needH {
			clipRows = h
			if fy == 3 {
				clipRows++
			}
		}
		lumaHorizontal16SSE2(src[start*rs:], rs, w, rows, raw, horizontal, top-start, clipRows)
		if needJ {
			lumaDiagonal16SSE2(raw, w, h, diagonal)
		}
	}
	if needV {
		x := left
		if fx == 3 {
			x++
		}
		lumaVertical16SSE2(src[x:], rs, w, h, vertical)
	}
	return true
}
