package decoder

import (
	"errors"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/nal"
)

func TestDataPartition_NormativeSamples(t *testing.T) {
	for _, mode := range []string{"pcm", "i16", "inter", "mixed", "skip"} {
		t.Run(mode, func(t *testing.T) {
			d := mustDecoder(t, h264.DecoderConfig{})
			partitionReference(t, d)
			units := partitionVector(mode)
			// nal_ref_idc 的非零优先级可以不同，只有参考/非参考状态必须一致。
			for i := range units {
				units[i].Header.RefIDC = uint8(i%3 + 1)
			}
			if err := d.SendPacket(h264.Packet{Data: annex(units...)}); err != nil {
				t.Fatal(err)
			}
			partitionAssert(t, d, mode)
		})
	}
}

func TestDataPartition_InvalidAssociationsAndRecovery(t *testing.T) {
	for _, name := range []string{"missing_b", "missing_c", "duplicate_a", "duplicate_b", "duplicate_c", "orphan_b", "orphan_c", "wrong_id_b", "wrong_id_c", "nonref_b", "nonref_c", "truncated_a", "truncated_b", "truncated_c", "empty_b", "empty_c", "unexpected_b", "unexpected_c"} {
		t.Run(name, func(t *testing.T) {
			d := mustDecoder(t, h264.DecoderConfig{})
			partitionReference(t, d)
			units := partitionVector("mixed")
			prefix := func(kind nal.UnitType, id uint32, residual bool) nal.Unit {
				w := bitstream.NewWriter()
				w.WriteUE(id)
				if residual {
					w.WriteBits(4, 5)
				}
				bitstream.WriteRBSPTrailingBits(w)
				return fmoUnit(kind, w.Bytes())
			}
			switch name {
			case "missing_b":
				units = []nal.Unit{units[0], units[2]}
			case "missing_c":
				units = units[:2]
			case "duplicate_a":
				units = append(units, units[0])
			case "duplicate_b":
				units = append(units, units[1])
			case "duplicate_c":
				units = append(units, units[2])
			case "orphan_b":
				units = units[1:2]
			case "orphan_c":
				units = units[2:]
			case "wrong_id_b":
				units[1] = prefix(nal.TypeSlicePartitionB, 1, true)
			case "wrong_id_c":
				units[2] = prefix(nal.TypeSlicePartitionC, 1, true)
			case "nonref_b":
				units[1].Header.RefIDC = 0
			case "nonref_c":
				units[2].Header.RefIDC = 0
			case "truncated_a":
				units[0].Payload = units[0].Payload[:1]
			case "truncated_b":
				units = partitionVector("pcm")
				units[1].Payload = units[1].Payload[:len(units[1].Payload)/2]
			case "truncated_c":
				units[2].Payload = nil
			case "empty_b":
				units[1] = prefix(nal.TypeSlicePartitionB, 0, false)
			case "empty_c":
				units[2] = prefix(nal.TypeSlicePartitionC, 0, false)
			case "unexpected_b":
				units = append(partitionVector("skip"), prefix(nal.TypeSlicePartitionB, 0, false))
			case "unexpected_c":
				units = append(partitionVector("skip"), prefix(nal.TypeSlicePartitionC, 0, false))
			}
			if err := d.SendPacket(h264.Packet{Data: annex(units...)}); !errors.Is(err, h264.ErrInvalidData) {
				t.Fatalf("%s: got %v, want ErrInvalidData", name, err)
			}
			f, err := d.ReceiveFrame()
			if f != nil {
				f.Release()
			}
			if !errors.Is(err, h264.ErrAgain) {
				t.Fatalf("invalid partition produced frame: %v", err)
			}
			// 不 Reset、不重建 decoder：合法 IDR 恢复后再次解码完整 ABC 图像。
			partitionReference(t, d)
			if err := d.SendPacket(h264.Packet{Data: annex(partitionVector("mixed")...)}); err != nil {
				t.Fatalf("partition recovery: %v", err)
			}
			partitionAssert(t, d, "mixed")
		})
	}
}
