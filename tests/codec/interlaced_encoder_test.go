package codec_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/decoder"
	"github.com/goavc/h264/encoder"
	"github.com/goavc/h264/internal/entropy/cabac"
	"github.com/goavc/h264/nal"
	"github.com/goavc/h264/syntax"
)

// 相邻帧行来自不同时间的场；每场内部平滑并有独立水平运动和色度细节。
func interlacedEncoderFrame(cfg h264.EncoderConfig, index int) *h264.Frame {
	f := chromaEncoderFrame(cfg, index)
	step := f.Format.BytesPerSample()
	for comp := range f.Planes {
		w, h := f.Format.PlaneDimensions(comp, f.Width, f.Height)
		for y := range h {
			for x := range w {
				phase := index/2*2 + y%2
				v := 32 + comp*19 + y/2 + (x+phase*(1+comp))%32*2 + y%2*80
				if step == 2 {
					v = v*4 + (x+y/2+phase)%4
					binary.LittleEndian.PutUint16(f.Planes[comp].Data[y*f.Planes[comp].Stride+x*step:], uint16(v))
				} else {
					f.Planes[comp].Data[y*f.Planes[comp].Stride+x] = byte(v)
				}
			}
		}
	}
	return f
}

func interlacedEncoderHeaders(t *testing.T, data []byte, topFirst, entropy bool, sliceCount int) bool {
	t.Helper()
	units, err := nal.SplitAnnexB(data)
	if err != nil {
		t.Fatal(err)
	}
	var sps *syntax.SPS
	sets := make(map[uint32]*syntax.PPS)
	seenSEI, seenSlices := false, 0
	for _, u := range units {
		payload := u.Payload
		// CABAC的cabac_zero_word可能产生Annex-B末尾转义零。
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
			if err == nil && (sps.FrameMbsOnlyFlag || !sps.MbAdaptiveFrameFieldFlag || sps.VUI == nil || !sps.VUI.PicStructPresentFlag) {
				t.Fatal("未写真实MBAFF/VUI参数")
			}
		case nal.TypePPS:
			var p *syntax.PPS
			p, err = syntax.ParsePPS(rb, sps)
			if err == nil {
				sets[p.PicParameterSetID] = p
			}
		case nal.TypeSEI:
			want := byte(4)
			if topFirst {
				want = 3
			}
			if !bytes.Equal(rb, []byte{1, 1, want<<4 | 2, 0x80}) {
				t.Fatalf("pic_timing场顺序/时间戳标志错误：%x", rb)
			}
			seenSEI = true
		case nal.TypeSliceIDR:
			var h *syntax.SliceHeader
			h, err = syntax.ParseSliceHeaderEx(rb, sps, sets, true, u.Header.RefIDC)
			if err != nil {
				t.Fatal(err)
			}
			if h.FieldPicFlag || h.Type != syntax.SliceTypeI {
				t.Fatal("MBAFF编码必须为完整I帧而不是两个PAFF场")
			}
			r := bitstream.NewReader(rb)
			if err = r.SkipBits(uint(h.HeaderBits)); err != nil {
				t.Fatal(err)
			}
			if entropy {
				for r.BitsRead()%8 != 0 {
					if b, err := r.ReadBit(); err != nil || b != 1 {
						t.Fatal("CABAC对齐位", b, err)
					}
				}
				ctx, err := cabac.InitContexts(true, 0, min(51, max(0, h.SliceQP)))
				if err != nil {
					t.Fatal(err)
				}
				c, err := cabac.New(r)
				if err != nil {
					t.Fatal(err)
				}
				if c.Bin(&ctx[70]) != 1 || c.Err() != nil {
					t.Fatal("CABAC首宏块对未真实field-coded", c.Err())
				}
			} else if b, err := r.ReadBit(); err != nil || b != 1 {
				t.Fatal("CAVLC首宏块对未真实field-coded", b, err)
			}
			seenSlices++
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if !seenSEI || sps == nil || seenSlices != sliceCount*(1+2*boolInt(sps.SeparateColourPlaneFlag)) {
		t.Fatalf("缺少隔行时序或slice：SEI=%t slices=%d", seenSEI, seenSlices)
	}
	return sps.SeparateColourPlaneFlag
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func interlacedEncoderOracle(t *testing.T, cfg h264.EncoderConfig, topFirst, entropy bool, slices int, options ...encoder.Option) {
	t.Helper()
	options = append(options, encoder.WithInterlaced(topFirst), encoder.WithCABAC(entropy), encoder.WithSliceCount(slices), encoder.WithSearchRange(2))
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
	const count = 6
	recons := make(map[int64][]byte, count)
	var stream, want, decodedPixels []byte
	packets, decoded := 0, 0
	separate := false
	drainDecoder := func() {
		for {
			f, err := d.ReceiveFrame()
			if errors.Is(err, h264.ErrAgain) || errors.Is(err, h264.ErrEOF) {
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if f.PTS != int64(decoded)*100 {
				t.Fatalf("隔行显示重排错误：帧%d PTS%d", decoded, f.PTS)
			}
			pixels := chromaEncoderPacked(t, f, cfg)
			if !bytes.Equal(pixels, recons[f.PTS]) {
				step := cfg.PixelFormat.BytesPerSample()
				for offset := 0; offset < min(len(pixels), len(recons[f.PTS])); offset += step {
					if !bytes.Equal(pixels[offset:offset+step], recons[f.PTS][offset:offset+step]) {
						t.Errorf("MBAFF显示帧%d PTS%d首差采样%d：解码%x 本地%x", decoded, f.PTS, offset/step, pixels[offset:offset+step], recons[f.PTS][offset:offset+step])
						break
					}
				}
				if len(pixels) != len(recons[f.PTS]) {
					t.Errorf("MBAFF帧%d像素长度%d，重建%d", decoded, len(pixels), len(recons[f.PTS]))
				}
			}
			decodedPixels = append(decodedPixels, pixels...)
			want = append(want, recons[f.PTS]...)
			decoded++
			f.Release()
		}
	}
	drainEncoder := func() {
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
			recons[p.PTS] = chromaEncoderPacked(t, r, cfg)
			r.Release()
			if packets == 0 {
				separate = interlacedEncoderHeaders(t, p.Data, topFirst, entropy, slices)
				if len(p.Data) >= len(recons[p.PTS]) {
					t.Fatalf("首帧未真实压缩：%d >= %d", len(p.Data), len(recons[p.PTS]))
				}
			}
			packets++
			stream = append(stream, p.Data...)
			if err = d.SendPacket(p); err != nil {
				t.Fatal(err)
			}
			drainDecoder()
		}
	}
	for index := range count {
		f := interlacedEncoderFrame(cfg, index)
		if err = e.SendFrame(f); err != nil {
			t.Fatalf("输入%d：%v", index, err)
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
	if packets != count || decoded != count {
		t.Fatalf("隔行帧丢失：包%d 解码%d", packets, decoded)
	}
	if t.Failed() {
		// 仅失败时保留完整流和两侧像素，让外部Oracle可独立判定错误位于哪侧。
		dir, err := os.MkdirTemp("", "goavc-interlaced-")
		if err != nil {
			t.Fatal(err)
		}
		for name, data := range map[string][]byte{"stream.264": stream, "recon.yuv": want, "decoded.yuv": decodedPixels} {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
		t.Logf("MBAFF三角像素证据：%s，%s %dx%d", dir, cfg.PixelFormat, cfg.Width, cfg.Height)
	}
	if separate {
		// 原始三平面封装仍由上面的自解码逐像素验证；FFmpeg独立解码各场投影。
		chromaEncoderPlaneOracle(t, stream, want, cfg)
	} else {
		chromaEncoderFFmpegCheck(t, stream, want, cfg.PixelFormat)
	}
}

func TestEncoder_InterlacedFieldOrderOracle(t *testing.T) {
	for _, format := range chromaEncoderFormats {
		for _, entropy := range []bool{false, true} {
			for _, topFirst := range []bool{false, true} {
				for _, slices := range []int{1, 3} {
					t.Run(fmt.Sprintf("%s/cabac%t/tff%t/slices%d", format, entropy, topFirst, slices), func(t *testing.T) {
						cfg := chromaEncoderConfig(format)
						cfg.Height, cfg.GOPSize, cfg.IDRInterval = 36, 4, 20
						interlacedEncoderOracle(t, cfg, topFirst, entropy, slices)
					})
				}
			}
		}
	}
}

func TestEncoder_InterlacedSharedToolsOracle(t *testing.T) {
	for _, format := range chromaEncoderFormats {
		for _, entropy := range []bool{false, true} {
			for _, bframes := range []int{0, 2} {
				t.Run(fmt.Sprintf("%s/cabac%t/b%d", format, entropy, bframes), func(t *testing.T) {
					cfg := chromaEncoderConfig(format)
					cfg.Height, cfg.MaxBFrames, cfg.GOPSize, cfg.IDRInterval = 36, bframes, 20, 40
					options := []encoder.Option{encoder.WithTransform8x8(), encoder.WithWeightedPrediction()}
					var four [6][16]uint8
					for list := range four {
						for index := range four[list] {
							four[list][index] = uint8(8 + (list*3+index)%19)
						}
					}
					options = append(options, encoder.WithScalingMatrices4x4(four))
					var eight [6][64]uint8
					for list := range eight {
						for index := range eight[list] {
							eight[list][index] = uint8(9 + (list*7+index)%23)
						}
					}
					if sw, _ := format.ChromaSubsampling(); sw == 1 {
						options = append(options, encoder.WithScalingMatrices8x8Chroma(eight))
					} else {
						options = append(options, encoder.WithScalingMatrix8x8(eight[0], eight[1]))
					}
					interlacedEncoderOracle(t, cfg, bframes == 0, entropy, 3, options...)
				})
			}
		}
	}
	for _, entropy := range []bool{false, true} {
		cfg := chromaEncoderConfig(h264.YUV444P10LE)
		cfg.Height = 34
		interlacedEncoderOracle(t, cfg, true, entropy, 3, encoder.WithSeparateColourPlanes(), encoder.WithTransform8x8())
	}
}

func TestEncoder_InterlacedHigh10QPRangeOracle(t *testing.T) {
	for _, entropy := range []bool{false, true} {
		for _, qp := range []int{-12, 51} {
			t.Run(fmt.Sprintf("cabac%t/qp%d", entropy, qp), func(t *testing.T) {
				cfg := chromaEncoderConfig(h264.YUV420P10LE)
				cfg.Height, cfg.QP = 36, qp
				interlacedEncoderOracle(t, cfg, qp < 0, entropy, 3, encoder.WithTransform8x8())
			})
		}
	}
}
