//go:build purego || (!amd64 && !arm64)

package simd

func platformKernels(_ cpuFeatures, table kernelTable) kernelTable { return table }
