package encoder

import (
	"errors"
	"fmt"
	"math"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/internal/picture"
)

func (e *Encoder) encodeAtQP(src *picture.Picture, intra, idr bool, qp int) ([]byte, *frameState, error) {
	old := e.frameQP
	e.frameQP = qp
	defer func() { e.frameQP = old }()
	if e.encodingB {
		return e.encodeB(src)
	}
	if e.cabac {
		return e.encodeCABAC(src, intra, idr)
	}
	if e.pcm {
		data, err := e.encodePCM(src)
		return data, nil, err
	}
	if !intra {
		return e.encodeP(src)
	}
	return e.encodeIntra(src, idr)
}

func (e *Encoder) encodeRateControlled(src *picture.Picture, intra, idr bool) ([]byte, *frameState, int, error) {
	if e.cfg.RateControl == h264.RateControlCQP {
		data, state, err := e.encodeAtQP(src, intra, idr, e.cfg.QP)
		return data, state, e.cfg.QP, err
	}
	target := e.bitsPerFrame()
	if e.cfg.RateControl == h264.RateControlCBR || e.cfg.RateControl == h264.RateControlVBV {
		target += e.vbvBufferBits/2 - e.vbvFullness
	}
	if target < 1 {
		target = 1
	}
	low, high := 0, 51
	var best []byte
	var bestState *frameState
	bestQP := e.frameQP
	bestDelta := int64(math.MaxInt64)
	feasible := false
	unsupported, oversized := false, false
	var tried [52]bool
	for attempt := 0; attempt < 7 && low <= high; attempt++ {
		qp := (low + high) / 2
		tried[qp] = true
		data, state, err := e.encodeAtQP(src, intra, idr, qp)
		if err != nil {
			if errors.Is(err, h264.ErrUnsupported) {
				unsupported = true
				low = qp + 1
				continue
			}
			return nil, nil, qp, err
		}
		codedBits := int64(len(data)) * 8
		data, err = e.padRateControlledAU(data, target)
		if err != nil {
			oversized = true
			low = qp + 1
			continue
		}
		if len(data) > e.maxAUBytes {
			oversized = true
			low = qp + 1
			continue
		}
		ok, _ := e.vbvAccept(len(data))
		if e.cfg.RateControl != h264.RateControlVBV || ok {
			delta := int64(len(data))*8 - target
			if delta < 0 {
				delta = -delta
			}
			if !feasible || delta < bestDelta || delta == bestDelta && e.preferRateQP(qp, bestQP) {
				best, bestState, bestQP, bestDelta = data, state, qp, delta
			}
			feasible = true
		}
		// Filler只改变传输码量，不能掩盖VCL码量的QP搜索方向。
		if codedBits > target {
			low = qp + 1
		} else {
			high = qp - 1
		}
	}
	// QP 与实际码量及 Profile 合法性不保证严格单调。快速搜索未找到
	// 可行候选时穷举未试过的 QP，确保拒绝前检查整个合法范围。
	if !feasible {
		for qp := range 52 {
			if tried[qp] {
				continue
			}
			data, state, err := e.encodeAtQP(src, intra, idr, qp)
			if err != nil {
				if errors.Is(err, h264.ErrUnsupported) {
					unsupported = true
					continue
				}
				return nil, nil, qp, err
			}
			data, err = e.padRateControlledAU(data, target)
			if err != nil {
				oversized = true
				continue
			}
			if len(data) > e.maxAUBytes {
				oversized = true
				continue
			}
			ok, _ := e.vbvAccept(len(data))
			if e.cfg.RateControl == h264.RateControlVBV && !ok {
				continue
			}
			delta := int64(len(data))*8 - target
			if delta < 0 {
				delta = -delta
			}
			if !feasible || delta < bestDelta || delta == bestDelta && e.preferRateQP(qp, bestQP) {
				best, bestState, bestQP, bestDelta = data, state, qp, delta
			}
			feasible = true
		}
	}
	if !feasible {
		if unsupported && !oversized {
			return nil, nil, bestQP, fmt.Errorf("encoder: no QP candidate satisfies profile limits: %w", h264.ErrUnsupported)
		}
		return nil, nil, bestQP, fmt.Errorf("encoder: no QP satisfies rate-control and access-unit bounds: %w", h264.ErrResourceLimit)
	}
	return best, bestState, bestQP, nil
}

func (e *Encoder) preferRateQP(candidate, current int) bool {
	if e.cfg.RateControl == h264.RateControlCBR || e.cfg.RateControl == h264.RateControlVBV {
		return candidate < current
	}
	return absInt(candidate-e.lastQP) < absInt(current-e.lastQP)
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// vbvAccept 检查本帧写入与一个帧周期的恒定码率排空。
// 计算暂存，SendFrame 提交 AU 后才更新 fullness。
func (e *Encoder) vbvAccept(frameBytes int) (bool, int) {
	if e.cfg.RateControl != h264.RateControlVBV {
		return true, 0
	}
	drain := e.bitsPerFrame()
	fullness := e.vbvFullness + int64(frameBytes)*8 - drain
	if fullness > e.vbvBufferBits {
		return false, 1
	}
	if fullness < 0 {
		return false, -1
	}
	return true, 0
}

func (e *Encoder) commitRateControl(frameBytes int) {
	e.lastFrameBits = int64(frameBytes) * 8
	if e.cfg.RateControl == h264.RateControlCQP {
		return
	}
	target := e.bitsPerFrame()
	numerator := int64(e.cfg.Bitrate)*int64(e.cfg.FPSDen) + e.rcRemainder
	e.rcRemainder = numerator % int64(e.cfg.FPSNum)
	if target <= 0 {
		return
	}
	if e.cfg.RateControl == h264.RateControlCBR || e.cfg.RateControl == h264.RateControlVBV {
		e.vbvFullness += e.lastFrameBits - target
	}
	qpDelta := int(math.Round(6 * math.Log2(float64(max(int64(1), e.lastFrameBits))/float64(target))))
	qpDelta = max(-4, min(4, qpDelta))
	e.frameQP = max(0, min(51, e.lastQP+qpDelta))
}

func (e *Encoder) bitsPerFrame() int64 {
	bitrate, den := int64(e.cfg.Bitrate), int64(e.cfg.FPSDen)
	if den != 0 && bitrate > math.MaxInt64/den {
		return math.MaxInt64
	}
	return (bitrate*den + e.rcRemainder) / int64(e.cfg.FPSNum)
}

func (e *Encoder) sceneChanged(src *picture.Picture) bool {
	if e.sceneThreshold == 0 || e.reference == nil {
		return false
	}
	var difference, samples int64
	for comp := range src.Planes {
		for y := 0; y < src.Planes[comp].Height; y++ {
			a, b := src.Planes[comp].Row(y), e.reference.Planes[comp].Row(y)
			for x, value := range a {
				delta := int64(value) - int64(b[x])
				if delta < 0 {
					delta = -delta
				}
				difference += delta
				samples++
			}
		}
	}
	return difference*100 > samples*255*int64(e.sceneThreshold)
}

// LastQP 返回最近成功编码帧使用的基准 QP，不含 AQ 宏块偏移。
func (e *Encoder) LastQP() int { return e.lastQP }

// LastOutputBits 返回最近成功编码访问单元的实际大小，不含码流外部封装。
func (e *Encoder) LastOutputBits() int64 { return e.lastFrameBits }

// RateControlStats 是最近一次成功编码后的码控观测值。
type RateControlStats struct {
	QP            int
	LastFrameBits int64
	VBVFullness   int64
	VBVBufferBits int64
}

// RateControlStats 返回值副本，不暴露可变的编码器状态。
func (e *Encoder) RateControlStats() RateControlStats {
	return RateControlStats{QP: e.lastQP, LastFrameBits: e.lastFrameBits, VBVFullness: e.vbvFullness, VBVBufferBits: e.vbvBufferBits}
}
