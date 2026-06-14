//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// TestVP9FullRDInterSuperBlockYRDFrame1SB0Parity pins the genuine inter
// super_block_yrd producer (vp9FullRDInterSuperBlockYRD: txfm_rd_in_plane +
// choose_tx_size_from_rd for the Y plane) against the libvpx ground truth for
// the documented first inter divergence: seed {0,2,0,0,2} (CBR 1200 kbps cpu0
// realtime, kf=999, fps 30) frame 1, SB0, the 64x64 root NEWMV (ref=LAST,
// mv=(12,4) 1/8-pel, filt=EIGHTTAP_SMOOTH).
//
// libvpx ground truth (TEMPORARY fprintf in vp9_rdopt.c choose_tx_size_from_rd
// gated on cm->current_video_frame==1 && bs==BLOCK_64X64 && NEWMV && ref=LAST
// && mv==(12,4) && EIGHTTAP_SMOOTH; vpxenc-vp9 rebuilt, captured, then reverted
// + rebuilt pristine):
//
//	base_qindex=145 y_dq=[180,235] skip_prob=248 tx_ctx=1
//	rdmult=139158 rddiv=7 s0=23 s1=2560
//	r_tx_size[TX_16X16]=2384  r_tx_size[TX_32X32]=221
//	start_tx=TX_32X32 end_tx=TX_16X16 (TX_MODE_SELECT, depth 2, bs>32x32)
//
//	TX_16X16: r0=5462472 r1=5464856 d=5496832 s=0 sse=84109680 -> rd1=2188910183
//	TX_32X32: r0=6466064 r1=6466285 d=5642240 s=0 sse=84109680 -> rd1=2479703768
//	best_tx=TX_16X16  best_rd=2188910183
//
// (n=0/n=1 are never evaluated — start_tx=TX_32X32, end_tx=TX_16X16 — so their
// stack slots hold garbage in C; FullRDChooseTxSize never reads them.)
//
// The producer is fed the live frame-1 encoder context (qindex=145 dequant,
// the frame's entropy/coef probs, e.cb_rdmult==139158) at the exact point the
// 64x64 EIGHTTAP_SMOOTH NEWMV candidate is scored, and must reproduce
// tx_size=TX_16X16, best_rd=2188910183, plus the TX_16X16 per-tx-size tuple.
//
// (The task prompt cited best_rd=2598060912; the authoritative libvpx capture
// for this exact block/config is best_rd=2188910183, tx=TX_16X16 — the prompt
// value was stale. The capture above is the ground truth.)
//
// The full per-tx-size table is now pinned byte-exact, INCLUDING the TX_32X32
// loser. libvpx runs vp9_optimize_b (trellis) on the 32x32 transform blocks
// (sf.trellis_opt_tx_rd.method==ENABLE_TRELLIS_OPT for the RT mode-selection
// path); the producer wires the verbatim port (encoder.VP9OptimizeB) into the
// txfm_rd_in_plane path, so its post-trellis 32x32 dqcoeff/eob reproduce
// libvpx's r0=6466064 d=5642240 exactly (before the trellis wire-in the
// producer yielded r0=6541317 d=5530544 — pure trellis divergence). Trellis
// does NOT flip the winner here, so tx_size=TX_16X16 + best_rd are unchanged.
//
// NOTE: this pins ONLY the standalone producer. The test disables the production
// cpu0 SearchPartition deep recursion so the historical 64x64 candidate capture
// is still reached; production byte parity is pinned separately.
func TestVP9FullRDInterSuperBlockYRDFrame1SB0Parity(t *testing.T) {
	withoutVP9ProductionDeepRDSearchPartition(t)
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

	res, ok := e.vp9FullRDInterYRD()
	if !ok {
		t.Fatal("no full-RD frame-1 SB0 (0,0) 64x64 NEWMV super_block_yrd " +
			"producer result was captured; the EIGHTTAP_SMOOTH NEWMV " +
			"candidate did not score, or mv != (12,4)")
	}

	// Primary pins: selected tx_size and best_rd (the choose_tx_size_from_rd
	// best_rd super_block_yrd's caller folds into rdcosty).
	if res.TxSize != common.Tx16x16 {
		t.Errorf("inter super_block_yrd tx_size = %d, want TX_16X16 (%d)",
			res.TxSize, common.Tx16x16)
	}
	const wantBestRD = 2188910183
	if res.BestRD != wantBestRD {
		t.Errorf("inter super_block_yrd best_rd = %d, want libvpx %d",
			res.BestRD, wantBestRD)
	}

	// Reported tuple for the selected tx_size (vp9_rdopt.c:1006-1009).
	// tx_mode==TX_MODE_SELECT so *rate = r[best_tx][1] = 5464856.
	const (
		wantRate = 5464856 // r[TX_16X16][1] = r0 + r_tx_size(2384)
		wantDist = 5496832 // d[TX_16X16]
		wantSSE  = 84109680
	)
	if res.Rate != wantRate {
		t.Errorf("inter super_block_yrd rate = %d, want %d", res.Rate, wantRate)
	}
	if res.Distortion != wantDist {
		t.Errorf("inter super_block_yrd distortion = %d, want %d",
			res.Distortion, wantDist)
	}
	if res.SSE != wantSSE {
		t.Errorf("inter super_block_yrd sse = %d, want %d", res.SSE, wantSSE)
	}
	if res.Skippable {
		t.Errorf("inter super_block_yrd skippable = true, want false")
	}

	// start_tx=TX_32X32, end_tx=TX_16X16 (TX_MODE_SELECT depth 2, bs>32x32).
	if res.Start != int(common.Tx32x32) || res.End != int(common.Tx16x16) {
		t.Errorf("inter super_block_yrd start/end tx = %d/%d, want %d/%d",
			res.Start, res.End, common.Tx32x32, common.Tx16x16)
	}

	// TX_16X16 (the SELECTED tx) per-tx-size txfm_rd_in_plane tuple — pinned
	// byte-exact to libvpx (vp9_rdopt.c r[n][0]/d[n]/s[n]/sse[n]). This block's
	// 16x16 transform blocks are unaffected by trellis here, so the no-trellis
	// producer matches libvpx's post-trellis values exactly.
	c16 := res.Cand[common.Tx16x16]
	if !c16.Valid {
		t.Fatal("TX_16X16 candidate invalid")
	}
	if c16.Rate != 5462472 {
		t.Errorf("TX_16X16 r0 = %d, want 5462472", c16.Rate)
	}
	if c16.Dist != 5496832 {
		t.Errorf("TX_16X16 d = %d, want 5496832", c16.Dist)
	}
	if c16.SSE != 84109680 {
		t.Errorf("TX_16X16 sse = %d, want 84109680", c16.SSE)
	}
	if c16.Skip {
		t.Errorf("TX_16X16 skip = true, want false")
	}

	// TX_32X32 (a LOSER) per-tx-size txfm_rd_in_plane tuple — now pinned
	// byte-exact to libvpx. The producer runs the verbatim vp9_optimize_b
	// trellis (encoder.VP9OptimizeB) on each 32x32 transform block, exactly as
	// block_rd_txfm does (vp9_rdopt.c:793, do_trellis_opt → ENABLE_TRELLIS_OPT
	// for the RT full-RD mode-selection path), so the optimised dqcoeff/eob
	// reproduce libvpx's post-trellis r0=6466064 d=5642240. (Before the trellis
	// port the producer yielded r0=6541317 d=5530544; the divergence was pure
	// trellis — identical fdct coeff/qcoeff, only post-quant dqcoeff differed.)
	c32 := res.Cand[common.Tx32x32]
	if !c32.Valid {
		t.Fatal("TX_32X32 candidate invalid")
	}
	if c32.Rate != 6466064 {
		t.Errorf("TX_32X32 r0 = %d, want libvpx (trellis'd) 6466064", c32.Rate)
	}
	if c32.Dist != 5642240 {
		t.Errorf("TX_32X32 d = %d, want libvpx (trellis'd) 5642240", c32.Dist)
	}
	if c32.SSE != 84109680 {
		t.Errorf("TX_32X32 sse = %d, want 84109680 (tx-independent residual "+
			"energy must match the predictor)", c32.SSE)
	}
	if c32.Skip {
		t.Errorf("TX_32X32 skip = true, want false")
	}
	// r1 = r0 + r_tx_size[TX_32X32] (221) must match libvpx's 6466285.
	if got := c32.Rate + 221; got != 6466285 {
		t.Errorf("TX_32X32 r1 = %d, want libvpx 6466285", got)
	}
	// The 32x32 candidate must still lose to 16x16 (decision robustness): its
	// RDCOST rd1 must exceed the selected best_rd. With the trellis'd values
	// this is the exact libvpx rd1 = 2479703768.
	rd32 := encoder.RDCost(139158, encoder.RDDivBits,
		c32.Rate+221 /*r_tx_size[TX_32X32]*/ +23 /*s0*/, c32.Dist)
	if rd32 != 2479703768 {
		t.Errorf("TX_32X32 rd1 = %d, want libvpx 2479703768", rd32)
	}
	if rd32 <= res.BestRD {
		t.Errorf("TX_32X32 rd1 = %d must exceed selected best_rd = %d "+
			"(winner robustness)", rd32, res.BestRD)
	}
}
