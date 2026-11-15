package dpb

import (
	"fmt"
	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/syntax"
)

// POCState 实现 Spec: 8.2.1 的逐帧 type 0/1/2 推导。参考 POC 状态与
// 上一解码帧的 frame_num 分开更新，非参考帧不得覆盖 type 0 的锚点。
type POCState struct {
	maxLsb           int64
	prevMsb, prevLsb int64
	prevPOC          int32
	prevFrameNum     uint32
	frameNumOffset   int64
}

func NewPOCState(maxLsb int32) *POCState { return &POCState{maxLsb: int64(maxLsb)} }
func (p *POCState) Reset()               { maxLsb := p.maxLsb; *p = POCState{maxLsb: maxLsb} }
func (p *POCState) Prev() int32          { return p.prevPOC }

func (p *POCState) type0(idr bool, lsb int64, reference bool) int64 {
	if idr {
		p.prevMsb, p.prevLsb = 0, 0
	}
	msb := p.prevMsb
	if lsb < p.prevLsb && p.prevLsb-lsb >= p.maxLsb/2 {
		msb += p.maxLsb
	} else if lsb > p.prevLsb && lsb-p.prevLsb > p.maxLsb/2 {
		msb -= p.maxLsb
	}
	if reference {
		p.prevMsb, p.prevLsb = msb, lsb
	}
	return msb + lsb
}

// Compute 保留 type 0 内核的简便入口，调用者保证 lsb 合法且结果可表示。
func (p *POCState) Compute(idr bool, lsb int32) int32 {
	v := p.type0(idr, int64(lsb), true)
	p.prevPOC = int32(v)
	return int32(v)
}

func (p *POCState) ComputeFrame(s *syntax.SPS, h *syntax.SliceHeader, reference bool) (int32, int32, int32, error) {
	if h.IDR {
		p.Reset()
	}
	maxFrameNum := int64(1) << (s.Log2MaxFrameNumMinus4 + 4)
	if !h.IDR && p.prevFrameNum > h.FrameNum {
		p.frameNumOffset += maxFrameNum
	}
	offset := p.frameNumOffset
	var top, bottom int64
	switch s.PicOrderCntType {
	case 0:
		top = p.type0(h.IDR, int64(h.PicOrderCntLsb), reference)
		bottom = top + int64(h.DeltaPicOrderCntBottom)
	case 1:
		cycle := len(s.OffsetForRefFrame)
		absolute := offset + int64(h.FrameNum)
		if cycle == 0 {
			absolute = 0
		}
		if !reference && absolute > 0 {
			absolute--
		}
		expected := int64(0)
		if absolute > 0 {
			delta := int64(0)
			for _, v := range s.OffsetForRefFrame {
				delta += int64(v)
			}
			count, within := (absolute-1)/int64(cycle), (absolute-1)%int64(cycle)
			magnitude := delta
			if magnitude < 0 {
				magnitude = -magnitude
			}
			if magnitude > 0 && count > (1<<63-1)/magnitude {
				return 0, 0, 0, fmt.Errorf("dpb: type 1 POC multiplication overflow: %w", h264.ErrResourceLimit)
			}
			expected = count * delta
			for i := int64(0); i <= within; i++ {
				expected += int64(s.OffsetForRefFrame[i])
			}
		}
		if !reference {
			expected += int64(s.OffsetForNonRefPic)
		}
		top = expected + int64(h.DeltaPicOrderCnt[0])
		bottom = top + int64(s.OffsetForTopToBottomField) + int64(h.DeltaPicOrderCnt[1])
	case 2:
		top = 2 * (offset + int64(h.FrameNum))
		if !reference {
			top--
		}
		if h.IDR {
			top = 0
		}
		bottom = top
	default:
		return 0, 0, 0, fmt.Errorf("dpb: POC type %d: %w", s.PicOrderCntType, h264.ErrInvalidData)
	}
	if top < -1<<31 || top > 1<<31-1 || bottom < -1<<31 || bottom > 1<<31-1 {
		return 0, 0, 0, fmt.Errorf("dpb: POC outside int32: %w", h264.ErrResourceLimit)
	}
	p.prevFrameNum = h.FrameNum
	order := min(top, bottom)
	if reference {
		p.prevPOC = int32(order)
	}
	return int32(top), int32(bottom), int32(order), nil
}

// AfterMMCO5 为下一帧建立重置锚点；top 是本帧减去本帧 PicOrderCnt 后
// 的 TopFieldOrderCnt，可能非零，不能简单把 prevPicOrderCntLsb 清零。
func (p *POCState) AfterMMCO5(top int32) {
	p.prevMsb = 0
	p.prevLsb = int64(top)
	p.prevFrameNum = 0
	p.frameNumOffset = 0
	p.prevPOC = 0
}
