package codeccli

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/nal"
	"github.com/goavc/h264/syntax"
)

func TestCodecCLI_InterlacedFieldOrderOracle(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg不可用，CLI隔行独立像素Oracle未执行")
	}
	for _, order := range []string{"tff", "bff"} {
		for _, entropy := range []string{"cavlc", "cabac"} {
			t.Run(order+"/"+entropy, func(t *testing.T) {
				const width, height, count = 32, 32, 3
				var raw []byte
				for frame := range count {
					for comp := range 3 {
						w, h := width, height
						if comp > 0 {
							w, h = w/2, h/2
						}
						for y := range h {
							for x := range w {
								raw = append(raw, byte(32+comp*17+y%2*80+y/2+(x+frame*(1+y%2))%32*2))
							}
						}
					}
				}
				var coded, decoded bytes.Buffer
				args := encodeArgs("-width", "32", "-height", "32", "-profile", "high", "-field-order", order, "-entropy", entropy, "-slices", "2")
				if err := Encode(t.Context(), args, bytes.NewReader(raw), &coded, io.Discard); err != nil {
					t.Fatal(err)
				}
				units, err := nal.SplitAnnexB(coded.Bytes())
				if err != nil {
					t.Fatal(err)
				}
				seenSPS, seenSEI := false, 0
				for _, u := range units {
					if u.Header.Type != nal.TypeSPS && u.Header.Type != nal.TypeSEI {
						continue
					}
					rb, err := bitstream.EBSPToRBSP(u.Payload)
					if err != nil {
						t.Fatal(err)
					}
					if u.Header.Type == nal.TypeSPS {
						s, err := syntax.ParseSPS(rb)
						if err != nil || s.FrameMbsOnlyFlag || !s.MbAdaptiveFrameFieldFlag || !s.VUI.PicStructPresentFlag {
							t.Fatal("CLI未选择真实MBAFF通道", s, err)
						}
						seenSPS = true
					} else {
						structure := byte(3)
						if order == "bff" {
							structure = 4
						}
						if !bytes.Equal(rb, []byte{1, 1, structure<<4 | 2, 0x80}) {
							t.Fatalf("CLI场顺序错误：%x", rb)
						}
						seenSEI++
					}
				}
				if !seenSPS || seenSEI != count {
					t.Fatal("CLI隔行参数/每帧时序不完整", seenSPS, seenSEI)
				}
				if err := Decode(t.Context(), []string{"-progress", "0"}, bytes.NewReader(coded.Bytes()), &decoded, io.Discard); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(t.TempDir(), "interlaced.264")
				if err := os.WriteFile(path, coded.Bytes(), 0600); err != nil {
					t.Fatal(err)
				}
				oracle, err := exec.Command(ffmpeg, "-v", "error", "-xerror", "-i", path, "-fps_mode", "passthrough", "-f", "rawvideo", "-pix_fmt", "yuv420p", "-").Output()
				if err != nil {
					t.Fatal(err)
				}
				if len(oracle) != len(raw) || !bytes.Equal(decoded.Bytes(), oracle) {
					t.Fatalf("CLI隔行像素不符：输入%d 自解码%d FFmpeg%d", len(raw), decoded.Len(), len(oracle))
				}
			})
		}
	}
}
