package dpb

import (
	"errors"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/internal/picture"
)

func TestDPB_FrameNumSequence(t *testing.T) {
	d := New(16, false)
	if err := d.BeginFrame(true, 0); err != nil {
		t.Fatalf("IDR: %v", err)
	}
	for _, fn := range []uint32{1, 2, 3} {
		if err := d.BeginFrame(false, fn); err != nil {
			t.Fatalf("frame_num %d: %v", fn, err)
		}
	}
	if err := d.BeginFrame(false, 5); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("gap err = %v, want ErrInvalidData", err)
	}
	// 同一个 frame_num 重复也是非法。
	if err := d.BeginFrame(false, 3); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("duplicate err = %v, want ErrInvalidData", err)
	}
}

func TestDPB_WrapRequiresZero(t *testing.T) {
	d := New(16, false)
	if err := d.BeginFrame(false, 15); err != nil {
		t.Fatal(err)
	}
	if err := d.BeginFrame(false, 3); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal("accepted gap at wrap", err)
	}
	if err := d.BeginFrame(false, 16); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal("accepted out-of-range frame_num", err)
	}
	if err := d.BeginFrame(false, 0); err != nil {
		t.Fatal(err)
	}
}

func TestDPB_GapsAllowed(t *testing.T) {
	d := New(16, true)
	if err := d.BeginFrame(true, 0); err != nil {
		t.Fatal(err)
	}
	if err := d.BeginFrame(false, 4); err != nil {
		t.Fatalf("gaps allowed: %v", err)
	}
}

func TestDPB_FrameNumWrap(t *testing.T) {
	d := New(8, false)
	if err := d.BeginFrame(true, 0); err != nil {
		t.Fatal(err)
	}
	for fn := uint32(1); fn <= 7; fn++ {
		if err := d.BeginFrame(false, fn); err != nil {
			t.Fatalf("frame_num %d: %v", fn, err)
		}
	}
	if err := d.BeginFrame(false, 0); err != nil {
		t.Fatalf("wrap 7->0: %v", err)
	}
	d2 := New(8, false)
	if err := d2.BeginFrame(true, 0); err != nil {
		t.Fatal(err)
	}
	if err := d2.BeginFrame(false, 1); err != nil {
		t.Fatal(err)
	}
	if err := d2.BeginFrame(false, 2); err != nil {
		t.Fatal(err)
	}
	if err := d2.BeginFrame(false, 3); err != nil {
		t.Fatal(err)
	}
	if err := d2.BeginFrame(false, 0); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("invalid wrap err = %v, want ErrInvalidData", err)
	}
}

func TestDPB_IDRMustUseFrameZero(t *testing.T) {
	d := New(8, false)
	if err := d.BeginFrame(true, 1); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("err = %v, want ErrInvalidData", err)
	}
}

func TestDPB_PutReferenceReleasesOld(t *testing.T) {
	pool := picture.NewPool(2)
	a, err := pool.Get(1, 1, h264.YUV420P)
	if err != nil {
		t.Fatal(err)
	}
	b, err := pool.Get(1, 1, h264.YUV420P)
	if err != nil {
		t.Fatal(err)
	}
	d := New(16, false)
	d.PutReference(a, 0, 0)
	if d.Reference() != a || d.ReferenceFrameNum() != 0 {
		t.Fatal("reference not stored")
	}
	d.PutReference(b, 1, 2)
	if d.Reference() != b {
		t.Fatal("reference not replaced")
	}
	// a 应已归还：再次 Get 应拿到 a。
	c, err := pool.Get(1, 1, h264.YUV420P)
	if err != nil {
		t.Fatal(err)
	}
	if c != a {
		t.Fatal("old reference was not released to pool")
	}
	c.Release()
	d.Reset()
	if d.Reference() != nil {
		t.Fatal("Reset did not clear reference")
	}
	// b 应已归还。
	e, err := pool.Get(1, 1, h264.YUV420P)
	if err != nil {
		t.Fatal(err)
	}
	if e != b {
		t.Fatal("Reset did not release reference")
	}
	e.Release()
}
