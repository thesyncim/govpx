package govpx

import (
	"math"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

const (
	libvpxMinGFInterval = 4
	libvpxIIKFactor1    = 1.40
	libvpxRMax          = 14.0

	// libvpx vp8/encoder/firstpass.c intrapenalty inside the per-MB loop.
	libvpxFirstPassIntraPenalty = 256
	// new_mv_mode_penalty added to motion_error after a successful diamond
	// search in libvpx vp8/encoder/firstpass.c first_pass_motion_search.
	libvpxFirstPassNewMVModePenalty = 256
	// libvpx first_pass_motion_search starts the NSTEP diamond at step_param=3
	// rather than searching the full range.
	libvpxFirstPassSearchStepParam = 3
)

type FirstPassFrameStats struct {
	Frame               uint64
	IntraError          float64
	CodedError          float64
	SSIMWeightedPredErr float64
	PcntInter           float64
	PcntMotion          float64
	PcntSecondRef       float64
	PcntNeutral         float64
	MVr                 float64
	MVrAbs              float64
	MVc                 float64
	MVcAbs              float64
	MVrv                float64
	MVcv                float64
	MVInOutCount        float64
	NewMVCount          float64
	Duration            float64
	Count               float64
}

func (e *VP8Encoder) CollectFirstPassStats(src Image, pts uint64, duration uint64, flags EncodeFlags) (FirstPassFrameStats, error) {
	if e == nil || e.closed {
		return FirstPassFrameStats{}, ErrClosed
	}
	if !src.validForEncode(e.opts.Width, e.opts.Height) {
		return FirstPassFrameStats{}, ErrInvalidConfig
	}
	if err := validateEncodeFlags(flags); err != nil {
		return FirstPassFrameStats{}, err
	}
	_ = pts
	srcImg := sourceImageFromImage(src)
	stats := e.computeFirstPassStats(srcImg, duration)

	// Mirror libvpx vp8/encoder/firstpass.c "Copy the previous Last Frame
	// into the GF buffer if specific conditions for doing so are met":
	//   if (current_video_frame > 0 &&
	//       this_frame_stats.pcnt_inter > 0.20 &&
	//       (intra_error / coded_error) > 2.0) {
	//     vp8_yv12_copy_frame(lst_yv12, gld_yv12);
	//   }
	// The decision must use the *previous* LAST buffer, before we swap in
	// the reconstructed current frame as the new LAST.
	if e.firstPassCount > 0 &&
		stats.PcntInter > 0.20 &&
		(stats.IntraError/doubleDivideCheck(stats.CodedError)) > 2.0 &&
		e.firstPassLastRef.Img.Width == src.Width &&
		e.firstPassLastRef.Img.Height == src.Height {
		copyFrameImage(&e.firstPassGoldenRef.Img, &e.firstPassLastRef.Img)
		e.firstPassGoldenRef.ExtendBorders()
	}

	copyFrameImage(&e.firstPassLastRef.Img, &e.firstPassNewRef.Img)
	e.firstPassLastRef.ExtendBorders()
	copySourceToFrameBuffer(&e.firstPassLastSource, srcImg)

	// Special case for the first frame (libvpx firstpass.c): copy LAST into
	// GF as a second reference. Also keep the legacy scene-cut fallback that
	// resets GF when essentially the entire frame went intra; the libvpx
	// equivalent is the swap+initial GF copy plus the post-stats heuristic
	// above, but in govpx this fallback prevents PcntSecondRef from latching
	// to a stale GOLDEN across hard cuts.
	if e.firstPassCount == 0 || stats.PcntInter < 0.05 {
		copyFrameImage(&e.firstPassGoldenRef.Img, &e.firstPassLastRef.Img)
		e.firstPassGoldenRef.ExtendBorders()
	}
	e.firstPassCount++
	return stats, nil
}

func (e *VP8Encoder) computeFirstPassStats(src vp8enc.SourceImage, duration uint64) FirstPassFrameStats {
	rows := encoderMacroblockRows(src.Height)
	cols := encoderMacroblockCols(src.Width)
	mbs := rows * cols
	if mbs <= 0 {
		return FirstPassFrameStats{Frame: e.firstPassCount, Count: 1}
	}
	intraPenalty := libvpxFirstPassIntraPenalty
	encodeBreakout := e.opts.StaticThreshold
	intraError := int64(0)
	codedError := int64(0)
	interCount := 0
	secondRefCount := 0
	neutralCount := 0

	// MV accumulators mirror libvpx vp8/encoder/firstpass.c vp8_first_pass:
	// sum_mvr, sum_mvc, sum_mvr_abs, sum_mvc_abs, sum_mvrs, sum_mvcs,
	// sum_in_vectors, mvcount, new_mv_count.
	sumMVr := int64(0)
	sumMVc := int64(0)
	sumMVrAbs := int64(0)
	sumMVcAbs := int64(0)
	sumMVrs := int64(0)
	sumMVcs := int64(0)
	sumInVectors := int64(0)
	mvCount := 0
	newMVCount := 0
	lastMVAsInt := uint32(0)

	hasLast := e.firstPassCount > 0 && e.firstPassLastRef.Img.Width == src.Width && e.firstPassLastRef.Img.Height == src.Height
	hasLastSource := e.firstPassCount > 0 && e.firstPassLastSource.Img.Width == src.Width && e.firstPassLastSource.Img.Height == src.Height
	hasGolden := e.firstPassCount > 1 && e.firstPassGoldenRef.Img.Width == src.Width && e.firstPassGoldenRef.Img.Height == src.Height
	qIndex := e.rc.currentQuantizer
	copySourceToFrameBuffer(&e.firstPassNewRef, src)
	quantDeltas := libvpxFrameQuantDeltas(qIndex, e.opts.ScreenContentMode)
	var quants [vp8common.MaxMBSegments]vp8enc.MacroblockQuant
	_ = vp8enc.InitSegmentMacroblockQuants(qIndex, quantDeltas, vp8enc.SegmentationConfig{}, &quants)
	var dequantTables vp8common.FrameDequantTables
	var dequant vp8common.MacroblockDequant
	vp8common.BuildFrameDequantTables(quantDeltas, &dequantTables)
	vp8common.InitMacroblockDequant(&dequantTables, qIndex, &dequant)
	for row := 0; row < rows; row++ {
		bestRefMV := vp8enc.MotionVector{}
		for col := 0; col < cols; col++ {
			intra := macroblockMeanLumaSSE(src, row, col) + intraPenalty
			intraError += int64(intra)
			_ = e.reconstructFirstPassIntraMacroblock(src, row, col, qIndex, &quants[0], &dequant)

			thisError := intra
			lastErr := maxInt()
			bestMV := vp8enc.MotionVector{}

			if hasLast {
				// Raw zero-motion check (libvpx zz_motion_search). The
				// raw source gates encode_breakout, while the reconstructed
				// LAST reference seeds the actual motion error.
				zeroErr := macroblockLumaSSE(src, &e.firstPassLastRef.Img, row, col, vp8enc.MotionVector{}) + 128
				rawMotionErr := zeroErr
				if hasLastSource {
					rawMotionErr = macroblockLumaSSE(src, &e.firstPassLastSource.Img, row, col, vp8enc.MotionVector{}) + 128
				}
				motionErr := zeroErr

				if rawMotionErr >= encodeBreakout {
					if mv, err, ok := firstPassMotionSearch(src, &e.firstPassLastRef.Img, row, col, bestRefMV, qIndex); ok {
						err += libvpxFirstPassNewMVModePenalty
						if err < motionErr {
							motionErr = err
							bestMV = mv
						}
					}
					if !bestRefMV.IsZero() {
						if mv, err, ok := firstPassMotionSearch(src, &e.firstPassLastRef.Img, row, col, vp8enc.MotionVector{}, qIndex); ok {
							err += libvpxFirstPassNewMVModePenalty
							if err < motionErr {
								motionErr = err
								bestMV = mv
							}
						}
					}
				}
				lastErr = motionErr

				if motionErr <= thisError {
					// libvpx neutral-count gate uses motion_error rather
					// than zz_motion_error, so port it the same way:
					//   ((this_error - intrapenalty) * 9 <= motion_error * 10)
					//   && (this_error < 2 * intrapenalty)
					if ((intra-intraPenalty)*9 <= motionErr*10) && intra < 2*intraPenalty {
						neutralCount++
					}
					thisError = motionErr
					interCount++
					bestRefMV = bestMV
					_ = e.reconstructFirstPassInterMacroblock(src, row, col, bestMV, qIndex, &quants[0], &dequant)

					// libvpx multiplies bmi.mv by 8 here to convert from
					// pel to 1/8-pel (q3) before summing into
					// FIRSTPASS_STATS. govpx MotionVector is already q3,
					// so we accumulate the q3 components directly to land
					// on the same scale.
					mvR := int32(bestMV.Row)
					mvC := int32(bestMV.Col)

					sumMVr += int64(mvR)
					sumMVc += int64(mvC)
					sumMVrAbs += int64(abs32(mvR))
					sumMVcAbs += int64(abs32(mvC))
					sumMVrs += int64(mvR) * int64(mvR)
					sumMVcs += int64(mvC) * int64(mvC)

					if bestMV.Row != 0 || bestMV.Col != 0 {
						mvCount++
						mvAsInt := (uint32(uint16(mvR)) << 16) | uint32(uint16(mvC))
						if mvAsInt != lastMVAsInt {
							newMVCount++
						}
						lastMVAsInt = mvAsInt

						// Row vector inward/outward accumulation.
						if row < rows/2 {
							if mvR > 0 {
								sumInVectors--
							} else if mvR < 0 {
								sumInVectors++
							}
						} else if row > rows/2 {
							if mvR > 0 {
								sumInVectors++
							} else if mvR < 0 {
								sumInVectors--
							}
						}
						// Col vector inward/outward accumulation.
						if col < cols/2 {
							if mvC > 0 {
								sumInVectors--
							} else if mvC < 0 {
								sumInVectors++
							}
						} else if col > cols/2 {
							if mvC > 0 {
								sumInVectors++
							} else if mvC < 0 {
								sumInVectors--
							}
						}
					}
				} else {
					bestRefMV = vp8enc.MotionVector{}
				}
			}

			if hasGolden {
				// Experimental search in a second reference frame
				// ((0,0) based only) per libvpx.
				goldenErr := macroblockLumaSSE(src, &e.firstPassGoldenRef.Img, row, col, vp8enc.MotionVector{}) + 128
				if mv, err, ok := firstPassMotionSearch(src, &e.firstPassGoldenRef.Img, row, col, vp8enc.MotionVector{}, qIndex); ok {
					_ = mv
					err += libvpxFirstPassNewMVModePenalty
					if err < goldenErr {
						goldenErr = err
					}
				}
				if goldenErr < lastErr && goldenErr < intra {
					secondRefCount++
				}
			}

			codedError += int64(thisError)
		}
		vp8dec.ExtendIntraRightEdgeForRow(&e.firstPassNewRef.Img, row)
	}
	e.firstPassNewRef.ExtendBorders()

	stats := FirstPassFrameStats{
		Frame:         e.firstPassCount,
		IntraError:    float64(intraError >> 8),
		CodedError:    float64(codedError >> 8),
		PcntInter:     float64(interCount) / float64(mbs),
		PcntSecondRef: float64(secondRefCount) / float64(mbs),
		PcntNeutral:   float64(neutralCount) / float64(mbs),
		Duration:      float64(duration),
		Count:         1,
	}

	// libvpx ssim_weighted_pred_err = coded_error * simple_weight(source),
	// clamped to a 0.1 floor on the weight.
	weight := simpleWeightLuma(src)
	if weight < 0.1 {
		weight = 0.1
	}
	stats.SSIMWeightedPredErr = stats.CodedError * weight

	// libvpx finalisation:
	//   if (mvcount > 0) {
	//     fps.MVr = sum_mvr / mvcount;
	//     fps.MVrv = (sum_mvrs - MVr*MVr/mvcount) / mvcount;
	//     ...
	//     fps.pcnt_motion = mvcount / MBs;
	//   }
	if mvCount > 0 {
		mvCountF := float64(mvCount)
		stats.MVr = float64(sumMVr) / mvCountF
		stats.MVrAbs = float64(sumMVrAbs) / mvCountF
		stats.MVc = float64(sumMVc) / mvCountF
		stats.MVcAbs = float64(sumMVcAbs) / mvCountF
		stats.MVrv = (float64(sumMVrs) - (stats.MVr * stats.MVr / mvCountF)) / mvCountF
		stats.MVcv = (float64(sumMVcs) - (stats.MVc * stats.MVc / mvCountF)) / mvCountF
		stats.MVInOutCount = float64(sumInVectors) / float64(mvCount*2)
		stats.NewMVCount = float64(newMVCount)
		stats.PcntMotion = float64(mvCount) / float64(mbs)
	}

	if stats.Duration == 0 {
		stats.Duration = 1
	}
	return stats
}

func (e *VP8Encoder) reconstructFirstPassIntraMacroblock(src vp8enc.SourceImage, mbRow int, mbCol int, qIndex int, quant *vp8enc.MacroblockQuant, dequant *vp8common.MacroblockDequant) bool {
	if e == nil || quant == nil || dequant == nil {
		return false
	}
	useDCPred := (mbCol != 0 || mbRow != 0) && (mbCol == 0 || mbRow == 0)
	if !useDCPred {
		mode := vp8dec.MacroblockMode{
			RefFrame: vp8common.IntraFrame,
			Mode:     vp8common.BPred,
			UVMode:   vp8common.DCPred,
			Is4x4:    true,
		}
		for i := range mode.BModes {
			mode.BModes[i] = vp8common.BDCPred
		}
		var coeffs vp8enc.MacroblockCoefficients
		return buildReconstructingBPredMacroblockCoefficients(
			&vp8tables.DefaultCoefProbs,
			src, mbRow, mbCol,
			&e.firstPassNewRef.Img,
			&mode,
			nil, nil,
			quant, qIndex,
			0,
			e.libvpxUseFastQuant(),
			e.libvpxOptimizeCoefficients(),
			&coeffs,
			&e.reconstructScratch,
		)
	}

	mode := vp8dec.MacroblockMode{
		RefFrame: vp8common.IntraFrame,
		Mode:     vp8common.DCPred,
		UVMode:   vp8common.DCPred,
	}
	if !predictAnalysisMacroblock(&e.firstPassNewRef.Img, mbRow, mbCol, &mode, &e.reconstructScratch) {
		return false
	}
	var coeffs vp8enc.MacroblockCoefficients
	buildPredictedMacroblockCoefficients(
		&vp8tables.DefaultCoefProbs,
		src, mbRow, mbCol,
		&e.firstPassNewRef.Img,
		nil, nil,
		quant, qIndex,
		0, 0,
		false,
		true,
		e.libvpxUseFastQuant(),
		e.libvpxOptimizeCoefficients(),
		&coeffs,
	)
	var tokens vp8dec.MacroblockTokens
	convertMacroblockCoefficients(&coeffs, false, &tokens)
	return reconstructAnalysisMacroblock(&e.firstPassNewRef.Img, mbRow, mbCol, &mode, &tokens, dequant, &e.reconstructScratch)
}

func (e *VP8Encoder) reconstructFirstPassInterMacroblock(src vp8enc.SourceImage, mbRow int, mbCol int, mv vp8enc.MotionVector, qIndex int, quant *vp8enc.MacroblockQuant, dequant *vp8common.MacroblockDequant) bool {
	if e == nil || quant == nil || dequant == nil {
		return false
	}
	mode := vp8enc.InterFrameMacroblockMode{
		RefFrame: vp8common.LastFrame,
		Mode:     vp8common.NewMV,
		UVMode:   vp8common.DCPred,
		MV:       mv,
	}
	var decMode vp8dec.MacroblockMode
	convertInterFrameMode(&mode, &decMode)
	decMode.MBSkipCoeff = true
	var emptyTokens vp8dec.MacroblockTokens
	if !reconstructInterAnalysisMacroblock(&e.firstPassNewRef.Img, &e.firstPassLastRef.Img, mbRow, mbCol, &decMode, &emptyTokens, dequant, &e.reconstructScratch) {
		return false
	}

	var coeffs vp8enc.MacroblockCoefficients
	buildPredictedMacroblockCoefficients(
		&vp8tables.DefaultCoefProbs,
		src, mbRow, mbCol,
		&e.firstPassNewRef.Img,
		nil, nil,
		quant, qIndex,
		0, 0,
		false,
		false,
		e.libvpxUseFastQuant(),
		e.libvpxOptimizeCoefficients(),
		&coeffs,
	)
	var tokens vp8dec.MacroblockTokens
	convertMacroblockCoefficients(&coeffs, false, &tokens)
	decMode.MBSkipCoeff = macroblockCoefficientsEmpty(&coeffs, false)
	return reconstructInterAnalysisMacroblock(&e.firstPassNewRef.Img, &e.firstPassLastRef.Img, mbRow, mbCol, &decMode, &tokens, dequant, &e.reconstructScratch)
}

func firstPassMotionSearch(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, seed vp8enc.MotionVector, qIndex int) (vp8enc.MotionVector, int, bool) {
	if ref == nil || ref.Width <= 0 || ref.Height <= 0 {
		return vp8enc.MotionVector{}, 0, false
	}
	mbRows := encoderMacroblockRows(src.Height)
	mbCols := encoderMacroblockCols(src.Width)
	bounds := interFrameFullPixelSearchBounds(seed, mbRow, mbCol, mbRows, mbCols)
	center := bounds.clampEighth(vp8enc.MotionVector{
		Row: int16(int(seed.Row) & ^7),
		Col: int16(int(seed.Col) & ^7),
	})
	centerCost := interMotionSearchCost(src, ref, mbRow, mbCol, center, seed, qIndex)
	search := interAnalysisSearchConfig{
		fullPixelSearchParam:  libvpxFirstPassSearchStepParam,
		fullPixelFurtherSteps: interFrameMaxMVSearchSteps - 1 - libvpxFirstPassSearchStepParam,
	}
	mv, cost := firstPassNstepMotionSearch(src, ref, mbRow, mbCol, center, centerCost, seed, qIndex, bounds, search)
	return mv, cost, true
}

func firstPassNstepMotionSearch(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, center vp8enc.MotionVector, centerWalkCost int, bestRefMV vp8enc.MotionVector, qIndex int, bounds interFrameFullPixelBounds, search interAnalysisSearchConfig) (vp8enc.MotionVector, int) {
	stepParam := search.fullPixelSearchParam
	if stepParam < 0 {
		stepParam = 0
	} else if stepParam >= interFrameMaxMVSearchSteps {
		stepParam = interFrameMaxMVSearchSteps - 1
	}

	result := firstPassDiamondNstepMotionSearch(src, ref, mbRow, mbCol, center, centerWalkCost, bestRefMV, qIndex, bounds, stepParam)
	best := result.mv
	bestCost := result.cost
	n := result.num00
	num00 := 0
	for n < search.fullPixelFurtherSteps {
		n++
		if num00 > 0 {
			num00--
			continue
		}
		candidate := firstPassDiamondNstepMotionSearch(src, ref, mbRow, mbCol, center, centerWalkCost, bestRefMV, qIndex, bounds, stepParam+n)
		num00 = candidate.num00
		if candidate.cost < bestCost {
			best = candidate.mv
			bestCost = candidate.cost
		}
	}
	return best, bestCost
}

func firstPassDiamondNstepMotionSearch(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, center vp8enc.MotionVector, centerWalkCost int, bestRefMV vp8enc.MotionVector, qIndex int, bounds interFrameFullPixelBounds, searchParam int) interFrameNstepSearchResult {
	sites := interFrameNstepSearchSites()
	result := diamondSearchSitesInterFrameFullPixelMotionVector(src, ref, mbRow, mbCol, center, centerWalkCost, bestRefMV, qIndex, bounds, sites[:], 8, searchParam, &vp8tables.DefaultMVContext)
	result.cost = firstPassMotionSearchReturnCost(src, ref, mbRow, mbCol, result.mv, bestRefMV, qIndex)
	return result
}

func firstPassMotionSearchReturnCost(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int) int {
	return macroblockLumaSSE(src, ref, mbRow, mbCol, mv) + interMotionSearchErrorVectorCost(mv, bestRefMV, qIndex, &vp8tables.DefaultMVContext)
}

// simpleWeightLuma ports libvpx vp8/encoder/firstpass.c simple_weight: it
// reads every Y pixel through a 256-entry weight_table and averages, returning
// a low/high-luma SSIM bias. The table flattens to 0.02 below code 32, ramps
// linearly to 1.0 between 32 and 64, and stays at 1.0 above 64.
func simpleWeightLuma(src vp8enc.SourceImage) float64 {
	if src.Width <= 0 || src.Height <= 0 {
		return 1.0
	}
	sum := 0.0
	for row := 0; row < src.Height; row++ {
		base := row * src.YStride
		for col := 0; col < src.Width; col++ {
			sum += firstPassWeightTable[src.Y[base+col]]
		}
	}
	return sum / float64(src.Width*src.Height)
}

// firstPassWeightTable mirrors weight_table[256] in libvpx
// vp8/encoder/firstpass.c. Codes [0..32] -> 0.02, [33..63] ramps in 0.03125
// steps from 0.03125 up to 0.96875, [64..255] -> 1.0.
var firstPassWeightTable = func() [256]float64 {
	var t [256]float64
	for i := 0; i <= 32; i++ {
		t[i] = 0.02
	}
	// libvpx values for 33..63 (matches weight_table literal):
	//   0.03125, 0.0625, 0.09375, ..., 0.96875
	for i := 33; i < 64; i++ {
		t[i] = float64(i-32) / 32.0
	}
	for i := 64; i < 256; i++ {
		t[i] = 1.0
	}
	return t
}()

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

type twoPassState struct {
	stats       []FirstPassFrameStats
	totalStats  FirstPassFrameStats
	bitsLeft    int64
	errorLeft   float64
	frameIndex  uint64
	vbrBiasPct  int
	minPct      int
	maxPct      int
	lastKeySeen uint64
}

func (t *twoPassState) configure(stats []FirstPassFrameStats, bitsPerFrame int, biasPct int, minPct int, maxPct int) {
	*t = twoPassState{}
	if len(stats) == 0 || bitsPerFrame <= 0 {
		return
	}
	t.stats, t.totalStats = normalizeTwoPassStats(stats)
	if len(t.stats) == 0 {
		return
	}
	t.bitsLeft = int64(bitsPerFrame) * int64(len(t.stats))
	t.vbrBiasPct = biasPct
	if t.vbrBiasPct <= 0 {
		t.vbrBiasPct = 50
	}
	t.minPct = minPct
	if t.minPct <= 0 {
		t.minPct = 50
	}
	t.maxPct = maxPct
	if t.maxPct <= 0 {
		t.maxPct = 200
	}
	for i := range t.stats {
		t.errorLeft += t.modifiedError(t.stats[i])
	}
}

func (t *twoPassState) enabled() bool {
	return len(t.stats) > 0
}

func (t *twoPassState) statsForFrame(frame uint64) FirstPassFrameStats {
	if !t.enabled() || frame >= uint64(len(t.stats)) {
		return FirstPassFrameStats{}
	}
	return t.stats[frame]
}

func (t *twoPassState) shouldKeyFrame(frame uint64, framesSinceKeyFrame int, keyFrameInterval int) bool {
	if !t.enabled() || frame == 0 || frame+1 >= uint64(len(t.stats)) {
		return false
	}
	if framesSinceKeyFrame < libvpxMinGFInterval {
		return false
	}
	if keyFrameInterval > 0 && framesSinceKeyFrame >= keyFrameInterval {
		return true
	}
	return libvpxTestCandidateKeyFrame(t.stats, int(frame))
}

func (t *twoPassState) frameTargetBits(frame uint64, keyFrame bool, defaultTargetBits int) int {
	if !t.enabled() || frame >= uint64(len(t.stats)) || defaultTargetBits <= 0 {
		return 0
	}
	modErr := t.modifiedError(t.stats[frame])
	if modErr <= 0 || t.errorLeft <= 0 || t.bitsLeft <= 0 {
		return defaultTargetBits
	}
	target := int64(float64(t.bitsLeft) * modErr / t.errorLeft)
	sectionMin, sectionMax := t.pass2VBRSectionLimits(frame, defaultTargetBits)
	if keyFrame {
		// libvpx's KF allocator (find_next_key_frame -> kf_group_bits)
		// runs through a separate path that biases more bits toward the
		// KF; until that landing is fully wired the govpx shim keeps the
		// historical 3x boost on the err-fraction target and the 4x
		// expansion of the section ceiling so KF allocation does not
		// regress against the existing oracle pins.
		sectionMax *= 4
		target *= 3
	}
	if target < sectionMin {
		target = sectionMin
	}
	if target > sectionMax {
		target = sectionMax
	}
	if target < 1 {
		target = 1
	}
	if target > int64(maxInt()) {
		return maxInt()
	}
	return int(target)
}

// pass2VBRSectionLimits ports the libvpx vp8/encoder/firstpass.c
// Pass2Encode VBR section-limit application on the per-frame target.
// Returns the (section_min_bits, section_max_bits) bounds derived from
// the configured `two_pass_vbrmin_section` / `two_pass_vbrmax_section`
// percentages applied to (a) the live VBR per-frame budget
// `(bits_left/frames_left)` for the max ceiling, mirroring libvpx's
// `frame_max_bits` VBR branch, and (b) the per-frame average
// `defaultTargetBits` for the min floor, mirroring
// `cpi->min_frame_bandwidth = av_per_frame_bandwidth *
// two_pass_vbrmin_section / 100`. Frames past the end of the stats
// stream return the static fallback bounds.
func (t *twoPassState) pass2VBRSectionLimits(frame uint64, defaultTargetBits int) (int64, int64) {
	minPct := t.minPct
	if minPct <= 0 {
		minPct = 50
	}
	maxPct := t.maxPct
	if maxPct <= 0 {
		maxPct = 200
	}
	sectionMin := int64(defaultTargetBits) * int64(minPct) / 100
	sectionMax := int64(defaultTargetBits) * int64(maxPct) / 100
	if t.enabled() && frame < uint64(len(t.stats)) {
		framesLeft := int64(len(t.stats)) - int64(frame)
		if vbrMax := libvpxFrameMaxBitsVBR(t.bitsLeft, framesLeft, maxPct); vbrMax > 0 {
			sectionMax = int64(vbrMax)
		}
	}
	if sectionMin < 0 {
		sectionMin = 0
	}
	if sectionMax < sectionMin {
		sectionMax = sectionMin
	}
	return sectionMin, sectionMax
}

// pass2DetectARFPending ports the libvpx vp8/encoder/firstpass.c
// `define_gf_group` / `select_arf_period` ARF-pending decision, the
// branch at lines 1758-1842 that sets `cpi->source_alt_ref_pending = 1`
// when the upcoming GF section is a high-motion / high-quality run that
// will benefit from a hidden alt-ref. Returns the ARF section interval
// (in frames, mirroring libvpx's `cpi->baseline_gf_interval`) and a
// pending flag.
//
// Heuristic mirrored:
//
//   - allow_alt_ref guard (caller passes
//     `cpi->oxcf.play_alternate && lag_in_frames`).
//   - i >= MIN_GF_INTERVAL.
//   - i <= frames_to_key - MIN_GF_INTERVAL (don't ARF very near KF).
//   - next_frame.pcnt_inter > 0.75 (start of section is strongly
//     predicted from LAST so a hidden ARF is worth the cost).
//   - mv_in_out_accumulator/i > -0.2 OR mv_in_out_accumulator > -2.0
//     (motion is not collapsing inward only).
//   - gfu_boost > 100 (the boost score crossed the libvpx floor).
//
// The interval is the libvpx GF-loop length, capped at
// `min(static_scene_max_gf_interval, frames_to_key)` and floored at
// MIN_GF_INTERVAL.
func (t *twoPassState) pass2DetectARFPending(currentFrame uint64, framesToKey int, allowAltRef bool, maxGFInterval int) (int, bool) {
	if !t.enabled() || !allowAltRef || framesToKey <= 0 {
		return 0, false
	}
	if currentFrame >= uint64(len(t.stats)) {
		return 0, false
	}
	if maxGFInterval < libvpxMinGFInterval {
		maxGFInterval = libvpxMinGFInterval
	}
	// libvpx walks i forward up to static_scene_max_gf_interval (or
	// frames_to_key, whichever is smaller), accumulating motion stats.
	// Also cap at frames_to_key - MIN_GF_INTERVAL so the eventual
	// `i <= frames_to_key - MIN_GF_INTERVAL` ARF guard can be
	// satisfied by the walk; otherwise a strongly-predicted clip near
	// the end of stats would always fail the post-loop check.
	maxLookahead := framesToKey
	if maxLookahead > maxGFInterval {
		maxLookahead = maxGFInterval
	}
	if cap := framesToKey - libvpxMinGFInterval; cap > 0 && maxLookahead > cap {
		maxLookahead = cap
	}
	if remaining := int(uint64(len(t.stats)) - currentFrame); maxLookahead > remaining {
		maxLookahead = remaining
	}
	if maxLookahead < libvpxMinGFInterval {
		return 0, false
	}
	mvInOutAccumulator := 0.0
	decayAccumulator := 1.0
	boostScore := 0.0
	oldBoostScore := 0.0
	interval := 0
	for i := 1; i <= maxLookahead; i++ {
		idx := currentFrame + uint64(i)
		if idx >= uint64(len(t.stats)) {
			break
		}
		next := t.stats[idx]
		mvInOutAccumulator += next.MVInOutCount
		// libvpx calc_frame_boost (vp8/encoder/firstpass.c lines
		// 1451-1480): frame_boost = IIFACTOR * intra_error /
		// coded_error, clamped to GF_RMAX=48.0, then biased by
		// mv_in_out (positive doubles, negative halves). govpx omits
		// the `gf_intra_err_min` floor (which depends on per-frame
		// MB count from the encoder context, not the stats stream)
		// and uses the raw intra/coded ratio as the boost signal.
		const iiFactor = 1.5
		const gfRMax = 48.0
		denom := next.CodedError
		if denom > -1e-12 && denom < 1e-12 {
			denom = 1.0
		}
		frameBoost := iiFactor * next.IntraError / denom
		if next.MVInOutCount > 0 {
			frameBoost += frameBoost * (next.MVInOutCount * 2.0)
		} else {
			frameBoost += frameBoost * (next.MVInOutCount / 2.0)
		}
		if frameBoost > gfRMax {
			frameBoost = gfRMax
		}
		// Cumulative effect of prediction quality decay, mirroring
		// libvpx's `decay_accumulator = decay_accumulator *
		// loop_decay_rate; clamp(0.1, 1.0)`.
		loopDecayRate := libvpxGetPredictionDecayRate(next)
		decayAccumulator *= loopDecayRate
		if decayAccumulator < 0.1 {
			decayAccumulator = 0.1
		}
		boostScore += decayAccumulator * frameBoost
		// Break-out conditions mirroring libvpx's loop tail.
		if i > libvpxMinGFInterval &&
			(framesToKey-i) >= libvpxMinGFInterval &&
			((boostScore > 20.0) || (next.PcntInter < 0.75)) &&
			((boostScore - oldBoostScore) < 2.0) {
			break
		}
		interval = i
		oldBoostScore = boostScore
	}
	if interval < libvpxMinGFInterval {
		return 0, false
	}
	if interval > framesToKey-libvpxMinGFInterval {
		// libvpx: don't use ARF very near next KF.
		return 0, false
	}
	// Look at the frame just past current to apply the libvpx
	// `next_frame.pcnt_inter > 0.75` gate.
	if currentFrame+1 >= uint64(len(t.stats)) {
		return 0, false
	}
	nextFrame := t.stats[currentFrame+1]
	if nextFrame.PcntInter <= 0.75 {
		return 0, false
	}
	if !((mvInOutAccumulator/float64(interval) > -0.2) || (mvInOutAccumulator > -2.0)) {
		return 0, false
	}
	gfuBoost := int(boostScore*100.0) >> 4
	if gfuBoost <= 100 {
		return 0, false
	}
	return interval, true
}

// pass2MaybeArmAltRefPending wires the libvpx
// vp8/encoder/firstpass.c `define_gf_group` ARF-pending decision into
// the encoder. It runs once per non-key inter frame at a GF-group
// boundary (framesTillAltRefFrame == 0 and ARF not already pending or
// active) and, when the second-pass stats indicate a high-motion
// section ahead, calls `scheduleAltRefSource` so the auto-ARF driver
// can emit the hidden alt-ref at the predicted offset.
//
// The wiring is gated on:
//   - Two-pass stats loaded.
//   - `EncoderOptions.AutoAltRef` (libvpx `oxcf.play_alternate`).
//   - `LookaheadFrames > 1` (the auto-ARF driver requires future peeks).
//   - `!ErrorResilient` (libvpx zeroes source_alt_ref_pending in
//     error-resilient mode inside Pass2Encode).
//   - `keyFrame == false` (KF frames reset the ARF lifecycle in
//     libvpx).
//   - No alt-ref already pending or active.
func (e *VP8Encoder) pass2MaybeArmAltRefPending(currentFrame uint64, currentPTS uint64, keyFrame bool) {
	if e == nil || keyFrame {
		return
	}
	if !e.twoPass.enabled() {
		return
	}
	if !e.opts.AutoAltRef || e.opts.ErrorResilient || e.opts.LookaheadFrames <= 1 {
		return
	}
	if e.sourceAltRefPending || e.sourceAltRefActive {
		return
	}
	if e.framesTillAltRefFrame > 0 {
		return
	}
	framesToKey := e.twoPass.framesToKey(currentFrame, e.opts.KeyFrameInterval)
	if framesToKey <= 0 {
		return
	}
	maxGFInterval := e.opts.KeyFrameInterval
	if maxGFInterval <= 0 || maxGFInterval > e.opts.LookaheadFrames-1 {
		maxGFInterval = e.opts.LookaheadFrames - 1
	}
	interval, pending := e.twoPass.pass2DetectARFPending(currentFrame, framesToKey, true, maxGFInterval)
	if !pending {
		return
	}
	// libvpx alt_ref_source identifies the future lookahead entry that
	// will become the hidden ARF source. govpx uses PTS as the
	// identifier; without an exact future PTS we fall back to a
	// per-frame offset on the assumption of constant duration. The
	// driver matches by PTS via isSrcFrameAltRef, so as long as we
	// arrive at the same value when the frame is later popped from
	// the lookahead, scheduling is consistent.
	futurePTS := currentPTS + uint64(interval)
	e.scheduleAltRefSource(futurePTS, interval)
}

func (t *twoPassState) finishFrame(actualBits int) {
	if !t.enabled() {
		return
	}
	if t.frameIndex < uint64(len(t.stats)) {
		t.errorLeft -= t.modifiedError(t.stats[t.frameIndex])
		if t.errorLeft < 0 {
			t.errorLeft = 0
		}
	}
	t.bitsLeft -= int64(actualBits)
	if t.bitsLeft < 0 {
		t.bitsLeft = 0
	}
	t.frameIndex++
}

// libvpxEstimateMaxQ ports the libvpx vp8/encoder/firstpass.c
// estimate_max_q Q-search loop: walk Q from maxq_min_limit upward
// computing
//
//	bits_per_mb = err_correction * speed_correction * est_max_qcorrection
//	            * section_max_qfactor * (vp8_bits_per_mb[INTER][Q] + overhead)
//
// where err_correction is `libvpxCalcCorrectionFactor(err_per_mb,
// 150.0, 0.40, 0.90, Q)` and overhead decays by 0.98 per Q step. The
// search returns the lowest Q for which `bits_per_mb_at_q <=
// target_norm_bits_per_mb`. target_norm_bits_per_mb derives from
// section_target_bandwidth via libvpx's overflow-aware
// `(512 * section_target_bandwidth) / num_mbs` formula. When
// `section_target_bandwidth <= 0`, libvpx returns
// `maxq_max_limit` immediately.
//
// The CQ floor (`USAGE_CONSTRAINED_QUALITY` -> max(Q, cq_target_quality))
// is left to callers since it depends on encoder mode state.
func libvpxEstimateMaxQ(numMBs int, sectionTargetBandwidth int, overheadBits int, errPerMB float64, speedCorrection float64, estMaxQCorrection float64, sectionMaxQFactor float64, maxqMinLimit int, maxqMaxLimit int) int {
	if numMBs <= 0 || maxqMaxLimit <= maxqMinLimit {
		return maxqMaxLimit
	}
	if sectionTargetBandwidth <= 0 {
		return maxqMaxLimit
	}
	var targetNormBitsPerMB int
	if sectionTargetBandwidth < (1 << 20) {
		targetNormBitsPerMB = (512 * sectionTargetBandwidth) / numMBs
	} else {
		targetNormBitsPerMB = 512 * (sectionTargetBandwidth / numMBs)
	}
	overheadBitsPerMB := overheadBits / numMBs
	overheadBitsPerMB = int(float64(overheadBitsPerMB) * math.Pow(0.98, float64(maxqMinLimit)))
	for Q := maxqMinLimit; Q < maxqMaxLimit; Q++ {
		errCorrection := libvpxCalcCorrectionFactor(errPerMB, 150.0, 0.40, 0.90, Q)
		baseBitsPerMB := 0
		if Q >= 0 && Q < len(libvpxBitsPerMB[1]) {
			baseBitsPerMB = libvpxBitsPerMB[1][Q]
		}
		baseBitsPerMB += overheadBitsPerMB
		bitsPerMBAtQ := int(0.5 + errCorrection*speedCorrection*estMaxQCorrection*sectionMaxQFactor*float64(baseBitsPerMB))
		overheadBitsPerMB = int(float64(overheadBitsPerMB) * 0.98)
		if bitsPerMBAtQ <= targetNormBitsPerMB {
			return Q
		}
	}
	return maxqMaxLimit
}

// libvpxEstimateQ ports the libvpx vp8/encoder/firstpass.c
// estimate_q Q-search loop (the section-target Q probe used inside
// new_section_complete / Pass2Encode). It walks Q from 0 upward
// computing
//
//	bits_per_mb = err_correction * speed_correction *
//	              est_max_qcorrection * vp8_bits_per_mb[INTER][Q]
//
// (no overhead/section_max_qfactor scaling, distinguishing it from
// estimate_max_q). Returns the lowest Q whose bits_per_mb_at_q is at
// or below the target.
func libvpxEstimateQ(numMBs int, sectionTargetBandwidth int, errPerMB float64, speedCorrection float64, estMaxQCorrection float64) int {
	if numMBs <= 0 || sectionTargetBandwidth <= 0 {
		return vp8MaxQIndex
	}
	var targetNormBitsPerMB int
	if sectionTargetBandwidth < (1 << 20) {
		targetNormBitsPerMB = (512 * sectionTargetBandwidth) / numMBs
	} else {
		targetNormBitsPerMB = 512 * (sectionTargetBandwidth / numMBs)
	}
	for Q := 0; Q < len(libvpxBitsPerMB[1]); Q++ {
		errCorrection := libvpxCalcCorrectionFactor(errPerMB, 150.0, 0.40, 0.90, Q)
		bitsPerMBAtQ := int(0.5 + errCorrection*speedCorrection*estMaxQCorrection*float64(libvpxBitsPerMB[1][Q]))
		if bitsPerMBAtQ <= targetNormBitsPerMB {
			return Q
		}
	}
	return len(libvpxBitsPerMB[1]) - 1
}

// libvpxEstimateKFGroupQ ports the libvpx vp8/encoder/firstpass.c
// estimate_kf_group_q worst-case KF-group Q estimator. It mirrors:
//
//	pow_highq = (POW1 < 0.6) ? POW1+0.3 : 0.90
//	pow_lowq  = (POW1 < 0.7) ? POW1+0.1 : 0.80
//	if long_rolling_target_bits <= 0:
//	  current_spend_ratio = 10.0
//	else:
//	  current_spend_ratio = clamp(long_rolling_actual/long_rolling_target,
//	                              0.1, 10.0)
//	iiratio_correction_factor =
//	  max(0.5, 1.0 - (group_iiratio - 6.0) * 0.1)
//	combined = speed_correction * iiratio_correction_factor *
//	            current_spend_ratio
//	for Q in 0..MAXQ:
//	  cf = calc_correction_factor(err_per_mb, 150, pow_lowq, pow_highq, Q)
//	  bits = cf * combined * vp8_bits_per_mb[INTER][Q]
//	  if bits <= target: break
//	while (bits > target && Q < MAXQ*2):
//	  bits = 0.96 * bits; Q++
//
// POW1 in libvpx is `oxcf.two_pass_vbrbias / 100.0`; callers pass it
// directly. Returns MAXQ*2 when the budget is non-positive (libvpx's
// `if (target_norm_bits_per_mb <= 0) return MAXQ * 2;`).
func libvpxEstimateKFGroupQ(numMBs int, sectionTargetBandwidth int, errPerMB float64, groupIIRatio float64, vbrBiasPct int, longRollingActualBits int, longRollingTargetBits int, speedCorrection float64) int {
	const maxQ = vp8MaxQIndex + 1
	if numMBs <= 0 {
		return maxQ * 2
	}
	targetNormBitsPerMB := (512 * sectionTargetBandwidth) / numMBs
	if targetNormBitsPerMB <= 0 {
		return maxQ * 2
	}
	pow1 := float64(vbrBiasPct) / 100.0
	powHighQ := 0.90
	if pow1 < 0.6 {
		powHighQ = pow1 + 0.3
	}
	powLowQ := 0.80
	if pow1 < 0.7 {
		powLowQ = pow1 + 0.1
	}
	currentSpendRatio := 10.0
	if longRollingTargetBits > 0 {
		currentSpendRatio = float64(longRollingActualBits) / float64(longRollingTargetBits)
		if currentSpendRatio > 10.0 {
			currentSpendRatio = 10.0
		} else if currentSpendRatio < 0.1 {
			currentSpendRatio = 0.1
		}
	}
	iiratioCorrection := 1.0 - (groupIIRatio-6.0)*0.1
	if iiratioCorrection < 0.5 {
		iiratioCorrection = 0.5
	}
	combined := speedCorrection * iiratioCorrection * currentSpendRatio
	bitsPerMBAtQ := 0
	Q := 0
	for ; Q < maxQ; Q++ {
		errCorrection := libvpxCalcCorrectionFactor(errPerMB, 150.0, powLowQ, powHighQ, Q)
		bitsPerMBAtQ = int(0.5 + errCorrection*combined*float64(libvpxBitsPerMB[1][Q]))
		if bitsPerMBAtQ <= targetNormBitsPerMB {
			break
		}
	}
	for bitsPerMBAtQ > targetNormBitsPerMB && Q < maxQ*2 {
		bitsPerMBAtQ = int(0.96 * float64(bitsPerMBAtQ))
		Q++
	}
	return Q
}

// libvpxCalcCorrectionFactor ports the libvpx
// vp8/encoder/firstpass.c calc_correction_factor:
//
//	error_term = err_per_mb / err_devisor
//	power_term = clamp(pt_low + Q*0.01, +inf, pt_high)
//	correction_factor = pow(error_term, power_term)
//	clamp(correction_factor, 0.05, 5.0)
//
// Used by estimate_max_q / estimate_min_q / estimate_q to compute
// the per-Q rate model correction.
func libvpxCalcCorrectionFactor(errPerMB float64, errDevisor float64, ptLow float64, ptHigh float64, Q int) float64 {
	if errDevisor == 0 {
		errDevisor = 1.0
	}
	errorTerm := errPerMB / errDevisor
	powerTerm := ptLow + float64(Q)*0.01
	if powerTerm > ptHigh {
		powerTerm = ptHigh
	}
	cf := math.Pow(errorTerm, powerTerm)
	if cf < 0.05 {
		return 0.05
	}
	if cf > 5.0 {
		return 5.0
	}
	return cf
}

// libvpxEstimateMaxQRollingRatioAdjustment ports the rolling
// est_max_qcorrection_factor update from estimate_max_q:
//
//	rolling_ratio = rolling_actual_bits / rolling_target_bits
//	if ratio < 0.95: factor -= 0.005
//	if ratio > 1.05: factor += 0.005
//	clamp(factor, 0.1, 10.0)
//
// Returns the updated factor. Caller passes the previous factor and
// the rolling stats; the inner libvpx gate
// `(rolling_target_bits > 0) && (active_worst_quality < worst_quality)`
// is enforced by the caller.
func libvpxEstimateMaxQRollingRatioAdjustment(prevFactor float64, rollingActualBits int, rollingTargetBits int) float64 {
	if rollingTargetBits <= 0 {
		return prevFactor
	}
	ratio := float64(rollingActualBits) / float64(rollingTargetBits)
	factor := prevFactor
	if ratio < 0.95 {
		factor -= 0.005
	} else if ratio > 1.05 {
		factor += 0.005
	}
	if factor < 0.1 {
		factor = 0.1
	}
	if factor > 10.0 {
		factor = 10.0
	}
	return factor
}

// libvpxSectionStats accumulates the libvpx FIRSTPASS_STATS section
// totals used by find_next_key_frame and define_gf_group to derive
// section_intra_rating and section_max_qfactor. Mirrors libvpx's
// FIRSTPASS_STATS accumulate_stats / avg_stats pattern: callers
// call addFrame for each frame in the section and then call avg()
// once before reading sectionIntra / sectionCoded.
type libvpxSectionStats struct {
	count        int
	sectionIntra float64
	sectionCoded float64
}

// addFrame mirrors libvpx's accumulate_stats over the per-frame
// FIRSTPASS_STATS intra_error / coded_error fields.
func (s *libvpxSectionStats) addFrame(intraError, codedError float64) {
	s.count++
	s.sectionIntra += intraError
	s.sectionCoded += codedError
}

// avg mirrors libvpx's avg_stats: divides each accumulator by
// `count`. Callers should call this exactly once before reading
// sectionIntra / sectionCoded.
func (s *libvpxSectionStats) avg() {
	if s.count <= 0 {
		return
	}
	s.sectionIntra /= float64(s.count)
	s.sectionCoded /= float64(s.count)
}

// libvpxSectionIntraRating ports the libvpx vp8/encoder/firstpass.c
// section_intra_rating computation:
//
//	section_intra_rating = sectionIntra / DOUBLE_DIVIDE_CHECK(sectionCoded)
//
// where DOUBLE_DIVIDE_CHECK(x) returns 1.0 when |x|<1e-12 and x
// otherwise. Returns 0 when both error totals are 0 (libvpx asserts
// non-empty section in normal flow). The libvpx field is unsigned int,
// so the result is truncated to non-negative.
func libvpxSectionIntraRating(sectionIntra, sectionCoded float64) int {
	denom := sectionCoded
	if denom < 1e-12 && denom > -1e-12 {
		denom = 1.0
	}
	v := sectionIntra / denom
	if v < 0 {
		return 0
	}
	return int(v)
}

// libvpxSectionMaxQFactor ports the libvpx vp8/encoder/firstpass.c
// section_max_qfactor formula:
//
//	Ratio = sectionIntra / DOUBLE_DIVIDE_CHECK(sectionCoded)
//	section_max_qfactor = 1.0 - ((Ratio - 10.0) * 0.025)
//	if section_max_qfactor < 0.80: section_max_qfactor = 0.80
//
// The 0.80 floor mirrors libvpx exactly. Returns 1.0 when both error
// totals are 0 (libvpx's DOUBLE_DIVIDE_CHECK fallback).
func libvpxSectionMaxQFactor(sectionIntra, sectionCoded float64) float64 {
	denom := sectionCoded
	if denom < 1e-12 && denom > -1e-12 {
		denom = 1.0
	}
	ratio := sectionIntra / denom
	factor := 1.0 - ((ratio - 10.0) * 0.025)
	if factor < 0.80 {
		factor = 0.80
	}
	return factor
}

// libvpxAssignStdFrameBits ports the libvpx vp8/encoder/firstpass.c
// assign_std_frame_bits per-frame allocator inside a GF group:
//
//	err_fraction = modified_err / gf_group_error_left
//	target = gf_group_bits * err_fraction
//	clamp(target, 0, min(max_bits, gf_group_bits))
//	target += min_frame_bandwidth
//	if (frames_since_golden & 1) && frames_till_gf_update_due>0:
//	    target += alt_extra_bits
//
// Returns the per-frame bit target. Callers are expected to update
// gf_group_error_left and gf_group_bits themselves so the allocator
// stays a pure function.
func libvpxAssignStdFrameBits(modifiedErr float64, gfGroupErrorLeft float64, gfGroupBits int64, maxBitsPerFrame int, minFrameBandwidth int, framesSinceGolden int, framesTillGFUpdateDue int, altExtraBits int) int {
	if gfGroupBits <= 0 {
		return 0
	}
	errFraction := 0.0
	if gfGroupErrorLeft > 0 {
		errFraction = modifiedErr / gfGroupErrorLeft
	}
	target := int(float64(gfGroupBits) * errFraction)
	if target < 0 {
		target = 0
	} else {
		if maxBitsPerFrame > 0 && target > maxBitsPerFrame {
			target = maxBitsPerFrame
		}
		if int64(target) > gfGroupBits {
			target = int(gfGroupBits)
		}
	}
	target += minFrameBandwidth
	if (framesSinceGolden&0x01) != 0 && framesTillGFUpdateDue > 0 {
		target += altExtraBits
	}
	if target < 0 {
		return 0
	}
	return target
}

// libvpxFrameMaxBitsCBR ports the CBR branch of libvpx's
// vp8/encoder/firstpass.c frame_max_bits:
//
//	max_bits = av_per_frame_bandwidth * (two_pass_vbrmax_section / 100)
//	if buffer_level < optimal:
//	  buffer_fullness_ratio = buffer_level / optimal
//	  max_bits *= buffer_fullness_ratio
//	  min_max_bits = min(av_per_frame_bandwidth>>2, max_bits>>2 (pre-scale))
//	  max_bits = max(max_bits, min_max_bits)
//
// avPerFrameBandwidth is libvpx's `cpi->av_per_frame_bandwidth`, which
// equals govpx's `bitsPerFrame` in steady state. vbrMaxSection is
// `cpi->oxcf.two_pass_vbrmax_section` (govpx's
// EncoderOptions.TwoPassMaxPct). Returns 0 when the budget would be
// negative.
func libvpxFrameMaxBitsCBR(avPerFrameBandwidth int, vbrMaxSection int, bufferLevel int, optimalBufferLevel int) int {
	if avPerFrameBandwidth <= 0 || vbrMaxSection <= 0 {
		return 0
	}
	maxBits := avPerFrameBandwidth * vbrMaxSection / 100
	if optimalBufferLevel > 0 && bufferLevel < optimalBufferLevel {
		// Capture the pre-scale max_bits>>2 for the min floor calculation
		// (libvpx evaluates the min before the buffer-ratio scale).
		minMaxBits := avPerFrameBandwidth >> 2
		if (maxBits >> 2) < minMaxBits {
			minMaxBits = maxBits >> 2
		}
		maxBits = int(float64(maxBits) * float64(bufferLevel) / float64(optimalBufferLevel))
		if maxBits < minMaxBits {
			maxBits = minMaxBits
		}
	}
	if maxBits < 0 {
		return 0
	}
	return maxBits
}

// libvpxFrameMaxBitsVBR ports the VBR branch of libvpx's frame_max_bits:
//
//	max_bits = (bits_left / frames_left) * (two_pass_vbrmax_section / 100)
//
// Returns 0 when bits_left or frames_left are non-positive.
func libvpxFrameMaxBitsVBR(bitsLeft int64, framesLeft int64, vbrMaxSection int) int {
	if bitsLeft <= 0 || framesLeft <= 0 || vbrMaxSection <= 0 {
		return 0
	}
	bitsPerFrame := float64(bitsLeft) / float64(framesLeft)
	maxBits := int(bitsPerFrame * float64(vbrMaxSection) / 100.0)
	if maxBits < 0 {
		return 0
	}
	return maxBits
}

// libvpxGFGroupBits ports the libvpx vp8/encoder/firstpass.c GF-group
// allocation:
//
//	gf_group_bits = kf_group_bits * (gf_group_err / kf_group_error_left)
//
// then clamped to [0, kf_group_bits], then capped at
// `max_bits * baseline_gf_interval`. Returns 0 when kf_group_bits<=0
// or kf_group_error_left<=0.
func libvpxGFGroupBits(kfGroupBits int64, gfGroupErr float64, kfGroupErrorLeft float64, maxBitsPerFrame int, baselineGFInterval int) int64 {
	if kfGroupBits <= 0 || kfGroupErrorLeft <= 0 {
		return 0
	}
	gfGroupBits := int64(float64(kfGroupBits) * (gfGroupErr / kfGroupErrorLeft))
	if gfGroupBits < 0 {
		gfGroupBits = 0
	}
	if gfGroupBits > kfGroupBits {
		gfGroupBits = kfGroupBits
	}
	if maxBitsPerFrame > 0 && baselineGFInterval > 0 {
		cap := int64(maxBitsPerFrame) * int64(baselineGFInterval)
		if gfGroupBits > cap {
			gfGroupBits = cap
		}
	}
	return gfGroupBits
}

// libvpxGFBitsAllocation ports the libvpx vp8/encoder/firstpass.c
// gf_bits allocator: for the GF (or ARF when isARF=true), pre-clamp
// the boost via the GFQ_ADJUSTMENT scaling, apply min/max caps based
// on baseline_gf_interval, and compute
//
//	gf_bits = Boost * (gf_group_bits / allocation_chunks)
//
// with the libvpx >1000-boost halving guard. The two branches diverge:
//   - ARF (i==0 with source_alt_ref_pending):
//     Boost = (gfu_boost * 3 * GFQ_ADJUSTMENT) / (2 * 100) + interval*50
//     cap = (interval+1)*200, floor = 125
//     allocation_chunks = (interval+1)*100 + Boost
//   - GF: Boost = (gfu_boost * GFQ_ADJUSTMENT) / 100
//     cap = interval*150, floor = 125
//     allocation_chunks = interval*100 + (Boost - 100)
//
// gfuBoost is the libvpx `cpi->gfu_boost` (last_boost-equivalent),
// gfqAdjustment is `vp8_gf_boost_qadjustment[Q]`. interval is
// `baseline_gf_interval`.
func libvpxGFBitsAllocation(isARF bool, gfuBoost int, gfqAdjustment int, gfGroupBits int64, baselineGFInterval int) int {
	if gfGroupBits <= 0 || baselineGFInterval <= 0 {
		return 0
	}
	var boost, allocationChunks int
	if isARF {
		boost = (gfuBoost * 3 * gfqAdjustment) / (2 * 100)
		boost += baselineGFInterval * 50
		if cap := (baselineGFInterval + 1) * 200; boost > cap {
			boost = cap
		}
		if boost < 125 {
			boost = 125
		}
		allocationChunks = (baselineGFInterval+1)*100 + boost
	} else {
		boost = (gfuBoost * gfqAdjustment) / 100
		if cap := baselineGFInterval * 150; boost > cap {
			boost = cap
		}
		if boost < 125 {
			boost = 125
		}
		allocationChunks = baselineGFInterval*100 + (boost - 100)
	}
	for boost > 1000 {
		boost /= 2
		allocationChunks /= 2
		if allocationChunks <= 0 {
			return 0
		}
	}
	if allocationChunks <= 0 {
		return 0
	}
	gfBits := int(float64(boost) * (float64(gfGroupBits) / float64(allocationChunks)))
	if gfBits < 0 {
		gfBits = 0
	}
	return gfBits
}

// kfGroupModifiedError ports the inner-loop accumulator
//
//	kf_group_err += calculate_modified_err(cpi, this_frame);
//
// from libvpx vp8/encoder/firstpass.c find_next_key_frame: total
// modified error across the KF group starting at `frame` and lasting
// `framesToKey` frames. Returns 0 when stats are not loaded.
func (t *twoPassState) kfGroupModifiedError(frame uint64, framesToKey int) float64 {
	if !t.enabled() || framesToKey <= 0 || frame >= uint64(len(t.stats)) {
		return 0
	}
	end := frame + uint64(framesToKey)
	if end > uint64(len(t.stats)) {
		end = uint64(len(t.stats))
	}
	var sum float64
	for i := frame; i < end; i++ {
		sum += t.modifiedError(t.stats[i])
	}
	return sum
}

// kfGroupBits ports the libvpx vp8/encoder/firstpass.c
// find_next_key_frame KF-group bit allocation:
//
//	kf_group_bits = bits_left * (kf_group_err / modified_error_left)
//
// clamped by max_bits * frames_to_key (the per-frame ceiling). Returns
// 0 when stats are not loaded, when bits_left has been depleted, or
// when modified_error_left is 0 (libvpx's `if (bits_left > 0 &&
// modified_error_left > 0.0)` gate). The caller passes the libvpx
// frame_max_bits value (libvpx caps any single normal frame at this
// rate; defaults to av_per_frame_bandwidth * (max_section_pct/100)).
func (t *twoPassState) kfGroupBits(frame uint64, framesToKey int, maxBitsPerFrame int) int64 {
	if !t.enabled() || framesToKey <= 0 || t.bitsLeft <= 0 || t.errorLeft <= 0 {
		return 0
	}
	groupErr := t.kfGroupModifiedError(frame, framesToKey)
	if groupErr <= 0 {
		return 0
	}
	groupBits := int64(float64(t.bitsLeft) * (groupErr / t.errorLeft))
	if maxBitsPerFrame > 0 {
		maxGroupBits := int64(maxBitsPerFrame) * int64(framesToKey)
		if groupBits > maxGroupBits {
			groupBits = maxGroupBits
		}
	}
	if groupBits < 0 {
		groupBits = 0
	}
	return groupBits
}

// framesToKey ports a simplified `cpi->twopass.frames_to_key` lookahead
// from libvpx's vp8/encoder/firstpass.c find_next_key_frame: starting at
// `frame`, walk forward until libvpxTestCandidateKeyFrame fires (with
// the libvpx `i >= MIN_GF_INTERVAL` gate), or until the user-configured
// keyFrameInterval is exhausted, or until end-of-stats. Returns the
// number of frames remaining until the next predicted KF, including
// the current frame at index `frame`. Returns 0 when stats are not
// loaded or `frame` is past the end (libvpx falls back to default
// targets in that case).
func (t *twoPassState) framesToKey(frame uint64, keyFrameInterval int) int {
	if !t.enabled() || frame >= uint64(len(t.stats)) {
		return 0
	}
	maxLookahead := uint64(len(t.stats)) - frame
	if keyFrameInterval > 0 && uint64(2*keyFrameInterval) < maxLookahead {
		// libvpx breaks the loop when frames_to_key >= 2*key_freq.
		maxLookahead = uint64(2 * keyFrameInterval)
	}
	for i := uint64(1); i < maxLookahead; i++ {
		idx := frame + i
		if idx >= uint64(len(t.stats)) {
			break
		}
		// libvpx requires `i >= MIN_GF_INTERVAL` before firing the
		// candidate-KF predicate; mirror that gate.
		if int(i) >= libvpxMinGFInterval && libvpxTestCandidateKeyFrame(t.stats, int(idx)) {
			return int(i) + 1
		}
		if keyFrameInterval > 0 && int(i) >= keyFrameInterval {
			return int(i) + 1
		}
	}
	return int(maxLookahead)
}

func (t *twoPassState) markKeyFrame(frame uint64) {
	if t.enabled() {
		t.lastKeySeen = frame
	}
}

// libvpxGetPredictionDecayRate ports the libvpx
// vp8/encoder/firstpass.c get_prediction_decay_rate:
//
//	rate = pcnt_inter
//	motion_decay = 1.0 - (pcnt_motion / 20.0)
//	rate = min(rate, motion_decay)
//	mv_rabs = |mvr_abs * pcnt_motion|
//	mv_cabs = |mvc_abs * pcnt_motion|
//	distance_factor = sqrt(mv_rabs^2 + mv_cabs^2) / 250.0
//	distance_factor = (distance_factor > 1.0) ? 0.0 : (1.0 - distance_factor)
//	rate = min(rate, distance_factor)
func libvpxGetPredictionDecayRate(stats FirstPassFrameStats) float64 {
	rate := stats.PcntInter
	motionDecay := 1.0 - (stats.PcntMotion / 20.0)
	if motionDecay < rate {
		rate = motionDecay
	}
	mvRAbs := math.Abs(stats.MVrAbs * stats.PcntMotion)
	mvCAbs := math.Abs(stats.MVcAbs * stats.PcntMotion)
	distanceFactor := math.Sqrt(mvRAbs*mvRAbs+mvCAbs*mvCAbs) / 250.0
	if distanceFactor > 1.0 {
		distanceFactor = 0.0
	} else {
		distanceFactor = 1.0 - distanceFactor
	}
	if distanceFactor < rate {
		rate = distanceFactor
	}
	return rate
}

// libvpxDetectTransitionToStill ports the libvpx
// vp8/encoder/firstpass.c detect_transition_to_still: returns true
// when a complex transition is followed by a static section (used to
// trigger an extra KF for slide-show / fade content).
//
//	trans_to_still = (frameInterval > MIN_GF_INTERVAL) &&
//	                 (loop_decay_rate >= 0.999) &&
//	                 (decay_accumulator < 0.9) &&
//	                 (all next still_interval frames have
//	                   prediction_decay_rate >= 0.999)
//
// The lookahead-walk parameter `nextDecayRates` holds the decay rates
// for the next `still_interval` frames; libvpx peeks them from
// `cpi->twopass.stats_in` and resets the file position afterwards.
func libvpxDetectTransitionToStill(frameInterval int, stillInterval int, loopDecayRate float64, decayAccumulator float64, nextDecayRates []float64) bool {
	if frameInterval <= libvpxMinGFInterval {
		return false
	}
	if loopDecayRate < 0.999 || decayAccumulator >= 0.9 {
		return false
	}
	if stillInterval <= 0 {
		return false
	}
	limit := stillInterval
	if limit > len(nextDecayRates) {
		// libvpx returns false when the lookahead runs out before
		// still_interval frames have been examined.
		return false
	}
	for j := 0; j < limit; j++ {
		if nextDecayRates[j] < 0.999 {
			return false
		}
	}
	return true
}

// libvpxCalculateModifiedErr ports the libvpx vp8/encoder/firstpass.c
// calculate_modified_err formula:
//
//	av_err = total_ssim_weighted_pred_err / count
//	this_err = this_frame.ssim_weighted_pred_err
//	if this_err > av_err: modified = av_err * pow(this/av_err, POW1)
//	else:                  modified = av_err * pow(this/av_err, POW2)
//
// where POW1 == POW2 == oxcf.two_pass_vbrbias / 100. Mirrors the
// libvpx DOUBLE_DIVIDE_CHECK fallback for av_err==0.
func libvpxCalculateModifiedErr(thisErr float64, totalSSIMErr float64, count float64, vbrBiasPct int) float64 {
	if count <= 0 {
		return 0
	}
	avErr := totalSSIMErr / count
	avDenom := avErr
	if avDenom < 1e-12 && avDenom > -1e-12 {
		avDenom = 1.0
	}
	pow := float64(vbrBiasPct) / 100.0
	return avErr * math.Pow(thisErr/avDenom, pow)
}

func normalizeTwoPassStats(stats []FirstPassFrameStats) ([]FirstPassFrameStats, FirstPassFrameStats) {
	if len(stats) == 0 {
		return nil, FirstPassFrameStats{}
	}
	if len(stats) > 1 {
		last := stats[len(stats)-1]
		if last.Count > 1 && math.Abs(last.Count-float64(len(stats)-1)) < 1e-9 {
			return stats[:len(stats)-1], last
		}
	}
	var total FirstPassFrameStats
	for i := range stats {
		total.Frame += stats[i].Frame
		total.IntraError += stats[i].IntraError
		total.CodedError += stats[i].CodedError
		total.SSIMWeightedPredErr += stats[i].SSIMWeightedPredErr
		total.PcntInter += stats[i].PcntInter
		total.PcntMotion += stats[i].PcntMotion
		total.PcntSecondRef += stats[i].PcntSecondRef
		total.PcntNeutral += stats[i].PcntNeutral
		total.MVr += stats[i].MVr
		total.MVrAbs += stats[i].MVrAbs
		total.MVc += stats[i].MVc
		total.MVcAbs += stats[i].MVcAbs
		total.MVrv += stats[i].MVrv
		total.MVcv += stats[i].MVcv
		total.MVInOutCount += stats[i].MVInOutCount
		total.NewMVCount += stats[i].NewMVCount
		total.Duration += stats[i].Duration
		total.Count += stats[i].Count
	}
	return stats, total
}

func (t *twoPassState) modifiedError(stats FirstPassFrameStats) float64 {
	if t.totalStats.Count > 0 && t.totalStats.SSIMWeightedPredErr > 0 && stats.SSIMWeightedPredErr > 0 {
		if err := libvpxCalculateModifiedErr(stats.SSIMWeightedPredErr, t.totalStats.SSIMWeightedPredErr, t.totalStats.Count, t.vbrBiasPct); err > 0 {
			return err
		}
	}
	return twoPassModifiedError(stats, t.vbrBiasPct)
}

func twoPassModifiedError(stats FirstPassFrameStats, biasPct int) float64 {
	err := stats.CodedError
	if stats.SSIMWeightedPredErr > 0 {
		err = stats.SSIMWeightedPredErr
	}
	if err < 1 {
		err = 1
	}
	pow := float64(biasPct) / 100.0
	if pow <= 0 {
		return err
	}
	return math.Pow(err, pow)
}

func libvpxTestCandidateKeyFrame(stats []FirstPassFrameStats, idx int) bool {
	if idx <= 0 || idx+1 >= len(stats) {
		return false
	}
	lastFrame := stats[idx-1]
	thisFrame := stats[idx]
	nextFrame := stats[idx+1]
	if thisFrame.PcntSecondRef >= 0.10 || nextFrame.PcntSecondRef >= 0.10 {
		return false
	}
	if !((thisFrame.PcntInter < 0.05) ||
		(((thisFrame.PcntInter - thisFrame.PcntNeutral) < 0.25) &&
			((thisFrame.IntraError / doubleDivideCheck(thisFrame.CodedError)) < 2.5) &&
			((math.Abs(lastFrame.CodedError-thisFrame.CodedError)/doubleDivideCheck(thisFrame.CodedError) > 0.40) ||
				(math.Abs(lastFrame.IntraError-thisFrame.IntraError)/doubleDivideCheck(thisFrame.IntraError) > 0.40) ||
				((nextFrame.IntraError / doubleDivideCheck(nextFrame.CodedError)) > 3.5)))) {
		return false
	}
	boostScore := 0.0
	oldBoostScore := 0.0
	decayAccumulator := 1.0
	i := 0
	for ; i < 16 && idx+1+i < len(stats); i++ {
		localNext := stats[idx+1+i]
		nextIIRatio := libvpxIIKFactor1 * localNext.IntraError / doubleDivideCheck(localNext.CodedError)
		if nextIIRatio > libvpxRMax {
			nextIIRatio = libvpxRMax
		}
		if localNext.PcntInter > 0.85 {
			decayAccumulator *= localNext.PcntInter
		} else {
			decayAccumulator *= (0.85 + localNext.PcntInter) / 2.0
		}
		boostScore += decayAccumulator * nextIIRatio
		if localNext.PcntInter < 0.05 ||
			nextIIRatio < 1.5 ||
			(((localNext.PcntInter - localNext.PcntNeutral) < 0.20) && nextIIRatio < 3.0) ||
			((boostScore - oldBoostScore) < 0.5) ||
			localNext.IntraError < 200 {
			break
		}
		oldBoostScore = boostScore
	}
	return boostScore > 5.0 && i > 3
}

func doubleDivideCheck(v float64) float64 {
	if v < 0 {
		return v - 0.000001
	}
	return v + 0.000001
}
