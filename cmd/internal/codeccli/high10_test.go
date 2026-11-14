package codeccli

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/nal"
	"github.com/goavc/h264/syntax"
)

func TestCodecCLI_High10EntropyAndPixelOracle(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg不可用，CLI独立像素Oracle未执行")
	}
	for _, entropy := range []string{"cavlc", "cabac"} {
		t.Run(entropy, func(t *testing.T) {
			raw := make([]byte, 3*768)
			for i := range len(raw) / 2 {
				binary.LittleEndian.PutUint16(raw[2*i:], uint16(400+i%97))
			}
			var coded, decoded bytes.Buffer
			args := encodeArgs("-profile", "high10", "-pixel-format", "yuv420p10le", "-qp", "-1", "-entropy", entropy)
			if err := Encode(t.Context(), args, bytes.NewReader(raw), &coded, io.Discard); err != nil {
				t.Fatal(err)
			}
			units, err := nal.SplitAnnexB(coded.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			var sps *syntax.SPS
			seen := false
			for _, u := range units {
				if u.Header.Type != nal.TypeSPS && u.Header.Type != nal.TypePPS {
					continue
				}
				rb, err := bitstream.EBSPToRBSP(u.Payload)
				if err != nil {
					t.Fatal(err)
				}
				if u.Header.Type == nal.TypeSPS {
					sps, err = syntax.ParseSPS(rb)
				} else {
					var p *syntax.PPS
					p, err = syntax.ParsePPS(rb, sps)
					if err == nil && p.EntropyCodingModeFlag != (entropy == "cabac") {
						t.Fatal("熵选项未体现在码流")
					}
					seen = true
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if !seen || sps.ProfileIDC != 110 || sps.BitDepthLumaMinus8 != 2 {
				t.Fatal("CLI未写出High10参数集")
			}
			if err := Decode(t.Context(), []string{"-progress", "0"}, bytes.NewReader(coded.Bytes()), &decoded, io.Discard); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "coded.264")
			if err := os.WriteFile(path, coded.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			want, err := exec.Command(ffmpeg, "-v", "error", "-i", path, "-fps_mode", "passthrough", "-f", "rawvideo", "-pix_fmt", "yuv420p10le", "-").Output()
			if err != nil {
				t.Fatal(err)
			}
			if len(want) != len(raw) || !bytes.Equal(decoded.Bytes(), want) {
				t.Fatalf("CLI/Oracle像素不符：输入%d，Go%d，Oracle%d", len(raw), decoded.Len(), len(want))
			}
		})
	}
}
