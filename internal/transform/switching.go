package transform

import (
	"fmt"

	h264 "github.com/cry5tallize/goavc"
)

// 规范依据：Rec. ITU-T H.264 (08/2021)，8.6，式8-414至8-441；
// 其中8-420/432与8-425/429/435/439的符号位置、8-431与8-441的DC域
// 有意逐式保留，不能用普通P重建、统一死区或统一DC反量化替换。
// 主源：https://www.itu.int/rec/dologin_pub.asp?id=T-REC-H.264-202108-S!!PDF-E&lang=e&type=items

// SwitchingMode 选择规范8.6的系数域重建过程，而不是普通P残差相加。
// SP/SI属于8-bit Extended工具；普通I宏块（包括SI slice中的I宏块）不使用此内核。
type SwitchingMode uint8

const (
	// SPPrimary 对应sp_for_switch_flag=0：先在QPY域合成，再量化至QSY域。
	SPPrimary SwitchingMode = iota
	// SPSwitching 对应sp_for_switch_flag=1：先量化预测，再加QSY域的码流level。
	SPSwitching
	// SI 使用SI宏块的空间预测样本，系数域操作与SPSwitching一致。
	SI
)

// ReconstructSwitchingLuma4x4 重建一个SP帧间或SI宏块的亮度4×4块。
// Spec: 8.6.1.1、8.6.2.1。pred是MC/空间预测后的采样，levels已逆扫描为
// 光栅顺序；qpY和qsY分别为语法QPY、QSY（0..51），不能互换或使用QP'。
// dst是裁剪后的完整重建采样，调用方不得再加pred。SP Skip也必须以零levels
// 调用本过程，不能直接复制预测。允许dst与pred相同；失败不改变dst。
func ReconstructSwitchingLuma4x4(dst, pred *[16]uint8, levels *[16]int32, qpY, qsY int, mode SwitchingMode) error {
	if dst == nil || pred == nil || levels == nil {
		return fmt.Errorf("transform: nil switching luma block: %w", h264.ErrInvalidData)
	}
	if err := validateSwitchingQP(qpY, qsY, mode); err != nil {
		return err
	}
	var samples, prediction, combined, scaled, reconstructed [16]int32
	for i, v := range pred {
		samples[i] = int32(v)
	}
	Forward4x4(&prediction, &samples)
	for i, level := range levels {
		v, err := switchingCoefficient(prediction[i], level, qpY, qsY, i, mode, false)
		if err != nil {
			return err
		}
		combined[i] = v
	}
	if err := Dequant4x4(&scaled, &combined, qsY); err != nil {
		return err
	}
	IDCT4x4(&reconstructed, &scaled)
	var out [16]uint8
	for i, v := range reconstructed {
		out[i] = clipSwitchingSample(v)
	}
	*dst = out
	return nil
}

// ReconstructSwitchingChroma420 重建一个分量的8×8色度采样（Cb、Cr分别调用）。
// Spec: 8.6.1.2、8.6.2.2。dc是原始ChromaDCLevel列表（不作4×4逆扫描），ac的
// 四个4×4块按{左上,右上,左下,右下}排列，每块已逆扫描且ac[b][0]必须为零。
// qpY/qsY为亮度语法QP，offset为该分量PPS偏移；分别映射为QPC/QSC，
// 不能先映射QPY再从结果导出QSC。dst是完整重建采样，允许与pred相同。
// DC保持Hadamard域直至完成合成和QSC量化，不能逐4×4块作为普通DC处理。
func ReconstructSwitchingChroma420(dst, pred *[64]uint8, dc *[4]int32, ac *[4][16]int32, qpY, qsY, offset int, mode SwitchingMode) error {
	if dst == nil || pred == nil || dc == nil || ac == nil {
		return fmt.Errorf("transform: nil switching chroma block: %w", h264.ErrInvalidData)
	}
	if err := validateSwitchingQP(qpY, qsY, mode); err != nil {
		return err
	}
	if offset < -12 || offset > 12 {
		return fmt.Errorf("transform: switching chroma offset %d outside -12..12: %w", offset, h264.ErrInvalidData)
	}
	qp, qs := ChromaQP(qpY, offset), ChromaQP(qsY, offset)
	var prediction [4][16]int32
	var predictionDC, transformedDC, combinedDC, scaledDC [4]int32
	for block := range 4 {
		if ac[block][0] != 0 {
			return fmt.Errorf("transform: switching chroma AC contains DC: %w", h264.ErrInvalidData)
		}
		var samples [16]int32
		x, y := (block%2)*4, (block/2)*4
		for row := range 4 {
			for col := range 4 {
				samples[row*4+col] = int32(pred[(y+row)*8+x+col])
			}
		}
		Forward4x4(&prediction[block], &samples)
		predictionDC[block] = prediction[block][0]
	}
	// Spec: 8-427的预测矩阵是空间光栅，但8-428/440按[j*2+i]注入码流，
	// 8-431/441也按[j*2+i]写回。把两次转置移至预测端，便可保留dc列表
	// 与输出块的自然顺序；不能只交换码流level或只交换最终输出。
	predictionDC[1], predictionDC[2] = predictionDC[2], predictionDC[1]
	ForwardChromaDC(&transformedDC, &predictionDC)
	for i, p := range transformedDC {
		v := int64(p)
		if mode == SPPrimary {
			// LevelScale=16*normAdjust、A00=16，规范>>9即flat下的>>1。
			v += (int64(dc[i]) * int64(levelScale4x4[qp%6][0]) << uint(qp/6)) >> 1
		}
		value, err := switchingInt32(v)
		if err != nil {
			return err
		}
		quantized := switchingQuantize(value, quantScale[qs%6][0], 16+qs/6, true)
		v = int64(quantized)
		if mode != SPPrimary {
			v += int64(dc[i])
		}
		combinedDC[i], err = switchingInt32(v)
		if err != nil {
			return err
		}
	}
	if mode == SPPrimary {
		// Spec: 8-430、8-431。primary逆Hadamard后乘LevelScale/32。
		if err := DequantChromaDC(&scaledDC, &combinedDC, qs); err != nil {
			return err
		}
	} else {
		// Spec: 8-440、8-441。switching/SI的c00直接等于f，明确没有
		// primary的LevelScale/32；不能把两种DC域合并为同一反量化路径。
		// 合法16-bit level界同时保证复用int32 Hadamard蝶形不会溢出。
		for _, v := range combinedDC {
			if v < -1<<15 || v > 1<<15-1 {
				return fmt.Errorf("transform: switching chroma DC level outside signed 16-bit range: %w", h264.ErrInvalidData)
			}
		}
		ForwardChromaDC(&scaledDC, &combinedDC)
	}
	var out [64]uint8
	for block := range 4 {
		var combined, scaled, reconstructed [16]int32
		for i := 1; i < 16; i++ {
			v, err := switchingCoefficient(prediction[block][i], ac[block][i], qp, qs, i, mode, true)
			if err != nil {
				return err
			}
			combined[i] = v
		}
		if err := Dequant4x4(&scaled, &combined, qs); err != nil {
			return err
		}
		scaled[0] = scaledDC[block]
		IDCT4x4(&reconstructed, &scaled)
		x, y := (block%2)*4, (block/2)*4
		for row := range 4 {
			for col := range 4 {
				out[(y+row)*8+x+col] = clipSwitchingSample(reconstructed[row*4+col])
			}
		}
	}
	*dst = out
	return nil
}

func validateSwitchingQP(qp, qs int, mode SwitchingMode) error {
	if qp < 0 || qp > MaxQP || qs < 0 || qs > MaxQP {
		return fmt.Errorf("transform: switching QP/QS %d/%d outside 0..51: %w", qp, qs, h264.ErrInvalidData)
	}
	if mode > SI {
		return fmt.Errorf("transform: invalid switching mode %d: %w", mode, h264.ErrInvalidData)
	}
	return nil
}

// switchingCoefficient 保留规范的运算次序：primary的有符号右移先于量化，
// 不能用普通逆量化加IDCT再正变换替代，否则会引入额外空间域舍入。
func switchingCoefficient(pred, residual int32, qp, qs, index int, mode SwitchingMode, chroma bool) (int32, error) {
	v := int64(pred)
	if mode == SPPrimary {
		class := (index/4%2 + index%2)
		a := [...]int64{16, 20, 25}
		// Spec: 8.6.1，LevelScale=16*normAdjust；>>10精确化简为flat下的>>6。
		v += (int64(residual) * int64(levelScale4x4[qp%6][index]) * a[class] << uint(qp/6)) >> 6
	}
	value, err := switchingInt32(v)
	if err != nil {
		return 0, err
	}
	class := (index/4%2 + index%2)
	quantized := switchingQuantize(value, quantScale[qs%6][class], 15+qs/6, chroma)
	v = int64(quantized)
	if mode != SPPrimary {
		v += int64(residual)
	}
	return switchingInt32(v)
}

func switchingQuantize(v int32, multiplier int64, shift int, signedShift bool) int32 {
	if !signedShift {
		// 8.6亮度使用Sign(v)*(rounded Abs(v))，不是编码器的1/3或1/6死区。
		return quantizeWithDeadZone(v, multiplier, shift, 2)
	}
	// 色度公式把Sign放在右移之前；负数必须算术右移，不能先量化绝对值再取负。
	a := int64(v)
	if a < 0 {
		a = -a
	}
	q := a*multiplier + (int64(1) << uint(shift-1))
	if v < 0 {
		q = -q
	} else if v == 0 {
		return 0
	}
	return int32(q >> uint(shift))
}

func switchingInt32(v int64) (int32, error) {
	if v < -1<<31 || v > 1<<31-1 {
		return 0, fmt.Errorf("transform: switching coefficient overflow: %w", h264.ErrInvalidData)
	}
	return int32(v), nil
}

func clipSwitchingSample(v int32) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}
