package encoder

import (
	"github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// Ported from libvpx v1.16.0 vp8/encoder/bitstream.c interframe frame-header
// helpers.

func writeInterRefreshHeader(w *BoolWriter, cfg *InterFrameStateConfig) {
	writeBoolBit(w, cfg.RefreshGolden)
	writeBoolBit(w, cfg.RefreshAltRef)
	if !cfg.RefreshGolden {
		w.WriteLiteral(uint32(cfg.CopyBufferToGolden), 2)
	}
	if !cfg.RefreshAltRef {
		w.WriteLiteral(uint32(cfg.CopyBufferToAltRef), 2)
	}
	writeBoolBit(w, cfg.GoldenSignBias)
	writeBoolBit(w, cfg.AltRefSignBias)
	writeBoolBit(w, cfg.RefreshEntropyProbs)
	writeBoolBit(w, cfg.RefreshLast)
}

func writeInterModeHeader(w *BoolWriter, cfg *InterFrameStateConfig) error {
	writeBoolBit(w, cfg.MBNoCoeffSkip)
	if cfg.MBNoCoeffSkip {
		w.WriteLiteral(uint32(cfg.ProbSkipFalse), 8)
	}
	w.WriteLiteral(uint32(cfg.ProbIntra), 8)
	w.WriteLiteral(uint32(cfg.ProbLast), 8)
	w.WriteLiteral(uint32(cfg.ProbGolden), 8)
	if cfg.YModeUpdate {
		w.WriteBit(1)
		for _, prob := range cfg.YModeProbs {
			if prob == 0 {
				return ErrInvalidPacketConfig
			}
			w.WriteLiteral(uint32(prob), 8)
		}
	} else {
		w.WriteBit(0)
	}
	if cfg.UVModeUpdate {
		w.WriteBit(1)
		for _, prob := range cfg.UVModeProbs {
			if prob == 0 {
				return ErrInvalidPacketConfig
			}
			w.WriteLiteral(uint32(prob), 8)
		}
	} else {
		w.WriteBit(0)
	}
	for component := range 2 {
		for i := range tables.MVPCount {
			if cfg.MVUpdate[component][i] {
				encoded, ok := encodeMotionVectorProbabilityUpdate(cfg.MVProbs[component][i])
				if !ok {
					return ErrInvalidPacketConfig
				}
				w.WriteBool(1, tables.MVUpdateProbs[component][i])
				w.WriteLiteral(uint32(encoded), 7)
			} else {
				w.WriteBool(0, tables.MVUpdateProbs[component][i])
			}
		}
	}
	if w.Err() != nil {
		return w.Err()
	}
	return nil
}

func encodeMotionVectorProbabilityUpdate(prob uint8) (uint8, bool) {
	if prob == 1 {
		return 0, true
	}
	if prob >= 2 && prob <= 254 && prob&1 == 0 {
		return prob >> 1, true
	}
	return 0, false
}

func writeBoolBit(w *BoolWriter, value bool) {
	if value {
		w.WriteBit(1)
		return
	}
	w.WriteBit(0)
}

func zeroMVInterModeCounts(row int, col int) uint8 {
	var counts uint8
	if row > 0 {
		counts += 2
	}
	if col > 0 {
		counts += 2
	}
	if row > 0 && col > 0 {
		counts++
	}
	return counts
}

func validInterFrameStateConfig(cfg *InterFrameStateConfig) bool {
	return cfg != nil &&
		cfg.LoopFilterLevel <= 63 &&
		cfg.SharpnessLevel <= 7 &&
		cfg.TokenPartition >= common.OnePartition &&
		cfg.TokenPartition <= common.EightPartition &&
		cfg.BaseQIndex <= 127 &&
		validQuantDeltas(cfg.QuantDeltas) &&
		cfg.CopyBufferToGolden >= 0 &&
		cfg.CopyBufferToGolden <= 2 &&
		cfg.CopyBufferToAltRef >= 0 &&
		cfg.CopyBufferToAltRef <= 2 &&
		(!cfg.RefreshGolden || cfg.CopyBufferToGolden == 0) &&
		(!cfg.RefreshAltRef || cfg.CopyBufferToAltRef == 0) &&
		validSegmentationConfig(cfg.Segmentation)
}

// Ported from libvpx v1.16.0 vp8/encoder/mcomp.c and
// vp8/encoder/rdopt.c full-pixel motion-search bounds.

const (
	InterFrameFullPixelSearchRadius = 16
	InterFrameMVFullPixelStep       = 8
	InterFrameMaxMVSearchSteps      = 8
	InterFrameMaxFirstStep          = 1 << (InterFrameMaxMVSearchSteps - 1)
	InterFrameMaxFullPelVal         = mvFullPixelMax
	InterFrameUMVBorderPixels       = 32
)

type InterFrameFullPixelBounds struct {
	RowMin int
	RowMax int
	ColMin int
	ColMax int
}

func InterFrameFullPixelSearchBounds(bestRefMV MotionVector, mbRow int, mbCol int, mbRows int, mbCols int) InterFrameFullPixelBounds {
	bounds := InterFrameFullPixelBounds{
		RowMin: ((int(bestRefMV.Row) + 7) >> 3) - InterFrameMaxFullPelVal,
		RowMax: (int(bestRefMV.Row) >> 3) + InterFrameMaxFullPelVal,
		ColMin: ((int(bestRefMV.Col) + 7) >> 3) - InterFrameMaxFullPelVal,
		ColMax: (int(bestRefMV.Col) >> 3) + InterFrameMaxFullPelVal,
	}
	if mbRows > 0 {
		umv := InterFrameUMVBorderPixels - 16
		bounds.RowMin = max(bounds.RowMin, -((mbRow * 16) + umv))
		bounds.RowMax = min(bounds.RowMax, ((mbRows-1-mbRow)*16)+umv)
	}
	if mbCols > 0 {
		umv := InterFrameUMVBorderPixels - 16
		bounds.ColMin = max(bounds.ColMin, -((mbCol * 16) + umv))
		bounds.ColMax = min(bounds.ColMax, ((mbCols-1-mbCol)*16)+umv)
	}
	return bounds
}

// InterFrameUMVOnlyFullPixelSearchBounds mirrors libvpx's MB-scope UMV
// window without the [bestRefMV +/- MAX_FULL_PEL_VAL] intersection. libvpx's
// SPLITMV picker (vp8/encoder/rdopt.c:1230 vp8_rd_pick_best_mbsegmentation)
// runs the first BLOCK_8X8 segmentation with x->mv_col_min/x->mv_col_max
// set to the wide MB-scope UMV window. Only the secondary segmentations
// (BLOCK_8X16/BLOCK_16X8/BLOCK_4X4 at rdopt.c:1245-1248) intersect that
// window with [best_ref_mv +/- MAX_FULL_PEL_VAL], and the window is restored
// at rdopt.c:1297-1301 after the secondary calls return. This helper feeds
// the BLOCK_8X8 path so its per-sub-block diamond_search_sad sees the
// full MB-scope UMV reach and can find MVs farther from best_ref_mv than
// MAX_FULL_PEL_VAL, matching libvpx byte-for-byte.
func InterFrameUMVOnlyFullPixelSearchBounds(mbRow int, mbCol int, mbRows int, mbCols int) InterFrameFullPixelBounds {
	bounds := InterFrameFullPixelBounds{
		RowMin: -InterFrameMaxFullPelVal,
		RowMax: InterFrameMaxFullPelVal,
		ColMin: -InterFrameMaxFullPelVal,
		ColMax: InterFrameMaxFullPelVal,
	}
	if mbRows > 0 {
		umv := InterFrameUMVBorderPixels - 16
		bounds.RowMin = -((mbRow * 16) + umv)
		bounds.RowMax = ((mbRows - 1 - mbRow) * 16) + umv
	}
	if mbCols > 0 {
		umv := InterFrameUMVBorderPixels - 16
		bounds.ColMin = -((mbCol * 16) + umv)
		bounds.ColMax = ((mbCols - 1 - mbCol) * 16) + umv
	}
	return bounds
}

func InterFrameUMVFullPixelInRange(mv MotionVector, mbRow int, mbCol int, mbRows int, mbCols int) bool {
	if min(mbRows, mbCols) <= 0 {
		return true
	}
	umv := InterFrameUMVBorderPixels - 16
	row := int(mv.Row) >> 3
	col := int(mv.Col) >> 3
	rowMin := -((mbRow * 16) + umv)
	rowMax := ((mbRows - 1 - mbRow) * 16) + umv
	colMin := -((mbCol * 16) + umv)
	colMax := ((mbCols - 1 - mbCol) * 16) + umv
	return row >= rowMin && row <= rowMax && col >= colMin && col <= colMax
}

func ClampInterMotionVectorToModeEdges(mv MotionVector, mbRow int, mbCol int, mbRows int, mbCols int) MotionVector {
	return clampInterMotionVectorToModeEdges(mv, mbRow, mbCol, mbRows, mbCols)
}

func (b InterFrameFullPixelBounds) ContainsFullPel(row int, col int) bool {
	return row >= b.RowMin && row <= b.RowMax && col >= b.ColMin && col <= b.ColMax
}

func (b InterFrameFullPixelBounds) ContainsFullPelStrict(row int, col int) bool {
	return row > b.RowMin && row < b.RowMax && col > b.ColMin && col < b.ColMax
}

func (b InterFrameFullPixelBounds) ClampEighth(mv MotionVector) MotionVector {
	row := min(max(int(mv.Row)>>3, b.RowMin), b.RowMax)
	col := min(max(int(mv.Col)>>3, b.ColMin), b.ColMax)
	return MotionVector{Row: int16(row * InterFrameMVFullPixelStep), Col: int16(col * InterFrameMVFullPixelStep)}
}

// Ported from libvpx v1.16.0 vp8/encoder/rdopt.c inter-mode rate and split-MV
// cost helpers.

const (
	FastNewMVBitCostWeight = 128
	RDNewMVBitCostWeight   = 96
)

func largeMotionCost() int {
	return int(^uint(0)>>1) / 4
}

func InterMotionModeVectorCost(mode *InterFrameMacroblockMode,
	above *InterFrameMacroblockMode, left *InterFrameMacroblockMode,
	aboveLeft *InterFrameMacroblockMode,
	mbRow, mbCol, mbRows, mbCols int,
	mvProbs *[2][tables.MVPCount]uint8,
	mvCosts *MotionVectorCostTables,
	subMVRefProbs *[3]uint8,
	newMVWeight int,
	signBias [common.MaxRefFrames]bool,
) int {
	if mode == nil || mode.RefFrame == common.IntraFrame {
		return 0
	}
	if mvProbs == nil && mvCosts == nil {
		return largeMotionCost()
	}
	best := InterFrameBestMotionVectorAt(above, left, aboveLeft, mode.RefFrame,
		mbRow, mbCol, mbRows, mbCols, signBias)
	if mode.Mode == common.SplitMV {
		return SplitMotionModeVectorCost(mode, left, above, best, mvProbs,
			mvCosts, subMVRefProbs)
	}
	if mode.Mode != common.NewMV {
		return 0
	}
	return InterNewMVVectorCost(mode.MV, best, mvProbs, mvCosts, newMVWeight)
}

func InterMotionModeVectorCostWithBestRef(mode *InterFrameMacroblockMode,
	left *InterFrameMacroblockMode, above *InterFrameMacroblockMode,
	bestRefMV MotionVector,
	mvProbs *[2][tables.MVPCount]uint8,
	mvCosts *MotionVectorCostTables,
	subMVRefProbs *[3]uint8,
	newMVWeight int,
) int {
	if mode == nil || mode.RefFrame == common.IntraFrame {
		return 0
	}
	if mvProbs == nil && mvCosts == nil {
		return largeMotionCost()
	}
	if mode.Mode == common.SplitMV {
		return SplitMotionModeVectorCost(mode, left, above, bestRefMV,
			mvProbs, mvCosts, subMVRefProbs)
	}
	if mode.Mode != common.NewMV {
		return 0
	}
	return InterNewMVVectorCost(mode.MV, bestRefMV, mvProbs, mvCosts,
		newMVWeight)
}

func InterMacroblockSkipRate(prob uint8, skip bool) int {
	if prob == 0 {
		prob = 128
	}
	if skip {
		return BoolBitCost(prob, 1)
	}
	return BoolBitCost(prob, 0)
}

func InterReferenceFrameRate(refFrame common.MVReferenceFrame,
	probLast uint8, probGolden uint8,
) int {
	switch refFrame {
	case common.LastFrame:
		return BoolBitCost(probLast, 0)
	case common.GoldenFrame:
		return BoolBitCost(probLast, 1) + BoolBitCost(probGolden, 0)
	case common.AltRefFrame:
		return BoolBitCost(probLast, 1) + BoolBitCost(probGolden, 1)
	default:
		return 1 << 30
	}
}

func InterPredictionModeRate(mode common.MBPredictionMode, counts InterModeCounts) int {
	probs := tables.InterModeContexts
	const maxCtx = tables.InterModeContextCount - 1
	intra := min(int(counts.Intra), maxCtx)
	nearest := min(int(counts.Nearest), maxCtx)
	near := min(int(counts.Near), maxCtx)
	split := min(int(counts.Split), maxCtx)
	switch mode {
	case common.ZeroMV:
		return BoolBitCost(probs[intra][0], 0)
	case common.NearestMV:
		return BoolBitCost(probs[intra][0], 1) +
			BoolBitCost(probs[nearest][1], 0)
	case common.NearMV:
		return BoolBitCost(probs[intra][0], 1) +
			BoolBitCost(probs[nearest][1], 1) +
			BoolBitCost(probs[near][2], 0)
	case common.NewMV:
		return BoolBitCost(probs[intra][0], 1) +
			BoolBitCost(probs[nearest][1], 1) +
			BoolBitCost(probs[near][2], 1) +
			BoolBitCost(probs[split][3], 0)
	case common.SplitMV:
		return BoolBitCost(probs[intra][0], 1) +
			BoolBitCost(probs[nearest][1], 1) +
			BoolBitCost(probs[near][2], 1) +
			BoolBitCost(probs[split][3], 1)
	default:
		return 1 << 30
	}
}

func SplitMotionModeVectorCost(mode *InterFrameMacroblockMode,
	left *InterFrameMacroblockMode, above *InterFrameMacroblockMode,
	best MotionVector,
	mvProbs *[2][tables.MVPCount]uint8,
	mvCosts *MotionVectorCostTables,
	subMVRefProbs *[3]uint8,
) int {
	if mode.Partition >= tables.NumMBSplits {
		return 1 << 30
	}
	if mvProbs == nil && mvCosts == nil {
		return largeMotionCost()
	}
	cost := MBSplitPartitionRate(mode.Partition)
	partitions := int(tables.MBSplitCount[mode.Partition&3])
	for subset := range partitions {
		block := int(tables.MBSplitOffset[mode.Partition&3][subset&15])
		leftMV := SplitLeftMV(mode, left, block)
		aboveMV := SplitAboveMV(mode, above, block)
		target := mode.BlockMV[block&15]
		bMode := mode.BModes[block&15]
		if !splitSubMotionLabelMatchesMV(bMode, target, leftMV, aboveMV) {
			return largeMotionCost()
		}
		cost += SplitSubMotionLabelRate(bMode, subMVRefProbs)
		if bMode == common.New4x4 {
			delta := MotionVector{
				Row: int16(int(target.Row) - int(best.Row)),
				Col: int16(int(target.Col) - int(best.Col)),
			}
			cost += SplitMotionVectorCost(delta, mvProbs, mvCosts)
		}
	}
	return cost
}

func SplitSubMotionLabelRate(mode common.BPredictionMode, probs *[3]uint8) int {
	if probs == nil {
		return SplitSubMotionLabelCost(mode, DefaultSubMVRefProbs)
	}
	return SplitSubMotionLabelCost(mode, *probs)
}

func SplitSubMotionLabelCost(mode common.BPredictionMode, probs [3]uint8) int {
	if uint(mode-common.Left4x4) > uint(common.New4x4-common.Left4x4) {
		return largeMotionCost()
	}
	return TreeTokenCost(tables.SubMVRefTree[:], probs[:], int(mode))
}

func MBSplitPartitionRate(partition uint8) int {
	if partition >= tables.NumMBSplits {
		return largeMotionCost()
	}
	return TreeTokenCost(tables.MBSplitTree[:], tables.MBSplitProbs[:],
		int(partition))
}

func InterNewMVVectorCost(mv MotionVector, best MotionVector,
	mvProbs *[2][tables.MVPCount]uint8,
	mvCosts *MotionVectorCostTables,
	weight int,
) int {
	if mvCosts != nil {
		return mvCosts.BitCost(mv, best, weight)
	}
	if mvProbs == nil {
		return largeMotionCost()
	}
	return MotionVectorBitCost(mv, best, mvProbs, weight)
}

func SplitMotionVectorCost(mv MotionVector,
	mvProbs *[2][tables.MVPCount]uint8,
	mvCosts *MotionVectorCostTables,
) int {
	if mvCosts != nil {
		return mvCosts.BitCost(mv, MotionVector{}, 102)
	}
	if mvProbs == nil {
		return largeMotionCost()
	}
	return MotionVectorBitCost(mv, MotionVector{}, mvProbs, 102)
}

// Ported from libvpx v1.16.0 vp8/encoder/mcomp.c full-pixel search-site
// tables.

// InterFrameNstepSearchSites and InterFrameDiamondSearchSites are
// pre-computed full-pel search-site arrays reused by VP8 inter motion search.
var InterFrameNstepSearchSites = buildInterFrameNstepSearchSites()
var InterFrameDiamondSearchSites = buildInterFrameDiamondSearchSites()

func buildInterFrameNstepSearchSites() [1 + InterFrameMaxMVSearchSteps*8]MotionVector {
	var sites [1 + InterFrameMaxMVSearchSteps*8]MotionVector
	count := 1
	for length := 1 << (InterFrameMaxMVSearchSteps - 1); length > 0; length /= 2 {
		sites[count] = MotionVector{Row: int16(-length), Col: 0}
		count++
		sites[count] = MotionVector{Row: int16(length), Col: 0}
		count++
		sites[count] = MotionVector{Row: 0, Col: int16(-length)}
		count++
		sites[count] = MotionVector{Row: 0, Col: int16(length)}
		count++
		sites[count] = MotionVector{Row: int16(-length), Col: int16(-length)}
		count++
		sites[count] = MotionVector{Row: int16(-length), Col: int16(length)}
		count++
		sites[count] = MotionVector{Row: int16(length), Col: int16(-length)}
		count++
		sites[count] = MotionVector{Row: int16(length), Col: int16(length)}
		count++
	}
	return sites
}

func buildInterFrameDiamondSearchSites() [1 + InterFrameMaxMVSearchSteps*4]MotionVector {
	var sites [1 + InterFrameMaxMVSearchSteps*4]MotionVector
	count := 1
	for length := 1 << (InterFrameMaxMVSearchSteps - 1); length > 0; length /= 2 {
		sites[count] = MotionVector{Row: int16(-length), Col: 0}
		count++
		sites[count] = MotionVector{Row: int16(length), Col: 0}
		count++
		sites[count] = MotionVector{Row: 0, Col: int16(-length)}
		count++
		sites[count] = MotionVector{Row: 0, Col: int16(length)}
		count++
	}
	return sites
}

// Ported from libvpx v1.16.0 vp8/encoder/mcomp.c sub-pixel motion-search
// range and bounds checks.

const subpelMVQuarterPelLongLimit = (1 << 10) - 1 // libvpx mvlong_width = 10.

func InterFrameSubpixelMotionVectorInRange(mv MotionVector, bestRefMV MotionVector) bool {
	maxFullPelEighths := InterFrameMaxFullPelVal << 3
	rowDelta := int(mv.Row) - int(bestRefMV.Row)
	colDelta := int(mv.Col) - int(bestRefMV.Col)
	rMask := rowDelta >> intSignShift
	cMask := colDelta >> intSignShift
	rowDelta = (rowDelta ^ rMask) - rMask
	colDelta = (colDelta ^ cMask) - cMask
	return rowDelta <= maxFullPelEighths && colDelta <= maxFullPelEighths
}

// InterFrameSubpelSearchBounds mirrors the minc/maxc/minr/maxr clamps libvpx
// computes at the head of vp8_find_best_sub_pixel_step_iteratively (and
// _step). The bounds are the intersection of the UMV window (in 1/4-pel:
// x->mv_col_min*4, x->mv_col_max*4) and a per-component window of size
// `(1 << mvlong_width) - 1` 1/4-pel sites around the 1/4-pel-aligned ref_mv
// (`ref_mv->as_mv.col >> 1`). CHECK_BETTER's IFMVCV macro short-circuits any
// candidate outside this rectangle to UINT_MAX, which the govpx iter searches
// previously skipped, letting the iter chase variance gradients past the UMV
// edge into the replicated border.
type InterFrameSubpelSearchBounds struct {
	RowMin int
	RowMax int
	ColMin int
	ColMax int
}

func InterFrameSubpelSearchBoundsFor(bestRefMV MotionVector, mbRow int, mbCol int, mbRows int, mbCols int) InterFrameSubpelSearchBounds {
	// libvpx mv_col_min / mv_col_max are in integer-pel; *4 converts to 1/4-pel.
	// The UMV window: -(mb_col*16 + (UMV_BORDER - 16)) ... ((mb_cols-1-mb_col)*16 + (UMV_BORDER - 16)).
	umv := InterFrameUMVBorderPixels - 16
	rowMinIPel := -((mbRow * 16) + umv)
	rowMaxIPel := ((mbRows - 1 - mbRow) * 16) + umv
	colMinIPel := -((mbCol * 16) + umv)
	colMaxIPel := ((mbCols - 1 - mbCol) * 16) + umv

	rrQuarter := int(bestRefMV.Row) >> 1
	rcQuarter := int(bestRefMV.Col) >> 1
	rowMin := max(rrQuarter-subpelMVQuarterPelLongLimit, rowMinIPel*4)
	rowMax := min(rrQuarter+subpelMVQuarterPelLongLimit, rowMaxIPel*4)
	colMin := max(rcQuarter-subpelMVQuarterPelLongLimit, colMinIPel*4)
	colMax := min(rcQuarter+subpelMVQuarterPelLongLimit, colMaxIPel*4)
	return InterFrameSubpelSearchBounds{RowMin: rowMin, RowMax: rowMax, ColMin: colMin, ColMax: colMax}
}

func (b InterFrameSubpelSearchBounds) Contains(row int, col int) bool {
	return row >= b.RowMin && row <= b.RowMax && col >= b.ColMin && col <= b.ColMax
}

// Ported from libvpx v1.16.0 vp8/encoder/bitstream.c interframe coefficient
// token packing.

func WriteInterCoefficientTokenGrid(w *BoolWriter, rows int, cols int, modes []InterFrameMacroblockMode, coeffs []MacroblockCoefficients, above []TokenContextPlanes, probs *tables.CoefficientProbs) error {
	if rows < 0 || cols < 0 {
		return ErrModeBufferTooSmall
	}
	if rows != 0 && cols > int(^uint(0)>>1)/rows {
		return ErrModeBufferTooSmall
	}
	required := rows * cols
	if w == nil || probs == nil || len(modes) < required || len(coeffs) < required || len(above) < cols {
		return ErrModeBufferTooSmall
	}

	for col := range cols {
		above[col] = TokenContextPlanes{}
	}
	for row := range rows {
		left := TokenContextPlanes{}
		for col := range cols {
			index := row*cols + col
			is4x4 := interModeUses4x4Tokens(modes[index].Mode)
			if modes[index].MBSkipCoeff {
				resetTokenContext(&above[col], &left, is4x4)
				continue
			}
			if !validInterCoefficientTokenMode(&modes[index]) {
				return ErrInvalidPacketConfig
			}
			if err := writeCoefficientMacroblockTokensWithEOBs(w, probs, is4x4, &above[col], &left, &coeffs[index]); err != nil {
				return err
			}
		}
	}
	if w.Err() != nil {
		return w.Err()
	}
	return nil
}

func WriteInterCoefficientTokenGridPartitioned(writers *[8]BoolWriter, partitions int, rows int, cols int, modes []InterFrameMacroblockMode, coeffs []MacroblockCoefficients, above []TokenContextPlanes, probs *tables.CoefficientProbs) error {
	if rows < 0 || cols < 0 {
		return ErrModeBufferTooSmall
	}
	if rows != 0 && cols > int(^uint(0)>>1)/rows {
		return ErrModeBufferTooSmall
	}
	required := rows * cols
	if writers == nil || probs == nil || len(modes) < required || len(coeffs) < required || len(above) < cols || partitions != 2 && partitions != 4 && partitions != 8 {
		return ErrModeBufferTooSmall
	}

	for col := range cols {
		above[col] = TokenContextPlanes{}
	}
	for row := range rows {
		w := &writers[row&(partitions-1)]
		left := TokenContextPlanes{}
		for col := range cols {
			index := row*cols + col
			is4x4 := interModeUses4x4Tokens(modes[index].Mode)
			if modes[index].MBSkipCoeff {
				resetTokenContext(&above[col], &left, is4x4)
				continue
			}
			if !validInterCoefficientTokenMode(&modes[index]) {
				return ErrInvalidPacketConfig
			}
			if err := writeCoefficientMacroblockTokensWithEOBs(w, probs, is4x4, &above[col], &left, &coeffs[index]); err != nil {
				return err
			}
		}
	}
	return nil
}

func resetTokenContext(above *TokenContextPlanes, left *TokenContextPlanes, is4x4 bool) {
	ResetTokenContextPlanes(above, left, is4x4)
}

func validInterCoefficientTokenMode(mode *InterFrameMacroblockMode) bool {
	if mode == nil {
		return false
	}
	refFrame := interFrameReference(mode)
	if refFrame == common.IntraFrame {
		return validInterIntraMacroblockMode(mode)
	}
	switch refFrame {
	case common.LastFrame, common.GoldenFrame, common.AltRefFrame:
	default:
		return false
	}
	return common.IsWholeInterMacroblockMode(mode.Mode) || validSplitMVMode(mode)
}

func interModeUses4x4Tokens(mode common.MBPredictionMode) bool {
	return mode == common.BPred || mode == common.SplitMV
}
