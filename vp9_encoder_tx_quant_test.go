package govpx

import (
	"errors"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9enc "github.com/thesyncim/govpx/internal/vp9/encoder"
)

func TestVP9EncoderRejectsInvalidSourceShape(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	dst := make([]byte, 1024)

	if _, err := e.EncodeInto(nil, dst); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil source err = %v, want ErrInvalidConfig", err)
	}

	wrongSize := image.NewYCbCr(image.Rect(0, 0, 32, 64), image.YCbCrSubsampleRatio420)
	if _, err := e.EncodeInto(wrongSize, dst); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("wrong-size source err = %v, want ErrInvalidConfig", err)
	}

	wrongChroma := image.NewYCbCr(image.Rect(0, 0, 64, 64), image.YCbCrSubsampleRatio444)
	if _, err := e.EncodeInto(wrongChroma, dst); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("wrong-chroma source err = %v, want ErrInvalidConfig", err)
	}

	valid := image.NewYCbCr(image.Rect(0, 0, 64, 64), image.YCbCrSubsampleRatio420)
	if _, err := e.EncodeInto(valid, nil); !errors.Is(err, ErrBufferTooSmall) {
		t.Fatalf("empty dst err = %v, want ErrBufferTooSmall", err)
	}
}

// TestVP9EncoderFrameTxModeFromCountsBypassesNonSelect pins libvpx
// vp9_encodeframe.c:5911 — the post-encode tx_mode demotion is gated
// on `cm->tx_mode == TX_MODE_SELECT`, so any fixed tx_mode emitted by
// select_tx_mode is written verbatim to the bitstream regardless of
// counts. The libvpx-faithful TX_MODE_SELECT partition-context ladder
// lives in TestVP9EncoderFrameTxModeFromCountsLibvpxSelectLadder.
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
// vp9_encodeframe.c:4334-4345 select_tx_mode truth table for the
// realtime cpu_used=8 surface that drives govpx's byte-parity matrix.
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
		// Non-key inter at RT speed=1: tx_size_search_method =
		// USE_LARGESTALL -> ALLOW_32X32. Pinned at TX_MODE_SELECT in
		// govpx's vp9EncoderFrameTxMode (libvpx fallthrough) to preserve
		// byte parity against the established golden corpus — see the
		// comment block in vp9_encoder.go documenting the inter pin.
		{name: "inter-rt-cpu1-keeps-tx-mode-select", deadline: DeadlineRealtime, cpuUsed: 1, want: common.TxModeSelect},
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
func TestVP9InterCalculateTxSizeMirrorsLibvpx(t *testing.T) {
	t.Helper()
	type tcase struct {
		name      string
		aqMode    VP9AQMode
		screen    bool
		bsize     common.BlockSize
		txMode    common.TxMode
		sse       uint64
		residVar  uint64
		srcVar    uint64
		acThr     int64
		segmentID uint8
		want      common.TxSize
	}
	cases := []tcase{
		{
			// Default inter, textured 64x64: limit_tx=1, sse>var*4 →
			// libvpx caps Tx32x32 → Tx16x16 (vp9_pickmode.c:383-384).
			name:   "default-textured-64x64-caps-to-tx16",
			bsize:  common.Block64x64,
			txMode: common.TxModeSelect,
			sse:    1 << 20, residVar: 1 << 14,
			want: common.Tx16x16,
		},
		{
			// Default inter, smooth: tx_size = Tx8x8 branch
			// (vp9_pickmode.c:378-379).
			name:   "default-smooth-64x64-tx8",
			bsize:  common.Block64x64,
			txMode: common.TxModeSelect,
			sse:    1 << 14, residVar: 1 << 14,
			want: common.Tx8x8,
		},
		{
			// CYCLIC_REFRESH_AQ + source_variance==0: limit_tx=0, lifts
			// the Tx16x16 cap so 64x64 inter goes to Tx32x32
			// (vp9_pickmode.c:371-373, 383-384).
			name:   "cr-aq-source-var-zero-64x64-allows-tx32",
			aqMode: VP9AQCyclicRefresh,
			bsize:  common.Block64x64, txMode: common.TxModeSelect,
			sse: 1 << 20, residVar: 1 << 14, srcVar: 0,
			want: common.Tx32x32,
		},
		{
			// CYCLIC_REFRESH_AQ + residual var==0: same escape via the
			// (var < var_thresh) leg.
			name:   "cr-aq-residual-var-zero-64x64-allows-tx32",
			aqMode: VP9AQCyclicRefresh,
			bsize:  common.Block64x64, txMode: common.TxModeSelect,
			sse: 1 << 20, residVar: 0, srcVar: 1 << 10,
			want: common.Tx32x32,
		},
		{
			// CYCLIC_REFRESH_AQ + textured (var>0, src>0): limit_tx=1
			// applies, Tx16x16 cap holds.
			name:   "cr-aq-textured-64x64-caps-to-tx16",
			aqMode: VP9AQCyclicRefresh,
			bsize:  common.Block64x64, txMode: common.TxModeSelect,
			sse: 1 << 20, residVar: 1 << 14, srcVar: 1 << 14,
			want: common.Tx16x16,
		},
		{
			// CYCLIC_REFRESH_AQ + boosted segment + limit_tx=1: forced
			// Tx8x8 (vp9_pickmode.c:380-382).
			name:   "cr-aq-boosted-segment-forces-tx8",
			aqMode: VP9AQCyclicRefresh,
			bsize:  common.Block64x64, txMode: common.TxModeSelect,
			sse: 1 << 20, residVar: 1 << 14, srcVar: 1 << 14,
			segmentID: vp9enc.CyclicRefreshSegmentBoost1,
			want:      common.Tx8x8,
		},
		{
			// Non-TxModeSelect: tx_size = min(max_txsize_lookup,
			// tx_mode_to_biggest_tx_size) (vp9_pickmode.c:389-391).
			name:  "allow8x8-32x32-block-clamps-to-tx8",
			bsize: common.Block32x32, txMode: common.Allow8x8,
			sse: 1 << 20, residVar: 1 << 10,
			want: common.Tx8x8,
		},
		{
			// VP9E_CONTENT_SCREEN + Tx8x8 result + (var>>5) > ac_thr
			// forces Tx4x4 (vp9_pickmode.c:386-388).
			name:   "screen-content-forces-tx4-over-tx8",
			screen: true,
			bsize:  common.Block16x16, txMode: common.TxModeSelect,
			sse: 1 << 10, residVar: 1 << 16, srcVar: 1 << 16, acThr: 100,
			want: common.Tx4x4,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := &VP9Encoder{}
			e.opts.AQMode = tc.aqMode
			if tc.screen {
				e.opts.ScreenContentMode = int8(VP9ScreenContentScreen)
			}
			got := e.vp9InterCalculateTxSize(tc.bsize, tc.txMode, tc.sse,
				tc.residVar, tc.srcVar, tc.acThr, tc.segmentID)
			if got != tc.want {
				t.Fatalf("vp9InterCalculateTxSize = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestVP9InterTxApplyForcesMirrorsLibvpx pins the live picker
// post-pass vp9InterTxApplyForces against the libvpx
// vp9/encoder/vp9_pickmode.c:380-388 force cascade. The cases cover:
//
//   - Boosted-segment Tx8x8 force (vp9_pickmode.c:380-382).
//   - Tx16x16 cap (vp9_pickmode.c:383-384).
//   - VP9E_CONTENT_SCREEN Tx4x4 force (vp9_pickmode.c:386-388) — both
//     `(var >> 5) > ac_thr` firing and not firing, plus the
//     bsize <= BLOCK_16X16 gate.
func TestVP9InterTxApplyForcesMirrorsLibvpx(t *testing.T) {
	t.Helper()
	type tcase struct {
		name      string
		aqMode    VP9AQMode
		screen    bool
		tx        common.TxSize
		bsize     common.BlockSize
		residVar  uint64
		acThr     int64
		limitTx   bool
		segmentID uint8
		want      common.TxSize
	}
	cases := []tcase{
		{
			// Boosted-segment Tx8x8 force (vp9_pickmode.c:380-382).
			name:    "cr-aq-boosted-forces-tx8",
			aqMode:  VP9AQCyclicRefresh,
			tx:      common.Tx32x32,
			bsize:   common.Block64x64,
			limitTx: true, segmentID: vp9enc.CyclicRefreshSegmentBoost1,
			want: common.Tx8x8,
		},
		{
			// Tx16x16 cap (vp9_pickmode.c:383-384). Non-boosted CR-AQ
			// limit_tx=1 + Tx32x32 -> Tx16x16.
			name:    "cr-aq-non-boosted-caps-tx16",
			aqMode:  VP9AQCyclicRefresh,
			tx:      common.Tx32x32,
			bsize:   common.Block64x64,
			limitTx: true,
			want:    common.Tx16x16,
		},
		{
			// limit_tx=0 lifts the Tx16x16 cap.
			name:    "cr-aq-limit-tx-off-keeps-tx32",
			aqMode:  VP9AQCyclicRefresh,
			tx:      common.Tx32x32,
			bsize:   common.Block64x64,
			limitTx: false,
			want:    common.Tx32x32,
		},
		{
			// VP9E_CONTENT_SCREEN: Tx8x8 + (residVar >> 5) > acThr +
			// bsize <= BLOCK_16X16 -> Tx4x4 (vp9_pickmode.c:386-388).
			name:     "screen-content-forces-tx4-over-tx8",
			screen:   true,
			tx:       common.Tx8x8,
			bsize:    common.Block16x16,
			residVar: 1 << 16, acThr: 100, limitTx: true,
			want: common.Tx4x4,
		},
		{
			// VP9E_CONTENT_SCREEN: (residVar >> 5) <= acThr -> Tx8x8
			// stays put.
			name:     "screen-content-low-var-keeps-tx8",
			screen:   true,
			tx:       common.Tx8x8,
			bsize:    common.Block16x16,
			residVar: 1 << 5, acThr: 100, limitTx: true,
			want: common.Tx8x8,
		},
		{
			// VP9E_CONTENT_SCREEN: bsize > BLOCK_16X16 -> force does not
			// fire (vp9_pickmode.c:387 `bsize <= BLOCK_16X16`).
			name:     "screen-content-large-bsize-keeps-tx8",
			screen:   true,
			tx:       common.Tx8x8,
			bsize:    common.Block32x32,
			residVar: 1 << 16, acThr: 100, limitTx: true,
			want: common.Tx8x8,
		},
		{
			// Non-screen content: even with large var, no Tx4x4 force.
			name:     "default-content-no-tx4-force",
			tx:       common.Tx8x8,
			bsize:    common.Block16x16,
			residVar: 1 << 16, acThr: 100, limitTx: true,
			want: common.Tx8x8,
		},
		{
			// acThr <= 0 disables the screen-content force regardless of
			// residVar — govpx returns acThr=0 when the quantizer plumb
			// is unavailable.
			name:     "screen-content-zero-acthr-keeps-tx8",
			screen:   true,
			tx:       common.Tx8x8,
			bsize:    common.Block16x16,
			residVar: 1 << 16, acThr: 0, limitTx: true,
			want: common.Tx8x8,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := &VP9Encoder{}
			e.opts.AQMode = tc.aqMode
			if tc.screen {
				e.opts.ScreenContentMode = int8(VP9ScreenContentScreen)
			}
			got := e.vp9InterTxApplyForces(tc.tx, tc.bsize, tc.residVar,
				tc.acThr, tc.limitTx, tc.segmentID)
			if got != tc.want {
				t.Fatalf("vp9InterTxApplyForces = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestVP9CyclicRefreshSegmentIDBoostedMirrorsLibvpx pins the
// cyclic_refresh_segment_id_boosted port at libvpx
// vp9/encoder/vp9_aq_cyclicrefresh.h:127-130.
func TestVP9CyclicRefreshSegmentIDBoostedMirrorsLibvpx(t *testing.T) {
	if vp9enc.CyclicRefreshSegmentIDBoosted(vp9enc.CyclicRefreshSegmentBase) {
		t.Fatalf("base segment must not be boosted")
	}
	if !vp9enc.CyclicRefreshSegmentIDBoosted(vp9enc.CyclicRefreshSegmentBoost1) {
		t.Fatalf("BOOST1 must be boosted")
	}
	if !vp9enc.CyclicRefreshSegmentIDBoosted(vp9enc.CyclicRefreshSegmentBoost2) {
		t.Fatalf("BOOST2 must be boosted")
	}
	if vp9enc.CyclicRefreshSegmentIDBoosted(7) {
		t.Fatalf("non-CR segments must not be boosted")
	}
}

// TestVP9EncoderKeyframeStubProducesParseableBitstream: the constant
// source-backed keyframe path emits oracle-shaped Block32x32 / Tx16 DC
// skip leaves whose every layer parses cleanly through the decoder.
func TestVP9EncoderKeyframeStubProducesParseableBitstream(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	img := vp9test.NewYCbCr(64, 64, 128, 128, 128)
	got, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("Encode returned empty bytes")
	}

	// Layer 1: uncompressed header.
	var br vp9dec.BitReader
	br.Init(got)
	h, perr := vp9dec.ReadUncompressedHeader(&br, nil, nil)
	if perr != nil {
		t.Fatalf("ReadUncompressedHeader: %v", perr)
	}
	if h.Width != 64 || h.Height != 64 {
		t.Errorf("size = (%d, %d), want (64, 64)", h.Width, h.Height)
	}
	if h.FrameType != common.KeyFrame {
		t.Errorf("FrameType = %d, want KeyFrame", h.FrameType)
	}
	if h.FirstPartitionSize == 0 {
		t.Fatal("FirstPartitionSize = 0 (compressed header empty)")
	}
	uncSize := br.BytesRead()

	// Layer 2: compressed header. The encoder may emit counts-driven
	// probability updates, so the parsed frame context is the one the
	// tile body must use.
	compEnd := uncSize + int(h.FirstPartitionSize)
	if compEnd > len(got) {
		t.Fatalf("compressed header end %d past frame %d", compEnd, len(got))
	}
	var cr bitstream.Reader
	if err := cr.Init(got[uncSize:compEnd]); err != nil {
		t.Fatalf("compressed reader Init: %v", err)
	}
	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	out := vp9dec.ReadCompressedHeader(&cr, &fc, vp9dec.ReadCompressedHeaderArgs{
		Lossless:     false,
		IntraOnly:    true,
		KeyFrame:     true,
		InterpFilter: vp9dec.InterpEighttap,
	})
	if out.TxMode != common.Allow16x16 {
		t.Errorf("TxMode = %d, want Allow16x16", out.TxMode)
	}

	// Layer 3: tile body via full packet decode.
	grid := decodeVP9PacketMiGridForOracleTest(t, got)
	if len(grid) != 64 {
		t.Fatalf("decoded mi grid len = %d, want 64", len(grid))
	}
	for i, mi := range grid {
		if mi.SbType != common.Block32x32 || mi.TxSize != common.Tx16x16 ||
			mi.Mode != common.DcPred || mi.Skip != 1 ||
			mi.RefFrame[0] != vp9dec.IntraFrame {
			t.Fatalf("mi[%d] = %+v, want Block32x32/Tx16/DcPred/skip intra", i, mi)
		}
	}
}

func TestVP9EncoderKeyframeConstantSourceRoundTrip(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 96, Height: 80})
	img := vp9test.NewYCbCr(96, 80, 91, 143, 37)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after source-backed keyframe")
	}
	assertVP9FilledFrameWithin(t, frame, 96, 80, 91, 143, 37, 1)
}

func TestVP9EncoderKeyframeACResiduePreservesCheckerSource(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 32, Height: 32})
	img := vp9test.NewCheckerYCbCr(32, 32, 48, 208, 128, 128)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after checker keyframe")
	}
	assertVP9VisibleYContrast(t, frame, 32, 32, 40)
}

func TestVP9EncoderACKeyframeUsesOracleNoUpdateCompressedHeader(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	img := vp9test.NewCheckerYCbCr(64, 64, 48, 208, 128, 128)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	var br vp9dec.BitReader
	br.Init(packet)
	h, err := vp9dec.ReadUncompressedHeader(&br, nil, nil)
	if err != nil {
		t.Fatalf("ReadUncompressedHeader: %v", err)
	}
	if h.FirstPartitionSize != 2 {
		t.Fatalf("FirstPartitionSize = %d, want oracle no-update compressed header size 2", h.FirstPartitionSize)
	}
}

func TestVP9EncoderDefaultQuantizerUsesPinnedCQBaseQIndex(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	img := vp9test.NewCheckerYCbCr(64, 64, 32, 224, 128, 128)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	h, _ := vp9test.ParseHeader(t, packet)
	if got := int(h.Quant.BaseQindex); got != vp9DefaultBaseQIndex {
		t.Fatalf("BaseQindex = %d, want pinned default %d",
			got, vp9DefaultBaseQIndex)
	}
}

func TestVP9EncoderDefaultInterQuantizerUsesPinnedCQBaseQIndex(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	src := vp9test.NewYCbCr(64, 64, 128, 128, 128)
	key, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	inter, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	var keyBR vp9dec.BitReader
	keyBR.Init(key)
	keyHeader, err := vp9dec.ReadUncompressedHeader(&keyBR, nil, nil)
	if err != nil {
		t.Fatalf("ReadUncompressedHeader keyframe: %v", err)
	}
	var interBR vp9dec.BitReader
	interBR.Init(inter)
	interHeader, err := vp9dec.ReadUncompressedHeader(&interBR, &keyHeader,
		func(uint8) (uint32, uint32) { return 64, 64 })
	if err != nil {
		t.Fatalf("ReadUncompressedHeader inter: %v", err)
	}
	if got := int(interHeader.Quant.BaseQindex); got != vp9DefaultInterBaseQIndex {
		t.Fatalf("inter BaseQindex = %d, want pinned default %d",
			got, vp9DefaultInterBaseQIndex)
	}
}

func TestVP9EncoderPublicFixedQuantizerControlsQIndex(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:        width,
		Height:       height,
		MinQuantizer: 20,
		MaxQuantizer: 20,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	src := vp9test.NewYCbCr(width, height, 128, 128, 128)
	key, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	inter, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	wantQIndex := vp9enc.PublicQuantizerToQIndex(20)
	keyHeader, _ := vp9test.ParseHeader(t, key)
	if got := int(keyHeader.Quant.BaseQindex); got != wantQIndex {
		t.Fatalf("key BaseQindex = %d, want %d", got, wantQIndex)
	}
	interHeader, _ := vp9test.ParseHeader(t, inter)
	if got := int(interHeader.Quant.BaseQindex); got != wantQIndex {
		t.Fatalf("inter BaseQindex = %d, want %d", got, wantQIndex)
	}
}

func TestVP9EncoderExplicitQuantizerOverridesDefault(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width:     64,
		Height:    64,
		Quantizer: 1,
	})
	img := vp9test.NewCheckerYCbCr(64, 64, 32, 224, 128, 128)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	h, _ := vp9test.ParseHeader(t, packet)
	if h.Quant.BaseQindex != 1 {
		t.Fatalf("BaseQindex = %d, want explicit qindex 1", h.Quant.BaseQindex)
	}
}

func TestVP9EncoderLosslessKeyframeRoundTripExact(t *testing.T) {
	const width, height = 32, 32
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:    width,
		Height:   height,
		Lossless: true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	img := vp9test.NewCheckerYCbCr(width, height, 16, 240, 32, 224)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode lossless keyframe: %v", err)
	}

	var br vp9dec.BitReader
	br.Init(packet)
	h, err := vp9dec.ReadUncompressedHeader(&br, nil, nil)
	if err != nil {
		t.Fatalf("ReadUncompressedHeader: %v", err)
	}
	if h.Quant.BaseQindex != 0 || !h.Quant.Lossless {
		t.Fatalf("quantization = %+v, want lossless qindex 0", h.Quant)
	}
	if h.Loopfilter.FilterLevel != 0 {
		t.Fatalf("loop filter level = %d, want 0 for lossless", h.Loopfilter.FilterLevel)
	}
	uncSize := br.BytesRead()
	compEnd := uncSize + int(h.FirstPartitionSize)
	if compEnd > len(packet) {
		t.Fatalf("compressed header end %d past packet len %d", compEnd, len(packet))
	}
	var cr bitstream.Reader
	if err := cr.Init(packet[uncSize:compEnd]); err != nil {
		t.Fatalf("compressed reader Init: %v", err)
	}
	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	out := vp9dec.ReadCompressedHeader(&cr, &fc, vp9dec.ReadCompressedHeaderArgs{
		Lossless:     h.Quant.Lossless,
		IntraOnly:    true,
		KeyFrame:     true,
		InterpFilter: vp9dec.InterpEighttap,
	})
	if out.TxMode != common.Only4x4 {
		t.Fatalf("TxMode = %d, want Only4x4 for lossless", out.TxMode)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode lossless keyframe: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after lossless keyframe")
	}
	assertVP9ImageMatchesYCbCr(t, "lossless keyframe", frame, img)
}

func TestVP9EncoderLosslessInterRoundTripExact(t *testing.T) {
	const width, height = 32, 32
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:    width,
		Height:   height,
		Lossless: true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	keySrc := vp9test.NewMotionYCbCr(width, height)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode lossless keyframe: %v", err)
	}
	interSrc := vp9test.NewCheckerYCbCr(width, height, 16, 240, 32, 224)
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode lossless inter: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode lossless keyframe: %v", err)
	}
	keyFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after lossless keyframe")
	}
	assertVP9ImageMatchesYCbCr(t, "lossless keyframe", keyFrame, keySrc)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode lossless inter: %v", err)
	}
	interFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after lossless inter frame")
	}
	assertVP9ImageMatchesYCbCr(t, "lossless inter frame", interFrame, interSrc)
}
