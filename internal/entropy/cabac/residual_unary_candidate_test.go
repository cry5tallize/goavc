package cabac

// residualUnary 解码 Spec: 9.3.3.1.3 的幅值前缀（2..15）。
// 同一前缀只更新一个 Context；合法状态经转移仍合法，且普通 bin
// 不会终止引擎。入口检查一次，循环内仍保留每次归一化及粘性错误。
// C06 联合端点门槛未通过；仅保留测试候选，生产仍逐次调用 Bin。
func (d *Decoder) residualUnary(c *Context) int32 {
	level := int32(2)
	if d.err != nil {
		return level
	}
	if d.terminated || c == nil || c.State > 63 || c.MPS > 1 {
		// 复用 Bin 的检查顺序和错误，不消费任何位。
		d.Bin(c)
		return level
	}
	state, mps := c.State, c.MPS
	for {
		lps := uint32(rangeLPS[state][(d.rangeValue>>6)&3])
		d.rangeValue -= lps
		bin := mps
		if d.offset >= d.rangeValue {
			d.offset -= d.rangeValue
			d.rangeValue = lps
			bin ^= 1
			if state == 0 {
				mps ^= 1
			}
			state = transLPS[state]
		} else {
			state = nextMPS(state)
		}
		d.renormalize()
		if bin == 0 {
			break
		}
		// 即使归一化失败，旧循环也先计入本次返回的 1。
		level++
		if level == 15 || d.err != nil {
			break
		}
	}
	c.State, c.MPS = state, mps
	return level
}
