package benchcmd

import (
	"errors"
	"fmt"
	"image"
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
	// Codec selects which encoder to drive. Only "vp9" is currently
	// wired; "vp8" returns an error.
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

	// Baseline / Test apply codec-specific tweaks (feature toggles)
	// on top of the shared per-Q encoder configuration. Both
	// callbacks receive a *VP9EncoderOptions pre-populated with
	// width/height/Q-band/lookahead defaults; they may set any
	// feature toggle they want to compare.
	Baseline func(opts *govpx.VP9EncoderOptions)
	Test     func(opts *govpx.VP9EncoderOptions)

	// Lookahead overrides the lookahead-frame count baked into the
	// shared options. Zero leaves the harness default (8, the TPL
	// minimum) so AutoAltRef / TPL toggles can be evaluated without
	// further configuration. Set to a smaller value to compare
	// realtime-style paths.
	Lookahead int
}

// ComputeBDRate runs the harness and returns the BD-rate result.
// Returns an error only when the inputs are degenerate (e.g. missing
// callbacks, fewer than 4 Q points, encode failure that propagates).
// Encode failures at individual Q points are reported as a wrapped
// error so callers can inspect which operating point failed.
func ComputeBDRate(t testing.TB, opts BDRateOptions) (BDRateResult, error) {
	if err := validateBDRateOptions(opts); err != nil {
		return BDRateResult{}, err
	}
	if opts.FPS == 0 {
		opts.FPS = 30
	}
	if opts.Lookahead == 0 {
		opts.Lookahead = 8
	}
	qs := append([]int(nil), opts.QLadder...)
	sort.Ints(qs)
	baseline := make([]QualityPoint, 0, len(qs))
	test := make([]QualityPoint, 0, len(qs))
	for _, q := range qs {
		bPt, err := encodeBDOperatingPoint(opts, q, opts.Baseline)
		if err != nil {
			return BDRateResult{}, fmt.Errorf("baseline Q=%d: %w", q, err)
		}
		tPt, err := encodeBDOperatingPoint(opts, q, opts.Test)
		if err != nil {
			return BDRateResult{}, fmt.Errorf("test Q=%d: %w", q, err)
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
		return BDRateResult{Reference: baseline, Govpx: test, BDRate: bd}, err
	}
	return BDRateResult{
		Reference: baseline,
		Govpx:     test,
		BDRate:    bd,
		BDPSNR:    psnr,
	}, nil
}

func validateBDRateOptions(opts BDRateOptions) error {
	if opts.Codec != "vp9" {
		return fmt.Errorf("bdrate: codec %q not supported (only vp9)", opts.Codec)
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
	if opts.Baseline == nil || opts.Test == nil {
		return errors.New("bdrate: Baseline and Test callbacks required")
	}
	return nil
}

// encodeBDOperatingPoint encodes the source sequence at a fixed Q with
// the caller-applied feature toggles, decodes every emitted packet,
// and returns the (kbps, PSNR) point. Lookahead-aware encoders (which
// AutoAltRef and TPL both require) are drained via FlushIntoWithResult
// after all source frames are fed in so hidden ALTREFs and pending
// reordered packets are accounted for.
func encodeBDOperatingPoint(opts BDRateOptions, q int, apply func(*govpx.VP9EncoderOptions)) (QualityPoint, error) {
	encOpts := govpx.VP9EncoderOptions{
		Width:           opts.Width,
		Height:          opts.Height,
		FPS:             opts.FPS,
		LookaheadFrames: opts.Lookahead,
		// Lock Q to the ladder point. The public 0..63 range maps
		// to the libvpx CQ level; MinQ==MaxQ pins it.
		MinQuantizer: q,
		MaxQuantizer: q,
		CQLevel:      q,
	}
	if apply != nil {
		apply(&encOpts)
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
	dec, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		return QualityPoint{}, fmt.Errorf("NewVP9Decoder: %w", err)
	}
	bufSize := opts.Width * opts.Height * 6
	if bufSize < 65536 {
		bufSize = 65536
	}
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
		})
		totalBytes += result.SizeBytes
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
		})
		totalBytes += result.SizeBytes
	}

	// Decode pass: feed every emitted packet through the decoder
	// in order. Visible frames are paired with the next source
	// frame; hidden frames (AutoAltRef) advance decoder state but
	// not the source pointer.
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
		return QualityPoint{}, fmt.Errorf("no visible frames emitted at Q=%d", q)
	}
	avgPSNR := psnrSum / float64(visibleCount)
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
