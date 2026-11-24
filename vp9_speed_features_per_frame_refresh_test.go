package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
)

// TestVP9ApplySpeedFeaturesPerFrameRefreshesTxSizeSearchMethod pins the
// libvpx per-frame speed-feature refresh that
// encodeVP9FrameIntoWithFlagsResultInternal now invokes before the
// pickers fire. Mirrors libvpx vp9/encoder/vp9_encoder.c:3754 / 3765
// set_speed_features_framesize_{in,}dependent calls inside
// set_size_{in,}dependent_vars (vp9_encoder.c:4169-4170, 4377-4392):
// the SF dispatcher reads cm->frame_type via frame_is_kf_gf_arf, and
// at RT speed >= 5 (cpu_used >= 5, the govpx default) the keyframe
// branch picks USE_LARGESTALL while the non-keyframe branch picks
// USE_TX_8X8 (vp9_speed_features.c:1538-1542 / 1595-1597).
//
// Before this commit govpx pinned e.sf at compressor-create time and
// never re-applied, so non-key frames carried the keyframe-context
// USE_LARGESTALL value forever. This test verifies that
// vp9ApplySpeedFeatures applied with the live per-frame context
// produces the correct libvpx truth table.
func TestVP9ApplySpeedFeaturesPerFrameRefreshesTxSizeSearchMethod(t *testing.T) {
	for _, tc := range []struct {
		name      string
		deadline  Deadline
		cpuUsed   int8
		isKey     bool
		intraOnly bool
		want      TxSizeSearchMethod
	}{
		{
			// libvpx vp9_speed_features.c:1539 — RT speed>=5 keyframe
			// path: USE_LARGESTALL.
			name:     "rt-cpu8-keyframe-uses-largestall",
			deadline: DeadlineRealtime, cpuUsed: 8, isKey: true,
			want: UseLargestAll,
		},
		{
			// libvpx vp9_speed_features.c:1541 — RT speed>=5 non-key
			// path: USE_TX_8X8.
			name:     "rt-cpu8-inter-uses-tx8x8",
			deadline: DeadlineRealtime, cpuUsed: 8,
			want: UseTx8x8,
		},
		{
			// libvpx vp9_speed_features.c:1541 — intra-only frames
			// have cm->frame_type == INTER_FRAME so the `is_keyframe`
			// branch falls through and USE_TX_8X8 applies. The non-
			// keyframe TxSize is the load-bearing assertion for the
			// per-frame refresh: prior to this commit govpx pinned
			// e.sf at create time so intra-only inherited whatever
			// the create-time keyframe context picked (USE_LARGESTALL).
			name:     "rt-cpu8-intra-only-uses-tx8x8",
			deadline: DeadlineRealtime, cpuUsed: 8, intraOnly: true,
			want: UseTx8x8,
		},
		{
			// libvpx vp9_speed_features.c:387 GOOD speed>=4 sets
			// tx_size_search_method = USE_LARGESTALL unconditionally
			// (speed 5+ does not override). govpx mirrors this.
			name:     "good-cpu5-inter-uses-largestall",
			deadline: DeadlineGoodQuality, cpuUsed: 5,
			want: UseLargestAll,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, err := NewVP9Encoder(VP9EncoderOptions{
				Width:    64,
				Height:   64,
				Deadline: tc.deadline,
				CpuUsed:  tc.cpuUsed,
			})
			if err != nil {
				t.Fatalf("NewVP9Encoder: %v", err)
			}
			ctx := e.vp9PerFrameSpeedContext(vp9PerFrameSpeedContextArgs{
				IsKey:     tc.isKey,
				IntraOnly: tc.intraOnly,
				ShowFrame: true,
			})
			e.vp9ApplySpeedFeatures(ctx)
			if got := e.sf.TxSizeSearchMethod; got != tc.want {
				t.Fatalf("TxSizeSearchMethod = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestVP9ApplySpeedFeaturesPerFrameTransitionsKeyToInter mirrors the
// libvpx per-frame refresh sequence: keyframe -> inter -> intra-only
// must each see the correct TxSizeSearchMethod, not a stale value
// from the previous frame. A stale create-time e.sf value would leave the
// keyframe USE_LARGESTALL setting on every inter frame.
func TestVP9ApplySpeedFeaturesPerFrameTransitionsKeyToInter(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:    64,
		Height:   64,
		Deadline: DeadlineRealtime,
		CpuUsed:  8,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	// Frame 1: keyframe. libvpx vp9_speed_features.c:1539 -> USE_LARGESTALL.
	e.vp9ApplySpeedFeatures(e.vp9PerFrameSpeedContext(vp9PerFrameSpeedContextArgs{
		IsKey:     true,
		ShowFrame: true,
	}))
	if got := e.sf.TxSizeSearchMethod; got != UseLargestAll {
		t.Fatalf("keyframe TxSizeSearchMethod = %d, want UseLargestAll", got)
	}
	// Frame 2: inter. libvpx vp9_speed_features.c:1541 -> USE_TX_8X8.
	e.vp9ApplySpeedFeatures(e.vp9PerFrameSpeedContext(vp9PerFrameSpeedContextArgs{
		ShowFrame: true,
	}))
	if got := e.sf.TxSizeSearchMethod; got != UseTx8x8 {
		t.Fatalf("inter TxSizeSearchMethod = %d, want UseTx8x8 "+
			"(stale-from-keyframe bug)", got)
	}
	// Frame 3: intra-only. libvpx's is_keyframe predicate checks
	// cm->frame_type == KEY_FRAME literally and intra-only frames carry
	// INTER_FRAME — so the non-keyframe leg fires and USE_TX_8X8
	// applies (vp9_speed_features.c:1541).
	e.vp9ApplySpeedFeatures(e.vp9PerFrameSpeedContext(vp9PerFrameSpeedContextArgs{
		IntraOnly: true,
		ShowFrame: true,
	}))
	if got := e.sf.TxSizeSearchMethod; got != UseTx8x8 {
		t.Fatalf("intra-only TxSizeSearchMethod = %d, want UseTx8x8", got)
	}
	// Frame 4: back to inter -> USE_TX_8X8 (unchanged from frame 3).
	e.vp9ApplySpeedFeatures(e.vp9PerFrameSpeedContext(vp9PerFrameSpeedContextArgs{
		ShowFrame: true,
	}))
	if got := e.sf.TxSizeSearchMethod; got != UseTx8x8 {
		t.Fatalf("post-intra-only inter TxSizeSearchMethod = %d, want "+
			"UseTx8x8", got)
	}
	// Frame 5: another keyframe. SF refresh must flip back to
	// USE_LARGESTALL — the load-bearing inter->key transition.
	e.vp9ApplySpeedFeatures(e.vp9PerFrameSpeedContext(vp9PerFrameSpeedContextArgs{
		IsKey:     true,
		ShowFrame: true,
	}))
	if got := e.sf.TxSizeSearchMethod; got != UseLargestAll {
		t.Fatalf("second keyframe TxSizeSearchMethod = %d, want UseLargestAll "+
			"(stale-from-inter bug)", got)
	}
	// Sanity: common.KeyFrame import is used.
	_ = common.KeyFrame
}
