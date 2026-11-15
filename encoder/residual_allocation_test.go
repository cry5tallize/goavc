package encoder

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"hash"
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/internal/picture"
)

// 初始化完整原生采样和真实场视图；返回的恢复函数也在测量区执行，
// 不把逐次预测恢复或状态初始化移到计时外隐藏成本。
func preparedResidual(t testing.TB, format h264.PixelFormat, field, separate, matrix bool) (*frameState, func()) {
	t.Helper()
	cfg := h264.DefaultEncoderConfig()
	cfg.Width, cfg.Height, cfg.Profile, cfg.PixelFormat = 32, 64, h264.ProfileHigh444, format
	var options []Option
	if separate {
		options = append(options, WithSeparateColourPlanes())
	}
	e, err := New(cfg, options...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Close() })
	src, err := picture.New(2, 4, format)
	if err != nil {
		t.Fatal(err)
	}
	pred, err := picture.New(2, 4, format)
	if err != nil {
		t.Fatal(err)
	}
	limit := (1 << format.BitDepth()) - 1
	for comp := range 3 {
		p := &src.Planes[comp]
		for y := range p.Height {
			for x := range p.Width {
				p.SetSample(x, y, uint16((19*x+31*y+7*x*y+67*comp+3)&limit))
			}
		}
	}
	if field {
		a, _ := picture.FieldView(src, true)
		b, _ := picture.FieldView(pred, true)
		src, pred = &a, &b
	}
	s, err := e.newFrameState(src)
	if err != nil {
		t.Fatal(err)
	}
	s.pic, s.field = pred, field
	if matrix {
		s.scaling4, s.scaling8 = new([6][16]uint8), new([6][64]uint8)
		for c := range 6 {
			for i := range 16 {
				s.scaling4[c][i] = uint8(1 + (17*i+31*c)%255)
			}
			for i := range 64 {
				s.scaling8[c][i] = uint8(1 + (11*i+29*c)%255)
			}
		}
	}
	restore := func() {
		for c := range s.planeCount() {
			w, h := s.planeMBSize(c)
			p := &s.pic.Planes[c]
			for y := range h {
				for x := range w {
					p.SetSample(x, y, uint16((5*x+13*y+47*c+11)&limit))
				}
			}
		}
		s.info[0].cbp = 0
	}
	return s, restore
}

func hashResidual(h hash.Hash, res residual, s *frameState) {
	// binary.Write复制固定值，不序列化指针或padding；仅用于正确性指纹，不在基准内。
	_ = binary.Write(h, binary.LittleEndian, res)
	_ = binary.Write(h, binary.LittleEndian, int32(s.info[0].cbp))
	for c := range s.planeCount() {
		w, height := s.planeMBSize(c)
		for y := range height {
			for x := range w {
				_ = binary.Write(h, binary.LittleEndian, s.pic.Planes[c].Sample(x, y))
			}
		}
	}
}

func residualCases(t *testing.T, run func(*testing.T, *frameState, func())) {
	t.Helper()
	for _, format := range []h264.PixelFormat{h264.YUV420P, h264.YUV420P10LE, h264.YUV422P, h264.YUV422P10LE, h264.YUV444P, h264.YUV444P10LE} {
		separates := []bool{false}
		if subW, _ := format.ChromaSubsampling(); subW == 1 {
			separates = append(separates, true)
		}
		for _, separate := range separates {
			for _, field := range []bool{false, true} {
				for _, matrix := range []bool{false, true} {
					s, restore := preparedResidual(t, format, field, separate, matrix)
					for _, qp := range []int{-6 * (format.BitDepth() - 8), 26, 51} {
						for _, eight := range []bool{false, true} {
							for _, dead := range []bool{false, true} {
								t.Run(fmt.Sprintf("%s/separate=%v/field=%v/matrix=%v/qp=%d/t8=%v/dead=%v", format, separate, field, matrix, qp, eight, dead), func(t *testing.T) {
									s.info[0] = mbInfo{qp: qp, inter: true, transform8: eight, interDeadZone: dead}
									run(t, s, restore)
								})
							}
						}
					}
				}
			}
		}
	}
}

// 在11b902e上先采集所有候选，而不是仅冻结最终胜者。共享变换的数学正确性
// 仍由原解析向量和FFmpeg Oracle负责；此处保护系数、CBP及本地重建不因去逃逸改变。
func TestResidual_FrozenCandidates(t *testing.T) {
	h := sha256.New()
	residualCases(t, func(t *testing.T, s *frameState, restore func()) {
		restore()
		res := s.interResidual(0)
		s.reconstructInter(0, &res)
		hashResidual(h, res, s)
	})
	const want = "bb4e1a6cc1f3fc24cbd8f4567634d6765add07174b8d105f5c6f065da39f84e0"
	if got := fmt.Sprintf("%x", h.Sum(nil)); got != want {
		t.Fatalf("残差候选/CBP/重建改变: got %s, want %s", got, want)
	}
}

func TestAllocation_InterResidual(t *testing.T) {
	residualCases(t, func(t *testing.T, s *frameState, restore func()) {
		var res residual
		allocs := testing.AllocsPerRun(20, func() {
			restore()
			res = s.interResidual(0)
			s.reconstructInter(0, &res)
		})
		if allocs != 0 {
			t.Fatalf("prepared残差与重建: %g allocs", allocs)
		}
		if res == (residual{}) || s.info[0].cbp == 0 {
			t.Fatal("没有执行非空残差计算")
		}
	})
}

func TestAllocation_InterCandidate(t *testing.T) {
	residualCases(t, func(t *testing.T, s *frameState, restore func()) {
		reference, err := picture.New(2, 4, s.src.Format)
		if err != nil {
			t.Fatal(err)
		}
		limit := (1 << s.src.Format.BitDepth()) - 1
		for c := range 3 {
			p := &reference.Planes[c]
			for y := range p.Height {
				for x := range p.Width {
					p.SetSample(x, y, uint16((5*x+13*y+47*c+11)&limit))
				}
			}
		}
		if s.field {
			view, _ := picture.FieldView(reference, true)
			reference = &view
		}
		s.reference = reference
		base := s.info[0]
		for kind := range 4 {
			var res residual
			allocs := testing.AllocsPerRun(3, func() {
				restore()
				s.clearMotion(0)
				s.info[0] = base
				res = s.interCandidate(0, kind, 2, false)
			})
			if allocs != 0 || res == (residual{}) || s.info[0].cbp == 0 {
				t.Fatalf("分区%d: allocs=%g, cbp=%d, empty=%v", kind, allocs, s.info[0].cbp, res == (residual{}))
			}
		}
	})
}

func BenchmarkInterResidual_16x16_Auto(b *testing.B) {
	for _, format := range []h264.PixelFormat{h264.YUV420P, h264.YUV420P10LE, h264.YUV422P, h264.YUV422P10LE, h264.YUV444P, h264.YUV444P10LE} {
		for _, eight := range []bool{false, true} {
			b.Run(fmt.Sprintf("%s/t8=%v", format, eight), func(b *testing.B) {
				s, restore := preparedResidual(b, format, false, false, false)
				s.info[0] = mbInfo{qp: 26, inter: true, transform8: eight, interDeadZone: true}
				var res residual
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					restore()
					res = s.interResidual(0)
				}
				b.StopTimer()
				if res == (residual{}) || s.info[0].cbp == 0 {
					b.Fatal("未计算非空残差")
				}
			})
		}
	}
}
