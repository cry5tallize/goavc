package encoder

import (
	"github.com/goavc/h264/bitstream"
	"testing"
)

func TestRDCost_Monotonicity(t *testing.T) {
	var previous uint64
	for qp := 0; qp <= 51; qp++ {
		l := lambdaQ8(qp)
		if l == 0 || qp > 0 && l <= previous {
			t.Fatal(qp, l, previous)
		}
		previous = l
		root := motionLambdaQ8(qp)
		n := l * 256
		if root*root > n || (root+1)*(root+1) <= n {
			t.Fatal("integer lambda root", qp, root, n)
		}
		if rdCost(qp, 101, 4) <= rdCost(qp, 100, 4) || rdCost(qp, 100, 5) <= rdCost(qp, 100, 4) {
			t.Fatal(qp)
		}
	}
}
func TestRateEstimate_ExactGolomb(t *testing.T) {
	for _, value := range []int32{-32768, -51, -26, -2, -1, 0, 1, 2, 25, 51, 32768} {
		w := bitstream.NewWriter()
		if err := w.WriteSE(value); err != nil {
			t.Fatal(err)
		}
		if got := seBits(value); got != int(w.BitsWritten()) {
			t.Fatal(value, got, w.BitsWritten())
		}
	}
	for _, pair := range [][3]int32{{0, 0, 2}, {1, -1, 6}, {2, 0, 6}} {
		if mvBits(pair[0], pair[1]) != int(pair[2]) {
			t.Fatal(pair)
		}
	}
}
