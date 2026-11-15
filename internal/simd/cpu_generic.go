//go:build purego || (!amd64 && !arm64)

package simd

func detectCPU() cpuFeatures { return cpuFeatures{} }
