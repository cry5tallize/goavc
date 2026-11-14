package decoder

import (
	"errors"
	"fmt"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/internal/dpb"
	"github.com/goavc/h264/internal/entropy/cavlc"
	"github.com/goavc/h264/internal/picture"
	"github.com/goavc/h264/nal"
	"github.com/goavc/h264/syntax"
)

// 只构造规范语法，不借用编码流水线或切换重建内核计算期望。
func switchingParameters() (*syntax.SPS, *syntax.PPS) {
	return &syntax.SPS{ProfileIDC: 88, LevelIDC: 10, ChromaFormatIDC: 1,
			FrameMbsOnlyFlag: true, Direct8x8InferenceFlag: true, MaxNumRefFrames: 1, PicOrderCntType: 2},
		&syntax.PPS{DeblockingFilterControlPresentFlag: true}
}

func switchingEncodeBlock(t *testing.T, w *bitstream.Writer, levels [16]int32, typ cavlc.BlockType, nc int) {
	t.Helper()
	if _, err := cavlc.EncodeBlock(w, &levels, typ, nc); err != nil {
		t.Fatal(err)
	}
}

// coded_block_pattern=17：一个亮度8x8分组与两个色度分量的DC。
// 只有第一亮度块含level，后续三个块的nC依次为1、1、0。
func switchingMacroblockBits(t *testing.T, w *bitstream.Writer, si bool, level int32, odd bool) {
	t.Helper()
	w.WriteUE(0)
	if si {
		for range 16 {
			w.WriteBit(1) // MPM=DC，SI同样携带完整的Intra4x4预测模式。
		}
		w.WriteUE(0)  // 色度DC预测
		w.WriteUE(33) // intra映射：17
	} else {
		w.WriteSE(0)
		w.WriteSE(0)
		w.WriteUE(32) // inter映射：17；codeNum31实际映射46，不能少写其残差。
	}
	w.WriteSE(0)
	levels := [16]int32{0: level}
	if odd {
		levels = [16]int32{1: level}
	}
	switchingEncodeBlock(t, w, levels, cavlc.Luma4x4, 0)
	for _, nc := range []int{1, 1, 0} {
		switchingEncodeBlock(t, w, [16]int32{}, cavlc.Luma4x4, nc)
	}
	for range 2 {
		switchingEncodeBlock(t, w, [16]int32{0: 64}, cavlc.ChromaDC, 0)
	}
}

func switchingTestState(t *testing.T, hdr *syntax.SliceHeader, chroma byte) (*frameState, *sliceEntry) {
	t.Helper()
	sps, pps := switchingParameters()
	d := mustDecoder(t, h264.DecoderConfig{})
	fs, err := newFrameState(d, sps, pps, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(fs.release)
	s := &sliceEntry{hdr: hdr, sps: sps, pps: pps}
	ref, err := picture.New(1, 1, h264.YUV420P)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ref.Release)
	for c := range 3 {
		v := byte(128)
		if c != 0 {
			v = chroma
		}
		pl := &ref.Planes[c]
		for y := range pl.Height {
			for x := range pl.Width {
				pl.SetPix(x, y, v)
			}
		}
	}
	ref.FillBorders()
	s.refs[0] = []*dpb.Reference{{Identity: &dpb.Identity{}, Picture: ref}}
	return fs, s
}

func TestSwitchingMacroblock_PrimarySecondarySignedLevels(t *testing.T) {
	// DC基底的独立解析解：QP0、QS12，预测128的量化DC为205。
	// primary的level±64先变成±160，最终采样138/118；secondary直接
	// 在QS域加±64，最终采样168/88。色度预测0的DC64分别重建5/1。
	for _, secondary := range []bool{false, true} {
		for _, sign := range []int32{1, -1} {
			t.Run(fmt.Sprintf("secondary_%v_sign_%d", secondary, sign), func(t *testing.T) {
				fs, s := switchingTestState(t, &syntax.SliceHeader{Type: syntax.SliceTypeSP, SliceQP: 0, SliceQS: 12, SPForSwitchFlag: secondary}, 0)
				w := bitstream.NewWriter()
				switchingMacroblockBits(t, w, false, sign*64, false)
				s.data = bitstream.NewReader(w.Bytes())
				qp := 0
				if err := decodeMacroblock(fs, 0, s, 0, &qp); err != nil {
					t.Fatal(err)
				}
				wantY, wantC := 128+int(sign)*10, byte(5)
				if secondary {
					wantY, wantC = 128+int(sign)*40, 1
				}
				for y := range 16 {
					for x := range 16 {
						want := byte(128)
						if x < 4 && y < 4 {
							want = byte(wantY)
						}
						if got := fs.pic.Planes[0].Pix(x, y); got != want {
							t.Fatalf("luma (%d,%d)=%d want %d", x, y, got, want)
						}
					}
				}
				for c := 1; c < 3; c++ {
					for y := range 8 {
						for x := range 8 {
							if got := fs.pic.Planes[c].Pix(x, y); got != wantC {
								t.Fatalf("chroma %d (%d,%d)=%d want %d", c, x, y, got, wantC)
							}
						}
					}
				}
				if qp != 0 || fs.info[0].qp != 0 || fs.lumaNZ[0] != 1 {
					t.Fatal("QS不能覆盖语法QP或残差上下文")
				}
			})
		}
	}
}

func TestSwitchingMacroblock_OddBasisAndChromaOffsets(t *testing.T) {
	for _, sign := range []int32{1, -1} {
		fs, s := switchingTestState(t, &syntax.SliceHeader{Type: syntax.SliceTypeSP, SliceQP: 12, SliceQS: 0, SPForSwitchFlag: true}, 0)
		w := bitstream.NewWriter()
		switchingMacroblockBits(t, w, false, sign*64, true)
		s.data = bitstream.NewReader(w.Bytes())
		qp := 12
		if err := decodeMacroblock(fs, 0, s, 0, &qp); err != nil {
			t.Fatal(err)
		}
		want := [4]byte{141, 134, 121, 115}
		if sign < 0 {
			want = [4]byte{115, 121, 134, 141}
		}
		for y := range 4 {
			for x := range 4 {
				if got := fs.pic.Planes[0].Pix(x, y); got != want[x] {
					t.Fatalf("sign %d (%d,%d)=%d want %d", sign, x, y, got, want[x])
				}
			}
		}
	}
	// QPY30->QPC29、offset1->QPC30；QSY12/13必须独立映射，
	// 同一DC64的解析解为144/160，而不是使用同一个色度QP或QS。
	fs, s := switchingTestState(t, &syntax.SliceHeader{Type: syntax.SliceTypeSP, SliceQP: 30, SliceQS: 12}, 0)
	s.pps.SecondChromaQPIndexOffset = 1
	w := bitstream.NewWriter()
	switchingMacroblockBits(t, w, false, 0, false)
	s.data = bitstream.NewReader(w.Bytes())
	qp := 30
	if err := decodeMacroblock(fs, 0, s, 0, &qp); err != nil {
		t.Fatal(err)
	}
	for c, want := range []byte{144, 160} {
		for y := range 8 {
			for x := range 8 {
				if got := fs.pic.Planes[c+1].Pix(x, y); got != want {
					t.Fatalf("chroma %d (%d,%d)=%d want %d", c, x, y, got, want)
				}
			}
		}
	}
}

func TestSwitchingMacroblock_SkipQuantizesPrediction(t *testing.T) {
	for _, secondary := range []bool{false, true} {
		fs, s := switchingTestState(t, &syntax.SliceHeader{Type: syntax.SliceTypeSP, SliceQP: 0, SliceQS: 51, SPForSwitchFlag: secondary}, 20)
		for i := range s.refs[0][0].Picture.Planes[0].Data {
			s.refs[0][0].Picture.Planes[0].Data[i] = 20
		}
		if err := decodeSkipMacroblock(fs, 0, s, 0, 0); err != nil {
			t.Fatal(err)
		}
		for c := range 3 {
			want := byte(0)
			if c != 0 && !secondary {
				want = 21 // QSC39：预测Hadamard DC1280量化为3，逆缩放得1344。
			}
			pl := &fs.pic.Planes[c]
			for y := range pl.Height {
				for x := range pl.Width {
					if got := pl.Pix(x, y); got != want {
						t.Fatalf("SP skip secondary=%v plane%d (%d,%d)=%d want %d", secondary, c, x, y, got, want)
					}
				}
			}
		}
		if !fs.info[0].skip || fs.info[0].qp != 0 {
			t.Fatal("skip元数据丢失")
		}
	}
}

func switchingNAL(typ nal.UnitType, rbsp []byte) nal.Unit {
	return nal.Unit{Header: nal.Header{Type: typ, RefIDC: 3}, Payload: bitstream.RBSPToEBSP(rbsp)}
}

func switchingFrameStream(t *testing.T, si, secondary, skip bool, sign int32) []byte {
	t.Helper()
	sps, pps := switchingParameters()
	srb, err := sps.MarshalRBSP()
	if err != nil {
		t.Fatal(err)
	}
	prb, err := pps.MarshalRBSP(sps)
	if err != nil {
		t.Fatal(err)
	}
	units := []nal.Unit{switchingNAL(nal.TypeSPS, srb), switchingNAL(nal.TypePPS, prb)}
	if !si {
		w := bitstream.NewWriter()
		w.WriteUE(0)
		w.WriteUE(2)
		w.WriteUE(0)
		w.WriteBits(4, 0)
		w.WriteUE(0)
		w.WriteBits(2, 0)
		w.WriteSE(0)
		w.WriteUE(1)
		w.WriteUE(25)
		w.ByteAlign()
		luma := uint64(128)
		if skip {
			luma = 20
		}
		for range 256 {
			w.WriteBits(8, luma)
		}
		for range 128 {
			w.WriteBits(8, 0)
		}
		bitstream.WriteRBSPTrailingBits(w)
		units = append(units, switchingNAL(nal.TypeSliceIDR, w.Bytes()))
	}
	w := bitstream.NewWriter()
	w.WriteUE(0)
	typ := nal.TypeSlice
	if si {
		w.WriteUE(4)
		w.WriteUE(0)
		w.WriteBits(4, 0)
		w.WriteUE(0)
		w.WriteBits(2, 0)
		typ = nal.TypeSliceIDR
	} else {
		w.WriteUE(3)
		w.WriteUE(0)
		w.WriteBits(4, 1)
		w.WriteBit(0) // 参考数量沿用PPS
		w.WriteBit(0) // 不重排参考列表
		w.WriteBit(0) // sliding-window参考标记
	}
	w.WriteSE(-26) // QPY=0
	if !si {
		if secondary {
			w.WriteBit(1)
		} else {
			w.WriteBit(0)
		}
	}
	if skip {
		w.WriteSE(25) // QSY51：常量20的亮度预测也必须归零。
	} else {
		w.WriteSE(-14) // QSY12
	}
	w.WriteUE(1) // 不去块，保留逐采样解析解
	if skip {
		w.WriteUE(1) // 一个SP Skip宏块，不存在mb_type或残差语法。
	} else {
		if !si {
			w.WriteUE(0)
		}
		switchingMacroblockBits(t, w, si, sign*64, false)
	}
	bitstream.WriteRBSPTrailingBits(w)
	units = append(units, switchingNAL(typ, w.Bytes()))
	return annex(units...)
}

func TestSwitchingFrames_NormativeSignedDC(t *testing.T) {
	for _, si := range []bool{false, true} {
		for _, secondary := range []bool{false, true} {
			if si && secondary {
				continue
			}
			for _, sign := range []int32{1, -1} {
				t.Run(fmt.Sprintf("si_%v_secondary_%v_sign_%d", si, secondary, sign), func(t *testing.T) {
					d := mustDecoder(t, h264.DecoderConfig{})
					if err := d.SendPacket(h264.Packet{Data: switchingFrameStream(t, si, secondary, false, sign)}); err != nil {
						t.Fatal(err)
					}
					if err := d.Flush(); err != nil {
						t.Fatal(err)
					}
					count := 1
					if !si {
						count = 2
					}
					for index := range count {
						f, err := d.ReceiveFrame()
						if err != nil {
							t.Fatal(err)
						}
						if index == count-1 {
							wantY, wantC := 128+int(sign)*10, byte(5)
							if secondary || si {
								wantY, wantC = 128+int(sign)*40, 1
							}
							if si {
								wantC = 7 // (round(8192/20)+64+32)>>6
							}
							for c := range 3 {
								n := 16
								if c != 0 {
									n = 8
								}
								for y := range n {
									for x := range n {
										want := wantC
										if c == 0 {
											want = 128
											if si || x < 4 && y < 4 {
												want = byte(wantY)
											}
										}
										if got := f.Planes[c].Data[y*f.Planes[c].Stride+x]; got != want {
											t.Fatalf("plane%d (%d,%d)=%d want %d", c, x, y, got, want)
										}
									}
								}
							}
						}
						f.Release()
					}
					if _, err := d.ReceiveFrame(); !errors.Is(err, h264.ErrEOF) {
						t.Fatalf("unexpected extra output: %v", err)
					}
				})
			}
		}
	}
}

func TestSwitchingFrames_SkipUsesSwitchingDomain(t *testing.T) {
	for _, secondary := range []bool{false, true} {
		d := mustDecoder(t, h264.DecoderConfig{})
		if err := d.SendPacket(h264.Packet{Data: switchingFrameStream(t, false, secondary, true, 1)}); err != nil {
			t.Fatal(err)
		}
		if err := d.Flush(); err != nil {
			t.Fatal(err)
		}
		for index := range 2 {
			f, err := d.ReceiveFrame()
			if err != nil {
				t.Fatal(err)
			}
			if index == 1 {
				for c := range 3 {
					n := 16
					if c != 0 {
						n = 8
					}
					for y := range n {
						for x := range n {
							if got := f.Planes[c].Data[y*f.Planes[c].Stride+x]; got != 0 {
								t.Fatalf("skip secondary=%v plane%d (%d,%d)=%d want0", secondary, c, x, y, got)
							}
						}
					}
				}
			}
			f.Release()
		}
	}
}

func TestSwitchingMacroblock_ChromaACAndDCStaySeparate(t *testing.T) {
	// QS0、secondary的色度DC64直接给每个块常量1；最后一个块的
	// AC(0,1)=±64经反量化为±832，逆基底为±{832,416,-416,-832}。
	for _, sign := range []int32{1, -1} {
		fs, s := switchingTestState(t, &syntax.SliceHeader{Type: syntax.SliceTypeSP, SliceQP: 12, SliceQS: 0, SPForSwitchFlag: true}, 0)
		w := bitstream.NewWriter()
		w.WriteUE(0)
		w.WriteSE(0)
		w.WriteSE(0)
		w.WriteUE(6) // inter CBP32：仅色度DC+AC。
		w.WriteSE(0)
		for range 2 {
			switchingEncodeBlock(t, w, [16]int32{0: 64}, cavlc.ChromaDC, 0)
		}
		for range 2 {
			for b := range 4 {
				levels := [16]int32{}
				if b == 3 {
					levels[1] = sign * 64
				}
				switchingEncodeBlock(t, w, levels, cavlc.LumaAC, 0)
			}
		}
		s.data = bitstream.NewReader(w.Bytes())
		qp := 12
		if err := decodeMacroblock(fs, 0, s, 0, &qp); err != nil {
			t.Fatal(err)
		}
		wantAC := [4]byte{14, 8, 0, 0}
		if sign < 0 {
			wantAC = [4]byte{0, 0, 8, 14}
		}
		for c := 1; c < 3; c++ {
			for y := range 8 {
				for x := range 8 {
					want := byte(1)
					if x >= 4 && y >= 4 {
						want = wantAC[x-4]
					}
					if got := fs.pic.Planes[c].Pix(x, y); got != want {
						t.Fatalf("sign%d chroma%d (%d,%d)=%d want%d", sign, c, x, y, got, want)
					}
				}
			}
		}
	}
}

func TestSwitchingMacroblock_SIOrdinaryIMapping(t *testing.T) {
	for _, mbType := range []uint32{1, 4, 26} {
		fs, s := switchingTestState(t, &syntax.SliceHeader{Type: syntax.SliceTypeSI, SliceQP: 0, SliceQS: 51}, 0)
		w := bitstream.NewWriter()
		w.WriteUE(mbType)
		switch mbType {
		case 1: // SI的1映射到I_NxN，仍走普通QP域而非QS51。
			for range 16 {
				w.WriteBit(1)
			}
			w.WriteUE(0)
			w.WriteUE(3) // intra CBP=0
		case 4: // SI的4映射到I16x16 DC。
			w.WriteUE(0)
			w.WriteSE(0)
			switchingEncodeBlock(t, w, [16]int32{}, cavlc.LumaDC, 0)
		case 26: // SI的26映射到I_PCM。
			w.ByteAlign()
			for range 384 {
				w.WriteBits(8, 128)
			}
		}
		s.data = bitstream.NewReader(w.Bytes())
		qp := 0
		if err := decodeMacroblock(fs, 0, s, 0, &qp); err != nil {
			t.Fatalf("SI mb_type%d: %v", mbType, err)
		}
		for c := range 3 {
			pl := &fs.pic.Planes[c]
			for y := range pl.Height {
				for x := range pl.Width {
					if got := pl.Pix(x, y); got != 128 {
						t.Fatalf("SI普通I mb_type%d plane%d (%d,%d)=%d want128", mbType, c, x, y, got)
					}
				}
			}
		}
	}
}

func TestSwitchingErrors_InvalidAndUnsupported(t *testing.T) {
	for _, typ := range []syntax.SliceType{syntax.SliceTypeSP, syntax.SliceTypeSI} {
		for _, qs := range []int{-1, 52} {
			s, p := switchingParameters()
			if err := checkSwitchingSupported(s, p, &syntax.SliceHeader{Type: typ, SliceQS: qs}); !errors.Is(err, h264.ErrInvalidData) {
				t.Fatalf("invalid QS%d: %v", qs, err)
			}
		}
		for _, mutate := range []func(*syntax.SPS, *syntax.PPS){
			func(s *syntax.SPS, p *syntax.PPS) { s.ChromaFormatIDC = 2 },
			func(s *syntax.SPS, p *syntax.PPS) { s.BitDepthLumaMinus8, s.BitDepthChromaMinus8 = 2, 2 },
			func(s *syntax.SPS, p *syntax.PPS) { p.EntropyCodingModeFlag = true },
			func(s *syntax.SPS, p *syntax.PPS) { p.Transform8x8ModeFlag = true },
			func(s *syntax.SPS, p *syntax.PPS) { s.FrameMbsOnlyFlag = false },
		} {
			s, p := switchingParameters()
			mutate(s, p)
			if err := checkSwitchingSupported(s, p, &syntax.SliceHeader{Type: typ, SliceQS: 12}); !errors.Is(err, h264.ErrUnsupported) {
				t.Fatalf("unsupported %v: %v", typ, err)
			}
		}
		fs, s := switchingTestState(t, &syntax.SliceHeader{Type: typ, SliceQS: 12}, 0)
		w := bitstream.NewWriter()
		mbType := uint32(31)
		if typ == syntax.SliceTypeSI {
			mbType = 27
		}
		w.WriteUE(mbType)
		s.data = bitstream.NewReader(w.Bytes())
		qp := 0
		if err := decodeMacroblock(fs, 0, s, 0, &qp); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("invalid mb_type %d: %v", mbType, err)
		}
	}
}
