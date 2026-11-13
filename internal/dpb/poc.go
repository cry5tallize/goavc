package dpb

// POCState 实现 POC type 0（8.2.1.1）。
type POCState struct {
	maxLsb      int32
	prevMsb     int32
	prevLsb     int32
	prevPOC     int32
	initialized bool
}

// NewPOCState 创建 POC type 0 状态。maxLsb = 1<<(log2_max_poc_lsb_minus4+4)。
func NewPOCState(maxLsb int32) *POCState {
	return &POCState{maxLsb: maxLsb}
}

// Reset 清除状态（IDR 前调用；IDR 本身也会重置）。
func (p *POCState) Reset() {
	p.prevMsb = 0
	p.prevLsb = 0
	p.prevPOC = 0
	p.initialized = false
}

// Compute 计算当前帧的 PicOrderCnt 并更新状态。
func (p *POCState) Compute(idr bool, lsb int32) int32 {
	if idr {
		p.prevMsb = 0
		p.prevLsb = 0
	}
	msb := p.prevMsb
	if lsb < p.prevLsb && p.prevLsb-lsb >= p.maxLsb/2 {
		msb = p.prevMsb + p.maxLsb
	} else if lsb > p.prevLsb && lsb-p.prevLsb > p.maxLsb/2 {
		msb = p.prevMsb - p.maxLsb
	}
	poc := msb + lsb
	p.prevMsb = msb
	p.prevLsb = lsb
	p.prevPOC = poc
	p.initialized = true
	return poc
}

// Prev 返回上一次计算的 POC（未初始化时为 0）。
func (p *POCState) Prev() int32 { return p.prevPOC }
