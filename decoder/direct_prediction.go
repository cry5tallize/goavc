package decoder

// coalesceDirectPrediction 仅合并已经推导完成的像素预测任务（Spec: 8.4）。
// 输入为一组光栅排列的4×4块；参考索引和两列表MV均相等才可合并。
// 此时运动网格、MVD、参考身份和direct标记已按原粒度写入，不能随任务合并。
func coalesceDirectPrediction(parts []bPart, w, h int) int {
	first := parts[0]
	uniform := true
	for _, p := range parts[1:] {
		if p.refs != first.refs || p.mvs != first.mvs {
			uniform = false
			break
		}
	}
	if uniform {
		parts[0].w, parts[0].h = w, h
		return 1
	}
	var out [16]bPart
	used := uint16(0)
	n := 0
	for y := range h {
		for x := range w {
			i := y*w + x
			if used&(1<<i) != 0 {
				continue
			}
			p := parts[i]
			width := 1
			for x+width < w {
				j := i + width
				if used&(1<<j) != 0 || parts[j].refs != p.refs || parts[j].mvs != p.mvs {
					break
				}
				width++
			}
			height := 1
		rows:
			for y+height < h {
				for dx := range width {
					j := (y+height)*w + x + dx
					if used&(1<<j) != 0 || parts[j].refs != p.refs || parts[j].mvs != p.mvs {
						break rows
					}
				}
				height++
			}
			for dy := range height {
				for dx := range width {
					used |= 1 << ((y+dy)*w + x + dx)
				}
			}
			p.w, p.h = width, height
			out[n] = p
			n++
		}
	}
	copy(parts, out[:n])
	return n
}
