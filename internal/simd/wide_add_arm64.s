//go:build arm64 && !purego

#include "textflag.h"

TEXT ·wideAdd4NEON(SB), NOSPLIT, $0-96
	MOVD $a+0(FP), R0
	MOVD $b+32(FP), R1
	VLD1 (R0), [V0.D2, V1.D2]
	VLD1 (R1), [V2.D2, V3.D2]
	VADD V2.D2, V0.D2, V4.D2
	VADD V3.D2, V1.D2, V5.D2
	MOVD $out+64(FP), R0
	VST1 [V4.D2, V5.D2], (R0)
	RET
