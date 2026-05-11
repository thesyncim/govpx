package govpx

import (
	"math/bits"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

// mvKernelSignShift splats an int's sign bit (-1 for negatives, 0
// otherwise) for the branchless |delta| trick used by the per-q
// motion-cost lookups in this file.
const mvKernelSignShift = bits.UintSize - 1

// fullPelLocalStats accumulates motion-search counters in local
// variables for the duration of a single search call. Flushing once
// at the end removes the per-candidate method-call overhead that
// `recordFullPelSAD` / `recordFullPelEarlyBreak` would otherwise add
// inside the hot search loop.
type fullPelLocalStats struct {
	sadCalls      int
	sadCandidates int
	batchCalls    int
	boundsRejects int
	earlyBreaks   int
}

func (l *fullPelLocalStats) flush(stats *interFrameMotionSearchStats) {
	if stats == nil {
		return
	}
	if l.sadCalls > 0 || l.sadCandidates > 0 || l.batchCalls > 0 {
		stats.fullPelSADCalls += l.sadCalls
		stats.fullPelSADCandidates += l.sadCandidates
		stats.fullPelBatchCalls += l.batchCalls
		if phase := stats.phase; phase != nil {
			phase.FullPelSADCalls += int64(l.sadCalls)
			phase.FullPelSADCandidates += int64(l.sadCandidates)
			phase.FullPelBatchCalls += int64(l.batchCalls)
		}
	}
	if l.boundsRejects > 0 {
		stats.fullPelBoundsRejects += l.boundsRejects
		if phase := stats.phase; phase != nil {
			phase.FullPelBoundsRejects += int64(l.boundsRejects)
		}
	}
	if l.earlyBreaks > 0 {
		stats.fullPelEarlyBreaks += l.earlyBreaks
		if phase := stats.phase; phase != nil {
			phase.FullPelEarlyBreaks += int64(l.earlyBreaks)
		}
	}
}

// boundsInteriorByPad reports whether (row, col) ± pad stays strictly
// inside the open bounds box. Mirrors `containsFullPelStrict` for the
// row±pad/col±pad corners; lets the kernel skip per-candidate bounds
// checks when an entire fixed-magnitude site neighbourhood is interior.
func boundsInteriorByPad(b interFrameFullPixelBounds, row int, col int, padRow int, padCol int) bool {
	return row-padRow > b.rowMin && row+padRow < b.rowMax &&
		col-padCol > b.colMin && col+padCol < b.colMax
}

// fullPelMVSADCostInline mirrors libvpxFullPelMVSADCost16FromDeltas
// but reads the pinned per-q SAD-cost table directly so the inner
// loop avoids the function-call overhead and table-lookup helper.
// Branchless |delta| then symmetric 255 cap keeps both lookups
// straight-line so the hex/diamond steps can keep their accumulator
// pipeline tight.
func fullPelMVSADCostInline(mvRow8 int, mvCol8 int, refRow8 int, refCol8 int, costs *[256]int) int {
	rd := mvRow8 - refRow8
	rdMask := rd >> mvKernelSignShift
	rd = min((rd^rdMask)-rdMask, 255)
	cd := mvCol8 - refCol8
	cdMask := cd >> mvKernelSignShift
	cd = min((cd^cdMask)-cdMask, 255)
	return (costs[rd] + costs[cd] + 128) >> 8
}

// hexSuperKernel runs libvpx-style vp8_hex_search (initial 6-point
// ring + 3-point next-checkpoints walk + 4-neighbour final refine)
// as a single flat function:
//
//   - stats live in local counters and flush once at exit so the
//     inner loop is free of `recordFullPelSAD` method-call overhead;
//   - the per-q MV-SAD-cost lookup is inlined against a pinned table
//     pointer so the inner loop has no helper call;
//   - the 6-point initial ring is dispatched as one x4 SAD + two
//     singles when the centre is interior, reusing the same x4 NEON
//     kernel that searchSites/refine already exploit;
//   - the 4-neighbour refine ring is dispatched as one x4 SAD when
//     interior;
//   - a single padding-based interior check per iteration replaces
//     the per-candidate `containsFullPel` test.
//
// Returned cost is the SAD+MV-SAD-cost walk cost; the variance is
// re-computed by the caller via interMotionFullPixelSearchReturnCost.
func hexSuperKernel(s *fullPelMotionSearch, best vp8enc.MotionVector, bestCost int) (vp8enc.MotionVector, int) {
	// Site tables in libvpx hex_search order. The 6-point ring and
	// the three-of-six next-checkpoint slices use ±2 magnitudes; the
	// four-neighbour refine uses ±1. Encoded as flat arrays so the
	// inner loop can use simple int adds.
	const padRing int = 2
	const padRefine int = 1
	hexDR := [6]int8{-1, 1, 2, 1, -1, -2}
	hexDC := [6]int8{-2, -2, 0, 2, 2, 0}
	// nextCheckpointIdx[k] = three indices into hex[] that form the
	// 3-point walk after best site `k`.
	nextCheckpointIdx := [6][3]int8{
		{5, 0, 1},
		{0, 1, 2},
		{1, 2, 3},
		{2, 3, 4},
		{3, 4, 5},
		{4, 5, 0},
	}
	neighborDR := [4]int8{0, -1, 1, 0}
	neighborDC := [4]int8{-1, 0, 0, 1}

	ctx := &s.ctx
	bounds := s.bounds
	refRow8 := s.refRow8
	refCol8 := s.refCol8
	costs := &libvpxFullPelMVSADComponentCost16[vp8common.ClampQIndex(s.qIndex)]

	var local fullPelLocalStats

	bestRow := int(best.Row) >> 3
	bestCol := int(best.Col) >> 3

	// --- Initial 6-point HEX ring -----------------------------------------
	bestSite := -1
	nextRow := bestRow
	nextCol := bestCol
	var sad4 [4]uint32
	if boundsInteriorByPad(bounds, bestRow, bestCol, padRing, padRing) {
		// All six candidates in-bounds — batch the first four via the
		// x4 NEON SAD so the source plane is read once per quad.
		r0, c0 := bestRow+int(hexDR[0]), bestCol+int(hexDC[0])
		r1, c1 := bestRow+int(hexDR[1]), bestCol+int(hexDC[1])
		r2, c2 := bestRow+int(hexDR[2]), bestCol+int(hexDC[2])
		r3, c3 := bestRow+int(hexDR[3]), bestCol+int(hexDC[3])
		if ctx.fullPelSADFull4(r0, c0, r1, c1, r2, c2, r3, c3, &sad4) {
			local.sadCalls++
			local.sadCandidates += 4
			local.batchCalls++
			rows := [4]int{r0, r1, r2, r3}
			cols := [4]int{c0, c1, c2, c3}
			for i := range 4 {
				sad := int(sad4[i])
				if sad < bestCost {
					cost := sad + fullPelMVSADCostInline(rows[i], cols[i], refRow8, refCol8, costs)
					if cost < bestCost {
						nextRow = rows[i]
						nextCol = cols[i]
						bestCost = cost
						bestSite = i
					}
				}
			}
		} else {
			// Edge of the bordered ref plane: fall through to the
			// generic per-candidate path so the slow gather still
			// covers the four candidates correctly.
			for i := range 4 {
				row := bestRow + int(hexDR[i])
				col := bestCol + int(hexDC[i])
				local.sadCalls++
				local.sadCandidates++
				sad := ctx.fullPelSADFull(row, col)
				if sad < bestCost {
					cost := sad + fullPelMVSADCostInline(row, col, refRow8, refCol8, costs)
					if cost < bestCost {
						nextRow = row
						nextCol = col
						bestCost = cost
						bestSite = i
					}
				}
			}
		}
		// Trailing two singles (indices 4 and 5).
		for i := 4; i < 6; i++ {
			row := bestRow + int(hexDR[i])
			col := bestCol + int(hexDC[i])
			local.sadCalls++
			local.sadCandidates++
			sad := ctx.fullPelSADFull(row, col)
			if sad < bestCost {
				cost := sad + fullPelMVSADCostInline(row, col, refRow8, refCol8, costs)
				if cost < bestCost {
					nextRow = row
					nextCol = col
					bestCost = cost
					bestSite = i
				}
			}
		}
	} else {
		// Per-candidate path: at least one ring site may straddle the
		// search bounds, so we keep the legacy guard order.
		for i := range 6 {
			row := bestRow + int(hexDR[i])
			col := bestCol + int(hexDC[i])
			if !bounds.containsFullPel(row, col) {
				local.boundsRejects++
				continue
			}
			local.sadCalls++
			local.sadCandidates++
			sad := ctx.fullPelSADFull(row, col)
			if sad < bestCost {
				cost := sad + fullPelMVSADCostInline(row, col, refRow8, refCol8, costs)
				if cost < bestCost {
					nextRow = row
					nextCol = col
					bestCost = cost
					bestSite = i
				}
			}
		}
	}

	// --- 3-point next-checkpoints walk -----------------------------------
	if bestSite >= 0 {
		bestRow = nextRow
		bestCol = nextCol
		k := bestSite
		for j := 1; j < 127; j++ {
			bestSite = -1
			nextRow = bestRow
			nextCol = bestCol
			chk := nextCheckpointIdx[k]
			if boundsInteriorByPad(bounds, bestRow, bestCol, padRing, padRing) {
				for i := range 3 {
					idx := int(chk[i])
					row := bestRow + int(hexDR[idx])
					col := bestCol + int(hexDC[idx])
					local.sadCalls++
					local.sadCandidates++
					sad := ctx.fullPelSADFull(row, col)
					if sad < bestCost {
						cost := sad + fullPelMVSADCostInline(row, col, refRow8, refCol8, costs)
						if cost < bestCost {
							nextRow = row
							nextCol = col
							bestCost = cost
							bestSite = i
						}
					}
				}
			} else {
				for i := range 3 {
					idx := int(chk[i])
					row := bestRow + int(hexDR[idx])
					col := bestCol + int(hexDC[idx])
					if !bounds.containsFullPel(row, col) {
						local.boundsRejects++
						continue
					}
					local.sadCalls++
					local.sadCandidates++
					sad := ctx.fullPelSADFull(row, col)
					if sad < bestCost {
						cost := sad + fullPelMVSADCostInline(row, col, refRow8, refCol8, costs)
						if cost < bestCost {
							nextRow = row
							nextCol = col
							bestCost = cost
							bestSite = i
						}
					}
				}
			}
			if bestSite < 0 {
				local.earlyBreaks++
				break
			}
			bestRow = nextRow
			bestCol = nextCol
			k += 5 + bestSite
			if k >= 12 {
				k -= 12
			} else if k >= 6 {
				k -= 6
			}
		}
	}

	// --- 4-neighbour final refine ----------------------------------------
	for range 8 {
		bestSite = -1
		nextRow = bestRow
		nextCol = bestCol
		if boundsInteriorByPad(bounds, bestRow, bestCol, padRefine, padRefine) {
			// Single x4 SAD batch covers the whole 4-neighbour ring.
			r0, c0 := bestRow+int(neighborDR[0]), bestCol+int(neighborDC[0])
			r1, c1 := bestRow+int(neighborDR[1]), bestCol+int(neighborDC[1])
			r2, c2 := bestRow+int(neighborDR[2]), bestCol+int(neighborDC[2])
			r3, c3 := bestRow+int(neighborDR[3]), bestCol+int(neighborDC[3])
			if ctx.fullPelSADFull4(r0, c0, r1, c1, r2, c2, r3, c3, &sad4) {
				local.sadCalls++
				local.sadCandidates += 4
				local.batchCalls++
				rows := [4]int{r0, r1, r2, r3}
				cols := [4]int{c0, c1, c2, c3}
				for i := range 4 {
					sad := int(sad4[i])
					if sad < bestCost {
						cost := sad + fullPelMVSADCostInline(rows[i], cols[i], refRow8, refCol8, costs)
						if cost < bestCost {
							nextRow = rows[i]
							nextCol = cols[i]
							bestCost = cost
							bestSite = i
						}
					}
				}
			} else {
				for i := range 4 {
					row := bestRow + int(neighborDR[i])
					col := bestCol + int(neighborDC[i])
					local.sadCalls++
					local.sadCandidates++
					sad := ctx.fullPelSADFull(row, col)
					if sad < bestCost {
						cost := sad + fullPelMVSADCostInline(row, col, refRow8, refCol8, costs)
						if cost < bestCost {
							nextRow = row
							nextCol = col
							bestCost = cost
							bestSite = i
						}
					}
				}
			}
		} else {
			for i := range 4 {
				row := bestRow + int(neighborDR[i])
				col := bestCol + int(neighborDC[i])
				if !bounds.containsFullPel(row, col) {
					local.boundsRejects++
					continue
				}
				local.sadCalls++
				local.sadCandidates++
				sad := ctx.fullPelSADFull(row, col)
				if sad < bestCost {
					cost := sad + fullPelMVSADCostInline(row, col, refRow8, refCol8, costs)
					if cost < bestCost {
						nextRow = row
						nextCol = col
						bestCost = cost
						bestSite = i
					}
				}
			}
		}
		if bestSite < 0 {
			local.earlyBreaks++
			break
		}
		bestRow = nextRow
		bestCol = nextCol
	}

	local.flush(s.stats)
	return vp8enc.MotionVector{
		Row: int16(bestRow * interFrameMVFullPixelStep),
		Col: int16(bestCol * interFrameMVFullPixelStep),
	}, bestCost
}
