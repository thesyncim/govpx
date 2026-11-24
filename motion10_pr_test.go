// Frozen reference: 870699ce83f3eb6de44f507ef29f6d6a429e916d.
package govpx

import (
	"fmt"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"testing"
)

func (ctx *loopFilterPickContext) motion10ParentPickFastNoStats(seedLevel uint8, minLevel int) (uint8, error) {
	e := ctx.encoder
	traceEnabled := oracleTraceBuild && e.oracleTraceEnabled()
	maxLevel := e.libvpxMaxLoopFilterLevelForFrame()
	ssErr := [vp8common.MaxLoopFilter + 1]int{}
	ssSet := [vp8common.MaxLoopFilter + 1]bool{}
	level := vp8enc.ClampLoopFilterPickLevel(int(seedLevel), minLevel, maxLevel)
	bestLevel := level
	bestErr := ctx.cachedPartialLumaSSE(level, &ssErr, &ssSet)
	if traceEnabled {
		e.emitOracleLFTrial("seed", level, bestErr)
	}

	filtLevel := level - vp8enc.LoopFilterSearchStep(level)
	for filtLevel >= minLevel {
		filtErr := ctx.cachedPartialLumaSSE(filtLevel, &ssErr, &ssSet)
		if traceEnabled {
			e.emitOracleLFTrial("down", filtLevel, filtErr)
		}
		if filtErr < bestErr {
			bestErr = filtErr
			bestLevel = filtLevel
		} else {
			break
		}
		filtLevel -= vp8enc.LoopFilterSearchStep(filtLevel)
	}

	filtLevel = level + vp8enc.LoopFilterSearchStep(filtLevel)
	if bestLevel == level {
		bestErr -= bestErr >> 10
		for filtLevel < maxLevel {
			filtErr := ctx.cachedPartialLumaSSE(filtLevel, &ssErr, &ssSet)
			if traceEnabled {
				e.emitOracleLFTrial("up", filtLevel, filtErr)
			}
			if filtErr < bestErr {
				bestErr = filtErr - (filtErr >> 10)
				bestLevel = filtLevel
			} else {
				break
			}
			filtLevel += vp8enc.LoopFilterSearchStep(filtLevel)
		}
	}
	return uint8(vp8enc.ClampLoopFilterPickLevel(bestLevel, minLevel, maxLevel)), nil
}
func (ctx *loopFilterPickContext) motion10ParentPickFullNoStats(seedLevel uint8, minLevel int) (uint8, error) {
	e := ctx.encoder
	traceEnabled := oracleTraceBuild && e.oracleTraceEnabled()
	if ctx.fullFrameConfig.SegmentationEnabled {
		e.loopFilterSegmentLF = ctx.fullFrameConfig.SegmentLF
	}
	maxLevel := e.libvpxMaxLoopFilterLevelForFrame()
	filtMid := vp8enc.ClampLoopFilterPickLevel(int(seedLevel), minLevel, maxLevel)
	filterStep := 4
	if filtMid >= 16 {
		filterStep = filtMid / 4
	}
	ssErr := [vp8common.MaxLoopFilter + 1]int{}
	ssSet := [vp8common.MaxLoopFilter + 1]bool{}
	residentLevel := -1
	bestLumaLevel := -1

	bestErr := ctx.cachedFullLumaSSE(filtMid, &ssErr, &ssSet, &residentLevel)
	if traceEnabled {
		e.emitOracleLFTrial("full", filtMid, bestErr)
	}
	filtBest := filtMid
	filtDirection := 0
	for filterStep > 0 {
		// Mirror libvpx vp8/encoder/picklpf.c vp8cx_pick_filter_level
		// (Bias = (best_err >> (15 - (filt_mid / 8))) * filter_step), then
		// scale that bias by section_intra_rating / 20 when the intra
		// rating is below 20. One-pass and realtime paths can therefore
		// use zero bias because libvpx's calloc'd two-pass state leaves
		// section_intra_rating at zero.
		bias := vp8enc.LoopFilterFullPickerBias(bestErr, filtMid, filterStep, e.twoPass.sectionIntraRating)
		filtHigh := min(filtMid+filterStep, maxLevel)
		filtLow := max(filtMid-filterStep, minLevel)

		// Parallel filt_low / filt_high trials at Threads >= 2 when this
		// iteration would otherwise run both serially (filtDirection == 0,
		// both bracket levels distinct from filtMid, neither cached). The
		// filt_low trial runs on the row-worker pool against the alt
		// scratch (loopFilterPickAlt + loopInfoAlt); the filt_high trial
		// runs inline on the calling goroutine against the resident
		// scratch (loopFilterPick + loopInfo). After both finish the
		// score-update conditionals are applied in the same serial order
		// as the non-parallel branch, so bestErr / filtBest converge
		// byte-identically to the serial run. residentLevel is set to
		// filtHigh to match the post-iteration state of the serial code.
		// loopFilterPick (resident scratch) holds the filtHigh-filtered
		// luma; loopFilterPickAlt holds the filtLow-filtered luma. If
		// filtLow becomes the new filtBest we patch loopFilterBest with
		// the alt-scratch luma so the chroma post-pass reuse decision in
		// applyReconstructionLoopFilter sees the correct preserved image
		// (mirroring the serial preserveBestBeforeTrial-before-filtHigh
		// copy that would have run had the trials been serial).
		// ssSet is [MaxLoopFilter+1=64]bool (pow2); LF trial levels are
		// clamped to [0, MaxLoopFilter]. AND-mask with 63 elides the
		// per-iter bounds check on both lookups.
		if filtDirection == 0 && filtLow != filtMid && filtHigh != filtMid &&
			!ssSet[filtLow&63] && !ssSet[filtHigh&63] && e.canParallelLFTrials() {
			ctx.preserveBestLoopFilterLumaBeforeTrial(filtLow, filtBest, &ssSet, residentLevel, &bestLumaLevel)
			filtErrLow, filtErrHigh := ctx.dispatchLFTrialPair(filtLow, filtHigh)
			// ssErr/ssSet are [MaxLoopFilter+1=64]; mask with 63 to elide
			// the bounds check on the indexed writes (matches the read
			// path above).
			ssErr[filtLow&63] = filtErrLow
			ssSet[filtLow&63] = true
			ssErr[filtHigh&63] = filtErrHigh
			ssSet[filtHigh&63] = true
			if filtHigh != 0 {
				residentLevel = filtHigh
			}
			if traceEnabled {
				e.emitOracleLFTrial("full", filtLow, filtErrLow)
				e.emitOracleLFTrial("full", filtHigh, filtErrHigh)
			}
			if filtErrLow-bias < bestErr {
				if filtErrLow < bestErr {
					bestErr = filtErrLow
				}
				filtBest = filtLow
				// Replicate the serial "preserveBestBeforeTrial(filtHigh,
				// filtBest=filtLow)" copy that would have run between
				// the two trials: residentLevel == filtBest == filtLow,
				// so loopFilterPick (which would hold the filtLow-filtered
				// luma in serial) gets copied to loopFilterBest. In
				// parallel that filtLow-filtered luma is in
				// loopFilterPickAlt instead, so we copy from there.
				if filtBest > 0 && bestLumaLevel != filtBest {
					vp8common.CopyImageLuma(&e.loopFilterBest.Img, &e.loopFilterPickAlt.Img)
					bestLumaLevel = filtBest
				}
			}
			if filtErrHigh < bestErr-bias {
				bestErr = filtErrHigh
				filtBest = filtHigh
			}
		} else {
			if filtDirection <= 0 && filtLow != filtMid {
				ctx.preserveBestLoopFilterLumaBeforeTrial(filtLow, filtBest, &ssSet, residentLevel, &bestLumaLevel)
				freshTrial := !ssSet[filtLow]
				filtErr := ctx.cachedFullLumaSSE(filtLow, &ssErr, &ssSet, &residentLevel)
				if traceEnabled && freshTrial {
					e.emitOracleLFTrial("full", filtLow, filtErr)
				}
				if filtErr-bias < bestErr {
					if filtErr < bestErr {
						bestErr = filtErr
					}
					filtBest = filtLow
				}
			}
			if filtDirection >= 0 && filtHigh != filtMid {
				ctx.preserveBestLoopFilterLumaBeforeTrial(filtHigh, filtBest, &ssSet, residentLevel, &bestLumaLevel)
				freshTrial := !ssSet[filtHigh]
				filtErr := ctx.cachedFullLumaSSE(filtHigh, &ssErr, &ssSet, &residentLevel)
				if traceEnabled && freshTrial {
					e.emitOracleLFTrial("full", filtHigh, filtErr)
				}
				if filtErr < bestErr-bias {
					bestErr = filtErr
					filtBest = filtHigh
				}
			}
		}
		if filtBest == filtMid {
			filterStep /= 2
			filtDirection = 0
		} else {
			if filtBest < filtMid {
				filtDirection = -1
			} else {
				filtDirection = 1
			}
			filtMid = filtBest
		}
	}
	if filtBest > 0 {
		switch {
		case residentLevel == filtBest:
			e.loopFilterPickReady = true
			e.loopFilterPickLevel = uint8(filtBest)
			e.loopFilterPickBest = false
		case bestLumaLevel == filtBest:
			e.loopFilterPickReady = true
			e.loopFilterPickLevel = uint8(filtBest)
			e.loopFilterPickBest = true
		}
	}
	return uint8(filtBest), nil
}
func motion10PRContext(tb testing.TB, varied bool) loopFilterPickContext {
	const w, h = 128, 96
	src := testImage(w, h)
	e := newSizedTestEncoder(tb, w, h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := byte(128)
			if varied {
				v = byte(40 + (y*17+x*13)%180)
			}
			src.Y[y*src.YStride+x] = v
			e.analysis.Img.Y[y*e.analysis.Img.YStride+x] = v
		}
	}
	e.analysis.ExtendBorders()
	const rows, cols = h / 16, w / 16
	if len(e.reconstructModes) < rows*cols {
		e.reconstructModes = make([]vp8dec.MacroblockMode, rows*cols)
	}
	for i := range e.reconstructModes {
		e.reconstructModes[i] = vp8dec.MacroblockMode{Mode: vp8common.NearestMV, RefFrame: vp8common.LastFrame}
	}
	return e.newLoopFilterPickContext(sourceImageFromPublic(src), vp8common.InterFrame, 0, rows, cols, rows*cols, vp8enc.SegmentationConfig{})
}
func TestMotion10PRZeroLFError(t *testing.T) {
	for _, varied := range []bool{false, true} {
		for _, level := range []uint8{0, 1, 8, 16, 32, 63} {
			for _, full := range []bool{false, true} {
				a, b := motion10PRContext(t, varied), motion10PRContext(t, varied)
				var got, want uint8
				var ge, we error
				if full {
					got, ge = a.pickFullNoStats(level, 0)
					want, we = b.motion10ParentPickFullNoStats(level, 0)
				} else {
					got, ge = a.pickFastNoStats(level, 0)
					want, we = b.motion10ParentPickFastNoStats(level, 0)
				}
				if got != want || ge != we {
					t.Fatal(varied, level, full, got, want, ge, we)
				}
				if a.encoder.loopFilterPickReady != b.encoder.loopFilterPickReady || a.encoder.loopFilterPickLevel != b.encoder.loopFilterPickLevel {
					t.Fatal("resident flag", varied, level, full)
				}
				if a.encoder.loopFilterPickReady {
					p, q := &a.encoder.loopFilterPick.Img, &b.encoder.loopFilterPick.Img
					if a.encoder.loopFilterPickBest {
						p = &a.encoder.loopFilterBest.Img
					}
					if b.encoder.loopFilterPickBest {
						q = &b.encoder.loopFilterBest.Img
					}
					for y := 0; y < 96; y++ {
						for x := 0; x < 128; x++ {
							if p.Y[y*p.YStride+x] != q.Y[y*q.YStride+x] {
								t.Fatal("resident pixels", varied, level, full, y, x)
							}
						}
					}
				}
			}
		}
	}
}

var motion10PRInt int

func BenchmarkMotion10PRZeroLFError(b *testing.B) {
	for _, full := range []bool{false, true} {
		b.Run(fmt.Sprint("full", full), func(b *testing.B) {
			ctx := motion10PRContext(b, false)
			b.ReportAllocs()
			for b.Loop() {
				var l uint8
				var e error
				if full {
					l, e = ctx.pickFullNoStats(32, 0)
				} else {
					l, e = ctx.pickFastNoStats(32, 0)
				}
				motion10PRInt = int(l)
				if e != nil {
					b.Fatal(e)
				}
			}
		})
	}
}
