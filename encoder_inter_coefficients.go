package govpx

import (
	"unsafe"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

type predictedMacroblockRDStats struct {
	rateY        int
	rateUV       int
	distortionY  int
	distortionUV int
	tteob        int
}

type predictedMacroblockCoefficientArgs struct {
	coefProbs     *vp8tables.CoefficientProbs
	src           vp8enc.SourceImage
	mbRow         int
	mbCol         int
	pred          *vp8common.Image
	aboveTok      *vp8enc.TokenContextPlanes
	leftTok       *vp8enc.TokenContextPlanes
	quant         *vp8enc.MacroblockQuant
	qIndex        int
	zbinOverQuant int
	zbinModeBoost int
	is4x4         bool
	intra         bool
	fastQuant     bool
	optimize      bool
	collectOracle bool
	collectStats  bool
	coeffs        *vp8enc.MacroblockCoefficients
	// cacheOut, when non-nil, requests the picker → accepted-path
	// post-FDCT DCT cache to be populated. After the batched FDCT runs,
	// the function copies yDcts / uvDcts into the cache and marks it
	// valid. The accepted-path code can then pass cacheIn pointing at the
	// same buffer to skip predictor + residual gather + FDCT.
	cacheOut *interRDCoeffCacheState
	// cacheIn, when non-nil and the cache's mbRow/mbCol/is4x4/intra/
	// fastQuant/qIndex/zbin parameters match, requests the post-FDCT DCT
	// inputs to be loaded from the cache rather than recomputed. The
	// caller must verify cache validity (mbRow/mbCol/is4x4/intra/fastQuant/
	// qIndex/zbin parity) before passing it in. The cache is consumed
	// exactly once and remains valid for fall-back inspection until the
	// caller resets it.
	cacheIn *interRDCoeffCacheState
	// phaseStats, when non-nil, receives opt-in accepted-path coefficient
	// pipeline counters for govpx-bench phase reports.
	phaseStats *EncoderPhaseStats
}

// interRDCoeffCacheState stages the picker's post-FDCT residual DCT
// coefficients across the RD picker → accepted-path boundary in
// selectRDInterFrameModeDecision /
// buildReconstructingInterFrameCoefficientsWithSegmentation. The picker
// calls buildPredictedMacroblockCoefficientsRD on every candidate; when
// a candidate becomes the running best we swap it into the winner-cache
// slot (no copy — pointer swap). The accepted path then re-uses the
// cached yDcts/uvDcts arrays (16+8 = 24 4x4 DCT blocks of int16) so its
// own buildPredictedMacroblockCoefficients call skips predictor + residual
// gather + batched FDCT and re-runs only the per-block quantize (and
// trellis when optimize=true) starting from the same DCT inputs. This is
// byte-identical because the picker and accepted-path produce identical
// FDCT inputs whenever the winning mode's prediction matches (which it
// does — the accepted path replays the same mode via
// reconstructInterAnalysisMacroblock right before this call). The quant
// loop's per-block context evolution still runs end-to-end on top of the
// cached DCTs so all token-context outputs stay byte-identical.
type interRDCoeffCacheState struct {
	valid         bool
	coeffsValid   bool
	is4x4         bool
	intra         bool
	fastQuant     bool
	qIndex        int
	zbinOverQuant int
	zbinModeBoost int
	mbRow         int
	mbCol         int
	// YDCTs holds the 16 4x4 luma DCTs in the same scan order as
	// vp8enc.ForwardDCT4x4Batch writes them (block-major, 16 int16
	// per block). The snapshot is taken AFTER FDCT but BEFORE the
	// per-block quant loop zeroes dct[0] for non-4x4 luma blocks,
	// so YDCTs preserves the original DCs for the consumer's Y2 pass.
	YDCTs [16 * 16]int16
	// UVDCTs holds the 8 chroma DCTs (U0..U3 then V0..V3).
	UVDCTs [8 * 16]int16
	// coeffs holds the post-quantized coefficient package for picker
	// candidates whose quantizer output is reusable by the accepted path
	// (same quant identity, no trellis/optimizer dependency).
	coeffs vp8enc.MacroblockCoefficients
}

func (c *interRDCoeffCacheState) reset() {
	if c == nil {
		return
	}
	c.valid = false
	c.coeffsValid = false
}

// consumeInterRDCoeffCache returns the winner cache slot if it is valid.
// Returns nil when the cache is empty so the caller does not need to perform
// parity checks before falling back to the full coefficient build. Parity
// validation against the consumer's args is still performed inside
// buildPredictedMacroblockCoefficients via interRDCacheReusable, so a stale
// winner that survived the loop without becoming the actual winner is safely
// rejected there. The next picker invocation resets both slots, so keeping the
// valid bit set for this consumer does not leak across macroblocks.
func (e *VP8Encoder) consumeInterRDCoeffCache() *interRDCoeffCacheState {
	if e == nil {
		return nil
	}
	winner := &e.interRDCoeffCacheSlots[e.interRDCoeffCacheWinner]
	if !winner.valid {
		return nil
	}
	return winner
}

// interRDCacheReusable returns true when the picker → accepted-path DCT
// cache matches every parameter that contributes to FDCT output. Probs and
// optimize flags are NOT compared because they only affect post-quant
// (trellis) state, which the consumer re-runs end-to-end on top of the
// cached DCTs. fastQuant IS compared because it changes which quantize
// kernel runs and the cache currently only short-circuits the FDCT stage
// (not the quant stage) — but matching fastQuant is also a sanity guard
// against catching a picker run on a non-matching MB. The MB-position
// match guards against accidental cross-MB reuse if the picker scratch
// outlives a frame.
func interRDCacheReusable(c *interRDCoeffCacheState, args *predictedMacroblockCoefficientArgs) bool {
	if c == nil || !c.valid || args == nil {
		return false
	}
	return c.mbRow == args.mbRow &&
		c.mbCol == args.mbCol &&
		c.is4x4 == args.is4x4 &&
		c.intra == args.intra &&
		c.fastQuant == args.fastQuant &&
		c.qIndex == args.qIndex &&
		c.zbinOverQuant == args.zbinOverQuant &&
		c.zbinModeBoost == args.zbinModeBoost
}

func interRDCacheCoefficientsReusable(c *interRDCoeffCacheState, args *predictedMacroblockCoefficientArgs) bool {
	return interRDCacheReusable(c, args) &&
		c.coeffsValid &&
		!args.collectStats &&
		!args.collectOracle &&
		!args.optimize
}

func buildPredictedMacroblockCoefficients(args predictedMacroblockCoefficientArgs) {
	args.collectStats = false
	_ = buildPredictedMacroblockCoefficientsInternal(&args)
}

// buildPredictedMacroblockCoefficientsRD fuses per-MB residual gather,
// batched FDCT, per-block quantize+token-cost+context-update, and the
// Y2 second-order pass into one whole-MB pipeline. R11-C: replaces the
// per-block FDCT/quantize/token loop with batched FDCT (Y x16 and UV
// x8) + a single in-bounds residual gather, mirroring libvpx
// vp8/encoder/encodemb.c vp8_encode_inter16x16 / vp8_encode_intra16x16
// where vp8_transform_mb -> vp8_quantize_mb -> tokenize_mb run as one
// coordinated pass.
//
// Output (coeffs.QCoeff, coeffs.EOB, OracleY1DC*, OracleStaleY2*,
// returned predictedMacroblockRDStats) is byte-identical to the
// original per-block reference path.
func buildPredictedMacroblockCoefficientsRD(coefProbs *vp8tables.CoefficientProbs, src vp8enc.SourceImage, mbRow int, mbCol int, pred *vp8common.Image, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, qIndex int, zbinOverQuant int, zbinModeBoost int, is4x4 bool, intra bool, fastQuant bool, optimize bool, coeffs *vp8enc.MacroblockCoefficients) predictedMacroblockRDStats {
	return buildPredictedMacroblockCoefficientsInternal(&predictedMacroblockCoefficientArgs{
		coefProbs:     coefProbs,
		src:           src,
		mbRow:         mbRow,
		mbCol:         mbCol,
		pred:          pred,
		aboveTok:      aboveTok,
		leftTok:       leftTok,
		quant:         quant,
		qIndex:        qIndex,
		zbinOverQuant: zbinOverQuant,
		zbinModeBoost: zbinModeBoost,
		is4x4:         is4x4,
		intra:         intra,
		fastQuant:     fastQuant,
		optimize:      optimize,
		collectStats:  true,
		coeffs:        coeffs,
	})
}

func buildPredictedMacroblockCoefficientsInternal(args *predictedMacroblockCoefficientArgs) predictedMacroblockRDStats {
	var stats predictedMacroblockRDStats
	if args == nil {
		return stats
	}
	coefProbs := args.coefProbs
	src := args.src
	mbRow := args.mbRow
	mbCol := args.mbCol
	pred := args.pred
	aboveTok := args.aboveTok
	leftTok := args.leftTok
	quant := args.quant
	qIndex := args.qIndex
	zbinOverQuant := args.zbinOverQuant
	zbinModeBoost := args.zbinModeBoost
	is4x4 := args.is4x4
	intra := args.intra
	fastQuant := args.fastQuant
	optimize := args.optimize
	collectOracle := args.collectOracle
	coeffs := args.coeffs
	collectStats := args.collectStats
	if coefProbs == nil || pred == nil || quant == nil || coeffs == nil {
		return stats
	}
	if args.cacheIn != nil && args.phaseStats != nil {
		args.phaseStats.InterRDCoeffCacheRequests++
	}
	if interRDCacheCoefficientsReusable(args.cacheIn, args) {
		if args.phaseStats != nil {
			args.phaseStats.InterRDCoeffCacheCoeffHits++
		}
		*coeffs = args.cacheIn.coeffs
		return stats
	}
	var y2Input [16]int16
	var y2Coeff [16]int16
	var dq [16]int16
	var yAbove [4]uint8
	var yLeft [4]uint8
	var uvAbove [4]uint8
	var uvLeft [4]uint8
	var y2Above, y2Left uint8
	needTokenContext := collectStats || optimize
	if needTokenContext && aboveTok != nil {
		yAbove = aboveTok.Y1
		uvAbove = tokenUVContextArray(aboveTok)
		y2Above = aboveTok.Y2
	}
	if needTokenContext && leftTok != nil {
		yLeft = leftTok.Y1
		uvLeft = tokenUVContextArray(leftTok)
		y2Left = leftTok.Y2
	}

	// Whole-MB Y residual gather + batched FDCT. Mirrors libvpx
	// vp8_subtract_mby + vp8_transform_mb (16 fdct calls). When cacheIn
	// is valid for this MB, the per-block loop reads directly from
	// args.cacheIn.YDCTs which already holds the pre-DC-zero FDCT output
	// (the picker snapshot is taken before the loop runs). When cacheOut
	// is set, the FDCT writes into a stack-local buffer and a single
	// 512-byte snapshot is committed to args.cacheOut.YDCTs immediately
	// (before the per-block loop zeroes DCs). The stack-local buffer
	// keeps the per-block quant loop hot in L1 for non-winning candidates.
	cacheConsume := args.cacheIn != nil && interRDCacheReusable(args.cacheIn, args)
	if cacheConsume && args.phaseStats != nil {
		args.phaseStats.InterRDCoeffCacheDCTHits++
	}
	var yResiduals [16 * 16]int16
	var yDctsLocal [16 * 16]int16
	yDctsPtr := &yDctsLocal
	if cacheConsume {
		yDctsPtr = &args.cacheIn.YDCTs
	}
	if !cacheConsume {
		gatherMacroblockYResiduals4x4(src.Y, src.YStride, src.Width, src.Height, pred.Y, pred.YStride, mbCol*16, mbRow*16, yResiduals[:])
		vp8enc.ForwardDCT4x4Batch(yResiduals[:], yDctsPtr[:], 16)
	}
	if args.cacheOut != nil {
		args.cacheOut.YDCTs = *yDctsPtr
	}
	yDcts := yDctsPtr[:]

	if fastQuant {
		var yDQ [16 * 16]int16
		var yEOB [16]uint8
		yQuant := &quant.Y1DC
		blockType := 0
		skipDC := 1
		if is4x4 {
			yQuant = &quant.Y1
			blockType = 3
			skipDC = 0
		}
		for block := range 16 {
			dct := (*[16]int16)(yDcts[block*16 : block*16+16])
			y2Input[block] = dct[0]
			if !is4x4 {
				// Use quant.Y1 (not quant.Y1DC) because govpx's Y1DC dequant
				// table is normalized so dequant[0]=1 (the actual DC value
				// lives in the Y2 second-order block); the libvpx Y1quant[Q]
				// the encode path actually exercises has the proper DC at
				// slot 0, which govpx mirrors in quant.Y1.
				if collectOracle {
					coeffs.OracleY1DCEOB1[block] = libvpxY1DCWouldQuantizeNonzero(dct[0], &quant.Y1, zbinOverQuant, zbinModeBoost, fastQuant)
				}
				dct[0] = 0
			}
		}
		qY := unsafe.Slice((*int16)(unsafe.Pointer(&coeffs.QCoeff[0][0])), 16*16)
		vp8enc.FastQuantizeBlockBatch(yDcts, yQuant, qY, yDQ[:], yEOB[:], 16)
		for block := range 16 {
			dct := (*[16]int16)(yDcts[block*16 : block*16+16])
			dqY := (*[16]int16)(yDQ[block*16 : block*16+16])
			a := block & 3
			l := (block & 0x0c) >> 2
			ctx := 0
			if needTokenContext {
				ctx = int(yAbove[a] + yLeft[l])
			}
			eob := int(yEOB[block])
			coeffs.SetBlockEOB(block, eob)
			if collectStats {
				stats.rateY += coefficientBlockTokenRate(coefProbs, blockType, ctx, skipDC, &coeffs.QCoeff[block], eob)
				stats.distortionY += transformBlockError(dct, dqY)
				if eob > skipDC {
					stats.tteob++
				}
			}
			if needTokenContext {
				hasCoeffs := uint8(0)
				if eob > skipDC {
					hasCoeffs = 1
				}
				yAbove[a] = hasCoeffs
				yLeft[l] = hasCoeffs
			}
		}
	} else {
		for block := range 16 {
			dct := (*[16]int16)(yDcts[block*16 : block*16+16])
			if is4x4 {
				// Capture a local Y2-equivalent snapshot for direct helper
				// callers. The encoder path overwrites this from picker state.
				y2Input[block] = dct[0]
				a := block & 3
				l := (block & 0x0c) >> 2
				ctx := 0
				if needTokenContext {
					ctx = int(yAbove[a] + yLeft[l])
				}
				eob := quantizeEncodedBlock(coefProbs, qIndex, 3, ctx, 0, zbinOverQuant, zbinModeBoost, intra, fastQuant, optimize, dct, &quant.Y1, &coeffs.QCoeff[block], &dq)
				coeffs.SetBlockEOB(block, eob)
				if collectStats {
					stats.rateY += coefficientBlockTokenRate(coefProbs, 3, ctx, 0, &coeffs.QCoeff[block], eob)
					stats.distortionY += transformBlockError(dct, &dq)
					if eob > 0 {
						stats.tteob++
					}
				}
				if needTokenContext {
					hasCoeffs := uint8(0)
					if eob > 0 {
						hasCoeffs = 1
					}
					yAbove[a] = hasCoeffs
					yLeft[l] = hasCoeffs
				}
			} else {
				y2Input[block] = dct[0]
				// Use quant.Y1 (not quant.Y1DC) because govpx's Y1DC dequant
				// table is normalized so dequant[0]=1 (the actual DC value
				// lives in the Y2 second-order block); the libvpx Y1quant[Q]
				// the encode path actually exercises has the proper DC at
				// slot 0, which govpx mirrors in quant.Y1.
				if collectOracle {
					coeffs.OracleY1DCEOB1[block] = libvpxY1DCWouldQuantizeNonzero(dct[0], &quant.Y1, zbinOverQuant, zbinModeBoost, fastQuant)
				}
				dct[0] = 0
				a := block & 3
				l := (block & 0x0c) >> 2
				ctx := 0
				if needTokenContext {
					ctx = int(yAbove[a] + yLeft[l])
				}
				eob := quantizeEncodedBlock(coefProbs, qIndex, 0, ctx, 1, zbinOverQuant, zbinModeBoost, intra, fastQuant, optimize, dct, &quant.Y1DC, &coeffs.QCoeff[block], &dq)
				coeffs.SetBlockEOB(block, eob)
				if collectStats {
					stats.rateY += coefficientBlockTokenRate(coefProbs, 0, ctx, 1, &coeffs.QCoeff[block], eob)
					stats.distortionY += transformBlockError(dct, &dq)
					if eob > 1 {
						stats.tteob++
					}
				}
				if needTokenContext {
					hasCoeffs := uint8(0)
					if eob > 1 {
						hasCoeffs = 1
					}
					yAbove[a] = hasCoeffs
					yLeft[l] = hasCoeffs
				}
			}
		}
	}
	if !is4x4 {
		vp8enc.ForwardWalsh4x4(y2Input[:], 4, &y2Coeff)
		eob := quantizeEncodedBlockWithRDZbin(coefProbs, qIndex, 1, int(y2Above+y2Left), 0, zbinOverQuant/2, zbinModeBoost, zbinOverQuant, intra, fastQuant, optimize, &y2Coeff, &quant.Y2, &coeffs.QCoeff[24], &dq)
		coeffs.SetBlockEOB(24, eob)
		if collectStats {
			stats.rateY += coefficientBlockTokenRate(coefProbs, 1, int(y2Above+y2Left), 0, &coeffs.QCoeff[24], eob)
			y2Error := transformBlockError(&y2Coeff, &dq)
			stats.distortionY = ((stats.distortionY << 2) + y2Error) >> 4
			stats.tteob += eob
		}
	} else {
		coeffs.QCoeff[24] = [16]int16{}
		coeffs.SetBlockEOB(24, 0)
		if collectStats {
			stats.distortionY >>= 2
		}
		// Direct helper callers do not carry the RD picker's mutable Y2
		// block state. Populate a local trace-only snapshot here; the
		// encoder path overwrites it with the picker-carried snapshot from
		// the last whole-block mode that ran macro_block_yrd, matching
		// libvpx's stale xd->block[24]/eobs[24] state for SPLITMV/B_PRED.
		if collectOracle {
			var staleY2Coeff [16]int16
			var staleY2Q [16]int16
			var staleY2DQ [16]int16
			vp8enc.ForwardWalsh4x4(y2Input[:], 4, &staleY2Coeff)
			staleEOB := min(max(quantizeEncodedBlockWithRDZbin(coefProbs, qIndex, 1, int(y2Above+y2Left), 0, zbinOverQuant/2, zbinModeBoost, zbinOverQuant, intra, fastQuant, optimize, &staleY2Coeff, &quant.Y2, &staleY2Q, &staleY2DQ), 0), 16)
			coeffs.OracleStaleY2EOB = uint8(staleEOB)
			coeffs.OracleStaleY2QCoeff = staleY2Q
			coeffs.OracleStaleY2Set = true
		}
	}

	// Whole-MB UV residual gather + batched FDCT (8 blocks: U0..U3, V0..V3).
	// Mirrors libvpx vp8_subtract_mbuv + vp8_transform_mbuv (8 fdct calls).
	// Cache-consume short-circuit mirrors the Y path above; UV DCTs are
	// never mutated by the per-block loop, so no DC snapshot is needed.
	var uvResiduals [8 * 16]int16
	var uvDctsLocal [8 * 16]int16
	uvDctsPtr := &uvDctsLocal
	if cacheConsume {
		uvDctsPtr = &args.cacheIn.UVDCTs
	} else if args.cacheOut != nil {
		uvDctsPtr = &args.cacheOut.UVDCTs
	}
	if !cacheConsume {
		uvWidth := (src.Width + 1) >> 1
		uvHeight := (src.Height + 1) >> 1
		gatherMacroblockUVResiduals4x4(src.U, src.UStride, uvWidth, uvHeight, pred.U, pred.UStride, mbCol*8, mbRow*8, uvResiduals[0:64])
		gatherMacroblockUVResiduals4x4(src.V, src.VStride, uvWidth, uvHeight, pred.V, pred.VStride, mbCol*8, mbRow*8, uvResiduals[64:128])
		vp8enc.ForwardDCT4x4Batch(uvResiduals[:], uvDctsPtr[:], 8)
	}
	uvDcts := uvDctsPtr[:]
	if args.cacheOut != nil {
		// Cache stamping. Y/UV DCT buffers were written directly into the
		// cache slot via the yDctsPtr/uvDctsPtr aliases; here we only
		// commit metadata.
		args.cacheOut.is4x4 = is4x4
		args.cacheOut.intra = intra
		args.cacheOut.fastQuant = fastQuant
		args.cacheOut.qIndex = qIndex
		args.cacheOut.zbinOverQuant = zbinOverQuant
		args.cacheOut.zbinModeBoost = zbinModeBoost
		args.cacheOut.mbRow = mbRow
		args.cacheOut.mbCol = mbCol
		args.cacheOut.valid = true
	}

	if fastQuant {
		var uvDQ [8 * 16]int16
		var uvEOB [8]uint8
		qUV := unsafe.Slice((*int16)(unsafe.Pointer(&coeffs.QCoeff[16][0])), 8*16)
		vp8enc.FastQuantizeBlockBatch(uvDcts[:], &quant.UV, qUV, uvDQ[:], uvEOB[:], 8)
		for block := range 4 {
			dct := (*[16]int16)(uvDcts[block*16 : block*16+16])
			dqU := (*[16]int16)(uvDQ[block*16 : block*16+16])
			a, l := macroblockCoefficientUVContextIndex(16 + block)
			ctx := 0
			if needTokenContext {
				ctx = int(uvAbove[a] + uvLeft[l])
			}
			eob := int(uvEOB[block])
			coeffs.SetBlockEOB(16+block, eob)
			if collectStats {
				stats.rateUV += coefficientBlockTokenRate(coefProbs, 2, ctx, 0, &coeffs.QCoeff[16+block], eob)
				stats.distortionUV += transformBlockError(dct, dqU)
				stats.tteob += eob
			}
			if needTokenContext {
				hasCoeffs := uint8(0)
				if eob > 0 {
					hasCoeffs = 1
				}
				uvAbove[a] = hasCoeffs
				uvLeft[l] = hasCoeffs
			}

			dctV := (*[16]int16)(uvDcts[(4+block)*16 : (4+block)*16+16])
			dqV := (*[16]int16)(uvDQ[(4+block)*16 : (4+block)*16+16])
			a, l = macroblockCoefficientUVContextIndex(20 + block)
			ctx = 0
			if needTokenContext {
				ctx = int(uvAbove[a] + uvLeft[l])
			}
			eob = int(uvEOB[4+block])
			coeffs.SetBlockEOB(20+block, eob)
			if collectStats {
				stats.rateUV += coefficientBlockTokenRate(coefProbs, 2, ctx, 0, &coeffs.QCoeff[20+block], eob)
				stats.distortionUV += transformBlockError(dctV, dqV)
				stats.tteob += eob
			}
			if needTokenContext {
				hasCoeffs := uint8(0)
				if eob > 0 {
					hasCoeffs = 1
				}
				uvAbove[a] = hasCoeffs
				uvLeft[l] = hasCoeffs
			}
		}
		if collectStats {
			stats.distortionUV >>= 2
		}
		storeInterRDCacheCoefficients(args)
		return stats
	}

	for block := range 4 {
		dct := (*[16]int16)(uvDcts[block*16 : block*16+16])
		a, l := macroblockCoefficientUVContextIndex(16 + block)
		ctx := 0
		if needTokenContext {
			ctx = int(uvAbove[a] + uvLeft[l])
		}
		eob := quantizeEncodedBlock(coefProbs, qIndex, 2, ctx, 0, zbinOverQuant, zbinModeBoost, intra, fastQuant, optimize, dct, &quant.UV, &coeffs.QCoeff[16+block], &dq)
		coeffs.SetBlockEOB(16+block, eob)
		if collectStats {
			stats.rateUV += coefficientBlockTokenRate(coefProbs, 2, ctx, 0, &coeffs.QCoeff[16+block], eob)
			stats.distortionUV += transformBlockError(dct, &dq)
			stats.tteob += eob
		}
		if needTokenContext {
			hasCoeffs := uint8(0)
			if eob > 0 {
				hasCoeffs = 1
			}
			uvAbove[a] = hasCoeffs
			uvLeft[l] = hasCoeffs
		}

		dctV := (*[16]int16)(uvDcts[(4+block)*16 : (4+block)*16+16])
		a, l = macroblockCoefficientUVContextIndex(20 + block)
		ctx = 0
		if needTokenContext {
			ctx = int(uvAbove[a] + uvLeft[l])
		}
		eob = quantizeEncodedBlock(coefProbs, qIndex, 2, ctx, 0, zbinOverQuant, zbinModeBoost, intra, fastQuant, optimize, dctV, &quant.UV, &coeffs.QCoeff[20+block], &dq)
		coeffs.SetBlockEOB(20+block, eob)
		if collectStats {
			stats.rateUV += coefficientBlockTokenRate(coefProbs, 2, ctx, 0, &coeffs.QCoeff[20+block], eob)
			stats.distortionUV += transformBlockError(dctV, &dq)
			stats.tteob += eob
		}
		if needTokenContext {
			hasCoeffs := uint8(0)
			if eob > 0 {
				hasCoeffs = 1
			}
			uvAbove[a] = hasCoeffs
			uvLeft[l] = hasCoeffs
		}
	}
	if collectStats {
		stats.distortionUV >>= 2
	}
	storeInterRDCacheCoefficients(args)
	return stats
}

func storeInterRDCacheCoefficients(args *predictedMacroblockCoefficientArgs) {
	if args == nil || args.cacheOut == nil {
		return
	}
	args.cacheOut.coeffsValid = false
	if args.optimize || args.collectOracle || args.coeffs == nil {
		return
	}
	if args.coeffs != &args.cacheOut.coeffs {
		args.cacheOut.coeffs = *args.coeffs
	}
	args.cacheOut.coeffsValid = true
}

// gatherMacroblockYResiduals4x4 writes the 16 luma 4x4 residuals of
// the macroblock at top-left (baseX,baseY) into out as 16 contiguous
// int16-per-block slabs in scan order (block 0 first, block 15 last,
// each block laid out row-major at stride 4). For the in-bounds case
// (the entire 16x16 MB lies inside src) it skips per-pixel coordinate
// clamping; otherwise it falls back to the per-block clamped path
// (same numeric behavior as fillPredictedResidual4x4).
func gatherMacroblockYResiduals4x4(src []byte, srcStride int, width int, height int, pred []byte, predStride int, baseX int, baseY int, out []int16) {
	if baseY >= 0 && baseX >= 0 && baseY+16 <= height && baseX+16 <= width {
		srcEnd := (baseY+15)*srcStride + baseX + 15
		predEnd := (baseY+15)*predStride + baseX + 15
		if srcStride > 0 && predStride > 0 && srcEnd < len(src) && predEnd < len(pred) && len(out) >= 16*16 {
			gatherMacroblockYResiduals4x4Unchecked(unsafe.SliceData(src), srcStride, unsafe.SliceData(pred), predStride, baseX, baseY, unsafe.SliceData(out))
			return
		}
	}
	for block := range 16 {
		x := baseX + (block&3)*4
		y := baseY + (block>>2)*4
		fillPredictedResidual4x4Slice(src, srcStride, width, height, pred, predStride, x, y, out[block*16:block*16+16])
	}
}

func gatherMacroblockYResiduals4x4Unchecked(src *byte, srcStride int, pred *byte, predStride int, baseX int, baseY int, out *int16) {
	srcBase := (*byte)(unsafe.Add(unsafe.Pointer(src), baseY*srcStride+baseX))
	predBase := (*byte)(unsafe.Add(unsafe.Pointer(pred), baseY*predStride+baseX))
	dsp.ResidualGather16x16PtrFast(srcBase, srcStride, predBase, predStride, out)
}

// gatherMacroblockUVResiduals4x4 writes the 4 chroma 4x4 residuals of
// the 8x8 MB chroma block at top-left (baseX,baseY) into out (4 blocks,
// 16 int16 per block in scan order). Same fast/slow split as the Y
// gatherer.
func gatherMacroblockUVResiduals4x4(src []byte, srcStride int, width int, height int, pred []byte, predStride int, baseX int, baseY int, out []int16) {
	if baseY >= 0 && baseX >= 0 && baseY+8 <= height && baseX+8 <= width {
		srcEnd := (baseY+7)*srcStride + baseX + 7
		predEnd := (baseY+7)*predStride + baseX + 7
		if srcStride > 0 && predStride > 0 && srcEnd < len(src) && predEnd < len(pred) && len(out) >= 4*16 {
			gatherMacroblockUVResiduals4x4Unchecked(unsafe.SliceData(src), srcStride, unsafe.SliceData(pred), predStride, baseX, baseY, unsafe.SliceData(out))
			return
		}
	}
	for block := range 4 {
		x := baseX + (block&1)*4
		y := baseY + (block>>1)*4
		fillPredictedResidual4x4Slice(src, srcStride, width, height, pred, predStride, x, y, out[block*16:block*16+16])
	}
}

func gatherMacroblockUVResiduals4x4Unchecked(src *byte, srcStride int, pred *byte, predStride int, baseX int, baseY int, out *int16) {
	srcBase := (*byte)(unsafe.Add(unsafe.Pointer(src), baseY*srcStride+baseX))
	predBase := (*byte)(unsafe.Add(unsafe.Pointer(pred), baseY*predStride+baseX))
	dsp.ResidualGather8x8PtrFast(srcBase, srcStride, predBase, predStride, out)
}

func macroblockCoefficientsEmpty(coeffs *vp8enc.MacroblockCoefficients, is4x4 bool) bool {
	if coeffs.EOB[24] != 0 {
		return false
	}
	for i := range 16 {
		if (is4x4 && coeffs.EOB[i] != 0) || (!is4x4 && coeffs.EOB[i] > 1) {
			return false
		}
	}
	for i := 16; i < 24; i++ {
		if coeffs.EOB[i] != 0 {
			return false
		}
	}
	return true
}

func clearMacroblockCoefficients(coeffs *vp8enc.MacroblockCoefficients) {
	*coeffs = vp8enc.MacroblockCoefficients{}
}

func staticInterRDEncodeBreakout(src vp8enc.SourceImage, pred *vp8common.Image, mbRow int, mbCol int, quant *vp8enc.MacroblockQuant, encodeBreakout int) bool {
	breakout, _ := staticInterRDEncodeBreakoutDistortion(src, pred, mbRow, mbCol, quant, encodeBreakout)
	return breakout
}

func staticInterRDEncodeBreakoutDistortion(src vp8enc.SourceImage, pred *vp8common.Image, mbRow int, mbCol int, quant *vp8enc.MacroblockQuant, encodeBreakout int) (bool, int) {
	if encodeBreakout <= 0 || pred == nil || quant == nil {
		return false, 0
	}
	yAC := int(quant.Y1.Dequant[1])
	threshold := max((yAC*yAC)>>4, encodeBreakout)
	lumaVar, lumaSSE := macroblockLumaVarianceSSE(src, pred, mbRow, mbCol)
	if lumaSSE >= threshold {
		return false, 0
	}
	y2DC := int(quant.Y2.Dequant[0])
	dcError := lumaSSE - lumaVar
	if dcError >= (y2DC*y2DC)>>4 && (lumaSSE/2 <= lumaVar || dcError >= 64) {
		return false, 0
	}
	chromaSSE := macroblockChromaSSE(src, pred, mbRow, mbCol)
	return chromaSSE*2 < encodeBreakout, lumaSSE + chromaSSE
}

func staticInterFastEncodeBreakout(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mode *vp8enc.InterFrameMacroblockMode, quant *vp8enc.MacroblockQuant, encodeBreakout int, lumaSSE int) bool {
	if encodeBreakout <= 0 || ref == nil || mode == nil || quant == nil || mode.RefFrame == vp8common.IntraFrame || mode.Mode == vp8common.SplitMV {
		return false
	}
	yAC := int(quant.Y1.Dequant[1])
	threshold := max((yAC*yAC)>>4, encodeBreakout)
	if lumaSSE >= threshold {
		return false
	}
	chromaSSE, ok := macroblockChromaMotionSSE(src, ref, mbRow, mbCol, mode)
	return ok && chromaSSE*2 < encodeBreakout
}

func macroblockChromaMotionSSE(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mode *vp8enc.InterFrameMacroblockMode) (int, bool) {
	if ref == nil || mode == nil || mode.RefFrame == vp8common.IntraFrame || mode.Mode == vp8common.SplitMV {
		return 0, false
	}
	baseY := mbRow * 8
	baseX := mbCol * 8
	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	if baseY < 0 || baseX < 0 || baseY+8 > uvHeight || baseX+8 > uvWidth {
		return 0, false
	}
	srcUOff := baseY*src.UStride + baseX
	srcVOff := baseY*src.VStride + baseX
	if srcUOff < 0 || srcVOff < 0 ||
		srcUOff+7*src.UStride+7 >= len(src.U) ||
		srcVOff+7*src.VStride+7 >= len(src.V) {
		return 0, false
	}

	mvRow := chromaMotionVectorComponent(mode.MV.Row)
	mvCol := chromaMotionVectorComponent(mode.MV.Col)
	refY := baseY + (mvRow >> 3)
	refX := baseX + (mvCol >> 3)
	xOffset := mvCol & 7
	yOffset := mvRow & 7
	uPlane, uOrigin := referenceChromaPlane(ref.U, ref.UFull, ref.UOrigin)
	vPlane, vOrigin := referenceChromaPlane(ref.V, ref.VFull, ref.VOrigin)
	uOff, ok := referencePlaneBlockOffset(uPlane, ref.UStride, uOrigin, refY, refX, 8, 8, xOffset|yOffset != 0)
	if !ok {
		return 0, false
	}
	vOff, ok := referencePlaneBlockOffset(vPlane, ref.VStride, vOrigin, refY, refX, 8, 8, xOffset|yOffset != 0)
	if !ok {
		return 0, false
	}
	if xOffset|yOffset == 0 {
		return dsp.SSE8x8(uPlane[uOff:], ref.UStride, src.U[srcUOff:], src.UStride) +
			dsp.SSE8x8(vPlane[vOff:], ref.VStride, src.V[srcVOff:], src.VStride), true
	}
	_, uSSE := dsp.SubpelVariance8x8(uPlane[uOff:], ref.UStride, xOffset, yOffset, src.U[srcUOff:], src.UStride)
	_, vSSE := dsp.SubpelVariance8x8(vPlane[vOff:], ref.VStride, xOffset, yOffset, src.V[srcVOff:], src.VStride)
	return uSSE + vSSE, true
}

func chromaMotionVectorComponent(v int16) int {
	c := int(v)
	if c < 0 {
		c--
	} else {
		c++
	}
	return c / 2
}

func referenceChromaPlane(visible []byte, full []byte, origin int) ([]byte, int) {
	if len(full) != 0 {
		return full, origin
	}
	return visible, 0
}

func referencePlaneBlockOffset(plane []byte, stride int, origin int, y int, x int, width int, height int, subpel bool) (int, bool) {
	if len(plane) == 0 || stride <= 0 || width <= 0 || height <= 0 {
		return 0, false
	}
	if subpel {
		width++
		height++
	}
	off := origin + y*stride + x
	last := off + (height-1)*stride + width - 1
	if off < 0 || last < off || last >= len(plane) {
		return 0, false
	}
	return off, true
}

func macroblockChromaBufferSSE(src vp8enc.SourceImage, mbRow int, mbCol int, predU []byte, predUStride int, predV []byte, predVStride int) int {
	baseY := mbRow * 8
	baseX := mbCol * 8
	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	if baseY >= 0 && baseX >= 0 &&
		baseY+8 <= uvHeight && baseX+8 <= uvWidth &&
		len(predU) >= 7*predUStride+8 && len(predV) >= 7*predVStride+8 {
		srcOffset := baseY*src.UStride + baseX
		return dsp.SSE8x8(src.U[srcOffset:], src.UStride, predU, predUStride) +
			dsp.SSE8x8(src.V[baseY*src.VStride+baseX:], src.VStride, predV, predVStride)
	}

	sse := 0
	for row := range 8 {
		srcY := clampEncodeCoord(baseY+row, uvHeight)
		for col := range 8 {
			srcX := clampEncodeCoord(baseX+col, uvWidth)
			uDiff := int(src.U[srcY*src.UStride+srcX]) - int(predU[row*predUStride+col])
			vDiff := int(src.V[srcY*src.VStride+srcX]) - int(predV[row*predVStride+col])
			sse += uDiff*uDiff + vDiff*vDiff
		}
	}
	return sse
}

const (
	lastFrameZeroMVZbinBoost  = 6
	goldenAltZeroMVZbinBoost  = 12
	nonZeroInterModeZbinBoost = 4
	splitInterModeZbinBoost   = 0
	intraInterFrameZbinBoost  = 0
)

func interZbinModeBoost(mode *vp8enc.InterFrameMacroblockMode) int {
	if mode == nil || mode.RefFrame == vp8common.IntraFrame || mode.Mode >= vp8common.DCPred && mode.Mode <= vp8common.BPred {
		return intraInterFrameZbinBoost
	}
	switch mode.Mode {
	case vp8common.ZeroMV:
		if mode.RefFrame == vp8common.LastFrame {
			return lastFrameZeroMVZbinBoost
		}
		return goldenAltZeroMVZbinBoost
	case vp8common.SplitMV:
		return splitInterModeZbinBoost
	default:
		return nonZeroInterModeZbinBoost
	}
}

// libvpxY1DCWouldQuantizeNonzero returns 1 when libvpx's vp8_quantize_mb path
// would have produced a non-zero quantized DC for the given Y1DC quantizer
// on the supplied input coefficient dct0.
//
// Why: libvpx's transform_mb does NOT zero block[i].coeff[0] before
// vp8_quantize_mb, so vp8_fast_quantize_b_c / vp8_regular_quantize_b_c
// quantize the original Y-block DC against Y1DC's zbin/round/quant tables.
// When that quantization produces y != 0, libvpx records *d->eob = 1 even
// for an otherwise empty Y_NO_DC block. Later, vp8_inverse_transform_mby
// overwrites qcoeff[0] (with the inverse-Walsh DC) and
// vp8_dequant_idct_add_y_block memsets qcoeff[0..1] back to zero, but eob=1
// is preserved through the pipeline. The libvpx-side oracle reads this
// post-IDCT eob.
//
// govpx's pipeline zeroes dct[0] before quantize because Y_NO_DC tokenize
// starts at c=1 anyway, so coeffs.EOB[block] never carries that DC bump.
// This helper recovers the bump for the per-MB oracle trace so the
// scoreboard's eob_sum match-rate aligns with libvpx. The helper does NOT
// influence bitstream emission or reconstruction; the OracleY1DCEOB1 flag
// it populates is read only by emitOracleMBTrace.
//
// fastQuant selects between vp8_fast_quantize_b_c (no zbin gate) and
// vp8_regular_quantize_b_c (zbin gate at position 0, where zbin_boost[0]=0
// so only zbin_extra contributes). zbinOverQuant and zbinModeBoost mirror
// the macroblock-level fields fed to vp8_update_zbin_extra.
func libvpxY1DCWouldQuantizeNonzero(dct0 int16, quant *vp8enc.BlockQuant, zbinOverQuant int, zbinModeBoost int, fastQuant bool) uint8 {
	if quant == nil {
		return 0
	}
	z := int(dct0)
	if z == 0 {
		return 0
	}
	x := z
	if x < 0 {
		x = -x
	}
	if fastQuant {
		y := ((x + int(quant.Round[0])) * int(quant.QuantFast[0])) >> 16
		if y != 0 {
			return 1
		}
		return 0
	}
	zbin := int(quant.Zbin[0])
	zbin += int(quant.ZbinBoost[0])
	zbin += (int(quant.Dequant[1]) * (zbinOverQuant + zbinModeBoost)) >> 7
	if x < zbin {
		return 0
	}
	x += int(quant.Round[0])
	y := ((((x * int(quant.Quant[0])) >> 16) + x) * int(quant.QuantShift[0])) >> 16
	if y != 0 {
		return 1
	}
	return 0
}

func quantizeBlockWithZbin(coeff *[16]int16, quant *vp8enc.BlockQuant, qIndex int, zbinOverQuant int, zbinModeBoost int, qcoeff *[16]int16, dqcoeff *[16]int16) int {
	if coeff == nil || quant == nil || qcoeff == nil || dqcoeff == nil {
		return 0
	}
	eob := -1
	zeroRun := 0
	for pos := range 16 {
		rc := int(vp8tables.DefaultZigZag1D[pos])
		z := int(coeff[rc])
		if z == 0 {
			qcoeff[rc] = 0
			dqcoeff[rc] = 0
			if zeroRun < len(quant.ZbinBoost)-1 {
				zeroRun++
			}
			continue
		}

		x := z
		if x < 0 {
			x = -x
		}
		zbin := int(quant.Zbin[rc])
		zbin += int(quant.ZbinBoost[zeroRun])
		zbin += (int(quant.Dequant[1]) * (zbinOverQuant + zbinModeBoost)) >> 7
		if x < zbin {
			qcoeff[rc] = 0
			dqcoeff[rc] = 0
			if zeroRun < len(quant.ZbinBoost)-1 {
				zeroRun++
			}
			continue
		}

		x += int(quant.Round[rc])
		y := ((((x * int(quant.Quant[rc])) >> 16) + x) * int(quant.QuantShift[rc])) >> 16
		if z < 0 {
			y = -y
		}
		q := int16(y)
		qcoeff[rc] = q
		dqcoeff[rc] = q * quant.Dequant[rc]
		if y != 0 {
			eob = pos
			zeroRun = 0
		} else if zeroRun < len(quant.ZbinBoost)-1 {
			zeroRun++
		}
	}
	return eob + 1
}

func quantizeOptimizedBlock(coefProbs *vp8tables.CoefficientProbs, qIndex int, blockType int, ctx int, skipDC int, zbinOverQuant int, zbinModeBoost int, intra bool, coeff *[16]int16, quant *vp8enc.BlockQuant, qcoeff *[16]int16, dqcoeff *[16]int16) int {
	return quantizeOptimizedBlockWithRDZbin(coefProbs, qIndex, blockType, ctx, skipDC, zbinOverQuant, zbinModeBoost, zbinOverQuant, intra, coeff, quant, qcoeff, dqcoeff)
}

func quantizeOptimizedBlockWithRDZbin(coefProbs *vp8tables.CoefficientProbs, qIndex int, blockType int, ctx int, skipDC int, zbinOverQuant int, zbinModeBoost int, rdZbinOverQuant int, intra bool, coeff *[16]int16, quant *vp8enc.BlockQuant, qcoeff *[16]int16, dqcoeff *[16]int16) int {
	eob := quantizeBlockWithZbin(coeff, quant, qIndex, zbinOverQuant, zbinModeBoost, qcoeff, dqcoeff)
	eob = optimizeQuantizedBlock(coefProbs, qIndex, blockType, ctx, skipDC, rdZbinOverQuant, intra, coeff, quant, qcoeff, eob)
	dequantizeQuantizedBlock(quant, qcoeff, dqcoeff)
	return eob
}

func quantizeEncodedBlock(coefProbs *vp8tables.CoefficientProbs, qIndex int, blockType int, ctx int, skipDC int, zbinOverQuant int, zbinModeBoost int, intra bool, fastQuant bool, optimize bool, coeff *[16]int16, quant *vp8enc.BlockQuant, qcoeff *[16]int16, dqcoeff *[16]int16) int {
	return quantizeEncodedBlockWithRDZbin(coefProbs, qIndex, blockType, ctx, skipDC, zbinOverQuant, zbinModeBoost, zbinOverQuant, intra, fastQuant, optimize, coeff, quant, qcoeff, dqcoeff)
}

// quantizeEncodedBlockWithRDZbin keeps libvpx's Y2 split explicit: Y2 zbin
// thresholding uses zbin_over_quant/2, while the trellis optimizer scores with
// mb->rdmult computed from the full frame-level zbin_over_quant.
func quantizeEncodedBlockWithRDZbin(coefProbs *vp8tables.CoefficientProbs, qIndex int, blockType int, ctx int, skipDC int, zbinOverQuant int, zbinModeBoost int, rdZbinOverQuant int, intra bool, fastQuant bool, optimize bool, coeff *[16]int16, quant *vp8enc.BlockQuant, qcoeff *[16]int16, dqcoeff *[16]int16) int {
	if fastQuant {
		return vp8enc.FastQuantizeBlock(coeff, quant, qcoeff, dqcoeff)
	}
	if optimize {
		eob := quantizeOptimizedBlockWithRDZbin(coefProbs, qIndex, blockType, ctx, skipDC, zbinOverQuant, zbinModeBoost, rdZbinOverQuant, intra, coeff, quant, qcoeff, dqcoeff)
		if blockType == 1 && skipDC == 0 {
			eob = resetLibvpxSmallSecondOrderCoefficients(quant, qcoeff, dqcoeff, eob)
		}
		return eob
	}
	return quantizeBlockWithZbin(coeff, quant, qIndex, zbinOverQuant, zbinModeBoost, qcoeff, dqcoeff)
}

func quantizeDecisionBlock(fastQuant bool, coeff *[16]int16, quant *vp8enc.BlockQuant, qIndex int, zbinOverQuant int, zbinModeBoost int, qcoeff *[16]int16, dqcoeff *[16]int16) int {
	if fastQuant {
		return vp8enc.FastQuantizeBlock(coeff, quant, qcoeff, dqcoeff)
	}
	return quantizeBlockWithZbin(coeff, quant, qIndex, zbinOverQuant, zbinModeBoost, qcoeff, dqcoeff)
}

func dequantizeQuantizedBlock(quant *vp8enc.BlockQuant, qcoeff *[16]int16, dqcoeff *[16]int16) {
	if quant == nil || qcoeff == nil || dqcoeff == nil {
		return
	}
	for i := range 16 {
		dqcoeff[i] = qcoeff[i] * quant.Dequant[i]
	}
}

// optimizeQuantizedBlock ports libvpx v1.16.0 vp8/encoder/encodemb.c optimize_b.
// It walks the quantized block from eob-1 down to skipDC, builds a 2-state
// Viterbi trellis exploring (keep current value) vs (shift |x| toward 0 when
// the dequant boundary allows), scores transitions with libvpx's token_costs
// subtree elision, and applies the path that minimizes the libvpx RDCOST. Tied
// RDCOSTs use the libvpx RDTRUNC tie-break.
func optimizeQuantizedBlock(coefProbs *vp8tables.CoefficientProbs, qIndex int, blockType int, ctx int, skipDC int, zbinOverQuant int, intra bool, coeff *[16]int16, quant *vp8enc.BlockQuant, qcoeff *[16]int16, eob int) int {
	if coeff == nil || quant == nil || qcoeff == nil || eob <= skipDC {
		return eob
	}
	if blockType < 0 || blockType >= vp8tables.BlockTypes || ctx < 0 || ctx >= vp8tables.PrevCoefContexts || skipDC < 0 || skipDC > 1 {
		return eob
	}
	if coefProbs == nil {
		return eob
	}
	if eob > 16 {
		eob = 16
	}

	rdMult, rdDiv := libvpxRDConstantsWithZbin(qIndex, zbinOverQuant)
	rdMult *= blockPlaneRDMultiplier(blockType)
	if intra {
		rdMult = (rdMult * 9) >> 4
	}

	type tokenState struct {
		rate  int
		error int
		next  int8
		token int8
		qc    int16
	}
	var tokens [17][2]tokenState
	var bestMask [2]uint32

	tokens[eob][0] = tokenState{next: 16, token: int8(vp8tables.DCTEOBToken)}
	tokens[eob][1] = tokens[eob][0]
	next := eob

	for i := eob - 1; i >= skipDC; i-- {
		rc := int(vp8tables.DefaultZigZag1D[i])
		x := int(qcoeff[rc])
		if x != 0 {
			error0 := tokens[next][0].error
			error1 := tokens[next][1].error
			rate0 := tokens[next][0].rate
			rate1 := tokens[next][1].rate
			t0 := dctValueToken(x)

			if next < 16 {
				band := int(vp8tables.CoefBandsTable[i+1])
				pt := int(vp8tables.PrevTokenClass[t0])
				p := (*coefProbs)[blockType][band][pt]
				rate0 += coefficientTokenCost(p, int(tokens[next][0].token), blockType, band, pt)
				rate1 += coefficientTokenCost(p, int(tokens[next][1].token), blockType, band, pt)
			}

			rdCost0 := libvpxRDCost(rdMult, rdDiv, rate0, error0)
			rdCost1 := libvpxRDCost(rdMult, rdDiv, rate1, error1)
			if rdCost0 == rdCost1 {
				rdCost0 = libvpxRDTrunc(rdMult, rate0)
				rdCost1 = libvpxRDTrunc(rdMult, rate1)
			}
			best := 0
			if rdCost1 < rdCost0 {
				best = 1
			}

			baseBits := dctValueBaseCost(x)
			dq := int(quant.Dequant[rc])
			dx := x*dq - int(coeff[rc])
			d2 := dx * dx

			if best == 1 {
				tokens[i][0].rate = baseBits + rate1
				tokens[i][0].error = d2 + error1
			} else {
				tokens[i][0].rate = baseBits + rate0
				tokens[i][0].error = d2 + error0
			}
			tokens[i][0].next = int8(next)
			tokens[i][0].token = int8(t0)
			tokens[i][0].qc = int16(x)
			bestMask[0] |= uint32(best) << uint(i)

			rate0 = tokens[next][0].rate
			rate1 = tokens[next][1].rate

			absX := x
			if absX < 0 {
				absX = -absX
			}
			absC := int(coeff[rc])
			if absC < 0 {
				absC = -absC
			}
			shortcut := absX*dq > absC && absX*dq < absC+dq
			xs := x
			sz := 0
			if shortcut {
				if x < 0 {
					sz = -1
				}
				xs -= 2*sz + 1
			}

			var t1 int
			if xs == 0 {
				if int(tokens[next][0].token) == vp8tables.DCTEOBToken {
					t0 = vp8tables.DCTEOBToken
				} else {
					t0 = vp8tables.ZeroToken
				}
				if int(tokens[next][1].token) == vp8tables.DCTEOBToken {
					t1 = vp8tables.DCTEOBToken
				} else {
					t1 = vp8tables.ZeroToken
				}
			} else {
				t0 = dctValueToken(xs)
				t1 = t0
			}

			if next < 16 {
				band := int(vp8tables.CoefBandsTable[i+1])
				if t0 != vp8tables.DCTEOBToken {
					pt := int(vp8tables.PrevTokenClass[t0])
					p := (*coefProbs)[blockType][band][pt]
					rate0 += coefficientTokenCost(p, int(tokens[next][0].token), blockType, band, pt)
				}
				if t1 != vp8tables.DCTEOBToken {
					pt := int(vp8tables.PrevTokenClass[t1])
					p := (*coefProbs)[blockType][band][pt]
					rate1 += coefficientTokenCost(p, int(tokens[next][1].token), blockType, band, pt)
				}
			}

			rdCost0 = libvpxRDCost(rdMult, rdDiv, rate0, error0)
			rdCost1 = libvpxRDCost(rdMult, rdDiv, rate1, error1)
			if rdCost0 == rdCost1 {
				rdCost0 = libvpxRDTrunc(rdMult, rate0)
				rdCost1 = libvpxRDTrunc(rdMult, rate1)
			}
			best = 0
			if rdCost1 < rdCost0 {
				best = 1
			}

			baseBits = dctValueBaseCost(xs)

			d2s := d2
			if shortcut {
				dxs := dx - ((dq + sz) ^ sz)
				d2s = dxs * dxs
			}

			if best == 1 {
				tokens[i][1].rate = baseBits + rate1
				tokens[i][1].error = d2s + error1
				tokens[i][1].token = int8(t1)
			} else {
				tokens[i][1].rate = baseBits + rate0
				tokens[i][1].error = d2s + error0
				tokens[i][1].token = int8(t0)
			}
			tokens[i][1].next = int8(next)
			tokens[i][1].qc = int16(xs)
			bestMask[1] |= uint32(best) << uint(i)
			next = i
		} else {
			band := int(vp8tables.CoefBandsTable[i+1])
			p := (*coefProbs)[blockType][band][0]
			t0Tok := int(tokens[next][0].token)
			t1Tok := int(tokens[next][1].token)
			if t0Tok != vp8tables.DCTEOBToken {
				tokens[next][0].rate += coefficientTokenCost(p, t0Tok, blockType, band, 0)
				tokens[next][0].token = int8(vp8tables.ZeroToken)
			}
			if t1Tok != vp8tables.DCTEOBToken {
				tokens[next][1].rate += coefficientTokenCost(p, t1Tok, blockType, band, 0)
				tokens[next][1].token = int8(vp8tables.ZeroToken)
			}
		}
	}

	band := int(vp8tables.CoefBandsTable[skipDC])
	rate0 := tokens[next][0].rate
	rate1 := tokens[next][1].rate
	error0 := tokens[next][0].error
	error1 := tokens[next][1].error
	p := (*coefProbs)[blockType][band][ctx]
	rate0 += coefficientTokenCost(p, int(tokens[next][0].token), blockType, band, ctx)
	rate1 += coefficientTokenCost(p, int(tokens[next][1].token), blockType, band, ctx)
	rdCost0 := libvpxRDCost(rdMult, rdDiv, rate0, error0)
	rdCost1 := libvpxRDCost(rdMult, rdDiv, rate1, error1)
	if rdCost0 == rdCost1 {
		rdCost0 = libvpxRDTrunc(rdMult, rate0)
		rdCost1 = libvpxRDTrunc(rdMult, rate1)
	}
	best := 0
	if rdCost1 < rdCost0 {
		best = 1
	}

	finalEOB := skipDC - 1
	for i := next; i < eob; {
		x := tokens[i][best].qc
		if x != 0 {
			finalEOB = i
		}
		rc := int(vp8tables.DefaultZigZag1D[i])
		qcoeff[rc] = x
		nextI := int(tokens[i][best].next)
		best = int((bestMask[best] >> uint(i)) & 1)
		i = nextI
	}
	return finalEOB + 1
}

// libvpxRDTrunc mirrors the encodemb.c RDTRUNC macro used to break ties when
// two trellis paths have equal RDCOST.
func libvpxRDTrunc(rdMult int, rate int) int {
	return (128 + rate*rdMult) & 0xFF
}

// dctValueToken returns the libvpx coefficient-token classification for value x
// (mirrors the dct_value_tokens table indexed by signed value).
func dctValueToken(x int) int {
	abs := x
	if abs < 0 {
		abs = -abs
	}
	if abs == 0 {
		return vp8tables.ZeroToken
	}
	token, _, ok := coefficientTokenMagnitude(abs)
	if !ok {
		return vp8tables.ZeroToken
	}
	return token
}

// dctValueBaseCost mirrors libvpx's dct_value_cost table: extra bits cost plus
// sign bit cost for value x. The token-tree cost is added separately by the
// trellis using band/context-specific token costs.
func dctValueBaseCost(x int) int {
	if x == 0 {
		return 0
	}
	abs := x
	if abs < 0 {
		abs = -abs
	}
	token, _, ok := coefficientTokenMagnitude(abs)
	if !ok {
		return maxInt() / 4
	}
	cost := 0
	if x < 0 {
		cost += boolBitCost(128, 1)
	} else {
		cost += boolBitCost(128, 0)
	}
	cost += coefficientExtraBitsRate(token, abs)
	return cost
}

// Ported from libvpx v1.16.0 vp8/encoder/encodemb.c
// check_reset_2nd_coeffs. Very small Y2 residuals inverse-transform to a zero
// pixel delta, so libvpx drops the whole second-order block after optimization.
func resetLibvpxSmallSecondOrderCoefficients(quant *vp8enc.BlockQuant, qcoeff *[16]int16, dqcoeff *[16]int16, eob int) int {
	if quant == nil || qcoeff == nil || eob <= 0 {
		return eob
	}
	if quant.Dequant[0] >= 35 && quant.Dequant[1] >= 35 {
		return eob
	}
	sum := 0
	for pos := 0; pos < eob && pos < 16; pos++ {
		rc := int(vp8tables.DefaultZigZag1D[pos])
		coef := int(qcoeff[rc]) * int(quant.Dequant[rc])
		if coef < 0 {
			coef = -coef
		}
		sum += coef
		if sum >= 35 {
			return eob
		}
	}
	for pos := 0; pos < eob && pos < 16; pos++ {
		rc := int(vp8tables.DefaultZigZag1D[pos])
		qcoeff[rc] = 0
		if dqcoeff != nil {
			dqcoeff[rc] = 0
		}
	}
	return 0
}

func rdBlockScore(qIndex int, planeMultiplier int, intra bool, rate int, distortion int) int {
	return rdBlockScoreWithZbin(qIndex, 0, planeMultiplier, intra, rate, distortion)
}

func rdBlockScoreWithZbin(qIndex int, zbinOverQuant int, planeMultiplier int, intra bool, rate int, distortion int) int {
	if planeMultiplier <= 0 {
		planeMultiplier = 1
	}
	rdMult, rdDiv := libvpxRDConstantsWithZbin(qIndex, zbinOverQuant)
	rdMult *= planeMultiplier
	if intra {
		rdMult = (rdMult * 9) >> 4
	}
	return libvpxRDCost(rdMult, rdDiv, rate, distortion)
}

func blockPlaneRDMultiplier(blockType int) int {
	switch blockType {
	case 1:
		return 16
	case 2:
		return 2
	default:
		return 4
	}
}

func macroblockCoefficientTokenRate(probs *vp8tables.CoefficientProbs, is4x4 bool, coeffs *vp8enc.MacroblockCoefficients) int {
	return macroblockCoefficientTokenRateWithContext(probs, is4x4, nil, nil, coeffs)
}

func macroblockCoefficientTokenRateWithContext(probs *vp8tables.CoefficientProbs, is4x4 bool, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, coeffs *vp8enc.MacroblockCoefficients) int {
	if probs == nil || coeffs == nil {
		return maxInt() / 4
	}

	rate := 0
	blockType := 0
	skipDC := 0
	var yAbove [4]uint8
	var yLeft [4]uint8
	var uvAbove [4]uint8
	var uvLeft [4]uint8
	var y2Above, y2Left uint8
	if aboveTok != nil {
		yAbove = aboveTok.Y1
		uvAbove = tokenUVContextArray(aboveTok)
		y2Above = aboveTok.Y2
	}
	if leftTok != nil {
		yLeft = leftTok.Y1
		uvLeft = tokenUVContextArray(leftTok)
		y2Left = leftTok.Y2
	}
	if !is4x4 {
		eob := coeffs.BlockEOB(24, 0)
		rate += coefficientBlockTokenRate(probs, 1, int(y2Above+y2Left), 0, &coeffs.QCoeff[24], eob)
		blockType = 0
		skipDC = 1
	} else {
		blockType = 3
	}

	for block := range 16 {
		eob := coeffs.BlockEOB(block, skipDC)
		a := block & 3
		l := (block & 0x0c) >> 2
		ctx := int(yAbove[a] + yLeft[l])
		rate += coefficientBlockTokenRate(probs, blockType, ctx, skipDC, &coeffs.QCoeff[block], eob)
		hasCoeffs := uint8(0)
		if eob > skipDC {
			hasCoeffs = 1
		}
		yAbove[a] = hasCoeffs
		yLeft[l] = hasCoeffs
	}

	for block := 16; block < 24; block++ {
		eob := coeffs.BlockEOB(block, 0)
		a, l := macroblockCoefficientUVContextIndex(block)
		ctx := int(uvAbove[a] + uvLeft[l])
		rate += coefficientBlockTokenRate(probs, 2, ctx, 0, &coeffs.QCoeff[block], eob)
		hasCoeffs := uint8(0)
		if eob > 0 {
			hasCoeffs = 1
		}
		uvAbove[a] = hasCoeffs
		uvLeft[l] = hasCoeffs
	}
	return rate
}

func tokenUVContextArray(ctx *vp8enc.TokenContextPlanes) [4]uint8 {
	if ctx == nil {
		return [4]uint8{}
	}
	return [4]uint8{ctx.U[0], ctx.U[1], ctx.V[0], ctx.V[1]}
}

func macroblockCoefficientUVContextIndex(block int) (int, int) {
	base := 0
	if block > 19 {
		base = 2
	}
	a := base + (block & 1)
	l := base
	if block&3 > 1 {
		l++
	}
	return a, l
}
