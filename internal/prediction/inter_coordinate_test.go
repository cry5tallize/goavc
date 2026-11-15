package prediction

import (
	"github.com/goavc/h264/internal/picture"
	"testing"
)

func TestMCBlock_WideCoordinatesPreserveSampleClamp(t *testing.T) {
	// int最大值的MV：首样本钳到右边，下一样本加坐标步长回绕后钳到左边。
	// 固定此旧入口行为，防止把块分解带来的宽域变化隐藏在正常视频测试之外。
	maxInt := int(^uint(0) >> 1)
	p := picture.Plane{Width: 16, Height: 1, Stride: 16, BitDepth: 8, Data: make([]byte, 16)}
	for x := range p.Data {
		p.Data[x] = byte(40 + x)
	}
	var dst [4]byte
	MCBlockLuma(dst[:], 4, &p, 0, 0, 4, 1, maxInt, 0)
	if dst != [4]byte{55, 40, 40, 40} {
		t.Fatalf("亮度宽MV=%v", dst)
	}
	MCBlockChromaFormat(dst[:], 4, &p, 0, 0, 4, 1, maxInt, 0, 2, 2)
	if dst != [4]byte{55, 40, 40, 40} {
		t.Fatalf("色度宽MV=%v", dst)
	}
	p.BitDepth, p.Data, p.Data16 = 10, nil, make([]uint16, 16)
	for x := range p.Data16 {
		p.Data16[x] = uint16(400 + x*5)
	}
	var high [4]uint16
	MCBlockLumaHigh(high[:], 4, &p, 0, 0, 4, 1, maxInt, 0)
	if high != [4]uint16{475, 400, 400, 400} {
		t.Fatalf("高位深宽MV=%v", high)
	}
	MCBlockChromaFormatHigh(high[:], 4, &p, 0, 0, 4, 1, maxInt, 0, 2, 2)
	if high != [4]uint16{475, 400, 400, 400} {
		t.Fatalf("高位深色度宽MV=%v", high)
	}
}
