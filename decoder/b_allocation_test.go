package decoder

import (
	"fmt"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/internal/dpb"
	"github.com/goavc/h264/internal/entropy/cabac"
	"github.com/goavc/h264/internal/entropy/cavlc"
	"github.com/goavc/h264/internal/motion"
	"github.com/goavc/h264/internal/picture"
	"github.com/goavc/h264/internal/transform"
	"github.com/goavc/h264/syntax"
)

type bAllocationGroup struct {
	x, y, w, h int
	mask, n    int
	direct     bool
}

// 分区向量来自表7-14与表7-18；不借用待测实现生成预期像素或参考方向。
func bAllocationGroups(typ int, sub [4]int) []bAllocationGroup {
	if typ == 0 {
		return []bAllocationGroup{{w: 4, h: 4, direct: true}}
	}
	if typ <= 3 {
		return []bAllocationGroup{{w: 4, h: 4, mask: typ, n: 1}}
	}
	if typ <= 21 {
		masks := [18][2]int{
			{1, 1}, {1, 1}, {2, 2}, {2, 2}, {1, 2}, {1, 2},
			{2, 1}, {2, 1}, {1, 3}, {1, 3}, {2, 3}, {2, 3},
			{3, 1}, {3, 1}, {3, 2}, {3, 2}, {3, 3}, {3, 3},
		}[typ-4]
		if typ%2 == 0 {
			return []bAllocationGroup{{w: 4, h: 2, mask: masks[0], n: 1}, {y: 2, w: 4, h: 2, mask: masks[1], n: 1}}
		}
		return []bAllocationGroup{{w: 2, h: 4, mask: masks[0], n: 1}, {x: 2, w: 2, h: 4, mask: masks[1], n: 1}}
	}
	masks := [13]int{0, 1, 2, 3, 1, 1, 2, 2, 3, 3, 1, 2, 3}
	counts := [13]int{0, 1, 1, 1, 2, 2, 2, 2, 2, 2, 4, 4, 4}
	groups := make([]bAllocationGroup, 4)
	for i, v := range sub {
		groups[i] = bAllocationGroup{x: i % 2 * 2, y: i / 2 * 2, w: 2, h: 2, mask: masks[v], n: counts[v], direct: v == 0}
	}
	return groups
}

// 只在计量前编码合法宏块体；正常解码仍走真实熵、双向MC与非零残差重建。
func bAllocationBody(t *testing.T, typ int, sub [4]int, groups []bAllocationGroup, entropy bool) ([]byte, uint64) {
	t.Helper()
	w := bitstream.NewWriter()
	var e *cabac.Encoder
	ctx, err := cabac.InitContexts(false, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if entropy {
		e, err = cabac.NewEncoder(w)
		if err != nil {
			t.Fatal(err)
		}
	}
	bin := func(index int, value int) { e.Bin(&ctx[index], uint8(value)) }
	if typ == 22 {
		for _, v := range sub {
			if !entropy {
				w.WriteUE(uint32(v))
				continue
			}
			if v == 0 {
				bin(36, 0)
				continue
			}
			bin(36, 1)
			if v <= 2 {
				bin(37, 0)
				bin(39, v-1)
				continue
			}
			bin(37, 1)
			if v >= 11 {
				bin(38, 1)
				bin(39, 1)
				bin(39, v-11)
				continue
			}
			if v >= 7 {
				bin(38, 1)
				bin(39, 0)
				v -= 4
			} else {
				bin(38, 0)
			}
			bin(39, (v-3)/2)
			bin(39, (v-3)%2)
		}
	}
	// 两个活动参考都真实存在；普通分区选择索引1，direct选择索引0。
	for l := range 2 {
		var positive [16]bool
		for _, g := range groups {
			if g.mask&(1<<l) != 0 {
				if entropy {
					inc := 0
					if g.x > 0 && positive[g.y*4+g.x-1] {
						inc++
					}
					if g.y > 0 && positive[(g.y-1)*4+g.x] {
						inc += 2
					}
					bin(54+inc, 1)
					bin(58, 0)
				} else {
					w.WriteBit(0)
				}
				for y := g.y; y < g.y+g.h; y++ {
					for x := g.x; x < g.x+g.w; x++ {
						positive[y*4+x] = true
					}
				}
			}
		}
	}
	for l := range 2 {
		for _, g := range groups {
			if g.mask&(1<<l) == 0 {
				continue
			}
			for range g.n {
				if entropy {
					bin(40, 0)
					bin(47, 0)
				} else {
					w.WriteSE(0)
					w.WriteSE(0)
				}
			}
		}
	}
	if entropy {
		bin(73, 1)
		bin(73, 0)
		bin(73, 0)
		bin(76, 0)
		bin(77, 0)
		bin(60, 0)
	} else {
		w.WriteUE(2) // 帧间CBP=1，第一组8×8含四个非零DC块。
		w.WriteSE(0)
	}
	for n := range 4 {
		var coeff [16]int32
		coeff[0] = 64
		if entropy {
			bin(93+n, 1)
			if _, err := e.Residual(&ctx, cabac.Luma4x4, coeff[:], transform.Zigzag4x4[:]); err != nil {
				t.Fatal(err)
			}
		} else {
			nc := 1
			if n == 0 {
				nc = 0
			}
			if _, err := cavlc.EncodeBlock(w, &coeff, cavlc.Luma4x4, nc); err != nil {
				t.Fatal(err)
			}
		}
	}
	bits := uint64(w.BitsWritten())
	if entropy {
		e.Terminate(1)
		if err := e.Err(); err != nil {
			t.Fatal(err)
		}
	} else {
		bitstream.WriteRBSPTrailingBits(w)
	}
	return w.Bytes(), bits
}

func TestBMacroblockPreparedAllocations(t *testing.T) {
	for _, depth := range []int{8, 10} {
		for _, entropy := range []bool{false, true} {
			for typ := range 23 {
				vectors := [][4]int{{}}
				if typ == 22 {
					vectors = append(vectors, [4]int{0, 12, 10, 11})
					for v := 1; v <= 12; v++ {
						vectors = append(vectors, [4]int{v, v, v, v})
					}
				}
				for _, sub := range vectors {
					for _, spatial := range []bool{false, true} {
						if spatial && typ != 0 && typ != 22 {
							continue
						}
						t.Run(fmt.Sprintf("depth%d_cabac%t_type%d_sub%v_spatial%t", depth, entropy, typ, sub, spatial), func(t *testing.T) {
							testBAllocationPrepared(t, depth, entropy, typ, sub, spatial)
						})
					}
				}
			}
		}
	}
}

func testBAllocationPrepared(t *testing.T, depth int, entropy bool, typ int, sub [4]int, spatial bool) {
	t.Helper()
	d := mustDecoder(t, h264.DecoderConfig{})
	sps := &syntax.SPS{ProfileIDC: 110, LevelIDC: 10, ChromaFormatIDC: 1, FrameMbsOnlyFlag: true,
		BitDepthLumaMinus8: uint32(depth - 8), BitDepthChromaMinus8: uint32(depth - 8), Direct8x8InferenceFlag: spatial}
	pps := &syntax.PPS{EntropyCodingModeFlag: entropy}
	fs, err := newFrameState(d, sps, pps, true)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.release()
	fs.poc = 2
	entry := sliceEntry{sps: sps, pps: pps, hdr: &syntax.SliceHeader{Type: syntax.SliceTypeB, DirectSpatialMVPredFlag: spatial}}
	scale := uint16(1 << (depth - 8))
	for l := range 2 {
		for r := range 2 {
			pic, err := picture.New(1, 1, fs.pic.Format)
			if err != nil {
				t.Fatal(err)
			}
			for c := range pic.Planes {
				pl := &pic.Planes[c]
				value := uint16(20+l*40+r*20+c*5) * scale
				for y := range pl.Height {
					for x := range pl.Width {
						pl.SetSample(x, y, value)
					}
				}
				pl.FillBorder()
			}
			entry.refs[l] = append(entry.refs[l], &dpb.Reference{Identity: &dpb.Identity{ID: uint64(1 + l*2 + r), POC: int32(l * 4)}, Picture: pic})
		}
	}
	col := entry.refs[1][0]
	col.Motion[0] = motion.NewGrid(1, 1)
	col.Motion[0].SetRect(0, 0, 4, 4, motion.MV{X: 4, Y: -4}, 0)
	col.BlockReferences[0] = make([]*dpb.Identity, 16)
	for i := range col.BlockReferences[0] {
		col.BlockReferences[0][i] = entry.refs[0][0].Identity
	}
	groups := bAllocationGroups(typ, sub)
	body, bodyBits := bAllocationBody(t, typ, sub, groups, entropy)
	entry.data = bitstream.NewReader(body)
	var initialEngine cabac.Decoder
	var initialContexts [cabac.ContextCount]cabac.Context
	if entropy {
		engine, err := cabac.New(entry.data)
		if err != nil {
			t.Fatal(err)
		}
		entry.cabac = &cabacSyntax{d: engine}
		initialEngine = *engine
		initialContexts, err = cabac.InitContexts(false, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
	}
	initialReader := *entry.data
	var decodeErr error
	var qp int
	run := func() {
		*entry.data = initialReader
		if entropy {
			*entry.cabac.d = initialEngine
			copy(entry.cabac.ctx[:], initialContexts[:])
			entry.cabac.lastDelta = false
		}
		fs.mv.Reset()
		fs.mv1.Reset()
		clear(fs.mvd)
		clear(fs.mvd1)
		clear(fs.directBlocks)
		clear(fs.lumaNZ)
		fs.info[0] = mbInfo{sliceID: 0}
		for c := range fs.pic.Planes {
			clear(fs.pic.Planes[c].Data)
			clear(fs.pic.Planes[c].Data16)
		}
		qp = -6 * (depth - 8)
		decodeErr = decodeBMacroblock(fs, 0, 0, 0, typ, &entry, &qp, false)
	}
	run()
	if decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if n := testing.AllocsPerRun(20, run); n != 0 {
		t.Fatalf("准备后的B宏块分配次数为%g，预期0", n)
	}
	if decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if entropy {
		if entry.cabac.d.Terminate() != 1 || entry.cabac.d.Err() != nil {
			t.Fatalf("CABAC宏块未正确读至终止位：%v", entry.cabac.d.Err())
		}
	} else if got := entry.data.BitsRead(); got != bodyBits {
		t.Fatalf("CAVLC读取位数为%d，预期%d", got, bodyBits)
	}
	if qp != -6*(depth-8) || fs.info[0].cbp != 1 || fs.info[0].skip {
		t.Fatalf("正常宏块的QP/CBP/skip错误：%d/%d/%t", qp, fs.info[0].cbp, fs.info[0].skip)
	}
	wantAllow8 := true
	for _, g := range groups {
		if g.n > 1 || g.direct && !sps.Direct8x8InferenceFlag {
			wantAllow8 = false
		}
	}
	if fs.info[0].allow8 != wantAllow8 || fs.info[0].direct != (typ == 0) {
		t.Fatalf("宏块的8×8变换/direct许可错误：%t/%t", fs.info[0].allow8, fs.info[0].direct)
	}
	for _, g := range groups {
		for y := g.y; y < g.y+g.h; y++ {
			for x := g.x; x < g.x+g.w; x++ {
				i := y*4 + x
				for l, grid := range [2]*motion.Grid{fs.mv, fs.mv1} {
					ref, mv := int8(-1), motion.MV{}
					if g.direct {
						ref = 0
						if !spatial {
							mv = [2]motion.MV{{X: 2, Y: -2}, {X: -2, Y: 2}}[l]
						}
					} else if g.mask&(1<<l) != 0 {
						ref = 1
						// 混合向量首组temporal-direct提供左/上邻居；零MVD仍须
						// 继承(±2,∓2)的MVP，而非零MV（Spec: 8.4.1.3）。
						if !spatial && typ == 22 && sub[0] == 0 {
							mv = [2]motion.MV{{X: 2, Y: -2}, {X: -2, Y: 2}}[l]
						}
					}
					gotMV, gotRef, ok := grid.Get(x, y)
					if !ok || gotRef != ref || gotMV != mv {
						t.Fatalf("列表%d块(%d,%d)运动为%v/%d/%t，预期%v/%d", l, x, y, gotMV, gotRef, ok, mv, ref)
					}
					if ref >= 0 && fs.referenceIDs[l][i] != entry.refs[l][ref].Identity {
						t.Fatalf("列表%d块%d参考身份错误", l, i)
					}
				}
				if fs.directBlocks[i] != g.direct {
					t.Fatalf("块%d的direct标记错误", i)
				}
			}
		}
		for c := range fs.pic.Planes {
			div := 1
			if c > 0 {
				div = 2
			}
			base := 60
			if g.direct {
				base = 40
			} else if g.mask == 1 {
				base = 40
			} else if g.mask == 2 {
				base = 80
			}
			for y := g.y * 4 / div; y < (g.y+g.h)*4/div; y++ {
				for x := g.x * 4 / div; x < (g.x+g.w)*4/div; x++ {
					want := uint16(base+c*5) * scale
					// QP'=0、level=64的纯DC反量化为640，4×4逆变换恒加10。
					if c == 0 && x < 8 && y < 8 {
						want += 10
					}
					if got := fs.pic.Planes[c].Sample(x, y); got != want {
						t.Fatalf("平面%d像素(%d,%d)为%d，预期%d", c, x, y, got, want)
					}
				}
			}
		}
	}
	for _, i := range []int{0, 1, 4, 5} {
		if fs.lumaNZ[i] != 1 {
			t.Fatalf("块%d未解出非零残差", i)
		}
	}
}
