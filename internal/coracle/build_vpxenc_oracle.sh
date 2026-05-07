#!/usr/bin/env sh
#
# Build a libvpx v1.16.0 vpxenc binary patched to emit the same per-frame /
# per-MB JSON Lines oracle trace that govpx writes from
# encoder_oracle_trace.go. The trace is written to the file path passed via
# the GOVPX_ORACLE_TRACE_OUT env var; if the env var is unset the patched
# binary behaves exactly like stock vpxenc. The patched source tree lives
# in a sibling directory ("$build_dir/libvpx-$tag-vpxenc-oracle") so it
# does not affect the stock vpxenc build produced by build_vpxenc.sh.
#
# Sandbox limits: the harness in this repo does not always have network
# access at build time. The script reuses the libvpx tarball that
# build_libvpx.sh / build_vpxenc.sh fetch, so as long as one of those has
# been run first the source archive is already on disk. If running
# stand-alone in a clean tree, the script will attempt to curl the tarball
# directly. The Go-side oracle comparator (oracle_compare.go /
# oracle_compare_test.go) does not depend on this binary; it operates on
# JSON Lines streams from any source.
set -eu

tag="v1.16.0"
root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
build_dir=${GOVPX_CORACLE_BUILD_DIR:-"$root/build"}
src_dir="$build_dir/libvpx-$tag-vpxenc-oracle"
vpxenc_oracle_bin=${GOVPX_VPXENC_ORACLE_BIN:-"$build_dir/vpxenc-oracle"}
config_stamp="$src_dir/.govpx-vpxenc-oracle-config"
patch_stamp="$src_dir/.govpx-vpxenc-oracle-patched"
want_config="v1.16.0-vp8-vpxenc-oracle-trace-2026-05-07-prob-state
src_dir=$src_dir
vpxenc_oracle_bin=$vpxenc_oracle_bin"
jobs=${JOBS:-}

if [ -z "$jobs" ]; then
	if command -v getconf >/dev/null 2>&1; then
		jobs=$(getconf _NPROCESSORS_ONLN 2>/dev/null || printf '2')
	else
		jobs=2
	fi
fi

mkdir -p "$build_dir"
archive="$build_dir/libvpx-$tag.tar.gz"

fetch_source() {
	if [ ! -f "$archive" ]; then
		curl -L -o "$archive" "https://chromium.googlesource.com/webm/libvpx/+archive/refs/tags/$tag.tar.gz"
	fi
	rm -rf "$src_dir"
	mkdir -p "$src_dir"
	tar -xzf "$archive" -C "$src_dir"
}

current_config=
if [ -f "$config_stamp" ]; then
	current_config=$(cat "$config_stamp")
fi

if [ ! -d "$src_dir" ] || [ "$current_config" != "$want_config" ]; then
	fetch_source
	rm -f "$patch_stamp"
fi

# ----------------------------------------------------------------------------
# Patch: drop a self-contained instrumentation TU into vp8/encoder/, plus
# anchor-based in-place edits in vp8/encoder/encodeframe.c,
# vp8/encoder/bitstream.c, and vp8/vp8cx.mk. Anchor-based edits are used
# instead of a unified diff so the patch is robust against minor upstream
# whitespace shifts; each anchor is unique in the upstream v1.16.0 file.
# All output is gated on GOVPX_ORACLE_TRACE_OUT, and the patch does not
# modify any libvpx public header.
# ----------------------------------------------------------------------------
if [ ! -f "$patch_stamp" ]; then
	# (1) New translation unit: vp8/encoder/oracle_trace.c
	cat > "$src_dir/vp8/encoder/oracle_trace.c" <<'GOVPX_ORACLE_TU'
/*
 * govpx oracle trace instrumentation. Emits per-frame and per-MB JSON
 * Lines matching the schema in govpx's encoder_oracle_trace.go. Active
 * only when the GOVPX_ORACLE_TRACE_OUT environment variable is set;
 * otherwise every hook is a quick no-op. Allocations and the FILE* are
 * owned by this translation unit so no libvpx state is added.
 */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "vpx/vpx_encoder.h"
#include "vp8/common/blockd.h"
#include "vp8/common/onyxc_int.h"
#include "vp8/encoder/onyx_int.h"

/* Adler-32 over a Y/U/V plane's visible region. Mirrors govpx's
 * planeAdler32 (encoder_oracle_trace.go) so reference checksums line up. */
#define GOVPX_ADLER_MOD 65521u
static unsigned int govpx_plane_adler32(const unsigned char *plane,
                                        int width, int height, int stride) {
    unsigned int a = 1;
    unsigned int b = 0;
    int row, col;
    if (plane == NULL || width <= 0 || height <= 0 || stride <= 0) {
        return 0;
    }
    for (row = 0; row < height; ++row) {
        const unsigned char *p = plane + (size_t)row * (size_t)stride;
        for (col = 0; col < width; ++col) {
            a = (a + p[col]) % GOVPX_ADLER_MOD;
            b = (b + a) % GOVPX_ADLER_MOD;
        }
    }
    return (b << 16) | a;
}

/* Adler-32 over a flat byte buffer. Used for compact frame-level
 * probability-state digests (coef_probs, mode probs, MV probs). The govpx
 * side mirrors the same hash via Go's hash/adler32 in encoder_oracle_trace.go
 * so a one-byte divergence in any of the underlying tables surfaces as a
 * single field-level diff in the comparator. */
static unsigned int govpx_buf_adler32(const unsigned char *buf, size_t n) {
    unsigned int a = 1;
    unsigned int b = 0;
    size_t i;
    if (buf == NULL || n == 0) {
        return (b << 16) | a; /* Adler32 of an empty buffer is 1. */
    }
    for (i = 0; i < n; ++i) {
        a = (a + buf[i]) % GOVPX_ADLER_MOD;
        b = (b + a) % GOVPX_ADLER_MOD;
    }
    return (b << 16) | a;
}

typedef struct {
    int valid;
    int segment_id;
    int mode;
    int ref_frame;
    int mv_row;
    int mv_col;
    int skip;
    unsigned char eobs[25];
    int eob_sum;
    short qcoeff[25][16];
    /* Improved-MV predictor fields. Mirror the govpx-side oracleTraceMBRow
     * fields populated by attachImprovedMVTrace in encoder_reconstruct.go.
     * `improved_mv_start` is true only when the chosen NEWMV ref had its
     * predictor produced by vp8_mv_pred (i.e. the per-(mb, ref) slot was
     * recorded during the macroblock pick). When false, the schema still
     * emits the four numeric companions at their pre-extension defaults
     * (near_sadidx=-1, sr=-1, row=0, col=0) so the comparator's union diff
     * can still reason over them. `improved_mv_near_sadidx` matches the
     * libvpx slot index `near_sadidx[i]` for the matched rank `i` inside
     * vp8_mv_pred, or -1 when the median fallback (`*sr=0`) fired. */
    int improved_mv_start;
    int improved_mv_near_sadidx;
    int improved_mv_row;
    int improved_mv_col;
    int improved_mv_sr;
} govpx_mb_row_t;

/* Per-reference improved-MV predictor slot. vp8_mv_pred populates this
 * (via govpx_oracle_record_improved_mv) for each candidate ref tested by
 * the inter-mode pick, indexed by ref_frame (LAST_FRAME / GOLDEN_FRAME /
 * ALTREF_FRAME). govpx_oracle_capture_mb reads the slot keyed by the
 * chosen MB's ref_frame and clears all four slots so the next MB starts
 * fresh. The `valid` flag distinguishes "vp8_mv_pred ran for this ref"
 * from "the pick path skipped this ref entirely". */
typedef struct {
    int valid;
    int near_sadidx; /* -1 == median fallback (find=0 / *sr=0) */
    int mvp_row;
    int mvp_col;
    int sr;
} govpx_improved_mv_slot_t;

static govpx_improved_mv_slot_t govpx_improved_mv_slots[4];

void govpx_oracle_record_improved_mv(int ref_frame, int near_sadidx,
                                     int mvp_row, int mvp_col, int sr) {
    if (ref_frame < 1 || ref_frame > 3) {
        /* INTRA_FRAME (0) and out-of-range refs are ignored: vp8_mv_pred
         * early-returns for INTRA, and the calling code never sets a ref
         * outside [LAST_FRAME, ALTREF_FRAME]. */
        return;
    }
    govpx_improved_mv_slots[ref_frame].valid = 1;
    govpx_improved_mv_slots[ref_frame].near_sadidx = near_sadidx;
    govpx_improved_mv_slots[ref_frame].mvp_row = mvp_row;
    govpx_improved_mv_slots[ref_frame].mvp_col = mvp_col;
    govpx_improved_mv_slots[ref_frame].sr = sr;
}

static void govpx_oracle_clear_improved_mv_slots(void) {
    int r;
    for (r = 0; r < 4; ++r) {
        govpx_improved_mv_slots[r].valid = 0;
        govpx_improved_mv_slots[r].near_sadidx = 0;
        govpx_improved_mv_slots[r].mvp_row = 0;
        govpx_improved_mv_slots[r].mvp_col = 0;
        govpx_improved_mv_slots[r].sr = 0;
    }
}

typedef struct {
    FILE *out;
    int initialized;
    int enabled;
    govpx_mb_row_t *mb_rows;
    int mb_capacity;
    int mb_cols;
    unsigned long long frame_index;
} govpx_oracle_state_t;

static govpx_oracle_state_t govpx_oracle_state;

static void govpx_oracle_init(void) {
    const char *path;
    if (govpx_oracle_state.initialized) {
        return;
    }
    govpx_oracle_state.initialized = 1;
    path = getenv("GOVPX_ORACLE_TRACE_OUT");
    if (path == NULL || path[0] == '\0') {
        govpx_oracle_state.enabled = 0;
        return;
    }
    govpx_oracle_state.out = fopen(path, "wb");
    if (govpx_oracle_state.out == NULL) {
        fprintf(stderr,
                "govpx oracle: failed to open %s for writing; "
                "trace disabled\n", path);
        govpx_oracle_state.enabled = 0;
        return;
    }
    govpx_oracle_state.enabled = 1;
}

static void govpx_oracle_ensure_capacity(int needed) {
    if (needed <= govpx_oracle_state.mb_capacity) {
        return;
    }
    free(govpx_oracle_state.mb_rows);
    govpx_oracle_state.mb_rows =
        (govpx_mb_row_t *)calloc((size_t)needed, sizeof(govpx_mb_row_t));
    govpx_oracle_state.mb_capacity =
        (govpx_oracle_state.mb_rows != NULL) ? needed : 0;
}

static const char *govpx_oracle_mode_name(int mode) {
    switch (mode) {
        case DC_PRED: return "DC_PRED";
        case V_PRED: return "V_PRED";
        case H_PRED: return "H_PRED";
        case TM_PRED: return "TM_PRED";
        case B_PRED: return "B_PRED";
        case NEARESTMV: return "NEARESTMV";
        case NEARMV: return "NEARMV";
        case ZEROMV: return "ZEROMV";
        case NEWMV: return "NEWMV";
        case SPLITMV: return "SPLITMV";
        default: return "MODE_UNKNOWN";
    }
}

static const char *govpx_oracle_ref_name(int ref) {
    switch (ref) {
        case INTRA_FRAME: return "INTRA_FRAME";
        case LAST_FRAME: return "LAST_FRAME";
        case GOLDEN_FRAME: return "GOLDEN_FRAME";
        case ALTREF_FRAME: return "ALTREF_FRAME";
        default: return "REF_UNKNOWN";
    }
}

/* Capture per-MB state. Called from encodeframe.c immediately after the
 * macroblock has been encoded and tokenized, while xd->eobs is still
 * populated for the just-finished MB. Subsequent calls for the same
 * (mb_row, mb_col) within a recoded frame overwrite the buffer entry, so
 * only the final accepted attempt is flushed. */
void govpx_oracle_capture_mb(struct VP8_COMP *cpi, int mb_row, int mb_col) {
    VP8_COMMON *cm;
    MACROBLOCKD *xd;
    int idx;
    int i;
    int j;
    int sum;
    govpx_mb_row_t *row;

    govpx_oracle_init();
    if (!govpx_oracle_state.enabled) {
        return;
    }
    cm = &cpi->common;
    xd = &cpi->mb.e_mbd;
    if (cm->frame_type == KEY_FRAME) {
        /* govpx-side schema only emits per-MB rows for inter frames. */
        return;
    }
    govpx_oracle_state.mb_cols = cm->mb_cols;
    govpx_oracle_ensure_capacity(cm->mb_rows * cm->mb_cols);
    if (govpx_oracle_state.mb_rows == NULL) {
        return;
    }
    idx = mb_row * cm->mb_cols + mb_col;
    row = &govpx_oracle_state.mb_rows[idx];
    row->valid = 1;
    row->segment_id = xd->mode_info_context->mbmi.segment_id;
    row->mode = xd->mode_info_context->mbmi.mode;
    row->ref_frame = xd->mode_info_context->mbmi.ref_frame;
    row->mv_row = xd->mode_info_context->mbmi.mv.as_mv.row;
    row->mv_col = xd->mode_info_context->mbmi.mv.as_mv.col;
    row->skip = xd->mode_info_context->mbmi.mb_skip_coeff;
    sum = 0;
    for (i = 0; i < 25; ++i) {
        unsigned char e = (unsigned char)xd->eobs[i];
        row->eobs[i] = e;
        sum += (int)e;
        for (j = 0; j < 16; ++j) {
            row->qcoeff[i][j] = xd->block[i].qcoeff[j];
        }
    }
    row->eob_sum = sum;
    /* Improved-MV trace: only NEWMV uses the vp8_mv_pred predictor; for
     * other modes the per-MB row keeps the pre-extension defaults that
     * mirror govpx's oracleTraceMBRow zero-state. The slot for the chosen
     * ref is read here and ALL four slots are cleared so a stale slot
     * from a previous candidate ref (or a previous MB) cannot leak. */
    if (row->ref_frame >= 1 && row->ref_frame <= 3 && row->mode == NEWMV &&
        govpx_improved_mv_slots[row->ref_frame].valid) {
        const govpx_improved_mv_slot_t *s =
            &govpx_improved_mv_slots[row->ref_frame];
        row->improved_mv_start = 1;
        row->improved_mv_near_sadidx = s->near_sadidx;
        row->improved_mv_row = s->mvp_row;
        row->improved_mv_col = s->mvp_col;
        row->improved_mv_sr = s->sr;
    } else {
        row->improved_mv_start = 0;
        row->improved_mv_near_sadidx = -1;
        row->improved_mv_row = 0;
        row->improved_mv_col = 0;
        row->improved_mv_sr = -1;
    }
    govpx_oracle_clear_improved_mv_slots();
}

/* Emit per-frame and accumulated per-MB rows. Called from bitstream.c at
 * the tail of vp8_pack_bitstream so size_bytes reflects the final packed
 * frame and so per-MB rows are flushed only for the accepted recode
 * attempt. */
void govpx_oracle_emit_frame(struct VP8_COMP *cpi, size_t frame_size) {
    VP8_COMMON *cm;
    MACROBLOCKD *xd;
    YV12_BUFFER_CONFIG *ref;
    unsigned int y_adler, u_adler, v_adler;
    unsigned int coef_probs_adler;
    unsigned int ymode_probs_adler;
    unsigned int uv_mode_probs_adler;
    unsigned int mv_probs_adler;
    unsigned char mv_probs_buf[2 * 19];
    int mb_total;
    int i;
    int refresh_entropy_probs;
    int default_coef_reset;
    FILE *out;

    govpx_oracle_init();
    if (!govpx_oracle_state.enabled || govpx_oracle_state.out == NULL) {
        return;
    }
    cm = &cpi->common;
    xd = &cpi->mb.e_mbd;
    out = govpx_oracle_state.out;
    /* Visible region of the LAST reference, matching govpx semantics. */
    ref = &cm->yv12_fb[cm->lst_fb_idx];
    y_adler = govpx_plane_adler32(ref->y_buffer, ref->y_crop_width,
                                  ref->y_crop_height, ref->y_stride);
    u_adler = govpx_plane_adler32(ref->u_buffer, ref->uv_crop_width,
                                  ref->uv_crop_height, ref->uv_stride);
    v_adler = govpx_plane_adler32(ref->v_buffer, ref->uv_crop_width,
                                  ref->uv_crop_height, ref->uv_stride);
    /* Capture the refresh-entropy-probs decision after vp8_pack_bitstream's
     * error-resilient override (bitstream.c around line 1226). The default
     * coef-count/probs reset fires on key frames when error-resilient
     * partitions are enabled and refresh_entropy_probs is forced to 1; the
     * reset itself is done by vp8_setup_key_frame -> vp8_default_coef_probs
     * earlier in the encode pipeline. The flag exposes the gate so govpx
     * parity tests can confirm the same branch fired. */
    refresh_entropy_probs = cm->refresh_entropy_probs ? 1 : 0;
    default_coef_reset =
        ((cpi->oxcf.error_resilient_mode & VPX_ERROR_RESILIENT_PARTITIONS) &&
         cm->frame_type == KEY_FRAME)
            ? 1
            : 0;
    /* Probability-state digests. coef_probs is the full 4x8x3x11 table;
     * ymode/uv_mode use the inter-frame mode probs (cm->fc.ymode_prob /
     * cm->fc.uv_mode_prob). MV probs combine both components (row + col)
     * so a single hash detects either-side drift. The govpx side mirrors
     * each digest from `e.coefProbs`, `e.modeProbs.YMode`,
     * `e.modeProbs.UVMode`, and `e.modeProbs.MV[0..1]` respectively. */
    coef_probs_adler = govpx_buf_adler32(
        (const unsigned char *)&cm->fc.coef_probs[0][0][0][0],
        sizeof(cm->fc.coef_probs));
    ymode_probs_adler = govpx_buf_adler32(
        (const unsigned char *)&cm->fc.ymode_prob[0],
        sizeof(cm->fc.ymode_prob));
    uv_mode_probs_adler = govpx_buf_adler32(
        (const unsigned char *)&cm->fc.uv_mode_prob[0],
        sizeof(cm->fc.uv_mode_prob));
    /* MV probs: pack components 0 and 1 into a contiguous buffer so the
     * digest is order-independent of struct padding inside MV_CONTEXT. */
    for (i = 0; i < 19; ++i) {
        mv_probs_buf[i]      = (unsigned char)cm->fc.mvc[0].prob[i];
        mv_probs_buf[19 + i] = (unsigned char)cm->fc.mvc[1].prob[i];
    }
    mv_probs_adler = govpx_buf_adler32(mv_probs_buf, sizeof(mv_probs_buf));
    fprintf(out,
            "{\"type\":\"frame\","
            "\"frame_index\":%llu,"
            "\"frame_type\":\"%s\","
            "\"q_index\":%d,"
            "\"base_q_index\":%d,"
            "\"loop_filter_level\":%d,"
            "\"sharpness_level\":%d,"
            "\"ref_lf_deltas\":[%d,%d,%d,%d],"
            "\"mode_lf_deltas\":[%d,%d,%d,%d],"
            "\"mode_ref_lf_delta_enabled\":%s,"
            "\"mode_ref_lf_delta_update\":%s,"
            "\"refresh_last\":%s,"
            "\"refresh_golden\":%s,"
            "\"refresh_altref\":%s,"
            "\"sign_bias_golden\":%s,"
            "\"sign_bias_altref\":%s,"
            "\"segmentation_enabled\":%s,"
            "\"refresh_entropy_probs\":%s,"
            "\"default_coef_reset\":%s,"
            "\"y_adler32\":%u,"
            "\"u_adler32\":%u,"
            "\"v_adler32\":%u,"
            "\"coef_probs_adler\":%u,"
            "\"ymode_probs_adler\":%u,"
            "\"uv_mode_probs_adler\":%u,"
            "\"mv_probs_adler\":%u,"
            "\"prob_intra_coded\":%d,"
            "\"prob_last_coded\":%d,"
            "\"prob_gf_coded\":%d,"
            "\"size_bytes\":%zu}\n",
            govpx_oracle_state.frame_index,
            cm->frame_type == KEY_FRAME ? "key" : "inter",
            cm->base_qindex,
            cm->base_qindex,
            cm->filter_level,
            cm->sharpness_level,
            (int)xd->ref_lf_deltas[0], (int)xd->ref_lf_deltas[1],
            (int)xd->ref_lf_deltas[2], (int)xd->ref_lf_deltas[3],
            (int)xd->mode_lf_deltas[0], (int)xd->mode_lf_deltas[1],
            (int)xd->mode_lf_deltas[2], (int)xd->mode_lf_deltas[3],
            xd->mode_ref_lf_delta_enabled ? "true" : "false",
            xd->mode_ref_lf_delta_update ? "true" : "false",
            cm->refresh_last_frame ? "true" : "false",
            cm->refresh_golden_frame ? "true" : "false",
            cm->refresh_alt_ref_frame ? "true" : "false",
            cm->ref_frame_sign_bias[GOLDEN_FRAME] ? "true" : "false",
            cm->ref_frame_sign_bias[ALTREF_FRAME] ? "true" : "false",
            xd->segmentation_enabled ? "true" : "false",
            refresh_entropy_probs ? "true" : "false",
            default_coef_reset ? "true" : "false",
            y_adler, u_adler, v_adler,
            coef_probs_adler,
            ymode_probs_adler,
            uv_mode_probs_adler,
            mv_probs_adler,
            cpi->prob_intra_coded,
            cpi->prob_last_coded,
            cpi->prob_gf_coded,
            frame_size);
    /* Flush per-MB rows captured during encode_mb_row (inter frames only). */
    if (cm->frame_type != KEY_FRAME && govpx_oracle_state.mb_rows != NULL) {
        mb_total = cm->mb_rows * cm->mb_cols;
        for (i = 0; i < mb_total; ++i) {
            govpx_mb_row_t *r = &govpx_oracle_state.mb_rows[i];
            int mb_row, mb_col, j, k;
            if (!r->valid) {
                continue;
            }
            mb_row = i / cm->mb_cols;
            mb_col = i % cm->mb_cols;
            fprintf(out,
                    "{\"type\":\"mb\","
                    "\"frame_index\":%llu,"
                    "\"mb_row\":%d,\"mb_col\":%d,"
                    "\"segment_id\":%d,"
                    "\"mode\":\"%s\","
                    "\"ref_frame\":\"%s\","
                    "\"mv_row\":%d,\"mv_col\":%d,"
                    "\"skip\":%s,"
                    "\"eob\":[",
                    govpx_oracle_state.frame_index,
                    mb_row, mb_col,
                    r->segment_id,
                    govpx_oracle_mode_name(r->mode),
                    govpx_oracle_ref_name(r->ref_frame),
                    r->mv_row, r->mv_col,
                    r->skip ? "true" : "false");
            for (j = 0; j < 25; ++j) {
                fprintf(out, "%s%u", j == 0 ? "" : ",",
                        (unsigned int)r->eobs[j]);
            }
            fprintf(out,
                    "],\"eob_sum\":%d,"
                    "\"qcoeff\":[",
                    r->eob_sum);
            for (j = 0; j < 25; ++j) {
                fprintf(out, "%s[", j == 0 ? "" : ",");
                for (k = 0; k < 16; ++k) {
                    fprintf(out, "%s%d", k == 0 ? "" : ",",
                            (int)r->qcoeff[j][k]);
                }
                fprintf(out, "]");
            }
            fprintf(out,
                    "],"
                    "\"improved_mv_start\":%s,"
                    "\"improved_mv_near_sadidx\":%d,"
                    "\"improved_mv_row\":%d,"
                    "\"improved_mv_col\":%d,"
                    "\"improved_mv_sr\":%d}\n",
                    r->improved_mv_start ? "true" : "false",
                    r->improved_mv_near_sadidx,
                    r->improved_mv_row,
                    r->improved_mv_col,
                    r->improved_mv_sr);
            r->valid = 0;
        }
    }
    fflush(out);
    govpx_oracle_state.frame_index++;
}

/* Per-frame recode-loop counter. Reset by govpx_oracle_emit_rate after the
 * row is flushed, incremented from the top of encode_frame_to_data_rate's
 * recode loop body. The counter lives here so the patch only touches
 * onyx_if.c with two narrow anchor edits. */
static int govpx_recode_iter_count;

void govpx_oracle_recode_iter(void) {
    govpx_recode_iter_count++;
}

/* Emit per-frame rate-control state and (when the recode loop ran more than
 * once) a "recode" row capturing loop count, final Q, and an inferred reason
 * for why the loop terminated. Called from encode_frame_to_data_rate just
 * before vp8_pack_bitstream so cpi state reflects the accepted attempt. */
void govpx_oracle_emit_rate(struct VP8_COMP *cpi, int final_q) {
    VP8_COMMON *cm;
    FILE *out;
    const char *reason;

    govpx_oracle_init();
    if (!govpx_oracle_state.enabled || govpx_oracle_state.out == NULL) {
        govpx_recode_iter_count = 0;
        return;
    }
    cm = &cpi->common;
    out = govpx_oracle_state.out;
    fprintf(out,
            "{\"type\":\"rate\","
            "\"frame_index\":%llu,"
            "\"frame_type\":\"%s\","
            "\"q_index\":%d,"
            "\"active_worst_quality\":%d,"
            "\"active_best_quality\":%d,"
            "\"buffer_level\":%lld,"
            "\"total_byte_count\":%lld,"
            "\"projected_frame_size\":%d,"
            "\"this_frame_target\":%d,"
            "\"kf_overspend_bits\":%d,"
            "\"gf_overspend_bits\":%d,"
            "\"zbin_over_quant\":%d}\n",
            govpx_oracle_state.frame_index,
            cm->frame_type == KEY_FRAME ? "key" : "inter",
            final_q,
            cpi->active_worst_quality,
            cpi->active_best_quality,
            (long long)cpi->buffer_level,
            (long long)cpi->total_byte_count,
            cpi->projected_frame_size,
            cpi->this_frame_target,
            cpi->kf_overspend_bits,
            cpi->gf_overspend_bits,
            cpi->mb.zbin_over_quant);
    if (govpx_recode_iter_count > 1) {
        if (cpi->is_src_frame_alt_ref) {
            reason = "altref_src";
        } else if (cm->frame_type == KEY_FRAME && cpi->this_key_frame_forced) {
            reason = "kf_forced_quality";
        } else {
            reason = "size_recode";
        }
        fprintf(out,
                "{\"type\":\"recode\","
                "\"frame_index\":%llu,"
                "\"loop_count\":%d,"
                "\"final_q\":%d,"
                "\"reason\":\"%s\"}\n",
                govpx_oracle_state.frame_index,
                govpx_recode_iter_count,
                final_q,
                reason);
    }
    fflush(out);
    govpx_recode_iter_count = 0;
}
GOVPX_ORACLE_TU

	# (2) Add extern declarations + the per-MB capture call to encodeframe.c.
	# Anchor: the line "extern void vp8_stuff_mb(...)" in v1.16.0
	# uniquely identifies the top of the file's extern block.
	awk '
		BEGIN { inserted_decl = 0; inserted_call = 0 }
		!inserted_decl && /^extern void vp8_stuff_mb\(/ {
			print "extern void govpx_oracle_capture_mb(struct VP8_COMP *cpi, int mb_row, int mb_col);"
			print $0
			inserted_decl = 1
			next
		}
		!inserted_call && /^    segment_counts\[xd->mode_info_context->mbmi\.segment_id\]\+\+;$/ {
			print $0
			print ""
			print "    /* govpx oracle: capture per-MB decision before xd advances. */"
			print "    govpx_oracle_capture_mb(cpi, mb_row, mb_col);"
			inserted_call = 1
			next
		}
		{ print }
		END {
			if (!inserted_decl) {
				print "build_vpxenc_oracle.sh: anchor missing in encodeframe.c (extern decl)" > "/dev/stderr"
				exit 2
			}
			if (!inserted_call) {
				print "build_vpxenc_oracle.sh: anchor missing in encodeframe.c (per-MB call)" > "/dev/stderr"
				exit 2
			}
		}
	' "$src_dir/vp8/encoder/encodeframe.c" > "$src_dir/vp8/encoder/encodeframe.c.tmp"
	mv "$src_dir/vp8/encoder/encodeframe.c.tmp" "$src_dir/vp8/encoder/encodeframe.c"

	# (3) Add extern declaration + the per-frame emit call to bitstream.c.
	# Anchor for extern: '#include "defaultcoefcounts.h"' (only place that
	# appears in v1.16.0). Anchor for the call: the closing brace of
	# vp8_pack_bitstream is preceded by '#endif' immediately followed by
	# '}'. Match the LAST '#endif' followed by '}' in the file to find
	# the function tail.
	awk '
		BEGIN { inserted_decl = 0 }
		!inserted_decl && /^#include "defaultcoefcounts\.h"$/ {
			print $0
			print ""
			print "extern void govpx_oracle_emit_frame(struct VP8_COMP *cpi, size_t frame_size);"
			inserted_decl = 1
			next
		}
		{ print }
		END {
			if (!inserted_decl) {
				print "build_vpxenc_oracle.sh: anchor missing in bitstream.c (extern decl)" > "/dev/stderr"
				exit 2
			}
		}
	' "$src_dir/vp8/encoder/bitstream.c" > "$src_dir/vp8/encoder/bitstream.c.tmp"
	mv "$src_dir/vp8/encoder/bitstream.c.tmp" "$src_dir/vp8/encoder/bitstream.c"

	# Inject the per-frame emit just before the final closing brace of
	# vp8_pack_bitstream. The function ends at the last "^}" preceded by
	# "^#endif" in the file (this pattern is stable in v1.16.0).
	python3 - "$src_dir/vp8/encoder/bitstream.c" <<'GOVPX_BITSTREAM_TAIL_PY'
import sys, io, re
path = sys.argv[1]
with io.open(path, 'r', encoding='utf-8') as f:
    text = f.read()
# Find the start of vp8_pack_bitstream and rewrite only the matching closing
# brace at the function's end. We look for a "^}" that is the first standalone
# closing brace following the "void vp8_pack_bitstream(" header line.
header = re.search(r'^void\s+vp8_pack_bitstream\s*\(', text, flags=re.MULTILINE)
if not header:
    sys.stderr.write('build_vpxenc_oracle.sh: vp8_pack_bitstream header missing\n')
    sys.exit(2)
# Walk braces to find the matching closer.
i = header.start()
brace_open = text.find('{', i)
if brace_open == -1:
    sys.stderr.write('build_vpxenc_oracle.sh: vp8_pack_bitstream { missing\n')
    sys.exit(2)
depth = 1
j = brace_open + 1
while j < len(text) and depth > 0:
    c = text[j]
    if c == '{':
        depth += 1
    elif c == '}':
        depth -= 1
        if depth == 0:
            break
    j += 1
if depth != 0:
    sys.stderr.write('build_vpxenc_oracle.sh: vp8_pack_bitstream } missing\n')
    sys.exit(2)
sentinel = '/* govpx oracle: emit per-frame row + buffered per-MB rows. */'
if sentinel in text:
    sys.exit(0)  # already patched
insertion = '  ' + sentinel + '\n  govpx_oracle_emit_frame(cpi, *size);\n'
text = text[:j] + insertion + text[j:]
with io.open(path, 'w', encoding='utf-8') as f:
    f.write(text)
GOVPX_BITSTREAM_TAIL_PY

	# (3.5) Add extern declarations + the per-frame rate/recode emit + the
	# per-iteration counter increment to onyx_if.c. Two anchor edits:
	#   - just after "  do {\n    vpx_clear_system_state();" inside
	#     encode_frame_to_data_rate (the recode loop body).
	#   - immediately before the unique
	#     "vp8_pack_bitstream(cpi, dest, dest_end, size);" call site.
	# The extern declarations go at the top of the encoder section, right
	# after the existing "extern void vp8cx_init_quantizer(...)" header which
	# is unique in v1.16.0.
	python3 - "$src_dir/vp8/encoder/onyx_if.c" <<'GOVPX_ONYX_PY'
import sys, io
path = sys.argv[1]
with io.open(path, 'r', encoding='utf-8') as f:
    text = f.read()
sentinel = '/* govpx oracle: rate/recode emit hook. */'
if sentinel in text:
    sys.exit(0)  # already patched
# Anchor 1: inject extern declarations directly after the "extern void
# vp8cx_init_quantizer" line (unique in v1.16.0). If absent, fall back to
# inserting after the first "#include \"onyx_int.h\"" so the patch still
# compiles.
decl = ('extern void govpx_oracle_recode_iter(void);\n'
        'extern void govpx_oracle_emit_rate(struct VP8_COMP *cpi, int final_q);\n')
ext_anchor = 'extern void vp8cx_init_quantizer(VP8_COMP *cpi);'
if ext_anchor in text:
    text = text.replace(ext_anchor, ext_anchor + '\n' + decl, 1)
else:
    inc_anchor = '#include "vp8/encoder/onyx_int.h"'
    if inc_anchor not in text:
        sys.stderr.write('build_vpxenc_oracle.sh: extern anchor missing in onyx_if.c\n')
        sys.exit(2)
    text = text.replace(inc_anchor, inc_anchor + '\n\n' + decl, 1)
# Anchor 2: increment the per-frame loop counter at the top of the recode
# loop body inside encode_frame_to_data_rate. The unique pattern is
# "  do {\n    vpx_clear_system_state();\n\n    vp8_set_quantizer(cpi, Q);"
# (the only do-loop in the file that pairs vpx_clear_system_state() with
# vp8_set_quantizer at this indentation).
loop_anchor = ('  do {\n'
               '    vpx_clear_system_state();\n'
               '\n'
               '    vp8_set_quantizer(cpi, Q);')
loop_replacement = ('  do {\n'
                    '    /* govpx oracle: count recode-loop iterations. */\n'
                    '    govpx_oracle_recode_iter();\n'
                    '    vpx_clear_system_state();\n'
                    '\n'
                    '    vp8_set_quantizer(cpi, Q);')
if loop_anchor not in text:
    sys.stderr.write('build_vpxenc_oracle.sh: recode-loop anchor missing in onyx_if.c\n')
    sys.exit(2)
text = text.replace(loop_anchor, loop_replacement, 1)
# Anchor 3: emit rate/recode rows immediately before vp8_pack_bitstream.
pack_anchor = '  vp8_pack_bitstream(cpi, dest, dest_end, size);'
if pack_anchor not in text:
    sys.stderr.write('build_vpxenc_oracle.sh: pack_bitstream anchor missing in onyx_if.c\n')
    sys.exit(2)
pack_replacement = ('  ' + sentinel + '\n'
                    '  govpx_oracle_emit_rate(cpi, Q);\n'
                    + pack_anchor)
text = text.replace(pack_anchor, pack_replacement, 1)
with io.open(path, 'w', encoding='utf-8') as f:
    f.write(text)
GOVPX_ONYX_PY

	# (3.6) Instrument vp8_mv_pred in rdopt.c to record the improved-MV
	# predictor result per (mb, ref). The matched slot index `near_sadidx[i]`
	# at the chosen rank `i` is the libvpx-side analogue of govpx's
	# `interFrameSearchStart.nearSADIndex`. The patch:
	#   - injects an extern declaration just before vp8_mv_pred,
	#   - threads a local `govpx_match_slot` through the search loop,
	#   - records (ref_frame, slot, mvp.row, mvp.col, sr) immediately after
	#     vp8_clamp_mv2 on the function's tail.
	# All edits are guarded by sentinel strings so the patch is idempotent.
	python3 - "$src_dir/vp8/encoder/rdopt.c" <<'GOVPX_RDOPT_PY'
import sys, io
path = sys.argv[1]
with io.open(path, 'r', encoding='utf-8') as f:
    text = f.read()
sentinel = '/* govpx oracle: improved-MV predictor record. */'
if sentinel in text:
    sys.exit(0)  # already patched
# Anchor 1: inject extern just before the vp8_mv_pred definition. The
# definition opens with this exact line in v1.16.0.
def_anchor = ('void vp8_mv_pred(VP8_COMP *cpi, MACROBLOCKD *xd, '
              'const MODE_INFO *here,\n'
              '                 int_mv *mvp, int refframe, '
              'int *ref_frame_sign_bias, int *sr,\n'
              '                 int near_sadidx[]) {\n')
extern_decl = ('extern void govpx_oracle_record_improved_mv(int ref_frame,\n'
               '                                            int near_sadidx,\n'
               '                                            int mvp_row,\n'
               '                                            int mvp_col,\n'
               '                                            int sr);\n')
if def_anchor not in text:
    sys.stderr.write('build_vpxenc_oracle.sh: vp8_mv_pred def anchor missing\n')
    sys.exit(2)
text = text.replace(def_anchor,
                    extern_decl + '\n' + def_anchor + \
                    '  /* govpx oracle: matched slot index, -1 == median fallback. */\n' \
                    '  int govpx_match_slot = -1;\n',
                    1)
# Anchor 2: inside the search loop, capture the slot at the matched break.
loop_anchor = ('          mv.as_int = near_mvs[near_sadidx[i]].as_int;\n'
               '          find = 1;\n'
               '          if (i < 3) {\n'
               '            *sr = 3;\n'
               '          } else {\n'
               '            *sr = 2;\n'
               '          }\n'
               '          break;\n')
loop_replacement = ('          mv.as_int = near_mvs[near_sadidx[i]].as_int;\n'
                    '          find = 1;\n'
                    '          /* govpx oracle: record matched near_sadidx slot. */\n'
                    '          govpx_match_slot = near_sadidx[i];\n'
                    '          if (i < 3) {\n'
                    '            *sr = 3;\n'
                    '          } else {\n'
                    '            *sr = 2;\n'
                    '          }\n'
                    '          break;\n')
if loop_anchor not in text:
    sys.stderr.write('build_vpxenc_oracle.sh: vp8_mv_pred loop anchor missing\n')
    sys.exit(2)
text = text.replace(loop_anchor, loop_replacement, 1)
# Anchor 3: at the function tail, after vp8_clamp_mv2, emit the record
# call. The "Set up return values" comment is unique to this function in
# v1.16.0.
tail_anchor = ('  /* Set up return values */\n'
               '  mvp->as_int = mv.as_int;\n'
               '  vp8_clamp_mv2(mvp, xd);\n'
               '}\n')
tail_replacement = ('  /* Set up return values */\n'
                    '  mvp->as_int = mv.as_int;\n'
                    '  vp8_clamp_mv2(mvp, xd);\n'
                    '  ' + sentinel + '\n'
                    '  govpx_oracle_record_improved_mv(here->mbmi.ref_frame,\n'
                    '                                  govpx_match_slot,\n'
                    '                                  mvp->as_mv.row,\n'
                    '                                  mvp->as_mv.col,\n'
                    '                                  *sr);\n'
                    '}\n')
if tail_anchor not in text:
    sys.stderr.write('build_vpxenc_oracle.sh: vp8_mv_pred tail anchor missing\n')
    sys.exit(2)
text = text.replace(tail_anchor, tail_replacement, 1)
with io.open(path, 'w', encoding='utf-8') as f:
    f.write(text)
GOVPX_RDOPT_PY

	# (4) Wire the new TU into the makefile.
	if ! grep -q 'encoder/oracle_trace\.c' "$src_dir/vp8/vp8cx.mk"; then
		# Insert immediately after the encoder/copy_c.c line so the new
		# entry sits next to similarly-tiny utility files.
		awk '
			BEGIN { inserted = 0 }
			!inserted && /^VP8_CX_SRCS-yes \+= encoder\/copy_c\.c$/ {
				print $0
				print "VP8_CX_SRCS-yes += encoder/oracle_trace.c"
				inserted = 1
				next
			}
			{ print }
			END {
				if (!inserted) {
					print "build_vpxenc_oracle.sh: anchor missing in vp8cx.mk" > "/dev/stderr"
					exit 2
				}
			}
		' "$src_dir/vp8/vp8cx.mk" > "$src_dir/vp8/vp8cx.mk.tmp"
		mv "$src_dir/vp8/vp8cx.mk.tmp" "$src_dir/vp8/vp8cx.mk"
	fi

	touch "$patch_stamp"
fi

if [ ! -x "$src_dir/vpxenc" ] || [ "$current_config" != "$want_config" ]; then
	(
		cd "$src_dir"
		./configure \
			--disable-docs \
			--disable-unit-tests \
			--disable-debug \
			--disable-gprof \
			--enable-optimizations \
			--disable-vp9 \
			--disable-vp9-highbitdepth \
			--enable-vp8_encoder \
			--enable-vp8_decoder \
			--enable-postproc \
			--enable-error-concealment \
			--enable-vp8
		make -j"$jobs"
	)
	printf '%s\n' "$want_config" > "$config_stamp"
fi

cp "$src_dir/vpxenc" "$vpxenc_oracle_bin"
chmod +x "$vpxenc_oracle_bin"
printf '%s\n' "$vpxenc_oracle_bin"
