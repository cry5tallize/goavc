//go:build !windows && !linux && !darwin && !freebsd && !netbsd && !openbsd && !dragonfly

package simd

import "testing"

const guardPagesAvailable = false

func guardedRows(t *testing.T, _, _, _ int) ([]byte, int) {
	t.Helper()
	t.Skip("本平台保护页辅助未实现；动态越读未执行，最小len及静态范围证明仍保留")
	return nil, 0
}
