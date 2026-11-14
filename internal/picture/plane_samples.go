package picture

// Sample 读取8/10-bit采样；调用方保证平面布局已经验证，坐标含合法边界区。
// 8-bit核心仍可使用无位深分支的Pix，不为既有内核增加逐像素转换。
func (p *Plane) Sample(x, y int) uint16 {
	if p.BitDepth == 10 {
		return p.Data16[p.Index(x, y)]
	}
	return uint16(p.Pix(x, y))
}

// SetSample 写入采样，不作Clip1；调用方保证值在0..(1<<BitDepth)-1内。
// 残差重建必须使用AddResidual4x4执行位深相关裁剪。
func (p *Plane) SetSample(x, y int, value uint16) {
	if p.BitDepth == 10 {
		p.Data16[p.Index(x, y)] = value
	} else {
		p.SetPix(x, y, byte(value))
	}
}

// Row16 返回10-bit平面第y行有效采样，与Data16共享底层数组。
// 仅供已验证的10-bit平面调用，不包含左右边界或行尾填充。
func (p *Plane) Row16(y int) []uint16 {
	off := p.Index(0, y)
	return p.Data16[off : off+p.Width]
}

// fillBorder 是两种采样宽度共用的边缘复制内核。泛型按采样类型实例化，
// 不分配临时行，不修改行尾对齐填充；Spec: 8.4.2.1。
func fillBorder[T ~uint8 | ~uint16](data []T, stride, width, height, border int) {
	if width <= 0 || height <= 0 {
		return
	}
	for y := range height {
		row := (y + border) * stride
		left, right := data[row+border], data[row+border+width-1]
		for x := range border {
			data[row+x] = left
			data[row+border+width+x] = right
		}
	}
	rowLen := width + 2*border
	first := border * stride
	last := (border + height - 1) * stride
	for y := range border {
		top := y * stride
		bottom := (border + height + y) * stride
		copy(data[top:top+rowLen], data[first:first+rowLen])
		copy(data[bottom:bottom+rowLen], data[last:last+rowLen])
	}
}
