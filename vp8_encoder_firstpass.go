package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
	"github.com/thesyncim/govpx/internal/vpx/geometry"
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

// FirstPassFrameStats mirrors libvpx FIRSTPASS_STATS for one analyzed frame or
// for the finalized sequence total.
type FirstPassFrameStats struct {
	// Frame is the source-frame ordinal accumulated by libvpx first pass.
	Frame uint64
	// IntraError is the intra prediction error.
	IntraError float64
	// CodedError is the selected coded prediction error.
	CodedError float64
	// SSIMWeightedPredErr is the SSIM-weighted prediction error.
	SSIMWeightedPredErr float64
	// PcntInter is the fraction of macroblocks coded as inter.
	PcntInter float64
	// PcntMotion is the fraction of macroblocks with non-zero motion.
	PcntMotion float64
	// PcntSecondRef is the fraction using the second reference.
	PcntSecondRef float64
	// PcntNeutral is libvpx's neutral-block fraction.
	PcntNeutral float64
	// MVr accumulates signed row motion vectors.
	MVr float64
	// MVrAbs accumulates absolute row motion vectors.
	MVrAbs float64
	// MVc accumulates signed column motion vectors.
	MVc float64
	// MVcAbs accumulates absolute column motion vectors.
	MVcAbs float64
	// MVrv accumulates row motion-vector variance terms.
	MVrv float64
	// MVcv accumulates column motion-vector variance terms.
	MVcv float64
	// MVInOutCount is libvpx's in/out motion-vector accumulator.
	MVInOutCount float64
	// NewMVCount counts macroblocks that selected a new motion vector.
	NewMVCount float64
	// Duration is the frame duration in caller timebase units.
	Duration float64
	// Count is the number of frames represented by this record.
	Count float64
	// IsTotal marks an entry as the libvpx terminal "total stats" packet
	// emitted at end-of-encode (vp8_end_first_pass). The total mirrors
	// cpi->twopass.total_stats: a running aggregate of every per-frame
	// FIRSTPASS_STATS produced by vp8_first_pass via accumulate_stats.
	// When set, the entry is the last element of a finalized stats slice
	// and is consumed by the second-pass setup driven by SetTwoPassStats.
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

// subtractFirstPassStats mirrors libvpx vp8/encoder/firstpass.c
// subtract_stats (firstpass.c lines 190-209): per-field subtraction of a
// per-frame FIRSTPASS_STATS record from a running section accumulator.
// vp8_second_pass calls this at end-of-frame on
// cpi->twopass.total_left_stats (firstpass.c line 2398), draining the
// just-encoded frame's stats so the next frame's estimate_modemvcost /
// estimate_max_q reads see the still-unencoded tail's averages.
//
// libvpx reference:
//
//	static void subtract_stats(FIRSTPASS_STATS *section,
//	                           FIRSTPASS_STATS *frame) {
//	  section->frame                  -= frame->frame;
//	  section->intra_error            -= frame->intra_error;
//	  section->coded_error            -= frame->coded_error;
//	  section->ssim_weighted_pred_err -= frame->ssim_weighted_pred_err;
//	  section->pcnt_inter             -= frame->pcnt_inter;
//	  section->pcnt_motion            -= frame->pcnt_motion;
//	  section->pcnt_second_ref        -= frame->pcnt_second_ref;
//	  section->pcnt_neutral           -= frame->pcnt_neutral;
//	  section->MVr                    -= frame->MVr;
//	  section->mvr_abs                -= frame->mvr_abs;
//	  section->MVc                    -= frame->MVc;
//	  section->mvc_abs                -= frame->mvc_abs;
//	  section->MVrv                   -= frame->MVrv;
//	  section->MVcv                   -= frame->MVcv;
//	  section->mv_in_out_count        -= frame->mv_in_out_count;
//	  section->new_mv_count           -= frame->new_mv_count;
//	  section->count                  -= frame->count;
//	  section->duration               -= frame->duration;
//	}
//
// Field order matches libvpx verbatim.
func subtractFirstPassStats(section *FirstPassFrameStats, frame FirstPassFrameStats) {
	if section == nil {
		return
	}
	section.Frame -= frame.Frame
	section.IntraError -= frame.IntraError
	section.CodedError -= frame.CodedError
	section.SSIMWeightedPredErr -= frame.SSIMWeightedPredErr
	section.PcntInter -= frame.PcntInter
	section.PcntMotion -= frame.PcntMotion
	section.PcntSecondRef -= frame.PcntSecondRef
	section.PcntNeutral -= frame.PcntNeutral
	section.MVr -= frame.MVr
	section.MVrAbs -= frame.MVrAbs
	section.MVc -= frame.MVc
	section.MVcAbs -= frame.MVcAbs
	section.MVrv -= frame.MVrv
	section.MVcv -= frame.MVcv
	section.MVInOutCount -= frame.MVInOutCount
	section.NewMVCount -= frame.NewMVCount
	section.Count -= frame.Count
	section.Duration -= frame.Duration
}

// FinalizeFirstPassStats appends the libvpx-style terminal total-stats
// record to a slice of per-frame [FirstPassFrameStats] records produced
// by [VP8Encoder.CollectFirstPassStats]. Each per-frame entry is folded
// into a running aggregate, which is appended with IsTotal=true so
// [VP8Encoder.SetTwoPassStats] can recover the sequence-wide totals
// libvpx's second pass expects.
//
// If the input already ends with an IsTotal entry, or is empty, the
// slice is returned unchanged.
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

// CollectFirstPassStats runs libvpx-style first-pass analysis on one
// source frame for two-pass VBR planning. The returned [FirstPassFrameStats]
// should be accumulated in a slice across all input frames and then
// passed through [FinalizeFirstPassStats] before being handed to a
// second-pass encoder via EncoderOptions.TwoPassStats or
// [VP8Encoder.SetTwoPassStats].
//
// First-pass analysis updates internal reference state but emits no VP8
// bitstream. pts is currently accepted for API symmetry but is not
// consumed; duration is recorded in the returned stats. flags accepts
// the same EncodeFlags as EncodeInto for validation.
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
		vp8common.CopyImage(&e.firstPassGoldenRef.Img, &e.firstPassLastRef.Img)
		e.firstPassGoldenRef.ExtendBorders()
	}

	vp8common.CopyImage(&e.firstPassLastRef.Img, &e.firstPassNewRef.Img)
	e.firstPassLastRef.ExtendBorders()
	vp8enc.CopySourceToFrameBuffer(&e.firstPassLastSource, srcImg)

	// Special case for the first frame (libvpx firstpass.c): copy LAST into
	// GF as a second reference.
	if e.firstPassCount == 0 {
		vp8common.CopyImage(&e.firstPassGoldenRef.Img, &e.firstPassLastRef.Img)
		e.firstPassGoldenRef.ExtendBorders()
	}
	e.firstPassCount++
	return stats, nil
}

func (e *VP8Encoder) computeFirstPassStats(src vp8enc.SourceImage, duration uint64) FirstPassFrameStats {
	rows := geometry.MacroblockRows(src.Height)
	cols := geometry.MacroblockCols(src.Width)
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
	vp8enc.CopySourceToFrameBuffer(&e.firstPassNewRef, src)
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
				intraErrorForMB = vp8enc.MacroblockMeanLumaSSE(src, row, col)
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
	if quant == nil || dequant == nil {
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
	vp8enc.ConvertMacroblockCoefficients(&coeffs, false, &tokens)
	return predictionSSE, reconstructAnalysisMacroblock(&e.firstPassNewRef.Img, mbRow, mbCol, &mode, &tokens, dequant, &e.reconstructScratch)
}

func (e *VP8Encoder) reconstructFirstPassBPredIntraMacroblock(src vp8enc.SourceImage, mbRow int, mbCol int, qIndex int, quant *vp8enc.MacroblockQuant) (int, bool) {
	if quant == nil {
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
	if quant == nil || dequant == nil {
		return false
	}
	mode := vp8enc.InterFrameMacroblockMode{
		RefFrame: vp8common.LastFrame,
		Mode:     vp8common.NewMV,
		UVMode:   vp8common.DCPred,
		MV:       mv,
	}
	var decMode vp8dec.MacroblockMode
	vp8enc.ConvertInterFrameMode(&mode, &decMode)
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
	vp8enc.ConvertMacroblockCoefficients(&coeffs, false, &tokens)
	decMode.MBSkipCoeff = vp8enc.MacroblockCoefficientsEmpty(&coeffs, false)
	return reconstructInterAnalysisMacroblock(&e.firstPassNewRef.Img, &e.firstPassLastRef.Img, mbRow, mbCol, &decMode, &tokens, dequant, &e.reconstructScratch)
}

func firstPassMotionSearch(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, seed vp8enc.MotionVector, qIndex int) (vp8enc.MotionVector, int, bool) {
	if ref == nil || ref.Width <= 0 || ref.Height <= 0 {
		return vp8enc.MotionVector{}, 0, false
	}
	mbRows := geometry.MacroblockRows(src.Height)
	mbCols := geometry.MacroblockCols(src.Width)
	bounds := vp8enc.InterFrameFullPixelSearchBounds(seed, mbRow, mbCol, mbRows, mbCols)
	center := bounds.ClampEighth(vp8enc.MotionVector{
		Row: int16(int(seed.Row) & ^7),
		Col: int16(int(seed.Col) & ^7),
	})
	searcher := newFullPelMotionSearch(src, ref, mbRow, mbCol, seed, qIndex, bounds, &vp8tables.DefaultMVContext, nil, 0, nil)
	// Mirror libvpx vp8_first_pass: vp8cx_initialize_me_consts is never
	// called before the first-pass loop, so x->sadperbit16 is the
	// zero-init calloc value (0) and mvsad_err_cost collapses to 0
	// inside diamond_search_sad. firstPassMode wires the diamond search
	// to use the zero-sadPerBit MV-SAD cost table accordingly, otherwise
	// govpx over-penalises off-center candidates and converges to a
	// different MV than libvpx — observed as frame-3+ MV-stats drift on
	// the F2 fuzz seed corpus (plan-§3 gap E Step 2).
	searcher.firstPassMode = true
	centerCost := searcher.walkCostNoStats(center, maxInt())
	search := interAnalysisSearchConfig{
		fullPixelSearchParam:  libvpxFirstPassSearchStepParam,
		fullPixelFurtherSteps: interFrameMaxMVSearchSteps - 1 - libvpxFirstPassSearchStepParam,
	}
	mv, cost := searcher.firstPassNstep(center, centerCost, search)
	return mv, cost, true
}

func (s *fullPelMotionSearch) firstPassNstep(center vp8enc.MotionVector, centerWalkCost int, search interAnalysisSearchConfig) (vp8enc.MotionVector, int) {
	stepParam := int(search.fullPixelSearchParam)
	if stepParam < 0 {
		stepParam = 0
	} else if stepParam >= interFrameMaxMVSearchSteps {
		stepParam = interFrameMaxMVSearchSteps - 1
	}

	result := s.firstPassSearchSites(center, centerWalkCost, stepParam)
	best := result.mv
	bestCost := result.cost
	n := int(result.num00)
	num00 := 0
	furtherSteps := int(search.fullPixelFurtherSteps)
	for n < furtherSteps {
		n++
		if num00 > 0 {
			num00--
			continue
		}
		candidate := s.firstPassSearchSites(center, centerWalkCost, stepParam+n)
		num00 = int(candidate.num00)
		if candidate.cost < bestCost {
			best = candidate.mv
			bestCost = candidate.cost
		}
	}
	return best, bestCost
}

func (s *fullPelMotionSearch) firstPassSearchSites(center vp8enc.MotionVector, centerWalkCost int, searchParam int) interFrameNstepSearchResult {
	result := s.searchSitesNoStats(center, centerWalkCost, vp8enc.InterFrameNstepSearchSites[:], 8, searchParam)
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
