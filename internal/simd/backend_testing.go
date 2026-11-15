//go:build goavc_simdtest

package simd

import (
	"errors"
	"sync"
)

// ErrBackendUnavailable 表示指定的测试后端未实现或当前CPU不能安全执行。
var ErrBackendUnavailable = errors.New("simd：测试后端不可用")

var backendTestMu sync.Mutex

// ForceBackendForTesting 仅存在于测试标签。所有调用者须串行使用，且切换/恢复
// 前join全部worker；不能与t.Parallel或并发benchmark混用。生产构建不含此入口。
func ForceBackendForTesting(backend Backend) (restore func(), err error) {
	backendTestMu.Lock()
	table, err := testBackendTable(backend)
	if err != nil {
		backendTestMu.Unlock()
		return nil, err
	}
	previous := kernels
	kernels = table
	var once sync.Once
	return func() { once.Do(func() { kernels = previous; backendTestMu.Unlock() }) }, nil
}

func testBackendTable(backend Backend) (kernelTable, error) {
	if backend == Generic {
		return genericTable(), nil
	}
	// 只有交付真实计算后端后才能扩展；CPU支持不能代替实现。
	return kernelTable{}, ErrBackendUnavailable
}
