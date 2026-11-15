package bitstream

import (
	"errors"
	"strings"
	"testing"

	h264 "github.com/cry5tallize/goavc"
)

// packBits 把由 '0'/'1' 组成的字符串打包成大端比特流，末尾补零。
// 用字符串表达码字是为了让测试直接对应规范表 9-1，避免手工算字节。
func packBits(t *testing.T, bits string) []byte {
	t.Helper()
	out := make([]byte, (len(bits)+7)/8)
	for i, c := range bits {
		switch c {
		case '1':
			out[i/8] |= 1 << (7 - i%8)
		case '0':
		default:
			t.Fatalf("非法比特字符 %q", c)
		}
	}
	return out
}

// TestReadUE_KnownCodewords 对照 H.264 表 9-1 的已知码字。
func TestReadUE_KnownCodewords(t *testing.T) {
	cases := []struct {
		bits string
		want uint32
	}{
		{"1", 0},
		{"010", 1},
		{"011", 2},
		{"00100", 3},
		{"00101", 4},
		{"00110", 5},
		{"00111", 6},
		{"0001000", 7},
		{"0001001", 8},
	}
	for _, c := range cases {
		r := NewReader(packBits(t, c.bits))
		got, err := r.ReadUE()
		if err != nil {
			t.Fatalf("%q: ReadUE 失败: %v", c.bits, err)
		}
		if got != c.want {
			t.Fatalf("%q: ReadUE = %d，期望 %d", c.bits, got, c.want)
		}
		if r.BitsRead() != uint64(len(c.bits)) {
			t.Fatalf("%q: 消费 %d bit，期望 %d", c.bits, r.BitsRead(), len(c.bits))
		}
	}
}

// TestReadSE_KnownCodewords 验证 codeNum 到有符号值的映射。
func TestReadSE_KnownCodewords(t *testing.T) {
	cases := []struct {
		bits string
		want int32
	}{
		{"1", 0},
		{"010", 1},
		{"011", -1},
		{"00100", 2},
		{"00101", -2},
		{"00110", 3},
		{"00111", -3},
	}
	for _, c := range cases {
		r := NewReader(packBits(t, c.bits))
		got, err := r.ReadSE()
		if err != nil {
			t.Fatalf("%q: ReadSE 失败: %v", c.bits, err)
		}
		if got != c.want {
			t.Fatalf("%q: ReadSE = %d，期望 %d", c.bits, got, c.want)
		}
	}
}

// TestReadUE_MaxUint32 验证 32 个前导零的最大合法码字。
func TestReadUE_MaxUint32(t *testing.T) {
	bits := strings.Repeat("0", 32) + "1" + strings.Repeat("0", 32)
	r := NewReader(packBits(t, bits))
	got, err := r.ReadUE()
	if err != nil {
		t.Fatalf("ReadUE 失败: %v", err)
	}
	if got != ^uint32(0) {
		t.Fatalf("ReadUE = %#x，期望 %#x", got, ^uint32(0))
	}
	if r.BitsRead() != 65 {
		t.Fatalf("消费 %d bit，期望 65", r.BitsRead())
	}
}

// TestReadUE_OverflowRejected 验证无法放入 uint32 的码字被拒绝。
func TestReadUE_OverflowRejected(t *testing.T) {
	// 32 个 0、1、31 个 0、1 ⇒ codeNum = (2^32-1) + 1 = 2^32。
	bits := strings.Repeat("0", 32) + "1" + strings.Repeat("0", 31) + "1"
	r := NewReader(packBits(t, bits))
	if _, err := r.ReadUE(); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("溢出码字错误 = %v，期望 ErrInvalidData", err)
	}
}

// TestReadSE_OverflowRejected 验证映射结果超出 int32 时被拒绝。
func TestReadSE_OverflowRejected(t *testing.T) {
	bits := strings.Repeat("0", 32) + "1" + strings.Repeat("0", 32)
	r := NewReader(packBits(t, bits))
	if _, err := r.ReadSE(); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("se(v) 溢出错误 = %v，期望 ErrInvalidData", err)
	}
}

// TestReadUE_Truncated 验证输入耗尽与超长前导零都返回 ErrInvalidData。
func TestReadUE_Truncated(t *testing.T) {
	r := NewReader([]byte{0x00})
	if _, err := r.ReadUE(); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("全零输入错误 = %v，期望 ErrInvalidData", err)
	}

	// 33 个前导零已经超过规范上限；即使后面还有数据也必须立即拒绝。
	r = NewReader(make([]byte, 16))
	if _, err := r.ReadUE(); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("超长前导零错误 = %v，期望 ErrInvalidData", err)
	}
	if r.BitsRead() != 33 {
		t.Fatalf("超长前导零消费 %d bit，期望在第 33 个零后停止", r.BitsRead())
	}
}
