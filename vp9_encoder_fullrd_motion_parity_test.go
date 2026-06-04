//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

// TestVP9EncoderFullRDFrame1SB0FullPelMvParity pins the first landed step of
// the holistic full-RD inter port: the verbatim full_pixel_diamond
// (vp9_mcomp.c:2486, step_param=cpi->mv_step_param==0 on the no-recode RT
// path, variance-rescoring via vp9_get_mvpred_var @ :1454) now selects the
// SAME full-pel MV as libvpx's single_motion_search for the documented first
// inter divergence: seed {0,2,0,0,2} (CBR 1200 kbps, cpu0 realtime) frame 1,
// SB0, block (0,0) -> the 64x64 root block.
//
// libvpx ground truth (TEMPORARY fprintf in vp9_rdopt.c single_motion_search,
// vpxenc-vp9 rebuilt, then reverted): for frame 1 / mi(0,0) / ref=LAST /
// bsize=BLOCK_64X64 / mvp_full=(0,0) / ref_mv=(0,0) the full-pixel result is
// tmp_mv=(row=1,col=1) with bestsme=8896511. The pre-wiring SAD-only fan
// produced (row=2,col=0). This test asserts the wired full-pel MV == (1,1).
//
// NOTE: end-to-end byte parity for this seed does NOT close from the MV alone
// (frame 1 still diverges downstream): the per-block mode/ref/filter/tx/coef
// RD loop and partition recursion remain to be ported. This pins only the
// motion-search step.
func TestVP9EncoderFullRDFrame1SB0FullPelMvParity(t *testing.T) {
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
	// Encode frame 0 (keyframe) then frame 1 (first inter frame).
	for i := 0; i < 2; i++ {
		if _, err := e.EncodeIntoWithResult(sources[i], dst); err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", i, err)
		}
	}

	row, col, ok := e.vp9FullRDFirstInterMv()
	if !ok {
		t.Fatal("no full-RD frame-1 SB0 (0,0) NEWMV full-pel MV was captured; " +
			"the full-RD single-ref NEWMV search did not run")
	}
	const wantRow, wantCol = 1, 1 // libvpx single_motion_search tmp_mv
	if row != wantRow || col != wantCol {
		t.Fatalf("full-RD frame-1 SB0 (0,0) full-pel MV = (row=%d,col=%d), "+
			"want libvpx (row=%d,col=%d)", row, col, wantRow, wantCol)
	}
}

// TestVP9EncoderFullRDFrame1SB0SubpelMvParity pins the SUBPEL refinement step
// of the holistic full-RD inter port. After the full-pel diamond returns (1,1)
// for the frame-1 SB0 64x64 (0,0) NEWMV, the full-RD subpel tree (libvpx
// vp9_rdopt.c:2728 cpi->find_fractional_mv_step = vp9_find_best_sub_pixel_tree,
// x->errorperbit) refines it to the libvpx subpel result.
//
// libvpx ground truth (TEMPORARY fprintf in single_motion_search, vpxenc-vp9
// rebuilt then reverted; seed {0,2,0,0,2} = CBR 1200 kbps cpu0 realtime,
// frame 1 / mi(0,0) / ref=LAST):
//   - BLOCK_64X64: subpel tmp_mv=(row=12,col=4) (1/8-pel), bestsme=8896511.
//   - BLOCK_32X32 (0,0): mvp_full=(row=1,col=0), best_predmv_idx=2,
//     pred_mv[2]=(12,4) — i.e. the propagated 64x64 subpel x->pred_mv[LAST].
//
// This test pins the verified 64x64 subpel MV (12,4) only. libvpx then stores
// it as x->pred_mv[LAST] and the 32x32 (0,0) reuses it as vp9_mv_pred's third
// candidate (pred_mv[2]) to derive mvp_full=(1,0). That candidate[2]
// propagation is NOT yet wired into govpx's full-RD vp9_mv_pred: govpx's
// exploratory shallow-partition picker, combined with its still-incomplete
// full-pel large-motion search, propagates a poor root-block MV on
// SEARCH_PARTITION unit content and regresses the planted-MV motion-search
// tests. A faithful propagation needs the holistic single-pass
// rd_pick_partition recursion so the committed depth-first order matches
// libvpx. End-to-end byte parity does NOT close (more per-block RD steps
// remain).
func TestVP9EncoderFullRDFrame1SB0SubpelMvParity(t *testing.T) {
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

	subRow, subCol, ok := e.vp9FullRDFirstInterSubpelMv()
	if !ok {
		t.Fatal("no full-RD frame-1 SB0 (0,0) 64x64 NEWMV subpel MV was " +
			"captured; the full-RD subpel refinement did not run")
	}
	const wantSubRow, wantSubCol = 12, 4 // libvpx single_motion_search tmp_mv
	if subRow != wantSubRow || subCol != wantSubCol {
		t.Fatalf("full-RD frame-1 SB0 (0,0) 64x64 subpel MV = (row=%d,col=%d), "+
			"want libvpx (row=%d,col=%d)", subRow, subCol, wantSubRow, wantSubCol)
	}
}
