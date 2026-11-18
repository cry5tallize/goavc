package decoder

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/bitstream"
	"github.com/cry5tallize/goavc/internal/entropy/cabac"
	"github.com/cry5tallize/goavc/nal"
	"github.com/cry5tallize/goavc/syntax"
)

func mbaffPCMStream(t *testing.T, format h264.PixelFormat, entropy bool) ([]byte, []byte) {
	t.Helper()
	idc := uint32(1)
	profile := uint8(77)
	sw, sh := format.ChromaSubsampling()
	if sh == 1 {
		idc, profile = 2, 122
	}
	if sw == 1 {
		idc, profile = 3, 244
	}
	if idc == 1 && format.BitDepth() == 10 {
		profile = 110
	}
	s := &syntax.SPS{ProfileIDC: profile, LevelIDC: 30, ChromaFormatIDC: idc, BitDepthLumaMinus8: uint32(format.BitDepth() - 8), BitDepthChromaMinus8: uint32(format.BitDepth() - 8),
		PicWidthInMbsMinus1: 1, MaxNumRefFrames: 1, PicOrderCntType: 2, MbAdaptiveFrameFieldFlag: true, Direct8x8InferenceFlag: true}
	p := &syntax.PPS{EntropyCodingModeFlag: entropy, DeblockingFilterControlPresentFlag: true}
	srb, err := s.MarshalRBSP()
	if err != nil {
		t.Fatal(err)
	}
	prb, err := p.MarshalRBSP(s)
	if err != nil {
		t.Fatal(err)
	}
	value := func(c, x, y int) uint16 {
		v := uint16((5*x + 7*y + 53*c) & 255)
		if format.BitDepth() == 10 {
			v = 4*v + uint16((x+y+c)%4)
		}
		return v
	}
	var want []byte
	for c := range 3 {
		w, h := format.PlaneDimensions(c, 32, 32)
		for y := range h {
			for x := range w {
				v := value(c, x, y)
				if format.BitDepth() == 10 {
					want = binary.LittleEndian.AppendUint16(want, v)
				} else {
					want = append(want, byte(v))
				}
			}
		}
	}
	w := bitstream.NewWriter()
	w.WriteUE(0)
	w.WriteUE(2)
	w.WriteUE(0)
	w.WriteBits(4, 0)
	w.WriteBit(0)
	w.WriteUE(0)
	w.WriteBits(2, 0)
	w.WriteSE(0)
	w.WriteUE(1)
	var e *cabac.Encoder
	var ctx [cabac.ContextCount444]cabac.Context
	if entropy {
		for w.BitsWritten()%8 != 0 {
			w.WriteBit(1)
		}
		ctx, err = cabac.InitContexts444(true, 0, 26)
		if err != nil {
			t.Fatal(err)
		}
		e, err = cabac.NewEncoder(w)
		if err != nil {
			t.Fatal(err)
		}
	}
	for scan := range 4 {
		field := scan < 2
		if scan&1 == 0 {
			v := uint8(0)
			if field {
				v = 1
			}
			if entropy {
				base := 70
				if scan == 2 {
					base++
				}
				e.Bin(&ctx[base], v)
			} else {
				w.WriteBit(v)
			}
		}
		if entropy {
			inc := 0
			if scan >= 2 {
				inc++
			}
			if scan == 3 {
				inc++
			}
			e.Bin(&ctx[3+inc], 1)
			e.Terminate(1)
			if err := e.Err(); err != nil {
				t.Fatal(err)
			}
		} else {
			w.WriteUE(25)
		}
		w.ByteAlign()
		for c := range 3 {
			cw, ch := format.PlaneDimensions(c, 16, 16)
			for y := range ch {
				for x := range cw {
					py := y + (scan&1)*ch
					if field {
						py = 2*y + (scan & 1)
					}
					w.WriteBits(uint(format.BitDepth()), uint64(value(c, (scan/2)*cw+x, py)))
				}
			}
		}
		if entropy {
			e, err = cabac.NewEncoder(w)
			if err != nil {
				t.Fatal(err)
			}
			if scan&1 != 0 {
				e.Terminate(uint8(boolToInt(scan == 3)))
			}
		}
	}
	if !entropy {
		bitstream.WriteRBSPTrailingBits(w)
	} else if err := e.Err(); err != nil {
		t.Fatal(err)
	}
	unit := func(typ nal.UnitType, rb []byte) nal.Unit {
		return nal.Unit{Header: nal.Header{Type: typ, RefIDC: 3}, Payload: bitstream.RBSPToEBSP(rb)}
	}
	return annex(unit(nal.TypeSPS, srb), unit(nal.TypePPS, prb), unit(nal.TypeSliceIDR, w.Bytes())), want
}

func TestMBAFF_MixedPCMPairsNormativeSamples(t *testing.T) {
	for _, format := range []h264.PixelFormat{h264.YUV420P, h264.YUV420P10LE, h264.YUV422P, h264.YUV422P10LE, h264.YUV444P, h264.YUV444P10LE} {
		for _, entropy := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/cabac%t", format, entropy), func(t *testing.T) {
				data, want := mbaffPCMStream(t, format, entropy)
				d := mustDecoder(t, h264.DecoderConfig{Threads: 2, OutputFormat: format})
				if err := d.SendPacket(h264.Packet{Data: data}); err != nil {
					t.Fatal(err)
				}
				if err := d.Flush(); err != nil {
					t.Fatal(err)
				}
				compareHigh422(t, collectNativeFrames(t, d, nil), want, format)
			})
		}
	}
}

func TestMBAFF_RegisteredX264PixelOracle(t *testing.T) {
	const path = "../testdata/main/main_64x64_yuv420p_cabac_interlaced_013.264"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	d := mustDecoder(t, h264.DecoderConfig{})
	if err := d.SendPacket(h264.Packet{Data: data}); err != nil {
		t.Fatal(err)
	}
	if err := d.Flush(); err != nil {
		t.Fatal(err)
	}
	got := collectNativeFrames(t, d, nil)
	compareHigh422(t, got, high422Raw(t, ffmpegPath(t), path, h264.YUV420P), h264.YUV420P)
	const want = "9797520dcb4f2679b4b1975e143ae8b1b6ea4bdfbd3c5f6498f12139594989d1"
	if hash := fmt.Sprintf("%x", sha256.Sum256(got)); hash != want {
		t.Fatalf("MBAFF registered pixel hash %s, want %s", hash, want)
	}
}

func TestMBAFF_OppositeFieldChromaMotionOracle(t *testing.T) {
	ff := ffmpegPath(t)
	for _, format := range []h264.PixelFormat{h264.YUV420P, h264.YUV420P10LE, h264.YUV422P, h264.YUV422P10LE, h264.YUV444P, h264.YUV444P10LE} {
		t.Run(format.String(), func(t *testing.T) {
			data, _ := mbaffPCMStream(t, format, false)
			w := bitstream.NewWriter()
			// P帧，frame_num=1，场参考列表的ref_idx=1选择相反奇偶。
			w.WriteUE(0)
			w.WriteUE(0)
			w.WriteUE(0)
			w.WriteBits(4, 1)
			w.WriteBit(0)
			w.WriteBit(0)
			w.WriteBit(0)
			w.WriteBit(0)
			w.WriteSE(0)
			w.WriteUE(1)
			for scan := range 4 {
				w.WriteUE(0) // mb_skip_run=0
				if scan&1 == 0 {
					w.WriteBit(1)
				}
				w.WriteUE(0)
				w.WriteBit(0) // P16×16，te(v)参考索引1
				w.WriteSE(0)
				w.WriteSE(0)
				w.WriteUE(0) // MV=0，无残差
			}
			bitstream.WriteRBSPTrailingBits(w)
			p := nal.Unit{Header: nal.Header{Type: nal.TypeSlice, RefIDC: 2}, Payload: bitstream.RBSPToEBSP(w.Bytes())}
			data = append(data, annex(p)...)
			path := filepath.Join(t.TempDir(), "opposite.264")
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
