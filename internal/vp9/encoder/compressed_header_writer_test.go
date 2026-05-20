package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

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
