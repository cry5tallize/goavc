package encoder

import (
	"encoding/binary"
	"fmt"
	h264 "github.com/cry5tallize/goavc"
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
	queued, err := copyQueuedFrame(frame)
	if err != nil {
		return fmt.Errorf("encoder: copy lookahead frame: %w", err)
	}
	e.pendingFrames = append(e.pendingFrames, queued)
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
				av, bv := uint16(ar[x]), uint16(br[x])
				if a.Format.BitDepth() > 8 {
					av, bv = binary.LittleEndian.Uint16(ar[x*2:]), binary.LittleEndian.Uint16(br[x*2:])
				}
				d := int64(av) - int64(bv)
				if d < 0 {
					d = -d
				}
				difference += d
				samples++
			}
		}
	}
	return difference*100 > samples*int64((1<<a.Format.BitDepth())-1)*int64(threshold)
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

// copyQueuedFrame 只复制有效平面，避免外部stride填充放大延迟队列内存。
// 调用前已验证几何和数据长度；队列仅保留各格式显示尺寸的有效字节。
func copyQueuedFrame(src *h264.Frame) (*h264.Frame, error) {
	out, err := h264.NewFrame(src.Width, src.Height, src.Format)
	if err != nil {
		return nil, err
	}
	out.PTS = src.PTS
	out.Duration = src.Duration
	out.KeyFrame = src.KeyFrame
	for comp := range out.Planes {
		_, h := src.Format.PlaneDimensions(comp, src.Width, src.Height)
		rowBytes := src.Format.RowBytes(comp, src.Width)
		for y := 0; y < h; y++ {
			copy(out.Planes[comp].Data[y*rowBytes:(y+1)*rowBytes], src.Planes[comp].Data[y*src.Planes[comp].Stride:y*src.Planes[comp].Stride+rowBytes])
		}
	}
	return out, nil
}
