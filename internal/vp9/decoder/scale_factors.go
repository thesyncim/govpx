package decoder

// VP9 inter-reference scale-factor math. Ported from libvpx v1.16.0
// vp9/common/vp9_scale.{h,c} — vp9_setup_scale_factors_for_frame,
// vp9_scale_mv, and the supporting validity / is-scaled predicates.
//
// VP9 lets a frame consult a reference frame at a different
// resolution (within the libvpx-defined 2x-or-1/16x bounds). The
// scale factor projects MVs at the current frame's scale onto the
// reference frame's pixel grid using a Q14 fixed-point ratio.

const (
	// RefScaleShift mirrors REF_SCALE_SHIFT.
	RefScaleShift = 14
	// RefNoScale is the canonical 1x ratio (no scale change).
	RefNoScale = 1 << RefScaleShift
	// RefInvalidScale flags a ref frame whose dimensions land
	// outside libvpx's accepted range; predicted blocks from such a
	// reference are corrupt.
	RefInvalidScale = -1
)

// ScaleFactors mirrors the parser-visible subset of libvpx's
// struct scale_factors. The C struct also carries a convolve-fn
// dispatch table; we keep it out of this struct because the Go port
// drives the dispatch from PredictTable in convolve_dispatch.go
// (the non-scaled path) — scaled paths require the vpx_scaled_*
// DSP kernels which land separately.
type ScaleFactors struct {
	XScaleFp int32
	YScaleFp int32
	XStepQ4  int
	YStepQ4  int
}

// MV32 mirrors libvpx's MV32 — int32 row/col pair the scaled MV
// helper produces (the post-scale projection can exceed int16).
type MV32 struct {
	Row int32
	Col int32
}

// SetupScaleFactorsForFrame mirrors libvpx's
// vp9_setup_scale_factors_for_frame. Builds the per-reference scale
// factor from (otherW, otherH) → (thisW, thisH). Invalid dimension
// pairs (outside the 2x-or-1/16x range) land with REF_INVALID_SCALE
// in both axes, signalling the decoder to mark the block corrupt.
func SetupScaleFactorsForFrame(sf *ScaleFactors, otherW, otherH, thisW, thisH int) {
	if !validRefFrameSize(otherW, otherH, thisW, thisH) {
		sf.XScaleFp = RefInvalidScale
		sf.YScaleFp = RefInvalidScale
		return
	}
	sf.XScaleFp = int32(getFixedPointScale(otherW, thisW))
	sf.YScaleFp = int32(getFixedPointScale(otherH, thisH))
	sf.XStepQ4 = scaledXInt(16, sf)
	sf.YStepQ4 = scaledYInt(16, sf)
}

// ScaleMv mirrors libvpx's vp9_scale_mv. Projects an MV at the
// current frame's grid onto the reference frame's grid at the given
// (x, y) integer block position, returning a Q4 MV32.
func ScaleMv(mv MV, x, y int, sf *ScaleFactors) MV32 {
	xOffQ4 := scaledXInt(x<<SubpelBitsConst, sf) & (SubpelShifts - 1)
	yOffQ4 := scaledYInt(y<<SubpelBitsConst, sf) & (SubpelShifts - 1)
	return MV32{
		Row: int32(scaledYInt(int(mv.Row), sf) + yOffQ4),
		Col: int32(scaledXInt(int(mv.Col), sf) + xOffQ4),
	}
}

// IsValidScale mirrors vp9_is_valid_scale.
func (sf *ScaleFactors) IsValidScale() bool {
	return sf.XScaleFp != RefInvalidScale && sf.YScaleFp != RefInvalidScale
}

// IsScaled mirrors vp9_is_scaled.
func (sf *ScaleFactors) IsScaled() bool {
	return sf.IsValidScale() &&
		(sf.XScaleFp != RefNoScale || sf.YScaleFp != RefNoScale)
}

// ScaleValueX / ScaleValueY mirror libvpx's scale_value_{x,y}
// function-pointer dispatch — under no scaling they pass `val`
// through, otherwise they apply the per-axis Q14 scale.
func (sf *ScaleFactors) ScaleValueX(val int) int {
	if sf.XScaleFp == RefNoScale {
		return val
	}
	return scaledXInt(val, sf)
}

// ScaleValueY companion to ScaleValueX.
func (sf *ScaleFactors) ScaleValueY(val int) int {
	if sf.YScaleFp == RefNoScale {
		return val
	}
	return scaledYInt(val, sf)
}

func scaledXInt(val int, sf *ScaleFactors) int {
	return int((int64(val) * int64(sf.XScaleFp)) >> RefScaleShift)
}

func scaledYInt(val int, sf *ScaleFactors) int {
	return int((int64(val) * int64(sf.YScaleFp)) >> RefScaleShift)
}

func getFixedPointScale(otherSize, thisSize int) int {
	return (otherSize << RefScaleShift) / thisSize
}

// validRefFrameSize mirrors libvpx's valid_ref_frame_size predicate:
// reference width/height must be within 2x of the current frame in
// either direction.
func validRefFrameSize(refW, refH, thisW, thisH int) bool {
	return 2*thisW >= refW && 2*thisH >= refH &&
		thisW <= 16*refW && thisH <= 16*refH
}
