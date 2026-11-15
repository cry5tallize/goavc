package prediction

import "github.com/cry5tallize/goavc/internal/motion"

// WeightSample 实现 Spec: 8.4.3.2 的单向显式加权，先按分母舍入，再加
// offset 并 Clip1。调用方只传入已完成插值和裁剪的 8-bit 样本。
func WeightSample(sample byte, weight, offset int32, denom uint32) byte {
	return weightSample(sample, weight, offset, denom, 8)
}

// WeightSampleHigh 的 offset 保持码流的 8-bit 标度，在此乘 2^(bitDepth-8)。
func WeightSampleHigh(sample uint16, weight, offset int32, denom uint32, bitDepth int) uint16 {
	return weightSample(sample, weight, offset, denom, bitDepth)
}

func weightSample[T sample](value T, weight, offset int32, denom uint32, bitDepth int) T {
	round := int64(0)
	if denom > 0 {
		round = int64(1) << (denom - 1)
	}
	scaledOffset := int64(offset) << (bitDepth - 8)
	return clipSample[T](int(((int64(value)*int64(weight)+round)>>denom)+scaledOffset), bitDepth)
}

func BiWeightSample(a, b byte, w0, w1, o0, o1 int32, denom uint32) byte {
	return biWeightSample(a, b, w0, w1, o0, o1, denom, 8)
}

// BiWeightSampleHigh 同时服务显式权重与 denom=5、offset=0 的隐式权重。
func BiWeightSampleHigh(a, b uint16, w0, w1, o0, o1 int32, denom uint32, bitDepth int) uint16 {
	return biWeightSample(a, b, w0, w1, o0, o1, denom, bitDepth)
}

func biWeightSample[T sample](a, b T, w0, w1, o0, o1 int32, denom uint32, bitDepth int) T {
	sum := int64(a)*int64(w0) + int64(b)*int64(w1) + (int64(1) << denom)
	// 必须先缩放两个 offset 再求平均；先平均再缩放会损失半个 offset。
	offset := (((int64(o0) + int64(o1)) << (bitDepth - 8)) + 1) >> 1
	return clipSample[T](int((sum>>(denom+1))+offset), bitDepth)
}

// ImplicitWeights 的权重总和为 64，配合 BiWeightSample 的 denom=5。
// 长期参考或等 POC 使用均值；越过规定缩放区间也回到均值。
func ImplicitWeights(poc, poc0, poc1 int32, long0, long1 bool) (int32, int32) {
	if long0 || long1 {
		return 32, 32
	}
	if poc0 == poc1 {
		return 32, 32
	}
	scale := motion.DistanceScaleFactor(poc, poc0, poc1)
	w1 := int32(scale >> 2)
	if w1 < -64 || w1 > 128 {
		return 32, 32
	}
	return 64 - w1, w1
}
