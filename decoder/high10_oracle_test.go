package decoder

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/internal/entropy/cavlc"
	"github.com/goavc/h264/nal"
	"github.com/goavc/h264/syntax"
)

func high10FFmpeg(t *testing.T) string {
	t.Helper()
	ffmpeg := ffmpegPath(t)
	out, err := exec.Command(ffmpeg, "-hide_banner", "-h", "encoder=libx264").CombinedOutput()
	if err != nil {
		t.Fatalf("query libx264: %v\n%s", err, out)
	}
	if !bytes.Contains(out, []byte("yuv420p10le")) {
		t.Skip("FFmpeg libx264 does not advertise yuv420p10le")
	}
	return ffmpeg
}

// 外部编码器生成压缩码流，外部解码器给出逐采样Oracle，不依赖本库编码器。
func makeHigh10Oracle(t *testing.T, ffmpeg string, qp int, params string) string {
	t.Helper()
	const width, height, frames = 94, 62, 8
	var raw []byte
	for n := range frames {
		for c := range 3 {
			w, h := width, height
			if c != 0 {
				w, h = w/2, h/2
			}
			for y := range h {
				for x := range w {
					// 平滑块、纹理、平移和曝光变化同时驱动帧内/运动/权重工具。
					value := 80 + ((x+2*n)%w*5+y*3+n*11+c*113)%760
					if x > w/2 && y > h/3 {
						value = (value + (x*y+17*n)%129) & 1023
					}
					raw = binary.LittleEndian.AppendUint16(raw, uint16(value))
				}
			}
		}
	}
	dir := t.TempDir()
	input, output := filepath.Join(dir, "input.yuv"), filepath.Join(dir, "high10.264")
	if err := os.WriteFile(input, raw, 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"-v", "error", "-y", "-f", "rawvideo", "-pixel_format", "yuv420p10le", "-video_size", "94x62", "-framerate", "12", "-i", input,
		"-frames:v", strconv.Itoa(frames), "-c:v", "libx264", "-profile:v", "high10", "-preset", "medium", "-qp", strconv.Itoa(qp),
		"-x264-params", "threads=1:lookahead-threads=1:sync-lookahead=0:scenecut=0:aud=1:repeat-headers=1:" + params, "-f", "h264", output}
	if out, err := exec.Command(ffmpeg, args...).CombinedOutput(); err != nil {
		t.Fatalf("encode High10: %v\n%s", err, out)
	}
	return output
}

func high10FFmpegRaw(t *testing.T, ffmpeg, path string) []byte {
	t.Helper()
	cmd := exec.Command(ffmpeg, "-v", "error", "-i", path, "-fps_mode", "passthrough", "-f", "rawvideo", "-pix_fmt", "yuv420p10le", "-")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("decode High10 oracle: %v\n%s", err, stderr.String())
	}
	return out
}

func compareHigh10(t *testing.T, got, want []byte) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("decoded %d bytes, oracle %d", len(got), len(want))
	}
	for i := 0; i < len(got); i += 2 {
		g, w := binary.LittleEndian.Uint16(got[i:]), binary.LittleEndian.Uint16(want[i:])
		if g != w {
			t.Fatalf("sample %d: got %d, oracle %d", i/2, g, w)
		}
	}
}

func collectNativeFrames(t *testing.T, d *Decoder, formats *[]h264.PixelFormat) []byte {
	t.Helper()
	var out []byte
	for {
		f, err := d.ReceiveFrame()
		if errors.Is(err, h264.ErrAgain) || errors.Is(err, h264.ErrEOF) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		if formats != nil {
			*formats = append(*formats, f.Format)
		}
		for c := range 3 {
			rowBytes := f.Format.RowBytes(c, f.Width)
			_, rows := f.Format.PlaneDimensions(c, f.Width, f.Height)
			if f.Planes[c].Stride < rowBytes {
				t.Fatalf("plane %d byte stride %d < %d", c, f.Planes[c].Stride, rowBytes)
			}
			for y := range rows {
				row := f.Planes[c].Data[y*f.Planes[c].Stride:][:rowBytes]
				if f.Format == h264.YUV420P10LE {
					for x := 0; x < rowBytes; x += 2 {
						if v := binary.LittleEndian.Uint16(row[x:]); v > 1023 {
							t.Fatalf("plane %d sample exceeds 10 bits: %d", c, v)
						}
					}
				}
				out = append(out, row...)
			}
		}
		f.Release()
	}
}

func TestHigh10_DynamicPixelOracle(t *testing.T) {
	ffmpeg := high10FFmpeg(t)
	for _, tc := range []struct {
		name                string
		qp                  int
		params              string
		bframes, multislice bool
	}{
		{"cavlc_i_low", 1, "cabac=0:keyint=1:bframes=0:8x8dct=0", false, false},
		{"cabac_i8_matrix", 23, "cabac=1:keyint=1:bframes=0:8x8dct=1:cqm=jvt", false, false},
		{"cavlc_ip_high_multislice", 48, "cabac=0:keyint=24:bframes=0:slices=3:ref=3:weightp=2:8x8dct=0", false, true},
		{"cabac_ip_weight_matrix", 25, "cabac=1:keyint=24:bframes=0:slices=3:ref=3:weightp=2:8x8dct=1:cqm=jvt:chroma-qp-offset=-2", false, true},
		{"cavlc_b_spatial", 25, "cabac=0:keyint=24:bframes=2:b-adapt=0:slices=3:direct=spatial:weightb=1:8x8dct=1", true, true},
		// 此x264构建拒绝QP1+JVT（Impossible QP constraints for CQM）。
		// 分别保留低QP temporal B与JVT temporal B；低QP自定义矩阵另有编码三方Oracle。
		{"cabac_b_temporal_low", 1, "cabac=1:keyint=24:bframes=2:b-adapt=0:slices=3:direct=temporal:weightb=1:8x8dct=1", true, true},
		{"cabac_b_temporal_matrix", 23, "cabac=1:keyint=24:bframes=2:b-adapt=0:slices=3:direct=temporal:weightb=1:8x8dct=1:cqm=jvt", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := makeHigh10Oracle(t, ffmpeg, tc.qp, tc.params)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			// 确认动态输入确实包含指定的B帧与非首切片，避免配置被外部工具忽略。
			units, err := nal.SplitAnnexB(data)
			if err != nil {
				t.Fatal(err)
			}
			seenB, seenLaterSlice := false, false
			for _, u := range units {
				if !u.Header.IsVCL() {
					continue
				}
				rbsp, err := bitstream.EBSPToRBSP(u.Payload)
				if err != nil {
					t.Fatal(err)
				}
				r := bitstream.NewReader(rbsp)
				first, err := r.ReadUE()
				if err != nil {
					t.Fatal(err)
				}
				typ, err := r.ReadUE()
				if err != nil {
					t.Fatal(err)
				}
				seenB = seenB || typ%5 == uint32(syntax.SliceTypeB)
				seenLaterSlice = seenLaterSlice || first > 0
			}
			if seenB != tc.bframes || seenLaterSlice != tc.multislice {
				t.Fatalf("oracle tools: B=%v multislice=%v", seenB, seenLaterSlice)
			}
			d := mustDecoder(t, h264.DecoderConfig{})
			if err := d.SendPacket(h264.Packet{Data: data}); err != nil {
				t.Fatal(err)
			}
			if err := d.Flush(); err != nil {
				t.Fatal(err)
			}
			var formats []h264.PixelFormat
			got := collectNativeFrames(t, d, &formats)
			if len(formats) != 8 {
				t.Fatalf("decoded %d frames, want 8", len(formats))
			}
			for _, format := range formats {
				if format != h264.YUV420P10LE {
					t.Fatalf("native High10 output format %v", format)
				}
			}
			compareHigh10(t, got, high10FFmpegRaw(t, ffmpeg, path))
		})
	}
}

func TestHigh10_DepthSwitchAndRecovery(t *testing.T) {
	ffmpeg := high10FFmpeg(t)
	highPath := makeHigh10Oracle(t, ffmpeg, 27, "cabac=1:keyint=24:bframes=2:b-adapt=0:8x8dct=1")
	high, err := os.ReadFile(highPath)
	if err != nil {
		t.Fatal(err)
	}
	const lowPath = "../testdata/baseline/baseline_64x48_yuv420p_cavlc_ipp_001.264"
	low, err := os.ReadFile(lowPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("native_sps_switch", func(t *testing.T) {
		d := mustDecoder(t, h264.DecoderConfig{})
		var got, want []byte
		var formats []h264.PixelFormat
		for _, stream := range []struct {
			data   []byte
			path   string
			format h264.PixelFormat
		}{
			{low, lowPath, h264.YUV420P}, {high, highPath, h264.YUV420P10LE}, {low, lowPath, h264.YUV420P},
		} {
			if err := d.SendPacket(h264.Packet{Data: stream.data}); err != nil {
				t.Fatal(err)
			}
			got = append(got, collectNativeFrames(t, d, &formats)...)
			if stream.format == h264.YUV420P10LE {
				want = append(want, high10FFmpegRaw(t, ffmpeg, stream.path)...)
			} else {
				want = append(want, ffmpegRaw(t, ffmpeg, stream.path)...)
			}
		}
		if err := d.Flush(); err != nil {
			t.Fatal(err)
		}
		got = append(got, collectNativeFrames(t, d, &formats)...)
		if !bytes.Equal(got, want) {
			t.Fatal("native depth switch differs from independent per-sequence oracles")
		}
		seenHigh, seenLowAfter := false, false
		for _, format := range formats {
			seenHigh = seenHigh || format == h264.YUV420P10LE
			seenLowAfter = seenLowAfter || seenHigh && format == h264.YUV420P
		}
		if !seenLowAfter {
			t.Fatalf("missing native 8->10->8 switch: %v", formats)
		}
	})
	t.Run("corruption_then_idr", func(t *testing.T) {
		d := mustDecoder(t, h264.DecoderConfig{})
		units, err := nal.SplitAnnexB(high)
		if err != nil {
			t.Fatal(err)
		}
		var broken []byte
		for _, u := range units {
			if u.Header.Type == nal.TypeSPS || u.Header.Type == nal.TypePPS {
				broken = append(broken, annex(u)...)
			}
			if u.Header.IsIDR() {
				u.Payload = u.Payload[:len(u.Payload)/2]
				broken = append(broken, annex(u)...)
				break
			}
		}
		if err := d.SendPacket(h264.Packet{Data: broken}); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("truncated High10 IDR: %v", err)
		}
		if err := d.SendPacket(h264.Packet{Data: high}); err != nil {
			t.Fatal(err)
		}
		if err := d.Flush(); err != nil {
			t.Fatal(err)
		}
		compareHigh10(t, collectNativeFrames(t, d, nil), high10FFmpegRaw(t, ffmpeg, highPath))
	})
}

func high10SyntaxStream(t *testing.T, pcm bool, initialQP int, mixedPCM bool) []byte {
	t.Helper()
	s := &syntax.SPS{ProfileIDC: 110, LevelIDC: 10, ChromaFormatIDC: 1, BitDepthLumaMinus8: 2, BitDepthChromaMinus8: 2,
		MaxNumRefFrames: 1, FrameMbsOnlyFlag: true, Direct8x8InferenceFlag: true}
	if !pcm {
		s.PicWidthInMbsMinus1 = 3
	} else {
		s.FrameCroppingFlag = true
		s.FrameCropLeftOffset, s.FrameCropTopOffset = 1, 1
		if mixedPCM {
			s.PicWidthInMbsMinus1 = 1
		}
	}
	p := &syntax.PPS{DeblockingFilterControlPresentFlag: true}
	srb, err := s.MarshalRBSP()
	if err != nil {
		t.Fatal(err)
	}
	prb, err := p.MarshalRBSP(s)
	if err != nil {
		t.Fatal(err)
	}
	w := bitstream.NewWriter()
	w.WriteUE(0)
	w.WriteUE(2)
	w.WriteUE(0)
	w.WriteBits(4, 0)
	w.WriteUE(0)
	w.WriteBits(4, 0)
	w.WriteBits(2, 0)
	w.WriteSE(int32(initialQP - 26))
	if mixedPCM {
		w.WriteUE(0)
		w.WriteSE(0)
		w.WriteSE(0)
	} else {
		w.WriteUE(1)
	}
	if pcm {
		w.WriteUE(25)
		w.ByteAlign()
		for c := range 3 {
			size := 16
			if c > 0 {
				size = 8
			}
			for y := range size {
				for x := range size {
					value := (x*71 + y*43 + c*217) & 1023
					if mixedPCM {
						value = 512
						if c == 0 && x >= 12 {
							value = 500 + (x-12)*4 - 4
							if y < 4 {
								value += 16
							}
						}
					}
					w.WriteBits(10, uint64(value))
				}
			}
		}
		if mixedPCM {
			// 左侧PCM使lumaDC的nC为16；右侧I16使用当前slice QPY。
			w.WriteUE(3)
			w.WriteUE(0)
			w.WriteSE(0)
			var dc [16]int32
			if _, err := cavlc.EncodeBlock(w, &dc, cavlc.LumaDC, 16); err != nil {
				t.Fatal(err)
			}
		}
	} else {
		for _, delta := range []int32{-1, 1, -32, 31} {
			w.WriteUE(3) // I16x16 DC，亮度AC/色度均未编码。
			w.WriteUE(0)
			w.WriteSE(delta)
			var dc [16]int32
			dc[0] = 3
			if _, err := cavlc.EncodeBlock(w, &dc, cavlc.LumaDC, 0); err != nil {
				t.Fatal(err)
			}
		}
	}
	bitstream.WriteRBSPTrailingBits(w)
	unit := func(typ nal.UnitType, rbsp []byte) nal.Unit {
		return nal.Unit{Header: nal.Header{Type: typ, RefIDC: 3}, Payload: bitstream.RBSPToEBSP(rbsp)}
	}
	return annex(unit(nal.TypeSPS, srb), unit(nal.TypePPS, prb), unit(nal.TypeSliceIDR, w.Bytes()))
}

func TestHigh10_PCMAndNegativeQPOracle(t *testing.T) {
	ffmpeg := ffmpegPath(t)
	for _, tc := range []struct {
		pcm   bool
		qp    int
		mixed bool
	}{{true, -12, false}, {false, -12, false}, {false, 51, false}, {true, 51, true}} {
		t.Run(fmt.Sprintf("pcm_%v_qp_%d_mixed_%v", tc.pcm, tc.qp, tc.mixed), func(t *testing.T) {
			data := high10SyntaxStream(t, tc.pcm, tc.qp, tc.mixed)
			path := filepath.Join(t.TempDir(), "syntax.264")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			d := mustDecoder(t, h264.DecoderConfig{OutputFormat: h264.YUV420P10LE})
			if err := d.SendPacket(h264.Packet{Data: data}); err != nil {
				t.Fatal(err)
			}
			if err := d.Flush(); err != nil {
				t.Fatal(err)
			}
			compareHigh10(t, collectNativeFrames(t, d, nil), high10FFmpegRaw(t, ffmpeg, path))
		})
	}
}

func TestHigh10_PCMNativeCrop(t *testing.T) {
	d := mustDecoder(t, h264.DecoderConfig{})
	if err := d.SendPacket(h264.Packet{Data: high10SyntaxStream(t, true, -12, false), PTS: 42, Duration: 3}); err != nil {
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
	if f.Format != h264.YUV420P10LE || f.Width != 14 || f.Height != 14 || f.PTS != 42 || f.Duration != 3 || !f.KeyFrame {
		t.Fatalf("native cropped High10 frame metadata: %+v", f)
	}
	for c := range 3 {
		size, crop := 14, 2
		if c > 0 {
			size, crop = 7, 1
		}
		if f.Planes[c].Stride != size*2 {
			t.Fatalf("plane %d stride is not byte based: %d", c, f.Planes[c].Stride)
		}
		for y := range size {
			for x := range size {
				got := binary.LittleEndian.Uint16(f.Planes[c].Data[y*f.Planes[c].Stride+x*2:])
				want := uint16(((x+crop)*71 + (y+crop)*43 + c*217) & 1023)
				if got != want {
					t.Fatalf("plane %d (%d,%d) got %d want %d", c, x, y, got, want)
				}
			}
		}
	}
}

func TestHigh10_CorpusPixelOracle(t *testing.T) {
	ffmpeg := ffmpegPath(t)
	for _, path := range []string{
		"../testdata/high10/high10_32x32_yuv420p10le_cavlc_ip_001.264",
		"../testdata/high10/high10_48x32_yuv420p10le_cabac_ipb_002.264",
		"../testdata/high10/high10_32x32_yuv420p10le_cabac_lowqp_003.264",
	} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			compareHigh10(t, decodeFile(t, path), high10FFmpegRaw(t, ffmpeg, path))
		})
	}
}

func TestHigh10_ResourceBudget(t *testing.T) {
	d := mustDecoder(t, h264.DecoderConfig{MaxWidth: 32, MaxHeight: 32, MaxFrameSize: 3000})
	s := &syntax.SPS{ChromaFormatIDC: 1, PicWidthInMbsMinus1: 1, PicHeightInMapUnitsMinus1: 1, FrameMbsOnlyFlag: true, MaxNumRefFrames: 1}
	if err := d.checkSPSLimits(s); err != nil {
		t.Fatalf("8-bit budget contract changed: %v", err)
	}
	s.BitDepthLumaMinus8, s.BitDepthChromaMinus8 = 2, 2
	if err := d.checkSPSLimits(s); !errors.Is(err, h264.ErrResourceLimit) {
		t.Fatalf("High10 sample storage not charged: %v", err)
	}
}
