package syntax

import (
	"fmt"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
)

const (
	// aspectRatioExtendedSAR 是 aspect_ratio_idc 的 Extended_SAR 值。
	aspectRatioExtendedSAR = 255

	// maxCPBCountMinus1 是 hrd_parameters() 中 cpb_cnt_minus1 的规范上限。
	maxCPBCountMinus1 = 31
)

// CPBEntry 是 hrd_parameters() 中的一个 CPB 描述。
type CPBEntry struct {
	BitRateValueMinus1 uint32
	CPBSizeValueMinus1 uint32
	CBRFlag            bool
}

// HRDParameters 对应 hrd_parameters()（H.264 E.1.2）。
type HRDParameters struct {
	CPBCountMinus1 uint32
	BitRateScale   uint8
	CPBSizeScale   uint8

	CPB []CPBEntry

	InitialCPBRemovalDelayLengthMinus1 uint8
	CPBRemovalDelayLengthMinus1        uint8
	DPBOutputDelayLengthMinus1         uint8
	TimeOffsetLength                   uint8
}

// VUI 对应 vui_parameters()（H.264 E.1.1）。
//
// 字段与规范同名（Go 命名为导出形式），条件字段只有在对应 present_flag
// 为 true 时才参与解析/序列化。HRD 参数完整解析，但 M1 不消费它们；
// 应用 HRD 语义属于更高里程碑。
type VUI struct {
	AspectRatioInfoPresentFlag bool
	AspectRatioIDC             uint8
	SARWidth                   uint16
	SARHeight                  uint16

	OverscanInfoPresentFlag bool
	OverscanAppropriateFlag bool

	VideoSignalTypePresentFlag   bool
	VideoFormat                  uint8
	VideoFullRangeFlag           bool
	ColourDescriptionPresentFlag bool
	ColourPrimaries              uint8
	TransferCharacteristics      uint8
	MatrixCoefficients           uint8

	ChromaLocInfoPresentFlag       bool
	ChromaSampleLocTypeTopField    uint32
	ChromaSampleLocTypeBottomField uint32

	TimingInfoPresentFlag bool
	NumUnitsInTick        uint32
	TimeScale             uint32
	FixedFrameRateFlag    bool

	NALHRDParametersPresentFlag bool
	VCLHRDParametersPresentFlag bool
	NALHRD                      *HRDParameters
	VCLHRD                      *HRDParameters
	LowDelayHRDFlag             bool

	PicStructPresentFlag bool

	BitstreamRestrictionFlag           bool
	MotionVectorsOverPicBoundariesFlag bool
	MaxBytesPerPicDenom                uint32
	MaxBitsPerMBDenom                  uint32
	Log2MaxMVLengthHorizontal          uint32
	Log2MaxMVLengthVertical            uint32
	MaxNumReorderFrames                uint32
	MaxDecFrameBuffering               uint32
}

// ParseVUI 从当前读取位置解析 vui_parameters()。
func ParseVUI(r *bitstream.Reader) (*VUI, error) {
	v := &VUI{}
	var err error

	if v.AspectRatioInfoPresentFlag, err = readFlag(r, "aspect_ratio_info_present_flag"); err != nil {
		return nil, err
	}
	if v.AspectRatioInfoPresentFlag {
		idc, err := readBits(r, 8, "aspect_ratio_idc")
		if err != nil {
			return nil, err
		}
		v.AspectRatioIDC = uint8(idc)
		if v.AspectRatioIDC == aspectRatioExtendedSAR {
			w, err := readBits(r, 16, "sar_width")
			if err != nil {
				return nil, err
			}
			h, err := readBits(r, 16, "sar_height")
			if err != nil {
				return nil, err
			}
			v.SARWidth, v.SARHeight = uint16(w), uint16(h)
		}
	}

	if v.OverscanInfoPresentFlag, err = readFlag(r, "overscan_info_present_flag"); err != nil {
		return nil, err
	}
	if v.OverscanInfoPresentFlag {
		if v.OverscanAppropriateFlag, err = readFlag(r, "overscan_appropriate_flag"); err != nil {
			return nil, err
		}
	}

	if v.VideoSignalTypePresentFlag, err = readFlag(r, "video_signal_type_present_flag"); err != nil {
		return nil, err
	}
	if v.VideoSignalTypePresentFlag {
		format, err := readBits(r, 3, "video_format")
		if err != nil {
			return nil, err
		}
		v.VideoFormat = uint8(format)
		if v.VideoFullRangeFlag, err = readFlag(r, "video_full_range_flag"); err != nil {
			return nil, err
		}
		if v.ColourDescriptionPresentFlag, err = readFlag(r, "colour_description_present_flag"); err != nil {
			return nil, err
		}
		if v.ColourDescriptionPresentFlag {
			primaries, err := readBits(r, 8, "colour_primaries")
			if err != nil {
				return nil, err
			}
			transfer, err := readBits(r, 8, "transfer_characteristics")
			if err != nil {
				return nil, err
			}
			matrix, err := readBits(r, 8, "matrix_coefficients")
			if err != nil {
				return nil, err
			}
			v.ColourPrimaries = uint8(primaries)
			v.TransferCharacteristics = uint8(transfer)
			v.MatrixCoefficients = uint8(matrix)
		}
	}

	if v.ChromaLocInfoPresentFlag, err = readFlag(r, "chroma_loc_info_present_flag"); err != nil {
		return nil, err
	}
	if v.ChromaLocInfoPresentFlag {
		if v.ChromaSampleLocTypeTopField, err = readUE(r, "chroma_sample_loc_type_top_field"); err != nil {
			return nil, err
		}
		if v.ChromaSampleLocTypeBottomField, err = readUE(r, "chroma_sample_loc_type_bottom_field"); err != nil {
			return nil, err
		}
	}

	if v.TimingInfoPresentFlag, err = readFlag(r, "timing_info_present_flag"); err != nil {
		return nil, err
	}
	if v.TimingInfoPresentFlag {
		numUnits, err := readBits(r, 32, "num_units_in_tick")
		if err != nil {
			return nil, err
		}
		timeScale, err := readBits(r, 32, "time_scale")
		if err != nil {
			return nil, err
		}
		v.NumUnitsInTick = uint32(numUnits)
		v.TimeScale = uint32(timeScale)
		if v.FixedFrameRateFlag, err = readFlag(r, "fixed_frame_rate_flag"); err != nil {
			return nil, err
		}
	}

	if v.NALHRDParametersPresentFlag, err = readFlag(r, "nal_hrd_parameters_present_flag"); err != nil {
		return nil, err
	}
	if v.NALHRDParametersPresentFlag {
		if v.NALHRD, err = parseHRDParameters(r); err != nil {
			return nil, fmt.Errorf("syntax: nal hrd_parameters: %w", err)
		}
	}
	if v.VCLHRDParametersPresentFlag, err = readFlag(r, "vcl_hrd_parameters_present_flag"); err != nil {
		return nil, err
	}
	if v.VCLHRDParametersPresentFlag {
		if v.VCLHRD, err = parseHRDParameters(r); err != nil {
			return nil, fmt.Errorf("syntax: vcl hrd_parameters: %w", err)
		}
	}
	if v.NALHRDParametersPresentFlag || v.VCLHRDParametersPresentFlag {
		if v.LowDelayHRDFlag, err = readFlag(r, "low_delay_hrd_flag"); err != nil {
			return nil, err
		}
	}

	if v.PicStructPresentFlag, err = readFlag(r, "pic_struct_present_flag"); err != nil {
		return nil, err
	}

	if v.BitstreamRestrictionFlag, err = readFlag(r, "bitstream_restriction_flag"); err != nil {
		return nil, err
	}
	if v.BitstreamRestrictionFlag {
		if v.MotionVectorsOverPicBoundariesFlag, err = readFlag(r, "motion_vectors_over_pic_boundaries_flag"); err != nil {
			return nil, err
		}
		if v.MaxBytesPerPicDenom, err = readUE(r, "max_bytes_per_pic_denom"); err != nil {
			return nil, err
		}
		if v.MaxBitsPerMBDenom, err = readUE(r, "max_bits_per_mb_denom"); err != nil {
			return nil, err
		}
		if v.Log2MaxMVLengthHorizontal, err = readUE(r, "log2_max_mv_length_horizontal"); err != nil {
			return nil, err
		}
		if v.Log2MaxMVLengthVertical, err = readUE(r, "log2_max_mv_length_vertical"); err != nil {
			return nil, err
		}
		if v.MaxNumReorderFrames, err = readUE(r, "max_num_reorder_frames"); err != nil {
			return nil, err
		}
		if v.MaxDecFrameBuffering, err = readUE(r, "max_dec_frame_buffering"); err != nil {
			return nil, err
		}
	}
	return v, nil
}

// parseHRDParameters 解析 hrd_parameters()（H.264 E.1.2）。
func parseHRDParameters(r *bitstream.Reader) (*HRDParameters, error) {
	h := &HRDParameters{}
	var err error
	if h.CPBCountMinus1, err = readUE(r, "cpb_cnt_minus1"); err != nil {
		return nil, err
	}
	if h.CPBCountMinus1 > maxCPBCountMinus1 {
		return nil, fmt.Errorf("syntax: cpb_cnt_minus1 = %d exceeds %d: %w",
			h.CPBCountMinus1, maxCPBCountMinus1, h264.ErrInvalidData)
	}
	bitRateScale, err := readBits(r, 4, "bit_rate_scale")
	if err != nil {
		return nil, err
	}
	cpbSizeScale, err := readBits(r, 4, "cpb_size_scale")
	if err != nil {
		return nil, err
	}
	h.BitRateScale, h.CPBSizeScale = uint8(bitRateScale), uint8(cpbSizeScale)

	h.CPB = make([]CPBEntry, 0, h.CPBCountMinus1+1)
	for i := uint32(0); i <= h.CPBCountMinus1; i++ {
		var entry CPBEntry
		if entry.BitRateValueMinus1, err = readUE(r, "bit_rate_value_minus1"); err != nil {
			return nil, err
		}
		if entry.CPBSizeValueMinus1, err = readUE(r, "cpb_size_value_minus1"); err != nil {
			return nil, err
		}
		if entry.CBRFlag, err = readFlag(r, "cbr_flag"); err != nil {
			return nil, err
		}
		h.CPB = append(h.CPB, entry)
	}

	initial, err := readBits(r, 5, "initial_cpb_removal_delay_length_minus1")
	if err != nil {
		return nil, err
	}
	cpbDelay, err := readBits(r, 5, "cpb_removal_delay_length_minus1")
	if err != nil {
		return nil, err
	}
	dpbDelay, err := readBits(r, 5, "dpb_output_delay_length_minus1")
	if err != nil {
		return nil, err
	}
	timeOffset, err := readBits(r, 5, "time_offset_length")
	if err != nil {
		return nil, err
	}
	h.InitialCPBRemovalDelayLengthMinus1 = uint8(initial)
	h.CPBRemovalDelayLengthMinus1 = uint8(cpbDelay)
	h.DPBOutputDelayLengthMinus1 = uint8(dpbDelay)
	h.TimeOffsetLength = uint8(timeOffset)
	return h, nil
}

// WriteTo 写出 vui_parameters()。
func (v *VUI) WriteTo(w *bitstream.Writer) error {
	if v == nil {
		return fmt.Errorf("syntax: nil vui")
	}
	var err error
	if err = writeFlag(w, v.AspectRatioInfoPresentFlag); err != nil {
		return err
	}
	if v.AspectRatioInfoPresentFlag {
		if err = writeBits(w, 8, uint64(v.AspectRatioIDC), "aspect_ratio_idc"); err != nil {
			return err
		}
		if v.AspectRatioIDC == aspectRatioExtendedSAR {
			if err = writeBits(w, 16, uint64(v.SARWidth), "sar_width"); err != nil {
				return err
			}
			if err = writeBits(w, 16, uint64(v.SARHeight), "sar_height"); err != nil {
				return err
			}
		}
	}
	if err = writeFlag(w, v.OverscanInfoPresentFlag); err != nil {
		return err
	}
	if v.OverscanInfoPresentFlag {
		if err = writeFlag(w, v.OverscanAppropriateFlag); err != nil {
			return err
		}
	}
	if err = writeFlag(w, v.VideoSignalTypePresentFlag); err != nil {
		return err
	}
	if v.VideoSignalTypePresentFlag {
		if err = writeBits(w, 3, uint64(v.VideoFormat), "video_format"); err != nil {
			return err
		}
		if err = writeFlag(w, v.VideoFullRangeFlag); err != nil {
			return err
		}
		if err = writeFlag(w, v.ColourDescriptionPresentFlag); err != nil {
			return err
		}
		if v.ColourDescriptionPresentFlag {
			if err = writeBits(w, 8, uint64(v.ColourPrimaries), "colour_primaries"); err != nil {
				return err
			}
			if err = writeBits(w, 8, uint64(v.TransferCharacteristics), "transfer_characteristics"); err != nil {
				return err
			}
			if err = writeBits(w, 8, uint64(v.MatrixCoefficients), "matrix_coefficients"); err != nil {
				return err
			}
		}
	}
	if err = writeFlag(w, v.ChromaLocInfoPresentFlag); err != nil {
		return err
	}
	if v.ChromaLocInfoPresentFlag {
		if err = writeUE(w, v.ChromaSampleLocTypeTopField, "chroma_sample_loc_type_top_field"); err != nil {
			return err
		}
		if err = writeUE(w, v.ChromaSampleLocTypeBottomField, "chroma_sample_loc_type_bottom_field"); err != nil {
			return err
		}
	}
	if err = writeFlag(w, v.TimingInfoPresentFlag); err != nil {
		return err
	}
	if v.TimingInfoPresentFlag {
		if err = writeBits(w, 32, uint64(v.NumUnitsInTick), "num_units_in_tick"); err != nil {
			return err
		}
		if err = writeBits(w, 32, uint64(v.TimeScale), "time_scale"); err != nil {
			return err
		}
		if err = writeFlag(w, v.FixedFrameRateFlag); err != nil {
			return err
		}
	}
	if err = writeFlag(w, v.NALHRDParametersPresentFlag); err != nil {
		return err
	}
	if v.NALHRDParametersPresentFlag {
		if v.NALHRD == nil {
			return fmt.Errorf("syntax: nal_hrd_parameters_present_flag set but NALHRD is nil")
		}
		if err = writeHRDParameters(w, v.NALHRD); err != nil {
			return fmt.Errorf("syntax: nal hrd_parameters: %w", err)
		}
	}
	if err = writeFlag(w, v.VCLHRDParametersPresentFlag); err != nil {
		return err
	}
	if v.VCLHRDParametersPresentFlag {
		if v.VCLHRD == nil {
			return fmt.Errorf("syntax: vcl_hrd_parameters_present_flag set but VCLHRD is nil")
		}
		if err = writeHRDParameters(w, v.VCLHRD); err != nil {
			return fmt.Errorf("syntax: vcl hrd_parameters: %w", err)
		}
	}
	if v.NALHRDParametersPresentFlag || v.VCLHRDParametersPresentFlag {
		if err = writeFlag(w, v.LowDelayHRDFlag); err != nil {
			return err
		}
	}
	if err = writeFlag(w, v.PicStructPresentFlag); err != nil {
		return err
	}
	if err = writeFlag(w, v.BitstreamRestrictionFlag); err != nil {
		return err
	}
	if v.BitstreamRestrictionFlag {
		if err = writeFlag(w, v.MotionVectorsOverPicBoundariesFlag); err != nil {
			return err
		}
		if err = writeUE(w, v.MaxBytesPerPicDenom, "max_bytes_per_pic_denom"); err != nil {
			return err
		}
		if err = writeUE(w, v.MaxBitsPerMBDenom, "max_bits_per_mb_denom"); err != nil {
			return err
		}
		if err = writeUE(w, v.Log2MaxMVLengthHorizontal, "log2_max_mv_length_horizontal"); err != nil {
			return err
		}
		if err = writeUE(w, v.Log2MaxMVLengthVertical, "log2_max_mv_length_vertical"); err != nil {
			return err
		}
		if err = writeUE(w, v.MaxNumReorderFrames, "max_num_reorder_frames"); err != nil {
			return err
		}
		if err = writeUE(w, v.MaxDecFrameBuffering, "max_dec_frame_buffering"); err != nil {
			return err
		}
	}
	return nil
}

// writeHRDParameters 写出 hrd_parameters()。
func writeHRDParameters(w *bitstream.Writer, h *HRDParameters) error {
	if h.CPBCountMinus1 > maxCPBCountMinus1 {
		return fmt.Errorf("cpb_cnt_minus1 = %d exceeds %d", h.CPBCountMinus1, maxCPBCountMinus1)
	}
	if len(h.CPB) != int(h.CPBCountMinus1)+1 {
		return fmt.Errorf("cpb_cnt_minus1 = %d requires %d entries, got %d",
			h.CPBCountMinus1, h.CPBCountMinus1+1, len(h.CPB))
	}
	if err := writeUE(w, h.CPBCountMinus1, "cpb_cnt_minus1"); err != nil {
		return err
	}
	if err := writeBits(w, 4, uint64(h.BitRateScale), "bit_rate_scale"); err != nil {
		return err
	}
	if err := writeBits(w, 4, uint64(h.CPBSizeScale), "cpb_size_scale"); err != nil {
		return err
	}
	for i, e := range h.CPB {
		if err := writeUE(w, e.BitRateValueMinus1, "bit_rate_value_minus1"); err != nil {
			return fmt.Errorf("entry %d: %w", i, err)
		}
		if err := writeUE(w, e.CPBSizeValueMinus1, "cpb_size_value_minus1"); err != nil {
			return fmt.Errorf("entry %d: %w", i, err)
		}
		if err := writeFlag(w, e.CBRFlag); err != nil {
			return fmt.Errorf("entry %d: %w", i, err)
		}
	}
	if err := writeBits(w, 5, uint64(h.InitialCPBRemovalDelayLengthMinus1), "initial_cpb_removal_delay_length_minus1"); err != nil {
		return err
	}
	if err := writeBits(w, 5, uint64(h.CPBRemovalDelayLengthMinus1), "cpb_removal_delay_length_minus1"); err != nil {
		return err
	}
	if err := writeBits(w, 5, uint64(h.DPBOutputDelayLengthMinus1), "dpb_output_delay_length_minus1"); err != nil {
		return err
	}
	if err := writeBits(w, 5, uint64(h.TimeOffsetLength), "time_offset_length"); err != nil {
		return err
	}
	return nil
}
