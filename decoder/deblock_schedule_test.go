package decoder

import (
	"fmt"
	"slices"
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/internal/filter"
	"github.com/cry5tallize/goavc/internal/motion"
	"github.com/cry5tallize/goavc/internal/picture"
	"github.com/cry5tallize/goavc/syntax"
)

func deblockTestState(t testing.TB, w, h int, format h264.PixelFormat, intra bool) *frameState {
	t.Helper()
	pic, err := picture.New(w, h, format)
	if err != nil {
		t.Fatal(err)
	}
	sw, sh := format.ChromaSubsampling()
	depth := format.BitDepth()
	fs := &frameState{pic: pic, mbW: w, mbH: h, total: w * h, subW: sw, subH: sh,
		chromaW: 16 / sw, chromaH: 16 / sh, info: make([]mbInfo, w*h),
		lumaNZ: make([]uint8, w*h*16), mv: motion.NewGrid(w, h),
		pps: &syntax.PPS{ChromaQPIndexOffset: -2, SecondChromaQPIndexOffset: 5},
		sps: &syntax.SPS{
			BitDepthLumaMinus8: uint32(depth - 8), BitDepthChromaMinus8: uint32(depth - 8)}}
	// 六种原生格式的ChromaFormatIDC分别为1/2/3。
	if sw == 1 {
		fs.sps.ChromaFormatIDC = 3
	} else if sh == 1 {
		fs.sps.ChromaFormatIDC = 2
	} else {
		fs.sps.ChromaFormatIDC = 1
	}
	for i := range fs.info {
		fs.info[i] = mbInfo{intra: intra || i%7 == 0, pcm: i%13 == 0, qp: 20 + i%32,
			transform8: !intra && i%3 == 0, sliceID: i / 3,
			alphaDiv2: int8(i%5 - 2), betaDiv2: int8(i%3 - 1)}
		if !intra {
			if i%11 == 1 {
				fs.info[i].deblockIDC = 1
			} else if i%5 == 2 {
				fs.info[i].deblockIDC = 2
			}
		}
	}
	for by := range h * 4 {
		for bx := range w * 4 {
			fs.mv.SetRect(bx, by, 1, 1, motion.MV{X: int32(bx % 9), Y: int32(by % 7)}, 0)
		}
	}
	for i := range fs.lumaNZ {
		if i%19 == 0 {
			fs.lumaNZ[i] = 1
		}
	}
	for c := range pic.Planes {
		p := &pic.Planes[c]
		for y := range p.Height {
			for x := range p.Width {
				v := (90 + (x/4+y/4*3+c*7)%35) << (depth - 8)
				p.SetSample(x, y, uint16(v+(x+y)%(1<<(depth-8))))
			}
		}
	}
	return fs
}

// 保留优化前的调度作为逐像素回归参考；算术仍调用共享内核，独立规范值
// 与FFmpeg Oracle由filter测试及语料测试覆盖。这里只核对顺序、bS、QP和分段几何。
func deblockReference(fs *frameState) {
	for my := range fs.mbH {
		for mx := range fs.mbW {
			info := &fs.info[my*fs.mbW+mx]
			if info.deblockIDC == 1 {
				continue
			}
			for dir := range 2 {
				for edge := 0; edge < 16; edge += 4 {
					boundary := edge == 0
					if boundary && (dir == 0 && (mx == 0 || fs.skipSliceBoundary(mx, my, mx-1, my)) ||
						dir == 1 && (my == 0 || fs.skipSliceBoundary(mx, my, mx, my-1))) {
						continue
					}
					for comp := range 3 {
						if fs.sps.ChromaFormatIDC == 3 && comp != 0 {
							continue
						}
						if comp == 0 && info.transform8 && edge%8 != 0 {
							continue
						}
						sx, sy := 1, 1
						plane := fs.component
						if comp != 0 {
							sx, sy, plane = fs.subW, fs.subH, comp
						}
						acrossSub, alongSub := sx, sy
						if dir == 1 {
							acrossSub, alongSub = sy, sx
						}
						if edge%(4*acrossSub) != 0 {
							continue
						}
						p := &fs.pic.Planes[plane]
						for seg := range 4 {
							qx, qy, px, py := mx*4+edge/4, my*4+seg, mx*4+edge/4-1, my*4+seg
							x, y, along, across := (mx*16+edge)/sx, (my*16+seg*4)/sy, p.Stride, 1
							if dir == 1 {
								qx, qy, px, py = mx*4+seg, my*4+edge/4, mx*4+seg, my*4+edge/4-1
								x, y, along, across = (mx*16+seg*4)/sx, (my*16+edge)/sy, 1, p.Stride
							}
							bs := fs.bSAtBlocks(px, py, qx, qy, boundary)
							if bs == 0 {
								continue
							}
							if comp == 0 {
								qp := fs.edgeQP(mx, my, boundary, dir == 0 && boundary)
								if p.BitDepth == 10 {
									filter.LumaEdgeHigh(p.Data16, p.Index(x, y), along, across, bs, qp, int(info.alphaDiv2), int(info.betaDiv2), 10)
								} else {
									filter.LumaEdge(p.Data, p.Index(x, y), along, across, bs, qp, int(info.alphaDiv2), int(info.betaDiv2))
								}
							} else {
								qp := fs.edgeChromaQP(mx, my, boundary, dir == 0 && boundary, comp-1)
								if p.BitDepth == 10 {
									filter.ChromaEdgeHigh(p.Data16, p.Index(x, y), along, across, bs, qp, int(info.alphaDiv2), int(info.betaDiv2), 4/alongSub, 10)
								} else {
									filter.ChromaEdge(p.Data, p.Index(x, y), along, across, bs, qp, int(info.alphaDiv2), int(info.betaDiv2), 4/alongSub)
								}
							}
						}
					}
				}
			}
		}
	}
}

func TestDeblock_PreparedScheduleParity(t *testing.T) {
	for _, format := range []h264.PixelFormat{h264.YUV420P, h264.YUV422P, h264.YUV444P, h264.YUV420P10LE, h264.YUV422P10LE, h264.YUV444P10LE} {
		for _, intra := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/intra=%v", format, intra), func(t *testing.T) {
				got, want := deblockTestState(t, 3, 3, format, intra), deblockTestState(t, 3, 3, format, intra)
				for comp := range 3 {
					if comp > 0 && format != h264.YUV444P && format != h264.YUV444P10LE {
						break
					}
					got.component, want.component = comp, comp
					if err := got.deblockPlane(); err != nil {
						t.Fatal(err)
					}
					deblockReference(want)
				}
				for c := range 3 {
					if !slices.Equal(got.pic.Planes[c].Data, want.pic.Planes[c].Data) || !slices.Equal(got.pic.Planes[c].Data16, want.pic.Planes[c].Data16) {
						t.Fatalf("平面%d与原调度不一致", c)
					}
				}
			})
		}
	}
}

func BenchmarkDeblock_1080p_Auto(b *testing.B) {
	for _, intra := range []bool{true, false} {
		b.Run(fmt.Sprintf("intra=%v", intra), func(b *testing.B) {
			fs := deblockTestState(b, 120, 68, h264.YUV420P, intra)
			var initial [3][]byte
			for c := range initial {
				initial[c] = slices.Clone(fs.pic.Planes[c].Data)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				// 重置计入计时，避免反复过滤同一帧而改变条件命中率。
				for c := range initial {
					copy(fs.pic.Planes[c].Data, initial[c])
				}
				if err := fs.deblockPlane(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
