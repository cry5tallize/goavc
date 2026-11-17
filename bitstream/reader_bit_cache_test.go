package bitstream

import (
	"fmt"
	"testing"
)

func TestReader_CachedBitMixedOperations(t *testing.T) {
	for size := 0; size <= 17; size++ {
		data := make([]byte, size)
		for i := range data {
			data[i] = byte(i*71 + 0x5a)
		}
		for start := 0; start <= 8*size; start++ {
			a, b := NewReader(data), NewReader(data)
			_ = a.SkipBits(uint(start))
			_ = b.SkipBits(uint(start))
			for step := range 80 {
				var av, bv uint64
				var ae, be error
				switch step % 5 {
				case 0:
					v, e := a.ReadBit()
					av, ae = uint64(v), e
					bv, be = b.ReadBits(1)
				case 1:
					av, ae = a.ReadBits(uint(step * 13 % 65))
					bv, be = b.ReadBits(uint(step * 13 % 65))
				case 2:
					av, ae = a.PeekBits(uint(step * 3 % 65))
					bv, be = b.PeekBits(uint(step * 3 % 65))
				case 3:
					ae, be = a.AlignToByte(), b.AlignToByte()
				case 4:
					ae, be = a.SkipBits(uint(step*7%13)), b.SkipBits(uint(step*7%13))
				}
				if av != bv || fmt.Sprint(ae) != fmt.Sprint(be) || a.BitsRead() != b.BitsRead() || a.BitsRemaining() != b.BitsRemaining() || a.ByteAligned() != b.ByteAligned() {
					t.Fatalf("size%d start%d step%d值/位置/错误改变", size, start, step)
				}
			}
		}
	}
}
