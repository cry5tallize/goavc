package dpb

import (
	"fmt"
	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/internal/motion"
	"github.com/goavc/h264/internal/picture"
	"github.com/goavc/h264/syntax"
	"sort"
)

// Identity 独立于图像存储生命周期。共位块只保留身份和 POC，不引用完整
// 参考图像对象，防止参考链把早已回池的图像与运动网格永久保留。
type Identity struct {
	ID          uint64
	FrameNum    uint32
	POC         int32
	LongTerm    bool
	LongTermIdx uint32
}

type Reference struct {
	*Identity
	Picture         *picture.Picture
	Motion          [2]*motion.Grid
	BlockReferences [2][]*Identity
	// 独立色平面的Cb/Cr运动及块引用；共同编码444只使用上面的共享网格。
	ChromaMotion          [2][2]*motion.Grid
	ChromaBlockReferences [2][2][]*Identity
}

func (d *DPB) SetCapacity(n int) error {
	if n < 1 || n > 16 {
		return fmt.Errorf("dpb: capacity %d outside 1..16: %w", n, h264.ErrResourceLimit)
	}
	d.maxRefs = n
	return nil
}

func (d *DPB) NewReference(pic *picture.Picture, frameNum uint32, poc int32) *Reference {
	d.nextID++
	return &Reference{Identity: &Identity{ID: d.nextID, FrameNum: frameNum, POC: poc}, Picture: pic}
}

func (d *DPB) PicNum(r *Reference, current uint32) int64 {
	n := int64(r.FrameNum)
	if r.FrameNum > current {
		n -= int64(d.maxFrameNum)
	}
	return n
}

// Lists 构造 Spec: 8.2.4.2 的帧参考列表。P 按 PicNum 降序；B 按当前
// POC 两侧分组，list1 交换两侧；长期参考总是按索引升序追加。
func (d *DPB) Lists(current uint32, poc int32, bSlice bool) [2][]*Reference {
	var lists [2][]*Reference
	var short, long []*Reference
	for _, r := range d.references {
		if r.LongTerm {
			long = append(long, r)
		} else {
			short = append(short, r)
		}
	}
	sort.Slice(long, func(i, j int) bool { return long[i].LongTermIdx < long[j].LongTermIdx })
	if !bSlice {
		sort.Slice(short, func(i, j int) bool { return d.PicNum(short[i], current) > d.PicNum(short[j], current) })
		lists[0] = append(short, long...)
		return lists
	}
	var before, after []*Reference
	for _, r := range short {
		if r.POC < poc {
			before = append(before, r)
		} else {
			after = append(after, r)
		}
	}
	sort.Slice(before, func(i, j int) bool { return before[i].POC > before[j].POC })
	sort.Slice(after, func(i, j int) bool { return after[i].POC < after[j].POC })
	lists[0] = append(append(append([]*Reference{}, before...), after...), long...)
	lists[1] = append(append(append([]*Reference{}, after...), before...), long...)
	if len(lists[0]) > 1 {
		same := true
		for i := range lists[0] {
			if lists[0][i] != lists[1][i] {
				same = false
			}
		}
		if same {
			lists[1][0], lists[1][1] = lists[1][1], lists[1][0]
		}
	}
	return lists
}

func (d *DPB) Modify(list []*Reference, current uint32, commands []syntax.RefListModification, active int) ([]*Reference, error) {
	if active < 1 || active > 32 {
		return nil, fmt.Errorf("dpb: active list size %d: %w", active, h264.ErrInvalidData)
	}
	out := append([]*Reference{}, list...)
	prediction := int64(current)
	for index, cmd := range commands {
		if index >= active {
			return nil, fmt.Errorf("dpb: too many list modifications: %w", h264.ErrInvalidData)
		}
		var selected *Reference
		if cmd.IDC < 2 {
			if cmd.Value >= d.maxFrameNum {
				return nil, fmt.Errorf("dpb: reordering difference exceeds modulus: %w", h264.ErrInvalidData)
			}
			difference := int64(cmd.Value) + 1
			if cmd.IDC == 0 {
				prediction -= difference
			} else {
				prediction += difference
			}
			prediction = (prediction + int64(d.maxFrameNum)) % int64(d.maxFrameNum)
			target := prediction
			if target > int64(current) {
				target -= int64(d.maxFrameNum)
			}
			for _, r := range d.references {
				if !r.LongTerm && d.PicNum(r, current) == target {
					selected = r
					break
				}
			}
		} else if cmd.IDC == 2 {
			for _, r := range d.references {
				if r.LongTerm && r.LongTermIdx == cmd.Value {
					selected = r
					break
				}
			}
		}
		if selected == nil {
			return nil, fmt.Errorf("dpb: reordering references missing picture: %w", h264.ErrInvalidData)
		}
		if index > len(out) {
			return nil, fmt.Errorf("dpb: invalid list modification position: %w", h264.ErrInvalidData)
		}
		next := append([]*Reference{}, out[:index]...)
		next = append(next, selected)
		for _, r := range out[index:] {
			if r != selected {
				next = append(next, r)
			}
		}
		out = next
	}
	if len(out) < active {
		return nil, fmt.Errorf("dpb: %d active references, only %d available: %w", active, len(out), h264.ErrInvalidData)
	}
	return out[:active], nil
}

func (d *DPB) remove(index int) {
	r := d.references[index]
	if r.Picture != nil {
		r.Picture.Release()
		r.Picture = nil
	}
	r.Motion = [2]*motion.Grid{}
	r.BlockReferences = [2][]*Identity{}
	d.references = append(d.references[:index], d.references[index+1:]...)
}

func (d *DPB) removeLong(index uint32) {
	for i := len(d.references) - 1; i >= 0; i-- {
		if r := d.references[i]; r.LongTerm && r.LongTermIdx == index {
			d.remove(i)
		}
	}
}

func (d *DPB) shortIndex(picNum int64, current uint32) int {
	for i, r := range d.references {
		if !r.LongTerm && d.PicNum(r, current) == picNum {
			return i
		}
	}
	return -1
}

// Store 在重建完成后按 Spec: 8.2.5 应用滑窗或 MMCO 1..6，然后转移
// 当前图像所有权。失败时调用方仍拥有当前图像，必须走错误清理路径。
func (d *DPB) Store(cur *Reference, idr, longTerm, adaptive bool, ops []syntax.MMCO) error {
	if cur == nil || cur.Picture == nil {
		return fmt.Errorf("dpb: nil current reference: %w", h264.ErrInvalidData)
	}
	if idr {
		d.clearReferences()
		d.maxLongTermIdx = -1
		if longTerm {
			d.maxLongTermIdx = 0
			cur.LongTerm = true
			cur.LongTermIdx = 0
		}
	}
	for _, m := range ops {
		switch m.Operation {
		case 1, 3:
			if m.DifferenceOfPicNumsMinus1 >= d.maxFrameNum {
				return fmt.Errorf("dpb: MMCO short-term difference too large: %w", h264.ErrInvalidData)
			}
			target := int64(cur.FrameNum) - int64(m.DifferenceOfPicNumsMinus1) - 1
			index := d.shortIndex(target, cur.FrameNum)
			if index < 0 {
				return fmt.Errorf("dpb: MMCO missing short-term picture: %w", h264.ErrInvalidData)
			}
			if m.Operation == 1 {
				d.remove(index)
			} else {
				if int64(m.LongTermFrameIdx) > d.maxLongTermIdx {
					return fmt.Errorf("dpb: MMCO long-term index exceeds maximum: %w", h264.ErrInvalidData)
				}
				r := d.references[index]
				d.removeLong(m.LongTermFrameIdx)
				r.LongTerm = true
				r.LongTermIdx = m.LongTermFrameIdx
			}
		case 2:
			found := false
			for _, r := range d.references {
				if r.LongTerm && r.LongTermIdx == m.LongTermPicNum {
					found = true
				}
			}
			if !found {
				return fmt.Errorf("dpb: MMCO missing long-term picture: %w", h264.ErrInvalidData)
			}
			d.removeLong(m.LongTermPicNum)
		case 4:
			if m.MaxLongTermFrameIdxPlus1 > uint32(d.maxRefs) {
				return fmt.Errorf("dpb: MMCO long-term maximum exceeds capacity: %w", h264.ErrInvalidData)
			}
			d.maxLongTermIdx = int64(m.MaxLongTermFrameIdxPlus1) - 1
			for i := len(d.references) - 1; i >= 0; i-- {
				r := d.references[i]
				if r.LongTerm && int64(r.LongTermIdx) > d.maxLongTermIdx {
					d.remove(i)
				}
			}
		case 5:
			d.clearReferences()
			d.maxLongTermIdx = -1
			cur.FrameNum = 0
			d.prevFrameNum = 0
			d.hasPrev = true
		case 6:
			if int64(m.LongTermFrameIdx) > d.maxLongTermIdx {
				return fmt.Errorf("dpb: current long-term index exceeds maximum: %w", h264.ErrInvalidData)
			}
			d.removeLong(m.LongTermFrameIdx)
			cur.LongTerm = true
			cur.LongTermIdx = m.LongTermFrameIdx
		default:
			return fmt.Errorf("dpb: invalid MMCO %d: %w", m.Operation, h264.ErrInvalidData)
		}
	}
	if !idr && !adaptive {
		for len(d.references) >= d.maxRefs {
			oldest := -1
			for i, r := range d.references {
				if !r.LongTerm && (oldest < 0 || d.PicNum(r, cur.FrameNum) < d.PicNum(d.references[oldest], cur.FrameNum)) {
					oldest = i
				}
			}
			if oldest < 0 {
				return fmt.Errorf("dpb: sliding window contains only long-term references: %w", h264.ErrInvalidData)
			}
			d.remove(oldest)
		}
	}
	if len(d.references) >= d.maxRefs {
		return fmt.Errorf("dpb: reference count exceeds capacity: %w", h264.ErrInvalidData)
	}
	d.references = append(d.references, cur)
	d.ref = cur.Picture
	d.refFrameNum = cur.FrameNum
	d.refPOC = cur.POC
	return nil
}

func (d *DPB) clearReferences() {
	for len(d.references) > 0 {
		d.remove(len(d.references) - 1)
	}
	d.ref = nil
}
