package h264

import "testing"

func TestPacketClone(t *testing.T) {
	src := Packet{
		Data:     []byte{0x00, 0x00, 0x00, 0x01, 0x67},
		PTS:      3000,
		DTS:      2000,
		Duration: 1001,
		KeyFrame: true,
	}
	dst := src.Clone()

	if dst.PTS != src.PTS || dst.DTS != src.DTS || dst.Duration != src.Duration || !dst.KeyFrame {
		t.Fatal("时间戳或标志未复制")
	}
	if len(dst.Data) != len(src.Data) {
		t.Fatal("载荷长度不一致")
	}
	if &dst.Data[0] == &src.Data[0] {
		t.Fatal("Clone 共享了载荷底层数组")
	}
	src.Data[4] ^= 0xFF
	if dst.Data[4] == src.Data[4] {
		t.Fatal("修改源数据影响了克隆体")
	}
}

// nil 载荷的 Clone 必须保持 nil，而不是变成空切片：
// 二者在下游"是否包含 IDR"的判断里语义不同。
func TestPacketCloneNilData(t *testing.T) {
	var src Packet
	dst := src.Clone()
	if dst.Data != nil {
		t.Fatalf("Clone 把 nil 载荷变成了 %v", dst.Data)
	}
}

func TestPacketValid(t *testing.T) {
	if (Packet{}).Valid() {
		t.Fatal("空载荷不应有效")
	}
	if !(Packet{Data: []byte{0x00}}).Valid() {
		t.Fatal("非空载荷应有效")
	}
}
