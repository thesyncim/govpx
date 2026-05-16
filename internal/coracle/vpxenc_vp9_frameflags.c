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

#include "vpx/vp8.h"
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

static int parse_tune(const char *value) {
  if (strcmp(value, "psnr") == 0) return VP8_TUNE_PSNR;
  if (strcmp(value, "ssim") == 0) return VP8_TUNE_SSIM;
  fprintf(stderr, "invalid --tune: %s\n", value);
  exit(EXIT_FAILURE);
}

static int parse_tune_content(const char *value) {
  if (strcmp(value, "default") == 0) return VP9E_CONTENT_DEFAULT;
  if (strcmp(value, "screen") == 0) return VP9E_CONTENT_SCREEN;
  if (strcmp(value, "film") == 0) return VP9E_CONTENT_FILM;
  return parse_int(value, "--tune-content");
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
  fprintf(stderr, "invalid reference frame: %s\n", value);
  exit(EXIT_FAILURE);
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

static void apply_vp9_copy_reference_token(vpx_codec_ctx_t *ctx, int width,
                                           int height, int frame_idx,
                                           const char *ref_name, FILE *log) {
  if (!log) die_msg("copyref token requires --copy-ref-log");
  vpx_image_t ref_img;
  if (!vpx_img_alloc(&ref_img, VPX_IMG_FMT_I420, (unsigned)width,
                     (unsigned)height, 1)) {
    die_msg("vpx_img_alloc copyref failed");
  }
  vpx_ref_frame_t ref;
  ref.frame_type = parse_ref_frame_type(ref_name);
  ref.img = ref_img;
  if (vpx_codec_control(ctx, VP8_COPY_REFERENCE, &ref)) {
    die_codec_msg(ctx, "VP8_COPY_REFERENCE");
  }
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

static void apply_vp9_set_reference_token(vpx_codec_ctx_t *ctx, int width,
                                          int height, const char *spec) {
  char buf[128];
  size_t len = strlen(spec);
  if (len >= sizeof(buf)) {
    fprintf(stderr, "setref token too long: %s\n", spec);
    exit(EXIT_FAILURE);
  }
  memcpy(buf, spec, len + 1);

  char *ref_name = buf;
  char *kind = strchr(ref_name, ':');
  if (!kind) {
    fprintf(stderr, "setref expects ref:pattern:index: %s\n", spec);
    exit(EXIT_FAILURE);
  }
  *kind++ = '\0';
  char *index_text = strchr(kind, ':');
  if (!index_text) {
    fprintf(stderr, "setref expects ref:pattern:index: %s\n", spec);
    exit(EXIT_FAILURE);
  }
  *index_text++ = '\0';
  if (strcmp(kind, "panning") != 0) {
    fprintf(stderr, "unsupported setref pattern: %s\n", kind);
    exit(EXIT_FAILURE);
  }
  int index = parse_int(index_text, "setref index");

  vpx_image_t ref_img;
  if (!vpx_img_alloc(&ref_img, VPX_IMG_FMT_I420, (unsigned)width,
                     (unsigned)height, 1)) {
    die_msg("vpx_img_alloc setref failed");
  }
  fill_panning_image(&ref_img, width, height, index);
  vpx_ref_frame_t ref;
  ref.frame_type = parse_ref_frame_type(ref_name);
  ref.img = ref_img;
  if (vpx_codec_control(ctx, VP8_SET_REFERENCE, &ref)) {
    die_codec_msg(ctx, "VP8_SET_REFERENCE");
  }
  vpx_img_free(&ref_img);
}

static unsigned char *alloc_vp9_active_map_pattern(const char *pattern,
                                                   int rows, int cols) {
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
        fprintf(stderr, "invalid active-map pattern: %s\n", pattern);
        exit(EXIT_FAILURE);
      }
      map[(size_t)r * (size_t)cols + (size_t)c] = v;
    }
  }
  return map;
}

static void apply_vp9_active_map(vpx_codec_ctx_t *ctx, int width, int height,
                                 const char *pattern) {
  int rows = (height + 15) >> 4;
  int cols = (width + 15) >> 4;
  if (rows <= 0 || cols <= 0) die_msg("invalid active-map dimensions");
  vpx_active_map_t active;
  active.rows = (unsigned int)rows;
  active.cols = (unsigned int)cols;
  active.active_map = NULL;
  if (strcmp(pattern, "off") != 0) {
    active.active_map = alloc_vp9_active_map_pattern(pattern, rows, cols);
  }
  if (vpx_codec_control(ctx, VP8E_SET_ACTIVEMAP, &active)) {
    die_codec_msg(ctx, "VP8E_SET_ACTIVEMAP");
  }
  free(active.active_map);
}

static int parse_slash_ints(const char *spec, int *out, int max_count,
                            const char *flag) {
  int count = 0;
  const char *start = spec;
  if (!spec || max_count <= 0) {
    fprintf(stderr, "%s expects slash-separated integers\n", flag);
    exit(EXIT_FAILURE);
  }
  for (;;) {
    if (count >= max_count) {
      fprintf(stderr, "%s has too many entries\n", flag);
      exit(EXIT_FAILURE);
    }
    const char *end = strchr(start, '/');
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
  return count;
}

static void parse_slash_ints_exact8(const char *spec, int out[8],
                                    const char *flag) {
  int count = parse_slash_ints(spec, out, 8, flag);
  if (count != 8) {
    fprintf(stderr, "%s expects exactly 8 slash-separated integers, got %d\n",
            flag, count);
    exit(EXIT_FAILURE);
  }
}

static unsigned char *alloc_vp9_roi_map_pattern(const char *pattern, int rows,
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
        v = (unsigned char)((r >= rows / 2 ? 2 : 0) +
                            (c >= cols / 2 ? 1 : 0));
      } else if (strcmp(pattern, "border1") == 0) {
        v = (r == 0 || c == 0 || r == rows - 1 || c == cols - 1) ? 1 : 0;
      } else {
        fprintf(stderr, "invalid roi-map pattern: %s\n", pattern);
        exit(EXIT_FAILURE);
      }
      map[(size_t)r * (size_t)cols + (size_t)c] = v;
    }
  }
  return map;
}

static void default_vp9_roi_params(const char *pattern, int delta_q[8],
                                   int delta_lf[8], int skip[8],
                                   int ref_frame[8]) {
  for (int i = 0; i < 8; ++i) {
    delta_q[i] = 0;
    delta_lf[i] = 0;
    skip[i] = 0;
    ref_frame[i] = -1;
  }
  if (strcmp(pattern, "checker") == 0 || strcmp(pattern, "left1") == 0) {
    delta_q[1] = -10;
    delta_lf[1] = -3;
  } else if (strcmp(pattern, "quadrants") == 0) {
    delta_q[1] = -8;
    delta_q[2] = 8;
    delta_lf[3] = 4;
  } else if (strcmp(pattern, "border1") == 0) {
    delta_q[1] = -6;
  } else if (strcmp(pattern, "off") == 0) {
    return;
  } else {
    fprintf(stderr, "invalid roi-map pattern: %s\n", pattern);
    exit(EXIT_FAILURE);
  }
}

static void apply_vp9_roi_map(vpx_codec_ctx_t *ctx, int width, int height,
                              const char *pattern, const int delta_q[8],
                              const int delta_lf[8], const int skip[8],
                              const int ref_frame[8]) {
  int rows = (height + 7) >> 3;
  int cols = (width + 7) >> 3;
  if (rows <= 0 || cols <= 0) die_msg("invalid roi-map dimensions");
  vpx_roi_map_t roi;
  memset(&roi, 0, sizeof(roi));
  roi.enabled = strcmp(pattern, "off") != 0;
  roi.rows = (unsigned int)rows;
  roi.cols = (unsigned int)cols;
  if (roi.enabled) roi.roi_map = alloc_vp9_roi_map_pattern(pattern, rows, cols);
  for (int i = 0; i < 8; ++i) {
    roi.delta_q[i] = delta_q[i];
    roi.delta_lf[i] = delta_lf[i];
    roi.skip[i] = skip[i];
    roi.ref_frame[i] = ref_frame[i];
  }
  if (vpx_codec_control(ctx, VP9E_SET_ROI_MAP, &roi)) {
    die_codec_msg(ctx, "VP9E_SET_ROI_MAP");
  }
  free(roi.roi_map);
}

static void apply_vp9_roi_token(vpx_codec_ctx_t *ctx, int width, int height,
                                const char *pattern) {
  int delta_q[8];
  int delta_lf[8];
  int skip[8];
  int ref_frame[8];
  default_vp9_roi_params(pattern, delta_q, delta_lf, skip, ref_frame);
  apply_vp9_roi_map(ctx, width, height, pattern, delta_q, delta_lf, skip,
                    ref_frame);
}

static void apply_vp9_roi_custom_token(vpx_codec_ctx_t *ctx, int width,
                                       int height, const char *spec) {
  char buf[768];
  size_t len = strlen(spec);
  if (len >= sizeof(buf)) {
    fprintf(stderr, "roicustom token too long: %s\n", spec);
    exit(EXIT_FAILURE);
  }
  memcpy(buf, spec, len + 1);

  char *pattern = buf;
  char *dq_text = strchr(pattern, ':');
  if (!dq_text) {
    fprintf(stderr, "roicustom expects pattern:dq8:dlf8:skip8:ref8: %s\n",
            spec);
    exit(EXIT_FAILURE);
  }
  *dq_text++ = '\0';
  char *dlf_text = strchr(dq_text, ':');
  if (!dlf_text) {
    fprintf(stderr, "roicustom expects pattern:dq8:dlf8:skip8:ref8: %s\n",
            spec);
    exit(EXIT_FAILURE);
  }
  *dlf_text++ = '\0';
  char *skip_text = strchr(dlf_text, ':');
  if (!skip_text) {
    fprintf(stderr, "roicustom expects pattern:dq8:dlf8:skip8:ref8: %s\n",
            spec);
    exit(EXIT_FAILURE);
  }
  *skip_text++ = '\0';
  char *ref_text = strchr(skip_text, ':');
  if (!ref_text) {
    fprintf(stderr, "roicustom expects pattern:dq8:dlf8:skip8:ref8: %s\n",
            spec);
    exit(EXIT_FAILURE);
  }
  *ref_text++ = '\0';
  if (strchr(ref_text, ':')) {
    fprintf(stderr, "roicustom has too many fields: %s\n", spec);
    exit(EXIT_FAILURE);
  }

  int delta_q[8];
  int delta_lf[8];
  int skip[8];
  int ref_frame[8];
  parse_slash_ints_exact8(dq_text, delta_q, "roicustom dq");
  parse_slash_ints_exact8(dlf_text, delta_lf, "roicustom dlf");
  parse_slash_ints_exact8(skip_text, skip, "roicustom skip");
  parse_slash_ints_exact8(ref_text, ref_frame, "roicustom ref");
  apply_vp9_roi_map(ctx, width, height, pattern, delta_q, delta_lf, skip,
                    ref_frame);
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
  int *undershoot_pct;
  int *overshoot_pct;
  int *min_q;
  int *max_q;
  int *cq_level;
  int frame_idx;
  FILE *copy_ref_log;
  int config_changed;
};

static void flush_vp9_runtime_config(
    struct vp9_runtime_control_context *ctx) {
  if (ctx->config_changed && vpx_codec_enc_config_set(ctx->ctx, ctx->cfg)) {
    die_codec_msg(ctx->ctx, "runtime vpx_codec_enc_config_set");
  }
  ctx->config_changed = 0;
}

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
  } else if (starts_with(token, "undershoot:")) {
    *ctx->undershoot_pct = control_value_int(token, "undershoot:");
    ctx->cfg->rc_undershoot_pct = (unsigned)*ctx->undershoot_pct;
    ctx->config_changed = 1;
  } else if (starts_with(token, "overshoot:")) {
    *ctx->overshoot_pct = control_value_int(token, "overshoot:");
    ctx->cfg->rc_overshoot_pct = (unsigned)*ctx->overshoot_pct;
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
  } else if (starts_with(token, "kfmax:")) {
    ctx->cfg->kf_max_dist = (unsigned)control_value_int(token, "kfmax:");
    ctx->config_changed = 1;
  } else if (starts_with(token, "deadline:")) {
    *ctx->deadline = parse_deadline(token + strlen("deadline:"));
  } else if (starts_with(token, "cpu:")) {
    if (vpx_codec_control(ctx->ctx, VP8E_SET_CPUUSED,
                          control_value_int(token, "cpu:"))) {
      die_codec_msg(ctx->ctx, "runtime VP8E_SET_CPUUSED");
    }
  } else if (starts_with(token, "tune:")) {
    if (vpx_codec_control(ctx->ctx, VP8E_SET_TUNING,
                          parse_tune(token + strlen("tune:")))) {
      die_codec_msg(ctx->ctx, "runtime VP8E_SET_TUNING");
    }
  } else if (starts_with(token, "screen:")) {
    if (vpx_codec_control(ctx->ctx, VP9E_SET_TUNE_CONTENT,
                          control_value_int(token, "screen:"))) {
      die_codec_msg(ctx->ctx, "runtime VP9E_SET_TUNE_CONTENT");
    }
  } else if (starts_with(token, "cq:")) {
    *ctx->cq_level = control_value_int(token, "cq:");
    if (vpx_codec_control(ctx->ctx, VP8E_SET_CQ_LEVEL,
                          (unsigned)*ctx->cq_level)) {
      die_codec_msg(ctx->ctx, "runtime VP8E_SET_CQ_LEVEL");
    }
  } else if (starts_with(token, "noise:")) {
    if (vpx_codec_control(ctx->ctx, VP9E_SET_NOISE_SENSITIVITY,
                          (unsigned)control_value_int(token, "noise:"))) {
      die_codec_msg(ctx->ctx, "runtime VP9E_SET_NOISE_SENSITIVITY");
    }
  } else if (starts_with(token, "sharpness:")) {
    if (vpx_codec_control(ctx->ctx, VP8E_SET_SHARPNESS,
                          control_value_int(token, "sharpness:"))) {
      die_codec_msg(ctx->ctx, "runtime VP8E_SET_SHARPNESS");
    }
  } else if (starts_with(token, "static:")) {
    if (vpx_codec_control(ctx->ctx, VP8E_SET_STATIC_THRESHOLD,
                          (unsigned)control_value_int(token, "static:"))) {
      die_codec_msg(ctx->ctx, "runtime VP8E_SET_STATIC_THRESHOLD");
    }
  } else if (starts_with(token, "maxintra:")) {
    if (vpx_codec_control(ctx->ctx, VP8E_SET_MAX_INTRA_BITRATE_PCT,
                          (unsigned)control_value_int(token, "maxintra:"))) {
      die_codec_msg(ctx->ctx, "runtime VP8E_SET_MAX_INTRA_BITRATE_PCT");
    }
  } else if (starts_with(token, "gfboost:")) {
    if (vpx_codec_control(ctx->ctx, VP8E_SET_GF_CBR_BOOST_PCT,
                          (unsigned)control_value_int(token, "gfboost:"))) {
      die_codec_msg(ctx->ctx, "runtime VP8E_SET_GF_CBR_BOOST_PCT");
    }
  } else if (starts_with(token, "frame-parallel:")) {
    if (vpx_codec_control(ctx->ctx, VP9E_SET_FRAME_PARALLEL_DECODING,
                          (unsigned)control_value_int(token,
                                                      "frame-parallel:"))) {
      die_codec_msg(ctx->ctx, "runtime VP9E_SET_FRAME_PARALLEL_DECODING");
    }
  } else if (starts_with(token, "rtc:")) {
    if (vpx_codec_control(ctx->ctx, VP9E_SET_RTC_EXTERNAL_RATECTRL,
                          (unsigned)control_value_int(token, "rtc:"))) {
      die_codec_msg(ctx->ctx, "runtime VP9E_SET_RTC_EXTERNAL_RATECTRL");
    }
  } else if (starts_with(token, "deltaquv:")) {
    if (vpx_codec_control(ctx->ctx, VP9E_SET_DELTA_Q_UV,
                          control_value_int(token, "deltaquv:"))) {
      die_codec_msg(ctx->ctx, "runtime VP9E_SET_DELTA_Q_UV");
    }
  } else if (starts_with(token, "colorspace:")) {
    if (vpx_codec_control(ctx->ctx, VP9E_SET_COLOR_SPACE,
                          (unsigned)control_value_int(token, "colorspace:"))) {
      die_codec_msg(ctx->ctx, "runtime VP9E_SET_COLOR_SPACE");
    }
  } else if (starts_with(token, "colorrange:")) {
    if (vpx_codec_control(ctx->ctx, VP9E_SET_COLOR_RANGE,
                          (unsigned)control_value_int(token, "colorrange:"))) {
      die_codec_msg(ctx->ctx, "runtime VP9E_SET_COLOR_RANGE");
    }
  } else if (starts_with(token, "rendersize:")) {
    int dims[2] = {0, 0};
    if (parse_slash_ints(token + strlen("rendersize:"), dims, 2, "rendersize") !=
        2) {
      die_msg("rendersize: token must be width/height");
    }
    if (vpx_codec_control(ctx->ctx, VP9E_SET_RENDER_SIZE, dims)) {
      die_codec_msg(ctx->ctx, "runtime VP9E_SET_RENDER_SIZE");
    }
  } else if (starts_with(token, "targetlevel:")) {
    if (vpx_codec_control(ctx->ctx, VP9E_SET_TARGET_LEVEL,
                          (unsigned)control_value_int(token,
                                                      "targetlevel:"))) {
      die_codec_msg(ctx->ctx, "runtime VP9E_SET_TARGET_LEVEL");
    }
  } else if (starts_with(token, "disableloopfilter:")) {
    if (vpx_codec_control(ctx->ctx, VP9E_SET_DISABLE_LOOPFILTER,
                          (unsigned)control_value_int(token,
                                                      "disableloopfilter:"))) {
      die_codec_msg(ctx->ctx, "runtime VP9E_SET_DISABLE_LOOPFILTER");
    }
  } else if (starts_with(token, "maxinter:")) {
    if (vpx_codec_control(ctx->ctx, VP9E_SET_MAX_INTER_BITRATE_PCT,
                          (unsigned)control_value_int(token, "maxinter:"))) {
      die_codec_msg(ctx->ctx, "runtime VP9E_SET_MAX_INTER_BITRATE_PCT");
    }
  } else if (starts_with(token, "mingf:")) {
    if (vpx_codec_control(ctx->ctx, VP9E_SET_MIN_GF_INTERVAL,
                          (unsigned)control_value_int(token, "mingf:"))) {
      die_codec_msg(ctx->ctx, "runtime VP9E_SET_MIN_GF_INTERVAL");
    }
  } else if (starts_with(token, "maxgf:")) {
    if (vpx_codec_control(ctx->ctx, VP9E_SET_MAX_GF_INTERVAL,
                          (unsigned)control_value_int(token, "maxgf:"))) {
      die_codec_msg(ctx->ctx, "runtime VP9E_SET_MAX_GF_INTERVAL");
    }
  } else if (starts_with(token, "periodicboost:")) {
    if (vpx_codec_control(ctx->ctx, VP9E_SET_FRAME_PERIODIC_BOOST,
                          (unsigned)control_value_int(token,
                                                      "periodicboost:"))) {
      die_codec_msg(ctx->ctx, "runtime VP9E_SET_FRAME_PERIODIC_BOOST");
    }
  } else if (starts_with(token, "altrefaq:")) {
    if (vpx_codec_control(ctx->ctx, VP9E_SET_ALT_REF_AQ,
                          control_value_int(token, "altrefaq:"))) {
      die_codec_msg(ctx->ctx, "runtime VP9E_SET_ALT_REF_AQ");
    }
  } else if (starts_with(token, "postdrop:")) {
    if (vpx_codec_control(ctx->ctx, VP9E_SET_POSTENCODE_DROP,
                          (unsigned)control_value_int(token, "postdrop:"))) {
      die_codec_msg(ctx->ctx, "runtime VP9E_SET_POSTENCODE_DROP");
    }
  } else if (starts_with(token, "disovershoot:")) {
    if (vpx_codec_control(ctx->ctx, VP9E_SET_DISABLE_OVERSHOOT_MAXQ_CBR,
                          (unsigned)control_value_int(token,
                                                      "disovershoot:"))) {
      die_codec_msg(ctx->ctx,
                    "runtime VP9E_SET_DISABLE_OVERSHOOT_MAXQ_CBR");
    }
  } else if (starts_with(token, "qonepass:")) {
    if (vpx_codec_control(ctx->ctx, VP9E_SET_QUANTIZER_ONE_PASS,
                          control_value_int(token, "qonepass:"))) {
      die_codec_msg(ctx->ctx, "runtime VP9E_SET_QUANTIZER_ONE_PASS");
    }
  } else if (starts_with(token, "active:")) {
    flush_vp9_runtime_config(ctx);
    apply_vp9_active_map(ctx->ctx, (int)ctx->cfg->g_w, (int)ctx->cfg->g_h,
                         token + strlen("active:"));
  } else if (starts_with(token, "roi:")) {
    flush_vp9_runtime_config(ctx);
    apply_vp9_roi_token(ctx->ctx, (int)ctx->cfg->g_w, (int)ctx->cfg->g_h,
                        token + strlen("roi:"));
  } else if (starts_with(token, "roicustom:")) {
    flush_vp9_runtime_config(ctx);
    apply_vp9_roi_custom_token(ctx->ctx, (int)ctx->cfg->g_w,
                               (int)ctx->cfg->g_h,
                               token + strlen("roicustom:"));
  } else if (starts_with(token, "setref:")) {
    flush_vp9_runtime_config(ctx);
    apply_vp9_set_reference_token(ctx->ctx, (int)ctx->cfg->g_w,
                                  (int)ctx->cfg->g_h,
                                  token + strlen("setref:"));
  } else if (starts_with(token, "copyref:")) {
    flush_vp9_runtime_config(ctx);
    apply_vp9_copy_reference_token(ctx->ctx, (int)ctx->cfg->g_w,
                                   (int)ctx->cfg->g_h, ctx->frame_idx,
                                   token + strlen("copyref:"),
                                   ctx->copy_ref_log);
  } else if (starts_with(token, "autoaltref:")) {
    if (vpx_codec_control(ctx->ctx, VP8E_SET_ENABLEAUTOALTREF,
                          (unsigned)control_value_int(token, "autoaltref:"))) {
      die_codec_msg(ctx->ctx, "runtime VP8E_SET_ENABLEAUTOALTREF");
    }
  } else if (starts_with(token, "arnrmax:")) {
    if (vpx_codec_control(ctx->ctx, VP8E_SET_ARNR_MAXFRAMES,
                          (unsigned)control_value_int(token, "arnrmax:"))) {
      die_codec_msg(ctx->ctx, "runtime VP8E_SET_ARNR_MAXFRAMES");
    }
  } else if (starts_with(token, "arnrstrength:")) {
    if (vpx_codec_control(ctx->ctx, VP8E_SET_ARNR_STRENGTH,
                          (unsigned)control_value_int(token, "arnrstrength:"))) {
      die_codec_msg(ctx->ctx, "runtime VP8E_SET_ARNR_STRENGTH");
    }
  } else if (starts_with(token, "arnrtype:")) {
    if (vpx_codec_control(ctx->ctx, VP8E_SET_ARNR_TYPE,
                          (unsigned)control_value_int(token, "arnrtype:"))) {
      die_codec_msg(ctx->ctx, "runtime VP8E_SET_ARNR_TYPE");
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
    int *drop_frame_water_mark, int *undershoot_pct, int *overshoot_pct,
    int *min_q, int *max_q, int *cq_level, int frame_idx,
    FILE *copy_ref_log, const char *entry) {
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
      buffer_initial_ms, buffer_optimal_ms, drop_frame_water_mark,
      undershoot_pct, overshoot_pct, min_q, max_q, cq_level, frame_idx,
      copy_ref_log, 0};
  char *start = buf;
  for (;;) {
    char *end = strchr(start, '+');
    if (end) *end = '\0';
    apply_vp9_runtime_control_token(&ctx, start);
    if (!end) break;
    start = end + 1;
  }
  flush_vp9_runtime_config(&ctx);
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
  const char *copy_ref_log_path = NULL;
  int width = 0, height = 0, frames = 0;
  int fps_num = 30, fps_den = 1;
  int target_kbps = 700;
  int buffer_size_ms = 6000;
  int buffer_initial_ms = 4000;
  int buffer_optimal_ms = 5000;
  int drop_frame_water_mark = 0;
  int undershoot_pct = -1;
  int overshoot_pct = -1;
  int min_q = 4, max_q = 56, cq_level = 32;
  int kf_min_dist = 0, kf_max_dist = 128;
  int deadline = (int)VPX_DL_REALTIME;
  int cpu_used = 8;
  int tune = VP8_TUNE_PSNR;
  int tune_content = VP9E_CONTENT_DEFAULT;
  int noise_sensitivity = 0;
  int sharpness = 0;
  int static_thresh = 0;
  int error_resilient = 0;
  int lag_in_frames = 0;
  int auto_alt_ref = 0;
  int arnr_maxframes = 0, arnr_maxframes_set = 0;
  int arnr_strength = 0, arnr_strength_set = 0;
  int arnr_type = 0, arnr_type_set = 0;
  int aq_mode = 0;
  int row_mt = 0;
  int tile_columns = 0;
  int tile_rows = 0;
  int lossless = 0;
  int temporal_layers = 0;
  int temporal_periodicity = 0;
  int max_intra_rate = -1;
  int gf_cbr_boost = -1;
  int max_bitrate_kbps = -1;
  int min_bitrate_kbps = -1;
  int frame_parallel = -1;
  int min_gf_interval = -1;
  int max_gf_interval = -1;
  int frame_periodic_boost = -1;
  int alt_ref_aq = -1;
  int postencode_drop = -1;
  int disable_overshoot_maxq_cbr = -1;
  int color_space = -1;
  int color_range = -1;
  int render_width = 0;
  int render_height = 0;
  int target_level = -1;
  int disable_loopfilter = -1;
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
    } else if ((v = flag_value(a, "--undershoot-pct"))) {
      undershoot_pct = parse_int(v, "--undershoot-pct");
    } else if ((v = flag_value(a, "--overshoot-pct"))) {
      overshoot_pct = parse_int(v, "--overshoot-pct");
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
    } else if ((v = flag_value(a, "--tune"))) {
      tune = parse_tune(v);
    } else if ((v = flag_value(a, "--tune-content"))) {
      tune_content = parse_tune_content(v);
    } else if ((v = flag_value(a, "--noise-sensitivity"))) {
      noise_sensitivity = parse_int(v, "--noise-sensitivity");
    } else if ((v = flag_value(a, "--sharpness"))) {
      sharpness = parse_int(v, "--sharpness");
    } else if ((v = flag_value(a, "--static-thresh"))) {
      static_thresh = parse_int(v, "--static-thresh");
    } else if ((v = flag_value(a, "--max-intra-rate"))) {
      max_intra_rate = parse_int(v, "--max-intra-rate");
    } else if ((v = flag_value(a, "--gf-cbr-boost"))) {
      gf_cbr_boost = parse_int(v, "--gf-cbr-boost");
    } else if ((v = flag_value(a, "--max-bitrate"))) {
      max_bitrate_kbps = parse_int(v, "--max-bitrate");
    } else if ((v = flag_value(a, "--min-bitrate"))) {
      min_bitrate_kbps = parse_int(v, "--min-bitrate");
    } else if ((v = flag_value(a, "--frame-parallel"))) {
      frame_parallel = parse_int(v, "--frame-parallel");
    } else if ((v = flag_value(a, "--min-gf-interval"))) {
      min_gf_interval = parse_int(v, "--min-gf-interval");
    } else if ((v = flag_value(a, "--max-gf-interval"))) {
      max_gf_interval = parse_int(v, "--max-gf-interval");
    } else if ((v = flag_value(a, "--frame-boost"))) {
      frame_periodic_boost = parse_int(v, "--frame-boost");
    } else if ((v = flag_value(a, "--alt-ref-aq"))) {
      alt_ref_aq = parse_int(v, "--alt-ref-aq");
    } else if ((v = flag_value(a, "--postencode-drop"))) {
      postencode_drop = parse_int(v, "--postencode-drop");
    } else if ((v = flag_value(a, "--disable-overshoot-maxq-cbr"))) {
      disable_overshoot_maxq_cbr =
          parse_int(v, "--disable-overshoot-maxq-cbr");
    } else if ((v = flag_value(a, "--color-space"))) {
      color_space = parse_int(v, "--color-space");
    } else if ((v = flag_value(a, "--color-range"))) {
      color_range = parse_int(v, "--color-range");
    } else if ((v = flag_value(a, "--render-width"))) {
      render_width = parse_int(v, "--render-width");
    } else if ((v = flag_value(a, "--render-height"))) {
      render_height = parse_int(v, "--render-height");
    } else if ((v = flag_value(a, "--target-level"))) {
      target_level = parse_int(v, "--target-level");
    } else if ((v = flag_value(a, "--disable-loopfilter"))) {
      disable_loopfilter = parse_int(v, "--disable-loopfilter");
    } else if ((v = flag_value(a, "--error-resilient"))) {
      error_resilient = parse_int(v, "--error-resilient");
    } else if ((v = flag_value(a, "--lag-in-frames"))) {
      lag_in_frames = parse_int(v, "--lag-in-frames");
    } else if ((v = flag_value(a, "--auto-alt-ref"))) {
      auto_alt_ref = parse_int(v, "--auto-alt-ref");
    } else if ((v = flag_value(a, "--arnr-maxframes"))) {
      arnr_maxframes = parse_int(v, "--arnr-maxframes");
      arnr_maxframes_set = 1;
    } else if ((v = flag_value(a, "--arnr-strength"))) {
      arnr_strength = parse_int(v, "--arnr-strength");
      arnr_strength_set = 1;
    } else if ((v = flag_value(a, "--arnr-type"))) {
      arnr_type = parse_int(v, "--arnr-type");
      arnr_type_set = 1;
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
    } else if ((v = flag_value(a, "--copy-ref-log"))) {
      copy_ref_log_path = v;
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
  if (min_bitrate_kbps >= 0) {
    cfg.rc_2pass_vbr_minsection_pct =
        cfg.rc_target_bitrate > 0
            ? (unsigned)((100u * (unsigned)min_bitrate_kbps) /
                         cfg.rc_target_bitrate)
            : 0u;
  }
  if (max_bitrate_kbps >= 0) {
    cfg.rc_2pass_vbr_maxsection_pct =
        cfg.rc_target_bitrate > 0
            ? (unsigned)((100u * (unsigned)max_bitrate_kbps) /
                         cfg.rc_target_bitrate)
            : 0u;
  }
  cfg.rc_min_quantizer = (unsigned)min_q;
  cfg.rc_max_quantizer = (unsigned)max_q;
  cfg.rc_buf_sz = (unsigned)buffer_size_ms;
  cfg.rc_buf_initial_sz = (unsigned)buffer_initial_ms;
  cfg.rc_buf_optimal_sz = (unsigned)buffer_optimal_ms;
  cfg.rc_dropframe_thresh = (unsigned)drop_frame_water_mark;
  if (undershoot_pct >= 0) {
    cfg.rc_undershoot_pct = (unsigned)undershoot_pct;
  }
  if (overshoot_pct >= 0) {
    cfg.rc_overshoot_pct = (unsigned)overshoot_pct;
  }
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
  if (vpx_codec_control(&ctx, VP8E_SET_TUNING, tune))
    die_codec_msg(&ctx, "VP8E_SET_TUNING");
  if (vpx_codec_control(&ctx, VP9E_SET_TUNE_CONTENT, tune_content))
    die_codec_msg(&ctx, "VP9E_SET_TUNE_CONTENT");
  if (vpx_codec_control(&ctx, VP9E_SET_NOISE_SENSITIVITY,
                        (unsigned)noise_sensitivity))
    die_codec_msg(&ctx, "VP9E_SET_NOISE_SENSITIVITY");
  if (vpx_codec_control(&ctx, VP8E_SET_SHARPNESS, sharpness))
    die_codec_msg(&ctx, "VP8E_SET_SHARPNESS");
  if (vpx_codec_control(&ctx, VP8E_SET_STATIC_THRESHOLD,
                        (unsigned)static_thresh))
    die_codec_msg(&ctx, "VP8E_SET_STATIC_THRESHOLD");
  if (vpx_codec_control(&ctx, VP8E_SET_ENABLEAUTOALTREF,
                        (unsigned)auto_alt_ref))
    die_codec_msg(&ctx, "VP8E_SET_ENABLEAUTOALTREF");
  if (arnr_maxframes_set &&
      vpx_codec_control(&ctx, VP8E_SET_ARNR_MAXFRAMES,
                        (unsigned)arnr_maxframes))
    die_codec_msg(&ctx, "VP8E_SET_ARNR_MAXFRAMES");
  if (arnr_strength_set &&
      vpx_codec_control(&ctx, VP8E_SET_ARNR_STRENGTH,
                        (unsigned)arnr_strength))
    die_codec_msg(&ctx, "VP8E_SET_ARNR_STRENGTH");
  if (arnr_type_set &&
      vpx_codec_control(&ctx, VP8E_SET_ARNR_TYPE, (unsigned)arnr_type))
    die_codec_msg(&ctx, "VP8E_SET_ARNR_TYPE");
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
  if (max_intra_rate >= 0 &&
      vpx_codec_control(&ctx, VP8E_SET_MAX_INTRA_BITRATE_PCT,
                        (unsigned)max_intra_rate))
    die_codec_msg(&ctx, "VP8E_SET_MAX_INTRA_BITRATE_PCT");
  if (gf_cbr_boost >= 0 &&
      vpx_codec_control(&ctx, VP8E_SET_GF_CBR_BOOST_PCT,
                        (unsigned)gf_cbr_boost))
    die_codec_msg(&ctx, "VP8E_SET_GF_CBR_BOOST_PCT");
  if (frame_parallel >= 0 &&
      vpx_codec_control(&ctx, VP9E_SET_FRAME_PARALLEL_DECODING,
                        (unsigned)frame_parallel))
    die_codec_msg(&ctx, "VP9E_SET_FRAME_PARALLEL_DECODING");
  if (min_gf_interval >= 0 &&
      vpx_codec_control(&ctx, VP9E_SET_MIN_GF_INTERVAL,
                        (unsigned)min_gf_interval))
    die_codec_msg(&ctx, "VP9E_SET_MIN_GF_INTERVAL");
  if (max_gf_interval >= 0 &&
      vpx_codec_control(&ctx, VP9E_SET_MAX_GF_INTERVAL,
                        (unsigned)max_gf_interval))
    die_codec_msg(&ctx, "VP9E_SET_MAX_GF_INTERVAL");
  if (frame_periodic_boost >= 0 &&
      vpx_codec_control(&ctx, VP9E_SET_FRAME_PERIODIC_BOOST,
                        (unsigned)frame_periodic_boost))
    die_codec_msg(&ctx, "VP9E_SET_FRAME_PERIODIC_BOOST");
  if (alt_ref_aq >= 0 &&
      vpx_codec_control(&ctx, VP9E_SET_ALT_REF_AQ, alt_ref_aq))
    die_codec_msg(&ctx, "VP9E_SET_ALT_REF_AQ");
  if (postencode_drop >= 0 &&
      vpx_codec_control(&ctx, VP9E_SET_POSTENCODE_DROP,
                        (unsigned)postencode_drop))
    die_codec_msg(&ctx, "VP9E_SET_POSTENCODE_DROP");
  if (disable_overshoot_maxq_cbr >= 0 &&
      vpx_codec_control(&ctx, VP9E_SET_DISABLE_OVERSHOOT_MAXQ_CBR,
                        (unsigned)disable_overshoot_maxq_cbr))
    die_codec_msg(&ctx, "VP9E_SET_DISABLE_OVERSHOOT_MAXQ_CBR");
  if (color_space >= 0 &&
      vpx_codec_control(&ctx, VP9E_SET_COLOR_SPACE, (unsigned)color_space))
    die_codec_msg(&ctx, "VP9E_SET_COLOR_SPACE");
  if (color_range >= 0 &&
      vpx_codec_control(&ctx, VP9E_SET_COLOR_RANGE, (unsigned)color_range))
    die_codec_msg(&ctx, "VP9E_SET_COLOR_RANGE");
  if (render_width > 0 && render_height > 0) {
    int dims[2] = {render_width, render_height};
    if (vpx_codec_control(&ctx, VP9E_SET_RENDER_SIZE, dims))
      die_codec_msg(&ctx, "VP9E_SET_RENDER_SIZE");
  }
  if (target_level >= 0 &&
      vpx_codec_control(&ctx, VP9E_SET_TARGET_LEVEL, (unsigned)target_level))
    die_codec_msg(&ctx, "VP9E_SET_TARGET_LEVEL");
  if (disable_loopfilter >= 0 &&
      vpx_codec_control(&ctx, VP9E_SET_DISABLE_LOOPFILTER,
                        (unsigned)disable_loopfilter))
    die_codec_msg(&ctx, "VP9E_SET_DISABLE_LOOPFILTER");

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
  FILE *copy_ref_log = NULL;
  if (copy_ref_log_path) {
    copy_ref_log = fopen(copy_ref_log_path, "wb");
    if (!copy_ref_log) {
      fprintf(stderr, "open %s for copy-ref log: %s\n", copy_ref_log_path,
              strerror(errno));
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
            &undershoot_pct, &overshoot_pct, &min_q, &max_q, &cq_level,
            frame_idx, copy_ref_log, control_script[frame_idx]);
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
  if (copy_ref_log) fclose(copy_ref_log);
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
