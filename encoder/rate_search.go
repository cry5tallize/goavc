package encoder

// qpSearch 先从同帧型已提交的反馈QP向相邻级扩展，跨过目标后改为二分。
// 新帧型仍从完整语法QP域二分；不把单调启发式当成合法性证明，最终无可行
// 候选时由encodeRateControlled穷举剩余QP。失败的尝试不写入反馈历史。
type qpSearch struct {
	low, high, qp, step int
	direction           int
	bracketed           bool
}

func newQPSearch(qp int, known bool, minimum int) qpSearch {
	if !known {
		qp = (minimum + 51) / 2
	}
	return qpSearch{low: minimum, high: 51, qp: qp, step: 1, bracketed: !known}
}

func (s *qpSearch) next() (int, bool) {
	if s.low > s.high {
		return 0, false
	}
	return s.qp, true
}

func (s *qpSearch) observe(higher bool) {
	direction := -1
	if higher {
		s.low = s.qp + 1
		direction = 1
	} else {
		s.high = s.qp - 1
	}
	if s.direction != 0 && s.direction != direction {
		s.bracketed = true
	}
	s.direction = direction
	if s.bracketed {
		s.qp = (s.low + s.high) / 2
	} else {
		s.qp = min(s.high, max(s.low, s.qp+direction*s.step))
		s.step *= 2
	}
}

func (e *Encoder) ratePictureKind(intra bool) int {
	if e.encodingB {
		return 2
	}
	if intra {
		return 0
	}
	return 1
}
