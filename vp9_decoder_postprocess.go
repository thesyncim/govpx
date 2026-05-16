package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// vp9MFQEWalker implements libvpx's vp9_mfqe semantics on top of the
// shared VP8/VP9 postprocess kernel. The shared kernel walks 16x16
// macroblocks; VP9 splits the frame into 64x64 superblocks whose
// internal partition tree (Block8x8..Block64x64) the encoder picked
// per MI. Walking by SbType lets MFQE blend a 64x64 stationary
// background as a single block (matching libvpx) instead of stitching
// 16 independent decisions, and lets it skip a single 64x64 SB whose
// SbType says "high motion".
//
// The walker is wired into vp8dec.PostProcessOptions.MFQEOverride and
// fires only when shouldApplyMFQE has already passed.
func (d *VP9Decoder) vp9MFQEWalker(src *vp8common.Image, dst *vp8common.Image,
	keyFrame bool, qcurr int, qprev int,
) {
	miCols := (d.lastFrame.Width + 7) >> 3
	miRows := (d.lastFrame.Height + 7) >> 3
	if miCols <= 0 || miRows <= 0 || len(d.miGrid) < miRows*miCols {
		// Fall back to a straight previous-frame copy — the
		// post-MFQE deblock pass that runs on top will perturb the
		// pixels into a reasonable output even without partition
		// awareness.
		copy(dst.Y, src.Y)
		copy(dst.U, src.U)
		copy(dst.V, src.V)
		return
	}
	// Seed dst with src so any partial-block fragment the walker
	// skips at the right/bottom edges keeps the previous-frame
	// pixels instead of leaving dst stale. The walker writes over
	// every MI it visits; this pre-seed only matters for the
	// edge-clipped 8x8 fragments.
	copy(dst.Y, src.Y)
	copy(dst.U, src.U)
	copy(dst.V, src.V)

	// Walk the MI grid in 8x8 units. Every MI within a VP9 partition
	// leaf carries the same SbType; we dispatch the kernel only at
	// the leaf's top-left (i.e. when (miRow, miCol) is aligned to
	// the leaf's step) so larger leaves blend as one block instead
	// of being re-touched once per 8x8 MI.
	for miRow := 0; miRow < miRows; miRow++ {
		for miCol := 0; miCol < miCols; miCol++ {
			idx := miRow*miCols + miCol
			mi := &d.miGrid[idx]
			step := vp9MFQEStepFromSbType(mi.SbType)
			if step == 0 {
				step = 1
			}
			// Only dispatch at the leaf's top-left corner — the
			// MI grid stamps the same SbType across all MIs the
			// leaf covers, so this both deduplicates work and
			// keeps the MFQE kernel running on power-of-two
			// square blocks.
			if step > 1 && (miRow&(step-1)) != 0 {
				continue
			}
			if step > 1 && (miCol&(step-1)) != 0 {
				continue
			}
			d.vp9MFQEProcessBlock(src, dst, keyFrame, qcurr, qprev,
				miRow, miCol, step, miRows, miCols, mi)
		}
	}
}

// vp9MFQEProcessBlock processes a step*8 x step*8 block rooted at
// (miRow, miCol). When the block runs off the visible region the
// walker splits into four step/2 quadrants until either the block
// fits or step shrinks to 1 (8x8) — at which point any non-fitting
// fragment is dropped to keep MFQE from reading past the visible
// frame.
func (d *VP9Decoder) vp9MFQEProcessBlock(src *vp8common.Image, dst *vp8common.Image,
	keyFrame bool, qcurr int, qprev int,
	miRow int, miCol int, step int, miRows int, miCols int, mi *vp9dec.NeighborMi,
) {
	if miRow >= miRows || miCol >= miCols || step <= 0 {
		return
	}
	if miRow+step > miRows || miCol+step > miCols {
		if step <= 1 {
			// 8x8 partial block — skip to avoid writing past the
			// visible region. The deblock pass that follows MFQE
			// re-touches these pixels anyway.
			return
		}
		half := step >> 1
		d.vp9MFQEProcessBlock(src, dst, keyFrame, qcurr, qprev,
			miRow, miCol, half, miRows, miCols, mi)
		d.vp9MFQEProcessBlock(src, dst, keyFrame, qcurr, qprev,
			miRow, miCol+half, half, miRows, miCols, mi)
		d.vp9MFQEProcessBlock(src, dst, keyFrame, qcurr, qprev,
			miRow+half, miCol, half, miRows, miCols, mi)
		d.vp9MFQEProcessBlock(src, dst, keyFrame, qcurr, qprev,
			miRow+half, miCol+half, half, miRows, miCols, mi)
		return
	}

	blockSize := step * 8 // 8 / 16 / 32 / 64
	yPx := miCol * 8
	xPx := miRow * 8
	yOff := xPx*src.YStride + yPx
	uOff := (xPx>>1)*src.UStride + (yPx >> 1)
	vOff := (xPx>>1)*src.VStride + (yPx >> 1)
	ydOff := xPx*dst.YStride + yPx
	udOff := (xPx>>1)*dst.UStride + (yPx >> 1)
	vdOff := (xPx>>1)*dst.VStride + (yPx >> 1)

	// Decide whether this SB-level partition qualifies for MFQE
	// blending. Keyframes always qualify (libvpx forces totmap=4 in
	// the VP8 walker). Inter blocks need:
	//   - mi.Skip != 0, OR
	//   - intra (RefFrame[0] <= IntraFrame), OR
	//   - small MV (|row| <= 16 and |col| <= 16 in 1/8-pel units, ~2 pels).
	// libvpx mfqe.c uses an analogous test (mi->mode > NEARESTMV
	// disables MFQE; small MVs pass).
	qualifies := keyFrame || vp9MFQEQualifiesBlock(mi)
	if !qualifies {
		vp8dec.CopyMFQEBlock(blockSize,
			src.Y[yOff:], src.U[uOff:], src.V[vOff:],
			src.YStride, src.UStride,
			dst.Y[ydOff:], dst.U[udOff:], dst.V[vdOff:],
			dst.YStride, dst.UStride)
		return
	}

	vp8dec.MultiframeQualityEnhanceBlock(blockSize, qcurr, qprev,
		src.Y[yOff:], src.U[uOff:], src.V[vOff:],
		src.YStride, src.UStride,
		dst.Y[ydOff:], dst.U[udOff:], dst.V[vdOff:],
		dst.YStride, dst.UStride)
}

// vp9MFQEStepFromSbType returns the side length (in 8x8 MIs) of the
// largest square that fits inside the SbType's bounding box.
// Non-square partitions (Block16x8 etc.) are downgraded to their
// narrower side so MFQE sees power-of-two squares.
//
// Mapping (in 8x8 MIs):
//   Block8x8/8x4/4x8/4x4   -> 1
//   Block16x16/16x8/8x16   -> 2
//   Block32x32/32x16/16x32 -> 4
//   Block64x64/64x32/32x64 -> 8
func vp9MFQEStepFromSbType(sb common.BlockSize) int {
	if sb >= common.BlockSizes {
		return 1
	}
	w := int(common.Num8x8BlocksWideLookup[sb])
	h := int(common.Num8x8BlocksHighLookup[sb])
	if w < h {
		return w
	}
	return h
}

// vp9MFQEQualifiesBlock returns true when the MI's mode / MV / skip
// state matches libvpx's mfqe_decision precondition for blending the
// previous frame in. Intra blocks and skipped blocks always qualify;
// inter blocks need their motion vector below the 2-pel threshold
// used by VP8's qualifyInterMFQEMacroblock.
func vp9MFQEQualifiesBlock(mi *vp9dec.NeighborMi) bool {
	if mi.Skip != 0 {
		return true
	}
	if mi.RefFrame[0] <= vp9dec.IntraFrame {
		return true
	}
	mv := mi.Mv[0]
	// VP9 MVs are in 1/8-pel units. 2 pels == 16. Stay conservative
	// (small motion only, matches libvpx mfqe.c threshold).
	if abs16(mv.Row) > 16 || abs16(mv.Col) > 16 {
		return false
	}
	return true
}

func abs16(v int16) int {
	if v < 0 {
		return -int(v)
	}
	return int(v)
}
