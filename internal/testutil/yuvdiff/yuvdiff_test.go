package yuvdiff

import (
	"strings"
	"testing"
)

func TestPlanarLayout420(t *testing.T) {
	l := PlanarLayout(352, 288, 1, 2, 2)
	if len(l.Planes) != 3 {
		t.Fatalf("平面数 = %d", len(l.Planes))
	}
	if l.Planes[0].Width != 352 || l.Planes[0].Height != 288 {
		t.Fatalf("亮度平面 = %dx%d", l.Planes[0].Width, l.Planes[0].Height)
	}
	if l.Planes[1].Width != 176 || l.Planes[1].Height != 144 {
		t.Fatalf("色度平面 = %dx%d", l.Planes[1].Width, l.Planes[1].Height)
	}
	size, err := l.Size()
	if err != nil {
		t.Fatalf("Size 失败: %v", err)
	}
	if want := 352*288 + 2*176*144; size != want {
		t.Fatalf("Size = %d，期望 %d", size, want)
	}
}

// 裁剪后的奇数尺寸必须按向上取整计算，否则会少比较一行/一列色度。
func TestPlanarLayoutOddDimensions(t *testing.T) {
	l := PlanarLayout(641, 361, 1, 2, 2)
	if l.Planes[1].Width != 321 || l.Planes[1].Height != 181 {
		t.Fatalf("色度平面 = %dx%d，期望 321x181", l.Planes[1].Width, l.Planes[1].Height)
	}
}

func TestPlanarLayout10Bit(t *testing.T) {
	l := PlanarLayout(64, 32, 2, 2, 2)
	size, err := l.Size()
	if err != nil {
		t.Fatalf("Size 失败: %v", err)
	}
	if want := 64*32*2 + 2*32*16*2; size != want {
		t.Fatalf("Size = %d，期望 %d", size, want)
	}
}

func TestCompareIdentical(t *testing.T) {
	l := PlanarLayout(4, 4, 1, 2, 2)
	size, _ := l.Size()
	a := make([]byte, size)
	b := make([]byte, size)
	for i := range a {
		a[i] = byte(i)
	}
	copy(b, a)

	res, err := Compare(a, b, l)
	if err != nil {
		t.Fatalf("Compare 失败: %v", err)
	}
	if !res.Same() {
		t.Fatalf("相同数据被判为不同: %v", res)
	}
	if res.Samples != 4*4+2*2+2*2 {
		t.Fatalf("Samples = %d", res.Samples)
	}
}

func TestCompareDetectsChromaMismatch(t *testing.T) {
	l := PlanarLayout(4, 4, 1, 2, 2)
	size, _ := l.Size()
	a := make([]byte, size)
	b := make([]byte, size)
	// 色度平面从第 16 字节开始：每行 2 个采样，偏移 18 对应 (0,1)。
	b[18] = 7
	a[18] = 9

	res, err := Compare(a, b, l)
	if err != nil {
		t.Fatalf("Compare 失败: %v", err)
	}
	if res.Diffs != 1 {
		t.Fatalf("Diffs = %d，期望 1", res.Diffs)
	}
	if res.MaxDelta != 2 {
		t.Fatalf("MaxDelta = %d，期望 2", res.MaxDelta)
	}
	if res.First == nil {
		t.Fatal("缺少 First 描述")
	}
	if res.First.Plane != 1 || res.First.X != 0 || res.First.Y != 1 {
		t.Fatalf("首个差异位置错误: %+v", *res.First)
	}
	if res.First.A != 9 || res.First.B != 7 {
		t.Fatalf("首个差异值错误: %+v", *res.First)
	}
	if !strings.Contains(res.String(), "first=plane1") {
		t.Fatalf("摘要缺少定位信息: %s", res.String())
	}
}

// 10 bit 必须按 16 bit 采样比较，而不是按字节：
// 按字节比较会把「低位相同、高位不同」拆成两个像素差异，掩盖真实位置。
func TestCompare10Bit(t *testing.T) {
	l := Layout{Planes: []PlaneLayout{{Width: 2, Height: 1, BytesPerSample: 2}}}
	ref := []byte{0x01, 0x00, 0xFF, 0x03}
	got := []byte{0x01, 0x00, 0x00, 0x03}

	res, err := Compare(ref, got, l)
	if err != nil {
		t.Fatalf("Compare 失败: %v", err)
	}
	if res.Samples != 2 {
		t.Fatalf("Samples = %d，期望 2", res.Samples)
	}
	if res.Diffs != 1 {
		t.Fatalf("Diffs = %d，期望 1", res.Diffs)
	}
	if res.MaxDelta != 255 {
		t.Fatalf("MaxDelta = %d，期望 255", res.MaxDelta)
	}
}

func TestCompareLengthMismatch(t *testing.T) {
	l := PlanarLayout(4, 4, 1, 2, 2)
	size, _ := l.Size()
	if _, err := Compare(make([]byte, size), make([]byte, size-1), l); err == nil {
		t.Fatal("长度不符必须报错")
	}
}

func TestCompareInvalidLayout(t *testing.T) {
	bad := []Layout{
		{},
		{Planes: []PlaneLayout{{Width: 0, Height: 4, BytesPerSample: 1}}},
		{Planes: []PlaneLayout{{Width: 4, Height: 4, BytesPerSample: 3}}},
	}
	for i, l := range bad {
		if _, err := Compare(nil, nil, l); err == nil {
			t.Fatalf("非法布局 %d 未报错", i)
		}
	}
}
