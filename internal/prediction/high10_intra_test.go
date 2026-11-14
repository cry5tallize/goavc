package prediction

import (
	"errors"
	"testing"

	h264 "github.com/goavc/h264"
)

// 固定向量直接由规范的整数平均式解析计算，包含非 4 倍数采样与 1023。
func TestHigh10Intra4x4AnalyticVectors(t *testing.T) {
	s := Intra4x4Samples16{
		Top: [4]uint16{101, 300, 700, 1023}, TopRight: [4]uint16{900, 800, 500, 17},
		Left: [4]uint16{999, 722, 411, 3}, TopLeft: 509,
	}
	want := [9][16]uint16{
		{101, 300, 700, 1023, 101, 300, 700, 1023, 101, 300, 700, 1023, 101, 300, 700, 1023},
		{999, 999, 999, 999, 722, 722, 722, 722, 411, 411, 411, 411, 3, 3, 3, 3},
		{532, 532, 532, 532, 532, 532, 532, 532, 532, 532, 532, 532, 532, 532, 532, 532},
		{350, 681, 912, 906, 681, 912, 906, 750, 912, 906, 750, 454, 906, 750, 454, 138},
		{530, 253, 350, 681, 807, 530, 253, 350, 714, 807, 530, 253, 387, 714, 807, 530},
		{305, 201, 500, 862, 530, 253, 350, 681, 807, 305, 201, 500, 714, 530, 253, 350},
		{754, 530, 253, 350, 861, 807, 754, 530, 567, 714, 861, 807, 207, 387, 567, 714},
		{201, 500, 862, 962, 350, 681, 912, 906, 500, 862, 962, 850, 681, 912, 906, 750},
		{861, 714, 567, 387, 567, 387, 207, 105, 207, 105, 3, 3, 3, 3, 3, 3},
	}
	for mode := range want {
		var dst [4 * 7]uint16
		for i := range dst {
			dst[i] = 65535
		}
		if err := Intra4x4High(dst[:], 7, mode, s, true, true, 10); err != nil {
			t.Fatal(err)
		}
		for y := range 4 {
			for x := range 7 {
				v := uint16(65535)
				if x < 4 {
					v = want[mode][4*y+x]
				}
				if dst[7*y+x] != v {
					t.Fatalf("mode=%d (%d,%d): got %d want %d", mode, x, y, dst[7*y+x], v)
				}
			}
		}
	}
}

func TestHigh10Intra8x8AnalyticVectors(t *testing.T) {
	s := Intra8x8Samples16{TopLeft: 584, TopLeftAvailable: true}
	for i := range s.Top {
		s.Top[i] = uint16(600 + 16*i)
	}
	for i := range s.Left {
		s.Left[i] = uint16(568 - 16*i)
	}
	// 邻域是解析斜坡；末端三抽头外推使 top[15]=836、left[7]=460。
	// 固定期望包含这些端点对方向预测的影响，不从生产函数生成期望。
	want := [9][64]uint16{
		{
			600, 616, 632, 648, 664, 680, 696, 712,
			600, 616, 632, 648, 664, 680, 696, 712,
			600, 616, 632, 648, 664, 680, 696, 712,
			600, 616, 632, 648, 664, 680, 696, 712,
			600, 616, 632, 648, 664, 680, 696, 712,
			600, 616, 632, 648, 664, 680, 696, 712,
			600, 616, 632, 648, 664, 680, 696, 712,
			600, 616, 632, 648, 664, 680, 696, 712,
		},
		{
			568, 568, 568, 568, 568, 568, 568, 568,
			552, 552, 552, 552, 552, 552, 552, 552,
			536, 536, 536, 536, 536, 536, 536, 536,
			520, 520, 520, 520, 520, 520, 520, 520,
			504, 504, 504, 504, 504, 504, 504, 504,
			488, 488, 488, 488, 488, 488, 488, 488,
			472, 472, 472, 472, 472, 472, 472, 472,
			460, 460, 460, 460, 460, 460, 460, 460,
		},
		{
			584, 584, 584, 584, 584, 584, 584, 584,
			584, 584, 584, 584, 584, 584, 584, 584,
			584, 584, 584, 584, 584, 584, 584, 584,
			584, 584, 584, 584, 584, 584, 584, 584,
			584, 584, 584, 584, 584, 584, 584, 584,
			584, 584, 584, 584, 584, 584, 584, 584,
			584, 584, 584, 584, 584, 584, 584, 584,
			584, 584, 584, 584, 584, 584, 584, 584,
		},
		{
			616, 632, 648, 664, 680, 696, 712, 728,
			632, 648, 664, 680, 696, 712, 728, 744,
			648, 664, 680, 696, 712, 728, 744, 760,
			664, 680, 696, 712, 728, 744, 760, 776,
			680, 696, 712, 728, 744, 760, 776, 792,
			696, 712, 728, 744, 760, 776, 792, 808,
			712, 728, 744, 760, 776, 792, 808, 823,
			728, 744, 760, 776, 792, 808, 823, 833,
		},
		{
			584, 600, 616, 632, 648, 664, 680, 696,
			568, 584, 600, 616, 632, 648, 664, 680,
			552, 568, 584, 600, 616, 632, 648, 664,
			536, 552, 568, 584, 600, 616, 632, 648,
			520, 536, 552, 568, 584, 600, 616, 632,
			504, 520, 536, 552, 568, 584, 600, 616,
			488, 504, 520, 536, 552, 568, 584, 600,
			473, 488, 504, 520, 536, 552, 568, 584,
		},
		{
			592, 608, 624, 640, 656, 672, 688, 704,
			584, 600, 616, 632, 648, 664, 680, 696,
			568, 592, 608, 624, 640, 656, 672, 688,
			552, 584, 600, 616, 632, 648, 664, 680,
			536, 568, 592, 608, 624, 640, 656, 672,
			520, 552, 584, 600, 616, 632, 648, 664,
			504, 536, 568, 592, 608, 624, 640, 656,
			488, 520, 552, 584, 600, 616, 632, 648,
		},
		{
			576, 584, 600, 616, 632, 648, 664, 680,
			560, 568, 576, 584, 600, 616, 632, 648,
			544, 552, 560, 568, 576, 584, 600, 616,
			528, 536, 544, 552, 560, 568, 576, 584,
			512, 520, 528, 536, 544, 552, 560, 568,
			496, 504, 512, 520, 528, 536, 544, 552,
			480, 488, 496, 504, 512, 520, 528, 536,
			466, 473, 480, 488, 496, 504, 512, 520,
		},
		{
			608, 624, 640, 656, 672, 688, 704, 720,
			616, 632, 648, 664, 680, 696, 712, 728,
			624, 640, 656, 672, 688, 704, 720, 736,
			632, 648, 664, 680, 696, 712, 728, 744,
			640, 656, 672, 688, 704, 720, 736, 752,
			648, 664, 680, 696, 712, 728, 744, 760,
			656, 672, 688, 704, 720, 736, 752, 768,
			664, 680, 696, 712, 728, 744, 760, 776,
		},
		{
			560, 552, 544, 536, 528, 520, 512, 504,
			544, 536, 528, 520, 512, 504, 496, 488,
			528, 520, 512, 504, 496, 488, 480, 473,
			512, 504, 496, 488, 480, 473, 466, 463,
			496, 488, 480, 473, 466, 463, 460, 460,
			480, 473, 466, 463, 460, 460, 460, 460,
			466, 463, 460, 460, 460, 460, 460, 460,
			460, 460, 460, 460, 460, 460, 460, 460,
		},
	}
	for mode := range want {
		var dst [8 * 11]uint16
		if err := Intra8x8High(dst[:], 11, mode, s, true, true, 10); err != nil {
			t.Fatal(err)
		}
		for y := range 8 {
			for x := range 11 {
				v := uint16(0)
				if x < 8 {
					v = want[mode][8*y+x]
				}
				if dst[11*y+x] != v {
					t.Fatalf("mode=%d (%d,%d): got %d want %d", mode, x, y, dst[11*y+x], v)
				}
			}
		}
	}
}

func TestHigh10IntraDCDefaultsAndAvailability(t *testing.T) {
	for _, c := range []struct {
		top, left            bool
		want4, want8, want16 uint16
	}{{false, false, 512, 512, 512}, {true, false, 519, 543, 591}, {false, true, 785, 729, 617}, {true, true, 652, 636, 604}} {
		var s4 Intra4x4Samples16
		var s8 Intra8x8Samples16
		var top, left [16]uint16
		for i := range top {
			top[i], left[i] = uint16(501+12*i), uint16(827-28*i)
			if i < 4 {
				s4.Top[i], s4.Left[i] = top[i], left[i]
			}
			if i < 8 {
				s8.Left[i] = left[i]
			}
			s8.Top[i] = top[i]
		}
		var d4 [16]uint16
		var d8 [64]uint16
		var d16 [256]uint16
		if err := Intra4x4High(d4[:], 4, 2, s4, c.top, c.left, 10); err != nil {
			t.Fatal(err)
		}
		if err := Intra8x8High(d8[:], 8, 2, s8, c.top, c.left, 10); err != nil {
			t.Fatal(err)
		}
		if err := Intra16x16High(d16[:], 16, 2, IntraBlockSamples16{Top: top[:], Left: left[:]}, c.top, c.left, 10); err != nil {
			t.Fatal(err)
		}
		for _, block := range []struct {
			data []uint16
			want uint16
		}{{d4[:], c.want4}, {d8[:], c.want8}, {d16[:], c.want16}} {
			for _, v := range block.data {
				if v != block.want {
					t.Fatalf("top=%v left=%v n=%d: got %d want %d", c.top, c.left, len(block.data), v, block.want)
				}
			}
		}
	}
	var out [64]uint16
	s := Intra8x8Samples16{}
	if err := Intra8x8High(out[:], 8, 4, s, true, true, 10); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("missing top-left accepted: %v", err)
	}
	for i := range s.Top {
		s.Top[i] = uint16(501 + 12*i)
	}
	if err := Intra8x8High(out[:], 8, 0, s, true, false, 10); err != nil || out[0] != 504 || out[7] != 585 {
		t.Fatalf("unavailable top-left smoothing: %v, %v", err, out[:8])
	}
}

func TestHigh10IntraBlockAnalyticVectors(t *testing.T) {
	var top, left [16]uint16
	for i := range top {
		top[i], left[i] = uint16(501+12*i), uint16(827-28*i)
	}
	s := IntraBlockSamples16{Top: top[:], Left: left[:], TopLeft: 613}
	for _, c := range []struct {
		top, left bool
		quadrants [4]uint16
	}{{false, false, [4]uint16{512, 512, 512, 512}}, {true, false, [4]uint16{519, 567, 519, 567}}, {false, true, [4]uint16{785, 785, 673, 673}}, {true, true, [4]uint16{652, 567, 673, 620}}} {
		var dst [64]uint16
		if err := Chroma8x8High(dst[:], 8, 0, s, c.top, c.left, 10); err != nil {
			t.Fatal(err)
		}
		for y := range 8 {
			for x := range 8 {
				if got, want := dst[8*y+x], c.quadrants[(y/4)*2+x/4]; got != want {
					t.Fatalf("chroma DC top=%v left=%v (%d,%d): got %d want %d", c.top, c.left, x, y, got, want)
				}
			}
		}
	}
	for _, n := range []int{8, 16} {
		for _, vertical := range []bool{false, true} {
			dst := make([]uint16, n*(n+3))
			mode := 1
			if vertical {
				mode = 0
			}
			var err error
			if n == 16 {
				err = Intra16x16High(dst, n+3, mode, s, true, true, 10)
			} else {
				if vertical {
					mode = 2
				}
				err = Chroma8x8High(dst, n+3, mode, s, true, true, 10)
			}
			if err != nil {
				t.Fatal(err)
			}
			for y := range n {
				for x := range n + 3 {
					want := uint16(0)
					if x < n {
						want = left[y]
						if vertical {
							want = top[x]
						}
					}
					if dst[y*(n+3)+x] != want {
						t.Fatalf("n=%d mode=%d (%d,%d)", n, mode, x, y)
					}
				}
			}
		}
	}
	for _, n := range []int{8, 16} {
		var edge [16]uint16
		for i := n / 2; i < n-1; i++ {
			edge[i] = 1023
		}
		var dst [256]uint16
		s := IntraBlockSamples16{Top: edge[:n], Left: edge[:n]}
		var err error
		coefficient := 3261
		if n == 16 {
			coefficient = 2238
			err = Intra16x16High(dst[:], n, 3, s, true, true, 10)
		} else {
			err = Chroma8x8High(dst[:], n, 3, s, true, true, 10)
		}
		if err != nil {
			t.Fatal(err)
		}
		// 此邻域 a=0，b=c 分别为 3261/2238；显式核对上下限裁剪。
		for y := range n {
			for x := range n {
				want := max(0, min(1023, (coefficient*(x+y-2*(n/2-1))+16)>>5))
				if int(dst[n*y+x]) != want {
					t.Fatalf("plane n=%d (%d,%d): got %d want %d", n, x, y, dst[n*y+x], want)
				}
			}
		}
	}
}
