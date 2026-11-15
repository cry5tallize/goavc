package simd

// 四个独立候选的最后一次求和；参数和结果均为固定值，不保留邻域指针。
func filterDeltaGeneric(a, b [4]int32) (out [4]int32) {
	for i := range out {
		out[i] = a[i] + b[i]
	}
	return
}
