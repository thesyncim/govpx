package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9enc "github.com/thesyncim/govpx/internal/vp9/encoder"
	"testing"
)

func TestVP9EncoderFrameTxModeFromCountsBypassesNonSelect(t *testing.T) {
	counts := &vp9enc.FrameCounts{}
	counts.TxTotals[common.Tx16x16] = 1
	counts.TxTotals[common.Tx8x8] = 1
	if got := vp9EncoderFrameTxModeFromCounts(common.Allow32x32, false, true, counts); got != common.Allow32x32 {
		t.Fatalf("non-select Allow32x32 should bypass demotion: got tx mode = %d, want Allow32x32", got)
	}

	counts = &vp9enc.FrameCounts{}
	counts.TxTotals[common.Tx4x4] = 1
	if got := vp9EncoderFrameTxModeFromCounts(common.Allow32x32, false, true, counts); got != common.Allow32x32 {
		t.Fatalf("non-select Allow32x32 should bypass demotion: got tx mode = %d, want Allow32x32", got)
	}

	counts = &vp9enc.FrameCounts{}
	counts.TxTotals[common.Tx32x32] = 1
	if got := vp9EncoderFrameTxModeFromCounts(common.Allow32x32, false, true, counts); got != common.Allow32x32 {
		t.Fatalf("non-select Allow32x32 with Tx32x32 hits: got tx mode = %d, want Allow32x32", got)
	}
}

// TestVP9EncoderFrameTxModeFromCountsLibvpxSelectLadder pins the
// verbatim libvpx vp9/encoder/vp9_encodeframe.c:5911-5944 demotion
// ladder for TX_MODE_SELECT — partition-context counts
// (counts.TxMode.{P8x8, P16x16, P32x32}) are bucketed into six
// trackers and tested against the four libvpx demotion cascade
// thresholds in order:
//
//   - ALLOW_8X8 (vp9_encodeframe.c:5930-5933)
//   - ONLY_4X4 (vp9_encodeframe.c:5934-5937)
//   - ALLOW_32X32 (vp9_encodeframe.c:5938-5939)
//   - ALLOW_16X16 (vp9_encodeframe.c:5940-5943)
//
// Untouched-counts (all six trackers zero) hits the ALLOW_8X8 branch
// first per the libvpx if/else chain — this is the "no statistics =
// degenerate frame" libvpx behaviour and is intentional.

func TestVP9EncoderFrameTxModeFromCountsLibvpxSelectLadder(t *testing.T) {
	// ALLOW_8X8 — only 8x8 counters non-zero anywhere.
	// libvpx vp9_encodeframe.c:5930-5933.
	counts := &vp9enc.FrameCounts{}
	counts.TxMode.P8x8[0][common.Tx8x8] = 5
	counts.TxMode.P16x16[1][common.Tx8x8] = 2 // count8x8_lp != 0 OK
	if got := vp9EncoderFrameTxModeFromCounts(common.TxModeSelect, false, true, counts); got != common.Allow8x8 {
		t.Fatalf("ALLOW_8X8 demotion: got tx_mode = %d, want Allow8x8", got)
	}

	// ONLY_4X4 — every non-4x4 counter zero.
	// libvpx vp9_encodeframe.c:5934-5937.
	counts = &vp9enc.FrameCounts{}
	counts.TxMode.P32x32[0][common.Tx4x4] = 3
	counts.TxMode.P16x16[1][common.Tx4x4] = 4
	counts.TxMode.P8x8[0][common.Tx4x4] = 1
	if got := vp9EncoderFrameTxModeFromCounts(common.TxModeSelect, false, true, counts); got != common.Only4x4 {
		t.Fatalf("ONLY_4X4 demotion: got tx_mode = %d, want Only4x4", got)
	}

	// ALLOW_32X32 — count8x8_lp == count16x16_lp == count4x4 == 0,
	// but count16x16p16x16 != 0 (i.e. p16x16 picked 16x16) and
	// count32x32 != 0. libvpx vp9_encodeframe.c:5938-5939.
	counts = &vp9enc.FrameCounts{}
	counts.TxMode.P32x32[0][common.Tx32x32] = 7
	counts.TxMode.P16x16[1][common.Tx16x16] = 2
	counts.TxMode.P8x8[0][common.Tx8x8] = 3 // count8x8_8x8p != 0 OK
	if got := vp9EncoderFrameTxModeFromCounts(common.TxModeSelect, false, true, counts); got != common.Allow32x32 {
		t.Fatalf("ALLOW_32X32 demotion: got tx_mode = %d, want Allow32x32", got)
	}

	// ALLOW_16X16 — count32x32 == count8x8_lp == count4x4 == 0 but
	// count16x16_lp != 0 (p32x32 picked 16x16). libvpx
	// vp9_encodeframe.c:5940-5943.
	counts = &vp9enc.FrameCounts{}
	counts.TxMode.P32x32[0][common.Tx16x16] = 4 // count16x16_lp != 0
	counts.TxMode.P16x16[1][common.Tx16x16] = 1
	counts.TxMode.P8x8[0][common.Tx8x8] = 2 // count8x8_8x8p != 0 OK
	if got := vp9EncoderFrameTxModeFromCounts(common.TxModeSelect, false, true, counts); got != common.Allow16x16 {
		t.Fatalf("ALLOW_16X16 demotion: got tx_mode = %d, want Allow16x16", got)
	}

	// No demotion — every bucket has a non-zero entry so the libvpx
	// if/else chain at vp9_encodeframe.c:5930-5943 falls through and
	// leaves cm->tx_mode at TX_MODE_SELECT.
	counts = &vp9enc.FrameCounts{}
	counts.TxMode.P32x32[0][common.Tx32x32] = 1
	counts.TxMode.P32x32[0][common.Tx16x16] = 1 // count16x16_lp != 0
	counts.TxMode.P32x32[0][common.Tx8x8] = 1   // count8x8_lp != 0
	counts.TxMode.P32x32[0][common.Tx4x4] = 1   // count4x4 != 0
	counts.TxMode.P16x16[0][common.Tx16x16] = 1
	counts.TxMode.P8x8[0][common.Tx8x8] = 1
	if got := vp9EncoderFrameTxModeFromCounts(common.TxModeSelect, false, true, counts); got != common.TxModeSelect {
		t.Fatalf("no-demotion fall-through: got tx_mode = %d, want TxModeSelect", got)
	}

	// frame_parameter_update=false — libvpx vp9_encodeframe.c:5846
	// gates the entire post-encode demotion block on the speed
	// feature. Even with degenerate (all-zero) partition counts the
	// TxModeSelect input must pass through unchanged when the gate is
	// off (matches RT speed>=4 path at vp9_speed_features.c:568).
	counts = &vp9enc.FrameCounts{}
	if got := vp9EncoderFrameTxModeFromCounts(common.TxModeSelect, false, false, counts); got != common.TxModeSelect {
		t.Fatalf("frame_parameter_update=false: got tx_mode = %d, want TxModeSelect", got)
	}
}

// TestVP9EncoderFrameTxModeMirrorsLibvpxSelectTxMode pins
// vp9EncoderFrameTxMode against the libvpx vp9/encoder/
// vp9_encodeframe.c:4334-4345 select_tx_mode truth table.
// The KEY_FRAME && use_nonrd_pick_mode -> ALLOW_16X16 clamp is the
// signature change a843f45d introduced; intra-only frames now route
// through the non-key dispatch (libvpx's `cm->frame_type == KEY_FRAME`
// predicate is literal at vp9_encodeframe.c:4336) — they pick up
// USE_TX_8X8 from the RT cpu_used >= 5 leg
// (vp9_speed_features.c:1541) and resolve to TX_MODE_SELECT.
// Keyframes at cpu_used=0 (RT speed=0, use_nonrd_pick_mode=0,
// tx_size_search_method=USE_FULL_RD) now route to TX_MODE_SELECT
// per vp9_encodeframe.c:4340-4342 — the unified
// write_mb_modes_kf at vp9_bitstream.c:344-376 services both
// frame types from a single TxModeSelect-shaped tx_probs row.

func TestVP9EncoderFrameTxModeMirrorsLibvpxSelectTxMode(t *testing.T) {
	for _, tc := range []struct {
		name      string
		deadline  Deadline
		cpuUsed   int8
		isKey     bool
		intraOnly bool
		lossless  bool
		want      common.TxMode
	}{
		{name: "lossless", deadline: DeadlineRealtime, cpuUsed: vp9DefaultCPUUsed, isKey: true, lossless: true, want: common.Only4x4},
		{name: "lossless-inter", deadline: DeadlineRealtime, cpuUsed: vp9DefaultCPUUsed, lossless: true, want: common.Only4x4},
		{name: "keyframe-nonrd-allow16x16", deadline: DeadlineRealtime, cpuUsed: vp9DefaultCPUUsed, isKey: true, want: common.Allow16x16},
		// libvpx vp9_speed_features.c:855 default sets
		// sf.tx_size_search_method = USE_FULL_RD; the RT speed>=1 leg at
		// :492-493 (and the GOOD speed>=2 leg at :326-327, :381-382)
		// overrides it. At RT speed=0 the configurator leaves the default
		// in place, so select_tx_mode at vp9_encodeframe.c:4340-4342
		// returns TX_MODE_SELECT. This is the seed #0 surface the
		// FuzzVP9OracleEncoderRuntimeControls fuzz exercises.
		{name: "keyframe-rt-cpu0-uses-tx-mode-select", deadline: DeadlineRealtime, cpuUsed: 0, isKey: true, want: common.TxModeSelect},
		// At RT speed>=1, sf.tx_size_search_method =
		// frame_is_intra_only(cm) ? USE_FULL_RD : USE_LARGESTALL
		// (vp9_speed_features.c:492-493). KEY_FRAME satisfies
		// frame_is_intra_only (vp9_onyxc_int.h:363-365), so keyframes at
		// RT speed=1..4 also see USE_FULL_RD -> TX_MODE_SELECT
		// (vp9_encodeframe.c:4340-4342).
		{name: "keyframe-rt-cpu1-uses-tx-mode-select", deadline: DeadlineRealtime, cpuUsed: 1, isKey: true, want: common.TxModeSelect},
		// Non-key inter at RT speed=1..3: tx_size_search_method =
		// USE_LARGESTALL -> ALLOW_32X32.
		{name: "inter-rt-cpu1-uses-allow32x32", deadline: DeadlineRealtime, cpuUsed: 1, want: common.Allow32x32},
		{name: "inter-rt-cpu3-uses-allow32x32", deadline: DeadlineRealtime, cpuUsed: 3, want: common.Allow32x32},
		// RT speed=4 switches non-key inter to USE_TX_8X8 -> TX_MODE_SELECT.
		{name: "inter-rt-cpu4-uses-tx-mode-select", deadline: DeadlineRealtime, cpuUsed: 4, want: common.TxModeSelect},
		// GOOD speed=4 keeps USE_LARGESTALL -> ALLOW_32X32.
		{name: "inter-good-cpu4-uses-allow32x32", deadline: DeadlineGoodQuality, cpuUsed: 4, want: common.Allow32x32},
		{name: "intra-only-uses-tx-mode-select", deadline: DeadlineRealtime, cpuUsed: vp9DefaultCPUUsed, intraOnly: true, want: common.TxModeSelect},
		{name: "inter-uses-tx-mode-select", deadline: DeadlineRealtime, cpuUsed: vp9DefaultCPUUsed, want: common.TxModeSelect},
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
			if tc.cpuUsed == 0 {
				e.opts.CpuUsed = 0
			}
			// Mirror the per-frame SF refresh
			// encodeVP9FrameIntoWithFlagsResultInternal runs before
			// vp9EncoderFrameTxMode at libvpx vp9_encoder.c:3754 /
			// 3765 so e.sf carries the live per-frame value.
			e.vp9ApplySpeedFeatures(e.vp9PerFrameSpeedContext(vp9PerFrameSpeedContextArgs{
				IsKey:     tc.isKey,
				IntraOnly: tc.intraOnly,
				ShowFrame: true,
			}))
			got := e.vp9EncoderFrameTxMode(tc.isKey, tc.intraOnly, tc.lossless)
			if got != tc.want {
				t.Fatalf("vp9EncoderFrameTxMode(isKey=%t intraOnly=%t lossless=%t cpuUsed=%d) = %d, want %d",
					tc.isKey, tc.intraOnly, tc.lossless, tc.cpuUsed, got, tc.want)
			}
		})
	}
}

// TestVP9InterCalculateTxSizeMirrorsLibvpx pins govpx's
// vp9InterCalculateTxSize port against the libvpx
// vp9/encoder/vp9_pickmode.c:363-393 calculate_tx_size truth table for
// the inter path (is_intra=0, var_thresh=1). The cases cover all four
// limit_tx branches (CYCLIC_REFRESH_AQ + source/residual var zero,
// boosted segment, default Tx16x16 cap, non-TxModeSelect bypass) plus
// the TX_MODE_SELECT sse > var*4 split.
