//go:build arm64 && !purego

#include "textflag.h"

// unsigned max-min避免byte差回绕；VUADDLV先扩为16位，每行<=4080，
// 然后转入64位标量累加，不在byte lane中跨行累加。
TEXT ·sad8x16NEON(SB), NOSPLIT, $0-80
	MOVD a_base+0(FP), R0
	MOVD as+24(FP), R1
	MOVD b_base+32(FP), R2
	MOVD bs+56(FP), R3
	MOVD height+64(FP), R4
	MOVD ZR, R5
sad8_neon_row:
	VLD1 (R0), [V0.B16]
	VLD1 (R2), [V1.B16]
	VUMAX V1.B16, V0.B16, V2.B16
	VUMIN V1.B16, V0.B16, V3.B16
	VSUB V3.B16, V2.B16, V2.B16
	VUADDLV V2.B16, V2
	VMOV V2.H[0], R6
	ADD R6, R5, R5
	ADD R1, R0, R0
	ADD R3, R2, R2
	SUB $1, R4, R4
	CBNZ R4, sad8_neon_row
	MOVD R5, ret+72(FP)
	RET

// uint16每8采样扩宽归约到32位（10位域每组<=8184），再以64位累加。
// 同一条行的两组向量合计只读32字节，末行不依赖padding或另一场。
TEXT ·sad16x16NEON(SB), NOSPLIT, $0-80
	MOVD a_base+0(FP), R0
	MOVD as+24(FP), R1
	MOVD b_base+32(FP), R2
	MOVD bs+56(FP), R3
	LSL $1, R1, R1
	LSL $1, R3, R3
	MOVD height+64(FP), R4
	MOVD ZR, R5
sad16_neon_row:
	VLD1 (R0), [V0.H8, V1.H8]
	VLD1 (R2), [V2.H8, V3.H8]
	VUMAX V2.H8, V0.H8, V4.H8
	VUMIN V2.H8, V0.H8, V5.H8
	VSUB V5.H8, V4.H8, V4.H8
	VUADDLV V4.H8, V4
	VMOV V4.S[0], R6
	ADD R6, R5, R5
	VUMAX V3.H8, V1.H8, V4.H8
	VUMIN V3.H8, V1.H8, V5.H8
	VSUB V5.H8, V4.H8, V4.H8
	VUADDLV V4.H8, V4
	VMOV V4.S[0], R6
	ADD R6, R5, R5
	ADD R1, R0, R0
	ADD R3, R2, R2
	SUB $1, R4, R4
	CBNZ R4, sad16_neon_row
	MOVD R5, ret+72(FP)
	RET
