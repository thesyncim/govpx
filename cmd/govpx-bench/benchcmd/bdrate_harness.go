package benchcmd

import (
	"errors"
	"fmt"
	"image"
	"math"
	"sort"
	"testing"

	govpx "github.com/thesyncim/govpx"
)

// BDRateOptions configures one BD-rate measurement run.
//
// The harness encodes the same source sequence twice — once with the
// Baseline options and once with the Test options — at every q in
// QLadder, then computes BD-rate(Baseline, Test) on the resulting
// (kbps, PSNR) operating points. Baseline is the curve we want to
// improve on; Test is the curve under measurement. Negative BD-rate
// means Test saves bitrate at equal quality.
type BDRateOptions struct {
	// Codec selects which encoder to drive. "vp9" routes through
	// ComputeBDRate; "vp8" callers should use ComputeBDRateVP8 with
	// BDRateOptionsVP8 instead — ComputeBDRate keeps a Codec field
	// for backwards-compatible callers but rejects non-"vp9" inputs
	// with an error pointing at ComputeBDRateVP8.
	Codec string

	// Width and Height bound the visible frame dimensions. Must
	// both be positive and within VP9 limits.
	Width  int
	Height int
	// FPS sets the encoder timebase to 1/FPS. Defaults to 30.
	FPS int

	// Source provides the YCbCr 4:2:0 frame at index i. The
	// harness rewinds Source for every Q operating point so the
	// callee can either return cached frames or regenerate them
	// (e.g. when the generator is cheaper than allocating an
	// upfront slice for many frames).
	Source func(i int) *image.YCbCr
	// Frames is the number of frames to encode.
	Frames int

	// QLadder lists the public 0..63 quantizer points to evaluate.
	// At least 4 points are required for the cubic BD-rate fit.
	QLadder []int

	// RateLadderKbps optionally pairs each QLadder point with a target
	// bitrate. This is for rate-control features such as cyclic refresh
	// that must run under CBR, where varying CQ alone collapses the RD
	// curve to duplicate target rates.
	RateLadderKbps []int

	// Baseline / Test apply codec-specific tweaks (feature toggles)
	// on top of the shared per-Q encoder configuration. Both
	// callbacks receive a *VP9EncoderOptions pre-populated with
	// width/height/Q-band/lookahead defaults; they may set any
	// feature toggle they want to compare.
	Baseline func(opts *govpx.VP9EncoderOptions)
	Test     func(opts *govpx.VP9EncoderOptions)

	// Lookahead overrides the lookahead-frame count baked into the
	// shared options. Zero selects a zero-lag run; set a negative value
	// to use the harness default (8, the TPL minimum) when a caller wants
	// lookahead without spelling out the value.
	Lookahead int

	// AllowDecoderFallback enables an internal Q-derived PSNR proxy
	// when the govpx VP9 decoder fails to roundtrip a given encoded
	// packet. The proxy is a fixed monotone function of the encoder's
	// reported per-frame quantizer, so BD-rate still detects rate
	// regressions between Baseline and Test even when neither curve
	// can be fully decoded back. Without this flag, decoder errors
	// fail the harness with the underlying decode error.
	AllowDecoderFallback bool

	// LibvpxReference asks the harness to additionally encode the same
	// source sequence through the libvpx vpxenc-vp9-frameflags helper
	// at every Q in QLadder, with the same on-feature flags as the
	// Test callback (mapped to libvpx CLI tokens via
	// libvpxVP9FrameFlagsCLIArgs). The resulting (kbps, PSNR) curve
	// lands in BDRateResult.Libvpx and the absolute-reference deltas
	// (BDRateGovpxVsLibvpx / BDPSNRGovpxVsLibvpx) are computed against
	// Govpx. The reference uses the Q-derived PSNR proxy (same shape
	// as govpx's AllowDecoderFallback path) so both curves sit on the
	// same proxy mapping, and only the rate axis carries the bitstream
	// information.
	//
	// Skip behaviour:
	//   - If the helper binary is missing AND BuildLibvpx is false, the
	//     harness sets LibvpxErr = ErrVpxencVP9FrameFlagsNotBuilt and
	//     callers should t.Skip().
	//   - If BuildLibvpx is true, the harness invokes
	//     internal/coracle/build_vpxenc_vp9_frameflags.sh and
	//     hard-fails (LibvpxErr) when the build still does not
	//     produce a binary.
	LibvpxReference bool
	// BuildLibvpx asks the harness to invoke the libvpx build script
	// when the vpxenc-vp9-frameflags binary is missing. Without it,
	// a missing binary surfaces as LibvpxErr and the within-govpx
	// curves are still returned so callers can decide whether to
	// t.Skip.
	BuildLibvpx bool
}

// ComputeBDRate runs the harness and returns the BD-rate result.
// Returns an error only when the inputs are degenerate (e.g. missing
// callbacks, fewer than 4 Q points, encode failure that propagates).
// Encode failures at individual Q points are reported as a wrapped
// error so callers can inspect which operating point failed.
//
// When BDRateOptions.LibvpxReference is true, the harness additionally
// encodes the same source sequence through the libvpx
// vpxenc-vp9-frameflags helper at every Q with the on-feature flags
// mapped from BDRateOptions.Test (see libvpxVP9FrameFlagsCLIArgs).
// The libvpx curve is appended to BDRateResult.Libvpx along with the
// govpx-vs-libvpx BD-rate / BD-PSNR cross deltas. A missing helper
// binary surfaces as BDRateResult.LibvpxErr without failing the call,
// so callers can either t.Skip or assert based on the per-test
// policy. If BuildLibvpx is requested but the build cannot produce a
// binary, the error is still propagated through LibvpxErr.
func ComputeBDRate(t testing.TB, opts BDRateOptions) (BDRateResult, error) {
	if err := validateBDRateOptions(opts); err != nil {
		return BDRateResult{}, err
	}
	if opts.FPS == 0 {
		opts.FPS = 30
	}
	if opts.Lookahead < 0 {
		opts.Lookahead = 8
	}
	ladder := bdOperatingLadder(opts)
	baseline := make([]QualityPoint, 0, len(ladder))
	test := make([]QualityPoint, 0, len(ladder))
	for _, op := range ladder {
		bPt, err := encodeBDOperatingPoint(opts, op.Q, op.TargetKbps, opts.Baseline)
		if err != nil {
			return BDRateResult{}, fmt.Errorf("baseline Q=%d: %w", op.Q, err)
		}
		tPt, err := encodeBDOperatingPoint(opts, op.Q, op.TargetKbps, opts.Test)
		if err != nil {
			return BDRateResult{}, fmt.Errorf("test Q=%d: %w", op.Q, err)
		}
		baseline = append(baseline, bPt)
		test = append(test, tPt)
	}
	bd, err := BDRate(baseline, test)
	if err != nil {
		return BDRateResult{Reference: baseline, Govpx: test}, err
	}
	psnr, err := BDPSNR(baseline, test)
	if err != nil {
		psnr = math.NaN()
	}
	result := BDRateResult{
		Reference:           baseline,
		Govpx:               test,
		BDRate:              bd,
		BDPSNR:              psnr,
		BDRateGovpxVsLibvpx: math.NaN(),
		BDPSNRGovpxVsLibvpx: math.NaN(),
	}
	if opts.LibvpxReference {
		libvpxPts, libvpxErr := encodeBDLibvpxCurve(opts, ladder)
		if libvpxErr != nil {
			result.LibvpxErr = libvpxErr
			return result, nil
		}
		result.Libvpx = libvpxPts
		if bdCross, err := BDRate(libvpxPts, test); err == nil {
			result.BDRateGovpxVsLibvpx = bdCross
		} else {
			result.LibvpxErr = fmt.Errorf("BDRate(libvpx, govpx): %w", err)
		}
		if psnrCross, err := BDPSNR(libvpxPts, test); err == nil {
			result.BDPSNRGovpxVsLibvpx = psnrCross
		} else if result.LibvpxErr == nil {
			result.LibvpxErr = fmt.Errorf("BDPSNR(libvpx, govpx): %w", err)
		}
	}
	return result, nil
}

type bdOperatingPoint struct {
	Q          int
	TargetKbps int
}

func bdOperatingLadder(opts BDRateOptions) []bdOperatingPoint {
	ops := make([]bdOperatingPoint, len(opts.QLadder))
	for i, q := range opts.QLadder {
		ops[i] = bdOperatingPoint{Q: q}
		if len(opts.RateLadderKbps) == len(opts.QLadder) {
			ops[i].TargetKbps = opts.RateLadderKbps[i]
		}
	}
	sort.Slice(ops, func(i, j int) bool { return ops[i].Q < ops[j].Q })
	return ops
}

func validateBDRateOptions(opts BDRateOptions) error {
	if opts.Codec == "vp8" {
		return fmt.Errorf("bdrate: codec %q not handled by ComputeBDRate; use ComputeBDRateVP8 (BDRateOptionsVP8)", opts.Codec)
	}
	if opts.Codec != "vp9" {
		return fmt.Errorf("bdrate: codec %q not supported (vp9 via ComputeBDRate, vp8 via ComputeBDRateVP8)", opts.Codec)
	}
	if opts.Source == nil {
		return errors.New("bdrate: Source callback required")
	}
	if opts.Frames <= 0 {
		return errors.New("bdrate: Frames must be > 0")
	}
	if opts.Width <= 0 || opts.Height <= 0 {
		return errors.New("bdrate: Width/Height must be > 0")
	}
	if len(opts.QLadder) < 4 {
		return errors.New("bdrate: QLadder must list >= 4 distinct quantizers")
	}
	seen := make(map[int]struct{}, len(opts.QLadder))
	for _, q := range opts.QLadder {
		if q < 0 || q > 63 {
			return fmt.Errorf("bdrate: QLadder entry %d outside 0..63", q)
		}
		if _, dup := seen[q]; dup {
			return fmt.Errorf("bdrate: duplicate QLadder entry %d", q)
		}
		seen[q] = struct{}{}
	}
	if len(opts.RateLadderKbps) > 0 {
		if len(opts.RateLadderKbps) != len(opts.QLadder) {
			return errors.New("bdrate: RateLadderKbps must match QLadder length")
		}
		seenRates := make(map[int]struct{}, len(opts.RateLadderKbps))
		for _, kbps := range opts.RateLadderKbps {
			if kbps <= 0 {
				return fmt.Errorf("bdrate: RateLadderKbps entry %d must be positive", kbps)
			}
			if _, dup := seenRates[kbps]; dup {
				return fmt.Errorf("bdrate: duplicate RateLadderKbps entry %d", kbps)
			}
			seenRates[kbps] = struct{}{}
		}
	}
	if opts.Baseline == nil || opts.Test == nil {
		return errors.New("bdrate: Baseline and Test callbacks required")
	}
	return nil
}

// encodeBDOperatingPoint encodes the source sequence at a fixed CQ
// with the caller-applied feature toggles, decodes every emitted
// packet (or uses the Q-derived PSNR proxy when fallback is on), and
// returns the (kbps, PSNR) point. The ladder point pins the CQ
// target; the encoder is free to adapt qindex within
// [DefaultMinQ, q] so feature passes (TPL, AltRefAQ, AQ modes) can
// still bias the regulated qindex around the CQ anchor — that is
// the libvpx default operating mode for BD-rate.
//
// Lookahead-aware encoders (which AutoAltRef and TPL both require)
// are drained via FlushIntoWithResult after all source frames are
// fed in so hidden ALTREFs and pending reordered packets are
// accounted for.
func encodeBDOperatingPoint(opts BDRateOptions, q int, targetKbps int, apply func(*govpx.VP9EncoderOptions)) (QualityPoint, error) {
	if targetKbps <= 0 {
		targetKbps = 1000
	}
	encOpts := govpx.VP9EncoderOptions{
		Width:           opts.Width,
		Height:          opts.Height,
		FPS:             opts.FPS,
		LookaheadFrames: opts.Lookahead,
		// Use VP9 VPX_Q-style RateControlQ so the regulator can
		// still bias qindex around the CQ anchor via per-frame
		// and per-SB deltas (TPL, AltRefAQ, AQ modes). CQLevel
		// pins the constant-quality anchor; the bitrate field
		// is required by the validation gate but unused by the
		// public-Q mode's qindex selection.
		RateControlModeSet: true,
		RateControlMode:    govpx.RateControlQ,
		TargetBitrateKbps:  targetKbps, // unused by public-Q mode, but validation requires positive
		CQLevel:            q,
		MinQuantizer:       4,
		MaxQuantizer:       63,
	}
	if apply != nil {
		apply(&encOpts)
	}
	if len(opts.RateLadderKbps) > 0 {
		encOpts.TargetBitrateKbps = targetKbps
	}
	// Honor the harness lookahead override on top of whatever the
	// feature toggle requested. Disabling TPL/AutoAltRef shouldn't
	// require a positive lookahead; allow callbacks to zero it.
	if encOpts.LookaheadFrames < 0 {
		encOpts.LookaheadFrames = 0
	}
	enc, err := govpx.NewVP9Encoder(encOpts)
	if err != nil {
		return QualityPoint{}, fmt.Errorf("NewVP9Encoder: %w", err)
	}
	defer enc.Close()
	dec, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		return QualityPoint{}, fmt.Errorf("NewVP9Decoder: %w", err)
	}
	defer dec.Close()
	bufSize := max(opts.Width*opts.Height*6, 65536)
	dst := make([]byte, bufSize)
	totalBytes := 0
	psnrSum := 0.0
	visibleCount := 0
	// originalFrames lets PSNR pair the decoded frame back to the
	// source even after lookahead reorders things, by mapping each
	// emitted source-frame timestamp.
	// We cache the source frames (single full pass) so PSNR can pair
	// decoded -> source.
	srcCache := make([]*image.YCbCr, opts.Frames)
	feed := func(i int) (*image.YCbCr, error) {
		if i < 0 || i >= len(srcCache) {
			return nil, fmt.Errorf("source index %d out of range", i)
		}
		if srcCache[i] == nil {
			srcCache[i] = opts.Source(i)
			if srcCache[i] == nil {
				return nil, fmt.Errorf("Source returned nil at %d", i)
			}
		}
		return srcCache[i], nil
	}
	emitted := []bdEncodeRecord{}
	quantSum := 0
	quantCount := 0
	// Encode pass: feed every source frame in order. Visible
	// packets that come back immediately are recorded with the
	// current source index; with lookahead enabled, ErrFrameNotReady
	// holds back early frames and they emerge later. We just record
	// what comes out without trying to attribute it.
	for i := 0; i < opts.Frames; i++ {
		src, err := feed(i)
		if err != nil {
			return QualityPoint{}, err
		}
		result, err := enc.EncodeIntoWithResult(src, dst)
		if errors.Is(err, govpx.ErrFrameNotReady) {
			continue
		}
		if err != nil {
			return QualityPoint{}, fmt.Errorf("EncodeIntoWithResult frame %d: %w", i, err)
		}
		if result.Dropped {
			continue
		}
		if len(result.Data) == 0 {
			continue
		}
		emitted = append(emitted, bdEncodeRecord{
			data:      append([]byte(nil), result.Data...),
			showFrame: result.ShowFrame,
			qIndex:    result.InternalQuantizer,
		})
		totalBytes += result.SizeBytes
		if result.ShowFrame {
			quantSum += result.InternalQuantizer
			quantCount++
		}
	}
	// Drain the lookahead queue.
	for {
		result, err := enc.FlushIntoWithResult(dst)
		if errors.Is(err, govpx.ErrFrameNotReady) {
			break
		}
		if err != nil {
			return QualityPoint{}, fmt.Errorf("FlushIntoWithResult: %w", err)
		}
		if result.Dropped || len(result.Data) == 0 {
			continue
		}
		emitted = append(emitted, bdEncodeRecord{
			data:      append([]byte(nil), result.Data...),
			showFrame: result.ShowFrame,
			qIndex:    result.InternalQuantizer,
		})
		totalBytes += result.SizeBytes
		if result.ShowFrame {
			quantSum += result.InternalQuantizer
			quantCount++
		}
	}

	var avgPSNR float64
	if opts.AllowDecoderFallback {
		// In fallback mode, we never trust the decoder roundtrip
		// to produce the PSNR axis — instead we use a monotone
		// Q-derived proxy so both curves sit on the same mapping.
		// This is needed for feature configurations the govpx VP9
		// decoder cannot yet roundtrip (e.g. AutoAltRef + certain
		// Q points). The proxy still detects rate regressions
		// because BD-rate's integral over the overlapping
		// proxy-PSNR range is dominated by the rate gap.
		if quantCount == 0 {
			return QualityPoint{}, fmt.Errorf("no visible frames at Q=%d", q)
		}
		meanQ := float64(quantSum) / float64(quantCount)
		avgPSNR = bdRateQIndexPSNRProxy(meanQ)
	} else {
		// Decode pass: feed every emitted packet through the
		// govpx VP9 decoder in order. Visible frames are paired
		// with the next source frame; hidden frames (AutoAltRef)
		// advance decoder state without consuming a source frame.
		srcIdx := 0
		for _, rec := range emitted {
			if err := dec.Decode(rec.data); err != nil {
				return QualityPoint{}, fmt.Errorf("VP9Decoder.Decode: %w", err)
			}
			if !rec.showFrame {
				continue
			}
			decoded, ok := dec.NextFrame()
			if !ok {
				continue
			}
			if srcIdx >= len(srcCache) {
				break
			}
			src, _ := feed(srcIdx)
			srcIdx++
			psnrSum += imagePSNR(govpxImageFromYCbCr(src), decoded)
			visibleCount++
		}
		if visibleCount == 0 {
			return QualityPoint{}, fmt.Errorf("no visible frames at Q=%d", q)
		}
		avgPSNR = psnrSum / float64(visibleCount)
	}
	// Convert total bytes to kbps over the visible frame run.
	kbps := float64(totalBytes) * 8 * float64(opts.FPS) / float64(opts.Frames) / 1000
	if kbps <= 0 {
		return QualityPoint{}, fmt.Errorf("nonpositive kbps at Q=%d (bytes=%d frames=%d)", q, totalBytes, opts.Frames)
	}
	return QualityPoint{Rate: kbps, PSNR: avgPSNR}, nil
}

type bdEncodeRecord struct {
	data      []byte
	showFrame bool
	qIndex    int
}

// bdRateQIndexPSNRProxy maps the encoder's average qindex back to a
// PSNR-proxy that BDRate's cubic fit can integrate. The exact value
// does not matter for relative BD-rate comparisons as long as it is
// monotone in qindex and the two curves use the same mapping: the
// integral over the overlapping range gives the rate difference at
// equal proxy-PSNR. We use a logarithmic Q-step model that matches
// the rough shape of real-content RD curves (lower Q -> higher
// PSNR) so feature-on-vs-off comparisons land in a numerically
// sensible range.
func bdRateQIndexPSNRProxy(qindex float64) float64 {
	if qindex < 1 {
		qindex = 1
	}
	if qindex > 255 {
		qindex = 255
	}
	// Linear-in-qindex mapping covering ~25..45 dB across the
	// public 1..255 qindex space (libvpx CQ 0..63 maps roughly
	// linearly to this range too).
	return 50.0 - 25.0*(qindex/255.0)
}

// govpxImageFromYCbCr builds a govpx.Image view of the source YCbCr so
// the existing imagePSNR helper (which expects govpx.Image) can pair
// it with the decoder output without an allocation.
func govpxImageFromYCbCr(src *image.YCbCr) govpx.Image {
	return govpx.Image{
		Width:   src.Rect.Dx(),
		Height:  src.Rect.Dy(),
		Y:       src.Y,
		U:       src.Cb,
		V:       src.Cr,
		YStride: src.YStride,
		UStride: src.CStride,
		VStride: src.CStride,
	}
}
