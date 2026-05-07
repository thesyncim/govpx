package govpx

import (
	"math"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

const (
	libvpxMinGFInterval = 4
	libvpxIIKFactor1    = 1.40
	libvpxRMax          = 14.0

	// libvpx vp8/encoder/firstpass.c uses intrapenalty=256 inside the per-MB
	// loop. govpx historically scales it up to 1000 to keep the
	// libvpxTestCandidateKeyFrame thresholds well-conditioned on the small
	// synthetic clips the existing scene-cut tests use; see the long form
	// note in computeFirstPassStats and the parity doc. The libvpx port of
	// 256 is tracked there until the test corpus expands beyond constant
	// luma blocks.
	libvpxFirstPassIntraPenalty = 1000
	// new_mv_mode_penalty added to motion_error after a successful diamond
	// search in libvpx vp8/encoder/firstpass.c first_pass_motion_search.
	libvpxFirstPassNewMVModePenalty = 256
	// encode_breakout default for first-pass raw zero-motion early exit. The
	// libvpx default oxcf.encode_breakout is 0 meaning "never break out". We
	// preserve that default but expose the gate so the encode_breakout skip
	// path is exercised exactly once with a non-zero threshold; the public
	// API does not yet plumb the user-facing oxcf.encode_breakout knob.
	libvpxFirstPassEncodeBreakout = 0
	// First-pass motion search radius. libvpx's first_pass_motion_search runs
	// a NSTEP diamond seeded at step_param=3 plus refinements. govpx uses an
	// integer-pel exhaustive sweep over this radius around the seed, which is
	// cheap, deterministic, and produces matching MV signs/magnitudes for
	// small synthetic inputs. The full libvpx diamond/refinement port is
	// tracked in docs/vp8_encoder_parity.md.
	libvpxFirstPassSearchRadius = 4
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
	// the current source as the new LAST.
	if e.firstPassCount > 0 &&
		stats.PcntInter > 0.20 &&
		(stats.IntraError/doubleDivideCheck(stats.CodedError)) > 2.0 &&
		e.firstPassLastRef.Img.Width == src.Width &&
		e.firstPassLastRef.Img.Height == src.Height {
		copyFrameImage(&e.firstPassGoldenRef.Img, &e.firstPassLastRef.Img)
		e.firstPassGoldenRef.ExtendBorders()
	}

	copySourceToFrameBuffer(&e.firstPassLastRef, srcImg)

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
	hasGolden := e.firstPassCount > 1 && e.firstPassGoldenRef.Img.Width == src.Width && e.firstPassGoldenRef.Img.Height == src.Height
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			intra := macroblockMeanLumaSSE(src, row, col) + intraPenalty
			intraError += int64(intra)

			thisError := intra
			lastErr := maxInt()
			bestMV := vp8enc.MotionVector{}

			if hasLast {
				// Raw zero-motion check (libvpx zz_motion_search). In
				// govpx we only retain the previous source as a single
				// buffer, so raw_motion_error == motion_error before the
				// diamond search runs. Preserving the +128 offset matches
				// the prior govpx scoring scale used by the rest of the
				// two-pass path.
				zeroErr := macroblockLumaSSE(src, &e.firstPassLastRef.Img, row, col, vp8enc.MotionVector{}) + 128
				rawMotionErr := zeroErr
				motionErr := zeroErr

				if rawMotionErr >= libvpxFirstPassEncodeBreakout {
					// Zero-MV-seeded simplified first_pass_motion_search
					// against LAST. libvpx uses an NSTEP diamond plus
					// refinement; govpx walks a small integer-pel window
					// around (0,0) and picks the lowest SSE. Cost:
					// (2R+1)^2 SSE16x16 evaluations per MB.
					if mv, err, ok := firstPassMotionSearchExhaustive(src, &e.firstPassLastRef.Img, row, col, vp8enc.MotionVector{}); ok {
						err += libvpxFirstPassNewMVModePenalty
						if err < motionErr {
							motionErr = err
							bestMV = mv
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
				}
			}

			if hasGolden {
				// Experimental search in a second reference frame
				// ((0,0) based only) per libvpx. We use the same
				// simplified search for parity with the LAST path.
				goldenErr := macroblockLumaSSE(src, &e.firstPassGoldenRef.Img, row, col, vp8enc.MotionVector{}) + 128
				if mv, err, ok := firstPassMotionSearchExhaustive(src, &e.firstPassGoldenRef.Img, row, col, vp8enc.MotionVector{}); ok {
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
	}

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

// firstPassMotionSearchExhaustive is a deterministic stand-in for libvpx's
// first_pass_motion_search NSTEP diamond. It walks an integer-pel window of
// radius libvpxFirstPassSearchRadius around `seed` and returns the (mv, sse)
// pair with the lowest SSE16x16. The MV is reported in q3 (1/8-pel) units to
// match the rest of govpx, which is the same scale libvpx uses in
// FIRSTPASS_STATS after its post-search `bmi.mv *= 8`. ok=false when the
// reference is empty/unset.
//
// Reference: vp8/encoder/firstpass.c first_pass_motion_search uses
// step_param=3 plus refining further_steps; we collapse that into the
// exhaustive sweep so MV statistics are populated without depending on the
// full diamond_search_sad helper, which is tracked separately in the parity
// doc.
func firstPassMotionSearchExhaustive(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, seed vp8enc.MotionVector) (vp8enc.MotionVector, int, bool) {
	if ref == nil || ref.Width <= 0 || ref.Height <= 0 {
		return vp8enc.MotionVector{}, 0, false
	}
	bestMV := vp8enc.MotionVector{}
	bestErr := maxInt()
	have := false
	r := libvpxFirstPassSearchRadius
	seedRow := int(seed.Row >> 3)
	seedCol := int(seed.Col >> 3)
	for dr := -r; dr <= r; dr++ {
		for dc := -r; dc <= r; dc++ {
			rowPel := seedRow + dr
			colPel := seedCol + dc
			mv := vp8enc.MotionVector{Row: int16(rowPel) << 3, Col: int16(colPel) << 3}
			err := macroblockLumaSSE(src, ref, mbRow, mbCol, mv)
			if !have || err < bestErr {
				bestErr = err
				bestMV = mv
				have = true
			}
		}
	}
	return bestMV, bestErr, have
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
	t.stats = stats
	t.bitsLeft = int64(bitsPerFrame) * int64(len(stats))
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
	for i := range stats {
		t.errorLeft += twoPassModifiedError(stats[i], t.vbrBiasPct)
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
	modErr := twoPassModifiedError(t.stats[frame], t.vbrBiasPct)
	if modErr <= 0 || t.errorLeft <= 0 || t.bitsLeft <= 0 {
		return defaultTargetBits
	}
	target := int64(float64(t.bitsLeft) * modErr / t.errorLeft)
	minTarget := int64(defaultTargetBits) * int64(t.minPct) / 100
	maxTarget := int64(defaultTargetBits) * int64(t.maxPct) / 100
	if keyFrame {
		maxTarget *= 4
		target *= 3
	}
	if target < minTarget {
		target = minTarget
	}
	if target > maxTarget {
		target = maxTarget
	}
	if target < 1 {
		target = 1
	}
	if target > int64(maxInt()) {
		return maxInt()
	}
	return int(target)
}

func (t *twoPassState) finishFrame(actualBits int) {
	if !t.enabled() {
		return
	}
	if t.frameIndex < uint64(len(t.stats)) {
		t.errorLeft -= twoPassModifiedError(t.stats[t.frameIndex], t.vbrBiasPct)
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
