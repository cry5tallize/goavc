package nal

import (
	"bufio"
	"bytes"
	"fmt"
	"io"

	h264 "github.com/goavc/h264"
)

// AnnexBScanner 从字节流中增量切分 Annex-B NAL unit。
//
// 它不要求一次 Read 返回完整 NAL：bufio.Reader 负责跨 chunk 缓冲，
// 扫描状态只依赖已读取的字节，因此任意分块大小（1 byte、7 bytes、整文件）
// 都会得到相同的 NAL 序列。
//
// 起始码恢复：扫描器接受 3 字节（00 00 01）或 4 字节（00 00 00 01）
// 起始码；起始码之前允许出现前导零。NAL payload 末尾、下一个起始码之前的
// 零字节按 trailing_zero_8bits 处理并被裁掉——这与 §B.1 的字节流格式一致，
// 且合法的 RBSP trailing bits 保证 payload 最后一个字节非零。
//
// 所有权：Next 返回的 Unit.Payload 是新分配的拷贝，调用方可以安全地保留。
type AnnexBScanner struct {
	br *bufio.Reader

	// err 是粘性错误：一旦遇到非法 NAL，后续调用继续返回同一个错误，
	// 避免调用方在损坏码流上意外"恢复"到错误的位置继续解析。
	err error

	// payloadStart 表示上一个 NAL 是在遇到下一个起始码时结束的，
	// 该起始码已经被消费，下一次 Next 应直接从 payload 开始读，
	// 而不是再搜索一次起始码。
	payloadStart bool

	unitIndex int
}

// NewAnnexBScanner 创建一个 Annex-B 扫描器。
func NewAnnexBScanner(r io.Reader) *AnnexBScanner {
	return &AnnexBScanner{br: bufio.NewReaderSize(r, 64*1024)}
}

// Next 返回下一个 NAL unit；流正常结束时返回 io.EOF。
//
// 语法错误（空 NAL、forbidden_zero_bit 置位等）返回包装 ErrInvalidData 的错误，
// 且扫描器进入粘性错误状态。I/O 错误原样返回。
func (s *AnnexBScanner) Next() (Unit, error) {
	if s.err != nil {
		return Unit{}, s.err
	}
	if s.payloadStart {
		s.payloadStart = false
	} else if err := s.skipToStartCode(); err != nil {
		return Unit{}, err
	}

	payload := make([]byte, 0, 1024)
	zeros := 0
	for {
		b, err := s.br.ReadByte()
		if err == io.EOF {
			s.payloadStart = true
			end := trimTrailingZeros(payload)
			if end == 0 {
				return Unit{}, io.EOF
			}
			return s.finishUnit(payload[:end])
		}
		if err != nil {
			s.err = err
			return Unit{}, err
		}

		// 检测下一个起始码。EBSP 中的防竞争机制保证 payload 内不会出现
		// 00 00 01，因此这个模式只可能是起始码。
		if b == 1 && zeros >= 2 {
			// 最后两个 0 属于起始码，不属于 payload。
			payload = payload[:len(payload)-2]
			end := trimTrailingZeros(payload)
			if end == 0 {
				s.err = fmt.Errorf("nal: empty NAL unit at index %d: %w", s.unitIndex, h264.ErrInvalidData)
				return Unit{}, s.err
			}
			unit, err := s.finishUnit(payload[:end])
			if err != nil {
				return Unit{}, err
			}
			// 当前起始码已经被消费；下一次 Next 直接从新 NAL 的 header 开始。
			s.payloadStart = true
			return unit, nil
		}

		payload = append(payload, b)
		if b == 0 {
			zeros++
		} else {
			zeros = 0
		}
	}
}

// skipToStartCode 丢弃第一个起始码之前的字节。
//
// 前导零是 Annex-B 允许的 leading_zero_8bits；其他前导字节也一并跳过，
// 这样在同步丢失后可以重新找到起始码。找不到时返回 io.EOF。
func (s *AnnexBScanner) skipToStartCode() error {
	var secondLast, last byte
	seen := 0
	for {
		b, err := s.br.ReadByte()
		if err == io.EOF {
			return io.EOF
		}
		if err != nil {
			s.err = err
			return err
		}
		if seen >= 2 && secondLast == 0 && last == 0 && b == 1 {
			return nil
		}
		secondLast, last = last, b
		if seen < 2 {
			seen++
		}
	}
}

// finishUnit 解析 header 并构造 Unit。
func (s *AnnexBScanner) finishUnit(raw []byte) (Unit, error) {
	h, err := ParseHeader(raw[0])
	if err != nil {
		s.err = fmt.Errorf("nal: unit %d header: %w", s.unitIndex, err)
		return Unit{}, s.err
	}
	payload := make([]byte, len(raw)-1)
	copy(payload, raw[1:])
	s.unitIndex++
	return Unit{Header: h, Payload: payload}, nil
}

// trimTrailingZeros 返回去掉末尾零字节后的长度。
func trimTrailingZeros(b []byte) int {
	end := len(b)
	for end > 0 && b[end-1] == 0 {
		end--
	}
	return end
}

// SplitAnnexB 一次性切分内存中的 Annex-B 数据。
//
// 这是 Next 循环的便捷封装，主要用于测试与整文件处理；流式处理请使用 Scanner。
func SplitAnnexB(data []byte) ([]Unit, error) {
	scanner := NewAnnexBScanner(bytes.NewReader(data))
	var units []Unit
	for {
		u, err := scanner.Next()
		if err == io.EOF {
			return units, nil
		}
		if err != nil {
			return nil, err
		}
		units = append(units, u)
	}
}
