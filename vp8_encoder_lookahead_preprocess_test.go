package govpx

import (
	"errors"
	"testing"
)

func TestEncodeIntoLookaheadBuffersAndFlushes(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		KeyFrameInterval:    120,
		LookaheadFrames:     2,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	dst := make([]byte, 8192)
	first := testImage(16, 16)
	second := testImage(16, 16)
	third := testImage(16, 16)
	fillImage(first, 30, 90, 170)
	fillImage(second, 50, 90, 170)
	fillImage(third, 70, 90, 170)

	if _, err := e.EncodeInto(dst, first, 10, 1, 0); !errors.Is(err, ErrFrameNotReady) {
		t.Fatalf("first EncodeInto error = %v, want ErrFrameNotReady", err)
	}
	result, err := e.EncodeInto(dst, second, 11, 1, 0)
	if err != nil {
		t.Fatalf("second EncodeInto returned error: %v", err)
	}
	if !result.KeyFrame || result.PTS != 10 || result.LookaheadDepth != 1 {
		t.Fatalf("second result = key:%t pts:%d depth:%d, want first queued keyframe with depth 1", result.KeyFrame, result.PTS, result.LookaheadDepth)
	}
	result, err = e.EncodeInto(dst, third, 12, 1, 0)
	if err != nil {
		t.Fatalf("third EncodeInto returned error: %v", err)
	}
	if result.PTS != 11 || result.LookaheadDepth != 1 {
		t.Fatalf("third result pts/depth = %d/%d, want second queued frame/depth 1", result.PTS, result.LookaheadDepth)
	}
	result, err = e.FlushInto(dst)
	if err != nil {
		t.Fatalf("FlushInto returned error: %v", err)
	}
	if result.PTS != 12 || result.LookaheadDepth != 0 {
		t.Fatalf("flush result pts/depth = %d/%d, want final queued frame/depth 0", result.PTS, result.LookaheadDepth)
	}
	if _, err := e.FlushInto(dst); !errors.Is(err, ErrFrameNotReady) {
		t.Fatalf("empty FlushInto error = %v, want ErrFrameNotReady", err)
	}
}

func TestEncodeIntoARNRAndSpatialDenoiserReportPreprocessing(t *testing.T) {
	// libvpx vp8_temporal_filter_prepare_c only fires for the hidden alt-ref
	// source (gated on `cpi->source_alt_ref_pending`). govpx mirrors that by
	// running ARNR only when the encode flags carry the hidden-ARF combo
	// (EncodeForceAltRefFrame|EncodeInvisibleFrame). Drive the encoder with
	// AutoAltRef=true and a synthetic two-pass stats section so the auto-ARF
	// driver schedules a hidden frame on the libvpx-faithful path
	// (calc_pframe_target_size clears source_alt_ref_pending on every
	// one-pass frame, so ARF only ever schedules in two-pass mode); on that
	// hidden frame both ARNR and the spatial denoiser report having run.
	// Use backward ARNR here: at the hidden ARF point this fixture has
	// adjacent prior lookahead frames available, while forward/centered
	// ARNR may legitimately prepare a center-only window and skip filtering.
	stats := make([]FirstPassFrameStats, 32)
	for i := range stats {
		stats[i] = FirstPassFrameStats{
			IntraError:    20000,
			CodedError:    200,
			PcntInter:     0.95,
			PcntMotion:    0.4,
			PcntSecondRef: 0.0,
			PcntNeutral:   0.0,
			MVrAbs:        5,
			MVcAbs:        5,
			Count:         1,
			Duration:      1,
		}
	}
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             16,
		Height:            16,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: 1200,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  120,
		LookaheadFrames:   8,
		AutoAltRef:        true,
		ARNRMaxFrames:     3,
		ARNRStrength:      6,
		ARNRType:          1,
		NoiseSensitivity:  2,
		TwoPassStats:      FinalizeFirstPassStats(stats),
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	dst := make([]byte, 8192)
	noisy := testImage(16, 16)
	for i := range noisy.Y {
		if i%2 == 0 {
			noisy.Y[i] = 40
		} else {
			noisy.Y[i] = 60
		}
	}
	clean := testImage(16, 16)
	fillImage(clean, 50, 90, 170)
	const totalFrames = 12
	frames := make([]Image, totalFrames)
	for i := range frames {
		if i == 0 {
			frames[i] = noisy
		} else {
			frames[i] = clean
		}
	}
	var sawARNR bool
	for i, src := range frames {
		result, err := e.EncodeInto(dst, src, uint64(i), 1, 0)
		if err != nil {
			if errors.Is(err, ErrFrameNotReady) {
				continue
			}
			t.Fatalf("EncodeInto frame %d returned error: %v", i, err)
		}
		if result.ARNRFiltered {
			if !result.Denoised {
				t.Fatalf("frame %d arnr=true but denoised=false", i)
			}
			sawARNR = true
			break
		}
	}
	if !sawARNR {
		// Drain the lookahead so the hidden ARF can fire on flush.
		for {
			result, err := e.FlushInto(dst)
			if err != nil {
				if errors.Is(err, ErrFrameNotReady) {
					break
				}
				t.Fatalf("FlushInto returned error: %v", err)
			}
			if result.ARNRFiltered {
				if !result.Denoised {
					t.Fatalf("flush arnr=true but denoised=false")
				}
				sawARNR = true
				break
			}
		}
	}
	if !sawARNR {
		t.Fatalf("no encoded frame reported ARNR filtering: auto-ARF driver did not emit a hidden ARF on the configured fixture")
	}
}
