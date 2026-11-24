package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// TestWriteModesBKeyframeNoResidue: keyframe leaf with all-zero
// coefficients. Mode info parses via ReadIntraFrameModeInfo and the
// residue walk emits per-plane EOB-at-0 fragments the decoder reads
// back as eob=0.
func TestWriteModesBKeyframeNoResidue(t *testing.T) {
	var seg vp9dec.SegmentationParams
	maps := vp9dec.IntraSegmentMaps{
		CurrentFrameSegMap: make([]uint8, 16),
		MiCols:             4,
	}
	fc := seedDefaultCoefProbsForEnc()
	var fcCtx vp9dec.FrameContext
	fcCtx.SkipProbs[0] = 128
	fcCtx.CoefProbs = fc

	mi := &vp9dec.NeighborMi{
		SbType: common.Block8x8,
		Mode:   common.DcPred,
		TxSize: common.Tx4x4,
	}
	uvMode := common.VPred

	var planes [vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane
	vp9dec.SetupBlockPlanes(&planes, 1, 1)
	planes[0].AboveContext = make([]uint8, 4)
	planes[0].LeftContext = make([]uint8, 4)
	planes[1].AboveContext = make([]uint8, 2)
	planes[1].LeftContext = make([]uint8, 2)
	planes[2].AboveContext = make([]uint8, 2)
	planes[2].LeftContext = make([]uint8, 2)

	zeroCoeffs := make([]int16, 16)
	coefArgs := WriteCoefSbArgs{
		BSize:    common.Block8x8,
		MiTxSize: common.Tx4x4,
		Planes:   &planes,
		PlaneDequant: [vp9dec.MaxMbPlane][2]int16{
			{16, 16}, {16, 16}, {16, 16},
		},
		Fc:        &fc,
		GetCoeffs: func(plane, r, c int, tx common.TxSize) []int16 { return zeroCoeffs },
	}
	kfArgs := WriteKeyframeBlockArgs{
		Seg:       &seg,
		Mi:        mi,
		TxMode:    common.Only4x4,
		SkipProbs: fcCtx.SkipProbs,
	}

	buf := make([]byte, 256)
	var bw bitstream.Writer
	bw.Start(buf)
	if err := WriteModesBKeyframe(&bw, kfArgs, uvMode, coefArgs); err != nil {
		t.Fatalf("WriteModesBKeyframe: %v", err)
	}
	size, _ := bw.Stop()

	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	got := &vp9dec.NeighborMi{SbType: common.Block8x8}
	out := vp9dec.ReadIntraFrameModeInfo(vp9dec.IntraFrameDriverArgs{
		Reader:   &r,
		Fc:       &fcCtx,
		Seg:      &seg,
		Maps:     &maps,
		TxMode:   common.Only4x4,
		MiOffset: 0,
		XMis:     1, YMis: 1,
	}, got)
	if got.Mode != mi.Mode {
		t.Errorf("Y mode = %d, want %d", got.Mode, mi.Mode)
	}
	if out.UvMode != uvMode {
		t.Errorf("UV mode = %d, want %d", out.UvMode, uvMode)
	}

	// Reset entropy contexts; walk the residue side with all-zero
	// blocks should each read eob=0.
	for i := range planes[0].AboveContext {
		planes[0].AboveContext[i] = 0
		planes[0].LeftContext[i] = 0
	}
	for i := range planes[1].AboveContext {
		planes[1].AboveContext[i] = 0
		planes[1].LeftContext[i] = 0
		planes[2].AboveContext[i] = 0
		planes[2].LeftContext[i] = 0
	}
	dqcoeff := make([]int16, 16)
	for plane := range vp9dec.MaxMbPlane {
		pd := &planes[plane]
		var txSize common.TxSize
		if plane == 0 {
			txSize = common.Tx4x4
		} else {
			txSize = vp9dec.GetUvTxSize(common.Block8x8, common.Tx4x4, pd)
		}
		pbsize := vp9dec.GetPlaneBlockSize(common.Block8x8, pd)
		n4w := int(common.Num4x4BlocksWideLookup[pbsize])
		n4h := int(common.Num4x4BlocksHighLookup[pbsize])
		step := 1 << uint(txSize)
		scan, neighbors := scanForTxSize(txSize)
		planeType := 0
		if plane > 0 {
			planeType = 1
		}
		for rr := 0; rr < n4h; rr += step {
			for cc := 0; cc < n4w; cc += step {
				ec := vp9dec.GetEntropyContext(txSize,
					pd.AboveContext[cc:cc+step],
					pd.LeftContext[rr:rr+step])
				for i := range dqcoeff {
					dqcoeff[i] = 0
				}
				eob := vp9dec.DecodeCoefs(&r, txSize, planeType, 0,
					[2]int16{16, 16}, ec, scan, neighbors, &fc, dqcoeff)
				if eob != 0 {
					t.Errorf("plane=%d (%d,%d) eob=%d, want 0", plane, rr, cc, eob)
				}
			}
		}
	}
}

// TestWriteModesBKeyframeSkipShortcircuit: skip=1 blocks emit only
// mode-info (no residue). Verifies the dispatcher omits the
// WriteCoefSb call entirely so the wire fragment matches libvpx's
// skip path.
func TestWriteModesBKeyframeSkipShortcircuit(t *testing.T) {
	var seg vp9dec.SegmentationParams
	var fcCtx vp9dec.FrameContext
	fcCtx.SkipProbs[0] = 128
	fc := seedDefaultCoefProbsForEnc()
	fcCtx.CoefProbs = fc

	mi := &vp9dec.NeighborMi{
		SbType: common.Block8x8,
		Mode:   common.DcPred,
		TxSize: common.Tx4x4,
		Skip:   1,
	}

	// Plant a non-zero coefficient in the getCoeffs closure that should
	// NOT be emitted because skip=1 short-circuits the walker.
	dq := int16(16)
	loud := make([]int16, 16)
	loud[0] = dq * 3
	coefArgs := WriteCoefSbArgs{
		BSize:        common.Block8x8,
		MiTxSize:     common.Tx4x4,
		PlaneDequant: [vp9dec.MaxMbPlane][2]int16{{dq, dq}, {dq, dq}, {dq, dq}},
		Fc:           &fc,
		// Even though Planes is nil the walker should never be invoked,
		// so a nil pointer here proves the short-circuit happened.
		Planes:    nil,
		GetCoeffs: func(plane, r, c int, tx common.TxSize) []int16 { return loud },
	}
	kfArgs := WriteKeyframeBlockArgs{
		Seg:       &seg,
		Mi:        mi,
		TxMode:    common.Only4x4,
		SkipProbs: fcCtx.SkipProbs,
	}

	buf := make([]byte, 256)
	var bw bitstream.Writer
	bw.Start(buf)
	if err := WriteModesBKeyframe(&bw, kfArgs, common.DcPred, coefArgs); err != nil {
		t.Fatalf("WriteModesBKeyframe: %v", err)
	}
	if _, err := bw.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

// TestWriteModesBInterSkipShortcircuit: skip=1 inter leaf emits
// mode-info only — same shape as the keyframe skip path.
func TestWriteModesBInterSkipShortcircuit(t *testing.T) {
	var seg vp9dec.SegmentationParams
	var fcCtx vp9dec.FrameContext
	fillFcUniform(&fcCtx)
	fc := seedDefaultCoefProbsForEnc()
	fcCtx.CoefProbs = fc

	mi := &vp9dec.NeighborMi{
		SbType:   common.Block8x8,
		Mode:     common.ZeroMv,
		TxSize:   common.Tx4x4,
		Skip:     1,
		RefFrame: [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame},
	}
	interArgs := WriteInterBlockArgs{
		Seg:          &seg,
		Mi:           mi,
		Fc:           &fcCtx,
		TxMode:       common.Only4x4,
		FrameRefMode: vp9dec.SingleReference,
		InterpFilter: vp9dec.InterpEighttap,
	}
	dq := int16(16)
	loud := make([]int16, 16)
	loud[0] = dq * 3
	coefArgs := WriteCoefSbArgs{
		BSize:        common.Block8x8,
		MiTxSize:     common.Tx4x4,
		PlaneDequant: [vp9dec.MaxMbPlane][2]int16{{dq, dq}, {dq, dq}, {dq, dq}},
		Fc:           &fc,
		Planes:       nil, // unreachable if short-circuit holds
		GetCoeffs:    func(plane, r, c int, tx common.TxSize) []int16 { return loud },
	}

	buf := make([]byte, 256)
	var bw bitstream.Writer
	bw.Start(buf)
	if err := WriteModesBInter(&bw, interArgs, coefArgs); err != nil {
		t.Fatalf("WriteModesBInter: %v", err)
	}
	if _, err := bw.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}
