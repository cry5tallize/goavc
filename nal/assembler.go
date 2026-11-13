package nal

// AccessUnit 是一组属于同一帧的 NAL unit，按码流顺序排列。
//
// 前缀 NAL（SPS/PPS/SEI/AUD）与所有 slice NAL 都在同一个 AccessUnit 中；
// 结尾的 end of sequence/stream 也归入当前 AU。
type AccessUnit struct {
	Units []Unit
}

// HasVCL 报告该 AU 是否包含至少一个 VCL NAL。
func (au *AccessUnit) HasVCL() bool {
	if au == nil {
		return false
	}
	for _, u := range au.Units {
		if u.Header.IsVCL() {
			return true
		}
	}
	return false
}

// VCLCount 返回 VCL NAL 数量。
func (au *AccessUnit) VCLCount() int {
	if au == nil {
		return 0
	}
	n := 0
	for _, u := range au.Units {
		if u.Header.IsVCL() {
			n++
		}
	}
	return n
}

// IsIDR 报告该 AU 是否包含 IDR slice。
func (au *AccessUnit) IsIDR() bool {
	if au == nil {
		return false
	}
	for _, u := range au.Units {
		if u.Header.IsIDR() {
			return true
		}
	}
	return false
}

// Assembler 把 NAL unit 序列组装成 Access Unit。
//
// 边界判定委托给 DetectBoundary；调用方可以为每个 VCL NAL 提供 SliceInfo，
// 也可以用 PushHinted 显式指定边界。缺少 SliceInfo 时 BoundaryUnknown
// 按"同一 AU"处理，因此单 slice 流可以直接按 NAL 序列组装，多 slice 流
// 需要提供 hint（M2 的 slice header 解析会填充这些字段）。
type Assembler struct {
	pending  []Unit
	last     Unit
	hasLast  bool
	lastInfo *SliceInfo
}

// NewAssembler 创建一个空的 AU 组装器。
func NewAssembler() *Assembler { return &Assembler{} }

// Push 加入一个 NAL unit。如果该 unit 开始新 AU，返回刚刚完成的 AU
// （否则返回 nil）。info 仅对 VCL NAL 有意义，可以为 nil。
func (a *Assembler) Push(u Unit, info *SliceInfo) *AccessUnit {
	return a.PushHinted(u, info, BoundaryUnknown)
}

// PushHinted 与 Push 相同，但允许调用方用 hint 覆盖自动边界判定。
//
// hint 为 BoundaryUnknown 时仍使用 DetectBoundary。显式 hint 适用于
// 调用方已经从 slice header 得到更可靠的信息、或测试需要构造特定分组。
func (a *Assembler) PushHinted(u Unit, info *SliceInfo, hint Boundary) *AccessUnit {
	if !a.hasLast {
		a.append(u, info)
		return nil
	}

	boundary := hint
	if boundary == BoundaryUnknown {
		boundary = DetectBoundary(a.last, u, a.lastInfo, info)
	}
	if boundary == BoundaryNew {
		completed := a.takePending()
		a.append(u, info)
		return completed
	}

	// BoundarySame 与 BoundaryUnknown 都追加到当前 AU。Unknown 的语义是
	// "信息不足，保守地认为属于同一帧"，这是单 slice 流最常见的情况。
	a.append(u, info)
	return nil
}

// Flush 返回尚未完成的 AU 并重置组装器；没有挂起数据时返回 nil。
func (a *Assembler) Flush() *AccessUnit {
	if len(a.pending) == 0 {
		a.reset()
		return nil
	}
	au := a.takePending()
	a.reset()
	return au
}

// Pending 返回当前挂起的 NAL 数量，主要用于测试与诊断。
func (a *Assembler) Pending() int { return len(a.pending) }

// append 把 unit 加入 pending 并更新边界判定所需的"上一单元"状态。
func (a *Assembler) append(u Unit, info *SliceInfo) {
	a.pending = append(a.pending, u)
	a.last = u
	a.hasLast = true
	if u.Header.IsVCL() {
		a.lastInfo = info
	} else {
		// 非 VCL 没有 slice 信息；后续若紧接 VCL，DetectBoundary 会走
		// "VCL 跟在非 VCL 之后"的分支，不需要上一 slice 的字段。
		a.lastInfo = nil
	}
}

// takePending 复制并清空 pending。
func (a *Assembler) takePending() *AccessUnit {
	units := make([]Unit, len(a.pending))
	copy(units, a.pending)
	a.pending = a.pending[:0]
	return &AccessUnit{Units: units}
}

// reset 清空所有状态（保留已分配容量）。
func (a *Assembler) reset() {
	a.pending = a.pending[:0]
	a.last = Unit{}
	a.hasLast = false
	a.lastInfo = nil
}
