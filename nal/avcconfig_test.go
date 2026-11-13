package nal

import (
	"bytes"
	"encoding/hex"
	"errors"
	"testing"

	h264 "github.com/goavc/h264"
)

func decodeHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("解码十六进制失败: %v", err)
	}
	return b
}

// baselineRecord 是一份结构完整的 Baseline avcC 记录。
func baselineRecord() []byte {
	return []byte{
		0x01,       // configurationVersion
		0x42,       // AVCProfileIndication (66, Baseline)
		0x00,       // profile_compatibility
		0x1E,       // AVCLevelIndication (30, level 3.0)
		0xFF,       // reserved 111111 + lengthSizeMinusOne 3
		0xE1,       // reserved 111 + numOfSPS 1
		0x00, 0x04, // SPS length
		0x67, 0x42, 0xC0, 0x1E,
		0x01,       // numOfPPS
		0x00, 0x02, // PPS length
		0x68, 0xCE,
	}
}

// highRecord 是一份带扩展字段的 High avcC 记录。
func highRecord() []byte {
	return []byte{
		0x01, 0x64, 0x00, 0x1F,
		0xFF, // lengthSizeMinusOne 3
		0xE1, 0x00, 0x04, 0x67, 0x64, 0x00, 0x1F,
		0x01, 0x00, 0x02, 0x68, 0xEB,
		0xFC, // reserved 111111 + chroma_format 0
		0xF8, // reserved 11111 + bit_depth_luma_minus8 0
		0xF8, // reserved 11111 + bit_depth_chroma_minus8 0
		0x00, // numOfSequenceParameterSetExt
	}
}

func TestParseAVCDecoderConfig_Baseline(t *testing.T) {
	cfg, err := ParseAVCDecoderConfig(baselineRecord())
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if cfg.ConfigurationVersion != 1 || cfg.ProfileIndication != 66 || cfg.LevelIndication != 30 {
		t.Fatalf("头部字段 = %+v", cfg)
	}
	if cfg.LengthSizeMinusOne != 3 || cfg.LengthSize() != 4 {
		t.Fatalf("lengthSizeMinusOne=%d LengthSize=%d", cfg.LengthSizeMinusOne, cfg.LengthSize())
	}
	if len(cfg.SPS) != 1 || !bytes.Equal(cfg.SPS[0], []byte{0x67, 0x42, 0xC0, 0x1E}) {
		t.Fatalf("SPS = %x", cfg.SPS)
	}
	if len(cfg.PPS) != 1 || !bytes.Equal(cfg.PPS[0], []byte{0x68, 0xCE}) {
		t.Fatalf("PPS = %x", cfg.PPS)
	}
}

// realX264AvcC 是 ffmpeg + x264（Main profile，64x48，yuv420p）写出的
// 真实 AVCDecoderConfigurationRecord，由 MP4 的 avcC box 提取：
//
//	ffmpeg -f lavfi -i testsrc2=size=64x48:rate=10 -frames:v 3 \
//	  -c:v libx264 -profile:v main -level 1.0 -pix_fmt yuv420p \
//	  -x264-params keyint=10:bframes=2:cabac=1 -f mp4 sample.mp4
const realX264AvcC = "014d400affe10017" +
	"674d400aeca23d8088000003000800000300a078912cb0" +
	"01000568ebe3cb20"

func TestParseAVCDecoderConfig_RealX264(t *testing.T) {
	record := decodeHex(t, realX264AvcC)
	cfg, err := ParseAVCDecoderConfig(record)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if cfg.ProfileIndication != 77 || cfg.LevelIndication != 10 {
		t.Fatalf("profile/level = %d/%d", cfg.ProfileIndication, cfg.LevelIndication)
	}
	if cfg.LengthSize() != 4 {
		t.Fatalf("LengthSize = %d，期望 4", cfg.LengthSize())
	}
	if len(cfg.SPS) != 1 || len(cfg.PPS) != 1 {
		t.Fatalf("SPS/PPS 数量 = %d/%d", len(cfg.SPS), len(cfg.PPS))
	}
	if got := hex.EncodeToString(cfg.SPS[0]); got != "674d400aeca23d8088000003000800000300a078912cb0" {
		t.Fatalf("SPS = %s", got)
	}
	if got := hex.EncodeToString(cfg.PPS[0]); got != "68ebe3cb20" {
		t.Fatalf("PPS = %s", got)
	}
	back, err := cfg.MarshalBinary()
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if !bytes.Equal(back, record) {
		t.Fatalf("真实 avcC 往返不一致:\n got %x\nwant %x", back, record)
	}
}

func TestParseAVCDecoderConfig_High(t *testing.T) {
	cfg, err := ParseAVCDecoderConfig(highRecord())
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if cfg.ProfileIndication != 100 {
		t.Fatalf("profile = %d", cfg.ProfileIndication)
	}
	if cfg.ChromaFormat != 0 || cfg.BitDepthLumaMinus8 != 0 || cfg.BitDepthChromaMinus8 != 0 {
		t.Fatalf("扩展字段 = chroma=%d luma=%d chromaDepth=%d",
			cfg.ChromaFormat, cfg.BitDepthLumaMinus8, cfg.BitDepthChromaMinus8)
	}
	if len(cfg.SPSExt) != 0 {
		t.Fatalf("SPSExt = %x", cfg.SPSExt)
	}
}

func TestParseAVCDecoderConfig_NoParameters(t *testing.T) {
	data := []byte{0x01, 0x42, 0x00, 0x1E, 0xFF, 0xE0, 0x00}
	cfg, err := ParseAVCDecoderConfig(data)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(cfg.SPS) != 0 || len(cfg.PPS) != 0 {
		t.Fatalf("SPS/PPS 应为空: %+v", cfg)
	}
}

func TestParseAVCDecoderConfig_Errors(t *testing.T) {
	tests := map[string]struct {
		data []byte
		want error
	}{
		"empty": {
			data: nil,
			want: h264.ErrInvalidData,
		},
		"unsupported version": {
			data: append([]byte{0x02}, baselineRecord()[1:]...),
			want: h264.ErrUnsupported,
		},
		"reserved length bits": {
			data: append(append([]byte{}, baselineRecord()[:4]...), 0x00),
			want: h264.ErrInvalidData,
		},
		"reserved sps count bits": {
			data: func() []byte {
				d := append([]byte{}, baselineRecord()...)
				d[5] = 0x01
				return d
			}(),
			want: h264.ErrInvalidData,
		},
		"zero sps length": {
			data: func() []byte {
				d := append([]byte{}, baselineRecord()...)
				d[6], d[7] = 0, 0
				return d
			}(),
			want: h264.ErrInvalidData,
		},
		"forbidden bit": {
			data: func() []byte {
				d := append([]byte{}, baselineRecord()...)
				d[8] = 0x80
				return d
			}(),
			want: h264.ErrInvalidData,
		},
		"pps overrun": {
			data: func() []byte {
				d := append([]byte{}, baselineRecord()...)
				d[12] = 0x00
				d[13] = 0x09
				return d
			}(),
			want: h264.ErrInvalidData,
		},
		"trailing byte": {
			data: append(baselineRecord(), 0x00),
			want: h264.ErrInvalidData,
		},
		"high missing extension": {
			data: highRecord()[:len(highRecord())-4],
			want: h264.ErrInvalidData,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseAVCDecoderConfig(tc.data); !errors.Is(err, tc.want) {
				t.Fatalf("错误 = %v，期望 %v", err, tc.want)
			}
		})
	}
}

func TestParseAVCDecoderConfig_CopiesParameters(t *testing.T) {
	data := baselineRecord()
	cfg, err := ParseAVCDecoderConfig(data)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	for i := range data {
		data[i] = 0xFF
	}
	if !bytes.Equal(cfg.SPS[0], []byte{0x67, 0x42, 0xC0, 0x1E}) {
		t.Fatalf("SPS 被输入修改: %x", cfg.SPS[0])
	}
}
