//go:build arm64 && !purego

#include "textflag.h"

// 固定值参数 a、b 各16字节，返回值16字节，不访问像素缓冲。
TEXT ·filterDeltaNEON(SB), NOSPLIT, $0-48
	MOVD $a+0(FP), R0
	VLD1 (R0), [V0.S4, V1.S4]
	VADD V1.S4, V0.S4, V0.S4
	MOVD $out+32(FP), R0
	VST1 [V0.S4], (R0)
	RET
