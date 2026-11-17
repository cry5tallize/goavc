//go:build amd64 && !purego

package picture

import "unsafe"

// PackRow16LE 复制整行原生采样到独立的字节缓冲。amd64 本身是小端；
// 借用源字节视图仅供 copy，范围恰好覆盖 src，不返回、修改或保留该视图。
// 先截取目标范围既校验容量，也避免对源的额外读取。
func PackRow16LE(dst []byte, src []uint16) {
	dst = dst[:len(src)*2]
	copy(dst, unsafe.Slice((*byte)(unsafe.Pointer(unsafe.SliceData(src))), len(dst)))
}
