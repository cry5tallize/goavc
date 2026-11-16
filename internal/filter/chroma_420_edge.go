package filter

import "github.com/cry5tallize/goavc/internal/simd"

// Chroma4208 处理逐行8-bit 420整边。调用方负责格式与逐行条件；
// 其他色度几何继续使用Chroma8，旧入口不增加邻域或padding要求。
func (e EdgeParameters) Chroma4208(data []byte, q0, lineStep, acrossStep int, strengths [4]uint8) {
	if e.alpha == 0 || e.beta == 0 || strengths == [4]uint8{} {
		return
	}
	if e.bitDepth == 8 && simd.ActiveBackends().FilterChromaEdge == simd.SSE2 && chroma420EdgeHasCore(len(data), q0, lineStep, acrossStep) {
		simd.FilterChroma8Edge(data, q0, lineStep, acrossStep, strengths, e.alpha, e.beta, e.tc)
		return
	}
	simd.FilterChroma8EdgeScalar(data, q0, lineStep, acrossStep, strengths, e.alpha, e.beta, e.tc, e.bitDepth)
}

// 除法先于乘法，避免不可信下标或步长在邻域检查中溢出。
func chroma420EdgeHasCore(size, q0, lineStep, acrossStep int) bool {
	if q0 < 0 || q0 >= size || lineStep <= 0 || acrossStep <= 0 {
		return false
	}
	if !(acrossStep == 1 && lineStep >= 8 || lineStep == 1 && acrossStep >= 8) {
		return false
	}
	if lineStep > (size-1-q0)/7 || acrossStep > q0/2 {
		return false
	}
	return acrossStep <= size-1-(q0+7*lineStep)
}
