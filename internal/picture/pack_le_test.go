package picture

import (
	"bytes"
	"encoding/binary"
	"slices"
	"testing"
)

func TestPackRow16LE_ExactBytesAndBounds(t *testing.T) {
	PackRow16LE(nil, nil)
	for _, n := range []int{1, 3, 7, 8, 15, 16, 17, 31, 959, 1919, 1920} {
		src := make([]uint16, n+1)
		for i := range src {
			src[i] = uint16(i * 7987)
		}
		src[1] = 65535
		orig := slices.Clone(src)
		dst := bytes.Repeat([]byte{0xa5}, n*2+2)
		want := slices.Clone(dst)
		for i, v := range src[1:] {
			binary.LittleEndian.PutUint16(want[1+2*i:], v)
		}
		PackRow16LE(dst[1:len(dst)-1:len(dst)-1], src[1:])
		if !bytes.Equal(dst, want) || !slices.Equal(src, orig) {
			t.Fatalf("宽 %d 的像素、源或边界不一致", n)
		}
	}
}

func TestPackRow16LE_LocalAllocation(t *testing.T) {
	if n := testing.AllocsPerRun(100, func() {
		src := [3]uint16{0xffff, 0x1234, 0x400}
		var dst [6]byte
		PackRow16LE(dst[:], src[:])
		if dst != [6]byte{255, 255, 0x34, 0x12, 0, 4} {
			panic("字节序不符")
		}
	}); n != 0 {
		t.Fatalf("局部缓冲 %g alloc/op，期望 0", n)
	}
}
