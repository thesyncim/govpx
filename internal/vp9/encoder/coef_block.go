package encoder

import (
	"unsafe"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// WriteCoefBlockArgs bundles the inputs WriteCoefBlock consults.
//
// Layout mirrors libvpx's tokenize_b context: the coefficient
// stream is walked in scan order; band + ctx pick a probability
// row from `fc`; the per-position token cache feeds the next
// context.
type WriteCoefBlockArgs struct {
	TxSize    common.TxSize
	PlaneType int
	IsInter   int
	DequantDC int16
	DequantAC int16
	Scan      []int16
	Neighbors []int16

	// Coeffs are the dequantized residual coefficients in raster
	// order (indexed by scan[c]).
	Coeffs []int16

	// QCoeffs are the signed quantized coefficients in raster order.
	// When supplied, coefficient tokens are derived from qcoeff exactly
	// like libvpx's tokenize_b; Coeffs remain the reconstructed dqcoeff
	// plane consumed by inverse transforms and legacy callers.
	QCoeffs []int16

	// Fc carries the active per-frame coefficient probabilities.
	Fc *vp9dec.FrameCoefProbs

	// CoefBranchStats, when non-nil, receives the branch counts for
	// the coefficient token tree slots touched by this block. These
	// are the counts consumed by WriteCoefProbsFromCounts.
	CoefBranchStats *FrameCoefBranchStats

	// InitCtx is the band-0 coefficient context derived from the
	// above/left entropy-context cache via GetEntropyContext. Mirrors
	// libvpx's get_entropy_context result (0..2). Zero is correct
	// only when there's no neighbor residue (top-left of the SB or
	// directly after a skip block).
	InitCtx int

	// EOB, when non-nil, receives the computed end-of-block value so
	// callers that need residue presence can avoid rescanning coeffs.
	EOB *int

	// KnownEOB, when valid, is the quantizer-produced end-of-block value.
	// libvpx carries this through token staging, so callers that already have
	// qcoeff/eob can avoid rescanning the transform block before writing it.
	KnownEOB      int
	KnownEOBValid bool

	// TokenCache, when non-nil, is reusable (possibly dirty) scratch for the
	// per-position token energy-class cache. libvpx tokenize_b keeps this as
	// an uninitialized stack array: the scan-order walk writes every position
	// before any neighbor context read touches it, so no clearing between
	// blocks is required. When nil a zeroed local is used.
	TokenCache *[1024]uint8
}

// WriteCoefBlock emits the wire fragment for one transform block's
// coefficient stream. Mirrors the t >= TWO_TOKEN branch of
// libvpx's tokenize_b inverted into the encoder side: walks
// `Coeffs` in scan order, emitting non-EOB / non-ZERO / ONE-or-tree
// + sign for each non-zero entry, ZERO inside runs, and EOB once
// the trailing zeros begin. Returns the boolean-coded byte count
// written.
func WriteCoefBlock(bw *bitstream.Writer, a WriteCoefBlockArgs) error {
	maxEob := vp9dec.MaxEobForTxSize(a.TxSize)
	scan := a.Scan[:maxEob]
	bandTrans := vp9dec.BandTranslateForTxSize(a.TxSize)[:maxEob]
	_ = a.Neighbors[(maxEob<<1)-1]
	dq := [2]int16{a.DequantDC, a.DequantAC}
	qcoeffs := a.QCoeffs
	if len(qcoeffs) < maxEob {
		qcoeffs = nil
	}

	// Find EOB position: one past the last non-zero coefficient.
	eob := 0
	if a.KnownEOBValid && a.KnownEOB >= 0 && a.KnownEOB <= maxEob {
		eob = a.KnownEOB
	} else if qcoeffs != nil {
		eob = coeffBlockEOBCompleteQCoeffWindow(scan, maxEob, qcoeffs)
	} else {
		eob = coeffBlockEOBEncode(scan, maxEob, a.Coeffs, qcoeffs)
	}
	if a.EOB != nil {
		*a.EOB = eob
	}

	coefModel := &a.Fc[a.TxSize][a.PlaneType][a.IsInter]
	branchStatsRows := coefBranchStatsRowsFor(a.CoefBranchStats, a.TxSize,
		a.PlaneType, a.IsInter)
	tokenCache := a.TokenCache
	if tokenCache == nil {
		var local [1024]uint8
		tokenCache = &local
	}
	ctx := a.InitCtx

	c := 0
	for c < maxEob {
		band := int(bandTrans[c])
		probs := &coefModel[band][ctx]
		branchStats := coefBranchStatsSlot(branchStatsRows, band, ctx)
		if c == eob {
			recordCoefBranch00(branchStats)
			bw.Write(0, uint32(probs[0])) // EOB
			return nil
		}
		recordCoefBranch01(branchStats)
		bw.Write(1, uint32(probs[0])) // not EOB

		// ZERO inner loop: mirror the decoder, which reads only the
		// ZERO bit (no fresh EOB) for each zero in a run.
		raster := int(scan[c])
		for !coeffBlockHasCoeffAtRaster(raster, a.Coeffs, qcoeffs) {
			recordCoefBranch10(branchStats)
			bw.Write(0, uint32(probs[1])) // ZERO
			tokenCacheSet(tokenCache, raster, 0)
			c++
			if c >= maxEob {
				return nil
			}
			ctx = tokenCacheContext(a.Neighbors, tokenCache, c)
			band = int(bandTrans[c])
			probs = &coefModel[band][ctx]
			branchStats = coefBranchStatsSlot(branchStatsRows, band, ctx)
			raster = int(scan[c])
		}

		// Non-zero at c.
		recordCoefBranch11(branchStats)
		bw.Write(1, uint32(probs[1])) // not ZERO

		dqv := dq[1]
		if c == 0 {
			dqv = dq[0]
		}
		var absVal, sign int
		if qcoeffs != nil {
			absVal, sign = coeffMagnitudeAndSignQ(qcoeffAt(qcoeffs, raster))
		} else {
			absVal, sign = coeffMagnitudeAndSignDQ(a.Coeffs[raster],
				dqv, a.TxSize == common.Tx32x32)
		}
		writeTokenForCoeff(bw, probs, absVal, sign, branchStats)

		switch {
		case absVal == 1:
			tokenCacheSet(tokenCache, raster, 1)
		case absVal == 2:
			tokenCacheSet(tokenCache, raster, 2)
		case absVal == 3 || absVal == 4:
			tokenCacheSet(tokenCache, raster, 3)
		case absVal <= 10:
			tokenCacheSet(tokenCache, raster, 4)
		default:
			tokenCacheSet(tokenCache, raster, 5)
		}
		c++
		if c < maxEob {
			ctx = tokenCacheContext(a.Neighbors, tokenCache, c)
		}
	}
	return nil
}

func tokenCacheSet(tokenCache *[1024]uint8, raster int, v uint8) {
	tokenCache[raster&1023] = v
}

func tokenCacheContext(neighbors []int16, tokenCache *[1024]uint8, c int) int {
	off := c << 1
	// Production callers preflight neighbors for the full transform window.
	// The VP9 scan-neighbor tables contain token-cache positions in [0,1023],
	// mirroring libvpx's unchecked table indexing in get_coef_context.
	neighborPtr := unsafe.SliceData(neighbors)
	byteOff := uintptr(off) * unsafe.Sizeof(int16(0))
	aIdx := *(*int16)(unsafe.Add(unsafe.Pointer(neighborPtr), byteOff))
	bIdx := *(*int16)(unsafe.Add(unsafe.Pointer(neighborPtr), byteOff+unsafe.Sizeof(int16(0))))
	a := tokenCache[int(aIdx)&1023]
	b := tokenCache[int(bIdx)&1023]
	return (1 + int(a) + int(b)) >> 1
}

func qcoeffAt(qcoeffs []int16, raster int) int16 {
	// Full-window coefficient callers have already checked qcoeff length and
	// use VP9 scan tables whose raster entries are within the transform area.
	ptr := unsafe.SliceData(qcoeffs)
	return *(*int16)(unsafe.Add(unsafe.Pointer(ptr), uintptr(raster)*unsafe.Sizeof(int16(0))))
}

func coeffBlockEOBCompleteQCoeffWindow(scan []int16, maxEob int, qcoeffs []int16) int {
	_ = scan[maxEob-1]
	_ = qcoeffs[maxEob-1]
	for i := maxEob - 1; i >= 0; i-- {
		if qcoeffAt(qcoeffs, int(scan[i])) != 0 {
			return i + 1
		}
	}
	return 0
}

type coefBranchStatsRows = [vp9dec.CoefBands][vp9dec.CoefContexts][EntropyNodes][2]uint32

func coefBranchStatsRowsFor(
	stats *FrameCoefBranchStats, tx common.TxSize, planeType, isInter int,
) *coefBranchStatsRows {
	if stats == nil {
		return nil
	}
	return &stats[tx][planeType][isInter]
}

func coefBranchStatsSlot(
	stats *coefBranchStatsRows, band, ctx int,
) *[EntropyNodes][2]uint32 {
	if stats == nil {
		return nil
	}
	// band is from the VP9 coefficient band tables and ctx is the
	// coefficient context (0..CoefContexts-1), matching the unchecked
	// libvpx count-slot access in tokenize_b.
	bandStride := unsafe.Sizeof([vp9dec.CoefContexts][EntropyNodes][2]uint32{})
	ctxStride := unsafe.Sizeof([EntropyNodes][2]uint32{})
	off := uintptr(band)*bandStride + uintptr(ctx)*ctxStride
	return (*[EntropyNodes][2]uint32)(unsafe.Add(unsafe.Pointer(stats), off))
}
