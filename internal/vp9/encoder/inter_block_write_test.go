package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// TestWriteInterBlockSingleRefNearestMv: 16x16 inter block, single ref
// (LastFrame), NearestMv mode (no MV emit), non-switchable interp,
// TxMode=Only4x4 (no tx-size cascade), seg disabled. Round-trips via
// the individual decoder readers in pack_inter_mode_mvs order.
func TestWriteInterBlockSingleRefNearestMv(t *testing.T) {
	var seg vp9dec.SegmentationParams
	var fc vp9dec.FrameContext
	// 128 for every prob; halfway point so bits land mid-range.
	for i := range fc.SkipProbs {
		fc.SkipProbs[i] = 128
	}
	for i := range fc.IntraInterProb {
		fc.IntraInterProb[i] = 128
	}
	for i := range fc.InterModeProbs {
		for j := range fc.InterModeProbs[i] {
			fc.InterModeProbs[i][j] = 128
		}
	}
	for i := range fc.ReferenceModeProbs.SingleRefProb {
		fc.ReferenceModeProbs.SingleRefProb[i] = [2]uint8{128, 128}
	}

	mi := &vp9dec.NeighborMi{
		SbType:   common.Block16x16,
		Mode:     common.NearestMv,
		TxSize:   common.Tx4x4,
		RefFrame: [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame},
	}

	buf := make([]byte, 128)
	var bw bitstream.Writer
	bw.Start(buf)
	WriteInterBlock(&bw, WriteInterBlockArgs{
		Seg:          &seg,
		Mi:           mi,
		Fc:           &fc,
		TxMode:       common.Only4x4,
		FrameRefMode: vp9dec.SingleReference,
		InterpFilter: vp9dec.InterpEighttap, // non-switchable: no bit emitted
		InterModeCtx: 0,
	})
	size, err := bw.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}

	gotSkip := vp9dec.ReadSkipWithSeg(&r, &seg, 0, &fc, nil, nil)
	if gotSkip != 0 {
		t.Errorf("skip = %d, want 0", gotSkip)
	}
	gotInter := vp9dec.ReadIntraInterFlag(&r, &fc, nil, nil)
	if gotInter != 1 {
		t.Errorf("is_inter = %d, want 1", gotInter)
	}

	var refOut [2]int8
	vp9dec.ReadRefFrames(&r, vp9dec.SingleReference,
		[vp9dec.MaxRefFrames]uint8{},
		vp9dec.CompoundFrameRefs{},
		&seg, 0, &fc, nil, nil, &refOut)
	if refOut[0] != vp9dec.LastFrame || refOut[1] != vp9dec.NoRefFrame {
		t.Errorf("ref_frame = %v, want (Last, NoRef)", refOut)
	}

	mode := vp9dec.ReadInterMode(&r, fc.InterModeProbs[0])
	if mode != common.NearestMv {
		t.Errorf("inter mode = %d, want %d", mode, common.NearestMv)
	}
}

// TestWriteInterBlockIntraPath: inter frame, intra block. Emits
// segment + skip + is_inter=0 + Y intra mode + UV intra mode. Verify
// via the size-group-indexed Y/UV readers.
func TestWriteInterBlockIntraPath(t *testing.T) {
	var seg vp9dec.SegmentationParams
	var fc vp9dec.FrameContext
	for i := range fc.SkipProbs {
		fc.SkipProbs[i] = 128
	}
	for i := range fc.IntraInterProb {
		fc.IntraInterProb[i] = 128
	}
	for g := range fc.YModeProb {
		for j := range fc.YModeProb[g] {
			fc.YModeProb[g][j] = 128
		}
	}
	for y := range fc.UvModeProb {
		for j := range fc.UvModeProb[y] {
			fc.UvModeProb[y][j] = 128
		}
	}

	mi := &vp9dec.NeighborMi{
		SbType:   common.Block16x16,
		Mode:     common.HPred,
		TxSize:   common.Tx4x4,
		RefFrame: [2]int8{vp9dec.IntraFrame, vp9dec.NoRefFrame},
	}
	uvMode := common.D135Pred

	buf := make([]byte, 128)
	var bw bitstream.Writer
	bw.Start(buf)
	WriteInterBlock(&bw, WriteInterBlockArgs{
		Seg:          &seg,
		Mi:           mi,
		Fc:           &fc,
		TxMode:       common.Only4x4,
		FrameRefMode: vp9dec.SingleReference,
		InterpFilter: vp9dec.InterpEighttap,
		UvMode:       uvMode,
	})
	size, _ := bw.Stop()

	var r bitstream.Reader
	r.Init(buf[:size])
	if got := vp9dec.ReadSkipWithSeg(&r, &seg, 0, &fc, nil, nil); got != 0 {
		t.Errorf("skip = %d, want 0", got)
	}
	if got := vp9dec.ReadIntraInterFlag(&r, &fc, nil, nil); got != 0 {
		t.Errorf("is_inter = %d, want 0", got)
	}
	sg := int(common.SizeGroupLookup[common.Block16x16])
	gotY := vp9dec.ReadIntraModeYInter(&r, &fc, sg)
	if gotY != mi.Mode {
		t.Errorf("Y mode = %d, want %d", gotY, mi.Mode)
	}
	gotUv := vp9dec.ReadIntraModeUvInter(&r, &fc, gotY)
	if gotUv != uvMode {
		t.Errorf("UV mode = %d, want %d", gotUv, uvMode)
	}
}

// fillFcUniform initializes the half of FrameContext the inter-block
// path consults with a uniform 128 prob so writer/reader stay in
// sync on every node.
func fillFcUniform(fc *vp9dec.FrameContext) {
	for i := range fc.SkipProbs {
		fc.SkipProbs[i] = 128
	}
	for i := range fc.IntraInterProb {
		fc.IntraInterProb[i] = 128
	}
	for i := range fc.InterModeProbs {
		for j := range fc.InterModeProbs[i] {
			fc.InterModeProbs[i][j] = 128
		}
	}
	for i := range fc.SwitchableInterpProb {
		for j := range fc.SwitchableInterpProb[i] {
			fc.SwitchableInterpProb[i][j] = 128
		}
	}
	for i := range fc.ReferenceModeProbs.SingleRefProb {
		fc.ReferenceModeProbs.SingleRefProb[i] = [2]uint8{128, 128}
	}
	for i := range fc.Nmvc.Joints {
		fc.Nmvc.Joints[i] = 128
	}
	for c := range 2 {
		cc := &fc.Nmvc.Comps[c]
		cc.Sign = 128
		for i := range cc.Classes {
			cc.Classes[i] = 128
		}
		for i := range cc.Class0 {
			cc.Class0[i] = 128
		}
		for i := range cc.Bits {
			cc.Bits[i] = 128
		}
		for i := range cc.Class0Fp {
			for j := range cc.Class0Fp[i] {
				cc.Class0Fp[i][j] = 128
			}
		}
		for i := range cc.Fp {
			cc.Fp[i] = 128
		}
		cc.Class0Hp = 128
		cc.Hp = 128
	}
}

// TestWriteInterBlockNewMvSwitchable: NEWMV on a single-ref inter
// block with switchable interp + non-zero MV. Round-trips through
// the per-block readers including ReadSwitchableInterpFilter +
// ReadMv.
func TestWriteInterBlockNewMvSwitchable(t *testing.T) {
	var seg vp9dec.SegmentationParams
	var fc vp9dec.FrameContext
	fillFcUniform(&fc)

	mi := &vp9dec.NeighborMi{
		SbType:       common.Block16x16,
		Mode:         common.NewMv,
		TxSize:       common.Tx4x4,
		RefFrame:     [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame},
		InterpFilter: 1, // pick a non-default switchable filter
	}
	bestRef := vp9dec.MV{Row: 0, Col: 0}
	wantMv := vp9dec.MV{Row: 16, Col: -32}

	buf := make([]byte, 256)
	var bw bitstream.Writer
	bw.Start(buf)
	WriteInterBlock(&bw, WriteInterBlockArgs{
		Seg:                 &seg,
		Mi:                  mi,
		Fc:                  &fc,
		TxMode:              common.Only4x4,
		FrameRefMode:        vp9dec.SingleReference,
		InterpFilter:        vp9dec.InterpSwitchable,
		InterModeCtx:        0,
		SwitchableInterpCtx: 0,
		AllowHP:             false,
		Mv:                  [2]vp9dec.MV{wantMv, {}},
		BestRefMv:           [2]vp9dec.MV{bestRef, {}},
	})
	size, _ := bw.Stop()

	var r bitstream.Reader
	r.Init(buf[:size])
	vp9dec.ReadSkipWithSeg(&r, &seg, 0, &fc, nil, nil)
	vp9dec.ReadIntraInterFlag(&r, &fc, nil, nil)
	var refOut [2]int8
	vp9dec.ReadRefFrames(&r, vp9dec.SingleReference,
		[vp9dec.MaxRefFrames]uint8{},
		vp9dec.CompoundFrameRefs{},
		&seg, 0, &fc, nil, nil, &refOut)
	mode := vp9dec.ReadInterMode(&r, fc.InterModeProbs[0])
	if mode != common.NewMv {
		t.Fatalf("inter mode = %d, want NewMv", mode)
	}
	filt := vp9dec.ReadSwitchableInterpFilter(&r, &fc, nil, nil)
	if int(filt) != int(mi.InterpFilter) {
		t.Errorf("filt = %d, want %d", filt, mi.InterpFilter)
	}
	var gotMv vp9dec.MV
	ref := bestRef
	vp9dec.ReadMv(&r, &gotMv, &ref, &fc.Nmvc, false)
	if gotMv.Row != wantMv.Row || gotMv.Col != wantMv.Col {
		t.Errorf("mv = %+v, want %+v", gotMv, wantMv)
	}
}

func TestWriteInterBlockCompoundNewMvWritesBothHalves(t *testing.T) {
	var seg vp9dec.SegmentationParams
	var fc vp9dec.FrameContext
	fillFcUniform(&fc)

	signBias := [vp9dec.MaxRefFrames]uint8{vp9dec.AltrefFrame: 1}
	refs := vp9dec.SetupCompoundReferenceMode(signBias)
	mi := &vp9dec.NeighborMi{
		SbType:       common.Block16x16,
		Mode:         common.NewMv,
		TxSize:       common.Tx4x4,
		RefFrame:     [2]int8{vp9dec.LastFrame, vp9dec.AltrefFrame},
		InterpFilter: uint8(vp9dec.InterpEighttapSharp),
	}
	bestRef := [2]vp9dec.MV{{Row: 4, Col: 2}, {Row: -3, Col: 6}}
	wantMv := [2]vp9dec.MV{{Row: 11, Col: -10}, {Row: -7, Col: 23}}

	buf := make([]byte, 256)
	var bw bitstream.Writer
	bw.Start(buf)
	WriteInterBlock(&bw, WriteInterBlockArgs{
		Seg:                 &seg,
		Mi:                  mi,
		Fc:                  &fc,
		TxMode:              common.Only4x4,
		FrameRefMode:        vp9dec.CompoundReference,
		InterpFilter:        vp9dec.InterpSwitchable,
		CompFixedRef:        refs.CompFixedRef,
		CompVarRef:          refs.CompVarRef,
		RefFrameSignBias:    signBias,
		InterModeCtx:        0,
		SwitchableInterpCtx: 0,
		IsCompound:          true,
		AllowHP:             true,
		Mv:                  wantMv,
		BestRefMv:           bestRef,
	})
	size, _ := bw.Stop()

	var r bitstream.Reader
	r.Init(buf[:size])
	vp9dec.ReadSkipWithSeg(&r, &seg, 0, &fc, nil, nil)
	vp9dec.ReadIntraInterFlag(&r, &fc, nil, nil)
	var refOut [2]int8
	vp9dec.ReadRefFrames(&r, vp9dec.CompoundReference, signBias, refs,
		&seg, 0, &fc, nil, nil, &refOut)
	if refOut != mi.RefFrame {
		t.Fatalf("ref frames = %v, want %v", refOut, mi.RefFrame)
	}
	mode := vp9dec.ReadInterMode(&r, fc.InterModeProbs[0])
	if mode != common.NewMv {
		t.Fatalf("inter mode = %d, want NewMv", mode)
	}
	filt := vp9dec.ReadSwitchableInterpFilter(&r, &fc, nil, nil)
	if filt != vp9dec.InterpEighttapSharp {
		t.Fatalf("interp filter = %d, want EighttapSharp", filt)
	}
	var got [2]vp9dec.MV
	for ref := range got {
		refMv := bestRef[ref]
		vp9dec.ReadMv(&r, &got[ref], &refMv, &fc.Nmvc, true)
	}
	if got != wantMv {
		t.Fatalf("compound MVs = %+v, want %+v", got, wantMv)
	}
}
