//go:build govpx_oracle_trace

package govpx

import (
	"io"
	"testing"

	vp9test "github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// TestVP9FullRDInterSub8x8WrapperFrame1SB0Committed pins the genuine sub-8x8
// joint-RD wrapper (rdPickInterModeSub8x8) + the candidate[2] pred_mv thread +
// the SEARCH->WRITE sub-8x8 replay end-to-end: with the full deep-RD inter
// stack enabled, the (CBR 1200 kbps kf=999 cpu0 realtime) frame-1 SB0 16x16(0,0)
// FOUR 8x8 children commit exactly the partition + per-sub modes/MVs libvpx
// commits — including the bottom-left INTRA sub-8x8 leaf.
//
// libvpx ground truth (vpxenc-vp9 + TEMPORARY fprintf in encode_b /
// vp9_rd_pick_inter_mode_sub8x8 / rd_pick_intra_sub_8x8_y_mode, reverted),
// frame 1, decoded mi grid (miCols=8):
//   - mi(0,0): BLOCK_8X8 NONE, NEWMV mv=(9,15), ref=LAST, EIGHTTAP, tx=TX_8X8.
//   - mi(0,1): BLOCK_4X4 (PARTITION_SPLIT), bmi = [NEARESTMV(9,15),
//     NEARESTMV(9,15), NEWMV(9,4), NEARESTMV(9,4)], ref=LAST, EIGHTTAP.
//   - mi(1,0): BLOCK_8X4 (PARTITION_HORZ) INTRA, bmi modes =
//     [V_PRED, V_PRED, DC_PRED, DC_PRED], mi.mode=DC_PRED, uv_mode=D63_PRED,
//     interp=SWITCHABLE_FILTERS. The committed intra arm:
//     rd_pick_intra_sub_8x8_y_mode rate=109851 (incl. mbmode_cost) /
//     rate_y(tok)=106638 / distortion_y=44385; choose_intra_uv_mode
//     rate_uv_intra=8813 / rate_uv(tok)=5972 / dist_uv=5712; intra_cost_penalty
//     225; ref_costs_single[INTRA]+skip_cost0 = 2496; rate2=121385 dist2=50097
//     this_rd=39404006 (rdmult=139158 rddiv=7).
//   - mi(1,1): BLOCK_4X8 (PARTITION_VERT) INTER NEWMV, ref=LAST,
//     EIGHTTAP_SMOOTH, bmi = [NEARESTMV(9,4), NEWMV(16,-8), NEARESTMV(9,4),
//     NEWMV(16,-8)].
func TestVP9FullRDInterSub8x8WrapperFrame1SB0Committed(t *testing.T) {
	vp9test.RequireVpxenc(t)

	prevP, prevTh, prevS := vp9InterUseDeepRDPartition, vp9InterUseDeepRDThisRDScore, vp9InterUseDeepRDSub8x8
	prevRB := vp9InterUseDeepRDRefBestRD
	vp9InterUseDeepRDPartition = true
	vp9InterUseDeepRDThisRDScore = true
	vp9InterUseDeepRDSub8x8 = true
	vp9InterUseDeepRDRefBestRD = true
	t.Cleanup(func() {
		vp9InterUseDeepRDPartition = prevP
		vp9InterUseDeepRDThisRDScore = prevTh
		vp9InterUseDeepRDSub8x8 = prevS
		vp9InterUseDeepRDRefBestRD = prevRB
	})

	const width, height = 64, 64
	opts := VP9EncoderOptions{
		Width: width, Height: height, FPS: 30,
		RateControlModeSet: true, RateControlMode: RateControlCBR,
		TargetBitrateKbps: 1200, BufferSizeMs: 600, BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500, MinQuantizer: 4, MaxQuantizer: 56,
		MaxKeyframeInterval: 999, Deadline: DeadlineRealtime, CpuUsed: 0,
	}
	e, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	// Enable the oracle-trace capture so recordVP9Sub8x8WrapperCommit stores the
	// live committed segment rate for mi(0,1) (the writer output is unused here).
	e.SetOracleTraceWriter(io.Discard)
	srcs := vp9test.NewPanningSources(width, height, 2)
	var frames [][]byte
	for i, s := range srcs {
		pkt, err := e.Encode(s)
		if err != nil {
			t.Fatalf("Encode frame %d: %v", i, err)
		}
		frames = append(frames, pkt)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(frames[0]); err != nil {
		t.Fatalf("decode keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame after keyframe")
	}
	if err := d.Decode(frames[1]); err != nil {
		t.Fatalf("decode inter frame: %v", err)
	}

	const miCols = 8
	mi00 := d.miGrid[0*miCols+0]
	if mi00.SbType != common.Block8x8 || mi00.Mode != common.NewMv ||
		mi00.RefFrame[0] != int8(vp9dec.LastFrame) ||
		mi00.Mv[0] != (vp9dec.MV{Row: 9, Col: 15}) ||
		mi00.InterpFilter != uint8(vp9dec.InterpEighttap) {
		t.Fatalf("mi(0,0) = {sb=%d mode=%d ref=%d mv=%v interp=%d}, want "+
			"{Block8x8 NEWMV LAST (9,15) EIGHTTAP}", mi00.SbType, mi00.Mode,
			mi00.RefFrame[0], mi00.Mv[0], mi00.InterpFilter)
	}

	mi01 := d.miGrid[0*miCols+1]
	if mi01.SbType != common.Block4x4 ||
		mi01.RefFrame[0] != int8(vp9dec.LastFrame) ||
		mi01.InterpFilter != uint8(vp9dec.InterpEighttap) {
		t.Fatalf("mi(0,1) = {sb=%d ref=%d interp=%d}, want {Block4x4 LAST EIGHTTAP}",
			mi01.SbType, mi01.RefFrame[0], mi01.InterpFilter)
	}
	wantBmi := [4]struct {
		mode common.PredictionMode
		mv   vp9dec.MV
	}{
		{common.NearestMv, vp9dec.MV{Row: 9, Col: 15}},
		{common.NearestMv, vp9dec.MV{Row: 9, Col: 15}},
		{common.NewMv, vp9dec.MV{Row: 9, Col: 4}},
		{common.NearestMv, vp9dec.MV{Row: 9, Col: 4}},
	}
	for i, w := range wantBmi {
		if mi01.Bmi[i].AsMode != w.mode || mi01.Bmi[i].AsMv[0] != w.mv {
			t.Fatalf("mi(0,1).bmi[%d] = {mode=%d mv=%v}, want {mode=%d mv=%v}",
				i, mi01.Bmi[i].AsMode, mi01.Bmi[i].AsMv[0], w.mode, w.mv)
		}
	}

	// Sibling entropy-context propagation pin: the wrapper's LIVE-derived
	// committed segment for mi(0,1) must reproduce libvpx's bsi->r exactly. mi(0,0)
	// (the left-sibling 8x8) is encode_b-stamped before mi(0,1)'s
	// rd_pick_best_sub8x8_mode runs, so its plane entropy context seeds mi(0,1)'s
	// t_left = [1,1] (vp9_rdopt.c:2120-2121 memcpy(t_above, pd->above_context);
	// vp9_encodeframe.c:4163 encode_sb on split children with index != 3).
	//
	// libvpx ground truth (vpxenc-vp9 cpu0 CBR 1200 kbps kf=999 fps 30, the
	// panning source; TEMPORARY fprintf in rd_pick_best_sub8x8_mode +
	// encode_inter_mb_segment, reverted): for the committed EIGHTTAP BLOCK_4X4
	// segment the four labels brate = 3989 + 5226 + 11906 + 33832 = 54953
	// (byrate 3229 + 4466 + 7296 + 33072 = 48063) with the per-label seed/threading
	//   blk0 SEED ta=[0,0] tl=[1,1] -> byrate 3229; blk1 ta=[1,0] tl=[1,1] -> 4466;
	//   blk2 ta=[1,1] tl=[1,1] -> 7296; blk3 ta=[1,1] tl=[1,1] -> 33072.
	// Before the fix the live seed was tl=[0,0] (mi(0,0) not stamped), inflating
	// blk0 byrate 3229->3626 and blk2 7296->7596 (the +697 brate gap 54953->55650).
	const wantSegR = 54953
	gotSegR, gotFltr, ok := e.vp9CapturedSub8x8WrapperCommit()
	if !ok {
		t.Fatal("no sub-8x8 wrapper commit captured for mi(0,1); the live deep-RD " +
			"sub-8x8 leaf did not commit at mi=(0,1)")
	}
	if gotFltr != vp9dec.InterpEighttap {
		t.Errorf("mi(0,1) committed filter = %d, want EIGHTTAP", gotFltr)
	}
	if gotSegR != wantSegR {
		t.Errorf("mi(0,1) committed segment bsi->r = %d, want %d (sibling "+
			"entropy-context propagation: mi(0,0) encode_b stamp must seed "+
			"mi(0,1) t_left=[1,1]; a stale t_left=[0,0] gives 55650, the +697 gap)",
			gotSegR, wantSegR)
	}

	// --- mi(1,0): the committed INTRA sub-8x8 leaf (BLOCK_8X4, PARTITION_HORZ).
	// The decoded mi grid must reproduce libvpx's intra Y modes + UV mode exactly,
	// and the wrapper's live intra capture must reproduce the intra Y/UV rate +
	// this_rd (rd_pick_intra_sub_8x8_y_mode + choose_intra_uv_mode port).
	mi10 := d.miGrid[1*miCols+0]
	if mi10.SbType != common.Block8x4 ||
		mi10.RefFrame[0] != int8(vp9dec.IntraFrame) ||
		mi10.Mode != common.DcPred ||
		mi10.InterpFilter != uint8(vp9dec.SwitchableFilters) {
		t.Fatalf("mi(1,0) = {sb=%d mode=%d ref=%d interp=%d}, want "+
			"{Block8x4 DC_PRED INTRA SWITCHABLE_FILTERS}", mi10.SbType, mi10.Mode,
			mi10.RefFrame[0], mi10.InterpFilter)
	}
	wantMi10Bmi := [4]common.PredictionMode{
		common.VPred, common.VPred, common.DcPred, common.DcPred,
	}
	for i, w := range wantMi10Bmi {
		if mi10.Bmi[i].AsMode != w {
			t.Fatalf("mi(1,0).bmi[%d].mode = %d, want %d (intra sub-block mode)",
				i, mi10.Bmi[i].AsMode, w)
		}
	}

	// Wrapper intra capture pins (rd_pick_intra_sub_8x8_y_mode +
	// choose_intra_uv_mode ground truth, see the function docstring).
	intra, ok := e.vp9CapturedSub8x8IntraCommit()
	if !ok {
		t.Fatal("no sub-8x8 intra commit captured for mi(1,0); the deep-RD wrapper " +
			"did not commit an INTRA sub-8x8 leaf at mi=(1,0)")
	}
	if intra.Bsize != common.Block8x4 {
		t.Errorf("mi(1,0) intra bsize = %d, want Block8x4", intra.Bsize)
	}
	if intra.Mode != common.DcPred {
		t.Errorf("mi(1,0) intra Y mode = %d, want DC_PRED", intra.Mode)
	}
	if intra.Bmi != wantMi10Bmi {
		t.Errorf("mi(1,0) intra bmi modes = %v, want %v", intra.Bmi, wantMi10Bmi)
	}
	if intra.UVMode != common.D63Pred {
		t.Errorf("mi(1,0) intra UV mode = %d, want D63_PRED", intra.UVMode)
	}
	// libvpx rd_pick_intra_sub_8x8_y_mode *rate = 109851 (cost incl. mbmode_cost);
	// choose_intra_uv_mode rate_uv_intra = 8813; rate2 = 121385; dist2 = 50097;
	// this_rd = 39404006 (BLOCK_8X4, rdmult=139158 rddiv=7).
	if intra.YRate != 109851 {
		t.Errorf("mi(1,0) intra Y rate = %d, want 109851 "+
			"(rd_pick_intra_sub_8x8_y_mode *rate)", intra.YRate)
	}
	if intra.UVRate != 8813 {
		t.Errorf("mi(1,0) intra UV rate = %d, want 8813 (rate_uv_intra)",
			intra.UVRate)
	}
	if intra.Rate != 121385 {
		t.Errorf("mi(1,0) intra rate2 = %d, want 121385", intra.Rate)
	}
	if intra.Distortion != 50097 {
		t.Errorf("mi(1,0) intra distortion2 = %d, want 50097", intra.Distortion)
	}
	if intra.ThisRD != 39404006 {
		t.Errorf("mi(1,0) intra this_rd = %d, want 39404006", intra.ThisRD)
	}

	// --- mi(1,1): the committed INTER sub-8x8 leaf (BLOCK_4X8, PARTITION_VERT).
	// The intra evaluation never beats inter here (it returns early per
	// vp9_rdopt.c:1337). mi(1,1) byte-matches only once mi(1,0)'s intra commit +
	// the post-coding entropy-context stamp are correct (else mi(1,1)'s sub-8x8
	// search diverges on the wrong sibling context).
	mi11 := d.miGrid[1*miCols+1]
	if mi11.SbType != common.Block4x8 || mi11.Mode != common.NewMv ||
		mi11.RefFrame[0] != int8(vp9dec.LastFrame) ||
		mi11.InterpFilter != uint8(vp9dec.InterpEighttapSmooth) {
		t.Fatalf("mi(1,1) = {sb=%d mode=%d ref=%d interp=%d}, want "+
			"{Block4x8 NEWMV LAST EIGHTTAP_SMOOTH}", mi11.SbType, mi11.Mode,
			mi11.RefFrame[0], mi11.InterpFilter)
	}
	wantMi11Bmi := [4]struct {
		mode common.PredictionMode
		mv   vp9dec.MV
	}{
		{common.NearestMv, vp9dec.MV{Row: 9, Col: 4}},
		{common.NewMv, vp9dec.MV{Row: 16, Col: -8}},
		{common.NearestMv, vp9dec.MV{Row: 9, Col: 4}},
		{common.NewMv, vp9dec.MV{Row: 16, Col: -8}},
	}
	for i, w := range wantMi11Bmi {
		if mi11.Bmi[i].AsMode != w.mode || mi11.Bmi[i].AsMv[0] != w.mv {
			t.Fatalf("mi(1,1).bmi[%d] = {mode=%d mv=%v}, want {mode=%d mv=%v}",
				i, mi11.Bmi[i].AsMode, mi11.Bmi[i].AsMv[0], w.mode, w.mv)
		}
	}
}
