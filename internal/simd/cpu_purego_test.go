//go:build purego || (!amd64 && !arm64)

package simd

import "testing"

func TestCPU_PureGoHasNoAssemblySelection(t *testing.T) {
	if nativeCPU != (cpuFeatures{}) {
		t.Fatalf("Generic平台错误启用汇编能力：%+v", nativeCPU)
	}
}
