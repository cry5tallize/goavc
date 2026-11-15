//go:build purego || !amd64

package simd

// 尚无本平台整数复制汇编时，由调用者继续执行唯一 Generic 算术。
func copy16Platform(_ []byte, _ int, _ []byte, _, _ int) bool { return false }
