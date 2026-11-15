package decoder

import (
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/bitstream"
	"github.com/cry5tallize/goavc/nal"
	"github.com/cry5tallize/goavc/syntax"
)

// fmoVector 的地址表是手算规范向量，不调用待测的 map/NextMbAddress 实现。
type fmoVector struct {
	pps    syntax.PPS
	groups [][]int
	cycle  uint32
}

type fmoSliceVector struct {
	addresses []int
	cycle     uint32
	i4        int
}

func fmoSample(c, mb, x, y int) byte {
	return byte(17 + (mb*29+c*53+x*7+y*11)%221)
}

func fmoUnit(kind nal.UnitType, rb []byte) nal.Unit {
	return nal.Unit{Header: nal.Header{Type: kind, RefIDC: 3}, Payload: bitstream.RBSPToEBSP(rb)}
}

func fmoPacket(t *testing.T, v fmoVector, slices []fmoSliceVector) []byte {
	t.Helper()
	// Extended profile 支持 FMO/ASO；始终使用 CAVLC 和逐行 4:2:0。
	s := &syntax.SPS{ProfileIDC: 88, LevelIDC: 20, ChromaFormatIDC: 1, MaxNumRefFrames: 1, PicOrderCntType: 2, PicWidthInMbsMinus1: 3, PicHeightInMapUnitsMinus1: 3, FrameMbsOnlyFlag: true, Direct8x8InferenceFlag: true}
	// 用真实 VUI 声明零重排，ReceiveFrame 可在每次 SendPacket 后取帧，
	// 无需 Flush 终止仍需继续提交 P 帧的解码器。
	s.VUIParametersPresentFlag = true
	s.VUI = &syntax.VUI{
		BitstreamRestrictionFlag:           true,
		MotionVectorsOverPicBoundariesFlag: true,
		MaxBytesPerPicDenom:                0,
		MaxBitsPerMBDenom:                  0,
		Log2MaxMVLengthHorizontal:          15,
		Log2MaxMVLengthVertical:            15,
		MaxNumReorderFrames:                0,
		MaxDecFrameBuffering:               1,
	}
	p := v.pps
	p.DeblockingFilterControlPresentFlag = true
	srb, err := s.MarshalRBSP()
	if err != nil {
		t.Fatal(err)
	}
	prb, err := p.MarshalRBSP(s)
	if err != nil {
		t.Fatal(err)
	}
	units := []nal.Unit{fmoUnit(nal.TypeSPS, srb), fmoUnit(nal.TypePPS, prb)}
	for _, sl := range slices {
		w := bitstream.NewWriter()
		w.WriteUE(uint32(sl.addresses[0]))
		w.WriteUE(2)
		w.WriteUE(0)
		w.WriteBits(4, 0)
		w.WriteUE(0)
		w.WriteBits(2, 0)
		w.WriteSE(0)
		w.WriteUE(1) // 禁用去块，像素期望不受跨宏块滤波影响。
		if p.NumSliceGroupsMinus1 > 0 && p.SliceGroupMapType >= 3 && p.SliceGroupMapType <= 5 {
			// 16 map units，rate=1，ceil(log2(16+1))=5。
			w.WriteBits(5, uint64(sl.cycle))
		}
		for _, mb := range sl.addresses {
			if mb == sl.i4 {
				// I_NxN：16 个 4x4 块均使用最可能的 DC 模式。
				w.WriteUE(0)
				for range 16 {
					w.WriteBit(1)
				}
				w.WriteUE(0)
				w.WriteUE(3) // intra coded_block_pattern=0，无残差。
				continue
			}
			w.WriteUE(25)
			w.ByteAlign()
			for c := range 3 {
				n := 16
				if c != 0 {
					n = 8
				}
				for y := range n {
					for x := range n {
						w.WriteBits(8, uint64(fmoSample(c, mb, x, y)))
					}
				}
			}
		}
		bitstream.WriteRBSPTrailingBits(w)
		units = append(units, fmoUnit(nal.TypeSliceIDR, w.Bytes()))
	}
	return annex(units...)
}

func fmoSlices(v fmoVector) []fmoSliceVector {
	var out []fmoSliceVector
	// 逆序提交组；组内地址仍由规范 NextMbAddress 前进。
	for i := len(v.groups) - 1; i >= 0; i-- {
		out = append(out, fmoSliceVector{addresses: v.groups[i], cycle: v.cycle, i4: -1})
	}
	return out
}

func fmoAssertPicture(t *testing.T, d *Decoder, i4 int) {
	t.Helper()
	f, err := d.ReceiveFrame()
	if err != nil {
		t.Fatal(err)
	}
	defer f.Release()
	if f.Width != 64 || f.Height != 64 || f.Format != h264.YUV420P {
		t.Fatalf("unexpected frame: %dx%d %v", f.Width, f.Height, f.Format)
	}
	for c := range 3 {
		n := 16
		if c != 0 {
			n = 8
		}
		for y := range n * 4 {
			for x := range n * 4 {
				mb := (y/n)*4 + x/n
				want := fmoSample(c, mb, x%n, y%n)
				if mb == i4 {
					want = 128
				}
				if got := f.Planes[c].Data[y*f.Planes[c].Stride+x]; got != want {
					t.Fatalf("plane %d mb %d (%d,%d): got %d want %d", c, mb, x, y, got, want)
				}
			}
		}
	}
}

// fmoInterPacket 的显式地址为组1的两行（0..3、8..11）及组0的两行。
// 最后一次 skip_run=4 必须跨过不属于本组的 4..7，而不是填满光栅空洞。
func fmoInterPacket(plain bool) []byte {
	var units []nal.Unit
	starts := []uint32{0, 12, 4}
	if plain {
		starts = []uint32{0, 8, 12, 4}
	}
	for _, first := range starts {
		w := bitstream.NewWriter()
		w.WriteUE(first)
		w.WriteUE(0) // P slice
		w.WriteUE(0)
		w.WriteBits(4, 1)
		w.WriteBit(0) // num_ref_idx_active_override_flag
		w.WriteBit(0) // ref_pic_list_modification_flag_l0
		w.WriteBit(0) // adaptive_ref_pic_marking_mode_flag
		w.WriteSE(0)
		w.WriteUE(1)
		count := 4
		if plain && first == 8 {
			w.WriteUE(4)
			count = 0
		}
		for i := range count {
			w.WriteUE(0) // mb_skip_run
			w.WriteUE(0) // P_L0_16x16，唯一参考无需 ref_idx_l0
			dx := int32(0)
			if i == 0 {
				dx = -8
				if first == 0 {
					dx = 8
				}
			}
			w.WriteSE(dx)
			w.WriteSE(0)
			w.WriteUE(0) // inter coded_block_pattern=0
		}
		if first == 0 && !plain {
			w.WriteUE(4)
		}
		bitstream.WriteRBSPTrailingBits(w)
		units = append(units, fmoUnit(nal.TypeSlice, w.Bytes()))
	}
	return annex(units...)
}

func fmoAssertInterPicture(t *testing.T, d *Decoder) {
	t.Helper()
	f, err := d.ReceiveFrame()
	if err != nil {
		t.Fatal(err)
	}
	defer f.Release()
	if f.Width != 64 || f.Height != 64 || f.Format != h264.YUV420P {
		t.Fatalf("unexpected inter frame: %dx%d %v", f.Width, f.Height, f.Format)
	}
	for c := range 3 {
		n := 16
		if c != 0 {
			n = 8
		}
		for y := range n * 4 {
			for x := range n * 4 {
				// MV=±8 quarter luma，即亮度±2、色度±1个整采样点。
				dx := 0
				switch y / n {
				case 0:
					dx = 2
				case 1, 3:
					dx = -2
				}
				if c != 0 {
					dx /= 2
				}
				sx := x + dx
				if sx < 0 {
					sx = 0
				}
				if sx >= n*4 {
					sx = n*4 - 1
				}
				// 从原始 PCM 的空间坐标直接求参考像素，不借用解码帧或 MC 实现。
				mb := (y/n)*4 + sx/n
				want := fmoSample(c, mb, sx%n, y%n)
				if got := f.Planes[c].Data[y*f.Planes[c].Stride+x]; got != want {
					t.Fatalf("inter plane %d (%d,%d): got %d want %d", c, x, y, got, want)
				}
			}
		}
	}
}
