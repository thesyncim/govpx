package encoder

import (
	"unsafe"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// VP9 per-leaf coefficient walker. Ported from libvpx v1.16.0
// vp9/common/vp9_blockd.h — vp9_foreach_transformed_block_in_plane
// driving the same tokenize_b inner kernel pack_mb_tokens replays.
//
// The walker iterates the Y plane then the U / V planes; per plane it
// projects the luma block size onto the chroma subsampling, picks the
// transform size (luma_tx_size for Y, get_uv_tx_size for UV), and
// walks the tx-block grid in scan order. For each tx block the
// initial coefficient context (band-0 ctx) is recomputed from the
// above + left entropy-context cache, the residual is emitted via
// WriteCoefBlock, and the above + left bytes are stamped with the
// (eob > 0) flag so the next neighbor read sees the right state.
//
// Scan order picking mirrors libvpx's get_scan: inter blocks, chroma planes,
// and lossless frames use the default scan; intra luma blocks select the
// DCT/ADST scan from the current Y mode.

const (
	compactCoefSlots = 64 * 64
	compactEOBSlots  = 16 * 16
)

// WriteCoefSbArgs bundles the inputs WriteCoefSb consults across the
// three planes of one leaf block.
type WriteCoefSbArgs struct {
	BSize common.BlockSize
	// MiTxSize is mi->tx_size for the luma plane. Chroma plane tx size
	// is derived via GetUvTxSize against MiTxSize + per-plane
	// subsampling.
	MiTxSize common.TxSize

	IsInter int

	// Lossless forces every tx block to the default scan, mirroring
	// libvpx's get_scan fallback for xd->lossless frames.
	Lossless bool

	// Mi is the leaf NeighborMi; consulted for the Y prediction mode
	// (per-block mode for sub-8x8) when picking the intra scan. May be
	// nil for inter blocks (which always take the default scan branch).
	Mi *vp9dec.NeighborMi

	// MiRows/MiCols and MiRow/MiCol clip transform-block emission at
	// right/bottom frame edges. Zero dimensions preserve the historical
	// full-block walk used by standalone unit tests.
	MiRows int
	MiCols int
	MiRow  int
	MiCol  int

	// Planes carries the per-plane macroblockd_plane shape (subsampling
	// + above/left entropy context buffers).
	Planes *[vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane

	// AboveOffsets / LeftOffsets are the entropy-context offsets the
	// caller has already advanced to for this leaf. AboveOffsets[p]
	// points at column 0 of plane p's residue context; LeftOffsets[p]
	// points at row 0 of plane p's residue context.
	AboveOffsets [vp9dec.MaxMbPlane]int
	LeftOffsets  [vp9dec.MaxMbPlane]int

	// PlaneDequant is the (DC, AC) dequant pair per plane.
	PlaneDequant [vp9dec.MaxMbPlane][2]int16

	// Fc is the active per-frame coefficient probability table.
	Fc *vp9dec.FrameCoefProbs

	// CoefBranchStats, when non-nil, receives coefficient branch counts
	// for every tx block emitted by this leaf.
	CoefBranchStats *FrameCoefBranchStats

	// GetCoeffs is called per tx block to fetch the dequantized
	// coefficient buffer in raster (NOT scan) order, sized to
	// MaxEobForTxSize(txSize) entries. (r, c) are 4x4-unit indices into
	// the plane.
	GetCoeffs func(plane int, r, c int, txSize common.TxSize) []int16

	// GetQCoeffs optionally returns the signed quantized coefficient
	// buffer in raster order for the same tx block. This mirrors libvpx
	// tokenize_b reading p->qcoeff; callers that only have dqcoeff can
	// leave it nil and fall back to magnitude recovery from Coeffs.
	GetQCoeffs func(plane int, r, c int, txSize common.TxSize) []int16

	// GetEOB optionally returns the quantizer-produced end-of-block value
	// for the same tx block. When absent, WriteCoefBlock falls back to
	// deriving EOB from coeff/qcoeff.
	GetEOB func(plane int, r, c int, txSize common.TxSize) (int, bool)

	// CompactQCoeffs / CompactEOBs provide the production tx-block-major
	// coefficient sidecar directly. When both stores are present the
	// walker derives the tx span from the grid it already owns and bypasses the
	// per-block callbacks above.
	CompactQCoeffs *[vp9dec.MaxMbPlane][compactCoefSlots]int16
	CompactEOBs    *[vp9dec.MaxMbPlane][compactEOBSlots]int16

	// TokenDst/TokenIndex opt into libvpx-shaped coefficient token staging.
	// When TokenOnly is false, WriteCoefSb stages each tx block then replays it
	// immediately, byte-matching the direct writer while exercising the staged
	// path. When TokenOnly is true, tokens are collected and not written; callers
	// replay them later after compressed-header probability updates.
	TokenDst   []TokenExtra
	TokenIndex *int
	TokenOnly  bool

	// TokenCache, when non-nil, is caller-owned (possibly dirty) persistent
	// scratch for the per-position token energy-class cache shared by every
	// tx block in this leaf. libvpx tokenize_b keeps the equivalent array
	// uninitialized across blocks (vp9/encoder/vp9_tokenize.c): the
	// scan-order walk writes each position before any neighbor context read
	// touches it, so dirty scratch is byte-equivalent and avoids a 1KB
	// zeroing per leaf. When nil a zeroed local is used.
	TokenCache *[1024]uint8
}

// scanForTxSize returns the default scan/neighbors pair for `tx`.
// Used as the unconditional fallback when there's no intra-mode
// signal to consult (inter blocks, chroma planes, lossless frames).
func scanForTxSize(tx common.TxSize) (scan, neighbors []int16) {
	o := &common.DefaultScanOrders[tx]
	return o.Scan, o.Neighbors
}

func planeMaxBlocks4x4(miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, pd *vp9dec.MacroblockdPlane,
	planeBsize common.BlockSize,
) (int, int) {
	w := int(common.Num4x4BlocksWideLookup[planeBsize])
	h := int(common.Num4x4BlocksHighLookup[planeBsize])
	if miRows <= 0 || miCols <= 0 {
		return w, h
	}
	mbToRightEdge := ((miCols - int(common.Num8x8BlocksWideLookup[bsize]) - miCol) *
		common.MiSize) * 8
	mbToBottomEdge := ((miRows - int(common.Num8x8BlocksHighLookup[bsize]) - miRow) *
		common.MiSize) * 8
	if mbToRightEdge < 0 {
		w += mbToRightEdge >> (5 + pd.SubsamplingX)
	}
	if mbToBottomEdge < 0 {
		h += mbToBottomEdge >> (5 + pd.SubsamplingY)
	}
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	return w, h
}

func stampCoefContexts(aboveCtx, leftCtx []uint8, v uint8) {
	switch len(aboveCtx) {
	case 8:
		_ = aboveCtx[7]
		_ = leftCtx[7]
		aboveCtx[0], leftCtx[0] = v, v
		aboveCtx[1], leftCtx[1] = v, v
		aboveCtx[2], leftCtx[2] = v, v
		aboveCtx[3], leftCtx[3] = v, v
		aboveCtx[4], leftCtx[4] = v, v
		aboveCtx[5], leftCtx[5] = v, v
		aboveCtx[6], leftCtx[6] = v, v
		aboveCtx[7], leftCtx[7] = v, v
	case 4:
		_ = aboveCtx[3]
		_ = leftCtx[3]
		aboveCtx[0], leftCtx[0] = v, v
		aboveCtx[1], leftCtx[1] = v, v
		aboveCtx[2], leftCtx[2] = v, v
		aboveCtx[3], leftCtx[3] = v, v
	case 2:
		_ = aboveCtx[1]
		_ = leftCtx[1]
		aboveCtx[0], leftCtx[0] = v, v
		aboveCtx[1], leftCtx[1] = v, v
	case 1:
		aboveCtx[0], leftCtx[0] = v, v
	default:
		for i := range aboveCtx {
			aboveCtx[i] = v
			leftCtx[i] = v
		}
	}
}

func stampCoefContextsAt(aboveCtx []uint8, aboveOff int, leftCtx []uint8, leftOff int, step int, v uint8) {
	switch step {
	case 8:
		_ = aboveCtx[aboveOff+7]
		_ = leftCtx[leftOff+7]
		stampCoefContextBytes(aboveCtx, aboveOff, leftCtx, leftOff, 8, v)
	case 4:
		_ = aboveCtx[aboveOff+3]
		_ = leftCtx[leftOff+3]
		stampCoefContextBytes(aboveCtx, aboveOff, leftCtx, leftOff, 4, v)
	case 2:
		_ = aboveCtx[aboveOff+1]
		_ = leftCtx[leftOff+1]
		stampCoefContextBytes(aboveCtx, aboveOff, leftCtx, leftOff, 2, v)
	case 1:
		_ = aboveCtx[aboveOff]
		_ = leftCtx[leftOff]
		stampCoefContextBytes(aboveCtx, aboveOff, leftCtx, leftOff, 1, v)
	default:
		stampCoefContexts(aboveCtx[aboveOff:aboveOff+step],
			leftCtx[leftOff:leftOff+step], v)
	}
}

func stampCoefContextBytes(aboveCtx []uint8, aboveOff int, leftCtx []uint8, leftOff int, n int, v uint8) {
	abovePtr := unsafe.Add(unsafe.Pointer(unsafe.SliceData(aboveCtx)), uintptr(aboveOff))
	leftPtr := unsafe.Add(unsafe.Pointer(unsafe.SliceData(leftCtx)), uintptr(leftOff))
	for i := range n {
		*(*uint8)(unsafe.Add(abovePtr, uintptr(i))) = v
		*(*uint8)(unsafe.Add(leftPtr, uintptr(i))) = v
	}
}

// yModeForBlock mirrors libvpx's get_y_mode — picks the Y mode for
// a sub-block index `block` from mi->bmi[] when sb_type is sub-8x8,
// otherwise returns mi->mode.
func yModeForBlock(mi *vp9dec.NeighborMi, block int) common.PredictionMode {
	if mi.SbType < common.Block8x8 {
		return mi.Bmi[block].AsMode
	}
	return mi.Mode
}

func compactCoefBlock(qcoeffs *[compactCoefSlots]int16,
	eobs *[compactEOBSlots]int16, planeBsize common.BlockSize,
	r, c int, txSize common.TxSize,
) ([]int16, int, bool) {
	if qcoeffs == nil || eobs == nil ||
		planeBsize < 0 || planeBsize >= common.BlockSizes ||
		txSize < 0 || txSize >= common.TxSizes || r < 0 || c < 0 {
		return nil, 0, false
	}
	full4x4W := int(common.Num4x4BlocksWideLookup[planeBsize])
	full4x4H := int(common.Num4x4BlocksHighLookup[planeBsize])
	shift := uint(txSize)
	step := 1 << shift
	if full4x4W <= 0 || full4x4H <= 0 || (r|c)&(step-1) != 0 ||
		r+step > full4x4H || c+step > full4x4W {
		return nil, 0, false
	}
	maxEob := 16 << (shift << 1)
	off := ((r>>shift)*(full4x4W>>shift) + (c >> shift)) * maxEob
	eobOff := r*full4x4W + c
	if off < 0 || off+maxEob > len(qcoeffs) ||
		eobOff < 0 || eobOff >= len(eobs) {
		return nil, 0, false
	}
	eob := int(eobs[eobOff])
	if eob < 0 || eob > maxEob {
		return nil, 0, false
	}
	return qcoeffs[off : off+maxEob], eob, true
}

// WriteCoefSb mirrors libvpx's per-block residue pack — the loop
// pack_mb_tokens replays after tokenize_b stages tokens for one
// leaf. Iterates the Y / U / V planes, walks each plane's tx-block
// grid in raster order, computes the initial coefficient context
// from the live above/left entropy context bytes, and emits each tx
// block's coefficient stream via WriteCoefBlock. Updates the
// above/left context bytes from (eob > 0) after each block so the
// next neighbor read sees the right state.
func WriteCoefSb(bw *bitstream.Writer, a WriteCoefSbArgs) error {
	return WriteCoefSbFromArgs(bw, &a)
}

// WriteCoefSbFromArgs avoids copying the leaf argument bundle when the caller
// already owns it behind a stable pointer.
func WriteCoefSbFromArgs(bw *bitstream.Writer, a *WriteCoefSbArgs) error {
	if a == nil {
		return ErrTokenBufferFull
	}
	// Shared token-cache scratch for every tx block in this leaf. The
	// scan-order walk writes each position before reading it as a neighbor
	// context (libvpx tokenize_b keeps this uninitialized), so a.TokenCache
	// may be dirty persistent caller scratch; the zeroed local is only a
	// fallback for callers that don't own one.
	tokenCache := a.TokenCache
	if tokenCache == nil {
		var local [1024]uint8
		tokenCache = &local
	}
	compactCoeffs := a.CompactQCoeffs != nil || a.CompactEOBs != nil
	if compactCoeffs && (a.CompactQCoeffs == nil || a.CompactEOBs == nil) {
		return ErrTokenBufferFull
	}
	for plane := range vp9dec.MaxMbPlane {
		pd := &a.Planes[plane]
		planeType := 0
		if plane > 0 {
			planeType = 1
		}

		var txSize common.TxSize
		if plane == 0 {
			txSize = a.MiTxSize
		} else {
			// SbType is recovered from the leaf-size argument the
			// dispatcher already passed in: when the walker is invoked
			// for a leaf, BSize is the leaf bsize (not the SB root).
			txSize = vp9dec.GetUvTxSize(a.BSize, a.MiTxSize, pd)
		}

		planeBsize := vp9dec.GetPlaneBlockSize(a.BSize, pd)
		num4x4W := int(common.Num4x4BlocksWideLookup[planeBsize])
		num4x4H := int(common.Num4x4BlocksHighLookup[planeBsize])
		if a.MiRows > 0 && a.MiCols > 0 {
			num4x4W, num4x4H = planeMaxBlocks4x4(a.MiRows, a.MiCols,
				a.MiRow, a.MiCol, a.BSize, pd, planeBsize)
		}
		step := 1 << uint(txSize)
		// Default-scan fallback: inter blocks, chroma planes, and
		// lossless frames all take it. Intra-Y blocks pick the per-tx
		// scan from the Y mode of the sub-block being walked.
		defaultScan, defaultNeighbors := scanForTxSize(txSize)
		dequant := a.PlaneDequant[plane]

		aboveBase := a.AboveOffsets[plane]
		leftBase := a.LeftOffsets[plane]

		blockIdx := 0
		for r := 0; r < num4x4H; r += step {
			for c := 0; c < num4x4W; c += step {
				aboveOff := aboveBase + c
				leftOff := leftBase + r
				aboveCtx := pd.AboveContext[aboveOff : aboveOff+step]
				leftCtx := pd.LeftContext[leftOff : leftOff+step]
				initCtx := vp9dec.GetEntropyContextFull(txSize, aboveCtx, leftCtx)

				scan, neighbors := defaultScan, defaultNeighbors
				if a.IsInter == 0 && planeType == 0 && !a.Lossless && a.Mi != nil {
					so := common.GetScanPtr(txSize, planeType, a.IsInter, a.Lossless,
						yModeForBlock(a.Mi, blockIdx))
					scan, neighbors = so.Scan, so.Neighbors
				}

				var coeffs []int16
				var qcoeffs []int16
				knownEOB, knownEOBValid := 0, false
				if compactCoeffs {
					var ok bool
					qcoeffs, knownEOB, ok = compactCoefBlock(&a.CompactQCoeffs[plane],
						&a.CompactEOBs[plane], planeBsize, r, c, txSize)
					if !ok {
						return ErrTokenBufferFull
					}
					knownEOBValid = true
				} else {
					if a.GetCoeffs != nil {
						coeffs = a.GetCoeffs(plane, r, c, txSize)
					}
					if a.GetQCoeffs != nil {
						qcoeffs = a.GetQCoeffs(plane, r, c, txSize)
					}
					if a.GetEOB != nil {
						knownEOB, knownEOBValid = a.GetEOB(plane, r, c, txSize)
					}
				}
				eob := 0
				maxEob := vp9dec.MaxEobForTxSize(txSize)
				blockArgs := WriteCoefBlockArgs{
					TxSize:          txSize,
					PlaneType:       planeType,
					IsInter:         a.IsInter,
					DequantDC:       dequant[0],
					DequantAC:       dequant[1],
					Scan:            scan,
					Neighbors:       neighbors,
					Coeffs:          coeffs,
					QCoeffs:         qcoeffs,
					Fc:              a.Fc,
					CoefBranchStats: a.CoefBranchStats,
					InitCtx:         initCtx,
					EOB:             &eob,
					KnownEOB:        knownEOB,
					KnownEOBValid:   knownEOBValid,
					TokenCache:      tokenCache,
				}
				if a.TokenIndex != nil {
					start := *a.TokenIndex
					if start < 0 || start > len(a.TokenDst) {
						return ErrTokenBufferFull
					}
					tokenWindow := a.TokenDst[start:]
					n, stagedEOB, ok := 0, 0, false
					if len(tokenWindow) >= maxEob {
						tokenWindow = tokenWindow[:maxEob]
						n, stagedEOB, ok = stageCoefBlockFullWindow(tokenWindow,
							blockArgs, maxEob)
					} else {
						n, stagedEOB, ok = StageCoefBlock(tokenWindow, blockArgs)
					}
					if !ok {
						return ErrTokenBufferFull
					}
					eob = stagedEOB
					*a.TokenIndex = start + n
					if !a.TokenOnly {
						PackTokens(bw, tokenWindow[:n], a.Fc)
					}
				} else {
					if err := WriteCoefBlock(bw, blockArgs); err != nil {
						return err
					}
				}

				hasResidue := uint8(0)
				if eob > 0 {
					hasResidue = 1
				}
				stampCoefContextsAt(pd.AboveContext, aboveOff,
					pd.LeftContext, leftOff, step, hasResidue)
				// libvpx's foreach_transformed_block_in_plane bumps the
				// block-counter `i` by step^2 per tx block — matching
				// the bmi[] index for sub-8x8 sub-block lookups.
				blockIdx += step * step
			}
		}
	}
	return nil
}

// CommitCoefSbContexts mirrors WriteCoefSb's transformed-block walk but only
// stamps above/left entropy contexts from each tx block's EOB. This is used by
// token replay: coefficient tokens have already been staged and packed, but the
// following leaf still needs the same live context state that WriteCoefSb would
// have produced.
func CommitCoefSbContexts(a WriteCoefSbArgs) error {
	for plane := range vp9dec.MaxMbPlane {
		pd := &a.Planes[plane]
		planeType := 0
		if plane > 0 {
			planeType = 1
		}

		var txSize common.TxSize
		if plane == 0 {
			txSize = a.MiTxSize
		} else {
			txSize = vp9dec.GetUvTxSize(a.BSize, a.MiTxSize, pd)
		}

		planeBsize := vp9dec.GetPlaneBlockSize(a.BSize, pd)
		num4x4W := int(common.Num4x4BlocksWideLookup[planeBsize])
		num4x4H := int(common.Num4x4BlocksHighLookup[planeBsize])
		if a.MiRows > 0 && a.MiCols > 0 {
			num4x4W, num4x4H = planeMaxBlocks4x4(a.MiRows, a.MiCols,
				a.MiRow, a.MiCol, a.BSize, pd, planeBsize)
		}
		step := 1 << uint(txSize)
		defaultScan, _ := scanForTxSize(txSize)

		aboveBase := a.AboveOffsets[plane]
		leftBase := a.LeftOffsets[plane]

		blockIdx := 0
		for r := 0; r < num4x4H; r += step {
			for c := 0; c < num4x4W; c += step {
				aboveOff := aboveBase + c
				leftOff := leftBase + r

				eob, eobValid := 0, false
				if a.GetEOB != nil {
					eob, eobValid = a.GetEOB(plane, r, c, txSize)
				}
				if !eobValid {
					scan := defaultScan
					if a.IsInter == 0 && planeType == 0 && !a.Lossless && a.Mi != nil {
						so := common.GetScanPtr(txSize, planeType, a.IsInter, a.Lossless,
							yModeForBlock(a.Mi, blockIdx))
						scan = so.Scan
					}
					coeffs := a.GetCoeffs(plane, r, c, txSize)
					var qcoeffs []int16
					if a.GetQCoeffs != nil {
						qcoeffs = a.GetQCoeffs(plane, r, c, txSize)
					}
					eob = coeffBlockEOBEncode(scan, vp9dec.MaxEobForTxSize(txSize),
						coeffs, qcoeffs)
				}

				hasResidue := uint8(0)
				if eob > 0 {
					hasResidue = 1
				}
				stampCoefContextsAt(pd.AboveContext, aboveOff,
					pd.LeftContext, leftOff, step, hasResidue)
				blockIdx += step * step
			}
		}
	}
	return nil
}

// CommitCoefSbContextsFromTokens mirrors CommitCoefSbContexts while deriving
// each tx block's nonzero/EOB state from a staged TOKENEXTRA stream. This is
// the pack-side equivalent of libvpx replaying tokenize_b output: the caller
// already has coefficient tokens, so it can advance above/left contexts without
// rescanning qcoeff/dqcoeff buffers.
func CommitCoefSbContextsFromTokens(a WriteCoefSbArgs, tokens []TokenExtra) error {
	cursor := 0
	for plane := range vp9dec.MaxMbPlane {
		pd := &a.Planes[plane]

		var txSize common.TxSize
		if plane == 0 {
			txSize = a.MiTxSize
		} else {
			txSize = vp9dec.GetUvTxSize(a.BSize, a.MiTxSize, pd)
		}

		planeBsize := vp9dec.GetPlaneBlockSize(a.BSize, pd)
		num4x4W := int(common.Num4x4BlocksWideLookup[planeBsize])
		num4x4H := int(common.Num4x4BlocksHighLookup[planeBsize])
		if a.MiRows > 0 && a.MiCols > 0 {
			num4x4W, num4x4H = planeMaxBlocks4x4(a.MiRows, a.MiCols,
				a.MiRow, a.MiCol, a.BSize, pd, planeBsize)
		}
		step := 1 << uint(txSize)
		maxEob := vp9dec.MaxEobForTxSize(txSize)

		aboveBase := a.AboveOffsets[plane]
		leftBase := a.LeftOffsets[plane]

		for r := 0; r < num4x4H; r += step {
			for c := 0; c < num4x4W; c += step {
				hasResidue, n, ok := stagedBlockHasResidue(tokens, cursor, maxEob)
				if !ok {
					return ErrTokenBufferFull
				}
				cursor += n

				v := uint8(0)
				if hasResidue {
					v = 1
				}
				stampCoefContextsAt(pd.AboveContext, aboveBase+c,
					pd.LeftContext, leftBase+r, step, v)
			}
		}
	}
	if cursor >= len(tokens) || tokens[cursor].Token != EOSBToken {
		return ErrTokenBufferFull
	}
	return nil
}

// PackTokensAndCommitCoefSbContexts replays one staged leaf's TOKENEXTRA
// stream and advances the above/left coefficient contexts from that same
// stream. This is the pack-side shape libvpx gets from tokenizing once during
// encode_sb and later walking TOKENEXTRA records in pack_mb_tokens: replay does
// not need to re-open qcoeff/dqcoeff buffers just to recover EOB state.
func PackTokensAndCommitCoefSbContexts(
	bw *bitstream.Writer, tokens []TokenExtra, fc *vp9dec.FrameCoefProbs,
	a WriteCoefSbArgs,
) (int, error) {
	cursor := 0
	for plane := range vp9dec.MaxMbPlane {
		pd := &a.Planes[plane]

		var txSize common.TxSize
		if plane == 0 {
			txSize = a.MiTxSize
		} else {
			txSize = vp9dec.GetUvTxSize(a.BSize, a.MiTxSize, pd)
		}

		planeBsize := vp9dec.GetPlaneBlockSize(a.BSize, pd)
		num4x4W := int(common.Num4x4BlocksWideLookup[planeBsize])
		num4x4H := int(common.Num4x4BlocksHighLookup[planeBsize])
		if a.MiRows > 0 && a.MiCols > 0 {
			num4x4W, num4x4H = planeMaxBlocks4x4(a.MiRows, a.MiCols,
				a.MiRow, a.MiCol, a.BSize, pd, planeBsize)
		}
		step := 1 << uint(txSize)
		maxEob := vp9dec.MaxEobForTxSize(txSize)

		aboveBase := a.AboveOffsets[plane]
		leftBase := a.LeftOffsets[plane]

		for r := 0; r < num4x4H; r += step {
			for c := 0; c < num4x4W; c += step {
				hasResidue, n, ok := packTokenBlockAndHasResidue(bw,
					tokens, cursor, maxEob, fc)
				if !ok {
					return cursor, ErrTokenBufferFull
				}
				cursor += n

				v := uint8(0)
				if hasResidue {
					v = 1
				}
				stampCoefContextsAt(pd.AboveContext, aboveBase+c,
					pd.LeftContext, leftBase+r, step, v)
			}
		}
	}
	if cursor >= len(tokens) || tokens[cursor].Token != EOSBToken {
		return cursor, ErrTokenBufferFull
	}
	return cursor + 1, nil
}

func stagedBlockHasResidue(tokens []TokenExtra, start, maxEob int) (bool, int, bool) {
	if maxEob <= 0 || start < 0 || start > len(tokens) {
		return false, 0, false
	}
	hasResidue := false
	end := start + maxEob
	c := start
	for c < end {
		if c >= len(tokens) {
			return false, 0, false
		}
		tok := tokens[c]
		if tok.Token == EOSBToken {
			return false, 0, false
		}
		if tok.Token == EobToken {
			return hasResidue, c - start + 1, true
		}
		if tok.Token != ZeroToken {
			hasResidue = true
		}
		c++
	}
	return hasResidue, maxEob, true
}
