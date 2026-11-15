//go:build purego || (!amd64 && !arm64)

package simd

func installQuantKernels(_ cpuFeatures, t kernelTable) kernelTable { return t }
