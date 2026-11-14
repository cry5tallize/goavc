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
	"github.com/goavc/h264/nal"
	"github.com/goavc/h264/syntax"
)

// 全部压缩样本由libx264生成；FFmpeg原生422输出是独立逐采样Oracle。
func high422FFmpeg(t *testing.T) string {
	t.Helper()
	ff := ffmpegPath(t)
	out, err := exec.Command(ff, "-hide_banner", "-h", "encoder=libx264").CombinedOutput()
	if err != nil {
		t.Fatalf("query libx264: %v\n%s", err, out)
	}
	for _, format := range []string{"yuv422p", "yuv422p10le"} {
		if !bytes.Contains(out, []byte(format)) {
			t.Skipf("libx264 does not advertise %s", format)
		}
	}
	return ff
}

func makeHigh422Oracle(t *testing.T, ff string, format h264.PixelFormat, qp int, params string) string {
	t.Helper()
	const width, frames = 94, 8
	height := 61 // 422垂直裁剪单位为1，不能丢弃最后一行。
	if _, subH := format.ChromaSubsampling(); subH == 2 {
		height = 62
	}
	var raw []byte
	for n := range frames {
		for c := range 3 {
			w, h := format.PlaneDimensions(c, width, height)
			for y := range h {
				for x := range w {
					v := 20 + ((x+2*n)%w*5+y*3+n*11+c*47)%190
					if x > w/2 && y > h/3 {
						v = (v + (x*y+17*n)%33) & 255
					}
					if format.BitDepth() > 8 {
						raw = binary.LittleEndian.AppendUint16(raw, uint16(v*4+(x+y+n+c)%4))
					} else {
						raw = append(raw, byte(v))
					}
				}
			}
		}
	}
	dir := t.TempDir()
	input, output := filepath.Join(dir, "input.yuv"), filepath.Join(dir, "high422.264")
	if err := os.WriteFile(input, raw, 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"-v", "error", "-y", "-f", "rawvideo", "-pixel_format", format.String(), "-video_size", fmt.Sprintf("%dx%d", width, height), "-framerate", "12", "-i", input,
		"-frames:v", strconv.Itoa(frames), "-c:v", "libx264", "-profile:v", "high422", "-preset", "medium", "-qp", strconv.Itoa(qp),
		"-x264-params", "threads=1:lookahead-threads=1:sync-lookahead=0:scenecut=0:aud=1:repeat-headers=1:" + params, "-f", "h264", output}
	if out, err := exec.Command(ff, args...).CombinedOutput(); err != nil {
		t.Fatalf("encode %v High422: %v\n%s", format, err, out)
	}
	return output
}

func high422Raw(t *testing.T, ff, path string, format h264.PixelFormat) []byte {
	t.Helper()
	cmd := exec.Command(ff, "-v", "error", "-i", path, "-fps_mode", "passthrough", "-f", "rawvideo", "-pix_fmt", format.String(), "-")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("decode High422 oracle: %v\n%s", err, stderr.String())
	}
	return out
}

func compareHigh422(t *testing.T, got, want []byte, format h264.PixelFormat) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("native %v decoded %d bytes, oracle %d", format, len(got), len(want))
	}
	step := format.BytesPerSample()
	for i := 0; i < len(got); i += step {
		g, w := uint16(got[i]), uint16(want[i])
		if step == 2 {
			g, w = binary.LittleEndian.Uint16(got[i:]), binary.LittleEndian.Uint16(want[i:])
			if g > 1023 {
				t.Fatalf("sample %d exceeds 10 bits: %d", i/step, g)
			}
		}
		if g != w {
			t.Fatalf("%v sample %d: got %d, oracle %d", format, i/step, g, w)
		}
	}
}

// 验证实际语法，不把配置字符串当成工具覆盖的证据。
func assertHigh422Tools(t *testing.T, data []byte, format h264.PixelFormat, entropy, bframes, multislice, matrix, weighted bool) {
	t.Helper()
	units, err := nal.SplitAnnexB(data)
	if err != nil {
		t.Fatal(err)
	}
	var s *syntax.SPS
	var p *syntax.PPS
	seenB, seenLater, seenP, seenI, seenHigh422 := false, false, false, false, false
	for _, u := range units {
		rbsp, err := bitstream.EBSPToRBSP(u.Payload)
		if err != nil {
			t.Fatal(err)
		}
		switch u.Header.Type {
		case nal.TypeSPS:
			s, err = syntax.ParseSPS(rbsp)
			if err != nil {
				t.Fatal(err)
			}
			_, subH := format.ChromaSubsampling()
			wantIDC := uint32(2)
			if subH == 2 {
				wantIDC = 1
			}
			if s.ChromaFormatIDC != wantIDC || int(s.BitDepthLumaMinus8)+8 != format.BitDepth() {
				t.Fatalf("oracle SPS does not carry native %v: %+v", format, s)
			}
			// x264的-profile设置上限；实际profile按用到的工具取最小值。
			seenHigh422 = seenHigh422 || s.ProfileIDC == 122
		case nal.TypePPS:
			p, err = syntax.ParsePPS(rbsp, s)
			if err != nil {
				t.Fatal(err)
			}
			if p.EntropyCodingModeFlag != entropy {
				t.Fatalf("oracle entropy CABAC=%v, want %v", p.EntropyCodingModeFlag, entropy)
			}
			if matrix && !p.PicScalingMatrixPresentFlag && !s.SeqScalingMatrixPresentFlag {
				t.Fatal("oracle omitted requested scaling matrices")
			}
			if weighted && !p.WeightedPredFlag {
				t.Fatal("oracle omitted requested P weights")
			}
		default:
			if !u.Header.IsVCL() {
				continue
			}
			r := bitstream.NewReader(rbsp)
			first, e := r.ReadUE()
			if e != nil {
				t.Fatal(e)
			}
			typ, e := r.ReadUE()
			if e != nil {
				t.Fatal(e)
			}
			seenB = seenB || typ%5 == uint32(syntax.SliceTypeB)
			seenP = seenP || typ%5 == uint32(syntax.SliceTypeP)
			seenI = seenI || typ%5 == uint32(syntax.SliceTypeI)
			seenLater = seenLater || first > 0
		}
	}
	if !seenI || seenB != bframes || seenLater != multislice || (weighted && !seenP) {
		t.Fatalf("oracle tools I=%v P=%v B=%v multislice=%v", seenI, seenP, seenB, seenLater)
	}
	if _, subH := format.ChromaSubsampling(); subH == 1 && !seenHigh422 {
		t.Fatal("422 oracle did not use High422 profile")
	}
}

func TestHigh422_DynamicPixelOracle(t *testing.T) {
	ff := high422FFmpeg(t)
	for _, format := range []h264.PixelFormat{h264.YUV422P, h264.YUV422P10LE} {
		for _, tc := range []struct {
			name                                         string
			qp                                           int
			params                                       string
			cabac, bframes, multislice, matrix, weighted bool
		}{
			{"cavlc_i_low", 1, "cabac=0:keyint=1:bframes=0:8x8dct=0", false, false, false, false, false},
			{"cabac_i_high", 48, "cabac=1:keyint=1:bframes=0:8x8dct=0", true, false, false, false, false},
			{"cavlc_i8_matrix", 23, "cabac=0:keyint=1:bframes=0:8x8dct=1:cqm=jvt", false, false, false, true, false},
			{"cabac_i8_matrix", 23, "cabac=1:keyint=1:bframes=0:8x8dct=1:cqm=jvt", true, false, false, true, false},
			{"cavlc_ip_high_multislice", 48, "cabac=0:keyint=24:bframes=0:slices=3:ref=3:weightp=2:8x8dct=0", false, false, true, false, true},
			{"cabac_ip_weight_matrix", 25, "cabac=1:keyint=24:bframes=0:slices=3:ref=3:weightp=2:8x8dct=1:cqm=jvt:chroma-qp-offset=-2", true, false, true, true, true},
			{"cavlc_b_spatial", 25, "cabac=0:keyint=24:bframes=2:b-adapt=0:slices=3:direct=spatial:weightb=1:8x8dct=1", false, true, true, false, false},
			{"cabac_b_temporal_low", 1, "cabac=1:keyint=24:bframes=2:b-adapt=0:slices=3:direct=temporal:weightb=1:8x8dct=1", true, true, true, false, false},
			{"cabac_b_temporal_matrix", 23, "cabac=1:keyint=24:bframes=2:b-adapt=0:slices=3:direct=temporal:weightb=1:8x8dct=1:cqm=jvt", true, true, true, true, false},
		} {
			t.Run(format.String()+"/"+tc.name, func(t *testing.T) {
				path := makeHigh422Oracle(t, ff, format, tc.qp, tc.params)
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				assertHigh422Tools(t, data, format, tc.cabac, tc.bframes, tc.multislice, tc.matrix, tc.weighted)
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
					t.Fatalf("decoded %d frames, want 8", len(formats))
				}
				for _, f := range formats {
					if f != format {
						t.Fatalf("native output %v, want %v", f, format)
					}
				}
				compareHigh422(t, got, high422Raw(t, ff, path, format), format)
			})
		}
	}
}

func TestHigh422_FormatSwitchAndRecovery(t *testing.T) {
	ff := high422FFmpeg(t)
	var streams []struct {
		data, want []byte
		format     h264.PixelFormat
	}
	for _, format := range []h264.PixelFormat{h264.YUV420P, h264.YUV422P10LE, h264.YUV422P, h264.YUV420P10LE, h264.YUV422P10LE, h264.YUV420P} {
		path := makeHigh422Oracle(t, ff, format, 23, "cabac=1:keyint=24:bframes=2:b-adapt=0:slices=3:8x8dct=1")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		streams = append(streams, struct {
			data, want []byte
			format     h264.PixelFormat
		}{data, high422Raw(t, ff, path, format), format})
	}
	t.Run("native_switch", func(t *testing.T) {
		d := mustDecoder(t, h264.DecoderConfig{})
		var got, want []byte
		var formats []h264.PixelFormat
		for _, stream := range streams {
			if err := d.SendPacket(h264.Packet{Data: stream.data}); err != nil {
				t.Fatal(err)
			}
			got = append(got, collectNativeFrames(t, d, &formats)...)
			want = append(want, stream.want...)
		}
		if err := d.Flush(); err != nil {
			t.Fatal(err)
		}
		got = append(got, collectNativeFrames(t, d, &formats)...)
		if !bytes.Equal(got, want) {
			t.Fatalf("native format switching changed samples: got %d bytes, want %d", len(got), len(want))
		}
		if len(formats) != len(streams)*8 {
			t.Fatalf("switch output frames %d", len(formats))
		}
		for i, f := range formats {
			if f != streams[i/8].format {
				t.Fatalf("switch frame %d format %v, want %v", i, f, streams[i/8].format)
			}
		}
	})
	t.Run("truncation_idr", func(t *testing.T) {
		d := mustDecoder(t, h264.DecoderConfig{})
		stream := streams[1]
		units, err := nal.SplitAnnexB(stream.data)
		if err != nil {
			t.Fatal(err)
		}
		var prefix []nal.Unit
		for _, u := range units {
			if u.Header.IsVCL() {
				u.Payload = u.Payload[:min(4, len(u.Payload))]
				prefix = append(prefix, u)
				break
			}
			prefix = append(prefix, u)
		}
		if err := d.SendPacket(h264.Packet{Data: annex(prefix...)}); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("truncated 422 slice: %v", err)
		}
		if err := d.SendPacket(h264.Packet{Data: stream.data}); err != nil {
			t.Fatal(err)
		}
		if err := d.Flush(); err != nil {
			t.Fatal(err)
		}
		compareHigh422(t, collectNativeFrames(t, d, nil), stream.want, stream.format)
	})
}
