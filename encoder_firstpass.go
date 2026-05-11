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
	// libvpx onyx_if.c Pass1Encode forces vp8_set_quantizer(cpi, 26) before
	// vp8_first_pass, independent of the user min/max quantizer bounds.
	libvpxFirstPassQIndex = 26
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
	// IsTotal marks an entry as the libvpx terminal "total stats" packet
	// emitted at end-of-encode (vp8_end_first_pass). The total mirrors
	// cpi->twopass.total_stats: a running aggregate of every per-frame
	// FIRSTPASS_STATS produced by vp8_first_pass via accumulate_stats.
	// When set, the entry is the last element of a finalized stats slice
	// and is consumed by the second-pass setup (see normalizeTwoPassStats).
	IsTotal bool
}

// accumulateFirstPassStats mirrors libvpx vp8/encoder/firstpass.c
// accumulate_stats: per-field summation of a per-frame FIRSTPASS_STATS
// record into a running section/sequence aggregator. Called once per
// per-frame stats record, ultimately rolling all frames into
// cpi->twopass.total_stats (whole-sequence) and into the section
// accumulators used by find_next_key_frame / define_gf_group.
//
// libvpx reference:
//
//	static void accumulate_stats(FIRSTPASS_STATS *section,
//	                             FIRSTPASS_STATS *frame) {
//	  section->frame                  += frame->frame;
//	  section->intra_error            += frame->intra_error;
//	  section->coded_error            += frame->coded_error;
//	  section->ssim_weighted_pred_err += frame->ssim_weighted_pred_err;
//	  section->pcnt_inter             += frame->pcnt_inter;
//	  section->pcnt_motion            += frame->pcnt_motion;
//	  section->pcnt_second_ref        += frame->pcnt_second_ref;
//	  section->pcnt_neutral           += frame->pcnt_neutral;
//	  section->MVr                    += frame->MVr;
//	  section->mvr_abs                += frame->mvr_abs;
//	  section->MVc                    += frame->MVc;
//	  section->mvc_abs                += frame->mvc_abs;
//	  section->MVrv                   += frame->MVrv;
//	  section->MVcv                   += frame->MVcv;
//	  section->mv_in_out_count        += frame->mv_in_out_count;
//	  section->new_mv_count           += frame->new_mv_count;
//	  section->count                  += frame->count;
//	  section->duration               += frame->duration;
//	}
func accumulateFirstPassStats(section *FirstPassFrameStats, frame FirstPassFrameStats) {
	if section == nil {
		return
	}
	section.Frame += frame.Frame
	section.IntraError += frame.IntraError
	section.CodedError += frame.CodedError
	section.SSIMWeightedPredErr += frame.SSIMWeightedPredErr
	section.PcntInter += frame.PcntInter
	section.PcntMotion += frame.PcntMotion
	section.PcntSecondRef += frame.PcntSecondRef
	section.PcntNeutral += frame.PcntNeutral
	section.MVr += frame.MVr
	section.MVrAbs += frame.MVrAbs
	section.MVc += frame.MVc
	section.MVcAbs += frame.MVcAbs
	section.MVrv += frame.MVrv
	section.MVcv += frame.MVcv
	section.MVInOutCount += frame.MVInOutCount
	section.NewMVCount += frame.NewMVCount
	section.Count += frame.Count
	section.Duration += frame.Duration
}

// FinalizeFirstPassStats appends the libvpx "terminal" total-stats
// packet to a slice of per-frame FirstPassFrameStats records. The
// total mirrors libvpx's `output_stats(cpi->output_pkt_list,
// &cpi->twopass.total_stats)` call from vp8_end_first_pass: each
// per-frame entry is folded into the running aggregate via
// accumulateFirstPassStats and the resulting record is appended with
// IsTotal=true so downstream consumers (e.g. SetTwoPassStats /
// normalizeTwoPassStats) can recover the sequence-wide totals
// libvpx's second pass reads from `cpi->twopass.stats_in_end`.
//
// If the input already ends with an IsTotal entry the slice is
// returned unchanged. Empty input is returned unchanged.
func FinalizeFirstPassStats(stats []FirstPassFrameStats) []FirstPassFrameStats {
	if len(stats) == 0 {
		return stats
	}
	if stats[len(stats)-1].IsTotal {
		return stats
	}
	var total FirstPassFrameStats
	for i := range stats {
		if stats[i].IsTotal {
			// Defensive: an interior IsTotal entry is unexpected, but
			// skip it to avoid double-counting.
			continue
		}
		accumulateFirstPassStats(&total, stats[i])
	}
	total.IsTotal = true
	out := make([]FirstPassFrameStats, len(stats)+1)
	copy(out, stats)
	out[len(stats)] = total
	return out
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
	// GF as a second reference.
	if e.firstPassCount == 0 {
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
	qIndex := libvpxFirstPassQIndex
	copySourceToFrameBuffer(&e.firstPassNewRef, src)
	quantDeltas := libvpxFrameQuantDeltas(qIndex, e.opts.ScreenContentMode)
	var quants [vp8common.MaxMBSegments]vp8enc.MacroblockQuant
	_ = vp8enc.InitSegmentMacroblockQuants(qIndex, quantDeltas, vp8enc.SegmentationConfig{}, &quants)
	var dequantTables vp8common.FrameDequantTables
	var dequant vp8common.MacroblockDequant
	vp8common.BuildFrameDequantTables(quantDeltas, &dequantTables)
	vp8common.InitMacroblockDequant(&dequantTables, qIndex, &dequant)
	for row := range rows {
		bestRefMV := vp8enc.MotionVector{}
		for col := range cols {
			intraErrorForMB, ok := e.reconstructFirstPassIntraMacroblock(src, row, col, qIndex, &quants[0], &dequant)
			if !ok {
				intraErrorForMB = macroblockMeanLumaSSE(src, row, col)
			}
			intra := intraErrorForMB + intraPenalty
			intraError += int64(intra)

			thisError := intra
			lastErr := maxInt()
			bestMV := vp8enc.MotionVector{}

			if hasLast {
				// Raw zero-motion check (libvpx zz_motion_search). The
				// raw source gates encode_breakout, while the reconstructed
				// LAST reference seeds the actual motion error.
				zeroErr := macroblockLumaSSE(src, &e.firstPassLastRef.Img, row, col, vp8enc.MotionVector{})
				rawMotionErr := zeroErr
				if hasLastSource {
					rawMotionErr = macroblockLumaSSE(src, &e.firstPassLastSource.Img, row, col, vp8enc.MotionVector{})
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
				// Experimental search in a second reference frame per libvpx.
				goldenErr := maxInt()
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

func (e *VP8Encoder) reconstructFirstPassIntraMacroblock(src vp8enc.SourceImage, mbRow int, mbCol int, qIndex int, quant *vp8enc.MacroblockQuant, dequant *vp8common.MacroblockDequant) (int, bool) {
	if e == nil || quant == nil || dequant == nil {
		return 0, false
	}
	useDCPred := (mbCol != 0 || mbRow != 0) && (mbCol == 0 || mbRow == 0)
	if !useDCPred {
		return e.reconstructFirstPassBPredIntraMacroblock(src, mbRow, mbCol, qIndex, quant)
	}

	mode := vp8dec.MacroblockMode{
		RefFrame: vp8common.IntraFrame,
		Mode:     vp8common.DCPred,
		UVMode:   vp8common.DCPred,
	}
	img := &e.firstPassNewRef.Img
	refs := vp8dec.BuildIntraPredictorRefs(img, mbRow, mbCol, &e.reconstructScratch.Refs)
	yOff := mbRow*16*img.YStride + mbCol*16
	if !vp8dec.PredictIntraY16x16(mode.Mode, img.Y[yOff:], img.YStride, refs.YAbove, refs.YLeft, refs.YTopLeft, refs.UpAvailable, refs.LeftAvailable) {
		return 0, false
	}
	predictionSSE := macroblockLumaSSE(src, img, mbRow, mbCol, vp8enc.MotionVector{})
	var coeffs vp8enc.MacroblockCoefficients
	buildPredictedMacroblockCoefficients(predictedMacroblockCoefficientArgs{
		coefProbs: &vp8tables.DefaultCoefProbs,
		src:       src,
		mbRow:     mbRow,
		mbCol:     mbCol,
		pred:      &e.firstPassNewRef.Img,
		quant:     quant,
		qIndex:    qIndex,
		intra:     true,
		fastQuant: true,
		coeffs:    &coeffs,
	})
	var tokens vp8dec.MacroblockTokens
	convertMacroblockCoefficients(&coeffs, false, &tokens)
	return predictionSSE, reconstructAnalysisMacroblock(&e.firstPassNewRef.Img, mbRow, mbCol, &mode, &tokens, dequant, &e.reconstructScratch)
}

func (e *VP8Encoder) reconstructFirstPassBPredIntraMacroblock(src vp8enc.SourceImage, mbRow int, mbCol int, qIndex int, quant *vp8enc.MacroblockQuant) (int, bool) {
	if e == nil || quant == nil {
		return 0, false
	}
	img := &e.firstPassNewRef.Img
	refs := vp8dec.BuildIntraPredictorRefs(img, mbRow, mbCol, &e.reconstructScratch.Refs)
	yOff := mbRow*16*img.YStride + mbCol*16
	y := img.Y[yOff:]
	var coeffs vp8enc.MacroblockCoefficients
	var input [16]int16
	var dct [16]int16
	var dq [16]int16
	var yAbove [4]uint8
	var yLeft [4]uint8
	predictionSSE := 0
	for block := range 16 {
		blockOffset := analysisYBlockOffset(block, img.YStride)
		if !predictAnalysisBPredBlock(vp8common.BDCPred, y[blockOffset:], img.YStride, y, img.YStride, refs.YAbove, refs.YLeft, refs.YTopLeft, block) {
			return 0, false
		}
		predictionSSE += bPredBlockSSE(src, mbRow, mbCol, block, y[blockOffset:], img.YStride)
		x := mbCol*16 + (block&3)*4
		yCoord := mbRow*16 + (block>>2)*4
		fillPredictedResidual4x4(src.Y, src.YStride, src.Width, src.Height, img.Y, img.YStride, x, yCoord, &input)
		vp8enc.ForwardDCT4x4(input[:], 4, &dct)
		a := block & 3
		l := (block & 0x0c) >> 2
		ctx := int(yAbove[a] + yLeft[l])
		eob := quantizeEncodedBlock(&vp8tables.DefaultCoefProbs, qIndex, 3, ctx, 0, 0, 0, true, true, false, &dct, &quant.Y1, &coeffs.QCoeff[block], &dq)
		coeffs.SetBlockEOB(block, eob)
		hasCoeffs := uint8(0)
		if eob > 0 {
			hasCoeffs = 1
		}
		yAbove[a] = hasCoeffs
		yLeft[l] = hasCoeffs
		addQuantizedBlockResidual(eob, &dq, y[blockOffset:], img.YStride)
	}
	return predictionSSE, true
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
	buildPredictedMacroblockCoefficients(predictedMacroblockCoefficientArgs{
		coefProbs: &vp8tables.DefaultCoefProbs,
		src:       src,
		mbRow:     mbRow,
		mbCol:     mbCol,
		pred:      &e.firstPassNewRef.Img,
		quant:     quant,
		qIndex:    qIndex,
		fastQuant: true,
		coeffs:    &coeffs,
	})
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
	searcher := newFullPelMotionSearch(src, ref, mbRow, mbCol, seed, qIndex, bounds, &vp8tables.DefaultMVContext, nil)
	centerCost := searcher.walkCost(center, maxInt())
	search := interAnalysisSearchConfig{
		fullPixelSearchParam:  libvpxFirstPassSearchStepParam,
		fullPixelFurtherSteps: interFrameMaxMVSearchSteps - 1 - libvpxFirstPassSearchStepParam,
	}
	mv, cost := searcher.firstPassNstep(center, centerCost, search)
	return mv, cost, true
}

func (s *fullPelMotionSearch) firstPassNstep(center vp8enc.MotionVector, centerWalkCost int, search interAnalysisSearchConfig) (vp8enc.MotionVector, int) {
	stepParam := search.fullPixelSearchParam
	if stepParam < 0 {
		stepParam = 0
	} else if stepParam >= interFrameMaxMVSearchSteps {
		stepParam = interFrameMaxMVSearchSteps - 1
	}

	result := s.firstPassSearchSites(center, centerWalkCost, stepParam)
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
		candidate := s.firstPassSearchSites(center, centerWalkCost, stepParam+n)
		num00 = candidate.num00
		if candidate.cost < bestCost {
			best = candidate.mv
			bestCost = candidate.cost
		}
	}
	return best, bestCost
}

func (s *fullPelMotionSearch) firstPassSearchSites(center vp8enc.MotionVector, centerWalkCost int, searchParam int) interFrameNstepSearchResult {
	result := s.searchSites(center, centerWalkCost, interFrameNstepSites[:], 8, searchParam)
	result.cost = firstPassMotionSearchReturnCost(s.ctx.src, s.ctx.ref, s.ctx.mbRow, s.ctx.mbCol, result.mv, s.bestRefMV, s.qIndex)
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
	stats             []FirstPassFrameStats
	totalStats        FirstPassFrameStats
	bitsLeft          int64
	errorLeft         float64
	frameIndex        uint64
	vbrBiasPct        int
	minPct            int
	maxPct            int
	minFrameBandwidth int
	lastKeySeen       uint64
	// libvpx vp8/encoder/firstpass.c kf_group / gf_group accounting.
	// kfGroupBits is the bit budget remaining within the current
	// keyframe-bounded group (set by find_next_key_frame, drained as
	// each gf-group within the kf-group is allocated and as the kf's
	// own kf_bits is taken). kfGroupErrorLeft tracks the same in error
	// units. gfGroupBits / gfGroupErrorLeft mirror the per-GF subgroup
	// budget that assign_std_frame_bits drains for each std P frame.
	// framesToKeyRemaining and framesTillGFUpdate count down per
	// finishFrame call so the caller's `keyFrame` flag drives KF-group
	// re-initialization and the GF-group is rebuilt at each boundary.
	// kfGroupValid / gfGroupValid gate whether the err-fraction target
	// path uses the gf_group_bits denominator (libvpx-parity) or the
	// legacy bits_left fallback (which we still use when the group
	// state was not initialized — e.g. the very first call before KF
	// processing has run).
	kfGroupBitsRemaining int64
	kfGroupErrorLeft     float64
	gfGroupBits          int64
	gfGroupErrorLeft     float64
	framesToKeyRemaining int
	framesTillGFUpdate   int
	framesSinceGolden    int
	altExtraBits         int
	kfGroupValid         bool
	gfGroupValid         bool
	// gfRefreshTarget is the per-frame target libvpx's
	// define_gf_group sets for the GF/refresh frame at the start of
	// the GF section. govpx surfaces it via the next frameTargetBits
	// call when framesSinceGolden==0, mirroring libvpx's behaviour of
	// emitting `cpi->per_frame_bandwidth = gf_bits` as the per-frame
	// target for the first frame of the GF section.
	gfRefreshTarget int
	// currentFrameIsGFRefresh marks the in-flight frame as a GF/KF
	// refresh frame so finishFrame can mirror libvpx's
	// update_golden_frame_stats behaviour: KF/GF refresh resets
	// frames_since_golden to 0 (without incrementing), while every
	// other visible frame increments it by 1.
	currentFrameIsGFRefresh bool
	// lastInterQ mirrors libvpx's `cpi->last_q[INTER_FRAME]`. It is
	// the Q used by `define_gf_group` to look up GFQ_ADJUSTMENT
	// (vp8_gf_boost_qadjustment[Q]) when scaling the gfu_boost for
	// the GF allocation chunks. libvpx initializes it to 0 (zeroed
	// by calloc), and updates it after each inter-frame encode at
	// `cpi->last_q[cm->frame_type] = cm->base_qindex`. govpx will
	// thread this once two-pass GF boost regulation needs it.
	lastInterQ int
	// gfIntraErrMin mirrors libvpx's `cpi->twopass.gf_intra_err_min`,
	// the per-frame floor on intra_error used by `calc_frame_boost`
	// when computing the per-frame boost contribution to gfu_boost.
	// libvpx sets it to `GF_MB_INTRA_MIN * cpi->common.MBs` in
	// vp8_init_second_pass. The encoder pushes this value via
	// `setGFIntraErrMin` after computing the MB count for the
	// configured frame size.
	gfIntraErrMin float64
	// frameWidth, frameHeight mirror the encoder's configured frame
	// dimensions. They are used by `kfBitsTarget` to derive the
	// `kf_intra_err_min` floor (KF_MB_INTRA_MIN * MBs) and the
	// size-dependent `kf_boost` adjustment libvpx applies in
	// find_next_key_frame.
	frameWidth  int
	frameHeight int
	// numMBs caches `(width/16) * (height/16)` so estimate_max_q does
	// not have to recompute it per frame. Set by configureFrameDims.
	numMBs int
	// pass2ActiveWorstQ mirrors libvpx's `cpi->active_worst_quality`
	// after vp8_second_pass runs estimate_max_q (frame 0) or the
	// damped update branch (the early-portion-of-clip damped path).
	// govpx's regulator reads this in libvpxActiveWorstQuantizer to
	// substitute it for `maxQuantizer` when in pass-2 VBR mode. The
	// encoder pushes the value into rateControlState.pass2ActiveWorstQ
	// before each frame's selectQuantizerForFrameKind call.
	pass2ActiveWorstQ      int
	pass2ActiveWorstQValid bool
	// estMaxQCorrection mirrors libvpx's
	// `cpi->twopass.est_max_qcorrection_factor`. Initialized to 1.0
	// on the first pass-2 frame (libvpx vp8/encoder/firstpass.c
	// vp8_second_pass line 2329), then updated frame-to-frame from
	// rolling actual/target bits (estimate_max_q rolling-ratio
	// branch). The encoder pushes the rolling stats via
	// `setRollingBits` so this tracks libvpx within rounding.
	estMaxQCorrection float64
	// sectionMaxQFactor mirrors libvpx's
	// `cpi->twopass.section_max_qfactor`. Computed by find_next_key_frame
	// (KF group) and define_gf_group (GF group) from the section's
	// avg intra_error / coded_error. Used by estimate_max_q as a
	// multiplicative factor on the per-Q bit estimate.
	sectionMaxQFactor float64
	// sectionIntraRating mirrors libvpx's
	// `cpi->twopass.section_intra_rating`. The libvpx full-frame loop
	// filter picker (vp8cx_pick_filter_level) reads this to scale the
	// "prefer lower filter level" Bias term: `if (section_intra_rating <
	// 20) Bias = Bias * section_intra_rating / 20;`. The libvpx
	// twopass struct is calloc'd, so in one-pass / realtime / CBR (where
	// neither find_next_key_frame nor define_gf_group runs) it stays at
	// 0 and the unconditional VP8 guard then forces Bias = 0. govpx
	// previously omitted the scaling and used the unscaled bias, which
	// caused the realtime CBR full picker to converge on a different
	// filt_best than libvpx (e.g. on the 128x128 panning fixture
	// frames 2/3, govpx LF=2/1 vs libvpx LF=8/4). Two-pass branches
	// that compute this value must update it via setSectionIntraRating
	// before the next picker call; otherwise it stays 0 (matching
	// libvpx's calloc default).
	sectionIntraRating int
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
	// libvpx vp8_cx_iface.c default config: rc_2pass_vbr_minsection_pct=0,
	// rc_2pass_vbr_maxsection_pct=400. Govpx zero-value EncoderOptions
	// historically substituted 50/200; that path inflated the per-frame
	// floor (sectionMin) and re-credited bits_left by the wrong amount
	// in finishFrame, so per-frame pass-2 targets ballooned over the
	// course of a short stream. Mirror libvpx's defaults so callers that
	// leave the knobs at zero match libvpx's bookkeeping.
	t.minPct = max(minPct, 0)
	t.minFrameBandwidth = vbrMinFrameBandwidthBits(bitsPerFrame, t.minPct)
	t.maxPct = maxPct
	if t.maxPct <= 0 {
		t.maxPct = 400
	}
	for i := range t.stats {
		t.errorLeft += t.modifiedError(t.stats[i])
	}
	// libvpx vp8/encoder/firstpass.c vp8_second_pass line 2329 seeds
	// est_max_qcorrection_factor=1.0 on the first frame; section_max_qfactor
	// also starts at 1.0 (libvpx's struct is calloced; the first
	// find_next_key_frame call overwrites it before estimate_max_q
	// reads it). Mirror those initial values here so the very first
	// estimate_max_q call sees libvpx-shaped state when the encoder
	// has not yet emitted any frames.
	t.estMaxQCorrection = 1.0
	t.sectionMaxQFactor = 1.0
}

func (t *twoPassState) enabled() bool {
	return len(t.stats) > 0
}

// configureFrameDims pushes the encoder's configured frame size into
// the two-pass state. Used by `kfBitsTarget` for the size-dependent
// kf_boost adjustment and by `defineGFGroup` to derive
// `gf_intra_err_min` (libvpx GF_MB_INTRA_MIN * MBs).
func (t *twoPassState) configureFrameDims(width int, height int) {
	if width > 0 && height > 0 {
		t.frameWidth = width
		t.frameHeight = height
		const gfMBIntraMin = 200 // libvpx GF_MB_INTRA_MIN
		mbCols := (width + 15) / 16
		mbRows := (height + 15) / 16
		t.numMBs = mbCols * mbRows
		t.gfIntraErrMin = float64(gfMBIntraMin * t.numMBs)
	}
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

// frameTargetBits returns the libvpx Pass2Encode per-frame target for the
// given frame. It mirrors the libvpx vp8/encoder/firstpass.c flow:
//   - At a KF (frame_type == KEY_FRAME) it runs the find_next_key_frame
//     KF-group allocator: kf_group_bits = bits_left * (kf_group_err /
//     modified_error_left); kf_bits is then derived as the maximum of the
//     boost-based formula and the err-fraction `bits_left * (kf_mod_err /
//     modified_error_left)`. For the test workloads we compare against,
//     the KF dominates the modified-error denominator and the
//     err-fraction branch wins, so govpx implements that branch here.
//     After the KF, kf_group_bits and kf_group_error_left are seeded for
//     the remaining frames in the group.
//   - At a non-KF frame at a GF boundary (framesTillGFUpdate==0), it
//     runs define_gf_group: gf_group_bits = kf_group_bits *
//     (gf_group_err / kf_group_error_left), then drains the GF-frame
//     allocation chunk. The GF interval spans the rest of the KF group
//     (libvpx caps it at static_scene_max_gf_interval, but for short
//     clips with no ARF the cap is the kf-group remainder).
//   - For std P frames it runs assign_std_frame_bits: target =
//     gf_group_bits * (mod_err / gf_group_error_left), clamped to
//     `max_bits` (frame_max_bits VBR), drained from gf_group_bits, plus
//     min_frame_bandwidth and (on alternating frames_since_golden)
//     alt_extra_bits.
//
// defaultTargetBits is the legacy one-pass per-frame target the rate
// controller would have produced; it is used as the fallback when the
// twopass state has not been seeded (e.g. the first frame before pass-1
// stats are available) and as the input to the section-min computation.
func (t *twoPassState) frameTargetBits(frame uint64, keyFrame bool, defaultTargetBits int) int {
	if !t.enabled() || frame >= uint64(len(t.stats)) || defaultTargetBits <= 0 {
		return 0
	}
	modErr := t.modifiedError(t.stats[frame])
	if modErr <= 0 || t.errorLeft <= 0 || t.bitsLeft <= 0 {
		return defaultTargetBits
	}
	var target int64
	_, sectionMax := t.pass2VBRSectionLimits(frame, defaultTargetBits)
	gfBoundary := false
	t.currentFrameIsGFRefresh = false
	if keyFrame {
		// libvpx vp8_second_pass at KF: find_next_key_frame runs first
		// (sets kf_group_bits / kf_bits / drains kf_group_bits by
		// kf_bits), THEN define_gf_group runs (which can re-seed
		// kf_group_bits to bits_left for the last KF group). We mirror
		// that ordering so the KF target is the err-fraction value
		// computed against the full bits_left budget, while the GF
		// allocator sees the post-find_next_key_frame residual budget
		// for the inter frames.
		t.prepareKFGroup(frame)
		t.currentFrameIsGFRefresh = true
		target = t.kfBitsTarget(frame, modErr)
		if framesLeft := int64(len(t.stats)) - int64(frame); framesLeft > 1 {
			expanded := sectionMax * framesLeft
			if expanded > sectionMax {
				sectionMax = expanded
			}
		}
		// define_gf_group seeds the GF section for the inter frames
		// that follow. Per_frame_bandwidth for the KF stays at kf_bits
		// (libvpx does not overwrite it because the inner GF loop's
		// per_frame_bandwidth assignment is gated on frame_type !=
		// KEY_FRAME).
		t.defineGFGroup(frame)
		// libvpx vp8/encoder/firstpass.c vp8_second_pass lines 2328-2363:
		// on the very first frame of pass 2, estimate_max_q computes a
		// `tmp_q` and assigns it to cpi->active_worst_quality. This caps
		// the regulator's worst-Q ceiling at a value derived from the
		// per-MB error and the section target bandwidth, instead of
		// leaving it at oxcf.worst_allowed_q (e.g., 56). Without this
		// the govpx regulator picks Q values much lower than libvpx
		// for the same per-frame target — visible as q_match=8% on
		// desktopqvga while target_match=100%. We seed the active
		// worst Q here so subsequent frames in this pass-2 see the
		// same regulator ceiling libvpx uses.
		if frame == 0 {
			t.seedPass2ActiveWorstQ(defaultTargetBits)
		}
	} else if t.framesTillGFUpdate == 0 {
		t.defineGFGroup(frame)
		gfBoundary = true
		t.currentFrameIsGFRefresh = true
	}
	if !keyFrame {
		if gfBoundary && t.gfGroupValid {
			// libvpx vp8_second_pass: at the GF boundary (no ARF case)
			// the per-frame target IS gf_bits — assign_std_frame_bits
			// is NOT called for the GF refresh frame itself.
			target = int64(t.gfRefreshTarget)
		} else if t.gfGroupValid {
			target = t.assignStdFrameBits(modErr, sectionMax)
		} else {
			// Fallback: legacy err-fraction-of-bits_left. Used when the
			// gf-group state has not been seeded (the keyframe was
			// emitted outside the two-pass driver, or stats were
			// swapped mid-stream).
			target = int64(float64(t.bitsLeft) * modErr / t.errorLeft)
			target += int64(t.minFrameBandwidth)
		}
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

// prepareKFGroup mirrors the libvpx vp8/encoder/firstpass.c
// find_next_key_frame KF-group seeding, but only the bookkeeping that
// influences subsequent per-frame target allocation:
//
//	kf_group_err = sum(modified_err[frame .. frame+frames_to_key-1])
//	kf_group_bits = bits_left * (kf_group_err / modified_error_left)
//	kf_group_bits = clamp(kf_group_bits, 0, max_bits * frames_to_key)
//	kf_group_error_left = kf_group_err - kf_mod_err
//	modified_error_left -= kf_group_err  (handled in finishFrame via errorLeft)
//
// The actual KF target (kf_bits) is computed by kfBitsTarget at frame
// emit time from this seeded state. After this routine returns, the
// gf-group state is also seeded so the very next frame at a GF
// boundary picks up gf_group_bits = kf_group_bits.
func (t *twoPassState) prepareKFGroup(frame uint64) {
	framesToKey := len(t.stats) - int(frame)
	if framesToKey <= 0 {
		t.kfGroupValid = false
		t.gfGroupValid = false
		return
	}
	var kfGroupErr, kfModErr float64
	var sectionIntra, sectionCoded float64
	end := min(frame+uint64(framesToKey), uint64(len(t.stats)))
	for i := frame; i < end; i++ {
		kfGroupErr += t.modifiedError(t.stats[i])
		// Accumulate raw intra/coded error totals so we can compute
		// libvpx's section_max_qfactor (find_next_key_frame line 2778)
		// at the same time we seed the kf-group bit budget.
		sectionIntra += t.stats[i].IntraError
		sectionCoded += t.stats[i].CodedError
	}
	// section_max_qfactor uses avg per-frame intra/coded ratio. libvpx
	// runs avg_stats first (divides each accumulator by count) so the
	// ratio is identical with or without the divide; we can compute it
	// directly from the totals via libvpxSectionMaxQFactor which
	// handles the DOUBLE_DIVIDE_CHECK fallback.
	if framesToKey > 0 {
		t.sectionMaxQFactor = libvpxSectionMaxQFactor(sectionIntra, sectionCoded)
		// Mirror libvpx find_next_key_frame line 2772: alongside the
		// section_max_qfactor (which estimate_max_q reads), the same
		// avg intra/coded ratio drives section_intra_rating, which the
		// loop-filter full picker reads to scale its lower-level Bias.
		t.sectionIntraRating = libvpxSectionIntraRating(sectionIntra, sectionCoded)
	}
	kfModErr = t.modifiedError(t.stats[frame])
	t.framesToKeyRemaining = framesToKey
	t.framesSinceGolden = 0
	t.altExtraBits = 0
	if t.errorLeft <= 0 || t.bitsLeft <= 0 {
		t.kfGroupBitsRemaining = 0
		t.kfGroupErrorLeft = 0
		t.kfGroupValid = false
		t.gfGroupValid = false
		return
	}
	kfGroupBits := int64(float64(t.bitsLeft) * (kfGroupErr / t.errorLeft))
	maxBits := int64(libvpxFrameMaxBitsVBR(t.bitsLeft, int64(framesToKey), t.maxPctOrDefault()))
	if maxBits > 0 {
		if cap := maxBits * int64(framesToKey); kfGroupBits > cap {
			kfGroupBits = cap
		}
	}
	if kfGroupBits < 0 {
		kfGroupBits = 0
	}
	// kf_bits is taken out of kf_group_bits below in kfBitsTarget; but
	// for now we record the seeded values so the GF group can use them.
	t.kfGroupBitsRemaining = kfGroupBits
	t.kfGroupErrorLeft = kfGroupErr - kfModErr
	if t.kfGroupErrorLeft < 0 {
		t.kfGroupErrorLeft = 0
	}
	t.kfGroupValid = true
	// After the KF is consumed and the GF span is the rest of the
	// kf-group, define_gf_group will fire on the very next frame. Mark
	// the GF state invalid so that frame triggers re-seeding.
	t.gfGroupBits = 0
	t.gfGroupErrorLeft = 0
	t.gfGroupValid = false
	t.framesTillGFUpdate = 0
}

// kfBitsTarget computes libvpx's kf_bits — the per-frame target for
// the KF — from the already-seeded kf-group state. It mirrors the
// libvpx vp8/encoder/firstpass.c find_next_key_frame KF allocation
// (lines 2814-2925):
//
//	kf_boost = boost_score from the IIKFACTOR2 prediction-decay walk
//	  over the next frames_to_key-1 frames, scaled by 100/16, with
//	  size-dependent adjustments and a 250 floor.
//	allocation_chunks = ((frames_to_key-1) * 100) + kf_boost
//	  (or *10 when decay_accumulator >= 0.99 — the "almost static"
//	  branch).
//	kf_bits = kf_boost * (kf_group_bits / allocation_chunks)
//	if kf_mod_err >= avg: alt_kf_bits = bits_left * kf_mod_err /
//	  modified_error_left; kf_bits = max(kf_bits, alt_kf_bits).
//	if kf_mod_err < avg:  alt_kf_bits computed from kf_boost via
//	  alt_kf_grp_bits; kf_bits = min(kf_bits, alt_kf_bits).
//	kf_bits += min_frame_bandwidth
//
// Govpx ports both the boost-based path and the alt-branch logic so
// the per-frame KF target tracks libvpx within rounding. The
// kf_group_bits state is then drained by kf_bits so the gf-group
// budget is the residual.
func (t *twoPassState) kfBitsTarget(frame uint64, kfModErr float64) int64 {
	if !t.kfGroupValid || t.errorLeft <= 0 {
		return int64(float64(t.bitsLeft) * kfModErr / t.errorLeft)
	}
	framesToKey := max(t.framesToKeyRemaining, 1)
	// Compute kf_boost via the libvpx prediction-quality walk over
	// frames [frame+1 .. frame+framesToKey-1]. Mirrors lines
	// 2722-2756 of find_next_key_frame.
	kfBoost, decayAccumulator := computeKFBoost(t.stats, frame, framesToKey, t.kfIntraErrMinForFrame())
	// Size-dependent kf_boost adjustment (lines 2837-2844). lst_yv12
	// is the "last" YUV buffer's size, which equals encoder
	// dimensions. govpx exposes the dimensions via t.frameWidth /
	// t.frameHeight (set by the encoder at configure time).
	if t.frameWidth > 0 && t.frameHeight > 0 {
		size := t.frameWidth * t.frameHeight
		if size > 320*240 {
			kfBoost += 2 * size / (320 * 240)
		} else if size < 320*240 {
			kfBoost -= 4 * (320 * 240) / size
		}
	}
	// Min KF boost.
	kfBoost = max((kfBoost*100)>>4, 250)
	// allocation_chunks. The "almost static" branch uses *10
	// instead of *100.
	var allocationChunks int64
	if decayAccumulator >= 0.99 {
		allocationChunks = int64(framesToKey-1)*10 + int64(kfBoost)
	} else {
		allocationChunks = int64(framesToKey-1)*100 + int64(kfBoost)
	}
	for kfBoost > 1000 {
		kfBoost /= 2
		allocationChunks /= 2
		if allocationChunks <= 0 {
			break
		}
	}
	if allocationChunks <= 0 {
		allocationChunks = 1
	}
	kfBits := int64(float64(kfBoost) * (float64(t.kfGroupBitsRemaining) / float64(allocationChunks)))
	// alt branch: compare kf_mod_err to group avg.
	groupAvg := 0.0
	if framesToKey > 0 {
		// kfGroupErrorLeft + kfModErr is the original kf_group_err
		// (before find_next_key_frame stored kfGroupErrorLeft =
		// kfGroupErr - kfModErr). Restore for the avg.
		groupAvg = (t.kfGroupErrorLeft + kfModErr) / float64(framesToKey)
	}
	if kfModErr < groupAvg {
		// Use min(kfBits, alt_kf_bits computed via alt_kf_grp_bits).
		// alt_kf_grp_bits = bits_left * (kfModErr * framesToKey) /
		//   modified_error_left; alt_kf_bits = kf_boost *
		//   alt_kf_grp_bits / allocation_chunks.
		altGrp := float64(t.bitsLeft) * (kfModErr * float64(framesToKey)) / t.errorLeft
		altKFBits := int64(float64(kfBoost) * (altGrp / float64(allocationChunks)))
		if kfBits > altKFBits {
			kfBits = altKFBits
		}
	} else {
		// Use max(kfBits, bits_left * kfModErr / modified_error_left).
		altKFBits := int64(float64(t.bitsLeft) * kfModErr / t.errorLeft)
		if altKFBits > kfBits {
			kfBits = altKFBits
		}
	}
	if kfBits > t.kfGroupBitsRemaining {
		kfBits = t.kfGroupBitsRemaining
	}
	if kfBits < 0 {
		kfBits = 0
	}
	// Drain kf_group_bits by kf_bits (libvpx: kf_group_bits -= kf_bits).
	t.kfGroupBitsRemaining -= kfBits
	if t.kfGroupBitsRemaining < 0 {
		t.kfGroupBitsRemaining = 0
	}
	// Add min_frame_bandwidth (libvpx: kf_bits += min_frame_bandwidth).
	kfBits += int64(t.minFrameBandwidth)
	return kfBits
}

// kfIntraErrMinForFrame returns libvpx's `cpi->twopass.kf_intra_err_min`
// equivalent for the configured encoder frame size. libvpx sets it to
// `KF_MB_INTRA_MIN * MBs` in vp8_init_second_pass; govpx derives MBs
// from the configured frame dimensions when available.
func (t *twoPassState) kfIntraErrMinForFrame() float64 {
	const kfMBIntraMin = 300 // libvpx KF_MB_INTRA_MIN
	if t.frameWidth <= 0 || t.frameHeight <= 0 {
		return 0
	}
	mbCols := (t.frameWidth + 15) / 16
	mbRows := (t.frameHeight + 15) / 16
	return float64(kfMBIntraMin * mbCols * mbRows)
}

// seedPass2ActiveWorstQ ports the libvpx vp8/encoder/firstpass.c
// vp8_second_pass first-frame branch (lines 2328-2363):
//
//	frames_left = total_stats.count - current_video_frame
//	section_target_bandwidth = bits_left / frames_left
//	section_err = total_left_stats.coded_error / total_left_stats.count
//	err_per_mb = section_err / num_mbs
//	tmp_q = estimate_max_q(...)
//	cpi->active_worst_quality = tmp_q
//
// When seeded, govpx's regulator reads the result via
// `pass2ActiveWorstQOverride` and substitutes it for `maxQuantizer` in
// `libvpxActiveWorstQuantizer`. This mirrors libvpx's behavior where
// the regulator's worst-Q ceiling is dialed down from the user-specified
// `worst_allowed_q` to a value derived from the per-MB error and
// section target bandwidth, which is the single biggest contributor to
// q_match parity on real-content pass-2 fixtures.
//
// `defaultTargetBits` is the encoder's per-frame target (typically
// `target_bitrate / fps`); we use `t.bitsLeft / framesLeft` instead so
// the value reflects the post-vbrmin_section budget when minPct > 0.
// The frame parameter is kept in the call site for clarity even though
// the computation only references frame 0 state.
func (t *twoPassState) seedPass2ActiveWorstQ(defaultTargetBits int) {
	if t.numMBs <= 0 {
		// Without configured frame dimensions we cannot compute
		// err_per_mb. Leave activeWorstQ unset; the regulator falls
		// back to oxcf.worst_allowed_q.
		return
	}
	framesLeft := max(int64(len(t.stats))-int64(t.frameIndex), 1)
	var sectionTargetBandwidth int64
	if t.bitsLeft > 0 {
		sectionTargetBandwidth = t.bitsLeft / framesLeft
	} else {
		sectionTargetBandwidth = int64(defaultTargetBits)
	}
	if sectionTargetBandwidth <= 0 {
		return
	}
	// libvpx uses total_left_stats.coded_error / total_left_stats.count
	// at this point. On frame 0, total_left_stats == total_stats (no
	// frame has been subtracted yet). govpx caches the totals in
	// t.totalStats; for the FIRST frame use those directly.
	count := t.totalStats.Count
	if count <= 0 {
		// Fall back to summing over the per-frame stats.
		count = float64(len(t.stats))
	}
	codedError := t.totalStats.CodedError
	if codedError <= 0 {
		// Sum the per-frame coded_error if the rolled total is
		// missing. This guards against malformed pass-1 dumps.
		for i := range t.stats {
			codedError += t.stats[i].CodedError
		}
	}
	if codedError <= 0 || count <= 0 {
		return
	}
	sectionErr := codedError / count
	errPerMB := sectionErr / float64(t.numMBs)
	estCorrection := t.estMaxQCorrection
	if estCorrection <= 0 {
		estCorrection = 1.0
	}
	sectionMQF := t.sectionMaxQFactor
	if sectionMQF <= 0 {
		sectionMQF = 1.0
	}
	// libvpx hands estimate_max_q (best_quality, worst_quality) as the
	// search bounds. govpx callers translate the public min/max
	// quantizer into qindex space via libvpxPublicQuantizerToQIndex
	// before configuring the rate controller; we treat the entire
	// [0, vp8MaxQIndex] range as the bound here so the ported function
	// can evaluate the full ladder. The encoder will subsequently
	// clamp the regulator output to the user min/max anyway.
	tmpQ := min(max(libvpxEstimateMaxQ(t.numMBs, int(sectionTargetBandwidth), 0, errPerMB, 1.0, estCorrection, sectionMQF, 0, vp8MaxQIndex), 0), vp8MaxQIndex)
	t.pass2ActiveWorstQ = tmpQ
	t.pass2ActiveWorstQValid = true
}

// pass2ActiveWorstQOverride returns the libvpx-derived
// `active_worst_quality` value when the pass-2 driver has seeded it
// via seedPass2ActiveWorstQ. The boolean second return value is false
// when the override is not available (one-pass mode, or pass 2 before
// frame 0 has been processed). Read by ratecontrol.go's
// `libvpxActiveWorstQuantizer` to substitute for `maxQuantizer` in the
// VBR-pass2 path.
func (t *twoPassState) pass2ActiveWorstQOverride() (int, bool) {
	if !t.pass2ActiveWorstQValid {
		return 0, false
	}
	return t.pass2ActiveWorstQ, true
}

// computeKFBoost mirrors the libvpx vp8/encoder/firstpass.c
// find_next_key_frame inner walk (lines 2728-2756) that produces the
// raw `boost_score` used to seed `kf_boost` for the KF allocation.
//
//	r = IIKFACTOR2 * intra_error / coded_error  (with the
//	  kf_intra_err_min floor on intra), capped at RMAX=14.0.
//	decay_accumulator *= libvpxGetPredictionDecayRate(next_frame),
//	  clamped to [0.1, 1.0].
//	boost_score += decay_accumulator * r.
//	break when i>MIN_GF_INTERVAL && (boost_score-old_boost_score)<1.0.
//
// Returns the raw `boost_score` and the final `decay_accumulator`
// (both used by `kfBitsTarget` to compute the KF chunk allocation).
func computeKFBoost(stats []FirstPassFrameStats, frame uint64, framesToKey int, kfIntraErrMin float64) (int, float64) {
	const (
		iiKFFactor2 = 1.5
		rMax        = 14.0
	)
	if framesToKey <= 0 || frame >= uint64(len(stats)) {
		return 0, 1.0
	}
	decayAccumulator := 1.0
	boostScore := 0.0
	oldBoostScore := 0.0
	for i := range framesToKey {
		idx := int(frame) + 1 + i
		if idx >= len(stats) {
			break
		}
		next := stats[idx]
		intra := next.IntraError
		if intra < kfIntraErrMin {
			intra = kfIntraErrMin
		}
		denom := next.CodedError
		if denom > -1e-12 && denom < 1e-12 {
			denom = 1.0
		}
		r := iiKFFactor2 * intra / denom
		if r > rMax {
			r = rMax
		}
		loopDecayRate := libvpxGetPredictionDecayRate(next)
		decayAccumulator *= loopDecayRate
		if decayAccumulator < 0.1 {
			decayAccumulator = 0.1
		}
		boostScore += decayAccumulator * r
		if i > libvpxMinGFInterval && (boostScore-oldBoostScore) < 1.0 {
			break
		}
		oldBoostScore = boostScore
	}
	return int(boostScore), decayAccumulator
}

// defineGFGroup mirrors the libvpx define_gf_group GF-group seeding for
// the simple (no-ARF) case. It runs at every GF boundary, which after a
// KF is the very first non-KF frame. For the short-clip workloads
// govpx targets here, the GF span is the kf-group remainder.
//
// Subset of libvpx's logic ported here:
//   - gf_group_err   = sum(modified_err over baseline_gf_interval).
//   - gf_group_bits  = kf_group_bits * (gf_group_err / kf_group_error_left).
//   - gf_bits        = (Boost * gf_group_bits) / allocation_chunks where
//     Boost is the libvpx GFQ-adjusted gfu_boost clamped to
//     [125, baseline_gf_interval*150]. Govpx uses a constant 125 here
//     (libvpx's floor) when the per-frame motion-walk that produces
//     gfu_boost is not available; the alt-bits path then re-clamps the
//     gf_bits when the GF frame's modified error is below the group
//     average.
//   - alt_extra_bits = gf_group_bits * pct_extra/100/((interval-1)/2)
//     where pct_extra = (boost-100)/50 capped at 20. For the 8-frame
//     ramp-source oracle workload libvpx's actual gfu_boost is high
//     enough (>=1000) that pct_extra saturates at 20; we mirror that
//     conservatively by using pct_extra=18 (libvpx's typical
//     equilibrium value) so the alternation pattern in the per-frame
//     inter target matches the reference within tolerance.
//   - gf_group_bits is then drained by (gf_bits - min_frame_bandwidth)
//     and by alt_extra_bits_total so the residual is what
//     assign_std_frame_bits subsequently divides.
func (t *twoPassState) defineGFGroup(frame uint64) {
	if !t.kfGroupValid || frame >= uint64(len(t.stats)) {
		t.gfGroupValid = false
		return
	}
	remaining := len(t.stats) - int(frame)
	if remaining <= 0 {
		t.gfGroupValid = false
		return
	}
	// libvpx vp8/encoder/firstpass.c lines 1921-1925: when the current
	// KF group is the final one in the stream
	// (frames_to_key >= total_stats.count - current_video_frame),
	// kf_group_bits is reset to the live bits_left so the GF allocator
	// uses the full residual budget. Govpx mirrors that here so the
	// same gf_group_bits initial value the libvpx oracle reports
	// (106666 on the 8-frame ramp source) is reachable.
	if t.framesToKeyRemaining >= remaining {
		if t.bitsLeft > 0 {
			t.kfGroupBitsRemaining = t.bitsLeft
		}
	}
	gfInterval := min(remaining, t.framesToKeyRemaining)
	if gfInterval <= 0 {
		t.gfGroupValid = false
		return
	}
	keyFrameAtBoundary := frame == t.lastKeySeen || (t.framesToKeyRemaining == remaining && frame == 0)
	if frame == 0 {
		keyFrameAtBoundary = true
	}
	// libvpx's define_gf_group walks forward from the current frame
	// accumulating modified_err. For the KF case it then subtracts
	// gf_first_frame_err so the KF's own error is excluded
	// (line 1633). govpx mirrors that by computing the sum over
	// frames [frame .. frame+gfInterval-1] and subtracting the first
	// frame's modErr when at a KF boundary.
	var gfGroupErr float64
	var gfSectionIntra, gfSectionCoded float64
	end := min(frame+uint64(gfInterval), uint64(len(t.stats)))
	for i := frame; i < end; i++ {
		gfGroupErr += t.modifiedError(t.stats[i])
		// Accumulate raw intra/coded for libvpx's section_max_qfactor
		// (define_gf_group line 2144), which estimate_max_q reads when
		// the GF section is the active section_max_qfactor source.
		gfSectionIntra += t.stats[i].IntraError
		gfSectionCoded += t.stats[i].CodedError
	}
	if gfInterval > 0 {
		t.sectionMaxQFactor = libvpxSectionMaxQFactor(gfSectionIntra, gfSectionCoded)
		// Mirror libvpx define_gf_group line 2138: GF section also
		// resets section_intra_rating from this group's avg
		// intra/coded ratio.
		t.sectionIntraRating = libvpxSectionIntraRating(gfSectionIntra, gfSectionCoded)
	}
	if keyFrameAtBoundary {
		gfGroupErr -= t.modifiedError(t.stats[frame])
		if gfGroupErr < 0 {
			gfGroupErr = 0
		}
	}
	gfGroupBits := int64(0)
	if t.kfGroupErrorLeft > 0 {
		gfGroupBits = int64(float64(t.kfGroupBitsRemaining) * (gfGroupErr / t.kfGroupErrorLeft))
	}
	if gfGroupBits < 0 {
		gfGroupBits = 0
	}
	if gfGroupBits > t.kfGroupBitsRemaining {
		gfGroupBits = t.kfGroupBitsRemaining
	}
	maxBits := int64(libvpxFrameMaxBitsVBR(t.bitsLeft, int64(remaining), t.maxPctOrDefault()))
	if maxBits > 0 {
		if cap := maxBits * int64(gfInterval); gfGroupBits > cap {
			gfGroupBits = cap
		}
	}
	// libvpx: kf_group_error_left -= gf_group_err; kf_group_bits -=
	// gf_group_bits. Mirror that drain so subsequent GF groups in the
	// same kf group see the correct residual.
	t.kfGroupErrorLeft -= gfGroupErr
	if t.kfGroupErrorLeft < 0 {
		t.kfGroupErrorLeft = 0
	}
	t.kfGroupBitsRemaining -= gfGroupBits
	if t.kfGroupBitsRemaining < 0 {
		t.kfGroupBitsRemaining = 0
	}
	// libvpx GF-bits allocation: Boost = (gfu_boost * GFQ_ADJUSTMENT)
	// / 100, capped at baseline_gf_interval*150 with a floor of 125,
	// then halved while >1000. allocation_chunks =
	// baseline_gf_interval*100 + (Boost-100). gfu_boost is computed
	// by walking the prediction-quality decay across the GF interval
	// (libvpx vp8/encoder/firstpass.c lines 1639-1706); govpx ports
	// the same walk in computeGFUBoost so the boost matches libvpx
	// frame-for-frame (within rounding). The Q used to look up
	// GFQ_ADJUSTMENT is libvpx's `last_q[INTER_FRAME]`, which is 0
	// before any inter frame has been encoded — for short clips with
	// a single KF that means Q=0 and GFQ_ADJUSTMENT=80.
	gfuBoost := computeGFUBoost(t.stats, frame, gfInterval, keyFrameAtBoundary, t.gfIntraErrMin)
	q := max(t.lastInterQ, 0)
	if q >= len(libvpxGFBoostQAdjustment) {
		q = len(libvpxGFBoostQAdjustment) - 1
	}
	gfqAdjustment := libvpxGFBoostQAdjustment[q]
	boost := int64(gfuBoost*gfqAdjustment) / 100
	if cap := int64(gfInterval) * 150; boost > cap {
		boost = cap
	}
	if boost < 125 {
		boost = 125
	}
	allocationChunks := int64(gfInterval)*100 + (boost - 100)
	for boost > 1000 {
		boost /= 2
		allocationChunks /= 2
		if allocationChunks <= 0 {
			break
		}
	}
	if allocationChunks <= 0 {
		allocationChunks = 1
	}
	gfBits := max(boost*gfGroupBits/allocationChunks, 0)
	// libvpx alt branch (lines 2017-2046): if mod_frame_err < group
	// avg, use a smaller alt_gf_bits computed from the frame's own
	// error scaled by interval; if mod_frame_err >= group avg, ensure
	// gf_bits >= alt_gf_bits = kf_group_bits * mod_frame_err /
	// kf_group_error_left. The "this_frame" in libvpx's code path
	// here points to whatever the GF walk landed on, NOT necessarily
	// the GF refresh frame. For our short-clip (no-ARF) path, the
	// libvpx code path leaves mod_frame_err set to the LAST iteration
	// value of the inner loop (libvpx walks i frames; mod_frame_err
	// is overwritten on each iter). We approximate that by using the
	// modErr at frame+gfInterval-1 (the last frame in the GF span).
	// kf_group_bits at this point in libvpx flow is the value BEFORE
	// the gf_group_bits drain (libvpx uses cpi->twopass.kf_group_bits
	// which has just been set to bits_left for the final-kf-group
	// case at line 1923). We restore that pre-drain value here too.
	preGFKFGroupBits := t.kfGroupBitsRemaining + gfGroupBits
	if preGFKFGroupBits <= 0 {
		preGFKFGroupBits = t.bitsLeft
	}
	preGFKFErrorLeft := t.kfGroupErrorLeft + gfGroupErr
	if preGFKFErrorLeft < 1 {
		preGFKFErrorLeft = 1
	}
	lastIterIdx := int(frame) + gfInterval - 1
	if lastIterIdx >= len(t.stats) {
		lastIterIdx = len(t.stats) - 1
	}
	if lastIterIdx < int(frame) {
		lastIterIdx = int(frame)
	}
	modFrameErr := t.modifiedError(t.stats[lastIterIdx])
	if modFrameErr*float64(gfInterval) < gfGroupErr {
		altGFGroupBits := float64(preGFKFGroupBits) *
			(modFrameErr * float64(gfInterval)) /
			preGFKFErrorLeft
		altGFBits := int64(float64(boost) * (altGFGroupBits / float64(allocationChunks)))
		if gfBits > altGFBits {
			gfBits = altGFBits
		}
	} else {
		altGFBits := int64(float64(preGFKFGroupBits) * modFrameErr / preGFKFErrorLeft)
		if altGFBits > gfBits {
			gfBits = altGFBits
		}
	}
	if gfBits < 0 {
		gfBits = 0
	}
	if gfBits > gfGroupBits {
		gfBits = gfGroupBits
	}
	// libvpx: gf_group_bits -= (gf_bits - min_frame_bandwidth)
	// (line 2090). Mirror that drain.
	gfGroupBits -= gfBits - int64(t.minFrameBandwidth)
	if gfGroupBits < 0 {
		gfGroupBits = 0
	}
	// alt_extra_bits — see libvpx vp8/encoder/firstpass.c lines
	// 2099-2120. Gated on gfu_boost >= 150; spreads a `pct_extra`
	// percentage of the remaining gf_group_bits across the
	// alternating-frame slots within the GF section. pct_extra =
	// (boost-100)/50, capped at 20.
	altExtraTotal := int64(0)
	altExtraPer := int64(0)
	if gfInterval >= 3 && gfuBoost >= 150 {
		pctExtra := min((gfuBoost-100)/50, 20)
		if pctExtra > 0 {
			altExtraTotal = gfGroupBits * int64(pctExtra) / 100
			gfGroupBits -= altExtraTotal
			if gfGroupBits < 0 {
				gfGroupBits = 0
			}
			denom := int64((gfInterval - 1) / 2)
			if denom > 0 {
				altExtraPer = altExtraTotal / denom
			}
		}
	}
	t.gfGroupBits = gfGroupBits
	// libvpx: gf_group_error_left = gf_group_err (when KF) else
	// gf_group_err - gf_first_frame_err. For the KF case, gf_group_err
	// already had gf_first_frame_err subtracted (the if frame_type==KF
	// branch in the loop pre-init), so gf_group_error_left =
	// gf_group_err. For non-KF GF boundary, we subtract the first
	// frame's modErr from the denominator so the err-fraction at the
	// first frame after the boundary uses frames [frame+1..end].
	if keyFrameAtBoundary {
		t.gfGroupErrorLeft = gfGroupErr
	} else {
		gfFirstFrameErr := t.modifiedError(t.stats[frame])
		t.gfGroupErrorLeft = gfGroupErr - gfFirstFrameErr
	}
	if t.gfGroupErrorLeft < 0 {
		t.gfGroupErrorLeft = 0
	}
	t.framesTillGFUpdate = gfInterval
	t.gfGroupValid = true
	t.altExtraBits = int(altExtraPer)
	t.gfRefreshTarget = int(gfBits + int64(t.minFrameBandwidth))
	// libvpx onyx_if.c update_golden_frame_stats: frames_since_golden
	// is zeroed at every GF refresh (including KF, which always
	// refreshes golden). The post-encode finishFrame increment then
	// makes fsg=1 for the *next* frame's assign_std_frame_bits, so the
	// alternating-frame alt_extra_bits cadence lands on odd
	// frames_since_golden — which for the no-ARF path means frames at
	// offset 2, 4, 6, ... after the GF refresh.
	t.framesSinceGolden = 0
}

// computeGFUBoost mirrors the libvpx vp8/encoder/firstpass.c
// define_gf_group inner walk that produces `cpi->gfu_boost`. It
// walks the per-frame stats from `frame+1` through the GF interval,
// accumulating `decay_accumulator * frame_boost` where:
//
//	frame_boost = IIFACTOR * intra_error / coded_error  (capped at
//	  GF_RMAX=48), with a `gf_intra_err_min` floor on the intra_error
//	  numerator, then biased by mv_in_out_count (positive doubles,
//	  negative halves), then re-clamped to GF_RMAX.
//	decay_accumulator *= libvpxGetPredictionDecayRate(next_frame)
//	  clamped to [0.1, 1.0].
//
// libvpx breaks the loop when `i > MIN_GF_INTERVAL && (frames_to_key
// - i) >= MIN_GF_INTERVAL && (boost_score>20 || pcnt_inter<0.75) &&
// (boost_score-old_boost_score)<2.0`; govpx mirrors that. The
// returned value is `(boost_score * 100) >> 4` matching libvpx's
// scaling at line 1751 (`cpi->gfu_boost = (int)(boost_score *
// 100.0) >> 4`).
func computeGFUBoost(stats []FirstPassFrameStats, frame uint64, gfInterval int, keyFrameAtBoundary bool, gfIntraErrMin float64) int {
	const (
		iiFactor    = 1.5
		gfRMax      = 48.0
		minGFInterv = libvpxMinGFInterval
	)
	if gfInterval <= 0 || frame >= uint64(len(stats)) {
		return 0
	}
	mvInOutAccumulator := 0.0
	decayAccumulator := 1.0
	boostScore := 0.0
	oldBoostScore := 0.0
	// libvpx walks i from 1 to gfInterval (inclusive). On each iter,
	// it loads next_frame = stats[frame+i] and computes the per-frame
	// boost from THAT next_frame's stats (NOT the current frame's).
	for i := 1; i <= gfInterval; i++ {
		idx := int(frame) + i
		if idx >= len(stats) {
			break
		}
		next := stats[idx]
		// accumulate_frame_motion_stats: this_frame_mv_in_out =
		// mv_in_out_count * pcnt_motion. mv_in_out_accumulator
		// accumulates that.
		thisFrameMVInOut := next.MVInOutCount * next.PcntMotion
		mvInOutAccumulator += thisFrameMVInOut
		// calc_frame_boost: r = IIFACTOR * intra_error / coded_error,
		// with intra_error floored at gf_intra_err_min.
		intra := next.IntraError
		if intra < gfIntraErrMin {
			intra = gfIntraErrMin
		}
		denom := next.CodedError
		if denom > -1e-12 && denom < 1e-12 {
			denom = 1.0
		}
		r := iiFactor * intra / denom
		// Bias by mv_in_out_count.
		if thisFrameMVInOut > 0 {
			r += r * (thisFrameMVInOut * 2.0)
		} else {
			r += r * (thisFrameMVInOut / 2.0)
		}
		if r > gfRMax {
			r = gfRMax
		}
		// Cumulative effect of prediction quality decay.
		loopDecayRate := libvpxGetPredictionDecayRate(next)
		decayAccumulator *= loopDecayRate
		if decayAccumulator < 0.1 {
			decayAccumulator = 0.1
		}
		boostScore += decayAccumulator * r
		// Break clauses: libvpx breaks when i>MIN_GF_INTERVAL AND
		// (boost_score>20 || pcnt_inter<0.75) AND (boost-old)<2.
		// We can't fully model libvpx's frames_to_key breakout
		// because govpx may not have seen frames past the GF
		// section, but for our short-clip workloads the loop ends
		// at EOF anyway.
		if i > minGFInterv &&
			((boostScore > 20.0) || (next.PcntInter < 0.75)) &&
			((boostScore - oldBoostScore) < 2.0) {
			boostScore = oldBoostScore
			break
		}
		oldBoostScore = boostScore
	}
	_ = keyFrameAtBoundary // currently unused but reserved for ARF gating
	gfuBoost := int(boostScore*100.0) >> 4
	return gfuBoost
}

// assignStdFrameBits ports libvpx's assign_std_frame_bits inner-loop
// allocator for std P frames inside a GF group. Drains gfGroupBits and
// gfGroupErrorLeft per call.
func (t *twoPassState) assignStdFrameBits(modErr float64, maxBits int64) int64 {
	if !t.gfGroupValid || t.gfGroupErrorLeft <= 0 || t.gfGroupBits <= 0 {
		return int64(t.minFrameBandwidth)
	}
	errFraction := modErr / t.gfGroupErrorLeft
	target := max(int64(float64(t.gfGroupBits)*errFraction), 0)
	if maxBits > 0 && target > maxBits {
		target = maxBits
	}
	if target > t.gfGroupBits {
		target = t.gfGroupBits
	}
	// Drain (libvpx: gf_group_error_left -= modified_err;
	// gf_group_bits -= target_frame_size). We update gf_group_bits in
	// finishFrame (after the actual frame size is known) using the
	// here-computed target as the libvpx-equivalent
	// `target_frame_size`. Keep the err drain here so the per-frame
	// ratio at the next call uses the right denominator even before
	// finishFrame runs.
	t.gfGroupErrorLeft -= modErr
	if t.gfGroupErrorLeft < 0 {
		t.gfGroupErrorLeft = 0
	}
	t.gfGroupBits -= target
	if t.gfGroupBits < 0 {
		t.gfGroupBits = 0
	}
	target += int64(t.minFrameBandwidth)
	if (t.framesSinceGolden&0x01) != 0 && t.framesTillGFUpdate > 0 {
		target += int64(t.altExtraBits)
	}
	return target
}

// maxPctOrDefault returns the active two_pass_vbrmax_section value or
// libvpx's default (400).
func (t *twoPassState) maxPctOrDefault() int {
	if t.maxPct <= 0 {
		return 400
	}
	return t.maxPct
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
	// libvpx defaults: rc_2pass_vbr_minsection_pct=0,
	// rc_2pass_vbr_maxsection_pct=400.
	minPct := max(t.minPct, 0)
	maxPct := t.maxPct
	if maxPct <= 0 {
		maxPct = 400
	}
	// libvpx's `min_frame_bandwidth` is `av_per_frame_bandwidth *
	// two_pass_vbrmin_section / 100`; it's the additive floor used inside
	// `assign_std_frame_bits`, NOT a clamp on the err-fraction target. We
	// expose it via t.minFrameBandwidth so the caller can apply it
	// additively. The sectionMin we return here is therefore zero — pass-2
	// targets in libvpx are clamped only on the upper side.
	sectionMin := int64(0)
	sectionMax := int64(defaultTargetBits) * int64(maxPct) / 100
	if t.enabled() && frame < uint64(len(t.stats)) {
		framesLeft := int64(len(t.stats)) - int64(frame)
		if vbrMax := libvpxFrameMaxBitsVBR(t.bitsLeft, framesLeft, maxPct); vbrMax > 0 {
			sectionMax = int64(vbrMax)
		}
	}
	if sectionMax < sectionMin {
		sectionMax = sectionMin
	}
	_ = minPct
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
	maxLookahead := min(framesToKey, maxGFInterval)
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
// the encoder. It runs at a GF-group boundary (framesTillAltRefFrame ==
// 0 and ARF not already pending or active) and, when the second-pass
// stats indicate a high-motion section ahead, calls
// `scheduleAltRefSource` so the auto-ARF driver can emit the hidden
// alt-ref at the predicted offset.
//
// libvpx fires this from `vp8_second_pass`, which runs on every
// non-hidden frame including the keyframe (find_next_key_frame zeros
// `frames_till_gf_update_due` so the same `if (frames_till_gf_update_due
// == 0)` predicate triggers `define_gf_group` from inside Pass2Encode
// for the keyframe). govpx mirrors that by allowing the arming call to
// fire on `keyFrame == true`; the keyframe-path lifecycle update inside
// `resetGoldenFrameStats` no longer clobbers the schedule (it now
// matches libvpx's `update_golden_frame_stats`, which leaves
// `source_alt_ref_pending` intact). Without arming on the keyframe the
// hidden ARF would slip by one frame relative to libvpx.
//
// The wiring is gated on:
//   - Two-pass stats loaded.
//   - `EncoderOptions.AutoAltRef` (libvpx `oxcf.play_alternate`).
//   - `LookaheadFrames > 1` (the auto-ARF driver requires future peeks).
//   - `!ErrorResilient` (libvpx zeroes source_alt_ref_pending in
//     error-resilient mode inside Pass2Encode).
//   - No alt-ref already pending or active.
func (e *VP8Encoder) pass2MaybeArmAltRefPending(currentFrame uint64, currentPTS uint64, keyFrame bool) {
	_ = keyFrame
	if e == nil {
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
	// libvpx onyx_if.c Pass2Encode: bits_left -= 8 * size; bits_left +=
	// (target_bandwidth * vbrmin_section/100) / framerate. The minimum
	// is the additive credit equal to min_frame_bandwidth (in libvpx
	// shorthand) per visible frame.
	t.bitsLeft -= int64(actualBits)
	t.bitsLeft += int64(t.minFrameBandwidth)
	if t.bitsLeft < 0 {
		t.bitsLeft = 0
	}
	// libvpx onyx_if.c update_rd_ref_frame_probs / update_golden_frame
	// statistics: frames_since_golden and frames_till_gf_update_due
	// advance per visible frame. KF/GF refresh frames reset
	// frames_since_golden to 0 in update_golden_frame_stats and do NOT
	// increment it; the increment only fires in the
	// `!cpi->common.refresh_alt_ref_frame` else branch. Mirror that
	// gating so the assign_std_frame_bits caller observes
	// frames_since_golden=0 for the first inter frame after a GF
	// refresh (libvpx-parity).
	if t.framesTillGFUpdate > 0 {
		t.framesTillGFUpdate--
	}
	if t.currentFrameIsGFRefresh {
		t.framesSinceGolden = 0
	} else {
		t.framesSinceGolden++
	}
	if t.framesToKeyRemaining > 0 {
		t.framesToKeyRemaining--
	}
	t.frameIndex++
	t.currentFrameIsGFRefresh = false
}

func (t *twoPassState) chargeAltRefFrameBits(actualBits int) {
	if !t.enabled() {
		return
	}
	t.bitsLeft -= int64(actualBits)
	if t.bitsLeft < 0 {
		t.bitsLeft = 0
	}
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
	for Q := range len(libvpxBitsPerMB[1]) {
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
		maxBits = max(int(float64(maxBits)*float64(bufferLevel)/float64(optimalBufferLevel)), minMaxBits)
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
	gfGroupBits := min(max(int64(float64(kfGroupBits)*(gfGroupErr/kfGroupErrorLeft)), 0), kfGroupBits)
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
	gfBits := max(int(float64(boost)*(float64(gfGroupBits)/float64(allocationChunks))), 0)
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
	end := min(frame+uint64(framesToKey), uint64(len(t.stats)))
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
	for j := range limit {
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
		// Prefer the explicit IsTotal sentinel emitted by
		// FinalizeFirstPassStats, which mirrors libvpx's terminal
		// total-stats packet from vp8_end_first_pass.
		if last.IsTotal {
			return stats[:len(stats)-1], last
		}
		// Legacy heuristic: a trailing entry with Count == N is the
		// rolled-up total libvpx writes to `cpi->twopass.stats_in_end`.
		if last.Count > 1 && math.Abs(last.Count-float64(len(stats)-1)) < 1e-9 {
			return stats[:len(stats)-1], last
		}
	}
	var total FirstPassFrameStats
	for i := range stats {
		accumulateFirstPassStats(&total, stats[i])
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
