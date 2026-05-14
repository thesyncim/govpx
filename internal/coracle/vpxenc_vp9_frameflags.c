//go:build ignore

// This file is built directly by build_vpxenc_vp9_frameflags.sh into the
// vpxenc-vp9-frameflags helper binary; it is not part of any Go cgo package.

#include <errno.h>
#include <limits.h>
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "vpx/vp8cx.h"
#include "vpx/vpx_codec.h"
#include "vpx/vpx_encoder.h"
#include "vpx/vpx_image.h"

#define VP9_FOURCC 0x30395056

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

static void write_ivf_file_header(FILE *outfile, int width, int height,
                                  int timebase_num, int timebase_den,
                                  int frame_count) {
  char header[32];
  header[0] = 'D';
  header[1] = 'K';
  header[2] = 'I';
  header[3] = 'F';
  mem_put_le16(header + 4, 0);
  mem_put_le16(header + 6, 32);
  mem_put_le32(header + 8, VP9_FOURCC);
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
  mem_put_le32(header + 4, (int)(pts & 0xffffffff));
  mem_put_le32(header + 8, (int)(pts >> 32));
  if (fwrite(header, 1, 12, outfile) != 12) {
    fprintf(stderr, "write ivf frame header\n");
    exit(EXIT_FAILURE);
  }
}

static void die_msg(const char *msg) {
  fputs(msg, stderr);
  fputc('\n', stderr);
  exit(EXIT_FAILURE);
}

static void die_codec_msg(vpx_codec_ctx_t *ctx, const char *what) {
  const char *detail = vpx_codec_error_detail(ctx);
  fprintf(stderr, "%s: %s%s%s\n", what, vpx_codec_error(ctx),
          detail ? ": " : "", detail ? detail : "");
  exit(EXIT_FAILURE);
}

static int parse_int(const char *arg, const char *flag) {
  char *end = NULL;
  long v = strtol(arg, &end, 10);
  if (end == arg || (end && *end != '\0')) {
    fprintf(stderr, "invalid integer for %s: %s\n", flag, arg);
    exit(EXIT_FAILURE);
  }
  if (v < INT_MIN || v > INT_MAX) {
    fprintf(stderr, "integer for %s out of range: %s\n", flag, arg);
    exit(EXIT_FAILURE);
  }
  return (int)v;
}

static const char *flag_value(const char *arg, const char *flag) {
  size_t n = strlen(flag);
  if (strncmp(arg, flag, n) != 0) return NULL;
  if (arg[n] != '=') return NULL;
  return arg + n + 1;
}

static enum vpx_rc_mode parse_end_usage(const char *value) {
  if (strcmp(value, "vbr") == 0) return VPX_VBR;
  if (strcmp(value, "cbr") == 0) return VPX_CBR;
  if (strcmp(value, "cq") == 0) return VPX_CQ;
  if (strcmp(value, "q") == 0) return VPX_Q;
  fprintf(stderr, "invalid --end-usage: %s\n", value);
  exit(EXIT_FAILURE);
}

static int parse_deadline(const char *value) {
  if (strcmp(value, "good") == 0) return (int)VPX_DL_GOOD_QUALITY;
  if (strcmp(value, "best") == 0) return (int)VPX_DL_BEST_QUALITY;
  if (strcmp(value, "rt") == 0) return (int)VPX_DL_REALTIME;
  fprintf(stderr, "invalid --deadline: %s\n", value);
  exit(EXIT_FAILURE);
}

static unsigned int *parse_frame_flags(const char *csv, int *out_count) {
  if (!csv) {
    *out_count = 0;
    return NULL;
  }
  int count = 1;
  for (const char *p = csv; *p; ++p) {
    if (*p == ',') ++count;
  }
  unsigned int *out = calloc((size_t)count, sizeof(*out));
  if (!out) die_msg("calloc frame flags");
  int idx = 0;
  const char *start = csv;
  for (;;) {
    const char *end = strchr(start, ',');
    char buf[32];
    size_t len = end ? (size_t)(end - start) : strlen(start);
    if (len >= sizeof(buf)) die_msg("frame-flags token too long");
    memcpy(buf, start, len);
    buf[len] = '\0';
    char *parse_end = NULL;
    unsigned long v = strtoul(buf, &parse_end, 0);
    if (parse_end == buf || (parse_end && *parse_end != '\0')) {
      fprintf(stderr, "invalid --frame-flags token: %s\n", buf);
      exit(EXIT_FAILURE);
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
  const char *frame_flags_csv = NULL;
  int width = 0, height = 0, frames = 0;
  int fps_num = 30, fps_den = 1;
  int target_kbps = 700;
  int min_q = 4, max_q = 56, cq_level = 32;
  int kf_min_dist = 0, kf_max_dist = 128;
  int deadline = (int)VPX_DL_REALTIME;
  int cpu_used = 8;
  int error_resilient = 0;
  int lag_in_frames = 0;
  int auto_alt_ref = 0;
  int aq_mode = 0;
  int row_mt = 0;
  int tile_columns = 0;
  int tile_rows = 0;
  int lossless = 0;
  enum vpx_rc_mode end_usage = VPX_Q;

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
    } else if ((v = flag_value(a, "--frames"))) {
      frames = parse_int(v, "--frames");
    } else if ((v = flag_value(a, "--fps-num"))) {
      fps_num = parse_int(v, "--fps-num");
    } else if ((v = flag_value(a, "--fps-den"))) {
      fps_den = parse_int(v, "--fps-den");
    } else if ((v = flag_value(a, "--target-bitrate"))) {
      target_kbps = parse_int(v, "--target-bitrate");
    } else if ((v = flag_value(a, "--min-q"))) {
      min_q = parse_int(v, "--min-q");
    } else if ((v = flag_value(a, "--max-q"))) {
      max_q = parse_int(v, "--max-q");
    } else if ((v = flag_value(a, "--cq-level"))) {
      cq_level = parse_int(v, "--cq-level");
    } else if ((v = flag_value(a, "--kf-min-dist"))) {
      kf_min_dist = parse_int(v, "--kf-min-dist");
    } else if ((v = flag_value(a, "--kf-max-dist"))) {
      kf_max_dist = parse_int(v, "--kf-max-dist");
    } else if ((v = flag_value(a, "--deadline"))) {
      deadline = parse_deadline(v);
    } else if ((v = flag_value(a, "--cpu-used"))) {
      cpu_used = parse_int(v, "--cpu-used");
    } else if ((v = flag_value(a, "--error-resilient"))) {
      error_resilient = parse_int(v, "--error-resilient");
    } else if ((v = flag_value(a, "--lag-in-frames"))) {
      lag_in_frames = parse_int(v, "--lag-in-frames");
    } else if ((v = flag_value(a, "--auto-alt-ref"))) {
      auto_alt_ref = parse_int(v, "--auto-alt-ref");
    } else if ((v = flag_value(a, "--aq-mode"))) {
      aq_mode = parse_int(v, "--aq-mode");
    } else if ((v = flag_value(a, "--row-mt"))) {
      row_mt = parse_int(v, "--row-mt");
    } else if ((v = flag_value(a, "--tile-columns"))) {
      tile_columns = parse_int(v, "--tile-columns");
    } else if ((v = flag_value(a, "--tile-rows"))) {
      tile_rows = parse_int(v, "--tile-rows");
    } else if ((v = flag_value(a, "--lossless"))) {
      lossless = parse_int(v, "--lossless");
    } else if ((v = flag_value(a, "--end-usage"))) {
      end_usage = parse_end_usage(v);
    } else if ((v = flag_value(a, "--frame-flags"))) {
      frame_flags_csv = v;
    } else if (strcmp(a, "--disable-warning-prompt") == 0) {
    } else {
      fprintf(stderr, "unknown argument: %s\n", a);
      exit(EXIT_FAILURE);
    }
  }

  if (!infile_path || !outfile_path) die_msg("missing --infile/--outfile");
  if (width <= 0 || height <= 0) die_msg("invalid width/height");
  if (frames <= 0) die_msg("--frames must be > 0");
  if (fps_num <= 0 || fps_den <= 0) die_msg("invalid fps");

  int frame_flag_count = 0;
  unsigned int *per_frame_flags =
      parse_frame_flags(frame_flags_csv, &frame_flag_count);

  vpx_codec_iface_t *iface = vpx_codec_vp9_cx();
  vpx_codec_enc_cfg_t cfg;
  if (vpx_codec_enc_config_default(iface, &cfg, 0)) {
    die_msg("vpx_codec_enc_config_default");
  }
  cfg.g_w = (unsigned)width;
  cfg.g_h = (unsigned)height;
  cfg.g_profile = 0;
  cfg.g_timebase.num = 1;
  cfg.g_timebase.den = 1000;
  cfg.g_threads = 1;
  cfg.g_error_resilient = (unsigned)error_resilient;
  cfg.g_lag_in_frames = (unsigned)lag_in_frames;
  cfg.rc_end_usage = end_usage;
  cfg.rc_target_bitrate = (unsigned)target_kbps;
  cfg.rc_min_quantizer = (unsigned)min_q;
  cfg.rc_max_quantizer = (unsigned)max_q;
  cfg.kf_min_dist = (unsigned)kf_min_dist;
  cfg.kf_max_dist = (unsigned)kf_max_dist;

  vpx_codec_ctx_t ctx;
  if (vpx_codec_enc_init(&ctx, iface, &cfg, 0)) {
    die_codec_msg(&ctx, "vpx_codec_enc_init");
  }
  if (vpx_codec_control(&ctx, VP8E_SET_CPUUSED, cpu_used))
    die_codec_msg(&ctx, "VP8E_SET_CPUUSED");
  if (vpx_codec_control(&ctx, VP8E_SET_ENABLEAUTOALTREF,
                        (unsigned)auto_alt_ref))
    die_codec_msg(&ctx, "VP8E_SET_ENABLEAUTOALTREF");
  if (vpx_codec_control(&ctx, VP8E_SET_CQ_LEVEL, (unsigned)cq_level))
    die_codec_msg(&ctx, "VP8E_SET_CQ_LEVEL");
  if (vpx_codec_control(&ctx, VP9E_SET_AQ_MODE, (unsigned)aq_mode))
    die_codec_msg(&ctx, "VP9E_SET_AQ_MODE");
  if (vpx_codec_control(&ctx, VP9E_SET_ROW_MT, (unsigned)row_mt))
    die_codec_msg(&ctx, "VP9E_SET_ROW_MT");
  if (vpx_codec_control(&ctx, VP9E_SET_TILE_COLUMNS, tile_columns))
    die_codec_msg(&ctx, "VP9E_SET_TILE_COLUMNS");
  if (vpx_codec_control(&ctx, VP9E_SET_TILE_ROWS, tile_rows))
    die_codec_msg(&ctx, "VP9E_SET_TILE_ROWS");
  if (vpx_codec_control(&ctx, VP9E_SET_LOSSLESS, (unsigned)lossless))
    die_codec_msg(&ctx, "VP9E_SET_LOSSLESS");

  vpx_image_t img;
  if (!vpx_img_alloc(&img, VPX_IMG_FMT_I420, (unsigned)width,
                     (unsigned)height, 1)) {
    die_msg("vpx_img_alloc failed");
  }

  FILE *in = fopen(infile_path, "rb");
  if (!in) {
    fprintf(stderr, "open %s for read: %s\n", infile_path, strerror(errno));
    exit(EXIT_FAILURE);
  }
  FILE *out = fopen(outfile_path, "wb");
  if (!out) {
    fprintf(stderr, "open %s for write: %s\n", outfile_path, strerror(errno));
    exit(EXIT_FAILURE);
  }
  write_ivf_file_header(out, width, height, 1, 1000, frames);

  int uv_w = (width + 1) >> 1;
  int uv_h = (height + 1) >> 1;
  size_t y_size = (size_t)width * (size_t)height;
  size_t uv_size = (size_t)uv_w * (size_t)uv_h;
  size_t frame_size = y_size + 2 * uv_size;
  uint8_t *plane_buf = (uint8_t *)malloc(frame_size);
  if (!plane_buf) die_msg("alloc plane buffer");

  vpx_codec_pts_t pts = 0;
  int frame_duration = (fps_den * 1000) / fps_num;
  if (frame_duration <= 0) frame_duration = 1;
  int total_emitted = 0;
  for (int frame_idx = 0; frame_idx <= frames; ++frame_idx) {
    int have_input = frame_idx < frames;
    vpx_image_t *input_img = NULL;
    if (have_input) {
      if (fread(plane_buf, 1, frame_size, in) != frame_size) {
        fprintf(stderr, "short read from %s at frame %d\n", infile_path,
                frame_idx);
        exit(EXIT_FAILURE);
      }
      uint8_t *src_y = plane_buf;
      uint8_t *src_u = plane_buf + y_size;
      uint8_t *src_v = plane_buf + y_size + uv_size;
      for (int row = 0; row < height; ++row) {
        memcpy(img.planes[VPX_PLANE_Y] +
                   (ptrdiff_t)row * img.stride[VPX_PLANE_Y],
               src_y + (ptrdiff_t)row * width, (size_t)width);
      }
      for (int row = 0; row < uv_h; ++row) {
        memcpy(img.planes[VPX_PLANE_U] +
                   (ptrdiff_t)row * img.stride[VPX_PLANE_U],
               src_u + (ptrdiff_t)row * uv_w, (size_t)uv_w);
        memcpy(img.planes[VPX_PLANE_V] +
                   (ptrdiff_t)row * img.stride[VPX_PLANE_V],
               src_v + (ptrdiff_t)row * uv_w, (size_t)uv_w);
      }
      input_img = &img;
    }

    unsigned int frame_flags = 0;
    if (have_input && frame_idx < frame_flag_count) {
      frame_flags = per_frame_flags[frame_idx];
    }
    if (vpx_codec_encode(&ctx, input_img, pts, (unsigned long)frame_duration,
                         frame_flags,
                         (unsigned long)deadline)) {
      die_codec_msg(&ctx, "vpx_codec_encode");
    }
    if (have_input) pts += frame_duration;

    vpx_codec_iter_t iter = NULL;
    const vpx_codec_cx_pkt_t *pkt;
    while ((pkt = vpx_codec_get_cx_data(&ctx, &iter))) {
      if (pkt->kind != VPX_CODEC_CX_FRAME_PKT) continue;
      write_ivf_frame_header(out, pkt->data.frame.pts, pkt->data.frame.sz);
      if (fwrite(pkt->data.frame.buf, 1, pkt->data.frame.sz, out) !=
          pkt->data.frame.sz) {
        die_msg("write frame payload");
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
  if (total_emitted == 0) die_msg("no frames emitted by encoder");
  return 0;
}
