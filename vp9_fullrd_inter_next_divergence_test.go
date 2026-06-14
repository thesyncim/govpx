//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"fmt"
	"image"
	"io"
	"strings"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vpx/buffers"
)

// vp9_fullrd_inter_next_divergence_test.go is an oracle-trace ANALYSIS probe (no
// production .go touched). With the full deep-RD inter stack enabled
// (vp9InterUseDeepRDPartition + vp9InterUseDeepRDThisRDScore +
// vp9InterUseDeepRDSub8x8 — the exact set the sub-8x8 wrapper test flips), it
// drives the {0,2,0,0,2} long-fixture seed (CBR 1200 kbps kf=999 realtime cpu0)
// through govpx's deep engine, decodes the emitted inter frame, and walks the
// frame-1 SB0 8x8/16x16 leaves in libvpx ENCODE ORDER (the rd_pick_partition
// z-order: 64 -> 4x32 -> 4x16 -> 4x8, vp9/encoder/vp9_encodeframe.c:2253
// encode_sb partition walk -> :2226 encode_b commit). For each leaf it compares
// govpx's COMMITTED mi (decoded from the bitstream govpx actually wrote — the
// ground truth of what the deep engine selected) against the libvpx-committed
// decomposition (docs/vp9_fullrd_frame1_decomposition.md, embedded + md5-anchored
// in vp9_oracle_fullrd_frame1_decomposition_test.go: the two-frame IVF the
// capture probe produced is byte-identical to the pinned vpxenc-vp9 oracle).
//
// The 16x16(0,0) quad (mi(0,0),(0,1),(1,0),(1,1)) is already byte-exact (pinned
// by TestVP9FullRDInterSub8x8WrapperFrame1SB0Committed). This probe's job is to
// localise the FIRST committed-block divergence AFTER 16x16(0,0) in encode order
// and classify the distinct libvpx code path that leaf needs, so the parent can
// aim the next integration at exactly the right path.
//
// It is a REPORT, not a hard gate past 16x16(0,0): the four 16x16(0,0) children
// are asserted equal (regression guard for the closed milestone); everything
// after is logged with the precise govpx-vs-libvpx field delta and the first
// divergence is surfaced via t.Logf (not t.Fatal) so the frontier is captured
// even while the gap is open. The 16x16(0,0)-quad assertion is the only hard
// failure.

// nextDivBmi is one libvpx-committed sub-block (b_mode_info) — the per-4x4 intra
// mode (as_mode) and the ref0 motion vector (as_mv[0]). Modes use PREDICTION_MODE
// (10 NEARESTMV 11 NEARMV 12 ZEROMV 13 NEWMV; <10 intra). For an inter sub-block
// only the mode + mv are meaningful; for an intra sub-block only the mode.
type nextDivBmi struct {
	mode common.PredictionMode
	mv   vp9dec.MV
}

// nextDivBlock is one libvpx-committed leaf of frame-1 SB0, carrying the full
// decision the deep engine must reproduce. Fields mirror MODE_INFO; bsize/mode/
// ref/interp use the same integer encodings as fullRDFrame1Block. bmiSet marks
// whether the per-sub-block quartet is pinned (sub-8x8 leaves) vs a whole-block
// 8x8 NONE leaf (block-level mv only). For an 8x8 NONE inter leaf, mv pins
// mi.mv[0]; bmi is ignored. For a sub-8x8 leaf, the four bmi entries pin the
// committed per-sub decision and the block-level mv is the last sub-block's mv
// (libvpx mi->mv[0] = bmi[3].as_mv[0]).
//
// Ground truth: docs/vp9_fullrd_frame1_decomposition.md "Seed {0,2,0,0,2}" map
// (block-level fields) + its per-block bmi annotations (the [bmi: ...] / [bmi y:
// ...] columns and the mv(r,c) whole-8x8 annotations). Encode order is the table
// order verbatim (z-order). intra==true marks the lone sub-8x8 INTRA leaf mi(1,0).
type nextDivBlock struct {
	miRow, miCol int
	bsize        int // 0=4x4 1=4x8 2=8x4 3=8x8
	mode         int // top-level mode (intra: y mode; inter: last-sub mode)
	ref0, ref1   int
	interp       int // 0 EIGHTTAP 1 SMOOTH 2 SHARP 3 SWITCHABLE(intra default)
	intra        bool
	mv           vp9dec.MV     // whole-8x8 NONE leaf: mi.mv[0]
	bmiSet       bool          // true => bmi[] pinned (sub-8x8 leaf)
	bmi          [4]nextDivBmi // sub-8x8 per-sub committed decision
}

// vp9FullRDSeed0_2_0_0_2Frame1SB0EncodeOrder is the libvpx-committed frame-1 SB0
// decomposition in ENCODE ORDER with per-sub bmi detail, for the {0,2,0,0,2}
// seed. Transcribed verbatim from docs/vp9_fullrd_frame1_decomposition.md (the
// md5-anchored capture). The first four entries are the 16x16(0,0) quad (already
// byte-exact); the cross-check against the block-level fullRDFrame1Block anchor
// table (same data, raster order) runs in the test body so a transcription slip
// fails loudly. mv/bmi are in the decoder's MV units (row,col), matching the
// markdown's mv(row,col) and [bmi ...] annotations.
//
// Legend reminder for bmi modes: NEAREST=10, NEAR=11, NEW=13; intra V_PRED=1,
// DC_PRED=0, D63=8.
var vp9FullRDSeed0_2_0_0_2Frame1SB0EncodeOrder = []nextDivBlock{
	// ---- 16x16(0,0): mi(0,0),(0,1),(1,0),(1,1) — already byte-exact ----
	{miRow: 0, miCol: 0, bsize: 3, mode: 13, ref0: 1, ref1: -1, interp: 0,
		mv: vp9dec.MV{Row: 9, Col: 15}},
	{miRow: 0, miCol: 1, bsize: 0, mode: 10, ref0: 1, ref1: -1, interp: 0,
		bmiSet: true, bmi: [4]nextDivBmi{
			{mode: 10, mv: vp9dec.MV{Row: 9, Col: 15}},
			{mode: 10, mv: vp9dec.MV{Row: 9, Col: 15}},
			{mode: 13, mv: vp9dec.MV{Row: 9, Col: 4}},
			{mode: 10, mv: vp9dec.MV{Row: 9, Col: 4}},
		}},
	{miRow: 1, miCol: 0, bsize: 2, mode: 0, ref0: 0, ref1: -1, interp: 3,
		intra: true, bmiSet: true, bmi: [4]nextDivBmi{
			{mode: 1}, {mode: 1}, {mode: 0}, {mode: 0}, // V,V,DC,DC
		}},
	{miRow: 1, miCol: 1, bsize: 1, mode: 13, ref0: 1, ref1: -1, interp: 1,
		bmiSet: true, bmi: [4]nextDivBmi{
			{mode: 10, mv: vp9dec.MV{Row: 9, Col: 4}},
			{mode: 13, mv: vp9dec.MV{Row: 16, Col: -8}},
			{mode: 10, mv: vp9dec.MV{Row: 9, Col: 4}},
			{mode: 13, mv: vp9dec.MV{Row: 16, Col: -8}},
		}},

	// ---- 16x16(0,1): mi(0,2),(0,3),(1,2),(1,3) — FIRST post-16x16(0,0) quad ----
	// Per-sub MVs added from the libvpx ground-truth (GOVPX_BMI_TRACE encode_b
	// dump of the md5 c41fc299 oracle stream): the decoder-replicated bmi[0..3]
	// as_mv array. For 4x8 (bsize 1) bmi[0]/bmi[1] are the two distinct columns;
	// for 8x4 (bsize 2) bmi[0]==bmi[1] (the first row, replicated). These MVs are
	// now byte-exact in govpx (the sub-8x8 NEWMV write-reference fix), so the gate
	// pins them.
	{miRow: 0, miCol: 2, bsize: 0, mode: 11, ref0: 1, ref1: -1, interp: 0,
		bmiSet: true, bmi: [4]nextDivBmi{
			{mode: 13, mv: vp9dec.MV{Row: 24, Col: -8}}, // NEW
			{mode: 13, mv: vp9dec.MV{Row: 16, Col: 8}},  // NEW
			{mode: 10, mv: vp9dec.MV{Row: 24, Col: -8}}, // NEAREST
			{mode: 11, mv: vp9dec.MV{Row: 16, Col: 8}},  // NEAR
		}},
	{miRow: 0, miCol: 3, bsize: 2, mode: 13, ref0: 1, ref1: -1, interp: 0,
		bmiSet: true, bmi: [4]nextDivBmi{ // NEW,NEW
			{mode: 13, mv: vp9dec.MV{Row: -8, Col: 40}},
			{mode: 13, mv: vp9dec.MV{Row: -8, Col: 40}},
		}},
	{miRow: 1, miCol: 2, bsize: 1, mode: 13, ref0: 1, ref1: -1, interp: 0,
		bmiSet: true, bmi: [4]nextDivBmi{ // NEW,NEW
			{mode: 13, mv: vp9dec.MV{Row: 16, Col: 10}},
			{mode: 13, mv: vp9dec.MV{Row: 1, Col: 9}},
		}},
	{miRow: 1, miCol: 3, bsize: 3, mode: 13, ref0: 1, ref1: -1, interp: 0,
		mv: vp9dec.MV{Row: 0, Col: 8}},

	// ---- 16x16(1,0): mi(2,0),(2,1),(3,0),(3,1) ----
	{miRow: 2, miCol: 0, bsize: 1, mode: 13, ref0: 1, ref1: -1, interp: 0,
		bmiSet: true, bmi: [4]nextDivBmi{ // NEAREST,NEW
			{mode: 10, mv: vp9dec.MV{Row: 9, Col: 15}},
			{mode: 13, mv: vp9dec.MV{Row: 19, Col: 0}},
		}},
	{miRow: 2, miCol: 1, bsize: 1, mode: 13, ref0: 1, ref1: -1, interp: 0,
		bmiSet: true, bmi: [4]nextDivBmi{ // NEW,NEW
			{mode: 13, mv: vp9dec.MV{Row: 12, Col: -1}},
			{mode: 13, mv: vp9dec.MV{Row: 8, Col: 0}},
		}},
	{miRow: 3, miCol: 0, bsize: 2, mode: 13, ref0: 1, ref1: -1, interp: 0,
		bmiSet: true, bmi: [4]nextDivBmi{ // NEW,NEW
			{mode: 13, mv: vp9dec.MV{Row: 1, Col: 16}},
			{mode: 13, mv: vp9dec.MV{Row: 1, Col: 16}},
		}},
	{miRow: 3, miCol: 1, bsize: 0, mode: 11, ref0: 1, ref1: -1, interp: 0,
		bmiSet: true, bmi: [4]nextDivBmi{ // NEW,NEAREST,NEW,NEAR
			{mode: 13, mv: vp9dec.MV{Row: 8, Col: 0}},
			{mode: 10, mv: vp9dec.MV{Row: 8, Col: 0}},
			{mode: 13, mv: vp9dec.MV{Row: 16, Col: 8}},
			{mode: 11, mv: vp9dec.MV{Row: 8, Col: 0}},
		}},

	// ---- 16x16(1,1): mi(2,2),(2,3),(3,2),(3,3) ----
	{miRow: 2, miCol: 2, bsize: 1, mode: 13, ref0: 1, ref1: -1, interp: 0,
		bmiSet: true, bmi: [4]nextDivBmi{ // NEAR,NEW
			{mode: 11, mv: vp9dec.MV{Row: 8, Col: 0}},
			{mode: 13, mv: vp9dec.MV{Row: 17, Col: -5}},
		}},
	{miRow: 2, miCol: 3, bsize: 0, mode: 13, ref0: 1, ref1: -1, interp: 0,
		bmiSet: true, bmi: [4]nextDivBmi{ // NEW,NEW,NEW,NEW
			{mode: 13, mv: vp9dec.MV{Row: 14, Col: 1}},
			{mode: 13, mv: vp9dec.MV{Row: 24, Col: -16}},
			{mode: 13, mv: vp9dec.MV{Row: 3, Col: 32}},
			{mode: 13, mv: vp9dec.MV{Row: -15, Col: 31}},
		}},
	{miRow: 3, miCol: 2, bsize: 1, mode: 13, ref0: 1, ref1: -1, interp: 0,
		bmiSet: true, bmi: [4]nextDivBmi{ // NEW,NEW
			{mode: 13, mv: vp9dec.MV{Row: 9, Col: -2}},
			{mode: 13, mv: vp9dec.MV{Row: 9, Col: 8}},
		}},
	{miRow: 3, miCol: 3, bsize: 1, mode: 13, ref0: 1, ref1: -1, interp: 0,
		bmiSet: true, bmi: [4]nextDivBmi{ // NEW,NEW
			{mode: 13, mv: vp9dec.MV{Row: 17, Col: 0}},
			{mode: 13, mv: vp9dec.MV{Row: 5, Col: 16}},
		}},

	// ---- TOP-RIGHT 32x32 (indices 16..31): the next 32x32 in z-order ----
	// Ground truth: MI16BMI encode_b dump of the md5 c41fc299 oracle stream
	// (private $TMPDIR vpxenc, IVF byte-identical to the pinned vpxenc-vp9
	// oracle). mi(1,6) (index 22) is the PARTITION_SPLIT-vs-NONE frontier the
	// sub-8x8 x->pred_mv write (vp9_rdopt.c:2259) closed: govpx now commits the
	// libvpx 4x4 SPLIT [bmi: NEW,NEAREST,NEW,NEAREST] last-sub NEARESTMV mv(-7,39).
	// ---- 16x16(0,4): mi(0,4),(0,5),(1,4),(1,5) ----
	{miRow: 0, miCol: 4, bsize: 0, mode: 11, ref0: 1, ref1: -1, interp: 0,
		bmiSet: true, bmi: [4]nextDivBmi{ // NEAREST,NEAR,NEW,NEAR
			{mode: 10, mv: vp9dec.MV{Row: -8, Col: 40}},
			{mode: 11, mv: vp9dec.MV{Row: 16, Col: 8}},
			{mode: 13, mv: vp9dec.MV{Row: 24, Col: -16}},
			{mode: 11, mv: vp9dec.MV{Row: 16, Col: 8}},
		}},
	{miRow: 0, miCol: 5, bsize: 1, mode: 13, ref0: 1, ref1: -1, interp: 0,
		bmiSet: true, bmi: [4]nextDivBmi{ // NEW,NEW
			{mode: 13, mv: vp9dec.MV{Row: 8, Col: 16}},
			{mode: 13, mv: vp9dec.MV{Row: 8, Col: 8}},
		}},
	{miRow: 1, miCol: 4, bsize: 0, mode: 10, ref0: 1, ref1: -1, interp: 0,
		bmiSet: true, bmi: [4]nextDivBmi{ // NEW,NEW,NEW,NEAREST
			{mode: 13, mv: vp9dec.MV{Row: -6, Col: 8}},
			{mode: 13, mv: vp9dec.MV{Row: 8, Col: 8}},
			{mode: 13, mv: vp9dec.MV{Row: 40, Col: -40}},
			{mode: 10, mv: vp9dec.MV{Row: 40, Col: -40}},
		}},
	{miRow: 1, miCol: 5, bsize: 2, mode: 13, ref0: 1, ref1: -1, interp: 0,
		bmiSet: true, bmi: [4]nextDivBmi{ // NEW,NEW (8x4: bmi[0]==bmi[1] top row)
			{mode: 13, mv: vp9dec.MV{Row: 0, Col: 20}},
			{mode: 13, mv: vp9dec.MV{Row: 0, Col: 20}},
		}},

	// ---- 16x16(0,6): mi(0,6),(0,7),(1,6),(1,7) — contains the closed frontier ----
	{miRow: 0, miCol: 6, bsize: 3, mode: 10, ref0: 1, ref1: -1, interp: 0,
		mv: vp9dec.MV{Row: 8, Col: 8}},
	{miRow: 0, miCol: 7, bsize: 2, mode: 10, ref0: 1, ref1: -1, interp: 1,
		bmiSet: true, bmi: [4]nextDivBmi{ // NEW,NEW (8x4 top row)
			{mode: 13, mv: vp9dec.MV{Row: 16, Col: 11}},
			{mode: 13, mv: vp9dec.MV{Row: 16, Col: 11}},
		}},
	{miRow: 1, miCol: 6, bsize: 0, mode: 10, ref0: 1, ref1: -1, interp: 0,
		bmiSet: true, bmi: [4]nextDivBmi{ // NEW,NEAREST,NEW,NEAREST (the frontier)
			{mode: 13, mv: vp9dec.MV{Row: 8, Col: 28}},
			{mode: 10, mv: vp9dec.MV{Row: 8, Col: 28}},
			{mode: 13, mv: vp9dec.MV{Row: -7, Col: 39}},
			{mode: 10, mv: vp9dec.MV{Row: -7, Col: 39}},
		}},
	{miRow: 1, miCol: 7, bsize: 1, mode: 13, ref0: 1, ref1: -1, interp: 0,
		bmiSet: true, bmi: [4]nextDivBmi{ // NEW,NEW
			{mode: 13, mv: vp9dec.MV{Row: 23, Col: -8}},
			{mode: 13, mv: vp9dec.MV{Row: 21, Col: -19}},
		}},

	// ---- 16x16(2,4): mi(2,4),(2,5),(3,4),(3,5) ----
	{miRow: 2, miCol: 4, bsize: 1, mode: 13, ref0: 1, ref1: -1, interp: 0,
		bmiSet: true, bmi: [4]nextDivBmi{ // NEAR,NEW
			{mode: 11, mv: vp9dec.MV{Row: 24, Col: -16}},
			{mode: 13, mv: vp9dec.MV{Row: 24, Col: 0}},
		}},
	{miRow: 2, miCol: 5, bsize: 0, mode: 13, ref0: 1, ref1: -1, interp: 1,
		bmiSet: true, bmi: [4]nextDivBmi{ // NEW,NEAR,NEAREST,NEW
			{mode: 13, mv: vp9dec.MV{Row: 20, Col: 0}},
			{mode: 11, mv: vp9dec.MV{Row: 13, Col: 0}},
			{mode: 10, mv: vp9dec.MV{Row: 20, Col: 0}},
			{mode: 13, mv: vp9dec.MV{Row: 24, Col: -16}},
		}},
	{miRow: 3, miCol: 4, bsize: 2, mode: 13, ref0: 1, ref1: -1, interp: 0,
		bmiSet: true, bmi: [4]nextDivBmi{ // NEAR,NEAR (8x4 top row)
			{mode: 11, mv: vp9dec.MV{Row: 5, Col: 16}},
			{mode: 11, mv: vp9dec.MV{Row: 5, Col: 16}},
		}},
	{miRow: 3, miCol: 5, bsize: 1, mode: 13, ref0: 1, ref1: -1, interp: 1,
		bmiSet: true, bmi: [4]nextDivBmi{ // NEW,NEW
			{mode: 13, mv: vp9dec.MV{Row: 16, Col: -4}},
			{mode: 13, mv: vp9dec.MV{Row: 16, Col: 6}},
		}},

	// ---- 16x16(2,6): mi(2,6),(2,7),(3,6),(3,7) ----
	{miRow: 2, miCol: 6, bsize: 1, mode: 13, ref0: 1, ref1: -1, interp: 1,
		bmiSet: true, bmi: [4]nextDivBmi{ // NEAREST,NEW
			{mode: 10, mv: vp9dec.MV{Row: -7, Col: 39}},
			{mode: 13, mv: vp9dec.MV{Row: 8, Col: 0}},
		}},
	{miRow: 2, miCol: 7, bsize: 2, mode: 10, ref0: 1, ref1: -1, interp: 0,
		bmiSet: true, bmi: [4]nextDivBmi{ // NEAREST,NEAREST (8x4 top row)
			{mode: 10, mv: vp9dec.MV{Row: 23, Col: -8}},
			{mode: 10, mv: vp9dec.MV{Row: 23, Col: -8}},
		}},
	{miRow: 3, miCol: 6, bsize: 0, mode: 13, ref0: 1, ref1: -1, interp: 0,
		bmiSet: true, bmi: [4]nextDivBmi{ // NEAR,NEAR,NEW,NEW
			{mode: 11, mv: vp9dec.MV{Row: 16, Col: 6}},
			{mode: 11, mv: vp9dec.MV{Row: 8, Col: 0}},
			{mode: 13, mv: vp9dec.MV{Row: 8, Col: 0}},
			{mode: 13, mv: vp9dec.MV{Row: 8, Col: 5}},
		}},
	{miRow: 3, miCol: 7, bsize: 3, mode: 13, ref0: 1, ref1: -1, interp: 0,
		mv: vp9dec.MV{Row: 10, Col: 8}},
}

// nextDivClassifyPath returns the distinct libvpx code-path label a committed
// leaf exercises, for the frontier report. It keys off the committed shape:
// intra vs inter, sub-8x8 (bmiSet on a <8x8 bsize) vs whole-8x8 NONE, and the
// top-level inter mode (NEW/NEAREST/NEAR). This is the classification the parent
// uses to pick the next integration target.
func nextDivClassifyPath(b nextDivBlock) string {
	if b.intra {
		if b.bsize < 3 {
			return "sub-8x8 INTRA in inter frame (per-sub y-modes)"
		}
		return "whole-8x8 INTRA (DC) in inter frame"
	}
	modeName := map[int]string{10: "NEARESTMV", 11: "NEARMV", 12: "ZEROMV", 13: "NEWMV"}[b.mode]
	if b.bsize < 3 {
		return fmt.Sprintf("sub-8x8 SPLIT inter RD (last-sub %s, per-bmi NEW/NEAREST/NEAR)", modeName)
	}
	return fmt.Sprintf("16x16/8x8 NONE inter %s", modeName)
}

func nextDivRefName(r int) string {
	switch r {
	case -1:
		return "NONE"
	case 0:
		return "INTRA"
	case 1:
		return "LAST"
	case 2:
		return "GOLDEN"
	case 3:
		return "ALTREF"
	}
	return fmt.Sprintf("?%d", r)
}

// nextDivFmtCommitted renders a committed leaf (decoded mi) compactly for the log.
func nextDivFmtCommitted(mi *vp9dec.NeighborMi) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "bsize=%d mode=%d ref0=%s ref1=%s interp=%d mv=(%d,%d)",
		int(mi.SbType), int(mi.Mode), nextDivRefName(int(mi.RefFrame[0])),
		nextDivRefName(int(mi.RefFrame[1])), int(mi.InterpFilter),
		mi.Mv[0].Row, mi.Mv[0].Col)
	if mi.SbType < common.Block8x8 {
		fmt.Fprintf(&sb, " bmi=[")
		for i := 0; i < 4; i++ {
			if i > 0 {
				sb.WriteByte(' ')
			}
			fmt.Fprintf(&sb, "{m%d (%d,%d)}", int(mi.Bmi[i].AsMode),
				mi.Bmi[i].AsMv[0].Row, mi.Bmi[i].AsMv[0].Col)
		}
		sb.WriteByte(']')
	}
	return sb.String()
}

// nextDivFmtWant renders the libvpx-committed expectation compactly.
func nextDivFmtWant(b nextDivBlock) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "bsize=%d mode=%d ref0=%s ref1=%s interp=%d",
		b.bsize, b.mode, nextDivRefName(b.ref0), nextDivRefName(b.ref1), b.interp)
	if b.bsize == 3 && !b.intra {
		fmt.Fprintf(&sb, " mv=(%d,%d)", b.mv.Row, b.mv.Col)
	}
	if b.bmiSet {
		fmt.Fprintf(&sb, " bmi=[")
		for i := 0; i < 4; i++ {
			if i > 0 {
				sb.WriteByte(' ')
			}
			fmt.Fprintf(&sb, "{m%d (%d,%d)}", int(b.bmi[i].mode), b.bmi[i].mv.Row, b.bmi[i].mv.Col)
		}
		sb.WriteByte(']')
	}
	return sb.String()
}

// nextDivLeafDiff compares one decoded committed leaf to its libvpx expectation
// and returns the list of mismatching fields ("" slice => exact match). For
// sub-8x8 inter leaves it compares per-bmi mode+mv; for the lone sub-8x8 intra
// leaf it compares per-bmi mode only (no mv); for an 8x8 NONE inter leaf it
// compares the block-level mv. interp is compared on all inter leaves; on the
// intra leaf interp is libvpx's SWITCHABLE_FILTERS don't-care default so it is
// reported but not counted as a divergence on its own (the bitstream never reads
// an intra block's interp_filter).
func nextDivLeafDiff(mi *vp9dec.NeighborMi, b nextDivBlock) []string {
	var diffs []string
	if int(mi.SbType) != b.bsize {
		diffs = append(diffs, fmt.Sprintf("bsize %d!=%d", int(mi.SbType), b.bsize))
	}
	if int(mi.RefFrame[0]) != b.ref0 {
		diffs = append(diffs, fmt.Sprintf("ref0 %d!=%d", int(mi.RefFrame[0]), b.ref0))
	}
	if int(mi.RefFrame[1]) != b.ref1 {
		diffs = append(diffs, fmt.Sprintf("ref1 %d!=%d", int(mi.RefFrame[1]), b.ref1))
	}
	if int(mi.Mode) != b.mode {
		diffs = append(diffs, fmt.Sprintf("mode %d!=%d", int(mi.Mode), b.mode))
	}
	if !b.intra && int(mi.InterpFilter) != b.interp {
		diffs = append(diffs, fmt.Sprintf("interp %d!=%d", int(mi.InterpFilter), b.interp))
	}
	if b.bsize == 3 && !b.intra {
		if mi.Mv[0] != b.mv {
			diffs = append(diffs, fmt.Sprintf("mv (%d,%d)!=(%d,%d)",
				mi.Mv[0].Row, mi.Mv[0].Col, b.mv.Row, b.mv.Col))
		}
	}
	if b.bmiSet {
		// Sub-blocks per the libvpx bsize: 4x4 has 4 distinct labels; 4x8/8x4
		// have 2 distinct labels replicated across the 4 bmi slots. Compare the
		// slots the markdown annotates (the table lists 2 entries for 4x8/8x4).
		n := 4
		if b.bsize == 1 || b.bsize == 2 {
			n = 2
		}
		for i := 0; i < n; i++ {
			if int(mi.Bmi[i].AsMode) != int(b.bmi[i].mode) {
				diffs = append(diffs, fmt.Sprintf("bmi[%d].mode %d!=%d",
					i, int(mi.Bmi[i].AsMode), int(b.bmi[i].mode)))
			}
			if !b.intra && b.bmi[i].mv != (vp9dec.MV{}) && mi.Bmi[i].AsMv[0] != b.bmi[i].mv {
				diffs = append(diffs, fmt.Sprintf("bmi[%d].mv (%d,%d)!=(%d,%d)",
					i, mi.Bmi[i].AsMv[0].Row, mi.Bmi[i].AsMv[0].Col,
					b.bmi[i].mv.Row, b.bmi[i].mv.Col))
			}
		}
	}
	return diffs
}

func nextDivBlockFromCommittedMi(miRow, miCol int, mi *vp9dec.NeighborMi) nextDivBlock {
	b := nextDivBlock{
		miRow:  miRow,
		miCol:  miCol,
		bsize:  int(mi.SbType),
		mode:   int(mi.Mode),
		ref0:   int(mi.RefFrame[0]),
		ref1:   int(mi.RefFrame[1]),
		interp: int(mi.InterpFilter),
		intra:  mi.RefFrame[0] == int8(vp9dec.IntraFrame),
		mv:     mi.Mv[0],
	}
	if mi.SbType < common.Block8x8 {
		b.bmiSet = true
		for i := 0; i < 4; i++ {
			b.bmi[i] = nextDivBmi{
				mode: mi.Bmi[i].AsMode,
				mv:   mi.Bmi[i].AsMv[0],
			}
		}
	}
	return b
}

func nextDivFullSB0EncodeOrder() [][2]int {
	order := make([][2]int, 0, 64)
	for sbRow := 0; sbRow < 8; sbRow += 4 {
		for sbCol := 0; sbCol < 8; sbCol += 4 {
			for quadRow := 0; quadRow < 4; quadRow += 2 {
				for quadCol := 0; quadCol < 4; quadCol += 2 {
					order = append(order,
						[2]int{sbRow + quadRow, sbCol + quadCol},
						[2]int{sbRow + quadRow, sbCol + quadCol + 1},
						[2]int{sbRow + quadRow + 1, sbCol + quadCol},
						[2]int{sbRow + quadRow + 1, sbCol + quadCol + 1},
					)
				}
			}
		}
	}
	return order
}

func nextDivDecodeFrame1(t *testing.T, packets [][]byte) *VP9Decoder {
	t.Helper()
	if len(packets) < 2 {
		t.Fatalf("need >=2 packets, got %d", len(packets))
	}
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(packets[0]); err != nil {
		t.Fatalf("decode keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame after keyframe")
	}
	if err := d.Decode(packets[1]); err != nil {
		t.Fatalf("decode inter frame: %v", err)
	}
	return d
}

func nextDivReportLibvpxDynamicFrontier(t *testing.T,
	govpxMi func(r, c int) *vp9dec.NeighborMi,
	sources []*image.YCbCr,
	closedPrefixLen int,
) {
	t.Helper()
	available, err := vp9test.VpxencAvailable()
	if err != nil {
		t.Fatalf("VpxencAvailable: %v", err)
	}
	if !available {
		t.Log("vpxenc-vp9 not built; dynamic bottom-half frontier unavailable " +
			"(run `make vp9-vpxdec-tools`)")
		return
	}

	args := []string{
		"--end-usage=cbr",
		"--target-bitrate=1200",
		"--cpu-used=0",
		"--kf-min-dist=0",
		"--kf-max-dist=999",
		"--buf-sz=600",
		"--buf-initial-sz=400",
		"--buf-optimal-sz=500",
		"--drop-frame=0",
		"--timebase=1/30",
	}
	libvpxFrames, diag, err := vp9test.VpxencPacketsResult(sources, args...)
	if err != nil {
		t.Fatalf("vpxenc-vp9 encode failed: %v\n%s", err, diag)
	}
	libvpxDec := nextDivDecodeFrame1(t, libvpxFrames)
	defer libvpxDec.Close()

	const miCols = 8
	order := nextDivFullSB0EncodeOrder()
	if len(order) != 64 {
		t.Fatalf("dynamic encode order has %d entries, want 64", len(order))
	}
	for i, b := range vp9FullRDSeed0_2_0_0_2Frame1SB0EncodeOrder {
		if i >= len(order) {
			t.Fatalf("static table entry %d outside dynamic order", i)
		}
		if order[i] != ([2]int{b.miRow, b.miCol}) {
			t.Fatalf("static table order[%d] = mi(%d,%d), dynamic order has mi(%d,%d)",
				i, b.miRow, b.miCol, order[i][0], order[i][1])
		}
	}

	t.Log("dynamic libvpx bottom-half frontier walk (expected leaves decoded from " +
		"the pinned vpxenc-vp9 frame-1 packet):")
	firstDivIdx := -1
	var firstWant nextDivBlock
	var firstGot *vp9dec.NeighborMi
	for idx, pos := range order {
		r, c := pos[0], pos[1]
		libMi := &libvpxDec.miGrid[r*miCols+c]
		want := nextDivBlockFromCommittedMi(r, c, libMi)
		if idx < closedPrefixLen {
			static := vp9FullRDSeed0_2_0_0_2Frame1SB0EncodeOrder[idx]
			if diffs := nextDivLeafDiff(libMi, static); len(diffs) != 0 {
				t.Fatalf("static libvpx anchor [%d] mi(%d,%d) disagrees with live "+
					"vpxenc-vp9 decode %v\n  live %s\n  static %s",
					idx, r, c, diffs, nextDivFmtCommitted(libMi),
					nextDivFmtWant(static))
			}
			continue
		}

		got := govpxMi(r, c)
		diffs := nextDivLeafDiff(got, want)
		path := nextDivClassifyPath(want)
		if len(diffs) == 0 {
			t.Logf("  [%2d] mi(%d,%d) MATCH   %-48s | %s",
				idx, r, c, path, nextDivFmtCommitted(got))
			continue
		}
		if firstDivIdx < 0 {
			firstDivIdx = idx
			firstWant = want
			firstGot = got
		}
		t.Logf("  [%2d] mi(%d,%d) DIVERGE %-48s\n        got  %s\n        want %s\n        delta %v",
			idx, r, c, path, nextDivFmtCommitted(got), nextDivFmtWant(want),
			diffs)
	}
	if firstDivIdx < 0 {
		t.Log("NO divergence in the full dynamic SB0 walk: govpx deep engine " +
			"matches libvpx's committed frame-1 SB0 leaves")
		return
	}
	t.Logf("DYNAMIC FRONTIER: first bottom-half divergence at encode index %d = mi(%d,%d)",
		firstDivIdx, firstWant.miRow, firstWant.miCol)
	t.Logf("  libvpx-committed: %s", nextDivFmtWant(firstWant))
	t.Logf("  govpx-committed : %s", nextDivFmtCommitted(firstGot))
	t.Logf("  delta           : %v", nextDivLeafDiff(firstGot, firstWant))
	t.Logf("  DISTINCT PATH NEEDED: %s", nextDivClassifyPath(firstWant))
}

// TestVP9FullRDInterNextDivergenceSeed0_2_0_0_2 drives the deep engine and reports
// the first committed-block divergence after 16x16(0,0) in encode order, plus the
// distinct libvpx path each frontier leaf needs.
func TestVP9FullRDInterNextDivergenceSeed0_2_0_0_2(t *testing.T) {
	// Integrity-check the embedded encode-order window: unique positions inside
	// SB0's 8x8 mi grid, intra/inter mode-vs-ref consistency, and the encode-order
	// z-order invariant (each consecutive group of 4 is one 16x16 quad whose four
	// 8x8 children are the z-order set {(r,c),(r,c+1),(r+1,c),(r+1,c+1)}). A
	// transcription slip in the table is caught here before any frontier is
	// reported.
	if n := len(vp9FullRDSeed0_2_0_0_2Frame1SB0EncodeOrder); n == 0 || n%4 != 0 || n > 64 {
		t.Fatalf("encode-order table has %d entries, want a positive multiple of 4 <= 64", n)
	}
	seen := map[[2]int]bool{}
	for i, b := range vp9FullRDSeed0_2_0_0_2Frame1SB0EncodeOrder {
		key := [2]int{b.miRow, b.miCol}
		if seen[key] {
			t.Fatalf("encode-order entry %d duplicates mi(%d,%d)", i, b.miRow, b.miCol)
		}
		seen[key] = true
		if b.miRow < 0 || b.miRow > 7 || b.miCol < 0 || b.miCol > 7 {
			t.Fatalf("encode-order entry %d mi(%d,%d) outside SB0 8x8 grid", i, b.miRow, b.miCol)
		}
		if b.bsize < 0 || b.bsize > 3 {
			t.Fatalf("encode-order entry %d mi(%d,%d) bsize=%d, want a leaf <=8x8 (0..3)",
				i, b.miRow, b.miCol, b.bsize)
		}
		if b.intra {
			if b.ref0 != 0 || b.mode > 9 {
				t.Fatalf("encode-order entry %d mi(%d,%d) marked intra but ref0=%d mode=%d",
					i, b.miRow, b.miCol, b.ref0, b.mode)
			}
		} else if b.ref0 < 1 || b.mode < 10 {
			t.Fatalf("encode-order entry %d mi(%d,%d) inter but ref0=%d mode=%d",
				i, b.miRow, b.miCol, b.ref0, b.mode)
		}
	}
	// z-order quad invariant.
	for q := 0; q < len(vp9FullRDSeed0_2_0_0_2Frame1SB0EncodeOrder); q += 4 {
		quad := vp9FullRDSeed0_2_0_0_2Frame1SB0EncodeOrder[q : q+4]
		r, c := quad[0].miRow, quad[0].miCol
		if r%2 != 0 || c%2 != 0 {
			t.Fatalf("encode-order quad starting at index %d has odd top-left mi(%d,%d)", q, r, c)
		}
		want := [4][2]int{{r, c}, {r, c + 1}, {r + 1, c}, {r + 1, c + 1}}
		for j := 0; j < 4; j++ {
			if quad[j].miRow != want[j][0] || quad[j].miCol != want[j][1] {
				t.Fatalf("encode-order quad at index %d is not z-order: entry %d = mi(%d,%d), want mi(%d,%d)",
					q, q+j, quad[j].miRow, quad[j].miCol, want[j][0], want[j][1])
			}
		}
	}

	// Enable the full deep-RD inter stack (same set the sub-8x8 wrapper pin uses).
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
	e.SetOracleTraceWriter(io.Discard)
	srcs := newVP9NextDivPanningSources(width, height, 2)
	var frames [][]byte
	for i, s := range srcs {
		pkt, encErr := e.Encode(s)
		if encErr != nil {
			t.Fatalf("Encode frame %d: %v", i, encErr)
		}
		frames = append(frames, pkt)
	}
	if len(frames) < 2 {
		t.Fatalf("expected 2 packets, got %d", len(frames))
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if decErr := d.Decode(frames[0]); decErr != nil {
		t.Fatalf("decode keyframe: %v", decErr)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame after keyframe")
	}
	if decErr := d.Decode(frames[1]); decErr != nil {
		t.Fatalf("decode inter frame: %v", decErr)
	}

	const miCols = 8
	mi := func(r, c int) *vp9dec.NeighborMi { return &d.miGrid[r*miCols+c] }

	// ---- HARD GATE: the closed prefix must still byte-match ----
	// The 16x16(0,0) quad (indices 0..3) is the original closed milestone. The
	// sub-8x8 SPLIT-vs-VERT partition-context fix (encode_sb update_partition_context
	// replay for non-last split siblings, vp9_encoder_inter_partition.go
	// scoreVP9InterPartitionSplit) closed mi(0,2)/(0,3)/(1,2) (indices 4..6). The
	// sub-8x8 NEWMV write-reference fix (vp9EncoderBestInterRefMvs now differences
	// every NEWMV against the block-level NEAREST ref_mvs[ref][0], matching libvpx
	// pack_inter_mode_mvs vp9/encoder/vp9_bitstream.c:328-339, NOT the committed
	// block mode) closed mi(1,3) and indices 7..15: the whole top-left-32x32
	// window (the four 16x16 quads, all 16 leaves) is byte-exact.
	//
	// The sub-8x8 x->pred_mv write fix (rd_pick_best_sub8x8_mode now writes
	// e.fullRDPredMv[ref] = the NEW sub-block's subpel MV, the verbatim port of
	// vp9/encoder/vp9_rdopt.c:2259, so a sibling committed as a sub-8x8 SPLIT
	// leaves the sub-8x8 last-NEW MV in fullRDPredMv — the seed vp9_mv_pred feeds
	// the next 8x8 NONE leaf's NEWMV search as candidate[2]/pred_mv[2]) closed the
	// mi(1,6) PARTITION_SPLIT-vs-NONE divergence (index 22) AND the rest of the
	// top-right 32x32 up to mi(2,4): indices 16..24 are now byte-exact. Before the
	// fix govpx's mi(1,6) 8x8 NONE NEWMV seeded from the stale (17,11) (mi(0,7)'s
	// 8x8-NONE result) instead of libvpx's (16,4) (mi(0,7)'s sub-8x8 last-NEW),
	// converging to a spuriously-good mv(8,-1) (dist 87168 vs libvpx 147280) that
	// won the NONE pick (score 18.78M) and kept the 8x8 NONE where libvpx splits to
	// 4x4 [bmi: NEW,NEAREST,NEW,NEAREST] last-sub NEARESTMV mv(-7,39). Now govpx's
	// cand2 == libvpx's (16,4), the NEWMV finds mv(17,-11) dist 147280 (loses to
	// NEARMV), and the 8x8 NONE (NEARMV score 22.97M) loses to the 4x4 SPLIT
	// (22.85M) — exactly as libvpx (ground truth: private $TMPDIR vpxenc
	// GOVPX_MI16_TRACE, IVF md5 c41fc299 == oracle).
	//
	// The sub-8x8 seg_mvs[4][MAX_REF_FRAMES] NEWMV cache fix (the per-sub-block
	// NEWMV motion search now runs ONCE per (block, ref) and is reused across the
	// 8x8 block's three switchable filters — the verbatim port of libvpx's
	// function-scope seg_mvs init at vp9/encoder/vp9_rdopt.c:4327/4343-4346 +
	// the :2170 INVALID_MV guard) closed mi(2,5) (index 25) AND mi(3,4)/(3,5)/(2,6)
	// (indices 26..28): indices 0..28 are now byte-exact. Before the fix govpx's
	// segMvs cache was local to each rdPickBestSub8x8Mode (per-filter) call, so the
	// NEW search re-ran per filter from that filter's own (filter-dependent)
	// committed-prior-block seed and converged to a different MV. At mi(2,5) the
	// SMOOTH filter's last sub-block (block 3) NEW search seeded from SMOOTH's
	// block-2 NEAREST(20,0) and found mv(16,7) (brdcost 8340098) instead of the
	// cached filt-0 result mv(24,-16) (brdcost 2993736), inflating SMOOTH's
	// segment_rd to 19116869 (vs libvpx 13770507) so SMOOTH lost to SHARP — govpx
	// committed the SHARP segment bmi=[NEW,NEAR,NEAR,NEW]. With the shared cache
	// SMOOTH's block 3 reuses (24,-16), segment_rd=13770507 wins (tmp_rd 14364375 <
	// SHARP 15447581 < EIGHTTAP 15875012), and govpx commits bmi=[NEW(20,0),
	// NEAR(13,0),NEAREST(20,0),NEW(24,-16)] + SMOOTH — exactly libvpx (ground truth:
	// private $TMPDIR vpxenc GTSUB, IVF md5 c41fc299 == oracle).
	//
	// Lock the entire closed window [0..closedPrefixLen) as a hard regression gate;
	// the walk below reports the first divergence past it. The whole embedded
	// top-32x32 pair (all 32 leaves: top-left 32x32 + top-right 32x32 through
	// mi(3,7)) is now byte-exact.
	//
	// mi(2,7) (index 29) was the last frontier in this window: govpx kept an 8x8
	// NONE NEWMV mv(18,-7) interp=SMOOTH (this_rd 27082942) where libvpx splits to
	// 8x4 HORZ NEAREST(23,-8)+NEAREST EIGHTTAP. The HORZ arm already matched
	// libvpx byte-for-byte (dist 62029); the divergence was entirely in the NONE
	// 8x8 leaf. libvpx's NONE leaf evaluates NEWMV(18,-7) too, but after NEARESTMV
	// commits best_rd=29697399 the NEWMV candidate is filter-locked to the model's
	// pick (EIGHTTAP, model rd 28.3M) and its genuine super_block_yrd accumulator
	// overruns that budget → rate_y=INT_MAX → handle_inter_mode returns INT64_MAX
	// → NEWMV is pruned and the NONE leaf commits NEARESTMV(21,-19) (rdcost 29.8M),
	// which then loses to HORZ (27.79M). govpx without the running-best budget ran
	// NEWMV's full RD with the cheaper SMOOTH filter at an INFINITE budget and let
	// it win. Enabling vp9InterUseDeepRDRefBestRD threads best_rd into every
	// handle_inter_mode call so the genuine yrd/uvrd budget early-exits fire,
	// reproducing the prune (libvpx ground truth: $TMPDIR vpxenc-vp9 fprintf at
	// rd_pick_inter_mode_sb/handle_inter_mode, IVF md5 c41fc299 == oracle). The
	// next frontier has moved into the BOTTOM half of SB0 (mi rows 4..7), which
	// this embedded table does not yet cover.
	const closedPrefixLen = 32 // full top-32x32 pair (top-left + top-right)
	for i := 0; i < closedPrefixLen; i++ {
		b := vp9FullRDSeed0_2_0_0_2Frame1SB0EncodeOrder[i]
		m := mi(b.miRow, b.miCol)
		if diffs := nextDivLeafDiff(m, b); len(diffs) != 0 {
			t.Fatalf("REGRESSION: closed-prefix leaf [%d] mi(%d,%d) diverged %v\n  got  %s\n  want %s",
				i, b.miRow, b.miCol, diffs, nextDivFmtCommitted(m), nextDivFmtWant(b))
		}
	}
	t.Logf("closed prefix [0..%d] byte-exact (entire top-32x32 pair: top-left 32x32 "+
		"all four 16x16 quads + top-right 32x32 through mi(3,7), incl. mi(1,6) SPLIT "+
		"closure, per-sub bmi MVs, the mi(2,5)/(2,6) seg_mvs cross-filter NEW-search "+
		"cache closure, and the mi(2,7) NONE NEWMV ref_best_rd budget prune)",
		closedPrefixLen-1)

	// ---- WALK the rest of SB0 in encode order; report the first divergence ----
	t.Logf("frame-1 SB0 post-closed-prefix walk (encode order), deep engine committed vs libvpx:")
	firstDivIdx := -1
	for idx, b := range vp9FullRDSeed0_2_0_0_2Frame1SB0EncodeOrder {
		if idx < closedPrefixLen {
			continue // closed prefix already asserted as a hard gate
		}
		m := mi(b.miRow, b.miCol)
		diffs := nextDivLeafDiff(m, b)
		path := nextDivClassifyPath(b)
		if len(diffs) == 0 {
			t.Logf("  [%2d] mi(%d,%d) MATCH   %-48s | %s", idx, b.miRow, b.miCol, path,
				nextDivFmtCommitted(m))
			continue
		}
		if firstDivIdx < 0 {
			firstDivIdx = idx
		}
		t.Logf("  [%2d] mi(%d,%d) DIVERGE %-48s\n        got  %s\n        want %s\n        delta %v",
			idx, b.miRow, b.miCol, path, nextDivFmtCommitted(m), nextDivFmtWant(b), diffs)
	}

	if firstDivIdx < 0 {
		t.Logf("NO divergence in the embedded top-32x32-pair window (%d leaves): the deep "+
			"engine matches libvpx for every leaf transcribed here. The frontier has "+
			"moved into the BOTTOM half of SB0 (mi rows 4..7); using the pinned "+
			"vpxenc-vp9 oracle, when available, to localise it dynamically.",
			len(vp9FullRDSeed0_2_0_0_2Frame1SB0EncodeOrder))
		nextDivReportLibvpxDynamicFrontier(t, mi, srcs, closedPrefixLen)
		mi72, ok := e.vp9CapturedSub8x8InterMi72Commit()
		if !ok {
			t.Fatal("no sub-8x8 inter commit capture for mi(7,2)")
		}
		if mi72.Bsize != common.Block4x4 || mi72.Mode != common.NearMv ||
			mi72.RefFrame != int8(vp9dec.LastFrame) ||
			mi72.InterpFilter != vp9dec.InterpEighttap || mi72.Skip2 {
			t.Fatalf("mi(7,2) capture = {bsize=%d mode=%d ref=%d filter=%d skip2=%t}, "+
				"want {Block4x4 NEARMV LAST EIGHTTAP skip2=false}",
				mi72.Bsize, mi72.Mode, mi72.RefFrame, mi72.InterpFilter, mi72.Skip2)
		}
		if mi72.RateY != 83846 || mi72.RateUV != 26511 ||
			mi72.DistUV != 31280 || mi72.Rate != 120990 ||
			mi72.Distortion != 79019 || mi72.ThisRD != 42998663 {
			t.Fatalf("mi(7,2) rate tuple = {rateY=%d rateUV=%d distUV=%d "+
				"rate=%d dist=%d rd=%d}, want {83846 26511 31280 120990 79019 42998663}",
				mi72.RateY, mi72.RateUV, mi72.DistUV, mi72.Rate, mi72.Distortion,
				mi72.ThisRD)
		}
		return
	}

	fb := vp9FullRDSeed0_2_0_0_2Frame1SB0EncodeOrder[firstDivIdx]
	fm := mi(fb.miRow, fb.miCol)
	t.Logf("FRONTIER: first post-16x16(0,0) divergence at encode index %d = mi(%d,%d)",
		firstDivIdx, fb.miRow, fb.miCol)
	t.Logf("  libvpx-committed: %s", nextDivFmtWant(fb))
	t.Logf("  govpx-committed : %s", nextDivFmtCommitted(fm))
	t.Logf("  delta           : %v", nextDivLeafDiff(fm, fb))
	t.Logf("  DISTINCT PATH NEEDED: %s", nextDivClassifyPath(fb))
	t.Logf("  capture points: libvpx commit encode_b vp9/encoder/vp9_encodeframe.c:2226 " +
		"(after update_state finalises xd->mi[0]); partition walk encode_sb :2253; " +
		"sub-8x8 RD vp9_rd_pick_inter_mode_sub8x8 (vp9_rdopt.c:4294); " +
		"single-ref inter RD vp9_rd_pick_inter_mode_sb (vp9_rdopt.c:3445).")
}

func TestVP9FullRDInterSub8x8Frame1ReconstructionParity(t *testing.T) {
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
	sources := newVP9NextDivPanningSources(width, height, 2)
	e, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()
	govpxFrames := make([][]byte, 0, len(sources))
	for i, src := range sources {
		pkt, encErr := e.Encode(src)
		if encErr != nil {
			t.Fatalf("Encode frame %d: %v", i, encErr)
		}
		govpxFrames = append(govpxFrames, append([]byte(nil), pkt...))
	}
	libvpxFrames := vp9test.VpxencPackets(t, sources,
		"--end-usage=cbr",
		"--target-bitrate=1200",
		"--cpu-used=0",
		"--kf-min-dist=0",
		"--kf-max-dist=999",
		"--buf-sz=600",
		"--buf-initial-sz=400",
		"--buf-optimal-sz=500",
		"--drop-frame=0",
		"--timebase=1/30",
	)
	if len(govpxFrames) != 2 || len(libvpxFrames) != 2 {
		t.Fatalf("frame count govpx=%d libvpx=%d, want 2/2",
			len(govpxFrames), len(libvpxFrames))
	}
	if len(govpxFrames[1]) != len(libvpxFrames[1]) {
		t.Fatalf("frame1 length govpx=%d libvpx=%d; sub-8x8 zcoeff replay drifted",
			len(govpxFrames[1]), len(libvpxFrames[1]))
	}
	gotI420 := vp9DecodeVisibleI420ForTest(t, govpxFrames...)
	wantI420 := vp9DecodeVisibleI420ForTest(t, libvpxFrames...)
	if !bytes.Equal(gotI420, wantI420) {
		t.Fatalf("frame1 decoded I420 mismatch after deep full-RD sub-8x8 encode: govpx=%s libvpx=%s",
			vp9test.MD5Hex(gotI420), vp9test.MD5Hex(wantI420))
	}
}

// newVP9NextDivPanningSources mirrors vp9test.NewPanningSources(w,h,n) (the exact
// panning generator the {0,2,0,0,2} ground-truth capture used) without importing
// the vp9test package into the in-package (govpx) test. The byte formula is
// identical to vp9test.NewPanningYCbCr (internal/testutil/vp9test/image.go:30),
// so the sources are pixel-identical to the capture and to the sub-8x8 wrapper
// pin. Kept local so this analysis probe drives the deep engine standalone (no
// oracle binary needed: the libvpx side is the md5-anchored embedded table).
func newVP9NextDivPanningSources(width, height, frames int) []*image.YCbCr {
	out := make([]*image.YCbCr, frames)
	for f := 0; f < frames; f++ {
		out[f] = newVP9NextDivPanningYCbCr(width, height, f)
	}
	return out
}

func newVP9NextDivPanningYCbCr(width, height, frame int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	for y := 0; y < height; y++ {
		row := img.Y[y*img.YStride:]
		for x := 0; x < width; x++ {
			row[x] = byte(24 + ((x+frame*3)*7+y*11+(x*y+frame*13)%37)%208)
		}
	}
	uvWidth, uvHeight := buffers.Chroma420Dimensions(width, height)
	for y := 0; y < uvHeight; y++ {
		cb := img.Cb[y*img.CStride:]
		cr := img.Cr[y*img.CStride:]
		for x := 0; x < uvWidth; x++ {
			cb[x] = byte(64 + ((x+frame)*5+y*3)%128)
			cr[x] = byte(72 + (x*3+(y+frame)*7)%112)
		}
	}
	return img
}
