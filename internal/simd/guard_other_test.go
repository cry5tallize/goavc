//go:build !windows

package simd

import "testing"

func TestGuardPage_SAD(t *testing.T) {
	t.Skip("当前保护页辅助只实现Windows；本平台动态越读测试未执行，静态访问证明和最小len parity仍保留")
}
