//go:build purego || (!amd64 && !arm64)

package simd

func installTransformKernels(_ cpuFeatures, table kernelTable) kernelTable { return table }
