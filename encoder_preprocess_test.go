package govpx

import (
	"hash/adler32"
	"testing"

	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

// arnrTestEncoder builds an encoder pre-wired for direct applyARNRFilter
// invocation. It enables a lookahead queue large enough to hold the
// requested forward frames and seeds the queue/back buffer with the
// supplied frames.
func arnrTestEncoder(t *testing.T, width int, height int, maxFrames int, strength int, arnrType int, back []byte, forward [][]byte) *VP8Encoder {
	t.Helper()
	opts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               30,
		TargetBitrateKbps: 800,
		LookaheadFrames:   max(1, len(forward)),
		ARNRMaxFrames:     maxFrames,
		ARNRStrength:      strength,
		ARNRType:          arnrType,
	}
	e, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder failed: %v", err)
	}
	if back != nil {
		copySourceToFrameBuffer(&e.arnrLastSource, syntheticSource(width, height, back))
		e.arnrLastReady = true
	}
	for i, plane := range forward {
		if err := e.pushLookahead(syntheticSource(width, height, plane), uint64(i+1), 1, 0); err != nil {
			t.Fatalf("pushLookahead[%d]: %v", i, err)
		}
	}
	return e
}

// syntheticSource returns a SourceImage holding the given Y plane plus a
// neutral chroma plane (value 128). The plane buffers are sized to the
// visible region with stride == width.
func syntheticSource(width int, height int, y []byte) vp8enc.SourceImage {
	if len(y) != width*height {
		panic("syntheticSource: y length mismatch")
	}
	uvW := (width + 1) >> 1
	uvH := (height + 1) >> 1
	u := make([]byte, uvW*uvH)
	v := make([]byte, uvW*uvH)
	for i := range u {
		u[i] = 128
		v[i] = 128
	}
	return vp8enc.SourceImage{
		Width:   width,
		Height:  height,
		Y:       y,
		U:       u,
		V:       v,
		YStride: width,
		UStride: uvW,
		VStride: uvW,
	}
}

// solidPlane returns a width*height byte plane filled with `val`.
func solidPlane(width int, height int, val byte) []byte {
	p := make([]byte, width*height)
	for i := range p {
		p[i] = val
	}
	return p
}

// movingSquarePlane draws a 32x32 white square on a black background at
// pixel offset (offX, offY).
func movingSquarePlane(width int, height int, offX int, offY int) []byte {
	p := make([]byte, width*height)
	for y := 0; y < 32; y++ {
		ty := y + offY
		if ty < 0 || ty >= height {
			continue
		}
		row := ty * width
		for x := 0; x < 32; x++ {
			tx := x + offX
			if tx < 0 || tx >= width {
				continue
			}
			p[row+tx] = 220
		}
	}
	return p
}

func TestARNRZeroStrengthIsIdentityOnIdenticalFrames(t *testing.T) {
	const w, h = 64, 64
	plane := solidPlane(w, h, 64)
	// Add a checkerboard pattern so the test would fail if the filter
	// shifted any pixel value.
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if (x^y)&1 == 0 {
				plane[y*w+x] = 100
			} else {
				plane[y*w+x] = 140
			}
		}
	}
	cp := func() []byte {
		c := make([]byte, len(plane))
		copy(c, plane)
		return c
	}
	e := arnrTestEncoder(t, w, h, 3, 0, 3, cp(), [][]byte{cp(), cp()})
	if !e.applyARNRFilter(syntheticSource(w, h, cp()), 0) {
		t.Fatalf("applyARNRFilter returned false; expected filtering to run")
	}
	got := e.arnrScratch.Img.Y
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			want := plane[y*w+x]
			if got[y*e.arnrScratch.Img.YStride+x] != want {
				t.Fatalf("strength=0 identity failed at (%d,%d): got %d want %d", x, y, got[y*e.arnrScratch.Img.YStride+x], want)
			}
		}
	}
}

// TestARNRChangesPixelsOnSyntheticMotionClip verifies that with the alt-ref
// motion-compensated temporal filter enabled, adjacent frames whose content
// is displaced by a known motion vector and offset slightly in luma cause
// the centered output to diverge from the raw center frame (i.e. the
// motion-compensated predictor must contribute non-zero weight).
func TestARNRChangesPixelsOnSyntheticMotionClip(t *testing.T) {
	const w, h = 64, 64
	// Square moves left in time: back has it at (12,16), center at
	// (16,16), forward at (20,16). The motion search must compensate
	// the displacement, leaving small residual luma differences (we
	// shade the squares with a horizontal gradient that the integer-pel
	// search snaps but doesn't fully cancel) so the temporal weighting
	// modifies pixels.
	makeFrame := func(offX int, lumaBias int) []byte {
		p := solidPlane(w, h, 80)
		for y := 0; y < 32; y++ {
			ty := y + 16
			if ty < 0 || ty >= h {
				continue
			}
			row := ty * w
			for x := 0; x < 32; x++ {
				tx := x + offX
				if tx < 0 || tx >= w {
					continue
				}
				p[row+tx] = byte(160 + (x % 4) + lumaBias)
			}
		}
		return p
	}
	back := makeFrame(12, 3)
	center := makeFrame(16, 0)
	fwd := makeFrame(20, 3)

	// Strength=3 keeps the per-pixel weight curve broad enough that
	// small residuals after MC still contribute (at strength=6 a diff
	// of 3 already collapses the weight to 0, which would make the
	// filter idempotent on this clip).
	e := arnrTestEncoder(t, w, h, 3, 3, 3, back, [][]byte{fwd})
	cp := make([]byte, len(center))
	copy(cp, center)
	if !e.applyARNRFilter(syntheticSource(w, h, cp), 0) {
		t.Fatalf("applyARNRFilter returned false")
	}
	got := e.arnrScratch.Img.Y
	stride := e.arnrScratch.Img.YStride
	changed := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if got[y*stride+x] != center[y*w+x] {
				changed++
			}
		}
	}
	if changed == 0 {
		t.Fatalf("expected ARNR motion-compensated filter to alter pixels; output identical to center")
	}
}

// TestARNRFixedAdler32 pins the exact ARF buffer Adler32 produced for a
// known input + (maxframes=3, strength=3, type=3) configuration. This
// guards against accidental drift in the libvpx-port math (per-pixel
// weight formula, normalization, motion search). Update this constant
// only with intent; it is the regression anchor for the temporal filter.
func TestARNRFixedAdler32(t *testing.T) {
	const w, h = 64, 64
	// Back: gradient. Center: same gradient + 5. Forward: same gradient + 10.
	back := make([]byte, w*h)
	center := make([]byte, w*h)
	fwd := make([]byte, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			base := byte((x + y) & 0xff)
			back[y*w+x] = base
			center[y*w+x] = base + 5
			fwd[y*w+x] = base + 10
		}
	}
	e := arnrTestEncoder(t, w, h, 3, 3, 3, back, [][]byte{fwd})
	cp := make([]byte, len(center))
	copy(cp, center)
	if !e.applyARNRFilter(syntheticSource(w, h, cp), 0) {
		t.Fatalf("applyARNRFilter returned false")
	}
	// Hash only the visible region to avoid relying on border bytes.
	dst := e.arnrScratch.Img.Y
	stride := e.arnrScratch.Img.YStride
	visible := make([]byte, w*h)
	for y := 0; y < h; y++ {
		copy(visible[y*w:(y+1)*w], dst[y*stride:y*stride+w])
	}
	got := adler32.Checksum(visible)
	const want uint32 = 0xB9AB403D
	if got != want {
		t.Fatalf("ARNR Adler32 = 0x%08X, want 0x%08X", got, want)
	}
}
