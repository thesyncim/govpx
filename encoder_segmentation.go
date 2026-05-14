package govpx

import (
	"sync/atomic"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

// Ported from libvpx v1.16.0 vp8/encoder/onyx_if.c cyclic background
// refresh setup. StaticThreshold itself feeds encode_breakout; cyclic refresh
// segmentation is enabled independently for CBR and error-resilient encodes.

const staticSegmentID = 1

func (e *VP8Encoder) cyclicRefreshSegmentationConfig(refreshGolden bool) vp8enc.SegmentationConfig {
	return e.cyclicRefreshSegmentationConfigForQuantizer(refreshGolden, e.rc.currentQuantizer)
}

func (e *VP8Encoder) cyclicRefreshSegmentationConfigForQuantizer(refreshGolden bool, q int) vp8enc.SegmentationConfig {
	if !e.cyclicRefreshModeEnabled(refreshGolden) {
		return vp8enc.SegmentationConfig{}
	}
	return e.cyclicRefreshSegmentationConfigForQuantizerUnchecked(q)
}

func (e *VP8Encoder) cyclicRefreshSegmentationConfigForQuantizerUnchecked(q int) vp8enc.SegmentationConfig {
	cfg := vp8enc.SegmentationConfig{
		Enabled:    true,
		UpdateMap:  true,
		UpdateData: true,
	}
	if e.aggressiveDenoiseSegmentationActiveForQuantizer(q) {
		// libvpx onyx_if.c cyclic_background_refresh: under aggressive
		// denoising, drop the cyclic Q delta and instead ship an alt-LF
		// delta of -40 so segment 1 macroblocks (steady ZEROMV-LAST) get
		// loop-filter-suppressed to avoid dot artifacts.
		cfg.FeatureEnabled[vp8common.MBLvlAltLF][staticSegmentID] = true
		cfg.FeatureData[vp8common.MBLvlAltLF][staticSegmentID] = aggressiveDenoiseAltLFDelta
		return cfg
	}
	if delta := cyclicRefreshQuantizerDeltaForQuantizer(q); delta != 0 {
		cfg.FeatureEnabled[vp8common.MBLvlAltQ][staticSegmentID] = true
		cfg.FeatureData[vp8common.MBLvlAltQ][staticSegmentID] = delta
	}
	return cfg
}

// aggressiveDenoiseAltLFDelta mirrors libvpx's lf_adjustment = -40 in the
// aggressive-denoise cyclic-refresh branch (vp8/encoder/onyx_if.c).
const aggressiveDenoiseAltLFDelta int8 = -40

// loopFilterSegmentationHeader translates the encoder-side
// SegmentationConfig (writer shape) into the decoder-side
// SegmentationHeader (reader shape) so the in-encoder reconstruction
// loop-filter honors the same per-segment ALT_LF deltas the bitstream
// signals. Mirrors libvpx vp8/common/vp8_loopfilter.c
// vp8_loop_filter_frame_init: when segmentation is enabled the
// loop-filter level for a macroblock is `clamp(base_level +
// FeatureData[MB_LVL_ALT_LF][seg], 0, 63)` (delta) or `clamp(value,
// 0, 63)` (abs). Disabled feature slots are emitted as zero so the
// decoder's matching translation in
// vp8/decoder/loopfilter.go:loopFilterFrameConfig sees a no-op delta.
func loopFilterSegmentationHeader(cfg vp8enc.SegmentationConfig) vp8dec.SegmentationHeader {
	header := vp8dec.SegmentationHeader{
		Enabled:    cfg.Enabled,
		UpdateMap:  cfg.UpdateMap,
		UpdateData: cfg.UpdateData,
		AbsDelta:   cfg.AbsDelta,
		TreeProbs:  cfg.TreeProbs,
	}
	if !cfg.Enabled {
		return header
	}
	for feature := range int(vp8common.MBLvlMax) {
		for segment := range vp8common.MaxMBSegments {
			if cfg.FeatureEnabled[feature][segment] {
				header.FeatureData[feature][segment] = cfg.FeatureData[feature][segment]
			}
		}
	}
	return header
}

// libvpx omits non-positive ALT_LF data from the packet-facing decoder state
// only when the chosen base loop-filter level is zero. Segmentation remains
// enabled, and a positive ALT_LF value is still emitted because it raises that
// segment above the zero base level.
func segmentationConfigForLoopFilterLevel(cfg vp8enc.SegmentationConfig, level uint8) vp8enc.SegmentationConfig {
	if !cfg.Enabled || level > 0 {
		return cfg
	}
	for segment := range vp8common.MaxMBSegments {
		if cfg.FeatureEnabled[vp8common.MBLvlAltLF][segment] && cfg.FeatureData[vp8common.MBLvlAltLF][segment] <= 0 {
			cfg.FeatureEnabled[vp8common.MBLvlAltLF][segment] = false
			cfg.FeatureData[vp8common.MBLvlAltLF][segment] = 0
		}
	}
	return cfg
}

func (e *VP8Encoder) cyclicRefreshModeEnabled(refreshGolden bool) bool {
	if e.opts.RTCExternalRateControl {
		return false
	}
	if e.opts.ScreenContentMode == 2 && refreshGolden {
		return false
	}
	if e.forceMaxQuantizer {
		// libvpx onyx_if.c gates cyclic refresh on force_maxqp == 0; when an
		// overshoot drop forces the next frame to max Q, segmentation is
		// disabled so the segment map and feature data don't fight the
		// max-Q clamp.
		return false
	}
	if e.roi.suppressCyclicRefresh {
		return false
	}
	// libvpx vp8/encoder/onyx_if.c (around line 1980) gates the static
	// `cpi->cyclic_refresh_mode_enabled` config on:
	//
	//   error_resilient_mode || (end_usage == USAGE_STREAM_FROM_SERVER &&
	//                            cpi->oxcf.Mode <= MODE_BESTQUALITY)
	//
	// Mode <= MODE_BESTQUALITY (==2) covers the three one-pass deadlines
	// (REALTIME=0, GOODQUALITY=1, BESTQUALITY=2). Two-pass second-pass
	// runs at MODE_SECONDPASS (4) / MODE_SECONDPASS_BEST (5), which fall
	// through. govpx's twoPass.enabled() flag mirrors the second-pass
	// gate (first-pass collection is a separate code path), so excluding
	// it here keeps cyclic refresh off on two-pass CBR runs the way
	// libvpx does. error_resilient still wins regardless of pass count.
	if e.opts.ErrorResilient {
		return true
	}
	return e.rc.mode == RateControlCBR && !e.twoPass.enabled()
}

// aggressiveDenoiseSegmentationActive matches libvpx's branch in
// cyclic_background_refresh that switches the cyclic-refresh segment from a
// Q delta to an alt-LF delta when noise sensitivity is aggressive, the
// current Q is below qp_thresh, and the frame is far enough past the last
// key frame (frames_since_key > 2 * consec_zerolast).
func (e *VP8Encoder) aggressiveDenoiseSegmentationActive() bool {
	return e.aggressiveDenoiseSegmentationActiveForQuantizer(e.rc.currentQuantizer)
}

func (e *VP8Encoder) aggressiveDenoiseSegmentationActiveForQuantizer(q int) bool {
	if e.opts.NoiseSensitivity < 3 {
		return false
	}
	mode := denoiserModeForSensitivity(e.opts.NoiseSensitivity)
	if mode != denoiserOnYUVAggressive {
		return false
	}
	_, params := denoiserSetParameters(mode)
	if q >= params.qpThresh {
		return false
	}
	if e.rc.framesSinceKeyframe <= 2*params.consecZeroLast {
		return false
	}
	return true
}

func (e *VP8Encoder) cyclicRefreshQuantizerDelta() int8 {
	return cyclicRefreshQuantizerDeltaForQuantizer(e.rc.currentQuantizer)
}

func cyclicRefreshQuantizerDeltaForQuantizer(q int) int8 {
	return int8(q/2 - q)
}

func updateKeyFrameSegmentationTreeProbs(cfg *vp8enc.SegmentationConfig, modes []vp8enc.KeyFrameMacroblockMode) {
	var counts [vp8common.MaxMBSegments]int
	for _, mode := range modes {
		if mode.SegmentID < vp8common.MaxMBSegments {
			counts[mode.SegmentID]++
		}
	}
	updateSegmentationTreeProbs(cfg, counts)
}

func updateInterFrameSegmentationTreeProbs(cfg *vp8enc.SegmentationConfig, modes []vp8enc.InterFrameMacroblockMode) {
	var counts [vp8common.MaxMBSegments]int
	for _, mode := range modes {
		if mode.SegmentID < vp8common.MaxMBSegments {
			counts[mode.SegmentID]++
		}
	}
	updateSegmentationTreeProbs(cfg, counts)
}

func updateSegmentationTreeProbs(cfg *vp8enc.SegmentationConfig, counts [vp8common.MaxMBSegments]int) {
	if cfg == nil || !cfg.Enabled || !cfg.UpdateMap {
		return
	}
	for i := range cfg.TreeProbUpdated {
		cfg.TreeProbUpdated[i] = false
		cfg.TreeProbs[i] = 0
	}
	probs := [vp8common.MBFeatureTreeProbs]uint8{255, 255, 255}
	total := counts[0] + counts[1] + counts[2] + counts[3]
	if total > 0 {
		probs[0] = nonZeroSegmentTreeProb(((counts[0] + counts[1]) * 255) / total)
		leftTotal := counts[0] + counts[1]
		if leftTotal > 0 {
			probs[1] = nonZeroSegmentTreeProb((counts[0] * 255) / leftTotal)
		}
		rightTotal := counts[2] + counts[3]
		if rightTotal > 0 {
			probs[2] = nonZeroSegmentTreeProb((counts[2] * 255) / rightTotal)
		}
	}
	for i, prob := range probs {
		if prob == 255 {
			continue
		}
		cfg.TreeProbs[i] = prob
		cfg.TreeProbUpdated[i] = true
	}
}

func nonZeroSegmentTreeProb(prob int) uint8 {
	return uint8(min(max(prob, 1), 255))
}

func assignKeyFrameStaticSegments(rows int, cols int, modes []vp8enc.KeyFrameMacroblockMode) {
	for row := range rows {
		for col := range cols {
			index := row*cols + col
			modes[index].SegmentID = 0
		}
	}
}

func assignInterFrameStaticSegments(rows int, cols int, start int, refreshCount int, modes []vp8enc.InterFrameMacroblockMode) {
	assignInterFrameStaticSegmentsWithMap(rows, cols, start, refreshCount, nil, modes)
}

func assignInterFrameStaticSegmentsWithMap(rows int, cols int, start int, refreshCount int, refreshMap []int8, modes []vp8enc.InterFrameMacroblockMode) int {
	count := rows * cols
	if count <= 0 {
		return 0
	}
	start %= count
	if start < 0 {
		start += count
	}
	for row := range rows {
		for col := range cols {
			index := row*cols + col
			modes[index].SegmentID = 0
		}
	}
	if refreshCount <= 0 {
		return start
	}
	if len(refreshMap) < count {
		for refreshed := 0; refreshed < refreshCount && refreshed < count; refreshed++ {
			modes[(start+refreshed)%count].SegmentID = staticSegmentID
		}
		return (start + min(refreshCount, count)) % count
	}
	i := start
	blockCount := refreshCount
	for {
		if refreshMap[i] == 0 {
			modes[i].SegmentID = staticSegmentID
			blockCount--
		} else if refreshMap[i] < 0 {
			refreshMap[i]++
		}
		i++
		if i == count {
			i = 0
		}
		if blockCount == 0 || i == start {
			break
		}
	}
	return i
}

func (e *VP8Encoder) assignInterFrameStaticSegments(src vp8enc.SourceImage, rows int, cols int, modes []vp8enc.InterFrameMacroblockMode) int {
	return e.assignInterFrameStaticSegmentsForQuantizer(src, rows, cols, modes, e.rc.currentQuantizer)
}

func (e *VP8Encoder) assignInterFrameStaticSegmentsForQuantizer(src vp8enc.SourceImage, rows int, cols int, modes []vp8enc.InterFrameMacroblockMode, q int) int {
	count := rows * cols
	if count <= 0 {
		return 0
	}
	if len(e.cyclicRefreshMap) < count || len(e.cyclicRefreshAttemptMap) < count {
		return assignInterFrameStaticSegmentsWithMap(rows, cols, e.cyclicRefreshIndex, e.cyclicRefreshMaxMBsPerFrameForQuantizer(rows, cols, q), nil, modes)
	}
	copy(e.cyclicRefreshAttemptMap[:count], e.cyclicRefreshMap[:count])
	return assignInterFrameStaticSegmentsWithMap(rows, cols, e.cyclicRefreshIndex, e.cyclicRefreshMaxMBsPerFrameForQuantizer(rows, cols, q), e.cyclicRefreshAttemptMap[:count], modes)
}

func (e *VP8Encoder) prepareInterFrameSkinMap(src vp8enc.SourceImage, rows int, cols int) {
	count := rows * cols
	if count <= 0 || len(e.skinMap) < count {
		return
	}
	if e.opts.ScreenContentMode != 0 {
		clearUint8Map(e.skinMap[:count])
		return
	}
	computeSkinMap(src, rows, cols, e.consecZeroLast, e.skinMap[:count])
}

func (e *VP8Encoder) commitCyclicRefresh(rows int, cols int, nextIndex int, modes []vp8enc.InterFrameMacroblockMode) {
	count := rows * cols
	if count <= 0 {
		e.cyclicRefreshIndex = 0
		return
	}
	if len(e.cyclicRefreshMap) >= count && len(e.cyclicRefreshAttemptMap) >= count && len(modes) >= count {
		copy(e.cyclicRefreshMap[:count], e.cyclicRefreshAttemptMap[:count])
		updateCyclicRefreshMapFromInterFrame(modes[:count], e.cyclicRefreshMap[:count])
	}
	nextIndex %= count
	if nextIndex < 0 {
		nextIndex += count
	}
	e.cyclicRefreshIndex = nextIndex
}

func (e *VP8Encoder) commitKeyFrameCyclicRefreshMap(rows int, cols int, modes []vp8enc.KeyFrameMacroblockMode, segmentationEnabled bool) {
	// Key frames do not feed back into libvpx's cyclic_refresh_map. The
	// keyframe cyclic_background_refresh call clears the packet segment map,
	// but encodeframe.c updates cyclic_refresh_map only after inter MBs.
	// Leave both the committed map and scratch attempt map untouched.
}

func updateCyclicRefreshMapFromInterFrame(modes []vp8enc.InterFrameMacroblockMode, refreshMap []int8) {
	count := min(len(modes), len(refreshMap))
	for index := range count {
		mode := modes[index]
		if mode.SegmentID != 0 {
			refreshMap[index] = -1
		} else if mode.Mode == vp8common.ZeroMV && mode.RefFrame == vp8common.LastFrame {
			if refreshMap[index] == 1 {
				refreshMap[index] = 0
			}
		} else {
			refreshMap[index] = 1
		}
	}
}

func (e *VP8Encoder) updateConsecutiveZeroLast(modes []vp8enc.InterFrameMacroblockMode) {
	if len(e.consecZeroLast) == 0 {
		return
	}
	updateConsecutiveZeroLast(modes, e.consecZeroLast)
	updateConsecutiveZeroLastWithDotSuppress(modes, e.consecZeroLastMVBias, e.dotArtifactChecked)
	clearBoolMap(e.dotArtifactChecked)
}

func updateConsecutiveZeroLast(modes []vp8enc.InterFrameMacroblockMode, counters []uint8) {
	count := min(len(modes), len(counters))
	for index := range count {
		mode := modes[index]
		if mode.RefFrame == vp8common.LastFrame && mode.Mode == vp8common.ZeroMV {
			if counters[index] < 255 {
				counters[index]++
			}
			continue
		}
		counters[index] = 0
	}
}

// updateConsecutiveZeroLastWithDotSuppress mirrors libvpx's
// consec_zero_last_mvbias counter: like consec_zero_last, but any MB that
// was checked for dot-artifact suppression this frame has the counter zeroed
// so the threshold gate gives the same MB a fresh chance after the next
// num_frames have passed.
func updateConsecutiveZeroLastWithDotSuppress(modes []vp8enc.InterFrameMacroblockMode, counters []uint8, dotChecked []bool) {
	count := min(len(modes), len(counters))
	for index := range count {
		mode := modes[index]
		if mode.RefFrame == vp8common.LastFrame && mode.Mode == vp8common.ZeroMV {
			if counters[index] < 255 {
				counters[index]++
			}
		} else {
			counters[index] = 0
		}
		if index < len(dotChecked) && dotChecked[index] {
			counters[index] = 0
		}
	}
}

func clearCyclicRefreshMap(refreshMap []int8) {
	for i := range refreshMap {
		refreshMap[i] = 0
	}
}

func clearBoolMap(values []bool) {
	for i := range values {
		values[i] = false
	}
}

func clearUint8Map(values []uint8) {
	for i := range values {
		values[i] = 0
	}
}

func (e *VP8Encoder) cyclicRefreshMaxMBsPerFrame(rows int, cols int) int {
	return e.cyclicRefreshMaxMBsPerFrameForQuantizer(rows, cols, e.rc.currentQuantizer)
}

func (e *VP8Encoder) cyclicRefreshMaxMBsPerFrameForQuantizer(rows int, cols int, q int) int {
	layers := 1
	if e.temporal.enabled {
		layers = e.temporal.pattern.Layers
	}
	return cyclicRefreshMaxMBsPerFrameForConfig(rows, cols, layers, e.opts.ScreenContentMode, q, e.rc.framesSinceKeyframe, e.lastInterSkipCount)
}

func cyclicRefreshMaxMBsPerFrame(rows int, cols int) int {
	return cyclicRefreshMaxMBsPerFrameForLayers(rows, cols, 1)
}

func cyclicRefreshMaxMBsPerFrameForConfig(rows int, cols int, layers int, screenContentMode int, q int, framesSinceKey int, lastSkipCount int) int {
	if min(rows, cols) <= 0 {
		return 0
	}
	count := rows * cols
	if screenContentMode > 0 {
		qpThreshold := 100
		if screenContentMode == 2 {
			qpThreshold = 80
		}
		if q >= qpThreshold {
			return count / 10
		}
		if framesSinceKey > 250 && q < 20 && lastSkipCount*100 > 95*count {
			return 0
		}
		return count / 20
	}
	return cyclicRefreshMaxMBsPerFrameForLayers(rows, cols, layers)
}

func cyclicRefreshMaxMBsPerFrameForLayers(rows int, cols int, layers int) int {
	if min(rows, cols) <= 0 {
		return 0
	}
	count := rows * cols
	switch layers {
	case 1:
		return count / 20
	case 2:
		return count / 10
	default:
		return count / 7
	}
}

// skinDetectionMaxSmallFrame matches libvpx's CIF threshold (352*288). Frames
// at or below this size use the SKIN_8X8 detector that subdivides each MB
// into four 8x8 sub-blocks; larger frames use the SKIN_16X16 single-sample.
const skinDetectionMaxSmallFrame = 352 * 288

func computeSkinMap(src vp8enc.SourceImage, rows int, cols int, consecZeroLast []uint8, skinMap []uint8) {
	count := rows * cols
	if count <= 0 || len(skinMap) < count || min(src.Width, src.Height) <= 0 {
		return
	}
	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	useSkin8x8 := src.Width*src.Height <= skinDetectionMaxSmallFrame
	for row := range rows {
		for col := range cols {
			index := row*cols + col
			consecutive := 0
			if len(consecZeroLast) > index {
				consecutive = int(consecZeroLast[index])
			}
			var skin bool
			if useSkin8x8 {
				skin = computeSkin8x8Block(src, uvWidth, uvHeight, row, col, consecutive)
			} else {
				y := average2x2Clamped(src.Y, src.YStride, src.Width, src.Height, row*16+7, col*16+7)
				u := average2x2Clamped(src.U, src.UStride, uvWidth, uvHeight, row*8+3, col*8+3)
				v := average2x2Clamped(src.V, src.VStride, uvWidth, uvHeight, row*8+3, col*8+3)
				skin = computeSkinBlock(y, u, v, consecutive, 0)
			}
			if skin {
				skinMap[index] = 1
			} else {
				skinMap[index] = 0
			}
		}
	}
	smoothSkinMap(rows, cols, skinMap[:count])
}

// computeSkin8x8Block mirrors libvpx vp8_compute_skin_block in SKIN_8X8 mode:
// each MB is split into four 8x8 sub-blocks; for each sub-block we sample the
// center 2x2 average (Y at offset (3,3), UV at (1,1)) and run the skin-pixel
// test. The MB is classified skin if at least two of its sub-blocks are skin.
func computeSkin8x8Block(src vp8enc.SourceImage, uvWidth int, uvHeight int, mbRow int, mbCol int, consecZeroLast int) bool {
	if consecZeroLast > 60 {
		return false
	}
	motion := 1
	if consecZeroLast > 25 {
		motion = 0
	}
	numSkin := 0
	for sb := range 4 {
		yRow := mbRow*16 + (sb>>1)*8 + 3
		yCol := mbCol*16 + (sb&1)*8 + 3
		uvRow := mbRow*8 + (sb>>1)*4 + 1
		uvCol := mbCol*8 + (sb&1)*4 + 1
		ySample := average2x2Clamped(src.Y, src.YStride, src.Width, src.Height, yRow, yCol)
		uSample := average2x2Clamped(src.U, src.UStride, uvWidth, uvHeight, uvRow, uvCol)
		vSample := average2x2Clamped(src.V, src.VStride, uvWidth, uvHeight, uvRow, uvCol)
		if skinPixel(ySample, uSample, vSample, motion) {
			numSkin++
			if numSkin >= 2 {
				return true
			}
		}
	}
	return false
}

func average2x2Clamped(plane []byte, stride int, width int, height int, y int, x int) int {
	if min(min(stride, width), height) <= 0 || len(plane) == 0 {
		return 0
	}
	y = min(max(y, 0), height-1)
	x = min(max(x, 0), width-1)
	y1 := y
	y2 := min(y+1, height-1)
	x1 := x
	x2 := min(x+1, width-1)
	if y1*stride+x1 >= len(plane) || y1*stride+x2 >= len(plane) || y2*stride+x1 >= len(plane) || y2*stride+x2 >= len(plane) {
		return 0
	}
	sum := int(plane[y1*stride+x1]) + int(plane[y1*stride+x2]) + int(plane[y2*stride+x1]) + int(plane[y2*stride+x2])
	return (sum + 2) >> 2
}

func computeSkinBlock(y int, u int, v int, consecZeroLast int, currentMotionMagnitude int) bool {
	if consecZeroLast > 60 && currentMotionMagnitude == 0 {
		return false
	}
	motion := 1
	if consecZeroLast > 25 && currentMotionMagnitude == 0 {
		motion = 0
	}
	return skinPixel(y, u, v, motion)
}

func skinPixel(y int, cb int, cr int, motion int) bool {
	if y < skinYLow || y > skinYHigh {
		return false
	}
	if cb == 128 && cr == 128 {
		return false
	}
	if cb > 150 && cr < 110 {
		return false
	}
	for i := range len(skinMean) {
		diff := evaluateSkinColorDifference(cb, cr, i)
		threshold := skinThreshold[i+1]
		if diff < threshold {
			if y < 60 && diff > 3*(threshold>>2) {
				return false
			}
			if motion == 0 && diff > threshold>>1 {
				return false
			}
			return true
		}
		if diff > threshold<<3 {
			return false
		}
	}
	return false
}

func evaluateSkinColorDifference(cb int, cr int, index int) int {
	cbQ6 := cb << 6
	crQ6 := cr << 6
	cbDelta := cbQ6 - skinMean[index][0]
	crDelta := crQ6 - skinMean[index][1]
	cbDiffQ12 := cbDelta * cbDelta
	cbcrDiffQ12 := cbDelta * crDelta
	crDiffQ12 := crDelta * crDelta
	cbDiffQ2 := (cbDiffQ12 + (1 << 9)) >> 10
	cbcrDiffQ2 := (cbcrDiffQ12 + (1 << 9)) >> 10
	crDiffQ2 := (crDiffQ12 + (1 << 9)) >> 10
	return skinInvCov[0]*cbDiffQ2 + skinInvCov[1]*cbcrDiffQ2 + skinInvCov[2]*cbcrDiffQ2 + skinInvCov[3]*crDiffQ2
}

func smoothSkinMap(rows int, cols int, skinMap []uint8) {
	if rows < 3 || cols < 3 || len(skinMap) < rows*cols {
		return
	}
	for row := 1; row < rows-1; row++ {
		for col := 1; col < cols-1; col++ {
			index := row*cols + col
			neighbors := 0
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					if skinMap[(row+dy)*cols+col+dx] != 0 {
						neighbors++
					}
				}
			}
			if skinMap[index] != 0 && neighbors < 2 {
				skinMap[index] = 0
			}
			if skinMap[index] == 0 && neighbors == 8 {
				skinMap[index] = 1
			}
		}
	}
}

const (
	skinYLow  = 40
	skinYHigh = 220
)

var skinMean = [5][2]int{
	{7463, 9614},
	{6400, 10240},
	{7040, 10240},
	{8320, 9280},
	{6800, 9614},
}

var skinInvCov = [4]int{4107, 1663, 1663, 2157}
var skinThreshold = [6]int{1570636, 1400000, 800000, 800000, 800000, 800000}

// dotArtifactCandidate matches libvpx's check_dot_artifact_candidate
// (vp8/encoder/pickinter.c): a base-layer macroblock that has been ZEROMV-LAST
// for at least dotArtifactConsecZeroLastFrames consecutive frames and whose
// LAST reference shows a sharp corner gradient while the source remains flat
// is flagged as a candidate. Candidate count is capped per frame at MBs/10
// to avoid runaway suppression.
const (
	dotArtifactCornerGradLast      = 6
	dotArtifactCornerGradSource    = 3
	dotArtifactConsecZeroLastBase  = 30
	dotArtifactConsecZeroLastLayer = 20
	dotArtifactSuppressCapDivisor  = 10
)

// checkDotArtifactCandidate matches libvpx's check_dot_artifact_candidate
// (vp8/encoder/pickinter.c): tests Y, then U, then V channel corner gradients
// and triggers a 1.5x ZEROMV-LAST RD penalty when the LAST reference shows a
// sharp corner gradient while the source remains flat. Eligibility uses
// consec_zero_last_mvbias (the bias-only counter), which is reset for any
// MB this function checks so a triggered MB gets a fresh num_frames window.
func (e *VP8Encoder) checkDotArtifactCandidate(src vp8enc.SourceImage, lastRef *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int) bool {
	if lastRef == nil {
		return false
	}
	if e.opts.ScreenContentMode != 0 {
		return false
	}
	if e.currentTemporalLayer != 0 {
		return false
	}
	totalMBs := mbRows * mbCols
	if totalMBs <= 0 {
		return false
	}
	cap := totalMBs / dotArtifactSuppressCapDivisor
	if e.mbsZeroLastDotSuppress >= cap {
		return false
	}
	index := mbRow*mbCols + mbCol
	if uint(index) >= uint(len(e.consecZeroLastMVBias)) {
		return false
	}
	threshold := dotArtifactConsecZeroLastBase
	if e.temporal.enabled && e.temporal.pattern.Layers > 1 {
		threshold = dotArtifactConsecZeroLastLayer
	}
	if int(e.consecZeroLastMVBias[index]) <= threshold {
		return false
	}
	// Mark this MB as checked so its mvbias counter resets at frame end,
	// regardless of whether the corner test below ultimately triggers.
	if index < len(e.dotArtifactChecked) {
		e.dotArtifactChecked[index] = true
	}
	if !dotArtifactCornerCandidateY(src, lastRef, mbRow, mbCol) &&
		!dotArtifactCornerCandidateUV(src.U, src.UStride, lastRef.U, lastRef.UStride, mbRow, mbCol) &&
		!dotArtifactCornerCandidateUV(src.V, src.VStride, lastRef.V, lastRef.VStride, mbRow, mbCol) {
		return false
	}
	if e.threadedRowsActive && e.threadedDotArtifactBudget != nil {
		if !reserveDotArtifactSuppressSlot(e.threadedDotArtifactBudget, cap) {
			return false
		}
	}
	e.mbsZeroLastDotSuppress++
	return true
}

// checkDotArtifactCandidateY is retained for back-compat and forwards to the
// full Y+UV check.
func (e *VP8Encoder) checkDotArtifactCandidateY(src vp8enc.SourceImage, lastRef *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int) bool {
	return e.checkDotArtifactCandidate(src, lastRef, mbRow, mbCol, mbRows, mbCols)
}

func reserveDotArtifactSuppressSlot(budget *atomic.Int32, cap int) bool {
	if budget == nil || cap <= 0 {
		return false
	}
	limit := int32(cap)
	for {
		used := budget.Load()
		if used >= limit {
			return false
		}
		if budget.CompareAndSwap(used, used+1) {
			return true
		}
	}
}

func dotArtifactCornerCandidateY(src vp8enc.SourceImage, lastRef *vp8common.Image, mbRow int, mbCol int) bool {
	if lastRef == nil || src.Y == nil {
		return false
	}
	srcOff := mbRow*16*src.YStride + mbCol*16
	refOff := mbRow*16*lastRef.YStride + mbCol*16
	return dotArtifactCornerCheck(src.Y[srcOff:], src.YStride, lastRef.Y[refOff:], lastRef.YStride, 15)
}

func dotArtifactCornerCandidateUV(srcPlane []byte, srcStride int, lastPlane []byte, lastStride int, mbRow int, mbCol int) bool {
	if srcPlane == nil || lastPlane == nil {
		return false
	}
	srcOff := mbRow*8*srcStride + mbCol*8
	refOff := mbRow*8*lastStride + mbCol*8
	return dotArtifactCornerCheck(srcPlane[srcOff:], srcStride, lastPlane[refOff:], lastStride, 7)
}

func dotArtifactCornerCheck(srcMB []byte, srcStride int, refMB []byte, refStride int, shift int) bool {
	corners := [4][4]int{
		{0, 0, 1, 1},
		{0, shift, 1, -1},
		{shift, 0, -1, 1},
		{shift, shift, -1, -1},
	}
	for _, c := range corners {
		gradLast := macroblockCornerGradient(refMB, refStride, c[0], c[1], c[2], c[3])
		gradSrc := macroblockCornerGradient(srcMB, srcStride, c[0], c[1], c[2], c[3])
		if gradLast >= dotArtifactCornerGradLast && gradSrc <= dotArtifactCornerGradSource {
			return true
		}
	}
	return false
}

// macroblockCornerGradient mirrors libvpx pickinter.c macroblock_corner_grad:
// max absolute delta from one corner pixel to its three immediate neighbours
// in the directions specified by sgnRow/sgnCol.
func macroblockCornerGradient(plane []byte, stride int, offRow int, offCol int, sgnRow int, sgnCol int) int {
	y1 := int(plane[offRow*stride+offCol])
	y2 := int(plane[offRow*stride+offCol+sgnCol])
	y3 := int(plane[(offRow+sgnRow)*stride+offCol])
	y4 := int(plane[(offRow+sgnRow)*stride+offCol+sgnCol])
	// Branchless |delta| via sign-mask XOR plus max() to fold the
	// per-neighbor d{1,2,3} max ladder into builtin calls.
	d1 := y1 - y2
	m1 := d1 >> mvKernelSignShift
	d1 = (d1 ^ m1) - m1
	d2 := y1 - y3
	m2 := d2 >> mvKernelSignShift
	d2 = (d2 ^ m2) - m2
	d3 := y1 - y4
	m3 := d3 >> mvKernelSignShift
	d3 = (d3 ^ m3) - m3
	return max(max(d1, d2), d3)
}
