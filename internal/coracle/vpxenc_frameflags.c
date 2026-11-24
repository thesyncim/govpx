// This file is built directly by build_vpxenc_frameflags.sh into the
// vpxenc-frameflags helper binary; it is not part of any Go cgo
// package. The build constraint below tells `go build` to skip the
// file when scanning the surrounding Go package directory.
//go:build ignore

/*
 * vpxenc-frameflags — minimal companion VP8 encoder driver that
 * encodes a raw I420 fixture through libvpx with a per-frame
 * frame_flags script. The base vpxenc binary does not expose
 * VPX_EFLAG_FORCE_KF / VP8_EFLAG_NO_REF_* / VP8_EFLAG_NO_UPD_* /
 * VPX_EFLAG_FORCE_GF / VPX_EFLAG_FORCE_ARF on a per-frame basis,
 * so the strict byte-parity matrix in oracle_encoder_stream_parity*
 * tests cannot drive those per-frame flags through the standard
 * libvpx path. This binary is the one-stop driver the Go-side test
 * harness invokes to obtain a libvpx reference output for the same
 * per-frame flag schedule that govpx's EncodeInto receives.
 *
 * Argument layout (positional, all required unless marked optional):
 *
 *   --infile=PATH        raw I420 input (planes packed contiguously,
 *                        Y then U then V, sized to width*height +
 *                        2*((width+1)/2)*((height+1)/2) bytes per
 *                        frame).
 *   --outfile=PATH       IVF output path.
 *   --width=N --height=N visible frame dimensions.
 *   --fps-num=N          frame-rate numerator.
 *   --fps-den=N          frame-rate denominator.
 *   --frames=N           number of frames to consume from infile.
 *   --target-bitrate=N   total target bitrate in kbps.
 *   --min-q=N --max-q=N  quantizer bounds.
 *   --kf-min-dist=N      keyframe min distance (frames).
 *   --kf-max-dist=N      keyframe max distance (frames).
 *   --kf-disabled        sets cfg.kf_mode = VPX_KF_DISABLED.
 *   --deadline=MODE      good | best | rt.
 *   --cpu-used=N         VP8E_SET_CPUUSED value.
 *   --tune=MODE          psnr | ssim.
 *   --error-resilient=N  cfg.g_error_resilient bitmask.
 *   --token-parts=N      VP8E_SET_TOKEN_PARTITIONS value (0..3).
 *   --static-thresh=N    VP8E_SET_STATIC_THRESHOLD value.
 *   --noise-sensitivity=N VP8E_SET_NOISE_SENSITIVITY value.
 *   --sharpness=N        VP8E_SET_SHARPNESS value.
 *   --max-intra-rate=N   VP8E_SET_MAX_INTRA_BITRATE_PCT value.
 *   --gf-cbr-boost=N     VP8E_SET_GF_CBR_BOOST_PCT value.
 *   --screen-content-mode=N VP8E_SET_SCREEN_CONTENT_MODE value.
 *   --threads=N          cfg.g_threads value.
 *   --end-usage=MODE     vbr | cbr | cq | q.
 *   --cq-level=N         VP8E_SET_CQ_LEVEL value.
 *   --undershoot-pct=N   cfg.rc_undershoot_pct.
 *   --overshoot-pct=N    cfg.rc_overshoot_pct.
 *   --buf-sz=N --buf-initial-sz=N --buf-optimal-sz=N
 *                        cfg.rc_buf_* values (ms).
 *   --drop-frame=N       cfg.rc_dropframe_thresh value.
 *   --lag-in-frames=N    cfg.g_lag_in_frames value.
 *   --auto-alt-ref=N     VP8E_SET_ENABLEAUTOALTREF flag (0|1).
 *   --arnr-maxframes=N --arnr-strength=N --arnr-type=N
 *                        ARNR controls.
 *   --rtc-external-rate-control=N
 *                        VP8E_SET_RTC_EXTERNAL_RATECTRL flag (0|1).
 *   --active-map=PATTERN
 *                        VP8E_SET_ACTIVEMAP pattern: all | checker |
 *                        left-off | right-off | border-off | off.
 *   --roi-map=PATTERN    VP8E_SET_ROI_MAP pattern: checker | left1 |
 *                        quadrants | border1 | off.
 *   --roi-dq=CSV4 --roi-dlf=CSV4 --roi-static=CSV4
 *                        ROI segment deltas/static thresholds.
 *   --temporal-layers=N  cfg.ts_number_layers.
 *   --temporal-bitrates=CSV
 *                        cumulative cfg.ts_target_bitrate entries.
 *   --temporal-decimators=CSV
 *                        cfg.ts_rate_decimator entries.
 *   --temporal-periodicity=N
 *                        cfg.ts_periodicity.
 *   --temporal-layer-ids=CSV
 *                        cfg.ts_layer_id entries.
 *   --frame-flags=CSV    comma-separated 32-bit unsigned values, one
 *                        per encode call. The value is passed
 *                        verbatim to vpx_codec_encode as the
 *                        flag bitmask, so the caller must use the
 *                        libvpx-defined bits:
 *                          1<<0  VPX_EFLAG_FORCE_KF
 *                          1<<16 VP8_EFLAG_NO_REF_LAST
 *                          1<<17 VP8_EFLAG_NO_REF_GF
 *                          1<<18 VP8_EFLAG_NO_UPD_LAST
 *                          1<<19 VP8_EFLAG_FORCE_GF
 *                          1<<20 VP8_EFLAG_NO_UPD_ENTROPY
 *                          1<<21 VP8_EFLAG_NO_REF_ARF
 *                          1<<22 VP8_EFLAG_NO_UPD_GF
 *                          1<<23 VP8_EFLAG_NO_UPD_ARF
 *                          1<<24 VP8_EFLAG_FORCE_ARF
 *                        Missing entries default to 0 (no per-frame
 *                        flag set).
 *   --invisible-frames=CSV
 *                        comma-separated 0/1 values, one per input PTS.
 *                        A non-zero entry clears the VP8 show_frame bit
 *                        in that output packet so govpx's hidden-frame
 *                        marker can be compared against libvpx output.
 *   --control-script=CSV optional per-frame runtime-control script. Each
 *                        CSV entry applies before the matching input frame.
 *                        Use "-" or an empty entry for no change. Multiple
 *                        controls within one frame are joined by '+':
 *                          bitrate:N fps:N minq:N maxq:N drop:N
 *                          bufsz:N bufinit:N bufopt:N undershoot:N
 *                          overshoot:N endusage:{vbr,cbr,cq,q}
 *                          deadline:{good,best,rt} cpu:N tune:{psnr,ssim}
 *                          token:N static:N noise:N sharpness:N
 *                          screen:N maxintra:N gfboost:N cq:N
 *                          autoaltref:N arnrmax:N arnrstrength:N arnrtype:N
 *                          rtc:N active:{pattern|off} roi:{pattern|off}
 *                          roicustom:PATTERN:DQ/DQ/DQ/DQ:DLF/DLF/DLF/DLF:STATIC/STATIC/STATIC/STATIC
 *                          resize:WxH
 *                          setref:{last,golden,altref}:panning:N
 *                          copyref:{last,golden,altref}
 *                          tlid:N tslayers:N tsperiodicity:N
 *                          tsbitrates:A/B[/...] tsdecimators:A/B[/...]
 *                          tsids:A/B[/...]
 *   --copy-ref-log=PATH  optional log path for copyref checksums.
 *   --quantizer-log=PATH optional per-encode-call VP8E_GET_LAST_QUANTIZER log.
 *
 * On success the binary writes the IVF container to --outfile and
 * exits with status 0. Any libvpx or option-parsing error is fatal
 * and exits non-zero with a diagnostic on stderr.
 */

#include <errno.h>
#include <limits.h>
#include <stdarg.h>
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "vpx/vp8cx.h"
#include "vpx/vpx_codec.h"
#include "vpx/vpx_encoder.h"
#include "vpx/vpx_image.h"

#define VP8_FOURCC 0x30385056

static void mem_put_le16(void *vmem, int val) {
  unsigned char *mem = (unsigned char *)vmem;
  mem[0] = (unsigned char)((val >> 0) & 0xff);
  mem[1] = (unsigned char)((val >> 8) & 0xff);
}

static void mem_put_le32(void *vmem, int val) {
  unsigned char *mem = (unsigned char *)vmem;
  mem[0] = (unsigned char)((val >> 0) & 0xff);
  mem[1] = (unsigned char)((val >> 8) & 0xff);
  mem[2] = (unsigned char)((val >> 16) & 0xff);
  mem[3] = (unsigned char)((val >> 24) & 0xff);
}

/* Minimal IVF header writer — kept identical to libvpx's
 * ivf_write_file_header layout so the patched vpxenc-oracle binary
 * and this driver produce byte-equivalent containers when fed the
 * same per-frame VP8 payloads. */
static void write_ivf_file_header(FILE *outfile, int width, int height,
                                  int timebase_num, int timebase_den,
                                  int frame_count) {
  char header[32];
  header[0] = 'D';
  header[1] = 'K';
  header[2] = 'I';
  header[3] = 'F';
  mem_put_le16(header + 4, 0);                /* version */
  mem_put_le16(header + 6, 32);               /* headersize */
  mem_put_le32(header + 8, VP8_FOURCC);
  mem_put_le16(header + 12, width);
  mem_put_le16(header + 14, height);
  mem_put_le32(header + 16, timebase_den);
  mem_put_le32(header + 20, timebase_num);
  mem_put_le32(header + 24, frame_count);
  mem_put_le32(header + 28, 0);
  if (fwrite(header, 1, 32, outfile) != 32) {
    fprintf(stderr, "write ivf header\n");
    exit(EXIT_FAILURE);
  }
}

static void write_ivf_frame_header(FILE *outfile, int64_t pts,
                                   size_t frame_size) {
  char header[12];
  mem_put_le32(header, (int)frame_size);
  mem_put_le32(header + 4, (int)(pts & 0xFFFFFFFF));
  mem_put_le32(header + 8, (int)(pts >> 32));
  if (fwrite(header, 1, 12, outfile) != 12) {
    fprintf(stderr, "write ivf frame header\n");
    exit(EXIT_FAILURE);
  }
}

static void die_msg(const char *fmt, ...) {
  va_list args;
  va_start(args, fmt);
  vfprintf(stderr, fmt, args);
  va_end(args);
  fputc('\n', stderr);
  exit(EXIT_FAILURE);
}

static void die_codec_msg(vpx_codec_ctx_t *ctx, const char *what) {
  const char *detail = vpx_codec_error_detail(ctx);
  die_msg("%s: %s%s%s", what, vpx_codec_error(ctx), detail ? ": " : "",
          detail ? detail : "");
}

static int parse_int(const char *arg, const char *flag) {
  char *end = NULL;
  long v = strtol(arg, &end, 10);
  if (end == arg || (end && *end != '\0')) {
    die_msg("invalid integer for %s: %s", flag, arg);
  }
  if (v < INT_MIN || v > INT_MAX) {
    die_msg("integer for %s out of range: %s", flag, arg);
  }
  return (int)v;
}

static int starts_with(const char *s, const char *prefix) {
  size_t n = strlen(prefix);
  return strncmp(s, prefix, n) == 0;
}

static const char *flag_value(const char *arg, const char *flag) {
  size_t n = strlen(flag);
  if (strncmp(arg, flag, n) != 0) return NULL;
  if (arg[n] != '=') return NULL;
  return arg + n + 1;
}

static int parse_deadline(const char *value) {
  if (strcmp(value, "good") == 0) return (int)VPX_DL_GOOD_QUALITY;
  if (strcmp(value, "best") == 0) return (int)VPX_DL_BEST_QUALITY;
  if (strcmp(value, "rt") == 0) return (int)VPX_DL_REALTIME;
  die_msg("invalid --deadline: %s", value);
  return 0;
}

static enum vpx_rc_mode parse_end_usage(const char *value) {
  if (strcmp(value, "vbr") == 0) return VPX_VBR;
  if (strcmp(value, "cbr") == 0) return VPX_CBR;
  if (strcmp(value, "cq") == 0) return VPX_CQ;
  if (strcmp(value, "q") == 0) return VPX_Q;
  die_msg("invalid --end-usage: %s", value);
  return VPX_CBR;
}

static int parse_tune(const char *value) {
  if (strcmp(value, "psnr") == 0) return VP8_TUNE_PSNR;
  if (strcmp(value, "ssim") == 0) return VP8_TUNE_SSIM;
  die_msg("invalid --tune: %s", value);
  return VP8_TUNE_PSNR;
}

static char **parse_csv_strings(const char *csv, int *out_count,
                                const char *flag_name);
static void free_csv_strings(char **tokens, int count);

static int mb_rows_for_height(int height) { return (height + 15) >> 4; }

static int mb_cols_for_width(int width) { return (width + 15) >> 4; }

static void parse_int4_csv(const char *csv, int out[4], const char *flag_name) {
  char **tokens = NULL;
  int count = 0;
  tokens = parse_csv_strings(csv, &count, flag_name);
  if (count != 4) die_msg("%s expects exactly 4 comma-separated integers", flag_name);
  for (int i = 0; i < 4; ++i) out[i] = parse_int(tokens[i], flag_name);
  free_csv_strings(tokens, count);
}

static void parse_uint4_csv(const char *csv, unsigned int out[4],
                            const char *flag_name) {
  int tmp[4] = {0, 0, 0, 0};
  parse_int4_csv(csv, tmp, flag_name);
  for (int i = 0; i < 4; ++i) {
    if (tmp[i] < 0) die_msg("%s value must be non-negative", flag_name);
    out[i] = (unsigned int)tmp[i];
  }
}

static void parse_int_csv_exact(const char *csv, int *out, int expected,
                                const char *flag_name) {
  char **tokens = NULL;
  int count = 0;
  tokens = parse_csv_strings(csv, &count, flag_name);
  if (count != expected) {
    die_msg("%s expects exactly %d comma-separated integers", flag_name,
            expected);
  }
  for (int i = 0; i < expected; ++i) out[i] = parse_int(tokens[i], flag_name);
  free_csv_strings(tokens, count);
}

static int parse_slash_ints(const char *spec, int *out, int max_count,
                            const char *flag_name) {
  char buf[256];
  size_t len = strlen(spec);
  if (len >= sizeof(buf)) die_msg("%s token too long: %s", flag_name, spec);
  memcpy(buf, spec, len + 1);

  int count = 0;
  char *start = buf;
  while (1) {
    if (count >= max_count) die_msg("%s has too many entries", flag_name);
    char *end = strchr(start, '/');
    if (end) *end = '\0';
    if (!*start) die_msg("%s contains an empty entry", flag_name);
    out[count++] = parse_int(start, flag_name);
    if (!end) break;
    start = end + 1;
  }
  return count;
}

static unsigned char *alloc_active_map_pattern(const char *pattern, int rows,
                                               int cols) {
  size_t count = (size_t)rows * (size_t)cols;
  unsigned char *map = (unsigned char *)malloc(count);
  if (!map) die_msg("malloc active map");
  for (int r = 0; r < rows; ++r) {
    for (int c = 0; c < cols; ++c) {
      unsigned char v;
      if (strcmp(pattern, "all") == 0) {
        v = 1;
      } else if (strcmp(pattern, "checker") == 0) {
        v = ((r + c) & 1) ? 0 : 1;
      } else if (strcmp(pattern, "left-off") == 0) {
        v = c == 0 ? 0 : 1;
      } else if (strcmp(pattern, "right-off") == 0) {
        v = c == cols - 1 ? 0 : 1;
      } else if (strcmp(pattern, "border-off") == 0) {
        v = (r == 0 || c == 0 || r == rows - 1 || c == cols - 1) ? 0 : 1;
      } else {
        die_msg("invalid active-map pattern: %s", pattern);
        v = 1;
      }
      map[(size_t)r * (size_t)cols + (size_t)c] = v;
    }
  }
  return map;
}

static unsigned char *alloc_roi_map_pattern(const char *pattern, int rows,
                                            int cols) {
  size_t count = (size_t)rows * (size_t)cols;
  unsigned char *map = (unsigned char *)malloc(count);
  if (!map) die_msg("malloc roi map");
  for (int r = 0; r < rows; ++r) {
    for (int c = 0; c < cols; ++c) {
      unsigned char v;
      if (strcmp(pattern, "checker") == 0) {
        v = (unsigned char)((r + c) & 1);
      } else if (strcmp(pattern, "left1") == 0) {
        v = c < (cols + 1) / 2 ? 1 : 0;
      } else if (strcmp(pattern, "quadrants") == 0) {
        v = (unsigned char)((r >= rows / 2 ? 2 : 0) + (c >= cols / 2 ? 1 : 0));
      } else if (strcmp(pattern, "border1") == 0) {
        v = (r == 0 || c == 0 || r == rows - 1 || c == cols - 1) ? 1 : 0;
      } else {
        die_msg("invalid roi-map pattern: %s", pattern);
        v = 0;
      }
      map[(size_t)r * (size_t)cols + (size_t)c] = v;
    }
  }
  return map;
}

static void default_roi_params(const char *pattern, int delta_q[4],
                               int delta_lf[4],
                               unsigned int static_threshold[4]) {
  for (int i = 0; i < 4; ++i) {
    delta_q[i] = 0;
    delta_lf[i] = 0;
    static_threshold[i] = 0;
  }
  if (strcmp(pattern, "checker") == 0 || strcmp(pattern, "left1") == 0) {
    delta_q[1] = -10;
    delta_lf[1] = -3;
  } else if (strcmp(pattern, "quadrants") == 0) {
    delta_q[1] = -8;
    delta_q[2] = 8;
    delta_lf[3] = 4;
    static_threshold[2] = 500;
  } else if (strcmp(pattern, "border1") == 0) {
    delta_q[1] = -6;
    static_threshold[1] = 900;
  } else if (strcmp(pattern, "off") == 0) {
    return;
  } else {
    die_msg("invalid roi-map pattern: %s", pattern);
  }
}

static void apply_active_map(vpx_codec_ctx_t *ctx, int rows, int cols,
                             const char *pattern) {
  vpx_active_map_t active;
  active.rows = (unsigned int)rows;
  active.cols = (unsigned int)cols;
  active.active_map = NULL;
  if (strcmp(pattern, "off") != 0) {
    active.active_map = alloc_active_map_pattern(pattern, rows, cols);
  }
  if (vpx_codec_control(ctx, VP8E_SET_ACTIVEMAP, &active))
    die_codec_msg(ctx, "VP8E_SET_ACTIVEMAP");
  free(active.active_map);
}

static void apply_roi_map(vpx_codec_ctx_t *ctx, int rows, int cols,
                          const char *pattern, const int delta_q[4],
                          const int delta_lf[4],
                          const unsigned int static_threshold[4]) {
  vpx_roi_map_t roi;
  memset(&roi, 0, sizeof(roi));
  roi.enabled = strcmp(pattern, "off") != 0;
  roi.rows = (unsigned int)rows;
  roi.cols = (unsigned int)cols;
  if (roi.enabled) roi.roi_map = alloc_roi_map_pattern(pattern, rows, cols);
  for (int i = 0; i < 4; ++i) {
    roi.delta_q[i] = delta_q[i];
    roi.delta_lf[i] = delta_lf[i];
    roi.static_threshold[i] = static_threshold[i];
  }
  if (vpx_codec_control(ctx, VP8E_SET_ROI_MAP, &roi))
    die_codec_msg(ctx, "VP8E_SET_ROI_MAP");
  free(roi.roi_map);
}

static void parse_slash_ints_exact(const char *spec, int out[4],
                                   const char *flag_name) {
  int count = parse_slash_ints(spec, out, 4, flag_name);
  if (count != 4) die_msg("%s expects exactly 4 slash-separated integers", flag_name);
}

static void apply_roi_custom_token(vpx_codec_ctx_t *ctx, int rows, int cols,
                                   const char *spec) {
  char buf[512];
  size_t len = strlen(spec);
  if (len >= sizeof(buf)) die_msg("roicustom token too long: %s", spec);
  memcpy(buf, spec, len + 1);

  char *pattern = buf;
  char *dq_text = strchr(pattern, ':');
  if (!dq_text) die_msg("roicustom expects pattern:dq:dlf:static: %s", spec);
  *dq_text++ = '\0';
  char *dlf_text = strchr(dq_text, ':');
  if (!dlf_text) die_msg("roicustom expects pattern:dq:dlf:static: %s", spec);
  *dlf_text++ = '\0';
  char *static_text = strchr(dlf_text, ':');
  if (!static_text) die_msg("roicustom expects pattern:dq:dlf:static: %s", spec);
  *static_text++ = '\0';
  if (strchr(static_text, ':'))
    die_msg("roicustom has too many fields: %s", spec);

  int delta_q[4] = {0, 0, 0, 0};
  int delta_lf[4] = {0, 0, 0, 0};
  int static_tmp[4] = {0, 0, 0, 0};
  unsigned int static_threshold[4] = {0, 0, 0, 0};
  parse_slash_ints_exact(dq_text, delta_q, "roicustom dq");
  parse_slash_ints_exact(dlf_text, delta_lf, "roicustom dlf");
  parse_slash_ints_exact(static_text, static_tmp, "roicustom static");
  for (int i = 0; i < 4; ++i) {
    if (static_tmp[i] < 0) die_msg("roicustom static value must be non-negative");
    static_threshold[i] = (unsigned int)static_tmp[i];
  }
  apply_roi_map(ctx, rows, cols, pattern, delta_q, delta_lf,
                static_threshold);
}

static void fill_panning_image(vpx_image_t *img, int width, int height,
                               int index) {
  int xoff = index * 2;
  int yoff = index;
  for (int y = 0; y < height; ++y) {
    for (int x = 0; x < width; ++x) {
      int src_x = x + xoff;
      int src_y = y + yoff;
      img->planes[VPX_PLANE_Y][(ptrdiff_t)y * img->stride[VPX_PLANE_Y] + x] =
          (uint8_t)(32 + ((src_y * 7 + src_x * 11 +
                           (src_x / 8) * (src_y / 8) * 13) &
                          191));
    }
  }
  int uv_w = (width + 1) >> 1;
  int uv_h = (height + 1) >> 1;
  for (int y = 0; y < uv_h; ++y) {
    for (int x = 0; x < uv_w; ++x) {
      int src_x = x + xoff / 2;
      int src_y = y + yoff / 2;
      img->planes[VPX_PLANE_U][(ptrdiff_t)y * img->stride[VPX_PLANE_U] + x] =
          (uint8_t)(96 + ((src_x * 5 + src_y * 3) & 63));
      img->planes[VPX_PLANE_V][(ptrdiff_t)y * img->stride[VPX_PLANE_V] + x] =
          (uint8_t)(144 + ((src_x * 2 + src_y * 7) & 63));
    }
  }
}

static vpx_ref_frame_type_t parse_ref_frame_type(const char *value) {
  if (strcmp(value, "last") == 0) return VP8_LAST_FRAME;
  if (strcmp(value, "golden") == 0) return VP8_GOLD_FRAME;
  if (strcmp(value, "altref") == 0) return VP8_ALTR_FRAME;
  die_msg("invalid reference frame: %s", value);
  return VP8_LAST_FRAME;
}

static int vp8_reference_storage_dimension(int v) {
  return (v + 15) & ~15;
}

static void pad_plane_visible_to_storage(uint8_t *plane, int stride, int width,
                                         int height, int storage_width,
                                         int storage_height) {
  if (!plane || width <= 0 || height <= 0) return;
  for (int y = 0; y < height; ++y) {
    uint8_t *row = plane + (ptrdiff_t)y * stride;
    uint8_t last = row[width - 1];
    for (int x = width; x < storage_width; ++x) row[x] = last;
  }
  uint8_t *last_row = plane + (ptrdiff_t)(height - 1) * stride;
  for (int y = height; y < storage_height; ++y) {
    memcpy(plane + (ptrdiff_t)y * stride, last_row, (size_t)storage_width);
  }
}

static void pad_reference_image_visible_to_storage(vpx_image_t *img, int width,
                                                   int height) {
  int storage_width = vp8_reference_storage_dimension(width);
  int storage_height = vp8_reference_storage_dimension(height);
  pad_plane_visible_to_storage(img->planes[VPX_PLANE_Y],
                               img->stride[VPX_PLANE_Y], width, height,
                               storage_width, storage_height);
  pad_plane_visible_to_storage(img->planes[VPX_PLANE_U],
                               img->stride[VPX_PLANE_U], (width + 1) >> 1,
                               (height + 1) >> 1, storage_width >> 1,
                               storage_height >> 1);
  pad_plane_visible_to_storage(img->planes[VPX_PLANE_V],
                               img->stride[VPX_PLANE_V], (width + 1) >> 1,
                               (height + 1) >> 1, storage_width >> 1,
                               storage_height >> 1);
}

#define GOVPX_ADLER_MOD 65521u
static unsigned int plane_adler32(const uint8_t *plane, int width, int height,
                                  int stride) {
  unsigned int a = 1;
  unsigned int b = 0;
  if (!plane || width <= 0 || height <= 0 || stride <= 0) return 0;
  for (int y = 0; y < height; ++y) {
    const uint8_t *row = plane + (ptrdiff_t)y * stride;
    for (int x = 0; x < width; ++x) {
      a = (a + row[x]) % GOVPX_ADLER_MOD;
      b = (b + a) % GOVPX_ADLER_MOD;
    }
  }
  return (b << 16) | a;
}

static void apply_copy_reference_token(vpx_codec_ctx_t *ctx, int width,
                                       int height, int frame_idx,
                                       const char *ref_name, FILE *log) {
  if (!log) die_msg("copyref token requires --copy-ref-log");
  vpx_image_t ref_img;
  int storage_width = vp8_reference_storage_dimension(width);
  int storage_height = vp8_reference_storage_dimension(height);
  if (!vpx_img_alloc(&ref_img, VPX_IMG_FMT_I420, (unsigned)storage_width,
                     (unsigned)storage_height, 1)) {
    die_msg("vpx_img_alloc copyref failed");
  }
  vpx_ref_frame_t ref;
  ref.frame_type = parse_ref_frame_type(ref_name);
  ref.img = ref_img;
  if (vpx_codec_control(ctx, VP8_COPY_REFERENCE, &ref))
    die_codec_msg(ctx, "VP8_COPY_REFERENCE");

  int uv_w = (width + 1) >> 1;
  int uv_h = (height + 1) >> 1;
  unsigned int y_adler =
      plane_adler32(ref_img.planes[VPX_PLANE_Y], width, height,
                    ref_img.stride[VPX_PLANE_Y]);
  unsigned int u_adler =
      plane_adler32(ref_img.planes[VPX_PLANE_U], uv_w, uv_h,
                    ref_img.stride[VPX_PLANE_U]);
  unsigned int v_adler =
      plane_adler32(ref_img.planes[VPX_PLANE_V], uv_w, uv_h,
                    ref_img.stride[VPX_PLANE_V]);
  if (fprintf(log,
              "frame=%d ref=%s y_adler32=%u u_adler32=%u v_adler32=%u\n",
              frame_idx, ref_name, y_adler, u_adler, v_adler) < 0) {
    die_msg("write copy-ref log failed");
  }
  vpx_img_free(&ref_img);
}

static void apply_set_reference_token(vpx_codec_ctx_t *ctx, int width,
                                      int height, const char *spec) {
  char buf[128];
  size_t len = strlen(spec);
  if (len >= sizeof(buf)) die_msg("setref token too long: %s", spec);
  memcpy(buf, spec, len + 1);

  char *ref_name = buf;
  char *kind = strchr(ref_name, ':');
  if (!kind) die_msg("setref expects ref:pattern:index: %s", spec);
  *kind++ = '\0';
  char *index_text = strchr(kind, ':');
  if (!index_text) die_msg("setref expects ref:pattern:index: %s", spec);
  *index_text++ = '\0';
  if (strcmp(kind, "panning") != 0) {
    die_msg("unsupported setref pattern: %s", kind);
  }
  int index = parse_int(index_text, "setref index");

  vpx_image_t ref_img;
  int storage_width = vp8_reference_storage_dimension(width);
  int storage_height = vp8_reference_storage_dimension(height);
  if (!vpx_img_alloc(&ref_img, VPX_IMG_FMT_I420, (unsigned)storage_width,
                     (unsigned)storage_height, 1)) {
    die_msg("vpx_img_alloc setref failed");
  }
  fill_panning_image(&ref_img, width, height, index);
  pad_reference_image_visible_to_storage(&ref_img, width, height);
  vpx_ref_frame_t ref;
  ref.frame_type = parse_ref_frame_type(ref_name);
  ref.img = ref_img;
  if (vpx_codec_control(ctx, VP8_SET_REFERENCE, &ref))
    die_codec_msg(ctx, "VP8_SET_REFERENCE");
  vpx_img_free(&ref_img);
}

static char **parse_csv_strings(const char *csv, int *out_count,
                                const char *flag_name) {
  if (!csv) {
    *out_count = 0;
    return NULL;
  }
  int count = 1;
  for (const char *p = csv; *p; ++p) {
    if (*p == ',') ++count;
  }
  char **out = calloc((size_t)count, sizeof(*out));
  if (!out) die_msg("calloc %s", flag_name);
  int idx = 0;
  const char *start = csv;
  while (1) {
    const char *end = strchr(start, ',');
    size_t len = end ? (size_t)(end - start) : strlen(start);
    out[idx] = malloc(len + 1);
    if (!out[idx]) die_msg("malloc %s token", flag_name);
    memcpy(out[idx], start, len);
    out[idx][len] = '\0';
    ++idx;
    if (!end) break;
    start = end + 1;
  }
  *out_count = idx;
  return out;
}

static void free_csv_strings(char **tokens, int count) {
  if (!tokens) return;
  for (int i = 0; i < count; ++i) free(tokens[i]);
  free(tokens);
}

static unsigned int *parse_uint_csv(const char *csv, int *out_count,
                                    const char *flag_name) {
  if (!csv) {
    *out_count = 0;
    return NULL;
  }
  /* Two-pass: count entries (commas + 1), then parse. */
  int count = 1;
  for (const char *p = csv; *p; ++p) {
    if (*p == ',') ++count;
  }
  unsigned int *out = calloc((size_t)count, sizeof(*out));
  if (!out) die_msg("calloc %s", flag_name);
  int idx = 0;
  const char *start = csv;
  while (1) {
    const char *end = strchr(start, ',');
    char buf[32];
    size_t len = end ? (size_t)(end - start) : strlen(start);
    if (len >= sizeof(buf)) die_msg("%s token too long", flag_name);
    memcpy(buf, start, len);
    buf[len] = '\0';
    char *parse_end = NULL;
    unsigned long v = strtoul(buf, &parse_end, 0);
    if (parse_end == buf || (parse_end && *parse_end != '\0')) {
      die_msg("invalid %s token: %s", flag_name, buf);
    }
    out[idx++] = (unsigned int)v;
    if (!end) break;
    start = end + 1;
  }
  *out_count = idx;
  return out;
}

static unsigned int *parse_frame_flags(const char *csv, int *out_count) {
  return parse_uint_csv(csv, out_count, "--frame-flags");
}

static int control_value_int(const char *token, const char *prefix) {
  return parse_int(token + strlen(prefix), prefix);
}

static void parse_resize_token(const char *spec, int *width, int *height) {
  char buf[64];
  size_t len = strlen(spec);
  if (len >= sizeof(buf)) die_msg("resize token too long: %s", spec);
  memcpy(buf, spec, len + 1);
  char *sep = strchr(buf, 'x');
  if (!sep) sep = strchr(buf, 'X');
  if (!sep) die_msg("resize expects WxH: %s", spec);
  *sep++ = '\0';
  int w = parse_int(buf, "resize width");
  int h = parse_int(sep, "resize height");
  if (w <= 0 || h <= 0) die_msg("resize dimensions must be positive: %s", spec);
  *width = w;
  *height = h;
}

static void apply_runtime_config_token(vpx_codec_enc_cfg_t *cfg, int *deadline,
                                       const char *token, int *need_config) {
  if (starts_with(token, "resize:")) {
    int width = 0;
    int height = 0;
    parse_resize_token(token + strlen("resize:"), &width, &height);
    cfg->g_w = (unsigned)width;
    cfg->g_h = (unsigned)height;
    *need_config = 1;
  } else if (starts_with(token, "bitrate:")) {
    cfg->rc_target_bitrate = (unsigned)control_value_int(token, "bitrate:");
    *need_config = 1;
  } else if (starts_with(token, "fps:")) {
    int fps = control_value_int(token, "fps:");
    if (fps <= 0) die_msg("fps control must be positive: %s", token);
    cfg->g_timebase.num = 1;
    cfg->g_timebase.den = fps;
    *need_config = 1;
  } else if (starts_with(token, "minq:")) {
    cfg->rc_min_quantizer = (unsigned)control_value_int(token, "minq:");
    *need_config = 1;
  } else if (starts_with(token, "maxq:")) {
    cfg->rc_max_quantizer = (unsigned)control_value_int(token, "maxq:");
    *need_config = 1;
  } else if (starts_with(token, "drop:")) {
    cfg->rc_dropframe_thresh = (unsigned)control_value_int(token, "drop:");
    *need_config = 1;
  } else if (starts_with(token, "bufsz:")) {
    cfg->rc_buf_sz = (unsigned)control_value_int(token, "bufsz:");
    *need_config = 1;
  } else if (starts_with(token, "bufinit:")) {
    cfg->rc_buf_initial_sz = (unsigned)control_value_int(token, "bufinit:");
    *need_config = 1;
  } else if (starts_with(token, "bufopt:")) {
    cfg->rc_buf_optimal_sz = (unsigned)control_value_int(token, "bufopt:");
    *need_config = 1;
  } else if (starts_with(token, "undershoot:")) {
    cfg->rc_undershoot_pct = (unsigned)control_value_int(token, "undershoot:");
    *need_config = 1;
  } else if (starts_with(token, "overshoot:")) {
    cfg->rc_overshoot_pct = (unsigned)control_value_int(token, "overshoot:");
    *need_config = 1;
  } else if (starts_with(token, "endusage:")) {
    cfg->rc_end_usage = parse_end_usage(token + strlen("endusage:"));
    *need_config = 1;
  } else if (starts_with(token, "threads:")) {
    cfg->g_threads = (unsigned)control_value_int(token, "threads:");
    *need_config = 1;
  } else if (starts_with(token, "error:")) {
    cfg->g_error_resilient = (unsigned)control_value_int(token, "error:");
    *need_config = 1;
  } else if (starts_with(token, "kfmin:")) {
    cfg->kf_min_dist = (unsigned)control_value_int(token, "kfmin:");
    *need_config = 1;
  } else if (starts_with(token, "kfmax:")) {
    cfg->kf_max_dist = (unsigned)control_value_int(token, "kfmax:");
    *need_config = 1;
  } else if (starts_with(token, "kfdisabled:")) {
    cfg->kf_mode = control_value_int(token, "kfdisabled:") ? VPX_KF_DISABLED : VPX_KF_AUTO;
    *need_config = 1;
  } else if (starts_with(token, "deadline:")) {
    *deadline = parse_deadline(token + strlen("deadline:"));
  } else if (starts_with(token, "tslayers:")) {
    int layers = control_value_int(token, "tslayers:");
    if (layers <= 0 || layers > VPX_TS_MAX_LAYERS)
      die_msg("tslayers out of range: %s", token);
    cfg->ts_number_layers = (unsigned int)layers;
    *need_config = 1;
  } else if (starts_with(token, "tsperiodicity:")) {
    int periodicity = control_value_int(token, "tsperiodicity:");
    if (periodicity <= 0 || periodicity > VPX_TS_MAX_PERIODICITY)
      die_msg("tsperiodicity out of range: %s", token);
    cfg->ts_periodicity = (unsigned int)periodicity;
    *need_config = 1;
  } else if (starts_with(token, "tsbitrates:")) {
    int values[VPX_TS_MAX_LAYERS] = {0};
    int count = parse_slash_ints(token + strlen("tsbitrates:"), values,
                                 VPX_TS_MAX_LAYERS, "tsbitrates");
    for (int i = 0; i < VPX_TS_MAX_LAYERS; ++i) cfg->ts_target_bitrate[i] = 0;
    for (int i = 0; i < count; ++i) {
      if (values[i] <= 0) die_msg("tsbitrates values must be positive");
      cfg->ts_target_bitrate[i] = (unsigned int)values[i];
    }
    *need_config = 1;
  } else if (starts_with(token, "tsdecimators:")) {
    int values[VPX_TS_MAX_LAYERS] = {0};
    int count = parse_slash_ints(token + strlen("tsdecimators:"), values,
                                 VPX_TS_MAX_LAYERS, "tsdecimators");
    for (int i = 0; i < VPX_TS_MAX_LAYERS; ++i) cfg->ts_rate_decimator[i] = 0;
    for (int i = 0; i < count; ++i) {
      if (values[i] <= 0) die_msg("tsdecimators values must be positive");
      cfg->ts_rate_decimator[i] = (unsigned int)values[i];
    }
    *need_config = 1;
  } else if (starts_with(token, "tsids:")) {
    int values[VPX_TS_MAX_PERIODICITY] = {0};
    int count = parse_slash_ints(token + strlen("tsids:"), values,
                                 VPX_TS_MAX_PERIODICITY, "tsids");
    for (int i = 0; i < VPX_TS_MAX_PERIODICITY; ++i) cfg->ts_layer_id[i] = 0;
    for (int i = 0; i < count; ++i) {
      if (values[i] < 0 || values[i] >= VPX_TS_MAX_LAYERS)
        die_msg("tsids entry out of range");
      cfg->ts_layer_id[i] = (unsigned int)values[i];
    }
    *need_config = 1;
  } else if (starts_with(token, "cpu:") || starts_with(token, "tune:") ||
             starts_with(token, "token:") || starts_with(token, "static:") ||
             starts_with(token, "noise:") || starts_with(token, "sharpness:") ||
             starts_with(token, "screen:") || starts_with(token, "maxintra:") ||
             starts_with(token, "gfboost:") || starts_with(token, "cq:") ||
             starts_with(token, "autoaltref:") || starts_with(token, "arnrmax:") ||
             starts_with(token, "arnrstrength:") || starts_with(token, "arnrtype:") ||
             starts_with(token, "rtc:") || starts_with(token, "active:") ||
             starts_with(token, "roi:") || starts_with(token, "roicustom:") ||
             starts_with(token, "setref:") ||
             starts_with(token, "copyref:") || starts_with(token, "tlid:")) {
    return;
  } else {
    die_msg("unknown control token: %s", token);
  }
}

struct runtime_codec_context {
  vpx_codec_ctx_t *ctx;
  int width;
  int height;
  int mb_rows;
  int mb_cols;
  int frame_idx;
  FILE *copy_ref_log;
};

static void apply_runtime_codec_token(struct runtime_codec_context *ctx,
                                      const char *token) {
  if (starts_with(token, "cpu:")) {
    if (vpx_codec_control(ctx->ctx, VP8E_SET_CPUUSED, control_value_int(token, "cpu:")))
      die_codec_msg(ctx->ctx, "runtime VP8E_SET_CPUUSED");
  } else if (starts_with(token, "tune:")) {
    if (vpx_codec_control(ctx->ctx, VP8E_SET_TUNING, parse_tune(token + strlen("tune:"))))
      die_codec_msg(ctx->ctx, "runtime VP8E_SET_TUNING");
  } else if (starts_with(token, "token:")) {
    if (vpx_codec_control(ctx->ctx, VP8E_SET_TOKEN_PARTITIONS, control_value_int(token, "token:")))
      die_codec_msg(ctx->ctx, "runtime VP8E_SET_TOKEN_PARTITIONS");
  } else if (starts_with(token, "static:")) {
    if (vpx_codec_control(ctx->ctx, VP8E_SET_STATIC_THRESHOLD, control_value_int(token, "static:")))
      die_codec_msg(ctx->ctx, "runtime VP8E_SET_STATIC_THRESHOLD");
  } else if (starts_with(token, "noise:")) {
    if (vpx_codec_control(ctx->ctx, VP8E_SET_NOISE_SENSITIVITY, control_value_int(token, "noise:")))
      die_codec_msg(ctx->ctx, "runtime VP8E_SET_NOISE_SENSITIVITY");
  } else if (starts_with(token, "sharpness:")) {
    if (vpx_codec_control(ctx->ctx, VP8E_SET_SHARPNESS, control_value_int(token, "sharpness:")))
      die_codec_msg(ctx->ctx, "runtime VP8E_SET_SHARPNESS");
  } else if (starts_with(token, "screen:")) {
    if (vpx_codec_control(ctx->ctx, VP8E_SET_SCREEN_CONTENT_MODE, control_value_int(token, "screen:")))
      die_codec_msg(ctx->ctx, "runtime VP8E_SET_SCREEN_CONTENT_MODE");
  } else if (starts_with(token, "maxintra:")) {
    if (vpx_codec_control(ctx->ctx, VP8E_SET_MAX_INTRA_BITRATE_PCT, control_value_int(token, "maxintra:")))
      die_codec_msg(ctx->ctx, "runtime VP8E_SET_MAX_INTRA_BITRATE_PCT");
  } else if (starts_with(token, "gfboost:")) {
    if (vpx_codec_control(ctx->ctx, VP8E_SET_GF_CBR_BOOST_PCT, control_value_int(token, "gfboost:")))
      die_codec_msg(ctx->ctx, "runtime VP8E_SET_GF_CBR_BOOST_PCT");
  } else if (starts_with(token, "cq:")) {
    if (vpx_codec_control(ctx->ctx, VP8E_SET_CQ_LEVEL, control_value_int(token, "cq:")))
      die_codec_msg(ctx->ctx, "runtime VP8E_SET_CQ_LEVEL");
  } else if (starts_with(token, "autoaltref:")) {
    if (vpx_codec_control(ctx->ctx, VP8E_SET_ENABLEAUTOALTREF, control_value_int(token, "autoaltref:")))
      die_codec_msg(ctx->ctx, "runtime VP8E_SET_ENABLEAUTOALTREF");
  } else if (starts_with(token, "arnrmax:")) {
    if (vpx_codec_control(ctx->ctx, VP8E_SET_ARNR_MAXFRAMES, control_value_int(token, "arnrmax:")))
      die_codec_msg(ctx->ctx, "runtime VP8E_SET_ARNR_MAXFRAMES");
  } else if (starts_with(token, "arnrstrength:")) {
    if (vpx_codec_control(ctx->ctx, VP8E_SET_ARNR_STRENGTH, control_value_int(token, "arnrstrength:")))
      die_codec_msg(ctx->ctx, "runtime VP8E_SET_ARNR_STRENGTH");
  } else if (starts_with(token, "arnrtype:")) {
    if (vpx_codec_control(ctx->ctx, VP8E_SET_ARNR_TYPE, control_value_int(token, "arnrtype:")))
      die_codec_msg(ctx->ctx, "runtime VP8E_SET_ARNR_TYPE");
  } else if (starts_with(token, "rtc:")) {
    if (vpx_codec_control(ctx->ctx, VP8E_SET_RTC_EXTERNAL_RATECTRL, control_value_int(token, "rtc:")))
      die_codec_msg(ctx->ctx, "runtime VP8E_SET_RTC_EXTERNAL_RATECTRL");
  } else if (starts_with(token, "active:")) {
    apply_active_map(ctx->ctx, ctx->mb_rows, ctx->mb_cols,
                     token + strlen("active:"));
  } else if (starts_with(token, "roi:")) {
    int delta_q[4];
    int delta_lf[4];
    unsigned int static_threshold[4];
    const char *pattern = token + strlen("roi:");
    default_roi_params(pattern, delta_q, delta_lf, static_threshold);
    apply_roi_map(ctx->ctx, ctx->mb_rows, ctx->mb_cols, pattern, delta_q,
                  delta_lf, static_threshold);
  } else if (starts_with(token, "roicustom:")) {
    apply_roi_custom_token(ctx->ctx, ctx->mb_rows, ctx->mb_cols,
                           token + strlen("roicustom:"));
  } else if (starts_with(token, "setref:")) {
    apply_set_reference_token(ctx->ctx, ctx->width, ctx->height,
                              token + strlen("setref:"));
  } else if (starts_with(token, "copyref:")) {
    apply_copy_reference_token(ctx->ctx, ctx->width, ctx->height,
                               ctx->frame_idx, token + strlen("copyref:"),
                               ctx->copy_ref_log);
  } else if (starts_with(token, "tlid:")) {
    int layer_id = parse_int(token + strlen("tlid:"), "tlid");
    if (vpx_codec_control(ctx->ctx, VP8E_SET_TEMPORAL_LAYER_ID, layer_id))
      die_codec_msg(ctx->ctx, "VP8E_SET_TEMPORAL_LAYER_ID");
  }
}

static void for_each_control_token(const char *entry,
                                   void (*fn)(void *opaque, const char *token),
                                   void *opaque) {
  if (!entry || !*entry || strcmp(entry, "-") == 0) return;
  char buf[1024];
  size_t len = strlen(entry);
  if (len >= sizeof(buf)) die_msg("control-script entry too long: %s", entry);
  memcpy(buf, entry, len + 1);
  char *start = buf;
  while (1) {
    char *end = strchr(start, '+');
    if (end) *end = '\0';
    if (*start) fn(opaque, start);
    if (!end) break;
    start = end + 1;
  }
}

struct config_token_context {
  vpx_codec_enc_cfg_t *cfg;
  int *deadline;
  int need_config;
};

static void config_token_callback(void *opaque, const char *token) {
  struct config_token_context *ctx = (struct config_token_context *)opaque;
  apply_runtime_config_token(ctx->cfg, ctx->deadline, token, &ctx->need_config);
}

static void codec_token_callback(void *opaque, const char *token) {
  apply_runtime_codec_token((struct runtime_codec_context *)opaque, token);
}

static void apply_runtime_controls(vpx_codec_ctx_t *ctx, vpx_codec_enc_cfg_t *cfg,
                                   int *deadline, const char *entry,
                                   int frame_idx, FILE *copy_ref_log) {
  struct config_token_context config_ctx = {cfg, deadline, 0};
  for_each_control_token(entry, config_token_callback, &config_ctx);
  if (config_ctx.need_config) {
    if (vpx_codec_enc_config_set(ctx, cfg)) {
      die_codec_msg(ctx, "runtime vpx_codec_enc_config_set");
    }
  }
  int mb_rows = mb_rows_for_height((int)cfg->g_h);
  int mb_cols = mb_cols_for_width((int)cfg->g_w);
  struct runtime_codec_context codec_ctx = {
      ctx, (int)cfg->g_w, (int)cfg->g_h, mb_rows, mb_cols, frame_idx,
      copy_ref_log};
  for_each_control_token(entry, codec_token_callback, &codec_ctx);
}

int main(int argc, char **argv) {
  const char *infile_path = NULL;
  const char *outfile_path = NULL;
  int width = 0, height = 0;
  int fps_num = 30, fps_den = 1;
  int frames = 0;
  int target_kbps = 700;
  int min_q = 4, max_q = 56;
  int kf_min_dist = 999, kf_max_dist = 999;
  int kf_disabled = 0;
  int deadline = (int)VPX_DL_GOOD_QUALITY;
  int cpu_used = 0;
  int tune = VP8_TUNE_PSNR;
  int error_resilient = 0;
  int token_parts = 0;
  int static_thresh = 0;
  int noise_sensitivity = 0;
  int sharpness = 0;
  int max_intra_rate = 0;
  int max_intra_rate_set = 0;
  int gf_cbr_boost = 0;
  int gf_cbr_boost_set = 0;
  int screen_content = 0;
  int threads = 1;
  enum vpx_rc_mode end_usage = VPX_CBR;
  int cq_level = 0;
  int cq_level_set = 0;
  int undershoot_pct = 0;
  int undershoot_set = 0;
  int overshoot_pct = 0;
  int overshoot_set = 0;
  int buf_sz = 0, buf_sz_set = 0;
  int buf_init = 0, buf_init_set = 0;
  int buf_opt = 0, buf_opt_set = 0;
  int drop_frame = 0, drop_frame_set = 0;
  int lag_in_frames = 0;
  int auto_alt_ref = 0;
  int arnr_max = 0, arnr_max_set = 0;
  int arnr_strength = 0, arnr_strength_set = 0;
  int arnr_type = 0, arnr_type_set = 0;
  int rtc_external = 0, rtc_external_set = 0;
  const char *active_map_pattern = NULL;
  const char *roi_map_pattern = NULL;
  const char *roi_dq_csv = NULL;
  const char *roi_dlf_csv = NULL;
  const char *roi_static_csv = NULL;
  int temporal_layers = 0;
  const char *temporal_bitrates_csv = NULL;
  const char *temporal_decimators_csv = NULL;
  int temporal_periodicity = 0;
  const char *temporal_layer_ids_csv = NULL;
  const char *frame_flags_csv = NULL;
  const char *invisible_frames_csv = NULL;
  const char *control_script_csv = NULL;
  const char *copy_ref_log_path = NULL;
  const char *quantizer_log_path = NULL;

  for (int i = 1; i < argc; ++i) {
    const char *a = argv[i];
    const char *v;
    if ((v = flag_value(a, "--infile"))) {
      infile_path = v;
    } else if ((v = flag_value(a, "--outfile"))) {
      outfile_path = v;
    } else if ((v = flag_value(a, "--width"))) {
      width = parse_int(v, "--width");
    } else if ((v = flag_value(a, "--height"))) {
      height = parse_int(v, "--height");
    } else if ((v = flag_value(a, "--fps-num"))) {
      fps_num = parse_int(v, "--fps-num");
    } else if ((v = flag_value(a, "--fps-den"))) {
      fps_den = parse_int(v, "--fps-den");
    } else if ((v = flag_value(a, "--frames"))) {
      frames = parse_int(v, "--frames");
    } else if ((v = flag_value(a, "--target-bitrate"))) {
      target_kbps = parse_int(v, "--target-bitrate");
    } else if ((v = flag_value(a, "--min-q"))) {
      min_q = parse_int(v, "--min-q");
    } else if ((v = flag_value(a, "--max-q"))) {
      max_q = parse_int(v, "--max-q");
    } else if ((v = flag_value(a, "--kf-min-dist"))) {
      kf_min_dist = parse_int(v, "--kf-min-dist");
    } else if ((v = flag_value(a, "--kf-max-dist"))) {
      kf_max_dist = parse_int(v, "--kf-max-dist");
    } else if (strcmp(a, "--kf-disabled") == 0) {
      kf_disabled = 1;
    } else if ((v = flag_value(a, "--deadline"))) {
      deadline = parse_deadline(v);
    } else if ((v = flag_value(a, "--cpu-used"))) {
      cpu_used = parse_int(v, "--cpu-used");
    } else if ((v = flag_value(a, "--tune"))) {
      tune = parse_tune(v);
    } else if ((v = flag_value(a, "--error-resilient"))) {
      error_resilient = parse_int(v, "--error-resilient");
    } else if ((v = flag_value(a, "--token-parts"))) {
      token_parts = parse_int(v, "--token-parts");
    } else if ((v = flag_value(a, "--static-thresh"))) {
      static_thresh = parse_int(v, "--static-thresh");
    } else if ((v = flag_value(a, "--noise-sensitivity"))) {
      noise_sensitivity = parse_int(v, "--noise-sensitivity");
    } else if ((v = flag_value(a, "--sharpness"))) {
      sharpness = parse_int(v, "--sharpness");
    } else if ((v = flag_value(a, "--max-intra-rate"))) {
      max_intra_rate = parse_int(v, "--max-intra-rate");
      max_intra_rate_set = 1;
    } else if ((v = flag_value(a, "--gf-cbr-boost"))) {
      gf_cbr_boost = parse_int(v, "--gf-cbr-boost");
      gf_cbr_boost_set = 1;
    } else if ((v = flag_value(a, "--screen-content-mode"))) {
      screen_content = parse_int(v, "--screen-content-mode");
    } else if ((v = flag_value(a, "--threads"))) {
      threads = parse_int(v, "--threads");
    } else if ((v = flag_value(a, "--end-usage"))) {
      end_usage = parse_end_usage(v);
    } else if ((v = flag_value(a, "--cq-level"))) {
      cq_level = parse_int(v, "--cq-level");
      cq_level_set = 1;
    } else if ((v = flag_value(a, "--undershoot-pct"))) {
      undershoot_pct = parse_int(v, "--undershoot-pct");
      undershoot_set = 1;
    } else if ((v = flag_value(a, "--overshoot-pct"))) {
      overshoot_pct = parse_int(v, "--overshoot-pct");
      overshoot_set = 1;
    } else if ((v = flag_value(a, "--buf-sz"))) {
      buf_sz = parse_int(v, "--buf-sz");
      buf_sz_set = 1;
    } else if ((v = flag_value(a, "--buf-initial-sz"))) {
      buf_init = parse_int(v, "--buf-initial-sz");
      buf_init_set = 1;
    } else if ((v = flag_value(a, "--buf-optimal-sz"))) {
      buf_opt = parse_int(v, "--buf-optimal-sz");
      buf_opt_set = 1;
    } else if ((v = flag_value(a, "--drop-frame"))) {
      drop_frame = parse_int(v, "--drop-frame");
      drop_frame_set = 1;
    } else if ((v = flag_value(a, "--lag-in-frames"))) {
      lag_in_frames = parse_int(v, "--lag-in-frames");
    } else if ((v = flag_value(a, "--auto-alt-ref"))) {
      auto_alt_ref = parse_int(v, "--auto-alt-ref");
    } else if ((v = flag_value(a, "--arnr-maxframes"))) {
      arnr_max = parse_int(v, "--arnr-maxframes");
      arnr_max_set = 1;
    } else if ((v = flag_value(a, "--arnr-strength"))) {
      arnr_strength = parse_int(v, "--arnr-strength");
      arnr_strength_set = 1;
    } else if ((v = flag_value(a, "--arnr-type"))) {
      arnr_type = parse_int(v, "--arnr-type");
      arnr_type_set = 1;
    } else if ((v = flag_value(a, "--rtc-external-rate-control"))) {
      rtc_external = parse_int(v, "--rtc-external-rate-control");
      rtc_external_set = 1;
    } else if ((v = flag_value(a, "--rtc-external"))) {
      rtc_external = parse_int(v, "--rtc-external");
      rtc_external_set = 1;
    } else if ((v = flag_value(a, "--active-map"))) {
      active_map_pattern = v;
    } else if ((v = flag_value(a, "--roi-map"))) {
      roi_map_pattern = v;
    } else if ((v = flag_value(a, "--roi-dq"))) {
      roi_dq_csv = v;
    } else if ((v = flag_value(a, "--roi-dlf"))) {
      roi_dlf_csv = v;
    } else if ((v = flag_value(a, "--roi-static"))) {
      roi_static_csv = v;
    } else if ((v = flag_value(a, "--temporal-layers"))) {
      temporal_layers = parse_int(v, "--temporal-layers");
    } else if ((v = flag_value(a, "--temporal-bitrates"))) {
      temporal_bitrates_csv = v;
    } else if ((v = flag_value(a, "--temporal-decimators"))) {
      temporal_decimators_csv = v;
    } else if ((v = flag_value(a, "--temporal-periodicity"))) {
      temporal_periodicity = parse_int(v, "--temporal-periodicity");
    } else if ((v = flag_value(a, "--temporal-layer-ids"))) {
      temporal_layer_ids_csv = v;
    } else if ((v = flag_value(a, "--frame-flags"))) {
      frame_flags_csv = v;
    } else if ((v = flag_value(a, "--invisible-frames"))) {
      invisible_frames_csv = v;
    } else if ((v = flag_value(a, "--control-script"))) {
      control_script_csv = v;
    } else if ((v = flag_value(a, "--copy-ref-log"))) {
      copy_ref_log_path = v;
    } else if ((v = flag_value(a, "--quantizer-log"))) {
      quantizer_log_path = v;
    } else {
      die_msg("unknown argument: %s", a);
    }
  }

  if (!infile_path || !outfile_path) die_msg("missing --infile/--outfile");
  if (width <= 0 || height <= 0) die_msg("invalid width/height");
  if (frames <= 0) die_msg("--frames must be > 0");
  int mb_rows = mb_rows_for_height(height);
  int mb_cols = mb_cols_for_width(width);

  int frame_flag_count = 0;
  unsigned int *per_frame_flags =
      parse_frame_flags(frame_flags_csv, &frame_flag_count);
  int invisible_frame_count = 0;
  unsigned int *invisible_frames =
      parse_uint_csv(invisible_frames_csv, &invisible_frame_count,
                     "--invisible-frames");
  int control_script_count = 0;
  char **control_script =
      parse_csv_strings(control_script_csv, &control_script_count, "control_script");

  vpx_codec_iface_t *iface = vpx_codec_vp8_cx();
  vpx_codec_enc_cfg_t cfg;
  vpx_codec_err_t res = vpx_codec_enc_config_default(iface, &cfg, 0);
  if (res) die_msg("vpx_codec_enc_config_default: %s", vpx_codec_err_to_string(res));
  cfg.g_w = (unsigned)width;
  cfg.g_h = (unsigned)height;
  cfg.g_timebase.num = fps_den;
  cfg.g_timebase.den = fps_num;
  cfg.g_threads = (unsigned)threads;
  cfg.g_error_resilient = (unsigned)error_resilient;
  cfg.g_lag_in_frames = (unsigned)lag_in_frames;
  cfg.rc_end_usage = end_usage;
  cfg.rc_target_bitrate = (unsigned)target_kbps;
  cfg.rc_min_quantizer = (unsigned)min_q;
  cfg.rc_max_quantizer = (unsigned)max_q;
  if (undershoot_set) cfg.rc_undershoot_pct = (unsigned)undershoot_pct;
  if (overshoot_set) cfg.rc_overshoot_pct = (unsigned)overshoot_pct;
  if (buf_sz_set) cfg.rc_buf_sz = (unsigned)buf_sz;
  if (buf_init_set) cfg.rc_buf_initial_sz = (unsigned)buf_init;
  if (buf_opt_set) cfg.rc_buf_optimal_sz = (unsigned)buf_opt;
  if (drop_frame_set) cfg.rc_dropframe_thresh = (unsigned)drop_frame;
  cfg.kf_min_dist = (unsigned)kf_min_dist;
  cfg.kf_max_dist = (unsigned)kf_max_dist;
  cfg.kf_mode = kf_disabled ? VPX_KF_DISABLED : VPX_KF_AUTO;
  if (temporal_layers > 0) {
    if (temporal_layers > VPX_TS_MAX_LAYERS)
      die_msg("--temporal-layers exceeds VPX_TS_MAX_LAYERS");
    if (temporal_periodicity <= 0 ||
        temporal_periodicity > VPX_TS_MAX_PERIODICITY)
      die_msg("--temporal-periodicity out of range");
    if (!temporal_bitrates_csv || !temporal_decimators_csv ||
        !temporal_layer_ids_csv) {
      die_msg("temporal config requires bitrates, decimators, and layer IDs");
    }
    int bitrates[VPX_TS_MAX_LAYERS] = {0};
    int decimators[VPX_TS_MAX_LAYERS] = {0};
    int layer_ids[VPX_TS_MAX_PERIODICITY] = {0};
    parse_int_csv_exact(temporal_bitrates_csv, bitrates, temporal_layers,
                        "--temporal-bitrates");
    parse_int_csv_exact(temporal_decimators_csv, decimators, temporal_layers,
                        "--temporal-decimators");
    parse_int_csv_exact(temporal_layer_ids_csv, layer_ids,
                        temporal_periodicity, "--temporal-layer-ids");
    cfg.ts_number_layers = (unsigned int)temporal_layers;
    cfg.ts_periodicity = (unsigned int)temporal_periodicity;
    for (int i = 0; i < temporal_layers; ++i) {
      if (bitrates[i] <= 0) die_msg("--temporal-bitrates must be positive");
      if (decimators[i] <= 0) die_msg("--temporal-decimators must be positive");
      cfg.ts_target_bitrate[i] = (unsigned int)bitrates[i];
      cfg.ts_rate_decimator[i] = (unsigned int)decimators[i];
    }
    for (int i = 0; i < temporal_periodicity; ++i) {
      if (layer_ids[i] < 0 || layer_ids[i] >= temporal_layers)
        die_msg("--temporal-layer-ids entry out of range");
      cfg.ts_layer_id[i] = (unsigned int)layer_ids[i];
    }
  }

  vpx_codec_ctx_t ctx;
  if (vpx_codec_enc_init(&ctx, iface, &cfg, 0)) {
    die_codec_msg(&ctx, "vpx_codec_enc_init");
  }

  if (vpx_codec_control(&ctx, VP8E_SET_CPUUSED, cpu_used))
    die_codec_msg(&ctx, "VP8E_SET_CPUUSED");
  if (vpx_codec_control(&ctx, VP8E_SET_TUNING, tune))
    die_codec_msg(&ctx, "VP8E_SET_TUNING");
  if (vpx_codec_control(&ctx, VP8E_SET_TOKEN_PARTITIONS, token_parts))
    die_codec_msg(&ctx, "VP8E_SET_TOKEN_PARTITIONS");
  if (vpx_codec_control(&ctx, VP8E_SET_STATIC_THRESHOLD, static_thresh))
    die_codec_msg(&ctx, "VP8E_SET_STATIC_THRESHOLD");
  if (vpx_codec_control(&ctx, VP8E_SET_NOISE_SENSITIVITY, noise_sensitivity))
    die_codec_msg(&ctx, "VP8E_SET_NOISE_SENSITIVITY");
  if (vpx_codec_control(&ctx, VP8E_SET_SHARPNESS, sharpness))
    die_codec_msg(&ctx, "VP8E_SET_SHARPNESS");
  if (max_intra_rate_set) {
    if (vpx_codec_control(&ctx, VP8E_SET_MAX_INTRA_BITRATE_PCT, max_intra_rate))
      die_codec_msg(&ctx, "VP8E_SET_MAX_INTRA_BITRATE_PCT");
  }
  if (gf_cbr_boost_set) {
    if (vpx_codec_control(&ctx, VP8E_SET_GF_CBR_BOOST_PCT, gf_cbr_boost))
      die_codec_msg(&ctx, "VP8E_SET_GF_CBR_BOOST_PCT");
  }
  if (vpx_codec_control(&ctx, VP8E_SET_SCREEN_CONTENT_MODE, screen_content))
    die_codec_msg(&ctx, "VP8E_SET_SCREEN_CONTENT_MODE");
  if (vpx_codec_control(&ctx, VP8E_SET_ENABLEAUTOALTREF, auto_alt_ref))
    die_codec_msg(&ctx, "VP8E_SET_ENABLEAUTOALTREF");
  if (cq_level_set) {
    if (vpx_codec_control(&ctx, VP8E_SET_CQ_LEVEL, cq_level))
      die_codec_msg(&ctx, "VP8E_SET_CQ_LEVEL");
  }
  if (arnr_max_set) {
    if (vpx_codec_control(&ctx, VP8E_SET_ARNR_MAXFRAMES, arnr_max))
      die_codec_msg(&ctx, "VP8E_SET_ARNR_MAXFRAMES");
  }
  if (arnr_strength_set) {
    if (vpx_codec_control(&ctx, VP8E_SET_ARNR_STRENGTH, arnr_strength))
      die_codec_msg(&ctx, "VP8E_SET_ARNR_STRENGTH");
  }
  if (arnr_type_set) {
    if (vpx_codec_control(&ctx, VP8E_SET_ARNR_TYPE, arnr_type))
      die_codec_msg(&ctx, "VP8E_SET_ARNR_TYPE");
  }
  if (rtc_external_set) {
    if (vpx_codec_control(&ctx, VP8E_SET_RTC_EXTERNAL_RATECTRL, rtc_external))
      die_codec_msg(&ctx, "VP8E_SET_RTC_EXTERNAL_RATECTRL");
  }
  if (active_map_pattern) {
    apply_active_map(&ctx, mb_rows, mb_cols, active_map_pattern);
  }
  if (roi_map_pattern) {
    int delta_q[4];
    int delta_lf[4];
    unsigned int static_threshold[4];
    default_roi_params(roi_map_pattern, delta_q, delta_lf, static_threshold);
    if (roi_dq_csv) parse_int4_csv(roi_dq_csv, delta_q, "--roi-dq");
    if (roi_dlf_csv) parse_int4_csv(roi_dlf_csv, delta_lf, "--roi-dlf");
    if (roi_static_csv) parse_uint4_csv(roi_static_csv, static_threshold,
                                        "--roi-static");
    apply_roi_map(&ctx, mb_rows, mb_cols, roi_map_pattern, delta_q, delta_lf,
                  static_threshold);
  }

  vpx_image_t img;
  if (!vpx_img_alloc(&img, VPX_IMG_FMT_I420, (unsigned)width, (unsigned)height,
                     1)) {
    die_msg("vpx_img_alloc failed");
  }

  FILE *in = fopen(infile_path, "rb");
  if (!in) die_msg("open %s for read: %s", infile_path, strerror(errno));
  FILE *out = fopen(outfile_path, "wb");
  if (!out) die_msg("open %s for write: %s", outfile_path, strerror(errno));
  FILE *copy_ref_log = NULL;
  if (copy_ref_log_path) {
    copy_ref_log = fopen(copy_ref_log_path, "w");
    if (!copy_ref_log)
      die_msg("open %s for write: %s", copy_ref_log_path, strerror(errno));
  }
  FILE *quantizer_log = NULL;
  if (quantizer_log_path) {
    quantizer_log = fopen(quantizer_log_path, "w");
    if (!quantizer_log)
      die_msg("open %s for write: %s", quantizer_log_path, strerror(errno));
  }

  write_ivf_file_header(out, width, height, fps_den, fps_num, frames);

  size_t plane_buf_capacity = 0;
  uint8_t *plane_buf = NULL;
  int img_width = width;
  int img_height = height;

  vpx_codec_pts_t pts = 0;
  int total_emitted = 0;
  for (int frame_idx = 0; frame_idx <= frames; ++frame_idx) {
    int have_input = frame_idx < frames;
    vpx_image_t *input_img = NULL;
    if (have_input && frame_idx < control_script_count) {
      apply_runtime_controls(&ctx, &cfg, &deadline, control_script[frame_idx],
                             frame_idx, copy_ref_log);
    }
    if (have_input) {
      int cur_width = (int)cfg.g_w;
      int cur_height = (int)cfg.g_h;
      if (cur_width <= 0 || cur_height <= 0) {
        die_msg("invalid runtime dimensions at frame %d: %dx%d", frame_idx,
                cur_width, cur_height);
      }
      if (cur_width != img_width || cur_height != img_height) {
        vpx_img_free(&img);
        if (!vpx_img_alloc(&img, VPX_IMG_FMT_I420, (unsigned)cur_width,
                           (unsigned)cur_height, 1)) {
          die_msg("vpx_img_alloc resize failed");
        }
        img_width = cur_width;
        img_height = cur_height;
      }
      int uv_w = (cur_width + 1) >> 1;
      int uv_h = (cur_height + 1) >> 1;
      size_t y_size = (size_t)cur_width * (size_t)cur_height;
      size_t uv_size = (size_t)uv_w * (size_t)uv_h;
      size_t frame_size = y_size + 2 * uv_size;
      if (frame_size > plane_buf_capacity) {
        uint8_t *new_buf = (uint8_t *)realloc(plane_buf, frame_size);
        if (!new_buf) die_msg("alloc plane buffer");
        plane_buf = new_buf;
        plane_buf_capacity = frame_size;
      }
      if (fread(plane_buf, 1, frame_size, in) != frame_size) {
        die_msg("short read from %s at frame %d", infile_path, frame_idx);
      }
      /* Copy planes into the libvpx-allocated image so the per-plane
       * strides match the libvpx alignment. */
      uint8_t *src_y = plane_buf;
      uint8_t *src_u = plane_buf + y_size;
      uint8_t *src_v = plane_buf + y_size + uv_size;
      for (int row = 0; row < cur_height; ++row) {
        memcpy(img.planes[VPX_PLANE_Y] + (ptrdiff_t)row * img.stride[VPX_PLANE_Y],
               src_y + (ptrdiff_t)row * cur_width, (size_t)cur_width);
      }
      for (int row = 0; row < uv_h; ++row) {
        memcpy(img.planes[VPX_PLANE_U] + (ptrdiff_t)row * img.stride[VPX_PLANE_U],
               src_u + (ptrdiff_t)row * uv_w, (size_t)uv_w);
        memcpy(img.planes[VPX_PLANE_V] + (ptrdiff_t)row * img.stride[VPX_PLANE_V],
               src_v + (ptrdiff_t)row * uv_w, (size_t)uv_w);
      }
      input_img = &img;
    }

    unsigned int frame_flags = 0;
    if (have_input && frame_idx < frame_flag_count) {
      frame_flags = per_frame_flags[frame_idx];
    }

    vpx_codec_err_t enc_err =
        vpx_codec_encode(&ctx, input_img, pts, 1, frame_flags,
                         (unsigned long)deadline);
    if (enc_err) die_codec_msg(&ctx, "vpx_codec_encode");
    if (have_input) ++pts;

    vpx_codec_iter_t iter = NULL;
    const vpx_codec_cx_pkt_t *pkt;
    int call_emitted = 0;
    while ((pkt = vpx_codec_get_cx_data(&ctx, &iter))) {
      if (pkt->kind != VPX_CODEC_CX_FRAME_PKT) continue;
      write_ivf_frame_header(out, pkt->data.frame.pts, pkt->data.frame.sz);
      int packet_pts = (int)pkt->data.frame.pts;
      int make_invisible =
          packet_pts >= 0 && packet_pts < invisible_frame_count &&
          invisible_frames[packet_pts] != 0;
      const uint8_t *packet = (const uint8_t *)pkt->data.frame.buf;
      if (make_invisible && pkt->data.frame.sz > 0) {
        uint8_t first = (uint8_t)(packet[0] & (uint8_t)~0x10u);
        if (fwrite(&first, 1, 1, out) != 1 ||
            fwrite(packet + 1, 1, pkt->data.frame.sz - 1, out) !=
                pkt->data.frame.sz - 1) {
          die_msg("write invisible frame payload to %s", outfile_path);
        }
      } else if (fwrite(packet, 1, pkt->data.frame.sz, out) !=
                 pkt->data.frame.sz) {
        die_msg("write frame payload to %s", outfile_path);
      }
      ++call_emitted;
      ++total_emitted;
    }
    if (quantizer_log) {
      int last_quantizer = -1;
      if (vpx_codec_control(&ctx, VP8E_GET_LAST_QUANTIZER, &last_quantizer))
        die_codec_msg(&ctx, "VP8E_GET_LAST_QUANTIZER");
      if (fprintf(quantizer_log,
                  "frame=%d have_input=%d emitted=%d last_quantizer=%d\n",
                  frame_idx, have_input, call_emitted, last_quantizer) < 0) {
        die_msg("write quantizer log failed");
      }
    }
  }

  free(plane_buf);
  free_csv_strings(control_script, control_script_count);
  fclose(in);
  fclose(out);
  if (copy_ref_log) fclose(copy_ref_log);
  if (quantizer_log) fclose(quantizer_log);
  if (vpx_codec_destroy(&ctx)) die_codec_msg(&ctx, "vpx_codec_destroy");
  vpx_img_free(&img);
  free(per_frame_flags);
  free(invisible_frames);

  if (total_emitted == 0) {
    die_msg("no frames emitted by encoder");
  }
  return 0;
}
