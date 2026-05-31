//go:build govpx_oracle_trace

package govpx

import (
	"fmt"
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// TestVP9OracleCyclicRefreshCompressedHeaderContextDiff logs which
// probability tables diverge on the panning cyclic-refresh inter frame.
func TestVP9OracleCyclicRefreshCompressedHeaderContextDiff(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 cyclic refresh compressed header diff")
	vp9test.RequireVpxencFrameFlags(t)

	const width, height, frames = 64, 64, 6
	sources := make([]*image.YCbCr, frames)
	for i := range sources {
		sources[i] = vp9test.NewPanningYCbCr(width, height, i)
	}
	opts := vp9OracleCyclicRefreshCBROptions(width, height, 700)
	extraArgs := vp9OracleCyclicRefreshCBRArgs(700, 600, 400, 500, 0)

	got := encodeVP9FramesWithGovpx(t, opts, sources, nil)
	want := vp9test.VpxencFrameFlagPackets(t, sources, vp9LibvpxFrameFlags(nil), extraArgs...)

	const frame = 2
	gKey, _ := readVP9OracleKeyHeaderWithLen(t, "govpx", got[0], width, height)
	lKey, _ := readVP9OracleKeyHeaderWithLen(t, "libvpx", want[0], width, height)
	gHdr := readVP9OraclePacketHeader(t, "govpx", frame, got[frame], &gKey, width, height)
	lHdr := readVP9OraclePacketHeader(t, "libvpx", frame, want[frame], &lKey, width, height)

	gComp, gFC, gUnc := vp9test.ReadCompressedHeader(t, got[frame], gHdr)
	lComp, lFC, lUnc := vp9test.ReadCompressedHeader(t, want[frame], lHdr)

	t.Logf("unc=%d/%d first_part=%d/%d tx_mode govpx=%d libvpx=%d ref_mode govpx=%d libvpx=%d",
		gUnc, lUnc, gHdr.FirstPartitionSize, lHdr.FirstPartitionSize,
		gComp.TxMode, lComp.TxMode, gComp.ReferenceMode, lComp.ReferenceMode)

	diffUint8Slices(t, "skip_probs", gFC.SkipProbs[:], lFC.SkipProbs[:])
	diffUint8Slices(t, "intra_inter", gFC.IntraInterProb[:], lFC.IntraInterProb[:])
	for ctx := range gFC.InterModeProbs {
		diffUint8Slices(t, fmt.Sprintf("inter_mode[%d]", ctx),
			flatten2D(gFC.InterModeProbs[ctx][:]),
			flatten2D(lFC.InterModeProbs[ctx][:]))
	}
	for ctx := range gFC.PartitionProb {
		diffUint8Slices(t, fmt.Sprintf("partition[%d]", ctx),
			flatten2D(gFC.PartitionProb[ctx][:]),
			flatten2D(lFC.PartitionProb[ctx][:]))
	}
	diffCoefProbs(t, &gFC.CoefProbs, &lFC.CoefProbs)
}

func flatten2D(s []uint8) []uint8 { return append([]uint8(nil), s...) }

func diffUint8Slices(t *testing.T, label string, a, b []uint8) {
	t.Helper()
	if len(a) != len(b) {
		t.Logf("%s: len %d vs %d", label, len(a), len(b))
		return
	}
	n := 0
	for i := range a {
		if a[i] != b[i] {
			n++
			if n <= 4 {
				t.Logf("%s[%d]: govpx=%d libvpx=%d", label, i, a[i], b[i])
			}
		}
	}
	if n > 0 {
		t.Logf("%s: %d/%d slots differ", label, n, len(a))
	}
}

func diffCoefProbs(t *testing.T, a, b *vp9dec.FrameCoefProbs) {
	t.Helper()
	if a == nil || b == nil {
		return
	}
	// Log only planes/refs/bands with any differing leaf prob.
	diff := 0
	for p := range a {
		for r := range a[p] {
			for bnd := range a[p][r] {
				for ctx := range a[p][r][bnd] {
					for node := range a[p][r][bnd][ctx] {
						for leaf := range a[p][r][bnd][ctx][node] {
							if a[p][r][bnd][ctx][node][leaf] != b[p][r][bnd][ctx][node][leaf] {
								diff++
								if diff <= 6 {
									t.Logf("coef[%d,%d,%d,%d,%d,%d]: %d vs %d",
										p, r, bnd, ctx, node, leaf,
										a[p][r][bnd][ctx][node][leaf],
										b[p][r][bnd][ctx][node][leaf])
								}
							}
						}
					}
				}
			}
		}
	}
	if diff > 0 {
		t.Logf("coef_probs: %d leaf slots differ after decode", diff)
	}
}

// TestVP9OracleCyclicRefreshPanningInterMiGridDiff logs how many decoded
// MI fields diverge on panning cyclic-refresh inter frames.
func TestVP9OracleCyclicRefreshPanningInterMiGridDiff(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 cyclic refresh panning MI grid diff")
	vp9test.RequireVpxencFrameFlags(t)

	const width, height, frames = 64, 64, 10
	sources := make([]*image.YCbCr, frames)
	for i := range sources {
		sources[i] = vp9test.NewPanningYCbCr(width, height, i)
	}
	opts := vp9OracleCyclicRefreshCBROptions(width, height, 700)
	extraArgs := vp9OracleCyclicRefreshCBRArgs(700, 600, 400, 500, 0)

	got := encodeVP9FramesWithGovpx(t, opts, sources, nil)
	want := vp9test.VpxencFrameFlagPackets(t, sources, vp9LibvpxFrameFlags(nil), extraArgs...)
	gotGrids := decodeVP9SequenceMiGridsForOracleTest(t, got)
	wantGrids := decodeVP9SequenceMiGridsForOracleTest(t, want)

	for frame := 1; frame < frames; frame++ {
		gGrid := gotGrids[frame]
		lGrid := wantGrids[frame]
		if len(gGrid) != len(lGrid) {
			t.Fatalf("frame %d mi grid len %d vs %d", frame, len(gGrid), len(lGrid))
		}
		var segHistG, segHistL [8]int
		var refHistG, refHistL [4]int
		segMismatch, skipMismatch, modeMismatch := 0, 0, 0
		refMismatch, txMismatch, bsizeMismatch := 0, 0, 0
		for i := range gGrid {
			if gGrid[i].SbType != lGrid[i].SbType {
				bsizeMismatch++
				if bsizeMismatch <= 4 {
					t.Logf("frame %d mi[%d] sbtype=%d/%d mode=%d/%d ref=%v/%v",
						frame, i, gGrid[i].SbType, lGrid[i].SbType,
						gGrid[i].Mode, lGrid[i].Mode,
						gGrid[i].RefFrame, lGrid[i].RefFrame)
				}
			}
			segHistG[gGrid[i].SegmentID]++
			segHistL[lGrid[i].SegmentID]++
			if gGrid[i].RefFrame[0] >= 0 && int(gGrid[i].RefFrame[0]) < len(refHistG) {
				refHistG[gGrid[i].RefFrame[0]]++
			}
			if lGrid[i].RefFrame[0] >= 0 && int(lGrid[i].RefFrame[0]) < len(refHistL) {
				refHistL[lGrid[i].RefFrame[0]]++
			}
			if gGrid[i].SegmentID != lGrid[i].SegmentID {
				segMismatch++
				if segMismatch <= 3 {
					t.Logf("frame %d mi[%d] seg=%d/%d mode=%d/%d skip=%d/%d",
						frame, i, gGrid[i].SegmentID, lGrid[i].SegmentID,
						gGrid[i].Mode, lGrid[i].Mode, gGrid[i].Skip, lGrid[i].Skip)
				}
			}
			if gGrid[i].Skip != lGrid[i].Skip {
				skipMismatch++
			}
			if gGrid[i].Mode != lGrid[i].Mode {
				modeMismatch++
				if modeMismatch <= 4 {
					t.Logf("frame %d mi[%d] mode=%d/%d ref=%v/%v mv=%v/%v",
						frame, i, gGrid[i].Mode, lGrid[i].Mode,
						gGrid[i].RefFrame, lGrid[i].RefFrame,
						gGrid[i].Mv, lGrid[i].Mv)
				}
			}
			if gGrid[i].RefFrame != lGrid[i].RefFrame {
				refMismatch++
				if refMismatch <= 4 {
					t.Logf("frame %d mi[%d] ref=%v/%v mode=%d/%d mv=%v/%v",
						frame, i, gGrid[i].RefFrame, lGrid[i].RefFrame,
						gGrid[i].Mode, lGrid[i].Mode,
						gGrid[i].Mv, lGrid[i].Mv)
				}
			}
			if gGrid[i].TxSize != lGrid[i].TxSize {
				txMismatch++
			}
		}
		t.Logf("panning inter frame %d: seg_mismatch=%d/%d skip_mismatch=%d mode_mismatch=%d ref_mismatch=%d tx_mismatch=%d bsize_mismatch=%d seg_hist_govpx=%v libvpx=%v ref_hist_govpx=%v libvpx=%v",
			frame, segMismatch, len(gGrid), skipMismatch, modeMismatch, refMismatch, txMismatch, bsizeMismatch, segHistG, segHistL, refHistG, refHistL)
	}
}

func TestVP9OracleCyclicRefreshPanningRCGoldenCounter(t *testing.T) {
	const width, height, frames = 64, 64, 3
	enc, err := NewVP9Encoder(vp9OracleCyclicRefreshCBROptions(width, height, 700))
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	t.Cleanup(func() { _ = enc.Close() })
	var keyHeader vp9dec.UncompressedHeader
	for i := 0; i < frames; i++ {
		pkt, err := enc.Encode(vp9test.NewPanningYCbCr(width, height, i))
		if err != nil {
			t.Fatalf("Encode frame %d: %v", i, err)
		}
		if i == 0 {
			keyHeader, _ = vp9test.ParseHeader(t, pkt)
		}
		var br vp9dec.BitReader
		br.Init(pkt)
		hdr, err := vp9dec.ReadUncompressedHeader(&br, &keyHeader,
			func(uint8) (uint32, uint32) { return width, height })
		if err != nil {
			t.Fatalf("ReadUncompressedHeader frame %d: %v", i, err)
		}
		t.Logf("frame %d: refresh=0x%x show=%t key=%t intra_only=%t framesSinceGolden=%d framesSinceKey=%d",
			i, hdr.RefreshFrameFlags, hdr.ShowFrame, hdr.FrameType == 0, hdr.IntraOnly,
			enc.rc.framesSinceGolden, enc.rc.framesSinceKey)
		t.Logf("frame %d refs valid: last=%t golden=%t alt=%t",
			i, enc.refFrames[vp9LastRefSlot].valid,
			enc.refFrames[vp9GoldenRefSlot].valid,
			enc.refFrames[vp9AltRefSlot].valid)
		t.Logf("frame %d sf: use_nonrd=%d ref_masking=%d short_circuit_ltv=%d",
			i, enc.sf.UseNonrdPickMode, enc.sf.ReferenceMasking, enc.sf.ShortCircuitLowTempVar)
	}
}
