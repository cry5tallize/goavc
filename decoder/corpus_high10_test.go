package decoder

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/cry5tallize/goavc/bitstream"
	"github.com/cry5tallize/goavc/internal/testutil/manifest"
	"github.com/cry5tallize/goavc/nal"
	"github.com/cry5tallize/goavc/syntax"
)

// 把M2/M3码流投影至High10：保留原有预测/分区/参考/残差语法，QPY不变，
// 有效QP'增加12。不能把8-bit解码后升位当作10-bit重建Oracle；高精度插值、
// 逆变换舍入与滤波必须由外部解码器独立执行。规范小向量的PCM另扩展至10位。
func projectCorpusHigh10(t *testing.T, data []byte, syntheticPCM bool) []byte {
	t.Helper()
	units, err := nal.SplitAnnexB(data)
	if err != nil {
		t.Fatal(err)
	}
	sps := make(map[uint32]*syntax.SPS)
	pps := make(map[uint32]*syntax.PPS)
	var out []byte
	for _, u := range units {
		switch u.Header.Type {
		case nal.TypeSPS:
			rbsp, err := bitstream.EBSPToRBSP(u.Payload)
			if err != nil {
				t.Fatal(err)
			}
			s, err := syntax.ParseSPS(rbsp)
			if err != nil {
				t.Fatal(err)
			}
			sps[s.SeqParameterSetID] = s
			projected := *s
			projected.ProfileIDC, projected.ConstraintSetFlags = 110, [6]bool{}
			projected.BitDepthLumaMinus8, projected.BitDepthChromaMinus8 = 2, 2
			rbsp, err = projected.MarshalRBSP()
			if err != nil {
				t.Fatal(err)
			}
			u.Payload = bitstream.RBSPToEBSP(rbsp)
		case nal.TypePPS:
			rbsp, err := bitstream.EBSPToRBSP(u.Payload)
			if err != nil {
				t.Fatal(err)
			}
			_, sid, err := peekPPSIDs(rbsp)
			if err != nil {
				t.Fatal(err)
			}
			p, err := syntax.ParsePPS(rbsp, sps[sid])
			if err != nil {
				t.Fatal(err)
			}
			pps[p.PicParameterSetID] = p
		case nal.TypeSlice, nal.TypeSliceIDR:
			if syntheticPCM {
				rbsp, err := bitstream.EBSPToRBSP(u.Payload)
				if err != nil {
					t.Fatal(err)
				}
				pid, err := peekSlicePPSID(rbsp)
				if err != nil {
					t.Fatal(err)
				}
				h, err := syntax.ParseSliceHeaderEx(rbsp, sps[pps[pid].SeqParameterSetID], pps, u.IsIDR(), u.Header.RefIDC)
				if err != nil {
					t.Fatal(err)
				}
				if h.Type == syntax.SliceTypeI {
					r := bitstream.NewReader(rbsp)
					if err := r.SkipBits(uint(h.HeaderBits)); err != nil {
						t.Fatal(err)
					}
					typ, err := r.ReadUE()
					if err != nil || typ != 25 {
						t.Fatalf("规范小向量预期单个I_PCM: type=%d err=%v", typ, err)
					}
					for !r.ByteAligned() {
						if v, err := r.ReadBits(1); err != nil || v != 0 {
							t.Fatalf("PCM对齐: %d %v", v, err)
						}
					}
					w := bitstream.NewWriter()
					copyCorpusBits(t, w, bitstream.NewReader(rbsp), r.BitsRead())
					for range 384 {
						v, err := r.ReadBits(8)
						if err != nil {
							t.Fatal(err)
						}
						if err := w.WriteBits(10, v<<2); err != nil {
							t.Fatal(err)
						}
					}
					copyCorpusBits(t, w, r, r.BitsRemaining())
					u.Payload = bitstream.RBSPToEBSP(w.Bytes())
				}
			}
		}
		out = append(out, 0, 0, 0, 1, u.Header.Byte())
		out = append(out, u.Payload...)
	}
	return out
}

func copyCorpusBits(t *testing.T, w *bitstream.Writer, r *bitstream.Reader, count uint64) {
	t.Helper()
	for count != 0 {
		n := uint(min(count, 64))
		v, err := r.ReadBits(n)
		if err != nil {
			t.Fatal(err)
		}
		if err := w.WriteBits(n, v); err != nil {
			t.Fatal(err)
		}
		count -= uint64(n)
	}
}

func TestCorpus_M2M3High10ProjectionOracle(t *testing.T) {
	ffmpeg := ffmpegPath(t)
	m, err := manifest.Load("../testdata/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range m.Entries {
		if e.Status != manifest.StatusConformant || e.BitDepth != 8 || e.PixFmt != "yuv420p" || (e.Profile != "baseline" && e.Profile != "main" && e.Profile != "high") {
			continue
		}
		// M7新增套件单独验收；本门禁复用原M2/M3输入，而非依赖字段缺省值筛选。
		if slices.Contains(e.Tags, "m7_advanced") {
			continue
		}
		t.Run(e.Name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", filepath.FromSlash(e.Path)))
			if err != nil {
				t.Fatal(err)
			}
			projected := projectCorpusHigh10(t, data, e.Source == "synthetic")
			path := filepath.Join(t.TempDir(), "high10.264")
			if err := os.WriteFile(path, projected, 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(ffmpeg, "-v", "error", "-xerror", "-err_detect", "explode", "-i", path, "-fps_mode", "passthrough", "-pix_fmt", "yuv420p10le", "-f", "rawvideo", "-")
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			want, err := cmd.Output()
			if err != nil {
				t.Fatalf("High10投影外部解码: %v\n%s", err, stderr.String())
			}
			if len(want) == 0 || len(want)%(e.Width*e.Height*3) != 0 {
				t.Fatalf("High10 Oracle不是完整原生帧: %d bytes", len(want))
			}
			got := decodeFile(t, path)
			if !bytes.Equal(got, want) {
				t.Fatalf("High10投影像素不同: Go %d bytes, FFmpeg %d bytes", len(got), len(want))
			}
		})
	}
}
