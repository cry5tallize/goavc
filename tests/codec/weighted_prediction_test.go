package codec_test

import (
	"bytes"
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

func TestEncoder_WeightedPredictionOracle(t *testing.T) {
	weights := []encoder.PredictionWeights{
		{LumaDenom: 1, ChromaDenom: 2, Weight: [3]int32{1, 3, 2}, Offset: [3]int32{9, -7, 17}},
		{Weight: [3]int32{-1, 0, 1}, Offset: [3]int32{127, 70, -128}},
		{LumaDenom: 7, ChromaDenom: 7, Weight: [3]int32{128, 128, 128}},
	}
	for _, profile := range []string{h264.ProfileMain, h264.ProfileHigh} {
		for mode := 0; mode <= len(weights); mode++ {
			t.Run(fmt.Sprintf("%s/mode%d", profile, mode), func(t *testing.T) {
				cfg := h264.DefaultEncoderConfig()
				cfg.Profile = profile
				cfg.Width, cfg.Height = 34, 18
				cfg.GOPSize, cfg.IDRInterval = 5, 13
				options := []encoder.Option{encoder.WithSliceCount(3), encoder.WithSearchRange(4)}
				if mode == len(weights) {
					options = append(options, encoder.WithWeightedPrediction())
				} else {
					options = append(options, encoder.WithPredictionWeights(weights[mode]))
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
				var stream, want []byte
				var sps *syntax.SPS
				var pps *syntax.PPS
				pSlices, nonIdentity := 0, 0
				for frame := 0; frame < 20; frame++ {
					f := movingFrame(cfg.Width, cfg.Height, frame)
					if mode == len(weights) {
						for comp := range f.Planes {
							for i, v := range f.Planes[comp].Data {
								f.Planes[comp].Data[i] = byte(int(v) * (30 - frame) / 32)
							}
						}
					}
					if err = e.SendFrame(f); err != nil {
						t.Fatalf("帧%d：%v", frame, err)
					}
					f.Release()
					p, err := e.ReceivePacket()
					if err != nil {
						t.Fatal(err)
					}
					r, err := e.Reconstruction()
					if err != nil {
						t.Fatal(err)
					}
					scanner := nal.NewAnnexBScanner(bytes.NewReader(p.Data))
					for {
						u, err := scanner.Next()
						if err == io.EOF {
							break
						}
						if err != nil {
							t.Fatal(err)
						}
						rbsp, err := bitstream.EBSPToRBSP(u.Payload)
						if err != nil {
							t.Fatal(err)
						}
						switch u.Header.Type {
						case nal.TypeSPS:
							sps, err = syntax.ParseSPS(rbsp)
						case nal.TypePPS:
							pps, err = syntax.ParsePPS(rbsp, sps)
							if !pps.WeightedPredFlag {
								t.Fatal("PPS 未启用显式权重")
							}
						case nal.TypeSlice:
							var hdr *syntax.SliceHeader
							hdr, err = syntax.ParseSliceHeaderEx(rbsp, sps, map[uint32]*syntax.PPS{0: pps}, false, u.Header.RefIDC)
							if err == nil && hdr.Type == syntax.SliceTypeP {
								pSlices++
								table := hdr.PredictionWeights
								if table == nil {
									t.Fatal("P slice 缺权重表")
								}
								v := table.Lists[0][0]
								if v.Weight != [3]int32{1 << table.LumaDenom, 1 << table.ChromaDenom, 1 << table.ChromaDenom} || v.Offset != [3]int32{} {
									nonIdentity++
								}
								if mode < len(weights) {
									w := weights[mode]
									if table.LumaDenom != w.LumaDenom || table.ChromaDenom != w.ChromaDenom || v.Weight != w.Weight || v.Offset != w.Offset {
										t.Fatalf("权重表不一致：%+v %+v", table, w)
									}
								}
							}
						}
						if err != nil {
							t.Fatal(err)
						}
					}
					if err = d.SendPacket(p); err != nil {
						t.Fatalf("帧%d：%v", frame, err)
					}
					got, err := d.ReceiveFrame()
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(packed(got), packed(r)) {
						t.Fatalf("帧%d 加权重建与解码不一致", frame)
					}
					stream = append(stream, p.Data...)
					want = append(want, packed(r)...)
					got.Release()
					r.Release()
				}
				if pSlices == 0 || mode != 2 && nonIdentity == 0 {
					t.Fatal("没有实际写出加权 P slice")
				}
				ffmpegCheck(t, stream, want)
			})
		}
	}
}
