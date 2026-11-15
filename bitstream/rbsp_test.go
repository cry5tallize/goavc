package bitstream

import (
	"bytes"
	"errors"
	"testing"

	h264 "github.com/cry5tallize/goavc"
)

// TestRBSPToEBSP 覆盖规范 §7.4.1 的插入条件。
func TestRBSPToEBSP(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want []byte
	}{
		{"empty", nil, nil},
		{"no zeros", []byte{0x01, 0x02, 0x03}, []byte{0x01, 0x02, 0x03}},
		{"single zero", []byte{0x00, 0x01}, []byte{0x00, 0x01}},
		{"two zeros then 01", []byte{0x00, 0x00, 0x01}, []byte{0x00, 0x00, 0x03, 0x01}},
		{"two zeros then 02", []byte{0x00, 0x00, 0x02}, []byte{0x00, 0x00, 0x03, 0x02}},
		{"two zeros then 03", []byte{0x00, 0x00, 0x03}, []byte{0x00, 0x00, 0x03, 0x03}},
		{"two zeros then 04", []byte{0x00, 0x00, 0x04}, []byte{0x00, 0x00, 0x04}},
		{"three zeros", []byte{0x00, 0x00, 0x00}, []byte{0x00, 0x00, 0x03, 0x00}},
		{"four zeros", []byte{0x00, 0x00, 0x00, 0x00}, []byte{0x00, 0x00, 0x03, 0x00, 0x00}},
		{
			"zero run with escape",
			[]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x01},
			[]byte{0x00, 0x00, 0x03, 0x00, 0x00, 0x03, 0x00, 0x01},
		},
		{"zero then non-zero resets", []byte{0x00, 0x01, 0x00, 0x00, 0x02}, []byte{0x00, 0x01, 0x00, 0x00, 0x03, 0x02}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := RBSPToEBSP(c.in)
			if !bytes.Equal(got, c.want) {
				t.Fatalf("RBSPToEBSP(%x) = %x，期望 %x", c.in, got, c.want)
			}
		})
	}
}

// TestEBSPToRBSP 覆盖防竞争字节移除与零游程重置。
func TestEBSPToRBSP(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want []byte
	}{
		{"empty", nil, nil},
		{"no escape", []byte{0x01, 0x02, 0x03}, []byte{0x01, 0x02, 0x03}},
		{"single zero", []byte{0x00, 0x01}, []byte{0x00, 0x01}},
		{"escape 01", []byte{0x00, 0x00, 0x03, 0x01}, []byte{0x00, 0x00, 0x01}},
		{"escape 02", []byte{0x00, 0x00, 0x03, 0x02}, []byte{0x00, 0x00, 0x02}},
		{"escape 03", []byte{0x00, 0x00, 0x03, 0x03}, []byte{0x00, 0x00, 0x03}},
		{"escape 00", []byte{0x00, 0x00, 0x03, 0x00}, []byte{0x00, 0x00, 0x00}},
		{"two escapes", []byte{0x00, 0x00, 0x03, 0x00, 0x00}, []byte{0x00, 0x00, 0x00, 0x00}},
		{"03 after one zero", []byte{0x00, 0x03, 0x01}, []byte{0x00, 0x03, 0x01}},
		{"zero run then data", []byte{0x00, 0x00, 0x03, 0x00, 0x7F}, []byte{0x00, 0x00, 0x00, 0x7F}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := EBSPToRBSP(c.in)
			if err != nil {
				t.Fatalf("EBSPToRBSP(%x) 失败: %v", c.in, err)
			}
			if !bytes.Equal(got, c.want) {
				t.Fatalf("EBSPToRBSP(%x) = %x，期望 %x", c.in, got, c.want)
			}
		})
	}
}

// TestEBSPToRBSP_Invalid 验证未转义的三零序列与悬挂 03 被拒绝。
func TestEBSPToRBSP_Invalid(t *testing.T) {
	cases := map[string][]byte{
		"unescaped 00": {0x00, 0x00, 0x00},
		"unescaped 01": {0x00, 0x00, 0x01},
		"unescaped 02": {0x00, 0x00, 0x02},
		"dangling 03":  {0x00, 0x00, 0x03},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := EBSPToRBSP(in); !errors.Is(err, h264.ErrInvalidData) {
				t.Fatalf("EBSPToRBSP(%x) 错误 = %v，期望 ErrInvalidData", in, err)
			}
		})
	}
}

// TestEBSPToRBSP_DoesNotModifyInput 验证转换是纯函数。
func TestEBSPToRBSP_DoesNotModifyInput(t *testing.T) {
	in := []byte{0x00, 0x00, 0x03, 0x01}
	before := append([]byte(nil), in...)
	_, _ = EBSPToRBSP(in)
	if !bytes.Equal(in, before) {
		t.Fatalf("输入被修改: %x -> %x", before, in)
	}
}

// TestValidateRBSPTrailingBits 验证最后一个字节非零这一判据。
func TestValidateRBSPTrailingBits(t *testing.T) {
	valid := [][]byte{
		{0x80},
		{0x01},
		{0x03},
		{0xFF},
		{0x00, 0x80},
		{0xAA, 0x01},
	}
	for _, rbsp := range valid {
		if err := ValidateRBSPTrailingBits(rbsp); err != nil {
			t.Fatalf("合法 RBSP %x 被拒绝: %v", rbsp, err)
		}
	}

	invalid := [][]byte{
		nil,
		{},
		{0x00},
		{0x80, 0x00},
		{0x00, 0x00},
	}
	for _, rbsp := range invalid {
		if err := ValidateRBSPTrailingBits(rbsp); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("非法 RBSP %x 错误 = %v，期望 ErrInvalidData", rbsp, err)
		}
	}
}

// TestWriteRBSPTrailingBits 验证停止位与零填充对齐。
func TestWriteRBSPTrailingBits(t *testing.T) {
	w := NewWriter()
	if err := w.WriteBits(3, 0b101); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if err := WriteRBSPTrailingBits(w); err != nil {
		t.Fatalf("写尾位失败: %v", err)
	}
	if w.BitsWritten() != 8 {
		t.Fatalf("写尾位后 %d bit，期望 8", w.BitsWritten())
	}
	got := w.Bytes()
	if !bytes.Equal(got, []byte{0b10110000}) {
		t.Fatalf("尾位结果 %08b，期望 10110000", got)
	}
	if err := ValidateRBSPTrailingBits(got); err != nil {
		t.Fatalf("自产 RBSP 校验失败: %v", err)
	}

	// 已对齐时停止位单独占一个字节。
	w = NewWriter()
	if err := w.WriteByte(0xAA); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if err := WriteRBSPTrailingBits(w); err != nil {
		t.Fatalf("写尾位失败: %v", err)
	}
	if got := w.Bytes(); !bytes.Equal(got, []byte{0xAA, 0x80}) {
		t.Fatalf("对齐后的尾位结果 %x，期望 aa80", got)
	}
}

// TestRBSPToEBSP_DoesNotModifyInput 验证转换是纯函数。
func TestRBSPToEBSP_DoesNotModifyInput(t *testing.T) {
	in := []byte{0x00, 0x00, 0x01, 0x02}
	before := append([]byte(nil), in...)
	_ = RBSPToEBSP(in)
	if !bytes.Equal(in, before) {
		t.Fatalf("输入被修改: %x -> %x", before, in)
	}
}
