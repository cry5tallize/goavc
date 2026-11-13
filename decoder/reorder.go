package decoder

import (
	"fmt"
	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/syntax"
	"sort"
)

type orderedFrame struct {
	frame *h264.Frame
	poc   int32
}

func (d *Decoder) reorderDepth(s *syntax.SPS) int {
	if s.VUI != nil && s.VUI.BitstreamRestrictionFlag {
		return int(s.VUI.MaxNumReorderFrames)
	}
	// Baseline 不允许 B 切片。Main/High 未声明重排上限时按资源上限保守
	// 缓冲；不能因为当前尚未见到 B 帧就提前输出未来参考 P 帧。
	if s.ProfileIDC == 66 {
		return 0
	}
	return min(16, d.cfg.MaxDPBFrames)
}

func (d *Decoder) queueFrame(f *h264.Frame, poc int32) error {
	if d.hasEmitted && poc <= d.lastEmittedPOC {
		return fmt.Errorf("decoder: POC exceeds reorder declaration: %w", h264.ErrInvalidData)
	}
	for _, p := range d.pending {
		if p.poc == poc {
			return fmt.Errorf("decoder: duplicate frame POC %d: %w", poc, h264.ErrInvalidData)
		}
	}
	depth := d.reorderDepth(d.activeSPS)
	if depth == 0 {
		d.emit(orderedFrame{f, poc})
		return nil
	}
	d.pending = append(d.pending, orderedFrame{f, poc})
	sort.SliceStable(d.pending, func(i, j int) bool { return d.pending[i].poc < d.pending[j].poc })
	for len(d.pending) > depth {
		next := d.pending[0]
		d.pending[0] = orderedFrame{}
		d.pending = d.pending[1:]
		d.emit(next)
	}
	return nil
}

func (d *Decoder) emit(f orderedFrame) {
	d.out = append(d.out, f.frame)
	d.lastEmittedPOC = f.poc
	d.hasEmitted = true
}
func (d *Decoder) drainPending() {
	for _, f := range d.pending {
		d.emit(f)
	}
	d.pending = nil
}
func (d *Decoder) discardPending() {
	for _, f := range d.pending {
		f.frame.Release()
	}
	d.pending = nil
}
func (d *Decoder) outputBoundary(discard bool) {
	if discard {
		d.discardPending()
		for _, f := range d.out {
			f.Release()
		}
		d.out = nil
	} else {
		d.drainPending()
	}
	d.hasEmitted = false
}
