package encoder

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"testing"

	h264 "github.com/goavc/h264"
)

// 在bb3c7d8上捕获全部帧内候选，避免仅以最终胜者掩盖未胜出模式的变化。
func TestIntra_FrozenCandidates(t *testing.T) {
	h := sha256.New()
	residualCases(t, func(t *testing.T, s *frameState, restore func()) {
		qp := s.info[0].qp
		for family := 1; family <= 3; family++ {
			restore()
			s.info[0] = mbInfo{qp: qp}
			res := s.analyzeIntra(0, family)
			hashResidual(h, res, s)
			_ = binary.Write(h, binary.LittleEndian, [3]int32{int32(family), int32(s.info[0].mode16), int32(s.info[0].chromaMode)})
			_ = binary.Write(h, binary.LittleEndian, s.info[0].modes)
		}
	})
	const want = "8e5b5258d07d30ef0509e47f4306e30a51f8447e743f85a54beba064618da41b"
	if got := fmt.Sprintf("%x", h.Sum(nil)); got != want {
		t.Fatalf("帧内候选/模式/重建改变: got %s, want %s", got, want)
	}
}

func TestAllocation_IntraCandidate(t *testing.T) {
	residualCases(t, func(t *testing.T, s *frameState, restore func()) {
		qp := s.info[0].qp
		for family := 1; family <= 3; family++ {
			var res residual
			allocs := testing.AllocsPerRun(5, func() {
				restore()
				s.info[0] = mbInfo{qp: qp}
				res = s.analyzeIntra(0, family)
			})
			if allocs != 0 {
				t.Fatalf("family%d: %g allocs", family, allocs)
			}
			if qp == 26 && res == (residual{}) {
				t.Fatalf("family%d: 未计算非空候选", family)
			}
		}
	})
}

func BenchmarkIntraCandidate_16x16_Auto(b *testing.B) {
	for family := 1; family <= 3; family++ {
		b.Run(fmt.Sprintf("family%d", family), func(b *testing.B) {
			s, restore := preparedResidual(b, h264.YUV420P, false, false, false)
			var res residual
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				restore()
				s.info[0] = mbInfo{qp: 26}
				res = s.analyzeIntra(0, family)
			}
			b.StopTimer()
			if res == (residual{}) {
				b.Fatal("未处理非空压缩候选")
			}
		})
	}
}
