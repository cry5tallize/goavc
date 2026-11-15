package syntax

import (
	"fmt"
	"math"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/bitstream"
)

// SEIPayloadTypeUserDataUnregistered 是 user_data_unregistered 的 payloadType。
const SEIPayloadTypeUserDataUnregistered = 5

// SEIMessage 是一条 SEI 消息：payloadType + 原始 payload 字节。
//
// Payload 按值拷贝，不引用输入 RBSP。
type SEIMessage struct {
	Type    uint32
	Payload []byte
}

// SEI 是一组按顺序排列的 SEI 消息（对应一个 SEI NAL 的 RBSP）。
type SEI struct {
	Messages []SEIMessage
}

// ParseSEI 解析完整的 SEI RBSP。
//
// 每条消息由 payloadType、payloadSize（两者都支持 0xFF 扩展）和 payload
// 组成。payloadSize 在分配前会与剩余比特数比较，避免恶意长度触发大分配。
func ParseSEI(rbsp []byte) (*SEI, error) {
	r := bitstream.NewReader(rbsp)
	sei := &SEI{}
	for {
		more, err := moreRBSPData(r)
		if err != nil {
			return nil, fmt.Errorf("syntax: sei inspect messages: %w", err)
		}
		if !more {
			break
		}

		payloadType, err := readSEITypeOrSize(r, "payloadType")
		if err != nil {
			return nil, err
		}
		payloadSize, err := readSEITypeOrSize(r, "payloadSize")
		if err != nil {
			return nil, err
		}
		if payloadSize > uint64(r.BitsRemaining()/8) {
			return nil, fmt.Errorf("syntax: sei payloadSize %d exceeds remaining %d bytes: %w",
				payloadSize, r.BitsRemaining()/8, h264.ErrInvalidData)
		}
		maxInt := uint64(^uint(0) >> 1)
		if payloadSize > maxInt {
			return nil, fmt.Errorf("syntax: sei payloadSize %d exceeds int range: %w",
				payloadSize, h264.ErrResourceLimit)
		}
		var payload []byte
		if payloadSize > 0 {
			payload = make([]byte, int(payloadSize))
			for i := range payload {
				b, err := r.ReadByte()
				if err != nil {
					return nil, fmt.Errorf("syntax: sei payload byte %d: %w", i, err)
				}
				payload[i] = b
			}
		}
		sei.Messages = append(sei.Messages, SEIMessage{
			Type:    uint32(payloadType),
			Payload: payload,
		})
	}
	if err := consumeRBSPTrailingBits(r); err != nil {
		return nil, fmt.Errorf("syntax: sei trailing bits: %w", err)
	}
	return sei, nil
}

// readSEITypeOrSize 读取 payloadType 或 payloadSize 的 0xFF 扩展编码。
func readSEITypeOrSize(r *bitstream.Reader, name string) (uint64, error) {
	var value uint64
	for {
		b, err := r.ReadByte()
		if err != nil {
			return 0, fmt.Errorf("syntax: sei %s: %w", name, err)
		}
		value += uint64(b)
		if b != 0xFF {
			if value > math.MaxUint32 {
				return 0, fmt.Errorf("syntax: sei %s %d exceeds uint32: %w",
					name, value, h264.ErrInvalidData)
			}
			return value, nil
		}
		if value > math.MaxUint32 {
			return 0, fmt.Errorf("syntax: sei %s overflow: %w", name, h264.ErrInvalidData)
		}
	}
}

// MarshalRBSP 序列化完整 SEI RBSP（含 rbsp_trailing_bits）。
func (s *SEI) MarshalRBSP() ([]byte, error) {
	if s == nil {
		return nil, fmt.Errorf("syntax: nil sei")
	}
	w := bitstream.NewWriter()
	for i, msg := range s.Messages {
		if err := writeSEITypeOrSize(w, msg.Type, "payloadType"); err != nil {
			return nil, fmt.Errorf("syntax: sei message %d: %w", i, err)
		}
		if uint64(len(msg.Payload)) > math.MaxUint32 {
			return nil, fmt.Errorf("syntax: sei message %d payload too large", i)
		}
		if err := writeSEITypeOrSize(w, uint32(len(msg.Payload)), "payloadSize"); err != nil {
			return nil, fmt.Errorf("syntax: sei message %d: %w", i, err)
		}
		for _, b := range msg.Payload {
			if err := w.WriteByte(b); err != nil {
				return nil, fmt.Errorf("syntax: sei message %d payload: %w", i, err)
			}
		}
	}
	if err := bitstream.WriteRBSPTrailingBits(w); err != nil {
		return nil, fmt.Errorf("syntax: sei trailing bits: %w", err)
	}
	return w.Bytes(), nil
}

// writeSEITypeOrSize 写出 0xFF 扩展编码。
func writeSEITypeOrSize(w *bitstream.Writer, value uint32, name string) error {
	v := value
	for v >= 0xFF {
		if err := w.WriteByte(0xFF); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		v -= 0xFF
	}
	if err := w.WriteByte(byte(v)); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// UserDataUnregistered 是 user_data_unregistered SEI 消息的解析结果。
type UserDataUnregistered struct {
	UUID [16]byte
	Data []byte
}

// ParseUserDataUnregistered 解析 payloadType == 5 的 payload。
//
// payload 至少包含 16 字节 UUID；其余字节是 user_data_payload_byte。
func ParseUserDataUnregistered(payload []byte) (*UserDataUnregistered, error) {
	if len(payload) < 16 {
		return nil, fmt.Errorf("syntax: user_data_unregistered payload %d bytes, need at least 16: %w",
			len(payload), h264.ErrInvalidData)
	}
	u := &UserDataUnregistered{}
	copy(u.UUID[:], payload[:16])
	u.Data = make([]byte, len(payload)-16)
	copy(u.Data, payload[16:])
	return u, nil
}
