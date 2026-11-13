package h264

import (
	"errors"
	"io"
	"testing"
)

// 哨兵错误必须是互不包含的独立值。
// 原因：内部实现大量使用 errors.Is 做分支，一旦两个哨兵重叠，
// "未支持"就可能被误判成"输入非法"，错误恢复策略随之走错路径。
func TestSentinelsAreDistinct(t *testing.T) {
	all := []error{ErrAgain, ErrEOF, ErrInvalidData, ErrUnsupported, ErrResourceLimit}
	for i := range all {
		if all[i] == nil {
			t.Fatalf("哨兵错误 %d 为 nil", i)
		}
		for j := i + 1; j < len(all); j++ {
			if errors.Is(all[i], all[j]) || errors.Is(all[j], all[i]) {
				t.Fatalf("哨兵错误 %d 与 %d 重叠", i, j)
			}
		}
	}
}

// ErrEOF 必须与 io.EOF 是同一个值，否则通用的 io 循环会漏判结束条件。
func TestErrEOFIsIOEOF(t *testing.T) {
	if ErrEOF != io.EOF {
		t.Fatalf("ErrEOF = %v，期望 io.EOF", ErrEOF)
	}
	wrapped := errors.New("wrapped: " + ErrEOF.Error())
	if errors.Is(wrapped, io.EOF) != errors.Is(wrapped, ErrEOF) {
		t.Fatal("ErrEOF 与 io.EOF 的匹配行为必须完全一致")
	}
}

// 包装之后仍然必须能被 errors.Is 命中，否则调用方无法做错误分类。
func TestSentinelsSupportWrapping(t *testing.T) {
	cause := errors.New("cause")
	if !errors.Is(errors.Join(cause, ErrUnsupported), ErrUnsupported) {
		t.Fatal("Join 之后丢失了哨兵错误")
	}
}

// IsFatal 的分类必须与文档一致：只有三类状态错误是致命的。
func TestIsFatalClassification(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"again", ErrAgain, false},
		{"eof", ErrEOF, false},
		{"invalid", ErrInvalidData, true},
		{"unsupported", ErrUnsupported, true},
		{"limit", ErrResourceLimit, true},
		{"wrapped invalid", errors.Join(errors.New("ctx"), ErrInvalidData), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsFatal(tc.err); got != tc.want {
				t.Fatalf("IsFatal(%v) = %v，期望 %v", tc.err, got, tc.want)
			}
		})
	}
}
