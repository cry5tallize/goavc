//go:build arm64 && !purego

#include "textflag.h"

// 先去掉各64位元素的零高半部，再做无符号widening乘法；只用基线ASIMD。
TEXT ·quantMultiplyNEON(SB), NOSPLIT, $0-96
 MOVD $a+0(FP), R0
 MOVD $b+32(FP), R1
 VLD1 (R0), [V0.D2, V1.D2]
 VLD1 (R1), [V2.D2, V3.D2]
 VUZP1 V0.S4, V0.S4, V0.S4
 VUZP1 V1.S4, V1.S4, V1.S4
 VUZP1 V2.S4, V2.S4, V2.S4
 VUZP1 V3.S4, V3.S4, V3.S4
 VUMULL V0.S2, V2.S2, V4.D2
 VUMULL V1.S2, V3.S2, V5.D2
 MOVD $out+64(FP), R0
 VST1 [V4.D2, V5.D2], (R0)
 RET
