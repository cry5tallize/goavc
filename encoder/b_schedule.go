package encoder

import (
	"fmt"
	"math"

	h264 "github.com/goavc/h264"
)

type bInput struct {
	frame *h264.Frame
	index int64
}

// queueBInput 按显示顺序接受有界深拷贝；组内先编码未来参考锚点，再输出
// 两锚点之间的非参考B帧。IDR/GOP/强制关键帧边界之前不跨界选未来锚点。
func (e *Encoder) queueBInput(frame *h264.Frame) (err error) {
	// 输入未被接受时恢复调度临时字段；参考/码控/输出由encodeFrameNow事务保护。
	clock, stepBefore, origin, lastPTS, set := e.bClock, e.bStep, e.bInputOrigin, e.bLastInputPTS, e.bClockSet
	index, poc := e.bCurrentIndex, e.currentPOC
	defer func() {
		if err != nil {
			e.bClock, e.bStep, e.bInputOrigin, e.bLastInputPTS, e.bClockSet = clock, stepBefore, origin, lastPTS, set
			e.bCurrentIndex, e.currentPOC = index, poc
		}
	}()
	if len(e.bReady) > 0 || len(e.bInputs) > e.cfg.MaxBFrames+e.lookahead {
		return h264.ErrAgain
	}
	step := frame.Duration
	if step <= 0 {
		step = 1
	}
	if e.bClockSet && step != e.bStep {
		return fmt.Errorf("encoder: B scheduling requires constant duration: %w", h264.ErrUnsupported)
	}
	if e.bNextIndex > 0 && (e.bLastInputPTS > math.MaxInt64-step || frame.PTS != e.bLastInputPTS+step) {
		return fmt.Errorf("encoder: B scheduling requires regular PTS: %w", h264.ErrInvalidData)
	}
	if !e.bClockSet {
		if step > math.MaxInt64/int64(e.cfg.MaxBFrames) {
			return fmt.Errorf("encoder: B DTS overflows: %w", h264.ErrInvalidData)
		}
		delay := int64(e.cfg.MaxBFrames) * step
		if frame.PTS < math.MinInt64+delay {
			return fmt.Errorf("encoder: B DTS overflows: %w", h264.ErrInvalidData)
		}
		e.bInputOrigin = frame.PTS
		e.bClock, e.bStep = frame.PTS-delay, step
		e.bClockSet = true
	}
	if e.bClock > math.MaxInt64-step || frame.PTS > math.MaxInt64-step {
		return fmt.Errorf("encoder: B timestamps overflow: %w", h264.ErrInvalidData)
	}
	e.bInputs = append(e.bInputs, bInput{frame: frame.Clone(), index: e.bNextIndex})
	e.bNextIndex++
	if e.reference != nil && len(e.bInputs) <= e.cfg.MaxBFrames+e.lookahead {
		e.bLastInputPTS = frame.PTS
		return nil
	}
	if err := e.encodeNextBOutput(); err != nil {
		i := len(e.bInputs) - 1
		e.bInputs[i].frame.Release()
		e.bInputs[i] = bInput{}
		e.bInputs = e.bInputs[:i]
		e.bNextIndex--
		if e.bNextIndex == 0 {
			e.bClockSet = false
		}
		return err
	}
	e.bLastInputPTS = frame.PTS
	return nil
}

func (e *Encoder) encodeNextBOutput() error {
	if len(e.bReady) > 0 {
		entry := e.bReady[0]
		e.bCurrentIndex = entry.index
		e.encodingB = true
		err := e.encodeFrameNow(entry.frame, false)
		e.encodingB = false
		if err != nil {
			return err
		}
		entry.frame.Release()
		e.bReady[0] = bInput{}
		e.bReady = e.bReady[1:]
		if len(e.bReady) == 0 {
			e.bPast, e.bFuture = nil, nil
		}
		return nil
	}
	if len(e.bInputs) == 0 {
		return nil
	}
	anchor := min(e.cfg.MaxBFrames, len(e.bInputs)-1)
	if e.reference == nil {
		anchor = 0
	} else {
		for i, entry := range e.bInputs[:anchor+1] {
			boundary := entry.frame.KeyFrame || entry.index-e.bIDRIndex >= int64(e.cfg.IDRInterval) || entry.index-e.bIntraIndex >= int64(e.cfg.GOPSize) || e.bSceneAt(i)
			if boundary {
				anchor = max(0, i-1)
				break
			}
		}
	}
	entry := e.bInputs[anchor]
	past := e.reference
	previousSource := e.bPreviousSource
	if anchor > 0 {
		previousSource = e.bInputs[anchor-1].frame
	}
	flash := previousSource != nil && anchor+1 < len(e.bInputs) && framesDiffer(previousSource, entry.frame, e.sceneThreshold) && !framesDiffer(previousSource, e.bInputs[anchor+1].frame, e.sceneThreshold)
	e.bCurrentIndex = entry.index
	if err := e.encodeFrameNow(entry.frame, e.bSceneAt(anchor)); err != nil {
		return err
	}
	if anchor > 0 {
		e.bPast, e.bFuture = past, e.reference
		e.bReady = append(e.bReady, e.bInputs[:anchor]...)
	}
	if flash {
		entry.frame.Release()
	} else {
		if e.bPreviousSource != nil {
			e.bPreviousSource.Release()
		}
		e.bPreviousSource = entry.frame
	}
	remaining := append([]bInput(nil), e.bInputs[anchor+1:]...)
	clear(e.bInputs)
	e.bInputs = remaining
	return nil
}

func (e *Encoder) releaseBScheduler() {
	for _, entry := range e.bInputs {
		entry.frame.Release()
	}
	for _, entry := range e.bReady {
		entry.frame.Release()
	}
	e.bInputs, e.bReady = nil, nil
	if e.bPreviousSource != nil {
		e.bPreviousSource.Release()
		e.bPreviousSource = nil
	}
	e.bPast, e.bFuture = nil, nil
	e.encodingB = false
	e.bCurrentIndex, e.bNextIndex = 0, 0
	e.bIDRIndex, e.bIntraIndex = 0, 0
	e.currentPOC = 0
	e.bClock, e.bStep, e.bInputOrigin = 0, 0, 0
	e.bLastInputPTS = 0
	e.bClockSet = false
}

func (e *Encoder) bSceneAt(index int) bool {
	if e.sceneThreshold == 0 {
		return false
	}
	previous := e.bPreviousSource
	if index > 0 {
		previous = e.bInputs[index-1].frame
	}
	if previous == nil || !framesDiffer(previous, e.bInputs[index].frame, e.sceneThreshold) {
		return false
	}
	if index+1 < len(e.bInputs) {
		// 单帧闪光与它的回落均不应作为新场景。
		if !framesDiffer(previous, e.bInputs[index+1].frame, e.sceneThreshold) {
			return false
		}
	}
	if index > 1 && !framesDiffer(e.bInputs[index-2].frame, e.bInputs[index].frame, e.sceneThreshold) {
		return false
	}
	return true
}
