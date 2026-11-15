//go:build arm64 && !purego

#include "textflag.h"

TEXT ·residualNEON(SB), NOSPLIT, $0-96
	MOVD $a+0(FP), R0
	MOVD $b+32(FP), R1
	VLD1 (R0), [V0.S4, V1.S4]
	VLD1 (R1), [V2.S4, V3.S4]
	VADD V2.S4, V0.S4, V0.S4
	VADD V3.S4, V1.S4, V1.S4
	MOVD $ret+64(FP), R0
	VST1 [V0.S4, V1.S4], (R0)
	RET
