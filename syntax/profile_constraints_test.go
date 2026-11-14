package syntax

import (
	"errors"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
)

func profileTestSPS(profile uint8) *SPS {
	return &SPS{ProfileIDC: profile, LevelIDC: 30, ChromaFormatIDC: 1, FrameMbsOnlyFlag: true, Direct8x8InferenceFlag: true}
}

func TestProfileConstraints_ParameterToolMatrix(t *testing.T) {
	for _, profile := range []uint8{66, 77, 88, 100, 110, 122, 244} {
		for _, feature := range []string{"cabac", "weighted", "fmo", "redundant", "optional"} {
			s, p := profileTestSPS(profile), &PPS{}
			valid := true
			switch feature {
			case "cabac":
				p.EntropyCodingModeFlag = true
				valid = profile != 66 && profile != 88
			case "weighted":
				p.WeightedPredFlag, p.WeightedBipredIDC = true, 2
				valid = profile != 66
			case "fmo":
				s.PicWidthInMbsMinus1 = 1
				p.NumSliceGroupsMinus1, p.SliceGroupMapType = 1, 1
				valid = profile == 66 || profile == 88
			case "redundant":
				p.RedundantPicCntPresentFlag = true
				valid = profile == 66 || profile == 88
			case "optional":
				p.HasOptionalBlock = true // 即使扩展内全是默认值，也不能出现在旧profile。
				valid = profile >= 100
			}
			err := validateProfilePPS(s, p)
			if valid && err != nil || !valid && !errors.Is(err, h264.ErrInvalidData) {
				t.Fatalf("profile%d feature%s valid%v: %v", profile, feature, valid, err)
			}
			if !valid {
				w := bitstream.NewWriter()
				w.WriteBits(3, 5)
				if err := p.WriteTo(w, s); !errors.Is(err, h264.ErrInvalidData) || w.BitsWritten() != 3 {
					t.Fatalf("profile%d feature%s写前未拒绝: bits%d err%v", profile, feature, w.BitsWritten(), err)
				}
			}
		}
	}
}

func TestProfileConstraints_LegacyConstraintFlagsAreCumulative(t *testing.T) {
	s := profileTestSPS(88)
	s.ConstraintSetFlags[0] = true
	if err := validateProfileSlice(s, &PPS{}, &SliceHeader{Type: SliceTypeSP}, false); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("Extended+Baseline声明不能再使用SP: %v", err)
	}
	if err := validateProfilePPS(s, &PPS{WeightedPredFlag: true}); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("Extended+Baseline声明不能再使用加权: %v", err)
	}
	s.ConstraintSetFlags[0], s.ConstraintSetFlags[1] = false, true
	if err := validateProfilePPS(s, &PPS{RedundantPicCntPresentFlag: true}); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("Extended+Main声明不能再使用冗余: %v", err)
	}
	s = profileTestSPS(66)
	s.ConstraintSetFlags[1] = true
	if err := validateProfilePPS(s, &PPS{NumSliceGroupsMinus1: 1}); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("Constrained Baseline不允许FMO: %v", err)
	}
	s = profileTestSPS(77)
	s.ConstraintSetFlags[2] = true
	if err := validateProfilePPS(s, &PPS{EntropyCodingModeFlag: true}); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("Main+Extended声明不允许CABAC: %v", err)
	}
}

func TestProfileConstraints_SliceAndPartitionMatrix(t *testing.T) {
	for _, profile := range []uint8{66, 77, 88, 100, 110, 122, 244} {
		for typ := range SliceTypeSI + 1 {
			for _, partitioned := range []bool{false, true} {
				h := &SliceHeader{Type: typ}
				valid := (profile != 66 || typ == SliceTypeI || typ == SliceTypeP) &&
					(profile == 88 || typ != SliceTypeSP && typ != SliceTypeSI) && (!partitioned || profile == 88)
				err := validateProfileSlice(profileTestSPS(profile), &PPS{}, h, partitioned)
				if valid && err != nil || !valid && !errors.Is(err, h264.ErrInvalidData) {
					t.Fatalf("profile%d type%d partition%v valid%v: %v", profile, typ, partitioned, valid, err)
				}
			}
		}
	}
	if err := validateProfileSlice(profileTestSPS(88), &PPS{}, &SliceHeader{Type: SliceTypeI, IDR: true}, true); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("IDR不能使用分区: %v", err)
	}
	for _, profile := range []uint8{77, 88, 100} {
		s := profileTestSPS(profile)
		s.ConstraintSetFlags[5] = true
		if err := validateProfileSlice(s, &PPS{}, &SliceHeader{Type: SliceTypeB}, false); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("profile%d的constraint_set5未拒绝B: %v", profile, err)
		}
	}
	for _, profile := range []uint8{100, 110, 122, 244} {
		s := profileTestSPS(profile)
		s.ConstraintSetFlags[3] = true
		if err := validateProfileSlice(s, &PPS{}, &SliceHeader{Type: SliceTypeI}, false); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("Intra profile%d要求IDR: %v", profile, err)
		}
		if err := validateProfileSlice(s, &PPS{}, &SliceHeader{Type: SliceTypeI, IDR: true}, false); err != nil {
			t.Fatalf("合法Intra IDR被拒绝: %v", err)
		}
	}
}

func TestProfileConstraints_LegalUnsupportedIsNotInvalid(t *testing.T) {
	for _, tc := range []struct {
		profile       uint8
		chroma, depth uint32
		bypass        bool
	}{
		{100, 0, 0, false}, // High单色合法，当前流水线未支持。
		{110, 1, 1, false}, // 9-bit合法，不得误判High10只允许10-bit。
		{244, 3, 4, false}, // 12/14-bit与bypass是High444合法工具。
		{244, 3, 6, true},
	} {
		s := profileTestSPS(tc.profile)
		s.ChromaFormatIDC, s.BitDepthLumaMinus8, s.BitDepthChromaMinus8 = tc.chroma, tc.depth, tc.depth
		s.QPPPrimeYZeroTransformBypassFlag = tc.bypass
		if err := validateProfileSPS(s); err != nil {
			t.Fatalf("合法组合被归为非法: %+v %v", tc, err)
		}
		if err := s.validateSupported(); !errors.Is(err, h264.ErrUnsupported) {
			t.Fatalf("未支持合法组合应为Unsupported: %+v %v", tc, err)
		}
	}
	// 冗余与场编码的支持边界不属于规范合法性helper。
	s := profileTestSPS(88)
	s.FrameMbsOnlyFlag = false
	if err := validateProfileSlice(s, &PPS{RedundantPicCntPresentFlag: true}, &SliceHeader{Type: SliceTypeSP, RedundantPicCnt: 1, FieldPicFlag: true}, false); err != nil {
		t.Fatalf("合法但流水线未支持的Extended组合: %v", err)
	}
	// 低层语法可只提供局部SPS；不把未声明的profile0伪装成已知profile。
	if err := validateProfileSlice(&SPS{}, &PPS{EntropyCodingModeFlag: true, NumSliceGroupsMinus1: 1}, &SliceHeader{Type: SliceTypeSI}, true); err != nil {
		t.Fatalf("局部profile上下文不应触发Annex A判断: %v", err)
	}
}

func TestProfileConstraints_InvalidSPSFailsBeforeOutput(t *testing.T) {
	for _, tc := range []struct {
		profile         uint8
		chroma, depth   uint32
		bypass, scaling bool
	}{
		{66, 1, 0, false, true},
		{88, 1, 2, false, false},
		{100, 2, 0, false, false},
		{100, 1, 2, false, false},
		{110, 1, 3, false, false},
		{122, 3, 0, false, false},
		{110, 1, 0, true, false},
	} {
		s := profileTestSPS(tc.profile)
		s.ChromaFormatIDC, s.BitDepthLumaMinus8, s.BitDepthChromaMinus8 = tc.chroma, tc.depth, tc.depth
		s.QPPPrimeYZeroTransformBypassFlag, s.SeqScalingMatrixPresentFlag = tc.bypass, tc.scaling
		w := bitstream.NewWriter()
		w.WriteBits(3, 5)
		if err := s.WriteTo(w); !errors.Is(err, h264.ErrInvalidData) || w.BitsWritten() != 3 {
			t.Fatalf("非法SPS配置未写前拒绝: %+v bits%d err%v", tc, w.BitsWritten(), err)
		}
	}
}

func profilePPSRBSP(entropy, redundant, optional bool) []byte {
	w := bitstream.NewWriter()
	w.WriteUE(0)
	w.WriteUE(0)
	flag := func(v bool) {
		if v {
			w.WriteBit(1)
		} else {
			w.WriteBit(0)
		}
	}
	flag(entropy)
	w.WriteBit(0)
	w.WriteUE(0)
	w.WriteUE(0)
	w.WriteUE(0)
	w.WriteBit(0)
	w.WriteBits(2, 0)
	w.WriteSE(0)
	w.WriteSE(0)
	w.WriteSE(0)
	w.WriteBit(0)
	w.WriteBit(0)
	flag(redundant)
	if optional {
		w.WriteBit(0)
		w.WriteBit(0)
		w.WriteSE(0)
	}
	bitstream.WriteRBSPTrailingBits(w)
	return w.Bytes()
}

func TestProfileConstraints_ParsePPSRejectsIllegalTools(t *testing.T) {
	for _, tc := range []struct {
		profile                      uint8
		entropy, redundant, optional bool
	}{
		{66, true, false, false},
		{88, true, false, false},
		{77, false, true, false},
		{100, false, true, false},
		{88, false, false, true},
	} {
		if _, err := ParsePPS(profilePPSRBSP(tc.entropy, tc.redundant, tc.optional), profileTestSPS(tc.profile)); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("profile%d非法PPS解析未拒绝: %v", tc.profile, err)
		}
	}
	for _, profile := range []uint8{66, 88} {
		if _, err := ParsePPS(profilePPSRBSP(false, true, false), profileTestSPS(profile)); err != nil {
			t.Fatalf("profile%d合法冗余声明被拒绝: %v", profile, err)
		}
	}
}

func profileSliceRBSP(typ SliceType) []byte {
	w := bitstream.NewWriter()
	w.WriteUE(0)
	w.WriteUE(uint32(typ))
	w.WriteUE(0)
	w.WriteBits(4, 0)
	if typ == SliceTypeB {
		w.WriteBit(0)
	}
	if typ == SliceTypeP || typ == SliceTypeSP || typ == SliceTypeB {
		w.WriteBit(0)
	}
	if typ != SliceTypeI && typ != SliceTypeSI {
		w.WriteBit(0)
		if typ == SliceTypeB {
			w.WriteBit(0)
		}
	}
	w.WriteSE(0)
	if typ == SliceTypeSP {
		w.WriteBit(0)
	}
	if typ == SliceTypeSP || typ == SliceTypeSI {
		w.WriteSE(0)
	}
	bitstream.WriteRBSPTrailingBits(w)
	return w.Bytes()
}

func TestProfileConstraints_ParseSliceChecksDeclaredProfile(t *testing.T) {
	for _, tc := range []struct {
		profile uint8
		typ     SliceType
	}{
		{66, SliceTypeB},
		{66, SliceTypeSP},
		{77, SliceTypeSI},
		{100, SliceTypeSP},
		{244, SliceTypeSI},
	} {
		s := profileTestSPS(tc.profile)
		s.PicOrderCntType = 2
		if _, err := ParseSliceHeaderEx(profileSliceRBSP(tc.typ), s, map[uint32]*PPS{0: {}}, false, 0); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("profile%d slice%d未按声明拒绝: %v", tc.profile, tc.typ, err)
		}
	}
	for _, typ := range []SliceType{SliceTypeB, SliceTypeSP, SliceTypeSI} {
		s := profileTestSPS(88)
		s.PicOrderCntType = 2
		if _, err := ParseSliceHeaderEx(profileSliceRBSP(typ), s, map[uint32]*PPS{0: {}}, false, 0); err != nil {
			t.Fatalf("Extended合法slice%d被拒绝: %v", typ, err)
		}
	}
}
