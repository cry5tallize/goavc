package decoder

// prepareDeblockNZ 在宏块成功后派生去块掩码，不修改 CAVLC/CABAC 的原计数。
// MBAFF 保留逐块几何；可能失败的整 slice 不会发布去块帧。
func (fs *frameState) prepareDeblockNZ(addr int) {
	if fs.mbaff || fs.field {
		return
	}
	m := &fs.info[addr]
	if m.intra || m.pcm || m.skip {
		m.deblockNZ, m.deblockNZReady = 0, true
		return
	}
	nz := fs.lumaNZ[addr*16:][:16]
	var mask uint16
	if m.transform8 {
		for _, i := range [...]int{0, 2, 8, 10} {
			if nz[i]|nz[i+1]|nz[i+4]|nz[i+5] != 0 {
				mask |= 0x33 << i
			}
		}
	} else {
		for i, n := range nz {
			if n != 0 {
				mask |= 1 << i
			}
		}
	}
	m.deblockNZ, m.deblockNZReady = mask, true
}

// preparedEdgeStrengths 使用两个已提交宏块的掩码，不预取四段运动元数据。
func (fs *frameState) preparedEdgeStrengths(mbx, mby, edge int, vertical bool, pMask, qMask uint16, uniform bool) (strengths [4]uint8) {
	local := edge / 4
	pCol := (local + 3) & 3
	p, q, stride := pCol, local, 4
	px, py := mbx*4+local-1, mby*4
	qx, qy := px+1, py
	if !vertical {
		p, q, stride = pCol*4, local*4, 1
		px, py = mbx*4, mby*4+local-1
		qx, qy = px, py+1
	}
	for seg := range 4 {
		if (pMask>>p|qMask>>q)&1 != 0 {
			strengths[seg] = 2
		} else if !uniform && fs.mv1 != nil {
			strengths[seg] = uint8(fs.preparedBStrength(px, py, qx, qy))
		} else if !uniform {
			strengths[seg] = uint8(fs.pMotionStrength(px, py, qx, qy))
		}
		p += stride
		q += stride
		if vertical {
			py++
			qy++
		} else {
			px++
			qx++
		}
	}
	return
}

// 对齐参考身份后才读参与比较的列表；nil 项与旧规则一样不比较 MV。
// Get 仍负责有效性与 slice 权限（不可用项保留旧零 MV 语义）。
func (fs *frameState) preparedBStrength(px, py, qx, qy int) int {
	w := fs.mv.BlockW
	p, q := py*w+px, qy*w+qx
	p0, p1 := fs.referenceIDs[0][p], fs.referenceIDs[1][p]
	q0, q1 := fs.referenceIDs[0][q], fs.referenceIDs[1][q]
	same, swapped := p0 == q0 && p1 == q1, p0 == q1 && p1 == q0
	if !same && !swapped {
		return 1
	}
	var ax, ay, bx, by, cx, cy, dx, dy int32
	if p0 != nil || q0 != nil {
		a, _, _ := fs.mv.Get(px, py)
		b, _, _ := fs.mv.Get(qx, qy)
		ax, ay, bx, by = a.X, a.Y, b.X, b.Y
	}
	if p1 != nil || q1 != nil {
		c, _, _ := fs.mv1.Get(px, py)
		d, _, _ := fs.mv1.Get(qx, qy)
		cx, cy, dx, dy = c.X, c.Y, d.X, d.Y
	}
	if same && (p0 == nil || closeDeblockMV(ax, ay, bx, by)) && (p1 == nil || closeDeblockMV(cx, cy, dx, dy)) ||
		swapped && (p0 == nil || closeDeblockMV(ax, ay, dx, dy)) && (p1 == nil || closeDeblockMV(cx, cy, bx, by)) {
		return 0
	}
	return 1
}

func closeDeblockMV(ax, ay, bx, by int32) bool {
	x, y := ax-bx, ay-by
	return x > -4 && x < 4 && y > -4 && y < 4
}
