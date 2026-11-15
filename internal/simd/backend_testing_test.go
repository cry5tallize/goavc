//go:build goavc_simdtest

package simd

import (
	"errors"
	"testing"
)

func TestParity_ForcedGeneric(t *testing.T) {
	restore, err := ForceBackendForTesting(Generic)
	if err != nil {
		t.Fatal(err)
	}
	defer restore()
	checkSADParity(t, kernels)
	TestAllocation_SAD(t)
}

func TestParity_RejectUnavailableBackend(t *testing.T) {
	if restore, err := ForceBackendForTesting(Backend(255)); !errors.Is(err, ErrBackendUnavailable) || restore != nil {
		t.Fatalf("非法后端选择：restore=%v err=%v", restore != nil, err)
	}
	// 错误必须释放测试锁，后续有效选择和幂等恢复仍可完成。
	restore, err := ForceBackendForTesting(Generic)
	if err != nil {
		t.Fatal(err)
	}
	restore()
	restore()
}
