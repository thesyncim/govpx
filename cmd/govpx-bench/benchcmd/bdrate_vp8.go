package benchcmd

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"testing"

	govpx "github.com/thesyncim/govpx"
)

// BDRateOptionsVP8 configures one VP8 BD-rate measurement run.
//
// Parallel to BDRateOptions but typed for the VP8 encoder. The harness
// encodes the same source sequence twice — once with Baseline and once
// with Test — at every (Q, target_bitrate_kbps) operating point in the
// ladder, then computes BD-rate(Baseline, Test) on the resulting
// (kbps, PSNR) operating points. Baseline is the curve we want to
// improve on; Test is the curve under measurement. Negative BD-rate
// means Test saves bitrate at equal quality.
//
// Unlike the VP9 harness, VP8 BD-rate uses real decoded PSNR (not the
// Q-derived proxy) for both govpx and libvpx because (a) the govpx VP8
// decoder roundtrips its encoder, and (b) the stock vpxenc binary
// emits a real average PSNR via --psnr that we can parse from stderr.
// This makes the gate stricter than the VP9 proxy path.
type BDRateOptionsVP8 struct {
	// Width and Height bound the visible frame dimensions. Both must
	// be positive and within VP8 limits.
	Width  int
	Height int
	// FPS sets the encoder timebase to 1/FPS. Defaults to 30.
	FPS int

	// Source provides the YCbCr 4:2:0 frame at index i. The harness
	// rewinds Source for every operating point so the callee can
	// either return cached frames or regenerate them.
	Source func(i int) *image.YCbCr
	// Frames is the number of frames to encode.
	Frames int

	// QLadder lists the public 0..63 quantizer points to evaluate. At
	// least 4 distinct points are required for the cubic BD-rate fit.
	// QLadder seeds MinQuantizer/MaxQuantizer/CQLevel; the actual
	// bitrate axis is driven by RateLadderKbps when set.
	QLadder []int

	// RateLadderKbps optionally pairs each QLadder point with a target
	// bitrate, for CBR ladders where the actual axis is bitrate (not
	// CQ). When set, the harness drives RateControlCBR with this
	// per-point target; otherwise RateControlQ with CQLevel.
	RateLadderKbps []int

	// Baseline / Test apply codec-specific tweaks (feature toggles)
	// on top of the shared per-Q encoder configuration. Both callbacks
	// receive an *EncoderOptions pre-populated with width/height/Q-band
	// defaults; they may set any feature toggle they want to compare.
	Baseline func(opts *govpx.EncoderOptions)
	Test     func(opts *govpx.EncoderOptions)

	// LibvpxReference asks the harness to additionally encode the same
	// source sequence through the stock libvpx vpxenc binary at every
	// ladder point with --codec=vp8 --psnr, parse the resulting PSNR
	// from stderr, and compute the absolute govpx-vs-libvpx BD-rate /
	// BD-PSNR deltas. Without it the harness only returns the
	// within-govpx baseline-vs-test curves.
	LibvpxReference bool

	// LibvpxVpxenc is the path to the libvpx vpxenc binary. When
	// empty, the harness probes the project-relative
	// internal/coracle/build/vpxenc location and falls back to
	// LookPath("vpxenc") on PATH. If the binary is still not
	// resolvable and LibvpxReference is true, LibvpxErr is set to
	// errVpxencVP8NotFound and the harness returns only the
	// within-govpx curves.
	LibvpxVpxenc string

	// RateControlOverride pins the rate-control mode applied to both
	// the govpx and the libvpx sides of the BD-rate run. When zero
	// (the default) the harness picks RateControlCBR when
	// RateLadderKbps is set and RateControlQ otherwise (matching the
	// original behavior). Callers that want VBR (or CQ) wire it here
	// — the harness can't distinguish a zero-value RateControlVBR set
	// inside a Test callback from a fully unset RateControlMode, so
	// the override channel is the only way to drive VBR on both sides
	// of the comparison.
	RateControlOverride govpx.RateControlMode
	// RateControlOverrideSet must be true when RateControlOverride is
	// authoritative (including the zero value, RateControlVBR). When
	// false the harness picks the historical default.
	RateControlOverrideSet bool

	// TwoPass selects the libvpx two-pass VBR planning path for both
	// the govpx and libvpx sides of the BD-rate run. When set:
	//   - The harness sweeps the source frames once through govpx
	//     CollectFirstPassStats, finalizes the stats, and passes the
	//     resulting slice through TwoPassStats on every Baseline/Test
	//     EncoderOptions before the per-Q encode pass runs.
	//   - The libvpx CLI is invoked with --passes=2 in two stages
	//     (--pass=1 to populate fpf, then --pass=2 to read it back),
	//     mirroring the two-pass workflow vpxenc itself runs when
	//     given --passes=2 without an explicit --pass.
	// TwoPass forces end-usage=vbr on both sides (libvpx two-pass is
	// only meaningful for VBR planning) — callers should not also set
	// RateControlOverride to a non-VBR mode.
	TwoPass bool

	// LibvpxOracleRuns optionally re-invokes the libvpx oracle this many
	// times per ladder point and uses the per-axis median (rate, PSNR)
	// as the canonical operating point. Default 0/1 means a single
	// invocation. Use >= 3 (odd, so the median is a real sample) for
	// known-nondeterministic libvpx configurations like
	// `--rt --cpu-used=>0`, where vp8_auto_select_speed (rdopt.c:261)
	// drives cpi->Speed off wall-clock measurements and produces
	// run-to-run variance in both rate and PSNR for the same source.
	//
	// The median is taken per-axis (rate and PSNR independently sorted),
	// not as a paired point. This is intentional: the libvpx side is
	// the reference distribution we're characterizing, not a paired
	// sample. For ladders that *are* deterministic, leaving this at 0
	// preserves the previous single-run behavior (zero cost).
	//
	// Task #367 enabled this for the RT cpu_used=8 BD-rate fixture
	// after #357 widened its gate to +20.0% / -1.2 dB to absorb a
	// 42.86 -> 44.48 dB run-to-run PSNR-Y spread driven entirely by
	// libvpx's RT auto-speed wall-clock dependency. With N=3 median
	// the spread collapses and the gate retightens toward the
	// post-#341/#342 +10%/-1.0 dB envelope.
	LibvpxOracleRuns int
}

// ComputeBDRateVP8 runs the VP8 BD-rate harness and returns the result.
// Returns an error only when the inputs are degenerate (missing
// callbacks, fewer than 4 Q points, encode failure that propagates).
// When BDRateOptionsVP8.LibvpxReference is true, the harness also
// encodes the same source through stock vpxenc and computes the
// absolute govpx-vs-libvpx BD-rate / BD-PSNR. A missing vpxenc binary
// surfaces as BDRateResult.LibvpxErr without failing the call.
func ComputeBDRateVP8(t testing.TB, opts BDRateOptionsVP8) (BDRateResult, error) {
	if err := validateBDRateOptionsVP8(opts); err != nil {
		return BDRateResult{}, err
	}
	if opts.FPS == 0 {
		opts.FPS = 30
	}
	// Two-pass: pre-compute the govpx first-pass stats once so the
	// per-Q encode pass can pin TwoPassStats on every EncoderOptions
	// without re-running pass 1. The stats are content-only (a govpx
	// CollectFirstPassStats sweep) so they're independent of the
	// per-Q ladder point and reusable across all Baseline/Test calls.
	baselineApply := opts.Baseline
	testApply := opts.Test
	if opts.TwoPass {
		stats, err := captureGovpxVP8FirstPassStats(opts)
		if err != nil {
			return BDRateResult{}, fmt.Errorf("vp8 two-pass first-pass capture: %w", err)
		}
		// Force VBR on both sides — libvpx two-pass is a VBR-planning
		// pipeline; CBR/CQ paths do not consume the fpf.
		opts.RateControlOverride = govpx.RateControlVBR
		opts.RateControlOverrideSet = true
		// Wrap the caller-supplied callbacks so TwoPassStats lands on
		// every EncoderOptions before the harness builds the encoder.
		baselineApply = func(o *govpx.EncoderOptions) {
			if opts.Baseline != nil {
				opts.Baseline(o)
			}
			o.TwoPassStats = stats
		}
		testApply = func(o *govpx.EncoderOptions) {
			if opts.Test != nil {
				opts.Test(o)
			}
			o.TwoPassStats = stats
		}
	}
	ladder := bdOperatingLadderVP8(opts)
	baseline := make([]QualityPoint, 0, len(ladder))
	test := make([]QualityPoint, 0, len(ladder))
	for _, op := range ladder {
		bPt, err := encodeBDOperatingPointVP8(opts, op.Q, op.TargetKbps, baselineApply)
		if err != nil {
			return BDRateResult{}, fmt.Errorf("vp8 baseline Q=%d kbps=%d: %w", op.Q, op.TargetKbps, err)
		}
		tPt, err := encodeBDOperatingPointVP8(opts, op.Q, op.TargetKbps, testApply)
		if err != nil {
			return BDRateResult{}, fmt.Errorf("vp8 test Q=%d kbps=%d: %w", op.Q, op.TargetKbps, err)
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
		libvpxPts, libvpxErr := encodeBDLibvpxVP8Curve(opts, ladder)
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

func validateBDRateOptionsVP8(opts BDRateOptionsVP8) error {
	if opts.Source == nil {
		return errors.New("bdrate vp8: Source callback required")
	}
	if opts.Frames <= 0 {
		return errors.New("bdrate vp8: Frames must be > 0")
	}
	if opts.Width <= 0 || opts.Height <= 0 {
		return errors.New("bdrate vp8: Width/Height must be > 0")
	}
	if len(opts.QLadder) < 4 {
		return errors.New("bdrate vp8: QLadder must list >= 4 distinct quantizers")
	}
	seen := make(map[int]struct{}, len(opts.QLadder))
	for _, q := range opts.QLadder {
		if q < 0 || q > 63 {
			return fmt.Errorf("bdrate vp8: QLadder entry %d outside 0..63", q)
		}
		if _, dup := seen[q]; dup {
			return fmt.Errorf("bdrate vp8: duplicate QLadder entry %d", q)
		}
		seen[q] = struct{}{}
	}
	if len(opts.RateLadderKbps) > 0 {
		if len(opts.RateLadderKbps) != len(opts.QLadder) {
			return errors.New("bdrate vp8: RateLadderKbps must match QLadder length")
		}
		seenRates := make(map[int]struct{}, len(opts.RateLadderKbps))
		for _, kbps := range opts.RateLadderKbps {
			if kbps <= 0 {
				return fmt.Errorf("bdrate vp8: RateLadderKbps entry %d must be positive", kbps)
			}
			if _, dup := seenRates[kbps]; dup {
				return fmt.Errorf("bdrate vp8: duplicate RateLadderKbps entry %d", kbps)
			}
			seenRates[kbps] = struct{}{}
		}
	}
	if opts.Baseline == nil || opts.Test == nil {
		return errors.New("bdrate vp8: Baseline and Test callbacks required")
	}
	return nil
}

func bdOperatingLadderVP8(opts BDRateOptionsVP8) []bdOperatingPoint {
	ops := make([]bdOperatingPoint, len(opts.QLadder))
	for i, q := range opts.QLadder {
		ops[i] = bdOperatingPoint{Q: q}
		if len(opts.RateLadderKbps) == len(opts.QLadder) {
			ops[i].TargetKbps = opts.RateLadderKbps[i]
		}
	}
	if len(opts.RateLadderKbps) > 0 {
		// CBR ladder: sort by ascending target bitrate so the
		// BD-rate curve fit sees the curve in monotonic rate order.
		sort.Slice(ops, func(i, j int) bool { return ops[i].TargetKbps < ops[j].TargetKbps })
	} else {
		// Public-Q ladder: sort by ascending Q (i.e. descending
		// quality / ascending rate). VP8 BD-rate cubic fit operates
		// on (rate, psnr) so the inner sort is delegated to
		// bdMetric; this is just for stable ordering during the
		// encode pass.
		sort.Slice(ops, func(i, j int) bool { return ops[i].Q < ops[j].Q })
	}
	return ops
}

// encodeBDOperatingPointVP8 encodes the source sequence at one ladder
// point through the govpx VP8 encoder, decodes every emitted packet
// through the govpx VP8 decoder, and returns the (kbps, PSNR) point.
// PSNR is taken from the real decoder roundtrip — no proxy — because
// govpx VP8 is byte-exact against libvpx and the decoder reproduces
// the encoder's output.
func encodeBDOperatingPointVP8(opts BDRateOptionsVP8, q int, targetKbps int, apply func(*govpx.EncoderOptions)) (QualityPoint, error) {
	encOpts := govpx.EncoderOptions{
		Width:             opts.Width,
		Height:            opts.Height,
		FPS:               opts.FPS,
		TargetBitrateKbps: targetKbps,
		Deadline:          govpx.DeadlineGoodQuality,
		MinQuantizer:      4,
		MaxQuantizer:      63,
		QuantizerRangeSet: true,
		CQLevel:           q,
	}
	if opts.RateControlOverrideSet {
		encOpts.RateControlMode = opts.RateControlOverride
		if encOpts.TargetBitrateKbps <= 0 {
			encOpts.TargetBitrateKbps = 1000
		}
	} else if len(opts.RateLadderKbps) > 0 {
		// CBR ladder: pin the rate axis explicitly.
		encOpts.RateControlMode = govpx.RateControlCBR
		if encOpts.TargetBitrateKbps <= 0 {
			encOpts.TargetBitrateKbps = 1000
		}
	} else {
		// Pure-Q ladder: libvpx VPX_Q mode pins the quantizer; the
		// bitrate field is validation ballast.
		encOpts.RateControlMode = govpx.RateControlQ
		if encOpts.TargetBitrateKbps <= 0 {
			encOpts.TargetBitrateKbps = 1000
		}
	}
	if apply != nil {
		apply(&encOpts)
	}
	enc, err := govpx.NewVP8Encoder(encOpts)
	if err != nil {
		return QualityPoint{}, fmt.Errorf("NewVP8Encoder: %w", err)
	}
	defer enc.Close()
	dec, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
	if err != nil {
		return QualityPoint{}, fmt.Errorf("NewVP8Decoder: %w", err)
	}
	defer dec.Close()
	bufSize := max(opts.Width*opts.Height*6, 65536)
	dst := make([]byte, bufSize)
	totalBytes := 0
	psnrSum := 0.0
	visibleCount := 0
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
	// packetRecord buffers one emitted VP8 packet plus the
	// pairing metadata the per-frame PSNR pass needs. resultPTS is the
	// encoder-echoed PTS (== the input source index govpx passes through
	// EncodeInto as `uint64(i)`); show is the VP8 frame-tag show_frame
	// bit from PeekVP8StreamInfo. Hidden packets (show=false) carry the
	// alt-ref's source PTS (a future frame in the lookahead) while the
	// matching deferred visible packet carries the original input PTS,
	// so pairing on the resultPTS-of-visible-packets-only is robust to
	// the alt-ref scheduler's hidden/visible interleaving (libvpx
	// vp8/encoder/onyx_if.c vp8_get_compressed_data hidden ARF -> show
	// frame fallback; analogous to VPX_FRAME_IS_INVISIBLE on the
	// vpxenc CLI side).
	type packetRecord struct {
		data      []byte
		resultPTS uint64
		dropped   bool
		show      bool
	}
	emitted := []packetRecord{}
	for i := 0; i < opts.Frames; i++ {
		src, err := feed(i)
		if err != nil {
			return QualityPoint{}, err
		}
		result, err := enc.EncodeInto(dst, govpxImageFromYCbCrVP8(src), uint64(i), 1, 0)
		if errors.Is(err, govpx.ErrFrameNotReady) {
			continue
		}
		if err != nil {
			return QualityPoint{}, fmt.Errorf("EncodeInto frame %d: %w", i, err)
		}
		if result.Dropped {
			emitted = append(emitted, packetRecord{resultPTS: uint64(i), dropped: true})
			totalBytes += result.SizeBytes
			continue
		}
		if len(result.Data) == 0 {
			continue
		}
		// Peek the VP8 frame tag to extract show_frame without paying
		// the full decode cost. Hidden alt-ref packets (show_frame=0)
		// contribute bytes to the rate axis (they're carried in the
		// IVF) but are intentionally excluded from PSNR pairing
		// because the decoder produces no visible image for them.
		info, infoErr := govpx.PeekVP8StreamInfo(result.Data)
		if infoErr != nil {
			return QualityPoint{}, fmt.Errorf("PeekVP8StreamInfo frame %d: %w", i, infoErr)
		}
		emitted = append(emitted, packetRecord{
			data:      append([]byte(nil), result.Data...),
			resultPTS: result.PTS,
			show:      info.ShowFrame,
		})
		totalBytes += result.SizeBytes
	}
	// Drain any lookahead. With LookaheadFrames+AutoAltRef the flush
	// loop produces hidden ARF packets and their matching deferred
	// visible packets interleaved; the PeekVP8StreamInfo pass below
	// keeps hidden bytes in the rate count and pairs only visible
	// packets back to their source PTS.
	for {
		result, err := enc.FlushInto(dst)
		if errors.Is(err, govpx.ErrFrameNotReady) {
			break
		}
		if err != nil {
			return QualityPoint{}, fmt.Errorf("FlushInto: %w", err)
		}
		if result.Dropped || len(result.Data) == 0 {
			continue
		}
		info, infoErr := govpx.PeekVP8StreamInfo(result.Data)
		if infoErr != nil {
			return QualityPoint{}, fmt.Errorf("PeekVP8StreamInfo flush pts=%d: %w", result.PTS, infoErr)
		}
		emitted = append(emitted, packetRecord{
			data:      append([]byte(nil), result.Data...),
			resultPTS: result.PTS,
			show:      info.ShowFrame,
		})
		totalBytes += result.SizeBytes
	}
	for _, rec := range emitted {
		if rec.dropped {
			continue
		}
		// Hidden packets must still be fed through the decoder so its
		// reference buffers stay in sync for the next visible packet,
		// but they contribute neither a NextFrame image nor a PSNR
		// sample (mirrors VPX_FRAME_IS_INVISIBLE handling on the
		// libvpx oracle side, which counts the hidden packet's bytes
		// toward the rate axis but does not pair it for PSNR).
		if err := dec.Decode(rec.data); err != nil {
			return QualityPoint{}, fmt.Errorf("VP8Decoder.Decode: %w", err)
		}
		if !rec.show {
			// Drain any speculative NextFrame state so subsequent
			// visible packets see a clean queue.
			dec.NextFrame()
			continue
		}
		decoded, ok := dec.NextFrame()
		if !ok {
			continue
		}
		// Pair on the encoder-echoed PTS. govpx passes `uint64(i)` as
		// the per-frame PTS on EncodeInto, and the encoder echoes it
		// on both EncodeInto and FlushInto outputs (encoder_frame.go:
		// result.PTS = pts). For visible packets this is the original
		// input source index even when the alt-ref scheduler defers
		// the show frame, so the pairing survives hidden/visible
		// interleaving.
		srcIndex := int(rec.resultPTS)
		if srcIndex < 0 || srcIndex >= len(srcCache) {
			continue
		}
		src, _ := feed(srcIndex)
		psnrSum += imagePSNR(govpxImageFromYCbCrVP8(src), decoded)
		visibleCount++
	}
	if visibleCount == 0 {
		return QualityPoint{}, fmt.Errorf("no visible frames at Q=%d kbps=%d", q, targetKbps)
	}
	kbps := float64(totalBytes) * 8 * float64(opts.FPS) / float64(opts.Frames) / 1000
	if kbps <= 0 {
		return QualityPoint{}, fmt.Errorf("nonpositive kbps at Q=%d (bytes=%d frames=%d)", q, totalBytes, opts.Frames)
	}
	return QualityPoint{Rate: kbps, PSNR: psnrSum / float64(visibleCount)}, nil
}

// captureGovpxVP8FirstPassStats runs the govpx VP8 encoder once over
// every source frame collecting per-frame first-pass stats and returns
// the finalized slice ready for TwoPassStats. The first pass is
// content-only (no Q dependency) so the result is reused across every
// Baseline/Test encode call in the ladder.
func captureGovpxVP8FirstPassStats(opts BDRateOptionsVP8) ([]govpx.FirstPassFrameStats, error) {
	encOpts := govpx.EncoderOptions{
		Width:           opts.Width,
		Height:          opts.Height,
		FPS:             opts.FPS,
		Deadline:        govpx.DeadlineGoodQuality,
		MinQuantizer:    4,
		MaxQuantizer:    63,
		RateControlMode: govpx.RateControlVBR,
		// TargetBitrate must be positive for validation; first-pass
		// collection ignores it.
		TargetBitrateKbps: 1000,
	}
	enc, err := govpx.NewVP8Encoder(encOpts)
	if err != nil {
		return nil, fmt.Errorf("NewVP8Encoder(first-pass): %w", err)
	}
	defer enc.Close()
	stats := make([]govpx.FirstPassFrameStats, opts.Frames)
	for i := 0; i < opts.Frames; i++ {
		src := opts.Source(i)
		if src == nil {
			return nil, fmt.Errorf("Source returned nil at %d", i)
		}
		s, err := enc.CollectFirstPassStats(govpxImageFromYCbCrVP8(src), uint64(i), 1, 0)
		if err != nil {
			return nil, fmt.Errorf("CollectFirstPassStats[%d]: %w", i, err)
		}
		stats[i] = s
	}
	return govpx.FinalizeFirstPassStats(stats), nil
}

// govpxImageFromYCbCrVP8 builds a govpx.Image view of the source YCbCr.
// Mirrors govpxImageFromYCbCr (defined in bdrate_harness.go) but is
// duplicated here so the VP8 path doesn't depend on a function whose
// docstring documents the VP9 harness specifically. They produce
// identical output for the same input.
func govpxImageFromYCbCrVP8(src *image.YCbCr) govpx.Image {
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

// errVpxencVP8NotFound is set on BDRateResult.LibvpxErr when
// LibvpxReference is requested but the harness cannot locate a
// vpxenc binary. Callers can use errors.Is to detect this and
// either t.Skip (default) or t.Fatal (LibvpxRequired).
var errVpxencVP8NotFound = errors.New("vpxenc (VP8) binary not found")

// encodeBDLibvpxVP8Curve drives stock libvpx vpxenc at every ladder
// point and returns the (kbps, PSNR) curve. PSNR comes from
// `vpxenc --psnr` stderr (real PSNR-Y; the harness uses the same
// average that libvpx prints, matching what govpx computes on its
// decoder roundtrip). Bitrate comes from the produced IVF (sum of
// frame payloads — matches the totalBytes math in
// encodeBDOperatingPointVP8 so the two curves sit on the same axis).
//
// The libvpx run mirrors govpx's harness-defaulted rate-control mode:
// when the caller provided RateLadderKbps, both sides drive CBR; when
// the caller provided only a Q ladder, both sides drive VPX_Q. This
// keeps the libvpx curve on the same end-usage axis as the govpx
// curve so the BD-rate cross-comparison is apples-to-apples.
func encodeBDLibvpxVP8Curve(opts BDRateOptionsVP8, ladder []bdOperatingPoint) ([]QualityPoint, error) {
	binPath, err := resolveLibvpxVP8Binary(opts.LibvpxVpxenc)
	if err != nil {
		return nil, err
	}
	testOpts := govpx.EncoderOptions{
		Width:    opts.Width,
		Height:   opts.Height,
		FPS:      opts.FPS,
		Deadline: govpx.DeadlineGoodQuality,
	}
	if opts.RateControlOverrideSet {
		testOpts.RateControlMode = opts.RateControlOverride
	}
	if opts.Test != nil {
		opts.Test(&testOpts)
	}
	// Apply the harness-default rate-control mode that
	// encodeBDOperatingPointVP8 imposes, so the libvpx and govpx
	// runs sit on the same end-usage axis. Without this, an
	// unset-callback Test path would leave RateControlMode at the
	// zero value (RateControlVBR) and the libvpx side would run
	// --end-usage=vbr against govpx's CBR/VPX_Q curve. When the
	// caller explicitly pinned RateControlOverride, that takes
	// precedence (set above) and this defaulting is skipped.
	if !opts.RateControlOverrideSet && testOpts.RateControlMode == govpx.RateControlVBR {
		if len(opts.RateLadderKbps) > 0 {
			testOpts.RateControlMode = govpx.RateControlCBR
		} else {
			testOpts.RateControlMode = govpx.RateControlQ
		}
	}
	srcFrames := make([]*image.YCbCr, opts.Frames)
	for i := range srcFrames {
		srcFrames[i] = opts.Source(i)
		if srcFrames[i] == nil {
			return nil, fmt.Errorf("Source returned nil at %d", i)
		}
	}
	raw, err := writeI420ToBytes(srcFrames, opts.Width, opts.Height)
	if err != nil {
		return nil, fmt.Errorf("write libvpx VP8 I420 input: %w", err)
	}
	pts := make([]QualityPoint, 0, len(ladder))
	for _, op := range ladder {
		pointOpts := testOpts
		pointOpts.CQLevel = op.Q
		if op.TargetKbps > 0 {
			pointOpts.TargetBitrateKbps = op.TargetKbps
		}
		pt, err := encodeLibvpxVP8BDOperatingPointMedian(binPath, raw, opts, pointOpts, op)
		if err != nil {
			return nil, fmt.Errorf("libvpx VP8 Q=%d kbps=%d: %w", op.Q, op.TargetKbps, err)
		}
		pts = append(pts, pt)
	}
	return pts, nil
}

// encodeLibvpxVP8BDOperatingPointMedian wraps encodeLibvpxVP8BDOperatingPoint
// with an optional N-run median band. When opts.LibvpxOracleRuns < 2 it
// degrades to a single oracle invocation (the historical behavior). When
// >= 2 it invokes the oracle that many times and returns the per-axis
// median QualityPoint.
//
// Rationale: libvpx's VP8 `--rt --cpu-used=>0` path runs
// vp8_auto_select_speed (rdopt.c:261) which makes per-frame cpi->Speed
// decisions off vpx_usec_timer wall-clock measurements. Two consecutive
// invocations of vpxenc on the same source therefore produce different
// rate and PSNR even though `--threads=1` is in effect. The task #357
// audit measured a 42.86 -> 44.48 dB PSNR-Y spread on a single rung,
// which mapped to a +14.5 pp BD-rate spread (BD-rate=+2.30/+6.94/+16.82
// across three back-to-back runs). The median-of-N collapses that
// spread without changing the comparison semantics: we still compare
// govpx-VP8 against the libvpx behavior under the configured cpu_used
// value, but we characterize the libvpx distribution rather than a
// single sample of it.
//
// The median is taken per-axis (rate and PSNR sorted independently) so
// a slow run with high rate doesn't also bias the PSNR sample.
//
// For N=3 the median is the middle element; for N=2 it's the mean (so
// the gate is still useful at the minimum quarantine setting). Higher
// odd N is preferred when the variance budget allows.
func encodeLibvpxVP8BDOperatingPointMedian(binPath string, raw []byte, opts BDRateOptionsVP8, t govpx.EncoderOptions, op bdOperatingPoint) (QualityPoint, error) {
	runs := opts.LibvpxOracleRuns
	if runs < 2 {
		return encodeLibvpxVP8BDOperatingPoint(binPath, raw, opts, t, op)
	}
	rates := make([]float64, 0, runs)
	psnrs := make([]float64, 0, runs)
	for run := range runs {
		pt, err := encodeLibvpxVP8BDOperatingPoint(binPath, raw, opts, t, op)
		if err != nil {
			return QualityPoint{}, fmt.Errorf("median run %d/%d: %w", run+1, runs, err)
		}
		rates = append(rates, pt.Rate)
		psnrs = append(psnrs, pt.PSNR)
	}
	sort.Float64s(rates)
	sort.Float64s(psnrs)
	return QualityPoint{
		Rate: medianFloat64Sorted(rates),
		PSNR: medianFloat64Sorted(psnrs),
	}, nil
}

// medianFloat64Sorted returns the median of a pre-sorted ascending
// slice. For odd lengths it's the middle element; for even lengths it's
// the mean of the two central elements. Caller MUST sort first.
func medianFloat64Sorted(xs []float64) float64 {
	n := len(xs)
	if n == 0 {
		return math.NaN()
	}
	if n%2 == 1 {
		return xs[n/2]
	}
	return (xs[n/2-1] + xs[n/2]) / 2
}

// encodeLibvpxVP8BDOperatingPoint runs `vpxenc --codec=vp8 --psnr`
// once and parses (kbps, PSNR-Y) from the produced IVF and stderr.
//
// libvpx CLI flag mapping for VP8 (per vpxenc/vp8_cx_iface):
//
//	govpx.EncoderOptions.CQLevel           -> --cq-level=N (end-usage=q only)
//	govpx.EncoderOptions.TargetBitrateKbps -> --target-bitrate=N
//	govpx.EncoderOptions.MinQuantizer      -> --min-q=N (4 floor matches govpx default)
//	govpx.EncoderOptions.MaxQuantizer      -> --max-q=N
//	govpx.EncoderOptions.RateControlMode   -> --end-usage={q,vbr,cbr,cq}
//	govpx.EncoderOptions.Deadline          -> --deadline={good,rt,best}
//	govpx.EncoderOptions.CpuUsed           -> --cpu-used=N
//	govpx.EncoderOptions.NoiseSensitivity  -> --noise-sensitivity=N
//	govpx.EncoderOptions.Sharpness         -> --sharpness=N
//	govpx.EncoderOptions.StaticThreshold   -> --static-thresh=N
//	govpx.EncoderOptions.KeyFrameInterval  -> --kf-min-dist / --kf-max-dist
//	govpx.EncoderOptions.TokenPartitions   -> --token-parts=N
//	govpx.EncoderOptions.MaxIntraBitratePct-> --max-intra-rate=N
//	govpx.EncoderOptions.GFCBRBoostPct     -> --gf-cbr-boost=N
//	govpx.EncoderOptions.AutoAltRef        -> --auto-alt-ref=N
//	govpx.EncoderOptions.ARNRMaxFrames     -> --arnr-maxframes=N
//	govpx.EncoderOptions.ARNRStrength      -> --arnr-strength=N
//	govpx.EncoderOptions.ARNRType          -> --arnr-type=N
//	govpx.EncoderOptions.LookaheadFrames   -> --lag-in-frames=N
//	govpx.EncoderOptions.DropFrameWaterMark-> --drop-frame=N (when DropFrameAllowed)
//	govpx.EncoderOptions.ScreenContentMode -> --screen-content-mode=N (1 or 2)
//
// Each `// libvpx token:` comment in the body anchors the field to the
// CLI flag it drives.
func encodeLibvpxVP8BDOperatingPoint(binPath string, raw []byte, opts BDRateOptionsVP8, t govpx.EncoderOptions, op bdOperatingPoint) (QualityPoint, error) {
	dir, err := os.MkdirTemp("", "govpx-bdrate-vp8-libvpx-*")
	if err != nil {
		return QualityPoint{}, err
	}
	defer os.RemoveAll(dir)
	inPath := filepath.Join(dir, "input.i420")
	outPath := filepath.Join(dir, "output.ivf")
	if err := os.WriteFile(inPath, raw, 0o600); err != nil {
		return QualityPoint{}, err
	}
	commonTail := []string{
		"--ivf",
		"--i420",
		fmt.Sprintf("--width=%d", opts.Width),
		fmt.Sprintf("--height=%d", opts.Height),
		fmt.Sprintf("--fps=%d/1", opts.FPS),
		fmt.Sprintf("--limit=%d", opts.Frames),
		"--output=" + outPath,
		inPath,
	}
	var stderr bytes.Buffer
	if opts.TwoPass {
		// Pass 1: populate the first-pass stats file. No PSNR is
		// emitted in pass 1; output is still written but discarded.
		fpfPath := filepath.Join(dir, "fpf.bin")
		pass1Args := libvpxVP8BDCLIArgsTwoPass(opts, t, op, 1, fpfPath)
		pass1Args = append(pass1Args, commonTail...)
		cmd1 := exec.Command(binPath, pass1Args...)
		cmd1.Stderr = &stderr
		if err := cmd1.Run(); err != nil {
			return QualityPoint{}, fmt.Errorf("libvpx vpxenc pass=1 run: %w\nargs=%v\nstderr:\n%s",
				err, pass1Args, stderr.Bytes())
		}
		stderr.Reset()
		// Pass 2: consume the fpf and emit the final IVF + PSNR.
		pass2Args := libvpxVP8BDCLIArgsTwoPass(opts, t, op, 2, fpfPath)
		pass2Args = append(pass2Args, "--psnr")
		pass2Args = append(pass2Args, commonTail...)
		cmd2 := exec.Command(binPath, pass2Args...)
		cmd2.Stderr = &stderr
		if err := cmd2.Run(); err != nil {
			return QualityPoint{}, fmt.Errorf("libvpx vpxenc pass=2 run: %w\nargs=%v\nstderr:\n%s",
				err, pass2Args, stderr.Bytes())
		}
	} else {
		args := libvpxVP8BDCLIArgs(opts, t, op)
		args = append(args, "--psnr")
		args = append(args, commonTail...)
		cmd := exec.Command(binPath, args...)
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return QualityPoint{}, fmt.Errorf("libvpx vpxenc run: %w\nargs=%v\nstderr:\n%s",
				err, args, stderr.Bytes())
		}
	}
	ivf, err := os.ReadFile(outPath)
	if err != nil {
		return QualityPoint{}, fmt.Errorf("read IVF: %w", err)
	}
	sizes, err := parseIVFFrameSizes(ivf)
	if err != nil {
		return QualityPoint{}, fmt.Errorf("parse IVF: %w", err)
	}
	totalBytes := 0
	for _, s := range sizes {
		totalBytes += s
	}
	psnrY, ok := parseVpxencPSNR(stderr.Bytes())
	if !ok {
		return QualityPoint{}, fmt.Errorf("vpxenc PSNR line not found in stderr:\n%s",
			stderr.Bytes())
	}
	if len(sizes) == 0 {
		return QualityPoint{}, fmt.Errorf("no libvpx VP8 frames produced at Q=%d kbps=%d", op.Q, op.TargetKbps)
	}
	kbps := float64(totalBytes) * 8 * float64(opts.FPS) / float64(opts.Frames) / 1000
	if kbps <= 0 {
		return QualityPoint{}, fmt.Errorf("nonpositive libvpx VP8 kbps at Q=%d (bytes=%d frames=%d)",
			op.Q, totalBytes, opts.Frames)
	}
	return QualityPoint{Rate: kbps, PSNR: psnrY}, nil
}

// libvpxVP8BDCLIArgs maps govpx EncoderOptions fields to the libvpx
// vpxenc CLI tokens for the VP8 BD-rate run. Each `// libvpx token:`
// comment anchors the govpx field to the CLI flag it drives. Feature
// fields not yet exercised by the BD-rate ladder (segmentation, ROI
// map, temporal layers) are intentionally not mapped here; add them
// with a `// libvpx token:` citation when a new VP8 BD-rate gate
// needs them.
func libvpxVP8BDCLIArgs(opts BDRateOptionsVP8, t govpx.EncoderOptions, op bdOperatingPoint) []string {
	// libvpx token: --codec
	args := []string{"--codec=vp8"}
	// libvpx token: --passes / --lag-in-frames (single-pass, no lag by
	// default; LookaheadFrames overrides).
	args = append(args, "--passes=1")
	endUsage := "q"
	switch t.RateControlMode {
	case govpx.RateControlVBR:
		endUsage = "vbr"
	case govpx.RateControlCBR:
		endUsage = "cbr"
	case govpx.RateControlCQ:
		endUsage = "cq"
	case govpx.RateControlQ:
		endUsage = "q"
	}
	// libvpx token: --end-usage
	args = append(args, "--end-usage="+endUsage)
	// libvpx token: --min-q / --max-q (4 is libvpx's good-quality floor
	// and matches govpx encodeBDOperatingPointVP8).
	args = append(args, "--min-q=4", "--max-q=63")
	// libvpx token: --target-bitrate. Mostly validation ballast for
	// end-usage=q but the actual ladder axis for CBR/VBR.
	target := t.TargetBitrateKbps
	if target <= 0 {
		target = 1000
	}
	args = append(args, fmt.Sprintf("--target-bitrate=%d", target))
	if endUsage == "q" || endUsage == "cq" {
		// libvpx token: --cq-level
		args = append(args, fmt.Sprintf("--cq-level=%d", op.Q))
	}
	// libvpx token: --kf-min-dist / --kf-max-dist. Match govpx's
	// default startup KeyFrameInterval=120.
	kfDist := t.KeyFrameInterval
	if kfDist <= 0 {
		kfDist = 120
	}
	args = append(args, fmt.Sprintf("--kf-min-dist=%d", kfDist), fmt.Sprintf("--kf-max-dist=%d", kfDist))
	// libvpx token: --good / --rt / --best. vpxenc accepts these as
	// the deadline-bucket flags (the `--deadline=<usec>` form takes
	// an integer microsecond budget instead and is not what we
	// want). Mirror govpx's good-quality default unless the test
	// callback requested realtime.
	if t.Deadline == govpx.DeadlineRealtime {
		args = append(args, "--rt")
	} else {
		args = append(args, "--good")
	}
	// libvpx token: --cpu-used. CpuUsed=0 is libvpx's default; emit
	// only when explicitly set so the libvpx defaults govern.
	if t.CpuUsed != 0 {
		args = append(args, fmt.Sprintf("--cpu-used=%d", t.CpuUsed))
	}
	// libvpx token: --lag-in-frames
	if t.LookaheadFrames > 0 {
		args = append(args, fmt.Sprintf("--lag-in-frames=%d", t.LookaheadFrames))
	} else {
		args = append(args, "--lag-in-frames=0")
	}
	// libvpx token: --auto-alt-ref
	if t.AutoAltRef {
		args = append(args, "--auto-alt-ref=1")
	} else {
		args = append(args, "--auto-alt-ref=0")
	}
	// libvpx token: --arnr-maxframes / --arnr-strength / --arnr-type
	if t.ARNRMaxFrames > 0 {
		args = append(args, fmt.Sprintf("--arnr-maxframes=%d", t.ARNRMaxFrames))
	}
	if t.ARNRStrength > 0 {
		args = append(args, fmt.Sprintf("--arnr-strength=%d", t.ARNRStrength))
	}
	if t.ARNRType > 0 {
		args = append(args, fmt.Sprintf("--arnr-type=%d", t.ARNRType))
	}
	// libvpx token: --noise-sensitivity / --sharpness / --static-thresh
	if t.NoiseSensitivity > 0 {
		args = append(args, fmt.Sprintf("--noise-sensitivity=%d", t.NoiseSensitivity))
	}
	if t.Sharpness > 0 {
		args = append(args, fmt.Sprintf("--sharpness=%d", t.Sharpness))
	}
	if t.StaticThreshold > 0 {
		args = append(args, fmt.Sprintf("--static-thresh=%d", t.StaticThreshold))
	}
	// libvpx token: --max-intra-rate / --gf-cbr-boost
	if t.MaxIntraBitratePct > 0 {
		args = append(args, fmt.Sprintf("--max-intra-rate=%d", t.MaxIntraBitratePct))
	}
	if t.GFCBRBoostPct > 0 {
		args = append(args, fmt.Sprintf("--gf-cbr-boost=%d", t.GFCBRBoostPct))
	}
	// libvpx token: --token-parts
	if t.TokenPartitions > 0 {
		args = append(args, fmt.Sprintf("--token-parts=%d", t.TokenPartitions))
	}
	// libvpx token: --tune. Maps govpx.Tuning to the vpxenc CLI flag.
	// TunePSNR is libvpx's default; emit only when the test path
	// explicitly switched to SSIM so the libvpx default governs the
	// PSNR-tuned baseline ladders.
	switch t.Tuning {
	case govpx.TuneSSIM:
		args = append(args, "--tune=ssim")
	default:
		args = append(args, "--tune=psnr")
	}
	// libvpx token: --drop-frame
	if t.DropFrameAllowed && t.DropFrameWaterMark > 0 {
		args = append(args, fmt.Sprintf("--drop-frame=%d", t.DropFrameWaterMark))
	} else {
		args = append(args, "--drop-frame=0")
	}
	// libvpx token: --screen-content-mode. Maps govpx.EncoderOptions.
	// ScreenContentMode to vpxenc's CLI surface (vpxenc.c:399-401
	// `--screen-content-mode <int>` -> VP8E_SET_SCREEN_CONTENT_MODE codec
	// control -> vp8_cx_iface.c:626 set_screen_content_mode). Without this
	// the libvpx oracle runs with screen_content_mode=0 (the
	// vp8_cx_iface.c:79 default) while govpx runs at whatever the test
	// callback set on the EncoderOptions, so the BD-rate cross-comparison
	// is encoder-vs-encoder on different content-mode axes. Govpx
	// validates 0/1/2 via vp8_cx_iface.c:215 RANGE_CHECK_HI; mirror the
	// range here so an out-of-band value surfaces as a libvpx CLI error
	// the same way an in-process govpx call would surface a validation
	// error.
	if t.ScreenContentMode > 0 {
		args = append(args, fmt.Sprintf("--screen-content-mode=%d", t.ScreenContentMode))
	}
	// libvpx token: --timebase
	if opts.FPS > 0 {
		args = append(args, fmt.Sprintf("--timebase=1/%d", opts.FPS))
	}
	return args
}

// libvpxVP8BDCLIArgsTwoPass builds the vpxenc CLI argument list for the
// two-pass VBR flow. Mirrors libvpxVP8BDCLIArgs but emits --passes=2,
// --pass=N (1 or 2), --fpf=<path>, and forces --end-usage=vbr because
// two-pass is a VBR-planning workflow. Other feature toggles
// (cpu-used, lookahead, ARNR, tune, etc.) are inherited from the
// single-pass mapper.
func libvpxVP8BDCLIArgsTwoPass(opts BDRateOptionsVP8, t govpx.EncoderOptions, op bdOperatingPoint, pass int, fpfPath string) []string {
	args := []string{"--codec=vp8", "--passes=2", fmt.Sprintf("--pass=%d", pass), "--fpf=" + fpfPath}
	// libvpx two-pass requires end-usage=vbr (the fpf is consumed by
	// the VBR planner; cbr/q paths do not use it).
	args = append(args, "--end-usage=vbr")
	args = append(args, "--min-q=4", "--max-q=63")
	target := t.TargetBitrateKbps
	if target <= 0 {
		target = 1000
	}
	args = append(args, fmt.Sprintf("--target-bitrate=%d", target))
	kfDist := t.KeyFrameInterval
	if kfDist <= 0 {
		kfDist = 120
	}
	args = append(args, fmt.Sprintf("--kf-min-dist=%d", kfDist), fmt.Sprintf("--kf-max-dist=%d", kfDist))
	if t.Deadline == govpx.DeadlineRealtime {
		args = append(args, "--rt")
	} else {
		args = append(args, "--good")
	}
	if t.CpuUsed != 0 {
		args = append(args, fmt.Sprintf("--cpu-used=%d", t.CpuUsed))
	}
	if t.LookaheadFrames > 0 {
		args = append(args, fmt.Sprintf("--lag-in-frames=%d", t.LookaheadFrames))
	} else {
		args = append(args, "--lag-in-frames=0")
	}
	if t.AutoAltRef {
		args = append(args, "--auto-alt-ref=1")
	} else {
		args = append(args, "--auto-alt-ref=0")
	}
	if t.ARNRMaxFrames > 0 {
		args = append(args, fmt.Sprintf("--arnr-maxframes=%d", t.ARNRMaxFrames))
	}
	if t.ARNRStrength > 0 {
		args = append(args, fmt.Sprintf("--arnr-strength=%d", t.ARNRStrength))
	}
	if t.ARNRType > 0 {
		args = append(args, fmt.Sprintf("--arnr-type=%d", t.ARNRType))
	}
	if t.NoiseSensitivity > 0 {
		args = append(args, fmt.Sprintf("--noise-sensitivity=%d", t.NoiseSensitivity))
	}
	if t.Sharpness > 0 {
		args = append(args, fmt.Sprintf("--sharpness=%d", t.Sharpness))
	}
	if t.StaticThreshold > 0 {
		args = append(args, fmt.Sprintf("--static-thresh=%d", t.StaticThreshold))
	}
	if t.MaxIntraBitratePct > 0 {
		args = append(args, fmt.Sprintf("--max-intra-rate=%d", t.MaxIntraBitratePct))
	}
	if t.GFCBRBoostPct > 0 {
		args = append(args, fmt.Sprintf("--gf-cbr-boost=%d", t.GFCBRBoostPct))
	}
	if t.TokenPartitions > 0 {
		args = append(args, fmt.Sprintf("--token-parts=%d", t.TokenPartitions))
	}
	switch t.Tuning {
	case govpx.TuneSSIM:
		args = append(args, "--tune=ssim")
	default:
		args = append(args, "--tune=psnr")
	}
	if t.DropFrameAllowed && t.DropFrameWaterMark > 0 {
		args = append(args, fmt.Sprintf("--drop-frame=%d", t.DropFrameWaterMark))
	} else {
		args = append(args, "--drop-frame=0")
	}
	// libvpx token: --screen-content-mode. Mirrors libvpxVP8BDCLIArgs so
	// the two-pass VBR path also pins the libvpx oracle to the same
	// screen_content_mode the govpx Test/Baseline callback set.
	if t.ScreenContentMode > 0 {
		args = append(args, fmt.Sprintf("--screen-content-mode=%d", t.ScreenContentMode))
	}
	if opts.FPS > 0 {
		args = append(args, fmt.Sprintf("--timebase=1/%d", opts.FPS))
	}
	return args
}

// vpxencPSNRRE captures the trailing "Stream 0 PSNR (Overall/Avg/Y/U/V) ..."
// line vpxenc prints after the encode completes when --psnr is set.
// Columns: Overall (global), Avg (per-frame avg), Y, U, V. We use the
// Y column because it matches the luminance-dominated PSNR govpx
// computes via imagePSNR (which is itself a single-number Y+U+V
// average; the Y column is the closest single value reported by vpxenc
// to that average, and PSNR-Y is also the BD-rate community's de facto
// PSNR axis).
var vpxencPSNRRE = regexp.MustCompile(`Stream 0 PSNR \(Overall/Avg/Y/U/V\)\s+([-\d.]+)\s+([-\d.]+)\s+([-\d.]+)\s+([-\d.]+)\s+([-\d.]+)`)

// parseVpxencPSNR returns the average Y-PSNR (dB) parsed from
// `vpxenc --psnr` stderr. Returns false when no PSNR line is found.
func parseVpxencPSNR(stderr []byte) (float64, bool) {
	m := vpxencPSNRRE.FindSubmatch(stderr)
	if m == nil {
		return 0, false
	}
	// Field index 3 is the Y column; index 2 is the per-frame Avg.
	// libvpx's Avg column is the per-frame mean of (Y+U+V) global
	// PSNR — closer to govpx's imagePSNR (which averages SSE over
	// Y+U+V samples). Use Avg as the BD-rate axis so both curves
	// sit on the same single-number-per-frame PSNR semantics.
	avg, err := strconv.ParseFloat(string(m[2]), 64)
	if err != nil {
		return 0, false
	}
	if !(avg > 0) {
		return 0, false
	}
	return avg, true
}

// resolveLibvpxVP8Binary finds the stock libvpx vpxenc binary that
// supports VP8 encoding. Search order:
//  1. the explicit path passed in opts.LibvpxVpxenc.
//  2. the GOVPX_VPXENC_VP8_BIN env var (override for CI).
//  3. the project's internal/coracle/build/vpxenc (built by
//     internal/coracle/build_vpxenc.sh — same binary the encode
//     benchmark uses).
//  4. exec.LookPath("vpxenc") on $PATH.
//
// Returns errVpxencVP8NotFound when none of those resolve.
func resolveLibvpxVP8Binary(explicit string) (string, error) {
	if explicit != "" {
		if st, err := os.Stat(explicit); err == nil && !st.IsDir() {
			return explicit, nil
		}
	}
	if envBin := os.Getenv("GOVPX_VPXENC_VP8_BIN"); envBin != "" {
		if st, err := os.Stat(envBin); err == nil && !st.IsDir() {
			return envBin, nil
		}
	}
	if root, ok := findGovpxRoot(); ok {
		candidate := filepath.Join(root, "internal", "coracle", "build", "vpxenc")
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate, nil
		}
	}
	if path, err := exec.LookPath("vpxenc"); err == nil {
		return path, nil
	}
	return "", errVpxencVP8NotFound
}

// LibvpxVP8Required reports whether a missing vpxenc should hard-fail
// the VP8 BD-rate gate. Off by default. Set
// GOVPX_BD_RATE_LIBVPX_VP8_REQUIRED=1 (or pass through
// `make verify-bd-rate`) when the gate must always observe the libvpx
// VP8 oracle.
func LibvpxVP8Required() bool {
	return os.Getenv("GOVPX_BD_RATE_LIBVPX_VP8_REQUIRED") == "1"
}
