package simd

func wideAdd4Generic(a, b [4]int64) (out [4]int64) {
	for i := range out {
		out[i] = a[i] + b[i]
	}
	return
}
