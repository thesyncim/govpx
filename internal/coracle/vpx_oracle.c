#include <errno.h>
#include <inttypes.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "vpx/vp8dx.h"
#include "vpx/vpx_decoder.h"

typedef struct {
	uint32_t state[4];
	uint64_t length;
	unsigned char buffer[64];
	size_t buffered;
} md5_ctx;

static uint32_t load_le32(const unsigned char *p) {
	return ((uint32_t)p[0]) | ((uint32_t)p[1] << 8) | ((uint32_t)p[2] << 16) | ((uint32_t)p[3] << 24);
}

static uint16_t load_le16(const unsigned char *p) {
	return (uint16_t)(((uint16_t)p[0]) | ((uint16_t)p[1] << 8));
}

static uint64_t load_le64(const unsigned char *p) {
	return ((uint64_t)load_le32(p)) | ((uint64_t)load_le32(p + 4) << 32);
}

static void store_le32(unsigned char *p, uint32_t v) {
	p[0] = (unsigned char)(v);
	p[1] = (unsigned char)(v >> 8);
	p[2] = (unsigned char)(v >> 16);
	p[3] = (unsigned char)(v >> 24);
}

static void store_le64(unsigned char *p, uint64_t v) {
	store_le32(p, (uint32_t)v);
	store_le32(p + 4, (uint32_t)(v >> 32));
}

static uint32_t rotate_left(uint32_t x, uint32_t n) {
	return (x << n) | (x >> (32 - n));
}

static void md5_init(md5_ctx *ctx) {
	ctx->state[0] = 0x67452301u;
	ctx->state[1] = 0xefcdab89u;
	ctx->state[2] = 0x98badcfeu;
	ctx->state[3] = 0x10325476u;
	ctx->length = 0;
	ctx->buffered = 0;
}

static void md5_transform(md5_ctx *ctx, const unsigned char block[64]) {
	static const uint32_t s[64] = {
		7, 12, 17, 22, 7, 12, 17, 22, 7, 12, 17, 22, 7, 12, 17, 22,
		5, 9, 14, 20, 5, 9, 14, 20, 5, 9, 14, 20, 5, 9, 14, 20,
		4, 11, 16, 23, 4, 11, 16, 23, 4, 11, 16, 23, 4, 11, 16, 23,
		6, 10, 15, 21, 6, 10, 15, 21, 6, 10, 15, 21, 6, 10, 15, 21,
	};
	static const uint32_t k[64] = {
		0xd76aa478u, 0xe8c7b756u, 0x242070dbu, 0xc1bdceeeu,
		0xf57c0fafu, 0x4787c62au, 0xa8304613u, 0xfd469501u,
		0x698098d8u, 0x8b44f7afu, 0xffff5bb1u, 0x895cd7beu,
		0x6b901122u, 0xfd987193u, 0xa679438eu, 0x49b40821u,
		0xf61e2562u, 0xc040b340u, 0x265e5a51u, 0xe9b6c7aau,
		0xd62f105du, 0x02441453u, 0xd8a1e681u, 0xe7d3fbc8u,
		0x21e1cde6u, 0xc33707d6u, 0xf4d50d87u, 0x455a14edu,
		0xa9e3e905u, 0xfcefa3f8u, 0x676f02d9u, 0x8d2a4c8au,
		0xfffa3942u, 0x8771f681u, 0x6d9d6122u, 0xfde5380cu,
		0xa4beea44u, 0x4bdecfa9u, 0xf6bb4b60u, 0xbebfbc70u,
		0x289b7ec6u, 0xeaa127fau, 0xd4ef3085u, 0x04881d05u,
		0xd9d4d039u, 0xe6db99e5u, 0x1fa27cf8u, 0xc4ac5665u,
		0xf4292244u, 0x432aff97u, 0xab9423a7u, 0xfc93a039u,
		0x655b59c3u, 0x8f0ccc92u, 0xffeff47du, 0x85845dd1u,
		0x6fa87e4fu, 0xfe2ce6e0u, 0xa3014314u, 0x4e0811a1u,
		0xf7537e82u, 0xbd3af235u, 0x2ad7d2bbu, 0xeb86d391u,
	};
	uint32_t m[16];
	uint32_t a = ctx->state[0];
	uint32_t b = ctx->state[1];
	uint32_t c = ctx->state[2];
	uint32_t d = ctx->state[3];

	for (int i = 0; i < 16; i++) {
		m[i] = load_le32(block + i * 4);
	}
	for (int i = 0; i < 64; i++) {
		uint32_t f;
		uint32_t g;
		if (i < 16) {
			f = (b & c) | (~b & d);
			g = (uint32_t)i;
		} else if (i < 32) {
			f = (d & b) | (~d & c);
			g = (uint32_t)((5 * i + 1) & 15);
		} else if (i < 48) {
			f = b ^ c ^ d;
			g = (uint32_t)((3 * i + 5) & 15);
		} else {
			f = c ^ (b | ~d);
			g = (uint32_t)((7 * i) & 15);
		}
		uint32_t next = d;
		d = c;
		c = b;
		b = b + rotate_left(a + f + k[i] + m[g], s[i]);
		a = next;
	}

	ctx->state[0] += a;
	ctx->state[1] += b;
	ctx->state[2] += c;
	ctx->state[3] += d;
}

static void md5_update(md5_ctx *ctx, const unsigned char *data, size_t len) {
	ctx->length += (uint64_t)len;
	if (ctx->buffered > 0) {
		size_t need = 64 - ctx->buffered;
		if (need > len) {
			need = len;
		}
		memcpy(ctx->buffer + ctx->buffered, data, need);
		ctx->buffered += need;
		data += need;
		len -= need;
		if (ctx->buffered == 64) {
			md5_transform(ctx, ctx->buffer);
			ctx->buffered = 0;
		}
	}
	while (len >= 64) {
		md5_transform(ctx, data);
		data += 64;
		len -= 64;
	}
	if (len > 0) {
		memcpy(ctx->buffer, data, len);
		ctx->buffered = len;
	}
}

static void md5_final(md5_ctx *ctx, unsigned char out[16]) {
	unsigned char pad[64] = {0x80};
	unsigned char lenbuf[8];
	uint64_t bit_length = ctx->length * 8;
	size_t pad_len = ctx->buffered < 56 ? 56 - ctx->buffered : 120 - ctx->buffered;

	store_le64(lenbuf, bit_length);
	md5_update(ctx, pad, pad_len);
	md5_update(ctx, lenbuf, sizeof(lenbuf));
	for (int i = 0; i < 4; i++) {
		store_le32(out + i * 4, ctx->state[i]);
	}
}

static int update_plane(md5_ctx *ctx, const unsigned char *plane, int stride, unsigned int width, unsigned int height) {
	if (stride < 0 || (unsigned int)stride < width) {
		return -1;
	}
	for (unsigned int row = 0; row < height; row++) {
		md5_update(ctx, plane + row * (size_t)stride, width);
	}
	return 0;
}

static void hex_encode(const unsigned char sum[16], char out[33]) {
	static const char hex[] = "0123456789abcdef";
	for (int i = 0; i < 16; i++) {
		out[i * 2] = hex[sum[i] >> 4];
		out[i * 2 + 1] = hex[sum[i] & 15];
	}
	out[32] = '\0';
}

static int plane_md5_hex(const unsigned char *plane, int stride, unsigned int width, unsigned int height, char out[33]) {
	md5_ctx ctx;
	unsigned char sum[16];
	md5_init(&ctx);
	if (update_plane(&ctx, plane, stride, width, height) != 0) {
		return -1;
	}
	md5_final(&ctx, sum);
	hex_encode(sum, out);
	return 0;
}

static int full_md5_hex(const vpx_image_t *img, char out[33]) {
	md5_ctx ctx;
	unsigned char sum[16];
	unsigned int uv_width = (img->d_w + 1) >> 1;
	unsigned int uv_height = (img->d_h + 1) >> 1;
	md5_init(&ctx);
	if (update_plane(&ctx, img->planes[VPX_PLANE_Y], img->stride[VPX_PLANE_Y], img->d_w, img->d_h) != 0 ||
	    update_plane(&ctx, img->planes[VPX_PLANE_U], img->stride[VPX_PLANE_U], uv_width, uv_height) != 0 ||
	    update_plane(&ctx, img->planes[VPX_PLANE_V], img->stride[VPX_PLANE_V], uv_width, uv_height) != 0) {
		return -1;
	}
	md5_final(&ctx, sum);
	hex_encode(sum, out);
	return 0;
}

static int read_file(const char *path, unsigned char **out_data, size_t *out_len) {
	FILE *f = fopen(path, "rb");
	long length;
	unsigned char *data;

	if (f == NULL) {
		fprintf(stderr, "open %s: %s\n", path, strerror(errno));
		return -1;
	}
	if (fseek(f, 0, SEEK_END) != 0) {
		fprintf(stderr, "seek %s: %s\n", path, strerror(errno));
		fclose(f);
		return -1;
	}
	length = ftell(f);
	if (length < 0) {
		fprintf(stderr, "tell %s: %s\n", path, strerror(errno));
		fclose(f);
		return -1;
	}
	if (fseek(f, 0, SEEK_SET) != 0) {
		fprintf(stderr, "seek %s: %s\n", path, strerror(errno));
		fclose(f);
		return -1;
	}
	data = (unsigned char *)malloc((size_t)length);
	if (data == NULL && length != 0) {
		fprintf(stderr, "malloc %ld bytes failed\n", length);
		fclose(f);
		return -1;
	}
	if (length != 0 && fread(data, 1, (size_t)length, f) != (size_t)length) {
		fprintf(stderr, "read %s failed\n", path);
		free(data);
		fclose(f);
		return -1;
	}
	fclose(f);
	*out_data = data;
	*out_len = (size_t)length;
	return 0;
}

static int validate_ivf_header(const unsigned char *data, size_t len) {
	if (len < 32 ||
	    data[0] != 'D' || data[1] != 'K' || data[2] != 'I' || data[3] != 'F' ||
	    load_le16(data + 4) != 0 ||
	    load_le16(data + 6) != 32 ||
	    data[8] != 'V' || data[9] != 'P' || data[10] != '8' || data[11] != '0') {
		return -1;
	}
	return 0;
}

static int emit_frame_json(unsigned int frame_index, int keyframe, int show_frame, const vpx_image_t *img) {
	char y_md5[33];
	char u_md5[33];
	char v_md5[33];
	char all_md5[33];
	unsigned int uv_width = (img->d_w + 1) >> 1;
	unsigned int uv_height = (img->d_h + 1) >> 1;

	if (plane_md5_hex(img->planes[VPX_PLANE_Y], img->stride[VPX_PLANE_Y], img->d_w, img->d_h, y_md5) != 0 ||
	    plane_md5_hex(img->planes[VPX_PLANE_U], img->stride[VPX_PLANE_U], uv_width, uv_height, u_md5) != 0 ||
	    plane_md5_hex(img->planes[VPX_PLANE_V], img->stride[VPX_PLANE_V], uv_width, uv_height, v_md5) != 0 ||
	    full_md5_hex(img, all_md5) != 0) {
		fprintf(stderr, "unsupported decoded image stride\n");
		return -1;
	}

	printf("{\"frame\":%u,\"width\":%u,\"height\":%u,\"keyframe\":%s,\"show_frame\":%s,"
	       "\"y_md5\":\"%s\",\"u_md5\":\"%s\",\"v_md5\":\"%s\",\"full_md5\":\"%s\"}\n",
	       frame_index,
	       img->d_w,
	       img->d_h,
	       keyframe ? "true" : "false",
	       show_frame ? "true" : "false",
	       y_md5,
	       u_md5,
	       v_md5,
	       all_md5);
	return 0;
}

static int decode_ivf(const char *path, vpx_codec_flags_t flags, vp8_postproc_cfg_t *postproc) {
	unsigned char *data = NULL;
	size_t len = 0;
	size_t offset = 32;
	unsigned int output_index = 0;
	vpx_codec_ctx_t codec;
	vpx_codec_dec_cfg_t cfg;

	if (read_file(path, &data, &len) != 0) {
		return 1;
	}
	if (validate_ivf_header(data, len) != 0) {
		fprintf(stderr, "invalid VP8 IVF input\n");
		free(data);
		return 1;
	}

	memset(&codec, 0, sizeof(codec));
	memset(&cfg, 0, sizeof(cfg));
	if (vpx_codec_dec_init(&codec, vpx_codec_vp8_dx(), &cfg, flags) != VPX_CODEC_OK) {
		fprintf(stderr, "vpx_codec_dec_init failed: %s\n", vpx_codec_error(&codec));
		free(data);
		return 1;
	}
	if (postproc != NULL && vpx_codec_control(&codec, VP8_SET_POSTPROC, postproc) != VPX_CODEC_OK) {
		fprintf(stderr, "VP8_SET_POSTPROC failed: %s\n", vpx_codec_error(&codec));
		vpx_codec_destroy(&codec);
		free(data);
		return 1;
	}

	while (offset < len) {
		uint32_t frame_size;
		const unsigned char *frame;
		int keyframe;
		int show_frame;
		vpx_codec_iter_t iter = NULL;
		vpx_image_t *img;

		if (len - offset < 12) {
			fprintf(stderr, "truncated IVF frame header\n");
			vpx_codec_destroy(&codec);
			free(data);
			return 1;
		}
		frame_size = load_le32(data + offset);
		(void)load_le64(data + offset + 4);
		offset += 12;
		if ((size_t)frame_size > len - offset) {
			fprintf(stderr, "truncated IVF frame payload\n");
			vpx_codec_destroy(&codec);
			free(data);
			return 1;
		}
		frame = data + offset;
		offset += frame_size;
		keyframe = frame_size > 0 && ((frame[0] & 1) == 0);
		show_frame = frame_size > 0 && ((frame[0] & 0x10) != 0);

		if (vpx_codec_decode(&codec, frame, frame_size, NULL, 0) != VPX_CODEC_OK) {
			fprintf(stderr, "vpx_codec_decode failed: %s\n", vpx_codec_error(&codec));
			vpx_codec_destroy(&codec);
			free(data);
			return 1;
		}
		while ((img = vpx_codec_get_frame(&codec, &iter)) != NULL) {
			if (emit_frame_json(output_index, keyframe, show_frame, img) != 0) {
				vpx_codec_destroy(&codec);
				free(data);
				return 1;
			}
			output_index++;
		}
	}

	vpx_codec_destroy(&codec);
	free(data);
	return 0;
}

static void usage(const char *argv0) {
	fprintf(stderr, "usage: %s decode|decode-postproc|decode-postproc-noise|decode-postproc-all-noise|decode-error-concealment input.ivf\n", argv0);
}

int main(int argc, char **argv) {
	if (argc == 2) {
		return decode_ivf(argv[1], 0, NULL);
	}
	if (argc == 3 && strcmp(argv[1], "decode") == 0) {
		return decode_ivf(argv[2], 0, NULL);
	}
	if (argc == 3 && strcmp(argv[1], "decode-postproc") == 0) {
		vp8_postproc_cfg_t postproc = { VP8_DEBLOCK | VP8_DEMACROBLOCK | VP8_MFQE, 4, 0 };
		return decode_ivf(argv[2], VPX_CODEC_USE_POSTPROC, &postproc);
	}
	if (argc == 3 && strcmp(argv[1], "decode-postproc-noise") == 0) {
		vp8_postproc_cfg_t postproc = { VP8_ADDNOISE, 0, 4 };
		srand(1);
		return decode_ivf(argv[2], VPX_CODEC_USE_POSTPROC, &postproc);
	}
	if (argc == 3 && strcmp(argv[1], "decode-postproc-all-noise") == 0) {
		vp8_postproc_cfg_t postproc = { VP8_DEBLOCK | VP8_DEMACROBLOCK | VP8_ADDNOISE | VP8_MFQE, 4, 4 };
		srand(1);
		return decode_ivf(argv[2], VPX_CODEC_USE_POSTPROC, &postproc);
	}
	if (argc == 3 && strcmp(argv[1], "decode-error-concealment") == 0) {
		return decode_ivf(argv[2], VPX_CODEC_USE_ERROR_CONCEALMENT, NULL);
	}
	usage(argv[0]);
	return 2;
}
