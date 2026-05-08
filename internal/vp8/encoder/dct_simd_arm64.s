// ARMv8 NEON port of the libvpx v1.16.0
// vp8/encoder/arm/neon/shortfdct_neon.c vp8_short_fdct4x4_neon kernel.
//
// Loads 4 rows of 4 int16, transposes to 4 column-vectors of 4 lanes each,
// runs the row-pass butterfly + odd-coefficient (cospi/sinpi) MAC, transposes
// again, then runs the column-pass butterfly with the +7 / +12000 / +51000
// rounding biases that match the scalar reference exactly.
//
// Most NEON ops aren't natively supported by Go's arm64 assembler so they
// are emitted as raw WORD encodings.

#include "textflag.h"

// forwardDCT4x4NEON ABI ($0-24):
//   input+0(FP)   *int16
//   stride+8(FP)  int (in int16 elements)
//   output+16(FP) *int16
TEXT ·forwardDCT4x4NEON(SB), NOSPLIT, $0-24
	MOVD	input+0(FP), R0
	MOVD	stride+8(FP), R3
	MOVD	output+16(FP), R1
	LSL	$1, R3, R3      // bytes = stride * 2

	WORD	$0x0c407400	// ld1 {v0.4h}, [x0]
	ADD	R3, R0, R4
	WORD	$0x0c407481	// ld1 {v1.4h}, [x4]
	ADD	R3, R4, R4
	WORD	$0x0c407482	// ld1 {v2.4h}, [x4]
	ADD	R3, R4, R4
	WORD	$0x0c407483	// ld1 {v3.4h}, [x4]

	// Transpose 4x4 (lanes -> columns):
	WORD	$0x0e822804	// trn1 v4.2s, v0.2s, v2.2s
	WORD	$0x0e826806	// trn2 v6.2s, v0.2s, v2.2s
	WORD	$0x0e832825	// trn1 v5.2s, v1.2s, v3.2s
	WORD	$0x0e836827	// trn2 v7.2s, v1.2s, v3.2s
	WORD	$0x0e452888	// trn1 v8.4h,  v4.4h, v5.4h    (col 0)
	WORD	$0x0e456889	// trn2 v9.4h,  v4.4h, v5.4h    (col 1)
	WORD	$0x0e4728ca	// trn1 v10.4h, v6.4h, v7.4h    (col 2)
	WORD	$0x0e4768cb	// trn2 v11.4h, v6.4h, v7.4h    (col 3)

	// Row-pass butterfly:
	WORD	$0x0e6b850c	// add v12.4h, v8.4h, v11.4h    (col0+col3)
	WORD	$0x0e6a852d	// add v13.4h, v9.4h, v10.4h    (col1+col2)
	WORD	$0x2e6a852e	// sub v14.4h, v9.4h, v10.4h    (c = col1-col2)
	WORD	$0x2e6b850f	// sub v15.4h, v8.4h, v11.4h    (d = col0-col3)
	WORD	$0x0f13558c	// shl v12.4h, v12.4h, #3
	WORD	$0x0f1355ad	// shl v13.4h, v13.4h, #3
	WORD	$0x0f1355ce	// shl v14.4h, v14.4h, #3
	WORD	$0x0f1355ef	// shl v15.4h, v15.4h, #3
	WORD	$0x0e6d8590	// add v16.4h, v12.4h, v13.4h   (tmp[*,0])
	WORD	$0x2e6d8592	// sub v18.4h, v12.4h, v13.4h   (tmp[*,2])

	MOVW	$14500, R5
	WORD	$0x4e040cb4	// dup v20.4s, w5     ; bias 14500
	MOVW	$7500, R5
	WORD	$0x4e040cb5	// dup v21.4s, w5     ; bias 7500
	MOVW	$2217, R5
	WORD	$0x0e020cb6	// dup v22.4h, w5
	MOVW	$5352, R5
	WORD	$0x0e020cb7	// dup v23.4h, w5

	WORD	$0x0e7681d4	// smlal v20.4s, v14.4h, v22.4h   (+ c*2217)
	WORD	$0x0e7781f4	// smlal v20.4s, v15.4h, v23.4h   (+ d*5352)
	WORD	$0x0e7681f5	// smlal v21.4s, v15.4h, v22.4h   (+ d*2217)
	WORD	$0x0e77a1d5	// smlsl v21.4s, v14.4h, v23.4h   (- c*5352)
	WORD	$0x0f148691	// shrn  v17.4h, v20.4s, #12      (tmp[*,1])
	WORD	$0x0f1486b3	// shrn  v19.4h, v21.4s, #12      (tmp[*,3])

	// Transpose for column pass:
	WORD	$0x0e922a04	// trn1 v4.2s, v16.2s, v18.2s
	WORD	$0x0e926a06	// trn2 v6.2s, v16.2s, v18.2s
	WORD	$0x0e932a25	// trn1 v5.2s, v17.2s, v19.2s
	WORD	$0x0e936a27	// trn2 v7.2s, v17.2s, v19.2s
	WORD	$0x0e452898	// trn1 v24.4h, v4.4h, v5.4h    (tmp_row0)
	WORD	$0x0e456899	// trn2 v25.4h, v4.4h, v5.4h    (tmp_row1)
	WORD	$0x0e4728da	// trn1 v26.4h, v6.4h, v7.4h    (tmp_row2)
	WORD	$0x0e4768db	// trn2 v27.4h, v6.4h, v7.4h    (tmp_row3)

	// Column-pass butterfly:
	WORD	$0x0e7b870c	// add v12.4h, v24.4h, v27.4h   (a1)
	WORD	$0x0e7a872d	// add v13.4h, v25.4h, v26.4h   (b1)
	WORD	$0x2e7a872e	// sub v14.4h, v25.4h, v26.4h   (c1)
	WORD	$0x2e7b870f	// sub v15.4h, v24.4h, v27.4h   (d1)

	WORD	$0x0e6d859c	// add v28.4h, v12.4h, v13.4h   (a1+b1)
	WORD	$0x2e6d859d	// sub v29.4h, v12.4h, v13.4h   (a1-b1)
	WORD	$0x0f0084e0	// movi v0.4h, #7
	WORD	$0x0e60879c	// add v28.4h, v28.4h, v0.4h
	WORD	$0x0e6087bd	// add v29.4h, v29.4h, v0.4h
	WORD	$0x0f1c079c	// sshr v28.4h, v28.4h, #4      (output[0..3])
	WORD	$0x0f1c07bd	// sshr v29.4h, v29.4h, #4      (output[8..11])

	MOVW	$12000, R5
	WORD	$0x4e040cb4	// dup v20.4s, w5
	MOVW	$51000, R5
	WORD	$0x4e040cb5	// dup v21.4s, w5
	WORD	$0x0e7681d4	// smlal v20.4s, v14.4h, v22.4h
	WORD	$0x0e7781f4	// smlal v20.4s, v15.4h, v23.4h
	WORD	$0x0e7681f5	// smlal v21.4s, v15.4h, v22.4h
	WORD	$0x0e77a1d5	// smlsl v21.4s, v14.4h, v23.4h
	WORD	$0x0f10869e	// shrn v30.4h, v20.4s, #16     (output[4..7] pre-correction)
	WORD	$0x0f1086bf	// shrn v31.4h, v21.4s, #16     (output[12..15])

	// (d1 != 0) correction: cmtst -> -1 mask in lanes where d != 0; sub from
	// v30 adds 1 (since sub of -1 == add 1). Lanes where d == 0 stay.
	WORD	$0x0e6f8def	// cmtst v15.4h, v15.4h, v15.4h
	WORD	$0x2e6f87de	// sub v30.4h, v30.4h, v15.4h

	// Stores: output[0..3]=v28, output[4..7]=v30, output[8..11]=v29, output[12..15]=v31
	WORD	$0x0c9f743c	// st1 {v28.4h}, [x1], #8
	WORD	$0x0c9f743e	// st1 {v30.4h}, [x1], #8
	WORD	$0x0c9f743d	// st1 {v29.4h}, [x1], #8
	WORD	$0x0c9f743f	// st1 {v31.4h}, [x1], #8
	RET
