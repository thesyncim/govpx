//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// TestVP9FullRDInterIntraSBFrame1SB0Parity pins the standalone larger-block
// INTRA RD producer (vp9FullRDInterIntraSB: the intra branch of
// vp9_rd_pick_inter_mode_sb — super_block_yrd on the intra residual +
// choose_intra_uv_mode + the inter-frame mbmode/uv-mode costs + intra_cost_penalty
// + the RDCOST) against the libvpx ground truth for the documented first inter
// frame: seed {0,2,0,0,2} (CBR 1200 kbps cpu0 realtime, kf=999, fps 30) frame 1,
// SB0, the 64x64 root block's INTRA evaluation.
//
// libvpx ground truth (TEMPORARY fprintf in vp9_rdopt.c rd_pick_inter_mode_sb's
// `if (ref_frame == INTRA_FRAME)` arm, right after rate2/distortion2 are formed
// at vp9_rdopt.c:3864-3867, gated on cm->current_video_frame==1 && mi_row==0 &&
// mi_col==0 && bsize==BLOCK_64X64; vpxenc-vp9 rebuilt with --rt --cpu-used=0
// --end-usage=cbr --target-bitrate=1200 --kf-max-dist=999 --timebase=1/30, the
// panning fixture I420, captured, then the binary discarded).
//
// The intra mode loop visits all 10 Y modes (DC..TM). At cpu0 the trailing
// oblique modes hit super_block_yrd's best_rd early-exit (best_rd == the inter
// NEWMV winner's this_rd == 2598060912, matching the inter-this_rd parity pin),
// so only DC/V/H/D45/D207/D63 reach the fprintf in the production run. With the
// early-exit neutralised (debug env) all 10 modes report their full RD, and the
// MINIMUM-RD intra mode — the committed intra candidate — is:
//
//	ymode=D207_PRED(7) uvmode=D207_PRED(7) txsize=TX_16X16(2) uvtx=TX_16X16(2)
//	rate_y=6151645  dist_y=5590128         (super_block_yrd, TX_16X16)
//	rate_uv=1175403 dist_uv=1860784        (choose_intra_uv_mode tokenonly, uv_tx=TX_16X16)
//	mbmode_cost=2117  rate_uv_intra=1177650 (-> uv_mode_cost = 2247)
//	intra_cost_penalty=3600 (= 20*vp9_dc_quant(145,0,8)=20*180, oblique mode)
//	rate2=7335012   dist2=7450912   rd=2947321423   skippable=0
//	rdmult=139158 rddiv=7  use_fast_coef_costing=1  tx_size_search_method=USE_FULL_RD
//
// The producer is fed the frame-1 SB0 (0,0) mode-decision context: the source
// (panning frame 1), the qindex-145 dequant (e.dqScratch, Y=[180,235]),
// x->rdmult=139158, use_fast_coef_costing=1, and — crucially — the frame-1 entropy
// context, which at the FIRST inter frame is the DEFAULT (pre-adaptation)
// FrameContext (the keyframe adapts kf-specific tables; the inter frame starts
// from vp9_default_coef_probs and only adapts AFTER frame 1 completes). The intra
// prediction at the top-left SB uses the default 127/129 edges (no reconstructed
// neighbours), so the recon-plane content is irrelevant; only source + dequant +
// rdmult + the default frame context + zeroed SB-corner entropy contexts feed the
// transform-RD. The reconstruction below restores exactly that state after a
// 2-frame encode (which allocates the planes and fills e.dqScratch), then drives
// the producer with refBestRD == INT64_MAX (no early-exit) so all 10 Y modes are
// scored and the true min-RD intra mode is selected — the same winner libvpx
// commits.
//
// This pins ONLY the standalone producer (it is not wired into the production
// mode loop; the parent wires it later), so production byte-parity is untouched.
func TestVP9FullRDInterIntraSBFrame1SB0Parity(t *testing.T) {
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
	// Encode frame 0 (keyframe) then frame 1 (first inter frame) to allocate the
	// planes and populate e.dqScratch with the frame-1 qindex-145 dequant.
	for i := 0; i < 2; i++ {
		if _, err := e.EncodeIntoWithResult(sources[i], dst); err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", i, err)
		}
	}

	// Reconstruct the frame-1 SB0 (0,0) intra mode-decision context (see the
	// doc comment): default frame context (the live frame-1 fc), x->rdmult, and
	// zeroed SB-corner entropy contexts.
	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	e.fc = fc
	e.cbRdmult = 139158 // x->rdmult at qindex 145 (matches the Y/UV/this_rd pins)
	e.resetVP9EncoderAboveEntropyContexts()
	for p := range e.planes {
		for i := range e.planes[p].LeftContext {
			e.planes[p].LeftContext[i] = 0
		}
	}

	tile := vp9dec.TileBounds{MiRowStart: 0, MiRowEnd: 8, MiColStart: 0, MiColEnd: 8}
	inter := &vp9InterEncodeState{
		img:        sources[1],
		dq:         &e.dqScratch,
		selectFc:   fc,
		baseQindex: 145,
		txMode:     common.TxModeSelect,
	}

	res, ok := e.vp9FullRDInterIntraSB(inter, tile, 8, 8, 0, 0,
		common.Block64x64, e.cbRdmult, ^uint64(0))
	if !ok {
		t.Fatal("vp9FullRDInterIntraSB returned !ok for frame-1 SB0 64x64 intra")
	}

	// Committed intra mode (the min-RD candidate over DC..TM).
	if res.YMode != common.D207Pred {
		t.Errorf("intra y_mode = %d, want D207_PRED (%d)", res.YMode, common.D207Pred)
	}
	if res.UvMode != common.D207Pred {
		t.Errorf("intra uv_mode = %d, want D207_PRED (%d)", res.UvMode, common.D207Pred)
	}
	if res.TxSize != common.Tx16x16 {
		t.Errorf("intra tx_size = %d, want TX_16X16 (%d)", res.TxSize, common.Tx16x16)
	}
	if res.UvTxSize != common.Tx16x16 {
		t.Errorf("intra uv_tx_size = %d, want TX_16X16 (%d)", res.UvTxSize, common.Tx16x16)
	}

	// Y-plane super_block_yrd outputs (rate_y == r[best_tx][TX_MODE_SELECT]).
	if res.RateY != 6151645 {
		t.Errorf("intra rate_y = %d, want libvpx 6151645", res.RateY)
	}
	if res.DistY != 5590128 {
		t.Errorf("intra dist_y = %d, want libvpx 5590128", res.DistY)
	}

	// Chroma choose_intra_uv_mode tokenonly outputs (rate_uv / dist_uv).
	if res.RateUV != 1175403 {
		t.Errorf("intra rate_uv = %d, want libvpx 1175403", res.RateUV)
	}
	if res.DistUV != 1860784 {
		t.Errorf("intra dist_uv = %d, want libvpx 1860784", res.DistUV)
	}

	// Mode signalling: mbmode_cost(2117) + uv_mode_cost(2247) = 4364.
	if res.ModeCost != 4364 {
		t.Errorf("intra mode_cost = %d, want 4364 (mbmode 2117 + uv_mode 2247)",
			res.ModeCost)
	}
	// intra_cost_penalty for the oblique D207 mode (20*vp9_dc_quant(145,0,8)).
	if res.IntraCostPenalty != 3600 {
		t.Errorf("intra_cost_penalty = %d, want 3600", res.IntraCostPenalty)
	}

	// rate2 = rate_y + mbmode_cost + rate_uv_intra + intra_cost_penalty
	//       = 6151645 + 2117 + 1177650 + 3600 = 7335012 (vp9_rdopt.c:3864-3866).
	if res.Rate2 != 7335012 {
		t.Errorf("intra rate2 = %d, want libvpx 7335012", res.Rate2)
	}
	// distortion2 = dist_y + dist_uv = 5590128 + 1860784 = 7450912.
	if res.Distortion2 != 7450912 {
		t.Errorf("intra distortion2 = %d, want libvpx 7450912", res.Distortion2)
	}
	// rd = RDCOST(rdmult=139158, rddiv=7, rate2, distortion2).
	const wantRD = 2947321423
	if res.RD != wantRD {
		t.Errorf("intra rd = %d, want libvpx %d", res.RD, wantRD)
	}
	if res.Skippable {
		t.Errorf("intra skippable = true, want false")
	}
}
