package decoder

import (
	"errors"
	"fmt"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/syntax"
)

func fmoNormativeVectors() []fmoVector {
	return []fmoVector{
		{pps: syntax.PPS{NumSliceGroupsMinus1: 1, SliceGroupMapType: 0, RunLengthMinus1: []uint32{1, 0}}, groups: [][]int{{0, 1, 3, 4, 6, 7, 9, 10, 12, 13, 15}, {2, 5, 8, 11, 14}}},
		{pps: syntax.PPS{NumSliceGroupsMinus1: 1, SliceGroupMapType: 1}, groups: [][]int{{0, 2, 5, 7, 8, 10, 13, 15}, {1, 3, 4, 6, 9, 11, 12, 14}}},
		{pps: syntax.PPS{NumSliceGroupsMinus1: 1, SliceGroupMapType: 2, TopLeft: []uint32{0}, BottomRight: []uint32{5}}, groups: [][]int{{0, 1, 4, 5}, {2, 3, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}}},
		{pps: syntax.PPS{NumSliceGroupsMinus1: 1, SliceGroupMapType: 3}, cycle: 1, groups: [][]int{{10}, {0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 11, 12, 13, 14, 15}}},
		{pps: syntax.PPS{NumSliceGroupsMinus1: 1, SliceGroupMapType: 4}, cycle: 5, groups: [][]int{{0, 1, 2, 3, 4}, {5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}}},
		{pps: syntax.PPS{NumSliceGroupsMinus1: 1, SliceGroupMapType: 5}, cycle: 5, groups: [][]int{{0, 1, 4, 8, 12}, {2, 3, 5, 6, 7, 9, 10, 11, 13, 14, 15}}},
		{pps: syntax.PPS{NumSliceGroupsMinus1: 2, SliceGroupMapType: 6, PicSizeInMapUnitsMinus1: 15, SliceGroupID: []uint8{0, 2, 1, 0, 1, 0, 2, 1, 2, 1, 0, 2, 0, 2, 1, 0}}, groups: [][]int{{0, 3, 5, 10, 12, 15}, {2, 4, 7, 9, 14}, {1, 6, 8, 11, 13}}},
	}
}

func TestFMO_NormativeMapTypesPCM(t *testing.T) {
	for _, v := range fmoNormativeVectors() {
		t.Run(fmt.Sprintf("map%d", v.pps.SliceGroupMapType), func(t *testing.T) {
			d := mustDecoder(t, h264.DecoderConfig{})
			if err := d.SendPacket(h264.Packet{Data: fmoPacket(t, v, fmoSlices(v))}); err != nil {
				t.Fatal(err)
			}
			fmoAssertPicture(t, d, -1)
		})
	}
}

func TestFMO_PlainASOReverseOrder(t *testing.T) {
	v := fmoVector{}
	var slices []fmoSliceVector
	for mb := 15; mb >= 0; mb-- {
		slices = append(slices, fmoSliceVector{addresses: []int{mb}, i4: -1})
	}
	d := mustDecoder(t, h264.DecoderConfig{})
	if err := d.SendPacket(h264.Packet{Data: fmoPacket(t, v, slices)}); err != nil {
		t.Fatal(err)
	}
	fmoAssertPicture(t, d, -1)
}

func TestFMO_CrossSliceNeighborsUnavailable(t *testing.T) {
	v := fmoNormativeVectors()[6]
	var slices []fmoSliceVector
	// 先解码物理左、上 PCM 邻居，最后解码独立 slice 的 I4 DC。
	// MB5 与左侧 MB4 分属不同组，与上方 MB1 也分属不同组；
	// 若错误借用已解码邻居，亮度/色度 DC 均不再是规范的 128。
	for mb := range 16 {
		if mb != 5 {
			slices = append(slices, fmoSliceVector{addresses: []int{mb}, i4: -1})
		}
	}
	slices = append(slices, fmoSliceVector{addresses: []int{5}, i4: 5})
	d := mustDecoder(t, h264.DecoderConfig{})
	if err := d.SendPacket(h264.Packet{Data: fmoPacket(t, v, slices)}); err != nil {
		t.Fatal(err)
	}
	fmoAssertPicture(t, d, 5)
}

func TestFMO_InvalidSliceCoverageAndRecovery(t *testing.T) {
	base := fmoNormativeVectors()[0]
	for _, name := range []string{"duplicate", "missing", "out_of_range", "overlap", "dynamic_cycle"} {
		t.Run(name, func(t *testing.T) {
			v := base
			slices := fmoSlices(v)
			switch name {
			case "duplicate":
				slices = append(slices, slices[0])
			case "missing":
				slices = slices[:1]
			case "out_of_range":
				slices = append(slices, fmoSliceVector{addresses: []int{16}, i4: -1})
			case "overlap":
				// 不同 first_mb_in_slice，但第二段覆盖第一段的尾部。
				slices = append(slices, fmoSliceVector{addresses: []int{5, 8, 11, 14}, i4: -1})
			case "dynamic_cycle":
				v = fmoNormativeVectors()[4]
				slices = fmoSlices(v)
				slices[1].cycle = 6
			}
			d := mustDecoder(t, h264.DecoderConfig{})
			if err := d.SendPacket(h264.Packet{Data: fmoPacket(t, v, slices)}); !errors.Is(err, h264.ErrInvalidData) {
				t.Fatalf("%s: got %v, want ErrInvalidData", name, err)
			}
			f, err := d.ReceiveFrame()
			if f != nil {
				f.Release()
			}
			if !errors.Is(err, h264.ErrAgain) {
				t.Fatalf("corrupt picture escaped: %v", err)
			}
			if err := d.SendPacket(h264.Packet{Data: fmoPacket(t, base, fmoSlices(base))}); err != nil {
				t.Fatalf("IDR recovery: %v", err)
			}
			fmoAssertPicture(t, d, -1)
		})
	}
}

func TestFMO_PSliceMVPOwnershipAndSkipRun(t *testing.T) {
	v := fmoVector{
		pps:    syntax.PPS{NumSliceGroupsMinus1: 1, SliceGroupMapType: 6, PicSizeInMapUnitsMinus1: 15, SliceGroupID: []uint8{1, 1, 1, 1, 0, 0, 0, 0, 1, 1, 1, 1, 0, 0, 0, 0}},
		groups: [][]int{{4, 5, 6, 7, 12, 13, 14, 15}, {0, 1, 2, 3, 8, 9, 10, 11}},
	}
	d := mustDecoder(t, h264.DecoderConfig{})
	if err := d.SendPacket(h264.Packet{Data: fmoPacket(t, v, fmoSlices(v))}); err != nil {
		t.Fatal(err)
	}
	fmoAssertPicture(t, d, -1)
	// 组1先解码：第一行 MV=+8，隔行 skip_run=4 得到零 MV。
	// 随后组0按下、上两条 slice 解码，各首 MB 的 MVD=-8；
	// 不可使用另一组已解码的上邻居 +8，否则整行像素都会偏移。
	// 同一 slice 后三个连续 MB 则必须继承本 slice 的左邻居 -8。
	if err := d.SendPacket(h264.Packet{Data: fmoInterPacket(false)}); err != nil {
		t.Fatal(err)
	}
	fmoAssertInterPicture(t, d)
}

func TestFMO_PlainASOCoverageErrors(t *testing.T) {
	for _, overlap := range []bool{false, true} {
		t.Run(fmt.Sprintf("overlap%t", overlap), func(t *testing.T) {
			v := fmoVector{}
			slices := []fmoSliceVector{
				{addresses: []int{12, 13, 14, 15}, i4: -1},
				{addresses: []int{8, 9, 10, 11}, i4: -1},
				{addresses: []int{0, 1, 2, 3}, i4: -1},
			}
			if overlap {
				slices = append(slices, fmoSliceVector{addresses: []int{3, 4, 5, 6, 7}, i4: -1})
			}
			d := mustDecoder(t, h264.DecoderConfig{})
			if err := d.SendPacket(h264.Packet{Data: fmoPacket(t, v, slices)}); !errors.Is(err, h264.ErrInvalidData) {
				t.Fatalf("ordinary ASO coverage: %v", err)
			}
			f, err := d.ReceiveFrame()
			if f != nil {
				f.Release()
			}
			if !errors.Is(err, h264.ErrAgain) {
				t.Fatalf("corrupt ASO picture escaped: %v", err)
			}
			valid := []fmoSliceVector{{addresses: []int{12, 13, 14, 15}, i4: -1}, {addresses: []int{8, 9, 10, 11}, i4: -1}, {addresses: []int{4, 5, 6, 7}, i4: -1}, {addresses: []int{0, 1, 2, 3}, i4: -1}}
			if err := d.SendPacket(h264.Packet{Data: fmoPacket(t, v, valid)}); err != nil {
				t.Fatalf("ASO IDR recovery: %v", err)
			}
			fmoAssertPicture(t, d, -1)
		})
	}
}

func TestFMO_PlainASOPSliceMVPOwnership(t *testing.T) {
	// 同一组的不同 slice 也必须隔离：先解码的顶行 MV=+8，
	// 不能污染下一行独立 slice 首宏块的 MVD=-8。
	v := fmoVector{groups: [][]int{{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}}}
	d := mustDecoder(t, h264.DecoderConfig{})
	if err := d.SendPacket(h264.Packet{Data: fmoPacket(t, v, fmoSlices(v))}); err != nil {
		t.Fatal(err)
	}
	fmoAssertPicture(t, d, -1)
	if err := d.SendPacket(h264.Packet{Data: fmoInterPacket(true)}); err != nil {
		t.Fatal(err)
	}
	fmoAssertInterPicture(t, d)
}
