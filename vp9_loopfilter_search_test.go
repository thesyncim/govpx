package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"testing"
)

// TestVP9SearchFilterLevelQuadraticDescent drives the ported
// search_filter_level (libvpx vp9_picklpf.c:78-157) with a synthetic
// SSE landscape: a parabola minimised at level=20 with broad noise
// floor. The search must converge on 20 (or a neighbour within the
// quadratic-step window) from a seed at last_filt_level=8. The bias
// formula prefers lower levels; we use a sharply peaked landscape so
// the bias cannot dominate.
func TestVP9SearchFilterLevelQuadraticDescent(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:  64,
		Height: 64,
		FPS:    30,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	e.vp9LastFiltLevel = 8

	// Parabola: sse(level) = 1_000_000 + 4_000_000*(level-20)^2
	// Sharp enough that the bias term `(best_err >> shift) *
	// filter_step` can't tilt the search away from 20.
	calls := 0
	sseFn := func(level int, partial bool) int64 {
		calls++
		d := int64(level - 20)
		return 1_000_000 + 4_000_000*d*d
	}
	got := e.vp9SearchFilterLevel( /*isKey=*/ false, common.TxModeSelect, false, sseFn)
	if got < 18 || got > 22 {
		t.Fatalf("vp9SearchFilterLevel got %d, want within [18, 22] (calls=%d)", got, calls)
	}
}

// TestVP9SearchFilterLevelClampsToMax verifies the quadratic search
// respects the upper bound returned by get_max_filter_level
// (vp9_picklpf.c:107). With a monotonically-decreasing SSE landscape
// the search should clamp at MaxLoopFilter.
func TestVP9SearchFilterLevelClampsToMax(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:  64,
		Height: 64,
		FPS:    30,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	e.vp9LastFiltLevel = 32
	// Monotonically decreasing in level: minimum at MaxLoopFilter.
	sseFn := func(level int, partial bool) int64 {
		return int64(1_000_000 - 10_000*level)
	}
	got := e.vp9SearchFilterLevel( /*isKey=*/ false, common.TxModeSelect, false, sseFn)
	if got != vp9dec.MaxLoopFilter {
		t.Fatalf("vp9SearchFilterLevel got %d, want %d (clamp)", got, vp9dec.MaxLoopFilter)
	}
}

// TestVP9SearchFilterLevelBiasPrefersLow verifies the libvpx bias
// formula `(best_err >> (15 - (filt_mid / 8))) * filter_step` steers
// the picker toward lower levels when the SSE landscape is flat. With
// constant SSE the search returns last_filt_level (no improvement
// from either side); but with a small positive slope the bias must
// reject the upper candidate and accept the lower.
func TestVP9SearchFilterLevelBiasPrefersLow(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:  64,
		Height: 64,
		FPS:    30,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	e.vp9LastFiltLevel = 20
	// Flat landscape — bias drives the picker toward filt_low. libvpx
	// vp9_picklpf.c:126 — `if ((ss_err[filt_low] - bias) < best_err)`.
	sseFn := func(level int, partial bool) int64 {
		return 1_000_000
	}
	got := e.vp9SearchFilterLevel( /*isKey=*/ false, common.TxModeSelect, false, sseFn)
	if got >= 20 {
		t.Fatalf("vp9SearchFilterLevel got %d, want < 20 (bias toward low)", got)
	}
}

// TestVP9SearchFilterLevelSubImageRunsPartialFrameCallback verifies
// the dispatcher plumbs the partialFrame flag through to the sseFn.
// The synthetic sseFn records whether it was invoked with partial=true
// at least once; both LpfPickFromFullImage and LpfPickFromSubImage
// invoke search_filter_level, but only the latter passes
// partial_frame=1 (libvpx vp9_picklpf.c:201). The dispatcher must
// forward that flag verbatim to try_filter_frame (libvpx vp9_picklpf.c
// :46-76 — partial_frame is the 4th arg).
func TestVP9SearchFilterLevelSubImageRunsPartialFrameCallback(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:  128,
		Height: 128,
		FPS:    30,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	e.vp9LastFiltLevel = 20

	var sawPartialTrue, sawPartialFalse bool
	sseFn := func(level int, partial bool) int64 {
		if partial {
			sawPartialTrue = true
		} else {
			sawPartialFalse = true
		}
		d := int64(level - 20)
		return 1_000_000 + 4_000_000*d*d
	}

	// LpfPickFromFullImage → partial_frame=false through the dispatcher.
	sawPartialTrue, sawPartialFalse = false, false
	_ = e.vp9PickFilterLevel(LpfPickFromFullImage, 60 /*isKey=*/, true, false,
		128, 128, common.TxModeSelect /*partialFrame=*/, false, sseFn)
	if sawPartialTrue || !sawPartialFalse {
		t.Fatalf("LpfPickFromFullImage: sawPartialTrue=%v sawPartialFalse=%v, want false/true",
			sawPartialTrue, sawPartialFalse)
	}

	// LpfPickFromSubImage → caller passes partialFrame=true through.
	sawPartialTrue, sawPartialFalse = false, false
	_ = e.vp9PickFilterLevel(LpfPickFromSubImage, 60 /*isKey=*/, true, false,
		128, 128, common.TxModeSelect /*partialFrame=*/, true, sseFn)
	if !sawPartialTrue || sawPartialFalse {
		t.Fatalf("LpfPickFromSubImage: sawPartialTrue=%v sawPartialFalse=%v, want true/false",
			sawPartialTrue, sawPartialFalse)
	}
}
