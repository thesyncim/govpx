// vp9_dsp_oracle runs every VP9 inverse-transform kernel ported into
// internal/vp9/dsp against a fixed PRNG-driven corpus and writes the
// (input, dest_before, dest_after) records to stdout as a binary blob.
//
// The output binary is committed under internal/vp9/dsp/testdata so the
// Go-side oracle test can replay it without re-running libvpx. Rebuild
// with `bash internal/coracle/build_libvpx_vp9.sh` when libvpx is
// updated or the corpus changes.
//
//go:build ignore
//
// (The build tag keeps `go build` from trying to compile this C file.
// It is built by build_libvpx_vp9.sh.)

#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

// The libvpx kernels are exported with C linkage from libvpx.a. We
// forward-declare them — the internal header vpx_dsp/inv_txfm.h has
// #include dependencies on libvpx's build-time-generated configs that
// are not reachable through the install prefix.
//
// tran_low_t is int16_t in the default 8-bit configuration we build
// against (CONFIG_VP9_HIGHBITDEPTH=0). govpx mirrors this: VP9
// coefficient buffers carry int16 values end-to-end on the
// non-highbitdepth path.
typedef int16_t tran_low_t;

void vpx_idct4x4_16_add_c(const tran_low_t *input, uint8_t *dest, int stride);
void vpx_idct4x4_1_add_c(const tran_low_t *input, uint8_t *dest, int stride);
void vpx_iwht4x4_16_add_c(const tran_low_t *input, uint8_t *dest, int stride);
void vpx_iwht4x4_1_add_c(const tran_low_t *input, uint8_t *dest, int stride);
void vpx_idct8x8_64_add_c(const tran_low_t *input, uint8_t *dest, int stride);
void vpx_idct8x8_12_add_c(const tran_low_t *input, uint8_t *dest, int stride);
void vpx_idct8x8_1_add_c(const tran_low_t *input, uint8_t *dest, int stride);
void vpx_idct16x16_256_add_c(const tran_low_t *input, uint8_t *dest, int stride);
void vpx_idct16x16_38_add_c(const tran_low_t *input, uint8_t *dest, int stride);
void vpx_idct16x16_10_add_c(const tran_low_t *input, uint8_t *dest, int stride);
void vpx_idct16x16_1_add_c(const tran_low_t *input, uint8_t *dest, int stride);
void vp9_iht4x4_16_add_c(const tran_low_t *input, uint8_t *dest, int stride, int tx_type);
void vp9_iht8x8_64_add_c(const tran_low_t *input, uint8_t *dest, int stride, int tx_type);
void vp9_iht16x16_256_add_c(const tran_low_t *input, uint8_t *dest, int stride, int tx_type);

void vpx_dc_predictor_4x4_c(uint8_t *dst, ptrdiff_t stride, const uint8_t *above, const uint8_t *left);
void vpx_dc_predictor_8x8_c(uint8_t *dst, ptrdiff_t stride, const uint8_t *above, const uint8_t *left);
void vpx_dc_predictor_16x16_c(uint8_t *dst, ptrdiff_t stride, const uint8_t *above, const uint8_t *left);
void vpx_dc_predictor_32x32_c(uint8_t *dst, ptrdiff_t stride, const uint8_t *above, const uint8_t *left);
void vpx_dc_left_predictor_4x4_c(uint8_t *dst, ptrdiff_t stride, const uint8_t *above, const uint8_t *left);
void vpx_dc_left_predictor_8x8_c(uint8_t *dst, ptrdiff_t stride, const uint8_t *above, const uint8_t *left);
void vpx_dc_left_predictor_16x16_c(uint8_t *dst, ptrdiff_t stride, const uint8_t *above, const uint8_t *left);
void vpx_dc_left_predictor_32x32_c(uint8_t *dst, ptrdiff_t stride, const uint8_t *above, const uint8_t *left);
void vpx_dc_top_predictor_4x4_c(uint8_t *dst, ptrdiff_t stride, const uint8_t *above, const uint8_t *left);
void vpx_dc_top_predictor_8x8_c(uint8_t *dst, ptrdiff_t stride, const uint8_t *above, const uint8_t *left);
void vpx_dc_top_predictor_16x16_c(uint8_t *dst, ptrdiff_t stride, const uint8_t *above, const uint8_t *left);
void vpx_dc_top_predictor_32x32_c(uint8_t *dst, ptrdiff_t stride, const uint8_t *above, const uint8_t *left);
void vpx_dc_128_predictor_4x4_c(uint8_t *dst, ptrdiff_t stride, const uint8_t *above, const uint8_t *left);
void vpx_dc_128_predictor_8x8_c(uint8_t *dst, ptrdiff_t stride, const uint8_t *above, const uint8_t *left);
void vpx_dc_128_predictor_16x16_c(uint8_t *dst, ptrdiff_t stride, const uint8_t *above, const uint8_t *left);
void vpx_dc_128_predictor_32x32_c(uint8_t *dst, ptrdiff_t stride, const uint8_t *above, const uint8_t *left);
void vpx_v_predictor_4x4_c(uint8_t *dst, ptrdiff_t stride, const uint8_t *above, const uint8_t *left);
void vpx_v_predictor_8x8_c(uint8_t *dst, ptrdiff_t stride, const uint8_t *above, const uint8_t *left);
void vpx_v_predictor_16x16_c(uint8_t *dst, ptrdiff_t stride, const uint8_t *above, const uint8_t *left);
void vpx_v_predictor_32x32_c(uint8_t *dst, ptrdiff_t stride, const uint8_t *above, const uint8_t *left);
void vpx_h_predictor_4x4_c(uint8_t *dst, ptrdiff_t stride, const uint8_t *above, const uint8_t *left);
void vpx_h_predictor_8x8_c(uint8_t *dst, ptrdiff_t stride, const uint8_t *above, const uint8_t *left);
void vpx_h_predictor_16x16_c(uint8_t *dst, ptrdiff_t stride, const uint8_t *above, const uint8_t *left);
void vpx_h_predictor_32x32_c(uint8_t *dst, ptrdiff_t stride, const uint8_t *above, const uint8_t *left);
void vpx_tm_predictor_4x4_c(uint8_t *dst, ptrdiff_t stride, const uint8_t *above, const uint8_t *left);
void vpx_tm_predictor_8x8_c(uint8_t *dst, ptrdiff_t stride, const uint8_t *above, const uint8_t *left);
void vpx_tm_predictor_16x16_c(uint8_t *dst, ptrdiff_t stride, const uint8_t *above, const uint8_t *left);
void vpx_tm_predictor_32x32_c(uint8_t *dst, ptrdiff_t stride, const uint8_t *above, const uint8_t *left);

#define DECL_DIR(NAME) \
	void vpx_##NAME##_predictor_8x8_c(uint8_t *dst, ptrdiff_t stride, const uint8_t *above, const uint8_t *left); \
	void vpx_##NAME##_predictor_16x16_c(uint8_t *dst, ptrdiff_t stride, const uint8_t *above, const uint8_t *left); \
	void vpx_##NAME##_predictor_32x32_c(uint8_t *dst, ptrdiff_t stride, const uint8_t *above, const uint8_t *left);
DECL_DIR(d207)
DECL_DIR(d63)
DECL_DIR(d45)
DECL_DIR(d117)
DECL_DIR(d135)
DECL_DIR(d153)
#undef DECL_DIR

// 4x4 hand-coded predictors (separate from the parametric *_8x8/16/32 set).
void vpx_d207_predictor_4x4_c(uint8_t *dst, ptrdiff_t stride, const uint8_t *above, const uint8_t *left);
void vpx_d63_predictor_4x4_c(uint8_t *dst, ptrdiff_t stride, const uint8_t *above, const uint8_t *left);
void vpx_d63e_predictor_4x4_c(uint8_t *dst, ptrdiff_t stride, const uint8_t *above, const uint8_t *left);
void vpx_d45_predictor_4x4_c(uint8_t *dst, ptrdiff_t stride, const uint8_t *above, const uint8_t *left);
void vpx_d45e_predictor_4x4_c(uint8_t *dst, ptrdiff_t stride, const uint8_t *above, const uint8_t *left);
void vpx_d117_predictor_4x4_c(uint8_t *dst, ptrdiff_t stride, const uint8_t *above, const uint8_t *left);
void vpx_d135_predictor_4x4_c(uint8_t *dst, ptrdiff_t stride, const uint8_t *above, const uint8_t *left);
void vpx_d153_predictor_4x4_c(uint8_t *dst, ptrdiff_t stride, const uint8_t *above, const uint8_t *left);
void vpx_he_predictor_4x4_c(uint8_t *dst, ptrdiff_t stride, const uint8_t *above, const uint8_t *left);
void vpx_ve_predictor_4x4_c(uint8_t *dst, ptrdiff_t stride, const uint8_t *above, const uint8_t *left);
void vpx_idct32x32_1024_add_c(const tran_low_t *input, uint8_t *dest, int stride);
void vpx_idct32x32_135_add_c(const tran_low_t *input, uint8_t *dest, int stride);
void vpx_idct32x32_34_add_c(const tran_low_t *input, uint8_t *dest, int stride);
void vpx_idct32x32_1_add_c(const tran_low_t *input, uint8_t *dest, int stride);

// xorshift32 PRNG: deterministic, identical on every build.
static uint32_t prng_state;
static uint32_t prng32(void) {
	uint32_t x = prng_state;
	x ^= x << 13;
	x ^= x >> 17;
	x ^= x << 5;
	prng_state = x;
	return x;
}

static int16_t prng_coef(int range) {
	return (int16_t)((int32_t)(prng32() % (2u * (uint32_t)range + 1u)) - range);
}

static uint8_t prng_pixel(void) {
	return (uint8_t)(prng32() & 0xff);
}

static void emit_u32(uint32_t v) {
	uint8_t b[4] = {
		(uint8_t)v, (uint8_t)(v >> 8), (uint8_t)(v >> 16), (uint8_t)(v >> 24),
	};
	fwrite(b, 1, 4, stdout);
}

static void emit_i16(int16_t v) {
	uint16_t u = (uint16_t)v;
	uint8_t b[2] = { (uint8_t)u, (uint8_t)(u >> 8) };
	fwrite(b, 1, 2, stdout);
}

static void emit_bytes(const void *p, size_t n) {
	fwrite(p, 1, n, stdout);
}

// Record layout (little-endian):
//   u32 kernel_id   1=idct4x4_16, 2=idct4x4_1, 3=iwht4x4_16, 4=iwht4x4_1,
//                   5=idct8x8_64, 6=idct8x8_12, 7=idct8x8_1
//   u32 tx_size    4 or 8
//   u32 n_coefs    16 or 64
//   n_coefs * i16 inputs
//   u32 stride
//   tx_size*tx_size bytes dest_in
//   tx_size*tx_size bytes dest_out

enum {
	K_IDCT4_16   = 1,
	K_IDCT4_1    = 2,
	K_IWHT4_16   = 3,
	K_IWHT4_1    = 4,
	K_IDCT8_64   = 5,
	K_IDCT8_12   = 6,
	K_IDCT8_1    = 7,
	K_IDCT16_256 = 8,
	K_IDCT16_38  = 9,
	K_IDCT16_10  = 10,
	K_IDCT16_1   = 11,
	// IHT kernels carry the TxType in the high byte of the kernel id:
	//   12, 13, 14 = iht4x4 with tx_type ADST_DCT, DCT_ADST, ADST_ADST
	//   15, 16, 17 = iht8x8 with tx_type ADST_DCT, DCT_ADST, ADST_ADST
	// (DCT_DCT is already covered by the dedicated idct kernels.)
	K_IHT4_ADST_DCT  = 12,
	K_IHT4_DCT_ADST  = 13,
	K_IHT4_ADST_ADST = 14,
	K_IHT8_ADST_DCT  = 15,
	K_IHT8_DCT_ADST  = 16,
	K_IHT8_ADST_ADST = 17,
	K_IDCT32_1024 = 18,
	K_IDCT32_135  = 19,
	K_IDCT32_34   = 20,
	K_IDCT32_1    = 21,
	K_IHT16_ADST_DCT  = 22,
	K_IHT16_DCT_ADST  = 23,
	K_IHT16_ADST_ADST = 24,
	// Intra prediction kernels follow. Records encode tx_size and an
	// extra "intra_kind" byte tucked into the kernel_id high nibble:
	//   100 + (kind*4) + log2(tx_size)-2 for sizes 4..32
	// kind values:
	//   0 = dc, 1 = dc_left, 2 = dc_top, 3 = dc_128,
	//   4 = v,  5 = h,       6 = tm
	K_INTRA_BASE = 100,
};

typedef void intra_fn(uint8_t *dst, ptrdiff_t stride, const uint8_t *above, const uint8_t *left);
static intra_fn *const intra_table[7][4] = {
	{ vpx_dc_predictor_4x4_c,      vpx_dc_predictor_8x8_c,      vpx_dc_predictor_16x16_c,      vpx_dc_predictor_32x32_c      },
	{ vpx_dc_left_predictor_4x4_c, vpx_dc_left_predictor_8x8_c, vpx_dc_left_predictor_16x16_c, vpx_dc_left_predictor_32x32_c },
	{ vpx_dc_top_predictor_4x4_c,  vpx_dc_top_predictor_8x8_c,  vpx_dc_top_predictor_16x16_c,  vpx_dc_top_predictor_32x32_c  },
	{ vpx_dc_128_predictor_4x4_c,  vpx_dc_128_predictor_8x8_c,  vpx_dc_128_predictor_16x16_c,  vpx_dc_128_predictor_32x32_c  },
	{ vpx_v_predictor_4x4_c,       vpx_v_predictor_8x8_c,       vpx_v_predictor_16x16_c,       vpx_v_predictor_32x32_c       },
	{ vpx_h_predictor_4x4_c,       vpx_h_predictor_8x8_c,       vpx_h_predictor_16x16_c,       vpx_h_predictor_32x32_c       },
	{ vpx_tm_predictor_4x4_c,      vpx_tm_predictor_8x8_c,      vpx_tm_predictor_16x16_c,      vpx_tm_predictor_32x32_c      },
};

// Directional predictor table — sized 8x8, 16x16, 32x32 only. The 4x4
// directional path uses dedicated hand-coded functions; see dir4_table.
static intra_fn *const dir_table[6][3] = {
	{ vpx_d207_predictor_8x8_c, vpx_d207_predictor_16x16_c, vpx_d207_predictor_32x32_c },
	{ vpx_d63_predictor_8x8_c,  vpx_d63_predictor_16x16_c,  vpx_d63_predictor_32x32_c  },
	{ vpx_d45_predictor_8x8_c,  vpx_d45_predictor_16x16_c,  vpx_d45_predictor_32x32_c  },
	{ vpx_d117_predictor_8x8_c, vpx_d117_predictor_16x16_c, vpx_d117_predictor_32x32_c },
	{ vpx_d135_predictor_8x8_c, vpx_d135_predictor_16x16_c, vpx_d135_predictor_32x32_c },
	{ vpx_d153_predictor_8x8_c, vpx_d153_predictor_16x16_c, vpx_d153_predictor_32x32_c },
};

// 4x4 hand-coded predictor table. Indices line up with the dir4_kind
// enum on the Go side (0..9).
static intra_fn *const dir4_table[10] = {
	vpx_d207_predictor_4x4_c,  // 0
	vpx_d63_predictor_4x4_c,   // 1
	vpx_d45_predictor_4x4_c,   // 2
	vpx_d117_predictor_4x4_c,  // 3
	vpx_d135_predictor_4x4_c,  // 4
	vpx_d153_predictor_4x4_c,  // 5
	vpx_he_predictor_4x4_c,    // 6
	vpx_ve_predictor_4x4_c,    // 7
	vpx_d63e_predictor_4x4_c,  // 8
	vpx_d45e_predictor_4x4_c,  // 9
};

static void run_dir4(int kind) {
	int bs = 4;
	uint8_t dst[16];
	uint8_t above_buf[1 + 8];
	uint8_t left_buf[4];

	for (int i = 0; i < 1 + 2*bs; i++) above_buf[i] = prng_pixel();
	for (int i = 0; i < bs; i++) left_buf[i] = prng_pixel();
	for (int i = 0; i < bs * bs; i++) dst[i] = prng_pixel();

	dir4_table[kind](dst, bs, above_buf + 1, left_buf);

	// Kernel ids 300..309 cover the 10 hand-coded 4x4 predictors.
	int kid = 300 + kind;
	emit_u32((uint32_t)kid);
	emit_u32((uint32_t)bs);
	emit_u32((uint32_t)(1 + 2*bs));
	emit_bytes(above_buf, (size_t)(1 + 2*bs));
	emit_u32((uint32_t)bs);
	emit_bytes(left_buf, (size_t)bs);
	emit_u32((uint32_t)bs);
	emit_u32((uint32_t)(bs * bs));
	emit_bytes(dst, (size_t)(bs * bs));
}

static void run_dir(int kind, int size_log2) {
	int bs = 1 << size_log2;
	uint8_t dst[32 * 32];
	uint8_t above_buf[1 + 64];
	uint8_t left_buf[32];

	for (int i = 0; i < 1 + 2*bs; i++) above_buf[i] = prng_pixel();
	for (int i = 0; i < bs; i++) left_buf[i] = prng_pixel();
	for (int i = 0; i < bs * bs; i++) dst[i] = prng_pixel();

	dir_table[kind][size_log2 - 3](dst, bs, above_buf + 1, left_buf);

	// Directional kernel ids start at 200 so the Go side can branch
	// cleanly: id = 200 + kind*4 + (size_log2 - 3)
	int kid = 200 + kind*4 + (size_log2 - 3);
	emit_u32((uint32_t)kid);
	emit_u32((uint32_t)bs);
	emit_u32((uint32_t)(1 + 2*bs));
	emit_bytes(above_buf, (size_t)(1 + 2*bs));
	emit_u32((uint32_t)bs);
	emit_bytes(left_buf, (size_t)bs);
	emit_u32((uint32_t)bs);
	emit_u32((uint32_t)(bs * bs));
	emit_bytes(dst, (size_t)(bs * bs));
}

static void run_intra(int kind, int size_log2) {
	int bs = 1 << size_log2;
	uint8_t dst[32 * 32];
	// Pad above with 1 byte on the left for the [-1] (top-left) access and
	// 2*bs bytes beyond for the directional predictors (unused here but
	// future-proofs the record layout).
	uint8_t above_buf[1 + 64];
	uint8_t left_buf[32];

	for (int i = 0; i < 1 + 2*bs; i++) above_buf[i] = prng_pixel();
	for (int i = 0; i < bs; i++) left_buf[i] = prng_pixel();
	for (int i = 0; i < bs * bs; i++) dst[i] = prng_pixel();

	// Call the libvpx kernel with above pointer at the post-[-1] entry,
	// just like libvpx callers do.
	intra_table[kind][size_log2 - 2](dst, bs, above_buf + 1, left_buf);

	int kid = K_INTRA_BASE + kind*4 + (size_log2 - 2);
	emit_u32((uint32_t)kid);
	emit_u32((uint32_t)bs);          // tx_size
	emit_u32((uint32_t)(1 + 2*bs));  // n_above
	emit_bytes(above_buf, (size_t)(1 + 2*bs));
	emit_u32((uint32_t)bs);          // n_left
	emit_bytes(left_buf, (size_t)bs);
	emit_u32((uint32_t)bs);          // stride
	emit_u32((uint32_t)(bs * bs));   // n_dst
	emit_bytes(dst, (size_t)(bs * bs));
}

static void run_case(int kernel_id, int tx_size, int n_coefs, int coef_range,
                     int sparse_top_left) {
	int16_t input[1024];
	uint8_t dest_in[1024];
	uint8_t dest_out[1024];
	int stride = tx_size;

	for (int i = 0; i < n_coefs; i++) {
		input[i] = prng_coef(coef_range);
	}
	if (sparse_top_left) {
		int top_left = sparse_top_left;
		for (int r = 0; r < tx_size; r++) {
			for (int c = 0; c < tx_size; c++) {
				if (r >= top_left || c >= top_left) input[r * tx_size + c] = 0;
			}
		}
	}
	for (int i = 0; i < tx_size * tx_size; i++) {
		dest_in[i] = prng_pixel();
	}
	memcpy(dest_out, dest_in, (size_t)(tx_size * tx_size));

	switch (kernel_id) {
		case K_IDCT4_16:   vpx_idct4x4_16_add_c(input, dest_out, stride);   break;
		case K_IDCT4_1:    vpx_idct4x4_1_add_c(input, dest_out, stride);    break;
		case K_IWHT4_16:   vpx_iwht4x4_16_add_c(input, dest_out, stride);   break;
		case K_IWHT4_1:    vpx_iwht4x4_1_add_c(input, dest_out, stride);    break;
		case K_IDCT8_64:   vpx_idct8x8_64_add_c(input, dest_out, stride);   break;
		case K_IDCT8_12:   vpx_idct8x8_12_add_c(input, dest_out, stride);   break;
		case K_IDCT8_1:    vpx_idct8x8_1_add_c(input, dest_out, stride);    break;
		case K_IDCT16_256: vpx_idct16x16_256_add_c(input, dest_out, stride); break;
		case K_IDCT16_38:  vpx_idct16x16_38_add_c(input, dest_out, stride);  break;
		case K_IDCT16_10:  vpx_idct16x16_10_add_c(input, dest_out, stride);  break;
		case K_IHT4_ADST_DCT:  vp9_iht4x4_16_add_c(input, dest_out, stride, 1); break;
		case K_IHT4_DCT_ADST:  vp9_iht4x4_16_add_c(input, dest_out, stride, 2); break;
		case K_IHT4_ADST_ADST: vp9_iht4x4_16_add_c(input, dest_out, stride, 3); break;
		case K_IHT8_ADST_DCT:  vp9_iht8x8_64_add_c(input, dest_out, stride, 1); break;
		case K_IHT8_DCT_ADST:  vp9_iht8x8_64_add_c(input, dest_out, stride, 2); break;
		case K_IHT8_ADST_ADST: vp9_iht8x8_64_add_c(input, dest_out, stride, 3); break;
		case K_IDCT32_1024:     vpx_idct32x32_1024_add_c(input, dest_out, stride); break;
		case K_IDCT32_135:      vpx_idct32x32_135_add_c(input, dest_out, stride);  break;
		case K_IDCT32_34:       vpx_idct32x32_34_add_c(input, dest_out, stride);   break;
		case K_IHT16_ADST_DCT:  vp9_iht16x16_256_add_c(input, dest_out, stride, 1); break;
		case K_IHT16_DCT_ADST:  vp9_iht16x16_256_add_c(input, dest_out, stride, 2); break;
		case K_IHT16_ADST_ADST: vp9_iht16x16_256_add_c(input, dest_out, stride, 3); break;
		default: return;
	}

	emit_u32((uint32_t)kernel_id);
	emit_u32((uint32_t)tx_size);
	emit_u32((uint32_t)n_coefs);
	for (int i = 0; i < n_coefs; i++) emit_i16(input[i]);
	emit_u32((uint32_t)stride);
	emit_bytes(dest_in, (size_t)(tx_size * tx_size));
	emit_bytes(dest_out, (size_t)(tx_size * tx_size));
}

static void run_dc_only(int kernel_id, int tx_size, int range) {
	int16_t input[1024];
	uint8_t dest_in[1024], dest_out[1024];
	int n = tx_size * tx_size;
	memset(input, 0, sizeof(input));
	input[0] = prng_coef(range);
	for (int j = 0; j < n; j++) dest_in[j] = prng_pixel();
	memcpy(dest_out, dest_in, (size_t)n);
	switch (kernel_id) {
		case K_IDCT4_1:   vpx_idct4x4_1_add_c(input, dest_out, tx_size);   break;
		case K_IWHT4_1:   vpx_iwht4x4_1_add_c(input, dest_out, tx_size);   break;
		case K_IDCT8_1:   vpx_idct8x8_1_add_c(input, dest_out, tx_size);   break;
		case K_IDCT16_1:  vpx_idct16x16_1_add_c(input, dest_out, tx_size); break;
		case K_IDCT32_1:  vpx_idct32x32_1_add_c(input, dest_out, tx_size); break;
		default: return;
	}
	emit_u32((uint32_t)kernel_id);
	emit_u32((uint32_t)tx_size);
	emit_u32((uint32_t)n);
	for (int j = 0; j < n; j++) emit_i16(input[j]);
	emit_u32((uint32_t)tx_size);
	emit_bytes(dest_in, (size_t)n);
	emit_bytes(dest_out, (size_t)n);
}

int main(void) {
	prng_state = 0x9e3779b9u;

	emit_u32(0x76503944u); // "vP9D" / version 2
	emit_u32(2u);

	for (int i = 0; i < 100; i++) {
		int range = 1 + (i * 30);
		run_case(K_IDCT4_16, 4, 16, range, 0);
	}
	for (int i = 0; i < 50; i++) {
		run_dc_only(K_IDCT4_1, 4, 2000);
	}
	for (int i = 0; i < 50; i++) {
		run_case(K_IWHT4_16, 4, 16, 200 + i, 0);
	}
	for (int i = 0; i < 30; i++) {
		run_dc_only(K_IWHT4_1, 4, 800);
	}
	for (int i = 0; i < 100; i++) {
		int range = 1 + (i * 25);
		run_case(K_IDCT8_64, 8, 64, range, 0);
	}
	for (int i = 0; i < 50; i++) {
		int range = 1 + (i * 40);
		run_case(K_IDCT8_12, 8, 64, range, 4);
	}
	for (int i = 0; i < 30; i++) {
		run_dc_only(K_IDCT8_1, 8, 2000);
	}
	for (int i = 0; i < 60; i++) {
		int range = 1 + (i * 20);
		run_case(K_IDCT16_256, 16, 256, range, 0);
	}
	for (int i = 0; i < 30; i++) {
		run_case(K_IDCT16_38, 16, 256, 1 + i*30, 8);
	}
	for (int i = 0; i < 20; i++) {
		run_case(K_IDCT16_10, 16, 256, 1 + i*40, 4);
	}
	for (int i = 0; i < 20; i++) {
		run_dc_only(K_IDCT16_1, 16, 2000);
	}

	// IHT4x4 — 30 cases per non-DCT_DCT TxType.
	for (int i = 0; i < 30; i++) run_case(K_IHT4_ADST_DCT,  4, 16, 1 + i*40, 0);
	for (int i = 0; i < 30; i++) run_case(K_IHT4_DCT_ADST,  4, 16, 1 + i*40, 0);
	for (int i = 0; i < 30; i++) run_case(K_IHT4_ADST_ADST, 4, 16, 1 + i*40, 0);
	// IHT8x8 — 30 cases per non-DCT_DCT TxType.
	for (int i = 0; i < 30; i++) run_case(K_IHT8_ADST_DCT,  8, 64, 1 + i*30, 0);
	for (int i = 0; i < 30; i++) run_case(K_IHT8_DCT_ADST,  8, 64, 1 + i*30, 0);
	for (int i = 0; i < 30; i++) run_case(K_IHT8_ADST_ADST, 8, 64, 1 + i*30, 0);

	// IDCT 32x32 — 30 dense + 15 sparse-16x16 + 10 sparse-8x8 + 10 DC.
	for (int i = 0; i < 30; i++) run_case(K_IDCT32_1024, 32, 1024, 1 + i*15, 0);
	for (int i = 0; i < 15; i++) run_case(K_IDCT32_135,  32, 1024, 1 + i*20, 16);
	for (int i = 0; i < 10; i++) run_case(K_IDCT32_34,   32, 1024, 1 + i*25, 8);
	for (int i = 0; i < 10; i++) run_dc_only(K_IDCT32_1, 32, 2000);

	// IHT 16x16 — 20 cases per non-DCT_DCT TxType.
	for (int i = 0; i < 20; i++) run_case(K_IHT16_ADST_DCT,  16, 256, 1 + i*25, 0);
	for (int i = 0; i < 20; i++) run_case(K_IHT16_DCT_ADST,  16, 256, 1 + i*25, 0);
	for (int i = 0; i < 20; i++) run_case(K_IHT16_ADST_ADST, 16, 256, 1 + i*25, 0);

	// Intra prediction kernels — 5 fresh randomized cases each across
	// the 7 modes × 4 sizes (DC, DC_LEFT, DC_TOP, DC_128, V, H, TM).
	for (int kind = 0; kind < 7; kind++) {
		for (int log2 = 2; log2 <= 5; log2++) {
			for (int i = 0; i < 5; i++) run_intra(kind, log2);
		}
	}

	// Directional predictors — 5 cases each across the 6 directions ×
	// 3 sizes (8/16/32).
	for (int kind = 0; kind < 6; kind++) {
		for (int log2 = 3; log2 <= 5; log2++) {
			for (int i = 0; i < 5; i++) run_dir(kind, log2);
		}
	}
	// 10 4x4 hand-coded predictors — 8 cases each.
	for (int kind = 0; kind < 10; kind++) {
		for (int i = 0; i < 8; i++) run_dir4(kind);
	}

	return 0;
}
