//go:build !amd64 || purego

package simd

func chroma16Native(dst []uint16, ds int, src []uint16, rs, w, h, fx, fy int) bool {
	return false
}

func luma16PartsNative(src []uint16, rs, w, h, fx, fy int, raw *[336]int32, horizontal, vertical, diagonal []uint16) bool {
	return false
}
