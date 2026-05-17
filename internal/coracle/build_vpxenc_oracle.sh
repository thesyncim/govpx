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
want_config="v1.16.0-vp8-vpxenc-oracle-trace-2026-05-16-mb-rate-entropy-split-lf-trial-full-v1-fast-pre-y-sse-r12-c-bmodes-inter-picker-entry-iter-outcome-r12d-speed-v7-key-boundary-niavqi-forcemaxqp-rcf
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
#include "vpx_ports/vpx_timer.h"
#include "vp8/common/blockd.h"
#include "vp8/common/onyxc_int.h"
#include "vp8/encoder/onyx_int.h"
#include "vp8/encoder/bitstream.h"

/* Per-process accumulator of microseconds spent inside oracle trace
 * emit/capture functions. The enclosing encoder reads and clears this
 * after each frame via govpx_oracle_trace_take_usec() and subtracts
 * the result from the wall-clock duration that feeds the realtime
 * auto-speed picker (vp8/encoder/rdopt.c:261 vp8_auto_select_speed,
 * driven from vp8/encoder/onyx_if.c:5120-5165 in this oracle tree).
 *
 * Without this accounting, the fopen/fwrite/fflush latency on the
 * trace channel inflates cpi->avg_encode_time / cpi->avg_pick_mode_time
 * and shifts the libvpx Speed trajectory, so the traced binary's
 * bitstream diverges from the untraced one for the same config. Each
 * GOVPX_TRACE_BEGIN/END pair below brackets the body of an
 * `enabled`-gated trace function with a vpx_usec_timer so the trace's
 * own runtime is subtracted before duration enters the picker. */
static int64_t govpx_oracle_trace_usec = 0;

int64_t govpx_oracle_trace_take_usec(void) {
    int64_t v = govpx_oracle_trace_usec;
    govpx_oracle_trace_usec = 0;
    return v;
}

#define GOVPX_TRACE_BEGIN()                              \
    struct vpx_usec_timer govpx__trace_t;                \
    vpx_usec_timer_start(&govpx__trace_t)
#define GOVPX_TRACE_END()                                \
    do {                                                 \
        vpx_usec_timer_mark(&govpx__trace_t);            \
        govpx_oracle_trace_usec +=                       \
            vpx_usec_timer_elapsed(&govpx__trace_t);     \
    } while (0)

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
    int uv_mode;
    int partition;
    int block_mv_row[16];
    int block_mv_col[16];
    int b_modes_valid;
    int b_modes[16];
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
    /* Per-MB picker rate (return value of vp8cx_encode_inter_macroblock /
     * vp8cx_encode_intra_macroblock) and the running totalrate accumulator
     * after this MB's contribution has been added. Mirrors the govpx-side
     * oracleTraceMBRow.MBRate / AggregatedRate captured at
     * libvpxAddProjectedMacroblockRate in encoder_reconstruct.go. Both
     * scalars are pre-shift (libvpx units, not bits): the final
     * `cpi->projected_frame_size = totalrate >> 8` is applied once after
     * all rows are processed. */
    int mb_rate;
    int aggregated_rate;
} govpx_mb_row_t;

typedef struct {
    int valid;
    int mb_row;
    int mb_col;
    const char *picker;
    int mode_index;
    int mode;
    int ref_slot;
    int ref_frame;
    int threshold;
    int best_score_before;
    int best_yrd_before;
    long long best_sse_before;
    int became_best;
    int loop_break;
    int score;
    int yrd;
    int rate;
    int rate_y;
    int rate_uv;
    int distortion;
    int distortion_uv;
    long long sse;
    int skip;
    int mv_row;
    int mv_col;
    int improved_mv_start;
    int improved_mv_near_sadidx;
    int improved_mv_row;
    int improved_mv_col;
    int improved_mv_sr;
} govpx_inter_candidate_row_t;

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

/* Forward declaration: govpx_oracle_state lives below alongside the rest
 * of the trace state struct. govpx_oracle_record_improved_mv runs on the
 * per-MB hot path (vp8_mv_pred) and must early-out when the trace channel
 * is disabled to avoid the timer-account overhead in the production
 * binary; the early-out path needs to read the `enabled` flag, so the
 * struct + variable are forward-declared here and defined below. */
typedef struct govpx_oracle_state_struct govpx_oracle_state_t;
struct govpx_oracle_state_struct {
    FILE *out;
    int initialized;
    int enabled;
    govpx_mb_row_t *mb_rows;
    int mb_capacity;
    int mb_cols;
    govpx_inter_candidate_row_t *candidate_rows;
    int candidate_capacity;
    int candidate_count;
    unsigned long long frame_index;
};
static govpx_oracle_state_t govpx_oracle_state;

void govpx_oracle_record_improved_mv(int ref_frame, int near_sadidx,
                                     int mvp_row, int mvp_col, int sr) {
    if (ref_frame < 1 || ref_frame > 3) {
        /* INTRA_FRAME (0) and out-of-range refs are ignored: vp8_mv_pred
         * early-returns for INTRA, and the calling code never sets a ref
         * outside [LAST_FRAME, ALTREF_FRAME]. */
        return;
    }
    /* Only account when the trace channel is live; otherwise this is a
     * pure memory write that we want to leave un-timed (the function is
     * an unconditional path inside vp8_mv_pred, not a trace-only hook).
     * Without this gate, building the oracle binary with trace OFF would
     * still pay the timer cost on every NEWMV candidate. */
    if (!govpx_oracle_state.enabled) {
        govpx_improved_mv_slots[ref_frame].valid = 1;
        govpx_improved_mv_slots[ref_frame].near_sadidx = near_sadidx;
        govpx_improved_mv_slots[ref_frame].mvp_row = mvp_row;
        govpx_improved_mv_slots[ref_frame].mvp_col = mvp_col;
        govpx_improved_mv_slots[ref_frame].sr = sr;
        return;
    }
    {
        GOVPX_TRACE_BEGIN();
        govpx_improved_mv_slots[ref_frame].valid = 1;
        govpx_improved_mv_slots[ref_frame].near_sadidx = near_sadidx;
        govpx_improved_mv_slots[ref_frame].mvp_row = mvp_row;
        govpx_improved_mv_slots[ref_frame].mvp_col = mvp_col;
        govpx_improved_mv_slots[ref_frame].sr = sr;
        GOVPX_TRACE_END();
    }
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

/* govpx_oracle_state_t / govpx_oracle_state were forward-declared above so
 * govpx_oracle_record_improved_mv (on the per-MB hot path) can early-out on
 * the trace-disabled path before paying the timer-account overhead. */

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

static void govpx_oracle_ensure_candidate_capacity(int needed) {
    int new_capacity;
    govpx_inter_candidate_row_t *rows;
    if (needed <= govpx_oracle_state.candidate_capacity) {
        return;
    }
    new_capacity = govpx_oracle_state.candidate_capacity;
    if (new_capacity <= 0) {
        new_capacity = 256;
    }
    while (new_capacity < needed) {
        new_capacity *= 2;
    }
    rows = (govpx_inter_candidate_row_t *)realloc(
        govpx_oracle_state.candidate_rows,
        (size_t)new_capacity * sizeof(govpx_inter_candidate_row_t));
    if (rows == NULL) {
        return;
    }
    govpx_oracle_state.candidate_rows = rows;
    govpx_oracle_state.candidate_capacity = new_capacity;
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

static const char *govpx_oracle_bmode_name(int mode) {
    switch (mode) {
        case B_DC_PRED: return "B_DC_PRED";
        case B_TM_PRED: return "B_TM_PRED";
        case B_VE_PRED: return "B_VE_PRED";
        case B_HE_PRED: return "B_HE_PRED";
        case B_LD_PRED: return "B_LD_PRED";
        case B_RD_PRED: return "B_RD_PRED";
        case B_VR_PRED: return "B_VR_PRED";
        case B_VL_PRED: return "B_VL_PRED";
        case B_HD_PRED: return "B_HD_PRED";
        case B_HU_PRED: return "B_HU_PRED";
        case LEFT4X4: return "LEFT4X4";
        case ABOVE4X4: return "ABOVE4X4";
        case ZERO4X4: return "ZERO4X4";
        case NEW4X4: return "NEW4X4";
        default: return "B_MODE_UNKNOWN";
    }
}

void govpx_oracle_begin_attempt(void) {
    govpx_oracle_init();
    if (!govpx_oracle_state.enabled) {
        return;
    }
    GOVPX_TRACE_BEGIN();
    govpx_oracle_state.candidate_count = 0;
    govpx_oracle_clear_improved_mv_slots();
    GOVPX_TRACE_END();
}

void govpx_oracle_capture_inter_candidate(
    struct VP8_COMP *cpi, int mb_row, int mb_col, const char *picker,
    int mode_index, int ref_slot, int threshold, int best_score_before,
    int best_yrd_before, long long best_sse_before, int score, int yrd,
    int rate, int rate_y, int rate_uv, int distortion, int distortion_uv,
    long long sse, int became_best, int loop_break) {
    MACROBLOCKD *xd;
    MB_MODE_INFO *mbmi;
    int idx;
    govpx_inter_candidate_row_t *row;
    govpx_improved_mv_slot_t *slot;

    govpx_oracle_init();
    if (!govpx_oracle_state.enabled || cpi == NULL) {
        return;
    }
    GOVPX_TRACE_BEGIN();
    do {
        govpx_oracle_ensure_candidate_capacity(
            govpx_oracle_state.candidate_count + 1);
        if (govpx_oracle_state.candidate_rows == NULL ||
            govpx_oracle_state.candidate_count >=
                govpx_oracle_state.candidate_capacity) {
            break;
        }
        xd = &cpi->mb.e_mbd;
        mbmi = &xd->mode_info_context->mbmi;
        idx = govpx_oracle_state.candidate_count++;
        row = &govpx_oracle_state.candidate_rows[idx];
        memset(row, 0, sizeof(*row));
        row->valid = 1;
        row->mb_row = mb_row;
        row->mb_col = mb_col;
        row->picker = picker;
        row->mode_index = mode_index;
        row->mode = mbmi->mode;
        row->ref_slot = ref_slot;
        row->ref_frame = mbmi->ref_frame;
        row->threshold = threshold;
        row->best_score_before = best_score_before;
        row->best_yrd_before = best_yrd_before;
        row->best_sse_before = best_sse_before;
        row->became_best = became_best;
        row->loop_break = loop_break;
        row->score = score;
        row->yrd = yrd;
        row->rate = rate;
        row->rate_y = rate_y;
        row->rate_uv = rate_uv;
        row->distortion = distortion;
        row->distortion_uv = distortion_uv;
        row->sse = sse;
        row->skip = loop_break;
        if (row->ref_frame == INTRA_FRAME || row->mode == SPLITMV) {
            row->mv_row = 0;
            row->mv_col = 0;
        } else {
            row->mv_row = mbmi->mv.as_mv.row;
            row->mv_col = mbmi->mv.as_mv.col;
        }
        row->improved_mv_near_sadidx = -1;
        row->improved_mv_sr = -1;
        if (row->mode == NEWMV && row->ref_frame >= 1 && row->ref_frame <= 3) {
            slot = &govpx_improved_mv_slots[row->ref_frame];
            if (slot->valid) {
                row->improved_mv_start = 1;
                row->improved_mv_near_sadidx = slot->near_sadidx;
                row->improved_mv_row = slot->mvp_row;
                row->improved_mv_col = slot->mvp_col;
                row->improved_mv_sr = slot->sr;
            }
        }
    } while (0);
    GOVPX_TRACE_END();
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
    GOVPX_TRACE_BEGIN();
    do {
    cm = &cpi->common;
    xd = &cpi->mb.e_mbd;
    govpx_oracle_state.mb_cols = cm->mb_cols;
    govpx_oracle_ensure_capacity(cm->mb_rows * cm->mb_cols);
    if (govpx_oracle_state.mb_rows == NULL) {
        break;
    }
    idx = mb_row * cm->mb_cols + mb_col;
    row = &govpx_oracle_state.mb_rows[idx];
    row->valid = 1;
    row->segment_id = xd->mode_info_context->mbmi.segment_id;
    row->mode = xd->mode_info_context->mbmi.mode;
    row->ref_frame =
        cm->frame_type == KEY_FRAME ? INTRA_FRAME
                                    : xd->mode_info_context->mbmi.ref_frame;
    row->mv_row = xd->mode_info_context->mbmi.mv.as_mv.row;
    row->mv_col = xd->mode_info_context->mbmi.mv.as_mv.col;
    row->skip = xd->mode_info_context->mbmi.mb_skip_coeff;
    row->uv_mode = xd->mode_info_context->mbmi.uv_mode;
    row->partition = -1;
    if (cm->frame_type != KEY_FRAME &&
        xd->mode_info_context->mbmi.mode == SPLITMV) {
        row->partition = xd->mode_info_context->mbmi.partitioning;
        for (i = 0; i < 16; ++i) {
            row->block_mv_row[i] =
                xd->mode_info_context->bmi[i].mv.as_mv.row;
            row->block_mv_col[i] =
                xd->mode_info_context->bmi[i].mv.as_mv.col;
        }
    }
    row->b_modes_valid = 0;
    if (xd->mode_info_context->mbmi.mode == B_PRED) {
        /* Both keyframes (rd_pick_intra4x4mby_modes / pick_intra4x4mby_modes
         * for KF) and inter frames (rd_pick_intra4x4mby_modes called via
         * intra fallback inside vp8_pick_inter_mode's B_PRED case, plus
         * rd_pick_inter_mode's B_PRED branch) populate
         * mode_info_context->bmi[i].as_mode after the picker commits. R11-J
         * needs the inter-side dump too because the 128x128 col-7 col-edge
         * BPred divergence shows up on frame 1 (an inter frame) and the
         * earlier KEY_FRAME-only gate masked it. */
        row->b_modes_valid = 1;
        for (i = 0; i < 16; ++i) {
            row->b_modes[i] = xd->mode_info_context->bmi[i].as_mode;
        }
    }
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
    } while (0);
    GOVPX_TRACE_END();
}

/* Record the per-MB rate accumulator scalars at the same point libvpx
 * folds the picker's chosen-mode rate into `*totalrate` inside
 * encode_mb_row (vp8/encoder/encodeframe.c). `mb_rate` is the rate
 * returned by vp8cx_encode_inter_macroblock / vp8cx_encode_intra_macroblock;
 * `aggregated_rate` is the running total after this MB's contribution
 * has been added but before any clamp to INT_MAX. The pair mirrors the
 * govpx-side oracleTraceMBRow.MBRate / AggregatedRate captured by
 * libvpxAddProjectedMacroblockRate in encoder_reconstruct.go. Stored
 * onto the existing govpx_oracle_state.mb_rows slot so the per-MB JSON
 * row carries both scalars. */
void govpx_oracle_record_mb_rate(struct VP8_COMP *cpi, int mb_row, int mb_col,
                                 int mb_rate, int aggregated_rate) {
    VP8_COMMON *cm;
    int idx;
    govpx_mb_row_t *row;

    govpx_oracle_init();
    if (!govpx_oracle_state.enabled) {
        return;
    }
    GOVPX_TRACE_BEGIN();
    do {
        cm = &cpi->common;
        if (govpx_oracle_state.mb_rows == NULL) {
            break;
        }
        idx = mb_row * cm->mb_cols + mb_col;
        if (idx < 0 || idx >= cm->mb_rows * cm->mb_cols) {
            break;
        }
        row = &govpx_oracle_state.mb_rows[idx];
        row->mb_rate = mb_rate;
        row->aggregated_rate = aggregated_rate;
    } while (0);
    GOVPX_TRACE_END();
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
    GOVPX_TRACE_BEGIN();
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
    /* Flush inter-candidate rows captured during mode picking. */
    for (i = 0; i < govpx_oracle_state.candidate_count; ++i) {
        govpx_inter_candidate_row_t *r =
            &govpx_oracle_state.candidate_rows[i];
        if (!r->valid) {
            continue;
        }
        fprintf(out,
                "{\"type\":\"inter_candidate\","
                "\"frame_index\":%llu,"
                "\"mb_row\":%d,\"mb_col\":%d,"
                "\"picker\":\"%s\","
                "\"mode_index\":%d,"
                "\"mode\":\"%s\","
                "\"ref_slot\":%d,"
                "\"ref_frame\":\"%s\","
                "\"threshold\":%d,"
                "\"best_score_before\":%d,"
                "\"best_yrd_before\":%d,"
                "\"best_sse_before\":%lld,"
                "\"outcome\":\"tested\","
                "\"became_best\":%s,"
                "\"loop_break\":%s,"
                "\"score\":%d,"
                "\"yrd\":%d,"
                "\"rate\":%d,"
                "\"rate_y\":%d,"
                "\"rate_uv\":%d,"
                "\"distortion\":%d,"
                "\"distortion_uv\":%d,"
                "\"sse\":%lld,"
                "\"skip\":%s,"
                "\"mv_row\":%d,\"mv_col\":%d,"
                "\"improved_mv_start\":%s,"
                "\"improved_mv_near_sadidx\":%d,"
                "\"improved_mv_row\":%d,"
                "\"improved_mv_col\":%d,"
                "\"improved_mv_sr\":%d}\n",
                govpx_oracle_state.frame_index,
                r->mb_row, r->mb_col,
                r->picker != NULL ? r->picker : "",
                r->mode_index,
                govpx_oracle_mode_name(r->mode),
                r->ref_slot,
                govpx_oracle_ref_name(r->ref_frame),
                r->threshold,
                r->best_score_before,
                r->best_yrd_before,
                r->best_sse_before,
                r->became_best ? "true" : "false",
                r->loop_break ? "true" : "false",
                r->score,
                r->yrd,
                r->rate,
                r->rate_y,
                r->rate_uv,
                r->distortion,
                r->distortion_uv,
                r->sse,
                r->skip ? "true" : "false",
                r->mv_row, r->mv_col,
                r->improved_mv_start ? "true" : "false",
                r->improved_mv_near_sadidx,
                r->improved_mv_row,
                r->improved_mv_col,
                r->improved_mv_sr);
        r->valid = 0;
    }
    govpx_oracle_state.candidate_count = 0;
    /* Flush per-MB rows captured during encode_mb_row. */
    if (govpx_oracle_state.mb_rows != NULL) {
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
            if (cm->frame_type == KEY_FRAME) {
                fprintf(out, "],\"uv_mode\":\"%s\"",
                        govpx_oracle_mode_name(r->uv_mode));
                if (r->b_modes_valid) {
                    fprintf(out, ",\"b_modes\":[");
                    for (j = 0; j < 16; ++j) {
                        fprintf(out, "%s\"%s\"", j == 0 ? "" : ",",
                                govpx_oracle_bmode_name(r->b_modes[j]));
                    }
                    fprintf(out, "]");
                }
                fprintf(out, ",\"eob_sum\":%d,\"qcoeff\":[", r->eob_sum);
            } else {
                fprintf(out, "]");
                if (r->mode == SPLITMV) {
                    fprintf(out, ",\"partition\":%d,\"block_mv_rows\":[",
                            r->partition);
                    for (j = 0; j < 16; ++j) {
                        fprintf(out, "%s%d", j == 0 ? "" : ",",
                                r->block_mv_row[j]);
                    }
                    fprintf(out, "],\"block_mv_cols\":[");
                    for (j = 0; j < 16; ++j) {
                        fprintf(out, "%s%d", j == 0 ? "" : ",",
                                r->block_mv_col[j]);
                    }
                    fprintf(out, "]");
                }
                if (r->b_modes_valid) {
                    /* R12-C: emit per-sub-block intra mode picks for
                     * inter-frame B_PRED MBs so the diag harness can compare
                     * govpx's pickFastBPredLumaModeKF / fast picker against
                     * libvpx's pick_intra4x4mby_modes / rd_pick_intra4x4mby
                     * at the col-7 right-edge MBs on 128x128 frame 1. */
                    fprintf(out, ",\"b_modes\":[");
                    for (j = 0; j < 16; ++j) {
                        fprintf(out, "%s\"%s\"", j == 0 ? "" : ",",
                                govpx_oracle_bmode_name(r->b_modes[j]));
                    }
                    fprintf(out, "]");
                }
                fprintf(out, ",\"eob_sum\":%d,\"qcoeff\":[", r->eob_sum);
            }
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
                    "\"improved_mv_sr\":%d,"
                    "\"mb_rate\":%d,"
                    "\"aggregated_rate\":%d}\n",
                    r->improved_mv_start ? "true" : "false",
                    r->improved_mv_near_sadidx,
                    r->improved_mv_row,
                    r->improved_mv_col,
                    r->improved_mv_sr,
                    r->mb_rate,
                    r->aggregated_rate);
            r->valid = 0;
        }
    }
    fflush(out);
    govpx_oracle_state.frame_index++;
    GOVPX_TRACE_END();
}

/* Per-frame recode-loop counter. Reset by govpx_oracle_emit_rate after the
 * row is flushed, incremented from the top of encode_frame_to_data_rate's
 * recode loop body. The counter lives here so the patch only touches
 * onyx_if.c with two narrow anchor edits. */
static int govpx_recode_iter_count;

/* Emit a dropped-frame row mirroring govpx's emitOracleDroppedFrameTrace.
 * Called from encode_frame_to_data_rate at each of the three drop-decision
 * return paths in onyx_if.c (vp8_check_drop_buffer decimation, the
 * vp8_pick_frame_size buffer-underrun branch, and the post-encode
 * vp8_drop_encodedframe_overshoot branch). The libvpx-side state is fully
 * committed at the time of each call: cpi->buffer_level reflects the post-
 * drop refund/clamp accounting and cpi->force_maxqp reflects the lifecycle
 * update applied for that drop class. The frame_index is advanced AFTER
 * emitting so the next non-dropped frame's row carries the next ordinal,
 * matching govpx semantics where e.frameCount++ runs after the trace
 * emission inside the drop branch of EncodeInto. */
void govpx_oracle_emit_dropped_frame(struct VP8_COMP *cpi,
                                     const char *reason) {
    FILE *out;
    govpx_oracle_init();
    if (!govpx_oracle_state.enabled || govpx_oracle_state.out == NULL) {
        return;
    }
    GOVPX_TRACE_BEGIN();
    out = govpx_oracle_state.out;
    fprintf(out,
            "{\"type\":\"frame\","
            "\"frame_index\":%llu,"
            "\"frame_type\":\"inter\","
            "\"dropped\":true,"
            "\"force_maxqp\":%s,"
            "\"buffer_level\":%lld,"
            "\"this_frame_target\":%d,"
            "\"reason\":\"%s\"}\n",
            govpx_oracle_state.frame_index,
            cpi->force_maxqp ? "true" : "false",
            (long long)cpi->buffer_level,
            cpi->this_frame_target,
            reason != NULL ? reason : "");
    fflush(out);
    /* Reset recode counter so a previously-aborted attempt's count cannot
     * leak into the next non-dropped frame's recode row. */
    govpx_recode_iter_count = 0;
    govpx_oracle_state.frame_index++;
    GOVPX_TRACE_END();
}

void govpx_oracle_recode_iter(void) {
    if (!govpx_oracle_state.enabled) {
        govpx_recode_iter_count++;
        return;
    }
    {
        GOVPX_TRACE_BEGIN();
        govpx_recode_iter_count++;
        GOVPX_TRACE_END();
    }
}

/* Recompute the inter-frame ref-frame branch of vp8_estimate_entropy_savings
 * (vp8/encoder/bitstream.c) using the same inputs libvpx feeds at the
 * pre-pack entropy-savings subtraction point in encode_frame_to_data_rate
 * (cpi->prob_intra_coded / prob_last_coded / prob_gf_coded after
 * update_rd_ref_frame_probs ran for this frame, plus the per-frame ref-
 * frame usage histogram). Returns 0 on key frames. Used by the oracle
 * rate row to expose the entropy-savings breakdown alongside
 * projected_frame_size for parity-gap localization. */
static int govpx_oracle_ref_frame_savings(struct VP8_COMP *cpi) {
    const int *rfct;
    int rf_intra, rf_inter;
    int new_intra, new_last, new_garf;
    int oldtotal, newtotal;
    int ref_frame_cost[4];

    if (cpi == NULL || cpi->common.frame_type == KEY_FRAME) {
        return 0;
    }
    rfct = cpi->mb.count_mb_ref_frame_usage;
    rf_intra = rfct[INTRA_FRAME];
    rf_inter = rfct[LAST_FRAME] + rfct[GOLDEN_FRAME] + rfct[ALTREF_FRAME];
    if (rf_intra + rf_inter <= 0) {
        return 0;
    }
    new_intra = (rf_intra * 255) / (rf_intra + rf_inter);
    if (new_intra == 0) new_intra = 1;
    new_last = rf_inter ? (rfct[LAST_FRAME] * 255) / rf_inter : 128;
    new_garf = (rfct[GOLDEN_FRAME] + rfct[ALTREF_FRAME])
                   ? (rfct[GOLDEN_FRAME] * 255) /
                         (rfct[GOLDEN_FRAME] + rfct[ALTREF_FRAME])
                   : 128;
    vp8_calc_ref_frame_costs(ref_frame_cost, new_intra, new_last, new_garf);
    newtotal = rfct[INTRA_FRAME] * ref_frame_cost[INTRA_FRAME] +
               rfct[LAST_FRAME] * ref_frame_cost[LAST_FRAME] +
               rfct[GOLDEN_FRAME] * ref_frame_cost[GOLDEN_FRAME] +
               rfct[ALTREF_FRAME] * ref_frame_cost[ALTREF_FRAME];
    vp8_calc_ref_frame_costs(ref_frame_cost, cpi->prob_intra_coded,
                             cpi->prob_last_coded, cpi->prob_gf_coded);
    oldtotal = rfct[INTRA_FRAME] * ref_frame_cost[INTRA_FRAME] +
               rfct[LAST_FRAME] * ref_frame_cost[LAST_FRAME] +
               rfct[GOLDEN_FRAME] * ref_frame_cost[GOLDEN_FRAME] +
               rfct[ALTREF_FRAME] * ref_frame_cost[ALTREF_FRAME];
    return (oldtotal - newtotal) / 256;
}

/* Emit per-frame rate-control state and (when the recode loop ran more than
 * once) a "recode" row capturing loop count, final Q, and an inferred reason
 * for why the loop terminated. Called from encode_frame_to_data_rate just
 * before vp8_pack_bitstream so cpi state reflects the accepted attempt. */
void govpx_oracle_emit_rate(struct VP8_COMP *cpi, int final_q) {
    VP8_COMMON *cm;
    FILE *out;
    const char *reason;
    int total_savings;
    int ref_frame_savings;
    int coef_savings;

    govpx_oracle_init();
    if (!govpx_oracle_state.enabled || govpx_oracle_state.out == NULL) {
        govpx_recode_iter_count = 0;
        return;
    }
    GOVPX_TRACE_BEGIN();
    cm = &cpi->common;
    out = govpx_oracle_state.out;
    /* Re-derive the entropy-savings breakdown so the oracle stream can
     * pin which component (coefficient-prob update savings vs ref-frame
     * branch savings) drove projected_frame_size divergence. The total
     * matches what libvpx already subtracted from cpi->projected_frame_size
     * at line 3996 of onyx_if.c (vp8_estimate_entropy_savings). On every
     * inter frame where vp8_encode_frame's trailing vp8_convert_rfct_to_prob
     * hook fired (encodeframe.c around line 980 -- single-layer non-GF/AR
     * refresh, or any multi-layer case), cpi->prob_*_coded has already been
     * overwritten with the rfct-derived probabilities, so the inter-frame
     * branch of vp8_estimate_entropy_savings returns 0; the breakdown
     * pins this contract for the govpx parity test. */
    total_savings = vp8_estimate_entropy_savings(cpi);
    ref_frame_savings = govpx_oracle_ref_frame_savings(cpi);
    coef_savings = total_savings - ref_frame_savings;
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
            "\"zbin_over_quant\":%d,"
            "\"coef_savings_bits\":%d,"
            "\"ref_frame_savings_bits\":%d,"
            "\"cpi_speed\":%d,"
            "\"avg_encode_time\":%d,"
            "\"avg_pick_mode_time\":%d,"
            "\"ni_av_qi\":%d,"
            "\"ni_frames\":%d,"
            "\"ni_tot_qi\":%d,"
            "\"force_maxqp\":%d,"
            "\"frames_since_last_drop_overshoot\":%d,"
            "\"rate_correction_factor\":%f}\n",
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
            cpi->mb.zbin_over_quant,
            coef_savings,
            ref_frame_savings,
            cpi->Speed,
            cpi->avg_encode_time,
            cpi->avg_pick_mode_time,
            cpi->ni_av_qi,
            cpi->ni_frames,
            cpi->ni_tot_qi,
            cpi->force_maxqp,
            cpi->frames_since_last_drop_overshoot,
            cpi->rate_correction_factor);
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
    GOVPX_TRACE_END();
}

/* Emit a "predictor" row capturing xd->dst.{y,u,v}_buffer for MB(0,0) of
 * inter frames only. Called from encode_macroblock between
 * vp8_encode_inter16x16 and vp8_inverse_transform_mby so dst still holds
 * the pure inter predictor (no residual added yet). The plane buffer is
 * encoded as hex so the row is one parseable JSON line. Gated on
 * GOVPX_ORACLE_PREDICTOR_DUMP env var to keep the regular oracle runs
 * lean — set GOVPX_ORACLE_PREDICTOR_DUMP=1 (alongside
 * GOVPX_ORACLE_TRACE_OUT) to capture the predictor. */
static int govpx_oracle_predictor_dump_enabled(void) {
    static int cached = -1;
    const char *env;
    if (cached >= 0) {
        return cached;
    }
    env = getenv("GOVPX_ORACLE_PREDICTOR_DUMP");
    cached = (env != NULL && env[0] != '\0' && env[0] != '0') ? 1 : 0;
    return cached;
}

static void govpx_oracle_emit_plane_hex(FILE *out, const unsigned char *plane,
                                        int width, int height, int stride) {
    static const char hex[] = "0123456789abcdef";
    int row, col;
    for (row = 0; row < height; ++row) {
        const unsigned char *p = plane + (size_t)row * (size_t)stride;
        for (col = 0; col < width; ++col) {
            unsigned char b = p[col];
            fputc(hex[(b >> 4) & 0xf], out);
            fputc(hex[b & 0xf], out);
        }
    }
}

/* Internal helper: emit Y/U/V planes of an MB to the oracle stream tagged
 * with row_type. Used both for "predictor" rows (pre-residual capture) and
 * "reconstructed" rows (post-residual capture). */
static void govpx_oracle_emit_mb_planes(FILE *out, const char *row_type,
                                        unsigned long long frame_index,
                                        int mb_row, int mb_col, MACROBLOCKD *xd) {
    fprintf(out,
            "{\"type\":\"%s\","
            "\"frame_index\":%llu,"
            "\"mb_row\":%d,\"mb_col\":%d,"
            "\"plane\":\"y\","
            "\"width\":16,\"height\":16,"
            "\"hex\":\"",
            row_type, frame_index, mb_row, mb_col);
    govpx_oracle_emit_plane_hex(out, xd->dst.y_buffer, 16, 16, xd->dst.y_stride);
    fprintf(out, "\"}\n");
    fprintf(out,
            "{\"type\":\"%s\","
            "\"frame_index\":%llu,"
            "\"mb_row\":%d,\"mb_col\":%d,"
            "\"plane\":\"u\","
            "\"width\":8,\"height\":8,"
            "\"hex\":\"",
            row_type, frame_index, mb_row, mb_col);
    govpx_oracle_emit_plane_hex(out, xd->dst.u_buffer, 8, 8, xd->dst.uv_stride);
    fprintf(out, "\"}\n");
    fprintf(out,
            "{\"type\":\"%s\","
            "\"frame_index\":%llu,"
            "\"mb_row\":%d,\"mb_col\":%d,"
            "\"plane\":\"v\","
            "\"width\":8,\"height\":8,"
            "\"hex\":\"",
            row_type, frame_index, mb_row, mb_col);
    govpx_oracle_emit_plane_hex(out, xd->dst.v_buffer, 8, 8, xd->dst.uv_stride);
    fprintf(out, "\"}\n");
}

/* Predictor-only capture point: between vp8_encode_inter16x16 (which writes
 * predictor + computes residual) and vp8_inverse_transform_mby (which adds
 * the dequantized residual back). Captures only MB row 0 by default; set
 * GOVPX_ORACLE_PREDICTOR_DUMP_ALL_ROWS=1 to capture every row (e.g. when
 * tracking down a divergence in MB row 4 that affects the loop-filter
 * picker's partial-frame trial). */
static int govpx_oracle_predictor_dump_all_rows(void) {
    static int cached = -1;
    const char *env;
    if (cached >= 0) {
        return cached;
    }
    env = getenv("GOVPX_ORACLE_PREDICTOR_DUMP_ALL_ROWS");
    cached = (env != NULL && env[0] != '\0' && env[0] != '0') ? 1 : 0;
    return cached;
}

void govpx_oracle_emit_predictor(struct VP8_COMP *cpi, int mb_row, int mb_col) {
    VP8_COMMON *cm;
    MACROBLOCKD *xd;
    FILE *out;

    govpx_oracle_init();
    if (!govpx_oracle_state.enabled || govpx_oracle_state.out == NULL) {
        return;
    }
    GOVPX_TRACE_BEGIN();
    do {
        if (!govpx_oracle_predictor_dump_enabled()) {
            break;
        }
        cm = &cpi->common;
        /* Inter frames only; MB row 0 by default, all rows if requested. */
        if (cm->frame_type == KEY_FRAME) {
            break;
        }
        if (!govpx_oracle_predictor_dump_all_rows() && mb_row != 0) {
            break;
        }
        xd = &cpi->mb.e_mbd;
        out = govpx_oracle_state.out;
        govpx_oracle_emit_mb_planes(out, "predictor",
                                    govpx_oracle_state.frame_index,
                                    mb_row, mb_col, xd);
        fflush(out);
    } while (0);
    GOVPX_TRACE_END();
}

/* Reconstruction-output capture point: after the IDCT-add residual stage in
 * encode_macroblock. Captures predictor + residual at the same MB scope as
 * govpx_oracle_emit_predictor so the comparator can pinpoint whether the
 * gap is in the predictor or the residual. */
void govpx_oracle_emit_reconstructed(struct VP8_COMP *cpi, int mb_row, int mb_col) {
    VP8_COMMON *cm;
    MACROBLOCKD *xd;
    FILE *out;

    govpx_oracle_init();
    if (!govpx_oracle_state.enabled || govpx_oracle_state.out == NULL) {
        return;
    }
    GOVPX_TRACE_BEGIN();
    do {
        if (!govpx_oracle_predictor_dump_enabled()) {
            break;
        }
        cm = &cpi->common;
        if (cm->frame_type == KEY_FRAME) {
            break;
        }
        if (!govpx_oracle_predictor_dump_all_rows() && mb_row != 0) {
            break;
        }
        xd = &cpi->mb.e_mbd;
        out = govpx_oracle_state.out;
        govpx_oracle_emit_mb_planes(out, "reconstructed",
                                    govpx_oracle_state.frame_index,
                                    mb_row, mb_col, xd);
        fflush(out);
    } while (0);
    GOVPX_TRACE_END();
}

/* Capture the LAST reference plane content at the start of an inter frame's
 * encode pass, including the top/right border bytes that the chroma sub-pel
 * filter taps reach for MB(0,0..7). The capture window is 16 rows x
 * (border_left + plane_width) columns starting border_top rows before
 * row 0, so the comparator sees the same border data govpx and libvpx use
 * when filtering the first MB row. Called once per inter frame, just
 * before the first MB is encoded. */
void govpx_oracle_emit_last_ref_window(struct VP8_COMP *cpi) {
    VP8_COMMON *cm;
    YV12_BUFFER_CONFIG *ref;
    FILE *out;
    int border, uv_border;
    int y_window_h, uv_window_h;
    int y_window_w, uv_window_w;

    govpx_oracle_init();
    if (!govpx_oracle_state.enabled || govpx_oracle_state.out == NULL) {
        return;
    }
    GOVPX_TRACE_BEGIN();
    do {
    if (!govpx_oracle_predictor_dump_enabled()) {
        break;
    }
    cm = &cpi->common;
    if (cm->frame_type == KEY_FRAME) {
        break;
    }
    ref = &cm->yv12_fb[cm->lst_fb_idx];
    border = ref->border;
    uv_border = border / 2;
    /* Capture top border + first MB row of Y (16 visible rows + border top
     * rows worth of context). Width spans left border + visible width to
     * cover both the left edge and any in-frame columns. */
    y_window_h = border + 16;
    uv_window_h = uv_border + 8;
    y_window_w = border + ref->y_crop_width;
    uv_window_w = uv_border + ref->uv_crop_width;
    out = govpx_oracle_state.out;
    fprintf(out,
            "{\"type\":\"last_ref_window\","
            "\"frame_index\":%llu,"
            "\"plane\":\"y\","
            "\"width\":%d,\"height\":%d,"
            "\"border_top\":%d,\"border_left\":%d,"
            "\"hex\":\"",
            govpx_oracle_state.frame_index, y_window_w, y_window_h,
            border, border);
    /* y_buffer points to the top-left of the visible region. Step back by
     * border rows and border columns to reach the top-left of the captured
     * window. */
    govpx_oracle_emit_plane_hex(out,
        ref->y_buffer - border * ref->y_stride - border,
        y_window_w, y_window_h, ref->y_stride);
    fprintf(out, "\"}\n");
    fprintf(out,
            "{\"type\":\"last_ref_window\","
            "\"frame_index\":%llu,"
            "\"plane\":\"u\","
            "\"width\":%d,\"height\":%d,"
            "\"border_top\":%d,\"border_left\":%d,"
            "\"hex\":\"",
            govpx_oracle_state.frame_index, uv_window_w, uv_window_h,
            uv_border, uv_border);
    govpx_oracle_emit_plane_hex(out,
        ref->u_buffer - uv_border * ref->uv_stride - uv_border,
        uv_window_w, uv_window_h, ref->uv_stride);
    fprintf(out, "\"}\n");
    fprintf(out,
            "{\"type\":\"last_ref_window\","
            "\"frame_index\":%llu,"
            "\"plane\":\"v\","
            "\"width\":%d,\"height\":%d,"
            "\"border_top\":%d,\"border_left\":%d,"
            "\"hex\":\"",
            govpx_oracle_state.frame_index, uv_window_w, uv_window_h,
            uv_border, uv_border);
    govpx_oracle_emit_plane_hex(out,
        ref->v_buffer - uv_border * ref->uv_stride - uv_border,
        uv_window_w, uv_window_h, ref->uv_stride);
    fprintf(out, "\"}\n");
    fflush(out);
    } while (0);
    GOVPX_TRACE_END();
}

/* R12-C: emit a per-iteration iteration_outcome row inside the libvpx
 * fast inter picker mode_index loop. Each row captures (mb_row, mb_col,
 * mode_index, this_mode, this_ref_frame, mode_mv, gate, this_rd,
 * best_rd_at_gate, rd_threshes_at_gate) where `gate` is one of
 * "rd_threshes", "ref_skip", "mode_check_freq", "alt_ref_skip",
 * "splitmv_unsupported", "umv_bounds", "near_zero_skip", "tested".
 * This pins down exactly which gate libvpx uses to skip a given
 * mode_index. NEAREST is mode_indexes 2, 5, 7. NEAR is mode_indexes
 * 3, 8, 9. */
void govpx_oracle_emit_iteration_outcome(struct VP8_COMP *cpi, int mb_row,
                                         int mb_col, int mode_index,
                                         int this_mode, int this_ref_frame,
                                         int mv_row, int mv_col,
                                         const char *gate, int this_rd,
                                         int best_rd_at_gate,
                                         int rd_threshes_at_gate) {
    FILE *out;
    govpx_oracle_init();
    if (!govpx_oracle_state.enabled || govpx_oracle_state.out == NULL) {
        return;
    }
    GOVPX_TRACE_BEGIN();
    (void)cpi;
    out = govpx_oracle_state.out;
    fprintf(out,
            "{\"type\":\"iteration_outcome\","
            "\"frame_index\":%llu,"
            "\"mb_row\":%d,\"mb_col\":%d,"
            "\"mode_index\":%d,"
            "\"mode\":\"%s\","
            "\"ref_frame\":%d,"
            "\"mv\":[%d,%d],"
            "\"gate\":\"%s\","
            "\"this_rd\":%d,"
            "\"best_rd_at_gate\":%d,"
            "\"rd_threshes_at_gate\":%d}\n",
            govpx_oracle_state.frame_index,
            mb_row, mb_col,
            mode_index,
            govpx_oracle_mode_name(this_mode),
            this_ref_frame,
            mv_row, mv_col,
            gate != NULL ? gate : "",
            this_rd,
            best_rd_at_gate,
            rd_threshes_at_gate);
    fflush(out);
    GOVPX_TRACE_END();
}

/* R12-C: emit a single picker_entry row capturing the libvpx fast inter
 * picker state at MB entry, immediately after vp8_find_near_mvs_bias has
 * populated mode_mv_sb / cnt[] but before the per-mode rd_threshes loop
 * begins. This pins down the NEARESTMV-skip cascade by surfacing exactly
 * which gate libvpx uses to skip NEARESTMV at MBs where govpx tests it
 * with non-zero MV. The row carries:
 *   - mode_mv[NEAREST/NEAR/NEW/ZERO/SPLIT].as_mv for the picker-active
 *     sign_bias slice (i.e. mode_mv_sb[sign_bias]),
 *   - cnt[CNT_INTRA..CNT_SPLITMV] (the mdcounts array filled by
 *     vp8_find_near_mvs_bias),
 *   - rd_threshes[NEARESTMV] / rd_thresh_mult[NEARESTMV] /
 *     mode_check_freq[NEARESTMV] / mode_test_hit_counts[NEARESTMV],
 *   - mbs_tested_so_far,
 *   - best_rd at picker entry (always INT_MAX on the first iteration),
 *   - sign_bias and ref_frame_map[1] (the LAST_FRAME ref slot).
 * NEARESTMV is mode_index 2 in libvpx's vp8_mode_order so we sample that
 * slot specifically; when the same MB's libvpx loop later short-circuits
 * the NEARESTMV iteration, the captured state at this hook explains why.
 * Mirrored on the govpx side via emitOraclePickerEntryTrace so the
 * comparator can diff the (mode_mv, cnt, rd_threshes, ...) tuple. */
void govpx_oracle_emit_picker_entry(struct VP8_COMP *cpi, int mb_row,
                                    int mb_col, int sign_bias,
                                    int ref_frame_last,
                                    int_mv mode_mv_sb[2][MB_MODE_COUNT],
                                    const int cnt[4], int best_rd) {
    FILE *out;
    MACROBLOCK *x;
    int_mv *mode_mv;

    govpx_oracle_init();
    if (!govpx_oracle_state.enabled || govpx_oracle_state.out == NULL) {
        return;
    }
    if (cpi == NULL) {
        return;
    }
    GOVPX_TRACE_BEGIN();
    x = &cpi->mb;
    out = govpx_oracle_state.out;
    mode_mv = mode_mv_sb[sign_bias];
    fprintf(out,
            "{\"type\":\"picker_entry\","
            "\"frame_index\":%llu,"
            "\"mb_row\":%d,\"mb_col\":%d,"
            "\"sign_bias\":%d,"
            "\"ref_frame_last\":%d,"
            "\"mode_mv\":{"
            "\"nearest\":[%d,%d],"
            "\"near\":[%d,%d],"
            "\"zero\":[%d,%d],"
            "\"new\":[%d,%d]"
            "},"
            "\"cnt\":{"
            "\"intra\":%d,\"nearest\":%d,\"near\":%d,\"splitmv\":%d"
            "},"
            "\"rd_threshes_nearest\":%d,"
            "\"rd_thresh_mult_nearest\":%d,"
            "\"rd_baseline_thresh_nearest\":%d,"
            "\"sf_thresh_mult_nearest\":%d,"
            "\"rdmult\":%d,"
            "\"rddiv\":%d,"
            "\"speed\":%d,"
            "\"base_qindex\":%d,"
            "\"y1dc_delta_q\":%d,"
            "\"zbin_over_quant\":%d,"
            "\"errorperbit\":%d,"
            "\"mode_check_freq_nearest\":%d,"
            "\"mode_test_hit_counts_nearest\":%d,"
            "\"mbs_tested_so_far\":%d,"
            "\"best_rd\":%d}\n",
            govpx_oracle_state.frame_index,
            mb_row, mb_col,
            sign_bias,
            ref_frame_last,
            mode_mv[NEARESTMV].as_mv.row, mode_mv[NEARESTMV].as_mv.col,
            mode_mv[NEARMV].as_mv.row, mode_mv[NEARMV].as_mv.col,
            mode_mv[ZEROMV].as_mv.row, mode_mv[ZEROMV].as_mv.col,
            mode_mv[NEWMV].as_mv.row, mode_mv[NEWMV].as_mv.col,
            cnt != NULL ? cnt[0] : 0,
            cnt != NULL ? cnt[1] : 0,
            cnt != NULL ? cnt[2] : 0,
            cnt != NULL ? cnt[3] : 0,
            x->rd_threshes[2],
            x->rd_thresh_mult[2],
            cpi->rd_baseline_thresh[2],
            cpi->sf.thresh_mult[2],
            cpi->RDMULT,
            cpi->RDDIV,
            cpi->Speed,
            cpi->common.base_qindex,
            cpi->common.y1dc_delta_q,
            x->zbin_over_quant,
            x->errorperbit,
            cpi->mode_check_freq[2],
            x->mode_test_hit_counts[2],
            x->mbs_tested_so_far,
            best_rd);
    fflush(out);
    GOVPX_TRACE_END();
}

/* Emit a single per-trial-level row from vp8cx_pick_filter_level_fast. The
 * row records (frame_index, trial_level, trial_y_sse) so the govpx-side
 * picker can be diffed level-by-level. Phase indicates which call site
 * inside the picker emitted the row: "seed" for the initial cm->filter_level
 * scoring, "down" for the decreasing-level loop, "up" for the increasing-
 * level loop. The emitted frame_index intentionally matches the *upcoming*
 * frame's index (i.e. govpx_oracle_state.frame_index is incremented after
 * govpx_oracle_emit_frame, so the picker call for frame N sees N). */
void govpx_oracle_emit_lf_trial(struct VP8_COMP *cpi, const char *phase,
                                int trial_level, int trial_y_sse) {
    FILE *out;
    (void)cpi;
    govpx_oracle_init();
    if (!govpx_oracle_state.enabled || govpx_oracle_state.out == NULL) {
        return;
    }
    GOVPX_TRACE_BEGIN();
    out = govpx_oracle_state.out;
    fprintf(out,
            "{\"type\":\"lf_trial\","
            "\"frame_index\":%llu,"
            "\"phase\":\"%s\","
            "\"trial_level\":%d,"
            "\"trial_y_sse\":%d}\n",
            govpx_oracle_state.frame_index,
            phase != NULL ? phase : "",
            trial_level,
            trial_y_sse);
    fflush(out);
    GOVPX_TRACE_END();
}
GOVPX_ORACLE_TU

	# (2) Add extern declarations + the per-MB capture call to encodeframe.c.
	# Anchor: the line "extern void vp8_stuff_mb(...)" in v1.16.0
	# uniquely identifies the top of the file's extern block.
	awk '
		BEGIN { inserted_decl = 0; inserted_call = 0 }
		!inserted_decl && /^extern void vp8_stuff_mb\(/ {
			print "extern void govpx_oracle_capture_mb(struct VP8_COMP *cpi, int mb_row, int mb_col);"
			print "extern void govpx_oracle_record_mb_rate(struct VP8_COMP *cpi, int mb_row, int mb_col, int mb_rate, int aggregated_rate);"
			print "extern void govpx_oracle_emit_predictor(struct VP8_COMP *cpi, int mb_row, int mb_col);"
			print "extern void govpx_oracle_emit_reconstructed(struct VP8_COMP *cpi, int mb_row, int mb_col);"
			print "extern void govpx_oracle_emit_last_ref_window(struct VP8_COMP *cpi);"
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

	# (2.1) Inject per-MB rate-aggregator capture calls after libvpx folds
	# the picker's chosen-mode rate into `*totalrate` inside encode_mb_row.
	# The capture point matches the govpx-side libvpxAddProjectedMacroblockRate
	# in encoder_reconstruct.go: rate is captured for both the KEY and INTER
	# branches, and the running totalrate is captured after this MB's
	# contribution has been added (matching the post-add snapshot govpx
	# emits via emitOracleMBTrace / emitOracleKeyFrameMBTrace).
	python3 - "$src_dir/vp8/encoder/encodeframe.c" <<'GOVPX_RATE_PY'
import sys, io
path = sys.argv[1]
with io.open(path, 'r', encoding='utf-8') as f:
    text = f.read()
sentinel = '/* govpx oracle: capture per-MB rate aggregator. */'
if sentinel in text:
    sys.exit(0)  # already patched
intra_anchor = ('      const int intra_rate_cost = vp8cx_encode_intra_macroblock(cpi, x, tp);\n'
                '      if (INT_MAX - *totalrate > intra_rate_cost)\n'
                '        *totalrate += intra_rate_cost;\n'
                '      else\n'
                '        *totalrate = INT_MAX;\n')
intra_replacement = ('      const int intra_rate_cost = vp8cx_encode_intra_macroblock(cpi, x, tp);\n'
                     '      if (INT_MAX - *totalrate > intra_rate_cost)\n'
                     '        *totalrate += intra_rate_cost;\n'
                     '      else\n'
                     '        *totalrate = INT_MAX;\n'
                     '      ' + sentinel + '\n'
                     '      govpx_oracle_record_mb_rate(cpi, mb_row, mb_col, intra_rate_cost, *totalrate);\n')
if intra_anchor not in text:
    sys.stderr.write('build_vpxenc_oracle.sh: intra rate anchor missing in encodeframe.c\n')
    sys.exit(2)
text = text.replace(intra_anchor, intra_replacement, 1)
inter_anchor = ('      const int inter_rate_cost = vp8cx_encode_inter_macroblock(\n'
                '          cpi, x, tp, recon_yoffset, recon_uvoffset, mb_row, mb_col);\n'
                '      if (INT_MAX - *totalrate > inter_rate_cost)\n'
                '        *totalrate += inter_rate_cost;\n'
                '      else\n'
                '        *totalrate = INT_MAX;\n')
inter_replacement = ('      const int inter_rate_cost = vp8cx_encode_inter_macroblock(\n'
                     '          cpi, x, tp, recon_yoffset, recon_uvoffset, mb_row, mb_col);\n'
                     '      if (INT_MAX - *totalrate > inter_rate_cost)\n'
                     '        *totalrate += inter_rate_cost;\n'
                     '      else\n'
                     '        *totalrate = INT_MAX;\n'
                     '      ' + sentinel + '\n'
                     '      govpx_oracle_record_mb_rate(cpi, mb_row, mb_col, inter_rate_cost, *totalrate);\n')
if inter_anchor not in text:
    sys.stderr.write('build_vpxenc_oracle.sh: inter rate anchor missing in encodeframe.c\n')
    sys.exit(2)
text = text.replace(inter_anchor, inter_replacement, 1)
with io.open(path, 'w', encoding='utf-8') as f:
    f.write(text)
GOVPX_RATE_PY

	# (2.5) Inject predictor-dump and reconstruction-dump calls into
	# vp8cx_encode_inter_macroblock. The predictor dump captures
	# xd->dst.{y,u,v}_buffer between vp8_encode_inter16x16 and
	# vp8_inverse_transform_mby — at that point dst holds the pure inter
	# predictor (no residual added yet) and matches govpx's analysis image
	# after reconstructInterAnalysisMacroblock(MBSkipCoeff=1). The
	# reconstruction dump captures the same buffer at function tail, after
	# the IDCT-add residual stage. Both are no-ops unless
	# GOVPX_ORACLE_PREDICTOR_DUMP=1 is set.
	python3 - "$src_dir/vp8/encoder/encodeframe.c" <<'GOVPX_PREDICTOR_PY'
import sys, io
path = sys.argv[1]
with io.open(path, 'r', encoding='utf-8') as f:
    text = f.read()
pred_sentinel = '/* govpx oracle: dump inter predictor before residual add. */'
recon_sentinel = '/* govpx oracle: dump inter MB after residual add. */'
if pred_sentinel in text and recon_sentinel in text:
    sys.exit(0)  # already patched
pred_anchor = ('    if (!x->skip) {\n'
               '      vp8_encode_inter16x16(x);\n'
               '    } else {\n'
               '      vp8_build_inter16x16_predictors_mb(xd, xd->dst.y_buffer, xd->dst.u_buffer,\n'
               '                                         xd->dst.v_buffer, xd->dst.y_stride,\n'
               '                                         xd->dst.uv_stride);\n'
               '    }\n'
               '  }\n')
pred_replacement = ('    if (!x->skip) {\n'
                    '      vp8_encode_inter16x16(x);\n'
                    '    } else {\n'
                    '      vp8_build_inter16x16_predictors_mb(xd, xd->dst.y_buffer, xd->dst.u_buffer,\n'
                    '                                         xd->dst.v_buffer, xd->dst.y_stride,\n'
                    '                                         xd->dst.uv_stride);\n'
                    '    }\n'
                    '    ' + pred_sentinel + '\n'
                    '    govpx_oracle_emit_predictor(cpi, mb_row, mb_col);\n'
                    '  }\n')
if pred_anchor not in text:
    sys.stderr.write('build_vpxenc_oracle.sh: predictor anchor missing in encodeframe.c\n')
    sys.exit(2)
text = text.replace(pred_anchor, pred_replacement, 1)

# Inject reconstruction dump just before the unique "return rate;" tail of
# vp8cx_encode_inter_macroblock. The function tail is identifiable by the
# preceding `vp8_stuff_mb` block since vp8cx_encode_intra_macro_block
# doesn't have that path.
recon_anchor = ('    if (cpi->common.mb_no_coeff_skip) {\n'
                '      x->skip_true_count++;\n'
                '      vp8_fix_contexts(xd);\n'
                '    } else {\n'
                '      vp8_stuff_mb(cpi, x, t);\n'
                '    }\n'
                '  }\n'
                '\n'
                '  return rate;\n'
                '}\n')
recon_replacement = ('    if (cpi->common.mb_no_coeff_skip) {\n'
                     '      x->skip_true_count++;\n'
                     '      vp8_fix_contexts(xd);\n'
                     '    } else {\n'
                     '      vp8_stuff_mb(cpi, x, t);\n'
                     '    }\n'
                     '  }\n'
                     '\n'
                     '  ' + recon_sentinel + '\n'
                     '  govpx_oracle_emit_reconstructed(cpi, mb_row, mb_col);\n'
                     '\n'
                     '  return rate;\n'
                     '}\n')
if recon_anchor not in text:
    sys.stderr.write('build_vpxenc_oracle.sh: reconstruction anchor missing in encodeframe.c\n')
    sys.exit(2)
text = text.replace(recon_anchor, recon_replacement, 1)

# Inject last_ref_window dump at the entry of encode_mb_row when mb_row==0.
# Anchor: the unique "/* reset above block coeffs */" comment (only one site
# in encodeframe.c) followed by xd->above_context assignment.
ref_sentinel = '/* govpx oracle: dump LAST reference content + border. */'
ref_anchor = ('  /* reset above block coeffs */\n'
              '  xd->above_context = cm->above_context;\n')
ref_replacement = ('  /* reset above block coeffs */\n'
                   '  xd->above_context = cm->above_context;\n'
                   '\n'
                   '  ' + ref_sentinel + '\n'
                   '  if (mb_row == 0) {\n'
                   '    govpx_oracle_emit_last_ref_window(cpi);\n'
                   '  }\n')
if ref_anchor not in text:
    sys.stderr.write('build_vpxenc_oracle.sh: encode_mb_row anchor missing in encodeframe.c\n')
    sys.exit(2)
text = text.replace(ref_anchor, ref_replacement, 1)

with io.open(path, 'w', encoding='utf-8') as f:
    f.write(text)
GOVPX_PREDICTOR_PY

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
def strip_autospeed_shim(src):
    start = src.find('    /* govpx oracle determinism shim: replace the wall-clock measurement')
    if start < 0:
        return src
    end_marker = '\n\n    if (cm->frame_type != KEY_FRAME) {'
    end = src.find(end_marker, start)
    if end < 0:
        sys.stderr.write('build_vpxenc_oracle.sh: autospeed shim terminator missing in onyx_if.c\n')
        sys.exit(2)
    return src[:start] + src[end + 2:]
def ensure_autospeed_boundary_shim(src):
    marker = '    /* govpx oracle determinism shim: pin keyframe auto-speed boundary.'
    if marker in src:
        return src
    anchor = ('    duration = (int)(vpx_usec_timer_elapsed(&ticktimer));\n'
              '    duration2 = (unsigned int)((double)duration / 2);\n'
              '\n'
              '    if (cm->frame_type != KEY_FRAME) {')
    replacement = (
        '    duration = (int)(vpx_usec_timer_elapsed(&ticktimer));\n'
        '    duration2 = (unsigned int)((double)duration / 2);\n'
        '\n'
        '    /* govpx oracle determinism shim: pin keyframe auto-speed boundary.\n'
        '     * libvpx realtime auto-speed is intentionally wall-clock driven;\n'
        '     * strict byte parity needs a stable branch when large positive-cpu\n'
        '     * keyframes land near the vp8_auto_select_speed budget boundary.\n'
        '     * Use the same large-frame gate as govpx so both encoders take\n'
        '     * the libvpx Speed 5 -> 4 trajectory deterministically. */\n'
        '    if (cpi->oxcf.cpu_used >= 0 && (*frame_flags & FRAMEFLAGS_KEY)) {\n'
        '      int n_mb = cm->mb_rows * cm->mb_cols;\n'
        '      int gate = 0;\n'
        '      if (n_mb >= 3600) gate = 1;\n'
        '      else if (cpi->oxcf.cpu_used >= 8 && n_mb >= 1900) gate = 1;\n'
        '      if (gate) {\n'
        '        int ms_for_compress = (int)(1000000 / cpi->framerate);\n'
        '        ms_for_compress = ms_for_compress * (16 - cpi->oxcf.cpu_used) / 16;\n'
        '        if (ms_for_compress > 1) {\n'
        '          duration = 2 * ms_for_compress - 2;\n'
        '          duration2 = (unsigned int)((double)duration / 2);\n'
        '        }\n'
        '      }\n'
        '    }\n'
        '\n'
        '    if (cm->frame_type != KEY_FRAME) {')
    if anchor not in src:
        sys.stderr.write('build_vpxenc_oracle.sh: autospeed timer anchor missing in onyx_if.c\n')
        sys.exit(2)
    return src.replace(anchor, replacement, 1)
def ensure_trace_take_usec_extern(src):
    """Insert the govpx_oracle_trace_take_usec extern declaration after the
    existing govpx oracle externs on already-patched trees. Idempotent."""
    marker = 'extern int64_t govpx_oracle_trace_take_usec(void);'
    if marker in src:
        return src
    anchor = ('extern void govpx_oracle_emit_dropped_frame(struct VP8_COMP *cpi,\n'
              '                                            const char *reason);')
    if anchor not in src:
        sys.stderr.write('build_vpxenc_oracle.sh: emit_dropped_frame extern anchor missing\n')
        sys.exit(2)
    replacement = (anchor + '\n'
                   '/* Returns the cumulative microseconds spent inside oracle trace\n'
                   ' * emit/capture functions since the last call, then resets the\n'
                   ' * accumulator. Used below to subtract trace overhead from the\n'
                   ' * realtime-mode wall-clock duration before it feeds the auto-speed\n'
                   ' * picker (vp8/encoder/rdopt.c:261 vp8_auto_select_speed), so the\n'
                   ' * traced binary stays byte-identical to the untraced one. */\n'
                   'extern int64_t govpx_oracle_trace_take_usec(void);')
    return src.replace(anchor, replacement, 1)
def ensure_trace_usec_subtraction(src):
    """Convert the wall-clock duration math inside encode_frame_to_data_rate
    so it subtracts the cumulative oracle-trace overhead before it feeds
    avg_encode_time / avg_pick_mode_time. Idempotent."""
    sentinel = '/* govpx oracle: subtract trace overhead from duration. */'
    if sentinel in src:
        return src
    old = ('  if (cpi->compressor_speed == 2) {\n'
           '    unsigned int duration, duration2;\n'
           '    vpx_usec_timer_mark(&ticktimer);\n'
           '\n'
           '    duration = (int)(vpx_usec_timer_elapsed(&ticktimer));\n'
           '    duration2 = (unsigned int)((double)duration / 2);')
    new = ('  if (cpi->compressor_speed == 2) {\n'
           '    unsigned int duration, duration2;\n'
           '    int64_t trace_usec;\n'
           '    int64_t raw_duration;\n'
           '    vpx_usec_timer_mark(&ticktimer);\n'
           '\n'
           '    ' + sentinel + '\n'
           '    /* Subtract the cumulative microseconds spent inside oracle trace\n'
           '     * emit/capture functions during this frame. Without this\n'
           '     * subtraction, the trace channel\'s fopen/fwrite/fflush latency\n'
           '     * inflates cpi->avg_encode_time / cpi->avg_pick_mode_time and\n'
           '     * shifts the libvpx vp8_auto_select_speed trajectory, so the\n'
           '     * traced oracle binary\'s bitstream diverges from the untraced\n'
           '     * production binary for the same config. The accumulator is\n'
           '     * always 0 when the trace channel is disabled (env var unset),\n'
           '     * so this is a true no-op in the non-traced build, which keeps\n'
           '     * vpxenc-frameflags\' byte-stream identical to a pristine\n'
           '     * v1.16.0 encode. */\n'
           '    raw_duration = vpx_usec_timer_elapsed(&ticktimer);\n'
           '    trace_usec = govpx_oracle_trace_take_usec();\n'
           '    if (trace_usec < 0) trace_usec = 0;\n'
           '    if (raw_duration > trace_usec) {\n'
           '      raw_duration -= trace_usec;\n'
           '    } else {\n'
           '      raw_duration = 0;\n'
           '    }\n'
           '    duration = (unsigned int)raw_duration;\n'
           '    duration2 = (unsigned int)((double)duration / 2);')
    if old not in src:
        sys.stderr.write('build_vpxenc_oracle.sh: trace-usec subtract anchor missing in onyx_if.c\n')
        sys.exit(2)
    return src.replace(old, new, 1)
sentinel = '/* govpx oracle: rate/recode emit hook. */'
if sentinel in text:
    updated = ensure_autospeed_boundary_shim(strip_autospeed_shim(text))
    updated = ensure_trace_take_usec_extern(updated)
    updated = ensure_trace_usec_subtraction(updated)
    if updated != text:
        with io.open(path, 'w', encoding='utf-8') as f:
            f.write(updated)
    sys.exit(0)  # already patched
# Anchor 1: inject extern declarations directly after the "extern void
# vp8cx_init_quantizer" line (unique in v1.16.0). If absent, fall back to
# inserting after the first "#include \"onyx_int.h\"" so the patch still
# compiles.
decl = ('extern void govpx_oracle_begin_attempt(void);\n'
        'extern void govpx_oracle_recode_iter(void);\n'
        'extern void govpx_oracle_emit_rate(struct VP8_COMP *cpi, int final_q);\n'
        'extern void govpx_oracle_emit_dropped_frame(struct VP8_COMP *cpi,\n'
        '                                            const char *reason);\n'
        '/* Returns the cumulative microseconds spent inside oracle trace\n'
        ' * emit/capture functions since the last call, then resets the\n'
        ' * accumulator. Used below to subtract trace overhead from the\n'
        ' * realtime-mode wall-clock duration before it feeds the auto-speed\n'
        ' * picker (vp8/encoder/rdopt.c:261 vp8_auto_select_speed), so the\n'
        ' * traced binary stays byte-identical to the untraced one. */\n'
        'extern int64_t govpx_oracle_trace_take_usec(void);\n')
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
                    '    govpx_oracle_begin_attempt();\n'
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
# Anchor 4: emit a dropped-frame row at each of the three drop-decision
# return paths inside encode_frame_to_data_rate (decimation,
# buffer-underrun, post-encode overshoot). Each anchor is unique in v1.16.0.
drop_sentinel = '/* govpx oracle: drop emit hook. */'
# 4a: decimation drop via vp8_check_drop_buffer.
decimation_anchor = ('  if (vp8_check_drop_buffer(cpi)) {\n'
                     '    return;\n'
                     '  }\n')
decimation_replacement = ('  if (vp8_check_drop_buffer(cpi)) {\n'
                          '    ' + drop_sentinel + '\n'
                          '    govpx_oracle_emit_dropped_frame(cpi, "decimation");\n'
                          '    return;\n'
                          '  }\n')
if decimation_anchor not in text:
    sys.stderr.write('build_vpxenc_oracle.sh: drop decimation anchor missing in onyx_if.c\n')
    sys.exit(2)
text = text.replace(decimation_anchor, decimation_replacement, 1)
# 4b: buffer-underrun drop via vp8_pick_frame_size returning 0.
underrun_anchor = ('  if (!vp8_pick_frame_size(cpi)) {\n'
                   '/*TODO: 2 drop_frame and return code could be put together. */\n'
                   '#if CONFIG_MULTI_RES_ENCODING\n'
                   '    vp8_store_drop_frame_info(cpi);\n'
                   '#endif\n'
                   '    cm->current_video_frame++;\n'
                   '    cpi->frames_since_key++;\n'
                   '    cpi->ext_refresh_frame_flags_pending = 0;\n'
                   '    // We advance the temporal pattern for dropped frames.\n'
                   '    cpi->temporal_pattern_counter++;\n'
                   '    return;\n'
                   '  }\n')
underrun_replacement = ('  if (!vp8_pick_frame_size(cpi)) {\n'
                        '/*TODO: 2 drop_frame and return code could be put together. */\n'
                        '#if CONFIG_MULTI_RES_ENCODING\n'
                        '    vp8_store_drop_frame_info(cpi);\n'
                        '#endif\n'
                        '    cm->current_video_frame++;\n'
                        '    cpi->frames_since_key++;\n'
                        '    cpi->ext_refresh_frame_flags_pending = 0;\n'
                        '    // We advance the temporal pattern for dropped frames.\n'
                        '    cpi->temporal_pattern_counter++;\n'
                        '    ' + drop_sentinel + '\n'
                        '    govpx_oracle_emit_dropped_frame(cpi, "buffer_underrun");\n'
                        '    return;\n'
                        '  }\n')
if underrun_anchor not in text:
    sys.stderr.write('build_vpxenc_oracle.sh: drop underrun anchor missing in onyx_if.c\n')
    sys.exit(2)
text = text.replace(underrun_anchor, underrun_replacement, 1)
# 4c: post-encode overshoot drop via vp8_drop_encodedframe_overshoot.
overshoot_anchor = ('      if (vp8_drop_encodedframe_overshoot(cpi, Q)) {\n'
                    '        vpx_clear_system_state();\n'
                    '        return;\n'
                    '      }\n')
overshoot_replacement = ('      if (vp8_drop_encodedframe_overshoot(cpi, Q)) {\n'
                         '        ' + drop_sentinel + '\n'
                         '        govpx_oracle_emit_dropped_frame(cpi, "overshoot");\n'
                         '        vpx_clear_system_state();\n'
                         '        return;\n'
                         '      }\n')
if overshoot_anchor not in text:
    sys.stderr.write('build_vpxenc_oracle.sh: drop overshoot anchor missing in onyx_if.c\n')
    sys.exit(2)
text = text.replace(overshoot_anchor, overshoot_replacement, 1)

# Anchor 5: remove the older deterministic slow-timer shim if it exists in an
# already-generated oracle source tree, then add the narrower keyframe boundary
# shim used by strict byte-parity runs. Realtime auto-speed is wall-clock
# sensitive; this pins only large positive-cpu keyframes that land on the
# branch boundary, instead of forcing every large frame onto an artificial
# speed-16 trajectory.
autospeed_sentinel = '/* govpx oracle determinism shim: replace the wall-clock measurement'
autospeed_anchor = ('    duration = (int)(vpx_usec_timer_elapsed(&ticktimer));\n'
                    '    duration2 = (unsigned int)((double)duration / 2);\n'
                    '\n'
                    '    if (cm->frame_type != KEY_FRAME) {')
autospeed_old = (
    '    duration = (int)(vpx_usec_timer_elapsed(&ticktimer));\n'
    '    duration2 = (unsigned int)((double)duration / 2);\n'
    '\n'
    '    ' + autospeed_sentinel + '\n'
    '     * with a synthetic deterministic value for realtime+positive-cpu_used\n'
    '     * fixtures whose MB count crosses the threshold where govpx\'s\n'
    '     * largeMBRealtimeAutoSpeedSynthetic gate fires. Mirrors the\n'
    '     * Go-side threshold exactly so both sides evolve cpi->Speed on\n'
    '     * the same trajectory. */\n'
    '    if (cpi->oxcf.cpu_used >= 0) {\n'
    '      int n_mb = cm->mb_rows * cm->mb_cols;\n'
    '      int gate = 0;\n'
    '      if (n_mb >= 3000) gate = 1;\n'
    '      else if (cpi->oxcf.cpu_used >= 8 && n_mb >= 1700) gate = 1;\n'
    '      if (gate) {\n'
    '        duration = 4000000;\n'
    '        duration2 = 2000000;\n'
    '      }\n'
    '    }\n'
    '\n'
    '    if (cm->frame_type != KEY_FRAME) {')
if autospeed_old in text:
    text = text.replace(autospeed_old, autospeed_anchor, 1)
elif autospeed_anchor not in text:
    sys.stderr.write('build_vpxenc_oracle.sh: autospeed timer anchor missing in onyx_if.c\n')
    sys.exit(2)
text = ensure_autospeed_boundary_shim(text)
# Subtract the cumulative oracle-trace overhead from the wall-clock
# duration before it feeds avg_encode_time / avg_pick_mode_time. This
# decouples vp8_auto_select_speed (vp8/encoder/rdopt.c:261) from the
# trace channel's fopen/fwrite/fflush latency so the traced binary's
# bitstream stays byte-identical to the untraced production binary for
# the same config. The accumulator is always 0 in the non-traced
# (GOVPX_ORACLE_TRACE_OUT unset) build, so this is a true no-op there.
text = ensure_trace_usec_subtraction(text)

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
               '                                            int sr);\n'
               'extern void govpx_oracle_capture_inter_candidate(\n'
               '    struct VP8_COMP *cpi, int mb_row, int mb_col,\n'
               '    const char *picker, int mode_index, int ref_slot,\n'
               '    int threshold, int best_score_before,\n'
               '    int best_yrd_before, long long best_sse_before,\n'
               '    int score, int yrd, int rate, int rate_y, int rate_uv,\n'
               '    int distortion, int distortion_uv, long long sse,\n'
               '    int became_best, int loop_break);\n')
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
# Anchor 4: capture best-before state for evaluated RD candidates.
rd_loop_anchor = ('    int this_ref_frame = ref_frame_map[vp8_ref_frame_order[mode_index]];\n'
                  '\n'
                  '    /* Test best rd so far against threshold for trying this mode. */')
rd_loop_replacement = ('    int this_ref_frame = ref_frame_map[vp8_ref_frame_order[mode_index]];\n'
                       '    int govpx_threshold_before = x->rd_threshes[mode_index];\n'
                       '    int govpx_best_rd_before = best_mode.rd;\n'
                       '    int govpx_best_yrd_before = best_mode.yrd;\n'
                       '\n'
                       '    /* Test best rd so far against threshold for trying this mode. */')
if rd_loop_anchor not in text:
    sys.stderr.write('build_vpxenc_oracle.sh: rd_pick_inter_mode loop anchor missing\n')
    sys.exit(2)
text = text.replace(rd_loop_anchor, rd_loop_replacement, 1)
# Anchor 5: emit a row after final RD accounting, before best-mode mutation.
rd_emit_anchor = ('    this_rd =\n'
                  '        calculate_final_rd_costs(this_rd, &rd, &other_cost, disable_skip,\n'
                  '                                 uv_intra_tteob, intra_rd_penalty, cpi, x);\n'
                  '\n'
                  '    /* Keep record of best intra distortion */')
rd_emit_replacement = ('    this_rd =\n'
                       '        calculate_final_rd_costs(this_rd, &rd, &other_cost, disable_skip,\n'
                       '                                 uv_intra_tteob, intra_rd_penalty, cpi, x);\n'
                       '\n'
                       '    if (this_rd != INT_MAX) {\n'
                       '      const int govpx_other_cost_for_yrd =\n'
                       '          other_cost + x->ref_frame_cost[x->e_mbd.mode_info_context->mbmi.ref_frame];\n'
                       '      const int govpx_this_yrd = RDCOST(\n'
                       '          x->rdmult, x->rddiv,\n'
                       '          rd.rate2 - rd.rate_uv - govpx_other_cost_for_yrd,\n'
                       '          rd.distortion2 - rd.distortion_uv);\n'
                       '      govpx_oracle_capture_inter_candidate(\n'
                       '          cpi, mb_row, mb_col, "rd", mode_index,\n'
                       '          vp8_ref_frame_order[mode_index], govpx_threshold_before,\n'
                       '          govpx_best_rd_before, govpx_best_yrd_before, -1,\n'
                       '          this_rd, govpx_this_yrd, rd.rate2, rd.rate_y, rd.rate_uv,\n'
                       '          rd.distortion2, rd.distortion_uv, -1,\n'
                       '          (this_rd < best_mode.rd || x->skip), x->skip ? 1 : 0);\n'
                       '    }\n'
                       '\n'
                       '    /* Keep record of best intra distortion */')
if rd_emit_anchor not in text:
    sys.stderr.write('build_vpxenc_oracle.sh: rd_pick_inter_mode emit anchor missing\n')
    sys.exit(2)
text = text.replace(rd_emit_anchor, rd_emit_replacement, 1)
with io.open(path, 'w', encoding='utf-8') as f:
    f.write(text)
GOVPX_RDOPT_PY

	# (3.7) Instrument the cheaper fast picker in pickinter.c with the same
	# evaluated-candidate row schema. It intentionally skips pruned modes,
	# unsupported SPLITMV candidates, and B_PRED candidates that failed
	# breakout, matching govpx's current "tested only" trace rows.
	python3 - "$src_dir/vp8/encoder/pickinter.c" <<'GOVPX_PICKINTER_PY'
import sys, io
path = sys.argv[1]
with io.open(path, 'r', encoding='utf-8') as f:
    text = f.read()
sentinel = '/* govpx oracle: fast inter-candidate record. */'
if sentinel in text:
    sys.exit(0)
extern_anchor = ('extern const int vp8_ref_frame_order[MAX_MODES];\n'
                 'extern const MB_PREDICTION_MODE vp8_mode_order[MAX_MODES];\n')
extern_decl = ('extern void govpx_oracle_capture_inter_candidate(\n'
               '    struct VP8_COMP *cpi, int mb_row, int mb_col,\n'
               '    const char *picker, int mode_index, int ref_slot,\n'
               '    int threshold, int best_score_before,\n'
               '    int best_yrd_before, long long best_sse_before,\n'
               '    int score, int yrd, int rate, int rate_y, int rate_uv,\n'
               '    int distortion, int distortion_uv, long long sse,\n'
               '    int became_best, int loop_break);\n'
               'extern void govpx_oracle_emit_picker_entry(\n'
               '    struct VP8_COMP *cpi, int mb_row, int mb_col,\n'
               '    int sign_bias, int ref_frame_last,\n'
               '    int_mv mode_mv_sb[2][MB_MODE_COUNT],\n'
               '    const int cnt[4], int best_rd);\n'
               'extern void govpx_oracle_emit_iteration_outcome(\n'
               '    struct VP8_COMP *cpi, int mb_row, int mb_col,\n'
               '    int mode_index, int this_mode, int this_ref_frame,\n'
               '    int mv_row, int mv_col, const char *gate, int this_rd,\n'
               '    int best_rd_at_gate, int rd_threshes_at_gate);\n')
if extern_anchor not in text:
    sys.stderr.write('build_vpxenc_oracle.sh: pickinter extern anchor missing\n')
    sys.exit(2)
text = text.replace(extern_anchor, extern_anchor + extern_decl, 1)
loop_anchor = ('    int frame_cost;\n'
               '    int this_rd = INT_MAX;\n'
               '    int this_ref_frame = ref_frame_map[vp8_ref_frame_order[mode_index]];\n'
               '\n'
               '    if (best_rd <= x->rd_threshes[mode_index]) continue;\n')
loop_replacement = ('    int frame_cost;\n'
                    '    int this_rd = INT_MAX;\n'
                    '    int this_ref_frame = ref_frame_map[vp8_ref_frame_order[mode_index]];\n'
                    '    int govpx_threshold_before = x->rd_threshes[mode_index];\n'
                    '    int govpx_best_rd_before = best_rd;\n'
                    '    unsigned int govpx_best_sse_before = best_rd_sse;\n'
                    '\n'
                    '    if (best_rd <= x->rd_threshes[mode_index]) {\n'
                    '      govpx_oracle_emit_iteration_outcome(\n'
                    '          cpi, mb_row, mb_col, mode_index,\n'
                    '          vp8_mode_order[mode_index], this_ref_frame,\n'
                    '          0, 0, "rd_threshes", INT_MAX,\n'
                    '          best_rd, x->rd_threshes[mode_index]);\n'
                    '      continue;\n'
                    '    }\n')
if loop_anchor not in text:
    sys.stderr.write('build_vpxenc_oracle.sh: pickinter loop anchor missing\n')
    sys.exit(2)
text = text.replace(loop_anchor, loop_replacement, 1)
emit_anchor = ('    if (this_rd < best_rd || x->skip) {\n'
               '      /* Note index of best mode */')
emit_replacement = ('    ' + sentinel + '\n'
                    '    if (this_mode != SPLITMV &&\n'
                    '        !(this_mode == B_PRED && distortion2 == INT_MAX) &&\n'
                    '        this_rd != INT_MAX) {\n'
                    '      govpx_oracle_capture_inter_candidate(\n'
                    '          cpi, mb_row, mb_col, "fast", mode_index,\n'
                    '          vp8_ref_frame_order[mode_index], govpx_threshold_before,\n'
                    '          govpx_best_rd_before, -1, (long long)govpx_best_sse_before,\n'
                    '          this_rd, -1, rate2, -1, -1, distortion2, -1,\n'
                    '          (long long)sse, (this_rd < best_rd || x->skip),\n'
                    '          x->skip ? 1 : 0);\n'
                    '    }\n'
                    '\n'
                    '    if (this_rd < best_rd || x->skip) {\n'
                    '      /* Note index of best mode */')
if emit_anchor not in text:
    sys.stderr.write('build_vpxenc_oracle.sh: pickinter emit anchor missing\n')
    sys.exit(2)
text = text.replace(emit_anchor, emit_replacement, 1)
# R12-C: instrument the remaining continue paths inside the mode_index loop
# so we can pin down exactly which gate skips a given mode.
ref_skip_anchor = '    if (this_ref_frame < 0) continue;\n'
ref_skip_replacement = ('    if (this_ref_frame < 0) {\n'
                        '      govpx_oracle_emit_iteration_outcome(\n'
                        '          cpi, mb_row, mb_col, mode_index,\n'
                        '          vp8_mode_order[mode_index], this_ref_frame,\n'
                        '          0, 0, "ref_skip", INT_MAX,\n'
                        '          best_rd, x->rd_threshes[mode_index]);\n'
                        '      continue;\n'
                        '    }\n')
if ref_skip_anchor not in text:
    sys.stderr.write('build_vpxenc_oracle.sh: pickinter ref_skip anchor missing\n')
    sys.exit(2)
text = text.replace(ref_skip_anchor, ref_skip_replacement, 1)
freq_skip_anchor = ('        x->rd_threshes[mode_index] =\n'
                    '            (cpi->rd_baseline_thresh[mode_index] >> 7) *\n'
                    '            x->rd_thresh_mult[mode_index];\n'
                    '        continue;\n'
                    '      }\n'
                    '    }\n')
freq_skip_replacement = ('        x->rd_threshes[mode_index] =\n'
                         '            (cpi->rd_baseline_thresh[mode_index] >> 7) *\n'
                         '            x->rd_thresh_mult[mode_index];\n'
                         '        govpx_oracle_emit_iteration_outcome(\n'
                         '            cpi, mb_row, mb_col, mode_index,\n'
                         '            vp8_mode_order[mode_index], this_ref_frame,\n'
                         '            0, 0, "mode_check_freq", INT_MAX,\n'
                         '            best_rd, x->rd_threshes[mode_index]);\n'
                         '        continue;\n'
                         '      }\n'
                         '    }\n')
if freq_skip_anchor not in text:
    sys.stderr.write('build_vpxenc_oracle.sh: pickinter freq_skip anchor missing\n')
    sys.exit(2)
text = text.replace(freq_skip_anchor, freq_skip_replacement, 1)
alt_skip_anchor = ('    if (cpi->is_src_frame_alt_ref && (cpi->oxcf.arnr_max_frames == 0)) {\n'
                   '      if (this_mode != ZEROMV ||\n'
                   '          x->e_mbd.mode_info_context->mbmi.ref_frame != ALTREF_FRAME) {\n'
                   '        continue;\n'
                   '      }\n'
                   '    }\n')
alt_skip_replacement = ('    if (cpi->is_src_frame_alt_ref && (cpi->oxcf.arnr_max_frames == 0)) {\n'
                        '      if (this_mode != ZEROMV ||\n'
                        '          x->e_mbd.mode_info_context->mbmi.ref_frame != ALTREF_FRAME) {\n'
                        '        govpx_oracle_emit_iteration_outcome(\n'
                        '            cpi, mb_row, mb_col, mode_index,\n'
                        '            this_mode, this_ref_frame,\n'
                        '            0, 0, "alt_ref_skip", INT_MAX,\n'
                        '            best_rd, x->rd_threshes[mode_index]);\n'
                        '        continue;\n'
                        '      }\n'
                        '    }\n')
if alt_skip_anchor not in text:
    sys.stderr.write('build_vpxenc_oracle.sh: pickinter alt_skip anchor missing\n')
    sys.exit(2)
text = text.replace(alt_skip_anchor, alt_skip_replacement, 1)
near_zero_anchor = ('      case NEARESTMV:\n'
                    '      case NEARMV:\n'
                    '        if (mode_mv[this_mode].as_int == 0) continue;\n')
near_zero_replacement = ('      case NEARESTMV:\n'
                         '      case NEARMV:\n'
                         '        if (mode_mv[this_mode].as_int == 0) {\n'
                         '          govpx_oracle_emit_iteration_outcome(\n'
                         '              cpi, mb_row, mb_col, mode_index,\n'
                         '              this_mode, this_ref_frame,\n'
                         '              mode_mv[this_mode].as_mv.row,\n'
                         '              mode_mv[this_mode].as_mv.col,\n'
                         '              "near_zero_skip", INT_MAX,\n'
                         '              best_rd, x->rd_threshes[mode_index]);\n'
                         '          continue;\n'
                         '        }\n')
if near_zero_anchor not in text:
    sys.stderr.write('build_vpxenc_oracle.sh: pickinter near_zero anchor missing\n')
    sys.exit(2)
text = text.replace(near_zero_anchor, near_zero_replacement, 1)
umv_anchor = ('        if (((mode_mv[this_mode].as_mv.row >> 3) < x->mv_row_min) ||\n'
              '            ((mode_mv[this_mode].as_mv.row >> 3) > x->mv_row_max) ||\n'
              '            ((mode_mv[this_mode].as_mv.col >> 3) < x->mv_col_min) ||\n'
              '            ((mode_mv[this_mode].as_mv.col >> 3) > x->mv_col_max)) {\n'
              '          continue;\n'
              '        }\n')
umv_replacement = ('        if (((mode_mv[this_mode].as_mv.row >> 3) < x->mv_row_min) ||\n'
                   '            ((mode_mv[this_mode].as_mv.row >> 3) > x->mv_row_max) ||\n'
                   '            ((mode_mv[this_mode].as_mv.col >> 3) < x->mv_col_min) ||\n'
                   '            ((mode_mv[this_mode].as_mv.col >> 3) > x->mv_col_max)) {\n'
                   '          govpx_oracle_emit_iteration_outcome(\n'
                   '              cpi, mb_row, mb_col, mode_index,\n'
                   '              this_mode, this_ref_frame,\n'
                   '              mode_mv[this_mode].as_mv.row,\n'
                   '              mode_mv[this_mode].as_mv.col,\n'
                   '              "umv_bounds", INT_MAX,\n'
                   '              best_rd, x->rd_threshes[mode_index]);\n'
                   '          continue;\n'
                   '        }\n')
if umv_anchor not in text:
    sys.stderr.write('build_vpxenc_oracle.sh: pickinter umv anchor missing\n')
    sys.exit(2)
text = text.replace(umv_anchor, umv_replacement, 1)
# R12-C: emit per-MB picker_entry row right after vp8_find_near_mvs_bias
# fills mode_mv_sb / mdcounts and before the per-mode rd_threshes loop.
# The anchor is the unique `*returnintra = INT_MAX;` line that sits between
# the find_near_mvs_bias call and the mode_index loop. ref_frame_map[1]
# gives the ref slot used to seed mode_mv_sb (LAST_FRAME normally).
picker_entry_sentinel = '/* govpx oracle: per-MB picker_entry record. */'
picker_entry_anchor = ('  *returnintra = INT_MAX;\n'
                      '  x->skip = 0;\n')
picker_entry_replacement = ('  *returnintra = INT_MAX;\n'
                            '  x->skip = 0;\n'
                            '\n'
                            '  ' + picker_entry_sentinel + '\n'
                            '  govpx_oracle_emit_picker_entry(\n'
                            '      cpi, mb_row, mb_col, sign_bias,\n'
                            '      ref_frame_map[1] > 0 ? ref_frame_map[1] : 0,\n'
                            '      mode_mv_sb, mdcounts, best_rd);\n')
if picker_entry_anchor not in text:
    sys.stderr.write('build_vpxenc_oracle.sh: pickinter picker_entry anchor missing\n')
    sys.exit(2)
text = text.replace(picker_entry_anchor, picker_entry_replacement, 1)
with io.open(path, 'w', encoding='utf-8') as f:
    f.write(text)
GOVPX_PICKINTER_PY

	# (3.8) Instrument vp8cx_pick_filter_level_fast in picklpf.c so each
	# evaluated trial filter level emits a {"type":"lf_trial",...} row. Three
	# anchor sites: the seed scoring, the decreasing-level loop body, and the
	# increasing-level loop body. Each row records the trial level and the
	# calc_partial_ssl_err result, which lets the govpx-side picker be diffed
	# level-by-level. This localizes a divergence in either the LF function
	# applied to the trial buffer (different filter math) or the partial-SSE
	# region (different rows sampled).
	python3 - "$src_dir/vp8/encoder/picklpf.c" <<'GOVPX_PICKLPF_PY'
import sys, io
path = sys.argv[1]
with io.open(path, 'r', encoding='utf-8') as f:
    text = f.read()
sentinel = '/* govpx oracle: lf-trial emit hook. */'
if sentinel in text:
    sys.exit(0)  # already patched
# Anchor 1: extern decl just before vp8cx_pick_filter_level_fast.
extern_anchor = 'void vp8cx_pick_filter_level_fast(YV12_BUFFER_CONFIG *sd, VP8_COMP *cpi) {\n'
extern_decl = ('extern void govpx_oracle_emit_lf_trial(struct VP8_COMP *cpi,\n'
               '                                       const char *phase,\n'
               '                                       int trial_level,\n'
               '                                       int trial_y_sse);\n\n')
if extern_anchor not in text:
    sys.stderr.write('build_vpxenc_oracle.sh: picklpf extern anchor missing\n')
    sys.exit(2)
text = text.replace(extern_anchor, extern_decl + extern_anchor, 1)
# Anchor 2: seed scoring. Emit immediately after the initial best_err is set.
seed_anchor = ('  best_err = calc_partial_ssl_err(sd, cm->frame_to_show);\n'
               '\n'
               '  filt_val -= 1 + (filt_val > 10);\n')
seed_replacement = ('  best_err = calc_partial_ssl_err(sd, cm->frame_to_show);\n'
                    '  ' + sentinel + '\n'
                    '  govpx_oracle_emit_lf_trial(cpi, "seed", filt_val, best_err);\n'
                    '  /* govpx oracle: emit pre-filter (unfiltered) partial Y SSE so the\n'
                    '   * per-trial diff harness can pin whether the gap is in the LF math\n'
                    '   * or in the upstream reconstruction. saved_frame still points at\n'
                    '   * the post-encode pre-LF Y plane at this point. */\n'
                    '  govpx_oracle_emit_lf_trial(cpi, "pre", 0, calc_partial_ssl_err(sd, saved_frame));\n'
                    '\n'
                    '  filt_val -= 1 + (filt_val > 10);\n')
if seed_anchor not in text:
    sys.stderr.write('build_vpxenc_oracle.sh: picklpf seed anchor missing\n')
    sys.exit(2)
text = text.replace(seed_anchor, seed_replacement, 1)
# Anchor 3: decreasing-level loop. Emit just after calc_partial_ssl_err.
down_anchor = ('  /* Search lower filter levels */\n'
               '  while (filt_val >= min_filter_level) {\n'
               '    /* Apply the loop filter */\n'
               '    yv12_copy_partial_frame(saved_frame, cm->frame_to_show);\n'
               '    vp8_loop_filter_partial_frame(cm, &cpi->mb.e_mbd, filt_val);\n'
               '\n'
               '    /* Get the err for filtered frame */\n'
               '    filt_err = calc_partial_ssl_err(sd, cm->frame_to_show);\n')
down_replacement = ('  /* Search lower filter levels */\n'
                    '  while (filt_val >= min_filter_level) {\n'
                    '    /* Apply the loop filter */\n'
                    '    yv12_copy_partial_frame(saved_frame, cm->frame_to_show);\n'
                    '    vp8_loop_filter_partial_frame(cm, &cpi->mb.e_mbd, filt_val);\n'
                    '\n'
                    '    /* Get the err for filtered frame */\n'
                    '    filt_err = calc_partial_ssl_err(sd, cm->frame_to_show);\n'
                    '    govpx_oracle_emit_lf_trial(cpi, "down", filt_val, filt_err);\n')
if down_anchor not in text:
    sys.stderr.write('build_vpxenc_oracle.sh: picklpf down anchor missing\n')
    sys.exit(2)
text = text.replace(down_anchor, down_replacement, 1)
# Anchor 4: increasing-level loop body.
up_anchor = ('    while (filt_val < max_filter_level) {\n'
             '      /* Apply the loop filter */\n'
             '      yv12_copy_partial_frame(saved_frame, cm->frame_to_show);\n'
             '\n'
             '      vp8_loop_filter_partial_frame(cm, &cpi->mb.e_mbd, filt_val);\n'
             '\n'
             '      /* Get the err for filtered frame */\n'
             '      filt_err = calc_partial_ssl_err(sd, cm->frame_to_show);\n')
up_replacement = ('    while (filt_val < max_filter_level) {\n'
                  '      /* Apply the loop filter */\n'
                  '      yv12_copy_partial_frame(saved_frame, cm->frame_to_show);\n'
                  '\n'
                  '      vp8_loop_filter_partial_frame(cm, &cpi->mb.e_mbd, filt_val);\n'
                  '\n'
                  '      /* Get the err for filtered frame */\n'
                  '      filt_err = calc_partial_ssl_err(sd, cm->frame_to_show);\n'
                  '      govpx_oracle_emit_lf_trial(cpi, "up", filt_val, filt_err);\n')
if up_anchor not in text:
    sys.stderr.write('build_vpxenc_oracle.sh: picklpf up anchor missing\n')
    sys.exit(2)
text = text.replace(up_anchor, up_replacement, 1)
# Anchor 5: the full picker vp8cx_pick_filter_level emits "full" rows after
# each vp8_calc_ss_err call. Three call sites: the baseline filt_mid score,
# the filt_low loop-body score, and the filt_high loop-body score. Each is
# uniquely identifiable by the surrounding context.
full_seed_anchor = ('  best_err = vp8_calc_ss_err(sd, cm->frame_to_show);\n'
                    '\n'
                    '  ss_err[filt_mid] = best_err;\n'
                    '\n'
                    '  filt_best = filt_mid;\n')
full_seed_replacement = ('  best_err = vp8_calc_ss_err(sd, cm->frame_to_show);\n'
                         '  govpx_oracle_emit_lf_trial(cpi, "full", filt_mid, best_err);\n'
                         '  /* govpx oracle: emit pre-filter SSE for full picker by\n'
                         '   * scoring saved_frame (the unfiltered recon) against sd.\n'
                         '   * This pins whether the gap is in LF apply or upstream\n'
                         '   * recon for the per-trial diff harness. */\n'
                         '  govpx_oracle_emit_lf_trial(cpi, "pre", 0, vp8_calc_ss_err(sd, saved_frame));\n'
                         '\n'
                         '  ss_err[filt_mid] = best_err;\n'
                         '\n'
                         '  filt_best = filt_mid;\n')
if full_seed_anchor not in text:
    sys.stderr.write('build_vpxenc_oracle.sh: picklpf full-seed anchor missing\n')
    sys.exit(2)
text = text.replace(full_seed_anchor, full_seed_replacement, 1)
full_low_anchor = ('        filt_err = vp8_calc_ss_err(sd, cm->frame_to_show);\n'
                   '        ss_err[filt_low] = filt_err;\n')
full_low_replacement = ('        filt_err = vp8_calc_ss_err(sd, cm->frame_to_show);\n'
                        '        govpx_oracle_emit_lf_trial(cpi, "full", filt_low, filt_err);\n'
                        '        ss_err[filt_low] = filt_err;\n')
if full_low_anchor not in text:
    sys.stderr.write('build_vpxenc_oracle.sh: picklpf full-low anchor missing\n')
    sys.exit(2)
text = text.replace(full_low_anchor, full_low_replacement, 1)
full_high_anchor = ('        filt_err = vp8_calc_ss_err(sd, cm->frame_to_show);\n'
                    '        ss_err[filt_high] = filt_err;\n')
full_high_replacement = ('        filt_err = vp8_calc_ss_err(sd, cm->frame_to_show);\n'
                         '        govpx_oracle_emit_lf_trial(cpi, "full", filt_high, filt_err);\n'
                         '        ss_err[filt_high] = filt_err;\n')
if full_high_anchor not in text:
    sys.stderr.write('build_vpxenc_oracle.sh: picklpf full-high anchor missing\n')
    sys.exit(2)
text = text.replace(full_high_anchor, full_high_replacement, 1)
with io.open(path, 'w', encoding='utf-8') as f:
    f.write(text)
GOVPX_PICKLPF_PY

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
