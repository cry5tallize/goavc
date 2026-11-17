package decoder

import "github.com/cry5tallize/goavc/internal/motion"

// 整宏块 Direct/Skip 没有 ref_idx 或 MVD 语法，不构造通用分组。
// 先完成两列表的运动/身份发布，再做与通用路径相同的预测合并。
func decodeBDirect16(fs *frameState, addr, mbx, mby int, s *sliceEntry, qp *int, skip bool) error {
	info := &fs.info[addr]
	info.direct, info.allow8 = true, fs.sps.Direct8x8InferenceFlag
	direct, err := fs.deriveDirect(s, mbx, mby)
	if err != nil {
		return err
	}
	bx, by := mbx*4, fs.motionY(mby)
	for y := range 4 {
		i := (by+y)*fs.mv.BlockW + bx
		for j := range fs.directBlocks[i : i+4] {
			fs.directBlocks[i+j] = true
		}
	}
	uniform := direct.uniform()
	for l, grid := range [2]*motion.Grid{fs.mv, fs.mv1} {
		if uniform {
			r, mv := direct.refs[l][0], direct.mvs[l][0]
			grid.SetRect(bx, by, 4, 4, mv, r)
			if r >= 0 {
				fs.setReferenceIdentity(l, bx, by, 4, 4, s.refs[l][r].Identity)
			}
			continue
		}
		for i, r := range direct.refs[l] {
			x, y := bx+i%4, by+i/4
			grid.SetRect(x, y, 1, 1, direct.mvs[l][i], r)
			if r >= 0 {
				fs.setReferenceIdentity(l, x, y, 1, 1, s.refs[l][r].Identity)
			}
		}
	}
	if uniform {
		fs.mcBPartition(s, mbx, mby, pPartition{0, 0, 4, 4},
			[2]int8{direct.refs[0][0], direct.refs[1][0]}, [2]motion.MV{direct.mvs[0][0], direct.mvs[1][0]})
	} else {
		fs.predictDirectParts(s, mbx, mby, &direct)
	}
	if skip {
		return nil
	}
	return fs.interResidual(s, addr, mbx, mby, qp)
}

func (d *directMotion) uniform() bool {
	for l := range 2 {
		for i := 1; i < 16; i++ {
			if d.refs[l][i] != d.refs[l][0] || d.mvs[l][i] != d.mvs[l][0] {
				return false
			}
		}
	}
	return true
}

func (fs *frameState) predictDirectParts(s *sliceEntry, mbx, mby int, d *directMotion) {
	var parts [16]bPart
	for i := range parts {
		parts[i] = bPart{pPartition{i % 4, i / 4, 1, 1}, [2]int8{d.refs[0][i], d.refs[1][i]}, [2]motion.MV{d.mvs[0][i], d.mvs[1][i]}}
	}
	for _, p := range parts[:coalesceDirectPrediction(parts[:], 4, 4)] {
		fs.mcBPartition(s, mbx, mby, p.pPartition, p.refs, p.mvs)
	}
}
