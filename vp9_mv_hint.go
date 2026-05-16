package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// vp9MVHintMap is a per-SB64 motion-vector hint slab. Each entry
// describes the representative LAST-ref MV chosen for one 64x64
// superblock during a prior encode pass (typically a lower-resolution
// layer in a multi-resolution simulcast pipeline).
//
// The slab is indexed [sbRow * sbCols + sbCol]; sbCols/sbRows are the
// SB64 dimensions of the encoder the slab is consumed by, not the
// encoder that produced it. The producer scales its MVs to the
// consumer's pixel domain before populating the slab; consumers walk
// the slab keyed by their own SB coordinate.
//
// Valid is a parallel bitmask: a zero entry means "no hint for this
// SB" and the consumer falls back to its native search. A populated
// entry adds the hint MV as one extra candidate to the consumer's
// motion search; the search still evaluates its own (0,0)-centered
// fan, so the hint can never make the encoder pick a worse MV than
// the local search would have found alone.
type vp9MVHintMap struct {
	sbCols int
	sbRows int
	mvs    []vp9dec.MV
	valid  []bool
}

// newVP9MVHintMap allocates an SB64 hint slab sized for an encoder
// whose visible dimensions round up to (sbCols, sbRows) at 64-pixel
// SB granularity. Returns nil if either dimension is non-positive.
func newVP9MVHintMap(width, height int) *vp9MVHintMap {
	if width <= 0 || height <= 0 {
		return nil
	}
	sbCols := (width + (1 << (common.MiBlockSizeLog2 + common.MiSizeLog2)) - 1) >>
		(common.MiBlockSizeLog2 + common.MiSizeLog2)
	sbRows := (height + (1 << (common.MiBlockSizeLog2 + common.MiSizeLog2)) - 1) >>
		(common.MiBlockSizeLog2 + common.MiSizeLog2)
	if sbCols <= 0 || sbRows <= 0 {
		return nil
	}
	return &vp9MVHintMap{
		sbCols: sbCols,
		sbRows: sbRows,
		mvs:    make([]vp9dec.MV, sbCols*sbRows),
		valid:  make([]bool, sbCols*sbRows),
	}
}

// at returns the hint MV for the SB containing the given mi-row/col,
// along with a "valid" flag. miRow/miCol are the consumer encoder's
// 8x8 mi coordinates; the lookup maps them to the SB64 grid.
func (m *vp9MVHintMap) at(miRow, miCol int) (vp9dec.MV, bool) {
	if m == nil || miRow < 0 || miCol < 0 {
		return vp9dec.MV{}, false
	}
	sbRow := miRow >> common.MiBlockSizeLog2
	sbCol := miCol >> common.MiBlockSizeLog2
	if sbRow >= m.sbRows || sbCol >= m.sbCols {
		return vp9dec.MV{}, false
	}
	idx := sbRow*m.sbCols + sbCol
	if !m.valid[idx] {
		return vp9dec.MV{}, false
	}
	return m.mvs[idx], true
}

// reset clears every entry to "no hint" without freeing the slab so
// the steady-state producer/consumer loop stays allocation-free.
func (m *vp9MVHintMap) reset() {
	if m == nil {
		return
	}
	for i := range m.valid {
		m.valid[i] = false
		m.mvs[i] = vp9dec.MV{}
	}
}

// exportVP9MVHints walks the last-encoded frame's miGrid and fills
// out with one representative LAST-ref MV per SB64. The
// representative is the MV of the SB's top-left 8x8 mi entry whose
// RefFrame[0] is LAST_FRAME; SBs that didn't pick LAST as a single
// reference contribute no hint and out's "valid" flag stays false.
//
// The output slab is sized to the encoder's own SB dimensions; the
// consumer caller is expected to scale the MVs through
// scaleVP9MVHintMap before installing them on a higher-resolution
// encoder.
func (e *VP9Encoder) exportVP9MVHints(out *vp9MVHintMap) {
	if e == nil || out == nil || len(e.miGrid) == 0 {
		return
	}
	miCols := e.vp9MiCols()
	miRows := e.vp9MiRows()
	if miCols <= 0 || miRows <= 0 {
		return
	}
	out.reset()
	for sbRow := 0; sbRow < out.sbRows; sbRow++ {
		for sbCol := 0; sbCol < out.sbCols; sbCol++ {
			miRow := sbRow << common.MiBlockSizeLog2
			miCol := sbCol << common.MiBlockSizeLog2
			if miRow >= miRows || miCol >= miCols {
				continue
			}
			mi := e.miGrid[miRow*miCols+miCol]
			if mi.RefFrame[0] != vp9dec.LastFrame ||
				mi.RefFrame[1] > vp9dec.IntraFrame {
				continue
			}
			idx := sbRow*out.sbCols + sbCol
			out.mvs[idx] = mi.Mv[0]
			out.valid[idx] = true
		}
	}
}

// scaleVP9MVHintMap copies src into dst with every MV scaled by the
// (numWidth/denWidth, numHeight/denHeight) ratio. The SB grids of
// src and dst can differ; entries in dst that don't have a
// corresponding src SB stay invalid. Caller owns dst's slab
// allocation.
//
// The typical pipeline: a 320x180 encoder produces a 5x3 SB64 grid;
// the next-higher 640x360 encoder consumes a 10x6 SB64 grid where
// each producer SB maps to a 2x2 block of consumer SBs sharing the
// scaled MV. scaleVP9MVHintMap explodes the src grid by the
// reciprocal of the downscale ratio (numWidth=640, denWidth=320, etc).
func scaleVP9MVHintMap(dst, src *vp9MVHintMap,
	numWidth, denWidth, numHeight, denHeight int,
) {
	if dst == nil || src == nil ||
		denWidth <= 0 || denHeight <= 0 ||
		numWidth <= 0 || numHeight <= 0 {
		return
	}
	dst.reset()
	// Map each dst SB to its src counterpart by dividing the dst SB's
	// pixel origin by the upscale ratio.
	const sbPixelLog2 = common.MiBlockSizeLog2 + common.MiSizeLog2
	for dstRow := 0; dstRow < dst.sbRows; dstRow++ {
		for dstCol := 0; dstCol < dst.sbCols; dstCol++ {
			dstPxX := dstCol << sbPixelLog2
			dstPxY := dstRow << sbPixelLog2
			srcPxX := dstPxX * denWidth / numWidth
			srcPxY := dstPxY * denHeight / numHeight
			srcCol := srcPxX >> sbPixelLog2
			srcRow := srcPxY >> sbPixelLog2
			if srcRow < 0 || srcCol < 0 ||
				srcRow >= src.sbRows || srcCol >= src.sbCols {
				continue
			}
			srcIdx := srcRow*src.sbCols + srcCol
			if !src.valid[srcIdx] {
				continue
			}
			srcMV := src.mvs[srcIdx]
			// Scale the MV by the (num/den) ratio. MV components are
			// in 1/8-pixel units (vp9dec.MV.Row/Col); a multiplicative
			// scale preserves the same units.
			scaledRow := int32(srcMV.Row) * int32(numHeight) / int32(denHeight)
			scaledCol := int32(srcMV.Col) * int32(numWidth) / int32(denWidth)
			if scaledRow > 32767 {
				scaledRow = 32767
			} else if scaledRow < -32768 {
				scaledRow = -32768
			}
			if scaledCol > 32767 {
				scaledCol = 32767
			} else if scaledCol < -32768 {
				scaledCol = -32768
			}
			dstIdx := dstRow*dst.sbCols + dstCol
			dst.mvs[dstIdx] = vp9dec.MV{
				Row: int16(scaledRow),
				Col: int16(scaledCol),
			}
			dst.valid[dstIdx] = true
		}
	}
}

// importVP9MVHints installs hints on the encoder. Subsequent
// pickVP9InterMvAllowZero calls consult the slab for an extra
// motion-search candidate. Passing nil clears any installed hints
// and restores the (0,0)-centered baseline search.
func (e *VP9Encoder) importVP9MVHints(hints *vp9MVHintMap) {
	if e == nil {
		return
	}
	e.mvHints = hints
}

// vp9MVHintCandidatePixelOffset converts an installed-hint MV at the
// given mi-row/col into a (dx, dy) integer-pixel candidate suitable
// for the (0,0)-centered integer search. Sub-pixel components are
// truncated; the search then refines the integer result through its
// subpel pass anyway.
//
// Returns (0, 0, false) when no hint is installed for this SB.
func (e *VP9Encoder) vp9MVHintCandidatePixelOffset(miRow, miCol int) (int, int, bool) {
	if e == nil || e.mvHints == nil {
		return 0, 0, false
	}
	hint, ok := e.mvHints.at(miRow, miCol)
	if !ok {
		return 0, 0, false
	}
	// MV.Row/Col are in 1/8-pixel units. The integer search operates
	// in whole pixels.
	dx := int(hint.Col) >> 3
	dy := int(hint.Row) >> 3
	return dx, dy, true
}
