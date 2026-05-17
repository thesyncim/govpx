package govpx

// vp9_yv12_border.go ports the YV12 frame-border substrate from libvpx
// v1.16.0. Constants and the edge-replication body are verbatim ports
// of the libvpx headers / sources cited inline below.
//
// Why govpx needs this:
//
//   govpx stores reference frames as plain image.YCbCr-shaped planes
//   (vp9_decoder.go: vp9ReferenceFrame { y, u, v }), with no padding
//   around the visible plane. libvpx allocates every YV12_BUFFER_CONFIG
//   with VP9_ENC_BORDER_IN_PIXELS pixels of edge-replicated padding on
//   all four sides (vpx_scale/yv12config.h:26 — VP9_ENC_BORDER_IN_PIXELS
//   = 160). Several encoder hot paths read pixels outside the visible
//   plane and rely on the border being valid:
//
//     - vp9_int_pro_motion_estimation reads up to (bw>>1) pixels before
//       the SB origin and (bh>>1) rows above it (libvpx
//       vp9/encoder/vp9_mcomp.c:2317-2320). For a BLOCK_64X64 SB at the
//       frame top-left this is a 32-pixel reach into the border.
//     - The luma / chroma sub-pel convolve fetches up to VP9_INTERP_EXTEND
//       = 4 pixels of tap context outside any motion-compensated block
//       (libvpx vpx_scale/yv12config.h:25).
//
//   This file gives encoder-side consumers a libvpx-faithful padded
//   plane buffer that callers populate from the visible plane via the
//   vpx_extend_frame_borders edge-replication rule
//   (vpx_scale/generic/yv12extend.c:22-60).
//
// Verbatim libvpx references cited inline below:
//
//   - vpx_scale/yv12config.h:23-27 — border constant definitions.
//   - vpx_scale/generic/yv12extend.c:22-60 — extend_plane.
//   - vpx_scale/generic/yv12extend.c:130-171 — extend_frame /
//     vpx_extend_frame_borders.
//   - vp9/encoder/vp9_encoder.c:1297-1367 — VP9_ENC_BORDER_IN_PIXELS
//     used for every encoder-side YV12 alloc.
//   - vp9/encoder/vp9_encoder.c:3102 / 3167 / 3424 / 3470 — the
//     post-frame-reconstruction extend hooks.

// VP9 YV12 border constants. Verbatim ports of vpx_scale/yv12config.h:
//
//	#define VP8BORDERINPIXELS       32   (line 23)
//	#define VP9INNERBORDERINPIXELS  96   (line 24)
//	#define VP9_INTERP_EXTEND        4   (line 25)
//	#define VP9_ENC_BORDER_IN_PIXELS 160 (line 26)
//	#define VP9_DEC_BORDER_IN_PIXELS 32  (line 27)
//
// The encoder always allocates VP9_ENC_BORDER_IN_PIXELS of padding per
// plane (vp9/encoder/vp9_encoder.c:1297 et al.). The decoder uses
// VP9_DEC_BORDER_IN_PIXELS. The int-pro motion search's worst-case
// reach (bw>>1 = 32 for BLOCK_64X64) fits inside the smaller decoder
// border, but on the encoder side libvpx provisions the full 160
// pixels so all hot paths share one allocation.
const (
	vp9Vp8BorderInPixels   = 32
	vp9InnerBorderInPixels = 96
	vp9InterpExtend        = 4
	vp9EncBorderInPixels   = 160
	vp9DecBorderInPixels   = 32
)

// vp9YV12BorderBuffer is a per-encoder scratch backing a border-padded
// copy of one luma (or chroma) plane. Callers fill it via
// vp9YV12BuildBorderedPlane after each frame's reconstruction, then
// hand the (Pixels, Stride, OriginX, OriginY) tuple to consumers that
// need to read pixels outside the visible plane.
//
// libvpx counterpart: YV12_BUFFER_CONFIG's per-plane y_buffer / u_buffer
// / v_buffer pointer, which always points at the (border, border)
// origin inside a (stride x (height+2*border)) allocation
// (vpx_scale/yv12config.h:29-65 + vpx_scale/generic/yv12extend.c:130-171).
type vp9YV12BorderBuffer struct {
	// Pixels holds the full (stride x rows) padded plane. The
	// visible plane lives at (OriginX, OriginY); the surrounding
	// `border` pixels on every side are edge-replicated.
	Pixels []uint8

	// Stride is the row pitch of Pixels in bytes (== W + 2*Border).
	Stride int

	// W / H are the dimensions of the visible plane.
	W int
	H int

	// Border is the per-side padding width in pixels. Always
	// vp9EncBorderInPixels for encoder-side allocations; the field is
	// kept explicit so consumers can derive the absolute origin.
	Border int
}

// OriginX / OriginY return the (col, row) coordinate of the visible
// plane's top-left pixel inside the Pixels buffer. Always equal to
// Border for a libvpx-shaped allocation.
func (b *vp9YV12BorderBuffer) OriginX() int { return b.Border }
func (b *vp9YV12BorderBuffer) OriginY() int { return b.Border }

// Rows returns the total number of rows in the padded buffer
// (== H + 2*Border).
func (b *vp9YV12BorderBuffer) Rows() int { return b.H + 2*b.Border }

// vp9ExtendPlane is a verbatim port of libvpx's static extend_plane
// (vpx_scale/generic/yv12extend.c:22-60). It writes the
// `extend_left`-wide left border and `extend_right`-wide right border
// of every visible row with the row's leftmost / rightmost pixel, then
// copies the top / bottom visible rows into the `extend_top` /
// `extend_bottom` rows above / below the plane.
//
//   - pixels: the (stride x (extend_top + height + extend_bottom)) backing
//     buffer.
//   - srcOff: byte offset of the visible plane's top-left inside pixels
//     (libvpx's `src` pointer; the function reads pixels[srcOff..]
//     for the visible body and pixels[srcOff - extend_left..] for the
//     left border).
//   - srcStride: row pitch of pixels in bytes.
//   - width / height: visible plane dimensions.
//   - extendTop / extendLeft / extendBottom / extendRight: per-side
//     padding widths in pixels.
//
// Verbatim libvpx body, all line numbers in
// vpx_scale/generic/yv12extend.c:
//
//	22-24  signature.
//	25     int i;
//	26     const int linesize = extend_left + extend_right + width;
//	28-31  src_ptr1 / src_ptr2 / dst_ptr1 / dst_ptr2 left-right setup.
//	33-41  per-row left & right memset loop.
//	46-49  top / bottom block setup (src_ptr1 = src - extend_left;
//	       src_ptr2 = src + src_stride * (height - 1) - extend_left;
//	       dst_ptr1 = src + src_stride * -extend_top - extend_left;
//	       dst_ptr2 = src + src_stride * height - extend_left;).
//	51-54  extend_top memcpy loop.
//	56-59  extend_bottom memcpy loop.
//
// The Go port keeps the libvpx pointer arithmetic verbatim: pixels is
// the single backing slice, srcOff is the address of the visible
// plane's top-left pixel relative to the slice base, and the four
// per-direction offsets (dst_ptr1 etc.) are reconstructed by index
// arithmetic into pixels[].
func vp9ExtendPlane(pixels []uint8, srcOff, srcStride, width, height,
	extendTop, extendLeft, extendBottom, extendRight int,
) {
	linesize := extendLeft + extendRight + width

	// libvpx 28-31:
	//   uint8_t *src_ptr1 = src;
	//   uint8_t *src_ptr2 = src + width - 1;
	//   uint8_t *dst_ptr1 = src - extend_left;
	//   uint8_t *dst_ptr2 = src + width;
	src1Off := srcOff
	src2Off := srcOff + width - 1
	dst1Off := srcOff - extendLeft
	dst2Off := srcOff + width

	// libvpx 33-41: per-visible-row left & right memset loop.
	for range height {
		left := pixels[src1Off]
		right := pixels[src2Off]
		for j := range extendLeft {
			pixels[dst1Off+j] = left
		}
		for j := range extendRight {
			pixels[dst2Off+j] = right
		}
		src1Off += srcStride
		src2Off += srcStride
		dst1Off += srcStride
		dst2Off += srcStride
	}

	// libvpx 46-49:
	//   src_ptr1 = src - extend_left;
	//   src_ptr2 = src + src_stride * (height - 1) - extend_left;
	//   dst_ptr1 = src + src_stride * -extend_top - extend_left;
	//   dst_ptr2 = src + src_stride * height - extend_left;
	src1Off = srcOff - extendLeft
	src2Off = srcOff + srcStride*(height-1) - extendLeft
	dst1Off = srcOff + srcStride*(-extendTop) - extendLeft
	dst2Off = srcOff + srcStride*height - extendLeft

	// libvpx 51-54: extend_top memcpy loop.
	for range extendTop {
		copy(pixels[dst1Off:dst1Off+linesize], pixels[src1Off:src1Off+linesize])
		dst1Off += srcStride
	}

	// libvpx 56-59: extend_bottom memcpy loop.
	for range extendBottom {
		copy(pixels[dst2Off:dst2Off+linesize], pixels[src2Off:src2Off+linesize])
		dst2Off += srcStride
	}
}

// vp9YV12BuildBorderedPlane (re)allocates buf to host a libvpx-shaped
// bordered copy of `plane` (width x height visible pixels, planeStride
// row pitch), then fills the visible body and the surrounding border
// of `border` pixels on every side. Edge replication matches libvpx's
// vpx_extend_frame_borders semantics
// (vpx_scale/generic/yv12extend.c:130-171, which calls extend_plane
// with extend_top = extend_left = border and
// extend_bottom = border + ybf->y_height - ybf->y_crop_height,
// extend_right = border + ybf->y_width - ybf->y_crop_width).
//
// govpx callers always supply the cropped visible plane (no internal
// vs. crop distinction; image.YCbCr planes are uncropped), so
// extend_bottom == border and extend_right == border. The asymmetric
// libvpx behaviour for ybf->y_height - ybf->y_crop_height > 0 frames
// would matter only if the caller passes the un-cropped allocation
// width / height; govpx's encoder lookahead delivers the visible
// dimensions directly.
//
// The function returns the underlying Pixels slice, its Stride, and
// the (originX, originY) coordinate of the visible plane's top-left
// inside Pixels (== border, border).
func vp9YV12BuildBorderedPlane(buf *vp9YV12BorderBuffer,
	plane []uint8, planeStride, width, height, border int,
) (pixels []uint8, stride, originX, originY int) {
	stride = width + 2*border
	rows := height + 2*border
	needed := stride * rows
	if cap(buf.Pixels) < needed {
		buf.Pixels = make([]uint8, needed)
	}
	buf.Pixels = buf.Pixels[:needed]
	buf.Stride = stride
	buf.W = width
	buf.H = height
	buf.Border = border

	// Copy the visible plane into the interior of the padded buffer.
	// libvpx's vpx_yv12_copy_frame_c body does this with a memcpy per
	// row (vpx_scale/generic/yv12extend.c:283-287) before calling
	// vpx_extend_frame_borders_c at the end. govpx mirrors that order
	// here.
	dstOriginOff := border*stride + border
	for y := range height {
		dst := buf.Pixels[dstOriginOff+y*stride:]
		src := plane[y*planeStride : y*planeStride+width]
		copy(dst[:width], src)
	}

	// Apply the four-sided edge replication. libvpx's
	// vpx_extend_frame_borders_c -> extend_frame -> extend_plane uses
	// extend_top = extend_left = border and
	// extend_bottom = border + (y_height - y_crop_height),
	// extend_right = border + (y_width - y_crop_width). govpx's
	// uncropped image.YCbCr planes have y_height == y_crop_height and
	// y_width == y_crop_width so the bottom / right extents collapse
	// to plain `border`.
	vp9ExtendPlane(buf.Pixels, dstOriginOff, stride, width, height,
		border, border, border, border)

	return buf.Pixels, stride, border, border
}
