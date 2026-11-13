package prediction

import "testing"

func TestWeightSample_AnalyticVectors(t *testing.T) {
	for _, c := range []struct {
		s    byte
		w, o int32
		d    uint32
		want byte
	}{{40, 3, -2, 1, 58}, {41, 1, 0, 1, 21}, {10, -3, 50, 0, 20}, {200, 2, 0, 0, 255}, {255, -1, 0, 0, 0}} {
		if got := WeightSample(c.s, c.w, c.o, c.d); got != c.want {
			t.Fatalf("%+v got %d", c, got)
		}
	}
	if got := BiWeightSample(10, 20, 1, 1, 0, 0, 0); got != 15 {
		t.Fatal(got)
	}
	if got := BiWeightSample(10, 20, 3, 1, 2, 4, 1); got != 16 {
		t.Fatal(got)
	}
}

func TestImplicitWeights_POCGeometry(t *testing.T) {
	for _, c := range []struct {
		p, p0, p1, w0, w1 int32
		long              bool
	}{{2, 0, 8, 48, 16, false}, {4, 0, 8, 32, 32, false}, {6, 0, 8, 16, 48, false}, {4, 0, 0, 32, 32, false}, {4, 0, 8, 32, 32, true}, {100, 0, 8, 32, 32, false}} {
		a, b := ImplicitWeights(c.p, c.p0, c.p1, c.long, false)
		if a != c.w0 || b != c.w1 {
			t.Fatalf("%+v got %d,%d", c, a, b)
		}
	}
}
