#!/usr/bin/env sh
set -eu

# Build a VP9-enabled libvpx vpxdec for the byte-parity oracle gate.
# The default build_libvpx.sh / build_vpxenc.sh both pass
# --disable-vp9 to keep their VP8 binaries lean; build_libvpx_vp9.sh
# enables VP9 but passes --disable-tools (it only needs libvpx.a for
# vp9_dsp_oracle.c). This script builds the standalone vpxdec tool
# with VP9 enabled so the Go-side oracle test can pipe the encoder's
# IVF output through it.
#
# Output binary lands at $build/vpxdec-vp9 by default; override via
# GOVPX_VPXDEC_VP9_BIN.

tag="v1.16.0"
root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
build_dir=${GOVPX_CORACLE_BUILD_DIR:-"$root/build"}
src_dir="$build_dir/libvpx-$tag-vpxdec-vp9"
vpxdec_vp9_bin=${GOVPX_VPXDEC_VP9_BIN:-"$build_dir/vpxdec-vp9"}
vpxenc_vp9_bin=${GOVPX_VPXENC_VP9_BIN:-"$build_dir/vpxenc-vp9"}
vp9_spatial_svc_bin=${GOVPX_VP9_SPATIAL_SVC_ENCODER_BIN:-"$build_dir/vp9_spatial_svc_encoder"}
config_stamp="$src_dir/.govpx-vpxdec-vp9-config"
want_config="v1.16.0-vp9-encoder+decoder-tools-optimized-govpx-decoder-controls-vp9-postproc-vp9-call-stats-r3
src_dir=$src_dir
vpxdec_vp9_bin=$vpxdec_vp9_bin
vpxenc_vp9_bin=$vpxenc_vp9_bin"
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

if [ ! -d "$src_dir" ]; then
	fetch_source
fi

patch_vpxdec_vp9_controls() {
	python3 - "$src_dir/vpxdec.c" <<'PY'
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
text = path.read_text()
sentinel = "govpx-skip-loop-filter"
if sentinel in text:
    sys.exit(0)

text = text.replace(
'''static const arg_def_t lpfoptarg =
    ARG_DEF(NULL, "lpf-opt", 1,
            "Do loopfilter without waiting for all threads to sync.");
''',
'''static const arg_def_t lpfoptarg =
    ARG_DEF(NULL, "lpf-opt", 1,
            "Do loopfilter without waiting for all threads to sync.");
/* govpx-skip-loop-filter: expose VP9 decoder test controls through vpxdec. */
static const arg_def_t govpx_skip_loop_filter_arg =
    ARG_DEF(NULL, "skip-loop-filter", 1,
            "Set VP9_SET_SKIP_LOOP_FILTER before decoding");
static const arg_def_t govpx_invert_tile_order_arg =
    ARG_DEF(NULL, "invert-tile-decode-order", 1,
            "Set VP9_INVERT_TILE_DECODE_ORDER before decoding");
static const arg_def_t govpx_vp9_postproc_flags_arg =
    ARG_DEF(NULL, "vp9-postproc-flags", 1,
            "Set VP8_SET_POSTPROC flags for VP9 decoder oracle");
static const arg_def_t govpx_vp9_postproc_noise_arg =
    ARG_DEF(NULL, "vp9-postproc-noise-level", 1,
            "Set VP8_SET_POSTPROC noise_level for VP9 decoder oracle");
static const arg_def_t govpx_vp9_postproc_deblock_arg =
    ARG_DEF(NULL, "vp9-postproc-deblock-level", 1,
            "Set VP8_SET_POSTPROC deblocking_level for VP9 decoder oracle");
''')
text = text.replace(
'''                                       &framestatsarg,
                                       &rowmtarg,
                                       &lpfoptarg,
                                       NULL };
''',
'''                                       &framestatsarg,
                                       &rowmtarg,
                                       &lpfoptarg,
                                       &govpx_skip_loop_filter_arg,
                                       &govpx_invert_tile_order_arg,
                                       &govpx_vp9_postproc_flags_arg,
                                       &govpx_vp9_postproc_noise_arg,
                                       &govpx_vp9_postproc_deblock_arg,
                                       NULL };
''')
text = text.replace(
'''#if CONFIG_VP8_DECODER
  vp8_postproc_cfg_t vp8_pp_cfg = { 0, 0, 0 };
#endif
''',
'''  vp8_postproc_cfg_t vp8_pp_cfg = { 0, 0, 0 };
''')
text = text.replace(
'''  int enable_row_mt = 0;
  int enable_lpf_opt = 0;
''',
'''  int enable_row_mt = 0;
  int enable_lpf_opt = 0;
  int govpx_skip_loop_filter = 0;
  int govpx_invert_tile_order = 0;
''')
text = text.replace(
'''    } else if (arg_match(&arg, &lpfoptarg, argi)) {
      enable_lpf_opt = arg_parse_uint(&arg);
    }
''',
'''    } else if (arg_match(&arg, &lpfoptarg, argi)) {
      enable_lpf_opt = arg_parse_uint(&arg);
    } else if (arg_match(&arg, &govpx_skip_loop_filter_arg, argi)) {
      govpx_skip_loop_filter = arg_parse_uint(&arg);
    } else if (arg_match(&arg, &govpx_invert_tile_order_arg, argi)) {
      govpx_invert_tile_order = arg_parse_uint(&arg);
    } else if (arg_match(&arg, &govpx_vp9_postproc_flags_arg, argi)) {
      postproc = 1;
      vp8_pp_cfg.post_proc_flag = arg_parse_uint(&arg);
    } else if (arg_match(&arg, &govpx_vp9_postproc_noise_arg, argi)) {
      postproc = 1;
      vp8_pp_cfg.noise_level = arg_parse_uint(&arg);
    } else if (arg_match(&arg, &govpx_vp9_postproc_deblock_arg, argi)) {
      postproc = 1;
      vp8_pp_cfg.deblocking_level = arg_parse_uint(&arg);
    }
''')
text = text.replace(
'''#if CONFIG_VP8_DECODER
  if (vp8_pp_cfg.post_proc_flag &&
      vpx_codec_control(&decoder, VP8_SET_POSTPROC, &vp8_pp_cfg)) {
    fprintf(stderr, "Failed to configure postproc: %s\\n",
            vpx_codec_error(&decoder));
    goto fail;
  }
#endif
''',
'''  if (vp8_pp_cfg.post_proc_flag &&
      vpx_codec_control(&decoder, VP8_SET_POSTPROC, &vp8_pp_cfg)) {
    fprintf(stderr, "Failed to configure postproc: %s\\n",
            vpx_codec_error(&decoder));
    goto fail;
  }
''')
text = text.replace(
'''  if (interface->fourcc == VP9_FOURCC &&
      vpx_codec_control(&decoder, VP9D_SET_LOOP_FILTER_OPT, enable_lpf_opt)) {
    fprintf(stderr, "Failed to set decoder in optimized loopfilter mode: %s\\n",
            vpx_codec_error(&decoder));
    goto fail;
  }
''',
'''  if (interface->fourcc == VP9_FOURCC &&
      vpx_codec_control(&decoder, VP9D_SET_LOOP_FILTER_OPT, enable_lpf_opt)) {
    fprintf(stderr, "Failed to set decoder in optimized loopfilter mode: %s\\n",
            vpx_codec_error(&decoder));
    goto fail;
  }
  if (interface->fourcc == VP9_FOURCC &&
      vpx_codec_control(&decoder, VP9_SET_SKIP_LOOP_FILTER,
                        govpx_skip_loop_filter)) {
    fprintf(stderr, "Failed to set decoder skip-loop-filter mode: %s\\n",
            vpx_codec_error(&decoder));
    goto fail;
  }
  if (interface->fourcc == VP9_FOURCC &&
      vpx_codec_control(&decoder, VP9_INVERT_TILE_DECODE_ORDER,
                        govpx_invert_tile_order)) {
    fprintf(stderr, "Failed to set decoder inverted tile order: %s\\n",
            vpx_codec_error(&decoder));
    goto fail;
  }
''')

if sentinel not in text:
    raise SystemExit("failed to patch vpxdec VP9 control hooks")
path.write_text(text)
PY
}

patch_vp9_call_stats() {
	python3 - "$src_dir" <<'PY'
import pathlib
import sys

root = pathlib.Path(sys.argv[1])

def replace_once(path, old, new):
    text = path.read_text()
    if new in text:
        return
    if old not in text:
        raise SystemExit(f"failed to patch {path}: missing needle")
    path.write_text(text.replace(old, new, 1))

header = root / "vp9" / "govpx_call_stats.h"
header.write_text("""#ifndef VPX_VP9_GOVPX_CALL_STATS_H_
#define VPX_VP9_GOVPX_CALL_STATS_H_

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

void govpx_vp9_count_inter_mode_pick(void);
void govpx_vp9_count_inter_mode_sub8x8_pick(void);
void govpx_vp9_count_build_inter_predictors_sby(void);
void govpx_vp9_count_build_inter_predictors_sbp(void);
void govpx_vp9_count_build_inter_predictors_sbuv(void);
void govpx_vp9_count_build_inter_predictors_sb(void);
void govpx_vp9_count_build_inter_predictors_plane(void);
void govpx_vp9_count_single_inter_predictor_build(void);
void govpx_vp9_count_fullpel_search(void);
void govpx_vp9_count_inter_predictor(int key);
void govpx_vp9_count_sad_candidates(uint64_t candidates, int batch);
void govpx_vp9_count_mode_block(int bsize);
void govpx_vp9_count_varpart_choose(int copied);
void govpx_vp9_count_varpart_content_state(int state);
void govpx_vp9_count_varpart_ysad(void);
void govpx_vp9_count_varpart_ysad_select64(void);
void govpx_vp9_count_varpart_copy_select(void);
void govpx_vp9_count_varpart_force_split(int bsize);
void govpx_vp9_count_varpart_threshold2(uint64_t threshold);
void govpx_vp9_count_varpart_var16(uint64_t variance);
void govpx_vp9_count_varpart_force_split16_variance(uint64_t variance, uint64_t threshold);
void govpx_vp9_count_varpart_force_split16_minmax(void);
void govpx_vp9_count_varpart_setvt(int bsize, int force_split, int selected);

#ifdef __cplusplus
}  // extern "C"
#endif

#endif  // VPX_VP9_GOVPX_CALL_STATS_H_
""")

pickmode = root / "vp9" / "encoder" / "vp9_pickmode.c"
replace_once(pickmode,
'''#include "vp9/common/vp9_blockd.h"
#include "vp9/common/vp9_common.h"
''',
'''#include "vp9/common/vp9_blockd.h"
#include "vp9/common/vp9_common.h"
#include "vp9/govpx_call_stats.h"
''')
replace_once(pickmode,
'''#include "vp9/encoder/vp9_rd.h"
''',
'''#include "vp9/encoder/vp9_rd.h"

static uint64_t govpx_vp9_inter_mode_picks;
static uint64_t govpx_vp9_inter_mode_sub8x8_picks;
static uint64_t govpx_vp9_build_sby;
static uint64_t govpx_vp9_build_sbp;
static uint64_t govpx_vp9_build_sbuv;
static uint64_t govpx_vp9_build_sb;
static uint64_t govpx_vp9_build_planes;
static uint64_t govpx_vp9_single_predictor_builds;
static uint64_t govpx_vp9_fullpel_searches;
static uint64_t govpx_vp9_sad_calls;
static uint64_t govpx_vp9_sad_candidates;
static uint64_t govpx_vp9_sad_batch_calls;
static uint64_t govpx_vp9_predictor[8];
static uint64_t govpx_vp9_mode_block[BLOCK_SIZES];
static uint64_t govpx_vp9_varpart_choose_calls;
static uint64_t govpx_vp9_varpart_copy_hits;
static uint64_t govpx_vp9_varpart_content_state[7];
static uint64_t govpx_vp9_varpart_ysad_valid;
static uint64_t govpx_vp9_varpart_ysad_select_64x64;
static uint64_t govpx_vp9_varpart_copy_partition_select;
static uint64_t govpx_vp9_varpart_force_split_64;
static uint64_t govpx_vp9_varpart_force_split_32;
static uint64_t govpx_vp9_varpart_force_split_16;
static uint64_t govpx_vp9_varpart_force_split_16_variance;
static uint64_t govpx_vp9_varpart_force_split_16_minmax;
static uint64_t govpx_vp9_varpart_threshold2_count;
static uint64_t govpx_vp9_varpart_threshold2_sum;
static uint64_t govpx_vp9_varpart_var16_samples;
static uint64_t govpx_vp9_varpart_var16_sum;
static uint64_t govpx_vp9_varpart_force16_variance_sum;
static uint64_t govpx_vp9_varpart_force16_threshold_sum;
static uint64_t govpx_vp9_varpart_setvt_calls;
static uint64_t govpx_vp9_varpart_setvt[BLOCK_SIZES];
static uint64_t govpx_vp9_varpart_setvt_force_split;
static uint64_t govpx_vp9_varpart_setvt_force_split_64;
static uint64_t govpx_vp9_varpart_setvt_force_split_32;
static uint64_t govpx_vp9_varpart_setvt_force_split_16;
static uint64_t govpx_vp9_varpart_setvt_select;
static uint64_t govpx_vp9_varpart_setvt_split;

static void govpx_vp9_add_u64(uint64_t *value, uint64_t delta) {
  __sync_fetch_and_add(value, delta);
}

void govpx_vp9_count_inter_mode_pick(void) {
  govpx_vp9_add_u64(&govpx_vp9_inter_mode_picks, 1);
}

void govpx_vp9_count_inter_mode_sub8x8_pick(void) {
  govpx_vp9_add_u64(&govpx_vp9_inter_mode_sub8x8_picks, 1);
}

void govpx_vp9_count_build_inter_predictors_sby(void) {
  govpx_vp9_add_u64(&govpx_vp9_build_sby, 1);
}

void govpx_vp9_count_build_inter_predictors_sbp(void) {
  govpx_vp9_add_u64(&govpx_vp9_build_sbp, 1);
}

void govpx_vp9_count_build_inter_predictors_sbuv(void) {
  govpx_vp9_add_u64(&govpx_vp9_build_sbuv, 1);
}

void govpx_vp9_count_build_inter_predictors_sb(void) {
  govpx_vp9_add_u64(&govpx_vp9_build_sb, 1);
}

void govpx_vp9_count_build_inter_predictors_plane(void) {
  govpx_vp9_add_u64(&govpx_vp9_build_planes, 1);
}

void govpx_vp9_count_single_inter_predictor_build(void) {
  govpx_vp9_add_u64(&govpx_vp9_single_predictor_builds, 1);
}

void govpx_vp9_count_fullpel_search(void) {
  govpx_vp9_add_u64(&govpx_vp9_fullpel_searches, 1);
}

void govpx_vp9_count_inter_predictor(int key) {
  if (key >= 0 && key < 8) govpx_vp9_add_u64(&govpx_vp9_predictor[key], 1);
}

void govpx_vp9_count_sad_candidates(uint64_t candidates, int batch) {
  govpx_vp9_add_u64(&govpx_vp9_sad_calls, 1);
  govpx_vp9_add_u64(&govpx_vp9_sad_candidates, candidates);
  if (batch) govpx_vp9_add_u64(&govpx_vp9_sad_batch_calls, 1);
}

void govpx_vp9_count_mode_block(int bsize) {
  if (bsize >= 0 && bsize < BLOCK_SIZES)
    govpx_vp9_add_u64(&govpx_vp9_mode_block[bsize], 1);
}

void govpx_vp9_count_varpart_choose(int copied) {
  govpx_vp9_add_u64(&govpx_vp9_varpart_choose_calls, 1);
  if (copied) govpx_vp9_add_u64(&govpx_vp9_varpart_copy_hits, 1);
}

void govpx_vp9_count_varpart_content_state(int state) {
  if (state >= 0 && state < 7)
    govpx_vp9_add_u64(&govpx_vp9_varpart_content_state[state], 1);
}

void govpx_vp9_count_varpart_ysad(void) {
  govpx_vp9_add_u64(&govpx_vp9_varpart_ysad_valid, 1);
}

void govpx_vp9_count_varpart_ysad_select64(void) {
  govpx_vp9_add_u64(&govpx_vp9_varpart_ysad_select_64x64, 1);
}

void govpx_vp9_count_varpart_copy_select(void) {
  govpx_vp9_add_u64(&govpx_vp9_varpart_copy_partition_select, 1);
}

void govpx_vp9_count_varpart_force_split(int bsize) {
  if (bsize == BLOCK_64X64)
    govpx_vp9_add_u64(&govpx_vp9_varpart_force_split_64, 1);
  else if (bsize == BLOCK_32X32)
    govpx_vp9_add_u64(&govpx_vp9_varpart_force_split_32, 1);
  else if (bsize == BLOCK_16X16)
    govpx_vp9_add_u64(&govpx_vp9_varpart_force_split_16, 1);
}

void govpx_vp9_count_varpart_threshold2(uint64_t threshold) {
  govpx_vp9_add_u64(&govpx_vp9_varpart_threshold2_count, 1);
  if (threshold < 0x7fffffffffffffffULL)
    govpx_vp9_add_u64(&govpx_vp9_varpart_threshold2_sum, threshold);
}

void govpx_vp9_count_varpart_var16(uint64_t variance) {
  govpx_vp9_add_u64(&govpx_vp9_varpart_var16_samples, 1);
  govpx_vp9_add_u64(&govpx_vp9_varpart_var16_sum, variance);
}

void govpx_vp9_count_varpart_force_split16_variance(uint64_t variance, uint64_t threshold) {
  govpx_vp9_count_varpart_force_split(BLOCK_16X16);
  govpx_vp9_add_u64(&govpx_vp9_varpart_force_split_16_variance, 1);
  govpx_vp9_add_u64(&govpx_vp9_varpart_force16_variance_sum, variance);
  if (threshold < 0x7fffffffffffffffULL)
    govpx_vp9_add_u64(&govpx_vp9_varpart_force16_threshold_sum, threshold);
}

void govpx_vp9_count_varpart_force_split16_minmax(void) {
  govpx_vp9_count_varpart_force_split(BLOCK_16X16);
  govpx_vp9_add_u64(&govpx_vp9_varpart_force_split_16_minmax, 1);
}

void govpx_vp9_count_varpart_setvt(int bsize, int force_split, int selected) {
  govpx_vp9_add_u64(&govpx_vp9_varpart_setvt_calls, 1);
  if (bsize >= 0 && bsize < BLOCK_SIZES)
    govpx_vp9_add_u64(&govpx_vp9_varpart_setvt[bsize], 1);
  if (force_split) {
    govpx_vp9_add_u64(&govpx_vp9_varpart_setvt_force_split, 1);
    if (bsize == BLOCK_64X64)
      govpx_vp9_add_u64(&govpx_vp9_varpart_setvt_force_split_64, 1);
    else if (bsize == BLOCK_32X32)
      govpx_vp9_add_u64(&govpx_vp9_varpart_setvt_force_split_32, 1);
    else if (bsize == BLOCK_16X16)
      govpx_vp9_add_u64(&govpx_vp9_varpart_setvt_force_split_16, 1);
  }
  if (selected)
    govpx_vp9_add_u64(&govpx_vp9_varpart_setvt_select, 1);
  else
    govpx_vp9_add_u64(&govpx_vp9_varpart_setvt_split, 1);
}

__attribute__((destructor)) static void govpx_vp9_dump_call_stats(void) {
  if (getenv("GOVPX_LIBVPX_VP9_CALL_STATS") == NULL) return;
  fprintf(stderr,
          "LIBVPX_VP9_CALL_STATS inter_mode_picks=%llu "
          "inter_mode_sub8x8_picks=%llu build_sby=%llu build_sbp=%llu "
          "build_sbuv=%llu build_sb=%llu build_planes=%llu "
          "single_predictor_builds=%llu fullpel_searches=%llu "
          "sad_calls=%llu sad_candidates=%llu sad_batch_calls=%llu "
          "predictor_copy=%llu predictor_avg=%llu predictor_vert=%llu "
          "predictor_avg_vert=%llu predictor_horiz=%llu "
          "predictor_avg_horiz=%llu predictor_2d=%llu "
          "predictor_avg_2d=%llu mode_block_64x64=%llu "
          "mode_block_32x32=%llu mode_block_32x16=%llu "
          "mode_block_16x32=%llu mode_block_16x16=%llu "
          "mode_block_16x8=%llu mode_block_8x16=%llu "
          "mode_block_8x8=%llu mode_block_sub8=%llu "
          "varpart_choose_calls=%llu varpart_copy_hits=%llu "
          "varpart_content_state_invalid=%llu "
          "varpart_content_state_low_sad_low_sumdiff=%llu "
          "varpart_content_state_low_sad_high_sumdiff=%llu "
          "varpart_content_state_high_sad_low_sumdiff=%llu "
          "varpart_content_state_high_sad_high_sumdiff=%llu "
          "varpart_content_state_low_var_high_sumdiff=%llu "
          "varpart_content_state_very_high_sad=%llu "
          "varpart_ysad_valid=%llu "
          "varpart_ysad_select_64x64=%llu "
          "varpart_copy_partition_select=%llu "
          "varpart_force_split_64=%llu "
          "varpart_force_split_32=%llu "
          "varpart_force_split_16=%llu "
          "varpart_force_split_16_variance=%llu "
          "varpart_force_split_16_minmax=%llu "
          "varpart_threshold2_count=%llu "
          "varpart_threshold2_sum=%llu "
          "varpart_var16_samples=%llu "
          "varpart_var16_sum=%llu "
          "varpart_force16_variance_sum=%llu "
          "varpart_force16_threshold_sum=%llu "
          "varpart_setvt_calls=%llu "
          "varpart_setvt_64x64=%llu "
          "varpart_setvt_32x32=%llu "
          "varpart_setvt_16x16=%llu "
          "varpart_setvt_8x8=%llu "
          "varpart_setvt_force_split=%llu "
          "varpart_setvt_force_split_64x64=%llu "
          "varpart_setvt_force_split_32x32=%llu "
          "varpart_setvt_force_split_16x16=%llu "
          "varpart_setvt_select=%llu "
          "varpart_setvt_split=%llu\\n",
          (unsigned long long)govpx_vp9_inter_mode_picks,
          (unsigned long long)govpx_vp9_inter_mode_sub8x8_picks,
          (unsigned long long)govpx_vp9_build_sby,
          (unsigned long long)govpx_vp9_build_sbp,
          (unsigned long long)govpx_vp9_build_sbuv,
          (unsigned long long)govpx_vp9_build_sb,
          (unsigned long long)govpx_vp9_build_planes,
          (unsigned long long)govpx_vp9_single_predictor_builds,
          (unsigned long long)govpx_vp9_fullpel_searches,
          (unsigned long long)govpx_vp9_sad_calls,
          (unsigned long long)govpx_vp9_sad_candidates,
          (unsigned long long)govpx_vp9_sad_batch_calls,
          (unsigned long long)govpx_vp9_predictor[0],
          (unsigned long long)govpx_vp9_predictor[1],
          (unsigned long long)govpx_vp9_predictor[2],
          (unsigned long long)govpx_vp9_predictor[3],
          (unsigned long long)govpx_vp9_predictor[4],
          (unsigned long long)govpx_vp9_predictor[5],
          (unsigned long long)govpx_vp9_predictor[6],
          (unsigned long long)govpx_vp9_predictor[7],
          (unsigned long long)govpx_vp9_mode_block[BLOCK_64X64],
          (unsigned long long)govpx_vp9_mode_block[BLOCK_32X32],
          (unsigned long long)govpx_vp9_mode_block[BLOCK_32X16],
          (unsigned long long)govpx_vp9_mode_block[BLOCK_16X32],
          (unsigned long long)govpx_vp9_mode_block[BLOCK_16X16],
          (unsigned long long)govpx_vp9_mode_block[BLOCK_16X8],
          (unsigned long long)govpx_vp9_mode_block[BLOCK_8X16],
          (unsigned long long)govpx_vp9_mode_block[BLOCK_8X8],
          (unsigned long long)(govpx_vp9_mode_block[BLOCK_4X4] +
                               govpx_vp9_mode_block[BLOCK_4X8] +
                               govpx_vp9_mode_block[BLOCK_8X4]),
          (unsigned long long)govpx_vp9_varpart_choose_calls,
          (unsigned long long)govpx_vp9_varpart_copy_hits,
          (unsigned long long)govpx_vp9_varpart_content_state[0],
          (unsigned long long)govpx_vp9_varpart_content_state[1],
          (unsigned long long)govpx_vp9_varpart_content_state[2],
          (unsigned long long)govpx_vp9_varpart_content_state[3],
          (unsigned long long)govpx_vp9_varpart_content_state[4],
          (unsigned long long)govpx_vp9_varpart_content_state[5],
          (unsigned long long)govpx_vp9_varpart_content_state[6],
          (unsigned long long)govpx_vp9_varpart_ysad_valid,
          (unsigned long long)govpx_vp9_varpart_ysad_select_64x64,
          (unsigned long long)govpx_vp9_varpart_copy_partition_select,
          (unsigned long long)govpx_vp9_varpart_force_split_64,
          (unsigned long long)govpx_vp9_varpart_force_split_32,
          (unsigned long long)govpx_vp9_varpart_force_split_16,
          (unsigned long long)govpx_vp9_varpart_force_split_16_variance,
          (unsigned long long)govpx_vp9_varpart_force_split_16_minmax,
          (unsigned long long)govpx_vp9_varpart_threshold2_count,
          (unsigned long long)govpx_vp9_varpart_threshold2_sum,
          (unsigned long long)govpx_vp9_varpart_var16_samples,
          (unsigned long long)govpx_vp9_varpart_var16_sum,
          (unsigned long long)govpx_vp9_varpart_force16_variance_sum,
          (unsigned long long)govpx_vp9_varpart_force16_threshold_sum,
          (unsigned long long)govpx_vp9_varpart_setvt_calls,
          (unsigned long long)govpx_vp9_varpart_setvt[BLOCK_64X64],
          (unsigned long long)govpx_vp9_varpart_setvt[BLOCK_32X32],
          (unsigned long long)govpx_vp9_varpart_setvt[BLOCK_16X16],
          (unsigned long long)govpx_vp9_varpart_setvt[BLOCK_8X8],
          (unsigned long long)govpx_vp9_varpart_setvt_force_split,
          (unsigned long long)govpx_vp9_varpart_setvt_force_split_64,
          (unsigned long long)govpx_vp9_varpart_setvt_force_split_32,
          (unsigned long long)govpx_vp9_varpart_setvt_force_split_16,
          (unsigned long long)govpx_vp9_varpart_setvt_select,
          (unsigned long long)govpx_vp9_varpart_setvt_split);
}
''')
replace_once(pickmode,
'''void vp9_pick_inter_mode(VP9_COMP *cpi, MACROBLOCK *x, TileDataEnc *tile_data,
                         int mi_row, int mi_col, RD_COST *rd_cost,
                         BLOCK_SIZE bsize, PICK_MODE_CONTEXT *ctx) {
''',
'''void vp9_pick_inter_mode(VP9_COMP *cpi, MACROBLOCK *x, TileDataEnc *tile_data,
                         int mi_row, int mi_col, RD_COST *rd_cost,
                         BLOCK_SIZE bsize, PICK_MODE_CONTEXT *ctx) {
  govpx_vp9_count_inter_mode_pick();
''')
replace_once(pickmode,
'''void vp9_pick_inter_mode_sub8x8(VP9_COMP *cpi, MACROBLOCK *x, int mi_row,
                                int mi_col, RD_COST *rd_cost, BLOCK_SIZE bsize,
                                PICK_MODE_CONTEXT *ctx) {
''',
'''void vp9_pick_inter_mode_sub8x8(VP9_COMP *cpi, MACROBLOCK *x, int mi_row,
                                int mi_col, RD_COST *rd_cost, BLOCK_SIZE bsize,
                                PICK_MODE_CONTEXT *ctx) {
  govpx_vp9_count_inter_mode_sub8x8_pick();
''')

recon_h = root / "vp9" / "common" / "vp9_reconinter.h"
replace_once(recon_h,
'''#include "vp9/common/vp9_filter.h"
#include "vp9/common/vp9_onyxc_int.h"
''',
'''#include "vp9/common/vp9_filter.h"
#include "vp9/govpx_call_stats.h"
#include "vp9/common/vp9_onyxc_int.h"
''')
replace_once(recon_h,
'''static INLINE void inter_predictor(const uint8_t *src, int src_stride,
                                   uint8_t *dst, int dst_stride,
                                   const int subpel_x, const int subpel_y,
                                   const struct scale_factors *sf, int w, int h,
                                   int ref, const InterpKernel *kernel, int xs,
                                   int ys) {
''',
'''static INLINE void inter_predictor(const uint8_t *src, int src_stride,
                                   uint8_t *dst, int dst_stride,
                                   const int subpel_x, const int subpel_y,
                                   const struct scale_factors *sf, int w, int h,
                                   int ref, const InterpKernel *kernel, int xs,
                                   int ys) {
  const int key = (ref != 0) | ((subpel_x != 0 || xs != 16) ? 4 : 0) |
                  ((subpel_y != 0 || ys != 16) ? 2 : 0);
  govpx_vp9_count_inter_predictor(key);
''')
replace_once(recon_h,
'''static INLINE void highbd_inter_predictor(
    const uint16_t *src, int src_stride, uint16_t *dst, int dst_stride,
    const int subpel_x, const int subpel_y, const struct scale_factors *sf,
    int w, int h, int ref, const InterpKernel *kernel, int xs, int ys, int bd) {
''',
'''static INLINE void highbd_inter_predictor(
    const uint16_t *src, int src_stride, uint16_t *dst, int dst_stride,
    const int subpel_x, const int subpel_y, const struct scale_factors *sf,
    int w, int h, int ref, const InterpKernel *kernel, int xs, int ys, int bd) {
  const int key = (ref != 0) | ((subpel_x != 0 || xs != 16) ? 4 : 0) |
                  ((subpel_y != 0 || ys != 16) ? 2 : 0);
  govpx_vp9_count_inter_predictor(key);
''')

recon_c = root / "vp9" / "common" / "vp9_reconinter.c"
for old, new in [
    ('''enum mv_precision precision, int x, int y) {
''', '''enum mv_precision precision, int x, int y) {
  govpx_vp9_count_single_inter_predictor_build();
'''),
    ('''int mi_x, int mi_y) {
''', '''int mi_x, int mi_y) {
  govpx_vp9_count_build_inter_predictors_plane();
'''),
    ('''void vp9_build_inter_predictors_sby(MACROBLOCKD *xd, int mi_row, int mi_col,
                                    BLOCK_SIZE bsize) {
''', '''void vp9_build_inter_predictors_sby(MACROBLOCKD *xd, int mi_row, int mi_col,
                                    BLOCK_SIZE bsize) {
  govpx_vp9_count_build_inter_predictors_sby();
'''),
    ('''void vp9_build_inter_predictors_sbp(MACROBLOCKD *xd, int mi_row, int mi_col,
                                    BLOCK_SIZE bsize, int plane) {
''', '''void vp9_build_inter_predictors_sbp(MACROBLOCKD *xd, int mi_row, int mi_col,
                                    BLOCK_SIZE bsize, int plane) {
  govpx_vp9_count_build_inter_predictors_sbp();
'''),
    ('''void vp9_build_inter_predictors_sbuv(MACROBLOCKD *xd, int mi_row, int mi_col,
                                     BLOCK_SIZE bsize) {
''', '''void vp9_build_inter_predictors_sbuv(MACROBLOCKD *xd, int mi_row, int mi_col,
                                     BLOCK_SIZE bsize) {
  govpx_vp9_count_build_inter_predictors_sbuv();
'''),
    ('''void vp9_build_inter_predictors_sb(MACROBLOCKD *xd, int mi_row, int mi_col,
                                   BLOCK_SIZE bsize) {
''', '''void vp9_build_inter_predictors_sb(MACROBLOCKD *xd, int mi_row, int mi_col,
                                   BLOCK_SIZE bsize) {
  govpx_vp9_count_build_inter_predictors_sb();
'''),
]:
    replace_once(recon_c, old, new)

mcomp = root / "vp9" / "encoder" / "vp9_mcomp.c"
replace_once(mcomp,
'''#include "vp9/common/vp9_common.h"
#include "vp9/common/vp9_mvref_common.h"
''',
'''#include "vp9/common/vp9_common.h"
#include "vp9/govpx_call_stats.h"
#include "vp9/common/vp9_mvref_common.h"
''')
replace_once(mcomp,
'''const MV *ref_mv, MV *tmp_mv, int var_max, int rd) {
''',
'''const MV *ref_mv, MV *tmp_mv, int var_max, int rd) {
  govpx_vp9_count_fullpel_search();
''')

encframe = root / "vp9" / "encoder" / "vp9_encodeframe.c"
replace_once(encframe,
'''#include "vp9/common/vp9_entropymode.h"
#include "vp9/common/vp9_idct.h"
''',
'''#include "vp9/common/vp9_entropymode.h"
#include "vp9/govpx_call_stats.h"
#include "vp9/common/vp9_idct.h"
''')
for old, new in [
    ('''      if (cpi->sf.svc_use_lowres_part &&
          cpi->svc.spatial_layer_id == cpi->svc.number_spatial_layers - 2)
        update_partition_svc(cpi, BLOCK_64X64, mi_row, mi_col);
      return 0;
    }
  }

  if (cpi->oxcf.aq_mode == CYCLIC_REFRESH_AQ && cm->seg.enabled &&
''', '''      if (cpi->sf.svc_use_lowres_part &&
          cpi->svc.spatial_layer_id == cpi->svc.number_spatial_layers - 2)
        update_partition_svc(cpi, BLOCK_64X64, mi_row, mi_col);
      govpx_vp9_count_varpart_copy_select();
      govpx_vp9_count_varpart_choose(1);
      return 0;
    }
  }

  if (cpi->oxcf.aq_mode == CYCLIC_REFRESH_AQ && cm->seg.enabled &&
'''),
    ('''    int q = vp9_get_qindex(&cm->seg, segment_id, cm->base_qindex);
    set_vbp_thresholds(cpi, thresholds, q, content_state);
  } else {
    set_vbp_thresholds(cpi, thresholds, cm->base_qindex, content_state);
  }
''', '''    int q = vp9_get_qindex(&cm->seg, segment_id, cm->base_qindex);
    govpx_vp9_count_varpart_content_state(content_state);
    set_vbp_thresholds(cpi, thresholds, q, content_state);
  } else {
    govpx_vp9_count_varpart_content_state(content_state);
    set_vbp_thresholds(cpi, thresholds, cm->base_qindex, content_state);
  }
  govpx_vp9_count_varpart_threshold2((uint64_t)thresholds[2]);
'''),
    ('''  if (force_split == 1) return 0;
''', '''  if (force_split == 1) {
    govpx_vp9_count_varpart_setvt(bsize, 1, 0);
    return 0;
  }
'''),
    ('''    if (mi_col + block_width / 2 < cm->mi_cols &&
        mi_row + block_height / 2 < cm->mi_rows &&
        vt.part_variances->none.variance < threshold) {
      set_block_size(cpi, x, xd, mi_row, mi_col, bsize);
      return 1;
    }
    return 0;
  } else if (bsize > bsize_min) {
''', '''    if (mi_col + block_width / 2 < cm->mi_cols &&
        mi_row + block_height / 2 < cm->mi_rows &&
        vt.part_variances->none.variance < threshold) {
      set_block_size(cpi, x, xd, mi_row, mi_col, bsize);
      govpx_vp9_count_varpart_setvt(bsize, 0, 1);
      return 1;
    }
    govpx_vp9_count_varpart_setvt(bsize, 0, 0);
    return 0;
  } else if (bsize > bsize_min) {
'''),
    ('''    if (frame_is_intra_only(cm) &&
        (bsize > BLOCK_32X32 ||
         vt.part_variances->none.variance > (threshold << 4))) {
      return 0;
    }
''', '''    if (frame_is_intra_only(cm) &&
        (bsize > BLOCK_32X32 ||
         vt.part_variances->none.variance > (threshold << 4))) {
      govpx_vp9_count_varpart_setvt(bsize, 0, 0);
      return 0;
    }
'''),
    ('''    if (mi_col + block_width / 2 < cm->mi_cols &&
        mi_row + block_height / 2 < cm->mi_rows &&
        vt.part_variances->none.variance < threshold) {
      set_block_size(cpi, x, xd, mi_row, mi_col, bsize);
      return 1;
    }

    // Check vertical split.
''', '''    if (mi_col + block_width / 2 < cm->mi_cols &&
        mi_row + block_height / 2 < cm->mi_rows &&
        vt.part_variances->none.variance < threshold) {
      set_block_size(cpi, x, xd, mi_row, mi_col, bsize);
      govpx_vp9_count_varpart_setvt(bsize, 0, 1);
      return 1;
    }

    // Check vertical split.
'''),
    ('''        set_block_size(cpi, x, xd, mi_row, mi_col, subsize);
        set_block_size(cpi, x, xd, mi_row, mi_col + block_width / 2, subsize);
        return 1;
      }
    }
    // Check horizontal split.
''', '''        set_block_size(cpi, x, xd, mi_row, mi_col, subsize);
        set_block_size(cpi, x, xd, mi_row, mi_col + block_width / 2, subsize);
        govpx_vp9_count_varpart_setvt(bsize, 0, 1);
        return 1;
      }
    }
    // Check horizontal split.
'''),
    ('''        set_block_size(cpi, x, xd, mi_row, mi_col, subsize);
        set_block_size(cpi, x, xd, mi_row + block_height / 2, mi_col, subsize);
        return 1;
      }
    }

    return 0;
  }
  return 0;
}
''', '''        set_block_size(cpi, x, xd, mi_row, mi_col, subsize);
        set_block_size(cpi, x, xd, mi_row + block_height / 2, mi_col, subsize);
        govpx_vp9_count_varpart_setvt(bsize, 0, 1);
        return 1;
      }
    }

    govpx_vp9_count_varpart_setvt(bsize, 0, 0);
    return 0;
  }
  govpx_vp9_count_varpart_setvt(bsize, 0, 0);
  return 0;
}
'''),
    ('''  force_split[0] = force_64_split;
''', '''  force_split[0] = force_64_split;
  if (force_64_split) govpx_vp9_count_varpart_force_split(BLOCK_64X64);
'''),
    ('''    y_sad_last = y_sad;
''', '''    y_sad_last = y_sad;
    govpx_vp9_count_varpart_ysad();
'''),
    ('''        set_block_size(cpi, x, xd, mi_row, mi_col, BLOCK_64X64);
        x->variance_low[0] = 1;
''', '''        set_block_size(cpi, x, xd, mi_row, mi_col, BLOCK_64X64);
        govpx_vp9_count_varpart_ysad_select64();
        x->variance_low[0] = 1;
'''),
    ('''        if (cpi->sf.copy_partition_flag) {
          update_prev_partition(cpi, x, segment_id, mi_row, mi_col, sb_offset);
        }
        return 0;
      }
''', '''        if (cpi->sf.copy_partition_flag) {
          update_prev_partition(cpi, x, segment_id, mi_row, mi_col, sb_offset);
        }
        govpx_vp9_count_varpart_choose(0);
        return 0;
      }
'''),
    ('''      if (cpi->sf.svc_use_lowres_part &&
          cpi->svc.spatial_layer_id == cpi->svc.number_spatial_layers - 2)
        update_partition_svc(cpi, BLOCK_64X64, mi_row, mi_col);
      return 0;
    }
  } else {
''', '''      if (cpi->sf.svc_use_lowres_part &&
          cpi->svc.spatial_layer_id == cpi->svc.number_spatial_layers - 2)
        update_partition_svc(cpi, BLOCK_64X64, mi_row, mi_col);
      govpx_vp9_count_varpart_copy_select();
      govpx_vp9_count_varpart_choose(1);
      return 0;
    }
  } else {
'''),
    ('''        get_variance(&vt.split[i].split[j].part_variances.none);
        avg_16x16[i] += vt.split[i].split[j].part_variances.none.variance;
''', '''        get_variance(&vt.split[i].split[j].part_variances.none);
        govpx_vp9_count_varpart_var16(
            vt.split[i].split[j].part_variances.none.variance);
        avg_16x16[i] += vt.split[i].split[j].part_variances.none.variance;
'''),
    ('''          force_split[split_index] = 1;
          force_split[i + 1] = 1;
          force_split[0] = 1;
''', '''          govpx_vp9_count_varpart_force_split16_variance(
              vt.split[i].split[j].part_variances.none.variance,
              (uint64_t)thresholds[2]);
          force_split[split_index] = 1;
          force_split[i + 1] = 1;
          force_split[0] = 1;
'''),
    ('''            force_split[split_index] = 1;
            force_split[i + 1] = 1;
            force_split[0] = 1;
''', '''            govpx_vp9_count_varpart_force_split16_minmax();
            force_split[split_index] = 1;
            force_split[i + 1] = 1;
            force_split[0] = 1;
'''),
    ('''          force_split[5 + i2 + j] = 1;
          force_split[i + 1] = 1;
          force_split[0] = 1;
''', '''          govpx_vp9_count_varpart_force_split16_variance(
              vtemp->part_variances.none.variance,
              (uint64_t)thresholds[2]);
          force_split[5 + i2 + j] = 1;
          force_split[i + 1] = 1;
          force_split[0] = 1;
'''),
    ('''        force_split[i + 1] = 1;
        force_split[0] = 1;
      } else if (!is_key_frame && noise_level < kLow && cm->height <= 360 &&
''', '''        govpx_vp9_count_varpart_force_split(BLOCK_32X32);
        force_split[i + 1] = 1;
        force_split[0] = 1;
      } else if (!is_key_frame && noise_level < kLow && cm->height <= 360 &&
'''),
    ('''        force_split[i + 1] = 1;
        force_split[0] = 1;
      }
      avg_32x32 += var_32x32;
''', '''        govpx_vp9_count_varpart_force_split(BLOCK_32X32);
        force_split[i + 1] = 1;
        force_split[0] = 1;
      }
      avg_32x32 += var_32x32;
'''),
    ('''    if (!is_key_frame && noise_level >= kMedium &&
        vt.part_variances.none.variance > (9 * avg_32x32) >> 5)
      force_split[0] = 1;
''', '''    if (!is_key_frame && noise_level >= kMedium &&
        vt.part_variances.none.variance > (9 * avg_32x32) >> 5) {
      govpx_vp9_count_varpart_force_split(BLOCK_64X64);
      force_split[0] = 1;
    }
'''),
    ('''    else if (!is_key_frame && noise_level < kMedium &&
             (max_var_32x32 - min_var_32x32) > 3 * (thresholds[0] >> 3) &&
             max_var_32x32 > thresholds[0] >> 1)
      force_split[0] = 1;
''', '''    else if (!is_key_frame && noise_level < kMedium &&
             (max_var_32x32 - min_var_32x32) > 3 * (thresholds[0] >> 3) &&
             max_var_32x32 > thresholds[0] >> 1) {
      govpx_vp9_count_varpart_force_split(BLOCK_64X64);
      force_split[0] = 1;
    }
'''),
    ('''  chroma_check(cpi, x, bsize, y_sad, is_key_frame, scene_change_detected);
  if (vt2) vpx_free(vt2);
  return 0;
}
''', '''  chroma_check(cpi, x, bsize, y_sad, is_key_frame, scene_change_detected);
  if (vt2) vpx_free(vt2);
  govpx_vp9_count_varpart_choose(0);
  return 0;
}
'''),
    ('''    case PARTITION_NONE:
      pc_tree->none.pred_pixel_ready = 1;
      nonrd_pick_sb_modes(cpi, tile_data, x, mi_row, mi_col, dummy_cost,
''', '''    case PARTITION_NONE:
      pc_tree->none.pred_pixel_ready = 1;
      govpx_vp9_count_mode_block(subsize);
      nonrd_pick_sb_modes(cpi, tile_data, x, mi_row, mi_col, dummy_cost,
'''),
    ('''    case PARTITION_VERT:
      pc_tree->vertical[0].pred_pixel_ready = 1;
      nonrd_pick_sb_modes(cpi, tile_data, x, mi_row, mi_col, dummy_cost,
''', '''    case PARTITION_VERT:
      pc_tree->vertical[0].pred_pixel_ready = 1;
      govpx_vp9_count_mode_block(subsize);
      nonrd_pick_sb_modes(cpi, tile_data, x, mi_row, mi_col, dummy_cost,
'''),
    ('''      if (mi_col + hbs < cm->mi_cols && bsize > BLOCK_8X8) {
        pc_tree->vertical[1].pred_pixel_ready = 1;
        nonrd_pick_sb_modes(cpi, tile_data, x, mi_row, mi_col + hbs, dummy_cost,
''', '''      if (mi_col + hbs < cm->mi_cols && bsize > BLOCK_8X8) {
        pc_tree->vertical[1].pred_pixel_ready = 1;
        govpx_vp9_count_mode_block(subsize);
        nonrd_pick_sb_modes(cpi, tile_data, x, mi_row, mi_col + hbs, dummy_cost,
'''),
    ('''    case PARTITION_HORZ:
      pc_tree->horizontal[0].pred_pixel_ready = 1;
      nonrd_pick_sb_modes(cpi, tile_data, x, mi_row, mi_col, dummy_cost,
''', '''    case PARTITION_HORZ:
      pc_tree->horizontal[0].pred_pixel_ready = 1;
      govpx_vp9_count_mode_block(subsize);
      nonrd_pick_sb_modes(cpi, tile_data, x, mi_row, mi_col, dummy_cost,
'''),
    ('''      if (mi_row + hbs < cm->mi_rows && bsize > BLOCK_8X8) {
        pc_tree->horizontal[1].pred_pixel_ready = 1;
        nonrd_pick_sb_modes(cpi, tile_data, x, mi_row + hbs, mi_col, dummy_cost,
''', '''      if (mi_row + hbs < cm->mi_rows && bsize > BLOCK_8X8) {
        pc_tree->horizontal[1].pred_pixel_ready = 1;
        govpx_vp9_count_mode_block(subsize);
        nonrd_pick_sb_modes(cpi, tile_data, x, mi_row + hbs, mi_col, dummy_cost,
'''),
    ('''      if (bsize == BLOCK_8X8) {
        nonrd_pick_sb_modes(cpi, tile_data, x, mi_row, mi_col, dummy_cost,
''', '''      if (bsize == BLOCK_8X8) {
        govpx_vp9_count_mode_block(subsize);
        nonrd_pick_sb_modes(cpi, tile_data, x, mi_row, mi_col, dummy_cost,
'''),
]:
    replace_once(encframe, old, new)

sad_c = root / "vpx_dsp" / "sad.c"
replace_once(sad_c,
'''#include "vpx_ports/mem.h"
''',
'''#include "vpx_ports/mem.h"
#include "vp9/govpx_call_stats.h"
''')
for old, new in [
    ('''unsigned int vpx_sad##m##x##n##_c(const uint8_t *src_ptr, int src_stride,   \\
                                    const uint8_t *ref_ptr, int ref_stride) { \\
    return sad(src_ptr, src_stride, ref_ptr, ref_stride, m, n);               \\
  }                                                                           \\
''', '''unsigned int vpx_sad##m##x##n##_c(const uint8_t *src_ptr, int src_stride,   \\
                                    const uint8_t *ref_ptr, int ref_stride) { \\
    govpx_vp9_count_sad_candidates(1, 0);                                      \\
    return sad(src_ptr, src_stride, ref_ptr, ref_stride, m, n);               \\
  }                                                                           \\
'''),
    ('''DECLARE_ALIGNED(32, uint8_t, comp_pred[m * n]);                           \\
    vpx_comp_avg_pred_c(comp_pred, second_pred, m, n, ref_ptr, ref_stride);   \\
''', '''DECLARE_ALIGNED(32, uint8_t, comp_pred[m * n]);                           \\
    govpx_vp9_count_sad_candidates(1, 0);                                      \\
    vpx_comp_avg_pred_c(comp_pred, second_pred, m, n, ref_ptr, ref_stride);   \\
'''),
    ('''int ref_stride) {                                                       \\
    return 2 * sad(src_ptr, 2 * src_stride, ref_ptr, 2 * ref_stride, (m),     \\
''', '''int ref_stride) {                                                       \\
    govpx_vp9_count_sad_candidates(1, 0);                                      \\
    return 2 * sad(src_ptr, 2 * src_stride, ref_ptr, 2 * ref_stride, (m),     \\
'''),
    ('''int ref_stride, uint32_t sad_array[4]) {        \\
    int i;                                                                     \\
    for (i = 0; i < 4; ++i)                                                    \\
      sad_array[i] =                                                           \\
          vpx_sad##m##x##n##_c(src_ptr, src_stride, ref_array[i], ref_stride); \\
  }                                                                            \\
''', '''int ref_stride, uint32_t sad_array[4]) {        \\
    int i;                                                                     \\
    govpx_vp9_count_sad_candidates(4, 1);                                      \\
    for (i = 0; i < 4; ++i)                                                    \\
      sad_array[i] = sad(src_ptr, src_stride, ref_array[i], ref_stride, m, n); \\
  }                                                                            \\
'''),
    ('''int ref_stride, uint32_t sad_array[4]) {  \\
    int i;                                                                     \\
    for (i = 0; i < 4; ++i) {                                                  \\
''', '''int ref_stride, uint32_t sad_array[4]) {  \\
    int i;                                                                     \\
    govpx_vp9_count_sad_candidates(4, 1);                                      \\
    for (i = 0; i < 4; ++i) {                                                  \\
'''),
]:
    replace_once(sad_c, old, new)

for rel in [
    "vpx_dsp/arm/sad_neon.c",
    "vpx_dsp/arm/sad_neon_dotprod.c",
    "vpx_dsp/arm/sad4d_neon.c",
    "vpx_dsp/arm/sad4d_neon_dotprod.c",
]:
    path = root / rel
    if not path.exists():
        continue
    replace_once(path,
'''#include "vpx_dsp/arm/sum_neon.h"
''',
'''#include "vpx_dsp/arm/sum_neon.h"
#include "vp9/govpx_call_stats.h"
''')

for rel in ["vpx_dsp/arm/sad_neon.c", "vpx_dsp/arm/sad_neon_dotprod.c"]:
    path = root / rel
    if not path.exists():
        continue
    text = path.read_text()
    text = text.replace(
'''int ref_stride) { \\
    return sad''',
'''int ref_stride) { \\
    govpx_vp9_count_sad_candidates(1, 0);                                    \\
    return sad''')
    text = text.replace(
'''int ref_stride) {                                                        \\
    return 2 *''',
'''int ref_stride) {                                                        \\
    govpx_vp9_count_sad_candidates(1, 0);                                      \\
    return 2 *''')
    text = text.replace(
'''const uint8_t *second_pred) {       \\
    return sad''',
'''const uint8_t *second_pred) {       \\
    govpx_vp9_count_sad_candidates(1, 0);                                  \\
    return sad''')
    text = text.replace(
'''const uint8_t *second_pred) {                                           \\
    return sad''',
'''const uint8_t *second_pred) {                                           \\
    govpx_vp9_count_sad_candidates(1, 0);                                     \\
    return sad''')
    path.write_text(text)

for rel in ["vpx_dsp/arm/sad4d_neon.c", "vpx_dsp/arm/sad4d_neon_dotprod.c"]:
    path = root / rel
    if not path.exists():
        continue
    text = path.read_text()
    text = text.replace(
'''int ref_stride, uint32_t sad_array[4]) {    \\
    sad''',
'''int ref_stride, uint32_t sad_array[4]) {    \\
    govpx_vp9_count_sad_candidates(4, 1);                                     \\
    sad''')
    text = text.replace(
'''uint32_t sad_array[4]) {                                             \\
    sad''',
'''uint32_t sad_array[4]) {                                             \\
    govpx_vp9_count_sad_candidates(4, 1);                                  \\
    sad''')
    text = text.replace(
'''uint32_t sad_array[4]) {                                         \\
    sad''',
'''uint32_t sad_array[4]) {                                         \\
    govpx_vp9_count_sad_candidates(4, 1);                              \\
    sad''')
    path.write_text(text)

sentinel = "LIBVPX_VP9_CALL_STATS"
if sentinel not in pickmode.read_text():
    raise SystemExit("failed to patch VP9 call stats")
PY
}

current_config=
if [ -f "$config_stamp" ]; then
	current_config=$(cat "$config_stamp")
fi

if [ ! -x "$src_dir/vpxdec" ] || [ ! -x "$src_dir/vpxenc" ] || [ "$current_config" != "$want_config" ]; then
	if [ "$current_config" != "$want_config" ]; then
		fetch_source
	fi
	patch_vpxdec_vp9_controls
	patch_vp9_call_stats
	(
		cd "$src_dir"
		./configure \
			--disable-docs \
			--disable-unit-tests \
			--disable-debug \
			--disable-gprof \
			--enable-optimizations \
			--disable-vp8 \
			--enable-vp9 \
			--enable-vp9_decoder \
			--enable-vp9_encoder \
			--disable-vp9-highbitdepth \
			--enable-postproc \
			--enable-vp9-postproc
		make -j"$jobs"
	)
	printf '%s\n' "$want_config" > "$config_stamp"
fi

cp "$src_dir/vpxdec" "$vpxdec_vp9_bin"
chmod +x "$vpxdec_vp9_bin"
cp "$src_dir/vpxenc" "$vpxenc_vp9_bin"
chmod +x "$vpxenc_vp9_bin"
cp "$src_dir/examples/vp9_spatial_svc_encoder" "$vp9_spatial_svc_bin"
chmod +x "$vp9_spatial_svc_bin"
printf '%s\n' "$vpxdec_vp9_bin"
printf '%s\n' "$vpxenc_vp9_bin"
printf '%s\n' "$vp9_spatial_svc_bin"
