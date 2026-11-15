package encoder

import "github.com/cry5tallize/goavc/internal/transform"

// QPY仅用于语法和上下文；正逆变换统一使用加上位深偏移后的QP'。
// 调用前配置和逐宏块QP已完成校验，故共享转换不会失败。
func (s *frameState) lumaQP(qpy int) int {
	// 共同444的色度借用亮度变换内核，但QP仍按色度非线性映射。
	if s.plane > 0 && !s.independent {
		qp, _ := transform.ScaleChromaQP(qpy, 0, s.pic.Planes[0].BitDepth)
		return qp
	}
	qp, _ := transform.ScaleQP(qpy, s.pic.Planes[0].BitDepth)
	return qp
}

func (s *frameState) chromaQP(qpy int) int {
	// 独立平面才使用QPY；共同444与420/422均使用规范QPC映射。
	if s.independent {
		return s.lumaQP(qpy)
	}
	qp, _ := transform.ScaleChromaQP(qpy, 0, s.pic.Planes[0].BitDepth)
	return qp
}

func (s *frameState) qpDelta(current, previous int) int {
	modulus := 52 + 6*(s.pic.Planes[0].BitDepth-8)
	delta := current - previous
	if delta >= modulus/2 {
		delta -= modulus
	}
	if delta < -modulus/2 {
		delta += modulus
	}
	return delta
}
