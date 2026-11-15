//go:build arm64 && !purego

package simd

// 当前支持的 Linux/Windows/macOS arm64 只使用基线 Advanced SIMD，
// 不据此假定 SVE、可选密码扩展等能力，也不读取 Linux 专属辅助向量。
func detectCPU() cpuFeatures { return cpuFeatures{neon: true} }
