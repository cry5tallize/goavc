package simd

import "testing"

func TestDispatch_SADAnalytic(t *testing.T) {
	// Spec: 运动估计代价保持原始样本差；全幅差的解析值不依赖另一实现。
	var a8, b8 [256]byte
	var a16, b16 [256]uint16
	for i := range b8 {
		b8[i], b16[i] = 255, 1023
	}
	if got := SAD8(a8[:], 16, b8[:], 16, 16, 16); got != 65280 {
		t.Fatalf("SAD8全幅差=%d，期望65280", got)
	}
	if got := SAD16(a16[:], 16, b16[:], 16, 16, 16); got != 261888 {
		t.Fatalf("SAD16全幅差=%d，期望261888", got)
	}
	// 最小末行len、不同非对齐stride、原生10位低两位。
	lo, hi := make([]uint16, 9*12+7), make([]uint16, 11*12+7)
	for y := range 13 {
		for x := range 7 {
			lo[y*9+x], hi[y*11+x] = 512, 515
		}
	}
	if got := SAD16(lo, 9, hi, 11, 7, 13); got != 273 {
		t.Fatalf("SAD16低两位矩形=%d，期望273", got)
	}
	t.Logf("实际后端：%+v", ActiveBackends())
}
