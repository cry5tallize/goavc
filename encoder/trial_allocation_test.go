package encoder

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/internal/picture"
)

func preparedDecision(t testing.TB, format h264.PixelFormat, field, separate, eight bool) (*frameState, *picture.Picture, func()) {
	t.Helper()
	s, restore := preparedResidual(t, format, field, separate, false)
	reference, err := picture.New(2, 4, format)
	if err != nil {
		t.Fatal(err)
	}
	scale := 1 << (format.BitDepth() - 8)
	for c := range 3 {
		p := &reference.Planes[c]
		for y := range p.Height {
			for x := range p.Width {
				p.SetSample(x, y, uint16(((60+2*x+3*y+3*c)&255)*scale+(x+y)%scale))
			}
		}
		p = &s.src.Planes[c]
		for y := range p.Height {
			for x := range p.Width {
				physicalY := y
				if field {
					physicalY = 2*y + 1
				}
				p.SetSample(x, y, uint16(((120+2*x+3*physicalY+3*c+x*physicalY%7)&255)*scale+(x+physicalY)%scale))
			}
		}
	}
	if field {
		view, _ := picture.FieldView(reference, true)
		reference = &view
	}
	s.allow8 = eight
	reset := func() {
		restore()
		s.info[0] = mbInfo{qp: 26}
		s.clearMotion(0)
		clear(s.nz[:16])
		for c := range s.cnz {
			clear(s.cnz[c][:16])
		}
	}
	return s, reference, reset
}

func decisionCases(t *testing.T, run func(*testing.T, *frameState, *picture.Picture, func(), string)) {
	t.Helper()
	for _, format := range []h264.PixelFormat{h264.YUV420P, h264.YUV420P10LE, h264.YUV422P, h264.YUV422P10LE, h264.YUV444P, h264.YUV444P10LE} {
		separates := []bool{false}
		if subW, _ := format.ChromaSubsampling(); subW == 1 {
			separates = append(separates, true)
		}
		for _, separate := range separates {
			for _, field := range []bool{false, true} {
				for _, eight := range []bool{false, true} {
					s, ref, reset := preparedDecision(t, format, field, separate, eight)
					for _, kind := range []string{"I", "P", "B"} {
						t.Run(fmt.Sprintf("%s/separate=%v/field=%v/t8=%v/%s", format, separate, field, eight, kind), func(t *testing.T) { run(t, s, ref, reset, kind) })
					}
				}
			}
		}
	}
}

func runDecision(s *frameState, ref *picture.Picture, kind string) (residual, error) {
	if kind == "I" {
		s.reference = nil
		return s.analyzeIntraRD(0, 26)
	}
	s.reference = ref
	if kind == "B" {
		return s.analyzeB(0, 26, ref, ref)
	}
	return s.analyzeRD(0, 26, 2, false)
}

// 在9a126d2上采集，包括真实试写率/失真/总代价和最终模式。
func TestTrial_FrozenDecisions(t *testing.T) {
	h := sha256.New()
	decisionCases(t, func(t *testing.T, s *frameState, ref *picture.Picture, reset func(), kind string) {
		reset()
		res, err := runDecision(s, ref, kind)
		if err != nil {
			t.Fatal(err)
		}
		c, err := s.captureRD(0, 26, 26, res)
		if err != nil {
			t.Fatal(err)
		}
		hashResidual(h, res, s)
		_ = binary.Write(h, binary.LittleEndian, [3]uint64{c.rate, c.distortion, c.cost})
		_ = binary.Write(h, binary.LittleEndian, [5]int32{int32(c.info.partition), int32(c.info.mode16), int32(c.info.chromaMode), int32(c.info.qp), int32(c.info.cbp)})
		_ = binary.Write(h, binary.LittleEndian, [4]bool{c.info.inter, c.info.skip, c.info.pcm, c.info.transform8})
		_ = binary.Write(h, binary.LittleEndian, c.info.modes)
		_ = binary.Write(h, binary.LittleEndian, c.vectors)
		if c.info.skip || c.info.pcm || res == (residual{}) {
			t.Fatalf("%s 未选择非空压缩候选: skip=%v pcm=%v", kind, c.info.skip, c.info.pcm)
		}
	})
	const want = "9b37334a5878629110a85858983c82d323913c16a40080ca44da76f4ee644115"
	if got := fmt.Sprintf("%x", h.Sum(nil)); got != want {
		t.Fatalf("RD模式/率/失真/重建改变: got %s, want %s", got, want)
	}
}

func TestAllocation_ModeDecision(t *testing.T) {
	decisionCases(t, func(t *testing.T, s *frameState, ref *picture.Picture, reset func(), kind string) {
		var res residual
		var err error
		allocs := testing.AllocsPerRun(3, func() { reset(); res, err = runDecision(s, ref, kind) })
		if err != nil {
			t.Fatal(err)
		}
		if allocs != 0 {
			t.Fatalf("%s: %g allocs", kind, allocs)
		}
		if s.info[0].skip || s.info[0].pcm || res == (residual{}) {
			t.Fatal("未处理非空压缩候选")
		}
	})
}

// 完整决策不能只以QP26/flat为证据；每次恢复、真实试写和重建都在计量内。
func TestAllocation_ModeDecisionMatrixAndQP(t *testing.T) {
	decisionCases(t, func(t *testing.T, s *frameState, ref *picture.Picture, reset func(), kind string) {
		// 使用强DC差异和独立低两位：低QP仍易压缩，高QP仍有非零系数。
		// 原QP26平滑向量在极端矩阵/低QP会合法选择PCM，不能据此证明正常路径。
		limit := uint16((1 << s.src.Format.BitDepth()) - 1)
		for c := range s.src.Planes {
			p := &s.src.Planes[c]
			for y := range p.Height {
				for x := range p.Width {
					p.SetSample(x, y, limit-uint16((x+y)&3))
				}
			}
		}
		matrix4, matrix8 := new([6][16]uint8), new([6][64]uint8)
		for c := range 6 {
			for i := range 16 {
				matrix4[c][i] = uint8(1 + (17*i+31*c)%255)
			}
			for i := range 64 {
				matrix8[c][i] = uint8(1 + (11*i+29*c)%255)
			}
		}
		for _, custom := range []bool{false, true} {
			nonempty := 0
			for _, qp := range []int{-6 * (s.src.Format.BitDepth() - 8), 26, 51} {
				t.Run(fmt.Sprintf("matrix=%v/qp=%d", custom, qp), func(t *testing.T) {
					s.scaling4, s.scaling8 = nil, nil
					if custom {
						s.scaling4, s.scaling8 = matrix4, matrix8
					}
					var res residual
					var err error
					run := func() {
						reset()
						s.info[0].qp = qp
						s.reference = ref
						switch kind {
						case "I":
							s.reference = nil
							res, err = s.analyzeIntraRD(0, qp)
						case "P":
							res, err = s.analyzeRD(0, qp, 2, false)
						case "B":
							res, err = s.analyzeB(0, qp, ref, ref)
						}
					}
					allocs := testing.AllocsPerRun(3, run)
					if err != nil {
						t.Fatal(err)
					}
					if allocs != 0 {
						t.Fatalf("%s完整决策分配%g次", kind, allocs)
					}
					if !s.info[0].skip && !s.info[0].pcm && res != (residual{}) {
						nonempty++
					}
				})
			}
			// 端点QP可以合法选择PCM或全零残差，不能为测试改决策策略。
			// 每个格式/场/平面/变换/family/矩阵仍必须实际覆盖非空压缩决策。
			if nonempty == 0 {
				t.Fatal("整组只有Skip/PCM/空残差，不能证明正常决策路径")
			}
		}
	})
}

func TestTrial_ErrorThenReuse(t *testing.T) {
	s, _, reset := preparedDecision(t, h264.YUV420P, false, false, false)
	base := mbInfo{qp: 26, cbp: 15}
	reset()
	s.info[0] = base
	var bad, good residual
	bad.luma[0][0] = -1 << 31
	good.luma[0][0] = 7
	if _, err := s.captureRD(0, 26, 26, bad); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("invalid level: %v", err)
	}
	reset()
	s.info[0] = base
	reused, err := s.captureRD(0, 26, 26, good)
	if err != nil {
		t.Fatal(err)
	}
	bits := s.trialWriters.rd.BitsWritten()
	data := s.trialWriters.rd.Bytes()
	s.trialWriters = newTrialWriters()
	reset()
	s.info[0] = base
	fresh, err := s.captureRD(0, 26, 26, good)
	if err != nil || fresh != reused || bits != s.trialWriters.rd.BitsWritten() || !bytes.Equal(data, s.trialWriters.rd.Bytes()) {
		t.Fatalf("失败试写污染后续复用: %v", err)
	}
}

func BenchmarkModeDecision_16x16_Auto(b *testing.B) {
	for _, kind := range []string{"I", "P", "B"} {
		b.Run(kind, func(b *testing.B) {
			s, ref, reset := preparedDecision(b, h264.YUV420P, false, false, true)
			var res residual
			var err error
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				reset()
				res, err = runDecision(s, ref, kind)
				if err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			if res == (residual{}) || s.info[0].skip || s.info[0].pcm {
				b.Fatal("未执行非空压缩候选")
			}
		})
	}
}
