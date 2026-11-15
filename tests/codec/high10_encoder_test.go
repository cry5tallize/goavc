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

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/bitstream"
	"github.com/cry5tallize/goavc/decoder"
	"github.com/cry5tallize/goavc/encoder"
	"github.com/cry5tallize/goavc/nal"
	"github.com/cry5tallize/goavc/syntax"
)

// 高低字节均参与图案，行尾故意留填充；不能靠8位复制或右移再编码通过。
func high10Frame(width, height, index int) *h264.Frame {
	f := &h264.Frame{Width: width, Height: height, Format: h264.YUV420P10LE, PTS: int64(index) * 100, Duration: 100}
	for c := range f.Planes {
		w, h := f.Format.PlaneDimensions(c, width, height)
		stride := w*2 + 6
		f.Planes[c] = h264.Plane{Stride: stride, Data: make([]byte, stride*h)}
		for y := range h {
			for x := range w {
				v := 420 + c*41 + x*5 + y*3 + index*7
				if (x+index)/8%2 != 0 {
					v += 29
				}
				binary.LittleEndian.PutUint16(f.Planes[c].Data[y*stride+x*2:], uint16(v%1024))
			}
		}
	}
	return f
}

func high10Packed(t *testing.T, f *h264.Frame) []byte {
	t.Helper()
	if f.Format != h264.YUV420P10LE {
		t.Fatalf("输出位深被改变：%v", f.Format)
	}
	var out []byte
	for c := range f.Planes {
		w, h := f.Format.PlaneDimensions(c, f.Width, f.Height)
		for y := range h {
			row := f.Planes[c].Data[y*f.Planes[c].Stride:][:w*2]
			for x := range w {
				if binary.LittleEndian.Uint16(row[x*2:]) > 1023 {
					t.Fatal("输出含非法10位采样")
				}
			}
			out = append(out, row...)
		}
	}
	return out
}

func high10FFmpegCheck(t *testing.T, stream, want []byte) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("FFmpeg不可用，独立High10 Oracle未执行")
	}
	path := filepath.Join(t.TempDir(), "high10.264")
	if err := os.WriteFile(path, stream, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("ffmpeg", "-v", "warning", "-xerror", "-i", path, "-f", "rawvideo", "-pix_fmt", "yuv420p10le", "pipe:1")
	var log bytes.Buffer
	cmd.Stderr = &log
	got, err := cmd.Output()
	if err != nil || log.Len() != 0 {
		t.Fatalf("FFmpeg High10解码：%v，%s", err, log.String())
	}
	if !bytes.Equal(got, want) {
		for i := 0; i+1 < min(len(got), len(want)); i += 2 {
			if !bytes.Equal(got[i:i+2], want[i:i+2]) {
				t.Fatalf("FFmpeg采样%d=%d，本地重建%d", i/2, binary.LittleEndian.Uint16(got[i:]), binary.LittleEndian.Uint16(want[i:]))
			}
		}
		t.Fatalf("FFmpeg长度%d，本地重建%d", len(got), len(want))
	}
}

func high10Oracle(t *testing.T, cfg h264.EncoderConfig, pcm bool, options ...encoder.Option) {
	t.Helper()
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
	recons := map[int64][]byte{}
	var stream []byte
	packets, decoded, pSlices, bSlices := 0, 0, 0, 0
	var sps *syntax.SPS
	var pps *syntax.PPS
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
			got, want := high10Packed(t, f), recons[f.PTS]
			if f.PTS != int64(decoded)*100 || !bytes.Equal(got, want) {
				for i := 0; i+1 < min(len(got), len(want)); i += 2 {
					if binary.LittleEndian.Uint16(got[i:]) != binary.LittleEndian.Uint16(want[i:]) {
						t.Fatalf("自研显示帧%d PTS%d采样%d=%d，本地重建%d", decoded, f.PTS, i/2, binary.LittleEndian.Uint16(got[i:]), binary.LittleEndian.Uint16(want[i:]))
					}
				}
				t.Fatalf("自研显示帧%d PTS%d长度%d，本地重建%d", decoded, f.PTS, len(got), len(want))
			}
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
			recons[p.PTS] = high10Packed(t, r)
			if pcm {
				source := high10Frame(cfg.Width, cfg.Height, int(p.PTS/100))
				if !bytes.Equal(recons[p.PTS], high10Packed(t, source)) {
					t.Fatal("显式PCM未保留原始10位采样")
				}
				source.Release()
			}
			r.Release()
			// 六个宏块的PCM仅采样就需2880字节；平滑首帧必须真实压缩。
			if packets == 0 && !pcm && len(p.Data) >= ((cfg.Width+15)/16)*((cfg.Height+15)/16)*480 {
				t.Fatalf("默认首帧未实际压缩：%d字节", len(p.Data))
			}
			stream = append(stream, p.Data...)
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
					if err == nil && (sps.ProfileIDC != 110 || sps.BitDepthLumaMinus8 != 2 || sps.BitDepthChromaMinus8 != 2) {
						t.Fatal("码流未声明真实High10")
					}
				case nal.TypePPS:
					pps, err = syntax.ParsePPS(rb, sps)
				case nal.TypeSlice, nal.TypeSliceIDR:
					var h *syntax.SliceHeader
					h, err = syntax.ParseSliceHeaderEx(rb, sps, map[uint32]*syntax.PPS{0: pps}, u.Header.Type == nal.TypeSliceIDR, u.Header.RefIDC)
					if err == nil {
						if h.Type == syntax.SliceTypeP {
							pSlices++
						}
						if h.Type == syntax.SliceTypeB {
							bSlices++
						}
					}
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if err = d.SendPacket(p); err != nil {
				t.Fatalf("解码包%d PTS%d：%v", packets, p.PTS, err)
			}
			packets++
			drainDecoder()
		}
	}
	for i := range count {
		f := high10Frame(cfg.Width, cfg.Height, i)
		if err = e.SendFrame(f); err != nil {
			t.Fatalf("送入帧%d：%v", i, err)
		}
		// SendFrame返回后改变整个输入，覆盖延迟B/前瞻队列所有权。
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
	if packets != count || decoded != count || !pcm && pSlices == 0 || cfg.MaxBFrames > 0 && bSlices == 0 {
		t.Fatalf("包%d，显示帧%d，P slice%d，B slice%d", packets, decoded, pSlices, bSlices)
	}
	var want []byte
	lowBits, above8 := false, false
	for i := range count {
		frame := recons[int64(i)*100]
		want = append(want, frame...)
		for j := 0; j < len(frame); j += 2 {
			v := binary.LittleEndian.Uint16(frame[j:])
			lowBits = lowBits || v&3 != 0
			above8 = above8 || v > 255
		}
	}
	if !above8 || cfg.QP <= 0 && !lowBits {
		t.Fatal("输出丢失高位深或有效低两位")
	}
	high10FFmpegCheck(t, stream, want)
}

func TestHigh10_CompressedThreeWayOracle(t *testing.T) {
	for _, qp := range []int{-12, -1, 0, 26, 51} {
		for _, cabac := range []bool{false, true} {
			for _, matrices := range []bool{false, true} {
				t.Run(fmt.Sprintf("qp%d/cabac%t/matrices%t", qp, cabac, matrices), func(t *testing.T) {
					cfg := h264.DefaultEncoderConfig()
					cfg.Profile, cfg.PixelFormat = h264.ProfileHigh10, h264.YUV420P10LE
					cfg.Width, cfg.Height, cfg.QP = 34, 18, qp
					cfg.GOPSize, cfg.IDRInterval = 5, 13
					options := []encoder.Option{encoder.WithCABAC(cabac), encoder.WithSliceCount(3), encoder.WithSearchRange(2)}
					if matrices {
						var a, b [64]uint8
						for i := range a {
							a[i], b[i] = uint8(8+i%19), uint8(11+i%13)
						}
						var four [6][16]uint8
						for list := range four {
							for i := range four[list] {
								four[list][i] = uint8(8 + (i+list)%17)
							}
						}
						options = append(options, encoder.WithScalingMatrix8x8(a, b), encoder.WithScalingMatrices4x4(four))
					}
					high10Oracle(t, cfg, false, options...)
				})
			}
		}
	}
}

func TestHigh10_BToolsThreeWayOracle(t *testing.T) {
	for _, cabac := range []bool{false, true} {
		for _, rc := range []h264.RateControl{h264.RateControlCQP, h264.RateControlABR, h264.RateControlCBR, h264.RateControlVBV} {
			t.Run(fmt.Sprintf("cabac%t/%s", cabac, rc), func(t *testing.T) {
				cfg := h264.DefaultEncoderConfig()
				cfg.Profile, cfg.PixelFormat = h264.ProfileHigh10, h264.YUV420P10LE
				cfg.Width, cfg.Height, cfg.QP = 34, 18, -1
				cfg.MaxBFrames, cfg.GOPSize, cfg.IDRInterval = 2, 7, 17
				cfg.RateControl = rc
				if rc != h264.RateControlCQP {
					cfg.Bitrate, cfg.MaxBitrate, cfg.VBVBufferSize = 100_000, 150_000, 150_000
				}
				high10Oracle(t, cfg, false, encoder.WithCABAC(cabac), encoder.WithTransform8x8(), encoder.WithWeightedPrediction(), encoder.WithAdaptiveQP(3), encoder.WithLookahead(1), encoder.WithSearchRange(2))
			})
		}
	}
}

func TestHigh10_PCMAndNegativeMacroblockQPOracle(t *testing.T) {
	for _, cabac := range []bool{false, true} {
		cfg := h264.DefaultEncoderConfig()
		cfg.Profile, cfg.PixelFormat = h264.ProfileHigh10, h264.YUV420P10LE
		cfg.Width, cfg.Height, cfg.QP = 34, 18, -12
		t.Run(fmt.Sprintf("pcm/cabac%t", cabac), func(t *testing.T) {
			high10Oracle(t, cfg, true, encoder.WithCABAC(cabac), encoder.WithPCM())
		})
		t.Run(fmt.Sprintf("mbqp/cabac%t", cabac), func(t *testing.T) {
			high10Oracle(t, cfg, false, encoder.WithCABAC(cabac), encoder.WithMacroblockQP(func(x, y int) int {
				return [...]int{-12, -1, 0, 26, 50, 51}[(y*3+x)%6]
			}), encoder.WithPredictionWeights(encoder.PredictionWeights{Weight: [3]int32{1, 1, 1}, Offset: [3]int32{1, -2, 3}}), encoder.WithSearchRange(2))
		})
	}
}
