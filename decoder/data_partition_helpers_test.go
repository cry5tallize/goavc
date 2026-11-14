package decoder

import (
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/nal"
)

// 分区码字直接取自规范 CAVLC 表，不调用 encoder 或解码器内部变换。
func partitionVector(mode string) []nal.Unit {
	a, b, c := bitstream.NewWriter(), bitstream.NewWriter(), bitstream.NewWriter()
	a.WriteUE(0)
	if mode == "pcm" {
		a.WriteUE(2)
	} else {
		a.WriteUE(0)
	}
	a.WriteUE(0)
	a.WriteBits(4, 1)
	if mode != "pcm" {
		a.WriteBit(0)
		a.WriteBit(0)
	}
	a.WriteBit(0) // 非 IDR 参考图像标记
	a.WriteSE(0)
	a.WriteUE(1) // disable_deblocking_filter_idc
	a.WriteUE(0) // slice_id
	b.WriteUE(0)
	c.WriteUE(0)
	hasB, hasC := false, false
	if mode == "pcm" {
		for mb := range 16 {
			a.WriteUE(25) // A 只包含 I_PCM 的 mb_type
			b.ByteAlign()
			for plane := range 3 {
				n := 16
				if plane != 0 {
					n = 8
				}
				for y := range n {
					for x := range n {
						b.WriteBits(8, uint64(fmoSample(plane, mb, x, y)))
					}
				}
			}
		}
		hasB = true
	} else {
		used := 0
		if mode == "i16" || mode == "mixed" {
			a.WriteUE(0) // mb_skip_run
			a.WriteUE(8) // P slice 的 I16x16 DC、CBP=0
			a.WriteUE(0) // intra_chroma_pred_mode
			a.WriteSE(0) // mb_qp_delta 属于 A
			// coeff_token(TotalCoeff=1,TrailingOnes=1)=01，sign=0，total_zeros=0 为 1。
			b.WriteBits(4, 5)
			hasB, used = true, 1
		}
		if mode == "inter" || mode == "mixed" {
			a.WriteUE(0)
			a.WriteUE(0) // P_L0_16x16
			a.WriteSE(8) // 整亮度 +2 / 色度 +1 采样点，非零 MVD
			a.WriteSE(0)
			a.WriteUE(2) // inter CBP=1：左上 8x8 的四个 4x4
			a.WriteSE(0)
			c.WriteBits(4, 5) // 首 4x4 的 DC level=+1
			for range 3 {
				c.WriteBit(1)
			} // 其余三个块 TotalCoeff=0
			hasC = true
			used++
		}
		a.WriteUE(uint32(16 - used))
	}
	bitstream.WriteRBSPTrailingBits(a)
	out := []nal.Unit{fmoUnit(nal.TypeSlicePartitionA, a.Bytes())}
	if hasB {
		bitstream.WriteRBSPTrailingBits(b)
		out = append(out, fmoUnit(nal.TypeSlicePartitionB, b.Bytes()))
	}
	if hasC {
		bitstream.WriteRBSPTrailingBits(c)
		out = append(out, fmoUnit(nal.TypeSlicePartitionC, c.Bytes()))
	}
	return out
}

func partitionReference(t *testing.T, d *Decoder) {
	t.Helper()
	v := fmoVector{groups: [][]int{{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}}}
	if err := d.SendPacket(h264.Packet{Data: fmoPacket(t, v, fmoSlices(v))}); err != nil {
		t.Fatal(err)
	}
	fmoAssertPicture(t, d, -1)
}

func partitionAssert(t *testing.T, d *Decoder, mode string) {
	t.Helper()
	if mode == "pcm" {
		fmoAssertPicture(t, d, -1)
		return
	}
	f, err := d.ReceiveFrame()
	if err != nil {
		t.Fatal(err)
	}
	defer f.Release()
	if f.Width != 64 || f.Height != 64 || f.Format != h264.YUV420P {
		t.Fatalf("unexpected partition frame: %dx%d %v", f.Width, f.Height, f.Format)
	}
	for plane := range 3 {
		n := 16
		if plane != 0 {
			n = 8
		}
		for y := range n * 4 {
			for x := range n * 4 {
				mb := y/n*4 + x/n
				intra := (mode == "i16" || mode == "mixed") && mb == 0
				dx := 0
				interMB := 0
				if mode == "mixed" {
					interMB = 1
				}
				if (mode == "inter" || mode == "mixed") && mb == interMB {
					dx = 2
					if plane != 0 {
						dx = 1
					}
				}
				sx := x + dx
				if sx >= n*4 {
					sx = n*4 - 1
				}
				want := int(fmoSample(plane, y/n*4+sx/n, sx%n, y%n))
				if intra {
					want = 128
					// QP26 的 DC 反量化 13*16/4=52，(52+32)>>6=1。
					if plane == 0 {
						want = 129
					}
				} else if dx != 0 && plane == 0 && x%n < 4 && y%n < 4 {
					// 普通4x4 DC：13*16=208，(208+32)>>6=3。
					want += 3
					if want > 255 {
						want = 255
					}
				}
				if got := int(f.Planes[plane].Data[y*f.Planes[plane].Stride+x]); got != want {
					t.Fatalf("%s plane %d (%d,%d): got %d want %d", mode, plane, x, y, got, want)
				}
			}
		}
	}
}
