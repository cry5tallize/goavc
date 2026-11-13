package encoder

import (
	"errors"
	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/internal/picture"
	"testing"
)

func TestAdaptiveQP_ActivityAndClipping(t *testing.T) {
	p, _ := picture.New(2, 1)
	for y := 0; y < 16; y++ {
		for x := 0; x < 32; x++ {
			value := byte(128)
			if x >= 16 {
				value = byte((x + y) % 2 * 255)
			}
			p.Planes[0].SetPix(x, y, value)
		}
	}
	for _, tc := range [][4]int{{0, 26, 3, 23}, {16, 26, 3, 29}, {0, 0, 6, 0}, {16, 51, 6, 51}, {0, 26, 0, 26}} {
		if got := activityQP(&p.Planes[0], tc[0], 0, tc[1], tc[2]); got != tc[3] {
			t.Fatal(tc, got)
		}
	}
	c := h264.DefaultEncoderConfig()
	c.Width = 32
	c.Height = 16
	if _, err := New(c, WithAdaptiveQP(7)); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal(err)
	}
	if _, err := New(c, WithPCM(), WithAdaptiveQP(1)); !errors.Is(err, h264.ErrUnsupported) {
		t.Fatal(err)
	}
	if _, err := New(c, WithMacroblockQP(func(x, y int) int { return 26 }), WithAdaptiveQP(1)); !errors.Is(err, h264.ErrUnsupported) {
		t.Fatal(err)
	}
	e, err := New(c, WithAdaptiveQP(3))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	f := testFrame(32, 16)
	for y := 0; y < 16; y++ {
		copy(f.Planes[0].Data[y*f.Planes[0].Stride:], p.Planes[0].Row(y))
	}
	if err = e.SendFrame(f); err != nil {
		t.Fatal(err)
	}
	if e.last.info[0].qp != 23 || e.last.info[1].qp != 29 {
		t.Fatal("coded QPs", e.last.info)
	}
}
