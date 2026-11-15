//go:build arm64 && !purego

package simd

func forward8NEON(src [64]int32) [64]int32 { return forward8Vectorized(src, quantMultiplyNEON) }

func idct4NEON(src [16]int32) [16]int32         { return idct4Vectorized(src, wideAdd4NEON) }
func idct8NEON(src [64]int32) ([64]int32, int)  { return idct8Vectorized(src, wideAdd4NEON) }
func forwardLumaDCNEON(src [16]int32) [16]int32 { return forwardLumaDCVectorized(src, wideAdd4NEON) }
func forwardChromaDCNEON(src [4]int32) [4]int32 { return forwardChromaDCVectorized(src, wideAdd4NEON) }
func forwardChromaDC422NEON(src [8]int32) [8]int64 {
	return forwardChromaDC422Vectorized(src, wideAdd4NEON)
}
