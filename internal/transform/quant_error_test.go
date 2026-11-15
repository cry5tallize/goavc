package transform

import (
	"errors"
	"math"
	"testing"

	h264 "github.com/cry5tallize/goavc"
)

func TestDequantization_FirstFailurePreservesPrefix(t *testing.T) {
	for _, zeroIndex := range []int{1, 13} {
		var s4, d4 [16]int32
		var m4 [16]uint8
		var s8, d8 [64]int32
		var m8 [64]uint8
		for i := range s4 {
			s4[i] = 1
			d4[i] = 7890123
			m4[i] = 16
		}
		for i := range s8 {
			s8[i] = 1
			d8[i] = 7890123
			m8[i] = 16
		}
		s4[2], s8[2] = math.MaxInt32, math.MaxInt32
		m4[zeroIndex], m8[zeroIndex] = 0, 0
		if err := Dequant4x4WithScaling(&d4, &s4, 63, &m4); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("4×4未拒绝第一个错误：%v", err)
		}
		if err := Dequant8x8(&d8, &s8, 63, &m8); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("8×8未拒绝第一个错误：%v", err)
		}
		written := min(zeroIndex, 2)
		for i, v := range d4 {
			want := int32(7890123)
			if i < written {
				want = [2]int32{14336, 18432}[i]
			}
			if v != want {
				t.Fatalf("零项%d 4×4写出位置%d=%d，期望%d", zeroIndex, i, v, want)
			}
		}
		for i, v := range d8 {
			want := int32(7890123)
			if i < written {
				want = [2]int32{7168, 6656}[i]
			}
			if v != want {
				t.Fatalf("零项%d 8×8写出位置%d=%d，期望%d", zeroIndex, i, v, want)
			}
		}
	}
}

func TestQuant8x8_LateFailurePreservesAliasedSuffix(t *testing.T) {
	for _, alias := range []bool{false, true} {
		var src, dst [64]int32
		var matrix [64]uint8
		for i := range src {
			src[i] = 1
			dst[i] = 7890123
			matrix[i] = 1
		}
		src[2] = math.MaxInt32
		matrix[13] = 0
		before := dst
		target := &dst
		if alias {
			target = &src
			before = src
		}
		if err := Quant8x8WithScaling(target, &src, 0, &matrix); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("8×8量化未拒绝较早溢出：%v", err)
		}
		for i, v := range target {
			want := before[i]
			if i < 2 {
				want = 3
			}
			if v != want {
				t.Fatalf("原地%t量化位置%d=%d，期望%d", alias, i, v, want)
			}
		}
	}
}
