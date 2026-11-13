package syntax

import (
	"github.com/goavc/h264/internal/transform"
	"testing"
)

func TestScalingSelection_FallbacksAndRasterOrder(t *testing.T) {
	s, p := &SPS{}, &PPS{}
	m, err := SelectScalingMatrices(s, p)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range m.Four[0] {
		if v != 16 {
			t.Fatal("flat default")
		}
	}
	s.SeqScalingMatrixPresentFlag = true
	m, err = SelectScalingMatrices(s, p)
	if err != nil {
		t.Fatal(err)
	}
	if m.Four[0] != defaultScaling4[0] || m.Four[1] != m.Four[0] || m.Four[2] != m.Four[0] || m.Four[3] != defaultScaling4[1] || m.Eight[0] != defaultScaling8[0] {
		t.Fatal("SPS fallback A")
	}
	list := &ScalingList{Size: 4, Values: make([]int32, 16)}
	for i := range list.Values {
		list.Values[i] = int32(10 + i)
	}
	s.SeqScalingListPresentFlag[0] = true
	s.SeqScalingList[0] = list
	m, err = SelectScalingMatrices(s, p)
	if err != nil {
		t.Fatal(err)
	}
	for k, index := range transform.Zigzag4x4 {
		if m.Four[0][index] != uint8(k+10) {
			t.Fatal("scan was not converted to raster")
		}
	}
	p.PicScalingMatrixPresentFlag = true
	m, err = SelectScalingMatrices(s, p)
	if err != nil {
		t.Fatal(err)
	}
	if m.Four[0] != m.Four[1] || m.Four[3] != defaultScaling4[1] {
		t.Fatal("PPS fallback B")
	}
	s.SeqScalingList[0] = &ScalingList{Size: 4, Deltas: []int32{-8}, Values: make([]int32, 16)}
	m, err = SelectScalingMatrices(s, p)
	if err != nil || m.Four[0] != defaultScaling4[0] {
		t.Fatal("useDefault matrix", err)
	}
}

func TestScalingSelection_PPSOverridesOnlyItsOwnList(t *testing.T) {
	s := &SPS{}
	p := &PPS{PicScalingMatrixPresentFlag: true, Transform8x8ModeFlag: true}
	p.PicScalingListPresentFlag[7] = true
	p.PicScalingList[7] = &ScalingList{Size: 8, Values: make([]int32, 64)}
	for i := range p.PicScalingList[7].Values {
		p.PicScalingList[7].Values[i] = 32
	}
	m, err := SelectScalingMatrices(s, p)
	if err != nil {
		t.Fatal(err)
	}
	if m.Eight[0] != defaultScaling8[0] || m.Four[0] != defaultScaling4[0] {
		t.Fatal("wrong PPS fallback")
	}
	for _, v := range m.Eight[1] {
		if v != 32 {
			t.Fatal("PPS override missing")
		}
	}
}
