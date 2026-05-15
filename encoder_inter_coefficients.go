package govpx

import (
	"unsafe"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
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
	coefProbs           *vp8tables.CoefficientProbs
	src                 vp8enc.SourceImage
	mbRow               int
	mbCol               int
	pred                *vp8common.Image
	aboveTok            *vp8enc.TokenContextPlanes
	leftTok             *vp8enc.TokenContextPlanes
	quant               *vp8enc.MacroblockQuant
	qIndex              int
	zbinOverQuant       int
	zbinModeBoost       int
	actZbinAdj          int
	rdMult              int
	rdDiv               int
	is4x4               bool
	splitPartitionValid bool
	splitPartition      uint8
	intra               bool
	fastQuant           bool
	optimize            bool
	collectOracle       bool
	collectStats        bool
	coeffs              *vp8enc.MacroblockCoefficients
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
	is4x4         bool
	intra         bool
	fastQuant     bool
	qIndex        int
	zbinOverQuant int
	zbinModeBoost int
	actZbinAdj    int
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
}

func (c *interRDCoeffCacheState) reset() {
	if c == nil {
		return
	}
	c.valid = false
}

func (e *VP8Encoder) resetInterRDCoeffCache() {
	e.interRDCoeffCacheSlots[0].reset()
	e.interRDCoeffCacheSlots[1].reset()
	e.interRDCoeffCacheWinner = 0
	e.interRDCoeffCacheScratchTarget = nil
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
	// interRDCoeffCacheWinner is toggled with ^= 1, so it is always 0 or
	// 1; AND-mask with 1 (pow2-1) elides the bounds check on the
	// [2]interRDCoeffCacheState array.
	winner := &e.interRDCoeffCacheSlots[e.interRDCoeffCacheWinner&1]
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
	if c == nil || !c.valid {
		return false
	}
	return c.mbRow == args.mbRow &&
		c.mbCol == args.mbCol &&
		c.is4x4 == args.is4x4 &&
		c.intra == args.intra &&
		c.fastQuant == args.fastQuant &&
		c.qIndex == args.qIndex &&
		c.zbinOverQuant == args.zbinOverQuant &&
		c.zbinModeBoost == args.zbinModeBoost &&
		c.actZbinAdj == args.actZbinAdj
}

func buildPredictedMacroblockCoefficients(args predictedMacroblockCoefficientArgs) {
	args.collectStats = false
	_ = buildPredictedMacroblockCoefficientsInternal(&args)
}

// buildPredictedMacroblockCoefficientsRD fuses per-MB residual gather,
// batched FDCT, per-block quantize+token-cost+context-update, and the
// Y2 second-order pass into one whole-MB pipeline. It uses batched FDCT
// (Y x16 and UV x8) plus a single in-bounds residual gather, mirroring libvpx
// vp8/encoder/encodemb.c vp8_encode_inter16x16 / vp8_encode_intra16x16
// where vp8_transform_mb -> vp8_quantize_mb -> tokenize_mb run as one
// coordinated pass.
//
// Output (coeffs.QCoeff, coeffs.EOB, trace side data, and returned
// predictedMacroblockRDStats) is byte-identical to the original per-block
// reference path.
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
	if args.coefProbs == nil || args.pred == nil || args.quant == nil || args.coeffs == nil {
		return stats
	}
	if args.cacheIn != nil && args.phaseStats != nil {
		args.phaseStats.InterRDCoeffCacheRequests++
	}
	return buildPredictedMacroblockCoefficientsWork(args)
}

func buildPredictedMacroblockCoefficientsWork(args *predictedMacroblockCoefficientArgs) predictedMacroblockRDStats {
	var stats predictedMacroblockRDStats
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
	actZbinAdj := args.actZbinAdj
	rdMult := args.rdMult
	rdDiv := args.rdDiv
	is4x4 := args.is4x4
	splitPartitionValid := is4x4 && args.splitPartitionValid && args.splitPartition < vp8tables.NumMBSplits
	splitPartition := args.splitPartition
	intra := args.intra
	fastQuant := args.fastQuant
	optimize := args.optimize
	collectOracle := oracleTraceBuild && args.collectOracle
	coeffs := args.coeffs
	collectStats := args.collectStats
	var y2Input [16]int16
	var y2Coeff [16]int16
	var dq [16]int16
	var splitYDist [16]int
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
					recordOracleY1DCEOB1(coeffs, block, libvpxY1DCWouldQuantizeNonzero(dct[0], &quant.Y1, zbinOverQuant, zbinModeBoost, actZbinAdj, fastQuant))
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
				blockDist := transformBlockError(dct, dqY)
				stats.distortionY += blockDist
				if splitPartitionValid {
					subset := int(vp8tables.MBSplits[splitPartition&3][block&15])
					splitYDist[subset&15] += blockDist
				}
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
				eob := quantizeEncodedBlockWithRDZbinAndActivity(coefProbs, qIndex, 3, ctx, 0, zbinOverQuant, zbinModeBoost, actZbinAdj, zbinOverQuant, rdMult, rdDiv, intra, fastQuant, optimize, dct, &quant.Y1, &coeffs.QCoeff[block], &dq)
				coeffs.SetBlockEOB(block, eob)
				if collectStats {
					stats.rateY += coefficientBlockTokenRate(coefProbs, 3, ctx, 0, &coeffs.QCoeff[block], eob)
					blockDist := transformBlockError(dct, &dq)
					stats.distortionY += blockDist
					if splitPartitionValid {
						subset := int(vp8tables.MBSplits[splitPartition&3][block&15])
						splitYDist[subset&15] += blockDist
					}
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
					recordOracleY1DCEOB1(coeffs, block, libvpxY1DCWouldQuantizeNonzero(dct[0], &quant.Y1, zbinOverQuant, zbinModeBoost, actZbinAdj, fastQuant))
				}
				a := block & 3
				l := (block & 0x0c) >> 2
				ctx := 0
				if needTokenContext {
					ctx = int(yAbove[a] + yLeft[l])
				}
				eob := quantizeEncodedBlockWithRDZbinAndActivity(coefProbs, qIndex, 0, ctx, 1, zbinOverQuant, zbinModeBoost, actZbinAdj, zbinOverQuant, rdMult, rdDiv, intra, fastQuant, optimize, dct, &quant.Y1, &coeffs.QCoeff[block], &dq)
				coeffs.QCoeff[block][0] = 0
				dq[0] = 0
				dct[0] = 0
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
		eob := quantizeEncodedBlockWithRDZbinAndActivity(coefProbs, qIndex, 1, int(y2Above+y2Left), 0, zbinOverQuant/2, zbinModeBoost, actZbinAdj, zbinOverQuant, rdMult, rdDiv, intra, fastQuant, optimize, &y2Coeff, &quant.Y2, &coeffs.QCoeff[24], &dq)
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
			if splitPartitionValid {
				stats.distortionY = splitMotionPartitionLumaDistortionFromSums(splitYDist, splitPartition)
			} else {
				stats.distortionY >>= 2
			}
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
			staleEOB := min(max(quantizeEncodedBlockWithRDZbinAndActivity(coefProbs, qIndex, 1, int(y2Above+y2Left), 0, zbinOverQuant/2, zbinModeBoost, actZbinAdj, zbinOverQuant, rdMult, rdDiv, intra, fastQuant, optimize, &staleY2Coeff, &quant.Y2, &staleY2Q, &staleY2DQ), 0), 16)
			recordOracleStaleY2(coeffs, uint8(staleEOB), staleY2Q)
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
		uvWidth, uvHeight := sourceImageUVDimensions(src)
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
		args.cacheOut.actZbinAdj = actZbinAdj
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
		return stats
	}

	for block := range 4 {
		dct := (*[16]int16)(uvDcts[block*16 : block*16+16])
		a, l := macroblockCoefficientUVContextIndex(16 + block)
		ctx := 0
		if needTokenContext {
			ctx = int(uvAbove[a] + uvLeft[l])
		}
		eob := quantizeEncodedBlockWithRDZbinAndActivity(coefProbs, qIndex, 2, ctx, 0, zbinOverQuant, zbinModeBoost, actZbinAdj, zbinOverQuant, rdMult, rdDiv, intra, fastQuant, optimize, dct, &quant.UV, &coeffs.QCoeff[16+block], &dq)
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
		eob = quantizeEncodedBlockWithRDZbinAndActivity(coefProbs, qIndex, 2, ctx, 0, zbinOverQuant, zbinModeBoost, actZbinAdj, zbinOverQuant, rdMult, rdDiv, intra, fastQuant, optimize, dctV, &quant.UV, &coeffs.QCoeff[20+block], &dq)
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
	return stats
}

// gatherMacroblockYResiduals4x4 writes the 16 luma 4x4 residuals of
// the macroblock at top-left (baseX,baseY) into out as 16 contiguous
// int16-per-block slabs in scan order (block 0 first, block 15 last,
// each block laid out row-major at stride 4). For the in-bounds case
// (the entire 16x16 MB lies inside src) it skips per-pixel coordinate
// clamping; otherwise it falls back to the per-block clamped path
// (same numeric behavior as fillPredictedResidual4x4).
