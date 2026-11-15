//go:build amd64 && !purego

package simd

func detectCPU() cpuFeatures { return x86Features(readCPUID, readXCR0) }

// 仅访问寄存器，不读写 Go 对象；ABI0 返回布局与汇编逐字段对应。
func readCPUID(leaf, subleaf uint32) (a, b, c, d uint32)
func readXCR0() uint64
