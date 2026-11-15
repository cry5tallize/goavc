package decoder

import (
	"errors"
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

type preparedPPartitionCase struct {
	name   string
	pType  int
	sub    [4]uint32
	parts  int
	allow8 bool
}

func (tc preparedPPartitionCase) groups() int {
	if tc.pType == 0 {
		return 1
	}
	if tc.pType < 3 {
		return 2
	}
	return 4
}

func (tc preparedPPartitionCase) reference(bx, by int) int {
	switch tc.pType {
	case 1:
		return by / 2
	case 2:
		return bx / 2
	case 3:
		return bx / 2
	default:
		return 0
	}
}

// 语料直接写规范语法，不借用编码器；每个宏块都有非零亮度残差。
func preparedPPartitionPayload(t *testing.T, tc preparedPPartitionCase, arithmetic bool) ([]byte, uint64) {
	t.Helper()
	w := bitstream.NewWriter()
	var dc [16]int32
	dc[0] = 1
	if !arithmetic {
		w.WriteUE(uint32(tc.pType))
		if tc.pType >= 3 {
			for _, typ := range tc.sub {
				w.WriteUE(typ)
			}
		}
		if tc.pType != 4 {
			for g := range tc.groups() {
				w.WriteBits(1, uint64(1-g%2))
			}
		}
		for range tc.parts {
			var v int32
			if tc.pType == 0 {
				v = 8
			}
			w.WriteSE(v)
			w.WriteSE(v)
		}
		w.WriteUE(2) // 帧间CBP=1，只编码左上8×8亮度组。
		w.WriteSE(0)
		for block := range 4 {
			nc := 1
			if block == 0 {
				nc = 0
			}
			if _, err := cavlc.EncodeBlock(w, &dc, cavlc.Luma4x4, nc); err != nil {
				t.Fatalf("构造CAVLC残差失败：%v", err)
			}
		}
		return w.Bytes(), w.BitsWritten()
	}
	ctx, err := cabac.InitContexts(false, 0, 26)
	if err != nil {
		t.Fatalf("构造CABAC上下文失败：%v", err)
	}
	e, err := cabac.NewEncoder(w)
	if err != nil {
		t.Fatalf("构造CABAC引擎失败：%v", err)
	}
	bin := func(index int, value uint8) { e.Bin(&ctx[index], value) }
	bin(14, 0)
	if tc.pType == 0 || tc.pType == 3 {
		bin(15, 0)
		bin(16, uint8(tc.pType/3))
	} else {
		bin(15, 1)
		bin(17, uint8(2-tc.pType))
	}
	if tc.pType == 3 {
		for _, typ := range tc.sub {
			if typ == 0 {
				bin(21, 1)
				continue
			}
			bin(21, 0)
			if typ == 1 {
				bin(22, 0)
			} else {
				bin(22, 1)
				bin(23, uint8(3-typ))
			}
		}
	}
	for g := range tc.groups() {
		inc := 0
		if g == 3 {
			inc = 2
		}
		bin(54+inc, uint8(g%2))
		if g%2 != 0 {
			bin(58, 0)
		}
	}
	for range tc.parts {
		for component := range 2 {
			base := 40 + 7*component
			if tc.pType != 0 {
				bin(base, 0)
				continue
			}
			bin(base, 1)
			for magnitude := 1; magnitude < 8; magnitude++ {
				bin(base+min(magnitude+2, 6), 1)
			}
			bin(base+6, 0)
			e.Bypass(0)
		}
	}
	bin(73, 1)
	bin(73, 0)
	bin(73, 0)
	bin(76, 0)
	bin(77, 0)
	bin(60, 0)
	base, err := cabac.CodedBlockFlagBase(cabac.Luma4x4)
	if err != nil {
		t.Fatalf("构造CABAC块标志失败：%v", err)
	}
	for block := range 4 {
		bin(base+block, 1)
		if _, err := e.Residual(&ctx, cabac.Luma4x4, dc[:], transform.Zigzag4x4[:]); err != nil {
			t.Fatalf("构造CABAC残差失败：%v", err)
		}
	}
	e.Terminate(1)
	if err := e.Err(); err != nil {
		t.Fatalf("完成CABAC语料失败：%v", err)
	}
	return w.Bytes(), w.BitsWritten()
}

func TestPreparedPPartitionMacroblockAllocations(t *testing.T) {
	cases := []preparedPPartitionCase{
		{name: "16x16", pType: 0, parts: 1, allow8: true},
		{name: "16x8", pType: 1, parts: 2, allow8: true},
		{name: "8x16", pType: 2, parts: 2, allow8: true},
		{name: "8x8", pType: 3, parts: 4, allow8: true},
		{name: "混合子分区", pType: 3, sub: [4]uint32{0, 1, 2, 3}, parts: 9},
		{name: "十六个4x4", pType: 3, sub: [4]uint32{3, 3, 3, 3}, parts: 16},
		{name: "隐式参考混合子分区", pType: 4, sub: [4]uint32{0, 1, 2, 3}, parts: 9},
		{name: "隐式参考十六个4x4", pType: 4, sub: [4]uint32{3, 3, 3, 3}, parts: 16},
	}
	for _, depth := range []uint32{8, 10} {
		for _, arithmetic := range []bool{false, true} {
			for _, tc := range cases {
				if arithmetic && tc.pType == 4 {
					continue // CABAC没有P_8x8ref0宏块类型。
				}
				t.Run(fmt.Sprintf("%dbit/CABAC=%v/%s", depth, arithmetic, tc.name), func(t *testing.T) {
					sps := &syntax.SPS{ChromaFormatIDC: 1, FrameMbsOnlyFlag: true, BitDepthLumaMinus8: depth - 8, BitDepthChromaMinus8: depth - 8}
					pps := &syntax.PPS{EntropyCodingModeFlag: arithmetic}
					fs, err := newFrameState(mustDecoder(t, h264.DecoderConfig{}), sps, pps, false)
					if err != nil {
						t.Fatalf("预备宏块失败：%v", err)
					}
					t.Cleanup(fs.release)
					var refs [2]*dpb.Reference
					for r := range refs {
						pic, err := picture.New(1, 1, fs.pic.Format)
						if err != nil {
							t.Fatalf("预备参考图像失败：%v", err)
						}
						for c := range pic.Planes {
							pl := &pic.Planes[c]
							for y := range pl.Height {
								for x := range pl.Width {
									pl.SetSample(x, y, uint16(16+c*20+r*80+x+2*y+int(depth-8)*150))
								}
							}
						}
						pic.FillBorders()
						refs[r] = &dpb.Reference{Picture: pic, Identity: &dpb.Identity{ID: uint64(r + 1)}}
					}
					payload, bits := preparedPPartitionPayload(t, tc, arithmetic)
					r := bitstream.NewReader(payload)
					s := sliceEntry{sps: sps, pps: pps, hdr: &syntax.SliceHeader{Type: syntax.SliceTypeP, SliceQP: 26}, data: r}
					s.refs[0] = refs[:]
					var engine cabac.Decoder
					var savedEngine cabac.Decoder
					var savedSyntax cabacSyntax
					if arithmetic {
						d, err := cabac.New(r)
						if err != nil {
							t.Fatalf("预备CABAC读取失败：%v", err)
						}
						engine, savedEngine = *d, *d
						s.cabac = &cabacSyntax{d: &engine}
						ctx, err := cabac.InitContexts(false, 0, 26)
						if err != nil {
							t.Fatalf("预备CABAC模型失败：%v", err)
						}
						copy(s.cabac.ctx[:], ctx[:])
						savedSyntax = *s.cabac
					}
					savedReader := *r
					var decodeErr error
					var last uint8
					qp := 26
					run := func() {
						*r = savedReader
						if arithmetic {
							engine = savedEngine
							*s.cabac = savedSyntax
						}
						fs.mv.Reset()
						clear(fs.info)
						clear(fs.lumaNZ)
						clear(fs.mvd)
						clear(fs.referenceIDs[0])
						for c := range fs.pic.Planes {
							clear(fs.pic.Planes[c].Data)
							clear(fs.pic.Planes[c].Data16)
						}
						qp = 26
						decodeErr = decodeMacroblock(fs, 0, &s, 0, &qp)
						if arithmetic && decodeErr == nil {
							last = engine.Terminate()
							decodeErr = engine.Err()
						}
					}
					run()
					if decodeErr != nil {
						t.Fatalf("预备P宏块解码失败：%v", decodeErr)
					}
					allocs := testing.AllocsPerRun(20, run)
					if decodeErr != nil || qp != 26 || fs.info[0].cbp != 1 || fs.info[0].allow8 != tc.allow8 {
						t.Fatalf("宏块结果错误：错误=%v QP=%d CBP=%d 允许8×8=%v", decodeErr, qp, fs.info[0].cbp, fs.info[0].allow8)
					}
					if arithmetic && last != 1 || !arithmetic && r.BitsRead() != bits {
						t.Fatalf("熵读取未到语料结尾：终止=%d 已读=%d 语料=%d", last, r.BitsRead(), bits)
					}
					for by := range 4 {
						for bx := range 4 {
							mv, ref, ok := fs.mv.Get(bx, by)
							wantMV := motion.MV{}
							if tc.pType == 0 {
								wantMV = motion.MV{X: 8, Y: 8}
							}
							wantRef := tc.reference(bx, by)
							if !ok || mv != wantMV || int(ref) != wantRef || fs.referenceIDs[0][by*4+bx] != refs[wantRef].Identity {
								t.Fatalf("运动网格错误：块=(%d,%d) MV=%v 参考=%d 可用=%v", bx, by, mv, ref, ok)
							}
							wantNZ := uint8(0)
							if bx < 2 && by < 2 {
								wantNZ = 1
							}
							if fs.lumaNZ[by*4+bx] != wantNZ {
								t.Fatalf("残差计数错误：块=(%d,%d) 实际=%d 预期=%d", bx, by, fs.lumaNZ[by*4+bx], wantNZ)
							}
						}
					}
					for c := range fs.pic.Planes {
						pl := &fs.pic.Planes[c]
						for y := range pl.Height {
							for x := range pl.Width {
								lx, ly := x, y
								if c != 0 {
									lx, ly = x*2, y*2
								}
								ref := tc.reference(lx/4, ly/4)
								sx, sy := x, y
								if tc.pType == 0 {
									delta := 2
									if c != 0 {
										delta = 1
									}
									sx, sy = min(x+delta, pl.Width-1), min(y+delta, pl.Height-1)
								}
								want := uint16(16 + c*20 + ref*80 + sx + 2*sy + int(depth-8)*150)
								if c == 0 && x < 8 && y < 8 {
									// QP'=26/38，DC=1反量化为208/832，IDCT后为3/13。
									if depth == 8 {
										want += 3
									} else {
										want += 13
									}
								}
								if got := pl.Sample(x, y); got != want {
									t.Fatalf("重建像素错误：平面=%d 坐标=(%d,%d) 实际=%d 预期=%d", c, x, y, got, want)
								}
							}
						}
					}
					if allocs != 0 {
						t.Fatalf("预备P宏块发生分配：%g", allocs)
					}
				})
			}
		}
	}
}

func TestPPartitionMacroblockInvalidData(t *testing.T) {
	sps := &syntax.SPS{ChromaFormatIDC: 1, FrameMbsOnlyFlag: true}
	pps := &syntax.PPS{}
	fs, err := newFrameState(mustDecoder(t, h264.DecoderConfig{}), sps, pps, false)
	if err != nil {
		t.Fatalf("预备错误路径失败：%v", err)
	}
	defer fs.release()
	ref := &dpb.Reference{Picture: fs.pic, Identity: &dpb.Identity{ID: 1}}
	for _, name := range []string{"缺失参考", "非法宏块类型", "非法子宏块类型", "运动矢量溢出", "截断差分"} {
		t.Run(name, func(t *testing.T) {
			w := bitstream.NewWriter()
			pType := 0
			s := sliceEntry{sps: sps, pps: pps, hdr: &syntax.SliceHeader{Type: syntax.SliceTypeP}}
			s.refs[0] = []*dpb.Reference{ref}
			switch name {
			case "缺失参考":
				s.refs[0] = nil
			case "非法宏块类型":
				pType = 5
			case "非法子宏块类型":
				pType = 3
				w.WriteUE(4)
			case "运动矢量溢出":
				w.WriteSE(motion.MaxAbsMV + 1)
				w.WriteSE(0)
			}
			s.data = bitstream.NewReader(w.Bytes())
			fs.mv.Reset()
			qp := 26
			if err := decodeInterMacroblock(fs, 0, 0, 0, pType, &s, &qp); !errors.Is(err, h264.ErrInvalidData) {
				t.Fatalf("非法语料未返回无效数据错误：%v", err)
			}
			if fs.mv.Available(0, 0) {
				t.Fatal("失败路径提前写入了运动网格")
			}
		})
	}
}
