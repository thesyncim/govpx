//go:build arm64 && !purego

// ARMv8 NEON port of libvpx v1.16.0 vp9/encoder/arm/neon/vp9_dct_neon.c
// vp9_fht16x16_neon hybrid-transform path. Instruction WORDs are
// generated from a local -fno-stack-protector build of the pinned libvpx source.

#include "textflag.h"

// forwardHT16x16NEON ABI ($0-32):
//   input+0(FP)   *int16
//   output+8(FP)  *int16
//   stride+16(FP) int (in int16 elements)
//   txType+24(FP) int (ADST_DCT, DCT_ADST, ADST_ADST only)
TEXT ·forwardHT16x16NEON(SB), NOSPLIT, $0-32
	MOVD	input+0(FP), R0
	MOVD	output+8(FP), R1
	MOVD	stride+16(FP), R2
	MOVD	txType+24(FP), R3
	WORD	$0xa9be4ff4	// 0d80: stp x20, x19, [sp, #-0x20]!
	WORD	$0xa9017bfd	// 0d84: stp x29, x30, [sp, #0x10]
	WORD	$0x910043fd	// 0d88: add x29, sp, #0x10
	WORD	$0xd10803ff	// 0d8c: sub sp, sp, #0x200
	WORD	$0xaa0103f3	// 0d90: mov x19, x1
	WORD	$0x7100087f	// 0d94: cmp w3, #0x2
	WORD	$0x540021a0	// 0d98: b.eq 0x11cc <_vp9_fht16x16_neon+0x44c>
	WORD	$0x7100047f	// 0d9c: cmp w3, #0x1
	WORD	$0x540000e0	// 0da0: b.eq 0xdbc <_vp9_fht16x16_neon+0x3c>
	WORD	$0x35002d63	// 0da4: cbnz w3, 0x1350 <_vp9_fht16x16_neon+0x5d0>
	WORD	$0xaa1303e1	// 0da8: mov x1, x19
	WORD	$0x910803ff	// 0dac: add sp, sp, #0x200
	WORD	$0xa9417bfd	// 0db0: ldp x29, x30, [sp, #0x10]
	WORD	$0xa8c24ff4	// 0db4: ldp x20, x19, [sp], #0x20
	WORD	$0x14000000	// 0db8: b 0xdb8 <_vp9_fht16x16_neon+0x38>
	WORD	$0x3dc00000	// 0dbc: ldr q0, [x0]
	WORD	$0x937f7c48	// 0dc0: sbfiz x8, x2, #1, #32
	WORD	$0x3ce86801	// 0dc4: ldr q1, [x0, x8]
	WORD	$0x4f125400	// 0dc8: shl.8h v0, v0, #0x2
	WORD	$0x4f125421	// 0dcc: shl.8h v1, v1, #0x2
	WORD	$0xad0807e0	// 0dd0: stp q0, q1, [sp, #0x100]
	WORD	$0x937e7c49	// 0dd4: sbfiz x9, x2, #2, #32
	WORD	$0x3ce96800	// 0dd8: ldr q0, [x0, x9]
	WORD	$0x93407c4c	// 0ddc: sxtw x12, w2
	WORD	$0x8b0c010d	// 0de0: add x13, x8, x12
	WORD	$0xd37ff9aa	// 0de4: lsl x10, x13, #1
	WORD	$0x3cea6801	// 0de8: ldr q1, [x0, x10]
	WORD	$0x4f125400	// 0dec: shl.8h v0, v0, #0x2
	WORD	$0x4f125421	// 0df0: shl.8h v1, v1, #0x2
	WORD	$0xad0907e0	// 0df4: stp q0, q1, [sp, #0x120]
	WORD	$0x937d7c4b	// 0df8: sbfiz x11, x2, #3, #32
	WORD	$0x3ceb6800	// 0dfc: ldr q0, [x0, x11]
	WORD	$0x8b0c012c	// 0e00: add x12, x9, x12
	WORD	$0xd37ff98c	// 0e04: lsl x12, x12, #1
	WORD	$0x3cec6801	// 0e08: ldr q1, [x0, x12]
	WORD	$0x4f125400	// 0e0c: shl.8h v0, v0, #0x2
	WORD	$0x4f125421	// 0e10: shl.8h v1, v1, #0x2
	WORD	$0xad0a07e0	// 0e14: stp q0, q1, [sp, #0x140]
	WORD	$0xd37ef5ad	// 0e18: lsl x13, x13, #2
	WORD	$0x3ced6800	// 0e1c: ldr q0, [x0, x13]
	WORD	$0x937c7c4f	// 0e20: sbfiz x15, x2, #4, #32
	WORD	$0xcb0801ee	// 0e24: sub x14, x15, x8
	WORD	$0x3cee6801	// 0e28: ldr q1, [x0, x14]
	WORD	$0x4f125400	// 0e2c: shl.8h v0, v0, #0x2
	WORD	$0x4f125421	// 0e30: shl.8h v1, v1, #0x2
	WORD	$0xad0b07e0	// 0e34: stp q0, q1, [sp, #0x160]
	WORD	$0x8b0f0010	// 0e38: add x16, x0, x15
	WORD	$0x3dc00200	// 0e3c: ldr q0, [x16]
	WORD	$0x4f125400	// 0e40: shl.8h v0, v0, #0x2
	WORD	$0x3ce86a01	// 0e44: ldr q1, [x16, x8]
	WORD	$0x4f125421	// 0e48: shl.8h v1, v1, #0x2
	WORD	$0xad0c07e0	// 0e4c: stp q0, q1, [sp, #0x180]
	WORD	$0x3ce96a00	// 0e50: ldr q0, [x16, x9]
	WORD	$0x3cea6a01	// 0e54: ldr q1, [x16, x10]
	WORD	$0x4f125400	// 0e58: shl.8h v0, v0, #0x2
	WORD	$0x4f125421	// 0e5c: shl.8h v1, v1, #0x2
	WORD	$0xad0d07e0	// 0e60: stp q0, q1, [sp, #0x1a0]
	WORD	$0x3ceb6a00	// 0e64: ldr q0, [x16, x11]
	WORD	$0x4f125400	// 0e68: shl.8h v0, v0, #0x2
	WORD	$0x3cec6a01	// 0e6c: ldr q1, [x16, x12]
	WORD	$0x4f125421	// 0e70: shl.8h v1, v1, #0x2
	WORD	$0xad0e07e0	// 0e74: stp q0, q1, [sp, #0x1c0]
	WORD	$0x3ced6a00	// 0e78: ldr q0, [x16, x13]
	WORD	$0x4f125400	// 0e7c: shl.8h v0, v0, #0x2
	WORD	$0x3cee6a01	// 0e80: ldr q1, [x16, x14]
	WORD	$0x4f125421	// 0e84: shl.8h v1, v1, #0x2
	WORD	$0xad0f07e0	// 0e88: stp q0, q1, [sp, #0x1e0]
	WORD	$0x3cc10c00	// 0e8c: ldr q0, [x0, #0x10]!
	WORD	$0x3ce86801	// 0e90: ldr q1, [x0, x8]
	WORD	$0x4f125400	// 0e94: shl.8h v0, v0, #0x2
	WORD	$0x4f125421	// 0e98: shl.8h v1, v1, #0x2
	WORD	$0xad0007e0	// 0e9c: stp q0, q1, [sp]
	WORD	$0x3ce96800	// 0ea0: ldr q0, [x0, x9]
	WORD	$0x4f125400	// 0ea4: shl.8h v0, v0, #0x2
	WORD	$0x3cea6801	// 0ea8: ldr q1, [x0, x10]
	WORD	$0x4f125421	// 0eac: shl.8h v1, v1, #0x2
	WORD	$0xad0107e0	// 0eb0: stp q0, q1, [sp, #0x20]
	WORD	$0x3ceb6800	// 0eb4: ldr q0, [x0, x11]
	WORD	$0x4f125400	// 0eb8: shl.8h v0, v0, #0x2
	WORD	$0x3cec6801	// 0ebc: ldr q1, [x0, x12]
	WORD	$0x4f125421	// 0ec0: shl.8h v1, v1, #0x2
	WORD	$0xad0207e0	// 0ec4: stp q0, q1, [sp, #0x40]
	WORD	$0x3ced6800	// 0ec8: ldr q0, [x0, x13]
	WORD	$0x3cee6801	// 0ecc: ldr q1, [x0, x14]
	WORD	$0x4f125400	// 0ed0: shl.8h v0, v0, #0x2
	WORD	$0x4f125421	// 0ed4: shl.8h v1, v1, #0x2
	WORD	$0xad0307e0	// 0ed8: stp q0, q1, [sp, #0x60]
	WORD	$0x8b0f000f	// 0edc: add x15, x0, x15
	WORD	$0x3dc001e0	// 0ee0: ldr q0, [x15]
	WORD	$0x4f125400	// 0ee4: shl.8h v0, v0, #0x2
	WORD	$0x3ce869e1	// 0ee8: ldr q1, [x15, x8]
	WORD	$0x4f125421	// 0eec: shl.8h v1, v1, #0x2
	WORD	$0xad0407e0	// 0ef0: stp q0, q1, [sp, #0x80]
	WORD	$0x3ce969e0	// 0ef4: ldr q0, [x15, x9]
	WORD	$0x4f125400	// 0ef8: shl.8h v0, v0, #0x2
	WORD	$0x3cea69e1	// 0efc: ldr q1, [x15, x10]
	WORD	$0x4f125421	// 0f00: shl.8h v1, v1, #0x2
	WORD	$0xad0507e0	// 0f04: stp q0, q1, [sp, #0xa0]
	WORD	$0x3ceb69e0	// 0f08: ldr q0, [x15, x11]
	WORD	$0x3cec69e1	// 0f0c: ldr q1, [x15, x12]
	WORD	$0x4f125400	// 0f10: shl.8h v0, v0, #0x2
	WORD	$0x4f125421	// 0f14: shl.8h v1, v1, #0x2
	WORD	$0xad0607e0	// 0f18: stp q0, q1, [sp, #0xc0]
	WORD	$0x3ced69e0	// 0f1c: ldr q0, [x15, x13]
	WORD	$0x4f125400	// 0f20: shl.8h v0, v0, #0x2
	WORD	$0x3cee69e1	// 0f24: ldr q1, [x15, x14]
	WORD	$0x4f125421	// 0f28: shl.8h v1, v1, #0x2
	WORD	$0xad0707e0	// 0f2c: stp q0, q1, [sp, #0xe0]
	WORD	$0x910403e0	// 0f30: add x0, sp, #0x100
	WORD	$0x910003e1	// 0f34: mov x1, sp
	WORD	$0x9400022d	// 0f38: bl 0xf38 <_vp9_fht16x16_neon+0x1b8>
	WORD	$0xad4b07e0	// 0f3c: ldp q0, q1, [sp, #0x160]
	WORD	$0x6f110422	// 0f40: ushr.8h v2, v1, #0xf
	WORD	$0x6f110403	// 0f44: ushr.8h v3, v0, #0xf
	WORD	$0xad4a17e4	// 0f48: ldp q4, q5, [sp, #0x140]
	WORD	$0x6f1104a6	// 0f4c: ushr.8h v6, v5, #0xf
	WORD	$0x6f110487	// 0f50: ushr.8h v7, v4, #0xf
	WORD	$0xad4947f0	// 0f54: ldp q16, q17, [sp, #0x120]
	WORD	$0x6f110632	// 0f58: ushr.8h v18, v17, #0xf
	WORD	$0x6f110613	// 0f5c: ushr.8h v19, v16, #0xf
	WORD	$0xad4857f4	// 0f60: ldp q20, q21, [sp, #0x100]
	WORD	$0x6f1106b6	// 0f64: ushr.8h v22, v21, #0xf
	WORD	$0x6f110697	// 0f68: ushr.8h v23, v20, #0xf
	WORD	$0x6e205a94	// 0f6c: mvn.16b v20, v20
	WORD	$0x6e7486f4	// 0f70: sub.8h v20, v23, v20
	WORD	$0x6e205ab5	// 0f74: mvn.16b v21, v21
	WORD	$0x6e7586d5	// 0f78: sub.8h v21, v22, v21
	WORD	$0x6e205a10	// 0f7c: mvn.16b v16, v16
	WORD	$0x6e708670	// 0f80: sub.8h v16, v19, v16
	WORD	$0x6e205a31	// 0f84: mvn.16b v17, v17
	WORD	$0x6e718651	// 0f88: sub.8h v17, v18, v17
	WORD	$0x6e205884	// 0f8c: mvn.16b v4, v4
	WORD	$0x6e6484e4	// 0f90: sub.8h v4, v7, v4
	WORD	$0x6e2058a5	// 0f94: mvn.16b v5, v5
	WORD	$0x6e6584c5	// 0f98: sub.8h v5, v6, v5
	WORD	$0x6e205800	// 0f9c: mvn.16b v0, v0
	WORD	$0x6e608460	// 0fa0: sub.8h v0, v3, v0
	WORD	$0x6e205821	// 0fa4: mvn.16b v1, v1
	WORD	$0x6e618441	// 0fa8: sub.8h v1, v2, v1
	WORD	$0x4f1e0682	// 0fac: sshr.8h v2, v20, #0x2
	WORD	$0x4f1e06a3	// 0fb0: sshr.8h v3, v21, #0x2
	WORD	$0x4f1e0606	// 0fb4: sshr.8h v6, v16, #0x2
	WORD	$0x4f1e0627	// 0fb8: sshr.8h v7, v17, #0x2
	WORD	$0x4f1e0484	// 0fbc: sshr.8h v4, v4, #0x2
	WORD	$0x4f1e04a5	// 0fc0: sshr.8h v5, v5, #0x2
	WORD	$0x4f1e0400	// 0fc4: sshr.8h v0, v0, #0x2
	WORD	$0x4f1e0421	// 0fc8: sshr.8h v1, v1, #0x2
	WORD	$0xad080fe2	// 0fcc: stp q2, q3, [sp, #0x100]
	WORD	$0xad091fe6	// 0fd0: stp q6, q7, [sp, #0x120]
	WORD	$0xad0a17e4	// 0fd4: stp q4, q5, [sp, #0x140]
	WORD	$0xad0b07e0	// 0fd8: stp q0, q1, [sp, #0x160]
	WORD	$0xad4c07e0	// 0fdc: ldp q0, q1, [sp, #0x180]
	WORD	$0xad4f0fe2	// 0fe0: ldp q2, q3, [sp, #0x1e0]
	WORD	$0x6f110464	// 0fe4: ushr.8h v4, v3, #0xf
	WORD	$0xad4e1be5	// 0fe8: ldp q5, q6, [sp, #0x1c0]
	WORD	$0x6f110447	// 0fec: ushr.8h v7, v2, #0xf
	WORD	$0x6f1104d0	// 0ff0: ushr.8h v16, v6, #0xf
	WORD	$0x6f1104b1	// 0ff4: ushr.8h v17, v5, #0xf
	WORD	$0xad4d4ff2	// 0ff8: ldp q18, q19, [sp, #0x1a0]
	WORD	$0x6f110674	// 0ffc: ushr.8h v20, v19, #0xf
	WORD	$0x6f110655	// 1000: ushr.8h v21, v18, #0xf
	WORD	$0x6f110436	// 1004: ushr.8h v22, v1, #0xf
	WORD	$0x6f110417	// 1008: ushr.8h v23, v0, #0xf
	WORD	$0x6e205800	// 100c: mvn.16b v0, v0
	WORD	$0x6e6086e0	// 1010: sub.8h v0, v23, v0
	WORD	$0x6e205821	// 1014: mvn.16b v1, v1
	WORD	$0x6e6186c1	// 1018: sub.8h v1, v22, v1
	WORD	$0x6e205a52	// 101c: mvn.16b v18, v18
	WORD	$0x6e7286b2	// 1020: sub.8h v18, v21, v18
	WORD	$0x6e205a73	// 1024: mvn.16b v19, v19
	WORD	$0x6e738693	// 1028: sub.8h v19, v20, v19
	WORD	$0x6e2058a5	// 102c: mvn.16b v5, v5
	WORD	$0x6e658625	// 1030: sub.8h v5, v17, v5
	WORD	$0x6e2058c6	// 1034: mvn.16b v6, v6
	WORD	$0x6e668606	// 1038: sub.8h v6, v16, v6
	WORD	$0x6e205842	// 103c: mvn.16b v2, v2
	WORD	$0x6e6284e2	// 1040: sub.8h v2, v7, v2
	WORD	$0x6e205863	// 1044: mvn.16b v3, v3
	WORD	$0x6e638483	// 1048: sub.8h v3, v4, v3
	WORD	$0x4f1e0400	// 104c: sshr.8h v0, v0, #0x2
	WORD	$0x4f1e0421	// 1050: sshr.8h v1, v1, #0x2
	WORD	$0x4f1e0644	// 1054: sshr.8h v4, v18, #0x2
	WORD	$0x4f1e0667	// 1058: sshr.8h v7, v19, #0x2
	WORD	$0x4f1e04a5	// 105c: sshr.8h v5, v5, #0x2
	WORD	$0x4f1e04c6	// 1060: sshr.8h v6, v6, #0x2
	WORD	$0x4f1e0442	// 1064: sshr.8h v2, v2, #0x2
	WORD	$0x4f1e0463	// 1068: sshr.8h v3, v3, #0x2
	WORD	$0xad0c07e0	// 106c: stp q0, q1, [sp, #0x180]
	WORD	$0xad0d1fe4	// 1070: stp q4, q7, [sp, #0x1a0]
	WORD	$0xad0e1be5	// 1074: stp q5, q6, [sp, #0x1c0]
	WORD	$0xad0f0fe2	// 1078: stp q2, q3, [sp, #0x1e0]
	WORD	$0xad4007e0	// 107c: ldp q0, q1, [sp]
	WORD	$0xad410fe2	// 1080: ldp q2, q3, [sp, #0x20]
	WORD	$0xad4217e4	// 1084: ldp q4, q5, [sp, #0x40]
	WORD	$0xad431fe6	// 1088: ldp q6, q7, [sp, #0x60]
	WORD	$0x6f1104f0	// 108c: ushr.8h v16, v7, #0xf
	WORD	$0x6f1104d1	// 1090: ushr.8h v17, v6, #0xf
	WORD	$0x6f1104b2	// 1094: ushr.8h v18, v5, #0xf
	WORD	$0x6f110493	// 1098: ushr.8h v19, v4, #0xf
	WORD	$0x6f110474	// 109c: ushr.8h v20, v3, #0xf
	WORD	$0x6f110455	// 10a0: ushr.8h v21, v2, #0xf
	WORD	$0x6f110436	// 10a4: ushr.8h v22, v1, #0xf
	WORD	$0x6f110417	// 10a8: ushr.8h v23, v0, #0xf
	WORD	$0x6e205800	// 10ac: mvn.16b v0, v0
	WORD	$0x6e6086e0	// 10b0: sub.8h v0, v23, v0
	WORD	$0x6e205821	// 10b4: mvn.16b v1, v1
	WORD	$0x6e6186c1	// 10b8: sub.8h v1, v22, v1
	WORD	$0x6e205842	// 10bc: mvn.16b v2, v2
	WORD	$0x6e6286a2	// 10c0: sub.8h v2, v21, v2
	WORD	$0x6e205863	// 10c4: mvn.16b v3, v3
	WORD	$0x6e638683	// 10c8: sub.8h v3, v20, v3
	WORD	$0x6e205884	// 10cc: mvn.16b v4, v4
	WORD	$0x6e648664	// 10d0: sub.8h v4, v19, v4
	WORD	$0x6e2058a5	// 10d4: mvn.16b v5, v5
	WORD	$0x6e658645	// 10d8: sub.8h v5, v18, v5
	WORD	$0x6e2058c6	// 10dc: mvn.16b v6, v6
	WORD	$0x6e668626	// 10e0: sub.8h v6, v17, v6
	WORD	$0x6e2058e7	// 10e4: mvn.16b v7, v7
	WORD	$0x6e678607	// 10e8: sub.8h v7, v16, v7
	WORD	$0x4f1e0400	// 10ec: sshr.8h v0, v0, #0x2
	WORD	$0x4f1e0421	// 10f0: sshr.8h v1, v1, #0x2
	WORD	$0x4f1e0442	// 10f4: sshr.8h v2, v2, #0x2
	WORD	$0x4f1e0463	// 10f8: sshr.8h v3, v3, #0x2
	WORD	$0x4f1e0484	// 10fc: sshr.8h v4, v4, #0x2
	WORD	$0x4f1e04a5	// 1100: sshr.8h v5, v5, #0x2
	WORD	$0xad0007e0	// 1104: stp q0, q1, [sp]
	WORD	$0x4f1e04c0	// 1108: sshr.8h v0, v6, #0x2
	WORD	$0x4f1e04e1	// 110c: sshr.8h v1, v7, #0x2
	WORD	$0xad010fe2	// 1110: stp q2, q3, [sp, #0x20]
	WORD	$0xad0217e4	// 1114: stp q4, q5, [sp, #0x40]
	WORD	$0xad0307e0	// 1118: stp q0, q1, [sp, #0x60]
	WORD	$0xad4407e0	// 111c: ldp q0, q1, [sp, #0x80]
	WORD	$0xad450fe2	// 1120: ldp q2, q3, [sp, #0xa0]
	WORD	$0xad4617e4	// 1124: ldp q4, q5, [sp, #0xc0]
	WORD	$0xad471fe6	// 1128: ldp q6, q7, [sp, #0xe0]
	WORD	$0x6f1104f0	// 112c: ushr.8h v16, v7, #0xf
	WORD	$0x6f1104d1	// 1130: ushr.8h v17, v6, #0xf
	WORD	$0x6f1104b2	// 1134: ushr.8h v18, v5, #0xf
	WORD	$0x6f110493	// 1138: ushr.8h v19, v4, #0xf
	WORD	$0x6f110474	// 113c: ushr.8h v20, v3, #0xf
	WORD	$0x6f110455	// 1140: ushr.8h v21, v2, #0xf
	WORD	$0x6f110436	// 1144: ushr.8h v22, v1, #0xf
	WORD	$0x6f110417	// 1148: ushr.8h v23, v0, #0xf
	WORD	$0x6e205800	// 114c: mvn.16b v0, v0
	WORD	$0x6e6086e0	// 1150: sub.8h v0, v23, v0
	WORD	$0x6e205821	// 1154: mvn.16b v1, v1
	WORD	$0x6e6186c1	// 1158: sub.8h v1, v22, v1
	WORD	$0x6e205842	// 115c: mvn.16b v2, v2
	WORD	$0x6e6286a2	// 1160: sub.8h v2, v21, v2
	WORD	$0x6e205863	// 1164: mvn.16b v3, v3
	WORD	$0x6e638683	// 1168: sub.8h v3, v20, v3
	WORD	$0x6e205884	// 116c: mvn.16b v4, v4
	WORD	$0x6e648664	// 1170: sub.8h v4, v19, v4
	WORD	$0x6e2058a5	// 1174: mvn.16b v5, v5
	WORD	$0x6e658645	// 1178: sub.8h v5, v18, v5
	WORD	$0x4f1e0400	// 117c: sshr.8h v0, v0, #0x2
	WORD	$0x4f1e0421	// 1180: sshr.8h v1, v1, #0x2
	WORD	$0xad0407e0	// 1184: stp q0, q1, [sp, #0x80]
	WORD	$0x6e2058c0	// 1188: mvn.16b v0, v6
	WORD	$0x4f1e0441	// 118c: sshr.8h v1, v2, #0x2
	WORD	$0x4f1e0462	// 1190: sshr.8h v2, v3, #0x2
	WORD	$0xad050be1	// 1194: stp q1, q2, [sp, #0xa0]
	WORD	$0x6e608620	// 1198: sub.8h v0, v17, v0
	WORD	$0x4f1e0481	// 119c: sshr.8h v1, v4, #0x2
	WORD	$0x4f1e04a2	// 11a0: sshr.8h v2, v5, #0x2
	WORD	$0xad060be1	// 11a4: stp q1, q2, [sp, #0xc0]
	WORD	$0x6e2058e1	// 11a8: mvn.16b v1, v7
	WORD	$0x6e618601	// 11ac: sub.8h v1, v16, v1
	WORD	$0x4f1e0400	// 11b0: sshr.8h v0, v0, #0x2
	WORD	$0x4f1e0421	// 11b4: sshr.8h v1, v1, #0x2
	WORD	$0xad0707e0	// 11b8: stp q0, q1, [sp, #0xe0]
	WORD	$0x910403e0	// 11bc: add x0, sp, #0x100
	WORD	$0x910003e1	// 11c0: mov x1, sp
	WORD	$0x9400022d	// 11c4: bl 0x11c4 <_vp9_fht16x16_neon+0x444>
	WORD	$0x14000165	// 11c8: b 0x175c <_vp9_fht16x16_neon+0x9dc>
	WORD	$0x3dc00000	// 11cc: ldr q0, [x0]
	WORD	$0x937f7c48	// 11d0: sbfiz x8, x2, #1, #32
	WORD	$0x3ce86801	// 11d4: ldr q1, [x0, x8]
	WORD	$0x4f125400	// 11d8: shl.8h v0, v0, #0x2
	WORD	$0x4f125421	// 11dc: shl.8h v1, v1, #0x2
	WORD	$0xad0807e0	// 11e0: stp q0, q1, [sp, #0x100]
	WORD	$0x937e7c49	// 11e4: sbfiz x9, x2, #2, #32
	WORD	$0x3ce96800	// 11e8: ldr q0, [x0, x9]
	WORD	$0x93407c4c	// 11ec: sxtw x12, w2
	WORD	$0x8b0c010d	// 11f0: add x13, x8, x12
	WORD	$0xd37ff9aa	// 11f4: lsl x10, x13, #1
	WORD	$0x3cea6801	// 11f8: ldr q1, [x0, x10]
	WORD	$0x4f125400	// 11fc: shl.8h v0, v0, #0x2
	WORD	$0x4f125421	// 1200: shl.8h v1, v1, #0x2
	WORD	$0xad0907e0	// 1204: stp q0, q1, [sp, #0x120]
	WORD	$0x937d7c4b	// 1208: sbfiz x11, x2, #3, #32
	WORD	$0x3ceb6800	// 120c: ldr q0, [x0, x11]
	WORD	$0x8b0c012c	// 1210: add x12, x9, x12
	WORD	$0xd37ff98c	// 1214: lsl x12, x12, #1
	WORD	$0x3cec6801	// 1218: ldr q1, [x0, x12]
	WORD	$0x4f125400	// 121c: shl.8h v0, v0, #0x2
	WORD	$0x4f125421	// 1220: shl.8h v1, v1, #0x2
	WORD	$0xad0a07e0	// 1224: stp q0, q1, [sp, #0x140]
	WORD	$0xd37ef5ad	// 1228: lsl x13, x13, #2
	WORD	$0x3ced6800	// 122c: ldr q0, [x0, x13]
	WORD	$0x937c7c4f	// 1230: sbfiz x15, x2, #4, #32
	WORD	$0xcb0801ee	// 1234: sub x14, x15, x8
	WORD	$0x3cee6801	// 1238: ldr q1, [x0, x14]
	WORD	$0x4f125400	// 123c: shl.8h v0, v0, #0x2
	WORD	$0x4f125421	// 1240: shl.8h v1, v1, #0x2
	WORD	$0xad0b07e0	// 1244: stp q0, q1, [sp, #0x160]
	WORD	$0x8b0f0010	// 1248: add x16, x0, x15
	WORD	$0x3dc00200	// 124c: ldr q0, [x16]
	WORD	$0x4f125400	// 1250: shl.8h v0, v0, #0x2
	WORD	$0x3ce86a01	// 1254: ldr q1, [x16, x8]
	WORD	$0x4f125421	// 1258: shl.8h v1, v1, #0x2
	WORD	$0xad0c07e0	// 125c: stp q0, q1, [sp, #0x180]
	WORD	$0x3ce96a00	// 1260: ldr q0, [x16, x9]
	WORD	$0x3cea6a01	// 1264: ldr q1, [x16, x10]
	WORD	$0x4f125400	// 1268: shl.8h v0, v0, #0x2
	WORD	$0x4f125421	// 126c: shl.8h v1, v1, #0x2
	WORD	$0xad0d07e0	// 1270: stp q0, q1, [sp, #0x1a0]
	WORD	$0x3ceb6a00	// 1274: ldr q0, [x16, x11]
	WORD	$0x4f125400	// 1278: shl.8h v0, v0, #0x2
	WORD	$0x3cec6a01	// 127c: ldr q1, [x16, x12]
	WORD	$0x4f125421	// 1280: shl.8h v1, v1, #0x2
	WORD	$0xad0e07e0	// 1284: stp q0, q1, [sp, #0x1c0]
	WORD	$0x3ced6a00	// 1288: ldr q0, [x16, x13]
	WORD	$0x4f125400	// 128c: shl.8h v0, v0, #0x2
	WORD	$0x3cee6a01	// 1290: ldr q1, [x16, x14]
	WORD	$0x4f125421	// 1294: shl.8h v1, v1, #0x2
	WORD	$0xad0f07e0	// 1298: stp q0, q1, [sp, #0x1e0]
	WORD	$0x3cc10c00	// 129c: ldr q0, [x0, #0x10]!
	WORD	$0x3ce86801	// 12a0: ldr q1, [x0, x8]
	WORD	$0x4f125400	// 12a4: shl.8h v0, v0, #0x2
	WORD	$0x4f125421	// 12a8: shl.8h v1, v1, #0x2
	WORD	$0xad0007e0	// 12ac: stp q0, q1, [sp]
	WORD	$0x3ce96800	// 12b0: ldr q0, [x0, x9]
	WORD	$0x4f125400	// 12b4: shl.8h v0, v0, #0x2
	WORD	$0x3cea6801	// 12b8: ldr q1, [x0, x10]
	WORD	$0x4f125421	// 12bc: shl.8h v1, v1, #0x2
	WORD	$0xad0107e0	// 12c0: stp q0, q1, [sp, #0x20]
	WORD	$0x3ceb6800	// 12c4: ldr q0, [x0, x11]
	WORD	$0x4f125400	// 12c8: shl.8h v0, v0, #0x2
	WORD	$0x3cec6801	// 12cc: ldr q1, [x0, x12]
	WORD	$0x4f125421	// 12d0: shl.8h v1, v1, #0x2
	WORD	$0xad0207e0	// 12d4: stp q0, q1, [sp, #0x40]
	WORD	$0x3ced6800	// 12d8: ldr q0, [x0, x13]
	WORD	$0x3cee6801	// 12dc: ldr q1, [x0, x14]
	WORD	$0x4f125400	// 12e0: shl.8h v0, v0, #0x2
	WORD	$0x4f125421	// 12e4: shl.8h v1, v1, #0x2
	WORD	$0xad0307e0	// 12e8: stp q0, q1, [sp, #0x60]
	WORD	$0x8b0f000f	// 12ec: add x15, x0, x15
	WORD	$0x3dc001e0	// 12f0: ldr q0, [x15]
	WORD	$0x4f125400	// 12f4: shl.8h v0, v0, #0x2
	WORD	$0x3ce869e1	// 12f8: ldr q1, [x15, x8]
	WORD	$0x4f125421	// 12fc: shl.8h v1, v1, #0x2
	WORD	$0xad0407e0	// 1300: stp q0, q1, [sp, #0x80]
	WORD	$0x3ce969e0	// 1304: ldr q0, [x15, x9]
	WORD	$0x4f125400	// 1308: shl.8h v0, v0, #0x2
	WORD	$0x3cea69e1	// 130c: ldr q1, [x15, x10]
	WORD	$0x4f125421	// 1310: shl.8h v1, v1, #0x2
	WORD	$0xad0507e0	// 1314: stp q0, q1, [sp, #0xa0]
	WORD	$0x3ceb69e0	// 1318: ldr q0, [x15, x11]
	WORD	$0x3cec69e1	// 131c: ldr q1, [x15, x12]
	WORD	$0x4f125400	// 1320: shl.8h v0, v0, #0x2
	WORD	$0x4f125421	// 1324: shl.8h v1, v1, #0x2
	WORD	$0xad0607e0	// 1328: stp q0, q1, [sp, #0xc0]
	WORD	$0x3ced69e0	// 132c: ldr q0, [x15, x13]
	WORD	$0x4f125400	// 1330: shl.8h v0, v0, #0x2
	WORD	$0x3cee69e1	// 1334: ldr q1, [x15, x14]
	WORD	$0x4f125421	// 1338: shl.8h v1, v1, #0x2
	WORD	$0xad0707e0	// 133c: stp q0, q1, [sp, #0xe0]
	WORD	$0x910403e0	// 1340: add x0, sp, #0x100
	WORD	$0x910003e1	// 1344: mov x1, sp
	WORD	$0x940001cc	// 1348: bl 0x1348 <_vp9_fht16x16_neon+0x5c8>
	WORD	$0x14000061	// 134c: b 0x14d0 <_vp9_fht16x16_neon+0x750>
	WORD	$0x3dc00000	// 1350: ldr q0, [x0]
	WORD	$0x937f7c48	// 1354: sbfiz x8, x2, #1, #32
	WORD	$0x3ce86801	// 1358: ldr q1, [x0, x8]
	WORD	$0x4f125400	// 135c: shl.8h v0, v0, #0x2
	WORD	$0x4f125421	// 1360: shl.8h v1, v1, #0x2
	WORD	$0xad0807e0	// 1364: stp q0, q1, [sp, #0x100]
	WORD	$0x937e7c49	// 1368: sbfiz x9, x2, #2, #32
	WORD	$0x3ce96800	// 136c: ldr q0, [x0, x9]
	WORD	$0x93407c4c	// 1370: sxtw x12, w2
	WORD	$0x8b0c010d	// 1374: add x13, x8, x12
	WORD	$0xd37ff9aa	// 1378: lsl x10, x13, #1
	WORD	$0x3cea6801	// 137c: ldr q1, [x0, x10]
	WORD	$0x4f125400	// 1380: shl.8h v0, v0, #0x2
	WORD	$0x4f125421	// 1384: shl.8h v1, v1, #0x2
	WORD	$0xad0907e0	// 1388: stp q0, q1, [sp, #0x120]
	WORD	$0x937d7c4b	// 138c: sbfiz x11, x2, #3, #32
	WORD	$0x3ceb6800	// 1390: ldr q0, [x0, x11]
	WORD	$0x8b0c012c	// 1394: add x12, x9, x12
	WORD	$0xd37ff98c	// 1398: lsl x12, x12, #1
	WORD	$0x3cec6801	// 139c: ldr q1, [x0, x12]
	WORD	$0x4f125400	// 13a0: shl.8h v0, v0, #0x2
	WORD	$0x4f125421	// 13a4: shl.8h v1, v1, #0x2
	WORD	$0xad0a07e0	// 13a8: stp q0, q1, [sp, #0x140]
	WORD	$0xd37ef5ad	// 13ac: lsl x13, x13, #2
	WORD	$0x3ced6800	// 13b0: ldr q0, [x0, x13]
	WORD	$0x937c7c4f	// 13b4: sbfiz x15, x2, #4, #32
	WORD	$0xcb0801ee	// 13b8: sub x14, x15, x8
	WORD	$0x3cee6801	// 13bc: ldr q1, [x0, x14]
	WORD	$0x4f125400	// 13c0: shl.8h v0, v0, #0x2
	WORD	$0x4f125421	// 13c4: shl.8h v1, v1, #0x2
	WORD	$0xad0b07e0	// 13c8: stp q0, q1, [sp, #0x160]
	WORD	$0x8b0f0010	// 13cc: add x16, x0, x15
	WORD	$0x3dc00200	// 13d0: ldr q0, [x16]
	WORD	$0x4f125400	// 13d4: shl.8h v0, v0, #0x2
	WORD	$0x3ce86a01	// 13d8: ldr q1, [x16, x8]
	WORD	$0x4f125421	// 13dc: shl.8h v1, v1, #0x2
	WORD	$0xad0c07e0	// 13e0: stp q0, q1, [sp, #0x180]
	WORD	$0x3ce96a00	// 13e4: ldr q0, [x16, x9]
	WORD	$0x3cea6a01	// 13e8: ldr q1, [x16, x10]
	WORD	$0x4f125400	// 13ec: shl.8h v0, v0, #0x2
	WORD	$0x4f125421	// 13f0: shl.8h v1, v1, #0x2
	WORD	$0xad0d07e0	// 13f4: stp q0, q1, [sp, #0x1a0]
	WORD	$0x3ceb6a00	// 13f8: ldr q0, [x16, x11]
	WORD	$0x4f125400	// 13fc: shl.8h v0, v0, #0x2
	WORD	$0x3cec6a01	// 1400: ldr q1, [x16, x12]
	WORD	$0x4f125421	// 1404: shl.8h v1, v1, #0x2
	WORD	$0xad0e07e0	// 1408: stp q0, q1, [sp, #0x1c0]
	WORD	$0x3ced6a00	// 140c: ldr q0, [x16, x13]
	WORD	$0x4f125400	// 1410: shl.8h v0, v0, #0x2
	WORD	$0x3cee6a01	// 1414: ldr q1, [x16, x14]
	WORD	$0x4f125421	// 1418: shl.8h v1, v1, #0x2
	WORD	$0xad0f07e0	// 141c: stp q0, q1, [sp, #0x1e0]
	WORD	$0x3cc10c00	// 1420: ldr q0, [x0, #0x10]!
	WORD	$0x3ce86801	// 1424: ldr q1, [x0, x8]
	WORD	$0x4f125400	// 1428: shl.8h v0, v0, #0x2
	WORD	$0x4f125421	// 142c: shl.8h v1, v1, #0x2
	WORD	$0xad0007e0	// 1430: stp q0, q1, [sp]
	WORD	$0x3ce96800	// 1434: ldr q0, [x0, x9]
	WORD	$0x4f125400	// 1438: shl.8h v0, v0, #0x2
	WORD	$0x3cea6801	// 143c: ldr q1, [x0, x10]
	WORD	$0x4f125421	// 1440: shl.8h v1, v1, #0x2
	WORD	$0xad0107e0	// 1444: stp q0, q1, [sp, #0x20]
	WORD	$0x3ceb6800	// 1448: ldr q0, [x0, x11]
	WORD	$0x4f125400	// 144c: shl.8h v0, v0, #0x2
	WORD	$0x3cec6801	// 1450: ldr q1, [x0, x12]
	WORD	$0x4f125421	// 1454: shl.8h v1, v1, #0x2
	WORD	$0xad0207e0	// 1458: stp q0, q1, [sp, #0x40]
	WORD	$0x3ced6800	// 145c: ldr q0, [x0, x13]
	WORD	$0x3cee6801	// 1460: ldr q1, [x0, x14]
	WORD	$0x4f125400	// 1464: shl.8h v0, v0, #0x2
	WORD	$0x4f125421	// 1468: shl.8h v1, v1, #0x2
	WORD	$0xad0307e0	// 146c: stp q0, q1, [sp, #0x60]
	WORD	$0x8b0f000f	// 1470: add x15, x0, x15
	WORD	$0x3dc001e0	// 1474: ldr q0, [x15]
	WORD	$0x4f125400	// 1478: shl.8h v0, v0, #0x2
	WORD	$0x3ce869e1	// 147c: ldr q1, [x15, x8]
	WORD	$0x4f125421	// 1480: shl.8h v1, v1, #0x2
	WORD	$0xad0407e0	// 1484: stp q0, q1, [sp, #0x80]
	WORD	$0x3ce969e0	// 1488: ldr q0, [x15, x9]
	WORD	$0x4f125400	// 148c: shl.8h v0, v0, #0x2
	WORD	$0x3cea69e1	// 1490: ldr q1, [x15, x10]
	WORD	$0x4f125421	// 1494: shl.8h v1, v1, #0x2
	WORD	$0xad0507e0	// 1498: stp q0, q1, [sp, #0xa0]
	WORD	$0x3ceb69e0	// 149c: ldr q0, [x15, x11]
	WORD	$0x3cec69e1	// 14a0: ldr q1, [x15, x12]
	WORD	$0x4f125400	// 14a4: shl.8h v0, v0, #0x2
	WORD	$0x4f125421	// 14a8: shl.8h v1, v1, #0x2
	WORD	$0xad0607e0	// 14ac: stp q0, q1, [sp, #0xc0]
	WORD	$0x3ced69e0	// 14b0: ldr q0, [x15, x13]
	WORD	$0x4f125400	// 14b4: shl.8h v0, v0, #0x2
	WORD	$0x3cee69e1	// 14b8: ldr q1, [x15, x14]
	WORD	$0x4f125421	// 14bc: shl.8h v1, v1, #0x2
	WORD	$0xad0707e0	// 14c0: stp q0, q1, [sp, #0xe0]
	WORD	$0x910403e0	// 14c4: add x0, sp, #0x100
	WORD	$0x910003e1	// 14c8: mov x1, sp
	WORD	$0x940000c8	// 14cc: bl 0x14cc <_vp9_fht16x16_neon+0x74c>
	WORD	$0xad4b07e0	// 14d0: ldp q0, q1, [sp, #0x160]
	WORD	$0x6f110422	// 14d4: ushr.8h v2, v1, #0xf
	WORD	$0x6f110403	// 14d8: ushr.8h v3, v0, #0xf
	WORD	$0xad4a17e4	// 14dc: ldp q4, q5, [sp, #0x140]
	WORD	$0x6f1104a6	// 14e0: ushr.8h v6, v5, #0xf
	WORD	$0x6f110487	// 14e4: ushr.8h v7, v4, #0xf
	WORD	$0xad4947f0	// 14e8: ldp q16, q17, [sp, #0x120]
	WORD	$0x6f110632	// 14ec: ushr.8h v18, v17, #0xf
	WORD	$0x6f110613	// 14f0: ushr.8h v19, v16, #0xf
	WORD	$0xad4857f4	// 14f4: ldp q20, q21, [sp, #0x100]
	WORD	$0x6f1106b6	// 14f8: ushr.8h v22, v21, #0xf
	WORD	$0x6f110697	// 14fc: ushr.8h v23, v20, #0xf
	WORD	$0x6e205a94	// 1500: mvn.16b v20, v20
	WORD	$0x6e7486f4	// 1504: sub.8h v20, v23, v20
	WORD	$0x6e205ab5	// 1508: mvn.16b v21, v21
	WORD	$0x6e7586d5	// 150c: sub.8h v21, v22, v21
	WORD	$0x6e205a10	// 1510: mvn.16b v16, v16
	WORD	$0x6e708670	// 1514: sub.8h v16, v19, v16
	WORD	$0x6e205a31	// 1518: mvn.16b v17, v17
	WORD	$0x6e718651	// 151c: sub.8h v17, v18, v17
	WORD	$0x6e205884	// 1520: mvn.16b v4, v4
	WORD	$0x6e6484e4	// 1524: sub.8h v4, v7, v4
	WORD	$0x6e2058a5	// 1528: mvn.16b v5, v5
	WORD	$0x6e6584c5	// 152c: sub.8h v5, v6, v5
	WORD	$0x6e205800	// 1530: mvn.16b v0, v0
	WORD	$0x6e608460	// 1534: sub.8h v0, v3, v0
	WORD	$0x6e205821	// 1538: mvn.16b v1, v1
	WORD	$0x6e618441	// 153c: sub.8h v1, v2, v1
	WORD	$0x4f1e0682	// 1540: sshr.8h v2, v20, #0x2
	WORD	$0x4f1e06a3	// 1544: sshr.8h v3, v21, #0x2
	WORD	$0x4f1e0606	// 1548: sshr.8h v6, v16, #0x2
	WORD	$0x4f1e0627	// 154c: sshr.8h v7, v17, #0x2
	WORD	$0x4f1e0484	// 1550: sshr.8h v4, v4, #0x2
	WORD	$0x4f1e04a5	// 1554: sshr.8h v5, v5, #0x2
	WORD	$0x4f1e0400	// 1558: sshr.8h v0, v0, #0x2
	WORD	$0x4f1e0421	// 155c: sshr.8h v1, v1, #0x2
	WORD	$0xad080fe2	// 1560: stp q2, q3, [sp, #0x100]
	WORD	$0xad091fe6	// 1564: stp q6, q7, [sp, #0x120]
	WORD	$0xad0a17e4	// 1568: stp q4, q5, [sp, #0x140]
	WORD	$0xad0b07e0	// 156c: stp q0, q1, [sp, #0x160]
	WORD	$0xad4c07e0	// 1570: ldp q0, q1, [sp, #0x180]
	WORD	$0xad4f0fe2	// 1574: ldp q2, q3, [sp, #0x1e0]
	WORD	$0x6f110464	// 1578: ushr.8h v4, v3, #0xf
	WORD	$0xad4e1be5	// 157c: ldp q5, q6, [sp, #0x1c0]
	WORD	$0x6f110447	// 1580: ushr.8h v7, v2, #0xf
	WORD	$0x6f1104d0	// 1584: ushr.8h v16, v6, #0xf
	WORD	$0x6f1104b1	// 1588: ushr.8h v17, v5, #0xf
	WORD	$0xad4d4ff2	// 158c: ldp q18, q19, [sp, #0x1a0]
	WORD	$0x6f110674	// 1590: ushr.8h v20, v19, #0xf
	WORD	$0x6f110655	// 1594: ushr.8h v21, v18, #0xf
	WORD	$0x6f110436	// 1598: ushr.8h v22, v1, #0xf
	WORD	$0x6f110417	// 159c: ushr.8h v23, v0, #0xf
	WORD	$0x6e205800	// 15a0: mvn.16b v0, v0
	WORD	$0x6e6086e0	// 15a4: sub.8h v0, v23, v0
	WORD	$0x6e205821	// 15a8: mvn.16b v1, v1
	WORD	$0x6e6186c1	// 15ac: sub.8h v1, v22, v1
	WORD	$0x6e205a52	// 15b0: mvn.16b v18, v18
	WORD	$0x6e7286b2	// 15b4: sub.8h v18, v21, v18
	WORD	$0x6e205a73	// 15b8: mvn.16b v19, v19
	WORD	$0x6e738693	// 15bc: sub.8h v19, v20, v19
	WORD	$0x6e2058a5	// 15c0: mvn.16b v5, v5
	WORD	$0x6e658625	// 15c4: sub.8h v5, v17, v5
	WORD	$0x6e2058c6	// 15c8: mvn.16b v6, v6
	WORD	$0x6e668606	// 15cc: sub.8h v6, v16, v6
	WORD	$0x6e205842	// 15d0: mvn.16b v2, v2
	WORD	$0x6e6284e2	// 15d4: sub.8h v2, v7, v2
	WORD	$0x6e205863	// 15d8: mvn.16b v3, v3
	WORD	$0x6e638483	// 15dc: sub.8h v3, v4, v3
	WORD	$0x4f1e0400	// 15e0: sshr.8h v0, v0, #0x2
	WORD	$0x4f1e0421	// 15e4: sshr.8h v1, v1, #0x2
	WORD	$0x4f1e0644	// 15e8: sshr.8h v4, v18, #0x2
	WORD	$0x4f1e0667	// 15ec: sshr.8h v7, v19, #0x2
	WORD	$0x4f1e04a5	// 15f0: sshr.8h v5, v5, #0x2
	WORD	$0x4f1e04c6	// 15f4: sshr.8h v6, v6, #0x2
	WORD	$0x4f1e0442	// 15f8: sshr.8h v2, v2, #0x2
	WORD	$0x4f1e0463	// 15fc: sshr.8h v3, v3, #0x2
	WORD	$0xad0c07e0	// 1600: stp q0, q1, [sp, #0x180]
	WORD	$0xad0d1fe4	// 1604: stp q4, q7, [sp, #0x1a0]
	WORD	$0xad0e1be5	// 1608: stp q5, q6, [sp, #0x1c0]
	WORD	$0xad0f0fe2	// 160c: stp q2, q3, [sp, #0x1e0]
	WORD	$0xad4007e0	// 1610: ldp q0, q1, [sp]
	WORD	$0xad410fe2	// 1614: ldp q2, q3, [sp, #0x20]
	WORD	$0xad4217e4	// 1618: ldp q4, q5, [sp, #0x40]
	WORD	$0xad431fe6	// 161c: ldp q6, q7, [sp, #0x60]
	WORD	$0x6f1104f0	// 1620: ushr.8h v16, v7, #0xf
	WORD	$0x6f1104d1	// 1624: ushr.8h v17, v6, #0xf
	WORD	$0x6f1104b2	// 1628: ushr.8h v18, v5, #0xf
	WORD	$0x6f110493	// 162c: ushr.8h v19, v4, #0xf
	WORD	$0x6f110474	// 1630: ushr.8h v20, v3, #0xf
	WORD	$0x6f110455	// 1634: ushr.8h v21, v2, #0xf
	WORD	$0x6f110436	// 1638: ushr.8h v22, v1, #0xf
	WORD	$0x6f110417	// 163c: ushr.8h v23, v0, #0xf
	WORD	$0x6e205800	// 1640: mvn.16b v0, v0
	WORD	$0x6e6086e0	// 1644: sub.8h v0, v23, v0
	WORD	$0x6e205821	// 1648: mvn.16b v1, v1
	WORD	$0x6e6186c1	// 164c: sub.8h v1, v22, v1
	WORD	$0x6e205842	// 1650: mvn.16b v2, v2
	WORD	$0x6e6286a2	// 1654: sub.8h v2, v21, v2
	WORD	$0x6e205863	// 1658: mvn.16b v3, v3
	WORD	$0x6e638683	// 165c: sub.8h v3, v20, v3
	WORD	$0x6e205884	// 1660: mvn.16b v4, v4
	WORD	$0x6e648664	// 1664: sub.8h v4, v19, v4
	WORD	$0x6e2058a5	// 1668: mvn.16b v5, v5
	WORD	$0x6e658645	// 166c: sub.8h v5, v18, v5
	WORD	$0x6e2058c6	// 1670: mvn.16b v6, v6
	WORD	$0x6e668626	// 1674: sub.8h v6, v17, v6
	WORD	$0x6e2058e7	// 1678: mvn.16b v7, v7
	WORD	$0x6e678607	// 167c: sub.8h v7, v16, v7
	WORD	$0x4f1e0400	// 1680: sshr.8h v0, v0, #0x2
	WORD	$0x4f1e0421	// 1684: sshr.8h v1, v1, #0x2
	WORD	$0x4f1e0442	// 1688: sshr.8h v2, v2, #0x2
	WORD	$0x4f1e0463	// 168c: sshr.8h v3, v3, #0x2
	WORD	$0x4f1e0484	// 1690: sshr.8h v4, v4, #0x2
	WORD	$0x4f1e04a5	// 1694: sshr.8h v5, v5, #0x2
	WORD	$0xad0007e0	// 1698: stp q0, q1, [sp]
	WORD	$0x4f1e04c0	// 169c: sshr.8h v0, v6, #0x2
	WORD	$0x4f1e04e1	// 16a0: sshr.8h v1, v7, #0x2
	WORD	$0xad010fe2	// 16a4: stp q2, q3, [sp, #0x20]
	WORD	$0xad0217e4	// 16a8: stp q4, q5, [sp, #0x40]
	WORD	$0xad0307e0	// 16ac: stp q0, q1, [sp, #0x60]
	WORD	$0xad4407e0	// 16b0: ldp q0, q1, [sp, #0x80]
	WORD	$0xad450fe2	// 16b4: ldp q2, q3, [sp, #0xa0]
	WORD	$0xad4617e4	// 16b8: ldp q4, q5, [sp, #0xc0]
	WORD	$0xad471fe6	// 16bc: ldp q6, q7, [sp, #0xe0]
	WORD	$0x6f1104f0	// 16c0: ushr.8h v16, v7, #0xf
	WORD	$0x6f1104d1	// 16c4: ushr.8h v17, v6, #0xf
	WORD	$0x6f1104b2	// 16c8: ushr.8h v18, v5, #0xf
	WORD	$0x6f110493	// 16cc: ushr.8h v19, v4, #0xf
	WORD	$0x6f110474	// 16d0: ushr.8h v20, v3, #0xf
	WORD	$0x6f110455	// 16d4: ushr.8h v21, v2, #0xf
	WORD	$0x6f110436	// 16d8: ushr.8h v22, v1, #0xf
	WORD	$0x6f110417	// 16dc: ushr.8h v23, v0, #0xf
	WORD	$0x6e205800	// 16e0: mvn.16b v0, v0
	WORD	$0x6e6086e0	// 16e4: sub.8h v0, v23, v0
	WORD	$0x6e205821	// 16e8: mvn.16b v1, v1
	WORD	$0x6e6186c1	// 16ec: sub.8h v1, v22, v1
	WORD	$0x6e205842	// 16f0: mvn.16b v2, v2
	WORD	$0x6e6286a2	// 16f4: sub.8h v2, v21, v2
	WORD	$0x6e205863	// 16f8: mvn.16b v3, v3
	WORD	$0x6e638683	// 16fc: sub.8h v3, v20, v3
	WORD	$0x6e205884	// 1700: mvn.16b v4, v4
	WORD	$0x6e648664	// 1704: sub.8h v4, v19, v4
	WORD	$0x6e2058a5	// 1708: mvn.16b v5, v5
	WORD	$0x6e658645	// 170c: sub.8h v5, v18, v5
	WORD	$0x4f1e0400	// 1710: sshr.8h v0, v0, #0x2
	WORD	$0x4f1e0421	// 1714: sshr.8h v1, v1, #0x2
	WORD	$0xad0407e0	// 1718: stp q0, q1, [sp, #0x80]
	WORD	$0x6e2058c0	// 171c: mvn.16b v0, v6
	WORD	$0x4f1e0441	// 1720: sshr.8h v1, v2, #0x2
	WORD	$0x4f1e0462	// 1724: sshr.8h v2, v3, #0x2
	WORD	$0xad050be1	// 1728: stp q1, q2, [sp, #0xa0]
	WORD	$0x6e608620	// 172c: sub.8h v0, v17, v0
	WORD	$0x4f1e0481	// 1730: sshr.8h v1, v4, #0x2
	WORD	$0x4f1e04a2	// 1734: sshr.8h v2, v5, #0x2
	WORD	$0xad060be1	// 1738: stp q1, q2, [sp, #0xc0]
	WORD	$0x6e2058e1	// 173c: mvn.16b v1, v7
	WORD	$0x6e618601	// 1740: sub.8h v1, v16, v1
	WORD	$0x4f1e0400	// 1744: sshr.8h v0, v0, #0x2
	WORD	$0x4f1e0421	// 1748: sshr.8h v1, v1, #0x2
	WORD	$0xad0707e0	// 174c: stp q0, q1, [sp, #0xe0]
	WORD	$0x910403e0	// 1750: add x0, sp, #0x100
	WORD	$0x910003e1	// 1754: mov x1, sp
	WORD	$0x94000025	// 1758: bl 0x1758 <_vp9_fht16x16_neon+0x9d8>
	WORD	$0xad4807e0	// 175c: ldp q0, q1, [sp, #0x100]
	WORD	$0xad400fe2	// 1760: ldp q2, q3, [sp]
	WORD	$0xad000a60	// 1764: stp q0, q2, [x19]
	WORD	$0xad490be0	// 1768: ldp q0, q2, [sp, #0x120]
	WORD	$0xad010e61	// 176c: stp q1, q3, [x19, #0x20]
	WORD	$0xad410fe1	// 1770: ldp q1, q3, [sp, #0x20]
	WORD	$0xad020660	// 1774: stp q0, q1, [x19, #0x40]
	WORD	$0xad4a07e0	// 1778: ldp q0, q1, [sp, #0x140]
	WORD	$0xad030e62	// 177c: stp q2, q3, [x19, #0x60]
	WORD	$0xad420fe2	// 1780: ldp q2, q3, [sp, #0x40]
	WORD	$0xad040a60	// 1784: stp q0, q2, [x19, #0x80]
	WORD	$0xad4b0be0	// 1788: ldp q0, q2, [sp, #0x160]
	WORD	$0xad050e61	// 178c: stp q1, q3, [x19, #0xa0]
	WORD	$0xad430fe1	// 1790: ldp q1, q3, [sp, #0x60]
	WORD	$0xad060660	// 1794: stp q0, q1, [x19, #0xc0]
	WORD	$0xad4c07e0	// 1798: ldp q0, q1, [sp, #0x180]
	WORD	$0xad070e62	// 179c: stp q2, q3, [x19, #0xe0]
	WORD	$0xad440fe2	// 17a0: ldp q2, q3, [sp, #0x80]
	WORD	$0xad080a60	// 17a4: stp q0, q2, [x19, #0x100]
	WORD	$0xad4d0be0	// 17a8: ldp q0, q2, [sp, #0x1a0]
	WORD	$0xad090e61	// 17ac: stp q1, q3, [x19, #0x120]
	WORD	$0xad450fe1	// 17b0: ldp q1, q3, [sp, #0xa0]
	WORD	$0xad0a0660	// 17b4: stp q0, q1, [x19, #0x140]
	WORD	$0xad4e07e0	// 17b8: ldp q0, q1, [sp, #0x1c0]
	WORD	$0xad0b0e62	// 17bc: stp q2, q3, [x19, #0x160]
	WORD	$0xad460fe2	// 17c0: ldp q2, q3, [sp, #0xc0]
	WORD	$0xad0c0a60	// 17c4: stp q0, q2, [x19, #0x180]
	WORD	$0xad4f0be0	// 17c8: ldp q0, q2, [sp, #0x1e0]
	WORD	$0xad0d0e61	// 17cc: stp q1, q3, [x19, #0x1a0]
	WORD	$0xad470fe1	// 17d0: ldp q1, q3, [sp, #0xe0]
	WORD	$0xad0e0660	// 17d4: stp q0, q1, [x19, #0x1c0]
	WORD	$0xad0f0e62	// 17d8: stp q2, q3, [x19, #0x1e0]
	WORD	$0x910803ff	// 17dc: add sp, sp, #0x200
	WORD	$0xa9417bfd	// 17e0: ldp x29, x30, [sp, #0x10]
	WORD	$0xa8c24ff4	// 17e4: ldp x20, x19, [sp], #0x20
	WORD	$0xd65f03c0	// 17e8: ret
	WORD	$0xa9be4ff4	// 17ec: stp x20, x19, [sp, #-0x20]!
	WORD	$0xa9017bfd	// 17f0: stp x29, x30, [sp, #0x10]
	WORD	$0x910043fd	// 17f4: add x29, sp, #0x10
	WORD	$0xaa0103f3	// 17f8: mov x19, x1
	WORD	$0xaa0003f4	// 17fc: mov x20, x0
	WORD	$0x94000141	// 1800: bl 0x1800 <_fadst16x16_neon+0x14>
	WORD	$0xaa1303e0	// 1804: mov x0, x19
	WORD	$0x9400013f	// 1808: bl 0x1808 <_fadst16x16_neon+0x1c>
	WORD	$0xad440680	// 180c: ldp q0, q1, [x20, #0x80]
	WORD	$0xad450e82	// 1810: ldp q2, q3, [x20, #0xa0]
	WORD	$0xad461684	// 1814: ldp q4, q5, [x20, #0xc0]
	WORD	$0xad471e86	// 1818: ldp q6, q7, [x20, #0xe0]
	WORD	$0x3dc00270	// 181c: ldr q16, [x19]
	WORD	$0x3d802290	// 1820: str q16, [x20, #0x80]
	WORD	$0x3dc00670	// 1824: ldr q16, [x19, #0x10]
	WORD	$0x3d802690	// 1828: str q16, [x20, #0x90]
	WORD	$0x3dc00a70	// 182c: ldr q16, [x19, #0x20]
	WORD	$0x3d802a90	// 1830: str q16, [x20, #0xa0]
	WORD	$0x3dc00e70	// 1834: ldr q16, [x19, #0x30]
	WORD	$0x3d802e90	// 1838: str q16, [x20, #0xb0]
	WORD	$0x3dc01270	// 183c: ldr q16, [x19, #0x40]
	WORD	$0x3d803290	// 1840: str q16, [x20, #0xc0]
	WORD	$0x3dc01670	// 1844: ldr q16, [x19, #0x50]
	WORD	$0x3d803690	// 1848: str q16, [x20, #0xd0]
	WORD	$0x3dc01a70	// 184c: ldr q16, [x19, #0x60]
	WORD	$0x3d803a90	// 1850: str q16, [x20, #0xe0]
	WORD	$0x3dc01e70	// 1854: ldr q16, [x19, #0x70]
	WORD	$0x3d803e90	// 1858: str q16, [x20, #0xf0]
	WORD	$0xad000660	// 185c: stp q0, q1, [x19]
	WORD	$0xad010e62	// 1860: stp q2, q3, [x19, #0x20]
	WORD	$0xad021664	// 1864: stp q4, q5, [x19, #0x40]
	WORD	$0xad031e66	// 1868: stp q6, q7, [x19, #0x60]
	WORD	$0xad400680	// 186c: ldp q0, q1, [x20]
	WORD	$0x4e412802	// 1870: trn1.8h v2, v0, v1
	WORD	$0x4e416800	// 1874: trn2.8h v0, v0, v1
	WORD	$0xad410e81	// 1878: ldp q1, q3, [x20, #0x20]
	WORD	$0x4e432824	// 187c: trn1.8h v4, v1, v3
	WORD	$0x4e436821	// 1880: trn2.8h v1, v1, v3
	WORD	$0xad421683	// 1884: ldp q3, q5, [x20, #0x40]
	WORD	$0x4e452866	// 1888: trn1.8h v6, v3, v5
	WORD	$0x4e456863	// 188c: trn2.8h v3, v3, v5
	WORD	$0xad431e85	// 1890: ldp q5, q7, [x20, #0x60]
	WORD	$0x4e4728b0	// 1894: trn1.8h v16, v5, v7
	WORD	$0x4e4768a5	// 1898: trn2.8h v5, v5, v7
	WORD	$0x4e842847	// 189c: trn1.4s v7, v2, v4
	WORD	$0x4e846842	// 18a0: trn2.4s v2, v2, v4
	WORD	$0x4e812804	// 18a4: trn1.4s v4, v0, v1
	WORD	$0x4e816800	// 18a8: trn2.4s v0, v0, v1
	WORD	$0x4e9028c1	// 18ac: trn1.4s v1, v6, v16
	WORD	$0x4e9068c6	// 18b0: trn2.4s v6, v6, v16
	WORD	$0x4e852870	// 18b4: trn1.4s v16, v3, v5
	WORD	$0x4e856863	// 18b8: trn2.4s v3, v3, v5
	WORD	$0x4ec178e5	// 18bc: zip2.2d v5, v7, v1
	WORD	$0x6e180427	// 18c0: mov.d v7[1], v1[0]
	WORD	$0x4ed07881	// 18c4: zip2.2d v1, v4, v16
	WORD	$0x6e180604	// 18c8: mov.d v4[1], v16[0]
	WORD	$0x4ec67850	// 18cc: zip2.2d v16, v2, v6
	WORD	$0x6e1804c2	// 18d0: mov.d v2[1], v6[0]
	WORD	$0x4ec37806	// 18d4: zip2.2d v6, v0, v3
	WORD	$0x6e180460	// 18d8: mov.d v0[1], v3[0]
	WORD	$0xad001287	// 18dc: stp q7, q4, [x20]
	WORD	$0xad010282	// 18e0: stp q2, q0, [x20, #0x20]
	WORD	$0xad020685	// 18e4: stp q5, q1, [x20, #0x40]
	WORD	$0xad031a90	// 18e8: stp q16, q6, [x20, #0x60]
	WORD	$0xad440680	// 18ec: ldp q0, q1, [x20, #0x80]
	WORD	$0x4e412802	// 18f0: trn1.8h v2, v0, v1
	WORD	$0x4e416800	// 18f4: trn2.8h v0, v0, v1
	WORD	$0xad450e81	// 18f8: ldp q1, q3, [x20, #0xa0]
	WORD	$0x4e432824	// 18fc: trn1.8h v4, v1, v3
	WORD	$0x4e436821	// 1900: trn2.8h v1, v1, v3
	WORD	$0xad461683	// 1904: ldp q3, q5, [x20, #0xc0]
	WORD	$0x4e452866	// 1908: trn1.8h v6, v3, v5
	WORD	$0x4e456863	// 190c: trn2.8h v3, v3, v5
	WORD	$0xad471e85	// 1910: ldp q5, q7, [x20, #0xe0]
	WORD	$0x4e4728b0	// 1914: trn1.8h v16, v5, v7
	WORD	$0x4e4768a5	// 1918: trn2.8h v5, v5, v7
	WORD	$0x4e842847	// 191c: trn1.4s v7, v2, v4
	WORD	$0x4e846842	// 1920: trn2.4s v2, v2, v4
	WORD	$0x4e812804	// 1924: trn1.4s v4, v0, v1
	WORD	$0x4e816800	// 1928: trn2.4s v0, v0, v1
	WORD	$0x4e9028c1	// 192c: trn1.4s v1, v6, v16
	WORD	$0x4e9068c6	// 1930: trn2.4s v6, v6, v16
	WORD	$0x4e852870	// 1934: trn1.4s v16, v3, v5
	WORD	$0x4e856863	// 1938: trn2.4s v3, v3, v5
	WORD	$0x4ec178e5	// 193c: zip2.2d v5, v7, v1
	WORD	$0x6e180427	// 1940: mov.d v7[1], v1[0]
	WORD	$0x4ed07881	// 1944: zip2.2d v1, v4, v16
	WORD	$0x6e180604	// 1948: mov.d v4[1], v16[0]
	WORD	$0x4ec67850	// 194c: zip2.2d v16, v2, v6
	WORD	$0x6e1804c2	// 1950: mov.d v2[1], v6[0]
	WORD	$0x4ec37806	// 1954: zip2.2d v6, v0, v3
	WORD	$0x6e180460	// 1958: mov.d v0[1], v3[0]
	WORD	$0xad041287	// 195c: stp q7, q4, [x20, #0x80]
	WORD	$0xad050282	// 1960: stp q2, q0, [x20, #0xa0]
	WORD	$0xad060685	// 1964: stp q5, q1, [x20, #0xc0]
	WORD	$0xad071a90	// 1968: stp q16, q6, [x20, #0xe0]
	WORD	$0xad400660	// 196c: ldp q0, q1, [x19]
	WORD	$0x4e412802	// 1970: trn1.8h v2, v0, v1
	WORD	$0x4e416800	// 1974: trn2.8h v0, v0, v1
	WORD	$0xad410e61	// 1978: ldp q1, q3, [x19, #0x20]
	WORD	$0x4e432824	// 197c: trn1.8h v4, v1, v3
	WORD	$0x4e436821	// 1980: trn2.8h v1, v1, v3
	WORD	$0xad421663	// 1984: ldp q3, q5, [x19, #0x40]
	WORD	$0x4e452866	// 1988: trn1.8h v6, v3, v5
	WORD	$0x4e456863	// 198c: trn2.8h v3, v3, v5
	WORD	$0xad431e65	// 1990: ldp q5, q7, [x19, #0x60]
	WORD	$0x4e4728b0	// 1994: trn1.8h v16, v5, v7
	WORD	$0x4e4768a5	// 1998: trn2.8h v5, v5, v7
	WORD	$0x4e842847	// 199c: trn1.4s v7, v2, v4
	WORD	$0x4e846842	// 19a0: trn2.4s v2, v2, v4
	WORD	$0x4e812804	// 19a4: trn1.4s v4, v0, v1
	WORD	$0x4e816800	// 19a8: trn2.4s v0, v0, v1
	WORD	$0x4e9028c1	// 19ac: trn1.4s v1, v6, v16
	WORD	$0x4e9068c6	// 19b0: trn2.4s v6, v6, v16
	WORD	$0x4e852870	// 19b4: trn1.4s v16, v3, v5
	WORD	$0x4e856863	// 19b8: trn2.4s v3, v3, v5
	WORD	$0x4ec178e5	// 19bc: zip2.2d v5, v7, v1
	WORD	$0x6e180427	// 19c0: mov.d v7[1], v1[0]
	WORD	$0x4ed07881	// 19c4: zip2.2d v1, v4, v16
	WORD	$0x6e180604	// 19c8: mov.d v4[1], v16[0]
	WORD	$0x4ec67850	// 19cc: zip2.2d v16, v2, v6
	WORD	$0x6e1804c2	// 19d0: mov.d v2[1], v6[0]
	WORD	$0x4ec37806	// 19d4: zip2.2d v6, v0, v3
	WORD	$0x6e180460	// 19d8: mov.d v0[1], v3[0]
	WORD	$0xad001267	// 19dc: stp q7, q4, [x19]
	WORD	$0xad010262	// 19e0: stp q2, q0, [x19, #0x20]
	WORD	$0xad020665	// 19e4: stp q5, q1, [x19, #0x40]
	WORD	$0xad031a70	// 19e8: stp q16, q6, [x19, #0x60]
	WORD	$0xad440660	// 19ec: ldp q0, q1, [x19, #0x80]
	WORD	$0x4e412802	// 19f0: trn1.8h v2, v0, v1
	WORD	$0x4e416800	// 19f4: trn2.8h v0, v0, v1
	WORD	$0xad450e61	// 19f8: ldp q1, q3, [x19, #0xa0]
	WORD	$0x4e432824	// 19fc: trn1.8h v4, v1, v3
	WORD	$0x4e436821	// 1a00: trn2.8h v1, v1, v3
	WORD	$0xad461663	// 1a04: ldp q3, q5, [x19, #0xc0]
	WORD	$0x4e452866	// 1a08: trn1.8h v6, v3, v5
	WORD	$0x4e456863	// 1a0c: trn2.8h v3, v3, v5
	WORD	$0xad471e65	// 1a10: ldp q5, q7, [x19, #0xe0]
	WORD	$0x4e4728b0	// 1a14: trn1.8h v16, v5, v7
	WORD	$0x4e4768a5	// 1a18: trn2.8h v5, v5, v7
	WORD	$0x4e842847	// 1a1c: trn1.4s v7, v2, v4
	WORD	$0x4e846842	// 1a20: trn2.4s v2, v2, v4
	WORD	$0x4e812804	// 1a24: trn1.4s v4, v0, v1
	WORD	$0x4e816800	// 1a28: trn2.4s v0, v0, v1
	WORD	$0x4e9028c1	// 1a2c: trn1.4s v1, v6, v16
	WORD	$0x4e9068c6	// 1a30: trn2.4s v6, v6, v16
	WORD	$0x4e852870	// 1a34: trn1.4s v16, v3, v5
	WORD	$0x4e856863	// 1a38: trn2.4s v3, v3, v5
	WORD	$0x4ec178e5	// 1a3c: zip2.2d v5, v7, v1
	WORD	$0x6e180427	// 1a40: mov.d v7[1], v1[0]
	WORD	$0x4ed07881	// 1a44: zip2.2d v1, v4, v16
	WORD	$0x6e180604	// 1a48: mov.d v4[1], v16[0]
	WORD	$0x4ec67850	// 1a4c: zip2.2d v16, v2, v6
	WORD	$0x6e1804c2	// 1a50: mov.d v2[1], v6[0]
	WORD	$0x4ec37806	// 1a54: zip2.2d v6, v0, v3
	WORD	$0x6e180460	// 1a58: mov.d v0[1], v3[0]
	WORD	$0xad041267	// 1a5c: stp q7, q4, [x19, #0x80]
	WORD	$0xad050262	// 1a60: stp q2, q0, [x19, #0xa0]
	WORD	$0xad060665	// 1a64: stp q5, q1, [x19, #0xc0]
	WORD	$0xad071a70	// 1a68: stp q16, q6, [x19, #0xe0]
	WORD	$0xa9417bfd	// 1a6c: ldp x29, x30, [sp, #0x10]
	WORD	$0xa8c24ff4	// 1a70: ldp x20, x19, [sp], #0x20
	WORD	$0xd65f03c0	// 1a74: ret
	WORD	$0xa9be4ff4	// 1a78: stp x20, x19, [sp, #-0x20]!
	WORD	$0xa9017bfd	// 1a7c: stp x29, x30, [sp, #0x10]
	WORD	$0x910043fd	// 1a80: add x29, sp, #0x10
	WORD	$0xaa0103f3	// 1a84: mov x19, x1
	WORD	$0xaa0003f4	// 1a88: mov x20, x0
	WORD	$0x940002cd	// 1a8c: bl 0x1a8c <_fdct16x16_neon+0x14>
	WORD	$0xaa1303e0	// 1a90: mov x0, x19
	WORD	$0x940002cb	// 1a94: bl 0x1a94 <_fdct16x16_neon+0x1c>
	WORD	$0xad440680	// 1a98: ldp q0, q1, [x20, #0x80]
	WORD	$0xad450e82	// 1a9c: ldp q2, q3, [x20, #0xa0]
	WORD	$0xad461684	// 1aa0: ldp q4, q5, [x20, #0xc0]
	WORD	$0xad471e86	// 1aa4: ldp q6, q7, [x20, #0xe0]
	WORD	$0x3dc00270	// 1aa8: ldr q16, [x19]
	WORD	$0x3d802290	// 1aac: str q16, [x20, #0x80]
	WORD	$0x3dc00670	// 1ab0: ldr q16, [x19, #0x10]
	WORD	$0x3d802690	// 1ab4: str q16, [x20, #0x90]
	WORD	$0x3dc00a70	// 1ab8: ldr q16, [x19, #0x20]
	WORD	$0x3d802a90	// 1abc: str q16, [x20, #0xa0]
	WORD	$0x3dc00e70	// 1ac0: ldr q16, [x19, #0x30]
	WORD	$0x3d802e90	// 1ac4: str q16, [x20, #0xb0]
	WORD	$0x3dc01270	// 1ac8: ldr q16, [x19, #0x40]
	WORD	$0x3d803290	// 1acc: str q16, [x20, #0xc0]
	WORD	$0x3dc01670	// 1ad0: ldr q16, [x19, #0x50]
	WORD	$0x3d803690	// 1ad4: str q16, [x20, #0xd0]
	WORD	$0x3dc01a70	// 1ad8: ldr q16, [x19, #0x60]
	WORD	$0x3d803a90	// 1adc: str q16, [x20, #0xe0]
	WORD	$0x3dc01e70	// 1ae0: ldr q16, [x19, #0x70]
	WORD	$0x3d803e90	// 1ae4: str q16, [x20, #0xf0]
	WORD	$0xad000660	// 1ae8: stp q0, q1, [x19]
	WORD	$0xad010e62	// 1aec: stp q2, q3, [x19, #0x20]
	WORD	$0xad021664	// 1af0: stp q4, q5, [x19, #0x40]
	WORD	$0xad031e66	// 1af4: stp q6, q7, [x19, #0x60]
	WORD	$0xad400680	// 1af8: ldp q0, q1, [x20]
	WORD	$0x4e412802	// 1afc: trn1.8h v2, v0, v1
	WORD	$0x4e416800	// 1b00: trn2.8h v0, v0, v1
	WORD	$0xad410e81	// 1b04: ldp q1, q3, [x20, #0x20]
	WORD	$0x4e432824	// 1b08: trn1.8h v4, v1, v3
	WORD	$0x4e436821	// 1b0c: trn2.8h v1, v1, v3
	WORD	$0xad421683	// 1b10: ldp q3, q5, [x20, #0x40]
	WORD	$0x4e452866	// 1b14: trn1.8h v6, v3, v5
	WORD	$0x4e456863	// 1b18: trn2.8h v3, v3, v5
	WORD	$0xad431e85	// 1b1c: ldp q5, q7, [x20, #0x60]
	WORD	$0x4e4728b0	// 1b20: trn1.8h v16, v5, v7
	WORD	$0x4e4768a5	// 1b24: trn2.8h v5, v5, v7
	WORD	$0x4e842847	// 1b28: trn1.4s v7, v2, v4
	WORD	$0x4e846842	// 1b2c: trn2.4s v2, v2, v4
	WORD	$0x4e812804	// 1b30: trn1.4s v4, v0, v1
	WORD	$0x4e816800	// 1b34: trn2.4s v0, v0, v1
	WORD	$0x4e9028c1	// 1b38: trn1.4s v1, v6, v16
	WORD	$0x4e9068c6	// 1b3c: trn2.4s v6, v6, v16
	WORD	$0x4e852870	// 1b40: trn1.4s v16, v3, v5
	WORD	$0x4e856863	// 1b44: trn2.4s v3, v3, v5
	WORD	$0x4ec178e5	// 1b48: zip2.2d v5, v7, v1
	WORD	$0x6e180427	// 1b4c: mov.d v7[1], v1[0]
	WORD	$0x4ed07881	// 1b50: zip2.2d v1, v4, v16
	WORD	$0x6e180604	// 1b54: mov.d v4[1], v16[0]
	WORD	$0x4ec67850	// 1b58: zip2.2d v16, v2, v6
	WORD	$0x6e1804c2	// 1b5c: mov.d v2[1], v6[0]
	WORD	$0x4ec37806	// 1b60: zip2.2d v6, v0, v3
	WORD	$0x6e180460	// 1b64: mov.d v0[1], v3[0]
	WORD	$0xad001287	// 1b68: stp q7, q4, [x20]
	WORD	$0xad010282	// 1b6c: stp q2, q0, [x20, #0x20]
	WORD	$0xad020685	// 1b70: stp q5, q1, [x20, #0x40]
	WORD	$0xad031a90	// 1b74: stp q16, q6, [x20, #0x60]
	WORD	$0xad440680	// 1b78: ldp q0, q1, [x20, #0x80]
	WORD	$0x4e412802	// 1b7c: trn1.8h v2, v0, v1
	WORD	$0x4e416800	// 1b80: trn2.8h v0, v0, v1
	WORD	$0xad450e81	// 1b84: ldp q1, q3, [x20, #0xa0]
	WORD	$0x4e432824	// 1b88: trn1.8h v4, v1, v3
	WORD	$0x4e436821	// 1b8c: trn2.8h v1, v1, v3
	WORD	$0xad461683	// 1b90: ldp q3, q5, [x20, #0xc0]
	WORD	$0x4e452866	// 1b94: trn1.8h v6, v3, v5
	WORD	$0x4e456863	// 1b98: trn2.8h v3, v3, v5
	WORD	$0xad471e85	// 1b9c: ldp q5, q7, [x20, #0xe0]
	WORD	$0x4e4728b0	// 1ba0: trn1.8h v16, v5, v7
	WORD	$0x4e4768a5	// 1ba4: trn2.8h v5, v5, v7
	WORD	$0x4e842847	// 1ba8: trn1.4s v7, v2, v4
	WORD	$0x4e846842	// 1bac: trn2.4s v2, v2, v4
	WORD	$0x4e812804	// 1bb0: trn1.4s v4, v0, v1
	WORD	$0x4e816800	// 1bb4: trn2.4s v0, v0, v1
	WORD	$0x4e9028c1	// 1bb8: trn1.4s v1, v6, v16
	WORD	$0x4e9068c6	// 1bbc: trn2.4s v6, v6, v16
	WORD	$0x4e852870	// 1bc0: trn1.4s v16, v3, v5
	WORD	$0x4e856863	// 1bc4: trn2.4s v3, v3, v5
	WORD	$0x4ec178e5	// 1bc8: zip2.2d v5, v7, v1
	WORD	$0x6e180427	// 1bcc: mov.d v7[1], v1[0]
	WORD	$0x4ed07881	// 1bd0: zip2.2d v1, v4, v16
	WORD	$0x6e180604	// 1bd4: mov.d v4[1], v16[0]
	WORD	$0x4ec67850	// 1bd8: zip2.2d v16, v2, v6
	WORD	$0x6e1804c2	// 1bdc: mov.d v2[1], v6[0]
	WORD	$0x4ec37806	// 1be0: zip2.2d v6, v0, v3
	WORD	$0x6e180460	// 1be4: mov.d v0[1], v3[0]
	WORD	$0xad041287	// 1be8: stp q7, q4, [x20, #0x80]
	WORD	$0xad050282	// 1bec: stp q2, q0, [x20, #0xa0]
	WORD	$0xad060685	// 1bf0: stp q5, q1, [x20, #0xc0]
	WORD	$0xad071a90	// 1bf4: stp q16, q6, [x20, #0xe0]
	WORD	$0xad400660	// 1bf8: ldp q0, q1, [x19]
	WORD	$0x4e412802	// 1bfc: trn1.8h v2, v0, v1
	WORD	$0x4e416800	// 1c00: trn2.8h v0, v0, v1
	WORD	$0xad410e61	// 1c04: ldp q1, q3, [x19, #0x20]
	WORD	$0x4e432824	// 1c08: trn1.8h v4, v1, v3
	WORD	$0x4e436821	// 1c0c: trn2.8h v1, v1, v3
	WORD	$0xad421663	// 1c10: ldp q3, q5, [x19, #0x40]
	WORD	$0x4e452866	// 1c14: trn1.8h v6, v3, v5
	WORD	$0x4e456863	// 1c18: trn2.8h v3, v3, v5
	WORD	$0xad431e65	// 1c1c: ldp q5, q7, [x19, #0x60]
	WORD	$0x4e4728b0	// 1c20: trn1.8h v16, v5, v7
	WORD	$0x4e4768a5	// 1c24: trn2.8h v5, v5, v7
	WORD	$0x4e842847	// 1c28: trn1.4s v7, v2, v4
	WORD	$0x4e846842	// 1c2c: trn2.4s v2, v2, v4
	WORD	$0x4e812804	// 1c30: trn1.4s v4, v0, v1
	WORD	$0x4e816800	// 1c34: trn2.4s v0, v0, v1
	WORD	$0x4e9028c1	// 1c38: trn1.4s v1, v6, v16
	WORD	$0x4e9068c6	// 1c3c: trn2.4s v6, v6, v16
	WORD	$0x4e852870	// 1c40: trn1.4s v16, v3, v5
	WORD	$0x4e856863	// 1c44: trn2.4s v3, v3, v5
	WORD	$0x4ec178e5	// 1c48: zip2.2d v5, v7, v1
	WORD	$0x6e180427	// 1c4c: mov.d v7[1], v1[0]
	WORD	$0x4ed07881	// 1c50: zip2.2d v1, v4, v16
	WORD	$0x6e180604	// 1c54: mov.d v4[1], v16[0]
	WORD	$0x4ec67850	// 1c58: zip2.2d v16, v2, v6
	WORD	$0x6e1804c2	// 1c5c: mov.d v2[1], v6[0]
	WORD	$0x4ec37806	// 1c60: zip2.2d v6, v0, v3
	WORD	$0x6e180460	// 1c64: mov.d v0[1], v3[0]
	WORD	$0xad001267	// 1c68: stp q7, q4, [x19]
	WORD	$0xad010262	// 1c6c: stp q2, q0, [x19, #0x20]
	WORD	$0xad020665	// 1c70: stp q5, q1, [x19, #0x40]
	WORD	$0xad031a70	// 1c74: stp q16, q6, [x19, #0x60]
	WORD	$0xad440660	// 1c78: ldp q0, q1, [x19, #0x80]
	WORD	$0x4e412802	// 1c7c: trn1.8h v2, v0, v1
	WORD	$0x4e416800	// 1c80: trn2.8h v0, v0, v1
	WORD	$0xad450e61	// 1c84: ldp q1, q3, [x19, #0xa0]
	WORD	$0x4e432824	// 1c88: trn1.8h v4, v1, v3
	WORD	$0x4e436821	// 1c8c: trn2.8h v1, v1, v3
	WORD	$0xad461663	// 1c90: ldp q3, q5, [x19, #0xc0]
	WORD	$0x4e452866	// 1c94: trn1.8h v6, v3, v5
	WORD	$0x4e456863	// 1c98: trn2.8h v3, v3, v5
	WORD	$0xad471e65	// 1c9c: ldp q5, q7, [x19, #0xe0]
	WORD	$0x4e4728b0	// 1ca0: trn1.8h v16, v5, v7
	WORD	$0x4e4768a5	// 1ca4: trn2.8h v5, v5, v7
	WORD	$0x4e842847	// 1ca8: trn1.4s v7, v2, v4
	WORD	$0x4e846842	// 1cac: trn2.4s v2, v2, v4
	WORD	$0x4e812804	// 1cb0: trn1.4s v4, v0, v1
	WORD	$0x4e816800	// 1cb4: trn2.4s v0, v0, v1
	WORD	$0x4e9028c1	// 1cb8: trn1.4s v1, v6, v16
	WORD	$0x4e9068c6	// 1cbc: trn2.4s v6, v6, v16
	WORD	$0x4e852870	// 1cc0: trn1.4s v16, v3, v5
	WORD	$0x4e856863	// 1cc4: trn2.4s v3, v3, v5
	WORD	$0x4ec178e5	// 1cc8: zip2.2d v5, v7, v1
	WORD	$0x6e180427	// 1ccc: mov.d v7[1], v1[0]
	WORD	$0x4ed07881	// 1cd0: zip2.2d v1, v4, v16
	WORD	$0x6e180604	// 1cd4: mov.d v4[1], v16[0]
	WORD	$0x4ec67850	// 1cd8: zip2.2d v16, v2, v6
	WORD	$0x6e1804c2	// 1cdc: mov.d v2[1], v6[0]
	WORD	$0x4ec37806	// 1ce0: zip2.2d v6, v0, v3
	WORD	$0x6e180460	// 1ce4: mov.d v0[1], v3[0]
	WORD	$0xad041267	// 1ce8: stp q7, q4, [x19, #0x80]
	WORD	$0xad050262	// 1cec: stp q2, q0, [x19, #0xa0]
	WORD	$0xad060665	// 1cf0: stp q5, q1, [x19, #0xc0]
	WORD	$0xad071a70	// 1cf4: stp q16, q6, [x19, #0xe0]
	WORD	$0xa9417bfd	// 1cf8: ldp x29, x30, [sp, #0x10]
	WORD	$0xa8c24ff4	// 1cfc: ldp x20, x19, [sp], #0x20
	WORD	$0xd65f03c0	// 1d00: ret
	WORD	$0x6dbb3bef	// 1d04: stp d15, d14, [sp, #-0x50]!
	WORD	$0x6d0133ed	// 1d08: stp d13, d12, [sp, #0x10]
	WORD	$0x6d022beb	// 1d0c: stp d11, d10, [sp, #0x20]
	WORD	$0x6d0323e9	// 1d10: stp d9, d8, [sp, #0x30]
	WORD	$0xa9046ffc	// 1d14: stp x28, x27, [sp, #0x40]
	WORD	$0xd10883ff	// 1d18: sub sp, sp, #0x220
	WORD	$0x5287fd88	// 1d1c: mov w8, #0x3fec             ; =16364
	WORD	$0x4e020d00	// 1d20: dup.8h v0, w8
	WORD	$0x52806488	// 1d24: mov w8, #0x324              ; =804
	WORD	$0x4e020d03	// 1d28: dup.8h v3, w8
	WORD	$0xad471001	// 1d2c: ldp q1, q4, [x0, #0xe0]
	WORD	$0x0e63c086	// 1d30: smull.4s v6, v4, v3
	WORD	$0x4e63c087	// 1d34: smull2.4s v7, v4, v3
	WORD	$0xad400805	// 1d38: ldp q5, q2, [x0]
	WORD	$0x0e63c0b0	// 1d3c: smull.4s v16, v5, v3
	WORD	$0x0e608090	// 1d40: smlal.4s v16, v4, v0
	WORD	$0x4e63c0a3	// 1d44: smull2.4s v3, v5, v3
	WORD	$0x4e608083	// 1d48: smlal2.4s v3, v4, v0
	WORD	$0x3d8067e3	// 1d4c: str q3, [sp, #0x190]
	WORD	$0x0e60a0a6	// 1d50: smlsl.4s v6, v5, v0
	WORD	$0xad1043e6	// 1d54: stp q6, q16, [sp, #0x200]
	WORD	$0x4e60a0a7	// 1d58: smlsl2.4s v7, v5, v0
	WORD	$0x4ea71cea	// 1d5c: mov.16b v10, v7
	WORD	$0x3d8013e7	// 1d60: str q7, [sp, #0x40]
	WORD	$0x5287c2a8	// 1d64: mov w8, #0x3e15             ; =15893
	WORD	$0x4e020d04	// 1d68: dup.8h v4, w8
	WORD	$0x5281f1a8	// 1d6c: mov w8, #0xf8d              ; =3981
	WORD	$0x4e020d05	// 1d70: dup.8h v5, w8
	WORD	$0xad461800	// 1d74: ldp q0, q6, [x0, #0xc0]
	WORD	$0x0e65c0d0	// 1d78: smull.4s v16, v6, v5
	WORD	$0x4e65c0d1	// 1d7c: smull2.4s v17, v6, v5
	WORD	$0xad410c07	// 1d80: ldp q7, q3, [x0, #0x20]
	WORD	$0x0e65c0f2	// 1d84: smull.4s v18, v7, v5
	WORD	$0x0e6480d2	// 1d88: smlal.4s v18, v6, v4
	WORD	$0x4e65c0e5	// 1d8c: smull2.4s v5, v7, v5
	WORD	$0x4e6480c5	// 1d90: smlal2.4s v5, v6, v4
	WORD	$0x0e64a0f0	// 1d94: smlsl.4s v16, v7, v4
	WORD	$0xad0d4bf0	// 1d98: stp q16, q18, [sp, #0x1a0]
	WORD	$0x4e64a0f1	// 1d9c: smlsl2.4s v17, v7, v4
	WORD	$0xad0e17f1	// 1da0: stp q17, q5, [sp, #0x1c0]
	WORD	$0x52873b68	// 1da4: mov w8, #0x39db             ; =14811
	WORD	$0x4e020d06	// 1da8: dup.8h v6, w8
	WORD	$0x52836ba8	// 1dac: mov w8, #0x1b5d             ; =7005
	WORD	$0x4e020d07	// 1db0: dup.8h v7, w8
	WORD	$0xad454004	// 1db4: ldp q4, q16, [x0, #0xa0]
	WORD	$0x0e67c20e	// 1db8: smull.4s v14, v16, v7
	WORD	$0x4e67c20f	// 1dbc: smull2.4s v15, v16, v7
	WORD	$0xad421411	// 1dc0: ldp q17, q5, [x0, #0x40]
	WORD	$0x0e67c23f	// 1dc4: smull.4s v31, v17, v7
	WORD	$0x0e66821f	// 1dc8: smlal.4s v31, v16, v6
	WORD	$0x4e67c23e	// 1dcc: smull2.4s v30, v17, v7
	WORD	$0x4e66821e	// 1dd0: smlal2.4s v30, v16, v6
	WORD	$0x0e66a22e	// 1dd4: smlsl.4s v14, v17, v6
	WORD	$0x4e66a22f	// 1dd8: smlsl2.4s v15, v17, v6
	WORD	$0x52866d08	// 1ddc: mov w8, #0x3368             ; =13160
	WORD	$0x4e020d06	// 1de0: dup.8h v6, w8
	WORD	$0x5284c408	// 1de4: mov w8, #0x2620             ; =9760
	WORD	$0x4e020d07	// 1de8: dup.8h v7, w8
	WORD	$0x52855f68	// 1dec: mov w8, #0x2afb             ; =11003
	WORD	$0x4e020d10	// 1df0: dup.8h v16, w8
	WORD	$0x5285ed88	// 1df4: mov w8, #0x2f6c             ; =12140
	WORD	$0x4e020d11	// 1df8: dup.8h v17, w8
	WORD	$0xad444813	// 1dfc: ldp q19, q18, [x0, #0x80]
	WORD	$0x0e67c257	// 1e00: smull.4s v23, v18, v7
	WORD	$0x4e67c259	// 1e04: smull2.4s v25, v18, v7
	WORD	$0xad435414	// 1e08: ldp q20, q21, [x0, #0x60]
	WORD	$0x0e67c298	// 1e0c: smull.4s v24, v20, v7
	WORD	$0x52841ce8	// 1e10: mov w8, #0x20e7             ; =8423
	WORD	$0x4e020d16	// 1e14: dup.8h v22, w8
	WORD	$0x0e668258	// 1e18: smlal.4s v24, v18, v6
	WORD	$0x3d8057f8	// 1e1c: str q24, [sp, #0x150]
	WORD	$0x5286dca8	// 1e20: mov w8, #0x36e5             ; =14053
	WORD	$0x4e020d18	// 1e24: dup.8h v24, w8
	WORD	$0x4e67c287	// 1e28: smull2.4s v7, v20, v7
	WORD	$0x5282b208	// 1e2c: mov w8, #0x1590             ; =5520
	WORD	$0x4e020d1a	// 1e30: dup.8h v26, w8
	WORD	$0x4e668247	// 1e34: smlal2.4s v7, v18, v6
	WORD	$0x0e66a297	// 1e38: smlsl.4s v23, v20, v6
	WORD	$0xad095fe7	// 1e3c: stp q7, q23, [sp, #0x120]
	WORD	$0x4e66a299	// 1e40: smlsl2.4s v25, v20, v6
	WORD	$0x3d8053f9	// 1e44: str q25, [sp, #0x140]
	WORD	$0x0e71c2ab	// 1e48: smull.4s v11, v21, v17
	WORD	$0x4e71c2a6	// 1e4c: smull2.4s v6, v21, v17
	WORD	$0x0e71c267	// 1e50: smull.4s v7, v19, v17
	WORD	$0x0e7082a7	// 1e54: smlal.4s v7, v21, v16
	WORD	$0x4ea71cf2	// 1e58: mov.16b v18, v7
	WORD	$0x3d801fe7	// 1e5c: str q7, [sp, #0x70]
	WORD	$0x4e71c267	// 1e60: smull2.4s v7, v19, v17
	WORD	$0x4e7082a7	// 1e64: smlal2.4s v7, v21, v16
	WORD	$0x4ea71cf4	// 1e68: mov.16b v20, v7
	WORD	$0x3d800fe7	// 1e6c: str q7, [sp, #0x30]
	WORD	$0x0e70a26b	// 1e70: smlsl.4s v11, v19, v16
	WORD	$0x4eab1d71	// 1e74: mov.16b v17, v11
	WORD	$0x4e70a266	// 1e78: smlsl2.4s v6, v19, v16
	WORD	$0x4ea61cd5	// 1e7c: mov.16b v21, v6
	WORD	$0xad02afe6	// 1e80: stp q6, q11, [sp, #0x50]
	WORD	$0x0e78c0ab	// 1e84: smull.4s v11, v5, v24
	WORD	$0x4e78c0a6	// 1e88: smull2.4s v6, v5, v24
	WORD	$0x0e78c08d	// 1e8c: smull.4s v13, v4, v24
	WORD	$0x0e7680ad	// 1e90: smlal.4s v13, v5, v22
	WORD	$0x4e78c08c	// 1e94: smull2.4s v12, v4, v24
	WORD	$0x4e7680ac	// 1e98: smlal2.4s v12, v5, v22
	WORD	$0x52878848	// 1e9c: mov w8, #0x3c42             ; =15426
	WORD	$0x4e020d07	// 1ea0: dup.8h v7, w8
	WORD	$0x0e76a08b	// 1ea4: smlsl.4s v11, v4, v22
	WORD	$0x4eab1d65	// 1ea8: mov.16b v5, v11
	WORD	$0x4e76a086	// 1eac: smlsl2.4s v6, v4, v22
	WORD	$0x0e67c06b	// 1eb0: smull.4s v11, v3, v7
	WORD	$0x0e67c009	// 1eb4: smull.4s v9, v0, v7
	WORD	$0x0e7a8069	// 1eb8: smlal.4s v9, v3, v26
	WORD	$0x4e67c068	// 1ebc: smull2.4s v8, v3, v7
	WORD	$0x4e67c01d	// 1ec0: smull2.4s v29, v0, v7
	WORD	$0x4e7a807d	// 1ec4: smlal2.4s v29, v3, v26
	WORD	$0x0e7aa00b	// 1ec8: smlsl.4s v11, v0, v26
	WORD	$0x4e7aa008	// 1ecc: smlsl2.4s v8, v0, v26
	WORD	$0x52812c88	// 1ed0: mov w8, #0x964              ; =2404
	WORD	$0x4e020d10	// 1ed4: dup.8h v16, w8
	WORD	$0x5287e9e8	// 1ed8: mov w8, #0x3f4f             ; =16207
	WORD	$0x4e020d00	// 1edc: dup.8h v0, w8
	WORD	$0x0e60c03c	// 1ee0: smull.4s v28, v1, v0
	WORD	$0x0e70805c	// 1ee4: smlal.4s v28, v2, v16
	WORD	$0x0e60c05a	// 1ee8: smull.4s v26, v2, v0
	WORD	$0x4e60c05b	// 1eec: smull2.4s v27, v2, v0
	WORD	$0x4e60c039	// 1ef0: smull2.4s v25, v1, v0
	WORD	$0x4e708059	// 1ef4: smlal2.4s v25, v2, v16
	WORD	$0x0e70a03a	// 1ef8: smlsl.4s v26, v1, v16
	WORD	$0x4e70a03b	// 1efc: smlsl2.4s v27, v1, v16
	WORD	$0xad500be0	// 1f00: ldp q0, q2, [sp, #0x200]
	WORD	$0x6eb28441	// 1f04: sub.4s v1, v2, v18
	WORD	$0x4f322421	// 1f08: srshr.4s v1, v1, #0xe
	WORD	$0x6eb18402	// 1f0c: sub.4s v2, v0, v17
	WORD	$0x4f322442	// 1f10: srshr.4s v2, v2, #0xe
	WORD	$0x5287d8a8	// 1f14: mov w8, #0x3ec5             ; =16069
	WORD	$0x52818f89	// 1f18: mov w9, #0xc7c              ; =3196
	WORD	$0x4e040d10	// 1f1c: dup.4s v16, w8
	WORD	$0x4e040d33	// 1f20: dup.4s v19, w9
	WORD	$0x4eb09c23	// 1f24: mul.4s v3, v1, v16
	WORD	$0x4eb39c20	// 1f28: mul.4s v0, v1, v19
	WORD	$0x4eb39443	// 1f2c: mla.4s v3, v2, v19
	WORD	$0x3d807fe3	// 1f30: str q3, [sp, #0x1f0]
	WORD	$0x1287d888	// 1f34: mov w8, #-0x3ec5            ; =-16069
	WORD	$0x4e040d01	// 1f38: dup.4s v1, w8
	WORD	$0x4ea19440	// 1f3c: mla.4s v0, v2, v1
	WORD	$0x3d805be0	// 1f40: str q0, [sp, #0x160]
	WORD	$0x3dc067f8	// 1f44: ldr q24, [sp, #0x190]
	WORD	$0x6eb48702	// 1f48: sub.4s v2, v24, v20
	WORD	$0x4f322442	// 1f4c: srshr.4s v2, v2, #0xe
	WORD	$0x6eb58554	// 1f50: sub.4s v20, v10, v21
	WORD	$0x4f322694	// 1f54: srshr.4s v20, v20, #0xe
	WORD	$0x4eb39c40	// 1f58: mul.4s v0, v2, v19
	WORD	$0x4ea19680	// 1f5c: mla.4s v0, v20, v1
	WORD	$0x3d8063e0	// 1f60: str q0, [sp, #0x180]
	WORD	$0x4eb09c40	// 1f64: mul.4s v0, v2, v16
	WORD	$0x4eb39680	// 1f68: mla.4s v0, v20, v19
	WORD	$0x3d805fe0	// 1f6c: str q0, [sp, #0x170]
	WORD	$0x6ea987e1	// 1f70: sub.4s v1, v31, v9
	WORD	$0x4f322421	// 1f74: srshr.4s v1, v1, #0xe
	WORD	$0x4ebe1fd7	// 1f78: mov.16b v23, v30
	WORD	$0x6ebd87c2	// 1f7c: sub.4s v2, v30, v29
	WORD	$0x4f322442	// 1f80: srshr.4s v2, v2, #0xe
	WORD	$0x4eae1dd6	// 1f84: mov.16b v22, v14
	WORD	$0x6eab85d4	// 1f88: sub.4s v20, v14, v11
	WORD	$0x4f322694	// 1f8c: srshr.4s v20, v20, #0xe
	WORD	$0x4eaf1df2	// 1f90: mov.16b v18, v15
	WORD	$0x6ea885f5	// 1f94: sub.4s v21, v15, v8
	WORD	$0x4f3226b5	// 1f98: srshr.4s v21, v21, #0xe
	WORD	$0x4eb09c20	// 1f9c: mul.4s v0, v1, v16
	WORD	$0x4eb39680	// 1fa0: mla.4s v0, v20, v19
	WORD	$0x3d803fe0	// 1fa4: str q0, [sp, #0xf0]
	WORD	$0x4eb09c40	// 1fa8: mul.4s v0, v2, v16
	WORD	$0x4eb396a0	// 1fac: mla.4s v0, v21, v19
	WORD	$0x3d8047e0	// 1fb0: str q0, [sp, #0x110]
	WORD	$0x12818f68	// 1fb4: mov w8, #-0xc7c             ; =-3196
	WORD	$0x4e040d13	// 1fb8: dup.4s v19, w8
	WORD	$0x4eb39c20	// 1fbc: mul.4s v0, v1, v19
	WORD	$0x4eb09680	// 1fc0: mla.4s v0, v20, v16
	WORD	$0x3d802fe0	// 1fc4: str q0, [sp, #0xb0]
	WORD	$0x4eb39c40	// 1fc8: mul.4s v0, v2, v19
	WORD	$0x4eb096a0	// 1fcc: mla.4s v0, v21, v16
	WORD	$0x3d8043e0	// 1fd0: str q0, [sp, #0x100]
	WORD	$0xad4d3bf1	// 1fd4: ldp q17, q14, [sp, #0x1a0]
	WORD	$0x6ead85c1	// 1fd8: sub.4s v1, v14, v13
	WORD	$0x4f322421	// 1fdc: srshr.4s v1, v1, #0xe
	WORD	$0x4ea51ca7	// 1fe0: mov.16b v7, v5
	WORD	$0x6ea58622	// 1fe4: sub.4s v2, v17, v5
	WORD	$0x4f322442	// 1fe8: srshr.4s v2, v2, #0xe
	WORD	$0x528471c8	// 1fec: mov w8, #0x238e             ; =9102
	WORD	$0x5286a6e9	// 1ff0: mov w9, #0x3537             ; =13623
	WORD	$0x4e040d10	// 1ff4: dup.4s v16, w8
	WORD	$0x4eb09c20	// 1ff8: mul.4s v0, v1, v16
	WORD	$0x4e040d33	// 1ffc: dup.4s v19, w9
	WORD	$0x4eb39c23	// 2000: mul.4s v3, v1, v19
	WORD	$0x4eb39440	// 2004: mla.4s v0, v2, v19
	WORD	$0x128471a8	// 2008: mov w8, #-0x238e            ; =-9102
	WORD	$0x4e040d01	// 200c: dup.4s v1, w8
	WORD	$0x4ea19443	// 2010: mla.4s v3, v2, v1
	WORD	$0xad068fe0	// 2014: stp q0, q3, [sp, #0xd0]
	WORD	$0xad4e3ffe	// 2018: ldp q30, q15, [sp, #0x1c0]
	WORD	$0x6eac85e2	// 201c: sub.4s v2, v15, v12
	WORD	$0x4f322442	// 2020: srshr.4s v2, v2, #0xe
	WORD	$0x6ea687d4	// 2024: sub.4s v20, v30, v6
	WORD	$0x4f322694	// 2028: srshr.4s v20, v20, #0xe
	WORD	$0x4eb39c40	// 202c: mul.4s v0, v2, v19
	WORD	$0x4ea19680	// 2030: mla.4s v0, v20, v1
	WORD	$0x3d8033e0	// 2034: str q0, [sp, #0xc0]
	WORD	$0x4eb09c40	// 2038: mul.4s v0, v2, v16
	WORD	$0x4eb39680	// 203c: mla.4s v0, v20, v19
	WORD	$0x3d802be0	// 2040: str q0, [sp, #0xa0]
	WORD	$0xad4a17e3	// 2044: ldp q3, q5, [sp, #0x140]
	WORD	$0x6ebc84a1	// 2048: sub.4s v1, v5, v28
	WORD	$0x4f322421	// 204c: srshr.4s v1, v1, #0xe
	WORD	$0xad492be4	// 2050: ldp q4, q10, [sp, #0x120]
	WORD	$0x6eb98482	// 2054: sub.4s v2, v4, v25
	WORD	$0x4f322442	// 2058: srshr.4s v2, v2, #0xe
	WORD	$0x6eba8554	// 205c: sub.4s v20, v10, v26
	WORD	$0x4f322694	// 2060: srshr.4s v20, v20, #0xe
	WORD	$0x6ebb8475	// 2064: sub.4s v21, v3, v27
	WORD	$0x4f3226a0	// 2068: srshr.4s v0, v21, #0xe
	WORD	$0x4eb09c35	// 206c: mul.4s v21, v1, v16
	WORD	$0x4eb39695	// 2070: mla.4s v21, v20, v19
	WORD	$0x3d8027f5	// 2074: str q21, [sp, #0x90]
	WORD	$0x4eb09c55	// 2078: mul.4s v21, v2, v16
	WORD	$0x4eb39415	// 207c: mla.4s v21, v0, v19
	WORD	$0x3d8023f5	// 2080: str q21, [sp, #0x80]
	WORD	$0x1286a6c8	// 2084: mov w8, #-0x3537            ; =-13623
	WORD	$0x4e040d13	// 2088: dup.4s v19, w8
	WORD	$0x4eb39c21	// 208c: mul.4s v1, v1, v19
	WORD	$0x4eb09681	// 2090: mla.4s v1, v20, v16
	WORD	$0x4ea11c35	// 2094: mov.16b v21, v1
	WORD	$0x3d8003e1	// 2098: str q1, [sp]
	WORD	$0x4eb39c41	// 209c: mul.4s v1, v2, v19
	WORD	$0x4eb09401	// 20a0: mla.4s v1, v0, v16
	WORD	$0x3d807be1	// 20a4: str q1, [sp, #0x1e0]
	WORD	$0x3dc087e0	// 20a8: ldr q0, [sp, #0x210]
	WORD	$0xad430be1	// 20ac: ldp q1, q2, [sp, #0x60]
	WORD	$0x4ea08454	// 20b0: add.4s v20, v2, v0
	WORD	$0x3dc00fe0	// 20b4: ldr q0, [sp, #0x30]
	WORD	$0x4eb88418	// 20b8: add.4s v24, v0, v24
	WORD	$0x3dc083e0	// 20bc: ldr q0, [sp, #0x200]
	WORD	$0x4ea08422	// 20c0: add.4s v2, v1, v0
	WORD	$0x3d8067e2	// 20c4: str q2, [sp, #0x190]
	WORD	$0xad4203e1	// 20c8: ldp q1, q0, [sp, #0x40]
	WORD	$0x4ea18410	// 20cc: add.4s v16, v0, v1
	WORD	$0xad0353f0	// 20d0: stp q16, q20, [sp, #0x60]
	WORD	$0x4eae85b3	// 20d4: add.4s v19, v13, v14
	WORD	$0xad01cff8	// 20d8: stp q24, q19, [sp, #0x30]
	WORD	$0x4eaf858e	// 20dc: add.4s v14, v12, v15
	WORD	$0x4eb184ed	// 20e0: add.4s v13, v7, v17
	WORD	$0x3d8017ed	// 20e4: str q13, [sp, #0x50]
	WORD	$0x4ebe84cc	// 20e8: add.4s v12, v6, v30
	WORD	$0xad00bbec	// 20ec: stp q12, q14, [sp, #0x10]
	WORD	$0x4ebf8520	// 20f0: add.4s v0, v9, v31
	WORD	$0x4eb787b1	// 20f4: add.4s v17, v29, v23
	WORD	$0x4eb68561	// 20f8: add.4s v1, v11, v22
	WORD	$0x4eb28512	// 20fc: add.4s v18, v8, v18
	WORD	$0x4ea58786	// 2100: add.4s v6, v28, v5
	WORD	$0x4ea48736	// 2104: add.4s v22, v25, v4
	WORD	$0x4eaa8747	// 2108: add.4s v7, v26, v10
	WORD	$0x4ea38769	// 210c: add.4s v9, v27, v3
	WORD	$0x4f322403	// 2110: srshr.4s v3, v0, #0xe
	WORD	$0x3d8077e3	// 2114: str q3, [sp, #0x1d0]
	WORD	$0x4f322421	// 2118: srshr.4s v1, v1, #0xe
	WORD	$0x3d8087e1	// 211c: str q1, [sp, #0x210]
	WORD	$0x4f322680	// 2120: srshr.4s v0, v20, #0xe
	WORD	$0x6ea38404	// 2124: sub.4s v4, v0, v3
	WORD	$0x4f322440	// 2128: srshr.4s v0, v2, #0xe
	WORD	$0x6ea18405	// 212c: sub.4s v5, v0, v1
	WORD	$0x52876428	// 2130: mov w8, #0x3b21             ; =15137
	WORD	$0x52830fc9	// 2134: mov w9, #0x187e             ; =6270
	WORD	$0x4e040d01	// 2138: dup.4s v1, w8
	WORD	$0x4ea19c83	// 213c: mul.4s v3, v4, v1
	WORD	$0x4e040d20	// 2140: dup.4s v0, w9
	WORD	$0x4ea09c82	// 2144: mul.4s v2, v4, v0
	WORD	$0x4ea094a3	// 2148: mla.4s v3, v5, v0
	WORD	$0x12876408	// 214c: mov w8, #-0x3b21            ; =-15137
	WORD	$0x4e040d17	// 2150: dup.4s v23, w8
	WORD	$0x4eb794a2	// 2154: mla.4s v2, v5, v23
	WORD	$0x3d8073e2	// 2158: str q2, [sp, #0x1c0]
	WORD	$0x4f322624	// 215c: srshr.4s v4, v17, #0xe
	WORD	$0xad0d13e3	// 2160: stp q3, q4, [sp, #0x1a0]
	WORD	$0x4f322643	// 2164: srshr.4s v3, v18, #0xe
	WORD	$0x3d8083e3	// 2168: str q3, [sp, #0x200]
	WORD	$0x4f322702	// 216c: srshr.4s v2, v24, #0xe
	WORD	$0x6ea48442	// 2170: sub.4s v2, v2, v4
	WORD	$0x4f322604	// 2174: srshr.4s v4, v16, #0xe
	WORD	$0x6ea38484	// 2178: sub.4s v4, v4, v3
	WORD	$0x4ea19c4a	// 217c: mul.4s v10, v2, v1
	WORD	$0x4ea09c42	// 2180: mul.4s v2, v2, v0
	WORD	$0x4ea0948a	// 2184: mla.4s v10, v4, v0
	WORD	$0x4eb79482	// 2188: mla.4s v2, v4, v23
	WORD	$0x4f3224c4	// 218c: srshr.4s v4, v6, #0xe
	WORD	$0x3d804fe4	// 2190: str q4, [sp, #0x130]
	WORD	$0x4f3224e3	// 2194: srshr.4s v3, v7, #0xe
	WORD	$0xad0a0be3	// 2198: stp q3, q2, [sp, #0x140]
	WORD	$0x4f322662	// 219c: srshr.4s v2, v19, #0xe
	WORD	$0x6ea48442	// 21a0: sub.4s v2, v2, v4
	WORD	$0x4f3225a4	// 21a4: srshr.4s v4, v13, #0xe
	WORD	$0x6ea38484	// 21a8: sub.4s v4, v4, v3
	WORD	$0x4ea19c48	// 21ac: mul.4s v8, v2, v1
	WORD	$0x4ea09488	// 21b0: mla.4s v8, v4, v0
	WORD	$0x12830fa8	// 21b4: mov w8, #-0x187e            ; =-6270
	WORD	$0x4e040d12	// 21b8: dup.4s v18, w8
	WORD	$0x4eb29c5b	// 21bc: mul.4s v27, v2, v18
	WORD	$0x4ea1949b	// 21c0: mla.4s v27, v4, v1
	WORD	$0x4f3226de	// 21c4: srshr.4s v30, v22, #0xe
	WORD	$0x4f322523	// 21c8: srshr.4s v3, v9, #0xe
	WORD	$0x3d804be3	// 21cc: str q3, [sp, #0x120]
	WORD	$0x4f3225c2	// 21d0: srshr.4s v2, v14, #0xe
	WORD	$0x6ebe8442	// 21d4: sub.4s v2, v2, v30
	WORD	$0x4f322584	// 21d8: srshr.4s v4, v12, #0xe
	WORD	$0x6ea38484	// 21dc: sub.4s v4, v4, v3
	WORD	$0x4ea19c49	// 21e0: mul.4s v9, v2, v1
	WORD	$0x4ea09489	// 21e4: mla.4s v9, v4, v0
	WORD	$0x4eb29c56	// 21e8: mul.4s v22, v2, v18
	WORD	$0x4ea19496	// 21ec: mla.4s v22, v4, v1
	WORD	$0x3dc02feb	// 21f0: ldr q11, [sp, #0xb0]
	WORD	$0x3dc07fe2	// 21f4: ldr q2, [sp, #0x1f0]
	WORD	$0x6eab8442	// 21f8: sub.4s v2, v2, v11
	WORD	$0x4f322442	// 21fc: srshr.4s v2, v2, #0xe
	WORD	$0xad4b7fee	// 2200: ldp q14, q31, [sp, #0x160]
	WORD	$0xad47f7ec	// 2204: ldp q12, q29, [sp, #0xf0]
	WORD	$0x6eac85c4	// 2208: sub.4s v4, v14, v12
	WORD	$0x4f322484	// 220c: srshr.4s v4, v4, #0xe
	WORD	$0x4ea19c59	// 2210: mul.4s v25, v2, v1
	WORD	$0x4ea09c5a	// 2214: mul.4s v26, v2, v0
	WORD	$0x4ea09499	// 2218: mla.4s v25, v4, v0
	WORD	$0x4eb7949a	// 221c: mla.4s v26, v4, v23
	WORD	$0x6ebd87e2	// 2220: sub.4s v2, v31, v29
	WORD	$0x4f322443	// 2224: srshr.4s v3, v2, #0xe
	WORD	$0x3dc063ef	// 2228: ldr q15, [sp, #0x180]
	WORD	$0x3dc047fc	// 222c: ldr q28, [sp, #0x110]
	WORD	$0x6ebc85e4	// 2230: sub.4s v4, v15, v28
	WORD	$0x4f322482	// 2234: srshr.4s v2, v4, #0xe
	WORD	$0x4ea09c74	// 2238: mul.4s v20, v3, v0
	WORD	$0x4eb79454	// 223c: mla.4s v20, v2, v23
	WORD	$0x4ea19c66	// 2240: mul.4s v6, v3, v1
	WORD	$0x4ea09446	// 2244: mla.4s v6, v2, v0
	WORD	$0xad46cff8	// 2248: ldp q24, q19, [sp, #0xd0]
	WORD	$0x6eb58702	// 224c: sub.4s v2, v24, v21
	WORD	$0x4f322442	// 2250: srshr.4s v2, v2, #0xe
	WORD	$0xad44d7f1	// 2254: ldp q17, q21, [sp, #0x90]
	WORD	$0x3dc07be3	// 2258: ldr q3, [sp, #0x1e0]
	WORD	$0x6ea386a3	// 225c: sub.4s v3, v21, v3
	WORD	$0x4f322463	// 2260: srshr.4s v3, v3, #0xe
	WORD	$0x6eb18677	// 2264: sub.4s v23, v19, v17
	WORD	$0x4f3226f7	// 2268: srshr.4s v23, v23, #0xe
	WORD	$0x3dc033f0	// 226c: ldr q16, [sp, #0xc0]
	WORD	$0x3dc023e7	// 2270: ldr q7, [sp, #0x80]
	WORD	$0x6ea7860d	// 2274: sub.4s v13, v16, v7
	WORD	$0x4f3225ad	// 2278: srshr.4s v13, v13, #0xe
	WORD	$0x4ea19c45	// 227c: mul.4s v5, v2, v1
	WORD	$0x4ea096e5	// 2280: mla.4s v5, v23, v0
	WORD	$0x4ea19c64	// 2284: mul.4s v4, v3, v1
	WORD	$0x4ea095a4	// 2288: mla.4s v4, v13, v0
	WORD	$0x4eb29c42	// 228c: mul.4s v2, v2, v18
	WORD	$0x4ea196e2	// 2290: mla.4s v2, v23, v1
	WORD	$0x4eb29c63	// 2294: mul.4s v3, v3, v18
	WORD	$0x4ea195a3	// 2298: mla.4s v3, v13, v1
	WORD	$0x3dc04fe0	// 229c: ldr q0, [sp, #0x130]
	WORD	$0x3dc013e1	// 22a0: ldr q1, [sp, #0x40]
	WORD	$0x4f323420	// 22a4: srsra.4s v0, v1, #0xe
	WORD	$0x3d8013e0	// 22a8: str q0, [sp, #0x40]
	WORD	$0x4ebe1fd7	// 22ac: mov.16b v23, v30
	WORD	$0x3dc00be0	// 22b0: ldr q0, [sp, #0x20]
	WORD	$0x4f323417	// 22b4: srsra.4s v23, v0, #0xe
	WORD	$0x3dc053e0	// 22b8: ldr q0, [sp, #0x140]
	WORD	$0x3dc017e1	// 22bc: ldr q1, [sp, #0x50]
	WORD	$0x4f323420	// 22c0: srsra.4s v0, v1, #0xe
	WORD	$0x3d800be0	// 22c4: str q0, [sp, #0x20]
	WORD	$0x3dc04be0	// 22c8: ldr q0, [sp, #0x120]
	WORD	$0x3dc007e1	// 22cc: ldr q1, [sp, #0x10]
	WORD	$0x4f323420	// 22d0: srsra.4s v0, v1, #0xe
	WORD	$0x3d8017e0	// 22d4: str q0, [sp, #0x50]
	WORD	$0x3dc077e1	// 22d8: ldr q1, [sp, #0x1d0]
	WORD	$0x3dc01fe0	// 22dc: ldr q0, [sp, #0x70]
	WORD	$0x4f323401	// 22e0: srsra.4s v1, v0, #0xe
	WORD	$0x3dc06ffe	// 22e4: ldr q30, [sp, #0x1b0]
	WORD	$0x3dc00fe0	// 22e8: ldr q0, [sp, #0x30]
	WORD	$0x4f32341e	// 22ec: srsra.4s v30, v0, #0xe
	WORD	$0xad5003f2	// 22f0: ldp q18, q0, [sp, #0x200]
	WORD	$0x3dc067ed	// 22f4: ldr q13, [sp, #0x190]
	WORD	$0x4f3235a0	// 22f8: srsra.4s v0, v13, #0xe
	WORD	$0x3dc01bed	// 22fc: ldr q13, [sp, #0x60]
	WORD	$0x4f3235b2	// 2300: srsra.4s v18, v13, #0xe
	WORD	$0xad1003f2	// 2304: stp q18, q0, [sp, #0x200]
	WORD	$0x3dc07fed	// 2308: ldr q13, [sp, #0x1f0]
	WORD	$0x4ead856d	// 230c: add.4s v13, v11, v13
	WORD	$0x4ebf87bd	// 2310: add.4s v29, v29, v31
	WORD	$0x4eae859f	// 2314: add.4s v31, v12, v14
	WORD	$0x4eaf879c	// 2318: add.4s v28, v28, v15
	WORD	$0xad09f3ff	// 231c: stp q31, q28, [sp, #0x130]
	WORD	$0x3dc003f2	// 2320: ldr q18, [sp]
	WORD	$0x4eb8864b	// 2324: add.4s v11, v18, v24
	WORD	$0x3dc07bf8	// 2328: ldr q24, [sp, #0x1e0]
	WORD	$0x4eb58712	// 232c: add.4s v18, v24, v21
	WORD	$0x4eb38631	// 2330: add.4s v17, v17, v19
	WORD	$0xad08cbf1	// 2334: stp q17, q18, [sp, #0x110]
	WORD	$0x4eb084ee	// 2338: add.4s v14, v7, v16
	WORD	$0x3dc06bf0	// 233c: ldr q16, [sp, #0x1a0]
	WORD	$0x4eb08767	// 2340: add.4s v7, v27, v16
	WORD	$0x3d806fe7	// 2344: str q7, [sp, #0x1b0]
	WORD	$0x6ebb8611	// 2348: sub.4s v17, v16, v27
	WORD	$0x4eaa86c7	// 234c: add.4s v7, v22, v10
	WORD	$0x3d806be7	// 2350: str q7, [sp, #0x1a0]
	WORD	$0x6eb68555	// 2354: sub.4s v21, v10, v22
	WORD	$0x3dc073f0	// 2358: ldr q16, [sp, #0x1c0]
	WORD	$0x4eb08507	// 235c: add.4s v7, v8, v16
	WORD	$0x3d807fe7	// 2360: str q7, [sp, #0x1f0]
	WORD	$0x6ea88607	// 2364: sub.4s v7, v16, v8
	WORD	$0x3dc057f3	// 2368: ldr q19, [sp, #0x150]
	WORD	$0x4eb38532	// 236c: add.4s v18, v9, v19
	WORD	$0x6ea98670	// 2370: sub.4s v16, v19, v9
	WORD	$0x4eb98453	// 2374: add.4s v19, v2, v25
	WORD	$0x6ea28736	// 2378: sub.4s v22, v25, v2
	WORD	$0x4ea68462	// 237c: add.4s v2, v3, v6
	WORD	$0xad0c4fe2	// 2380: stp q2, q19, [sp, #0x180]
	WORD	$0x6ea384d3	// 2384: sub.4s v19, v6, v3
	WORD	$0x4eba84a2	// 2388: add.4s v2, v5, v26
	WORD	$0xad0ecbe2	// 238c: stp q2, q18, [sp, #0x1d0]
	WORD	$0x6ea58745	// 2390: sub.4s v5, v26, v5
	WORD	$0x4eb48482	// 2394: add.4s v2, v4, v20
	WORD	$0x3d8073e2	// 2398: str q2, [sp, #0x1c0]
	WORD	$0x6ea48682	// 239c: sub.4s v2, v20, v4
	WORD	$0x1285a808	// 23a0: mov w8, #-0x2d41            ; =-11585
	WORD	$0x3dc00bf9	// 23a4: ldr q25, [sp, #0x20]
	WORD	$0x6eb98403	// 23a8: sub.4s v3, v0, v25
	WORD	$0x4e040d0f	// 23ac: dup.4s v15, w8
	WORD	$0x4eaf9c63	// 23b0: mul.4s v3, v3, v15
	WORD	$0x4ea11c20	// 23b4: mov.16b v0, v1
	WORD	$0xad4263f2	// 23b8: ldp q18, q24, [sp, #0x40]
	WORD	$0x6eb28424	// 23bc: sub.4s v4, v1, v18
	WORD	$0x4eaf9c84	// 23c0: mul.4s v4, v4, v15
	WORD	$0x4ea48469	// 23c4: add.4s v9, v3, v4
	WORD	$0x6ea48461	// 23c8: sub.4s v1, v3, v4
	WORD	$0x3d805fe1	// 23cc: str q1, [sp, #0x170]
	WORD	$0x3dc083ec	// 23d0: ldr q12, [sp, #0x200]
	WORD	$0x6eb88583	// 23d4: sub.4s v3, v12, v24
	WORD	$0x4eaf9c63	// 23d8: mul.4s v3, v3, v15
	WORD	$0x6eb787c4	// 23dc: sub.4s v4, v30, v23
	WORD	$0x4ebe1fdc	// 23e0: mov.16b v28, v30
	WORD	$0x4eaf9c84	// 23e4: mul.4s v4, v4, v15
	WORD	$0x4ea48466	// 23e8: add.4s v6, v3, v4
	WORD	$0x6ea4847f	// 23ec: sub.4s v31, v3, v4
	WORD	$0x4f322623	// 23f0: srshr.4s v3, v17, #0xe
	WORD	$0x4f3224e4	// 23f4: srshr.4s v4, v7, #0xe
	WORD	$0x5285a828	// 23f8: mov w8, #0x2d41             ; =11585
	WORD	$0x4e040d0a	// 23fc: dup.4s v10, w8
	WORD	$0x4eaa9c84	// 2400: mul.4s v4, v4, v10
	WORD	$0x4eaa9c67	// 2404: mul.4s v7, v3, v10
	WORD	$0x4ea78483	// 2408: add.4s v3, v4, v7
	WORD	$0x6ea78481	// 240c: sub.4s v1, v4, v7
	WORD	$0x3d805be1	// 2410: str q1, [sp, #0x160]
	WORD	$0x4f3226a4	// 2414: srshr.4s v4, v21, #0xe
	WORD	$0x4f322607	// 2418: srshr.4s v7, v16, #0xe
	WORD	$0x4eaa9cf1	// 241c: mul.4s v17, v7, v10
	WORD	$0x4eaa9c84	// 2420: mul.4s v4, v4, v10
	WORD	$0x4ea48627	// 2424: add.4s v7, v17, v4
	WORD	$0x6ea48628	// 2428: sub.4s v8, v17, v4
	WORD	$0x4f322575	// 242c: srshr.4s v21, v11, #0xe
	WORD	$0xad4893e1	// 2430: ldp q1, q4, [sp, #0x110]
	WORD	$0x4f32249e	// 2434: srshr.4s v30, v4, #0xe
	WORD	$0x4f322424	// 2438: srshr.4s v4, v1, #0xe
	WORD	$0x4f3225d1	// 243c: srshr.4s v17, v14, #0xe
	WORD	$0x4f3225b4	// 2440: srshr.4s v20, v13, #0xe
	WORD	$0x6eb58694	// 2444: sub.4s v20, v20, v21
	WORD	$0x4f3227ab	// 2448: srshr.4s v11, v29, #0xe
	WORD	$0x6ebe856b	// 244c: sub.4s v11, v11, v30
	WORD	$0xad49efe1	// 2450: ldp q1, q27, [sp, #0x130]
	WORD	$0x4f32242e	// 2454: srshr.4s v14, v1, #0xe
	WORD	$0x6ea485ce	// 2458: sub.4s v14, v14, v4
	WORD	$0x4f322770	// 245c: srshr.4s v16, v27, #0xe
	WORD	$0x6eb18610	// 2460: sub.4s v16, v16, v17
	WORD	$0x4eaa9dce	// 2464: mul.4s v14, v14, v10
	WORD	$0x4eaa9e10	// 2468: mul.4s v16, v16, v10
	WORD	$0x4eaa9e94	// 246c: mul.4s v20, v20, v10
	WORD	$0x4eaa9d6a	// 2470: mul.4s v10, v11, v10
	WORD	$0x4eb485cb	// 2474: add.4s v11, v14, v20
	WORD	$0x6eb485d4	// 2478: sub.4s v20, v14, v20
	WORD	$0x4eaa860e	// 247c: add.4s v14, v16, v10
	WORD	$0x6eaa860a	// 2480: sub.4s v10, v16, v10
	WORD	$0x4f3226d0	// 2484: srshr.4s v16, v22, #0xe
	WORD	$0x4f322676	// 2488: srshr.4s v22, v19, #0xe
	WORD	$0x4f3224a5	// 248c: srshr.4s v5, v5, #0xe
	WORD	$0x4f322442	// 2490: srshr.4s v2, v2, #0xe
	WORD	$0x4eaf9ca5	// 2494: mul.4s v5, v5, v15
	WORD	$0x4eaf9c42	// 2498: mul.4s v2, v2, v15
	WORD	$0x4eaf9e10	// 249c: mul.4s v16, v16, v15
	WORD	$0x4eaf9ed6	// 24a0: mul.4s v22, v22, v15
	WORD	$0x4eb084ba	// 24a4: add.4s v26, v5, v16
	WORD	$0x6eb084a5	// 24a8: sub.4s v5, v5, v16
	WORD	$0x4eb68450	// 24ac: add.4s v16, v2, v22
	WORD	$0x6eb68442	// 24b0: sub.4s v2, v2, v22
	WORD	$0x4ea08652	// 24b4: add.4s v18, v18, v0
	WORD	$0x4ebc86f6	// 24b8: add.4s v22, v23, v28
	WORD	$0x4e561a52	// 24bc: uzp1.8h v18, v18, v22
	WORD	$0x4f3235b5	// 24c0: srsra.4s v21, v13, #0xe
	WORD	$0x4f3237be	// 24c4: srsra.4s v30, v29, #0xe
	WORD	$0x4e5e1ab5	// 24c8: uzp1.8h v21, v21, v30
	WORD	$0x6e60bab5	// 24cc: neg.8h v21, v21
	WORD	$0xad005412	// 24d0: stp q18, q21, [x0]
	WORD	$0x3dc067e0	// 24d4: ldr q0, [sp, #0x190]
	WORD	$0x4f322412	// 24d8: srshr.4s v18, v0, #0xe
	WORD	$0x3dc063e0	// 24dc: ldr q0, [sp, #0x180]
	WORD	$0x4f322415	// 24e0: srshr.4s v21, v0, #0xe
	WORD	$0x4e551a52	// 24e4: uzp1.8h v18, v18, v21
	WORD	$0x3dc06fe0	// 24e8: ldr q0, [sp, #0x1b0]
	WORD	$0x4f322415	// 24ec: srshr.4s v21, v0, #0xe
	WORD	$0x3dc06be0	// 24f0: ldr q0, [sp, #0x1a0]
	WORD	$0x4f322416	// 24f4: srshr.4s v22, v0, #0xe
	WORD	$0x4e561ab5	// 24f8: uzp1.8h v21, v21, v22
	WORD	$0x6e60bab5	// 24fc: neg.8h v21, v21
	WORD	$0xad015412	// 2500: stp q18, q21, [x0, #0x20]
	WORD	$0x0f128c63	// 2504: rshrn.4h v3, v3, #0xe
	WORD	$0x4f128ce3	// 2508: rshrn2.8h v3, v7, #0xe
	WORD	$0x0f128f47	// 250c: rshrn.4h v7, v26, #0xe
	WORD	$0x4f128e07	// 2510: rshrn2.8h v7, v16, #0xe
	WORD	$0xad021c03	// 2514: stp q3, q7, [x0, #0x40]
	WORD	$0x0f128d63	// 2518: rshrn.4h v3, v11, #0xe
	WORD	$0x4f128dc3	// 251c: rshrn2.8h v3, v14, #0xe
	WORD	$0x0f128d27	// 2520: rshrn.4h v7, v9, #0xe
	WORD	$0x4f128cc7	// 2524: rshrn2.8h v7, v6, #0xe
	WORD	$0xad031c03	// 2528: stp q3, q7, [x0, #0x60]
	WORD	$0xad4b1fe0	// 252c: ldp q0, q7, [sp, #0x160]
	WORD	$0x0f128ce3	// 2530: rshrn.4h v3, v7, #0xe
	WORD	$0x4f128fe3	// 2534: rshrn2.8h v3, v31, #0xe
	WORD	$0x0f128e86	// 2538: rshrn.4h v6, v20, #0xe
	WORD	$0x4f128d46	// 253c: rshrn2.8h v6, v10, #0xe
	WORD	$0xad041803	// 2540: stp q3, q6, [x0, #0x80]
	WORD	$0x0f128ca3	// 2544: rshrn.4h v3, v5, #0xe
	WORD	$0x4f128c43	// 2548: rshrn2.8h v3, v2, #0xe
	WORD	$0x0f128c02	// 254c: rshrn.4h v2, v0, #0xe
	WORD	$0x4f128d02	// 2550: rshrn2.8h v2, v8, #0xe
	WORD	$0xad050803	// 2554: stp q3, q2, [x0, #0xa0]
	WORD	$0xad4f0fe0	// 2558: ldp q0, q3, [sp, #0x1e0]
	WORD	$0x4f322462	// 255c: srshr.4s v2, v3, #0xe
	WORD	$0x4f322403	// 2560: srshr.4s v3, v0, #0xe
	WORD	$0x4e431842	// 2564: uzp1.8h v2, v2, v3
	WORD	$0xad4e17e0	// 2568: ldp q0, q5, [sp, #0x1c0]
	WORD	$0x4f3224a3	// 256c: srshr.4s v3, v5, #0xe
	WORD	$0x4f322405	// 2570: srshr.4s v5, v0, #0xe
	WORD	$0x4e451863	// 2574: uzp1.8h v3, v3, v5
	WORD	$0x6e60b863	// 2578: neg.8h v3, v3
	WORD	$0xad060c02	// 257c: stp q2, q3, [x0, #0xc0]
	WORD	$0x4f323424	// 2580: srsra.4s v4, v1, #0xe
	WORD	$0x4f323771	// 2584: srsra.4s v17, v27, #0xe
	WORD	$0x4e511882	// 2588: uzp1.8h v2, v4, v17
	WORD	$0x3dc087e0	// 258c: ldr q0, [sp, #0x210]
	WORD	$0x4ea08721	// 2590: add.4s v1, v25, v0
	WORD	$0x4eac8700	// 2594: add.4s v0, v24, v12
	WORD	$0x4e401820	// 2598: uzp1.8h v0, v1, v0
	WORD	$0x6e60b800	// 259c: neg.8h v0, v0
	WORD	$0xad070002	// 25a0: stp q2, q0, [x0, #0xe0]
	WORD	$0x910883ff	// 25a4: add sp, sp, #0x220
	WORD	$0xa9446ffc	// 25a8: ldp x28, x27, [sp, #0x40]
	WORD	$0x6d4323e9	// 25ac: ldp d9, d8, [sp, #0x30]
	WORD	$0x6d422beb	// 25b0: ldp d11, d10, [sp, #0x20]
	WORD	$0x6d4133ed	// 25b4: ldp d13, d12, [sp, #0x10]
	WORD	$0x6cc53bef	// 25b8: ldp d15, d14, [sp], #0x50
	WORD	$0xd65f03c0	// 25bc: ret
	WORD	$0x6dbc3bef	// 25c0: stp d15, d14, [sp, #-0x40]!
	WORD	$0x6d0133ed	// 25c4: stp d13, d12, [sp, #0x10]
	WORD	$0x6d022beb	// 25c8: stp d11, d10, [sp, #0x20]
	WORD	$0x6d0323e9	// 25cc: stp d9, d8, [sp, #0x30]
	WORD	$0xad401c05	// 25d0: ldp q5, q7, [x0]
	WORD	$0xad471810	// 25d4: ldp q16, q6, [x0, #0xe0]
	WORD	$0x4e6584c0	// 25d8: add.8h v0, v6, v5
	WORD	$0x4e678601	// 25dc: add.8h v1, v16, v7
	WORD	$0xad415012	// 25e0: ldp q18, q20, [x0, #0x20]
	WORD	$0xad464c15	// 25e4: ldp q21, q19, [x0, #0xc0]
	WORD	$0x4e728662	// 25e8: add.8h v2, v19, v18
	WORD	$0x4e7486a3	// 25ec: add.8h v3, v21, v20
	WORD	$0xad427016	// 25f0: ldp q22, q28, [x0, #0x40]
	WORD	$0xad456c1d	// 25f4: ldp q29, q27, [x0, #0xa0]
	WORD	$0x4e768764	// 25f8: add.8h v4, v27, v22
	WORD	$0x4e7c87b1	// 25fc: add.8h v17, v29, v28
	WORD	$0xad43201e	// 2600: ldp q30, q8, [x0, #0x60]
	WORD	$0xad447c09	// 2604: ldp q9, q31, [x0, #0x80]
	WORD	$0x4e7e87f7	// 2608: add.8h v23, v31, v30
	WORD	$0x4e688538	// 260c: add.8h v24, v9, v8
	WORD	$0x4e608719	// 2610: add.8h v25, v24, v0
	WORD	$0x4e6186fa	// 2614: add.8h v26, v23, v1
	WORD	$0x4e62862a	// 2618: add.8h v10, v17, v2
	WORD	$0x4e63848b	// 261c: add.8h v11, v4, v3
	WORD	$0x6e64846c	// 2620: sub.8h v12, v3, v4
	WORD	$0x6e71844d	// 2624: sub.8h v13, v2, v17
	WORD	$0x6e778437	// 2628: sub.8h v23, v1, v23
	WORD	$0x6e788418	// 262c: sub.8h v24, v0, v24
	WORD	$0x4e6b8720	// 2630: add.8h v0, v25, v11
	WORD	$0x4e6a8741	// 2634: add.8h v1, v26, v10
	WORD	$0x6e6a8743	// 2638: sub.8h v3, v26, v10
	WORD	$0x6e6b8724	// 263c: sub.8h v4, v25, v11
	WORD	$0x0e610002	// 2640: saddl.4s v2, v0, v1
	WORD	$0x52ab5048	// 2644: mov w8, #0x5a820000         ; =1518469120
	WORD	$0x4e040d19	// 2648: dup.4s v25, w8
	WORD	$0x6eb9b442	// 264c: sqrdmulh.4s v2, v2, v25
	WORD	$0x4e610011	// 2650: saddl2.4s v17, v0, v1
	WORD	$0x6eb9b631	// 2654: sqrdmulh.4s v17, v17, v25
	WORD	$0x0e61201a	// 2658: ssubl.4s v26, v0, v1
	WORD	$0x6eb9b75a	// 265c: sqrdmulh.4s v26, v26, v25
	WORD	$0x4e612000	// 2660: ssubl2.4s v0, v0, v1
	WORD	$0x6eb9b400	// 2664: sqrdmulh.4s v0, v0, v25
	WORD	$0x4e511842	// 2668: uzp1.8h v2, v2, v17
	WORD	$0x4e401b41	// 266c: uzp1.8h v1, v26, v0
	WORD	$0x52876428	// 2670: mov w8, #0x3b21             ; =15137
	WORD	$0x4e020d00	// 2674: dup.8h v0, w8
	WORD	$0x52830fc8	// 2678: mov w8, #0x187e             ; =6270
	WORD	$0x4e020d11	// 267c: dup.8h v17, w8
	WORD	$0x0e71c09a	// 2680: smull.4s v26, v4, v17
	WORD	$0x4e71c08a	// 2684: smull2.4s v10, v4, v17
	WORD	$0x0e71c06b	// 2688: smull.4s v11, v3, v17
	WORD	$0x0e60808b	// 268c: smlal.4s v11, v4, v0
	WORD	$0x4e71c06e	// 2690: smull2.4s v14, v3, v17
	WORD	$0x4e60808e	// 2694: smlal2.4s v14, v4, v0
	WORD	$0x0e60a07a	// 2698: smlsl.4s v26, v3, v0
	WORD	$0x4e60a06a	// 269c: smlsl2.4s v10, v3, v0
	WORD	$0x0f129d64	// 26a0: sqrshrn.4h v4, v11, #0xe
	WORD	$0x0f129f43	// 26a4: sqrshrn.4h v3, v26, #0xe
	WORD	$0x4f129dc4	// 26a8: sqrshrn2.8h v4, v14, #0xe
	WORD	$0x4f129d43	// 26ac: sqrshrn2.8h v3, v10, #0xe
	WORD	$0x0e6d02fa	// 26b0: saddl.4s v26, v23, v13
	WORD	$0x6eb9b75a	// 26b4: sqrdmulh.4s v26, v26, v25
	WORD	$0x4e6d02ea	// 26b8: saddl2.4s v10, v23, v13
	WORD	$0x6eb9b54a	// 26bc: sqrdmulh.4s v10, v10, v25
	WORD	$0x0e6d22eb	// 26c0: ssubl.4s v11, v23, v13
	WORD	$0x6eb9b56b	// 26c4: sqrdmulh.4s v11, v11, v25
	WORD	$0x4e6d22f7	// 26c8: ssubl2.4s v23, v23, v13
	WORD	$0x6eb9b6f7	// 26cc: sqrdmulh.4s v23, v23, v25
	WORD	$0x4e4a1b59	// 26d0: uzp1.8h v25, v26, v10
	WORD	$0x4e571977	// 26d4: uzp1.8h v23, v11, v23
	WORD	$0x4e77859a	// 26d8: add.8h v26, v12, v23
	WORD	$0x6e778597	// 26dc: sub.8h v23, v12, v23
	WORD	$0x6e79870a	// 26e0: sub.8h v10, v24, v25
	WORD	$0x4e798718	// 26e4: add.8h v24, v24, v25
	WORD	$0x5287d8a8	// 26e8: mov w8, #0x3ec5             ; =16069
	WORD	$0x4e020d19	// 26ec: dup.8h v25, w8
	WORD	$0x52818f88	// 26f0: mov w8, #0xc7c              ; =3196
	WORD	$0x4e020d0b	// 26f4: dup.8h v11, w8
	WORD	$0x0e6bc30c	// 26f8: smull.4s v12, v24, v11
	WORD	$0x4e6bc30d	// 26fc: smull2.4s v13, v24, v11
	WORD	$0x0e6bc34e	// 2700: smull.4s v14, v26, v11
	WORD	$0x0e79830e	// 2704: smlal.4s v14, v24, v25
	WORD	$0x4e6bc34b	// 2708: smull2.4s v11, v26, v11
	WORD	$0x4e79830b	// 270c: smlal2.4s v11, v24, v25
	WORD	$0x0e79a34c	// 2710: smlsl.4s v12, v26, v25
	WORD	$0x4e79a34d	// 2714: smlsl2.4s v13, v26, v25
	WORD	$0x0f129dd8	// 2718: sqrshrn.4h v24, v14, #0xe
	WORD	$0x0f129d99	// 271c: sqrshrn.4h v25, v12, #0xe
	WORD	$0x4f129d78	// 2720: sqrshrn2.8h v24, v11, #0xe
	WORD	$0x4f129db9	// 2724: sqrshrn2.8h v25, v13, #0xe
	WORD	$0x528471c8	// 2728: mov w8, #0x238e             ; =9102
	WORD	$0x4e020d1a	// 272c: dup.8h v26, w8
	WORD	$0x5286a6e8	// 2730: mov w8, #0x3537             ; =13623
	WORD	$0x4e020d0b	// 2734: dup.8h v11, w8
	WORD	$0x0e6bc14c	// 2738: smull.4s v12, v10, v11
	WORD	$0x4e6bc14d	// 273c: smull2.4s v13, v10, v11
	WORD	$0x0e6bc2ee	// 2740: smull.4s v14, v23, v11
	WORD	$0x0e7a814e	// 2744: smlal.4s v14, v10, v26
	WORD	$0x4e6bc2eb	// 2748: smull2.4s v11, v23, v11
	WORD	$0x4e7a814b	// 274c: smlal2.4s v11, v10, v26
	WORD	$0x0e7aa2ec	// 2750: smlsl.4s v12, v23, v26
	WORD	$0x4e7aa2ed	// 2754: smlsl2.4s v13, v23, v26
	WORD	$0x0f129dd7	// 2758: sqrshrn.4h v23, v14, #0xe
	WORD	$0x0f129d9a	// 275c: sqrshrn.4h v26, v12, #0xe
	WORD	$0x4f129d77	// 2760: sqrshrn2.8h v23, v11, #0xe
	WORD	$0x4f129dba	// 2764: sqrshrn2.8h v26, v13, #0xe
	WORD	$0x4e58284a	// 2768: trn1.8h v10, v2, v24
	WORD	$0x4e586842	// 276c: trn2.8h v2, v2, v24
	WORD	$0x4e5a2898	// 2770: trn1.8h v24, v4, v26
	WORD	$0x4e5a6884	// 2774: trn2.8h v4, v4, v26
	WORD	$0x4e57283a	// 2778: trn1.8h v26, v1, v23
	WORD	$0x4e576821	// 277c: trn2.8h v1, v1, v23
	WORD	$0x4e592877	// 2780: trn1.8h v23, v3, v25
	WORD	$0x4e596863	// 2784: trn2.8h v3, v3, v25
	WORD	$0x4e982959	// 2788: trn1.4s v25, v10, v24
	WORD	$0x4e986958	// 278c: trn2.4s v24, v10, v24
	WORD	$0x4e84284a	// 2790: trn1.4s v10, v2, v4
	WORD	$0x4e846842	// 2794: trn2.4s v2, v2, v4
	WORD	$0x4e972b44	// 2798: trn1.4s v4, v26, v23
	WORD	$0x4e976b57	// 279c: trn2.4s v23, v26, v23
	WORD	$0x4e83283a	// 27a0: trn1.4s v26, v1, v3
	WORD	$0x4ec47b2b	// 27a4: zip2.2d v11, v25, v4
	WORD	$0x6e180499	// 27a8: mov.d v25[1], v4[0]
	WORD	$0x4e836821	// 27ac: trn2.4s v1, v1, v3
	WORD	$0x4eda7943	// 27b0: zip2.2d v3, v10, v26
	WORD	$0x6e18074a	// 27b4: mov.d v10[1], v26[0]
	WORD	$0x4ed77b04	// 27b8: zip2.2d v4, v24, v23
	WORD	$0x6e1806f8	// 27bc: mov.d v24[1], v23[0]
	WORD	$0x4ea21c57	// 27c0: mov.16b v23, v2
	WORD	$0x6e180437	// 27c4: mov.d v23[1], v1[0]
	WORD	$0x4ec17841	// 27c8: zip2.2d v1, v2, v1
	WORD	$0x4e4a2b22	// 27cc: trn1.8h v2, v25, v10
	WORD	$0x4e4a6b39	// 27d0: trn2.8h v25, v25, v10
	WORD	$0x4e572b1a	// 27d4: trn1.8h v26, v24, v23
	WORD	$0x4e576b17	// 27d8: trn2.8h v23, v24, v23
	WORD	$0x4e432978	// 27dc: trn1.8h v24, v11, v3
	WORD	$0x4e436963	// 27e0: trn2.8h v3, v11, v3
	WORD	$0x4e41288a	// 27e4: trn1.8h v10, v4, v1
	WORD	$0x4e416884	// 27e8: trn2.8h v4, v4, v1
	WORD	$0x4e9a284b	// 27ec: trn1.4s v11, v2, v26
	WORD	$0x4e9a685a	// 27f0: trn2.4s v26, v2, v26
	WORD	$0x4e972b2c	// 27f4: trn1.4s v12, v25, v23
	WORD	$0x4e976b2d	// 27f8: trn2.4s v13, v25, v23
	WORD	$0x4e8a2b02	// 27fc: trn1.4s v2, v24, v10
	WORD	$0x4e8a6b0a	// 2800: trn2.4s v10, v24, v10
	WORD	$0x4e842879	// 2804: trn1.4s v25, v3, v4
	WORD	$0x4ec27961	// 2808: zip2.2d v1, v11, v2
	WORD	$0x4eab1d77	// 280c: mov.16b v23, v11
	WORD	$0x6e180457	// 2810: mov.d v23[1], v2[0]
	WORD	$0x4ed97982	// 2814: zip2.2d v2, v12, v25
	WORD	$0x4eac1d98	// 2818: mov.16b v24, v12
	WORD	$0x6e180738	// 281c: mov.d v24[1], v25[0]
	WORD	$0x4e84686b	// 2820: trn2.4s v11, v3, v4
	WORD	$0x4eca7b44	// 2824: zip2.2d v4, v26, v10
	WORD	$0x4eba1f59	// 2828: mov.16b v25, v26
	WORD	$0x6e180559	// 282c: mov.d v25[1], v10[0]
	WORD	$0x4ecb79a3	// 2830: zip2.2d v3, v13, v11
	WORD	$0x4ead1dba	// 2834: mov.16b v26, v13
	WORD	$0x6e18057a	// 2838: mov.d v26[1], v11[0]
	WORD	$0x6e698508	// 283c: sub.8h v8, v8, v9
	WORD	$0x6e7f87de	// 2840: sub.8h v30, v30, v31
	WORD	$0x6e7d879c	// 2844: sub.8h v28, v28, v29
	WORD	$0x6e7b86d6	// 2848: sub.8h v22, v22, v27
	WORD	$0x6e758694	// 284c: sub.8h v20, v20, v21
	WORD	$0x6e738652	// 2850: sub.8h v18, v18, v19
	WORD	$0x6e7084e7	// 2854: sub.8h v7, v7, v16
	WORD	$0x6e6684a5	// 2858: sub.8h v5, v5, v6
	WORD	$0x6e7c8646	// 285c: sub.8h v6, v18, v28
	WORD	$0x6e768690	// 2860: sub.8h v16, v20, v22
	WORD	$0x4e7486d3	// 2864: add.8h v19, v22, v20
	WORD	$0x4e728792	// 2868: add.8h v18, v28, v18
	WORD	$0x5285a828	// 286c: mov w8, #0x2d41             ; =11585
	WORD	$0x4e020d14	// 2870: dup.8h v20, w8
	WORD	$0x0e74c0d5	// 2874: smull.4s v21, v6, v20
	WORD	$0x4e74c0c6	// 2878: smull2.4s v6, v6, v20
	WORD	$0x0e74c216	// 287c: smull.4s v22, v16, v20
	WORD	$0x4e74c210	// 2880: smull2.4s v16, v16, v20
	WORD	$0x0e74c27b	// 2884: smull.4s v27, v19, v20
	WORD	$0x4e74c273	// 2888: smull2.4s v19, v19, v20
	WORD	$0x0e74c25c	// 288c: smull.4s v28, v18, v20
	WORD	$0x4e74c252	// 2890: smull2.4s v18, v18, v20
	WORD	$0x0f128eb4	// 2894: rshrn.4h v20, v21, #0xe
	WORD	$0x0f128ed5	// 2898: rshrn.4h v21, v22, #0xe
	WORD	$0x0f128f76	// 289c: rshrn.4h v22, v27, #0xe
	WORD	$0x0f128f9b	// 28a0: rshrn.4h v27, v28, #0xe
	WORD	$0x4f128cd4	// 28a4: rshrn2.8h v20, v6, #0xe
	WORD	$0x4f128e15	// 28a8: rshrn2.8h v21, v16, #0xe
	WORD	$0x4f128e76	// 28ac: rshrn2.8h v22, v19, #0xe
	WORD	$0x4f128e5b	// 28b0: rshrn2.8h v27, v18, #0xe
	WORD	$0x4e6886a6	// 28b4: add.8h v6, v21, v8
	WORD	$0x4e7e8690	// 28b8: add.8h v16, v20, v30
	WORD	$0x6e7487d2	// 28bc: sub.8h v18, v30, v20
	WORD	$0x6e758513	// 28c0: sub.8h v19, v8, v21
	WORD	$0x6e7684b4	// 28c4: sub.8h v20, v5, v22
	WORD	$0x6e7b84f5	// 28c8: sub.8h v21, v7, v27
	WORD	$0x4e678767	// 28cc: add.8h v7, v27, v7
	WORD	$0x4e6586d6	// 28d0: add.8h v22, v22, v5
	WORD	$0x52989be8	// 28d4: mov w8, #0xc4df             ; =50399
	WORD	$0x4e020d05	// 28d8: dup.8h v5, w8
	WORD	$0x0e71c21b	// 28dc: smull.4s v27, v16, v17
	WORD	$0x4e71c21c	// 28e0: smull2.4s v28, v16, v17
	WORD	$0x0e71c0fd	// 28e4: smull.4s v29, v7, v17
	WORD	$0x0e65821d	// 28e8: smlal.4s v29, v16, v5
	WORD	$0x4e71c0f1	// 28ec: smull2.4s v17, v7, v17
	WORD	$0x4e658211	// 28f0: smlal2.4s v17, v16, v5
	WORD	$0x0e65a0fb	// 28f4: smlsl.4s v27, v7, v5
	WORD	$0x4e65a0fc	// 28f8: smlsl2.4s v28, v7, v5
	WORD	$0x529cf048	// 28fc: mov w8, #0xe782             ; =59266
	WORD	$0x4e020d05	// 2900: dup.8h v5, w8
	WORD	$0x0e60c2a7	// 2904: smull.4s v7, v21, v0
	WORD	$0x4e60c2b0	// 2908: smull2.4s v16, v21, v0
	WORD	$0x0e60c25e	// 290c: smull.4s v30, v18, v0
	WORD	$0x0e6582be	// 2910: smlal.4s v30, v21, v5
	WORD	$0x4e60c240	// 2914: smull2.4s v0, v18, v0
	WORD	$0x4e6582a0	// 2918: smlal2.4s v0, v21, v5
	WORD	$0x0e65a247	// 291c: smlsl.4s v7, v18, v5
	WORD	$0x4e65a250	// 2920: smlsl2.4s v16, v18, v5
	WORD	$0x0f128fa5	// 2924: rshrn.4h v5, v29, #0xe
	WORD	$0x0f128ce7	// 2928: rshrn.4h v7, v7, #0xe
	WORD	$0x0f128fd5	// 292c: rshrn.4h v21, v30, #0xe
	WORD	$0x0f128f7b	// 2930: rshrn.4h v27, v27, #0xe
	WORD	$0x4f128e25	// 2934: rshrn2.8h v5, v17, #0xe
	WORD	$0x4f128e07	// 2938: rshrn2.8h v7, v16, #0xe
	WORD	$0x4f128c15	// 293c: rshrn2.8h v21, v0, #0xe
	WORD	$0x4f128f9b	// 2940: rshrn2.8h v27, v28, #0xe
	WORD	$0x4e6684a0	// 2944: add.8h v0, v5, v6
	WORD	$0x6e6584dd	// 2948: sub.8h v29, v6, v5
	WORD	$0x4e7384f2	// 294c: add.8h v18, v7, v19
	WORD	$0x6e678667	// 2950: sub.8h v7, v19, v7
	WORD	$0x6e758685	// 2954: sub.8h v5, v20, v21
	WORD	$0x4e7486be	// 2958: add.8h v30, v21, v20
	WORD	$0x6e7b86c8	// 295c: sub.8h v8, v22, v27
	WORD	$0x4e768773	// 2960: add.8h v19, v27, v22
	WORD	$0x5287f628	// 2964: mov w8, #0x3fb1             ; =16305
	WORD	$0x4e020d06	// 2968: dup.8h v6, w8
	WORD	$0x5280c8c8	// 296c: mov w8, #0x646              ; =1606
	WORD	$0x4e020d14	// 2970: dup.8h v20, w8
	WORD	$0x0e74c271	// 2974: smull.4s v17, v19, v20
	WORD	$0x4e74c270	// 2978: smull2.4s v16, v19, v20
	WORD	$0x0e74c01f	// 297c: smull.4s v31, v0, v20
	WORD	$0x0e66827f	// 2980: smlal.4s v31, v19, v6
	WORD	$0x4e74c01c	// 2984: smull2.4s v28, v0, v20
	WORD	$0x4e66827c	// 2988: smlal2.4s v28, v19, v6
	WORD	$0x52851348	// 298c: mov w8, #0x289a             ; =10394
	WORD	$0x4e020d1b	// 2990: dup.8h v27, w8
	WORD	$0x52862f28	// 2994: mov w8, #0x3179             ; =12665
	WORD	$0x4e020d14	// 2998: dup.8h v20, w8
	WORD	$0x0e74c116	// 299c: smull.4s v22, v8, v20
	WORD	$0x4e74c115	// 29a0: smull2.4s v21, v8, v20
	WORD	$0x0e74c3b3	// 29a4: smull.4s v19, v29, v20
	WORD	$0x0e7b8113	// 29a8: smlal.4s v19, v8, v27
	WORD	$0x4e74c3b4	// 29ac: smull2.4s v20, v29, v20
	WORD	$0x4e7b8114	// 29b0: smlal2.4s v20, v8, v27
	WORD	$0x0e7ba3b6	// 29b4: smlsl.4s v22, v29, v27
	WORD	$0x52870e28	// 29b8: mov w8, #0x3871             ; =14449
	WORD	$0x4e020d08	// 29bc: dup.8h v8, w8
	WORD	$0x4e7ba3b5	// 29c0: smlsl2.4s v21, v29, v27
	WORD	$0x5283c568	// 29c4: mov w8, #0x1e2b             ; =7723
	WORD	$0x4e020d09	// 29c8: dup.8h v9, w8
	WORD	$0x0e69c3dd	// 29cc: smull.4s v29, v30, v9
	WORD	$0x4e69c3db	// 29d0: smull2.4s v27, v30, v9
	WORD	$0x0e69c24a	// 29d4: smull.4s v10, v18, v9
	WORD	$0x0e6883ca	// 29d8: smlal.4s v10, v30, v8
	WORD	$0x4e69c249	// 29dc: smull2.4s v9, v18, v9
	WORD	$0x4e6883c9	// 29e0: smlal2.4s v9, v30, v8
	WORD	$0x0e68a25d	// 29e4: smlsl.4s v29, v18, v8
	WORD	$0x52825288	// 29e8: mov w8, #0x1294             ; =4756
	WORD	$0x4e020d1e	// 29ec: dup.8h v30, w8
	WORD	$0x5287a7e8	// 29f0: mov w8, #0x3d3f             ; =15679
	WORD	$0x4e020d0b	// 29f4: dup.8h v11, w8
	WORD	$0x0e6bc0ac	// 29f8: smull.4s v12, v5, v11
	WORD	$0x4e6bc0ad	// 29fc: smull2.4s v13, v5, v11
	WORD	$0x0e7ea0ec	// 2a00: smlsl.4s v12, v7, v30
	WORD	$0x0f128fff	// 2a04: rshrn.4h v31, v31, #0xe
	WORD	$0x4f128f9f	// 2a08: rshrn2.8h v31, v28, #0xe
	WORD	$0xad007c17	// 2a0c: stp q23, q31, [x0]
	WORD	$0x4e7ea0ed	// 2a10: smlsl2.4s v13, v7, v30
	WORD	$0x0f128d97	// 2a14: rshrn.4h v23, v12, #0xe
	WORD	$0x4f128db7	// 2a18: rshrn2.8h v23, v13, #0xe
	WORD	$0xad015c18	// 2a1c: stp q24, q23, [x0, #0x20]
	WORD	$0x4e68a25b	// 2a20: smlsl2.4s v27, v18, v8
	WORD	$0x0f128d52	// 2a24: rshrn.4h v18, v10, #0xe
	WORD	$0x4f128d32	// 2a28: rshrn2.8h v18, v9, #0xe
	WORD	$0xad024819	// 2a2c: stp q25, q18, [x0, #0x40]
	WORD	$0x0e6bc0f2	// 2a30: smull.4s v18, v7, v11
	WORD	$0x0f128ed6	// 2a34: rshrn.4h v22, v22, #0xe
	WORD	$0x4f128eb6	// 2a38: rshrn2.8h v22, v21, #0xe
	WORD	$0xad03581a	// 2a3c: stp q26, q22, [x0, #0x60]
	WORD	$0x0e7e80b2	// 2a40: smlal.4s v18, v5, v30
	WORD	$0x0f128e73	// 2a44: rshrn.4h v19, v19, #0xe
	WORD	$0x4f128e93	// 2a48: rshrn2.8h v19, v20, #0xe
	WORD	$0xad044c01	// 2a4c: stp q1, q19, [x0, #0x80]
	WORD	$0x4e6bc0e1	// 2a50: smull2.4s v1, v7, v11
	WORD	$0x0f128fa7	// 2a54: rshrn.4h v7, v29, #0xe
	WORD	$0x4f128f67	// 2a58: rshrn2.8h v7, v27, #0xe
	WORD	$0xad051c02	// 2a5c: stp q2, q7, [x0, #0xa0]
	WORD	$0x4e7e80a1	// 2a60: smlal2.4s v1, v5, v30
	WORD	$0x0f128e42	// 2a64: rshrn.4h v2, v18, #0xe
	WORD	$0x4f128c22	// 2a68: rshrn2.8h v2, v1, #0xe
	WORD	$0xad060804	// 2a6c: stp q4, q2, [x0, #0xc0]
	WORD	$0x0e66a011	// 2a70: smlsl.4s v17, v0, v6
	WORD	$0x4e66a010	// 2a74: smlsl2.4s v16, v0, v6
	WORD	$0x0f128e20	// 2a78: rshrn.4h v0, v17, #0xe
	WORD	$0x4f128e00	// 2a7c: rshrn2.8h v0, v16, #0xe
	WORD	$0xad070003	// 2a80: stp q3, q0, [x0, #0xe0]
	WORD	$0x6d4323e9	// 2a84: ldp d9, d8, [sp, #0x30]
	WORD	$0x6d422beb	// 2a88: ldp d11, d10, [sp, #0x20]
	WORD	$0x6d4133ed	// 2a8c: ldp d13, d12, [sp, #0x10]
	WORD	$0x6cc43bef	// 2a90: ldp d15, d14, [sp], #0x40
	WORD	$0xd65f03c0	// 2a94: ret
