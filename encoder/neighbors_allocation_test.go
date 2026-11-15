package encoder

import (
	"testing"

	h264 "github.com/goavc/h264"
)

func TestNeighbors_ScratchLifetimeAndUnavailable(t *testing.T) {
	for _, format := range []h264.PixelFormat{h264.YUV420P, h264.YUV420P10LE, h264.YUV422P, h264.YUV422P10LE, h264.YUV444P, h264.YUV444P10LE} {
		for _, field := range []bool{false, true} {
			s, _ := preparedResidual(t, format, field, false, false)
			limit := (1 << format.BitDepth()) - 1
			value := func(c, x, y int) uint16 { return uint16((67*c + 19*x + 31*y + 3) & limit) }
			for c := range 3 {
				p := &s.pic.Planes[c]
				for y := range p.Height {
					for x := range p.Width {
						p.SetSample(x, y, value(c, x, y))
					}
				}
			}
			var scratch, other blockSamplesScratch
			for _, start := range []int{0, 1, 2, 3} {
				s.info[3].start = start
				for c := range 3 {
					w, h := s.planeMBSize(c)
					a, top, left, tl := s.blockSamples(3, c, &scratch)
					if top != (start <= 1) || left != (start <= 2) || tl != (start == 0) || len(a.Top) != w || len(a.Left) != h {
						t.Fatalf("%s field=%v start=%d comp=%d: 邻域几何/可用性改变", format, field, start, c)
					}
					// 另一组仍存活的邻域必须用另一槽，不得覆盖第一组的Top/Left。
					s.blockSamples(3, (c+1)%3, &other)
					for i, got := range a.Top {
						want := uint16(0)
						if top {
							want = value(c, w+i, h-1)
						}
						if got != want {
							t.Fatalf("top[%d]=%d, want %d", i, got, want)
						}
					}
					for i, got := range a.Left {
						want := uint16(0)
						if left {
							want = value(c, w-1, h+i)
						}
						if got != want {
							t.Fatalf("left[%d]=%d, want %d", i, got, want)
						}
					}
					wantTL := uint16(0)
					if tl {
						wantTL = value(c, w-1, h-1)
					}
					if a.TopLeft != wantTL {
						t.Fatal("左上值改变")
					}
				}
			}
		}
	}
}
