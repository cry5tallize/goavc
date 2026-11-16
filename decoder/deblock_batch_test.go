package decoder

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/internal/dpb"
	"github.com/cry5tallize/goavc/internal/motion"
)

func deblockMotionFixture(t testing.TB, w, h int, bFrame, nz, transform8 bool) *frameState {
	fs := deblockTestState(t, w, h, h264.YUV420P, false)
	t.Cleanup(fs.pic.Release)
	for i := range fs.info {
		fs.info[i] = mbInfo{qp: 32, transform8: transform8}
	}
	clear(fs.lumaNZ)
	if nz {
		for i := range fs.lumaNZ {
			if i%11 == 0 {
				fs.lumaNZ[i] = 1
			}
		}
	}
	ids := [2]*dpb.Identity{{ID: 1}, {ID: 2}}
	n := fs.mv.BlockW * fs.mv.BlockH
	fs.referenceIDs[0] = make([]*dpb.Identity, n)
	if bFrame {
		fs.mv1 = motion.NewGrid(w, h)
		fs.referenceIDs[1] = make([]*dpb.Identity, n)
	}
	for y := range fs.mv.BlockH {
		for x := range fs.mv.BlockW {
			i := y*fs.mv.BlockW + x
			fs.mv.SetRect(x, y, 1, 1, motion.MV{X: int32(x % 5), Y: int32(y % 5)}, 0)
			fs.referenceIDs[0][i] = ids[x/4%2]
			if bFrame {
				fs.mv1.SetRect(x, y, 1, 1, motion.MV{X: -int32(x % 5), Y: -int32(y % 5)}, 1)
				fs.referenceIDs[1][i] = ids[1-x/4%2]
			}
		}
	}
	return fs
}

func edgeStrengthsReference(fs *frameState, mx, my, edge int, vertical bool) (out [4]uint8) {
	for seg := range 4 {
		px, py := mx*4+edge/4-1, my*4+seg
		qx, qy := px+1, py
		if !vertical {
			px, py = mx*4+seg, my*4+edge/4-1
			qx, qy = px, py+1
		}
		out[seg] = uint8(fs.bSAtBlocks(px, py, qx, qy, edge == 0))
	}
	return
}

func TestDeblock_EdgeStrengthReferenceParity(t *testing.T) {
	rng := rand.New(rand.NewPCG(0xa06, 0xb5))
	for _, bFrame := range []bool{false, true} {
		fs := deblockMotionFixture(t, 3, 3, bFrame, false, false)
		identityBuffer := fs.referenceIDs[0]
		ids := [3]*dpb.Identity{nil, {ID: 1}, {ID: 2}}
		for iteration := range 128 {
			fs.referenceIDs[0] = identityBuffer
			for i := range fs.info {
				fs.info[i] = mbInfo{intra: rng.IntN(13) == 0, pcm: rng.IntN(19) == 0, transform8: rng.IntN(2) == 0}
			}
			for i := range fs.lumaNZ {
				fs.lumaNZ[i] = 0
				if iteration%3 == 1 && rng.IntN(7) == 0 {
					fs.lumaNZ[i] = 1
				}
			}
			fs.mv.Reset()
			if bFrame {
				fs.mv1.Reset()
			}
			for y := range fs.mv.BlockH {
				for x := range fs.mv.BlockW {
					i := y*fs.mv.BlockW + x
					if rng.IntN(5) != 0 {
						fs.mv.SetRect(x, y, 1, 1, motion.MV{X: int32(rng.IntN(9) - 4), Y: int32(rng.IntN(9) - 4)}, int8(rng.IntN(3)-1))
					}
					fs.referenceIDs[0][i] = ids[rng.IntN(3)]
					if bFrame {
						if rng.IntN(5) != 0 {
							fs.mv1.SetRect(x, y, 1, 1, motion.MV{X: int32(rng.IntN(9) - 4), Y: int32(rng.IntN(9) - 4)}, int8(rng.IntN(3)-1))
						}
						fs.referenceIDs[1][i] = ids[rng.IntN(3)]
					}
				}
			}
			var owners []int
			if !bFrame && iteration%5 == 0 {
				fs.referenceIDs[0] = nil
			}
			if iteration%4 == 1 {
				fs.mv.BeginSlice(4)
				if bFrame {
					fs.mv1.BeginSlice(2)
				}
			}
			if iteration%4 == 2 {
				owners = []int{0, 1, 0, 1, 0, 1, 0, 1, 0}
				fs.mv.BeginMappedSlice(owners, 1)
				if bFrame {
					fs.mv1.BeginMappedSlice(owners, 0)
				}
			}
			for my := range 3 {
				for mx := range 3 {
					for _, vertical := range []bool{true, false} {
						for edge := 0; edge < 16; edge += 4 {
							if edge == 0 && (vertical && mx == 0 || !vertical && my == 0) {
								continue
							}
							got := fs.edgeStrengths(mx, my, edge, vertical)
							want := edgeStrengthsReference(fs, mx, my, edge, vertical)
							if got != want {
								t.Fatalf("B%t iter%d mb%d,%d edge%d vertical%t got%v want%v", bFrame, iteration, mx, my, edge, vertical, got, want)
							}
						}
					}
				}
			}
		}
	}
}

func TestDeblock_ReferenceSwapEmptyAndFieldParity(t *testing.T) {
	fs := deblockMotionFixture(t, 3, 3, true, false, false)
	a, b := &dpb.Identity{ID: 11}, &dpb.Identity{ID: 12}
	for y := 4; y < 8; y++ {
		pi, qi := y*fs.mv.BlockW+3, y*fs.mv.BlockW+4
		fs.referenceIDs[0][pi], fs.referenceIDs[1][pi] = a, b
		fs.referenceIDs[0][qi], fs.referenceIDs[1][qi] = b, a
		fs.mv.SetRect(3, y, 1, 1, motion.MV{X: 4}, 0)
		fs.mv1.SetRect(3, y, 1, 1, motion.MV{X: -4}, 1)
		fs.mv.SetRect(4, y, 1, 1, motion.MV{X: -4}, 0)
		fs.mv1.SetRect(4, y, 1, 1, motion.MV{X: 4}, 1)
	}
	if got := fs.edgeStrengths(1, 1, 0, true); got != ([4]uint8{}) {
		t.Fatal("交换列表不应产生bS", got)
	}
	fs.mv1.SetRect(4, 5, 1, 1, motion.MV{X: 8}, 1)
	if got := fs.edgeStrengths(1, 1, 0, true); got != ([4]uint8{0, 1, 0, 0}) {
		t.Fatal("MV阈值4未保留", got)
	}
	clear(fs.referenceIDs[0])
	clear(fs.referenceIDs[1])
	if got := fs.edgeStrengths(1, 1, 0, true); got != ([4]uint8{}) {
		t.Fatal("空参考不应比较MV", got)
	}
	for _, flags := range [][2]bool{{true, false}, {false, true}} {
		fs.field, fs.mbaff = flags[0], flags[1]
		if got, want := fs.edgeStrengths(1, 1, 0, true), edgeStrengthsReference(fs, 1, 1, 0, true); got != want {
			t.Fatal("旧几何fallback不一致")
		}
	}
}

func TestDeblock_BFramePreparedScheduleParity(t *testing.T) {
	got, want := deblockMotionFixture(t, 3, 3, true, true, true), deblockMotionFixture(t, 3, 3, true, true, true)
	if err := got.deblockPlane(); err != nil {
		t.Fatal(err)
	}
	deblockReference(want)
	for c := range 3 {
		if !slices.Equal(got.pic.Planes[c].Data, want.pic.Planes[c].Data) {
			t.Fatalf("B帧平面%d或调度不一致", c)
		}
	}
}

var edgeStrengthBenchmarkSink [4]uint8

func BenchmarkEdgeStrengths_1080p_Auto(b *testing.B) {
	for _, mode := range []struct {
		name      string
		b, nz, tx bool
	}{{"p", false, false, false}, {"b", true, false, false}, {"b_nz", true, true, false}, {"b_transform8", true, true, true}} {
		for _, vertical := range []bool{true, false} {
			b.Run(fmt.Sprintf("%s/vertical%t", mode.name, vertical), func(b *testing.B) {
				fs := deblockMotionFixture(b, 120, 68, mode.b, mode.nz, mode.tx)
				mx, my, edge := 1, 1, 0
				b.ReportAllocs()
				for b.Loop() {
					edgeStrengthBenchmarkSink = fs.edgeStrengths(mx, my, edge, vertical)
					edge += 4
					if edge == 16 {
						edge = 0
						mx++
						if mx == fs.mbW {
							mx = 1
							my++
							if my == fs.mbH {
								my = 1
							}
						}
					}
				}
			})
		}
	}
}
