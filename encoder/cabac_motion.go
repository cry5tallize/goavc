package encoder

import "github.com/goavc/h264/internal/motion"

// motion 写出单参考 P 分区；MVD 按图像 4×4 光栅布局存储。
func (c *cabacWriter) motion(s *frameState, addr int) {
	i := &s.info[addr]
	if i.partition == 3 {
		for range 4 {
			c.bin(21, 1)
		}
	}
	x, y := addr%s.pic.MBWidth*4, addr/s.pic.MBWidth*4
	var actual [4]motion.MV
	for k, p := range partitions[i.partition] {
		actual[k], _, _ = s.mv.Get(x+p.x/4, y+p.y/4)
	}
	s.clearMotion(addr)
	for k, p := range partitions[i.partition] {
		bx, by := x+p.x/4, y+p.y/4
		pred := s.predictPartition(addr, i.partition, k)
		diff := motion.MV{X: actual[k].X - pred.X, Y: actual[k].Y - pred.Y}
		c.mvd(s, bx, by, 0, diff.X)
		c.mvd(s, bx, by, 1, diff.Y)
		s.mv.SetRect(bx, by, p.w/4, p.h/4, actual[k], 0)
		for j := 0; j < p.h/4; j++ {
			for col := 0; col < p.w/4; col++ {
				s.mvd[(by+j)*s.mv.BlockW+bx+col] = diff
			}
		}
	}
}

// Spec: 9.3.3，UEG3 的前九级使用上下文，其余使用 bypass 指数前缀。
func (c *cabacWriter) mvd(s *frameState, bx, by, component int, value int32) {
	sum := 0
	for _, p := range [][2]int{{bx - 1, by}, {bx, by - 1}} {
		if s.mv.Available(p[0], p[1]) {
			m := s.mvd[p[1]*s.mv.BlockW+p[0]]
			v := m.X
			if component == 1 {
				v = m.Y
			}
			if v < 0 {
				v = -v
			}
			sum += int(v)
		}
	}
	base := 40 + 7*component
	v := int(value)
	if v < 0 {
		v = -v
	}
	c.bin(base+flag(sum > 2)+flag(sum > 32), flag(v != 0))
	if v == 0 {
		return
	}
	inc := base + 3
	for magnitude := 1; magnitude < 9; magnitude++ {
		c.bin(inc, flag(v > magnitude))
		if v == magnitude {
			break
		}
		if magnitude < 4 {
			inc++
		}
	}
	if v >= 9 {
		rest, k := v-9, 3
		for rest >= 1<<k {
			c.e.Bypass(1)
			rest -= 1 << k
			k++
		}
		c.e.Bypass(0)
		for j := k - 1; j >= 0; j-- {
			c.e.Bypass(uint8((rest >> j) & 1))
		}
	}
	c.e.Bypass(uint8(flag(value < 0)))
}
