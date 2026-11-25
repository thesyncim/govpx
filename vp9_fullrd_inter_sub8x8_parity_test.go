//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"testing"
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

// TestVP9FullRDSub8x8Frame1Parity pins the GENUINE sub-8x8 joint RD producer
// (rdPickBestSub8x8Mode + encodeInterMbSegment + setAndCostBmiMvs + the
// per-sub-block NEWMV motion search) against the libvpx ground truth for the
// documented frame-1 SB0 16x16(0,0) child at mi=(0,1) BLOCK_4X4 ref=LAST
// EIGHTTAP (rdmult=139158 rddiv=7).
//
// libvpx ground truth (vpxenc-vp9 cpu0 CBR 1200 kbps, kf=999, fps 30, the
// panning source; TEMPORARY fprintf in rd_pick_best_sub8x8_mode +
// set_and_cost_bmi_mvs, reverted). Per-label decomposition (LABEL probe):
//
//	block 0: NEARESTMV mv=(9,15) brate=3989 byrate=3229 bdist=15809 bsse=22289 brdcost=3107734 eob=1
//	block 1: NEARESTMV mv=(9,15) brate=5226 byrate=4466 bdist=3769  bsse=29329 brdcost=1902822 eob=1
//	block 2: NEWMV     mv=(9,4)  brate=11906 byrate=7296 bdist=16990 bsse=24526 brdcost=5410688 eob=9
//	block 3: NEARESTMV mv=(9,4)  brate=33832 byrate=33072 bdist=23453 bsse=734187 brdcost=12197284 eob=16
//
// accumulating bsi->r=54953 bsi->d=60021 bsi->sse=810331 segment_rd=22618528.
//
// The NEWMV motion search (NEWMV probe) lands new_mv=(9,4) for block 2 from
// mvp_full=(1,1), ref_mv=(9,15).
func TestVP9FullRDSub8x8Frame1Parity(t *testing.T) {
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

	cap, ok := e.vp9CapturedFullRDSub8x8()
	if !ok {
		t.Fatal("no sub-8x8 producer capture; the frame-1 SB0 64x64 NEWMV " +
			"EIGHTTAP_SMOOTH candidate did not score")
	}

	type want struct {
		mode       common.PredictionMode
		mv         vp9dec.MV
		modeMvRate int
		byrate     int
		bdist      uint64
		bsse       uint64
		brdcost    uint64
		eob        int
	}
	wants := [4]want{
		{common.NearestMv, vp9dec.MV{Row: 9, Col: 15}, 3989 - 3229, 3229, 15809, 22289, 3107734, 1},
		{common.NearestMv, vp9dec.MV{Row: 9, Col: 15}, 5226 - 4466, 4466, 3769, 29329, 1902822, 1},
		{common.NewMv, vp9dec.MV{Row: 9, Col: 4}, 11906 - 7296, 7296, 16990, 24526, 5410688, 9},
		{common.NearestMv, vp9dec.MV{Row: 9, Col: 4}, 33832 - 33072, 33072, 23453, 734187, 12197284, 16},
	}

	for b := 0; b < 4; b++ {
		l := cap.Labels[b]
		w := wants[b]
		if !l.Valid {
			t.Errorf("block %d: label not produced", b)
			continue
		}
		if l.ModeMvRate != w.modeMvRate {
			t.Errorf("block %d mode/MV rate = %d, want %d (set_and_cost_bmi_mvs)",
				b, l.ModeMvRate, w.modeMvRate)
		}
		if l.Byrate != w.byrate {
			t.Errorf("block %d byrate = %d, want %d (cost_coeffs)", b, l.Byrate,
				w.byrate)
		}
		if l.Bdist != w.bdist {
			t.Errorf("block %d bdist = %d, want %d", b, l.Bdist, w.bdist)
		}
		if l.Bsse != w.bsse {
			t.Errorf("block %d bsse = %d, want %d", b, l.Bsse, w.bsse)
		}
		if l.Eob != w.eob {
			t.Errorf("block %d eob = %d, want %d", b, l.Eob, w.eob)
		}
		if l.Brdcost != w.brdcost {
			t.Errorf("block %d brdcost = %d, want %d", b, l.Brdcost, w.brdcost)
		}
	}

	// The block-2 NEWMV motion search must reproduce libvpx new_mv=(9,4).
	if !cap.SearchBlock2Valid {
		t.Error("block-2 NEWMV motion search did not run")
	} else if cap.SearchBlock2Mv != (vp9dec.MV{Row: 9, Col: 4}) {
		t.Errorf("block-2 NEWMV search MV = %+v, want (9,4)", cap.SearchBlock2Mv)
	}

	// The full genuine-derivation segment (rdPickBestSub8x8Mode, fed the libvpx
	// candidate context): per-label mode SELECTION + accumulation must reproduce
	// the libvpx BSI totals bsi->r=54953 d=60021 sse=810331 segment_rd=22618528
	// and pick NEARESTMV/NEARESTMV/NEWMV/NEARESTMV with (9,15)/(9,15)/(9,4)/(9,4).
	if !cap.SegValid {
		t.Fatal("genuine-derivation segment did not produce a result")
	}
	seg := cap.Seg
	const (
		wantR   = 54953
		wantD   = 60021
		wantSSE = 810331
		wantSeg = 22618528
	)
	if seg.R != wantR {
		t.Errorf("segment bsi->r = %d, want %d", seg.R, wantR)
	}
	if seg.D != wantD {
		t.Errorf("segment bsi->d = %d, want %d", seg.D, wantD)
	}
	if seg.SSE != wantSSE {
		t.Errorf("segment bsi->sse = %d, want %d", seg.SSE, wantSSE)
	}
	if seg.SegmentRD != wantSeg {
		t.Errorf("segment_rd = %d, want %d", seg.SegmentRD, wantSeg)
	}
	wantModes := [4]common.PredictionMode{
		common.NearestMv, common.NearestMv, common.NewMv, common.NearestMv,
	}
	wantMvs := [4]vp9dec.MV{
		{Row: 9, Col: 15}, {Row: 9, Col: 15}, {Row: 9, Col: 4}, {Row: 9, Col: 4},
	}
	for b := 0; b < 4; b++ {
		if seg.Labels[b].Mode != wantModes[b] {
			t.Errorf("segment label %d mode = %d, want %d", b,
				seg.Labels[b].Mode, wantModes[b])
		}
		if seg.Labels[b].Mv != wantMvs[b] {
			t.Errorf("segment label %d mv = %+v, want %+v", b,
				seg.Labels[b].Mv, wantMvs[b])
		}
	}
	if seg.Skippable {
		t.Error("segment skippable = true, want false (eobs nonzero)")
	}

	// VERT(4x8) partition-shape coverage: the (1,1) child BLOCK_4X8 segment.
	// libvpx BSI: r=73683 d=54955 sse=1751063 segment_rd=27060761, with
	// block 0 NEARESTMV(9,4) and block 1 NEWMV(16,-8) (the NEWMV search lands
	// (16,-8) from mvp=(9,4)). Exercises the multi-4x4-unit-per-label path
	// (each 4x8 label = two stacked 4x4 transform units).
	if !cap.VertValid {
		t.Fatal("VERT(4x8) segment did not produce a result")
	}
	v := cap.Vert
	if v.R != 73683 {
		t.Errorf("VERT bsi->r = %d, want 73683", v.R)
	}
	if v.D != 54955 {
		t.Errorf("VERT bsi->d = %d, want 54955", v.D)
	}
	if v.SSE != 1751063 {
		t.Errorf("VERT bsi->sse = %d, want 1751063", v.SSE)
	}
	if v.SegmentRD != 27060761 {
		t.Errorf("VERT segment_rd = %d, want 27060761", v.SegmentRD)
	}
	if v.Labels[0].Mode != common.NearestMv ||
		v.Labels[0].Mv != (vp9dec.MV{Row: 9, Col: 4}) {
		t.Errorf("VERT label 0 = %d/%+v, want NearestMv/(9,4)",
			v.Labels[0].Mode, v.Labels[0].Mv)
	}
	if v.Labels[1].Mode != common.NewMv ||
		v.Labels[1].Mv != (vp9dec.MV{Row: 16, Col: -8}) {
		t.Errorf("VERT label 1 = %d/%+v, want NewMv/(16,-8)",
			v.Labels[1].Mode, v.Labels[1].Mv)
	}
}
