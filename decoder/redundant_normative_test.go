package decoder

import (
	"errors"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/nal"
)

func TestRedundant_NormativeRepresentationSelection(t *testing.T) {
	for _, name := range []string{"primary_preferred", "redundant_only", "lowest_complete_count", "incomplete_lower_count", "partial_primary", "compressed_prediction"} {
		t.Run(name, func(t *testing.T) {
			d := mustDecoder(t, h264.DecoderConfig{})
			redundantInit(t, d)
			makeSlice := func(count uint32, first, length, bias int, predict bool) nal.Unit {
				return redundantSlice(redundantSliceVector{count: count, first: first, length: length, bias: bias, frame: 1, poc: 2, predict: predict}, false)
			}
			var units []nal.Unit
			want, predict := 31, false
			switch name {
			case "primary_preferred":
				// 不同表示允许覆盖同一地址，绝不能当作重复宏块拒绝。
				units = []nal.Unit{makeSlice(0, 0, 8, 31, false), makeSlice(0, 8, 8, 31, false), makeSlice(1, 0, 16, 77, false)}
			case "redundant_only":
				units = []nal.Unit{makeSlice(2, 8, 8, 31, false), makeSlice(2, 0, 8, 31, false)}
			case "lowest_complete_count":
				units = []nal.Unit{makeSlice(1, 0, 16, 31, false), makeSlice(2, 0, 16, 103, false), makeSlice(3, 0, 16, 77, false)}
			case "incomplete_lower_count":
				units = []nal.Unit{makeSlice(1, 0, 8, 77, false), makeSlice(2, 0, 16, 31, false)}
			case "partial_primary":
				units = []nal.Unit{makeSlice(0, 0, 8, 77, false), makeSlice(1, 0, 16, 31, false)}
			case "compressed_prediction":
				want, predict = 0, true
				units = []nal.Unit{makeSlice(2, 0, 16, 0, true)}
			}
			if err := d.SendPacket(h264.Packet{Data: annex(units...)}); err != nil {
				t.Fatal(err)
			}
			redundantAssert(t, d, want, predict)
			redundantNoFrame(t, d)
			if err := d.Flush(); err != nil {
				t.Fatal(err)
			}
			f, err := d.ReceiveFrame()
			if f != nil {
				f.Release()
			}
			if !errors.Is(err, h264.ErrEOF) {
				t.Fatalf("redundant representation emitted twice at drain: %v", err)
			}
		})
	}
}

func TestRedundant_DifferentPictureIdentities(t *testing.T) {
	d := mustDecoder(t, h264.DecoderConfig{})
	redundantInit(t, d)
	// 同一 SendPacket 可以包含多张图片，frame_num/POC 变化不是旧图的关联错误。
	units := []nal.Unit{
		redundantSlice(redundantSliceVector{count: 1, length: 16, bias: 37, frame: 1, poc: 2}, false),
		redundantSlice(redundantSliceVector{count: 1, length: 16, bias: 83, frame: 2, poc: 4}, false),
	}
	if err := d.SendPacket(h264.Packet{Data: annex(units...)}); err != nil {
		t.Fatal(err)
	}
	redundantAssert(t, d, 37, false)
	redundantAssert(t, d, 83, false)
	redundantNoFrame(t, d)
}

func TestRedundant_InvalidCoverageAndRecovery(t *testing.T) {
	for _, name := range []string{"duplicate", "overlap", "missing", "different_counts_cannot_fill_holes", "unknown_pps", "count_out_of_range", "decreasing_count"} {
		t.Run(name, func(t *testing.T) {
			d := mustDecoder(t, h264.DecoderConfig{})
			redundantInit(t, d)
			v := redundantSliceVector{count: 1, length: 16, frame: 1, poc: 2}
			var units []nal.Unit
			switch name {
			case "duplicate":
				units = []nal.Unit{redundantSlice(v, false), redundantSlice(v, false)}
			case "overlap":
				units = append(units, redundantSlice(v, false))
				v.first, v.length = 8, 8
				units = append(units, redundantSlice(v, false))
			case "missing":
				v.length = 8
				units = append(units, redundantSlice(v, false))
			case "different_counts_cannot_fill_holes":
				v.length = 8
				units = append(units, redundantSlice(v, false))
				v.count, v.first = 2, 8
				units = append(units, redundantSlice(v, false))
			case "unknown_pps":
				v.pps = 1
				units = append(units, redundantSlice(v, false))
			case "decreasing_count":
				v.count = 2
				units = append(units, redundantSlice(v, false))
				v.count = 1
				units = append(units, redundantSlice(v, false))
			case "count_out_of_range":
				v.count = 128
				units = append(units, redundantSlice(v, false))
			}
			if err := d.SendPacket(h264.Packet{Data: annex(units...)}); !errors.Is(err, h264.ErrInvalidData) {
				t.Fatalf("%s: got %v, want ErrInvalidData", name, err)
			}
			redundantNoFrame(t, d)
			redundantInit(t, d)
			v = redundantSliceVector{count: 1, length: 16, bias: 59, frame: 1, poc: 2}
			if err := d.SendPacket(h264.Packet{Data: annex(redundantSlice(v, false))}); err != nil {
				t.Fatalf("redundant recovery: %v", err)
			}
			redundantAssert(t, d, 59, false)
			redundantNoFrame(t, d)
		})
	}
}
