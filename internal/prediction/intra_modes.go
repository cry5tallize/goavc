package prediction

// Intra4x4ModeAvailable 根据 Spec: 8.3.1 的参考样本集合判断模式能否编码。
// 右上不可用时按规范复制最右顶部样本，不影响模式 3/7 的可用性。
func Intra4x4ModeAvailable(mode int, top, left, topLeft bool) bool {
	switch mode {
	case 0, 3, 7:
		return top
	case 1, 8:
		return left
	case 2:
		return true
	case 4, 5, 6:
		return top && left && topLeft
	}
	return false
}

// MostProbableIntra4x4 返回规范 MPM；任一邻块不可用（负数）时固定为 DC。
// I16/PCM 邻块由调用方表示为 DC，而不是不可用。
func MostProbableIntra4x4(left, top int) int {
	if left < 0 || top < 0 {
		return 2
	}
	return min(left, top)
}
