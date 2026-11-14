package decoder

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/internal/entropy/cabac"
	"github.com/goavc/h264/internal/entropy/cavlc"
	"github.com/goavc/h264/internal/transform"
	"github.com/goavc/h264/nal"
	"github.com/goavc/h264/syntax"
)

// 独立规范向量：左PCM、右I16，保留8个色度DC与每个色度AC块。
// 同一语法可选双熵；不调用本库编码流水线，逐采样期望由FFmpeg给出。
func high422SyntaxStream(t *testing.T, format h264.PixelFormat, entropy, mixed, crop, deblock bool, qp int) []byte {
	t.Helper()
	_, subH := format.ChromaSubsampling()
	idc := uint32(2)
	if subH == 2 {
		idc = 1
	}
	depth := uint32(format.BitDepth() - 8)
	s := &syntax.SPS{ProfileIDC: 122, LevelIDC: 10, ChromaFormatIDC: idc,
		BitDepthLumaMinus8: depth, BitDepthChromaMinus8: depth,
		MaxNumRefFrames: 1, FrameMbsOnlyFlag: true, Direct8x8InferenceFlag: true}
	if mixed {
		s.PicWidthInMbsMinus1 = 1
	}
	if crop {
		s.FrameCroppingFlag = true
		s.FrameCropLeftOffset, s.FrameCropRightOffset = 1, 1
		s.FrameCropTopOffset = 1
		if subH == 1 {
			s.FrameCropBottomOffset = 2
		}
	}
	p := &syntax.PPS{EntropyCodingModeFlag: entropy, DeblockingFilterControlPresentFlag: true,
		ChromaQPIndexOffset: -2, SecondChromaQPIndexOffset: 3}
	srb, err := s.MarshalRBSP()
	if err != nil {
		t.Fatal(err)
	}
	prb, err := p.MarshalRBSP(s)
	if err != nil {
		t.Fatal(err)
	}
	w := bitstream.NewWriter()
	w.WriteUE(0) // first_mb_in_slice
	w.WriteUE(2) // I slice
	w.WriteUE(0)
	w.WriteBits(4, 0)
	w.WriteUE(0) // idr_pic_id
	w.WriteBits(4, 0)
	w.WriteBits(2, 0) // IDR reference marking
	w.WriteSE(int32(qp - 26))
	if deblock {
		w.WriteUE(0)
		w.WriteSE(0)
		w.WriteSE(0)
	} else {
		w.WriteUE(1)
	}
	var e *cabac.Encoder
	var ctx [cabac.ContextCount]cabac.Context
	if entropy {
		for w.BitsWritten()%8 != 0 {
			w.WriteBit(1)
		}
		ctx, err = cabac.InitContexts(true, 0, max(0, min(51, qp)))
		if err != nil {
			t.Fatal(err)
		}
		e, err = cabac.NewEncoder(w)
		if err != nil {
			t.Fatal(err)
		}
		e.Bin(&ctx[3], 1)
		e.Terminate(1) // I_PCM，之后重启算术区间而不重置上下文。
		if err := e.Err(); err != nil {
			t.Fatal(err)
		}
	} else {
		w.WriteUE(25)
	}
	w.ByteAlign()
	for c := range 3 {
		mw, mh := format.PlaneDimensions(c, 16, 16)
		for y := range mh {
			for x := range mw {
				value := (x*17 + y*11 + c*53) & 255
				if format.BitDepth() > 8 {
					value = value*4 + (x+y+c)%4
				}
				w.WriteBits(uint(format.BitDepth()), uint64(value))
			}
		}
	}
	if entropy {
		e, err = cabac.NewEncoder(w)
		if err != nil {
			t.Fatal(err)
		}
		if mixed {
			e.Terminate(0) // PCM宏块不是slice最后一个宏块。
		}
	}
	if mixed {
		var ldc [16]int32
		ldc[0] = 3
		var dc [2][16]int32
		var ac [2][8][16]int32
		blocks := 8 / subH
		for c := range 2 {
			for b := range blocks {
				dc[c][b] = int32((b+c)%5 - 2)
				ac[c][b][1] = 1 - 2*int32((b+c)%2)
			}
		}
		if entropy {
			e.Bin(&ctx[4], 1)
			e.Terminate(0)
			e.Bin(&ctx[6], 0) // 无亮度AC。
			e.Bin(&ctx[7], 1)
			e.Bin(&ctx[8], 1) // 色度CBP=2。
			e.Bin(&ctx[9], 1)
			e.Bin(&ctx[10], 0) // I16亮度DC预测。
			e.Bin(&ctx[64], 0) // 色度DC预测，左PCM不增加mode上下文。
			e.Bin(&ctx[60], 0) // mb_qp_delta=0，PCM不改变slice QP。
			e.Bin(&ctx[88], 1)
			if _, err := e.Residual(&ctx, cabac.LumaDC, ldc[:], transform.LumaDCScan[:]); err != nil {
				t.Fatal(err)
			}
			scan := transform.DC2x2Scan[:]
			if subH == 1 {
				scan = transform.DC2x4Scan[:]
			}
			for c := range 2 {
				e.Bin(&ctx[100], 1) // 左PCM、上不可用：两个CBF条件均为1。
				if _, err := e.Residual(&ctx, cabac.ChromaDC, dc[c][:blocks], scan); err != nil {
					t.Fatal(err)
				}
			}
			for c := range 2 {
				for b := range blocks {
					e.Bin(&ctx[104], 1)
					if _, err := e.Residual(&ctx, cabac.ChromaAC, ac[c][b][:], transform.Zigzag4x4[1:]); err != nil {
						t.Fatal(err)
					}
				}
			}
		} else {
			w.WriteUE(11) // I16 DC、无亮度AC、色度CBP=2。
			w.WriteUE(0)
			w.WriteSE(0)
			if _, err := cavlc.EncodeBlock(w, &ldc, cavlc.LumaDC, 16); err != nil {
				t.Fatal(err)
			}
			dcType := cavlc.ChromaDC
			if subH == 1 {
				dcType = cavlc.ChromaDC422
			}
			for c := range 2 {
				if _, err := cavlc.EncodeBlock(w, &dc[c], dcType, 0); err != nil {
					t.Fatal(err)
				}
			}
			for c := range 2 {
				for b := range blocks {
					nc := 1
					if b%2 == 0 {
						nc = 9 // 左PCM=16，上方当前MB AC=1。
						if b == 0 {
							nc = 16 // 上不可用，只用左PCM。
						}
					}
					if _, err := cavlc.EncodeBlock(w, &ac[c][b], cavlc.LumaAC, nc); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
	}
	if entropy {
		e.Terminate(1)
		if err := e.Err(); err != nil {
			t.Fatal(err)
		}
	} else {
		bitstream.WriteRBSPTrailingBits(w)
	}
	unit := func(typ nal.UnitType, rbsp []byte) nal.Unit {
		return nal.Unit{Header: nal.Header{Type: typ, RefIDC: 3}, Payload: bitstream.RBSPToEBSP(rbsp)}
	}
	return annex(unit(nal.TypeSPS, srb), unit(nal.TypePPS, prb), unit(nal.TypeSliceIDR, w.Bytes()))
}

func TestHigh422_NormativeMixedPCMOracle(t *testing.T) {
	ff := high422FFmpeg(t)
	for _, format := range []h264.PixelFormat{h264.YUV420P, h264.YUV420P10LE, h264.YUV422P, h264.YUV422P10LE} {
		for _, entropy := range []bool{false, true} {
			for _, qp := range []int{-6 * (format.BitDepth() - 8), 1, 24, 48, 51} {
				for _, deblock := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/cabac_%v/qp_%d/deblock_%v", format, entropy, qp, deblock), func(t *testing.T) {
						data := high422SyntaxStream(t, format, entropy, true, true, deblock, qp)
						path := filepath.Join(t.TempDir(), "normative.264")
						if err := os.WriteFile(path, data, 0600); err != nil {
							t.Fatal(err)
						}
						d := mustDecoder(t, h264.DecoderConfig{})
						if err := d.SendPacket(h264.Packet{Data: data}); err != nil {
							t.Fatal(err)
						}
						if err := d.Flush(); err != nil {
							t.Fatal(err)
						}
						compareHigh422(t, collectNativeFrames(t, d, nil), high422Raw(t, ff, path, format), format)
					})
				}
			}
		}
	}
}

func TestHigh422_PCMNativeCrop(t *testing.T) {
	for _, format := range []h264.PixelFormat{h264.YUV422P, h264.YUV422P10LE} {
		for _, entropy := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/cabac_%v", format, entropy), func(t *testing.T) {
				d := mustDecoder(t, h264.DecoderConfig{})
				data := high422SyntaxStream(t, format, entropy, false, true, false, 26)
				if err := d.SendPacket(h264.Packet{Data: data, PTS: 42, Duration: 3}); err != nil {
					t.Fatal(err)
				}
				if err := d.Flush(); err != nil {
					t.Fatal(err)
				}
				f, err := d.ReceiveFrame()
				if err != nil {
					t.Fatal(err)
				}
				defer f.Release()
				if f.Format != format || f.Width != 12 || f.Height != 13 || f.PTS != 42 || f.Duration != 3 || !f.KeyFrame {
					t.Fatalf("native cropped High422 metadata: %+v", f)
				}
				for c := range 3 {
					w, h := format.PlaneDimensions(c, 12, 13)
					cropX := 2
					if c > 0 {
						cropX = 1
					}
					step := format.BytesPerSample()
					if f.Planes[c].Stride != w*step {
						t.Fatalf("plane %d byte stride %d, want %d", c, f.Planes[c].Stride, w*step)
					}
					for y := range h {
						for x := range w {
							pos := y*f.Planes[c].Stride + x*step
							got := uint16(f.Planes[c].Data[pos])
							want := uint16(((x+cropX)*17 + (y+1)*11 + c*53) & 255)
							if step == 2 {
								got = binary.LittleEndian.Uint16(f.Planes[c].Data[pos:])
								want = want*4 + uint16((x+cropX+y+1+c)%4)
							}
							if got != want {
								t.Fatalf("plane %d (%d,%d) got %d, want %d", c, x, y, got, want)
							}
						}
					}
				}
			})
		}
	}
}

func TestHigh422_ResourceBudgetBoundaries(t *testing.T) {
	for _, format := range []h264.PixelFormat{h264.YUV420P, h264.YUV420P10LE, h264.YUV422P, h264.YUV422P10LE} {
		// 编码尺寸32x16：420=768采样、422=1024采样；10-bit每采样双字节。
		samples := 1024
		if _, subH := format.ChromaSubsampling(); subH == 2 {
			samples = 768
		}
		actual := samples * format.BytesPerSample()
		for _, delta := range []int{-1, 0, 1} {
			t.Run(fmt.Sprintf("%s/%d", format, actual+delta), func(t *testing.T) {
				d := mustDecoder(t, h264.DecoderConfig{MaxWidth: 32, MaxHeight: 16, MaxFrameSize: actual + delta})
				data := high422SyntaxStream(t, format, false, true, true, false, 26)
				err := d.SendPacket(h264.Packet{Data: data})
				if delta < 0 {
					if !errors.Is(err, h264.ErrResourceLimit) {
						t.Fatalf("undersized native %v budget accepted: %v", format, err)
					}
					return
				}
				if err != nil {
					t.Fatalf("exact native sample budget rejected: %v", err)
				}
				if err := d.Flush(); err != nil {
					t.Fatal(err)
				}
				f, err := d.ReceiveFrame()
				if err != nil {
					t.Fatal(err)
				}
				defer f.Release()
				if f.Format != format {
					t.Fatalf("budget frame format %v, want %v", f.Format, format)
				}
			})
		}
	}
}

func TestHigh422_NormativePCMRecovery(t *testing.T) {
	for _, format := range []h264.PixelFormat{h264.YUV422P, h264.YUV422P10LE} {
		for _, entropy := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/cabac_%v", format, entropy), func(t *testing.T) {
				data := high422SyntaxStream(t, format, entropy, false, false, false, 26)
				units, err := nal.SplitAnnexB(data)
				if err != nil {
					t.Fatal(err)
				}
				last := units[len(units)-1]
				last.Payload = last.Payload[:len(last.Payload)/2]
				broken := append([]nal.Unit(nil), units[:len(units)-1]...)
				broken = append(broken, last)
				d := mustDecoder(t, h264.DecoderConfig{})
				if err := d.SendPacket(h264.Packet{Data: annex(broken...)}); !errors.Is(err, h264.ErrInvalidData) {
					t.Fatalf("truncated PCM accepted: %v", err)
				}
				if err := d.SendPacket(h264.Packet{Data: data}); err != nil {
					t.Fatal(err)
				}
				if err := d.Flush(); err != nil {
					t.Fatal(err)
				}
				var want []byte
				for c := range 3 {
					w, h := format.PlaneDimensions(c, 16, 16)
					for y := range h {
						for x := range w {
							v := (x*17 + y*11 + c*53) & 255
							if format.BitDepth() > 8 {
								want = binary.LittleEndian.AppendUint16(want, uint16(v*4+(x+y+c)%4))
							} else {
								want = append(want, byte(v))
							}
						}
					}
				}
				compareHigh422(t, collectNativeFrames(t, d, nil), want, format)
			})
		}
	}
}
