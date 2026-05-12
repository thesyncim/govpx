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
};

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

	return 0;
}
