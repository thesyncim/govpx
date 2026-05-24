package encoder

import "github.com/thesyncim/govpx/internal/vpx/buffers"

// VP9 perceptual adaptive quantization (AQ_MODE=PERCEPTUAL_AQ).
//
// This file is a verbatim port of libvpx v1.16.0's perceptual AQ
// pipeline. There is no standalone `vp9_aq_perceptual.c` in libvpx
// v1.16.0 — the pass is split across three translation units. Every
// constant, branch, formula, and structural choice below is sourced
// from the listed libvpx file:line; do not introduce values without a
// libvpx citation.
//
//	libvpx: vp9/encoder/vp9_encoder.c:5178   set_mb_wiener_variance
//	libvpx: vp9/encoder/vp9_encoder.c:5370   gate to PERCEPTUAL_AQ
//	libvpx: vp9/encoder/vp9_encodeframe.c:3505 log_wiener_var
//	libvpx: vp9/encoder/vp9_encodeframe.c:3509 build_kmeans_segmentation
//	libvpx: vp9/encoder/vp9_encodeframe.c:3560 wiener_var_segment
//	libvpx: vp9/encoder/vp9_encodeframe.c:5549 vp9_kmeans
//	libvpx: vp9/encoder/vp9_encodeframe.c:5528 compute_boundary_ls
//	libvpx: vp9/encoder/vp9_encodeframe.c:5538 vp9_get_group_idx
//	libvpx: vp9/encoder/vp9_segmentation.c:63  vp9_perceptual_aq_mode_setup
//	libvpx: vp9/encoder/vp9_ratectrl.c:170     vp9_convert_qindex_to_q
//	libvpx: vp9/encoder/vp9_ratectrl.c:185     vp9_convert_q_to_qindex
//	libvpx: vpx_dsp/avg.c:199                  hadamard_col8
//	libvpx: vpx_dsp/avg.c:231                  vpx_hadamard_8x8_c
//	libvpx: vpx_dsp/avg.c:257                  vpx_hadamard_16x16_c
//	libvpx: vpx_dsp/subtract.c:19              vpx_subtract_block_c

import (
	"image"
	"math"
	"slices"
	"sort"

	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

const (
	// libvpx: vp9/encoder/vp9_encoder.h:519 #define MAX_KMEANS_GROUPS 8.
	// build_kmeans_segmentation hardcodes cpi->kmeans_ctr_num = 8 at
	// vp9_encodeframe.c:3518.
	perceptualAQClusters = 8
	// libvpx: vp9/encoder/vp9_encodeframe.c:5568 for (itr = 0; itr < 10; ++itr).
	perceptualAQIterations = 10
	// libvpx: vp9/encoder/vp9_encoder.c:5199 const int block_size = 16.
	perceptualAQMBSize = 16
	// libvpx: vp9/encoder/vp9_segmentation.c:70 const double var_diff_scale = 4.0.
	perceptualAQVarDiffScale = 4.0
	// libvpx: 64x64 SB is BLOCK_64X64. mi_block_size is 8 (MI_BLOCK_SIZE),
	// which corresponds to 8 * 8 = 64 luma pixels per SB side, i.e. 4 MBs.
	perceptualAQSBSizeInMB = 4
)

// PerceptualAQState holds the per-frame Wiener-variance + k-means
// state. Renamed govpx-side, but the fields map 1:1 onto libvpx:
//
//	mbWienerVariance  <-> cpi->mb_wiener_variance       (vp9_encoder.h)
//	kmeansCenters     <-> cpi->kmeans_ctr_ls            (vp9_encoder.h:630)
//	kmeansBoundaries  <-> cpi->kmeans_boundary_ls       (vp9_encoder.h:631)
//	kmeansData        <-> cpi->kmeans_data_arr          (vp9_encoder.h)
//	deltas            <-> seg->feature_data[i][ALT_Q]   (vp9_segmentation.c:85)
//
// segments holds the per-SB segment_id assignment computed by
// wiener_var_segment (vp9_encodeframe.c:3560), keyed by mi_row/mi_col
// stepped by MI_BLOCK_SIZE = 8.
type PerceptualAQState struct {
	Enabled bool
	Ready   bool

	mbRows int
	mbCols int

	// Per-16x16-MB Wiener variances, libvpx mb_wiener_variance.
	mbWienerVariance []int64
	// Per-SB log-Wiener values fed to k-means.
	kmeansData []float64
	// Per-SB segment-id histogram-majority assignment, indexed
	// [sbRow*sbCols+sbCol].
	sbCols   int
	sbRows   int
	segments []uint8

	kmeansCenters    [perceptualAQClusters]float64
	kmeansBoundaries [perceptualAQClusters]float64

	// Per-segment AltQ deltas; populated from
	// vp9_perceptual_aq_mode_setup (vp9_segmentation.c:63).
	deltas [vp9dec.MaxSegments]int16
}

// Configure enables or disables perceptual AQ at construction time.
func (s *PerceptualAQState) Configure(enabled bool) {
	s.Enabled = enabled
	s.Ready = false
	if !enabled {
		s.mbWienerVariance = nil
		s.kmeansData = nil
		s.segments = nil
	}
}

// PrepareFrame runs the libvpx PERCEPTUAL_AQ pipeline for one show
// frame:
//   - set_mb_wiener_variance      (vp9_encoder.c:5178)
//   - build_kmeans_segmentation   (vp9_encodeframe.c:3509)
//     -> vp9_kmeans               (vp9_encodeframe.c:5549)
//     -> vp9_perceptual_aq_mode_setup (vp9_segmentation.c:63)
//   - wiener_var_segment          (vp9_encodeframe.c:3560)
//
// The gate in libvpx is `oxcf.aq_mode == PERCEPTUAL_AQ` plus
// `cm->show_frame` inside build_kmeans_segmentation. We mirror both:
// callers pass `showFrame` so non-displayed alt-ref frames skip the
// pass and inherit the previous show-frame segmentation, exactly as
// libvpx does (vp9_disable_segmentation is also gated on show_frame).
func (s *PerceptualAQState) PrepareFrame(img *image.YCbCr, baseQIndex int, showFrame bool) bool {
	if !s.Enabled || img == nil {
		s.Ready = false
		return false
	}
	if !showFrame {
		return s.Ready
	}
	s.Ready = false
	src := img.Y
	stride := img.YStride
	width := img.Rect.Dx()
	height := img.Rect.Dy()
	if len(src) == 0 || stride <= 0 || width <= 0 || height <= 0 {
		return false
	}
	// libvpx: mb_rows/mb_cols come from vp9_set_mb_size
	// (vp9_alloccommon.c:29). With 8-pixel MI alignment, this rounds
	// each dimension up to a 16-pixel multiple.
	mbCols := (width + perceptualAQMBSize - 1) / perceptualAQMBSize
	mbRows := (height + perceptualAQMBSize - 1) / perceptualAQMBSize
	if mbCols <= 0 || mbRows <= 0 {
		return false
	}
	s.mbRows = mbRows
	s.mbCols = mbCols
	total := mbRows * mbCols
	s.mbWienerVariance = buffers.EnsureLen(s.mbWienerVariance, total)
	// libvpx: set_mb_wiener_variance (vp9_encoder.c:5178). Iterates
	// every (mb_row, mb_col), Hadamard-transforms the 16x16 block
	// against a zero predictor, zeros the DC, sorts |AC|, picks the
	// median, runs a Wiener filter, and stores the result.
	perceptualSetMBWienerVariance(src, stride, width, height,
		mbRows, mbCols, s.mbWienerVariance)

	// libvpx: build_kmeans_segmentation (vp9_encodeframe.c:3509).
	// Aggregates the per-MB variances over each 64x64 SB (4x4 MBs),
	// divides by the count to get the per-SB mean, then pushes
	// log_wiener_var(mean) into the kmeans data array.
	sbStep := perceptualAQSBSizeInMB
	sbCols := (mbCols + sbStep - 1) / sbStep
	sbRows := (mbRows + sbStep - 1) / sbStep
	s.sbRows = sbRows
	s.sbCols = sbCols
	sbCount := sbRows * sbCols
	if cap(s.kmeansData) < sbCount {
		s.kmeansData = make([]float64, 0, sbCount)
	} else {
		s.kmeansData = s.kmeansData[:0]
	}
	for sbRow := range sbRows {
		for sbCol := range sbCols {
			mbRowStart := sbRow * sbStep
			mbColStart := sbCol * sbStep
			mbRowEnd := min(mbRowStart+sbStep, mbRows)
			mbColEnd := min(mbColStart+sbStep, mbCols)
			var sum int64
			for r := mbRowStart; r < mbRowEnd; r++ {
				for c := mbColStart; c < mbColEnd; c++ {
					sum += s.mbWienerVariance[r*mbCols+c]
				}
			}
			n := int64((mbRowEnd - mbRowStart) * (mbColEnd - mbColStart))
			if n == 0 {
				continue
			}
			// libvpx: vp9_encodeframe.c:3535 wiener_variance /= ...
			mean := sum / n
			// libvpx: vp9_encodeframe.c:3543 log_wiener_var(...)
			s.kmeansData = append(s.kmeansData, perceptualLogWienerVar(mean))
		}
	}
	if len(s.kmeansData) < perceptualAQClusters {
		// libvpx asserts k>=2 && k<=MAX_KMEANS_GROUPS and indexes
		// arr[(size * (2*j+1))/(2*k)] which requires size >= k. Tiny
		// frames (single SB) cannot be clustered into 8 groups; we
		// suppress segmentation entirely rather than paying a neutral
		// map/header. libvpx itself doesn't hit this path on real
		// content because it sizes its kmeans_data_arr from
		// cm->mb_rows * cm->mb_cols (~256 entries even for the
		// smallest 64x64 frame) and would assert / crash here, so
		// this is a Go-side guard for govpx synthetic unit-test
		// fixtures.
		for i := range s.kmeansCenters {
			s.kmeansCenters[i] = 0
			s.kmeansBoundaries[i] = math.Inf(1)
		}
		for i := range s.deltas {
			s.deltas[i] = 0
		}
		s.segments = s.segments[:0]
		return false
	}

	// libvpx: vp9_kmeans (vp9_encodeframe.c:5549). Sorts data,
	// quantile-initializes centers, runs 10 Lloyd iterations.
	perceptualKMeans(s.kmeansData, &s.kmeansCenters, &s.kmeansBoundaries)

	// libvpx: vp9_perceptual_aq_mode_setup (vp9_segmentation.c:63).
	// Note bit_depth fixed to 8: govpx only encodes 8-bit profile 0.
	perceptualAQModeSetup(s.kmeansCenters[:], baseQIndex, s.deltas[:])

	// libvpx: wiener_var_segment (vp9_encodeframe.c:3560) is called
	// once per BLOCK_64X64 SB inside encode_rd_sb. We materialize the
	// per-SB segment assignments up-front so the encoder's segment
	// lookup is O(1) per query.
	s.segments = buffers.EnsureLen(s.segments, sbCount)
	for sbRow := range sbRows {
		for sbCol := range sbCols {
			s.segments[sbRow*sbCols+sbCol] = perceptualWienerVarSegment(
				s.mbWienerVariance, mbCols, mbRows,
				sbRow*sbStep, sbCol*sbStep,
				&s.kmeansBoundaries)
		}
	}
	s.Ready = true
	return true
}

// SegmentationParams returns the segmentation header VP9AQPerceptual
// emits. libvpx (vp9_perceptual_aq_mode_setup -> vp9_enable_segmentation)
// rebuilds the segment data on every show frame, so UpdateData is
// always set when the AQ pass is ready for the current frame. Non-show
// frames inherit the previous show-frame data by setting UpdateData
// false.
//
// libvpx: vp9/encoder/vp9_segmentation.c:22 vp9_enable_segmentation
// sets enabled=update_map=update_data=1.
func (s *PerceptualAQState) SegmentationParams(intraFrame bool) vp9dec.SegmentationParams {
	if !s.Ready {
		return vp9dec.SegmentationParams{}
	}
	seg := vp9dec.SegmentationParams{
		Enabled:   true,
		UpdateMap: true,
		AbsDelta:  false,
	}
	initSegmentationProbDefaults(&seg)
	if !intraFrame {
		// Inter show-frame: libvpx rewrites segment data every show
		// frame, but the decoder treats UpdateData=0 as "reuse last
		// data". To preserve the libvpx feature_data values without
		// re-emitting them on every inter frame, we mark UpdateData
		// false on non-intra frames; the previous intra frame's
		// FeatureMask/FeatureData entries remain in force.
		return seg
	}
	seg.UpdateData = true
	for i := range vp9dec.MaxSegments {
		// libvpx: vp9_segmentation.c:86, :90, :99 unconditionally
		// enable SEG_LVL_ALT_Q for every segment (including the mid
		// segment where delta=0).
		seg.FeatureMask[i] |= 1 << uint(vp9dec.SegLvlAltQ)
		seg.FeatureData[i][vp9dec.SegLvlAltQ] = s.deltas[i]
	}
	return seg
}

// SegmentIDForBlock returns the cluster index assigned to the
// superblock containing (miRow, miCol). libvpx assigns segments at
// BLOCK_64X64 granularity via wiener_var_segment
// (vp9_encodeframe.c:3560). MI_BLOCK_SIZE is 8, so each SB spans 8 MI
// rows/cols = 64 luma pixels = 4 MBs per side.
func (s *PerceptualAQState) SegmentIDForBlock(miRow, miCol int) uint8 {
	if !s.Ready || len(s.segments) == 0 {
		return 0
	}
	sbRow := miRow >> 3
	sbCol := miCol >> 3
	if sbRow < 0 {
		sbRow = 0
	}
	if sbCol < 0 {
		sbCol = 0
	}
	if sbRow >= s.sbRows {
		sbRow = s.sbRows - 1
	}
	if sbCol >= s.sbCols {
		sbCol = s.sbCols - 1
	}
	return s.segments[sbRow*s.sbCols+sbCol]
}

func initSegmentationProbDefaults(seg *vp9dec.SegmentationParams) {
	if seg == nil {
		return
	}
	for i := range vp9dec.SegTreeProbs {
		seg.TreeProbs[i] = vp9dec.MaxProb
	}
	for i := range vp9dec.PredictionProbs {
		seg.PredProbs[i] = vp9dec.MaxProb
	}
}

// perceptualWienerVarSegment ports wiener_var_segment
// (vp9_encodeframe.c:3560). For the 64x64 SB at (mbRowStart,
// mbColStart) in MB coordinates, look up each MB's group via
// vp9_get_group_idx and return the majority cluster.
//
// libvpx: vp9_encodeframe.c:3572 int8_t seg_hist[MAX_SEGMENTS] = {0}
// libvpx: vp9_encodeframe.c:3573 int8_t max_count, max_index = -1
// libvpx: vp9_encodeframe.c:3580 for (col = ...) ++seg_hist[segment_id]
// libvpx: vp9_encodeframe.c:3589 pick the max-count cluster
func perceptualWienerVarSegment(mbVar []int64, mbCols, mbRows,
	mbRowStart, mbColStart int,
	boundaries *[perceptualAQClusters]float64,
) uint8 {
	mbRowEnd := min(mbRowStart+perceptualAQSBSizeInMB, mbRows)
	mbColEnd := min(mbColStart+perceptualAQSBSizeInMB, mbCols)
	var segHist [perceptualAQClusters]int
	for r := mbRowStart; r < mbRowEnd; r++ {
		for c := mbColStart; c < mbColEnd; c++ {
			wv := mbVar[r*mbCols+c]
			idx := perceptualGroupIndex(
				perceptualLogWienerVar(wv), boundaries)
			segHist[idx]++
		}
	}
	maxCount := -1
	maxIndex := 0
	for i := range perceptualAQClusters {
		// libvpx uses strict `seg_hist[idx] > max_count` so ties go
		// to the lower-index cluster.
		if segHist[i] > maxCount {
			maxCount = segHist[i]
			maxIndex = i
		}
	}
	return uint8(maxIndex)
}

// perceptualAQModeSetup ports vp9_perceptual_aq_mode_setup
// (vp9_segmentation.c:63). Translates the k-means centers into
// per-segment AltQ deltas, anchored at the middle cluster
// (seg_counts/2) with delta_q = 0.
//
// Constants:
//
//	libvpx: vp9_segmentation.c:69  mid_ctr = ctr_ls[seg_counts/2]
//	libvpx: vp9_segmentation.c:70  var_diff_scale = 4.0
//	libvpx: vp9_segmentation.c:81  target_qstep = base_qstep / (1 + d/4)
//	libvpx: vp9_segmentation.c:94  target_qstep = base_qstep * (1 + d/4)
//
// We hardcode bit_depth = 8 to match govpx's 8-bit-only encoder.
// vp9_convert_qindex_to_q at 8 bits returns ac_quant_lookup[q] / 4.0
// (vp9_ratectrl.c:181).
func perceptualAQModeSetup(centers []float64, baseQIndex int, deltas []int16) {
	for i := range deltas {
		deltas[i] = 0
	}
	if len(centers) < perceptualAQClusters {
		return
	}
	if baseQIndex < 0 {
		baseQIndex = 0
	}
	if baseQIndex > 255 {
		baseQIndex = 255
	}
	baseQStep := perceptualQIndexToQStep(baseQIndex)
	mid := perceptualAQClusters / 2
	midCtr := centers[mid]
	// libvpx: for (i = 0; i < seg_counts/2; ++i) (vp9_segmentation.c:79).
	for i := range mid {
		wienerVarDiff := midCtr - centers[i]
		if wienerVarDiff < 0 {
			// libvpx assert: wiener_var_diff >= 0. kmeans centers
			// are sorted ascending after kmeans converges, so this
			// holds. Defensive clamp for the corner case where two
			// initial quantile picks land in an empty cluster.
			wienerVarDiff = 0
		}
		targetQStep := baseQStep / (1.0 + wienerVarDiff/perceptualAQVarDiffScale)
		targetQIndex := perceptualQStepToQIndex(targetQStep)
		deltas[i] = int16(targetQIndex - baseQIndex)
	}
	// libvpx: vp9_segmentation.c:89 set segment i=mid to delta=0.
	deltas[mid] = 0
	// libvpx: for (; i < seg_counts; ++i) (vp9_segmentation.c:92). Note
	// the iterator starts at mid (=4) and the body recomputes a
	// non-negative diff; the i=mid case yields delta=0 (already set
	// above) and is overwritten with the same value.
	for i := mid; i < perceptualAQClusters; i++ {
		wienerVarDiff := centers[i] - midCtr
		if wienerVarDiff < 0 {
			wienerVarDiff = 0
		}
		targetQStep := baseQStep * (1.0 + wienerVarDiff/perceptualAQVarDiffScale)
		targetQIndex := perceptualQStepToQIndex(targetQStep)
		deltas[i] = int16(targetQIndex - baseQIndex)
	}
}

// perceptualLogWienerVar ports log_wiener_var
// (vp9_encodeframe.c:3505): log2(1 + wiener_variance).
func perceptualLogWienerVar(variance int64) float64 {
	if variance < 0 {
		variance = 0
	}
	return math.Log(1.0+float64(variance)) / math.Log(2.0)
}

// perceptualSetMBWienerVariance ports set_mb_wiener_variance
// (vp9_encoder.c:5178) over the source luma plane.
//
// libvpx subtracts a zero predictor (effectively casting u8 to s16),
// runs a 16x16 Walsh-Hadamard transform via vpx_hadamard_16x16, zeros
// the DC, takes absolute values of the 255 AC coefficients, sorts them,
// picks the median (coeff[coeff_count/2] = coeff[128]) as the noise
// estimate, applies a per-coefficient Wiener filter, and sums squared
// outputs. The per-MB result is wiener_variance / coeff_count.
//
// govpx note: libvpx reads 16x16 samples directly from the encoder
// source buffer which carries a small VP9_ENC_BORDER_IN_PIXELS border;
// govpx's image.YCbCr has no implicit border, so we replicate the last
// in-frame sample at the right/bottom edges. For all in-frame MBs the
// data path is bitwise identical to libvpx.
func perceptualSetMBWienerVariance(src []byte, stride, width, height,
	mbRows, mbCols int, mbVariance []int64,
) {
	const block = perceptualAQMBSize
	const coeffCount = block * block
	var srcDiff [coeffCount]int16
	var coeff [coeffCount]int32
	var absAC [coeffCount - 1]int32
	for mbRow := range mbRows {
		for mbCol := range mbCols {
			x0 := mbCol * block
			y0 := mbRow * block
			w := block
			if x0+w > width {
				w = width - x0
			}
			h := block
			if y0+h > height {
				h = height - y0
			}
			perceptualGatherMBBlock(src, stride, x0, y0, w, h, srcDiff[:])
			// libvpx: vpx_hadamard_16x16 + coeff[0] = 0.
			perceptualHadamard16x16(srcDiff[:], block, coeff[:])
			coeff[0] = 0
			// libvpx: for (idx = 1; idx < coeff_count; ++idx) abs(coeff)
			for i := 1; i < coeffCount; i++ {
				v := coeff[i]
				if v < 0 {
					v = -v
				}
				absAC[i-1] = v
			}
			// libvpx: qsort(coeff, coeff_count - 1, ...).
			slices.Sort(absAC[:])
			// libvpx: median_val = coeff[coeff_count / 2] = coeff[128].
			// After sorting the 255 AC values, libvpx still indexes
			// coeff[128] in the original buffer-with-DC layout. Since
			// the DC has been zeroed and sorting moved all AC values
			// into [0..254], element 128 of the *original* 256-entry
			// array equals the 128th element of the sorted AC array
			// (zero-indexed). We mirror that exactly.
			median := absAC[(coeffCount-1)/2]
			medianSq := int64(median) * int64(median)
			var wienerVar int64
			// libvpx: for (idx = 1; idx < coeff_count; ++idx) ...
			// We iterate the absAC[] slice which already excludes the
			// zero DC entry, but otherwise the inner math is identical.
			for i := range coeffCount - 1 {
				c := int64(absAC[i])
				sqr := c * c
				tmp := c
				if median != 0 {
					// libvpx: (sqr * coeff[idx]) / (sqr + median^2).
					tmp = (sqr * c) / (sqr + medianSq)
				}
				wienerVar += tmp * tmp
			}
			mbVariance[mbRow*mbCols+mbCol] = wienerVar / int64(coeffCount)
		}
	}
}

// perceptualGatherMBBlock copies a 16x16 region from the luma
// plane into a 16x16 int16 buffer. Out-of-frame samples are replicated
// from the last in-frame row/col to mimic libvpx's source-buffer
// border-padding.
func perceptualGatherMBBlock(src []byte, stride, x0, y0, w, h int, dst []int16) {
	const block = perceptualAQMBSize
	for y := range block {
		srcY := y0 + y
		if y >= h {
			srcY = y0 + h - 1
		}
		row := src[srcY*stride : srcY*stride+stride]
		for x := range block {
			srcX := x0 + x
			if x >= w {
				srcX = x0 + w - 1
			}
			dst[y*block+x] = int16(row[srcX])
		}
	}
}

// perceptualHadamardCol8 ports libvpx's hadamard_col8
// (vpx_dsp/avg.c:199): the 8-point column pass used in 8x8 and 16x16
// Hadamard.
func perceptualHadamardCol8(src []int16, srcStride int, dst []int16) {
	b0 := src[0*srcStride] + src[1*srcStride]
	b1 := src[0*srcStride] - src[1*srcStride]
	b2 := src[2*srcStride] + src[3*srcStride]
	b3 := src[2*srcStride] - src[3*srcStride]
	b4 := src[4*srcStride] + src[5*srcStride]
	b5 := src[4*srcStride] - src[5*srcStride]
	b6 := src[6*srcStride] + src[7*srcStride]
	b7 := src[6*srcStride] - src[7*srcStride]

	c0 := b0 + b2
	c1 := b1 + b3
	c2 := b0 - b2
	c3 := b1 - b3
	c4 := b4 + b6
	c5 := b5 + b7
	c6 := b4 - b6
	c7 := b5 - b7

	dst[0] = c0 + c4
	dst[7] = c1 + c5
	dst[3] = c2 + c6
	dst[4] = c3 + c7
	dst[2] = c0 - c4
	dst[6] = c1 - c5
	dst[1] = c2 - c6
	dst[5] = c3 - c7
}

// perceptualHadamard8x8 ports vpx_hadamard_8x8_c (vpx_dsp/avg.c:231).
func perceptualHadamard8x8(src []int16, srcStride int, coeff []int32) {
	var buffer [64]int16
	var buffer2 [64]int16
	for idx := range 8 {
		perceptualHadamardCol8(src[idx:], srcStride, buffer[idx*8:idx*8+8])
	}
	for idx := range 8 {
		col := [8]int16{
			buffer[idx],
			buffer[8+idx],
			buffer[16+idx],
			buffer[24+idx],
			buffer[32+idx],
			buffer[40+idx],
			buffer[48+idx],
			buffer[56+idx],
		}
		perceptualHadamardCol8(col[:], 1, buffer2[idx*8:idx*8+8])
	}
	for idx := range 64 {
		coeff[idx] = int32(buffer2[idx])
	}
}

// perceptualHadamard16x16 ports vpx_hadamard_16x16_c (vpx_dsp/avg.c:257).
func perceptualHadamard16x16(src []int16, srcStride int, coeff []int32) {
	for idx := range 4 {
		offset := (idx>>1)*8*srcStride + (idx&1)*8
		perceptualHadamard8x8(src[offset:], srcStride, coeff[idx*64:idx*64+64])
	}
	for idx := range 64 {
		a0 := coeff[idx]
		a1 := coeff[64+idx]
		a2 := coeff[128+idx]
		a3 := coeff[192+idx]

		b0 := (a0 + a1) >> 1
		b1 := (a0 - a1) >> 1
		b2 := (a2 + a3) >> 1
		b3 := (a2 - a3) >> 1

		coeff[idx] = b0 + b2
		coeff[64+idx] = b1 + b3
		coeff[128+idx] = b0 - b2
		coeff[192+idx] = b1 - b3
	}
}

// perceptualKMeans ports vp9_kmeans (vp9_encodeframe.c:5549) for
// k = MAX_KMEANS_GROUPS = 8 with 10 iterations. The values slice is
// sorted in-place (libvpx: qsort + compare_kmeans_data ascending).
//
// libvpx:
//
//	5561  qsort(arr, size, sizeof(*arr), compare_kmeans_data);
//	5565  ctr_ls[j] = arr[(size * (2*j+1)) / (2*k)].value;
//	5568  for (itr = 0; itr < 10; ++itr) { ... }
//	5604  compute_boundary_ls(...)
func perceptualKMeans(values []float64,
	centers, boundaries *[perceptualAQClusters]float64,
) {
	sort.Float64s(values)
	size := len(values)
	if size < perceptualAQClusters {
		return
	}
	k := perceptualAQClusters
	// libvpx: initialize the center points by quantile picks.
	for j := range k {
		idx := (size * (2*j + 1)) / (2 * k)
		if idx >= size {
			idx = size - 1
		}
		centers[j] = values[idx]
	}
	// libvpx: 10 Lloyd iterations.
	for range perceptualAQIterations {
		perceptualComputeBoundaries(centers, boundaries)
		var sums [perceptualAQClusters]float64
		var counts [perceptualAQClusters]int
		// libvpx note: because both data and centers are sorted
		// ascending, the group index for successive samples can only
		// increase, so the group cursor is only reset to zero between
		// iterations, not between samples.
		groupIdx := 0
		for i := range size {
			// libvpx: while (arr[i].value >= boundary_ls[group_idx]).
			for groupIdx < k-1 && values[i] >= boundaries[groupIdx] {
				groupIdx++
			}
			sums[groupIdx] += values[i]
			counts[groupIdx]++
		}
		// libvpx: only update non-empty clusters; otherwise the
		// previous center is retained.
		for j := range k {
			if counts[j] > 0 {
				centers[j] = sums[j] / float64(counts[j])
			}
		}
	}
	// libvpx: final compute_boundary_ls after the iteration loop.
	perceptualComputeBoundaries(centers, boundaries)
}

// perceptualComputeBoundaries ports compute_boundary_ls
// (vp9_encodeframe.c:5528): the boundary array stores per-cluster
// midpoints with the last entry pinned to DBL_MAX (math.Inf(+1)
// in Go) so any oversize value lands in the highest cluster.
func perceptualComputeBoundaries(centers,
	boundaries *[perceptualAQClusters]float64,
) {
	for j := range perceptualAQClusters - 1 {
		boundaries[j] = (centers[j] + centers[j+1]) / 2.0
	}
	boundaries[perceptualAQClusters-1] = math.Inf(1)
}

// perceptualGroupIndex ports vp9_get_group_idx
// (vp9_encodeframe.c:5538). The libvpx loop is a linear scan with
// `value >= boundary_ls[group_idx]` and breaks at k-1.
func perceptualGroupIndex(value float64,
	boundaries *[perceptualAQClusters]float64,
) int {
	groupIdx := 0
	for value >= boundaries[groupIdx] {
		groupIdx++
		if groupIdx == perceptualAQClusters-1 {
			break
		}
	}
	return groupIdx
}

// perceptualQIndexToQStep ports vp9_convert_qindex_to_q
// (vp9_ratectrl.c:170) at 8-bit profile 0: ac_quant_lookup[qindex]/4.
func perceptualQIndexToQStep(qindex int) float64 {
	if qindex < 0 {
		qindex = 0
	}
	if qindex > 255 {
		qindex = 255
	}
	return float64(perceptualAcQuant8[qindex]) / 4.0
}

// perceptualQStepToQIndex ports vp9_convert_q_to_qindex
// (vp9_ratectrl.c:185). libvpx linearly scans 0..QINDEX_RANGE looking
// for the first index whose qstep is >= q_val; if none is found it
// clamps to QINDEX_RANGE-1.
func perceptualQStepToQIndex(qstep float64) int {
	const qindexRange = 256
	for i := range qindexRange {
		if perceptualQIndexToQStep(i) >= qstep {
			return i
		}
	}
	return qindexRange - 1
}

// perceptualAcQuant8 is libvpx's ac_qlookup[256] (8-bit Profile 0)
// from vp9/common/vp9_quant_common.c. The value at index i is the AC
// quantizer step at qindex=i.
var perceptualAcQuant8 = [256]int16{
	4, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19,
	20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32,
	33, 34, 35, 36, 37, 38, 39, 40, 41, 42, 43, 44, 45,
	46, 47, 48, 49, 50, 51, 52, 53, 54, 55, 56, 57, 58,
	59, 60, 61, 62, 63, 64, 65, 66, 67, 68, 69, 70, 71,
	72, 73, 74, 75, 76, 77, 78, 79, 80, 81, 82, 83, 84,
	85, 86, 87, 88, 89, 90, 91, 92, 93, 94, 95, 96, 97,
	98, 99, 100, 101, 102, 104, 106, 108, 110, 112, 114, 116, 118,
	120, 122, 124, 126, 128, 130, 132, 134, 136, 138, 140, 142, 144,
	146, 148, 150, 152, 155, 158, 161, 164, 167, 170, 173, 176, 179,
	182, 185, 188, 191, 194, 197, 200, 203, 207, 211, 215, 219, 223,
	227, 231, 235, 239, 243, 247, 251, 255, 260, 265, 270, 275, 280,
	285, 290, 295, 300, 305, 311, 317, 323, 329, 335, 341, 347, 353,
	359, 366, 373, 380, 387, 394, 401, 408, 416, 424, 432, 440, 448,
	456, 465, 474, 483, 492, 501, 510, 520, 530, 540, 550, 560, 571,
	582, 593, 604, 615, 627, 639, 651, 663, 676, 689, 702, 715, 729,
	743, 757, 771, 786, 801, 816, 832, 848, 864, 881, 898, 915, 933,
	951, 969, 988, 1007, 1026, 1046, 1066, 1087, 1108, 1129, 1151, 1173, 1196,
	1219, 1243, 1267, 1292, 1317, 1343, 1369, 1396, 1423, 1451, 1479, 1508, 1537,
	1567, 1597, 1628, 1660, 1692, 1725, 1759, 1793, 1828,
}
