package motion

import (
	"testing"
)

func newTestGrid() *Grid {
	g := NewGrid(2, 2) // 8x8 个 4x4 块
	g.Reset()
	return g
}

func TestGrid_SetGetAvailable(t *testing.T) {
	g := newTestGrid()
	if g.Available(0, 0) {
		t.Fatal("zero grid should have no available blocks")
	}
	g.SetRect(1, 1, 2, 2, MV{X: 3, Y: -4}, 0)
	for y := 1; y <= 2; y++ {
		for x := 1; x <= 2; x++ {
			mv, ref, ok := g.Get(x, y)
			if !ok || mv != (MV{3, -4}) || ref != 0 {
				t.Fatalf("Get(%d,%d) = %v,%d,%v", x, y, mv, ref, ok)
			}
		}
	}
	g.Reset()
	if g.Available(1, 1) {
		t.Fatal("Reset did not clear availability")
	}
}

func TestPredict_NoNeighbors(t *testing.T) {
	g := newTestGrid()
	if got := Predict(g, 0, 0, 4, 0); got != (MV{}) {
		t.Fatalf("predict = %v, want zero", got)
	}
}

func TestPredict_LeftOnly(t *testing.T) {
	g := newTestGrid()
	g.SetRect(0, 1, 1, 1, MV{8, 0}, 0)
	// 分区的左边是 (0,1)，上方与右上越界。
	if got := Predict(g, 1, 1, 2, 0); got != (MV{8, 0}) {
		t.Fatalf("predict = %v, want left A", got)
	}
}

func TestPredict_Median(t *testing.T) {
	g := newTestGrid()
	// 分区原点 (2,2)，partW=2：A=(1,2), B=(2,1), C=(4,1)。
	g.SetRect(1, 2, 1, 1, MV{1, 0}, 0)
	g.SetRect(2, 1, 1, 1, MV{10, 0}, 0)
	g.SetRect(4, 1, 1, 1, MV{100, 0}, 0)
	if got := Predict(g, 2, 2, 2, 0); got != (MV{10, 0}) {
		t.Fatalf("median = %v, want 10", got)
	}
}

func TestPredict_DiagonalFallback(t *testing.T) {
	g := newTestGrid()
	// 分区原点 (4,1)，partW=2：C=(6,0) 越界，回退 D=(3,0)。
	g.SetRect(3, 0, 1, 1, MV{7, 9}, 0)
	// A/B 不可用，只有 C（来自 D）可用且参考一致 -> 取 C。
	if got := Predict(g, 4, 1, 2, 0); got != (MV{7, 9}) {
		t.Fatalf("fallback = %v, want D", got)
	}
}

func TestPredict16x8_Special(t *testing.T) {
	g := newTestGrid()
	g.SetRect(2, 1, 4, 1, MV{5, 6}, 0) // B for upper partition at (2,2)
	if got := Predict16x8(g, 2, 2, false, 0); got != (MV{5, 6}) {
		t.Fatalf("upper = %v, want B", got)
	}
	g.SetRect(1, 2, 1, 2, MV{-3, 4}, 0) // A for lower partition
	if got := Predict16x8(g, 2, 2, true, 0); got != (MV{-3, 4}) {
		t.Fatalf("lower = %v, want A", got)
	}
}

func TestPredictSkip_ZeroShortcut(t *testing.T) {
	g := newTestGrid()
	// 左邻块为零 MV 且参考一致：即使上方/右上非零，也应直接取零。
	g.SetRect(1, 2, 1, 1, MV{0, 0}, 0)
	g.SetRect(2, 1, 4, 1, MV{100, 100}, 0)
	if got := PredictSkip(g, 2, 2, 0); got != (MV{}) {
		t.Fatalf("skip = %v, want zero", got)
	}
	// 左邻不存在时按普通预测走。
	if got := PredictSkip(g, 2, 0, 0); got != (MV{}) {
		t.Fatalf("skip corner = %v, want zero", got)
	}
}

func TestMV_Validate(t *testing.T) {
	if err := (MV{X: MaxAbsMV, Y: -MaxAbsMV}).Validate(); err != nil {
		t.Fatalf("boundary MV rejected: %v", err)
	}
	if err := (MV{X: MaxAbsMV + 1, Y: 0}).Validate(); err == nil {
		t.Fatal("out-of-range MV accepted")
	}
}
