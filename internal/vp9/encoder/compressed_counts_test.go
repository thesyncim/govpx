package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
	"testing"
)

// TestWriteSkipProbsFromCountsRoundTrip: each of the 3 skip contexts
// runs the counts-driven cond-update; the decoder's
// ReadSkipProbs (per-slot VpxDiffUpdateProb) recovers the exact
// probability the savings_search settled on.
func TestWriteSkipProbsFromCountsRoundTrip(t *testing.T) {
	startProbs := [3]uint8{128, 128, 128}
	counts := [3][2]uint32{
		{900, 100}, // skewed: skip rare
		{100, 900}, // skewed: skip common
		{500, 500}, // balanced
	}

	buf := make([]byte, 32)
	var bw bitstream.Writer
	bw.Start(buf)
	writerProbs := startProbs
	WriteSkipProbsFromCounts(&bw, &writerProbs, &counts)
	size, err := bw.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	decodedProbs := startProbs
	for i := range decodedProbs {
		vp9dec.VpxDiffUpdateProb(&r, &decodedProbs[i])
	}
	if decodedProbs != writerProbs {
		t.Errorf("decoded = %v, writer = %v", decodedProbs, writerProbs)
	}
}

// TestWriteInterModeProbsFromCountsRoundTrip: 7 contexts × the
// 3-node InterModeTree. Different per-context distributions exercise
// every branch of the savings_search loop. Decoder side walks each
// branch slot via VpxDiffUpdateProb and arrives at the same probs.
func TestWriteInterModeProbsFromCountsRoundTrip(t *testing.T) {
	var probs [common.InterModeContexts][common.InterModes - 1]uint8
	var counts [common.InterModeContexts][common.InterModes]uint32
	for i := range probs {
		for j := range probs[i] {
			probs[i][j] = 128
		}
		// Plant a peak at a different mode per context to drive distinct
		// branch counts.
		counts[i][i%common.InterModes] = 500
		// Salt the rest so each context has a non-degenerate distribution.
		for k := range counts[i] {
			if k != i%common.InterModes {
				counts[i][k] = 20
			}
		}
	}
	scratch := make([][2]uint32, common.InterModes-1)

	writerProbs := probs
	buf := make([]byte, 128)
	var bw bitstream.Writer
	bw.Start(buf)
	WriteInterModeProbsFromCounts(&bw, &writerProbs, &counts, scratch)
	size, _ := bw.Stop()

	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	decodedProbs := probs
	for i := range decodedProbs {
		for j := range decodedProbs[i] {
			vp9dec.VpxDiffUpdateProb(&r, &decodedProbs[i][j])
		}
	}
	for i := range probs {
		if decodedProbs[i] != writerProbs[i] {
			t.Errorf("ctx %d: decoded=%v, writer=%v",
				i, decodedProbs[i], writerProbs[i])
		}
	}
}

// TestWritePartitionProbsFromCountsRoundTrip: 16 partition contexts
// × the 3-node PartitionTree. Each context gets a distinct event
// distribution so the savings_search picks different update
// patterns across slots; decoder reads them back via ReadPartitionProbs
// per-slot calls.
func TestWritePartitionProbsFromCountsRoundTrip(t *testing.T) {
	var probs [common.PartitionContexts][common.PartitionTypes - 1]uint8
	var counts [common.PartitionContexts][common.PartitionTypes]uint32
	for i := range probs {
		for j := range probs[i] {
			probs[i][j] = 128
		}
		// Different leaf gets the peak in each context.
		counts[i] = [4]uint32{10, 10, 10, 10}
		counts[i][i%4] = 500
	}
	scratch := make([][2]uint32, common.PartitionTypes-1)

	writerProbs := probs
	buf := make([]byte, 256)
	var bw bitstream.Writer
	bw.Start(buf)
	WritePartitionProbsFromCounts(&bw, &writerProbs, &counts, scratch)
	size, _ := bw.Stop()

	var r bitstream.Reader
	r.Init(buf[:size])
	decodedProbs := probs
	for i := range decodedProbs {
		for j := range decodedProbs[i] {
			vp9dec.VpxDiffUpdateProb(&r, &decodedProbs[i][j])
		}
	}
	for i := range probs {
		if decodedProbs[i] != writerProbs[i] {
			t.Errorf("ctx %d: decoded=%v, writer=%v",
				i, decodedProbs[i], writerProbs[i])
		}
	}
}

// seedFrameContext fills every probability slot in fc with the
// constant 128 so the test can detect post-update divergence
// cleanly. Real encoders seed from the libvpx default tables.
func seedFrameContext(fc *vp9dec.FrameContext) {
	for tx := range fc.CoefProbs {
		for p := range fc.CoefProbs[tx] {
			for r := range fc.CoefProbs[tx][p] {
				for b := range fc.CoefProbs[tx][p][r] {
					for c := range fc.CoefProbs[tx][p][r][b] {
						for n := range fc.CoefProbs[tx][p][r][b][c] {
							fc.CoefProbs[tx][p][r][b][c][n] = 128
						}
					}
				}
			}
		}
	}
	for i := range fc.TxProbs.P8x8 {
		fc.TxProbs.P8x8[i][0] = 128
		for j := range fc.TxProbs.P16x16[i] {
			fc.TxProbs.P16x16[i][j] = 128
		}
		for j := range fc.TxProbs.P32x32[i] {
			fc.TxProbs.P32x32[i][j] = 128
		}
	}
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
	for i := range fc.ReferenceModeProbs.CompInterProb {
		fc.ReferenceModeProbs.CompInterProb[i] = 128
	}
	for i := range fc.ReferenceModeProbs.SingleRefProb {
		fc.ReferenceModeProbs.SingleRefProb[i] = [2]uint8{128, 128}
	}
	for i := range fc.ReferenceModeProbs.CompRefProb {
		fc.ReferenceModeProbs.CompRefProb[i] = 128
	}
	for i := range fc.YModeProb {
		for j := range fc.YModeProb[i] {
			fc.YModeProb[i][j] = 128
		}
	}
	for i := range fc.UvModeProb {
		for j := range fc.UvModeProb[i] {
			fc.UvModeProb[i][j] = 128
		}
	}
	for i := range fc.PartitionProb {
		for j := range fc.PartitionProb[i] {
			fc.PartitionProb[i][j] = 128
		}
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

// TestWriteCompressedHeaderFromCountsInterFrameRoundTrip drives the
// full counts-driven compressed-header writer against an inter
// frame with allow_high_precision_mv, switchable interp, and
// REFERENCE_MODE_SELECT. The decoder's ReadCompressedHeader walks
// the same sections in the same order; every prob slot lands on
// the same value the encoder picked.
func TestWriteCompressedHeaderFromCountsInterFrameRoundTrip(t *testing.T) {
	var probs vp9dec.FrameContext
	seedFrameContext(&probs)

	counts := FrameCounts{}
	// Plant non-trivial bias in a few slots so the savings_search
	// picks updates somewhere in every section.
	counts.Skip[0] = [2]uint32{900, 100}
	counts.IntraInter[0] = [2]uint32{100, 900}
	counts.InterMode[0][0] = 500
	for i := 1; i < common.InterModes; i++ {
		counts.InterMode[0][i] = 20
	}
	counts.SwitchableInterp[0][0] = 400
	counts.SwitchableInterp[0][1] = 30
	counts.SwitchableInterp[0][2] = 30
	counts.ReferenceMode.CompInter[0] = [2]uint32{500, 100}
	counts.ReferenceMode.SingleRef[0][0] = [2]uint32{800, 50}
	counts.ReferenceMode.SingleRef[0][1] = [2]uint32{50, 800}
	counts.ReferenceMode.CompRef[0] = [2]uint32{600, 100}
	counts.YMode[0][0] = 400
	for i := 1; i < common.IntraModes; i++ {
		counts.YMode[0][i] = 20
	}
	counts.Partition[0][0] = 400
	counts.Partition[0][1] = 30
	counts.Partition[0][2] = 30
	counts.Partition[0][3] = 30
	counts.Mv.Joints[0] = 500
	counts.Mv.Joints[1] = 50
	counts.Mv.Joints[2] = 50
	counts.Mv.Joints[3] = 50
	for c := range counts.Mv.Comps {
		counts.Mv.Comps[c].Sign = [2]uint32{300, 50}
		counts.Mv.Comps[c].Classes[0] = 300
		for i := 1; i < len(counts.Mv.Comps[c].Classes); i++ {
			counts.Mv.Comps[c].Classes[i] = 10
		}
	}

	writerProbs := probs
	writerCounts := counts
	dst := make([]byte, 8192)
	n, err := WriteCompressedHeaderFromCounts(dst, WriteCompressedHeaderFromCountsArgs{
		Lossless:             false,
		TxMode:               common.Only4x4,
		IntraOnly:            false,
		InterpFilter:         vp9dec.InterpSwitchable,
		ReferenceMode:        vp9dec.ReferenceModeSelect,
		CompoundRefAllowed:   true,
		AllowHighPrecisionMv: true,
		CoefStepsize:         4,
		Probs:                &writerProbs,
		Counts:               &writerCounts,
	})
	if err != nil {
		t.Fatalf("WriteCompressedHeaderFromCounts: %v", err)
	}
	if n <= 0 {
		t.Fatalf("n = %d, want > 0", n)
	}

	// Decoder side mirrors the same walk.
	var r bitstream.Reader
	if err := r.Init(dst[:n]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	decProbs := probs
	out := vp9dec.ReadCompressedHeader(&r, &decProbs, vp9dec.ReadCompressedHeaderArgs{
		Lossless:             false,
		IntraOnly:            false,
		KeyFrame:             false,
		InterpFilter:         vp9dec.InterpSwitchable,
		AllowHighPrecisionMv: true,
		CompoundRefAllowed:   true,
	})
	if out.TxMode != common.Only4x4 {
		t.Errorf("TxMode = %d, want Only4x4", out.TxMode)
	}
	if out.ReferenceMode != vp9dec.ReferenceModeSelect {
		t.Errorf("ReferenceMode = %d, want ReferenceModeSelect", out.ReferenceMode)
	}
	if decProbs != writerProbs {
		t.Errorf("decoder side probs diverged from encoder side")
	}
}

// TestCompressedHeaderRoundTripKeyframeAllow32x32: emit the
// no-update compressed header for a TxMode=Allow32x32 keyframe and
// re-parse it through ReadCompressedHeader. The fc tables should
// stay byte-identical to the seed (DefaultIntraInter and friends
// are kept intact because every "update?" bit is 0).
func TestCompressedHeaderRoundTripKeyframeAllow32x32(t *testing.T) {
	dst := make([]byte, 4096)
	in := CompressedHeaderInputs{
		Lossless:      false,
		TxMode:        common.Allow32x32,
		IntraOnly:     true, // keyframe path → intra-only treated true.
		InterpFilter:  vp9dec.InterpEighttap,
		ReferenceMode: vp9dec.SingleReference,
	}
	n, err := WriteCompressedHeaderNoUpdate(dst, in)
	if err != nil {
		t.Fatalf("WriteCompressedHeaderNoUpdate: %v", err)
	}
	if n <= 0 {
		t.Fatalf("returned %d bytes", n)
	}

	var fc vp9dec.FrameContext
	// Seed FrameContext fields the parser walks.
	seedFC(&fc)
	pre := fc

	var br bitstream.Reader
	if err := br.Init(dst[:n]); err != nil {
		t.Fatalf("Reader.Init: %v", err)
	}
	out := vp9dec.ReadCompressedHeader(&br, &fc, vp9dec.ReadCompressedHeaderArgs{
		KeyFrame:     true,
		IntraOnly:    true,
		InterpFilter: vp9dec.InterpEighttap,
	})
	if out.TxMode != common.Allow32x32 {
		t.Errorf("TxMode = %d, want Allow32x32", out.TxMode)
	}
	if br.HasError() {
		t.Fatal("compressed header reader reported over-read")
	}
	// Every probability table should round-trip identical because
	// no "update?" bit was set.
	for ctx := range skipContexts {
		if fc.SkipProbs[ctx] != pre.SkipProbs[ctx] {
			t.Errorf("SkipProbs[%d] = %d, want %d (no-update)", ctx,
				fc.SkipProbs[ctx], pre.SkipProbs[ctx])
		}
	}
}

// TestCompressedHeaderRoundTripInterFrame exercises the inter-frame
// gates: comp_inter / single_ref / comp_ref blocks land based on the
// frame ReferenceMode, plus MV probs at the end.
func TestCompressedHeaderRoundTripInterFrame(t *testing.T) {
	dst := make([]byte, 4096)
	in := CompressedHeaderInputs{
		Lossless:             false,
		TxMode:               common.Allow16x16,
		IntraOnly:            false,
		InterpFilter:         vp9dec.InterpSwitchable,
		ReferenceMode:        vp9dec.ReferenceModeSelect,
		CompoundRefAllowed:   true,
		AllowHighPrecisionMv: true,
	}
	n, err := WriteCompressedHeaderNoUpdate(dst, in)
	if err != nil {
		t.Fatalf("WriteCompressedHeaderNoUpdate: %v", err)
	}

	var fc vp9dec.FrameContext
	seedFC(&fc)

	var br bitstream.Reader
	if err := br.Init(dst[:n]); err != nil {
		t.Fatalf("Reader.Init: %v", err)
	}
	out := vp9dec.ReadCompressedHeader(&br, &fc, vp9dec.ReadCompressedHeaderArgs{
		IntraOnly:            false,
		InterpFilter:         vp9dec.InterpSwitchable,
		AllowHighPrecisionMv: true,
		CompoundRefAllowed:   true,
	})
	if out.TxMode != common.Allow16x16 {
		t.Errorf("TxMode = %d, want Allow16x16", out.TxMode)
	}
	if out.ReferenceMode != vp9dec.ReferenceModeSelect {
		t.Errorf("ReferenceMode = %d, want ReferenceModeSelect", out.ReferenceMode)
	}
}

// seedFC seeds the FrameContext fields the parser consults with
// libvpx's default-tables seed, mirroring the past-independent
// state setup_past_independent_state would emit.
func seedFC(fc *vp9dec.FrameContext) {
	fc.SkipProbs = [3]uint8{192, 128, 64}
	for i := range fc.IntraInterProb {
		fc.IntraInterProb[i] = tables.DefaultIntraInter[i]
	}
	for i := range fc.InterModeProbs {
		for j := range fc.InterModeProbs[i] {
			fc.InterModeProbs[i][j] = tables.DefaultInterModeProbs[i][j]
		}
	}
	for i := range fc.SwitchableInterpProb {
		for j := range fc.SwitchableInterpProb[i] {
			fc.SwitchableInterpProb[i][j] = tables.DefaultSwitchableInterpProb[i][j]
		}
	}
	for i := range fc.PartitionProb {
		for j := range fc.PartitionProb[i] {
			fc.PartitionProb[i][j] = tables.DefaultPartitionProbs[i][j]
		}
	}
	for i := range fc.YModeProb {
		for j := range fc.YModeProb[i] {
			fc.YModeProb[i][j] = tables.DefaultIfYProbs[i][j]
		}
	}
	// Seed Nmvc joints + components from canonical defaults so the
	// MV-prob update walk has something to read against.
	copy(fc.Nmvc.Joints[:], tables.DefaultNmvJoints[:])
	for i := range 2 {
		src := &tables.DefaultNmvComps[i]
		dst := &fc.Nmvc.Comps[i]
		dst.Sign = src.Sign
		copy(dst.Classes[:], src.Classes[:])
		copy(dst.Class0[:], src.Class0[:])
		copy(dst.Bits[:], src.Bits[:])
		for j := range vp9dec.Class0Size {
			copy(dst.Class0Fp[j][:], src.Class0Fp[j][:])
		}
		copy(dst.Fp[:], src.Fp[:])
		dst.Class0Hp = src.Class0Hp
		dst.Hp = src.Hp
	}
}
