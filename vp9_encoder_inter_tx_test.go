package govpx

import (
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9enc "github.com/thesyncim/govpx/internal/vp9/encoder"
)

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

func TestVP9InterTxResidualStatsMatchesScalarReference(t *testing.T) {
	const width, height = 80, 80
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	for y := 0; y < height; y++ {
		row := img.Y[y*img.YStride:]
		for x := 0; x < width; x++ {
			row[x] = byte((x*13 + y*29 + x*y) & 0xff)
		}
	}

	e := &VP9Encoder{}
	e.prepareVP9EncoderOutputFrame(width, height)
	for y := 0; y < height; y++ {
		row := e.reconY[y*e.reconFrame.YStride:]
		for x := 0; x < width; x++ {
			row[x] = byte((x*7 + y*11 + 19) & 0xff)
		}
	}
	inter := &vp9InterEncodeState{img: img}
	cases := []struct {
		name         string
		miRow, miCol int
		bsize        common.BlockSize
	}{
		{name: "64x64", miRow: 1, miCol: 1, bsize: common.Block64x64},
		{name: "32x32", miRow: 3, miCol: 2, bsize: common.Block32x32},
		{name: "16x16", miRow: 5, miCol: 4, bsize: common.Block16x16},
		{name: "8x8", miRow: 8, miCol: 8, bsize: common.Block8x8},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotSSE, gotActivity, ok := e.vp9InterTxResidualStats(inter, tc.miRow, tc.miCol, tc.bsize)
			if !ok {
				t.Fatal("vp9InterTxResidualStats returned !ok")
			}
			wantSSE, wantActivity := vp9InterTxResidualStatsScalarReference(img, e, tc.miRow, tc.miCol, tc.bsize)
			if gotSSE != wantSSE || gotActivity != wantActivity {
				t.Fatalf("stats = sse/activity %d/%d, want %d/%d",
					gotSSE, gotActivity, wantSSE, wantActivity)
			}
		})
	}
}

func vp9InterTxResidualStatsScalarReference(img *image.YCbCr, e *VP9Encoder,
	miRow, miCol int, bsize common.BlockSize,
) (sse, activity uint64) {
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	for y := range blockH {
		srcRow := img.Y[(y0+y)*img.YStride:]
		predRow := e.reconY[(y0+y)*e.reconFrame.YStride:]
		for x := range blockW {
			diff := int(srcRow[x0+x]) - int(predRow[x0+x])
			sse += uint64(diff * diff)
			if x > 0 {
				leftDiff := int(srcRow[x0+x-1]) - int(predRow[x0+x-1])
				activity += uint64(vp9AbsInt(diff - leftDiff))
			}
			if y > 0 {
				upDiff := int(img.Y[(y0+y-1)*img.YStride+x0+x]) -
					int(e.reconY[(y0+y-1)*e.reconFrame.YStride+x0+x])
				activity += uint64(vp9AbsInt(diff - upDiff))
			}
		}
	}
	return sse, activity
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
			// libvpx casts ac_thr to unsigned in the comparison. If the
			// caller supplies zero, any non-zero (var >> 5) still forces
			// Tx4x4.
			name:     "screen-content-zero-acthr-still-forces-tx4",
			screen:   true,
			tx:       common.Tx8x8,
			bsize:    common.Block16x16,
			residVar: 1 << 16, acThr: 0, limitTx: true,
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
			got := e.vp9InterTxApplyForces(tc.tx, tc.bsize, tc.residVar,
				tc.acThr, tc.limitTx, tc.segmentID)
			if got != tc.want {
				t.Fatalf("vp9InterTxApplyForces = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestVP9InterCalculateTxAcThrUsesSegmentQIndex(t *testing.T) {
	seg := vp9dec.SegmentationParams{Enabled: true}
	seg.FeatureMask[vp9enc.CyclicRefreshSegmentBoost1] =
		1 << uint(vp9dec.SegLvlAltQ)
	seg.FeatureData[vp9enc.CyclicRefreshSegmentBoost1][vp9dec.SegLvlAltQ] = -40
	e := &VP9Encoder{}
	e.vp9HeaderScratch.Seg = seg

	var dq vp9dec.DequantTables
	vp9dec.SetupSegmentationDequant(&seg, vp9dec.SetupSegmentationDequantArgs{
		BaseQindex: 100,
		BitDepth:   vp9dec.Bits8,
	}, &dq)
	inter := &vp9InterEncodeState{
		dq:         &dq,
		baseQindex: 100,
	}
	got := e.vp9InterCalculateTxAcThr(inter, vp9enc.CyclicRefreshSegmentBoost1)
	_, want := vp9enc.ModelRdQuantThresholds(60,
		dq.Y[vp9enc.CyclicRefreshSegmentBoost1])
	if got != want {
		t.Fatalf("vp9InterCalculateTxAcThr = %d, want segment-q threshold %d",
			got, want)
	}
	if got := e.vp9SegmentQIndex(inter, vp9enc.CyclicRefreshSegmentBoost1); got != 60 {
		t.Fatalf("vp9SegmentQIndex = %d, want 60", got)
	}
}

func TestVP9InterTxRDSearchDepthMirrorsLibvpx(t *testing.T) {
	tests := []struct {
		name    string
		maxTx   common.TxSize
		depth   int
		bsize   common.BlockSize
		wantMin common.TxSize
	}{
		{
			name:    "tx8-depth-reaches-tx4",
			maxTx:   common.Tx8x8,
			depth:   2,
			bsize:   common.Block8x8,
			wantMin: common.Tx4x4,
		},
		{
			name:    "tx16-depth-reaches-tx4",
			maxTx:   common.Tx16x16,
			depth:   2,
			bsize:   common.Block16x16,
			wantMin: common.Tx4x4,
		},
		{
			name:    "block64-bumps-end-up-one",
			maxTx:   common.Tx32x32,
			depth:   2,
			bsize:   common.Block64x64,
			wantMin: common.Tx16x16,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := vp9InterTxRDSearchMin(tt.maxTx, tt.depth, tt.bsize)
			if got != tt.wantMin {
				t.Fatalf("vp9InterTxRDSearchMin = %d, want %d", got, tt.wantMin)
			}
		})
	}
}

// TestVP9CyclicRefreshSegmentIDBoostedMirrorsLibvpx pins the
// cyclic_refresh_segment_id_boosted port at libvpx
// vp9/encoder/vp9_aq_cyclicrefresh.h:127-130.
