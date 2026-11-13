package prediction

import "github.com/goavc/h264/internal/motion"

// WeightSample 实现 Spec: 8.4.3.2 的单向显式加权，先按分母舍入，再加
// offset 并 Clip1。调用方只传入已完成插值和裁剪的 8-bit 样本。
func WeightSample(sample byte, weight, offset int32, denom uint32) byte {
	round := int64(0)
	if denom > 0 {
		round = int64(1) << (denom - 1)
	}
	return clip8(int(((int64(sample)*int64(weight) + round) >> denom) + int64(offset)))
}

func BiWeightSample(a, b byte, w0, w1, o0, o1 int32, denom uint32) byte {
	sum := int64(a)*int64(w0) + int64(b)*int64(w1) + (int64(1) << denom)
	return clip8(int((sum >> (denom + 1)) + ((int64(o0) + int64(o1) + 1) >> 1)))
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
