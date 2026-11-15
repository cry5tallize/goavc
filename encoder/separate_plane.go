package encoder

import (
	"fmt"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/internal/picture"
)

// encodeSeparate 将三个独立 colour_plane_id slice 集合写入同一个 AU。
// 每次只替换 Plane 描述符，I/P/B、熵编码、RD 与滤波仍走同一内核。
func (e *Encoder) encodeSeparate(src *picture.Picture, intra, idr bool, qp int) ([]byte, *frameState, error) {
	e.ensureFrameStorage()
	var recon *picture.Picture
	var err error
	if e.picturePool != nil {
		recon, err = e.picturePool.Get(src.MBWidth, src.MBHeight, src.Format)
	} else {
		recon, err = picture.New(src.MBWidth, src.MBHeight, src.Format)
	}
	if err != nil {
		return nil, nil, err
	}
	var first *frameState
	success := false
	defer func() {
		if !success {
			releaseFrameMetadata(first)
			recon.Release()
		}
	}()
	var data []byte
	for comp := range 3 {
		channel := *e
		channel.frameQP, channel.colourPlaneID = qp, comp
		channel.analysisIndex = comp * 3
		input, output := *src, *recon
		input.Planes[0], output.Planes[0] = src.Planes[comp], recon.Planes[comp]
		channel.planeReconstruction = &output
		if e.reference != nil {
			view := *e.reference
			view.Planes[0] = e.reference.Planes[comp]
			channel.reference = &view
		}
		if e.bPast != nil {
			view := *e.bPast
			view.Planes[0] = e.bPast.Planes[comp]
			channel.bPast = &view
		}
		if e.bFuture != nil {
			view := *e.bFuture
			view.Planes[0] = e.bFuture.Planes[comp]
			channel.bFuture = &view
		}
		if e.fixedWeights != nil {
			weights := *e.fixedWeights
			weights.Weight[0], weights.Offset[0] = weights.Weight[comp], weights.Offset[comp]
			weights.LumaDenom = e.fixedWeights.denom(comp)
			channel.fixedWeights = &weights
		}
		bytes, state, err := channel.encodePlaneAtQP(&input, intra, idr, qp)
		if err == nil && channel.pcm && !channel.cabac {
			// CAVLC PCM直接写源采样；其余通道已经写入重建视图。
			copy(output.Planes[0].Data, input.Planes[0].Data)
			copy(output.Planes[0].Data16, input.Planes[0].Data16)
		}
		if err != nil {
			return nil, nil, err
		}
		if comp == 0 {
			first = state
		} else {
			// 仅首平面保留诊断元数据；其余平面的分析、熵写出和去块均已完成。
			releaseFrameMetadata(state)
		}
		// 三集合共用一个AU预算，不能把每个集合各自的上限相加。
		if len(bytes) > e.maxAUBytes-len(data) {
			return nil, nil, fmt.Errorf("encoder: separate colour plane access unit exceeds %d bytes: %w", e.maxAUBytes, h264.ErrResourceLimit)
		}
		data = append(data, bytes...)
	}
	if first == nil {
		first = &frameState{}
	}
	first.pic = recon
	first.ownsPic = true
	success = true
	return data, first, nil
}
