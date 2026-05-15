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

static int starts_with(const char *s, const char *prefix) {
  return strncmp(s, prefix, strlen(prefix)) == 0;
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

static unsigned int *parse_uint_csv(const char *csv, int *out_count,
                                    const char *flag) {
  if (!csv) {
    *out_count = 0;
    return NULL;
  }
  int count = 1;
  for (const char *p = csv; *p; ++p) {
    if (*p == ',') ++count;
  }
  unsigned int *out = calloc((size_t)count, sizeof(*out));
  if (!out) die_msg("calloc uint csv");
  int idx = 0;
  const char *start = csv;
  for (;;) {
    const char *end = strchr(start, ',');
    char buf[32];
    size_t len = end ? (size_t)(end - start) : strlen(start);
    if (len >= sizeof(buf)) {
      fprintf(stderr, "%s token too long\n", flag);
      exit(EXIT_FAILURE);
    }
    memcpy(buf, start, len);
    buf[len] = '\0';
    char *parse_end = NULL;
    unsigned long v = strtoul(buf, &parse_end, 0);
    if (parse_end == buf || (parse_end && *parse_end != '\0')) {
      fprintf(stderr, "invalid %s token: %s\n", flag, buf);
      exit(EXIT_FAILURE);
    }
    out[idx++] = (unsigned int)v;
    if (!end) break;
    start = end + 1;
  }
  *out_count = idx;
  return out;
}

static char **parse_csv_strings(const char *csv, int *out_count,
                                const char *flag) {
  if (!csv) {
    *out_count = 0;
    return NULL;
  }
  int count = 1;
  for (const char *p = csv; *p; ++p) {
    if (*p == ',') ++count;
  }
  char **out = calloc((size_t)count, sizeof(*out));
  if (!out) die_msg("calloc csv strings");
  int idx = 0;
  const char *start = csv;
  for (;;) {
    const char *end = strchr(start, ',');
    size_t len = end ? (size_t)(end - start) : strlen(start);
    char *token = malloc(len + 1);
    if (!token) die_msg("malloc csv token");
    memcpy(token, start, len);
    token[len] = '\0';
    out[idx++] = token;
    if (!end) break;
    start = end + 1;
  }
  *out_count = idx;
  (void)flag;
  return out;
}

static void free_csv_strings(char **tokens, int count) {
  if (!tokens) return;
  for (int i = 0; i < count; ++i) free(tokens[i]);
  free(tokens);
}

static int *parse_int_schedule(const char *csv, int frames, const char *flag) {
  if (!csv) return NULL;
  if (frames <= 0) die_msg("invalid schedule frame count");
  int *out = (int *)malloc((size_t)frames * sizeof(*out));
  if (!out) die_msg("malloc int schedule");
  for (int i = 0; i < frames; ++i) out[i] = -1;

  const char *start = csv;
  while (*start) {
    const char *end = strchr(start, ',');
    size_t len = end ? (size_t)(end - start) : strlen(start);
    char buf[64];
    if (len == 0 || len >= sizeof(buf)) {
      fprintf(stderr, "invalid %s token length\n", flag);
      exit(EXIT_FAILURE);
    }
    memcpy(buf, start, len);
    buf[len] = '\0';

    char *colon = strchr(buf, ':');
    if (!colon) {
      fprintf(stderr, "invalid %s token, want frame:value: %s\n", flag, buf);
      exit(EXIT_FAILURE);
    }
    *colon = '\0';
    int frame = parse_int(buf, flag);
    int value = parse_int(colon + 1, flag);
    if (frame < 0 || frame >= frames) {
      fprintf(stderr, "%s frame index out of range: %d for %d frames\n", flag,
              frame, frames);
      exit(EXIT_FAILURE);
    }
    out[frame] = value;

    if (!end) break;
    start = end + 1;
  }
  return out;
}

static void parse_int_csv_exact(const char *csv, int *out, int expected,
                                const char *flag) {
  if (!csv) {
    fprintf(stderr, "%s expects %d comma-separated integers\n", flag, expected);
    exit(EXIT_FAILURE);
  }
  if (expected <= 0) die_msg("invalid exact csv count");
  int count = 0;
  const char *start = csv;
  for (;;) {
    if (count >= expected) {
      fprintf(stderr, "%s has too many entries\n", flag);
      exit(EXIT_FAILURE);
    }
    const char *end = strchr(start, ',');
    size_t len = end ? (size_t)(end - start) : strlen(start);
    char buf[32];
    if (len == 0 || len >= sizeof(buf)) {
      fprintf(stderr, "invalid %s token length\n", flag);
      exit(EXIT_FAILURE);
    }
    memcpy(buf, start, len);
    buf[len] = '\0';
    out[count++] = parse_int(buf, flag);
    if (!end) break;
    start = end + 1;
  }
  if (count != expected) {
    fprintf(stderr, "%s expects exactly %d comma-separated integers, got %d\n",
            flag, expected, count);
    exit(EXIT_FAILURE);
  }
}

static int control_value_int(const char *token, const char *prefix) {
  return parse_int(token + strlen(prefix), prefix);
}

static void parse_resize_token(const char *spec, int *width, int *height) {
  char buf[64];
  size_t len = strlen(spec);
  if (len >= sizeof(buf)) {
    fprintf(stderr, "resize token too long: %s\n", spec);
    exit(EXIT_FAILURE);
  }
  memcpy(buf, spec, len + 1);
  char *sep = strchr(buf, 'x');
  if (!sep) sep = strchr(buf, 'X');
  if (!sep) {
    fprintf(stderr, "resize expects WxH: %s\n", spec);
    exit(EXIT_FAILURE);
  }
  *sep++ = '\0';
  int w = parse_int(buf, "resize width");
  int h = parse_int(sep, "resize height");
  if (w <= 0 || h <= 0) {
    fprintf(stderr, "resize dimensions must be positive: %s\n", spec);
    exit(EXIT_FAILURE);
  }
  *width = w;
  *height = h;
}

struct vp9_runtime_control_context {
  vpx_codec_ctx_t *ctx;
  vpx_codec_enc_cfg_t *cfg;
  int *deadline;
  int *target_kbps;
  int *fps_num;
  int *buffer_size_ms;
  int *buffer_initial_ms;
  int *buffer_optimal_ms;
  int *drop_frame_water_mark;
  int *min_q;
  int *max_q;
  int *cq_level;
  int config_changed;
};

static void apply_vp9_runtime_control_token(
    struct vp9_runtime_control_context *ctx, const char *token) {
  if (starts_with(token, "resize:")) {
    int width = 0;
    int height = 0;
    parse_resize_token(token + strlen("resize:"), &width, &height);
    ctx->cfg->g_w = (unsigned)width;
    ctx->cfg->g_h = (unsigned)height;
    ctx->config_changed = 1;
  } else if (starts_with(token, "bitrate:")) {
    *ctx->target_kbps = control_value_int(token, "bitrate:");
    ctx->cfg->rc_target_bitrate = (unsigned)*ctx->target_kbps;
    ctx->config_changed = 1;
  } else if (starts_with(token, "fps:")) {
    int fps = control_value_int(token, "fps:");
    if (fps <= 0) die_msg("runtime fps must be positive");
    *ctx->fps_num = fps;
  } else if (starts_with(token, "minq:")) {
    *ctx->min_q = control_value_int(token, "minq:");
    ctx->cfg->rc_min_quantizer = (unsigned)*ctx->min_q;
    ctx->config_changed = 1;
  } else if (starts_with(token, "maxq:")) {
    *ctx->max_q = control_value_int(token, "maxq:");
    ctx->cfg->rc_max_quantizer = (unsigned)*ctx->max_q;
    ctx->config_changed = 1;
  } else if (starts_with(token, "drop:")) {
    *ctx->drop_frame_water_mark = control_value_int(token, "drop:");
    ctx->cfg->rc_dropframe_thresh = (unsigned)*ctx->drop_frame_water_mark;
    ctx->config_changed = 1;
  } else if (starts_with(token, "bufsz:")) {
    *ctx->buffer_size_ms = control_value_int(token, "bufsz:");
    ctx->cfg->rc_buf_sz = (unsigned)*ctx->buffer_size_ms;
    ctx->config_changed = 1;
  } else if (starts_with(token, "bufinit:")) {
    *ctx->buffer_initial_ms = control_value_int(token, "bufinit:");
    ctx->cfg->rc_buf_initial_sz = (unsigned)*ctx->buffer_initial_ms;
    ctx->config_changed = 1;
  } else if (starts_with(token, "bufopt:")) {
    *ctx->buffer_optimal_ms = control_value_int(token, "bufopt:");
    ctx->cfg->rc_buf_optimal_sz = (unsigned)*ctx->buffer_optimal_ms;
    ctx->config_changed = 1;
  } else if (starts_with(token, "endusage:")) {
    ctx->cfg->rc_end_usage = parse_end_usage(token + strlen("endusage:"));
    ctx->config_changed = 1;
  } else if (starts_with(token, "deadline:")) {
    *ctx->deadline = parse_deadline(token + strlen("deadline:"));
  } else if (starts_with(token, "cpu:")) {
    if (vpx_codec_control(ctx->ctx, VP8E_SET_CPUUSED,
                          control_value_int(token, "cpu:"))) {
      die_codec_msg(ctx->ctx, "runtime VP8E_SET_CPUUSED");
    }
  } else if (starts_with(token, "cq:")) {
    *ctx->cq_level = control_value_int(token, "cq:");
    if (vpx_codec_control(ctx->ctx, VP8E_SET_CQ_LEVEL,
                          (unsigned)*ctx->cq_level)) {
      die_codec_msg(ctx->ctx, "runtime VP8E_SET_CQ_LEVEL");
    }
  } else if (starts_with(token, "autoaltref:")) {
    if (vpx_codec_control(ctx->ctx, VP8E_SET_ENABLEAUTOALTREF,
                          (unsigned)control_value_int(token, "autoaltref:"))) {
      die_codec_msg(ctx->ctx, "runtime VP8E_SET_ENABLEAUTOALTREF");
    }
  } else if (starts_with(token, "aq:")) {
    if (vpx_codec_control(ctx->ctx, VP9E_SET_AQ_MODE,
                          (unsigned)control_value_int(token, "aq:"))) {
      die_codec_msg(ctx->ctx, "runtime VP9E_SET_AQ_MODE");
    }
  } else if (starts_with(token, "rowmt:")) {
    if (vpx_codec_control(ctx->ctx, VP9E_SET_ROW_MT,
                          (unsigned)control_value_int(token, "rowmt:"))) {
      die_codec_msg(ctx->ctx, "runtime VP9E_SET_ROW_MT");
    }
  } else if (starts_with(token, "tilecols:")) {
    if (vpx_codec_control(ctx->ctx, VP9E_SET_TILE_COLUMNS,
                          control_value_int(token, "tilecols:"))) {
      die_codec_msg(ctx->ctx, "runtime VP9E_SET_TILE_COLUMNS");
    }
  } else if (starts_with(token, "tilerows:")) {
    if (vpx_codec_control(ctx->ctx, VP9E_SET_TILE_ROWS,
                          control_value_int(token, "tilerows:"))) {
      die_codec_msg(ctx->ctx, "runtime VP9E_SET_TILE_ROWS");
    }
  } else if (starts_with(token, "lossless:")) {
    if (vpx_codec_control(ctx->ctx, VP9E_SET_LOSSLESS,
                          (unsigned)control_value_int(token, "lossless:"))) {
      die_codec_msg(ctx->ctx, "runtime VP9E_SET_LOSSLESS");
    }
  } else if (*token != '\0' && strcmp(token, "-") != 0) {
    fprintf(stderr, "unknown runtime control token: %s\n", token);
    exit(EXIT_FAILURE);
  }
}

static void apply_vp9_runtime_controls(
    vpx_codec_ctx_t *codec_ctx, vpx_codec_enc_cfg_t *cfg, int *deadline,
    int *target_kbps, int *fps_num, int *buffer_size_ms,
    int *buffer_initial_ms, int *buffer_optimal_ms,
    int *drop_frame_water_mark, int *min_q, int *max_q, int *cq_level,
    const char *entry) {
  if (!entry || !*entry || strcmp(entry, "-") == 0) return;
  char buf[1024];
  size_t len = strlen(entry);
  if (len >= sizeof(buf)) {
    fprintf(stderr, "control-script entry too long: %s\n", entry);
    exit(EXIT_FAILURE);
  }
  memcpy(buf, entry, len + 1);
  struct vp9_runtime_control_context ctx = {
      codec_ctx, cfg, deadline, target_kbps, fps_num, buffer_size_ms,
      buffer_initial_ms, buffer_optimal_ms, drop_frame_water_mark, min_q,
      max_q, cq_level, 0};
  char *start = buf;
  for (;;) {
    char *end = strchr(start, '+');
    if (end) *end = '\0';
    apply_vp9_runtime_control_token(&ctx, start);
    if (!end) break;
    start = end + 1;
  }
  if (ctx.config_changed && vpx_codec_enc_config_set(codec_ctx, cfg)) {
    die_codec_msg(codec_ctx, "runtime vpx_codec_enc_config_set");
  }
}

static uint8_t vp9_packet_first_byte_with_show_frame(uint8_t first,
                                                     int show_frame) {
  if ((first & 0xc0u) != 0x80u) {
    die_msg("VP9 packet has invalid frame marker");
  }
  if ((first & 0x30u) != 0) {
    die_msg("VP9 invisible-frame patch only supports profile 0");
  }
  if ((first & 0x08u) != 0) {
    die_msg("VP9 invisible-frame patch does not support show-existing packets");
  }
  if (!show_frame && (first & 0x04u) != 0) {
    die_msg("VP9 invisible-frame patch only supports keyframe packets");
  }
  if (show_frame) return (uint8_t)(first | 0x02u);
  return (uint8_t)(first & (uint8_t)~0x02u);
}

static void vp9_temporal_metadata_for_frame(
    const vpx_codec_enc_cfg_t *cfg, int frame_idx, unsigned int frame_flags,
    int temporal_layers, int temporal_periodicity, int key_frame,
    int ref_layer[3], int *tl0_pic_idx, int *tl0_valid,
    int *out_layer_id, int *out_layer_count, int *out_tl0_pic_idx,
    int *out_layer_sync) {
  *out_layer_id = 0;
  *out_layer_count = 1;
  *out_tl0_pic_idx = 0;
  *out_layer_sync = 0;
  if (temporal_layers <= 0 || temporal_periodicity <= 0) return;

  int layer_id =
      (int)cfg->ts_layer_id[frame_idx % temporal_periodicity];
  int cur_tl0 = *tl0_pic_idx;
  if (layer_id == 0) {
    cur_tl0 = *tl0_valid ? *tl0_pic_idx + 1 : 0;
  }

  int layer_sync = 0;
  if (layer_id > 0) {
    layer_sync = 1;
    if ((frame_flags & VP8_EFLAG_NO_REF_LAST) == 0 &&
        ref_layer[0] >= layer_id) {
      layer_sync = 0;
    }
    if (layer_sync && (frame_flags & VP8_EFLAG_NO_REF_GF) == 0 &&
        ref_layer[1] >= layer_id) {
      layer_sync = 0;
    }
    if (layer_sync && (frame_flags & VP8_EFLAG_NO_REF_ARF) == 0 &&
        ref_layer[2] >= layer_id) {
      layer_sync = 0;
    }
  }

  int refresh_last = key_frame || ((frame_flags & VP8_EFLAG_NO_UPD_LAST) == 0);
  int refresh_golden = key_frame || ((frame_flags & VP8_EFLAG_NO_UPD_GF) == 0);
  int refresh_altref = key_frame || ((frame_flags & VP8_EFLAG_NO_UPD_ARF) == 0);
  if (key_frame) {
    ref_layer[0] = 0;
    ref_layer[1] = 0;
    ref_layer[2] = 0;
  } else {
    if (refresh_last) ref_layer[0] = layer_id;
    if (refresh_golden) ref_layer[1] = layer_id;
    if (refresh_altref) ref_layer[2] = layer_id;
  }
  if (layer_id == 0) {
    *tl0_pic_idx = cur_tl0 & 0xff;
    *tl0_valid = 1;
  }

  *out_layer_id = layer_id;
  *out_layer_count = temporal_layers;
  *out_tl0_pic_idx = cur_tl0 & 0xff;
  *out_layer_sync = layer_sync;
}

int main(int argc, char **argv) {
  const char *infile_path = NULL;
  const char *outfile_path = NULL;
  const char *trace_path = NULL;
  const char *frame_flags_csv = NULL;
  const char *target_bitrate_schedule_csv = NULL;
  const char *min_q_schedule_csv = NULL;
  const char *max_q_schedule_csv = NULL;
  const char *drop_frame_schedule_csv = NULL;
  const char *fps_schedule_csv = NULL;
  const char *buf_size_schedule_csv = NULL;
  const char *buf_initial_schedule_csv = NULL;
  const char *buf_optimal_schedule_csv = NULL;
  const char *temporal_bitrates_csv = NULL;
  const char *temporal_decimators_csv = NULL;
  const char *temporal_layer_ids_csv = NULL;
  const char *invisible_frames_csv = NULL;
  const char *control_script_csv = NULL;
  int width = 0, height = 0, frames = 0;
  int fps_num = 30, fps_den = 1;
  int target_kbps = 700;
  int buffer_size_ms = 6000;
  int buffer_initial_ms = 4000;
  int buffer_optimal_ms = 5000;
  int drop_frame_water_mark = 0;
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
  int temporal_layers = 0;
  int temporal_periodicity = 0;
  enum vpx_rc_mode end_usage = VPX_Q;

  for (int i = 1; i < argc; ++i) {
    const char *a = argv[i];
    const char *v;
    if ((v = flag_value(a, "--infile"))) {
      infile_path = v;
    } else if ((v = flag_value(a, "--outfile"))) {
      outfile_path = v;
    } else if ((v = flag_value(a, "--trace-out"))) {
      trace_path = v;
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
    } else if ((v = flag_value(a, "--buf-sz"))) {
      buffer_size_ms = parse_int(v, "--buf-sz");
    } else if ((v = flag_value(a, "--buf-initial-sz"))) {
      buffer_initial_ms = parse_int(v, "--buf-initial-sz");
    } else if ((v = flag_value(a, "--buf-optimal-sz"))) {
      buffer_optimal_ms = parse_int(v, "--buf-optimal-sz");
    } else if ((v = flag_value(a, "--drop-frame"))) {
      drop_frame_water_mark = parse_int(v, "--drop-frame");
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
    } else if ((v = flag_value(a, "--target-bitrate-schedule"))) {
      target_bitrate_schedule_csv = v;
    } else if ((v = flag_value(a, "--min-q-schedule"))) {
      min_q_schedule_csv = v;
    } else if ((v = flag_value(a, "--max-q-schedule"))) {
      max_q_schedule_csv = v;
    } else if ((v = flag_value(a, "--drop-frame-schedule"))) {
      drop_frame_schedule_csv = v;
    } else if ((v = flag_value(a, "--fps-schedule"))) {
      fps_schedule_csv = v;
    } else if ((v = flag_value(a, "--buf-sz-schedule"))) {
      buf_size_schedule_csv = v;
    } else if ((v = flag_value(a, "--buf-initial-sz-schedule"))) {
      buf_initial_schedule_csv = v;
    } else if ((v = flag_value(a, "--buf-optimal-sz-schedule"))) {
      buf_optimal_schedule_csv = v;
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
    } else if ((v = flag_value(a, "--invisible-frames"))) {
      invisible_frames_csv = v;
    } else if ((v = flag_value(a, "--control-script"))) {
      control_script_csv = v;
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
  int *target_bitrate_schedule =
      parse_int_schedule(target_bitrate_schedule_csv, frames,
                         "--target-bitrate-schedule");
  int *min_q_schedule =
      parse_int_schedule(min_q_schedule_csv, frames, "--min-q-schedule");
  int *max_q_schedule =
      parse_int_schedule(max_q_schedule_csv, frames, "--max-q-schedule");
  int *drop_frame_schedule =
      parse_int_schedule(drop_frame_schedule_csv, frames,
                         "--drop-frame-schedule");
  int *fps_schedule =
      parse_int_schedule(fps_schedule_csv, frames, "--fps-schedule");
  int *buf_size_schedule =
      parse_int_schedule(buf_size_schedule_csv, frames, "--buf-sz-schedule");
  int *buf_initial_schedule =
      parse_int_schedule(buf_initial_schedule_csv, frames,
                         "--buf-initial-sz-schedule");
  int *buf_optimal_schedule =
      parse_int_schedule(buf_optimal_schedule_csv, frames,
                         "--buf-optimal-sz-schedule");
  int invisible_frame_count = 0;
  unsigned int *invisible_frames =
      parse_uint_csv(invisible_frames_csv, &invisible_frame_count,
                     "--invisible-frames");
  int control_script_count = 0;
  char **control_script =
      parse_csv_strings(control_script_csv, &control_script_count,
                        "--control-script");

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
  cfg.rc_buf_sz = (unsigned)buffer_size_ms;
  cfg.rc_buf_initial_sz = (unsigned)buffer_initial_ms;
  cfg.rc_buf_optimal_sz = (unsigned)buffer_optimal_ms;
  cfg.rc_dropframe_thresh = (unsigned)drop_frame_water_mark;
  cfg.kf_min_dist = (unsigned)kf_min_dist;
  cfg.kf_max_dist = (unsigned)kf_max_dist;
  if (temporal_layers > 0) {
    if (temporal_layers > VPX_TS_MAX_LAYERS) {
      die_msg("--temporal-layers exceeds VPX_TS_MAX_LAYERS");
    }
    if (temporal_periodicity <= 0 ||
        temporal_periodicity > VPX_TS_MAX_PERIODICITY) {
      die_msg("--temporal-periodicity out of range");
    }
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
      if (layer_ids[i] < 0 || layer_ids[i] >= temporal_layers) {
        die_msg("--temporal-layer-ids entry out of range");
      }
      cfg.ts_layer_id[i] = (unsigned int)layer_ids[i];
    }
  }

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
  FILE *trace = NULL;
  if (trace_path) {
    trace = fopen(trace_path, "wb");
    if (!trace) {
      fprintf(stderr, "open %s for trace: %s\n", trace_path, strerror(errno));
      exit(EXIT_FAILURE);
    }
  }
  write_ivf_file_header(out, width, height, 1, 1000, frames);

  int img_width = width;
  int img_height = height;
  int base_uv_w = (width + 1) >> 1;
  int base_uv_h = (height + 1) >> 1;
  size_t base_y_size = (size_t)width * (size_t)height;
  size_t base_uv_size = (size_t)base_uv_w * (size_t)base_uv_h;
  size_t frame_size = base_y_size + 2 * base_uv_size;
  size_t plane_buf_capacity = 0;
  uint8_t *plane_buf = (uint8_t *)malloc(frame_size);
  if (!plane_buf) die_msg("alloc plane buffer");
  plane_buf_capacity = frame_size;

  vpx_codec_pts_t pts = 0;
  int frame_duration = (fps_den * 1000) / fps_num;
  if (frame_duration <= 0) frame_duration = 1;
  int bits_per_frame = (target_kbps * 1000 * fps_den) / fps_num;
  int64_t buffer_size_bits = (int64_t)target_kbps * buffer_size_ms;
  int64_t buffer_level_bits = (int64_t)target_kbps * buffer_initial_ms;
  int64_t buffer_optimal_bits = (int64_t)target_kbps * buffer_optimal_ms;
  int total_emitted = 0;
  int temporal_ref_layer[3] = {0, 0, 0};
  int temporal_tl0_pic_idx = 0;
  int temporal_tl0_valid = 0;
  for (int frame_idx = 0; frame_idx <= frames; ++frame_idx) {
    int have_input = frame_idx < frames;
    vpx_image_t *input_img = NULL;
    if (have_input) {
      int config_changed = 0;
      if (frame_idx < control_script_count) {
        apply_vp9_runtime_controls(
            &ctx, &cfg, &deadline, &target_kbps, &fps_num, &buffer_size_ms,
            &buffer_initial_ms, &buffer_optimal_ms, &drop_frame_water_mark,
            &min_q, &max_q, &cq_level, control_script[frame_idx]);
      }
      if (target_bitrate_schedule &&
          target_bitrate_schedule[frame_idx] >= 0) {
        target_kbps = target_bitrate_schedule[frame_idx];
        cfg.rc_target_bitrate = (unsigned)target_kbps;
        config_changed = 1;
      }
      if (min_q_schedule && min_q_schedule[frame_idx] >= 0) {
        min_q = min_q_schedule[frame_idx];
        cfg.rc_min_quantizer = (unsigned)min_q;
        config_changed = 1;
      }
      if (max_q_schedule && max_q_schedule[frame_idx] >= 0) {
        max_q = max_q_schedule[frame_idx];
        cfg.rc_max_quantizer = (unsigned)max_q;
        config_changed = 1;
      }
      if (drop_frame_schedule && drop_frame_schedule[frame_idx] >= 0) {
        drop_frame_water_mark = drop_frame_schedule[frame_idx];
        cfg.rc_dropframe_thresh = (unsigned)drop_frame_water_mark;
        config_changed = 1;
      }
      if (fps_schedule && fps_schedule[frame_idx] > 0) {
        fps_num = fps_schedule[frame_idx];
        config_changed = 1;
      }
      if (buf_size_schedule && buf_size_schedule[frame_idx] >= 0) {
        buffer_size_ms = buf_size_schedule[frame_idx];
        cfg.rc_buf_sz = (unsigned)buffer_size_ms;
        config_changed = 1;
      }
      if (buf_initial_schedule && buf_initial_schedule[frame_idx] >= 0) {
        buffer_initial_ms = buf_initial_schedule[frame_idx];
        cfg.rc_buf_initial_sz = (unsigned)buffer_initial_ms;
        config_changed = 1;
      }
      if (buf_optimal_schedule && buf_optimal_schedule[frame_idx] >= 0) {
        buffer_optimal_ms = buf_optimal_schedule[frame_idx];
        cfg.rc_buf_optimal_sz = (unsigned)buffer_optimal_ms;
        config_changed = 1;
      }
      if (min_q > max_q) die_msg("runtime min-q exceeds max-q");
      if (buffer_size_ms <= 0 || buffer_initial_ms < 0 ||
          buffer_optimal_ms < 0) {
        die_msg("invalid runtime buffer model");
      }
      if (config_changed && vpx_codec_enc_config_set(&ctx, &cfg)) {
        die_codec_msg(&ctx, "vpx_codec_enc_config_set");
      }
      frame_duration = (fps_den * 1000) / fps_num;
      if (frame_duration <= 0) frame_duration = 1;
      bits_per_frame = (target_kbps * 1000 * fps_den) / fps_num;
      buffer_size_bits = (int64_t)target_kbps * buffer_size_ms;
      buffer_optimal_bits = (int64_t)target_kbps * buffer_optimal_ms;
      if (buffer_level_bits > buffer_size_bits) {
        buffer_level_bits = buffer_size_bits;
      }

      int cur_width = (int)cfg.g_w;
      int cur_height = (int)cfg.g_h;
      if (cur_width <= 0 || cur_height <= 0) {
        fprintf(stderr, "invalid runtime dimensions at frame %d: %dx%d\n",
                frame_idx, cur_width, cur_height);
        exit(EXIT_FAILURE);
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
        if (!new_buf) die_msg("alloc resized plane buffer");
        plane_buf = new_buf;
        plane_buf_capacity = frame_size;
      }

      if (fread(plane_buf, 1, frame_size, in) != frame_size) {
        fprintf(stderr, "short read from %s at frame %d\n", infile_path,
                frame_idx);
        exit(EXIT_FAILURE);
      }
      uint8_t *src_y = plane_buf;
      uint8_t *src_u = plane_buf + y_size;
      uint8_t *src_v = plane_buf + y_size + uv_size;
      for (int row = 0; row < cur_height; ++row) {
        memcpy(img.planes[VPX_PLANE_Y] +
                   (ptrdiff_t)row * img.stride[VPX_PLANE_Y],
               src_y + (ptrdiff_t)row * cur_width, (size_t)cur_width);
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
    int emitted_this_input = 0;
    while ((pkt = vpx_codec_get_cx_data(&ctx, &iter))) {
      if (pkt->kind != VPX_CODEC_CX_FRAME_PKT) continue;
      write_ivf_frame_header(out, pkt->data.frame.pts, pkt->data.frame.sz);
      int make_invisible =
          have_input && frame_idx >= 0 && frame_idx < invisible_frame_count &&
          invisible_frames[frame_idx] != 0;
      const int show_frame =
          !make_invisible &&
          (pkt->data.frame.flags & VPX_FRAME_IS_INVISIBLE) == 0;
      const uint8_t *packet = (const uint8_t *)pkt->data.frame.buf;
      if (make_invisible && pkt->data.frame.sz > 0) {
        uint8_t first = vp9_packet_first_byte_with_show_frame(packet[0], 0);
        if (fwrite(&first, 1, 1, out) != 1 ||
            fwrite(packet + 1, 1, pkt->data.frame.sz - 1, out) !=
                pkt->data.frame.sz - 1) {
          die_msg("write invisible frame payload");
        }
      } else if (fwrite(packet, 1, pkt->data.frame.sz, out) !=
                 pkt->data.frame.sz) {
        die_msg("write frame payload");
      }
      emitted_this_input = 1;
      ++total_emitted;
      if (show_frame) buffer_level_bits += bits_per_frame;
      buffer_level_bits -= (int64_t)pkt->data.frame.sz * 8;
      if (buffer_level_bits > buffer_size_bits) buffer_level_bits = buffer_size_bits;
      if (trace) {
        int qindex = 0;
        if (vpx_codec_control(&ctx, VP8E_GET_LAST_QUANTIZER, &qindex)) {
          die_codec_msg(&ctx, "VP8E_GET_LAST_QUANTIZER");
        }
        int temporal_layer_id = 0;
        int temporal_layer_count = 1;
        int temporal_tl0 = 0;
        int temporal_sync = 0;
        vp9_temporal_metadata_for_frame(
            &cfg, frame_idx, frame_flags, temporal_layers,
            temporal_periodicity,
            (pkt->data.frame.flags & VPX_FRAME_IS_KEY) != 0,
            temporal_ref_layer, &temporal_tl0_pic_idx,
            &temporal_tl0_valid, &temporal_layer_id,
            &temporal_layer_count, &temporal_tl0, &temporal_sync);
        fprintf(trace,
                "{\"row\":\"vp9_frame\",\"frame_index\":%d,"
                "\"flags\":%u,\"dropped\":false,"
                "\"key_frame\":%s,\"show_frame\":%s,"
                "\"coded_width\":%u,\"coded_height\":%u,"
                "\"base_qindex\":%d,\"size_bytes\":%zu,"
                "\"size_bits\":%zu,\"target_bitrate_kbps\":%d,"
                "\"frame_target_bits\":%d,"
                "\"buffer_level_bits\":%lld,"
                "\"buffer_optimal_bits\":%lld,"
                "\"recode_allowed\":false,"
                "\"recode_loop_count\":0,"
                "\"temporal_layer_id\":%d,"
                "\"temporal_layer_count\":%d,"
                "\"tl0_pic_idx\":%d,"
                "\"temporal_layer_sync\":%s}\n",
                frame_idx, frame_flags,
                (pkt->data.frame.flags & VPX_FRAME_IS_KEY) ? "true" : "false",
                show_frame ? "true" : "false", cfg.g_w, cfg.g_h, qindex,
                pkt->data.frame.sz, pkt->data.frame.sz * 8, target_kbps,
                bits_per_frame,
                (long long)buffer_level_bits, (long long)buffer_optimal_bits,
                temporal_layer_id, temporal_layer_count, temporal_tl0,
                temporal_sync ? "true" : "false");
      }
    }
    if (trace && have_input && !emitted_this_input && lag_in_frames == 0) {
      buffer_level_bits += bits_per_frame;
      if (buffer_level_bits > buffer_size_bits) buffer_level_bits = buffer_size_bits;
      int temporal_layer_id = 0;
      int temporal_layer_count = temporal_layers > 0 ? temporal_layers : 1;
      if (temporal_layers > 0 && temporal_periodicity > 0) {
        temporal_layer_id =
            (int)cfg.ts_layer_id[frame_idx % temporal_periodicity];
      }
      fprintf(trace,
              "{\"row\":\"vp9_frame\",\"frame_index\":%d,"
              "\"flags\":%u,\"dropped\":true,"
              "\"drop_reason\":\"no_packet\",\"key_frame\":false,"
              "\"show_frame\":true,"
              "\"coded_width\":%u,\"coded_height\":%u,"
              "\"base_qindex\":0,"
              "\"size_bytes\":0,\"size_bits\":0,"
              "\"target_bitrate_kbps\":%d,"
              "\"frame_target_bits\":%d,"
              "\"buffer_level_bits\":%lld,"
              "\"buffer_optimal_bits\":%lld,"
              "\"recode_allowed\":false,"
              "\"recode_loop_count\":0,"
              "\"temporal_layer_id\":%d,"
              "\"temporal_layer_count\":%d,"
              "\"tl0_pic_idx\":0,"
              "\"temporal_layer_sync\":false}\n",
              frame_idx, frame_flags, cfg.g_w, cfg.g_h, target_kbps,
              bits_per_frame,
              (long long)buffer_level_bits, (long long)buffer_optimal_bits,
              temporal_layer_id, temporal_layer_count);
    }
  }

  free(plane_buf);
  fclose(in);
  fclose(out);
  if (trace) fclose(trace);
  if (vpx_codec_destroy(&ctx)) die_codec_msg(&ctx, "vpx_codec_destroy");
  vpx_img_free(&img);
  free(per_frame_flags);
  free(target_bitrate_schedule);
  free(min_q_schedule);
  free(max_q_schedule);
  free(drop_frame_schedule);
  free(fps_schedule);
  free(buf_size_schedule);
  free(buf_initial_schedule);
  free(buf_optimal_schedule);
  free(invisible_frames);
  free_csv_strings(control_script, control_script_count);
  if (total_emitted == 0) die_msg("no frames emitted by encoder");
  return 0;
}
