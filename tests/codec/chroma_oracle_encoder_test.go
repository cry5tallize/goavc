package codec_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/decoder"
	"github.com/goavc/h264/encoder"
	"github.com/goavc/h264/internal/testutil/chromaplane"
	"github.com/goavc/h264/nal"
	"github.com/goavc/h264/syntax"
)

var chromaEncoderFormats = [...]h264.PixelFormat{
	h264.YUV420P, h264.YUV420P10LE,
	h264.YUV422P, h264.YUV422P10LE,
	h264.YUV444P, h264.YUV444P10LE,
}

func chromaEncoderConfig(format h264.PixelFormat) h264.EncoderConfig {
	cfg := h264.DefaultEncoderConfig()
	cfg.PixelFormat, cfg.Width, cfg.Height = format, 34, 18
	subW, subH := format.ChromaSubsampling()
	switch {
	case subW == 1:
		cfg.Profile, cfg.Width, cfg.Height = h264.ProfileHigh444, 35, 19
	case subH == 1:
		cfg.Profile, cfg.Height = h264.ProfileHigh422, 19
	case format.BitDepth() == 10:
		cfg.Profile = h264.ProfileHigh10
	default:
		cfg.Profile = h264.ProfileHigh
	}
	cfg.GOPSize, cfg.IDRInterval = 5, 13
	return cfg
}

// 首帧平滑以排除PCM退路；后续帧含平移、独立色度细节和亮度变化。
// 每行附加非零填充，10位图案同时使用高位和低两位。
func chromaEncoderFrame(cfg h264.EncoderConfig, index int) *h264.Frame {
	f := &h264.Frame{Width: cfg.Width, Height: cfg.Height, Format: cfg.PixelFormat, PTS: int64(index) * 100, Duration: 100}
	step := f.Format.BytesPerSample()
	for c := range f.Planes {
		w, h := f.Format.PlaneDimensions(c, f.Width, f.Height)
		stride := w*step + 6
		f.Planes[c] = h264.Plane{Stride: stride, Data: bytes.Repeat([]byte{0xa5}, stride*h)}
		for y := range h {
			for x := range w {
				v := 80 + c*23 + x*2 + y + index*3
				if index > 0 && (x+index+c*3)/8%2 != 0 {
					v += 17 + c*5
				}
				v %= 256
				if step == 2 {
					v = v*4 + (x+y+c+index)%4
					binary.LittleEndian.PutUint16(f.Planes[c].Data[y*stride+x*step:], uint16(v))
				} else {
					f.Planes[c].Data[y*stride+x] = byte(v)
				}
			}
		}
	}
	return f
}

func chromaEncoderPacked(t *testing.T, f *h264.Frame, cfg h264.EncoderConfig) []byte {
	t.Helper()
	if f.Format != cfg.PixelFormat || f.Width != cfg.Width || f.Height != cfg.Height {
		t.Fatalf("输出格式/裁剪改变：%s %dx%d，期望%s %dx%d", f.Format, f.Width, f.Height, cfg.PixelFormat, cfg.Width, cfg.Height)
	}
	var out []byte
	step := f.Format.BytesPerSample()
	for c := range f.Planes {
		w, h := f.Format.PlaneDimensions(c, f.Width, f.Height)
		for y := range h {
			row := f.Planes[c].Data[y*f.Planes[c].Stride:][:w*step]
			if step == 2 {
				for x := range w {
					if binary.LittleEndian.Uint16(row[x*2:]) > 1023 {
						t.Fatalf("平面%d (%d,%d)含非法10位采样", c, x, y)
					}
				}
			}
			out = append(out, row...)
		}
	}
	return out
}

func chromaEncoderEqual(t *testing.T, label string, got, want []byte, step int) {
	t.Helper()
	if bytes.Equal(got, want) {
		return
	}
	for i := 0; i+step <= min(len(got), len(want)); i += step {
		if !bytes.Equal(got[i:i+step], want[i:i+step]) {
			g, w := uint16(got[i]), uint16(want[i])
			if step == 2 {
				g, w = binary.LittleEndian.Uint16(got[i:]), binary.LittleEndian.Uint16(want[i:])
			}
			t.Fatalf("%s采样%d=%d，本地重建%d", label, i/step, g, w)
		}
	}
	t.Fatalf("%s长度%d，本地重建%d", label, len(got), len(want))
}

// 仅独立Oracle缺少可执行文件时跳过，编码或解码不支持均是失败。
func chromaEncoderFFmpegCheck(t *testing.T, stream, want []byte, format h264.PixelFormat) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("FFmpeg不可用，独立色度Oracle未执行")
	}
	path := filepath.Join(t.TempDir(), "chroma.264")
	if err := os.WriteFile(path, stream, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("ffmpeg", "-v", "warning", "-xerror", "-i", path, "-f", "rawvideo", "-pix_fmt", format.String(), "pipe:1")
	var log bytes.Buffer
	cmd.Stderr = &log
	got, err := cmd.Output()
	if err != nil || log.Len() != 0 {
		t.Fatalf("FFmpeg %s解码：%v，%s", format, err, log.String())
	}
	chromaEncoderEqual(t, "FFmpeg", got, want, format.BytesPerSample())
}

type chromaEncoderTools struct {
	cabac, separate, transform8, matrices, weighted bool
	lowBits, pcm                                    bool
}

func chromaEncoderOracle(t *testing.T, cfg h264.EncoderConfig, tools chromaEncoderTools, options ...encoder.Option) {
	t.Helper()
	options = append([]encoder.Option{encoder.WithCABAC(tools.cabac), encoder.WithSliceCount(3), encoder.WithSearchRange(2)}, options...)
	e, err := encoder.New(cfg, options...)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	d, err := decoder.New(h264.DecoderConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	const count = 12
	recons := make(map[int64][]byte, count)
	var stream []byte
	var sps *syntax.SPS
	var pps *syntax.PPS
	parameterSets := make(map[uint32]*syntax.PPS)
	packets, decoded := 0, 0
	var slices [3]int
	var planes [3]int
	step := cfg.PixelFormat.BytesPerSample()
	weightedSamples := false
	sources := make(map[int64][]byte, count)
	drainDecoder := func() {
		t.Helper()
		for {
			f, err := d.ReceiveFrame()
			if errors.Is(err, h264.ErrAgain) || errors.Is(err, h264.ErrEOF) {
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if f.PTS != int64(decoded)*100 {
				t.Fatalf("显示帧%d PTS=%d，顺序错误", decoded, f.PTS)
			}
			want, ok := recons[f.PTS]
			if !ok {
				t.Fatalf("显示帧%d没有本地重建", f.PTS)
			}
			chromaEncoderEqual(t, fmt.Sprintf("自研显示帧%d", decoded), chromaEncoderPacked(t, f, cfg), want, step)
			decoded++
			f.Release()
		}
	}
	drainEncoder := func() {
		t.Helper()
		for {
			p, err := e.ReceivePacket()
			if errors.Is(err, h264.ErrAgain) || errors.Is(err, h264.ErrEOF) {
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			r, err := e.Reconstruction()
			if err != nil {
				t.Fatal(err)
			}
			if r.PTS != p.PTS {
				t.Fatal("重建快照与包不同步")
			}
			if _, exists := recons[p.PTS]; exists {
				t.Fatalf("重复编码PTS%d", p.PTS)
			}
			recons[p.PTS] = chromaEncoderPacked(t, r, cfg)
			if tools.pcm {
				chromaEncoderEqual(t, "PCM本地重建", recons[p.PTS], sources[p.PTS], step)
			}
			r.Release()
			if packets == 0 && !tools.pcm {
				sw, sh := cfg.PixelFormat.ChromaSubsampling()
				rawBytes := ((cfg.Width + 15) / 16) * ((cfg.Height + 15) / 16) * (256 + 2*(16/sw)*(16/sh)) * cfg.PixelFormat.BitDepth() / 8
				if len(p.Data) >= rawBytes {
					t.Fatalf("平滑首帧未真实压缩：%d字节，原始宏块采样%d字节", len(p.Data), rawBytes)
				}
			}
			stream = append(stream, p.Data...)
			var packetPlanes [3]int
			scanner := nal.NewAnnexBScanner(bytes.NewReader(p.Data))
			for {
				u, err := scanner.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				payload := u.Payload
				if len(payload) >= 3 && bytes.Equal(payload[len(payload)-3:], []byte{0, 0, 3}) {
					payload = payload[:len(payload)-1]
				}
				rb, err := bitstream.EBSPToRBSP(payload)
				if err != nil {
					t.Fatal(err)
				}
				switch u.Header.Type {
				case nal.TypeSPS:
					sps, err = syntax.ParseSPS(rb)
					if err == nil {
						profile, chroma := uint8(100), uint32(1)
						sw, sh := cfg.PixelFormat.ChromaSubsampling()
						switch {
						case sw == 1:
							profile, chroma = 244, 3
						case sh == 1:
							profile, chroma = 122, 2
						case step == 2:
							profile = 110
						}
						depth := uint32(cfg.PixelFormat.BitDepth() - 8)
						if sps.ProfileIDC != profile || sps.ChromaFormatIDC != chroma || sps.BitDepthLumaMinus8 != depth || sps.BitDepthChromaMinus8 != depth || sps.SeparateColourPlaneFlag != tools.separate {
							t.Fatalf("SPS未保留%s或独立平面配置：%+v", cfg.PixelFormat, sps)
						}
					}
				case nal.TypePPS:
					pps, err = syntax.ParsePPS(rb, sps)
					if err == nil {
						parameterSets[pps.PicParameterSetID] = pps
					}
					if err == nil && (pps.EntropyCodingModeFlag != tools.cabac || pps.Transform8x8ModeFlag != tools.transform8 || tools.weighted && !pps.WeightedPredFlag) {
						t.Fatalf("PPS丢失熵/8x8/权重配置：%+v", pps)
					}
					if err == nil && tools.matrices && !pps.PicScalingMatrixPresentFlag && !sps.SeqScalingMatrixPresentFlag {
						t.Fatal("自定义量化矩阵未写入参数集")
					}
					if err == nil && tools.matrices && !tools.separate {
						selected, selectErr := syntax.SelectScalingMatrices(sps, pps)
						if selectErr != nil {
							t.Fatal(selectErr)
						}
						for list := range selected.Four {
							for i, got := range selected.Four[list] {
								if want := uint8(8 + (i*3+list*5)%23); got != want {
									t.Fatalf("4x4矩阵%d项%d=%d，配置%d", list, i, got, want)
								}
							}
						}
						lists := 2
						if sps.ChromaFormatIDC == 3 {
							lists = 6
						}
						for list := range lists {
							for i, got := range selected.Eight[list] {
								if want := uint8(9 + (i*5+list*7)%29); got != want {
									t.Fatalf("8x8矩阵%d项%d=%d，配置%d", list, i, got, want)
								}
							}
						}
					}
				case nal.TypeSlice, nal.TypeSliceIDR:
					if sps == nil || pps == nil {
						t.Fatal("slice先于参数集")
					}
					var h *syntax.SliceHeader
					h, err = syntax.ParseSliceHeaderEx(rb, sps, parameterSets, u.Header.Type == nal.TypeSliceIDR, u.Header.RefIDC)
					if err == nil {
						switch h.Type {
						case syntax.SliceTypeI:
							slices[0]++
						case syntax.SliceTypeP:
							slices[1]++
						case syntax.SliceTypeB:
							slices[2]++
						}
						if h.ColourPlaneID > 2 {
							t.Fatalf("非法独立平面编号%d", h.ColourPlaneID)
						}
						planes[h.ColourPlaneID]++
						packetPlanes[h.ColourPlaneID]++
						if tools.weighted && h.Type == syntax.SliceTypeP {
							if !h.HasPredWeightTable || h.PredictionWeights == nil {
								t.Fatal("P slice未实际携带预测权重")
							}
							weights := h.PredictionWeights
							for _, entry := range weights.Lists[0] {
								components := 3
								if tools.separate {
									components = 1
								}
								for c := range components {
									denom := weights.LumaDenom
									if c > 0 {
										denom = weights.ChromaDenom
									}
									weightedSamples = weightedSamples || entry.Weight[c] != 1<<denom || entry.Offset[c] != 0
								}
							}
						}
					}
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			wantPlanes := [3]int{3, 0, 0}
			if tools.separate {
				wantPlanes = [3]int{3, 3, 3}
			}
			if packetPlanes != wantPlanes {
				t.Fatalf("AU %d平面slice集合=%v，期望%v", packets, packetPlanes, wantPlanes)
			}
			if err = d.SendPacket(p); err != nil {
				t.Fatalf("解码包%d PTS%d：%v", packets, p.PTS, err)
			}
			packets++
			drainDecoder()
		}
	}
	for i := range count {
		f := chromaEncoderFrame(cfg, i)
		if tools.pcm {
			sources[f.PTS] = chromaEncoderPacked(t, f, cfg)
		}
		if err = e.SendFrame(f); err != nil {
			t.Fatalf("送入帧%d：%v", i, err)
		}
		// 验证B帧和前瞻延迟不能借用调用方输入。
		for c := range f.Planes {
			clear(f.Planes[c].Data)
		}
		f.Release()
		drainEncoder()
	}
	if err = e.Flush(); err != nil {
		t.Fatal(err)
	}
	drainEncoder()
	if err = d.Flush(); err != nil {
		t.Fatal(err)
	}
	drainDecoder()
	if packets != count || decoded != count || slices[0] == 0 || !tools.pcm && slices[1] == 0 || cfg.MaxBFrames > 0 && slices[2] == 0 {
		t.Fatalf("包%d，显示帧%d，I/P/B slices=%v", packets, decoded, slices)
	}
	if tools.separate && (planes[0] == 0 || planes[1] == 0 || planes[2] == 0) {
		t.Fatalf("独立平面slice缺失：%v", planes)
	}
	if tools.weighted && !weightedSamples {
		t.Fatal("权重工具只写PPS标志，所有P slice仍使用恒等权重")
	}
	var want []byte
	var lowBits, above8 [3]bool
	for i := range count {
		frame := recons[int64(i)*100]
		want = append(want, frame...)
		if step == 2 {
			offset := 0
			for c := range lowBits {
				w, h := cfg.PixelFormat.PlaneDimensions(c, cfg.Width, cfg.Height)
				end := offset + w*h*step
				for j := offset; j < end; j += step {
					v := binary.LittleEndian.Uint16(frame[j:])
					lowBits[c] = lowBits[c] || v&3 != 0
					above8[c] = above8[c] || v > 255
				}
				offset = end
			}
		}
	}
	if step == 2 {
		for c := range lowBits {
			if !above8[c] || tools.lowBits && !lowBits[c] {
				t.Fatalf("平面%d丢失10位高位或有效低两位：高位%t，低位%t", c, above8[c], lowBits[c])
			}
		}
	}
	if tools.separate {
		chromaEncoderPlaneOracle(t, stream, want, cfg)
	} else {
		chromaEncoderFFmpegCheck(t, stream, want, cfg.PixelFormat)
	}
}

// FFmpeg不支持独立平面封装；按规范投影成单色码流后逐平面验证相同slice_data。
// 原始封装由固定PCM解析解与解码器覆盖，投影不声称FFmpeg可解原始封装。
func chromaEncoderPlaneOracle(t *testing.T, stream, want []byte, cfg h264.EncoderConfig) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("FFmpeg不可用，单色投影Oracle未执行")
	}
	projected, err := chromaplane.Project(stream)
	if err != nil {
		t.Fatal(err)
	}
	// 不经过gray的有限范围到全范围转换，只取单色投影的Y平面。
	pixelFormat := cfg.PixelFormat.String()
	step := cfg.PixelFormat.BytesPerSample()
	planeBytes := cfg.Width * cfg.Height * step
	for comp := range 3 {
		path := filepath.Join(t.TempDir(), "plane.264")
		if err := os.WriteFile(path, projected[comp], 0600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("ffmpeg", "-v", "warning", "-xerror", "-i", path, "-f", "rawvideo", "-pix_fmt", pixelFormat, "pipe:1")
		var log bytes.Buffer
		cmd.Stderr = &log
		got, err := cmd.Output()
		if err != nil || log.Len() != 0 {
			t.Fatalf("plane%d FFmpeg投影解码：%v，%s", comp, err, log.String())
		}
		if len(got) != len(want) {
			t.Fatalf("plane%d projected frame bytes %d, want %d", comp, len(got), len(want))
		}
		var samples []byte
		for offset := 0; offset < len(got); offset += 3 * planeBytes {
			samples = append(samples, got[offset:offset+planeBytes]...)
		}
		var expected []byte
		for offset := comp * planeBytes; offset < len(want); offset += 3 * planeBytes {
			expected = append(expected, want[offset:offset+planeBytes]...)
		}
		chromaEncoderEqual(t, "FFmpeg单色投影", samples, expected, step)
	}
}

func chromaEncoderMatrices(format h264.PixelFormat) []encoder.Option {
	var four [6][16]uint8
	var eight [6][64]uint8
	for list := range four {
		for i := range four[list] {
			four[list][i] = uint8(8 + (i*3+list*5)%23)
		}
		for i := range eight[list] {
			eight[list][i] = uint8(9 + (i*5+list*7)%29)
		}
	}
	options := []encoder.Option{encoder.WithScalingMatrices4x4(four)}
	if sw, _ := format.ChromaSubsampling(); sw == 1 {
		return append(options, encoder.WithScalingMatrices8x8Chroma(eight))
	}
	return append(options, encoder.WithScalingMatrix8x8(eight[0], eight[1]))
}

func TestChromaEncoder_QPExtremesThreeWayOracle(t *testing.T) {
	for _, format := range chromaEncoderFormats {
		qps := []int{0, 26, 51}
		if format.BitDepth() == 10 {
			qps = []int{-12, -1, 0, 26, 51}
		}
		for _, cabac := range []bool{false, true} {
			for _, qp := range qps {
				t.Run(fmt.Sprintf("%s/cabac%t/qp%d", format, cabac, qp), func(t *testing.T) {
					cfg := chromaEncoderConfig(format)
					cfg.QP = qp
					chromaEncoderOracle(t, cfg, chromaEncoderTools{cabac: cabac, lowBits: qp <= 0})
				})
			}
		}
	}
}

func TestChromaEncoder_BToolsAndRateControlThreeWayOracle(t *testing.T) {
	for _, format := range chromaEncoderFormats {
		for _, cabac := range []bool{false, true} {
			for _, rc := range []h264.RateControl{h264.RateControlCQP, h264.RateControlABR, h264.RateControlCBR, h264.RateControlVBV} {
				t.Run(fmt.Sprintf("%s/cabac%t/%s", format, cabac, rc), func(t *testing.T) {
					cfg := chromaEncoderConfig(format)
					cfg.QP, cfg.MaxBFrames, cfg.GOPSize, cfg.IDRInterval = 23, 2, 7, 17
					cfg.RateControl = rc
					if rc != h264.RateControlCQP {
						cfg.Bitrate, cfg.MaxBitrate, cfg.VBVBufferSize = 100_000, 150_000, 150_000
					}
					options := chromaEncoderMatrices(format)
					options = append(options, encoder.WithTransform8x8(), encoder.WithWeightedPrediction(), encoder.WithAdaptiveQP(3), encoder.WithLookahead(1))
					chromaEncoderOracle(t, cfg, chromaEncoderTools{cabac: cabac, transform8: true, matrices: true, weighted: true}, options...)
				})
			}
		}
	}
}

func TestChromaEncoder_MacroblockQPAndExplicitWeightsThreeWayOracle(t *testing.T) {
	for _, format := range chromaEncoderFormats {
		for _, cabac := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/cabac%t", format, cabac), func(t *testing.T) {
				cfg := chromaEncoderConfig(format)
				minimum := 0
				if format.BitDepth() == 10 {
					minimum = -12
				}
				cfg.QP = minimum
				options := chromaEncoderMatrices(format)
				options = append(options, encoder.WithTransform8x8(), encoder.WithMacroblockQP(func(x, y int) int {
					return [...]int{minimum, 0, 26, 51, 1, 50}[(y*3+x)%6]
				}), encoder.WithPredictionWeights(encoder.PredictionWeights{Weight: [3]int32{1, 1, 1}, Offset: [3]int32{1, -2, 3}}))
				chromaEncoderOracle(t, cfg, chromaEncoderTools{cabac: cabac, transform8: true, matrices: true, weighted: true}, options...)
			})
		}
	}
}

func TestChromaEncoder_SeparateColourPlanesThreeWayOracle(t *testing.T) {
	for _, format := range []h264.PixelFormat{h264.YUV444P, h264.YUV444P10LE} {
		for _, cabac := range []bool{false, true} {
			for _, bframes := range []int{0, 2} {
				t.Run(fmt.Sprintf("%s/cabac%t/b%d", format, cabac, bframes), func(t *testing.T) {
					cfg := chromaEncoderConfig(format)
					cfg.QP, cfg.MaxBFrames, cfg.GOPSize = 0, bframes, 7
					options := chromaEncoderMatrices(format)
					options = append(options, encoder.WithSeparateColourPlanes(), encoder.WithTransform8x8(), encoder.WithWeightedPrediction(), encoder.WithLookahead(1))
					chromaEncoderOracle(t, cfg, chromaEncoderTools{cabac: cabac, separate: true, transform8: true, matrices: true, weighted: true, lowBits: true}, options...)
				})
			}
		}
	}
}

func TestChromaEncoder_PCMThreeWayOracle(t *testing.T) {
	for _, format := range chromaEncoderFormats {
		for _, cabac := range []bool{false, true} {
			separates := []bool{false}
			if sw, _ := format.ChromaSubsampling(); sw == 1 {
				separates = append(separates, true)
			}
			for _, separate := range separates {
				t.Run(fmt.Sprintf("%s/cabac%t/separate%t", format, cabac, separate), func(t *testing.T) {
					cfg := chromaEncoderConfig(format)
					options := []encoder.Option{encoder.WithPCM()}
					if separate {
						options = append(options, encoder.WithSeparateColourPlanes())
					}
					chromaEncoderOracle(t, cfg, chromaEncoderTools{cabac: cabac, separate: separate, pcm: true, lowBits: true}, options...)
				})
			}
		}
	}
}
