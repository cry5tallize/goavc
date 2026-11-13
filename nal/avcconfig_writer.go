package nal

import (
	"fmt"
)

// MarshalBinary 把 AVCDecoderConfig 序列化为 AVCDecoderConfigurationRecord。
//
// 写出前会校验所有长度与保留位能否被记录格式表达；校验失败时不产生输出。
// 对解析得到的配置调用本方法，应当得到与原始记录逐字节相同的结果
// （前提是原始记录的 reserved 位符合规范）。
func (c *AVCDecoderConfig) MarshalBinary() ([]byte, error) {
	if c == nil {
		return nil, fmt.Errorf("nal: nil avcC configuration")
	}
	if c.ConfigurationVersion != 1 {
		return nil, fmt.Errorf("nal: avcC configurationVersion %d cannot be written", c.ConfigurationVersion)
	}
	if c.LengthSizeMinusOne > 3 {
		return nil, fmt.Errorf("nal: avcC lengthSizeMinusOne %d out of range", c.LengthSizeMinusOne)
	}
	if len(c.SPS) > 31 {
		return nil, fmt.Errorf("nal: avcC has %d SPS, maximum is 31", len(c.SPS))
	}
	if len(c.PPS) > 255 {
		return nil, fmt.Errorf("nal: avcC has %d PPS, maximum is 255", len(c.PPS))
	}
	if len(c.SPSExt) > 255 {
		return nil, fmt.Errorf("nal: avcC has %d SPS extensions, maximum is 255", len(c.SPSExt))
	}
	high := isHighProfile(c.ProfileIndication)
	if high {
		if c.ChromaFormat > 3 {
			return nil, fmt.Errorf("nal: avcC chroma_format %d out of range", c.ChromaFormat)
		}
		if c.BitDepthLumaMinus8 > 7 {
			return nil, fmt.Errorf("nal: avcC bit_depth_luma_minus8 %d out of range", c.BitDepthLumaMinus8)
		}
		if c.BitDepthChromaMinus8 > 7 {
			return nil, fmt.Errorf("nal: avcC bit_depth_chroma_minus8 %d out of range", c.BitDepthChromaMinus8)
		}
	}

	out := make([]byte, 0, 16+len(c.SPS)*8+len(c.PPS)*8)
	out = append(out,
		c.ConfigurationVersion,
		c.ProfileIndication,
		c.ProfileCompatibility,
		c.LevelIndication,
		0xFC|c.LengthSizeMinusOne,
		0xE0|byte(len(c.SPS)),
	)
	var err error
	for i, sps := range c.SPS {
		if out, err = appendConfigNAL(out, sps); err != nil {
			return nil, fmt.Errorf("nal: avcC sps[%d]: %w", i, err)
		}
	}
	out = append(out, byte(len(c.PPS)))
	for i, pps := range c.PPS {
		if out, err = appendConfigNAL(out, pps); err != nil {
			return nil, fmt.Errorf("nal: avcC pps[%d]: %w", i, err)
		}
	}
	if high {
		out = append(out,
			0xFC|c.ChromaFormat,
			0xF8|c.BitDepthLumaMinus8,
			0xF8|c.BitDepthChromaMinus8,
			byte(len(c.SPSExt)),
		)
		for i, ext := range c.SPSExt {
			if out, err = appendConfigNAL(out, ext); err != nil {
				return nil, fmt.Errorf("nal: avcC spsExt[%d]: %w", i, err)
			}
		}
	}
	return out, nil
}

// appendConfigNAL 追加 16 bit 长度前缀与 NAL 字节。
func appendConfigNAL(out, nal []byte) ([]byte, error) {
	if len(nal) == 0 {
		return nil, fmt.Errorf("empty NAL")
	}
	if len(nal) > 0xFFFF {
		return nil, fmt.Errorf("NAL length %d exceeds 16-bit field", len(nal))
	}
	if _, err := ParseHeader(nal[0]); err != nil {
		return nil, err
	}
	out = append(out, byte(len(nal)>>8), byte(len(nal)))
	return append(out, nal...), nil
}
