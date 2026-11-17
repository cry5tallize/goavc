package simd

func mcCombine[T ~byte | ~uint16](dst []T, ds int, a []T, as int, b []T, bs, w, h int, average bool) {
	if d, ok := any(dst).([]byte); ok {
		mcCombine8Native(d, ds, any(a).([]byte), as, any(b).([]byte), bs, w, h, average)
		return
	}
	mcCombineGeneric(dst, ds, a, as, b, bs, w, h, average)
}

func mcCombineGeneric[T ~byte | ~uint16](dst []T, ds int, a []T, as int, b []T, bs, w, h int, average bool) {
	for y := range h {
		for x := range w {
			v := a[y*as+x]
			if b != nil {
				v = T((int(v) + int(b[y*bs+x]) + 1) >> 1)
			}
			if average {
				v = T((int(dst[y*ds+x]) + int(v) + 1) >> 1)
			}
			dst[y*ds+x] = v
		}
	}
}
