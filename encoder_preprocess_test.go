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

func TestARNRZeroStrengthIsIdentityOnIdenticalFrames(t *testing.T) {
	const w, h = 64, 64
	plane := solidPlane(w, h, 64)
	// Add a checkerboard pattern so the test would fail if the filter
	// shifted any pixel value.
	for y := range h {
		for x := range w {
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
	for y := range h {
		for x := range w {
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
		for y := range 32 {
			ty := y + 16
			if ty < 0 || ty >= h {
				continue
			}
			row := ty * w
			for x := range 32 {
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
	for y := range h {
		for x := range w {
			if got[y*stride+x] != center[y*w+x] {
				changed++
			}
		}
	}
	if changed == 0 {
		t.Fatalf("expected ARNR motion-compensated filter to alter pixels; output identical to center")
	}
}

// TestARNRSubpelDeterministicAdler32 pins the exact ARF buffer Adler32
// produced for a known input + (maxframes=3, strength=3, type=3)
// configuration after subpel refinement is applied. This guards against
// accidental drift in the libvpx-port math (per-pixel weight formula,
// normalization, hex search seeded from the prior MV, and the 1/2-/1/4-/
// 1/8-pel diamond walk that adopts the lowest-SAD sixtap predictor).
// Update this constant only with intent; it is the regression anchor for
// the motion-compensated temporal filter.
func TestARNRSubpelDeterministicAdler32(t *testing.T) {
	const w, h = 64, 64
	// Back: gradient. Center: same gradient + 5. Forward: same gradient + 10.
	back := make([]byte, w*h)
	center := make([]byte, w*h)
	fwd := make([]byte, w*h)
	for y := range h {
		for x := range w {
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
	for y := range h {
		copy(visible[y*w:(y+1)*w], dst[y*stride:y*stride+w])
	}
	got := adler32.Checksum(visible)
	// Pinned against the libvpx-shaped hex+subpel-search ARNR output for
	// this configuration. Earlier integer-only revisions produced
	// 0x482C403D; subpel refinement may relocate predictors at fractional
	// positions, mixing sixtap-filtered samples into the accumulator.
	// On this gradient the integer-pel position wins every subpel probe
	// (the gradient is exactly aligned at integer offsets), so the
	// constant matches the integer-only baseline.
	const want uint32 = 0x482C403D
	if got != want {
		t.Fatalf("ARNR Adler32 = 0x%08X, want 0x%08X", got, want)
	}
}

// TestARNRHexSearchTracksLargeMotion validates that the hex search can
// follow motion well outside the previous local-exhaustive
// arnrSearchRadius=7 window. Each adjacent frame is a horizontally
// shifted-by-12 copy of the center with small additive noise injected.
// With the hex search engaged, the temporal filter must locate each MB's
// matching block in the reference frame at MV(±12, 0), classify SAD as
// "low", and average the noisy predictors back into the alt-ref output
// (de-noising it relative to the alt-ref source). The previous local
// scan only swept ±7 pixels around (0,0), so SAD on a striped panning
// clip would always exceed THRESH_HIGH=20000 and skip the references
// entirely (filter_weight=0); the output would equal the center input
// byte-for-byte.
func TestARNRHexSearchTracksLargeMotion(t *testing.T) {
	const w, h = 64, 64
	// Build a textured scene that shifts by ±12 pixels per frame so the
	// best match sits beyond the prior code's 15x15 search window. The
	// pattern uses a horizontal stripe pattern so SAD is sharply minimized
	// at the correct MV.
	stripe := func(sx int) byte {
		if (sx>>2)&1 == 0 {
			return 200
		}
		return 60
	}
	clean := func(offX int) []byte {
		p := make([]byte, w*h)
		for y := range h {
			for x := range w {
				sx := max(x-offX, 0)
				if sx >= w {
					sx = w - 1
				}
				p[y*w+x] = stripe(sx)
			}
		}
		return p
	}
	// Adjacent frames carry small +/- noise on each pixel so a successful
	// MC match yields a near-zero (but non-zero) SAD that lands under
	// THRESH_LOW=10000 and produces filter_weight=2. Without the hex
	// search reaching ±12, SAD would compare unrelated stripes and
	// exceed THRESH_HIGH=20000, zeroing filter_weight.
	noisy := func(base []byte, seed int) []byte {
		p := make([]byte, len(base))
		copy(p, base)
		// Deterministic small additive perturbation (bounded by ±3) so
		// the per-pixel modifier remains close to 16 even with
		// strength=3 (where (3*3*3 + 4) >> 3 = 3).
		for i := range p {
			d := ((i*1103515245 + seed*12345) >> 8) & 7
			d -= 3
			v := min(max(int(p[i])+d, 0), 255)
			p[i] = byte(v)
		}
		return p
	}
	centerClean := clean(0)
	back := noisy(clean(-12), 1)
	center := noisy(centerClean, 2)
	fwd := noisy(clean(12), 3)

	e := arnrTestEncoder(t, w, h, 3, 3, 3, back, [][]byte{fwd})
	cp := make([]byte, len(center))
	copy(cp, center)
	if !e.applyARNRFilter(syntheticSource(w, h, cp), 0) {
		t.Fatalf("applyARNRFilter returned false")
	}
	got := e.arnrScratch.Img.Y
	stride := e.arnrScratch.Img.YStride

	// SSE of the filtered output vs. the noiseless target. If the hex
	// search located the panning predictors and integrated them with
	// filter_weight>0, the temporal averaging reduces the noise. If the
	// search returned (0,0) and discarded the references (filter_weight
	// = 0 because SAD on stripe-misaligned content >>20000), the output
	// equals `center` and SSE is the noise's MSE * w*h.
	sseFiltered := 0
	sseCenter := 0
	for y := range h {
		for x := range w {
			truth := int(centerClean[y*w+x])
			f := int(got[y*stride+x])
			c := int(center[y*w+x])
			df := f - truth
			dc := c - truth
			sseFiltered += df * df
			sseCenter += dc * dc
		}
	}
	if sseFiltered >= sseCenter {
		t.Fatalf("hex-search ARNR did not reduce noise: filtered SSE=%d >= center SSE=%d", sseFiltered, sseCenter)
	}
	// Sanity-check that the temporal filter actually mixed in adjacent
	// frame samples (it cannot reduce noise without a non-(0,0) MV match
	// on a striped pan; the prior local-exhaustive search would have
	// returned (0,0) and skipped both references because SAD on
	// stripe-misaligned content sits above THRESH_HIGH=20000).
	differingPixels := 0
	for y := range h {
		for x := range w {
			if int(got[y*stride+x]) != int(center[y*w+x]) {
				differingPixels++
			}
		}
	}
	if differingPixels == 0 {
		t.Fatalf("hex-search ARNR output identical to center; expected MC contributions")
	}
}

// halfPelShiftedPlane returns a plane built by sampling `truth` at integer
// positions plus a 0.5-pixel offset along each axis (averaging the two
// neighbors). The result simulates a frame whose true motion is exactly
// half-pel relative to `truth`; integer-pel search cannot align this with
// `truth` byte-for-byte, but a sixtap subpel predictor at fracCol=4,
// fracRow=4 produces a much closer match.
func halfPelShiftedPlane(truth []byte, w, h int, shiftX, shiftY int, halfX, halfY bool) []byte {
	out := make([]byte, w*h)
	for y := range h {
		yA := y + shiftY
		yB := yA
		if halfY {
			yB = yA + 1
		}
		if yA < 0 {
			yA = 0
		}
		if yA >= h {
			yA = h - 1
		}
		if yB < 0 {
			yB = 0
		}
		if yB >= h {
			yB = h - 1
		}
		for x := range w {
			xA := x + shiftX
			xB := xA
			if halfX {
				xB = xA + 1
			}
			if xA < 0 {
				xA = 0
			}
			if xA >= w {
				xA = w - 1
			}
			if xB < 0 {
				xB = 0
			}
			if xB >= w {
				xB = w - 1
			}
			a := int(truth[yA*w+xA])
			b := int(truth[yA*w+xB])
			c := int(truth[yB*w+xA])
			d := int(truth[yB*w+xB])
			out[y*w+x] = byte((a + b + c + d + 2) >> 2)
		}
	}
	return out
}

// TestARNRSubpelRefinementImprovesNoisyMatch verifies that when adjacent
// frames carry true half-pel motion relative to the alt-ref center, the
// subpel-refined sixtap predictor delivers lower SSE against the noiseless
// ground-truth center than the integer-only predictor. Both paths share
// the same hex search (full-pel MV); only the synthesized predictor
// differs.
func TestARNRSubpelRefinementImprovesNoisyMatch(t *testing.T) {
	const w, h = 64, 64
	// Build a textured ground-truth plane. The pattern has high spatial
	// frequency so the half-pel-shifted reference materially differs
	// from any integer-aligned position.
	truth := make([]byte, w*h)
	for y := range h {
		for x := range w {
			v := min(max(96+((x*37+y*53)&0x3f)-((x*y)&0x1f), 16), 239)
			truth[y*w+x] = byte(v)
		}
	}
	// Adjacent frames are half-pel shifted in both axes (no whole-pel
	// component) so the hex search converges on MV=(0,0) and only subpel
	// refinement can buy lower SAD.
	back := halfPelShiftedPlane(truth, w, h, 0, 0, true, true)
	fwd := halfPelShiftedPlane(truth, w, h, 0, 0, true, true)
	center := make([]byte, len(truth))
	copy(center, truth)

	// Run ARNR (subpel path).
	e := arnrTestEncoder(t, w, h, 3, 3, 3, back, [][]byte{fwd})
	cp := make([]byte, len(center))
	copy(cp, center)
	if !e.applyARNRFilter(syntheticSource(w, h, cp), 0) {
		t.Fatalf("applyARNRFilter returned false")
	}
	subpelOut := e.arnrScratch.Img.Y
	stride := e.arnrScratch.Img.YStride
	subpelVisible := make([]byte, w*h)
	for y := range h {
		copy(subpelVisible[y*w:(y+1)*w], subpelOut[y*stride:y*stride+w])
	}

	// Build the integer-only reference output. We replay the exact same
	// per-MB hex search the encoder ran (the integer-pel MV is what
	// goes into mvHistory) but synthesize predictors with gatherBlock,
	// matching the pre-subpel implementation.
	intOut := arnrIntegerOnlyReference(t, w, h, back, center, fwd, 3, 3)

	// Compute SSE vs the noiseless ground truth.
	sse := func(plane []byte) int64 {
		var s int64
		for i := range w * h {
			d := int64(plane[i]) - int64(truth[i])
			s += d * d
		}
		return s
	}
	subpelSSE := sse(subpelVisible)
	intSSE := sse(intOut)
	if subpelSSE >= intSSE {
		t.Fatalf("subpel ARNR did not improve PSNR vs integer-only: subpelSSE=%d intSSE=%d", subpelSSE, intSSE)
	}
}

// arnrIntegerOnlyReference reproduces the pre-subpel ARNR luma output for
// the same (back, center, fwd, strength, type=3, maxFrames=3) setup the
// production encoder runs. The hex search is identical (it operates at
// integer-pel resolution) but predictors are gathered at the integer MV
// rather than passed through the sixtap subpel filter, so the output
// reflects what ARNR produced before subpel refinement landed.
func arnrIntegerOnlyReference(t *testing.T, w, h int, back, center, fwd []byte, strength int, arnrType int) []byte {
	t.Helper()
	e := arnrTestEncoder(t, w, h, 3, strength, arnrType, back, [][]byte{fwd})
	// Mirror applyARNRFilter's setup (copy the center into the scratch,
	// then iterate). We then walk the same per-MB loop but force the
	// sixtap predictor down to gatherBlock by zeroing the subpel offset
	// after the hex search picks the integer MV.
	cp := make([]byte, len(center))
	copy(cp, center)
	src := syntheticSource(w, h, cp)
	copySourceToFrameBuffer(&e.arnrScratch, src)

	mbCols := (w + 15) >> 4
	mbRows := (h + 15) >> 4
	dst := arnrFrameView{
		width:   e.arnrScratch.Img.Width,
		height:  e.arnrScratch.Img.Height,
		y:       e.arnrScratch.Img.Y,
		u:       e.arnrScratch.Img.U,
		v:       e.arnrScratch.Img.V,
		yStride: e.arnrScratch.Img.YStride,
		uStride: e.arnrScratch.Img.UStride,
		vStride: e.arnrScratch.Img.VStride,
	}
	refs := []arnrFrameView{
		arnrViewFromImage(&e.arnrLastSource.Img),
		arnrViewFromSource(src),
		arnrViewFromImage(&e.lookahead[0].frame.Img),
	}
	const centerIdx = 1

	mvHistory := make([]arnrMV, len(refs)*mbRows*mbCols)
	var accumulator [384]uint32
	var count [384]uint32
	for mbRow := range mbRows {
		for mbCol := range mbCols {
			mbX := mbCol << 4
			mbY := mbRow << 4
			for i := range accumulator {
				accumulator[i] = 0
				count[i] = 0
			}
			var srcY [256]byte
			gatherBlock(srcY[:], 16, dst.y, dst.yStride, mbX, mbY, dst.width, dst.height, 16)
			mbHistory := mbRow*mbCols + mbCol
			for fi, ref := range refs {
				var filterWeight, mvX, mvY int
				if fi == centerIdx {
					filterWeight = 2
				} else {
					seed := arnrMV{}
					if fi > 0 {
						seed = mvHistory[(fi-1)*mbRows*mbCols+mbHistory]
					}
					err, sx, sy := arnrFindMatchingMB(srcY[:], 16, ref, mbRow, mbCol, mbRows, mbCols, mbX, mbY, seed.x, seed.y)
					mvX, mvY = sx, sy
					switch {
					case err < arnrThreshLow:
						filterWeight = 2
					case err < arnrThreshHigh:
						filterWeight = 1
					default:
						filterWeight = 0
					}
				}
				mvHistory[fi*mbRows*mbCols+mbHistory] = arnrMV{x: mvX, y: mvY}
				if filterWeight == 0 {
					continue
				}
				var predY [256]byte
				gatherBlock(predY[:], 16, ref.y, ref.yStride, mbX+mvX, mbY+mvY, ref.width, ref.height, 16)
				applyTemporalFilter(srcY[:], 16, predY[:], 16, strength, filterWeight, accumulator[:256], count[:256])
			}
			writeARNRBlock(dst.y, dst.yStride, mbX, mbY, dst.width, dst.height, 16, accumulator[:256], count[:256])
		}
	}

	out := make([]byte, w*h)
	for y := range h {
		copy(out[y*w:(y+1)*w], dst.y[y*dst.yStride:y*dst.yStride+w])
	}
	return out
}
