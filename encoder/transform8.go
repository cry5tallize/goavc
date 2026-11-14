package encoder

import (
	"fmt"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/internal/picture"
	"github.com/goavc/h264/internal/prediction"
	"github.com/goavc/h264/internal/transform"
	"github.com/goavc/h264/syntax"
)

// WithTransform8x8 在 High Profile 的 I/P 宏块中加入8×8残差候选。
// 候选与4×4共享三平面RD代价；默认关闭，Baseline/Main不可使用。
func WithTransform8x8() Option {
	return func(e *Encoder) error {
		if e.cfg.Profile != h264.ProfileHigh && e.cfg.Profile != h264.ProfileHigh10 && e.cfg.Profile != h264.ProfileHigh422 && e.cfg.Profile != h264.ProfileHigh444 {
			return fmt.Errorf("encoder: 8x8 transform requires High profile: %w", h264.ErrUnsupported)
		}
		e.transform8 = true
		return nil
	}
}

// WithScalingMatrix8x8 指定光栅顺序的 intra/inter 亮度8×8矩阵。
// 所有项必须为1..255；按值复制，隐式启用8×8变换。4×4/DC矩阵保持flat=16。
func WithScalingMatrix8x8(intra, inter [64]uint8) Option {
	return func(e *Encoder) error {
		if err := WithTransform8x8()(e); err != nil {
			return err
		}
		matrices := [6][64]uint8{intra, inter, intra, inter, intra, inter}
		for _, matrix := range matrices {
			for _, value := range matrix {
				if value == 0 {
					return fmt.Errorf("encoder: zero 8x8 scaling entry: %w", h264.ErrInvalidData)
				}
			}
		}
		e.scaling8 = &matrices
		return nil
	}
}

// WithScalingMatrices8x8Chroma 按 intraY/interY/intraCb/interCb/intraCr/interCr
// 顺序指定六个独立矩阵；仅 4:4:4 语法会携带全部列表。
func WithScalingMatrices8x8Chroma(matrices [6][64]uint8) Option {
	return func(e *Encoder) error {
		if e.cfg.Profile != h264.ProfileHigh444 {
			return fmt.Errorf("encoder: six 8x8 matrices require High444: %w", h264.ErrUnsupported)
		}
		for _, matrix := range matrices {
			for _, value := range matrix {
				if value == 0 {
					return fmt.Errorf("encoder: zero 8x8 scaling entry: %w", h264.ErrInvalidData)
				}
			}
		}
		e.transform8 = true
		e.scaling8 = &matrices
		return nil
	}
}

func (s *frameState) matrix8(intra bool) *[64]uint8 {
	if s.scaling8 == nil {
		return nil
	}
	index := s.plane * 2
	if !intra {
		index++
	}
	return &s.scaling8[index]
}

// 显式写出六个flat 4×4列表，避免PPS fallback规则将默认JVT矩阵带入
// 原来的4×4/DC路径；8×8列表按Zigzag扫描从光栅矩阵计算规范delta。
func (e *Encoder) scalingParameters() {
	if e.scaling8 == nil && e.scaling4 == nil {
		return
	}
	e.pps.PicScalingMatrixPresentFlag = true
	count := 8
	if e.sps.ChromaFormatIDC == 3 {
		count = 12
	}
	for i := range count {
		size, scan := 4, transform.Zigzag4x4[:]
		if i >= 6 {
			size, scan = 8, transform.Zigzag8x8[:]
		}
		list := &syntax.ScalingList{Size: size}
		last := int32(8)
		for _, r := range scan {
			value := int32(16)
			if i < 6 && e.scaling4 != nil {
				value = int32(e.scaling4[i][r])
			}
			if i >= 6 && e.scaling8 != nil {
				value = int32(e.scaling8[i-6][r])
			}
			delta := (value-last+128+256)%256 - 128
			list.Deltas = append(list.Deltas, delta)
			list.Values = append(list.Values, value)
			last = value
		}
		e.pps.PicScalingListPresentFlag[i] = true
		e.pps.PicScalingList[i] = list
	}
}

func reconstruct8(pl *picture.Plane, x, y int, levels *[64]int32, qp int, matrix *[64]uint8) {
	var coeff, out [64]int32
	transform.Dequant8x8(&coeff, levels, qp, matrix)
	transform.IDCT8x8(&out, &coeff)
	for j := 0; j < 8; j++ {
		for i := 0; i < 8; i++ {
			value := int32(pl.Sample(x+i, y+j)) + out[j*8+i]
			pl.SetSample(x+i, y+j, uint16(max(0, min((1<<pl.BitDepth)-1, value))))
		}
	}
}

func (c *cabacWriter) transformFlag(s *frameState, addr int) {
	inc := 0
	for _, n := range s.cabacNeighbors(addr) {
		if n >= 0 && s.info[n].transform8 {
			inc++
		}
	}
	c.bin(399+inc, flag(s.info[addr].transform8))
}

func (s *frameState) samples8(addr, bx, by int) (prediction.Intra8x8Samples16, bool, bool) {
	mw := s.pic.MBWidth
	mbx := addr % mw
	x, y := mbx*16+bx*8, addr/mw*16+by*8
	top := by > 0 || s.available(addr, addr-mw)
	left := bx > 0 || mbx > 0 && s.available(addr, addr-1)
	topRight := bx == 0
	if by == 0 && bx == 1 {
		topRight = mbx+1 < mw && s.available(addr, addr-mw+1)
	}
	topLeft := top && left
	if bx == 0 && by == 0 {
		topLeft = mbx > 0 && s.available(addr, addr-mw-1)
	}
	a := prediction.Intra8x8Samples16{TopLeftAvailable: topLeft}
	p := &s.pic.Planes[0]
	if topLeft {
		a.TopLeft = p.Sample(x-1, y-1)
	}
	if top {
		for i := 0; i < 16; i++ {
			a.Top[i] = p.Sample(x+min(i, 7), y-1)
			if i >= 8 && topRight {
				a.Top[i] = p.Sample(x+i, y-1)
			}
		}
	}
	if left {
		for i := 0; i < 8; i++ {
			a.Left[i] = p.Sample(x-1, y+i)
		}
	}
	return a, top, left
}

func (s *frameState) analyzeIntra8(addr int) residual {
	info := &s.info[addr]
	info.transform8 = true
	x, y := addr%s.pic.MBWidth*16, addr/s.pic.MBWidth*16
	pl := &s.pic.Planes[0]
	var res residual
	for block := 0; block < 4; block++ {
		bx, by := block%2, block/2
		a, top, left := s.samples8(addr, bx, by)
		var best [64]uint16
		bestMode, bestCost := 2, int(^uint(0)>>1)
		for mode := 0; mode < 9; mode++ {
			if s.reuseModes {
				break
			}
			if !prediction.Intra4x4ModeAvailable(mode, top, left, a.TopLeftAvailable) {
				continue
			}
			var pred [64]uint16
			if prediction.Intra8x8High(pred[:], 8, mode, a, top, left, pl.BitDepth) != nil {
				continue
			}
			cost := blockSATD(&s.src.Planes[0], x+bx*8, y+by*8, 8, pred[:])
			if cost < bestCost {
				best, bestMode, bestCost = pred, mode, cost
			}
		}
		if s.reuseModes {
			bestMode = int(info.modes[by*8+bx*2])
			_ = prediction.Intra8x8High(best[:], 8, bestMode, a, top, left, pl.BitDepth)
		}
		putBlock(pl, x+bx*8, y+by*8, 8, best[:])
		r := by*8 + bx*2
		for _, offset := range []int{0, 1, 4, 5} {
			info.modes[r+offset] = int8(bestMode)
		}
		var input, coeff [64]int32
		for j := 0; j < 8; j++ {
			for i := 0; i < 8; i++ {
				input[j*8+i] = int32(s.src.Planes[0].Sample(x+bx*8+i, y+by*8+j)) - int32(best[j*8+i])
			}
		}
		transform.Forward8x8(&coeff, &input)
		transform.Quant8x8WithScaling(&res.luma8[block], &coeff, s.lumaQP(info.qp), s.matrix8(true))
		if nonzero(res.luma8[block][:]) {
			info.cbp |= 1 << block
		}
		reconstruct8(pl, x+bx*8, y+by*8, &res.luma8[block], s.lumaQP(info.qp), s.matrix8(true))
	}
	s.chroma(addr, &res)
	return res
}
