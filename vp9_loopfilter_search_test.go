package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"testing"
)

// TestVP9LoopFilterStrengthFromQAtSpeed8 covers libvpx's LPF_PICK_FROM_Q
// path (vp9_picklpf.c:168-198). At cpu_used=8 the speed-features
// dispatcher sets sf.lpf_pick = LPF_PICK_FROM_Q for speed >= 3 (libvpx
// vp9_speed_features.c:555). The encoded header's FilterLevel must
// equal the closed-form formula filt_guess = ROUND_POWER_OF_TWO(q *
// 20723 + 1015158, 18), with the KEY_FRAME -4 bias applied.
func TestVP9LoopFilterStrengthFromQAtSpeed8(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:    width,
		Height:   height,
		FPS:      30,
		CpuUsed:  8,
		Deadline: DeadlineRealtime,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if e.sf.LpfPick != LpfPickFromQ {
		t.Fatalf("CpuUsed=8 sf.LpfPick=%v, want LpfPickFromQ", e.sf.LpfPick)
	}
	dst := make([]byte, 65536)
	n, err := e.EncodeInto(vp9test.NewYCbCr(width, height, 128, 128, 128), dst)
	if err != nil {
		t.Fatalf("EncodeInto: %v", err)
	}
	hdr, _ := vp9test.ParseHeader(t, dst[:n])
	qindex := int(hdr.Quant.BaseQindex)
	// libvpx vp9_picklpf.c:177 — 8-bit: filt_guess = ROUND_POWER_OF_TWO(q * 20723 + 1015158, 18).
	q := int(vp9dec.VpxAcQuant(qindex, 0, vp9dec.BitDepth8))
	want := (q*20723 + 1015158 + (1 << 17)) >> 18
	// libvpx vp9_picklpf.c:197 — KEY_FRAME bias.
	want -= 4
	if want < 0 {
		want = 0
	}
	if want > vp9dec.MaxLoopFilter {
		want = vp9dec.MaxLoopFilter
	}
	if int(hdr.Loopfilter.FilterLevel) != want {
		t.Fatalf("FilterLevel=%d, want %d (qindex=%d ac_q=%d)",
			hdr.Loopfilter.FilterLevel, want, qindex, q)
	}
}

// TestVP9LoopFilterFromQFormulaParity exercises the FROM_Q closed-form
// directly across the qindex sweep [0, 255] for both KEY_FRAME and
// non-KEY_FRAME, asserting the govpx port matches libvpx's exact
// arithmetic (vp9_picklpf.c:175-198, 8-bit branch).
func TestVP9LoopFilterFromQFormulaParity(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:  64,
		Height: 64,
		FPS:    30,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	for qindex := 0; qindex <= 255; qindex++ {
		for _, isKey := range []bool{false, true} {
			got := e.vp9PickLpfFromQ(qindex, isKey /*segEnabled=*/, false, 64, 64)
			q := int(vp9dec.VpxAcQuant(qindex, 0, vp9dec.BitDepth8))
			want := (q*20723 + 1015158 + (1 << 17)) >> 18
			if isKey {
				want -= 4
			}
			if want < 0 {
				want = 0
			}
			if want > vp9dec.MaxLoopFilter {
				want = vp9dec.MaxLoopFilter
			}
			if got != want {
				t.Fatalf("qindex=%d isKey=%v: got %d, want %d", qindex, isKey, got, want)
			}
		}
	}
}

// TestVP9LoopFilterMinimalLpfZerosWhenLastNonzero covers
// LPF_PICK_MINIMAL_LPF (vp9_picklpf.c:166-167). When the previous
// frame's filter level was non-zero, the picker zeros this frame's
// level; otherwise it leaves it at zero.
func TestVP9LoopFilterMinimalLpfZerosWhenLastNonzero(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:  64,
		Height: 64,
		FPS:    30,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	e.vp9LastFiltLevel = 17
	if got := e.vp9PickLpfMinimal(); got != 0 {
		t.Fatalf("vp9LastFiltLevel=17 vp9PickLpfMinimal=%d, want 0", got)
	}
	e.vp9LastFiltLevel = 0
	if got := e.vp9PickLpfMinimal(); got != 0 {
		t.Fatalf("vp9LastFiltLevel=0 vp9PickLpfMinimal=%d, want 0", got)
	}
}

// TestVP9PickLpfMaxFilterLevelOnePass asserts get_max_filter_level
// returns MAX_LOOP_FILTER for one-pass (vp9_picklpf.c:41-43).
func TestVP9PickLpfMaxFilterLevelOnePass(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64, FPS: 30})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if got := e.vp9PickLpfMaxFilterLevel(false); got != vp9dec.MaxLoopFilter {
		t.Fatalf("vp9PickLpfMaxFilterLevel(one-pass)=%d, want %d",
			got, vp9dec.MaxLoopFilter)
	}
}

// TestVP9LoopFilterSubImagePickerWiringAtSpeed0 asserts the production
// wiring of the sub-image picker. When sf.LpfPick is overridden to
// LpfPickFromSubImage post-construction, the post-tile encoder branch
// must run the quadratic search with partial_frame=1 against the
// reconstructed luma (libvpx vp9_picklpf.c:201: `method ==
// LPF_PICK_FROM_SUBIMAGE`). The encoded header carries a valid 6-bit
// FilterLevel; the search must complete without bitstream corruption.
// Stock libvpx never selects SUBIMAGE through the speed-features
// dispatcher (vp9_speed_features.c only emits FROM_FULL_IMAGE and
// FROM_Q), so this test exercises the manual override path the C
// public API surfaces via VP9E_SET_LPF_PICK.
func TestVP9LoopFilterSubImagePickerWiringAtSpeed0(t *testing.T) {
	const width, height = 128, 128
	src := newVP9TexturedYCbCrForLpfPickerTest(width, height)
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:    width,
		Height:   height,
		FPS:      30,
		CpuUsed:  0,
		Deadline: DeadlineGoodQuality,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	e.sf.LpfPick = LpfPickFromSubImage
	dst := make([]byte, 65536)
	n, err := e.EncodeInto(src, dst)
	if err != nil {
		t.Fatalf("EncodeInto: %v", err)
	}
	hdr, _ := vp9test.ParseHeader(t, dst[:n])
	if hdr.Loopfilter.FilterLevel > vp9dec.MaxLoopFilter {
		t.Fatalf("FilterLevel=%d, want in [0, %d]",
			hdr.Loopfilter.FilterLevel, vp9dec.MaxLoopFilter)
	}
	// Decode round-trip — the stream must remain well-formed under the
	// sub-image picker's partial-frame trials. The post-pick final
	// filter pass runs on the full frame Y+U+V, so the encoded
	// reconstruction matches a decoder's full-frame deblock.
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	defer d.Close()
	if err := d.Decode(dst[:n]); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("NextFrame returned !ok")
	}
}

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
