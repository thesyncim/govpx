package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

const maxARNRFrames = 15

// applyARNRFilter implements libvpx's VP8 motion-compensated temporal filter
// (see vp8/encoder/temporal_filter.c). For every 16x16 luma macroblock (and
// the colocated 8x8 chroma blocks) in the center frame it searches the
// adjacent backward/forward source frames for a matching block and weights
// each predictor pixel by libvpx's
//
//	modifier = clamp((3*(src-pred)^2 + rounding) >> strength, 0, 16)
//	weight   = (16 - modifier) * filter_weight
//
// The center frame contributes with filter_weight=2 (as in libvpx); adjacent
// frames pick filter_weight in {2,1,0} based on the 16x16 SAD threshold pair
// THRESH_LOW=10000 / THRESH_HIGH=20000.
//
// The full-pixel motion search uses libvpx's hex search (vp8_hex_search) with
// a zero MV seed for each reference frame. Subpel refinement walks libvpx's
// 1/2-, 1/4- and 1/8-pel diamond around the integer-pel MV using 16x16
// sixtap-filtered SAD and adopts the lowest-SAD position. The synthesized predictor uses the
// 6-tap sixtap filter on luma and chroma (chroma's MV is the halved subpel
// MV per libvpx's `mv_offset = (1<<3 - 1) & mvR/mvC` dispatch).
//
// Simplification (vs. libvpx vp8_temporal_filter_iterate_c):
//   - The center frame is read from the input SourceImage's visible region;
//     accordingly the search position is clamped so the predictor 16x16
//     stays inside the visible area of every reference frame, and out-of-
//     visible taps fall back to gatherBlock's edge-replication rather than
//     libvpx's mirrored 16-pixel source-border extension.
//
// Everything else (per-pixel weighting, accumulator/count normalization via
// libvpx fixed_divide reciprocal LUT - see arnrFixedDivide below - and
// separate luma/chroma blocks with the 384-element per-MB scratch layout)
// follows libvpx exactly.
func (e *VP8Encoder) applyARNRFilter(center vp8enc.SourceImage, flags EncodeFlags, distance int) bool {
	maxFrames := min(e.opts.ARNRMaxFrames, maxARNRFrames)
	if maxFrames <= 1 {
		return false
	}
	backward, forward, ok := e.arnrFilterWindow(distance, maxFrames)
	if !ok {
		return false
	}
	if backward+forward == 0 {
		return false
	}
	strength := e.opts.ARNRStrength
	// The center frame is the alt-ref source. Copy it into the scratch
	// buffer first so we have a stable read source and an output in the
	// same place (libvpx writes filtered pixels into cpi->alt_ref_buffer).
	copySourceToFrameBuffer(&e.arnrScratch, center)
	// Whether the chroma planes participate matches the legacy gating
	// (invisible alt-ref or strong filter strength). Luma always runs.
	doChroma := flags&EncodeInvisibleFrame != 0 || e.opts.ARNRStrength > 4
	refs, centerIdx, ok := e.arnrFilterRefs(distance, backward, forward)
	if !ok {
		return false
	}
	e.iterateTemporalFilter(center, strength, refs, centerIdx, doChroma)
	e.arnrScratch.ExtendBorders()
	return true
}

func (e *VP8Encoder) arnrFilterWindow(distance int, maxFrames int) (int, int, bool) {
	if distance < 0 || maxFrames <= 0 {
		return 0, 0, false
	}
	numFramesBackward := distance
	numFramesForward := e.lookaheadSize() - (numFramesBackward + 1)
	if numFramesForward < 0 {
		return 0, 0, false
	}
	framesBackward := 0
	framesForward := 0
	switch e.opts.ARNRType {
	case 1:
		framesBackward = numFramesBackward
		if framesBackward >= maxFrames {
			framesBackward = maxFrames - 1
		}
	case 2:
		framesForward = numFramesForward
		if framesForward >= maxFrames {
			framesForward = maxFrames - 1
		}
	case 3:
		framesForward = numFramesForward
		framesBackward = numFramesBackward
		if framesForward > framesBackward {
			framesForward = framesBackward
		}
		if framesBackward > framesForward {
			framesBackward = framesForward
		}
		if framesForward > (maxFrames-1)/2 {
			framesForward = (maxFrames - 1) / 2
		}
		if framesBackward > maxFrames/2 {
			framesBackward = maxFrames / 2
		}
	default:
		return 0, 0, false
	}
	return framesBackward, framesForward, true
}

func (e *VP8Encoder) arnrFilterRefs(distance int, backward int, forward int) ([]arnrFrameView, int, bool) {
	framesToBlur := backward + forward + 1
	if framesToBlur <= 0 || framesToBlur > maxARNRFrames {
		return nil, 0, false
	}
	refs := make([]arnrFrameView, framesToBlur)
	startFrame := distance + forward
	for frame := range framesToBlur {
		whichBuffer := startFrame - frame
		entry := e.peekLookahead(whichBuffer, true)
		if entry == nil {
			return nil, 0, false
		}
		refs[framesToBlur-1-frame] = arnrViewFromImage(&entry.frame.Img)
	}
	return refs, backward, true
}

// arnrFrameView is the minimal frame description vp8_temporal_filter_iterate_c
// needs: visible-area pointers per plane plus their strides and dimensions.
// Adjacent frames are exposed as views over their owning vp8common.Image.
type arnrFrameView struct {
	width, height int
	y             []byte
	u             []byte
	v             []byte
	yStride       int
	uStride       int
	vStride       int
}

func arnrViewFromImage(img *vp8common.Image) arnrFrameView {
	return arnrFrameView{
		width:   img.Width,
		height:  img.Height,
		y:       img.Y,
		u:       img.U,
		v:       img.V,
		yStride: img.YStride,
		uStride: img.UStride,
		vStride: img.VStride,
	}
}

func arnrViewFromSource(src vp8enc.SourceImage) arnrFrameView {
	return arnrFrameView{
		width:   src.Width,
		height:  src.Height,
		y:       src.Y,
		u:       src.U,
		v:       src.V,
		yStride: src.YStride,
		uStride: src.UStride,
		vStride: src.VStride,
	}
}

// iterateTemporalFilter mirrors vp8_temporal_filter_iterate_c. It walks every
// 16x16 luma macroblock (with colocated 8x8 chroma blocks) in the alt-ref
// frame, picks per-frame filter weights by SAD-based error, and accumulates
// libvpx's per-pixel weighted average across the included frames.
func (e *VP8Encoder) iterateTemporalFilter(center vp8enc.SourceImage, strength int, refs []arnrFrameView, centerIdx int, doChroma bool) {
	mbCols := (center.Width + 15) >> 4
	mbRows := (center.Height + 15) >> 4
	// Bitwise OR is zero iff both are zero (mbCols/mbRows are non-
	// negative by construction). One compare instead of two.
	if mbCols|mbRows == 0 {
		return
	}

	if uint(centerIdx) >= uint(len(refs)) {
		return
	}

	dst := arnrFrameView{
		width:   e.arnrScratch.Img.Width,
		height:  e.arnrScratch.Img.Height,
		y:       e.arnrScratch.Img.Y,
		u:       e.arnrScratch.Img.U,
		v:       e.arnrScratch.Img.V,
		yStride: e.arnrScratch.Img.YStride,
		uStride: e.arnrScratch.Img.UStride,
		vStride: e.arnrScratch.Img.VStride,
	}

	// Reuse a single scratch across MBs (libvpx allocates 384 entries on
	// the stack). 16x16 luma + 8x8 U + 8x8 V = 256 + 64 + 64 = 384.
	var accumulator [384]uint32
	var count [384]uint32

	for mbRow := range mbRows {
		mbY := mbRow << 4
		for mbCol := range mbCols {
			mbX := mbCol << 4
			processARNRMacroblock(&dst, refs, centerIdx, mbRow, mbCol, mbRows, mbCols, mbX, mbY, strength, doChroma, accumulator[:], count[:])
		}
	}
}

// processARNRMacroblock corresponds to the inner mb_col loop body in
// vp8_temporal_filter_iterate_c: zero accumulators, search/weight every
// reference, then normalize accumulator/count back into the output frame.
func processARNRMacroblock(dst *arnrFrameView, refs []arnrFrameView, centerIdx int, mbRow int, mbCol int, mbRows int, mbCols int, mbX, mbY, strength int, doChroma bool, accumulator []uint32, count []uint32) {
	// Caller passes 384-element arrays for both accumulator and count.
	// Pin both to len 384 with full cap so subsequent sub-slice exprs
	// (accumulator[:256], accumulator[256:320], accumulator[320:384]
	// and the same for count) elide their IsSliceInBounds, and the
	// per-iter count[i]=0 write also loses its IsInBounds check.
	accumulator = accumulator[:384:384]
	count = count[:384:384]
	for i := range accumulator {
		accumulator[i] = 0
		count[i] = 0
	}

	// Pull the source 16x16 luma block (and 8x8 chroma blocks if they
	// will be filtered) into contiguous scratch arrays so SAD and the
	// per-pixel apply step both see a clean 16-byte stride. libvpx
	// avoids this copy because cpi->frames[alt_ref_index] is stored in
	// a contiguous YV12 buffer; for govpx this keeps the math identical
	// while accommodating arbitrary input strides.
	var srcY [256]byte
	gatherBlock(srcY[:], 16, dst.y, dst.yStride, mbX, mbY, dst.width, dst.height, 16)
	mbUVX := mbX >> 1
	mbUVY := mbY >> 1
	uvW := (dst.width + 1) >> 1
	uvH := (dst.height + 1) >> 1
	var srcU, srcV [64]byte
	if doChroma {
		gatherBlock(srcU[:], 8, dst.u, dst.uStride, mbUVX, mbUVY, uvW, uvH, 8)
		gatherBlock(srcV[:], 8, dst.v, dst.vStride, mbUVX, mbUVY, uvW, uvH, 8)
	}

	for fi, ref := range refs {
		// Choose per-frame filter weight. The center frame always uses
		// libvpx's filter_weight=2; adjacent frames are graded by the
		// 16x16 luma SAD against fixed thresholds, matching
		// vp8_temporal_filter_iterate_c's THRESH_LOW/THRESH_HIGH.
		var filterWeight int
		// Subpel MV components in 1/8-pel units. libvpx's
		// vp8_temporal_filter_predictors_mb_c expects mv_row/mv_col
		// scaled by 8 (so the integer-pel components are mvSubY>>3).
		var mvSubX, mvSubY int
		var fullX, fullY int
		if fi == centerIdx {
			filterWeight = 2
		} else {
			_, sx, sy := arnrFindMatchingMB(srcY[:], 16, ref, mbRow, mbCol, mbRows, mbCols, mbX, mbY, 0, 0)
			fullX, fullY = sx, sy
			// Subpixel refinement around the full-pel MV using the
			// 6-tap sixtap predictor and 16x16 SAD; mirrors
			// libvpx's find_fractional_mv_step diamond walk over
			// 1/2-, 1/4- and 1/8-pel offsets. The returned MV is
			// in 1/8-pel units; the final 16x16 SAD on the chosen
			// subpel position drives the THRESH_LOW/THRESH_HIGH
			// classification (matching vp8_hex_search returning
			// the subpel SAD via find_fractional_mv_step).
			subErr, sx8, sy8 := arnrSubpelRefine(srcY[:], 16, ref, mbRow, mbCol, mbRows, mbCols, mbX, mbY, fullX, fullY)
			mvSubX, mvSubY = sx8, sy8
			switch {
			case subErr < arnrThreshLow:
				filterWeight = 2
			case subErr < arnrThreshHigh:
				filterWeight = 1
			default:
				filterWeight = 0
			}
		}
		if filterWeight == 0 {
			continue
		}

		var predY [256]byte
		arnrPredictLuma16x16(predY[:], 16, ref, mbX, mbY, mvSubX, mvSubY)
		applyTemporalFilter(srcY[:], 16, predY[:], 16, strength, filterWeight, accumulator[:256], count[:256])

		if doChroma {
			// Chroma uses the half-resolution colocated subpel MV.
			// libvpx's vp8_temporal_filter_predictors_mb_c does
			// `mv_row >>= 1; mv_col >>= 1;` on the 1/8-pel MV
			// then dispatches subpixel_predict8x8 with
			// (mv_col & 7, mv_row & 7).
			mvSubUVX := mvSubX >> 1
			mvSubUVY := mvSubY >> 1
			var predU, predV [64]byte
			arnrPredictChroma8x8(predU[:], 8, ref.u, ref.uStride, (ref.width+1)>>1, (ref.height+1)>>1, mbUVX, mbUVY, mvSubUVX, mvSubUVY)
			arnrPredictChroma8x8(predV[:], 8, ref.v, ref.vStride, (ref.width+1)>>1, (ref.height+1)>>1, mbUVX, mbUVY, mvSubUVX, mvSubUVY)
			applyTemporalFilter(srcU[:], 8, predU[:], 8, strength, filterWeight, accumulator[256:320], count[256:320])
			applyTemporalFilter(srcV[:], 8, predV[:], 8, strength, filterWeight, accumulator[320:384], count[320:384])
		}
	}

	// Normalize accumulator/count into the output. libvpx uses a
	// per-count fixed-point reciprocal LUT (vp8/encoder/onyx_if.c:
	// fixed_divide[i] = 0x80000 / i for i in [1, 512)) and computes
	//     pval = (accumulator + count/2) * fixed_divide[count] >> 19
	// which is equivalent to (accumulator + count/2) / count modulo a
	// truncating-reciprocal rounding artifact (up to 1 LSB). govpx ports
	// the LUT verbatim so the temporal filter output is byte-identical
	// to vp8_temporal_filter_iterate_c.
	writeARNRBlock(dst.y, dst.yStride, mbX, mbY, dst.width, dst.height, 16, accumulator[:256], count[:256])
	if doChroma {
		writeARNRBlock(dst.u, dst.uStride, mbUVX, mbUVY, uvW, uvH, 8, accumulator[256:320], count[256:320])
		writeARNRBlock(dst.v, dst.vStride, mbUVX, mbUVY, uvW, uvH, 8, accumulator[320:384], count[320:384])
	}
}

// gatherBlock copies a (size x size) block at (srcX,srcY) from a planar
// surface (with arbitrary stride and visible width/height) into a tightly
// packed scratch buffer with stride dstStride. Reads outside the visible
// area are clamped to the nearest in-bounds pixel - the libvpx encoder
// extends source borders so all intra-MB reads are valid; in govpx the
// SourceImage has only the visible area and clamping replicates that
// effect when the search picks an MB that straddles the edge.
func gatherBlock(dst []byte, dstStride int, src []byte, srcStride, srcX, srcY, srcW, srcH, size int) {
	for j := range size {
		yy := min(max(srcY+j, 0), srcH-1)
		row := src[yy*srcStride:]
		for i := range size {
			xx := min(max(srcX+i, 0), srcW-1)
			dst[j*dstStride+i] = row[xx]
		}
	}
}

// arnrFixedDivide ports libvpx cpi->fixed_divide table from
// vp8/encoder/onyx_if.c (init_config):
//
//	cpi->fixed_divide[0] = 0;
//	for (i = 1; i < 512; ++i) cpi->fixed_divide[i] = 0x80000 / i;
//
// vp8_temporal_filter_iterate_c reads this LUT during the per-pixel
// normalization step. The reciprocal is precomputed as 0x80000/c (a
// truncating integer divide) and re-applied with a right shift of 19,
// so result = ((accumulator + count/2) * (0x80000 / count)) >> 19. For
// most count values 0x80000 mod count is nonzero, so the LUT result
// differs from native integer division by up to 1 LSB. Matching libvpx
// exactly requires the same truncating reciprocal table.
var arnrFixedDivide = func() [512]uint32 {
	var t [512]uint32
	for i := 1; i < 512; i++ {
		t[i] = 0x80000 / uint32(i)
	}
	return t
}()

// writeARNRBlock writes the (size x size) accumulated/count pair back into
// the destination plane, clipping to the visible area.
func writeARNRBlock(dst []byte, dstStride, dstX, dstY, dstW, dstH, size int, accumulator []uint32, count []uint32) {
	// Bound count/accumulator to size*size once: caller passes 384-entry
	// scratch sliced down to the active block ([:256] for luma, [256:320]
	// / [320:384] for chroma) so this matches the active range.
	n := size * size
	if len(count) < n || len(accumulator) < n {
		return
	}
	count = count[:n:n]
	accumulator = accumulator[:n:n]
	for j := range size {
		yy := dstY + j
		if uint(yy) >= uint(dstH) {
			continue
		}
		// Bound row to dstW so row[xx] with xx < dstW elides.
		end := yy*dstStride + dstW
		if end > len(dst) {
			continue
		}
		row := dst[yy*dstStride : end : end]
		for i := range size {
			xx := dstX + i
			if uint(xx) >= uint(dstW) {
				continue
			}
			k := j*size + i
			c := count[k]
			if c == 0 {
				continue
			}
			// libvpx fixed_divide LUT lookup. count[k] is bounded
			// by max_frames(15) * max_modifier(16) * max_weight(2)
			// = 480 < 512, so the index is always in range, but
			// guard for safety.
			if c >= uint32(len(arnrFixedDivide)) {
				row[xx] = byte(min((accumulator[k]+c/2)/c, 255))
				continue
			}
			pval := min((accumulator[k]+c>>1)*arnrFixedDivide[c]>>19, 255)
			row[xx] = byte(pval)
		}
	}
}

// arnr motion search constants. The thresholds match libvpx's
// THRESH_LOW/THRESH_HIGH (10000/20000 for a 16x16 SAD). The hex/diamond
// step counts mirror vp8_hex_search's hex_range=127 and dia_range=8.
const (
	arnrThreshLow  = 10000
	arnrThreshHigh = 20000
	arnrHexRange   = 127
	arnrDiaRange   = 8
)

// arnrFindMatchingMB performs libvpx's hex search (vp8_hex_search with
// NULL mvsadcost, i.e. pure 16x16 SAD) to locate the matching predictor
// block in an adjacent frame. It returns the best 16x16 SAD plus the
// integer-pixel MV (relative to the colocated position). The search is
// seeded at (seedX, seedY). ARNR passes (0,0), matching
// vp8_temporal_filter_find_matching_mb_c.
func arnrFindMatchingMB(src []byte, srcStride int, ref arnrFrameView, mbRow int, mbCol int, mbRows int, mbCols int, mbX, mbY int, seedX, seedY int) (int, int, int) {
	// Compute the libvpx-shaped MV bounds in pixel units. These permit
	// the predictor to overhang the visible region by 5 pixels on each
	// side; govpx's gatherBlock clamps any out-of-range read so the math
	// stays well-defined while preserving libvpx's reachable search set.
	mvColMin := -(mbCol*16 + (16 - 5))
	mvColMax := (mbCols-1-mbCol)*16 + (16 - 5)
	mvRowMin := -(mbRow*16 + (16 - 5))
	mvRowMax := (mbRows-1-mbRow)*16 + (16 - 5)

	// Clamp the seed into the legal window (libvpx's vp8_clamp_mv).
	br := arnrClamp(seedY, mvRowMin, mvRowMax)
	bc := arnrClamp(seedX, mvColMin, mvColMax)

	hex := [6][2]int{
		{-1, -2}, {1, -2}, {2, 0}, {1, 2}, {-1, 2}, {-2, 0},
	}
	nextChkpts := [6][3][2]int{
		{{-2, 0}, {-1, -2}, {1, -2}},
		{{-1, -2}, {1, -2}, {2, 0}},
		{{1, -2}, {2, 0}, {1, 2}},
		{{2, 0}, {1, 2}, {-1, 2}},
		{{1, 2}, {-1, 2}, {-2, 0}},
		{{-1, 2}, {-2, 0}, {-1, -2}},
	}
	neighbors := [4][2]int{{0, -1}, {-1, 0}, {1, 0}, {0, 1}}

	bestSAD := arnrSADAt(src, srcStride, ref, mbX, mbY, bc, br)

	// 6-vertex hexagon scan around the seed (libvpx's first hex iter).
	bestSite := -1
	for i, step := range hex {
		row := br + step[0]
		col := bc + step[1]
		if !arnrInBounds(col, row, mvColMin, mvColMax, mvRowMin, mvRowMax) {
			continue
		}
		sad := arnrSADAt(src, srcStride, ref, mbX, mbY, col, row)
		if sad < bestSAD {
			bestSAD = sad
			bestSite = i
		}
	}

	// Iterative 3-checkpoint walk along the hex's last-best edge.
	if bestSite >= 0 {
		br += hex[bestSite][0]
		bc += hex[bestSite][1]
		k := bestSite
		for j := 1; j < arnrHexRange; j++ {
			bestSite = -1
			for i, step := range nextChkpts[k] {
				row := br + step[0]
				col := bc + step[1]
				if !arnrInBounds(col, row, mvColMin, mvColMax, mvRowMin, mvRowMax) {
					continue
				}
				sad := arnrSADAt(src, srcStride, ref, mbX, mbY, col, row)
				if sad < bestSAD {
					bestSAD = sad
					bestSite = i
				}
			}
			if bestSite < 0 {
				break
			}
			br += nextChkpts[k][bestSite][0]
			bc += nextChkpts[k][bestSite][1]
			k += 5 + bestSite
			if k >= 12 {
				k -= 12
			} else if k >= 6 {
				k -= 6
			}
		}
	}

	// 4-neighbor diamond refinement (libvpx's cal_neighbors loop).
	for range arnrDiaRange {
		bestSite = -1
		for i, step := range neighbors {
			row := br + step[0]
			col := bc + step[1]
			if !arnrInBounds(col, row, mvColMin, mvColMax, mvRowMin, mvRowMax) {
				continue
			}
			sad := arnrSADAt(src, srcStride, ref, mbX, mbY, col, row)
			if sad < bestSAD {
				bestSAD = sad
				bestSite = i
			}
		}
		if bestSite < 0 {
			break
		}
		br += neighbors[bestSite][0]
		bc += neighbors[bestSite][1]
	}

	return bestSAD, bc, br
}

func arnrClamp(v, lo, hi int) int {
	return min(max(v, lo), hi)
}

func arnrInBounds(col, row, colMin, colMax, rowMin, rowMax int) bool {
	return col >= colMin && col <= colMax && row >= rowMin && row <= rowMax
}

// arnrSADAt computes the 16x16 SAD between the contiguous source block
// (already gathered into a 16-stride scratch) and the reference block at
// (mbX+mvX, mbY+mvY). When the candidate position straddles the visible
// boundary the read is routed through gatherBlock so libvpx's source-
// border-extension reads stay well-defined.
func arnrSADAt(src []byte, srcStride int, ref arnrFrameView, mbX, mbY, mvX, mvY int) int {
	x := mbX + mvX
	y := mbY + mvY
	// Uint range collapses (x/y >= 0) + (x/y+16 <= dim) into one compare
	// per dimension; smaller dims fall through to the border-extending
	// gatherBlock path.
	if uint(x) <= uint(ref.width-16) && uint(y) <= uint(ref.height-16) {
		return dsp.SAD16x16(src, srcStride, ref.y[y*ref.yStride+x:], ref.yStride)
	}
	var pred [256]byte
	gatherBlock(pred[:], 16, ref.y, ref.yStride, x, y, ref.width, ref.height, 16)
	return dsp.SAD16x16(src, srcStride, pred[:], 16)
}

// arnrSubpelRefine implements libvpx's find_fractional_mv_step diamond walk
// for the temporal filter. Inputs are the full-pel MV (fullX, fullY) chosen
// by the hex search; the output is the refined MV in 1/8-pel units along
// with the 16x16 SAD at that subpel position. The walk visits the four
// neighboring subpel offsets (left/right/up/down) at successively finer
// granularities (1/2 -> 1/4 -> 1/8 pel) and adopts the lowest-SAD position;
// libvpx also tests one diagonal per iteration which we mirror.
func arnrSubpelRefine(src []byte, srcStride int, ref arnrFrameView, mbRow, mbCol, mbRows, mbCols int, mbX, mbY, fullX, fullY int) (int, int, int) {
	// Subpel MV bounds (1/8-pel units). The integer-pel search has
	// already clamped to libvpx's mv_row_min/mv_col_min derivation; the
	// subpel walk stays within ±1 full pel of that integer position so
	// the sixtap predictor's 6-tap reach (5 pixels overhang) lines up
	// with the same pixel envelope the hex search reached.
	mvColMinPel := -(mbCol*16 + (16 - 5))
	mvColMaxPel := (mbCols-1-mbCol)*16 + (16 - 5)
	mvRowMinPel := -(mbRow*16 + (16 - 5))
	mvRowMaxPel := (mbRows-1-mbRow)*16 + (16 - 5)
	minCol := mvColMinPel << 3
	maxCol := mvColMaxPel << 3
	minRow := mvRowMinPel << 3
	maxRow := mvRowMaxPel << 3

	bestRow := fullY << 3
	bestCol := fullX << 3
	bestSAD := arnrSADAtSubpel(src, srcStride, ref, mbX, mbY, bestCol, bestRow)

	// libvpx does up to 4 half-pel iters then up to 4 quarter-pel iters
	// in find_fractional_mv_step_iteratively. We extend the same pattern
	// down to 1/8 pel because govpx's MV grid is 1/8 (libvpx's vp8 final
	// MV is also 1/8-pel after multiplying the *4 subpel result by 2).
	steps := [3]int{4, 2, 1} // 1/2-, 1/4-, 1/8-pel deltas in 1/8-pel units
	for _, step := range steps {
		iters := 4
		for range iters {
			startRow := bestRow
			startCol := bestCol
			// Test the 4 axis-aligned neighbors.
			leftSAD := arnrSubpelProbe(src, srcStride, ref, mbX, mbY, startRow, startCol-step, minRow, maxRow, minCol, maxCol)
			rightSAD := arnrSubpelProbe(src, srcStride, ref, mbX, mbY, startRow, startCol+step, minRow, maxRow, minCol, maxCol)
			upSAD := arnrSubpelProbe(src, srcStride, ref, mbX, mbY, startRow-step, startCol, minRow, maxRow, minCol, maxCol)
			downSAD := arnrSubpelProbe(src, srcStride, ref, mbX, mbY, startRow+step, startCol, minRow, maxRow, minCol, maxCol)
			if leftSAD < bestSAD {
				bestSAD = leftSAD
				bestRow = startRow
				bestCol = startCol - step
			}
			if rightSAD < bestSAD {
				bestSAD = rightSAD
				bestRow = startRow
				bestCol = startCol + step
			}
			if upSAD < bestSAD {
				bestSAD = upSAD
				bestRow = startRow - step
				bestCol = startCol
			}
			if downSAD < bestSAD {
				bestSAD = downSAD
				bestRow = startRow + step
				bestCol = startCol
			}
			// One diagonal probe in the direction of the better
			// horizontal+vertical neighbors, mirroring libvpx's
			// `whichdir` switch.
			dr := -step
			dc := -step
			if downSAD < upSAD {
				dr = step
			}
			if rightSAD < leftSAD {
				dc = step
			}
			diagSAD := arnrSubpelProbe(src, srcStride, ref, mbX, mbY, startRow+dr, startCol+dc, minRow, maxRow, minCol, maxCol)
			if diagSAD < bestSAD {
				bestSAD = diagSAD
				bestRow = startRow + dr
				bestCol = startCol + dc
			}
			if bestRow == startRow && bestCol == startCol {
				break
			}
		}
	}

	return bestSAD, bestCol, bestRow
}

// arnrSubpelProbe checks bounds and returns INT_MAX-equivalent on out-of-
// range so the caller's < bestSAD compare always rejects illegal positions.
func arnrSubpelProbe(src []byte, srcStride int, ref arnrFrameView, mbX, mbY, row, col, minRow, maxRow, minCol, maxCol int) int {
	if row < minRow || row > maxRow || col < minCol || col > maxCol {
		const big = 1<<30 - 1
		return big
	}
	return arnrSADAtSubpel(src, srcStride, ref, mbX, mbY, col, row)
}

// arnrSADAtSubpel computes the 16x16 SAD between the source block (in a
// contiguous 16-stride scratch) and a sixtap-filtered predictor at
// (mbX + col/8 + col%8 fractional, mbY + row/8 + row%8 fractional). When
// the subpel offset is zero this collapses to the integer-pel SAD path.
func arnrSADAtSubpel(src []byte, srcStride int, ref arnrFrameView, mbX, mbY, col, row int) int {
	if (row|col)&7 == 0 {
		return arnrSADAt(src, srcStride, ref, mbX, mbY, col>>3, row>>3)
	}
	var pred [256]byte
	arnrSubpelLuma16x16(pred[:], 16, ref, mbX, mbY, col, row)
	return dsp.SAD16x16(src, srcStride, pred[:], 16)
}

// arnrSubpelLuma16x16 fills a 16x16 predictor block with the sixtap-
// filtered luma reference at integer position (mbX + col>>3, mbY + row>>3)
// plus the (col&7, row&7) 1/8-pel fractional offset. Out-of-visible reads
// are routed through gatherBlock so the predictor stays defined when the
// MV pushes the 6-tap filter footprint past the visible edge.
func arnrSubpelLuma16x16(dst []byte, dstStride int, ref arnrFrameView, mbX, mbY, col, row int) {
	intCol := col >> 3
	intRow := row >> 3
	fracCol := col & 7
	fracRow := row & 7
	// 6-tap reads 2 pixels before and 3 pixels after the prediction
	// origin in each axis. Gather a (16+5)x(16+5)=21x21 region whose
	// origin sits 2 pixels above/left of the integer prediction origin
	// so SixTapPredict16x16 sees a contiguous 21-stride neighborhood.
	const pad = 2
	const gathered = 16 + 5 // 21
	var scratch [gathered * gathered]byte
	gatherBlock(scratch[:], gathered, ref.y, ref.yStride, mbX+intCol-pad, mbY+intRow-pad, ref.width, ref.height, gathered)
	dsp.SixTapPredict16x16(scratch[:], gathered, fracCol, fracRow, dst, dstStride)
}

// arnrPredictLuma16x16 synthesizes the 16x16 luma predictor at the given
// 1/8-pel MV. When the MV is integer-aligned this is gatherBlock; otherwise
// the sixtap filter runs on a clamped 21x21 neighborhood.
func arnrPredictLuma16x16(dst []byte, dstStride int, ref arnrFrameView, mbX, mbY, mvSubX, mvSubY int) {
	if (mvSubX|mvSubY)&7 == 0 {
		gatherBlock(dst, dstStride, ref.y, ref.yStride, mbX+(mvSubX>>3), mbY+(mvSubY>>3), ref.width, ref.height, 16)
		return
	}
	arnrSubpelLuma16x16(dst, dstStride, ref, mbX, mbY, mvSubX, mvSubY)
}

// arnrPredictChroma8x8 synthesizes an 8x8 chroma predictor at the chroma
// 1/8-pel MV. libvpx halves the luma MV before dispatching the chroma
// subpel predictor (vp8_temporal_filter_predictors_mb_c does
// `mv_row >>= 1; mv_col >>= 1`); callers pass the already-halved MV.
func arnrPredictChroma8x8(dst []byte, dstStride int, plane []byte, planeStride int, planeW, planeH int, mbUVX, mbUVY, mvSubX, mvSubY int) {
	intCol := mvSubX >> 3
	intRow := mvSubY >> 3
	fracCol := mvSubX & 7
	fracRow := mvSubY & 7
	if (fracCol | fracRow) == 0 {
		gatherBlock(dst, dstStride, plane, planeStride, mbUVX+intCol, mbUVY+intRow, planeW, planeH, 8)
		return
	}
	const pad = 2
	const gathered = 8 + 5 // 13
	var scratch [gathered * gathered]byte
	gatherBlock(scratch[:], gathered, plane, planeStride, mbUVX+intCol-pad, mbUVY+intRow-pad, planeW, planeH, gathered)
	dsp.SixTapPredict8x8(scratch[:], gathered, fracCol, fracRow, dst, dstStride)
}

// applyTemporalFilter is a direct port of libvpx's
// vp8_temporal_filter_apply_c. The integer formula approximates
//
//	coeff = (3 * (src - pred)^2) / 2^strength
//	modifier = clamp(round(coeff), 0, 16)
//	weight   = (16 - modifier) * filter_weight
//
// and accumulates count/accumulator for downstream normalization.
func applyTemporalFilter(src []byte, srcStride int, pred []byte, predStride int, strength int, filterWeight int, accumulator []uint32, count []uint32) {
	blockSize := 16
	if len(accumulator) == 64 {
		blockSize = 8
	}
	if applyTemporalFilterSIMD(src, srcStride, pred, predStride, blockSize, strength, filterWeight, accumulator, count) {
		return
	}
	applyTemporalFilterScalar(src, srcStride, pred, predStride, blockSize, strength, filterWeight, accumulator, count)
}

func applyTemporalFilterScalar(src []byte, srcStride int, pred []byte, predStride int, blockSize int, strength int, filterWeight int, accumulator []uint32, count []uint32) {
	rounding := 0
	if strength > 0 {
		rounding = 1 << (strength - 1)
	}
	k := 0
	for j := range blockSize {
		srcRow := src[j*srcStride:]
		predRow := pred[j*predStride:]
		for i := range blockSize {
			diff := int(srcRow[i]) - int(predRow[i])
			modifier := diff*diff*3 + rounding
			modifier >>= uint(strength)
			modifier = min(modifier, 16)
			modifier = (16 - modifier) * filterWeight
			count[k] += uint32(modifier)
			accumulator[k] += uint32(modifier) * uint32(predRow[i])
			k++
		}
	}
}
