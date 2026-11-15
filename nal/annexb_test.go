package nal

import (
	"bytes"
	"errors"
	"testing"

	h264 "github.com/cry5tallize/goavc"
)

// annexBWith 用 4 字节起始码拼接 NAL unit。
func annexBWith(units ...Unit) []byte {
	var out []byte
	for _, u := range units {
		out = append(out, 0x00, 0x00, 0x00, 0x01)
		out = append(out, u.Bytes()...)
	}
	return out
}

func sampleUnits() []Unit {
	return []Unit{
		{Header: Header{RefIDC: 3, Type: TypeSPS}, Payload: []byte{0x42, 0xC0, 0x0A, 0x00, 0x00, 0x03, 0x01, 0x80}},
		{Header: Header{RefIDC: 3, Type: TypePPS}, Payload: []byte{0xCE, 0x0F, 0x2C, 0x80}},
		{Header: Header{RefIDC: 3, Type: TypeSliceIDR}, Payload: []byte{0x88, 0x84, 0x11, 0xFF}},
	}
}

// TestSplitAnnexB_Basic 验证基本切分、header 与 payload 内容。
func TestSplitAnnexB_Basic(t *testing.T) {
	units, err := SplitAnnexB(annexBWith(sampleUnits()...))
	if err != nil {
		t.Fatalf("SplitAnnexB 失败: %v", err)
	}
	if len(units) != 3 {
		t.Fatalf("得到 %d 个 NAL，期望 3", len(units))
	}
	wantTypes := []UnitType{TypeSPS, TypePPS, TypeSliceIDR}
	wantPayloads := [][]byte{
		{0x42, 0xC0, 0x0A, 0x00, 0x00, 0x03, 0x01, 0x80},
		{0xCE, 0x0F, 0x2C, 0x80},
		{0x88, 0x84, 0x11, 0xFF},
	}
	for i, u := range units {
		if u.Header.Type != wantTypes[i] {
			t.Fatalf("unit %d type = %s，期望 %s", i, u.Header.Type, wantTypes[i])
		}
		if u.Header.RefIDC != 3 {
			t.Fatalf("unit %d ref_idc = %d", i, u.Header.RefIDC)
		}
		if !bytes.Equal(u.Payload, wantPayloads[i]) {
			t.Fatalf("unit %d payload = %x，期望 %x", i, u.Payload, wantPayloads[i])
		}
	}
}

// TestSplitAnnexB_MixedStartCodes 覆盖 3/4 字节起始码、前导零与尾随零。
func TestSplitAnnexB_MixedStartCodes(t *testing.T) {
	u1 := Unit{Header: Header{RefIDC: 2, Type: TypeSPS}, Payload: []byte{0x11}}
	u2 := Unit{Header: Header{RefIDC: 1, Type: TypePPS}, Payload: []byte{0x22, 0x33}}
	stream := []byte{
		0x00, 0x00, 0x00, 0x00, 0x00, // 前导零
		0x00, 0x00, 0x01, // 3 字节起始码
	}
	stream = append(stream, u1.Bytes()...)
	stream = append(stream, 0x00, 0x00, 0x00, 0x00) // trailing_zero_8bits
	stream = append(stream, 0x00, 0x00, 0x00, 0x01) // 4 字节起始码
	stream = append(stream, u2.Bytes()...)
	stream = append(stream, 0x00, 0x00, 0x00) // 文件末尾零

	units, err := SplitAnnexB(stream)
	if err != nil {
		t.Fatalf("SplitAnnexB 失败: %v", err)
	}
	if len(units) != 2 {
		t.Fatalf("得到 %d 个 NAL，期望 2", len(units))
	}
	if units[0].Header.Type != TypeSPS || !bytes.Equal(units[0].Payload, []byte{0x11}) {
		t.Fatalf("unit 0 = %+v", units[0])
	}
	if units[1].Header.Type != TypePPS || !bytes.Equal(units[1].Payload, []byte{0x22, 0x33}) {
		t.Fatalf("unit 1 = %+v", units[1])
	}
}

// TestSplitAnnexB_EscapedZerosDoNotSplit 验证 EBSP 内的 00 00 03 不会误判为起始码。
func TestSplitAnnexB_EscapedZerosDoNotSplit(t *testing.T) {
	payload := []byte{0x00, 0x00, 0x03, 0x01, 0x00, 0x00, 0x03, 0x7F}
	u := Unit{Header: Header{RefIDC: 3, Type: TypeSlice}, Payload: payload}
	units, err := SplitAnnexB(annexBWith(u))
	if err != nil {
		t.Fatalf("SplitAnnexB 失败: %v", err)
	}
	if len(units) != 1 {
		t.Fatalf("得到 %d 个 NAL，期望 1", len(units))
	}
	if !bytes.Equal(units[0].Payload, payload) {
		t.Fatalf("payload = %x，期望 %x", units[0].Payload, payload)
	}
}

// TestSplitAnnexB_HeaderOnly 验证只有 header 的 NAL。
func TestSplitAnnexB_HeaderOnly(t *testing.T) {
	u := Unit{Header: Header{RefIDC: 0, Type: TypeEndOfStream}}
	units, err := SplitAnnexB(annexBWith(u))
	if err != nil {
		t.Fatalf("SplitAnnexB 失败: %v", err)
	}
	if len(units) != 1 || units[0].Header.Type != TypeEndOfStream || len(units[0].Payload) != 0 {
		t.Fatalf("结果 = %+v", units)
	}
}

// TestSplitAnnexB_EmptyAndTruncated 验证空输入、仅起始码与不完整起始码。
func TestSplitAnnexB_EmptyAndTruncated(t *testing.T) {
	for name, data := range map[string][]byte{
		"empty":             nil,
		"only start code":   {0x00, 0x00, 0x01},
		"truncated zeros":   {0x00, 0x00},
		"leading zeros":     {0x00, 0x00, 0x00},
		"no start code":     {0x67, 0x42, 0x00},
		"start code only 3": {0x00, 0x00, 0x01, 0x00, 0x00},
	} {
		t.Run(name, func(t *testing.T) {
			units, err := SplitAnnexB(data)
			if err != nil {
				t.Fatalf("SplitAnnexB(%x) 失败: %v", data, err)
			}
			if len(units) != 0 {
				t.Fatalf("得到 %d 个 NAL，期望 0", len(units))
			}
		})
	}
}

// TestSplitAnnexB_Errors 验证空 NAL 与非法 header 的错误语义。
func TestSplitAnnexB_Errors(t *testing.T) {
	t.Run("empty NAL", func(t *testing.T) {
		data := []byte{0x00, 0x00, 0x01, 0x00, 0x00, 0x01}
		if _, err := SplitAnnexB(data); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("错误 = %v，期望 ErrInvalidData", err)
		}
	})
	t.Run("forbidden bit", func(t *testing.T) {
		data := []byte{0x00, 0x00, 0x01, 0x80, 0x00}
		if _, err := SplitAnnexB(data); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("错误 = %v，期望 ErrInvalidData", err)
		}
	})
	t.Run("sticky error", func(t *testing.T) {
		scanner := NewAnnexBScanner(bytes.NewReader([]byte{0x00, 0x00, 0x01, 0x80}))
		_, err1 := scanner.Next()
		_, err2 := scanner.Next()
		if !errors.Is(err1, h264.ErrInvalidData) || !errors.Is(err2, h264.ErrInvalidData) {
			t.Fatalf("粘性错误: %v / %v", err1, err2)
		}
	})
}

// TestSplitAnnexB_CopiesPayload 验证返回的 payload 不引用输入缓冲区。
func TestSplitAnnexB_CopiesPayload(t *testing.T) {
	data := annexBWith(Unit{Header: Header{RefIDC: 3, Type: TypeSPS}, Payload: []byte{0xAA, 0xBB}})
	units, err := SplitAnnexB(data)
	if err != nil {
		t.Fatalf("SplitAnnexB 失败: %v", err)
	}
	for i := range data {
		data[i] = 0xFF
	}
	if !bytes.Equal(units[0].Payload, []byte{0xAA, 0xBB}) {
		t.Fatalf("payload 被输入缓冲区修改: %x", units[0].Payload)
	}
}

// TestUnit_Bytes 验证 header + payload 的重新拼接。
func TestUnit_Bytes(t *testing.T) {
	u := Unit{Header: Header{RefIDC: 3, Type: TypeSPS}, Payload: []byte{0x11, 0x22}}
	want := []byte{0x67, 0x11, 0x22}
	if got := u.Bytes(); !bytes.Equal(got, want) {
		t.Fatalf("Bytes() = %x，期望 %x", got, want)
	}
}
