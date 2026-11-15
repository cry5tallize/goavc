package codec_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/bitstream"
	"github.com/cry5tallize/goavc/decoder"
	"github.com/cry5tallize/goavc/encoder"
	"github.com/cry5tallize/goavc/nal"
	"github.com/cry5tallize/goavc/syntax"
)

func TestEncoder_BFrameReorderOracle(t *testing.T) {
	for _, profile := range []string{h264.ProfileMain, h264.ProfileHigh} {
		for _, depth := range []int{1, 2, 3, 16} {
			rates := []h264.RateControl{h264.RateControlCQP}
			if depth == 2 {
				rates = append(rates, h264.RateControlABR, h264.RateControlCBR, h264.RateControlVBV)
			}
			for _, rc := range rates {
				t.Run(fmt.Sprintf("%s/b%d/%s", profile, depth, rc), func(t *testing.T) {
					cfg := h264.DefaultEncoderConfig()
					cfg.Profile = profile
					cfg.Width, cfg.Height = 32, 32
					cfg.MaxBFrames = depth
					cfg.RateControl = rc
					if rc != h264.RateControlCQP {
						cfg.Bitrate = 60_000
						cfg.VBVBufferSize = 60_000
						cfg.QP = 0
					}
					cfg.GOPSize, cfg.IDRInterval = 7, 17
					if depth == 16 {
						cfg.GOPSize, cfg.IDRInterval = 40, 67
					}
					options := []encoder.Option{encoder.WithSliceCount(2)}
					if profile == h264.ProfileHigh {
						options = append(options, encoder.WithTransform8x8(), encoder.WithWeightedPrediction())
					}
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
					var stream []byte
					recons := map[int64][]byte{}
					decoded := 0
					packets := 0
					lastDTS := int64(-1 << 62)
					reordered := false
					var sps *syntax.SPS
					var pps *syntax.PPS
					bPictures := 0
					drainDecoder := func() {
						t.Helper()
						for {
							got, err := d.ReceiveFrame()
							if errors.Is(err, h264.ErrAgain) || errors.Is(err, h264.ErrEOF) {
								return
							}
							if err != nil {
								t.Fatal(err)
							}
							expected, ok := recons[got.PTS]
							if !ok {
								t.Fatalf("PTS%d尚无重建", got.PTS)
							}
							if !bytes.Equal(packed(got), expected) {
								t.Fatalf("PTS%d B序列解码与重建不一致", got.PTS)
							}
							if got.PTS != int64(decoded)*100 {
								t.Fatalf("解码显示顺序%d，期望%d", got.PTS, decoded*100)
							}
							decoded++
							got.Release()
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
								t.Fatalf("输出%d：%v", packets, err)
							}
							if p.DTS <= lastDTS {
								t.Fatal("DTS未递增")
							}
							lastDTS = p.DTS
							if p.PTS != int64(packets)*100 {
								reordered = true
							}
							r, err := e.Reconstruction()
							if err != nil {
								t.Fatal(err)
							}
							if r.PTS != p.PTS {
								t.Fatal("重建与编码包不同步")
							}
							recons[p.PTS] = packed(r)
							r.Release()
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
								case nal.TypePPS:
									pps, err = syntax.ParsePPS(rb, sps)
								case nal.TypeSlice:
									var h *syntax.SliceHeader
									h, err = syntax.ParseSliceHeaderEx(rb, sps, map[uint32]*syntax.PPS{0: pps}, false, u.Header.RefIDC)
									if err == nil && h.Type == syntax.SliceTypeB {
										if u.Header.RefIDC != 0 || !h.DirectSpatialMVPredFlag {
											t.Fatal("B必须非参考并使用spatial-direct")
										}
										if h.FirstMBInSlice == 0 {
											bPictures++
										}
									}
								}
								if err != nil {
									t.Fatal(err)
								}
							}
							if err = d.SendPacket(p); err != nil {
								t.Fatalf("包%d PTS%d：%v", packets, p.PTS, err)
							}
							packets++
							drainDecoder()
						}
					}
					for i := 0; i < 80; i++ {
						f := movingFrame(32, 32, i)
						f.PTS = int64(i) * 100
						f.Duration = 100
						if i == 12 {
							f.KeyFrame = true
						}
						if err = e.SendFrame(f); err != nil {
							t.Fatalf("输入%d：%v", i, err)
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
					if packets != 80 || decoded != 80 || !reordered || bPictures == 0 {
						t.Fatalf("包%d 帧%d 重排%t", packets, decoded, reordered)
					}
					var want []byte
					for i := 0; i < 80; i++ {
						want = append(want, recons[int64(i)*100]...)
					}
					ffmpegCheck(t, stream, want)
				})
			}
		}
	}
}
