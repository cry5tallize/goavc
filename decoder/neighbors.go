package decoder

import "github.com/cry5tallize/goavc/internal/prediction"

// neighborAvailable 实现 Spec: 6.4.8 的宏块可用性：跨 slice 的邻块
// 即使在同一帧内已经重建，也不能用于 CAVLC 或预测上下文。
func (fs *frameState) neighborAvailable(addr, other int) bool {
	return other >= 0 && other < fs.total && fs.decoded[other] &&
		fs.info[addr].sliceID == fs.info[other].sliceID
}

func (fs *frameState) intraNeighbor(addr, other int) bool {
	return fs.neighborAvailable(addr, other) &&
		(!fs.pps.ConstrainedIntraPredFlag || fs.info[other].intra)
}

func blockOrder(bx, by int) int {
	return (by/2*2+bx/2)*4 + by%2*2 + bx%2
}

// 邻域可用性与采样宽度无关，使用同一实现服务8/10-bit路径。
type intra4Neighborhood[T ~uint8 | ~uint16] struct {
	Top, TopRight, Left [4]T
	TopLeft             T
}

type intraBlockNeighborhood[T ~uint8 | ~uint16] struct {
	Top, Left [16]T
	TopLeft   T
}

// intra4Samples 从当前图像读取 4x4 预测邻域。
func intra4Samples[T ~uint8 | ~uint16](fs *frameState, mbx, mby, bx, by int, data []T) (intra4Neighborhood[T], bool, bool) {
	if fs.mbaff {
		return mbaffIntra4Samples(fs, mbx, mby, bx, by, data)
	}
	var s intra4Neighborhood[T]
	pl := &fs.pic.Planes[fs.component]
	x, y := mbx*16+bx*4, mby*16+by*4
	addr := fs.mbAddress(mbx, mby)
	topAvail := by > 0 || fs.intraNeighbor(addr, addr-fs.mbW)
	leftAvail := bx > 0 || (mbx > 0 && fs.intraNeighbor(addr, addr-1))
	if topAvail {
		for i := 0; i < 4; i++ {
			s.Top[i] = data[pl.Index(x+i, y-1)]
		}
		// 右上样本只有在“已经被解码”时才可用：
		//   - bx<3 时在当前 MB 内，顶行已重建；
		//   - bx==3 时位于右上方 MB，只有上一行 MB 存在且已解码才可用。
		// 否则按规范用 Top[3] 复制填充。直接读图像会读到尚未解码的
		// 右邻 MB，产生随机的预测值。
		trAvail := false
		if by == 0 {
			trAvail = bx < 3 || (mbx+1 < fs.mbW && fs.intraNeighbor(addr, addr-fs.mbW+1))
		} else if bx < 3 {
			trAvail = blockOrder(bx+1, by-1) < blockOrder(bx, by)
		}
		for i := 0; i < 4; i++ {
			if trAvail {
				s.TopRight[i] = data[pl.Index(x+4+i, y-1)]
			} else {
				s.TopRight[i] = s.Top[3]
			}
		}
		if leftAvail {
			s.TopLeft = data[pl.Index(x-1, y-1)]
		}
	}
	if leftAvail {
		for i := 0; i < 4; i++ {
			s.Left[i] = data[pl.Index(x-1, y+i)]
		}
	}
	return s, topAvail, leftAvail
}

func intraBlockSamples[T ~uint8 | ~uint16](fs *frameState, mbx, mby, component int, data []T) (intraBlockNeighborhood[T], bool, bool) {
	if fs.mbaff {
		return mbaffIntraBlockSamples(fs, mbx, mby, component, data)
	}
	var s intraBlockNeighborhood[T]
	pl := &fs.pic.Planes[component]
	w, h := 16, 16
	if component != 0 {
		w, h = fs.chromaW, fs.chromaH
	}
	x, y := mbx*w, mby*h
	addr := fs.mbAddress(mbx, mby)
	topAvail := fs.intraNeighbor(addr, addr-fs.mbW)
	leftAvail := mbx > 0 && fs.intraNeighbor(addr, addr-1)
	if topAvail {
		for i := range w {
			s.Top[i] = data[pl.Index(x+i, y-1)]
		}
	}
	if leftAvail {
		for i := range h {
			s.Left[i] = data[pl.Index(x-1, y+i)]
		}
	}
	if topAvail && leftAvail {
		s.TopLeft = data[pl.Index(x-1, y-1)]
	}
	return s, topAvail, leftAvail
}

func (fs *frameState) mostProbableMode(mbx, mby, raster int) int {
	bx, by := raster%4, raster/4
	left, top := -1, -1
	addr := fs.mbAddress(mbx, mby)
	if fs.mbaff {
		return fs.mbaffMostProbableMode(addr, bx, by)
	}
	if bx > 0 {
		if fs.info[addr].intraModes[by*4+bx-1] >= 0 {
			left = int(fs.info[addr].intraModes[by*4+bx-1])
		}
	} else if mbx > 0 && fs.intraNeighbor(addr, addr-1) {
		left = fs.neighborIntraMode(addr-1, by*4+3)
	}
	if by > 0 {
		if fs.info[addr].intraModes[(by-1)*4+bx] >= 0 {
			top = int(fs.info[addr].intraModes[(by-1)*4+bx])
		}
	} else if fs.intraNeighbor(addr, addr-fs.mbW) {
		top = fs.neighborIntraMode(addr-fs.mbW, 3*4+bx)
	}
	return prediction.MostProbableIntra4x4(left, top)
}

// neighborIntraMode 返回邻块用于 MPM 的帧内模式。
//
// FFmpeg/libavcodec 对非 Intra4x4 的帧内邻块（I16x16、I_PCM）统一填入
// DC（2），而不是标记为不可用；只有非帧内邻块才是 -1。若把 I16x16
// 邻块当作不可用，当前块的 MPM 会错误退化为 DC，进而改变模式位长度
// 和整帧模式解析。
func (fs *frameState) neighborIntraMode(addr, modeIndex int) int {
	if addr < 0 || addr >= len(fs.info) {
		return -1
	}
	info := &fs.info[addr]
	if info.intraModes[modeIndex] >= 0 {
		return int(info.intraModes[modeIndex])
	}
	if info.intra {
		return prediction.Intra4x4DC
	}
	if !fs.pps.ConstrainedIntraPredFlag {
		return prediction.Intra4x4DC
	}
	return -1
}

func (fs *frameState) lumaNC(mbx, mby, bx, by int) int {
	addr := fs.mbAddress(mbx, mby)
	grid := fs.planeNZ()
	if fs.mbaff {
		return fs.mbaffNC(addr, 0, bx, by, 4, 16, grid)
	}
	var a, b int
	leftAvail, topAvail := true, true
	if bx > 0 {
		a = int(grid[addr*16+by*4+bx-1])
	} else if mbx > 0 && fs.neighborAvailable(addr, addr-1) {
		a = int(grid[(addr-1)*16+by*4+3])
	} else {
		leftAvail = false
	}
	if by > 0 {
		b = int(grid[addr*16+(by-1)*4+bx])
	} else if fs.neighborAvailable(addr, addr-fs.mbW) {
		b = int(grid[(addr-fs.mbW)*16+3*4+bx])
	} else {
		topAvail = false
	}
	return nC(a, b, leftAvail, topAvail)
}

func (fs *frameState) chromaNC(comp, mbx, mby, bx, by int) int {
	addr := fs.mbAddress(mbx, mby)
	grid := fs.chromaNZ[comp]
	if fs.mbaff {
		return fs.mbaffNC(addr, comp+1, bx, by, fs.chromaW/4, fs.chromaBlocks, grid)
	}
	var a, b int
	leftAvail, topAvail := true, true
	if bx > 0 {
		a = int(grid[addr*fs.chromaBlocks+by*2+bx-1])
	} else if mbx > 0 && fs.neighborAvailable(addr, addr-1) {
		a = int(grid[(addr-1)*fs.chromaBlocks+by*2+1])
	} else {
		leftAvail = false
	}
	if by > 0 {
		b = int(grid[addr*fs.chromaBlocks+(by-1)*2+bx])
	} else if fs.neighborAvailable(addr, addr-fs.mbW) {
		b = int(grid[(addr-fs.mbW)*fs.chromaBlocks+fs.chromaBlocks-2+bx])
	} else {
		topAvail = false
	}
	return nC(a, b, leftAvail, topAvail)
}

// nC 实现规范 9.2.1 的上下文推导。
//
// 关键点：邻块“不可用”与“可用但计数为 0”是两种不同情况。
// 两者都可用时才取平均；只有一侧可用时直接取该侧计数；都不可用为 0。
// 早期实现把不可用当作 0 参与平均，会在宏块边界选错 coeff_token VLC，
// 表现为块级解析错位但整帧仍可能“看起来能解”。
func nC(a, b int, leftAvail, topAvail bool) int {
	var v int
	switch {
	case leftAvail && topAvail:
		v = (a + b + 1) >> 1
	case leftAvail:
		v = a
	case topAvail:
		v = b
	default:
		v = 0
	}
	if v > 16 {
		v = 16
	}
	return v
}
