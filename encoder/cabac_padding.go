package encoder

import "github.com/cry5tallize/goavc/bitstream"

// cabacZeroWordPayload 保留旧 420 测试入口；编码路径传入实际 RawMbBits。
// 将不等式乘96消去分数：96*bins <= 1024*nalBytes + 3*RawMbBits*mbs。
// 每 slice 分别满足该式，则相加后整 picture 也满足；无需持有全部 slice。
// 调用方计数由已限界图像生成，uint64 中间值不会溢出。
func cabacZeroWordPayload(rbsp []byte, bins uint64, mbs, bitDepth int) []byte {
	return cabacZeroWordPayloadFormat(rbsp, bins, mbs, 384*bitDepth)
}

// cabacZeroWordPayloadFormat 实施 Spec: 7.4.2.10 的 bin 密度上限，
// 不计 Annex-B start code，NAL header 计 1 字节。
func cabacZeroWordPayloadFormat(rbsp []byte, bins uint64, mbs, rawMbBits int) []byte {
	ebsp := bitstream.RBSPToEBSP(rbsp)
	allowance := uint64(mbs) * uint64(rawMbBits) * 3
	if bins*96 <= uint64(len(ebsp)+1)*1024+allowance {
		return ebsp
	}
	needed := (bins*96 - allowance + 1023) / 1024
	missing := needed - uint64(len(ebsp)+1)
	words := (missing + 2) / 3
	// cabac_zero_word 是 RBSP 的16个零位；封装末尾额外03防止终端00 00，
	// 解码器按已知 CABAC PPS 接受该专用尾部。不可用于通用参数集封装。
	padded := make([]byte, len(rbsp)+int(words)*2)
	copy(padded, rbsp)
	ebsp = bitstream.RBSPToEBSP(padded)
	if len(ebsp) >= 2 && ebsp[len(ebsp)-2] == 0 && ebsp[len(ebsp)-1] == 0 {
		ebsp = append(ebsp, 3)
	}
	return ebsp
}
