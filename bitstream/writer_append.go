package bitstream

import "fmt"

// Append 将 src 的全部有效位追加到 w，不添加末字节填充，也不改变 src。
// 两者须为不同且未通过值复制共享 backing 的 Writer；否则返回错误且不写入。
// 容量充足时不分配临时副本，Bytes 的独立副本契约保持不变。
func (w *Writer) Append(src *Writer) error {
	if src == nil || w == src {
		return fmt.Errorf("bitstream: append requires a distinct source writer")
	}
	// buf仅从起点append/Reset，所有合法的共享backing都具有同一起点。
	// 此检查也覆盖目标Reset至len=0但保留cap的情况，无需unsafe或指针算术。
	if cap(src.buf) > 0 && cap(w.buf) > 0 && &src.buf[:cap(src.buf)][0] == &w.buf[:cap(w.buf)][0] {
		return fmt.Errorf("bitstream: append writers share backing storage")
	}
	if w.nbits == 0 {
		w.buf = append(w.buf, src.buf...)
		w.cur, w.nbits = src.cur, src.nbits
		return nil
	}
	for _, v := range src.buf {
		// 固定宽8且value为byte，不会触发WriteBits参数错误。
		_ = w.WriteBits(8, uint64(v))
	}
	if src.nbits != 0 {
		_ = w.WriteBits(src.nbits, uint64(src.cur>>(8-src.nbits)))
	}
	return nil
}
