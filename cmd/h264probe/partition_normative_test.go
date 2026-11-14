package main

import (
	"bytes"
	"errors"
	"io"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/nal"
	"github.com/goavc/h264/syntax"
)

func partitionProbeParameters(t *testing.T) []nal.Unit {
	t.Helper()
	s := &syntax.SPS{ProfileIDC: 88, LevelIDC: 20, ChromaFormatIDC: 1, MaxNumRefFrames: 1, PicOrderCntType: 2, PicWidthInMbsMinus1: 1, PicHeightInMapUnitsMinus1: 1, FrameMbsOnlyFlag: true, Direct8x8InferenceFlag: true}
	p := &syntax.PPS{RedundantPicCntPresentFlag: true, DeblockingFilterControlPresentFlag: true, NumSliceGroupsMinus1: 1, SliceGroupMapType: 6, PicSizeInMapUnitsMinus1: 3, SliceGroupID: []uint8{0, 1, 0, 1}}
	return syntaxParameterUnits(t, s, p)
}

// 只写规范头及残差占位位，不以探测器不解残差为完整解码承诺。
func partitionProbeSlice(kind nal.UnitType, first, id, count uint32, frame uint64) nal.Unit {
	w := bitstream.NewWriter()
	if kind == nal.TypeSlicePartitionA || kind == nal.TypeSlice {
		w.WriteUE(first)
		w.WriteUE(2)
		w.WriteUE(0)
		w.WriteBits(4, frame)
		w.WriteUE(count)
		w.WriteBit(0)
		w.WriteSE(0)
		w.WriteUE(1)
	}
	if kind != nal.TypeSlice {
		w.WriteUE(id)
	}
	if kind == nal.TypeSlicePartitionB || kind == nal.TypeSlicePartitionC {
		w.WriteUE(count)
	}
	w.WriteBit(1)
	bitstream.WriteRBSPTrailingBits(w)
	return nal.Unit{Header: nal.Header{Type: kind, RefIDC: 3}, Payload: bitstream.RBSPToEBSP(w.Bytes())}
}

func TestSyntax_DataPartitionsASOAssociation(t *testing.T) {
	units := partitionProbeParameters(t)
	units = append(units,
		partitionProbeSlice(nal.TypeSlicePartitionA, 3, 1, 0, 0),
		partitionProbeSlice(nal.TypeSlicePartitionA, 0, 0, 0, 0),
		partitionProbeSlice(nal.TypeSlicePartitionB, 0, 1, 0, 0),
		partitionProbeSlice(nal.TypeSlicePartitionC, 0, 1, 0, 0),
		partitionProbeSlice(nal.TypeSlicePartitionB, 0, 0, 0, 0))
	var out bytes.Buffer
	if err := runWithInput([]string{"-syntax", "-input", "-"}, bytes.NewReader(syntaxAnnexB(units)), &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	records := syntaxRecords(t, out.Bytes())
	if len(records) != 7 || records[2].PartitionA == nil || records[2].PartitionA.Context.SliceID != 1 || records[3].SliceHeader.FirstMBInSlice != 0 || records[4].PartitionBC == nil || records[4].PartitionBC.SliceID != 1 || records[5].PartitionBC.Type != nal.TypeSlicePartitionC || records[6].PartitionBC.SliceID != 0 {
		t.Fatalf("partition JSON lost association: %+v", records)
	}
	r, err := probe(bytes.NewReader(syntaxAnnexB(units)), 1)
	if err != nil || len(r.Pictures) != 1 || r.Pictures[0].Slices != 2 || r.Counts["I"] != 1 {
		t.Fatalf("partition GOP: %+v, %v", r, err)
	}
}

func TestSyntax_DataPartitionAssociationErrors(t *testing.T) {
	a := partitionProbeSlice(nal.TypeSlicePartitionA, 0, 0, 0, 0)
	b := partitionProbeSlice(nal.TypeSlicePartitionB, 0, 0, 0, 0)
	c := partitionProbeSlice(nal.TypeSlicePartitionC, 0, 0, 0, 0)
	for _, tc := range []struct {
		name  string
		units []nal.Unit
	}{
		{"orphan_b", []nal.Unit{b}},
		{"orphan_c", []nal.Unit{c}},
		{"duplicate_a", []nal.Unit{a, a}},
		{"duplicate_b", []nal.Unit{a, b, b}},
		{"duplicate_c", []nal.Unit{a, c, c}},
		{"reverse", []nal.Unit{a, c, b}},
		{"wrong_id", []nal.Unit{a, partitionProbeSlice(nal.TypeSlicePartitionB, 0, 1, 0, 0)}},
		{"new_picture_orphan", []nal.Unit{a, partitionProbeSlice(nal.TypeSlicePartitionA, 0, 1, 0, 1), b}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			units := append(partitionProbeParameters(t), tc.units...)
			var out bytes.Buffer
			if err := runWithInput([]string{"-syntax", "-input", "-"}, bytes.NewReader(syntaxAnnexB(units)), &out, io.Discard); !errors.Is(err, h264.ErrInvalidData) {
				t.Fatalf("syntax accepted invalid association: %v", err)
			}
			if _, err := probe(bytes.NewReader(syntaxAnnexB(units)), 10); !errors.Is(err, h264.ErrInvalidData) {
				t.Fatalf("GOP accepted invalid association: %v", err)
			}
		})
	}
}

func TestProbe_ASOFMORedundantPictureIdentity(t *testing.T) {
	units := partitionProbeParameters(t)
	for _, v := range []struct {
		first, count uint32
		frame        uint64
	}{
		{3, 0, 0}, {0, 0, 0}, {3, 1, 0}, {0, 1, 0},
		{2, 1, 1}, {0, 1, 1},
	} {
		units = append(units, partitionProbeSlice(nal.TypeSlice, v.first, 0, v.count, v.frame))
	}
	r, err := probe(bytes.NewReader(syntaxAnnexB(units)), 2)
	if err != nil || len(r.Pictures) != 2 || r.Counts["I"] != 2 || r.Pictures[0].Slices != 4 || r.Pictures[1].Slices != 2 {
		t.Fatalf("ASO/FMO/redundant GOP: %+v, %v", r, err)
	}
}
