package picture

// AddResidual4x4 将共享逆变换得到的残差加到预测块并执行 Clip1。
// 调用方必须保证 4×4 区域位于编码图像内；int64 避免极端残差加法回绕。
// 按平面位深选择采样类型，8-bit热路径不做逐像素位深分支或额外转换。
func AddResidual4x4(p *Plane, x, y int, residual *[16]int32) {
	if p.BitDepth == 10 {
		addResidual(p.Data16, p.Index(x, y), p.Stride, residual[:], 4, 1023)
	} else {
		addResidual(p.Data, p.Index(x, y), p.Stride, residual[:], 4, 255)
	}
}

// AddResidual8x8 与4×4共享Clip1算法，不拆块或复制变换输出。
func AddResidual8x8(p *Plane, x, y int, residual *[64]int32) {
	if p.BitDepth == 10 {
		addResidual(p.Data16, p.Index(x, y), p.Stride, residual[:], 8, 1023)
	} else {
		addResidual(p.Data, p.Index(x, y), p.Stride, residual[:], 8, 255)
	}
}

func addResidual[T ~uint8 | ~uint16](data []T, off, stride int, residual []int32, n int, limit int64) {
	for j := range n {
		for i := range n {
			index := off + j*stride + i
			v := int64(data[index]) + int64(residual[j*n+i])
			data[index] = T(max(0, min(limit, v)))
		}
	}
}
