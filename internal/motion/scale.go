package motion

// DistanceScaleFactor 实现 Spec: 8.4.1.2 的 POC 时间距离比例，供 temporal
// direct 和隐式加权共用。长期参考的特殊处理由各自语义调用方完成。
func DistanceScaleFactor(poc, poc0, poc1 int32) int32 {
	td := max(int64(-128), min(int64(127), int64(poc1)-int64(poc0)))
	if td == 0 {
		return 256
	}
	tb := max(int64(-128), min(int64(127), int64(poc)-int64(poc0)))
	half := td / 2
	if half < 0 {
		half = -half
	}
	tx := (16384 + half) / td
	return int32(max(int64(-1024), min(int64(1023), (tb*tx+32)>>6)))
}
