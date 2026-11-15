//go:build (!amd64 && !arm64) || purego

package simd

func installResidualKernels(_ cpuFeatures, t kernelTable) kernelTable { return t }
