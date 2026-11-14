package syntax

import (
	"fmt"
	"math"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
)

// CPBEntry 是 hrd_parameters() 中的一个 CPB 描述。
// 两个 minus1 字段的规范范围均为 0..2^32-2（E.2.2）。
type CPBEntry struct {
	BitRateValueMinus1 uint32
	CPBSizeValueMinus1 uint32
	CBRFlag            bool
}

// HRDParameters 对应 hrd_parameters()（H.264 E.1.2）。
// 码率单位为 bit/s，CPB 容量单位为 bit，均通过 uint64 安全推导。
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

// NewFixedHRDParameters 生成只有一个调度方案的固定 HRD 参数。
// bitRate 为 bit/s，cpbSize 为 bit；仅接受规范能够精确表示的正整数，
// 不会对粒度、范围或尺度作隐式舍入。错误包装 h264.ErrInvalidData。
// 三种延时长度默认均为 24 位，time_offset 为 24 位；调用方可在写出前
// 按配套 SEI 的实际需求调整它们，写出仍经过同一 Validate 校验。
func NewFixedHRDParameters(bitRate, cpbSize uint64, cbr bool) (*HRDParameters, error) {
	rateMinus1, rateScale, err := exactHRDValue(bitRate, 6, "bit_rate")
	if err != nil {
		return nil, err
	}
	sizeMinus1, sizeScale, err := exactHRDValue(cpbSize, 4, "cpb_size")
	if err != nil {
		return nil, err
	}
	h := &HRDParameters{
		BitRateScale: rateScale,
		CPBSizeScale: sizeScale,
		CPB: []CPBEntry{{
			BitRateValueMinus1: rateMinus1,
			CPBSizeValueMinus1: sizeMinus1,
			CBRFlag:            cbr,
		}},
		InitialCPBRemovalDelayLengthMinus1: 23,
		CPBRemovalDelayLengthMinus1:        23,
		DPBOutputDelayLengthMinus1:         23,
		TimeOffsetLength:                   24,
	}
	if err := h.Validate(); err != nil {
		return nil, err
	}
	return h, nil
}

// exactHRDValue 优先选择可精确表示该值的最小尺度。
func exactHRDValue(value uint64, base uint, name string) (uint32, uint8, error) {
	for scale := range uint8(16) {
		unit := uint64(1) << (base + uint(scale))
		if value != 0 && value%unit == 0 {
			units := value / unit
			if units <= math.MaxUint32 {
				return uint32(units - 1), scale, nil
			}
		}
	}
	return 0, 0, fmt.Errorf("syntax: %s = %d is not exactly representable by HRD: %w", name, value, h264.ErrInvalidData)
}

// Validate 校验 HRD 的结构、字段范围与 E.2.2 语义。
// 与 SPS 的 profile/level 有关的码率、CPB 上限由 SPS 上下文另行校验。
func (h *HRDParameters) Validate() error {
	if h == nil {
		return fmt.Errorf("syntax: nil hrd_parameters: %w", h264.ErrInvalidData)
	}
	if err := validateHRDCount(h.CPBCountMinus1); err != nil {
		return err
	}
	if len(h.CPB) != int(h.CPBCountMinus1)+1 {
		return fmt.Errorf("syntax: cpb_cnt_minus1 = %d requires %d entries, got %d: %w",
			h.CPBCountMinus1, h.CPBCountMinus1+1, len(h.CPB), h264.ErrInvalidData)
	}
	if h.BitRateScale > 15 || h.CPBSizeScale > 15 {
		return fmt.Errorf("syntax: HRD scales %d/%d exceed 15: %w", h.BitRateScale, h.CPBSizeScale, h264.ErrInvalidData)
	}
	if h.InitialCPBRemovalDelayLengthMinus1 > 31 || h.CPBRemovalDelayLengthMinus1 > 31 ||
		h.DPBOutputDelayLengthMinus1 > 31 || h.TimeOffsetLength > 31 {
		return fmt.Errorf("syntax: HRD delay length field exceeds 31: %w", h264.ErrInvalidData)
	}
	for i, e := range h.CPB {
		if e.BitRateValueMinus1 == math.MaxUint32 || e.CPBSizeValueMinus1 == math.MaxUint32 {
			return fmt.Errorf("syntax: CPB entry %d minus1 value exceeds 2^32-2: %w", i, h264.ErrInvalidData)
		}
		if i > 0 {
			previous := h.CPB[i-1]
			if e.BitRateValueMinus1 <= previous.BitRateValueMinus1 {
				return fmt.Errorf("syntax: CPB entry %d bit_rate_value_minus1 must increase: %w", i, h264.ErrInvalidData)
			}
			if e.CPBSizeValueMinus1 > previous.CPBSizeValueMinus1 {
				return fmt.Errorf("syntax: CPB entry %d cpb_size_value_minus1 must not increase: %w", i, h264.ErrInvalidData)
			}
		}
	}
	return nil
}

func validateHRDCount(count uint32) error {
	if count > maxCPBCountMinus1 {
		return fmt.Errorf("syntax: cpb_cnt_minus1 = %d exceeds %d: %w", count, maxCPBCountMinus1, h264.ErrInvalidData)
	}
	return nil
}

// BitRate 返回指定调度方案的码率（bit/s）。
// 在 uint64 中先加一再移位，避免 uint32 的 minus1+1 回绕。
func (h *HRDParameters) BitRate(index int) (uint64, error) {
	if err := h.Validate(); err != nil {
		return 0, err
	}
	if index < 0 || index >= len(h.CPB) {
		return 0, fmt.Errorf("syntax: CPB index %d out of range: %w", index, h264.ErrInvalidData)
	}
	return hrdValue(h.CPB[index].BitRateValueMinus1, 6+h.BitRateScale), nil
}

// CPBSize 返回指定调度方案的 CPB 容量（bit）。
func (h *HRDParameters) CPBSize(index int) (uint64, error) {
	if err := h.Validate(); err != nil {
		return 0, err
	}
	if index < 0 || index >= len(h.CPB) {
		return 0, fmt.Errorf("syntax: CPB index %d out of range: %w", index, h264.ErrInvalidData)
	}
	return hrdValue(h.CPB[index].CPBSizeValueMinus1, 4+h.CPBSizeScale), nil
}

// hrdValue 只用于已校验的字段，先提升至 uint64 再加一，统一派生公式。
func hrdValue(minus1 uint32, shift uint8) uint64 {
	return (uint64(minus1) + 1) << shift
}

// ParseHRDParameters 从当前位置解析 hrd_parameters() 并校验其语义。
// 不消费 rbsp_trailing_bits，因为 HRD 通常嵌在 VUI 中。
func ParseHRDParameters(r *bitstream.Reader) (*HRDParameters, error) {
	h, err := parseHRDParameters(r)
	if err != nil {
		return nil, err
	}
	if err := h.Validate(); err != nil {
		return nil, err
	}
	return h, nil
}

// parseHRDParameters 读取有界结构；独立入口和外层 VUI 各自只做一次语义校验。
func parseHRDParameters(r *bitstream.Reader) (*HRDParameters, error) {
	if r == nil {
		return nil, fmt.Errorf("syntax: nil HRD reader: %w", h264.ErrInvalidData)
	}
	h := &HRDParameters{}
	var err error
	if h.CPBCountMinus1, err = readUE(r, "cpb_cnt_minus1"); err != nil {
		return nil, err
	}
	// 必须先验证计数，再分配内存；其余语义统一交给 Validate。
	if err := validateHRDCount(h.CPBCountMinus1); err != nil {
		return nil, err
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
	// 每项至少 ue(0)、ue(0)、flag 共3位，末尾还有4个5位长度字段。
	// 计数合法但输入已确定截断时，同样不能先分配CPB数组。
	if uint64(h.CPBCountMinus1+1)*3+20 > r.BitsRemaining() {
		return nil, fmt.Errorf("syntax: truncated hrd_parameters CPB entries/delay lengths: %w", h264.ErrInvalidData)
	}
	h.CPB = make([]CPBEntry, h.CPBCountMinus1+1)
	for i := range h.CPB {
		e := &h.CPB[i]
		if e.BitRateValueMinus1, err = readUE(r, "bit_rate_value_minus1"); err != nil {
			return nil, fmt.Errorf("syntax: CPB entry %d: %w", i, err)
		}
		if e.CPBSizeValueMinus1, err = readUE(r, "cpb_size_value_minus1"); err != nil {
			return nil, fmt.Errorf("syntax: CPB entry %d: %w", i, err)
		}
		if e.CBRFlag, err = readFlag(r, "cbr_flag"); err != nil {
			return nil, fmt.Errorf("syntax: CPB entry %d: %w", i, err)
		}
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

// WriteTo 在写入任何位之前验证完整 HRD 结构，不写 rbsp_trailing_bits。
func (h *HRDParameters) WriteTo(w *bitstream.Writer) error {
	if err := h.Validate(); err != nil {
		return err
	}
	if w == nil {
		return fmt.Errorf("syntax: nil HRD writer: %w", h264.ErrInvalidData)
	}
	return writeHRDParameters(w, h)
}

// writeHRDParameters 只写出已经通过 HRD 或 VUI 预验证的结构。
func writeHRDParameters(w *bitstream.Writer, h *HRDParameters) error {
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
	return writeBits(w, 5, uint64(h.TimeOffsetLength), "time_offset_length")
}
