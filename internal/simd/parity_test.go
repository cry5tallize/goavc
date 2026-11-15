package simd

import (
	"bytes"
	"math/rand"
	"slices"
	"testing"
)

// 后端专用测试复用同一矩阵；不导入领域包，避免反向依赖环。
func checkSADParity(t *testing.T, table kernelTable) {
	t.Helper()
	const seed = 0x8a7c
	rng := rand.New(rand.NewSource(seed))
	sizes := []int{1, 3, 4, 7, 8, 13, 15, 16, 17, 31}
	for _, w := range sizes {
		for _, h := range sizes {
			for offset := range 32 {
				for padding := range 3 {
					as, bs := w+padding, w+padding*2+1
					a8, b8 := make([]byte, offset+(h-1)*as+w), make([]byte, offset+(h-1)*bs+w)
					a16, b16 := make([]uint16, offset/2+(h-1)*as+w), make([]uint16, offset/2+(h-1)*bs+w)
					for i := range a8 {
						a8[i] = byte(rng.Intn(256))
					}
					for i := range b8 {
						b8[i] = byte(rng.Intn(256))
					}
					for i := range a16 {
						a16[i] = uint16(rng.Intn(1024))
					}
					for i := range b16 {
						b16[i] = uint16(rng.Intn(1024))
					}
					beforeA8, beforeB8 := bytes.Clone(a8), bytes.Clone(b8)
					beforeA16, beforeB16 := slices.Clone(a16), slices.Clone(b16)
					want8 := sadGeneric(a8[offset:], as, b8[offset:], bs, w, h)
					want16 := sadGeneric(a16[offset/2:], as, b16[offset/2:], bs, w, h)
					got8 := table.sad8(a8[offset:], as, b8[offset:], bs, w, h)
					got16 := table.sad16(a16[offset/2:], as, b16[offset/2:], bs, w, h)
					if got8 != want8 || got16 != want16 || !bytes.Equal(a8, beforeA8) || !bytes.Equal(b8, beforeB8) || !slices.Equal(a16, beforeA16) || !slices.Equal(b16, beforeB16) {
						t.Fatalf("seed=%d backend=%+v w/h=%d/%d stride=%d/%d offset=%d SAD8=%d/%d SAD16=%d/%d 或输入被修改", seed, table.backends, w, h, as, bs, offset, got8, want8, got16, want16)
					}
				}
			}
		}
	}
	// 每个直接后端同时接受独立解析值，而不是只与Generic互比。
	var zero8, full8 [256]byte
	var zero16, full16 [256]uint16
	for i := range full8 {
		full8[i], full16[i] = 255, 1023
	}
	if got := table.sad8(zero8[:], 16, full8[:], 16, 16, 16); got != 65280 {
		t.Fatalf("后端%+v SAD8最大差=%d", table.backends, got)
	}
	if got := table.sad16(zero16[:], 16, full16[:], 16, 16, 16); got != 261888 {
		t.Fatalf("后端%+v SAD16最大差=%d", table.backends, got)
	}
	if got := table.sad16(full16[:], 16, zero16[:], 16, 16, 16); got != 261888 {
		t.Fatalf("后端%+v SAD16反向最大差=%d", table.backends, got)
	}
}

func TestParity_SAD(t *testing.T) {
	t.Run("Generic", func(t *testing.T) { checkSADParity(t, genericTable()) })
	t.Run("Auto", func(t *testing.T) { checkSADParity(t, kernels) })
}

func TestAllocation_SAD(t *testing.T) {
	var a8, b8 [256]byte
	var a16, b16 [256]uint16
	for i := range b8 {
		b8[i], b16[i] = 255, 1023
	}
	if got := testing.AllocsPerRun(100, func() {
		if SAD8(a8[:], 16, b8[:], 16, 16, 16) != 65280 || SAD16(a16[:], 16, b16[:], 16, 16, 16) != 261888 {
			panic("SAD计算错误")
		}
	}); got != 0 {
		t.Fatalf("生产包装分配=%v，期望0", got)
	}
}
