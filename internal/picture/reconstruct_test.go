package picture

import "testing"

func TestAddResidual4x4_ClipAndStride(t *testing.T) {
	p, _ := New(1, 1)
	var residual [16]int32
	for i := range residual {
		p.Planes[0].SetPix(3+i%4, 5+i/4, 120)
		residual[i] = int32(i*50 - 300)
	}
	AddResidual4x4(&p.Planes[0], 3, 5, &residual)
	for i, v := range residual {
		if got := p.Planes[0].Pix(3+i%4, 5+i/4); got != byte(max(0, min(255, 120+v))) {
			t.Fatal(i, got)
		}
	}
}
