package picture

import (
	"testing"

	h264 "github.com/cry5tallize/goavc"
)

func TestAddResidual8x8_BitDepthClipAndNeighbors(t *testing.T) {
	for _, format := range []h264.PixelFormat{h264.YUV420P, h264.YUV420P10LE} {
		p, err := New(1, 1, format)
		if err != nil {
			t.Fatal(err)
		}
		var residual [64]int32
		for i := range residual {
			p.Planes[0].SetSample(3+i%8, 2+i/8, 200)
			residual[i] = int32(i*40 - 400)
		}
		residual[0], residual[63] = -1<<31, 1<<31-1
		p.Planes[0].SetSample(2, 2, 123)
		AddResidual8x8(&p.Planes[0], 3, 2, &residual)
		limit := int64(255)
		if format == h264.YUV420P10LE {
			limit = 1023
		}
		for i, v := range residual {
			want := uint16(max(0, min(limit, int64(200)+int64(v))))
			if got := p.Planes[0].Sample(3+i%8, 2+i/8); got != want {
				t.Fatalf("%s sample%d=%d want%d", format, i, got, want)
			}
		}
		if p.Planes[0].Sample(2, 2) != 123 {
			t.Fatal("neighbor overwritten")
		}
		p.Release()
	}
}
