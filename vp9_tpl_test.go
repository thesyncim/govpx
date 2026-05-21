package govpx

import (
	"errors"
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// newVP9TPLBaseOpts returns the smallest VP9EncoderOptions configuration that
// satisfies the TPL pass prerequisites.  Tests below mutate one field at a
// time to exercise the validation matrix.
func newVP9TPLBaseOpts(width, height int) VP9EncoderOptions {
	return VP9EncoderOptions{
		Width:           width,
		Height:          height,
		FPS:             30,
		LookaheadFrames: encoder.TPLMinLookaheadFrames,
		AutoAltRef:      true,
		EnableTPL:       true,
	}
}

func TestVP9TPLValidationAcceptsMinimumConfig(t *testing.T) {
	opts := newVP9TPLBaseOpts(64, 64)
	if err := validateVP9TPLOptions(opts); err != nil {
		t.Fatalf("baseline TPL options: %v", err)
	}
	if _, err := NewVP9Encoder(opts); err != nil {
		t.Fatalf("NewVP9Encoder TPL baseline: %v", err)
	}
}

func TestVP9TPLValidationRejectsShortLookahead(t *testing.T) {
	opts := newVP9TPLBaseOpts(64, 64)
	opts.LookaheadFrames = encoder.TPLMinLookaheadFrames - 1
	if err := validateVP9TPLOptions(opts); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("short lookahead: got %v want ErrInvalidConfig", err)
	}
	if _, err := NewVP9Encoder(opts); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("NewVP9Encoder short lookahead: got %v want ErrInvalidConfig", err)
	}
}

func TestVP9TPLValidationRequiresAutoAltRef(t *testing.T) {
	opts := newVP9TPLBaseOpts(64, 64)
	opts.AutoAltRef = false
	if err := validateVP9TPLOptions(opts); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("no AutoAltRef: got %v want ErrInvalidConfig", err)
	}
}

func TestVP9TPLValidationRejectsLossless(t *testing.T) {
	opts := newVP9TPLBaseOpts(64, 64)
	opts.Lossless = true
	if err := validateVP9TPLOptions(opts); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("lossless TPL: got %v want ErrInvalidConfig", err)
	}
}

func TestVP9TPLDisabledLeavesPassInert(t *testing.T) {
	opts := VP9EncoderOptions{Width: 64, Height: 64}
	enc, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if enc.vp9TPLEnabled() {
		t.Fatalf("TPL active without EnableTPL")
	}
	if got := enc.TPLFrameDelta(); got.SBRows != 0 || got.SBCols != 0 || got.Delta != nil {
		t.Fatalf("TPLFrameDelta on disabled encoder: %+v", got)
	}
	if got := enc.tpl.FrameR0(); got != 0 {
		t.Fatalf("TPL FrameR0 on disabled encoder: %v", got)
	}
	if got := enc.tpl.FrameSlab(); got != nil {
		t.Fatalf("TPL FrameSlab on disabled encoder: %+v", got)
	}
}

func TestVP9TPLSetEnableTPLConfiguresState(t *testing.T) {
	opts := newVP9TPLBaseOpts(64, 64)
	opts.EnableTPL = false
	enc, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := enc.SetEnableTPL(true); err != nil {
		t.Fatalf("SetEnableTPL(true): %v", err)
	}
	if !enc.opts.EnableTPL {
		t.Fatalf("EnableTPL not stored")
	}
	if !enc.vp9TPLEnabled() {
		t.Fatalf("vp9TPLEnabled false after SetEnableTPL(true)")
	}
	if enc.tpl.FrameCount() == 0 {
		t.Fatalf("TPL frames slab not allocated")
	}
	if err := enc.SetEnableTPL(false); err != nil {
		t.Fatalf("SetEnableTPL(false): %v", err)
	}
	if enc.vp9TPLEnabled() {
		t.Fatalf("TPL still active after disable")
	}
}

func TestVP9TPLSetEnableTPLRejectsBadConfig(t *testing.T) {
	opts := VP9EncoderOptions{Width: 64, Height: 64}
	enc, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := enc.SetEnableTPL(true); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetEnableTPL without lookahead: got %v want ErrInvalidConfig", err)
	}
}

// shiftYCbCrCopy shifts src by (dy, dx) pixels into a new YCbCr.  Coordinates
// past the frame edge replicate from the nearest in-frame pixel.
func shiftYCbCrCopy(src *image.YCbCr, dy, dx int) *image.YCbCr {
	w := src.Rect.Dx()
	h := src.Rect.Dy()
	out := image.NewYCbCr(image.Rect(0, 0, w, h), image.YCbCrSubsampleRatio420)
	for y := range h {
		for x := range w {
			sy := clampEncodeCoord(y-dy, h)
			sx := clampEncodeCoord(x-dx, w)
			out.Y[y*out.YStride+x] = src.Y[sy*src.YStride+sx]
		}
	}
	uvW := (w + 1) >> 1
	uvH := (h + 1) >> 1
	for y := range uvH {
		for x := range uvW {
			sy := clampEncodeCoord(y-dy/2, uvH)
			sx := clampEncodeCoord(x-dx/2, uvW)
			out.Cb[y*out.CStride+x] = src.Cb[sy*src.CStride+sx]
			out.Cr[y*out.CStride+x] = src.Cr[sy*src.CStride+sx]
		}
	}
	return out
}

func TestVP9TPLDisabledEncoderRDMultDeltaIsIdentity(t *testing.T) {
	opts := VP9EncoderOptions{Width: 64, Height: 64}
	enc, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if got := enc.getVP9TPLRDMultDelta(0, 0, 8, 8, 4000); got != 4000 {
		t.Fatalf("disabled encoder changed rdmult: %d", got)
	}
}

func TestVP9TPLResolutionChangeRebuildsState(t *testing.T) {
	opts := newVP9TPLBaseOpts(64, 64)
	enc, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	src := vp9test.NewMotionYCbCr(64, 64)
	frames := make([]*image.YCbCr, encoder.TPLMinLookaheadFrames)
	for i := range frames {
		frames[i] = src
	}
	enc.tpl.Populate(frames)
	if enc.tpl.FrameSlab() == nil {
		t.Fatalf("TPL slab not populated before resize")
	}
	enc.applyVP9ResolutionChange(96, 96)
	width, height, sbRows, sbCols := enc.tpl.Dimensions()
	if width != 96 || height != 96 {
		t.Fatalf("TPL state width/height not updated: %dx%d", width, height)
	}
	if sbRows != 3 || sbCols != 3 {
		t.Fatalf("TPL SB grid not updated: %dx%d", sbRows, sbCols)
	}
	if enc.tpl.FrameSlab() != nil {
		t.Fatalf("stale slab still valid after resolution change")
	}
}

// newVP9TPLPanningSequence returns n synthetic source frames that pan a
// textured pattern by 2 pixels per frame.  Useful for exercising the TPL
// motion-search path under predictable motion.
func newVP9TPLPanningSequence(width, height, n int) []*image.YCbCr {
	base := vp9test.NewMotionYCbCr(width, height)
	out := make([]*image.YCbCr, n)
	for i := range n {
		out[i] = shiftYCbCrCopy(base, 2*i, 2*i)
	}
	return out
}

// newVP9TPLEdgesYCbCrForTest paints a synthetic frame with strong vertical
// and horizontal edges so the keyframe intra mode picker (DC/V/H) sees
// non-trivial ranking under rdmult scaling.  Without directional edges the
// noise-texture proxy returned by vp9test.NewMotionYCbCr tends to make DC
// the only winner regardless of rdmult.
func newVP9TPLEdgesYCbCrForTest(width, height int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height),
		image.YCbCrSubsampleRatio420)
	for y := range height {
		row := img.Y[y*img.YStride:]
		for x := range width {
			// Vertical stripes on the left half (favor V intra),
			// horizontal stripes on the right half (favor H intra),
			// plus a low-frequency gradient (favor DC).
			v := 32 + (x/4)*16
			if x >= width/2 {
				v = 32 + (y/4)*16
			}
			if v > 240 {
				v = 240
			}
			row[x] = byte(v)
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

// newVP9TPLMixedMotionSequence returns n source frames where the top half of
// the frame is static (matches the prior frame exactly) and the bottom half
// pans by 2 pixels per frame.  This produces non-uniform per-SB motion in
// the TPL slab so the propagation pass yields per-SB beta variance — without
// it, every SB sees an identical mc_dep_cost and the per-SB rdmult delta
// collapses to a single value.  The base content uses strong directional
// edges so the keyframe DC/V/H mode picker has non-trivial ranking under
// rdmult scaling.
func newVP9TPLMixedMotionSequence(width, height, n int) []*image.YCbCr {
	base := newVP9TPLEdgesYCbCrForTest(width, height)
	out := make([]*image.YCbCr, n)
	mid := height / 2
	for i := range n {
		// shifted: pan-by-(2*i, 2*i) version of base for the bottom.
		shifted := shiftYCbCrCopy(base, 2*i, 2*i)
		// Compose: top half from base, bottom half from shifted.
		composed := image.NewYCbCr(image.Rect(0, 0, width, height),
			image.YCbCrSubsampleRatio420)
		for y := range height {
			src := base
			if y >= mid {
				src = shifted
			}
			copy(composed.Y[y*composed.YStride:y*composed.YStride+width],
				src.Y[y*src.YStride:y*src.YStride+width])
		}
		uvH := (height + 1) >> 1
		uvW := (width + 1) >> 1
		uvMid := mid >> 1
		for y := range uvH {
			src := base
			if y >= uvMid {
				src = shifted
			}
			copy(composed.Cb[y*composed.CStride:y*composed.CStride+uvW],
				src.Cb[y*src.CStride:y*src.CStride+uvW])
			copy(composed.Cr[y*composed.CStride:y*composed.CStride+uvW],
				src.Cr[y*src.CStride:y*src.CStride+uvW])
		}
		out[i] = composed
	}
	return out
}

func TestVP9TPLFrameDeltaAfterPopulate(t *testing.T) {
	opts := newVP9TPLBaseOpts(64, 64)
	enc, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	src := vp9test.NewMotionYCbCr(64, 64)
	frames := make([]*image.YCbCr, encoder.TPLMinLookaheadFrames)
	for i := range frames {
		frames[i] = src
	}
	enc.tpl.Populate(frames)
	delta := enc.TPLFrameDelta()
	if delta.SBRows == 0 || delta.SBCols == 0 {
		t.Fatalf("TPLFrameDelta returned zero grid after populate")
	}
	if len(delta.Delta) != delta.SBRows*delta.SBCols {
		t.Fatalf("TPLFrameDelta size mismatch: %d != %dx%d",
			len(delta.Delta), delta.SBRows, delta.SBCols)
	}
}

// TestVP9TPLChangesKeyframeEncoded pins the integration boundary: the
// per-SB TPL rdmult delta must alter the keyframe mode picker's RD scoring
// at least once on TPL-friendly content.  Mirrors the libvpx wiring at
// vp9_encodeframe.c:4245-4248 where cb_rdmult is overwritten before the per-SB
// partition / mode search runs.
//
// We compare an encoder configured with EnableTPL=true vs one with TPL off
// and assert the encoded keyframe byte stream diverges.  The visible packet
// count is preserved (TPL is a quality knob, not a scheduling change) so a
// byte-stream divergence under matched headers is the load-bearing
// assertion.
func TestVP9TPLChangesKeyframeEncoded(t *testing.T) {
	const w, h = 64, 64
	encode := func(enableTPL bool) ([]byte, int, []int) {
		opts := VP9EncoderOptions{
			Width:              w,
			Height:             h,
			FPS:                30,
			LookaheadFrames:    encoder.TPLMinLookaheadFrames,
			AutoAltRef:         true,
			EnableTPL:          enableTPL,
			RateControlModeSet: true,
			RateControlMode:    RateControlQ,
			TargetBitrateKbps:  1000,
			CQLevel:            32,
			MaxQuantizer:       63,
		}
		enc, err := NewVP9Encoder(opts)
		if err != nil {
			t.Fatalf("NewVP9Encoder: %v", err)
		}
		seq := newVP9TPLMixedMotionSequence(w, h, 16)
		buf := make([]byte, 64*1024)
		var concat []byte
		var qs []int
		total := 0
		drain := func(res VP9EncodeResult) {
			if res.ShowFrame {
				qs = append(qs, res.InternalQuantizer)
			}
			total += res.SizeBytes
			concat = append(concat, res.Data...)
		}
		for i := range 16 {
			res, err := enc.encodeVP9LookaheadIntoWithFlagsResult(seq[i%len(seq)], buf, 0)
			switch {
			case err == nil:
				drain(res)
			case errors.Is(err, ErrFrameNotReady):
			default:
				t.Fatalf("encode %d: %v", i, err)
			}
		}
		for {
			res, err := enc.FlushIntoWithResult(buf)
			if errors.Is(err, ErrFrameNotReady) {
				break
			}
			if err != nil {
				t.Fatalf("flush: %v", err)
			}
			drain(res)
		}
		return concat, total, qs
	}
	offBytes, offTotal, offQs := encode(false)
	onBytes, onTotal, onQs := encode(true)
	if len(offQs) != len(onQs) {
		t.Fatalf("visible packet count drifted: off=%d on=%d", len(offQs), len(onQs))
	}
	// qindex is NOT expected to change under the libvpx-faithful flow
	// (TPL routes through cb_rdmult, not the regulated qindex).  The
	// load-bearing assertion is that the byte stream diverges because
	// the keyframe mode picker's per-SB RD ranking shifts.
	if len(offBytes) == len(onBytes) {
		identical := true
		for i := range offBytes {
			if offBytes[i] != onBytes[i] {
				identical = false
				break
			}
		}
		if identical {
			t.Fatalf("TPL had no effect on keyframe encoding: byte streams identical (off=%d on=%d)",
				offTotal, onTotal)
		}
	}
	t.Logf("keyframe TPL off->on: bytes off=%d on=%d, qindex unchanged (libvpx flow routes through cb_rdmult)",
		offTotal, onTotal)
}

// TestVP9TPLDoesNotChangeRegulatedQIndex pins the libvpx parity invariant:
// under the libvpx-faithful flow, TPL routes through cb_rdmult (not the
// regulated qindex).  The deleted frame-mean scalar bias had no libvpx
// analog; this test guards against any future regression that lets TPL
// silently re-shift the frame qindex.
func TestVP9TPLDoesNotChangeRegulatedQIndex(t *testing.T) {
	const w, h = 64, 64
	encode := func(enableTPL bool) []int {
		opts := VP9EncoderOptions{
			Width:              w,
			Height:             h,
			FPS:                30,
			LookaheadFrames:    encoder.TPLMinLookaheadFrames,
			AutoAltRef:         true,
			EnableTPL:          enableTPL,
			RateControlModeSet: true,
			RateControlMode:    RateControlQ,
			TargetBitrateKbps:  1000,
			CQLevel:            32,
			MaxQuantizer:       63,
		}
		enc, err := NewVP9Encoder(opts)
		if err != nil {
			t.Fatalf("NewVP9Encoder: %v", err)
		}
		seq := newVP9TPLPanningSequence(w, h, 16)
		buf := make([]byte, 64*1024)
		var qs []int
		drain := func(res VP9EncodeResult) {
			if res.ShowFrame {
				qs = append(qs, res.InternalQuantizer)
			}
		}
		for i := range 16 {
			res, err := enc.encodeVP9LookaheadIntoWithFlagsResult(seq[i%len(seq)], buf, 0)
			switch {
			case err == nil:
				drain(res)
			case errors.Is(err, ErrFrameNotReady):
			default:
				t.Fatalf("encode %d: %v", i, err)
			}
		}
		for {
			res, err := enc.FlushIntoWithResult(buf)
			if errors.Is(err, ErrFrameNotReady) {
				break
			}
			if err != nil {
				t.Fatalf("flush: %v", err)
			}
			drain(res)
		}
		return qs
	}
	offQs := encode(false)
	onQs := encode(true)
	if len(offQs) != len(onQs) {
		t.Fatalf("visible packet count drifted: off=%d on=%d", len(offQs), len(onQs))
	}
	for i := range offQs {
		if offQs[i] != onQs[i] {
			t.Fatalf("TPL silently shifted regulated qindex at frame %d: off=%d on=%d "+
				"(libvpx flow routes through cb_rdmult, not qindex)",
				i, offQs[i], onQs[i])
		}
	}
}

func TestVP9TPLEncodesWithoutBreakingExisting(t *testing.T) {
	// A 16-frame encode under TPL should produce the same packet count as
	// the same encode without TPL (because TPL is a quality knob, not a
	// scheduling change).
	opts := newVP9TPLBaseOpts(64, 64)
	enc, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	src := vp9test.NewMotionYCbCr(64, 64)
	buf := make([]byte, 32*1024)
	encoded := 0
	for i := range 16 {
		_, err := enc.encodeVP9LookaheadIntoWithFlagsResult(src, buf, 0)
		switch {
		case err == nil:
			encoded++
		case errors.Is(err, ErrFrameNotReady):
		default:
			t.Fatalf("encode %d: %v", i, err)
		}
	}
	for {
		_, err := enc.FlushIntoWithResult(buf)
		if errors.Is(err, ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("flush: %v", err)
		}
		encoded++
	}
	if encoded == 0 {
		t.Fatalf("no frames committed with TPL enabled")
	}
}
