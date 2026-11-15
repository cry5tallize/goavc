package simd

// QuantizeWithDeadZone 执行 H.264 量化的有符号死区和舍入。
// divisor 为3（帧内）或6（帧间）；调用者负责提供合法乘子与移位。
func QuantizeWithDeadZone(v int32, mul int64, shift int, divisor int64) int32 {
	a := int64(v)
	if a < 0 {
		a = -a
	}
	q := int32((a*mul + (int64(1)<<shift)/divisor) >> shift)
	if v < 0 {
		return -q
	}
	return q
}
