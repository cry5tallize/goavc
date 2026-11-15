package nal

import (
	"fmt"

	h264 "github.com/cry5tallize/goavc"
)

// AVCDecoderConfig 是 AVCDecoderConfigurationRecord（ISO/IEC 14496-15）的解析结果。
//
// 该记录通常存放在 MP4 的 avcC box 中，包含解码所需的 SPS/PPS、
// NAL 长度前缀宽度以及高位深/色度扩展字段。
//
// 所有权：SPS/PPS 字节在解析时复制，调用方修改原始记录不会影响结果。
type AVCDecoderConfig struct {
	ConfigurationVersion uint8
	ProfileIndication    uint8
	ProfileCompatibility uint8
	LevelIndication      uint8

	// LengthSizeMinusOne 是 NAL 长度前缀字节数减一（0..3）。
	LengthSizeMinusOne uint8

	SPS [][]byte
	PPS [][]byte

	// 仅 High 系列 profile 的扩展字段有效。
	ChromaFormat         uint8
	BitDepthLumaMinus8   uint8
	BitDepthChromaMinus8 uint8
	SPSExt               [][]byte
}

// LengthSize 返回 NAL 长度前缀的字节数（1..4）。
func (c *AVCDecoderConfig) LengthSize() int { return int(c.LengthSizeMinusOne) + 1 }

// ParseAVCDecoderConfig 解析 AVCDecoderConfigurationRecord。
//
// 解析是严格的：reserved 位必须为 1、长度字段不得越过输入、末尾不能有
// 未消费字节。非版本 1 的记录返回 ErrUnsupported（协议版本边界），
// 其余结构错误返回 ErrInvalidData。
func ParseAVCDecoderConfig(data []byte) (*AVCDecoderConfig, error) {
	return ParseAVCDecoderConfigWithLimit(data, 0)
}

// ParseAVCDecoderConfigWithLimit在复制参数集之前校验每个NAL的大小（含header）。
// 0保持原解析器的无额外限制语义；流适配器应传入明确的正上限。
func ParseAVCDecoderConfigWithLimit(data []byte, maxNALSize int) (*AVCDecoderConfig, error) {
	if maxNALSize < 0 {
		return nil, fmt.Errorf("nal: negative avcC NAL limit: %w", h264.ErrInvalidData)
	}
	r := &configReader{data: data, maxNALSize: maxNALSize}
	version, err := r.byte()
	if err != nil {
		return nil, err
	}
	if version != 1 {
		return nil, fmt.Errorf("nal: avcC configurationVersion %d is not supported: %w", version, h264.ErrUnsupported)
	}
	profile, err := r.byte()
	if err != nil {
		return nil, err
	}
	compat, err := r.byte()
	if err != nil {
		return nil, err
	}
	level, err := r.byte()
	if err != nil {
		return nil, err
	}
	lengthByte, err := r.byte()
	if err != nil {
		return nil, err
	}
	if lengthByte&0xFC != 0xFC {
		return nil, fmt.Errorf("nal: avcC reserved bits in lengthSizeMinusOne byte are not all ones: %#02x: %w",
			lengthByte, h264.ErrInvalidData)
	}
	countByte, err := r.byte()
	if err != nil {
		return nil, err
	}
	if countByte&0xE0 != 0xE0 {
		return nil, fmt.Errorf("nal: avcC reserved bits in numOfSequenceParameterSets byte are not all ones: %#02x: %w",
			countByte, h264.ErrInvalidData)
	}
	numSPS := int(countByte & 0x1F)

	cfg := &AVCDecoderConfig{
		ConfigurationVersion: version,
		ProfileIndication:    profile,
		ProfileCompatibility: compat,
		LevelIndication:      level,
		LengthSizeMinusOne:   lengthByte & 0x03,
	}

	for i := 0; i < numSPS; i++ {
		nal, err := r.lengthPrefixed()
		if err != nil {
			return nil, fmt.Errorf("nal: avcC sps[%d]: %w", i, err)
		}
		cfg.SPS = append(cfg.SPS, nal)
	}

	numPPSByte, err := r.byte()
	if err != nil {
		return nil, err
	}
	for i := 0; i < int(numPPSByte); i++ {
		nal, err := r.lengthPrefixed()
		if err != nil {
			return nil, fmt.Errorf("nal: avcC pps[%d]: %w", i, err)
		}
		cfg.PPS = append(cfg.PPS, nal)
	}

	if isHighProfile(profile) {
		chromaByte, err := r.byte()
		if err != nil {
			return nil, fmt.Errorf("nal: avcC high-profile chroma_format: %w", err)
		}
		if chromaByte&0xFC != 0xFC {
			return nil, fmt.Errorf("nal: avcC reserved bits in chroma_format byte are not all ones: %#02x: %w",
				chromaByte, h264.ErrInvalidData)
		}
		lumaByte, err := r.byte()
		if err != nil {
			return nil, fmt.Errorf("nal: avcC high-profile bit_depth_luma: %w", err)
		}
		if lumaByte&0xF8 != 0xF8 {
			return nil, fmt.Errorf("nal: avcC reserved bits in bit_depth_luma byte are not all ones: %#02x: %w",
				lumaByte, h264.ErrInvalidData)
		}
		chromaDepthByte, err := r.byte()
		if err != nil {
			return nil, fmt.Errorf("nal: avcC high-profile bit_depth_chroma: %w", err)
		}
		if chromaDepthByte&0xF8 != 0xF8 {
			return nil, fmt.Errorf("nal: avcC reserved bits in bit_depth_chroma byte are not all ones: %#02x: %w",
				chromaDepthByte, h264.ErrInvalidData)
		}
		numSPSExt, err := r.byte()
		if err != nil {
			return nil, fmt.Errorf("nal: avcC high-profile numOfSequenceParameterSetExt: %w", err)
		}
		for i := 0; i < int(numSPSExt); i++ {
			nal, err := r.lengthPrefixed()
			if err != nil {
				return nil, fmt.Errorf("nal: avcC spsExt[%d]: %w", i, err)
			}
			cfg.SPSExt = append(cfg.SPSExt, nal)
		}
		cfg.ChromaFormat = chromaByte & 0x03
		cfg.BitDepthLumaMinus8 = lumaByte & 0x07
		cfg.BitDepthChromaMinus8 = chromaDepthByte & 0x07
	}

	if r.remaining() != 0 {
		return nil, fmt.Errorf("nal: avcC has %d trailing bytes: %w", r.remaining(), h264.ErrInvalidData)
	}
	return cfg, nil
}

// isHighProfile 判断该 profile 的 avcC 是否携带色度/位深扩展字段。
func isHighProfile(profile uint8) bool {
	switch profile {
	case 100, 110, 122, 144:
		return true
	default:
		return false
	}
}

// configReader 是 avcC 字节游标。
type configReader struct {
	data       []byte
	off        int
	maxNALSize int
}

func (r *configReader) remaining() int { return len(r.data) - r.off }

func (r *configReader) byte() (byte, error) {
	if r.off >= len(r.data) {
		return 0, fmt.Errorf("nal: avcC truncated at offset %d: %w", r.off, h264.ErrInvalidData)
	}
	b := r.data[r.off]
	r.off++
	return b, nil
}

func (r *configReader) bytes(n int) ([]byte, error) {
	if n < 0 || n > r.remaining() {
		return nil, fmt.Errorf("nal: avcC needs %d bytes at offset %d but only %d remain: %w",
			n, r.off, r.remaining(), h264.ErrInvalidData)
	}
	out := make([]byte, n)
	copy(out, r.data[r.off:r.off+n])
	r.off += n
	return out, nil
}

// lengthPrefixed 读取一个 16 bit 长度及其后的 NAL 字节。
func (r *configReader) lengthPrefixed() ([]byte, error) {
	hi, err := r.byte()
	if err != nil {
		return nil, err
	}
	lo, err := r.byte()
	if err != nil {
		return nil, err
	}
	length := int(hi)<<8 | int(lo)
	if length == 0 {
		return nil, fmt.Errorf("nal: avcC contains a zero-length NAL: %w", h264.ErrInvalidData)
	}
	if r.maxNALSize > 0 && length > r.maxNALSize {
		return nil, fmt.Errorf("nal: avcC parameter set length %d exceeds %d: %w", length, r.maxNALSize, h264.ErrResourceLimit)
	}
	nal, err := r.bytes(length)
	if err != nil {
		return nil, err
	}
	if _, err := ParseHeader(nal[0]); err != nil {
		return nil, fmt.Errorf("nal: avcC NAL header: %w", err)
	}
	return nal, nil
}
