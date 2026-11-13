package bitstream

import (
	"errors"
	"testing"

	h264 "github.com/goavc/h264"
)

// FuzzReaderReadBits 检查位读取器在任意输入与任意宽度下都不 panic、
// 不越界，并保持"成功则精确推进、失败则不推进"的契约。
//
// 这类不变量比逐值断言更重要：Reader 是所有语法解析的入口，
// 一旦它在截断输入上静默补零，损坏码流会被解析成看似合法的 SPS/PPS。
func FuzzReaderReadBits(f *testing.F) {
	f.Add([]byte{}, uint8(0))
	f.Add([]byte{0xFF}, uint8(8))
	f.Add([]byte{0xFF}, uint8(9))
	f.Add([]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, uint8(64))
	f.Add([]byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF}, uint8(64))
	f.Add([]byte{0x80}, uint8(65))

	f.Fuzz(func(t *testing.T, data []byte, width uint8) {
		n := uint(width)
		r := NewReader(data)
		remaining := r.BitsRemaining()
		before := r.BitsRead()

		v, err := r.ReadBits(n)
		if n > 64 || uint64(n) > remaining {
			if !errors.Is(err, h264.ErrInvalidData) {
				t.Fatalf("data=%x n=%d: 期望 ErrInvalidData，实际 %v", data, n, err)
			}
			if r.BitsRead() != before {
				t.Fatalf("data=%x n=%d: 失败读取推进了位置 %d -> %d", data, n, before, r.BitsRead())
			}
			return
		}
		if err != nil {
			t.Fatalf("data=%x n=%d: 期望成功，实际 %v", data, n, err)
		}
		if r.BitsRead() != before+uint64(n) {
			t.Fatalf("data=%x n=%d: 位置 %d -> %d", data, n, before, r.BitsRead())
		}
		if n < 64 && v>>n != 0 {
			t.Fatalf("data=%x n=%d: 返回值 %#x 超出宽度", data, n, v)
		}
	})
}

// FuzzReaderPeekBits 锁定 PeekBits 不改变位置，且与 ReadBits 的取值一致。
func FuzzReaderPeekBits(f *testing.F) {
	f.Add([]byte{}, uint8(1))
	f.Add([]byte{0xAA}, uint8(8))
	f.Add([]byte{0xAA, 0x55}, uint8(16))
	f.Add([]byte{0xAA, 0x55}, uint8(17))
	f.Add([]byte{0x01, 0x02, 0x03}, uint8(64))

	f.Fuzz(func(t *testing.T, data []byte, width uint8) {
		n := uint(width)
		r := NewReader(data)
		remaining := r.BitsRemaining()
		before := r.BitsRead()

		v, err := r.PeekBits(n)
		if r.BitsRead() != before {
			t.Fatalf("data=%x n=%d: PeekBits 改变了位置 %d -> %d", data, n, before, r.BitsRead())
		}
		if n > 64 || uint64(n) > remaining {
			if !errors.Is(err, h264.ErrInvalidData) {
				t.Fatalf("data=%x n=%d: 期望 ErrInvalidData，实际 %v", data, n, err)
			}
			return
		}
		if err != nil {
			t.Fatalf("data=%x n=%d: 期望成功，实际 %v", data, n, err)
		}
		got, err := r.ReadBits(n)
		if err != nil || got != v {
			t.Fatalf("data=%x n=%d: Peek=%#x Read=%#x err=%v", data, n, v, got, err)
		}
	})
}

// FuzzReaderSkipBits 锁定 SkipBits 的原子性：失败时位置不得移动。
func FuzzReaderSkipBits(f *testing.F) {
	f.Add([]byte{}, uint8(1))
	f.Add([]byte{0x00}, uint8(8))
	f.Add([]byte{0x00}, uint8(9))
	f.Add([]byte{0xFF, 0xFF}, uint8(16))

	f.Fuzz(func(t *testing.T, data []byte, width uint8) {
		n := uint(width)
		r := NewReader(data)
		remaining := r.BitsRemaining()
		before := r.BitsRead()

		err := r.SkipBits(n)
		if uint64(n) > remaining {
			if !errors.Is(err, h264.ErrInvalidData) {
				t.Fatalf("data=%x n=%d: 期望 ErrInvalidData，实际 %v", data, n, err)
			}
			if r.BitsRead() != before {
				t.Fatalf("data=%x n=%d: 失败跳过推进了位置", data, n)
			}
			return
		}
		if err != nil {
			t.Fatalf("data=%x n=%d: 期望成功，实际 %v", data, n, err)
		}
		if r.BitsRead() != before+uint64(n) {
			t.Fatalf("data=%x n=%d: 位置 %d -> %d", data, n, before, r.BitsRead())
		}
	})
}
