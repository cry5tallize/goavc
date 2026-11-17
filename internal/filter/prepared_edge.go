package filter

import "github.com/cry5tallize/goavc/internal/simd"

// EdgeParameters 保存同一条边四个4×4段共用的阈值（Spec: 8.7.2）。
// 不保存像素或宏块引用；色度QP必须由调用方先逐侧映射再平均，Cb/Cr分别准备。
type EdgeParameters struct {
	alpha, beta int
	tc          [4]int
	bitDepth    int
}

// PrepareEdge 将QP和slice偏移转换为原生位深阈值。零值及零阈值均不访问像素。
// tc只保存按位深缩放的tc0；色度弱滤波的附加量+1在使用时加入，不随位深缩放。
func PrepareEdge(qp, alphaDiv2, betaDiv2, bitDepth int) EdgeParameters {
	a, b := qp+52+2*alphaDiv2, qp+52+2*betaDiv2
	if a < 0 || a >= len(alphaTable) || b < 0 || b >= len(betaTable) {
		return EdgeParameters{}
	}
	scale := 1 << (bitDepth - 8)
	return EdgeParameters{alpha: int(alphaTable[a]) * scale, beta: int(betaTable[b]) * scale,
		tc: [4]int{0, int(tc0Table[a][1]) * scale, int(tc0Table[a][2]) * scale, int(tc0Table[a][3]) * scale}, bitDepth: bitDepth}
}

// Luma8 使用整边内核处理满足完整邻域条件的16线边，其余保留逐段标量路径。
func (e EdgeParameters) Luma8(data []byte, q0, lineStep, acrossStep int, strengths [4]uint8) {
	if e.alpha == 0 || e.beta == 0 || strengths == [4]uint8{} {
		return
	}
	if e.bitDepth == 8 && luma8EdgeHasFullCore(data, q0, lineStep, acrossStep) &&
		simd.TryFilterLuma8Edge(data, q0, lineStep, acrossStep, strengths, e.alpha, e.beta, e.tc) {
		return
	}
	for seg, bs := range strengths {
		if bs == 0 {
			continue
		}
		tc := 0
		if bs < 4 {
			tc = e.tc[bs]
		}
		simd.FilterLuma8(data, q0+seg*4*lineStep, lineStep, acrossStep, int(bs), e.alpha, e.beta, tc, 4, e.bitDepth)
	}
}

// p2/q2必须覆盖整边；汇编核对p3/q3逐线作条件访问。
func luma8EdgeHasFullCore(data []byte, q0, lineStep, acrossStep int) bool {
	if q0 < 0 || q0 >= len(data) || lineStep <= 0 || acrossStep <= 0 {
		return false
	}
	if !(acrossStep == 1 && lineStep >= 16 || lineStep == 1 && acrossStep >= 16) {
		return false
	}
	if lineStep > (len(data)-1-q0)/15 || acrossStep > q0/3 {
		return false
	}
	lastQ0 := q0 + 15*lineStep
	return acrossStep <= (len(data)-1-lastQ0)/2
}

// Luma16 与Luma8相同，步长以uint16采样计。
func (e EdgeParameters) Luma16(data []uint16, q0, lineStep, acrossStep int, strengths [4]uint8) {
	if e.alpha == 0 || e.beta == 0 {
		return
	}
	for seg, bs := range strengths {
		if bs == 0 {
			continue
		}
		tc := 0
		if bs < 4 {
			tc = e.tc[bs]
		}
		simd.FilterLuma16(data, q0+seg*4*lineStep, lineStep, acrossStep, int(bs), e.alpha, e.beta, tc, 4, e.bitDepth)
	}
}

// Chroma8 的lines为每个亮度4×4段对应的色度跨度：420为2，422按方向为2或4。
func (e EdgeParameters) Chroma8(data []byte, q0, lineStep, acrossStep int, strengths [4]uint8, lines int) {
	if e.alpha == 0 || e.beta == 0 {
		return
	}
	for seg, bs := range strengths {
		if bs == 0 {
			continue
		}
		tc := 0
		if bs < 4 {
			tc = e.tc[bs] + 1
		}
		simd.FilterChroma8(data, q0+seg*lines*lineStep, lineStep, acrossStep, int(bs), e.alpha, e.beta, tc, lines, e.bitDepth)
	}
}

// Chroma16 保留10-bit原生样本和色度未缩放的+1修正。
func (e EdgeParameters) Chroma16(data []uint16, q0, lineStep, acrossStep int, strengths [4]uint8, lines int) {
	if e.alpha == 0 || e.beta == 0 {
		return
	}
	for seg, bs := range strengths {
		if bs == 0 {
			continue
		}
		tc := 0
		if bs < 4 {
			tc = e.tc[bs] + 1
		}
		simd.FilterChroma16(data, q0+seg*lines*lineStep, lineStep, acrossStep, int(bs), e.alpha, e.beta, tc, lines, e.bitDepth)
	}
}
