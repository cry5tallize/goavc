package bitstream

import (
	"bytes"
	"testing"
)

func appendPattern(i, seed int) byte { return byte((i*13 + i/7 + seed) % 2) }

func TestWriter_AppendValidBits(t *testing.T) {
	for prefix := 0; prefix < 16; prefix++ {
		for _, whole := range []int{0, 1, 7, 17} {
			for tail := 0; tail < 8; tail++ {
				dst, src := NewWriter(), NewWriter()
				n := whole*8 + tail
				want := make([]byte, (prefix+n+7)/8)
				for i := range prefix {
					bit := appendPattern(i, 0)
					_ = dst.WriteBit(bit)
					want[i/8] |= bit << uint(7-i%8)
				}
				for i := range n {
					bit := appendPattern(i, 1)
					_ = src.WriteBit(bit)
					j := prefix + i
					want[j/8] |= bit << uint(7-j%8)
				}
				before := src.Bytes()
				if err := dst.Append(src); err != nil {
					t.Fatal(err)
				}
				if dst.BitsWritten() != uint64(prefix+n) || !bytes.Equal(dst.Bytes(), want) {
					t.Fatalf("prefix=%d source=%d: got %08b/%d, want %08b/%d", prefix, n, dst.Bytes(), dst.BitsWritten(), want, prefix+n)
				}
				if src.BitsWritten() != uint64(n) || !bytes.Equal(src.Bytes(), before) {
					t.Fatal("源Writer改变")
				}
				// 追加后源仍独立可写，不能改变目标已经写出的任何有效位。
				_ = src.WriteBit(1)
				if !bytes.Equal(dst.Bytes(), want) {
					t.Fatal("后续源写入覆盖目标")
				}
				_ = dst.WriteBit(1)
				if dst.BitsWritten() != uint64(prefix+n+1) {
					t.Fatal("目标未保留末字节状态")
				}
			}
		}
	}
}

func TestWriter_AppendRejectsAlias(t *testing.T) {
	src := NewWriter()
	_ = src.WriteBits(17, 0x12345)
	before := src.Bytes()
	if err := src.Append(src); err == nil {
		t.Fatal("自身追加应拒绝")
	}
	if err := src.Append(nil); err == nil {
		t.Fatal("nil源应拒绝")
	}
	if !bytes.Equal(src.Bytes(), before) || src.BitsWritten() != 17 {
		t.Fatal("失败污染源")
	}
	alias := *src
	alias.Reset()
	if err := alias.Append(src); err == nil || alias.BitsWritten() != 0 {
		t.Fatal("Reset后的共享backing未拒绝")
	}
	if !bytes.Equal(src.Bytes(), before) {
		t.Fatal("共享backing失败污染源")
	}
	alias = *src
	src.Reset()
	if err := alias.Append(src); err == nil || alias.BitsWritten() != 17 {
		t.Fatal("源Reset后的共享backing未拒绝")
	}
}

func TestAllocation_WriterAppend(t *testing.T) {
	src, dst := NewWriter(), NewWriter()
	for i := range 127 {
		_ = src.WriteBit(appendPattern(i, 1))
	}
	allocs := testing.AllocsPerRun(50, func() {
		dst.Reset()
		_ = dst.WriteBits(3, 5)
		if err := dst.Append(src); err != nil {
			panic(err)
		}
	})
	if allocs != 0 || dst.BitsWritten() != 130 {
		t.Fatalf("allocs=%g bits=%d", allocs, dst.BitsWritten())
	}
}
