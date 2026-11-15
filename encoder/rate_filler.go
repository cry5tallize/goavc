package encoder

import (
	"fmt"

	h264 "github.com/cry5tallize/goavc"
)

// padRateControlledAU 只在CBR/VBV码量不足时写filler_data_rbsp，
// Spec: 7.3.2.7。NAL header nal_ref_idc=0,type=12；填充值0xff不需转义。
// 最小NAL含4字节起始码+header+尾位；最多多补5字节，不修改VCL/重建。
// 分配之前按Level预算限界，避免目标码率驱动无界内存分配。
func (e *Encoder) padRateControlledAU(data []byte, target int64) ([]byte, error) {
	if e.cfg.RateControl != h264.RateControlCBR && e.cfg.RateControl != h264.RateControlVBV {
		return data, nil
	}
	if e.cfg.RateControl == h264.RateControlVBV {
		// 向缓冲中心收敛的目标可超过当前可用容量，最多补到精确硬上界。
		target = min(target, e.bitsPerFrame()+e.vbvBufferBits-e.vbvFullness)
	}
	if target <= int64(len(data))*8 {
		return data, nil
	}
	wanted := (target + 7) / 8
	if wanted > int64(e.maxAUBytes) {
		return nil, fmt.Errorf("encoder: filler exceeds access unit budget: %w", h264.ErrResourceLimit)
	}
	extra := max(6, int(wanted)-len(data))
	if len(data) > e.maxAUBytes-extra {
		return nil, fmt.Errorf("encoder: filler exceeds access unit budget: %w", h264.ErrResourceLimit)
	}
	if e.cfg.RateControl == h264.RateControlVBV {
		if ok, _ := e.vbvAccept(len(data) + extra); !ok {
			return data, nil
		}
	}
	out := make([]byte, len(data)+extra)
	copy(out, data)
	copy(out[len(data):], []byte{0, 0, 0, 1, 12})
	for i := len(data) + 5; i < len(out)-1; i++ {
		out[i] = 255
	}
	out[len(out)-1] = 128
	return out, nil
}
