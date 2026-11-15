//go:build amd64 && !purego

package simd

//go:noescape
func forward4RowsAVX2(src [16]int32) (pairs [32]int32)

// forward4AVX2 以八个int32通道并行执行行蝶形，再执行列蝶形；
// 两轴均保持与Generic相同的int32模运算，不饱和窄化。
func forward4AVX2(src [16]int32) (dst [16]int32) {
	p := forward4RowsAVX2(src)
	var temp [16]int32
	for row := range 4 {
		s, d := row*4, 8+row*4
		if row >= 2 {
			s, d = 16+(row-2)*4, 24+(row-2)*4
		}
		a, b, c, e := p[s], p[s+1], p[d], p[d+1]
		temp[row*4], temp[row*4+1], temp[row*4+2], temp[row*4+3] = a+b, 2*c+e, a-b, c-2*e
	}
	for x := range 4 {
		a, b := temp[x]+temp[12+x], temp[4+x]+temp[8+x]
		c, d := temp[x]-temp[12+x], temp[4+x]-temp[8+x]
		dst[x], dst[4+x], dst[8+x], dst[12+x] = a+b, 2*c+d, a-b, c-2*d
	}
	return
}

func forward8SSE2(src [64]int32) [64]int32 { return forward8Vectorized(src, quantMultiplySSE2) }
func forward8AVX2(src [64]int32) [64]int32 { return forward8Vectorized(src, quantMultiplyAVX2) }

func forwardChromaDCSSE2(src [4]int32) (dst [4]int32) {
	p := forwardChromaDCPairsSSE2(src)
	return [4]int32{p[0] + p[2], p[4] + p[6], p[0] - p[2], p[4] - p[6]}
}

func forwardChromaDCAVX2(src [4]int32) [4]int32 { return forwardChromaDCVectorized(src, wideAdd4AVX2) }

func idct4SSE2(src [16]int32) [16]int32         { return idct4Vectorized(src, wideAdd4SSE2) }
func idct4AVX2(src [16]int32) [16]int32         { return idct4Vectorized(src, wideAdd4AVX2) }
func idct8SSE2(src [64]int32) ([64]int32, int)  { return idct8Vectorized(src, wideAdd4SSE2) }
func idct8AVX2(src [64]int32) ([64]int32, int)  { return idct8Vectorized(src, wideAdd4AVX2) }
func forwardLumaDCSSE2(src [16]int32) [16]int32 { return forwardLumaDCVectorized(src, wideAdd4SSE2) }
func forwardLumaDCAVX2(src [16]int32) [16]int32 { return forwardLumaDCVectorized(src, wideAdd4AVX2) }
func forwardChromaDC422SSE2(src [8]int32) [8]int64 {
	return forwardChromaDC422Vectorized(src, wideAdd4SSE2)
}
func forwardChromaDC422AVX2(src [8]int32) [8]int64 {
	return forwardChromaDC422Vectorized(src, wideAdd4AVX2)
}
