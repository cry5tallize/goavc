package filter

import "testing"

func TestChromaEdge_WeakKnownValues(t *testing.T) {
	pix := []byte{10, 10, 20, 20}
	// q0 在下标 2；p1=10,p0=10,q1=20；qp=51,bS=1 -> tc=14，delta=4。
	ChromaEdge(pix, 2, 1, 1, 1, 51, 0, 0, 1)
	if pix[1] != 14 || pix[2] != 16 {
		t.Fatalf("chroma weak p0/q0 = %d/%d, want 14/16", pix[1], pix[2])
	}
}

func TestChromaEdge_StrongKnownValues(t *testing.T) {
	pix := []byte{10, 10, 20, 20}
	// q0 在下标 2；p1=10,p0=10,q0=20,q1=20。
	ChromaEdge(pix, 2, 1, 1, 4, 51, 0, 0, 1)
	if pix[1] != 13 || pix[2] != 18 {
		t.Fatalf("chroma strong p0/q0 = %d/%d, want 13/18", pix[1], pix[2])
	}
}

func TestChromaEdge_ZeroBS(t *testing.T) {
	pix := []byte{10, 10, 20, 20}
	orig := append([]byte(nil), pix...)
	ChromaEdge(pix, 2, 1, 1, 0, 51, 0, 0, 1)
	for i := range pix {
		if pix[i] != orig[i] {
			t.Fatalf("bS=0 modified pixel %d", i)
		}
	}
}
