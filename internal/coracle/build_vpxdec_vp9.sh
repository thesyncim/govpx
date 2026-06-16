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
want_config="v1.16.0-vp9-encoder+decoder-tools-optimized-govpx-decoder-controls-vp9-postproc
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

current_config=
if [ -f "$config_stamp" ]; then
	current_config=$(cat "$config_stamp")
fi

if [ ! -x "$src_dir/vpxdec" ] || [ ! -x "$src_dir/vpxenc" ] || [ "$current_config" != "$want_config" ]; then
	if [ "$current_config" != "$want_config" ]; then
		fetch_source
	fi
	patch_vpxdec_vp9_controls
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
