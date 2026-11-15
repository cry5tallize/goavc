package dpb

import (
	"github.com/cry5tallize/goavc/syntax"
	"testing"
)

func TestPOC_FrameTypeOneCycleAndNonReference(t *testing.T) {
	s := &syntax.SPS{PicOrderCntType: 1, OffsetForRefFrame: []int32{2, 3}, OffsetForNonRefPic: -1, OffsetForTopToBottomField: 1}
	p := NewPOCState(16)
	for _, tc := range []struct {
		fn       uint32
		ref, idr bool
		want     int32
	}{{0, true, true, 0}, {1, true, false, 2}, {2, false, false, 1}, {2, true, false, 5}} {
		top, bottom, order, err := p.ComputeFrame(s, &syntax.SliceHeader{FrameNum: tc.fn, IDR: tc.idr}, tc.ref)
		if err != nil || top != tc.want || bottom != top+1 || order != top {
			t.Fatalf("fn=%d ref=%v: %d/%d/%d %v", tc.fn, tc.ref, top, bottom, order, err)
		}
	}
	p.prevFrameNum = 15
	p.frameNumOffset = 0
	top, _, _, err := p.ComputeFrame(s, &syntax.SliceHeader{FrameNum: 0}, false)
	if err != nil || top != 36 {
		t.Fatal(top, err)
	}
	top, _, _, err = p.ComputeFrame(s, &syntax.SliceHeader{FrameNum: 0}, true)
	if err != nil || top != 40 {
		t.Fatal(top, err)
	}
}

func TestPOC_FrameTypeTwoWrapAndMMCOReset(t *testing.T) {
	p := NewPOCState(16)
	p.prevFrameNum = 15
	s := &syntax.SPS{PicOrderCntType: 2}
	_, _, order, err := p.ComputeFrame(s, &syntax.SliceHeader{FrameNum: 0}, false)
	if err != nil || order != 31 {
		t.Fatal(order, err)
	}
	_, _, order, err = p.ComputeFrame(s, &syntax.SliceHeader{FrameNum: 0}, true)
	if err != nil || order != 32 {
		t.Fatal(order, err)
	}
	p.AfterMMCO5(2)
	s.PicOrderCntType = 0
	top, _, _, err := p.ComputeFrame(s, &syntax.SliceHeader{FrameNum: 1, PicOrderCntLsb: 0}, true)
	if err != nil || top != 0 {
		t.Fatal(top, err)
	}
}

func TestPOCType0_Sequence(t *testing.T) {
	p := NewPOCState(16)
	tests := []struct {
		idr  bool
		lsb  int32
		want int32
	}{
		{true, 0, 0}, // IDR
		{false, 4, 4},
		{false, 8, 8},
		{false, 12, 12},
		{false, 0, 16}, // 回绕
		{false, 4, 20},
	}
	for _, tt := range tests {
		if got := p.Compute(tt.idr, tt.lsb); got != tt.want {
			t.Fatalf("lsb %d: poc = %d, want %d", tt.lsb, got, tt.want)
		}
	}
}

func TestPOCType0_Rewind(t *testing.T) {
	p := NewPOCState(16)
	p.Compute(true, 2)
	// lsb 从 2 跳到 12，差值 10 > 8，msb 减 16。
	if got := p.Compute(false, 12); got != -4 {
		t.Fatalf("rewind poc = %d, want -4", got)
	}
}

func TestPOCType0_Reset(t *testing.T) {
	p := NewPOCState(16)
	p.Compute(true, 10)
	p.Reset()
	if got := p.Compute(false, 3); got != 3 {
		t.Fatalf("after reset poc = %d, want 3", got)
	}
}
