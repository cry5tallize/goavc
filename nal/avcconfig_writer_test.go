package nal

import (
	"bytes"
	"reflect"
	"testing"
)

// TestAVCDecoderConfig_RoundTripBytes 验证解析→写出与原始记录逐字节一致。
func TestAVCDecoderConfig_RoundTripBytes(t *testing.T) {
	for name, record := range map[string][]byte{
		"baseline":  baselineRecord(),
		"high":      highRecord(),
		"no params": {0x01, 0x42, 0x00, 0x1E, 0xFF, 0xE0, 0x00},
	} {
		t.Run(name, func(t *testing.T) {
			cfg, err := ParseAVCDecoderConfig(record)
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			got, err := cfg.MarshalBinary()
			if err != nil {
				t.Fatalf("写出失败: %v", err)
			}
			if !bytes.Equal(got, record) {
				t.Fatalf("写出 %x，期望 %x", got, record)
			}
		})
	}
}

// TestAVCDecoderConfig_ParseSerializeParse 验证结构级深比较。
func TestAVCDecoderConfig_ParseSerializeParse(t *testing.T) {
	original := &AVCDecoderConfig{
		ConfigurationVersion: 1,
		ProfileIndication:    100,
		ProfileCompatibility: 0x00,
		LevelIndication:      31,
		LengthSizeMinusOne:   2,
		SPS: [][]byte{
			{0x67, 0x64, 0x00, 0x1F, 0x01},
			{0x67, 0x64, 0x00, 0x1F, 0x02},
		},
		PPS: [][]byte{
			{0x68, 0xEB, 0xE3, 0xCB},
		},
		ChromaFormat:         1,
		BitDepthLumaMinus8:   2,
		BitDepthChromaMinus8: 2,
		SPSExt:               [][]byte{{0x6D, 0x01}},
	}
	data, err := original.MarshalBinary()
	if err != nil {
		t.Fatalf("写出失败: %v", err)
	}
	parsed, err := ParseAVCDecoderConfig(data)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if !reflect.DeepEqual(parsed, original) {
		t.Fatalf("深比较不一致:\n got %+v\nwant %+v", parsed, original)
	}
}

// TestAVCDecoderConfig_LengthSizes 验证 1..4 字节长度前缀配置的往返。
func TestAVCDecoderConfig_LengthSizes(t *testing.T) {
	for size := uint8(0); size <= 3; size++ {
		cfg := &AVCDecoderConfig{
			ConfigurationVersion: 1,
			ProfileIndication:    66,
			LevelIndication:      30,
			LengthSizeMinusOne:   size,
			SPS:                  [][]byte{{0x67, 0x42, 0xC0, 0x1E}},
			PPS:                  [][]byte{{0x68, 0xCE}},
		}
		data, err := cfg.MarshalBinary()
		if err != nil {
			t.Fatalf("size=%d 写出失败: %v", size, err)
		}
		parsed, err := ParseAVCDecoderConfig(data)
		if err != nil {
			t.Fatalf("size=%d 解析失败: %v", size, err)
		}
		if !reflect.DeepEqual(parsed, cfg) {
			t.Fatalf("size=%d 不一致: %+v vs %+v", size, parsed, cfg)
		}
	}
}

// TestAVCDecoderConfig_MarshalErrors 验证无法表达的配置被拒绝。
func TestAVCDecoderConfig_MarshalErrors(t *testing.T) {
	valid := func() *AVCDecoderConfig {
		return &AVCDecoderConfig{
			ConfigurationVersion: 1,
			ProfileIndication:    66,
			LevelIndication:      30,
			SPS:                  [][]byte{{0x67, 0x42}},
			PPS:                  [][]byte{{0x68, 0xCE}},
		}
	}

	tests := map[string]func() *AVCDecoderConfig{
		"nil": func() *AVCDecoderConfig { return nil },
		"bad version": func() *AVCDecoderConfig {
			c := valid()
			c.ConfigurationVersion = 2
			return c
		},
		"bad length size": func() *AVCDecoderConfig {
			c := valid()
			c.LengthSizeMinusOne = 4
			return c
		},
		"too many sps": func() *AVCDecoderConfig {
			c := valid()
			c.SPS = make([][]byte, 32)
			for i := range c.SPS {
				c.SPS[i] = []byte{0x67}
			}
			return c
		},
		"too many pps": func() *AVCDecoderConfig {
			c := valid()
			c.PPS = make([][]byte, 256)
			for i := range c.PPS {
				c.PPS[i] = []byte{0x68}
			}
			return c
		},
		"empty sps": func() *AVCDecoderConfig {
			c := valid()
			c.SPS = [][]byte{{}}
			return c
		},
		"oversized sps": func() *AVCDecoderConfig {
			c := valid()
			c.SPS = [][]byte{append([]byte{0x67}, bytes.Repeat([]byte{0x00}, 0xFFFF)...)}
			return c
		},
		"bad header": func() *AVCDecoderConfig {
			c := valid()
			c.SPS = [][]byte{{0x80}}
			return c
		},
		"bad chroma": func() *AVCDecoderConfig {
			c := valid()
			c.ProfileIndication = 100
			c.ChromaFormat = 4
			return c
		},
		"bad luma depth": func() *AVCDecoderConfig {
			c := valid()
			c.ProfileIndication = 100
			c.BitDepthLumaMinus8 = 8
			return c
		},
		"bad chroma depth": func() *AVCDecoderConfig {
			c := valid()
			c.ProfileIndication = 100
			c.BitDepthChromaMinus8 = 8
			return c
		},
		"too many sps ext": func() *AVCDecoderConfig {
			c := valid()
			c.ProfileIndication = 100
			c.SPSExt = make([][]byte, 256)
			for i := range c.SPSExt {
				c.SPSExt[i] = []byte{0x6D}
			}
			return c
		},
	}
	for name, makeCfg := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := makeCfg().MarshalBinary(); err == nil {
				t.Fatal("期望返回错误")
			}
		})
	}
}
