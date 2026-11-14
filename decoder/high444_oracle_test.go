package decoder

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/internal/testutil/chromaplane"
	"github.com/goavc/h264/nal"
	"github.com/goavc/h264/syntax"
)

// 外部x264压缩样本与FFmpeg原生输出独立于自研的444熵编码和重建路径。
func TestHigh444_DynamicPixelOracle(t *testing.T) {
	ff := high422FFmpeg(t)
	for _, format := range []h264.PixelFormat{h264.YUV444P, h264.YUV444P10LE} {
		for _, tc := range []struct {
			name   string
			qp     int
			params string
		}{
			{"cavlc_i_low", 1, "cabac=0:keyint=1:bframes=0:8x8dct=0"},
			{"cabac_i_high", 48, "cabac=1:keyint=1:bframes=0:8x8dct=0"},
			{"cavlc_i8_matrix", 23, "cabac=0:keyint=1:bframes=0:8x8dct=1:cqm=jvt"},
			{"cabac_i8_matrix", 23, "cabac=1:keyint=1:bframes=0:8x8dct=1:cqm=jvt"},
			{"cavlc_ip_weight", 25, "cabac=0:keyint=24:bframes=0:slices=3:ref=3:weightp=2:8x8dct=0:chroma-qp-offset=3"},
			{"cabac_ip_weight_matrix", 25, "cabac=1:keyint=24:bframes=0:slices=3:ref=3:weightp=2:8x8dct=1:cqm=jvt:chroma-qp-offset=-2"},
			{"cavlc_b_spatial", 25, "cabac=0:keyint=24:bframes=2:b-adapt=0:slices=3:direct=spatial:weightb=1:8x8dct=1"},
			{"cabac_b_temporal_low", 1, "cabac=1:keyint=24:bframes=2:b-adapt=0:slices=3:direct=temporal:weightb=1:8x8dct=1"},
			{"cabac_b_temporal_matrix", 23, "cabac=1:keyint=24:bframes=2:b-adapt=0:slices=3:direct=temporal:weightb=1:8x8dct=1:cqm=jvt"},
		} {
			t.Run(format.String()+"/"+tc.name, func(t *testing.T) {
				path := makeHigh422Oracle(t, ff, format, tc.qp, tc.params)
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				units, err := nal.SplitAnnexB(data)
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, u := range units {
					if u.Header.Type != nal.TypeSPS {
						continue
					}
					rb, err := bitstream.EBSPToRBSP(u.Payload)
					if err != nil {
						t.Fatal(err)
					}
					sps, err := syntax.ParseSPS(rb)
					if err != nil {
						t.Fatal(err)
					}
					if sps.ProfileIDC != 244 || sps.ChromaFormatIDC != 3 || sps.SeparateColourPlaneFlag {
						t.Fatalf("not common-plane High444: %+v", sps)
					}
					found = true
				}
				if !found {
					t.Fatal("missing SPS")
				}
				d := mustDecoder(t, h264.DecoderConfig{OutputFormat: format})
				if err := d.SendPacket(h264.Packet{Data: data}); err != nil {
					t.Fatal(err)
				}
				if err := d.Flush(); err != nil {
					t.Fatal(err)
				}
				var formats []h264.PixelFormat
				got := collectNativeFrames(t, d, &formats)
				if len(formats) != 8 {
					t.Fatalf("frames %d, want 8", len(formats))
				}
				for _, f := range formats {
					if f != format {
						t.Fatalf("format %v, want %v", f, format)
					}
				}
				compareHigh422(t, got, high422Raw(t, ff, path, format), format)
			})
		}
	}
}

// 固定PCM字节提供独立平面组装、奇数裁剪和LE打包的规范解析解。
func high444SeparatePCM(t *testing.T, format h264.PixelFormat) ([]nal.Unit, []byte) {
	t.Helper()
	sps := &syntax.SPS{ProfileIDC: 244, LevelIDC: 30, ChromaFormatIDC: 3, SeparateColourPlaneFlag: true,
		BitDepthLumaMinus8: uint32(format.BitDepth() - 8), BitDepthChromaMinus8: uint32(format.BitDepth() - 8),
		PicOrderCntType: 2, MaxNumRefFrames: 1, FrameMbsOnlyFlag: true, Direct8x8InferenceFlag: true,
		FrameCroppingFlag: true, FrameCropRightOffset: 3, FrameCropBottomOffset: 5}
	pps := &syntax.PPS{DeblockingFilterControlPresentFlag: true}
	sr, err := sps.MarshalRBSP()
	if err != nil {
		t.Fatal(err)
	}
	pr, err := pps.MarshalRBSP(sps)
	if err != nil {
		t.Fatal(err)
	}
	unit := func(typ nal.UnitType, rb []byte) nal.Unit {
		return nal.Unit{Header: nal.Header{RefIDC: 3, Type: typ}, Payload: bitstream.RBSPToEBSP(rb)}
	}
	units := []nal.Unit{unit(nal.TypeSPS, sr), unit(nal.TypePPS, pr)}
	var want []byte
	for comp := range 3 {
		w := bitstream.NewWriter()
		w.WriteUE(0)
		w.WriteUE(2)
		w.WriteUE(0)
		w.WriteBits(2, uint64(comp))
		w.WriteBits(4, 0)
		w.WriteUE(0)
		w.WriteBits(2, 0)
		w.WriteSE(0)
		w.WriteUE(1)
		w.WriteUE(25)
		for w.BitsWritten()%8 != 0 {
			w.WriteBit(0)
		}
		for y := range 16 {
			for x := range 16 {
				v := uint16((x*13 + y*17 + comp*61) & 255)
				if format.BitDepth() == 10 {
					v = v*4 + uint16((x+y+comp)%4)
				}
				w.WriteBits(uint(format.BitDepth()), uint64(v))
				if x < 13 && y < 11 {
					if format.BitDepth() == 10 {
						want = binary.LittleEndian.AppendUint16(want, v)
					} else {
						want = append(want, byte(v))
					}
				}
			}
		}
		if err := bitstream.WriteRBSPTrailingBits(w); err != nil {
			t.Fatal(err)
		}
		units = append(units, unit(nal.TypeSliceIDR, w.Bytes()))
	}
	return units, want
}

func TestHigh444_SeparatePCMAndIncompletePlanes(t *testing.T) {
	for _, format := range []h264.PixelFormat{h264.YUV444P, h264.YUV444P10LE} {
		t.Run(format.String(), func(t *testing.T) {
			units, want := high444SeparatePCM(t, format)
			for _, order := range [][3]int{{2, 3, 4}, {4, 2, 3}} {
				d := mustDecoder(t, h264.DecoderConfig{})
				data := annex(units[0], units[1], units[order[0]], units[order[1]], units[order[2]])
				if err := d.SendPacket(h264.Packet{Data: data, PTS: 9}); err != nil {
					t.Fatal(err)
				}
				if err := d.Flush(); err != nil {
					t.Fatal(err)
				}
				compareHigh422(t, collectNativeFrames(t, d, nil), want, format)
			}
			for _, invalid := range [][]nal.Unit{{units[0], units[1], units[2], units[3]}, {units[0], units[1], units[2], units[2], units[3], units[4]}} {
				d := mustDecoder(t, h264.DecoderConfig{})
				if err := d.SendPacket(h264.Packet{Data: annex(invalid...)}); !errors.Is(err, h264.ErrInvalidData) {
					t.Fatalf("incomplete/duplicate plane accepted: %v", err)
				}
				if err := d.SendPacket(h264.Packet{Data: annex(units...)}); err != nil {
					t.Fatal(err)
				}
				if err := d.Flush(); err != nil {
					t.Fatal(err)
				}
				compareHigh422(t, collectNativeFrames(t, d, nil), want, format)
			}
			// FFmpeg不支持原始独立平面封装；规范封装已由上面的固定采样验证。
			// 对每个单色投影再执行外部解码，原始slice_data不重编码。
			ff := high422FFmpeg(t)
			projected, err := chromaplane.Project(annex(units...))
			if err != nil {
				t.Fatal(err)
			}
			// 保留YUV域，避免gray转换把有限范围16..235扩展为0..255。
			pixelFormat := format.String()
			planeBytes := 13 * 11 * format.BytesPerSample()
			for comp := range 3 {
				path := filepath.Join(t.TempDir(), "plane.264")
				if err := os.WriteFile(path, projected[comp], 0600); err != nil {
					t.Fatal(err)
				}
				cmd := exec.Command(ff, "-v", "error", "-xerror", "-i", path, "-f", "rawvideo", "-pix_fmt", pixelFormat, "pipe:1")
				got, err := cmd.Output()
				if err != nil {
					t.Fatal(err)
				}
				if len(got) != 3*planeBytes {
					t.Fatalf("projected frame bytes %d", len(got))
				}
				compareHigh422(t, got[:planeBytes], want[comp*planeBytes:(comp+1)*planeBytes], format)
			}
		})
	}
}

func TestHigh444_ResourceBudget(t *testing.T) {
	for _, format := range []h264.PixelFormat{h264.YUV444P, h264.YUV444P10LE} {
		units, _ := high444SeparatePCM(t, format)
		for _, delta := range []int{-1, 0, 1} {
			t.Run(fmt.Sprintf("%v/%d", format, delta), func(t *testing.T) {
				d := mustDecoder(t, h264.DecoderConfig{MaxFrameSize: 3*16*16*format.BytesPerSample() + delta})
				err := d.SendPacket(h264.Packet{Data: annex(units...)})
				if delta < 0 {
					if !errors.Is(err, h264.ErrResourceLimit) {
						t.Fatalf("budget: %v", err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
