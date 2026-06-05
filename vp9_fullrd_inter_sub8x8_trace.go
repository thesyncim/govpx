package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// vp9_fullrd_inter_sub8x8_trace.go provides the oracle-trace verification
// driver for the genuine sub-8x8 joint RD producer. It reproduces, for the
// documented frame-1 SB0 16x16(0,0) child at mi=(0,1) BLOCK_4X4 ref=LAST
// EIGHTTAP, the per-label encode_inter_mb_segment RD + the set_and_cost_bmi_mvs
// mode/MV rate for the libvpx-committed sub-block modes/MVs, and the
// per-sub-block NEWMV motion search MV. The capture is compared against the
// libvpx ground truth (rd_pick_best_sub8x8_mode fprintf, reverted) by
// TestVP9FullRDSub8x8Frame1Parity.
//
// The production sub-8x8 partition search never descends to this 8x8 child
// (govpx commits NONE/HORZ at 16x16(0,0)), so the producer's per-label residual
// RD + cost machinery is verified directly here — fed the live frame-1 encoder
// context (source, reference, quantizer) + the libvpx-committed candidates —
// rather than through the production decision flow. This isolates the verified
// component (the residual-RD + cost + motion-search port) from the partition
// recursion + pred_mv grid-state thread that is the NEXT step.

// vp9Sub8x8CaptureLabel is one captured label's RD decomposition.
type vp9Sub8x8CaptureLabel struct {
	Block      int
	Mode       common.PredictionMode
	Mv         vp9dec.MV
	ModeMvRate int    // set_and_cost_bmi_mvs return (brate - byrate)
	Byrate     int    // encode_inter_mb_segment *labelyrate
	Bdist      uint64 // *distortion
	Bsse       uint64 // *sse
	Brdcost    uint64 // the assembled brdcost
	Eob        int
	Valid      bool
}

// vp9Sub8x8Capture is the full sub-8x8 producer verification capture.
type vp9Sub8x8Capture struct {
	Labels [4]vp9Sub8x8CaptureLabel
	// SearchBlock2Mv is the genuine per-sub-block NEWMV motion search result for
	// label 2 (libvpx new_mv=(9,4)).
	SearchBlock2Mv    vp9dec.MV
	SearchBlock2Valid bool
	// Seg is the full rdPickBestSub8x8Mode segment result run with the injected
	// libvpx candidate context (the (0,1) SPLIT 4x4 child).
	Seg      vp9Sub8x8SegResult
	SegValid bool
	// Vert is the (1,1) BLOCK_4X8 (VERT) child segment — partition-shape coverage
	// for the multi-4x4-unit-per-label path.
	Vert      vp9Sub8x8SegResult
	VertValid bool
}

// vp9Sub8x8FrameMv builds an injected NEAREST/NEAR candidate pair.
func vp9Sub8x8FrameMv(nrRow, nrCol, naRow, naCol int) vp9Sub8x8FrameMvPair {
	return vp9Sub8x8FrameMvPair{
		nearest: vp9dec.MV{Row: int16(nrRow), Col: int16(nrCol)},
		near:    vp9dec.MV{Row: int16(naRow), Col: int16(naCol)},
	}
}

// vp9TraceSub8x8Segment runs rdPickBestSub8x8Mode for an arbitrary 16x16(0,0)
// child with injected candidate context, for partition-shape coverage.
func (e *VP9Encoder) vp9TraceSub8x8Segment(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, bestRefMv vp9dec.MV, modeContext int,
	seed vp9Sub8x8SegmentEntropy, frameMv [4]vp9Sub8x8FrameMvPair,
) vp9Sub8x8SegResult {
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = e.vp9MiAt(miRows, miCols, miRow, miCol-1)
	}
	above := e.vp9MiAt(miRows, miCols, miRow-1, miCol)
	switchableCtx := vp9dec.GetPredContextSwitchableInterp(above, left)
	in := vp9Sub8x8Input{
		tile:              tile,
		miRows:            miRows,
		miCols:            miCols,
		miRow:             miRow,
		miCol:             miCol,
		interModeMask:     e.vp9InterModeMaskFor(bsize),
		switchableCtx:     switchableCtx,
		above:             above,
		left:              left,
		rdmult:            e.cbRdmult,
		bestRDInf:         true,
		injectValid:       true,
		injectBestRefMv:   bestRefMv,
		injectModeContext: modeContext,
		injectSeed:        seed,
		injectFrameMv:     frameMv,
	}
	return e.rdPickBestSub8x8Mode(inter, in, bsize, int8(vp9dec.LastFrame),
		vp9dec.InterpEighttap)
}

// vp9TraceSub8x8Producer is invoked from the inter mode loop's frame-1 SB0
// capture site. It builds the producer input for mi=(0,1) BLOCK_4X4 ref=LAST
// EIGHTTAP and captures (a) the per-label RD for the libvpx-committed modes/MVs,
// (b) the genuine block-2 NEWMV motion search MV, and (c) the full
// rdPickBestSub8x8Mode genuine-derivation segment result. Compile-elided in
// production (callers gate on vp9OracleTraceBuild).
func (e *VP9Encoder) vp9TraceSub8x8Producer(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols int,
) {
	const (
		miRow = 0
		miCol = 1 // the 16x16(0,0) child at mi=(0,1)
	)
	bsize := common.Block4x4
	refFrame := int8(vp9dec.LastFrame)
	filter := vp9dec.InterpEighttap

	interModeCtxArr := vp9dec.InterModeContext(e.miGrid, miCols, tile,
		miRows, miRow, miCol, common.Block8x8)
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = e.vp9MiAt(miRows, miCols, miRow, miCol-1)
	}
	above := e.vp9MiAt(miRows, miCols, miRow-1, miCol)
	switchableCtx := vp9dec.GetPredContextSwitchableInterp(above, left)
	in := vp9Sub8x8Input{
		tile:          tile,
		miRows:        miRows,
		miCols:        miCols,
		miRow:         miRow,
		miCol:         miCol,
		interModeMask: e.vp9InterModeMaskFor(bsize),
		switchableCtx: switchableCtx,
		above:         above,
		left:          left,
		rdmult:        e.cbRdmult,
		bestRDInf:     true,
	}

	var cap vp9Sub8x8Capture

	// (a) Per-label RD for the libvpx-committed modes/MVs. The bestRefMv is the
	// 8x8 NEAREST candidate (bsi->ref_mv[0]) and modeContext is
	// mbmi_ext->mode_context[LAST]. The production partition search never
	// descends to this 8x8 child, so the mi grid at the trace site does NOT hold
	// the SPLIT-recursion neighbour state libvpx has when it reaches (0,1)
	// (govpx derives bestRefMv=(0,0), ctx=5 here vs libvpx's (9,15), ctx=3).
	// Inject the libvpx-captured candidate context (CAND probe) so the verified
	// component — the residual-RD + cost + motion-search port — is checked on the
	// exact inputs libvpx fed it. The candidate-derivation grid-state thread is
	// the NEXT step (the genuine partition recursion).
	bestRefMv := vp9dec.MV{Row: 9, Col: 15}
	modeContext := 3
	_ = interModeCtxArr
	num4x4W := int(common.Num4x4BlocksWideLookup[bsize])
	num4x4H := int(common.Num4x4BlocksHighLookup[bsize])

	committed := [4]struct {
		mode common.PredictionMode
		mv   vp9dec.MV
	}{
		{common.NearestMv, vp9dec.MV{Row: 9, Col: 15}},
		{common.NearestMv, vp9dec.MV{Row: 9, Col: 15}},
		{common.NewMv, vp9dec.MV{Row: 9, Col: 4}},
		{common.NearestMv, vp9dec.MV{Row: 9, Col: 4}},
	}

	var mi vp9dec.NeighborMi
	mi.SbType = bsize
	mi.RefFrame = [2]int8{refFrame, vp9dec.NoRefFrame}
	mi.InterpFilter = uint8(filter)
	// t_above/t_left seed: the 8x8(0,1) plane context at the START of
	// rd_pick_best_sub8x8_mode. libvpx's SPLIT recursion coded the left 8x8 child
	// (0,0) first, so t_left=[1,1] (its coeffs); the frame top gives t_above=
	// [0,0] (SEED probe). The trace site has no such recursion state, so inject
	// the captured seed (same component-verification rationale as bestRefMv).
	var ent vp9Sub8x8SegmentEntropy
	ent.above = [2]uint8{0, 0}
	ent.left = [2]uint8{1, 1}
	ent0 := ent // pristine seed for the (c) genuine-derivation run

	for block := 0; block < 4; block++ {
		mode := committed[block].mode
		mv := committed[block].mv
		modeMvRate := e.setAndCostBmiMvs(inter, &mi, block, mode, mv, bestRefMv,
			modeContext, inter.allowHP, num4x4W, num4x4H)
		candEnt := ent
		seg, ok := e.encodeInterMbSegment(inter, in, &candEnt, bsize, block,
			refFrame, &mi, rdCostMaxLocal)
		if !ok {
			continue
		}
		ent = candEnt
		brdcost := seg.rdcost + encoder.RDCost(in.rdmult, encoder.RDDivBits,
			modeMvRate, 0)
		cap.Labels[block] = vp9Sub8x8CaptureLabel{
			Block:      block,
			Mode:       mode,
			Mv:         mv,
			ModeMvRate: modeMvRate,
			Byrate:     seg.byrate,
			Bdist:      seg.dist,
			Bsse:       seg.sse,
			Brdcost:    brdcost,
			Eob:        seg.eob,
			Valid:      true,
		}
	}

	// (b) Genuine block-2 NEWMV motion search. The grid bmi[0..1] must hold the
	// committed block 0/1 MVs for the bsi->mvp seed (block 2 uses bmi[0]); the
	// loop above committed them into mi.
	if sm, ok := e.vp9Sub8x8NewMvSearch(inter, in, bsize, 2, refFrame, &mi,
		bestRefMv); ok {
		cap.SearchBlock2Mv = sm
		cap.SearchBlock2Valid = true
	}

	// (c) Full genuine-derivation segment result: run the whole
	// rdPickBestSub8x8Mode (per-label mode SELECTION + accumulation) with the
	// injected libvpx candidate context, so the producer's end-to-end segment
	// (which mode it picks per label + bsi->r/d/sse/segment_rd) is verified.
	inSeg := in
	inSeg.injectValid = true
	inSeg.injectBestRefMv = bestRefMv
	inSeg.injectModeContext = modeContext
	inSeg.injectSeed = ent0
	// Per-block append_sub8x8 NEAREST/NEAR candidates (CAND probe):
	// block 0/1/2: nearest=(9,15) near=(0,0); block 3: nearest=(9,4) near=(9,15).
	inSeg.injectFrameMv[0] = vp9Sub8x8FrameMv(9, 15, 0, 0)
	inSeg.injectFrameMv[1] = vp9Sub8x8FrameMv(9, 15, 0, 0)
	inSeg.injectFrameMv[2] = vp9Sub8x8FrameMv(9, 15, 0, 0)
	inSeg.injectFrameMv[3] = vp9Sub8x8FrameMv(9, 4, 9, 15)
	seg := e.rdPickBestSub8x8Mode(inter, inSeg, bsize, refFrame, filter)
	if seg.Valid {
		cap.Seg = seg
		cap.SegValid = true
	}

	// (d) VERT(4x8) partition-shape coverage: the 16x16(0,0) child at mi=(1,1)
	// committed BLOCK_4X8 (label probe), exercising the multi-4x4-unit-per-label
	// path the 4x4 case doesn't. Inject its captured candidate context:
	// bestrefmv=(9,4) modectx=5 seed t_above=[1,1] t_left=[1,1];
	// block0 nearest=(9,4) near=(9,15); block1 nearest=(9,4) near=(9,15).
	cap.Vert = e.vp9TraceSub8x8Segment(inter, tile, miRows, miCols, 1, 1,
		common.Block4x8, vp9dec.MV{Row: 9, Col: 4}, 5,
		vp9Sub8x8SegmentEntropy{above: [2]uint8{1, 1}, left: [2]uint8{1, 1}},
		[4]vp9Sub8x8FrameMvPair{
			vp9Sub8x8FrameMv(9, 4, 9, 15),
			vp9Sub8x8FrameMv(9, 4, 9, 15),
			{}, {},
		})
	cap.VertValid = cap.Vert.Valid

	e.recordVP9FullRDSub8x8(cap)
}
