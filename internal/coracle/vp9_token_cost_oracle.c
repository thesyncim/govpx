// vp9_token_cost_oracle runs libvpx's vp9_cost_tokens against a corpus of
// model probability rows (the 3-entry [EOB, ZERO, PIVOT] tuples that
// vp9_model_to_full_probs expands via vp9_pareto8_full), emitting the
// resulting 12-entry per-leaf cost array. The output blob is committed
// to internal/vp9/encoder/testdata/token_cost_oracle.bin so the Go-side
// pinning test (TestVP9CostTokensMatchesLibvpxOracle) can replay it
// byte-for-byte without re-running libvpx or linking against it.
//
//go:build ignore
//
// Build via internal/coracle/build_libvpx_vp9.sh — the same VP9 libvpx
// install vp9_dsp_oracle.c uses. Run:
//
//   bash internal/coracle/build_libvpx_vp9.sh
//   cc -std=c99 -O2 -Wall -Wextra \
//      -I$prefix/include $root/vp9_token_cost_oracle.c \
//      -L$prefix/lib -lvpx -lm -pthread \
//      -o $build/govpx-vp9-token-cost-oracle
//   ./internal/coracle/build/govpx-vp9-token-cost-oracle \
//      > internal/vp9/encoder/testdata/token_cost_oracle.bin
//
// libvpx references (v1.16.0):
//   - vp9/encoder/vp9_rd.c:135-152          fill_token_costs
//   - vp9/encoder/vp9_cost.c                vp9_cost_tokens
//   - vp9/common/vp9_entropy.c:1035-1039    vp9_model_to_full_probs
//   - vp9/common/vp9_entropy.c              vp9_pareto8_full
//   - vp9/common/vp9_entropy.c:95           vp9_pt_energy_class
//   - vp9/encoder/vp9_tokenize.c:75         vp9_coef_tree
//
// Blob layout (little-endian, version 1):
//   magic   uint32 = 0x54394354 ("TC9T")
//   version uint32 = 1
//   numRows uint32                   // count of (eobP, zeroP, pivotP) tuples
//   entropy_tokens uint32 = 12
//   for each row:
//     uint8 eobP
//     uint8 zeroP
//     uint8 pivotP
//     uint8 _pad = 0
//     int32[12] costs                // vp9_cost_tokens output per leaf
//   followed by the vp9_pt_energy_class table (12 bytes) so the Go side
//   can pin the table byte-for-byte without reaching into source text:
//     uint32 sectionTag = 0x50450000  // "PE\0\0" (pt_energy_class)
//     uint8[12] vp9_pt_energy_class
//     uint32 trailMagic = 0x4544394e  // "END9"

#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

typedef uint8_t vpx_prob;
typedef int16_t vpx_tree_index;

// vp9_coef_tree is exported from libvpx — vp9/encoder/vp9_tokenize.c:75.
extern const vpx_tree_index vp9_coef_tree[];

// vp9_cost_tokens is exported from libvpx — vp9/encoder/vp9_cost.c.
void vp9_cost_tokens(int *costs, const vpx_prob *probs, const vpx_tree_index *tree);

// vp9_model_to_full_probs is exported from libvpx — vp9/common/vp9_entropy.c:1035.
void vp9_model_to_full_probs(const vpx_prob *model, vpx_prob *full);

// vp9_pt_energy_class is exported from libvpx — vp9/common/vp9_entropy.c:95.
extern const uint8_t vp9_pt_energy_class[12];

enum {
	ENTROPY_NODES = 11,
	ENTROPY_TOKENS = 12,
	UNCONSTRAINED_NODES = 3,
};

static void emit_u32(uint32_t v) {
	uint8_t b[4] = {(uint8_t)v, (uint8_t)(v >> 8), (uint8_t)(v >> 16),
	                (uint8_t)(v >> 24)};
	fwrite(b, 1, 4, stdout);
}

static void emit_i32(int32_t v) {
	emit_u32((uint32_t)v);
}

static void emit_u8(uint8_t v) {
	fwrite(&v, 1, 1, stdout);
}

// emit_row runs vp9_cost_tokens against the full probability row
// produced by vp9_model_to_full_probs([eobP, zeroP, pivotP, ...]) and
// writes the 12 per-leaf costs.
static void emit_row(uint8_t eobP, uint8_t zeroP, uint8_t pivotP) {
	vpx_prob model[ENTROPY_NODES] = {0};
	model[0] = eobP;
	model[1] = zeroP;
	model[2] = pivotP;  // PIVOT_NODE; expansion fills nodes [3..10]
	vpx_prob full[ENTROPY_NODES] = {0};
	vp9_model_to_full_probs(model, full);

	int costs[ENTROPY_TOKENS] = {0};
	vp9_cost_tokens(costs, full, vp9_coef_tree);

	emit_u8(eobP);
	emit_u8(zeroP);
	emit_u8(pivotP);
	emit_u8(0);
	for (int i = 0; i < ENTROPY_TOKENS; i++) {
		emit_i32((int32_t)costs[i]);
	}
}

int main(void) {
	// Sweep a corpus that covers:
	//   - every legal pivotP in [1, 255] (the pareto8 row index)
	//   - representative eobP / zeroP values to exercise the
	//     unconstrained 3 nodes' costs without producing 255^3 rows.
	static const uint8_t prob_axis[] = {1, 16, 32, 64, 96, 128, 160, 192, 224, 240, 255};
	const size_t na = sizeof(prob_axis) / sizeof(prob_axis[0]);
	uint32_t num_rows = (uint32_t)(na * na * 255);

	emit_u32(0x54394354);  // "TC9T"
	emit_u32(1);            // version
	emit_u32(num_rows);
	emit_u32(ENTROPY_TOKENS);

	for (size_t e = 0; e < na; e++) {
		for (size_t z = 0; z < na; z++) {
			for (int p = 1; p <= 255; p++) {
				emit_row(prob_axis[e], prob_axis[z], (uint8_t)p);
			}
		}
	}

	emit_u32(0x50450000);  // "PE\0\0" — pt_energy_class section tag
	for (int i = 0; i < ENTROPY_TOKENS; i++) {
		emit_u8(vp9_pt_energy_class[i]);
	}
	emit_u32(0x4544394e);  // "END9" trailer

	return 0;
}
