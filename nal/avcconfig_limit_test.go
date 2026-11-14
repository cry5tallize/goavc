package nal

import (
	"errors"
	"testing"

	h264 "github.com/goavc/h264"
)

func TestAVCDecoderConfig_ParameterSetLimit(t *testing.T) {
	if _, err := ParseAVCDecoderConfigWithLimit(baselineRecord(), 4); err != nil {
		t.Fatal("精确参数集上限未接受", err)
	}
	if _, err := ParseAVCDecoderConfigWithLimit(baselineRecord(), 3); !errors.Is(err, h264.ErrResourceLimit) {
		t.Fatal("参数集超限未拒绝", err)
	}
	// 仅提供声称超限的长度，尚无对应载荷；必须先拒绝资源预算，不能先复制。
	if _, err := ParseAVCDecoderConfigWithLimit([]byte{1, 66, 0, 30, 255, 225, 255, 255}, 16); !errors.Is(err, h264.ErrResourceLimit) {
		t.Fatal("分配/读取载荷前未校验上限", err)
	}
	if _, err := ParseAVCDecoderConfigWithLimit(baselineRecord(), -1); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal(err)
	}
}
