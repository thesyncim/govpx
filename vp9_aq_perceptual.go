package govpx

import (
	"image"
	"math"
	"sort"

	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

const (
	vp9PerceptualAQClusters    = 8
	vp9PerceptualAQIterations  = 10
	vp9PerceptualAQMBSize      = 16
	vp9PerceptualAQVarDiffScale = 4.0
)

// vp9PerceptualAQState holds the per-frame wiener-variance clustering state
// driven by VP9AQPerceptual mode. The state is rebuilt at the start of every
// visible frame from the source luma plane.
type vp9PerceptualAQState struct {
	enabled bool
	ready   bool
	mbRows  int
	mbCols  int
	mbVar   []int64
	values  []float64
	centers [vp9PerceptualAQClusters]float64
	bounds  [vp9PerceptualAQClusters]float64
	deltas  [vp9dec.MaxSegments]int16
}

// configure enables or disables perceptual AQ at construction time.
func (s *vp9PerceptualAQState) configure(enabled bool) {
	s.enabled = enabled
	s.ready = false
	if !enabled {
		s.mbVar = nil
		s.values = nil
	}
}

// prepareFrame computes per-16x16 wiener variances over the source luma
// plane, clusters them via k-means, and caches per-segment AltQ deltas.
// Mirrors libvpx's set_mb_wiener_variance + build_kmeans_segmentation
// pair. When the source is too small to produce stable clusters the state
// stays not-ready and the encoder falls back to no segmentation for the
// frame.
func (s *vp9PerceptualAQState) prepareFrame(img *image.YCbCr, baseQIndex int) bool {
	s.ready = false
	if !s.enabled || img == nil {
		return false
	}
	src, stride, width, height := vp9EncoderSourcePlane(img, 0)
	if len(src) == 0 || stride <= 0 || width <= 0 || height <= 0 {
		return false
	}
	mbCols := (width + vp9PerceptualAQMBSize - 1) / vp9PerceptualAQMBSize
	mbRows := (height + vp9PerceptualAQMBSize - 1) / vp9PerceptualAQMBSize
	if mbCols <= 0 || mbRows <= 0 {
		return false
	}
	s.mbRows = mbRows
	s.mbCols = mbCols
	total := mbRows * mbCols
	if cap(s.mbVar) < total {
		s.mbVar = make([]int64, total)
	} else {
		s.mbVar = s.mbVar[:total]
	}
	if cap(s.values) < total {
		s.values = make([]float64, 0, total)
	} else {
		s.values = s.values[:0]
	}
	for mbRow := 0; mbRow < mbRows; mbRow++ {
		for mbCol := 0; mbCol < mbCols; mbCol++ {
			x0 := mbCol * vp9PerceptualAQMBSize
			y0 := mbRow * vp9PerceptualAQMBSize
			w := min(vp9PerceptualAQMBSize, width-x0)
			h := min(vp9PerceptualAQMBSize, height-y0)
			variance := vp9PerceptualMBWienerVariance(src, stride, x0, y0, w, h)
			s.mbVar[mbRow*mbCols+mbCol] = variance
			s.values = append(s.values, vp9PerceptualLogWienerVar(variance))
		}
	}
	if len(s.values) < vp9PerceptualAQClusters {
		return false
	}
	vp9PerceptualKMeans(s.values, &s.centers, &s.bounds)
	s.computeSegmentDeltas(baseQIndex)
	s.ready = true
	return true
}

// segmentationParams returns the segmentation header VP9AQPerceptual emits.
// Intra (and other refresh) frames carry the full per-segment deltas; inter
// frames inherit them via segmentation header inheritance.
func (s *vp9PerceptualAQState) segmentationParams(intraFrame bool) vp9dec.SegmentationParams {
	seg := vp9dec.SegmentationParams{
		Enabled:   true,
		UpdateMap: true,
		AbsDelta:  false,
	}
	initVP9SegmentationProbDefaults(&seg)
	if !intraFrame || !s.ready {
		return seg
	}
	seg.UpdateData = true
	for i := 0; i < vp9dec.MaxSegments; i++ {
		delta := s.deltas[i]
		if delta == 0 {
			continue
		}
		seg.FeatureMask[i] |= 1 << uint(vp9dec.SegLvlAltQ)
		seg.FeatureData[i][vp9dec.SegLvlAltQ] = delta
	}
	return seg
}

// segmentIDForBlock returns the cluster index assigned to the macroblock at
// (mbRow, mbCol). For blocks that span multiple MBs the caller is expected
// to call once per inner MB and tally the histogram; this helper returns
// the per-MB cluster.
func (s *vp9PerceptualAQState) segmentIDForBlock(miRow, miCol int) uint8 {
	if !s.ready || len(s.mbVar) == 0 {
		return 0
	}
	mbRow := miRow >> 1
	mbCol := miCol >> 1
	if mbRow < 0 {
		mbRow = 0
	}
	if mbCol < 0 {
		mbCol = 0
	}
	if mbRow >= s.mbRows {
		mbRow = s.mbRows - 1
	}
	if mbCol >= s.mbCols {
		mbCol = s.mbCols - 1
	}
	value := vp9PerceptualLogWienerVar(s.mbVar[mbRow*s.mbCols+mbCol])
	return uint8(vp9PerceptualGroupIndex(value, &s.bounds))
}

func (s *vp9PerceptualAQState) computeSegmentDeltas(baseQIndex int) {
	for i := range s.deltas {
		s.deltas[i] = 0
	}
	if baseQIndex < 0 {
		baseQIndex = 0
	}
	if baseQIndex > 255 {
		baseQIndex = 255
	}
	baseQStep := vp9PerceptualQIndexToQStep(baseQIndex)
	mid := s.centers[vp9PerceptualAQClusters/2]
	for i := 0; i < vp9PerceptualAQClusters; i++ {
		if i == vp9PerceptualAQClusters/2 {
			s.deltas[i] = 0
			continue
		}
		var targetQStep float64
		if i < vp9PerceptualAQClusters/2 {
			diff := mid - s.centers[i]
			if diff < 0 {
				diff = 0
			}
			targetQStep = baseQStep / (1.0 + diff/vp9PerceptualAQVarDiffScale)
		} else {
			diff := s.centers[i] - mid
			if diff < 0 {
				diff = 0
			}
			targetQStep = baseQStep * (1.0 + diff/vp9PerceptualAQVarDiffScale)
		}
		targetQIndex := vp9PerceptualQStepToQIndex(targetQStep)
		delta := targetQIndex - baseQIndex
		if baseQIndex != 0 && baseQIndex+delta == 0 {
			delta = -baseQIndex + 1
		}
		if delta < -255 {
			delta = -255
		} else if delta > 255 {
			delta = 255
		}
		s.deltas[i] = int16(delta)
	}
}

// vp9PerceptualLogWienerVar mirrors libvpx's log_wiener_var:
// log2(1 + wiener_variance). Used as the k-means feature value.
func vp9PerceptualLogWienerVar(variance int64) float64 {
	if variance < 0 {
		variance = 0
	}
	return math.Log(1.0+float64(variance)) / math.Log(2.0)
}

// vp9PerceptualMBWienerVariance mirrors libvpx's set_mb_wiener_variance
// inner loop for a single 16x16 macroblock. The block is Hadamard-
// transformed without subtracting any prediction (i.e. against zero),
// the DC is dropped, AC magnitudes are sorted, and the median AC
// magnitude drives the Wiener-style attenuation that scores low-noise
// blocks down.
//
// Sub-16x16 boundary blocks are padded by repeating the last sample,
// matching the libvpx behavior of reading past the visible edge into
// the source border (the source plane includes a small extension).
func vp9PerceptualMBWienerVariance(src []byte, stride, x0, y0, w, h int) int64 {
	if w <= 0 || h <= 0 {
		return 0
	}
	const block = vp9PerceptualAQMBSize
	var srcDiff [block * block]int16
	vp9PerceptualGatherMBBlock(src, stride, x0, y0, w, h, srcDiff[:])
	var coeff [block * block]int32
	vp9PerceptualHadamard16x16(srcDiff[:], block, coeff[:])
	coeff[0] = 0
	const coeffCount = block * block
	abs := [coeffCount - 1]int32{}
	for i := 1; i < coeffCount; i++ {
		v := coeff[i]
		if v < 0 {
			v = -v
		}
		abs[i-1] = v
	}
	sortInt32(abs[:])
	median := abs[(coeffCount-1)/2]
	var wienerVar int64
	for i := 0; i < coeffCount-1; i++ {
		c := int64(abs[i])
		sqr := c * c
		t := c
		if median != 0 {
			t = (sqr * c) / (sqr + int64(median)*int64(median))
		}
		wienerVar += t * t
	}
	return wienerVar / int64(coeffCount)
}

// vp9PerceptualGatherMBBlock copies a 16x16 region from the source plane,
// repeating the last in-frame sample to pad partial right/bottom MBs.
func vp9PerceptualGatherMBBlock(src []byte, stride, x0, y0, w, h int, dst []int16) {
	const block = vp9PerceptualAQMBSize
	for y := 0; y < block; y++ {
		srcY := y0 + y
		if y >= h {
			srcY = y0 + h - 1
		}
		row := src[srcY*stride : srcY*stride+stride]
		for x := 0; x < block; x++ {
			srcX := x0 + x
			if x >= w {
				srcX = x0 + w - 1
			}
			dst[y*block+x] = int16(row[srcX])
		}
	}
}

// sortInt32 sorts an int32 slice in ascending order. The fixed slice
// fan-out keeps the call allocation-free on hot paths.
func sortInt32(a []int32) {
	sort.Slice(a, func(i, j int) bool { return a[i] < a[j] })
}

// vp9PerceptualHadamardCol8 is the column pass of libvpx's hadamard_col8,
// applied along stride.
func vp9PerceptualHadamardCol8(src []int16, srcStride int, dst []int16) {
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

// vp9PerceptualHadamard8x8 mirrors vpx_hadamard_8x8_c.
func vp9PerceptualHadamard8x8(src []int16, srcStride int, coeff []int32) {
	var buffer [64]int16
	var buffer2 [64]int16
	for idx := 0; idx < 8; idx++ {
		vp9PerceptualHadamardCol8(src[idx:], srcStride, buffer[idx*8:idx*8+8])
	}
	for idx := 0; idx < 8; idx++ {
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
		vp9PerceptualHadamardCol8(col[:], 1, buffer2[idx*8:idx*8+8])
	}
	for idx := 0; idx < 64; idx++ {
		coeff[idx] = int32(buffer2[idx])
	}
}

// vp9PerceptualHadamard16x16 mirrors vpx_hadamard_16x16_c.
func vp9PerceptualHadamard16x16(src []int16, srcStride int, coeff []int32) {
	for idx := 0; idx < 4; idx++ {
		offset := (idx>>1)*8*srcStride + (idx&1)*8
		vp9PerceptualHadamard8x8(src[offset:], srcStride, coeff[idx*64:idx*64+64])
	}
	for idx := 0; idx < 64; idx++ {
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

// vp9PerceptualKMeans mirrors libvpx's vp9_kmeans for k=8 with 10
// iterations. The values slice is sorted in-place. The centers and
// boundaries arrays are filled with the converged cluster statistics.
func vp9PerceptualKMeans(values []float64, centers, bounds *[vp9PerceptualAQClusters]float64) {
	sort.Float64s(values)
	size := len(values)
	if size < vp9PerceptualAQClusters {
		return
	}
	for j := 0; j < vp9PerceptualAQClusters; j++ {
		idx := (size * (2*j + 1)) / (2 * vp9PerceptualAQClusters)
		if idx >= size {
			idx = size - 1
		}
		centers[j] = values[idx]
	}
	for itr := 0; itr < vp9PerceptualAQIterations; itr++ {
		vp9PerceptualComputeBoundaries(centers, bounds)
		var sums [vp9PerceptualAQClusters]float64
		var counts [vp9PerceptualAQClusters]int
		groupIdx := 0
		for i := 0; i < size; i++ {
			for groupIdx < vp9PerceptualAQClusters-1 && values[i] >= bounds[groupIdx] {
				groupIdx++
			}
			sums[groupIdx] += values[i]
			counts[groupIdx]++
		}
		for j := 0; j < vp9PerceptualAQClusters; j++ {
			if counts[j] > 0 {
				centers[j] = sums[j] / float64(counts[j])
			}
		}
	}
	vp9PerceptualComputeBoundaries(centers, bounds)
}

// vp9PerceptualComputeBoundaries fills boundary[j] with the midpoint
// between centers[j] and centers[j+1]; the last boundary is +Inf so any
// value past it lands in the highest cluster.
func vp9PerceptualComputeBoundaries(centers *[vp9PerceptualAQClusters]float64,
	bounds *[vp9PerceptualAQClusters]float64,
) {
	for j := 0; j < vp9PerceptualAQClusters-1; j++ {
		bounds[j] = (centers[j] + centers[j+1]) / 2.0
	}
	bounds[vp9PerceptualAQClusters-1] = math.Inf(1)
}

// vp9PerceptualGroupIndex finds the cluster a value belongs to by linear
// scan of the boundary array; matches libvpx's vp9_get_group_idx.
func vp9PerceptualGroupIndex(value float64, bounds *[vp9PerceptualAQClusters]float64) int {
	for j := 0; j < vp9PerceptualAQClusters-1; j++ {
		if value < bounds[j] {
			return j
		}
	}
	return vp9PerceptualAQClusters - 1
}

// vp9PerceptualQIndexToQStep mirrors libvpx's vp9_convert_qindex_to_q
// (vp9_quantize.c) for 8-bit profile 0. The libvpx table reads
// qstep = ac_quant_lookup[qindex] / 4.0 in 8-bit mode.
func vp9PerceptualQIndexToQStep(qindex int) float64 {
	if qindex < 0 {
		qindex = 0
	}
	if qindex > 255 {
		qindex = 255
	}
	return float64(vp9PerceptualAcQuant8[qindex]) / 4.0
}

// vp9PerceptualQStepToQIndex inverts vp9PerceptualQIndexToQStep by binary
// search over the AC lookup table. Mirrors libvpx's vp9_convert_q_to_qindex
// (vp9_quantize.c).
func vp9PerceptualQStepToQIndex(qstep float64) int {
	target := qstep * 4.0
	lo, hi := 0, 255
	for lo < hi {
		mid := (lo + hi) >> 1
		if float64(vp9PerceptualAcQuant8[mid]) < target {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}

// vp9PerceptualAcQuant8 is libvpx's ac_qlookup[256] (8-bit Profile 0)
// from vp9/common/vp9_quant_common.c. The value at index i is the AC
// quantizer step at qindex=i.
var vp9PerceptualAcQuant8 = [256]int16{
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

