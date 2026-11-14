package picture

// AddResidual4x4 将共享逆变换得到的残差加到预测块并执行 Clip1。
// 调用方必须保证 4×4 区域位于编码图像内；int64 避免极端残差加法回绕。
// 按平面位深选择采样类型，8-bit热路径不做逐像素位深分支或额外转换。
func AddResidual4x4(p *Plane, x, y int, residual *[16]int32) {
	if p.BitDepth == 10 {
		addResidual4x4(p.Data16, p.Index(x, y), p.Stride, residual, 1023)
	} else {
		addResidual4x4(p.Data, p.Index(x, y), p.Stride, residual, 255)
	}
}

func addResidual4x4[T ~uint8 | ~uint16](data []T, off, stride int, residual *[16]int32, limit int64) {
	for j := range 4 {
		for i := range 4 {
			index := off + j*stride + i
			v := int64(data[index]) + int64(residual[j*4+i])
			data[index] = T(max(0, min(limit, v)))
		}
	}
}
