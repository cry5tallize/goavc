package decoder

import (
	"fmt"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/syntax"
)

// decodeMBAFFCAVLC 延迟处理奇数skip_run末尾的上MB：其场标志属于随后非Skip下MB。
// 先确定宏块对结构，再执行预测，不能先按邻对推导的结构重建然后覆盖元数据。
func decodeMBAFFCAVLC(fs *frameState, s *sliceEntry, sliceID, first, end int) error {
	if first&1 != 0 || end&1 != 0 {
		return fmt.Errorf("decoder: slice splits MBAFF pair: %w", h264.ErrInvalidData)
	}
	fs.mv.BeginSlice(0)
	if fs.mv1 != nil {
		fs.mv1.BeginSlice(0)
	}
	qp := s.hdr.SliceQP
	for scan := first; scan < end; {
		run := uint32(0)
		if s.hdr.Type == syntax.SliceTypeP || s.hdr.Type == syntax.SliceTypeB {
			var err error
			run, err = s.data.ReadUE()
			if err != nil {
				return err
			}
			if run > uint32(end-scan) {
				return fmt.Errorf("decoder: MBAFF skip run overruns slice: %w", h264.ErrInvalidData)
			}
		}
		for i := uint32(0); i < run; i++ {
			if scan >= end {
				return fmt.Errorf("decoder: MBAFF skip run exceeds slice group: %w", h264.ErrInvalidData)
			}
			if scan&1 == 0 {
				addr := fs.rasterMB(scan)
				fs.info[addr].sliceID = sliceID
				if i+1 == run && scan+1 < end {
					if err := s.readPairField(fs, scan); err != nil {
						return err
					}
				} else {
					fs.setPairField(scan, fs.inferPairField(scan))
				}
			}
			if err := fs.decodeMBAFFMB(s, scan, sliceID, &qp, true); err != nil {
				return err
			}
			scan = fs.nextMB(scan)
		}
		if scan == end {
			break
		}
		if scan&1 == 0 {
			addr := fs.rasterMB(scan)
			fs.info[addr].sliceID = sliceID
			if err := s.readPairField(fs, scan); err != nil {
				return err
			}
		}
		if err := fs.decodeMBAFFMB(s, scan, sliceID, &qp, false); err != nil {
			return err
		}
		scan = fs.nextMB(scan)
	}
	return nil
}

func (s *sliceEntry) mbaffSkipFlag(fs *frameState, addr, sliceID int) (bool, error) {
	if s.hdr.Type == syntax.SliceTypeI {
		return false, nil
	}
	fs.info[addr].sliceID = sliceID
	inc := 0
	for _, left := range []bool{true, false} {
		other := fs.neighborMB(addr, left)
		if fs.neighborAvailable(addr, other) && !fs.info[other].skip {
			inc++
		}
	}
	base := 11
	if s.hdr.Type == syntax.SliceTypeB {
		base = 24
	}
	value := s.cabac.bin(base+inc) != 0
	return value, s.cabac.d.Err()
}

func decodeMBAFFCABAC(fs *frameState, s *sliceEntry, sliceID, first, end int) error {
	if first&1 != 0 || end&1 != 0 {
		return fmt.Errorf("decoder: CABAC slice splits MBAFF pair: %w", h264.ErrInvalidData)
	}
	fs.mv.BeginSlice(0)
	if fs.mv1 != nil {
		fs.mv1.BeginSlice(0)
	}
	qp := s.hdr.SliceQP
	for scan := first; scan < end; scan += 2 {
		top, bottom := fs.rasterMB(scan), fs.rasterMB(scan+1)
		fs.info[top].sliceID, fs.info[bottom].sliceID = sliceID, sliceID
		fs.setPairField(scan, fs.inferPairField(scan))
		topSkip, err := s.mbaffSkipFlag(fs, top, sliceID)
		if err != nil {
			return err
		}
		bottomSkip := false
		if topSkip {
			// 下MB的skip上下文可见上MB为Skip，但尚不能提交其像素或运动网格。
			fs.info[top].skip, fs.decoded[top] = true, true
			bottomSkip, err = s.mbaffSkipFlag(fs, bottom, sliceID)
			fs.decoded[top] = false
			if err != nil {
				return err
			}
			if !bottomSkip {
				if err := s.readPairField(fs, scan); err != nil {
					return err
				}
			}
		} else {
			if err := s.readPairField(fs, scan); err != nil {
				return err
			}
		}
		if err := fs.decodeMBAFFMB(s, scan, sliceID, &qp, topSkip); err != nil {
			return err
		}
		if !topSkip {
			bottomSkip, err = s.mbaffSkipFlag(fs, bottom, sliceID)
			if err != nil {
				return err
			}
		}
		if err := fs.decodeMBAFFMB(s, scan+1, sliceID, &qp, bottomSkip); err != nil {
			return err
		}
		engine := s.cabac.d
		last := engine.Terminate() != 0
		if err := engine.Err(); err != nil {
			return err
		}
		if last != (scan+2 == end) {
			return fmt.Errorf("decoder: MBAFF CABAC pair termination at %d: %w", scan/2, h264.ErrInvalidData)
		}
	}
	return nil
}
