//go:build amd64 && !purego

package simd

// 一次处理完整水平/垂直/对角块；四像素一批，输入仅读取规范halo。
//
//go:noescape
func lumaHorizontal8SSE2(src []byte, rs, w, rows int, raw *[336]int32, horizontal []byte, clipStart, clipRows int)

//go:noescape
func lumaVertical8SSE2(src []byte, rs, w, h int, out []byte)

//go:noescape
func lumaDiagonal8SSE2(raw *[336]int32, w, h int, out []byte)

//go:noescape
func chroma8SSE2(dst []byte, ds int, src []byte, rs, w, h, fx, fy int, average bool)

func installMCFractionalKernels(cpu cpuFeatures, t kernelTable) kernelTable {
	if cpu.sse2 {
		t.backends.MCLumaFractional8, t.backends.MCChroma8 = SSE2, SSE2
		t.backends.MCLumaFractional16 = SSE2
		t.backends.MCChroma16 = SSE2
	}
	return t
}

func luma8PartsNative(src []byte, rs, w, h, fx, fy int, raw *[336]int32, horizontal, vertical, diagonal []byte) bool {
	if kernels.backends.MCLumaFractional8 != SSE2 || w < 4 || w > 16 || w%4 != 0 || h <= 0 || h > 16 || fx < 0 || fx > 3 || fy < 0 || fy > 3 {
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
		lumaHorizontal8SSE2(src[start*rs:], rs, w, rows, raw, horizontal, top-start, clipRows)
		if needJ {
			lumaDiagonal8SSE2(raw, w, h, diagonal)
		}
	}
	if needV {
		x := left
		if fx == 3 {
			x++
		}
		lumaVertical8SSE2(src[x:], rs, w, h, vertical)
	}
	return true
}

func chroma8Native(dst []byte, ds int, src []byte, rs, w, h, fx, fy int, average bool) bool {
	if kernels.backends.MCChroma8 != SSE2 || w < 4 || w%4 != 0 || h <= 0 || fx < 0 || fx > 7 || fy < 0 || fy > 7 || fx == 0 && fy == 0 {
		return false
	}
	chroma8SSE2(dst, ds, src, rs, w, h, fx, fy, average)
	return true
}
