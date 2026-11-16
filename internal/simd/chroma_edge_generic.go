package simd

// FilterChroma8Edge 处理四段、每段两条线的8-bit色度边（Spec: 8.7.2.4）。
// 垂直边要求acrossStep=1、lineStep>=4；水平边要求lineStep=1、acrossStep>=8。
// 调用方验证八条线的p1..q1邻域；tc传原始tc0，内核增加色度的+1。
// alpha/beta为8-bit表阈值，tc0为0..25；不保存像素引用。
func FilterChroma8Edge(data []byte, q0, lineStep, acrossStep int, strengths [4]uint8, alpha, beta int, tc [4]int) {
	filterChroma8EdgeNative(data, q0, lineStep, acrossStep, strengths, alpha, beta, tc)
}

func filterChroma8EdgeGeneric(data []byte, q0, lineStep, acrossStep int, strengths [4]uint8, alpha, beta int, tc [4]int) {
	FilterChroma8EdgeScalar(data, q0, lineStep, acrossStep, strengths, alpha, beta, tc, 8)
}

// FilterChroma8EdgeScalar 保留旧逐段访问规则和位深裁剪；直接命名调用标量公式，
// 不经函数值或跨包泛型字典，使领域栈上小缓冲保持不逃逸。
func FilterChroma8EdgeScalar(data []byte, q0, lineStep, acrossStep int, strengths [4]uint8, alpha, beta int, tc [4]int, bitDepth int) {
	if alpha <= 0 || beta <= 0 {
		return
	}
	for seg, bs := range strengths {
		if bs == 0 {
			continue
		}
		limit := 0
		if bs < 4 {
			limit = tc[bs] + 1
		}
		filterChroma(data, q0+seg*2*lineStep, lineStep, acrossStep, int(bs), alpha, beta, limit, 2, bitDepth, nil)
	}
}
