package encoder

import "math/bits"

// lambdaQ8 是约 0.85*2^((QP-12)/3) 的 Q8 定点近似。
// 固定整数基值与位移避免不同平台浮点近似改变接近阈值的模式选择。
func lambdaQ8(qp int) uint64 {
	exponent, remainder := (qp-12)/3, (qp-12)%3
	if remainder < 0 {
		remainder += 3
		exponent--
	}
	value := [3]uint64{218, 274, 345}[remainder]
	if exponent < 0 {
		return value >> (-exponent)
	}
	return value << exponent
}

func rdCost(qp int, distortion, rate uint64) uint64 { return distortion*256 + lambdaQ8(qp)*rate }

func ueBits(value uint64) int { return 2*bits.Len64(value+1) - 1 }
func seBits(value int32) int {
	v := int64(value)
	var code uint64
	if v <= 0 {
		code = uint64(-2 * v)
	} else {
		code = uint64(2*v - 1)
	}
	return ueBits(code)
}

func mvBits(x, y int32) int { return seBits(x) + seBits(y) }

// motionLambdaQ8 为 SAD 阶段使用 sqrt(lambda)，通过整数平方根保持确定性。
func motionLambdaQ8(qp int) uint64 {
	n := lambdaQ8(qp) * 256
	var result uint64
	for bit := uint64(1) << 62; bit != 0; bit >>= 2 {
		if n >= result+bit {
			n -= result + bit
			result = (result >> 1) + bit
		} else {
			result >>= 1
		}
	}
	return result
}
