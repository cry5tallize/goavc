package stream

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/nal"
)

// collectAnnexBAccessUnit供两种Writer共享写前验证。各上限须已由构造器设为正值，
// prefixSize为输出封装前缀宽度（1..4），输入始终为Annex-B。
// 同时限制原始输入与重封装输出，避免三字节起始码扩张等情况绕过AU预算。
func collectAnnexBAccessUnit(data []byte, maxNALSize, maxAccessUnitSize, maxNALUnits, prefixSize int) ([]nal.Unit, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("stream: 空Annex-B AU: %w", h264.ErrInvalidData)
	}
	if len(data) > maxAccessUnitSize {
		return nil, fmt.Errorf("stream: Annex-B输入AU超限: %w", h264.ErrResourceLimit)
	}
	r, err := NewAnnexBReader(bytes.NewReader(data), maxNALSize)
	if err != nil {
		return nil, err
	}
	var units []nal.Unit
	size, hasVCL := 0, false
	for {
		u, err := r.ReadNAL()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("stream: Annex-B AU封装: %w", err)
		}
		remaining := maxAccessUnitSize - size
		if remaining < prefixSize+1 || len(u.Payload) > remaining-prefixSize-1 || len(units) >= maxNALUnits {
			return nil, fmt.Errorf("stream: 输出AU或NAL数量超限: %w", h264.ErrResourceLimit)
		}
		size += len(u.Payload) + prefixSize + 1
		hasVCL = hasVCL || u.IsVCL()
		units = append(units, u)
	}
	if !hasVCL {
		return nil, fmt.Errorf("stream: Annex-B AU缺少VCL: %w", h264.ErrInvalidData)
	}
	return units, nil
}

// accessUnitOutput保留共享NAL writer，同时将违反io.Writer契约的短写立即视为失败。
// 数量越界也在进入NAL层切片前拒绝，避免错误底层Writer引发panic。
type accessUnitOutput struct{ dst io.Writer }

func (w accessUnitOutput) Write(p []byte) (int, error) {
	n, err := w.dst.Write(p)
	if n < 0 || n > len(p) {
		return 0, io.ErrShortWrite
	}
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	return n, err
}
