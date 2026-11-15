package encoder

import "github.com/goavc/h264/internal/picture"

// releaseFrameMetadata 只归还独占的临时存储；像素仍可能由reference/bPast借用，
// 因而替换e.last不能顺便释放其像素。
func releaseFrameMetadata(s *frameState) {
	if s == nil {
		return
	}
	for _, field := range s.fields {
		releaseFrameMetadata(field)
	}
	s.fields = [2]*frameState{}
	if s.storage != nil {
		storage := s.storage
		s.storage = nil
		s.info, s.nz, s.mvd, s.mv = nil, nil, nil, nil
		s.cnz, s.dcCoded = [2][]uint8{}, [3][]bool{}
		storage.owner.put(storage)
	}
	s.src, s.reference, s.searchReference = nil, nil, nil
}

// releaseFrameState 释放候选重建图像；src/reference 是借用引用，不在这里归还。
func releaseFrameState(s *frameState) {
	if s == nil {
		return
	}
	if s.ownsPic && s.pic != nil {
		s.pic.Release()
		s.pic = nil
	}
	releaseFrameMetadata(s)
}

// releaseOwnedPictures 只释放编码器当前持有的唯一图像。Picture.Release
// 不是可在重新借出后重复调用的通用引用计数，因此先按地址去重。
func (e *Encoder) releaseOwnedPictures() {
	seen := make(map[*picture.Picture]struct{}, 6)
	release := func(pic *picture.Picture) {
		if pic == nil {
			return
		}
		if _, ok := seen[pic]; ok {
			return
		}
		seen[pic] = struct{}{}
		pic.Release()
	}
	release(e.recon)
	release(e.reference)
	release(e.bPast)
	release(e.bFuture)
	if e.last != nil {
		release(e.last.pic)
		for _, field := range e.last.fields {
			if field != nil {
				release(field.pic)
			}
		}
	}
}

func (e *Encoder) releaseIfUnretained(pic *picture.Picture) {
	if pic == nil || pic == e.recon || pic == e.reference || pic == e.bPast || pic == e.bFuture {
		return
	}
	if e.last != nil && pic == e.last.pic {
		return
	}
	pic.Release()
}
