package filter

import "testing"

// 同一固定解析向量分别沿垂直/水平边界放置，验证步长以采样计。
func high10EdgeVector(t *testing.T, src, want [8]uint16, chroma bool, bs, qp, alphaDiv2, betaDiv2 int) {
	t.Helper()
	for _, horizontal := range []bool{false, true} {
		const stride = 11
		data := make([]uint16, 8*stride)
		for i := range data {
			data[i] = 65535
		}
		for line := range 4 {
			for across, value := range src {
				index := line*stride + across
				if horizontal {
					index = across*stride + line
				}
				data[index] = value
			}
		}
		q0, lineStep, acrossStep := 4, stride, 1
		if horizontal {
			q0, lineStep, acrossStep = 4*stride, 1, stride
		}
		if chroma {
			// 4:2:0 每个 bS 作用两条色度线；分两段测试 lines。
			ChromaEdgeHigh(data, q0, lineStep, acrossStep, bs, qp, alphaDiv2, betaDiv2, 2, 10)
			ChromaEdgeHigh(data, q0+2*lineStep, lineStep, acrossStep, bs, qp, alphaDiv2, betaDiv2, 2, 10)
		} else {
			LumaEdgeHigh(data, q0, lineStep, acrossStep, bs, qp, alphaDiv2, betaDiv2, 10)
		}
		for y := range 8 {
			for x := range stride {
				v := uint16(65535)
				if horizontal && x < 4 {
					v = want[y]
				}
				if !horizontal && y < 4 && x < 8 {
					v = want[x]
				}
				if got := data[y*stride+x]; got != v {
					t.Fatalf("horizontal=%v chroma=%v bs=%d qp=%d (%d,%d): got %d want %d", horizontal, chroma, bs, qp, x, y, got, v)
				}
			}
		}
	}
}

func TestHigh10LumaWeakAnalyticVectors(t *testing.T) {
	src := [8]uint16{499, 501, 500, 503, 527, 528, 529, 530}
	// QP20: alpha=28,beta=12，bS1/2 的 tc0=0，只有两个不缩放的 +1。
	// QP23: alpha=40,beta=16,tc0=4，tc=6；delta 原始值为 9。
	for _, c := range []struct {
		bs, qp, offset int
		want           [8]uint16
	}{
		{0, 23, 0, src},
		{1, 20, 0, [8]uint16{499, 501, 500, 505, 525, 528, 529, 530}},
		{2, 20, 0, [8]uint16{499, 501, 500, 505, 525, 528, 529, 530}},
		{3, 20, 0, [8]uint16{499, 501, 504, 509, 521, 524, 529, 530}},
		{1, 23, 0, [8]uint16{499, 501, 504, 509, 521, 524, 529, 530}},
		// 负语法 QP 直接参与索引；-12+2*20=28，不预加 QpBdOffset。
		{1, -12, 20, [8]uint16{499, 501, 504, 509, 521, 524, 529, 530}},
	} {
		high10EdgeVector(t, src, c.want, false, c.bs, c.qp, c.offset, c.offset)
	}
}

func TestHigh10ChromaWeakAnalyticVectors(t *testing.T) {
	src := [8]uint16{499, 501, 500, 503, 527, 528, 529, 530}
	// 色度 tc=(tc0<<2)+1，而不是 (tc0+1)<<2；p1/q1 永不修改。
	for _, c := range []struct {
		bs, qp int
		want   [8]uint16
	}{
		{0, 23, src},
		{1, 20, [8]uint16{499, 501, 500, 504, 526, 528, 529, 530}},
		{2, 20, [8]uint16{499, 501, 500, 504, 526, 528, 529, 530}},
		{3, 20, [8]uint16{499, 501, 500, 508, 522, 528, 529, 530}},
		{1, 23, [8]uint16{499, 501, 500, 508, 522, 528, 529, 530}},
	} {
		high10EdgeVector(t, src, c.want, true, c.bs, c.qp, 0, 0)
	}
}

func TestHigh10StrongAnalyticVectors(t *testing.T) {
	// QP20 的强滤波额外条件为 |p0-q0| < (28>>2)+2 = 9。
	// +2 必须保持原值；若错误缩放为 +8，第二个向量也会改写 p1/p2。
	for _, c := range []struct{ src, luma, chroma [8]uint16 }{
		{
			[8]uint16{499, 501, 500, 503, 511, 512, 513, 514},
			[8]uint16{499, 502, 504, 505, 508, 510, 512, 514},
			[8]uint16{499, 501, 500, 504, 509, 512, 513, 514},
		},
		{
			[8]uint16{499, 501, 500, 503, 512, 513, 514, 515},
			[8]uint16{499, 501, 500, 504, 510, 513, 514, 515},
			[8]uint16{499, 501, 500, 504, 510, 513, 514, 515},
		},
		// 只有 q 侧满足 |q2-q0|<beta：p0 走短滤波，q0/q1/q2 走长滤波。
		{
			[8]uint16{480, 490, 500, 503, 511, 512, 513, 514},
			[8]uint16{480, 490, 500, 504, 508, 510, 512, 514},
			[8]uint16{480, 490, 500, 504, 509, 512, 513, 514},
		},
	} {
		high10EdgeVector(t, c.src, c.luma, false, 4, 20, 0, 0)
		high10EdgeVector(t, c.src, c.chroma, true, 4, 20, 0, 0)
	}
}

func TestHigh10DeblockThresholdBoundaries(t *testing.T) {
	for _, src := range [][8]uint16{
		{499, 501, 500, 503, 543, 544, 545, 546}, // |p0-q0|=alpha=40。
		{499, 501, 487, 503, 527, 528, 529, 530}, // |p1-p0|=beta=16。
		{499, 501, 500, 503, 527, 543, 529, 530}, // |q1-q0|=beta=16。
	} {
		for _, bs := range []int{1, 2, 3, 4} {
			high10EdgeVector(t, src, src, false, bs, 23, 0, 0)
			high10EdgeVector(t, src, src, true, bs, 23, 0, 0)
		}
	}
	src := [8]uint16{499, 501, 500, 503, 527, 528, 529, 530}
	for _, qp := range []int{-12, 0, 15} {
		high10EdgeVector(t, src, src, false, 4, qp, 0, 0)
		high10EdgeVector(t, src, src, true, 4, qp, 0, 0)
	}
}

func TestHigh10DeblockClip1023AndZero(t *testing.T) {
	for _, c := range []struct{ src, luma, chroma [8]uint16 }{
		{
			[8]uint16{1022, 1022, 1023, 1022, 1023, 952, 1023, 1023},
			[8]uint16{1022, 1022, 1022, 1023, 1014, 1004, 1023, 1023},
			[8]uint16{1022, 1022, 1023, 1023, 1014, 952, 1023, 1023},
		},
		{
			[8]uint16{1, 1, 0, 1, 0, 71, 0, 0},
			[8]uint16{1, 1, 1, 0, 9, 19, 0, 0},
			[8]uint16{1, 1, 0, 0, 9, 71, 0, 0},
		},
	} {
		high10EdgeVector(t, c.src, c.luma, false, 1, 51, 0, 0)
		high10EdgeVector(t, c.src, c.chroma, true, 1, 51, 0, 0)
	}
}
