package syntax

import (
	"bytes"
	"errors"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/internal/transform"
)

func TestScalingList_WriteRejectsBeforeAnyOutput(t *testing.T) {
	for _, size := range []int{4, 8} {
		for _, kind := range []string{"short", "long", "late-range", "cache", "default-extra"} {
			list := &ScalingList{Size: size, Deltas: make([]int32, size*size)}
			switch kind {
			case "short":
				list.Deltas = list.Deltas[:len(list.Deltas)-1]
			case "long":
				list.Deltas = append(list.Deltas, 0)
			case "late-range":
				list.Deltas[len(list.Deltas)-1] = 128
			case "cache":
				list.Values = make([]int32, size*size)
				for i := range list.Values {
					list.Values[i] = 8
				}
				list.Values[len(list.Values)-1] = 9
			case "default-extra":
				list.Deltas = []int32{-8, 0}
			}
			w := bitstream.NewWriter()
			w.WriteBits(3, 5)
			before := append([]byte(nil), w.Bytes()...)
			if err := list.WriteTo(w); !errors.Is(err, h264.ErrInvalidData) {
				t.Fatalf("size%d %s: %v", size, kind, err)
			}
			if w.BitsWritten() != 3 || !bytes.Equal(w.Bytes(), before) {
				t.Fatalf("size%d %s留下半个scaling_list", size, kind)
			}
		}
	}
}

func TestScalingSelection_RejectsDivergentSyntaxCache(t *testing.T) {
	for _, usePPS := range []bool{false, true} {
		s := &SPS{ProfileIDC: 244, ChromaFormatIDC: 3, SeqScalingMatrixPresentFlag: !usePPS}
		p := &PPS{PicScalingMatrixPresentFlag: usePPS, Transform8x8ModeFlag: true}
		for index := range 12 {
			size := 4
			if index >= 6 {
				size = 8
			}
			list := &ScalingList{Size: size, Deltas: make([]int32, size*size), Values: make([]int32, size*size)}
			for i := range list.Values {
				list.Values[i] = 8
			}
			list.Values[len(list.Values)-1] = 9
			if usePPS {
				p.PicScalingListPresentFlag[index], p.PicScalingList[index] = true, list
			} else {
				s.SeqScalingListPresentFlag[index], s.SeqScalingList[index] = true, list
			}
			if _, err := SelectScalingMatrices(s, p); !errors.Is(err, h264.ErrInvalidData) {
				t.Fatalf("pps%v list%d本地矩阵和回读矩阵分裂: %v", usePPS, index, err)
			}
			if usePPS {
				p.PicScalingListPresentFlag[index] = false
			} else {
				s.SeqScalingListPresentFlag[index] = false
			}
		}
	}
}

func scalingParsedConstant(t *testing.T, size int, value int32) *ScalingList {
	t.Helper()
	list := &ScalingList{Size: size, Deltas: make([]int32, size*size)}
	list.Deltas[0] = value - 8
	if list.Deltas[0] > 127 {
		list.Deltas[0] -= 256
	}
	w := bitstream.NewWriter()
	if err := list.WriteTo(w); err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseScalingList(bitstream.NewReader(w.Bytes()), size)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestScalingParsedMatrices_FixedQuantizedConsumerValues(t *testing.T) {
	// 规范8.5.12/8.5.13的DC解析解：QP0的4x4 level16对应10*weight，
	// 8x8 level64对应20*weight。不读取被测实现的缩放表计算期望。
	for _, separate := range []bool{false, true} {
		for _, usePPS := range []bool{false, true} {
			s := &SPS{ProfileIDC: 244, ChromaFormatIDC: 3, SeparateColourPlaneFlag: separate, SeqScalingMatrixPresentFlag: !usePPS}
			p := &PPS{PicScalingMatrixPresentFlag: usePPS, Transform8x8ModeFlag: true}
			for index := range 12 {
				size, value := 4, int32(16+index*8)
				if index >= 6 {
					size = 8
				}
				list := scalingParsedConstant(t, size, value)
				if usePPS {
					p.PicScalingListPresentFlag[index], p.PicScalingList[index] = true, list
				} else {
					s.SeqScalingListPresentFlag[index], s.SeqScalingList[index] = true, list
				}
			}
			m, err := SelectScalingMatrices(s, p)
			if err != nil {
				t.Fatal(err)
			}
			for plane := range 3 {
				for mode := range 2 {
					index := mode*3 + plane
					var levels4, coeff4 [16]int32
					levels4[0] = 16
					if err := transform.Dequant4x4WithScaling(&coeff4, &levels4, 0, &m.Four[index]); err != nil {
						t.Fatal(err)
					}
					if want := int32(10 * (16 + 8*index)); coeff4[0] != want {
						t.Fatalf("separate%v pps%v 4x4 plane%d mode%d=%d want%d", separate, usePPS, plane, mode, coeff4[0], want)
					}
					index = plane*2 + mode
					var levels8, coeff8 [64]int32
					levels8[0] = 64
					if err := transform.Dequant8x8(&coeff8, &levels8, 0, &m.Eight[index]); err != nil {
						t.Fatal(err)
					}
					if want := int32(20 * (64 + 8*index)); coeff8[0] != want {
						t.Fatalf("separate%v pps%v 8x8 plane%d mode%d=%d want%d", separate, usePPS, plane, mode, coeff8[0], want)
					}
				}
			}
		}
	}
}

func TestScalingParameters_LastListFailsBeforeOutput(t *testing.T) {
	for _, separate := range []bool{false, true} {
		s := &SPS{ProfileIDC: 244, LevelIDC: 30, ChromaFormatIDC: 3, SeparateColourPlaneFlag: separate, FrameMbsOnlyFlag: true, Direct8x8InferenceFlag: true, SeqScalingMatrixPresentFlag: true}
		s.SeqScalingListPresentFlag[11] = true
		s.SeqScalingList[11] = &ScalingList{Size: 8, Deltas: make([]int32, 63)}
		w := bitstream.NewWriter()
		w.WriteBits(3, 5)
		if err := s.WriteTo(w); !errors.Is(err, h264.ErrInvalidData) || w.BitsWritten() != 3 {
			t.Fatalf("SPS最后一个444列表污染输出: bits%d err%v", w.BitsWritten(), err)
		}
		s.SeqScalingMatrixPresentFlag = false
		p := &PPS{Transform8x8ModeFlag: true, PicScalingMatrixPresentFlag: true}
		p.PicScalingListPresentFlag[11] = true
		p.PicScalingList[11] = &ScalingList{Size: 8, Deltas: make([]int32, 63)}
		if err := p.WriteTo(w, s); !errors.Is(err, h264.ErrInvalidData) || w.BitsWritten() != 3 {
			t.Fatalf("PPS最后一个444列表污染输出: bits%d err%v", w.BitsWritten(), err)
		}
	}
}
