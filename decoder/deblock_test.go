package decoder

import (
	"github.com/cry5tallize/goavc/internal/dpb"
	"github.com/cry5tallize/goavc/internal/motion"
	"github.com/cry5tallize/goavc/syntax"
	"testing"
)

func TestDeblock_StrengthAndSliceControl(t *testing.T) {
	fs := &frameState{mbW: 2, mbH: 1, total: 2, info: make([]mbInfo, 2), lumaNZ: make([]uint8, 32), mv: motion.NewGrid(2, 1)}
	fs.mv.SetRect(0, 0, 8, 4, motion.MV{}, 0)
	if got := fs.bSAtBlocks(3, 0, 4, 0, true); got != 0 {
		t.Fatal(got)
	}
	fs.mv.SetRect(4, 0, 1, 1, motion.MV{X: 4}, 0)
	if got := fs.bSAtBlocks(3, 0, 4, 0, true); got != 1 {
		t.Fatal(got)
	}
	fs.lumaNZ[16] = 1
	if got := fs.bSAtBlocks(3, 0, 4, 0, true); got != 2 {
		t.Fatal(got)
	}
	fs.info[1].intra = true
	if got := fs.bSAtBlocks(3, 0, 4, 0, true); got != 4 {
		t.Fatal(got)
	}
	if got := fs.bSAtBlocks(4, 0, 5, 0, false); got != 3 {
		t.Fatal(got)
	}
	fs.info[1].deblockIDC = 2
	fs.info[1].sliceID = 1
	if !fs.skipSliceBoundary(1, 0, 0, 0) {
		t.Fatal("must skip cross-slice edge")
	}
	fs.info[1].deblockIDC = 0
	if fs.skipSliceBoundary(1, 0, 0, 0) {
		t.Fatal("must filter cross-slice edge")
	}
}

func TestDeblock_BReferenceSwap(t *testing.T) {
	fs := &frameState{mbW: 2, total: 2, info: make([]mbInfo, 2), lumaNZ: make([]uint8, 32), mv: motion.NewGrid(2, 1), mv1: motion.NewGrid(2, 1)}
	fs.referenceIDs[0] = make([]*dpb.Identity, 32)
	fs.referenceIDs[1] = make([]*dpb.Identity, 32)
	a, b := &dpb.Identity{ID: 1}, &dpb.Identity{ID: 2}
	fs.referenceIDs[0][3], fs.referenceIDs[1][3] = a, b
	fs.referenceIDs[0][4], fs.referenceIDs[1][4] = b, a
	fs.mv.SetRect(3, 0, 1, 1, motion.MV{X: 4}, 0)
	fs.mv1.SetRect(3, 0, 1, 1, motion.MV{X: -4}, 1)
	fs.mv.SetRect(4, 0, 1, 1, motion.MV{X: -4}, 0)
	fs.mv1.SetRect(4, 0, 1, 1, motion.MV{X: 4}, 1)
	if got := fs.bSAtBlocks(3, 0, 4, 0, true); got != 0 {
		t.Fatal("swapped references must match", got)
	}
	fs.mv1.SetRect(4, 0, 1, 1, motion.MV{X: 8}, 1)
	if got := fs.bSAtBlocks(3, 0, 4, 0, true); got != 1 {
		t.Fatal("MV difference must raise strength", got)
	}
	fs.referenceIDs[0][4], fs.referenceIDs[1][4] = a, b
	fs.mv.SetRect(4, 0, 1, 1, motion.MV{X: 4}, 0)
	fs.mv1.SetRect(4, 0, 1, 1, motion.MV{X: -4}, 1)
	if got := fs.bSAtBlocks(3, 0, 4, 0, true); got != 0 {
		t.Fatal("normal pair must match", got)
	}
}

func TestDeblock_ChromaQPPerComponent(t *testing.T) {
	fs := &frameState{mbW: 2, info: []mbInfo{{qp: 30}, {qp: 32}}, pps: &syntax.PPS{SecondChromaQPIndexOffset: 6}}
	// 先映射再平均：Cb=(29+31+1)/2=30；Cr=(34+35+1)/2=35。
	if got := fs.edgeChromaQP(1, 0, true, true, 0); got != 30 {
		t.Fatal(got)
	}
	if got := fs.edgeChromaQP(1, 0, true, true, 1); got != 35 {
		t.Fatal(got)
	}
}
