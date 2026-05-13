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
 *   --lag-in-frames=N    cfg.g_lag_in_frames value.
 *   --auto-alt-ref=N     VP8E_SET_ENABLEAUTOALTREF flag (0|1).
 *   --arnr-maxframes=N --arnr-strength=N --arnr-type=N
 *                        ARNR controls.
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

static unsigned int *parse_frame_flags(const char *csv, int *out_count) {
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
  if (!out) die_msg("calloc frame_flags");
  int idx = 0;
  const char *start = csv;
  while (1) {
    const char *end = strchr(start, ',');
    char buf[32];
    size_t len = end ? (size_t)(end - start) : strlen(start);
    if (len >= sizeof(buf)) die_msg("frame-flags token too long");
    memcpy(buf, start, len);
    buf[len] = '\0';
    char *parse_end = NULL;
    unsigned long v = strtoul(buf, &parse_end, 0);
    if (parse_end == buf || (parse_end && *parse_end != '\0')) {
      die_msg("invalid --frame-flags token: %s", buf);
    }
    out[idx++] = (unsigned int)v;
    if (!end) break;
    start = end + 1;
  }
  *out_count = idx;
  return out;
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
  int lag_in_frames = 0;
  int auto_alt_ref = 0;
  int arnr_max = 0, arnr_max_set = 0;
  int arnr_strength = 0, arnr_strength_set = 0;
  int arnr_type = 0, arnr_type_set = 0;
  const char *frame_flags_csv = NULL;

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
    } else if ((v = flag_value(a, "--frame-flags"))) {
      frame_flags_csv = v;
    } else {
      die_msg("unknown argument: %s", a);
    }
  }

  if (!infile_path || !outfile_path) die_msg("missing --infile/--outfile");
  if (width <= 0 || height <= 0) die_msg("invalid width/height");
  if (frames <= 0) die_msg("--frames must be > 0");

  int frame_flag_count = 0;
  unsigned int *per_frame_flags =
      parse_frame_flags(frame_flags_csv, &frame_flag_count);

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
  cfg.kf_min_dist = (unsigned)kf_min_dist;
  cfg.kf_max_dist = (unsigned)kf_max_dist;
  cfg.kf_mode = kf_disabled ? VPX_KF_DISABLED : VPX_KF_AUTO;

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

  vpx_image_t img;
  if (!vpx_img_alloc(&img, VPX_IMG_FMT_I420, (unsigned)width, (unsigned)height,
                     1)) {
    die_msg("vpx_img_alloc failed");
  }

  FILE *in = fopen(infile_path, "rb");
  if (!in) die_msg("open %s for read: %s", infile_path, strerror(errno));
  FILE *out = fopen(outfile_path, "wb");
  if (!out) die_msg("open %s for write: %s", outfile_path, strerror(errno));

  write_ivf_file_header(out, width, height, fps_den, fps_num, frames);

  int uv_w = (width + 1) >> 1;
  int uv_h = (height + 1) >> 1;
  size_t y_size = (size_t)width * (size_t)height;
  size_t uv_size = (size_t)uv_w * (size_t)uv_h;
  uint8_t *plane_buf = (uint8_t *)malloc(y_size + 2 * uv_size);
  if (!plane_buf) die_msg("alloc plane buffer");

  vpx_codec_pts_t pts = 0;
  int total_emitted = 0;
  for (int frame_idx = 0; frame_idx <= frames; ++frame_idx) {
    int have_input = frame_idx < frames;
    vpx_image_t *input_img = NULL;
    if (have_input) {
      if (fread(plane_buf, 1, y_size + 2 * uv_size, in) !=
          y_size + 2 * uv_size) {
        die_msg("short read from %s at frame %d", infile_path, frame_idx);
      }
      /* Copy planes into the libvpx-allocated image so the per-plane
       * strides match the libvpx alignment. */
      uint8_t *src_y = plane_buf;
      uint8_t *src_u = plane_buf + y_size;
      uint8_t *src_v = plane_buf + y_size + uv_size;
      for (int row = 0; row < height; ++row) {
        memcpy(img.planes[VPX_PLANE_Y] + (ptrdiff_t)row * img.stride[VPX_PLANE_Y],
               src_y + (ptrdiff_t)row * width, (size_t)width);
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
    while ((pkt = vpx_codec_get_cx_data(&ctx, &iter))) {
      if (pkt->kind != VPX_CODEC_CX_FRAME_PKT) continue;
      write_ivf_frame_header(out, pkt->data.frame.pts, pkt->data.frame.sz);
      if (fwrite(pkt->data.frame.buf, 1, pkt->data.frame.sz, out) !=
          pkt->data.frame.sz) {
        die_msg("write frame payload to %s", outfile_path);
      }
      ++total_emitted;
    }
  }

  free(plane_buf);
  fclose(in);
  fclose(out);
  if (vpx_codec_destroy(&ctx)) die_codec_msg(&ctx, "vpx_codec_destroy");
  vpx_img_free(&img);
  free(per_frame_flags);

  if (total_emitted == 0) {
    die_msg("no frames emitted by encoder");
  }
  return 0;
}
