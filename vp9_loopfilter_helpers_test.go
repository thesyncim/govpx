package govpx

import "image"

// newVP9TexturedYCbCrForLpfPickerTest synthesises a deterministically-
// noisy YCbCr 4:2:0 frame for the LPF picker's PSNR-delta test. The
// luma plane carries a high-frequency mixture of horizontal stripes,
// vertical bars, and a per-pixel pseudo-random rotation so the
// quadratic search over filter levels has a meaningful SSE landscape
// (cf. libvpx vp9_picklpf.c:78-157 search_filter_level — the search
// only diverges from the FROM_Q seed when the post-filter SSE
// differs across candidate levels). The deterministic LCG seed pins
// the frame for byte-stable reproduction.
func newVP9TexturedYCbCrForLpfPickerTest(width, height int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	state := uint32(0xDEADBEEF)
	for y := range height {
		row := img.Y[y*img.YStride:]
		for x := range width {
			// Deterministic xorshift32 → 8-bit per-pixel noise.
			state ^= state << 13
			state ^= state >> 17
			state ^= state << 5
			noise := byte(state & 0xFF)
			// Stripe pattern with diagonal contrast.
			base := byte(((x + y*3) & 0x3F) + 64)
			row[x] = base ^ (noise >> 2)
		}
	}
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	for y := range uvHeight {
		cb := img.Cb[y*img.CStride:]
		cr := img.Cr[y*img.CStride:]
		for x := range uvWidth {
			state ^= state << 13
			state ^= state >> 17
			state ^= state << 5
			cb[x] = byte(96 + (state & 0x3F))
			cr[x] = byte(160 + ((state >> 8) & 0x3F))
		}
	}
	return img
}
