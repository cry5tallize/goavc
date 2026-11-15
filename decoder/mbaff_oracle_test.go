package decoder

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/nal"
	"github.com/cry5tallize/goavc/syntax"
)

func TestMBAFF_ExternalFieldPixelOracle(t *testing.T) {
	ff := ffmpegPath(t)
	for _, format := range []h264.PixelFormat{h264.YUV420P, h264.YUV420P10LE, h264.YUV422P, h264.YUV422P10LE, h264.YUV444P, h264.YUV444P10LE} {
		for _, entropy := range []int{0, 1} {
			for _, topFirst := range []bool{true, false} {
				for _, tools := range []struct {
					name, params string
					direct       int
				}{
					{"ip4", "", -1},
					{"ip8_matrix", "8x8dct=1:cqm=jvt", -1},
					{"b_spatial", "bframes=2:b-adapt=0:b-pyramid=none:direct=spatial:weightb=1:8x8dct=1:cqm=jvt", 1},
					{"b_temporal", "bframes=2:b-adapt=0:b-pyramid=none:direct=temporal:weightb=1:8x8dct=1:cqm=jvt", 0},
				} {
					for _, mixed := range []bool{false, true} {
						t.Run(fmt.Sprintf("%s/cabac%d/tff%t/%s/mixed%t", format, entropy, topFirst, tools.name, mixed), func(t *testing.T) {
							const width, height, count = 64, 64, 6
							var raw []byte
							for n := range count {
								for comp := range 3 {
									w, h := format.PlaneDimensions(comp, width, height)
									for y := range h {
										for x := range w {
											parity := y & 1
											vertical := y / 2
											if mixed && x >= w/2 {
												parity = 0
												vertical = y
											}
											v := uint16(20 + parity*140 + ((x+2*n*(1+parity))*3+vertical*5+comp*13+n*7)%70)
											if format.BitDepth() == 10 {
												v = 4*v + uint16((x+y+n+comp)%4)
												raw = binary.LittleEndian.AppendUint16(raw, v)
											} else {
												raw = append(raw, byte(v))
											}
										}
									}
								}
							}
							dir := t.TempDir()
							input, output := filepath.Join(dir, "source.yuv"), filepath.Join(dir, "field.264")
							if err := os.WriteFile(input, raw, 0600); err != nil {
								t.Fatal(err)
							}
							profile := "high"
							sw, sh := format.ChromaSubsampling()
							if format.BitDepth() == 10 {
								profile = "high10"
							}
							if sh == 1 {
								profile = "high422"
							}
							if sw == 1 {
								profile = "high444"
							}
							order := "tff=1"
							if !topFirst {
								order = "bff=1"
							}
							params := fmt.Sprintf("threads=1:lookahead-threads=1:sync-lookahead=0:aud=1:repeat-headers=1:scenecut=0:keyint=20:bframes=0:ref=2:weightp=0:8x8dct=0:slices=2:cabac=%d:interlaced=1:%s", entropy, order)
							params += ":" + tools.params
							if mixed {
								params += ":slices=3"
							}
							args := []string{"-v", "error", "-y", "-f", "rawvideo", "-pix_fmt", format.String(), "-video_size", "64x64", "-framerate", "6", "-i", input, "-frames:v", "6", "-c:v", "libx264", "-profile:v", profile, "-qp", "23", "-x264-params", params, "-f", "h264", output}
							if out, err := exec.Command(ff, args...).CombinedOutput(); err != nil {
								t.Fatalf("x264 field encode: %v\n%s", err, out)
							}
							data, err := os.ReadFile(output)
							if err != nil {
								t.Fatal(err)
							}
							d := mustDecoder(t, h264.DecoderConfig{OutputFormat: format})
							if tools.direct >= 0 {
								units, err := nal.SplitAnnexB(data)
								if err != nil {
									t.Fatal(err)
								}
								bSlices := 0
								for _, u := range units {
									if u.Header.Type == nal.TypeSPS || u.Header.Type == nal.TypePPS {
										if err := d.SendPacket(h264.Packet{Data: annex(u)}); err != nil {
											t.Fatal(err)
										}
									}
									if u.Header.Type != nal.TypeSlice && u.Header.Type != nal.TypeSliceIDR {
										continue
									}
									s, err := d.parseSlice(u)
									if err != nil {
										t.Fatal(err)
									}
									if s.hdr.Type == syntax.SliceTypeB {
										bSlices++
										if s.hdr.DirectSpatialMVPredFlag != (tools.direct == 1) {
											t.Fatal("外部工具未生成所请求的B Direct模式")
										}
									}
								}
								if bSlices == 0 {
									t.Fatal("外部工具未生成B slice")
								}
							}
							if err := d.SendPacket(h264.Packet{Data: data}); err != nil {
								t.Fatal(err)
							}
							if err := d.Flush(); err != nil {
								t.Fatal(err)
							}
							hasField := false
							hasFrame := false
							for _, r := range d.dpb.Lists(d.dpb.ReferenceFrameNum(), 0, false)[0] {
								for _, field := range r.MBField {
									hasField = hasField || field
									hasFrame = hasFrame || !field
								}
							}
							if !hasField {
								t.Fatal("外部样本未实际使用field-coded MB pairs")
							}
							if mixed && !hasFrame {
								t.Fatal("外部混合样本未使用frame-coded MB pairs")
							}
							compareHigh422(t, collectNativeFrames(t, d, nil), high422Raw(t, ff, output, format), format)
						})
					}
				}
			}
		}
	}
}
