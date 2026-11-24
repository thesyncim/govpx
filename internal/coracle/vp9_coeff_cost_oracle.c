// vp9_coeff_cost_oracle pins govpx's full-RD coefficient rate cost against
// libvpx v1.16.0. It emits two corpora:
//
//   (1) value_cost rows: vp9_get_token_cost(v) for a sweep of signed
//       quantized coefficient values v. This is the per-coefficient
//       token-tree-extra + sign cost libvpx adds inside cost_coeffs
//       (vp9/encoder/vp9_rdopt.c:394,406,426,439). govpx's
//       CoeffTokenExtraCost must reproduce these exact totals, including
//       the CATEGORY6 low/high split (vp9/encoder/vp9_tokenize.h:113-124).
//
//   (2) cost_coeffs rows: the total returned by a verbatim copy of
//       libvpx's static cost_coeffs (vp9/encoder/vp9_rdopt.c:358-459)
//       run against concrete quantized-coefficient blocks with a
//       token_costs table built by the same fill_token_costs path
//       libvpx uses (vp9/encoder/vp9_rd.c:135-152). govpx's
//       CoeffBlockRateCost must match these totals exactly.
//
// The output blob is committed to
// internal/vp9/encoder/testdata/coeff_cost_oracle.bin so the Go-side pin
// (coeff_cost_oracle_test.go) replays it without re-linking libvpx.
//
//go:build ignore
//
// Build via internal/coracle/build_vp9_coeff_cost_oracle.sh.
//
// libvpx references (v1.16.0):
//   - vp9/encoder/vp9_rdopt.c:347-459       band_counts, cost_coeffs
//   - vp9/encoder/vp9_rd.c:135-152          fill_token_costs
//   - vp9/encoder/vp9_tokenize.h:113-124    vp9_get_token_cost
//   - vp9/encoder/vp9_tokenize.c:56-71      vp9_dct_cat_lt_10_value_cost
//   - vp9/encoder/vp9_tokenize.c:104-133    vp9_cat6_low/high_cost
//   - vp9/common/vp9_scan.h:35-40           get_coef_context
//   - vp9/common/vp9_entropy.c:1035         vp9_model_to_full_probs
//   - vp9/common/vp9_entropy.c:95           vp9_pt_energy_class
//
// Blob layout (little-endian, version 1):
//   magic   uint32 = 0x43433943 ("CC9C")
//   version uint32 = 1
//   numValueRows uint32
//   numCostRows  uint32
//   --- value_cost section ---
//   for each value row:
//     int32 v
//     int16 token
//     int16 _pad
//     int32 cost
//   --- cost_coeffs section ---
//   for each cost row:
//     uint8 tx_size
//     uint8 plane_type
//     uint8 is_inter
//     uint8 use_fast
//     uint8 init_ctx
//     uint8 eobP
//     uint8 zeroP
//     uint8 pivotP
//     int32 eob
//     int32 n_edits
//     for each edit: int32 scan_idx, int32 val
//     int32 cost
//   trailer uint32 = 0x4544394e ("END9")

#include <assert.h>
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

typedef uint8_t vpx_prob;
typedef int16_t vpx_tree_index;
typedef int32_t tran_low_t;
typedef int16_t EXTRABIT;

// --- libvpx constants (verbatim) ---
enum {
  TX_4X4 = 0,
  TX_8X8 = 1,
  TX_16X16 = 2,
  TX_32X32 = 3,
  TX_SIZES = 4,
  PLANE_TYPES = 2,
  REF_TYPES = 2,
  COEF_BANDS = 6,
  COEFF_CONTEXTS = 6,
  ENTROPY_NODES = 11,
  ENTROPY_TOKENS = 12,
  UNCONSTRAINED_NODES = 3,
  EOB_TOKEN = 11,
  CATEGORY6_TOKEN = 10,
  CAT6_MIN_VAL = 67,
  MAX_NEIGHBORS = 2,
};

typedef struct ScanOrder {
  const int16_t *scan;
  const int16_t *iscan;
  const int16_t *neighbors;
} ScanOrder;

typedef struct TOKENVALUE {
  int16_t token;
  EXTRABIT extra;
} TOKENVALUE;

// --- libvpx exported symbols ---
extern const vpx_tree_index vp9_coef_tree[];
void vp9_cost_tokens(int *costs, const vpx_prob *probs,
                     const vpx_tree_index *tree);
void vp9_cost_tokens_skip(int *costs, const vpx_prob *probs,
                          const vpx_tree_index *tree);
void vp9_model_to_full_probs(const vpx_prob *model, vpx_prob *full);
extern const uint8_t vp9_pt_energy_class[ENTROPY_TOKENS];
extern const ScanOrder vp9_default_scan_orders[TX_SIZES];
extern const int16_t vp9_cat6_low_cost[256];
extern const uint16_t vp9_cat6_high_cost[64];
extern const int *vp9_dct_cat_lt_10_value_cost;
extern const TOKENVALUE *vp9_dct_cat_lt_10_value_tokens;

// vp9_get_token_cost — verbatim copy of the static inline in
// vp9/encoder/vp9_tokenize.h:113-124 (8-bit profile, cat6_high_table is
// always vp9_cat6_high_cost when CONFIG_VP9_HIGHBITDEPTH is off).
static int get_token_cost(int v, int16_t *token,
                          const uint16_t *cat6_high_table) {
  if (v >= CAT6_MIN_VAL || v <= -CAT6_MIN_VAL) {
    EXTRABIT extrabits;
    *token = CATEGORY6_TOKEN;
    extrabits = abs(v) - CAT6_MIN_VAL;
    return vp9_cat6_low_cost[extrabits & 0xff] +
           cat6_high_table[extrabits >> 8];
  }
  *token = vp9_dct_cat_lt_10_value_tokens[v].token;
  return vp9_dct_cat_lt_10_value_cost[v];
}

// get_coef_context — verbatim from vp9/common/vp9_scan.h:35-40.
static int get_coef_context(const int16_t *neighbors,
                            const uint8_t *token_cache, int c) {
  return (1 + token_cache[neighbors[MAX_NEIGHBORS * c + 0]] +
          token_cache[neighbors[MAX_NEIGHBORS * c + 1]]) >>
         1;
}

// band_counts — verbatim from vp9/encoder/vp9_rdopt.c:352-357.
static const int16_t band_counts[TX_SIZES][8] = {
  { 1, 2, 3, 4, 3, 16 - 13, 0 },
  { 1, 2, 3, 4, 11, 64 - 21, 0 },
  { 1, 2, 3, 4, 11, 256 - 21, 0 },
  { 1, 2, 3, 4, 11, 1024 - 21, 0 },
};

#define BAND_COEFF_CONTEXTS(band) ((band) == 0 ? 3 : COEFF_CONTEXTS)

// token_costs table for one (tx, type, is_inter) slice:
// [band][skip][ctx][token].
typedef unsigned int
    token_costs_t[COEF_BANDS][2][COEFF_CONTEXTS][ENTROPY_TOKENS];

// fill_one_slice builds the token_costs table for a single
// (tx, type, is_inter) using the band/ctx model probs derived from
// (eobP, zeroP, pivotP), mirroring fill_token_costs
// (vp9/encoder/vp9_rd.c:135-152). For the oracle every band/ctx uses the
// same 3-tuple model so the Go side can reconstruct it from a CoefModel
// filled the same way.
static void fill_one_slice(token_costs_t tc, uint8_t eobP, uint8_t zeroP,
                           uint8_t pivotP) {
  for (int k = 0; k < COEF_BANDS; ++k) {
    for (int l = 0; l < BAND_COEFF_CONTEXTS(k); ++l) {
      vpx_prob model[UNCONSTRAINED_NODES];
      vpx_prob probs[ENTROPY_NODES];
      model[0] = eobP;
      model[1] = zeroP;
      model[2] = pivotP;
      vp9_model_to_full_probs(model, probs);
      vp9_cost_tokens((int *)tc[k][0][l], probs, vp9_coef_tree);
      vp9_cost_tokens_skip((int *)tc[k][1][l], probs, vp9_coef_tree);
      assert(tc[k][0][l][EOB_TOKEN] == tc[k][1][l][EOB_TOKEN]);
    }
  }
}

// cost_coeffs_oracle is a verbatim copy of libvpx cost_coeffs body
// (vp9/encoder/vp9_rdopt.c:358-459) with the MACROBLOCK plumbing replaced
// by direct arguments.
static int cost_coeffs_oracle(token_costs_t token_costs_slice, int tx_size,
                              int pt, const int16_t *scan, const int16_t *nb,
                              int use_fast_coef_costing,
                              const tran_low_t *qcoeff, int eob) {
  const int16_t *band_count = &band_counts[tx_size][1];
  const uint16_t *cat6_high_cost = vp9_cat6_high_cost;
  uint8_t token_cache[32 * 32];
  int cost;
  unsigned int(*token_costs)[2][COEFF_CONTEXTS][ENTROPY_TOKENS] =
      token_costs_slice;

  if (eob == 0) {
    cost = token_costs[0][0][pt][EOB_TOKEN];
  } else {
    if (use_fast_coef_costing) {
      int band_left = *band_count++;
      int c;
      int v = qcoeff[0];
      int16_t prev_t;
      cost = get_token_cost(v, &prev_t, cat6_high_cost);
      cost += (*token_costs)[0][pt][prev_t];
      token_cache[0] = vp9_pt_energy_class[prev_t];
      ++token_costs;
      for (c = 1; c < eob; c++) {
        const int rc = scan[c];
        int16_t t;
        v = qcoeff[rc];
        cost += get_token_cost(v, &t, cat6_high_cost);
        cost += (*token_costs)[!prev_t][!prev_t][t];
        prev_t = t;
        if (!--band_left) {
          band_left = *band_count++;
          ++token_costs;
        }
      }
      if (band_left) cost += (*token_costs)[0][!prev_t][EOB_TOKEN];
    } else {
      int band_left = *band_count++;
      int c;
      int v = qcoeff[0];
      int16_t tok;
      unsigned int(*tok_cost_ptr)[COEFF_CONTEXTS][ENTROPY_TOKENS];
      cost = get_token_cost(v, &tok, cat6_high_cost);
      cost += (*token_costs)[0][pt][tok];
      token_cache[0] = vp9_pt_energy_class[tok];
      ++token_costs;
      tok_cost_ptr = &((*token_costs)[!tok]);
      for (c = 1; c < eob; c++) {
        const int rc = scan[c];
        v = qcoeff[rc];
        cost += get_token_cost(v, &tok, cat6_high_cost);
        pt = get_coef_context(nb, token_cache, c);
        cost += (*tok_cost_ptr)[pt][tok];
        token_cache[rc] = vp9_pt_energy_class[tok];
        if (!--band_left) {
          band_left = *band_count++;
          ++token_costs;
        }
        tok_cost_ptr = &((*token_costs)[!tok]);
      }
      if (band_left) {
        pt = get_coef_context(nb, token_cache, c);
        cost += (*token_costs)[0][pt][EOB_TOKEN];
      }
    }
  }
  return cost;
}

static void emit_u32(uint32_t v) {
  uint8_t b[4] = { (uint8_t)v, (uint8_t)(v >> 8), (uint8_t)(v >> 16),
                   (uint8_t)(v >> 24) };
  fwrite(b, 1, 4, stdout);
}
static void emit_i32(int32_t v) { emit_u32((uint32_t)v); }
static void emit_u16(uint16_t v) {
  uint8_t b[2] = { (uint8_t)v, (uint8_t)(v >> 8) };
  fwrite(b, 1, 2, stdout);
}
static void emit_i16(int16_t v) { emit_u16((uint16_t)v); }
static void emit_u8(uint8_t v) { fwrite(&v, 1, 1, stdout); }

static int max_eob_for_tx(int tx) {
  switch (tx) {
    case TX_4X4: return 16;
    case TX_8X8: return 64;
    case TX_16X16: return 256;
    default: return 1024;
  }
}

struct edit {
  int scan_idx;
  int val;
};
struct row {
  int tx, plane, is_inter, use_fast, init_ctx;
  uint8_t eobP, zeroP, pivotP;
  struct edit edits[12];
  int n_edits;
};

int main(void) {
  // ---- value_cost corpus ----
  static const int vvals[] = {
    -2000, -1024, -512, -300, -200, -150, -100, -67, -66, -50, -35, -34,
    -19,   -18,   -11,  -10,  -7,   -6,   -5,   -4,  -3,  -2,  -1,  0,
    1,     2,     3,    4,    5,    6,    7,    10,  11,  18,  19,  34,
    35,    66,    67,   100,  150,  200,  300,  512, 1024, 2000,
  };
  const int nv = (int)(sizeof(vvals) / sizeof(vvals[0]));

  // ---- cost_coeffs corpus ----
  static const struct row rows[] = {
    { TX_4X4, 0, 0, 0, 0, 100, 120, 110, { { 0, 1 } }, 1 },
    { TX_4X4, 0, 0, 0, 0, 100, 120, 110, { { 0, -3 }, { 1, 5 } }, 2 },
    { TX_4X4, 0, 1, 0, 2, 60, 90, 150,
      { { 0, 70 }, { 1, -2 }, { 2, 0 }, { 3, 11 }, { 5, -1 } }, 5 },
    { TX_4X4, 0, 0, 1, 0, 100, 120, 110, { { 0, 2 }, { 1, -2 } }, 2 },
    { TX_8X8, 0, 0, 0, 1, 80, 100, 128,
      { { 0, -4 }, { 1, 3 }, { 4, 1 }, { 10, -1 }, { 30, 2 } }, 5 },
    { TX_8X8, 1, 1, 0, 0, 64, 64, 64,
      { { 0, 19 }, { 2, -7 }, { 6, 1 } }, 3 },
    { TX_16X16, 0, 0, 0, 2, 96, 110, 140,
      { { 0, 1 }, { 1, -1 }, { 3, 5 }, { 20, -3 }, { 100, 1 } }, 5 },
    { TX_16X16, 0, 1, 1, 0, 70, 80, 90,
      { { 0, -2 }, { 5, 1 }, { 50, 2 } }, 3 },
    { TX_32X32, 0, 1, 0, 1, 50, 60, 100,
      { { 0, 200 }, { 1, -1 }, { 3, 3 }, { 200, -2 }, { 800, 1 } }, 5 },
    { TX_32X32, 0, 0, 1, 0, 90, 100, 120,
      { { 0, 4 }, { 500, -1 } }, 2 },
    { TX_4X4, 0, 0, 0, 0, 100, 120, 110, { { 0, 0 } }, 0 },
    { TX_4X4, 0, 1, 0, 0, 60, 90, 150,
      { { 0, 1 }, { 1, -2 }, { 2, 3 }, { 3, -4 }, { 4, 5 }, { 5, -6 },
        { 6, 7 }, { 7, -1 }, { 8, 2 }, { 9, -3 }, { 10, 1 }, { 11, -1 } },
      12 },
  };
  const int nr = (int)(sizeof(rows) / sizeof(rows[0]));

  emit_u32(0x43433943);  // "CC9C"
  emit_u32(1);           // version
  emit_u32((uint32_t)nv);
  emit_u32((uint32_t)nr);

  for (int i = 0; i < nv; i++) {
    int16_t token = 0;
    int cost = get_token_cost(vvals[i], &token, vp9_cat6_high_cost);
    emit_i32(vvals[i]);
    emit_i16(token);
    emit_i16(0);
    emit_i32(cost);
  }

  for (int r = 0; r < nr; r++) {
    const struct row *row = &rows[r];
    const ScanOrder *so = &vp9_default_scan_orders[row->tx];
    int maxeob = max_eob_for_tx(row->tx);
    tran_low_t qcoeff[1024];
    memset(qcoeff, 0, sizeof(qcoeff));
    int eob = 0;
    for (int e = 0; e < row->n_edits; e++) {
      int si = row->edits[e].scan_idx;
      int val = row->edits[e].val;
      if (si < 0 || si >= maxeob) continue;
      int rc = so->scan[si];
      qcoeff[rc] = val;
      if (val != 0 && si + 1 > eob) eob = si + 1;
    }

    token_costs_t tc;
    fill_one_slice(tc, row->eobP, row->zeroP, row->pivotP);

    int cost = cost_coeffs_oracle(tc, row->tx, row->init_ctx, so->scan,
                                  so->neighbors, row->use_fast, qcoeff, eob);

    emit_u8((uint8_t)row->tx);
    emit_u8((uint8_t)row->plane);
    emit_u8((uint8_t)row->is_inter);
    emit_u8((uint8_t)row->use_fast);
    emit_u8((uint8_t)row->init_ctx);
    emit_u8(row->eobP);
    emit_u8(row->zeroP);
    emit_u8(row->pivotP);
    emit_i32(eob);
    emit_i32(row->n_edits);
    for (int e = 0; e < row->n_edits; e++) {
      emit_i32(row->edits[e].scan_idx);
      emit_i32(row->edits[e].val);
    }
    emit_i32(cost);
  }

  emit_u32(0x4544394e);  // "END9"
  return 0;
}
