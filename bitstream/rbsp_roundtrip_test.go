package bitstream

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// realSPS / realPPS 来自 ffmpeg 8.0 调用 x264 生成的 Baseline 64x48 单帧码流：
//
//	ffmpeg -f lavfi -i testsrc=size=64x48:rate=10 -frames:v 1 \
//	  -c:v libx264 -profile:v baseline -level 1.0 -pix_fmt yuv420p \
//	  -x264-params keyint=1 -f h264 sample.264
//
// 这里只保留 NAL payload（不含起始码）。SPS 内部含 00 00 03 序列，
// 能覆盖真实防竞争字节的移除与重新插入。
const (
	realSPSHex = "6742c00adc47b011000003000100000300140f122780"
	realPPSHex = "68ce0f2c80"
)

var (
	realSPS = mustDecodeHex(realSPSHex)
	realPPS = mustDecodeHex(realPPSHex)
)

// mustDecodeHex 只用于包级测试常量；常量写错属于测试代码缺陷，直接 panic。
func mustDecodeHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic("invalid test hex: " + err.Error())
	}
	return b
}

// TestRBSPRoundTrip_Synthetic 覆盖单字节、零游程与确定性伪随机数据。
func TestRBSPRoundTrip_Synthetic(t *testing.T) {
	var inputs [][]byte
	for b := range 256 {
		inputs = append(inputs, []byte{byte(b)})
	}
	for b := range 256 {
		inputs = append(inputs, []byte{0x00, 0x00, byte(b)})
	}
	inputs = append(inputs,
		bytes.Repeat([]byte{0x00}, 1),
		bytes.Repeat([]byte{0x00}, 2),
		bytes.Repeat([]byte{0x00}, 3),
		bytes.Repeat([]byte{0x00}, 17),
		bytes.Repeat([]byte{0x00, 0x00, 0x01}, 100),
		bytes.Repeat([]byte{0xFF}, 64),
	)

	x := uint64(0x9E3779B97F4A7C15)
	buf := make([]byte, 4096)
	for i := range buf {
		x = x*6364136223846793005 + 1442695040888963407
		buf[i] = byte(x >> 56)
	}
	inputs = append(inputs, buf)

	for i, rbsp := range inputs {
		ebsp := RBSPToEBSP(rbsp)
		got, err := EBSPToRBSP(ebsp)
		if err != nil {
			t.Fatalf("case %d: 合法 EBSP %x 被拒绝: %v", i, ebsp, err)
		}
		if !bytes.Equal(got, rbsp) {
			t.Fatalf("case %d: round-trip %x -> %x", i, rbsp, got)
		}
	}
}

// TestRBSPRoundTrip_RealNAL 用真实 x264 NAL payload 验证往返。
func TestRBSPRoundTrip_RealNAL(t *testing.T) {
	for name, ebsp := range map[string][]byte{
		"SPS": realSPS,
		"PPS": realPPS,
	} {
		t.Run(name, func(t *testing.T) {
			rbsp, err := EBSPToRBSP(ebsp)
			if err != nil {
				t.Fatalf("去除防竞争字节失败: %v", err)
			}
			back := RBSPToEBSP(rbsp)
			if !bytes.Equal(back, ebsp) {
				t.Fatalf("真实 NAL 往返不一致:\n原: %x\n新: %x", ebsp, back)
			}
			if err := ValidateRBSPTrailingBits(rbsp); err != nil {
				t.Fatalf("真实 RBSP 尾位校验失败: %v", err)
			}
		})
	}
}

// FuzzRBSPRoundTrip 验证任意 RBSP 经插入/移除防竞争字节后保持不变。
func FuzzRBSPRoundTrip(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x00})
	f.Add([]byte{0x00, 0x00, 0x01})
	f.Add([]byte{0x00, 0x00, 0x00, 0x00, 0x00})
	f.Add([]byte{0xFF, 0xFF, 0xFF})
	f.Add(realSPS)
	f.Add(realPPS)

	f.Fuzz(func(t *testing.T, rbsp []byte) {
		ebsp := RBSPToEBSP(rbsp)
		got, err := EBSPToRBSP(ebsp)
		if err != nil {
			t.Fatalf("RBSPToEBSP 产出的 EBSP 无法逆转换: %v", err)
		}
		if !bytes.Equal(got, rbsp) {
			t.Fatalf("round-trip 不一致: %x -> %x", rbsp, got)
		}
	})
}
