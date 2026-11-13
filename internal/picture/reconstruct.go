package picture

// AddResidual4x4 将共享逆变换得到的残差加到预测块并执行 Clip1。
// 调用方必须保证 4×4 区域位于编码图像内；int64 避免极端残差加法回绕。
func AddResidual4x4(p *Plane, x, y int, residual *[16]int32) {
	for j := 0; j < 4; j++ {
		for i := 0; i < 4; i++ {
			v := int64(p.Pix(x+i, y+j)) + int64(residual[j*4+i])
			p.SetPix(x+i, y+j, byte(max(0, min(255, v))))
		}
	}
}
