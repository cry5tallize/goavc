package nal

import (
	"io"
)

// AnnexBWriter 使用四字节起始码写出 NAL；Payload 应已完成 RBSP→EBSP。
// 写出失败后调用方应放弃当前流，不能重试半个 NAL。
type AnnexBWriter struct{ w io.Writer }

func NewAnnexBWriter(w io.Writer) *AnnexBWriter { return &AnnexBWriter{w: w} }

func (w *AnnexBWriter) Write(u Unit) error {
	if err := writeAll(w.w, []byte{0, 0, 0, 1, u.Header.Byte()}); err != nil {
		return err
	}
	return writeAll(w.w, u.Payload)
}
