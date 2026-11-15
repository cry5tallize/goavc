package syntax

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	h264 "github.com/cry5tallize/goavc"
)

// fmoLiteralRBSP 仅打包字面位串，不调用任何被测语法写出函数。
func fmoLiteralRBSP(t *testing.T, text string) []byte {
	t.Helper()
	data := make([]byte, (len(text)+7)/8)
	for i, c := range text {
		switch c {
		case '0':
		case '1':
			data[i/8] |= 1 << (7 - uint(i)%8)
		default:
			t.Fatalf("invalid literal bit %q", c)
		}
	}
	return data
}

func TestFMOPPSLiteralSyntax(t *testing.T) {
	// 公共前缀：PPS 0、SPS 0、entropy=0、bottom_field=0、两组。
	const prefix = "1100010"
	// 公共后缀：两个默认参考数为0、weighted=0、bipred=0、三个QP偏移0、
	// deblocking/constrained/redundant均为0，最后是RBSP停止位。
	const suffix = "110001110001"
	for _, tc := range []struct {
		name, syntax string
		pps          PPS
	}{
		{"interleaved", "1" + "1" + "010", PPS{NumSliceGroupsMinus1: 1, RunLengthMinus1: []uint32{0, 1}}},
		{"dispersed", "010", PPS{NumSliceGroupsMinus1: 1, SliceGroupMapType: 1}},
		{"foreground", "011" + "010" + "00111", PPS{NumSliceGroupsMinus1: 1, SliceGroupMapType: 2, TopLeft: []uint32{1}, BottomRight: []uint32{6}}},
		{"box-out", "00100" + "1" + "00101", PPS{NumSliceGroupsMinus1: 1, SliceGroupMapType: 3, SliceGroupChangeDirectionFlag: true, SliceGroupChangeRateMinus1: 4}},
		{"raster", "00101" + "0" + "010", PPS{NumSliceGroupsMinus1: 1, SliceGroupMapType: 4, SliceGroupChangeRateMinus1: 1}},
		{"wipe", "00110" + "1" + "1", PPS{NumSliceGroupsMinus1: 1, SliceGroupMapType: 5, SliceGroupChangeDirectionFlag: true}},
		{"explicit", "00111" + "0001100" + "011010010110", PPS{NumSliceGroupsMinus1: 1, SliceGroupMapType: 6, PicSizeInMapUnitsMinus1: 11, SliceGroupID: []uint8{0, 1, 1, 0, 1, 0, 0, 1, 0, 1, 1, 0}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := fmoLiteralRBSP(t, prefix+tc.syntax+suffix)
			parsed, err := ParsePPS(data, fmoTestSPS())
			if err != nil || !reflect.DeepEqual(parsed, &tc.pps) {
				t.Fatalf("parsed=%+v err=%v want=%+v", parsed, err, tc.pps)
			}
			written, err := tc.pps.MarshalRBSP(fmoTestSPS())
			if err != nil || !bytes.Equal(written, data) {
				t.Fatalf("written=%x err=%v want=%x", written, err, data)
			}
			// 对每种语法逐字节截断，不能退回 Unsupported 或吞掉缺失尾位。
			for n := range len(data) {
				if _, err := ParsePPS(data[:n], fmoTestSPS()); !errors.Is(err, h264.ErrInvalidData) {
					t.Fatalf("truncated at %d err=%v", n, err)
				}
			}
		})
	}
}
