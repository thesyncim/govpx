package govpx

import (
	"unsafe"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

func interMotionSearchCost(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int) int {
	return macroblockSAD(src, ref, mbRow, mbCol, mv) + interMotionSearchVectorCost(mv, bestRefMV, qIndex)
}

func interMotionSplitBlockSearchCost(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, mv vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int) int {
	return splitBlockSAD(src, ref, mbRow, mbCol, block, width, height, mv) + interMotionSplitBlockSearchVectorCost(mv, bestRefMV, qIndex)
}

// fullPelSearchCtx hoists the per-MB invariants for the diamond / n-step /
// refine / hex / exhaustive full-pel search kernels out of the per-site inner
// loop. Like libvpx's mcomp.c, candidate SAD reads from the bordered reference
// plane (`base_pre + d->offset + row*stride + col`), so legal UMV edge
// candidates stay on the same SIMD path as interior candidates.
type fullPelSearchCtx struct {
	src        vp8enc.SourceImage
	ref        *vp8common.Image
	mbRow      int
	mbCol      int
	baseY      int
	baseX      int
	srcRowPtr  []byte // = src.Y[baseY*src.YStride+baseX : ]
	srcRowPtrP *byte  // = unsafe.SliceData(srcRowPtr) — hot SAD bypass
	srcYStride int
	refYFullP  *byte
	refYStride int
	refYOrigin int
	refYBorder int
	refRowH    uint // = uint(ref.CodedHeight + 2*ref.YBorder - 16)
	refRowW    uint // = uint(ref.CodedWidth + 2*ref.YBorder - 16)
}

func newFullPelSearchCtx(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int) fullPelSearchCtx {
	baseY := mbRow * 16
	baseX := mbCol * 16
	srcRowPtr := src.Y[baseY*src.YStride+baseX:]
	refYFull := ref.YFull
	refYFullP := unsafe.SliceData(refYFull)
	refRowH := uint(ref.CodedHeight + 2*ref.YBorder - 16)
	refRowW := uint(ref.CodedWidth + 2*ref.YBorder - 16)
	if !refFullPelBufferOK(ref, 16, 16) {
		refYFullP = nil
		refRowH = 0
		refRowW = 0
	}
	return fullPelSearchCtx{
		src:        src,
		ref:        ref,
		mbRow:      mbRow,
		mbCol:      mbCol,
		baseY:      baseY,
		baseX:      baseX,
		srcRowPtr:  srcRowPtr,
		srcRowPtrP: unsafe.SliceData(srcRowPtr),
		srcYStride: src.YStride,
		refYFullP:  refYFullP,
		refYStride: ref.YStride,
		refYOrigin: ref.YOrigin,
		refYBorder: ref.YBorder,
		refRowH:    refRowH,
		refRowW:    refRowW,
	}
}

func (c *fullPelSearchCtx) fullPelCostFull(row int, col int, refRow8 int, refCol8 int, qIndex int) int {
	return c.fullPelSADFull(row, col) + libvpxFullPelMVSADCost16FromDeltas(row, col, refRow8, refCol8, qIndex)
}

func (c *fullPelSearchCtx) fullPelSADFull(row int, col int) int {
	refBaseY := c.baseY + row
	refBaseX := c.baseX + col
	if c.refYFullP != nil &&
		uint(refBaseY+c.refYBorder) <= c.refRowH &&
		uint(refBaseX+c.refYBorder) <= c.refRowW {
		refPtr := (*byte)(unsafe.Add(unsafe.Pointer(c.refYFullP), c.refYOrigin+refBaseY*c.refYStride+refBaseX))
		return dsp.SAD16x16PtrFast(c.srcRowPtrP, c.srcYStride, refPtr, c.refYStride)
	}
	return c.fullPelCostLimitedSlow(col*interFrameMVFullPixelStep, row*interFrameMVFullPixelStep, refBaseY, refBaseX, maxInt())
}

func (c *fullPelSearchCtx) fullPelSADFull4(row0 int, col0 int, row1 int, col1 int, row2 int, col2 int, row3 int, col3 int, out *[4]uint32) bool {
	refBaseY0 := c.baseY + row0
	refBaseX0 := c.baseX + col0
	refBaseY1 := c.baseY + row1
	refBaseX1 := c.baseX + col1
	refBaseY2 := c.baseY + row2
	refBaseX2 := c.baseX + col2
	refBaseY3 := c.baseY + row3
	refBaseX3 := c.baseX + col3
	if c.refYFullP == nil ||
		uint(refBaseY0+c.refYBorder) > c.refRowH || uint(refBaseX0+c.refYBorder) > c.refRowW ||
		uint(refBaseY1+c.refYBorder) > c.refRowH || uint(refBaseX1+c.refYBorder) > c.refRowW ||
		uint(refBaseY2+c.refYBorder) > c.refRowH || uint(refBaseX2+c.refYBorder) > c.refRowW ||
		uint(refBaseY3+c.refYBorder) > c.refRowH || uint(refBaseX3+c.refYBorder) > c.refRowW {
		return false
	}
	base := unsafe.Pointer(c.refYFullP)
	refPtr0 := (*byte)(unsafe.Add(base, c.refYOrigin+refBaseY0*c.refYStride+refBaseX0))
	refPtr1 := (*byte)(unsafe.Add(base, c.refYOrigin+refBaseY1*c.refYStride+refBaseX1))
	refPtr2 := (*byte)(unsafe.Add(base, c.refYOrigin+refBaseY2*c.refYStride+refBaseX2))
	refPtr3 := (*byte)(unsafe.Add(base, c.refYOrigin+refBaseY3*c.refYStride+refBaseX3))
	dsp.SAD16x16x4PtrFast(c.srcRowPtrP, c.srcYStride, refPtr0, refPtr1, refPtr2, refPtr3, c.refYStride, out)
	return true
}

func (c *fullPelSearchCtx) fullPelCostLimited(mvRow int, mvCol int, limit int, refRow8 int, refCol8 int, qIndex int) int {
	mvCost := libvpxFullPelMVSADCost16FromDeltas(mvRow>>3, mvCol>>3, refRow8, refCol8, qIndex)
	sadLimit := limit - mvCost
	if sadLimit < 0 {
		return limit + 1
	}
	refBaseY := c.baseY + (mvRow >> 3)
	refBaseX := c.baseX + (mvCol >> 3)
	if c.refYFullP != nil &&
		uint(refBaseY+c.refYBorder) <= c.refRowH &&
		uint(refBaseX+c.refYBorder) <= c.refRowW {
		refPtr := (*byte)(unsafe.Add(unsafe.Pointer(c.refYFullP), c.refYOrigin+refBaseY*c.refYStride+refBaseX))
		return dsp.SAD16x16LimitPtrFast(c.srcRowPtrP, c.srcYStride, refPtr, c.refYStride, sadLimit) + mvCost
	}
	return c.fullPelCostLimitedSlow(mvCol, mvRow, refBaseY, refBaseX, sadLimit) + mvCost
}

func (c *fullPelSearchCtx) fullPelCostLimitedSlow(mvCol int, mvRow int, refBaseY int, refBaseX int, sadLimit int) int {
	return macroblockSADLimitedSlow(c.src, c.ref, c.baseY, c.baseX, refBaseY, refBaseX, mvCol, mvRow, sadLimit)
}

func refFullPelBufferOK(ref *vp8common.Image, width int, height int) bool {
	if ref == nil || min(width, height) <= 0 || len(ref.YFull) == 0 ||
		min(ref.YOrigin, ref.YBorder) < 0 ||
		ref.CodedWidth+2*ref.YBorder < width ||
		ref.CodedHeight+2*ref.YBorder < height ||
		ref.YStride < ref.CodedWidth+2*ref.YBorder {
		return false
	}
	if ref.YOrigin-ref.YBorder*ref.YStride-ref.YBorder < 0 {
		return false
	}
	maxRow := ref.CodedHeight + ref.YBorder - 1
	maxColEnd := ref.CodedWidth + ref.YBorder
	return ref.YOrigin+maxRow*ref.YStride+maxColEnd <= len(ref.YFull)
}

func refFullPelYOffset(ref *vp8common.Image, refBaseY int, refBaseX int, width int, height int) (int, bool) {
	if !refFullPelBufferOK(ref, width, height) {
		return 0, false
	}
	if uint(refBaseY+ref.YBorder) > uint(ref.CodedHeight+2*ref.YBorder-height) ||
		uint(refBaseX+ref.YBorder) > uint(ref.CodedWidth+2*ref.YBorder-width) {
		return 0, false
	}
	off := ref.YOrigin + refBaseY*ref.YStride + refBaseX
	if off < 0 || off+(height-1)*ref.YStride+width > len(ref.YFull) {
		return 0, false
	}
	return off, true
}

func refFullPelYPtr(ref *vp8common.Image, refBaseY int, refBaseX int, width int, height int) (*byte, bool) {
	off, ok := refFullPelYOffset(ref, refBaseY, refBaseX, width, height)
	if !ok {
		return nil, false
	}
	return (*byte)(unsafe.Add(unsafe.Pointer(unsafe.SliceData(ref.YFull)), off)), true
}

func refFullPelYSlice(ref *vp8common.Image, refBaseY int, refBaseX int, width int, height int) ([]byte, bool) {
	off, ok := refFullPelYOffset(ref, refBaseY, refBaseX, width, height)
	if !ok {
		return nil, false
	}
	return ref.YFull[off:], true
}

func interMotionFullPixelSearchReturnCost(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int, mvProbs *[2][vp8tables.MVPCount]uint8) int {
	variance, _ := macroblockLumaMotionVarianceSSE(src, ref, mbRow, mbCol, mv)
	return variance + interMotionSearchErrorVectorCost(mv, bestRefMV, qIndex, mvProbs)
}

// interMotionSearchVectorCost charges full-pel MV bits against bestRefMV like
// libvpx mvsad_err_cost — picking against (0,0) inflates the cost of motion
// far from a strong predictor and biases NEWMV away from correct candidates.
func interMotionSearchVectorCost(mv vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int) int {
	return vp8enc.MotionVectorSADCost(mv, bestRefMV, libvpxSADPerBit16(qIndex))
}

func interMotionSplitBlockSearchVectorCost(mv vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int) int {
	return vp8enc.MotionVectorSADCost(mv, bestRefMV, libvpxSADPerBit4(qIndex))
}

// interMotionSearchErrorVectorCost charges sub-pel MV bits against bestRefMV
// (libvpx find_best_sub_pixel_step_iteratively in mcomp.c).
func interMotionSearchErrorVectorCost(mv vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int, mvProbs *[2][vp8tables.MVPCount]uint8) int {
	if mvProbs == nil {
		return 0
	}
	return vp8enc.MotionVectorErrorCost(mv, bestRefMV, mvProbs, libvpxErrorPerBit(qIndex))
}

// interMotionSubpelCandidateVectorCost charges the sub-pel MV bits like the
// MVC macro inside libvpx's vp8_find_best_sub_pixel_step{_iteratively}: the
// 1/4-pel index is built from (mv>>1) - (ref>>1) — i.e. each operand is
// arithmetic-shifted to 1/4-pel before subtraction — and the lookup is
// signed (no clamp-to-zero). This matches the CHECK_BETTER candidate cost
// shape exactly when bestRefMV is fractional in 1/8-pel, which the
// mv_err_cost / vp8_mv_bit_cost variants used for the central cost do not.
// See MotionVectorSubpelSearchCost for the full derivation.
func interMotionSubpelCandidateVectorCost(mv vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int, mvProbs *[2][vp8tables.MVPCount]uint8) int {
	if mvProbs == nil {
		return 0
	}
	return vp8enc.MotionVectorSubpelSearchCost(mv, bestRefMV, mvProbs, libvpxErrorPerBit(qIndex))
}
