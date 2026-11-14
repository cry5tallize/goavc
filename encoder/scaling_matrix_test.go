package encoder

import (
	"errors"
	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/syntax"
	"testing"
)

func TestScalingMatrices4x4_IndependentListsAndCopy(t *testing.T) {
	cfg := h264.DefaultEncoderConfig()
	cfg.Width, cfg.Height = 32, 32
	cfg.Profile = h264.ProfileHigh
	var matrices [6][16]uint8
	if _, err := New(cfg, WithScalingMatrices4x4(matrices)); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal("零项未拒绝", err)
	}
	for list := range matrices {
		for i := range matrices[list] {
			matrices[list][i] = uint8(1 + (list*43+i*17)%255)
		}
	}
	want := matrices
	e, err := New(cfg, WithScalingMatrices4x4(matrices))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	matrices = [6][16]uint8{}
	rb, err := e.pps.MarshalRBSP(e.sps)
	if err != nil {
		t.Fatal(err)
	}
	pps, err := syntax.ParsePPS(rb, e.sps)
	if err != nil {
		t.Fatal(err)
	}
	got, err := syntax.SelectScalingMatrices(e.sps, pps)
	if err != nil {
		t.Fatal(err)
	}
	if got.Four != want || e.transform8 {
		t.Fatal("独立4×4列表信令/值拷贝/变换开关错误")
	}
	cfg.Profile = h264.ProfileMain
	if _, err := New(cfg, WithScalingMatrices4x4(want)); !errors.Is(err, h264.ErrUnsupported) {
		t.Fatal("Main矩阵配置未拒绝", err)
	}
}
