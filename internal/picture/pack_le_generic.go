//go:build !amd64 || purego

package picture

import "encoding/binary"

// PackRow16LE 将整行转换为小端字节，不依赖主机字节序。
func PackRow16LE(dst []byte, src []uint16) {
	dst = dst[:len(src)*2]
	for i, v := range src {
		binary.LittleEndian.PutUint16(dst[2*i:], v)
	}
}
