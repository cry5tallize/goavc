package encoder

import (
	"fmt"
	h264 "github.com/goavc/h264"
)

// WithLookahead 设置未来帧窗口1..16。缓存帧按显示顺序输出；该选项不启用B帧。
// 最多持有depth+1个内部深拷贝；默认场景阈值20%，可用显式阈值选项覆盖。
// 初次输出延迟depth帧；调用方必须循环Send/Receive并在末尾Flush后排空。
func WithLookahead(depth int) Option {
	return func(e *Encoder) error {
		if depth < 1 || depth > 16 {
			return fmt.Errorf("encoder: lookahead depth %d: %w", depth, h264.ErrInvalidData)
		}
		e.lookahead = depth
		if e.sceneThreshold == 0 {
			e.sceneThreshold = 20
		}
		return nil
	}
}

func (e *Encoder) queueLookahead(frame *h264.Frame) error {
	if len(e.pendingFrames) > e.lookahead {
		return h264.ErrAgain
	}
	e.pendingFrames = append(e.pendingFrames, frame.Clone())
	if len(e.pendingFrames) <= e.lookahead {
		return nil
	}
	if err := e.encodeLookaheadHead(); err != nil {
		// 当前Send失败：只撤回本次新增帧，先前接受的窗口继续保留供重试。
		i := len(e.pendingFrames) - 1
		e.pendingFrames[i].Release()
		e.pendingFrames[i] = nil
		e.pendingFrames = e.pendingFrames[:i]
		return err
	}
	return nil
}

func (e *Encoder) encodeLookaheadHead() error {
	f := e.pendingFrames[0]
	scene := false
	changed := e.lookaheadPrevious != nil && framesDiffer(e.lookaheadPrevious, f, e.sceneThreshold)
	if changed {
		scene = true
		if len(e.pendingFrames) > 1 {
			// 一帧闪光随后回到旧场景时抑制IDR；真正切换的未来帧应仍异于旧场景。
			scene = false
			for _, future := range e.pendingFrames[1:] {
				if framesDiffer(e.lookaheadPrevious, future, e.sceneThreshold) {
					scene = true
					break
				}
			}
		}
	}
	if err := e.encodeFrameNow(f, scene); err != nil {
		return err
	}
	if changed && !scene {
		// 保留闪光前场景作为分析锚点，避免下一帧回落再触发一次切换。
		f.Release()
	} else {
		if e.lookaheadPrevious != nil {
			e.lookaheadPrevious.Release()
		}
		e.lookaheadPrevious = f
	}
	copy(e.pendingFrames, e.pendingFrames[1:])
	e.pendingFrames[len(e.pendingFrames)-1] = nil
	e.pendingFrames = e.pendingFrames[:len(e.pendingFrames)-1]
	return nil
}

func framesDiffer(a, b *h264.Frame, threshold int) bool {
	var difference, samples int64
	for comp := range a.Planes {
		w, h := a.Format.PlaneDimensions(comp, a.Width, a.Height)
		for y := 0; y < h; y++ {
			ar, br := a.Planes[comp].Data[y*a.Planes[comp].Stride:], b.Planes[comp].Data[y*b.Planes[comp].Stride:]
			for x := 0; x < w; x++ {
				d := int64(ar[x]) - int64(br[x])
				if d < 0 {
					d = -d
				}
				difference += d
				samples++
			}
		}
	}
	return difference*100 > samples*255*int64(threshold)
}

func (e *Encoder) releaseLookahead() {
	for _, f := range e.pendingFrames {
		f.Release()
	}
	e.pendingFrames = nil
	if e.lookaheadPrevious != nil {
		e.lookaheadPrevious.Release()
		e.lookaheadPrevious = nil
	}
}
