package benchcmd

import (
	"image"
	"math/rand"
	"os"
)

// BDRateContent labels the synthetic content generator used by
// BD-rate quality measurements. Each generator stresses a different
// VP9 path: PanningContent exercises inter prediction, TextureNoise
// exercises ARNR, SharpEdgesContent exercises TPL, and
// VarianceHeavyContent stresses variance-AQ segmentation by mixing
// flat and detailed regions across each frame.
type BDRateContent int

const (
	// PanningContent is a constant-velocity translating texture.
	PanningContent BDRateContent = iota
	// TextureNoise is a stationary textured field with additive
	// per-frame noise.
	TextureNoise
	// SharpEdgesContent contains hard rectangular edges that move
	// across the frame; the high-frequency boundaries are where
	// TPL's per-SB qindex bias matters most.
	SharpEdgesContent
	// VarianceHeavyContent mixes a flat top half with a textured
	// bottom half, plus translation, so variance-AQ has both
	// segments to allocate Q to.
	VarianceHeavyContent
	// PerceptualContent gradients with both flat regions and
	// luma-modulated detail areas the perceptual AQ model picks up.
	PerceptualContent
)

// BDRateGenerator returns a generator suitable for direct use as the
// BDRateOptions.Source callback.
func BDRateGenerator(kind BDRateContent, width, height int) func(int) *image.YCbCr {
	switch kind {
	case PanningContent:
		return func(i int) *image.YCbCr {
			return makePanningContent(width, height, i)
		}
	case TextureNoise:
		return func(i int) *image.YCbCr {
			return makeTextureNoise(width, height, i)
		}
	case SharpEdgesContent:
		return func(i int) *image.YCbCr {
			return makeSharpEdges(width, height, i)
		}
	case VarianceHeavyContent:
		return func(i int) *image.YCbCr {
			return makeVarianceHeavy(width, height, i)
		}
	case PerceptualContent:
		return func(i int) *image.YCbCr {
			return makePerceptual(width, height, i)
		}
	}
	return func(i int) *image.YCbCr {
		return makePanningContent(width, height, i)
	}
}

// makePanningContent builds a constant-velocity translating textured
// frame. The texture's gradient ensures inter prediction with a small
// motion vector can model the next frame's reconstruction; this is
// the canonical "AltRef pays off" workload because the hidden ARF
// can be drawn from frames late in the panning sequence.
func makePanningContent(width, height, idx int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	shiftX := idx * 2
	shiftY := idx
	for y := range height {
		row := img.Y[y*img.YStride:]
		for x := range width {
			// Smooth gradient + low-frequency texture so motion
			// compensation has a coherent reconstruction.
			gx := (x + shiftX) % width
			gy := (y + shiftY) % height
			row[x] = byte(40 + ((gx*3 + gy*5) & 0x7F) + ((gx*gy)&0x3F)/2)
		}
	}
	uvW := (width + 1) >> 1
	uvH := (height + 1) >> 1
	for y := range uvH {
		cb := img.Cb[y*img.CStride:]
		cr := img.Cr[y*img.CStride:]
		for x := range uvW {
			gx := (x + shiftX/2) % uvW
			gy := (y + shiftY/2) % uvH
			cb[x] = byte(96 + (gx*5+gy*3)&0x3F)
			cr[x] = byte(112 + (gx*3+gy*7)&0x3F)
		}
	}
	return img
}

// makeTextureNoise builds a slowly-translating textured field with
// additive deterministic per-frame noise. ARNR's temporal filtering
// is supposed to suppress the noise on the hidden alt-ref reference,
// trading a small denoise pass at encode time for fewer bits to code
// residual on the visible frames that point at the cleaner ARF.
func makeTextureNoise(width, height, idx int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	// Seed by idx so noise is deterministic per frame, but varies
	// across frames (different rand draw each frame).
	r := rand.New(rand.NewSource(int64(idx) + 1))
	for y := range height {
		row := img.Y[y*img.YStride:]
		for x := range width {
			base := byte(64 + ((x*3 + y*2) & 0x7F))
			noise := byte(r.Intn(32) - 16) // small zero-mean noise
			row[x] = clampByte(int(base) + int(int8(noise)))
		}
	}
	uvW := (width + 1) >> 1
	uvH := (height + 1) >> 1
	for y := range uvH {
		cb := img.Cb[y*img.CStride:]
		cr := img.Cr[y*img.CStride:]
		for x := range uvW {
			cb[x] = byte(112 + (x*5+y*3)&0x3F)
			cr[x] = byte(128 + (x*3+y*5)&0x3F)
		}
	}
	return img
}

// makeSharpEdges builds a frame with hard rectangular shapes that
// translate over time. The shape boundaries are the high-contrast
// edges TPL's per-SB qindex bias is designed for.
func makeSharpEdges(width, height, idx int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	dx := idx % width
	for y := range height {
		row := img.Y[y*img.YStride:]
		for x := range width {
			// Tiled black/white rectangles, shifting per frame.
			cellX := ((x + dx) / 16) & 1
			cellY := (y / 16) & 1
			if cellX != cellY {
				row[x] = 235
			} else {
				row[x] = 16
			}
		}
	}
	uvW := (width + 1) >> 1
	uvH := (height + 1) >> 1
	for y := range uvH {
		cb := img.Cb[y*img.CStride:]
		cr := img.Cr[y*img.CStride:]
		for x := range uvW {
			cb[x] = 128
			cr[x] = 128
		}
	}
	return img
}

// makeVarianceHeavy mixes a flat upper half with a heavily textured
// lower half. Variance-AQ's segmentation must allocate fewer bits to
// the flat region (no Q reduction needed) and more bits to the
// textured region (Q reduced for better fidelity).
func makeVarianceHeavy(width, height, idx int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	halfH := height / 2
	dx := idx
	for y := range height {
		row := img.Y[y*img.YStride:]
		if y < halfH {
			// Flat region with very slow gradient so it isn't
			// completely uniform (rate is non-zero).
			for x := range width {
				row[x] = byte(96 + (y / 32))
			}
		} else {
			// Heavy texture with translation per frame.
			for x := range width {
				v := ((x+dx*2)*31 + (y-halfH)*47) & 0xFF
				row[x] = byte(v)
			}
		}
	}
	uvW := (width + 1) >> 1
	uvH := (height + 1) >> 1
	for y := range uvH {
		cb := img.Cb[y*img.CStride:]
		cr := img.Cr[y*img.CStride:]
		for x := range uvW {
			cb[x] = 128
			cr[x] = 128
		}
	}
	return img
}

// makePerceptual builds a frame with a clear contrast between large
// smooth (perceptually-important) regions and dense high-frequency
// (perceptually-masked) detail bands. Perceptual AQ is designed to
// save bitrate on this kind of content by quantising the detail
// regions more aggressively while preserving the smooth regions:
//
//   - The left third is a slow vertical luma gradient (flat, easy to
//     model with inter prediction or a small DC residual).
//   - The middle third is a dense, high-amplitude texture pattern
//     ("noise band") whose AC coefficients dominate the Wiener
//     variance — this is the perceptual-masking region.
//   - The right third is another slow gradient.
//
// Frame-to-frame translation lets inter prediction work; the texture
// region is panned so the texture itself is coherent under motion.
func makePerceptual(width, height, idx int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	bandStart := width / 3
	bandEnd := (2 * width) / 3
	shiftX := idx
	for y := range height {
		row := img.Y[y*img.YStride:]
		for x := range width {
			var v int
			switch {
			case x < bandStart:
				// Left smooth gradient (perceptually important).
				v = 48 + (y * 96 / height)
			case x < bandEnd:
				// Dense high-frequency band (perceptually masked).
				gx := (x + shiftX) & 0x3
				gy := y & 0x3
				if (gx+gy)&1 == 0 {
					v = 224
				} else {
					v = 32
				}
			default:
				// Right smooth gradient.
				v = 144 + (y * 96 / height)
			}
			row[x] = clampByte(v)
		}
	}
	uvW := (width + 1) >> 1
	uvH := (height + 1) >> 1
	for y := range uvH {
		cb := img.Cb[y*img.CStride:]
		cr := img.Cr[y*img.CStride:]
		for x := range uvW {
			cb[x] = 128
			cr[x] = 128
		}
	}
	return img
}

func clampByte(v int) byte {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v)
}

// BDRateGatesEnabled reports whether the slow BD-rate quality gates
// should run. They are gated behind GOVPX_BD_RATE_GATES=1 so
// `go test ./...` stays fast in the default short configuration while
// CI's BD-rate targets opt in.
func BDRateGatesEnabled() bool {
	return os.Getenv("GOVPX_BD_RATE_GATES") == "1"
}
