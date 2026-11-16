package decoder

import (
	h264 "github.com/cry5tallize/goavc"
	"testing"
)

func TestIntraBlock_LocalNeighborhoodAllocation(t *testing.T) {
	for _, format := range []h264.PixelFormat{h264.YUV420P, h264.YUV422P, h264.YUV444P, h264.YUV420P10LE, h264.YUV422P10LE, h264.YUV444P10LE} {
		fs := deblockTestState(t, 1, 1, format, true)
		var err error
		if n := testing.AllocsPerRun(100, func() {
			for c := range 3 {
				mode := 0
				if c == 0 || fs.sps.ChromaFormatIDC == 3 {
					mode = 2
				}
				err = fs.predictIntraBlock(0, 0, c, mode)
				if err != nil {
					return
				}
			}
		}); n != 0 || err != nil {
			t.Fatalf("%s局部帧内邻域分配=%v error=%v", format, n, err)
		}
	}
}
