package syntax

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/bitstream"
)

func TestSEI_RoundTrip(t *testing.T) {
	sei := &SEI{Messages: []SEIMessage{
		{Type: 0, Payload: nil},
		{Type: SEIPayloadTypeUserDataUnregistered, Payload: []byte("x264 options")},
		{Type: 255, Payload: []byte{0x00}},
		{Type: 300, Payload: bytes.Repeat([]byte{0xAA}, 255)},
		{Type: 65535, Payload: bytes.Repeat([]byte{0x55}, 300)},
	}}
	rbsp, err := sei.MarshalRBSP()
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if err := bitstream.ValidateRBSPTrailingBits(rbsp); err != nil {
		t.Fatalf("尾位非法: %v", err)
	}
	parsed, err := ParseSEI(rbsp)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if !reflect.DeepEqual(parsed, sei) {
		t.Fatalf("结构不一致:\n got %+v\nwant %+v", parsed, sei)
	}
}

func TestSEI_EmptyMessages(t *testing.T) {
	rbsp, err := (&SEI{}).MarshalRBSP()
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	parsed, err := ParseSEI(rbsp)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(parsed.Messages) != 0 {
		t.Fatalf("得到 %d 条消息", len(parsed.Messages))
	}
}

func TestParseSEI_Errors(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		if _, err := ParseSEI(nil); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("错误 = %v，期望 ErrInvalidData", err)
		}
	})
	t.Run("truncated payload", func(t *testing.T) {
		// payloadType=0, payloadSize=2, 但只有 1 个 payload 字节。
		w := bitstream.NewWriter()
		if err := w.WriteByte(0x00); err != nil {
			t.Fatal(err)
		}
		if err := w.WriteByte(0x02); err != nil {
			t.Fatal(err)
		}
		if err := w.WriteByte(0xAA); err != nil {
			t.Fatal(err)
		}
		if err := bitstream.WriteRBSPTrailingBits(w); err != nil {
			t.Fatal(err)
		}
		if _, err := ParseSEI(w.Bytes()); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("错误 = %v，期望 ErrInvalidData", err)
		}
	})
	t.Run("huge payload size", func(t *testing.T) {
		data := []byte{0x00, 0xFF, 0xFF, 0xFF, 0xFF, 0x0F}
		if _, err := ParseSEI(data); !errors.Is(err, h264.ErrInvalidData) &&
			!errors.Is(err, h264.ErrResourceLimit) {
			t.Fatalf("错误 = %v，期望 ErrInvalidData/ErrResourceLimit", err)
		}
	})
	t.Run("invalid trailing bits", func(t *testing.T) {
		w := bitstream.NewWriter()
		if err := w.WriteByte(0x00); err != nil {
			t.Fatal(err)
		}
		if err := w.WriteByte(0x00); err != nil {
			t.Fatal(err)
		}
		w.ByteAlign()
		if _, err := ParseSEI(w.Bytes()); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("错误 = %v，期望 ErrInvalidData", err)
		}
	})
}

func TestSEI_MarshalNil(t *testing.T) {
	var sei *SEI
	if _, err := sei.MarshalRBSP(); err == nil {
		t.Fatal("nil SEI 应返回错误")
	}
}

func TestParseUserDataUnregistered(t *testing.T) {
	payload := make([]byte, 16+5)
	for i := range payload {
		payload[i] = byte(i)
	}
	wantData := append([]byte(nil), payload[16:]...)
	u, err := ParseUserDataUnregistered(payload)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if !bytes.Equal(u.UUID[:], payload[:16]) {
		t.Fatalf("UUID = %x", u.UUID)
	}
	if !bytes.Equal(u.Data, payload[16:]) {
		t.Fatalf("Data = %x", u.Data)
	}
	// 返回值必须与输入解耦。
	for i := range payload {
		payload[i] = 0xFF
	}
	if !bytes.Equal(u.Data, wantData) {
		t.Fatalf("Data 被输入修改: %x，期望 %x", u.Data, wantData)
	}

	if _, err := ParseUserDataUnregistered([]byte{1, 2, 3}); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("短 payload 错误 = %v，期望 ErrInvalidData", err)
	}
}
