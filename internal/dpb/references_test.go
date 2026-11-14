package dpb

import (
	"errors"
	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/internal/picture"
	"github.com/goavc/h264/syntax"
	"testing"
)

func testReference(t *testing.T, d *DPB, pool *picture.Pool, fn uint32) *Reference {
	t.Helper()
	pic, err := pool.Get(1, 1, h264.YUV420P)
	if err != nil {
		t.Fatal(err)
	}
	return d.NewReference(pic, fn, int32(fn*2))
}

func TestReferences_DefaultListsAndSlidingWindow(t *testing.T) {
	d := New(16, false)
	d.SetCapacity(3)
	pool := picture.NewPool(4)
	defer d.Reset()
	for fn := uint32(0); fn < 4; fn++ {
		if err := d.Store(testReference(t, d, pool, fn), fn == 0, false, false, nil); err != nil {
			t.Fatal(err)
		}
	}
	lists := d.Lists(4, 8, false)
	for i, fn := range []uint32{3, 2, 1} {
		if lists[0][i].FrameNum != fn {
			t.Fatal("wrong PicNum order")
		}
	}
	b := d.Lists(4, 3, true)
	for l, want := range [][]uint32{{1, 2, 3}, {2, 3, 1}} {
		for i, fn := range want {
			if b[l][i].FrameNum != fn {
				t.Fatalf("B list %d: %+v", l, b[l])
			}
		}
	}
	modified, err := d.Modify(lists[0], 4, []syntax.RefListModification{{IDC: 0, Value: 1}, {IDC: 0, Value: 0}}, 3)
	if err != nil {
		t.Fatal(err)
	}
	for i, fn := range []uint32{2, 1, 3} {
		if modified[i].FrameNum != fn {
			t.Fatal("wrong modification order")
		}
	}
	gets, puts, _ := pool.Stats()
	if gets != 4 || puts != 1 {
		t.Fatalf("sliding ownership %d/%d", gets, puts)
	}
	d.Reset()
	gets, puts, _ = pool.Stats()
	if gets != puts {
		t.Fatal("DPB leaked pictures")
	}
}

func TestReferences_MMCOOneThroughSix(t *testing.T) {
	d := New(16, false)
	d.SetCapacity(3)
	pool := picture.NewPool(4)
	defer d.Reset()
	store := func(fn uint32, ops ...syntax.MMCO) {
		t.Helper()
		r := testReference(t, d, pool, fn)
		if err := d.Store(r, fn == 0, false, len(ops) > 0, ops); err != nil {
			r.Picture.Release()
			t.Fatal(err)
		}
	}
	store(0)
	store(1, syntax.MMCO{Operation: 4, MaxLongTermFrameIdxPlus1: 2}, syntax.MMCO{Operation: 3, DifferenceOfPicNumsMinus1: 0, LongTermFrameIdx: 1})
	store(2, syntax.MMCO{Operation: 1, DifferenceOfPicNumsMinus1: 0}, syntax.MMCO{Operation: 6, LongTermFrameIdx: 0})
	list := d.Lists(3, 6, false)[0]
	if len(list) != 2 || list[0].FrameNum != 2 || list[0].LongTermIdx != 0 || list[1].FrameNum != 0 || list[1].LongTermIdx != 1 {
		t.Fatal("MMCO 1/3/4/6 list")
	}
	store(3, syntax.MMCO{Operation: 2, LongTermPicNum: 1}, syntax.MMCO{Operation: 4, MaxLongTermFrameIdxPlus1: 1})
	if len(d.references) != 2 || d.references[0].FrameNum != 2 || !d.references[0].LongTerm {
		t.Fatal("MMCO 2/4")
	}
	store(4, syntax.MMCO{Operation: 5})
	if len(d.references) != 1 || d.references[0].FrameNum != 0 || d.references[0].LongTerm || d.maxLongTermIdx != -1 {
		t.Fatal("MMCO 5")
	}
	cur := testReference(t, d, pool, 1)
	if err := d.Store(cur, false, false, true, []syntax.MMCO{{Operation: 2, LongTermPicNum: 5}}); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal(err)
	}
	cur.Picture.Release()
	d.Reset()
	gets, puts, _ := pool.Stats()
	if gets != puts {
		t.Fatal("error path ownership leak")
	}
}

func TestReferences_ReorderingAcrossFrameNumWrap(t *testing.T) {
	d := New(16, false)
	d.SetCapacity(3)
	pool := picture.NewPool(3)
	defer d.Reset()
	for _, fn := range []uint32{14, 15, 0} {
		if err := d.Store(testReference(t, d, pool, fn), false, false, false, nil); err != nil {
			t.Fatal(err)
		}
	}
	list := d.Lists(1, 34, false)[0]
	for i, fn := range []uint32{0, 15, 14} {
		if list[i].FrameNum != fn {
			t.Fatal("wrapped sort")
		}
	}
	list, err := d.Modify(list, 1, []syntax.RefListModification{{IDC: 0, Value: 1}}, 3)
	if err != nil || list[0].FrameNum != 15 {
		t.Fatal("wrapped reordering", err)
	}
}
