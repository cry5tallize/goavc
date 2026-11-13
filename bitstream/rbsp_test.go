package bitstream

import (
	"bytes"
	"errors"
	"testing"

	h264 "github.com/goavc/h264"
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

// TestRBSPToEBSP_DoesNotModifyInput 验证转换是纯函数。
func TestRBSPToEBSP_DoesNotModifyInput(t *testing.T) {
	in := []byte{0x00, 0x00, 0x01, 0x02}
	before := append([]byte(nil), in...)
	_ = RBSPToEBSP(in)
	if !bytes.Equal(in, before) {
		t.Fatalf("输入被修改: %x -> %x", before, in)
	}
}
