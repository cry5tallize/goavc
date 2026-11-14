package syntax

import (
	"bytes"
	"testing"
)

func TestPPS_SixScalingListsWithoutTransform8x8(t *testing.T) {
	// Spec: 7.3.2.2。transform_8x8_mode_flag=0时仅六个4×4列表标志，
	// 随后立刻是second_chroma_qp_index_offset=-2，不能再读写两个8×8标志。
	// 独立字段展开：110011100011100001 000000 00101 1，再补零至整字节。
	rbsp := []byte{0xce, 0x38, 0x40, 0x2c}
	s := &SPS{ProfileIDC: 100, ChromaFormatIDC: 1, FrameMbsOnlyFlag: true}
	p, err := ParsePPS(rbsp, s)
	if err != nil {
		t.Fatal(err)
	}
	if p.Transform8x8ModeFlag || !p.PicScalingMatrixPresentFlag || p.SecondChromaQPIndexOffset != -2 {
		t.Fatalf("wrong six-list syntax: %+v", p)
	}
	got, err := p.MarshalRBSP(s)
	if err != nil || !bytes.Equal(got, rbsp) {
		t.Fatalf("six-list output %x, %v; want %x", got, err, rbsp)
	}
}
