//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// TestVP9FullRDInterThisRDFrame1SB0Parity pins the GENUINE per-candidate
// this_rd assembly (vp9FullRDInterThisRD: super_block_yrd + super_block_uvrd +
// mode/MV/filter/ref rate + the rd_pick_inter_mode_sb skip pick) against the
// libvpx ground truth for the documented first inter divergence: seed
// {0,2,0,0,2} (CBR 1200 kbps cpu0 realtime, kf=999, fps 30) frame 1, SB0, the
// 64x64 root NEWMV (ref=LAST, mv=(12,4) 1/8-pel, filt=EIGHTTAP_SMOOTH).
//
// libvpx ground truth (TEMPORARY fprintf in vp9_rdopt.c: one in handle_inter_mode
// after super_block_uvrd for the Y/UV split, one after this_rd is finalized in
// vp9_rd_pick_inter_mode_sb, both gated on cm->current_video_frame==1 &&
// mi_row==0 && mi_col==0 && bsize==BLOCK_64X64; vpxenc-vp9 rebuilt, captured,
// then reverted + rebuilt pristine):
//
//	rdmult=139158 rddiv=7   (matches the super_block_yrd capture)
//	rate_y=5464856 dist_y=5496832            (super_block_yrd, TX_16X16)
//	rate_uv=780630 dist_uv=1533408 sseuv=4672816  (super_block_uvrd, uv_tx=TX_16X16)
//	rate2_pre=2132 (rs=1069, discount=1, ref_cost=461)
//	rate2=6248102 dist2=7030240 total_sse=88782496  skip2=0 (no-skip chosen)
//	this_rd=2598060912
//
// The assembly is fed the live frame-1 encoder context at the exact point the
// 64x64 EIGHTTAP_SMOOTH NEWMV candidate is scored, and must reproduce the full
// per-candidate this_rd plus every Y/UV/rate component byte-exactly.
//
// (The task prompt's original this_rd=2598060912 was correct for the FULL
// per-mode this_rd; the super_block_yrd Y best_rd is the distinct 2188910183.)
//
// NOTE: this pins the assembly via the oracle-trace pin only. The deep-RD
// production wiring (vp9InterUseDeepRDPartition) stays OFF, so production
// byte-parity is untouched.
func TestVP9FullRDInterThisRDFrame1SB0Parity(t *testing.T) {
	const width, height = 64, 64
	opts := VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		MaxKeyframeInterval: 999,
		Deadline:            DeadlineRealtime,
		CpuUsed:             0,
	}
	e, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	var trace bytes.Buffer
	e.SetOracleTraceWriter(&trace)

	sources := vp9test.NewPanningSources(width, height, 256)
	dst := make([]byte, 1<<20)
	for i := 0; i < 2; i++ {
		if _, err := e.EncodeIntoWithResult(sources[i], dst); err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", i, err)
		}
	}

	res, ok := e.vp9CapturedFullRDInterThisRD()
	if !ok {
		t.Fatal("no genuine frame-1 SB0 (0,0) 64x64 NEWMV per-mode this_rd was " +
			"captured; the EIGHTTAP_SMOOTH NEWMV candidate did not score, or " +
			"mv != (12,4)")
	}

	// --- Y component (super_block_yrd, TX_16X16). Mirrors the inter-yrd pin.
	if res.TxSize != common.Tx16x16 {
		t.Errorf("this_rd Y tx_size = %d, want TX_16X16 (%d)", res.TxSize,
			common.Tx16x16)
	}
	if res.RateY != 5464856 {
		t.Errorf("this_rd rate_y = %d, want libvpx 5464856", res.RateY)
	}
	if res.DistY != 5496832 {
		t.Errorf("this_rd dist_y = %d, want libvpx 5496832", res.DistY)
	}

	// --- UV component (super_block_uvrd, uv_tx_size=TX_16X16). The genuine
	// inter UV-RD producer must reproduce these byte-exact.
	if res.UvTxSize != common.Tx16x16 {
		t.Errorf("this_rd uv_tx_size = %d, want TX_16X16 (%d)", res.UvTxSize,
			common.Tx16x16)
	}
	if res.RateUV != 780630 {
		t.Errorf("this_rd rate_uv = %d, want libvpx 780630", res.RateUV)
	}
	if res.DistUV != 1533408 {
		t.Errorf("this_rd dist_uv = %d, want libvpx 1533408", res.DistUV)
	}

	// --- assembled totals (handle_inter_mode + caller).
	const (
		wantRate2    = 6248102
		wantDist2    = 7030240
		wantTotalSSE = 88782496
		wantThisRD   = 2598060912
	)
	if res.SSE != wantTotalSSE {
		t.Errorf("this_rd total_sse = %d, want libvpx %d (psse_y + sse_uv = "+
			"84109680 + 4672816)", res.SSE, wantTotalSSE)
	}
	if res.Skip2 {
		t.Errorf("this_rd skip2 = true, want false (no-skip chosen)")
	}
	if res.Rate != wantRate2 {
		t.Errorf("this_rd rate2 = %d, want libvpx %d", res.Rate, wantRate2)
	}
	if res.Distortion != wantDist2 {
		t.Errorf("this_rd dist2 = %d, want libvpx %d", res.Distortion, wantDist2)
	}
	if res.ThisRD != wantThisRD {
		t.Errorf("this_rd = %d, want libvpx %d", res.ThisRD, wantThisRD)
	}

	// Cross-check the final RDCOST arithmetic against the assembled rate2/dist2
	// at the pinned rdmult — guards against a future RDCOST-helper regression.
	if got := encoder.RDCost(139158, encoder.RDDivBits, res.Rate, res.Distortion); got != wantThisRD {
		t.Errorf("RDCOST(139158, 7, rate2=%d, dist2=%d) = %d, want %d",
			res.Rate, res.Distortion, got, wantThisRD)
	}
}
