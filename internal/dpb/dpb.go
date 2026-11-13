// Package dpb 管理解码图像缓冲与显示顺序状态。
//
// M2 只需要“上一帧作为唯一参考 + frame_num 连续性校验 + POC type 0”，
// 因此本包刻意保持窄接口：不实现多参考列表、MMCO 与输出重排序（M3）。
// 图像内存归 picture.Pool 所有，DPB 只持有指针并在替换/Reset 时归还。
package dpb

import (
	"fmt"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/internal/picture"
)

// DPB 是单参考帧缓冲。
type DPB struct {
	ref          *picture.Picture
	refFrameNum  uint32
	refPOC       int32
	prevFrameNum uint32
	hasPrev      bool

	maxFrameNum uint32
	gapsAllowed bool
}

// New 创建 DPB。maxFrameNum 是 1<<(log2_max_frame_num_minus4+4)。
func New(maxFrameNum uint32, gapsAllowed bool) *DPB {
	return &DPB{maxFrameNum: maxFrameNum, gapsAllowed: gapsAllowed}
}

// Reset 清空参考帧并归还其内存。
func (d *DPB) Reset() {
	if d.ref != nil {
		d.ref.Release()
		d.ref = nil
	}
	d.hasPrev = false
	d.prevFrameNum = 0
	d.refFrameNum = 0
	d.refPOC = 0
}

// BeginFrame 校验 frame_num 连续性并更新内部状态。
//
// IDR 重置 frame_num 计数；非 IDR 的 frame_num 必须递增（允许回绕）。
// 出现空洞且 gaps_in_frame_num_value_allowed_flag=0 时返回 ErrInvalidData：
// 继续解码会让后续 P 帧参考到不存在的图像。
func (d *DPB) BeginFrame(idr bool, frameNum uint32) error {
	if d.maxFrameNum == 0 || frameNum >= d.maxFrameNum {
		return fmt.Errorf("dpb: frame_num %d outside modulus %d: %w", frameNum, d.maxFrameNum, h264.ErrInvalidData)
	}
	if idr {
		if frameNum != 0 {
			return fmt.Errorf("dpb: IDR frame_num %d, must be 0: %w", frameNum, h264.ErrInvalidData)
		}
		d.prevFrameNum = 0
		d.hasPrev = true
		return nil
	}
	if !d.hasPrev {
		// 非 IDR 起始：没有 IDR 锚点，允许从当前 frame_num 开始，
		// 但参考帧为空，P 帧解码会失败；这里只记录状态。
		d.prevFrameNum = frameNum
		d.hasPrev = true
		return nil
	}
	expected := (d.prevFrameNum + 1) % d.maxFrameNum
	if frameNum == d.prevFrameNum {
		return fmt.Errorf("dpb: duplicate frame_num %d: %w", frameNum, h264.ErrInvalidData)
	}
	if frameNum != expected && !d.gapsAllowed {
		return fmt.Errorf("dpb: frame_num gap %d -> %d not allowed: %w", d.prevFrameNum, frameNum, h264.ErrInvalidData)
	}
	d.prevFrameNum = frameNum
	return nil
}

// PutReference 把 pic 设为当前参考帧，并归还旧参考帧。
func (d *DPB) PutReference(pic *picture.Picture, frameNum uint32, poc int32) {
	if d.ref != nil && d.ref != pic {
		d.ref.Release()
	}
	d.ref = pic
	d.refFrameNum = frameNum
	d.refPOC = poc
}

// Reference 返回当前参考帧；没有参考帧时返回 nil。
func (d *DPB) Reference() *picture.Picture { return d.ref }

// ReferenceFrameNum 返回参考帧的 frame_num（仅诊断使用）。
func (d *DPB) ReferenceFrameNum() uint32 { return d.refFrameNum }

// ReferencePOC 返回参考帧的 POC（仅诊断使用）。
func (d *DPB) ReferencePOC() int32 { return d.refPOC }
