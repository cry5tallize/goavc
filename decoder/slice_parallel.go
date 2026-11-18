package decoder

import (
	"runtime"
	"sync"

	"github.com/cry5tallize/goavc/internal/motion"
	"github.com/cry5tallize/goavc/syntax"
)

const maxSliceWorkers = 64

func (fs *frameState) beginMotionSlice(first, end, id int) {
	for _, g := range [2]*motion.Grid{fs.mv, fs.mv1} {
		if g == nil {
			continue
		}
		switch {
		case fs.sliceEnd != 0:
			g.BeginSliceRange(first, end)
		case fs.sliceOwners != nil:
			g.BeginMappedSlice(fs.sliceOwners, id)
		default:
			g.BeginSlice(first)
		}
	}
}

// 只并行规范化前已按顺序排列的整行 slice；行内切片仍走原串行路径。
func parallelSliceWorkers(threads int, entries []sliceEntry) int {
	if threads == 1 || len(entries) < 2 {
		return 1
	}
	n := min(len(entries), runtime.GOMAXPROCS(0), maxSliceWorkers)
	if threads > 0 {
		n = min(n, threads)
	}
	if n <= 1 {
		return 1
	}
	sps, pps := entries[0].sps, entries[0].pps
	if !sps.FrameMbsOnlyFlag || sps.MbAdaptiveFrameFieldFlag || sps.SeparateColourPlaneFlag || pps.NumSliceGroupsMinus1 != 0 || pps.RedundantPicCntPresentFlag {
		return 1
	}
	width := sps.PicWidthInMbs()
	for i := range entries {
		s := &entries[i]
		if s.partition != nil || s.hdr.RedundantPicCnt != 0 || s.hdr.Type > syntax.SliceTypeI || uint64(s.hdr.FirstMBInSlice)%width != 0 {
			return 1
		}
		if i > 0 && s.hdr.FirstMBInSlice <= entries[i-1].hdr.FirstMBInSlice {
			return 1
		}
	}
	return n
}

// 列表在派发前构建。列表错误与重建错误共用同一序号，避免一个后序
// 列表错误抢过本应先返回的前序重建错误。所有 worker 退出后才归约。
func (d *Decoder) decodeParallelSlices(fs *frameState, entries []sliceEntry, workers int, poc int32) error {
	results := make([]error, len(entries))
	for i := range entries {
		results[i] = d.prepareSliceReferences(&entries[i], poc)
	}
	var wg sync.WaitGroup
	run := func(start int) {
		// 只共享帧 backing，不共享 Grid 描述符、熵状态或 444 残差 scratch。
		view := *fs
		view.d = nil // 任务不得访问 DPB、图像池或实例缓存。
		view.temporary = nil
		mv := *fs.mv
		view.mv = &mv
		var mv1 motion.Grid
		if fs.mv1 != nil {
			mv1 = *fs.mv1
			view.mv1 = &mv1
		}
		if fs.fullResidual != nil {
			view.fullResidual = new([2]planeResidual)
		}
		for i := start; i < len(entries); i += workers {
			if results[i] == nil {
				s := &entries[i]
				view.sliceFirst, view.sliceEnd = s.firstMB, s.endMB
				results[i] = decodeSlice(&view, s, i, s.firstMB, s.endMB)
			}
		}
	}
	for i := 1; i < workers; i++ {
		wg.Go(func() { run(i) })
	}
	run(0)
	wg.Wait()
	for _, err := range results {
		if err != nil {
			return err
		}
	}
	return nil
}
