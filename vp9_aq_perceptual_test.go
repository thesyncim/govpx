package govpx

import (
	"errors"
	"math"
	"testing"

	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func TestVP9PerceptualHadamard8x8MatchesLibvpx(t *testing.T) {
	// Constant 1-block input: every sample == 1.
	// Expected: 8x8 Hadamard yields a single DC peak at coeff[0] = 64
	// (sum) and zeros elsewhere; the libvpx routine emits an unordered
	// layout, so we check that exactly one coefficient is 64 and the
	// rest are zero.
	var src [64]int16
	for i := range src {
		src[i] = 1
	}
	var coeff [64]int32
	vp9PerceptualHadamard8x8(src[:], 8, coeff[:])
	hits := 0
	for _, c := range coeff {
		switch c {
		case 0:
		case 64:
			hits++
		default:
			t.Fatalf("unexpected coeff %d in constant-1 Hadamard 8x8", c)
		}
	}
	if hits != 1 {
		t.Fatalf("Hadamard 8x8 DC hit count = %d, want 1", hits)
	}
}

func TestVP9PerceptualHadamard16x16ConstantInput(t *testing.T) {
	// Constant input: the 16x16 Hadamard's only non-zero coefficient
	// should be the DC, equal to sum / 2 = (256 * 1) / 2 = 128 after
	// the >>1 normalization in the second pass.
	var src [256]int16
	for i := range src {
		src[i] = 1
	}
	var coeff [256]int32
	vp9PerceptualHadamard16x16(src[:], 16, coeff[:])
	dc := int32(0)
	nonZero := 0
	for _, c := range coeff {
		if c != 0 {
			nonZero++
			dc = c
		}
	}
	if nonZero != 1 {
		t.Fatalf("Hadamard 16x16 constant input non-zero coeffs = %d, want 1", nonZero)
	}
	if dc != 128 {
		t.Fatalf("Hadamard 16x16 DC = %d, want 128", dc)
	}
}

func TestVP9PerceptualLogWienerVarMonotone(t *testing.T) {
	cases := []int64{0, 1, 4, 100, 10_000, 1_000_000}
	prev := math.Inf(-1)
	for _, v := range cases {
		got := vp9PerceptualLogWienerVar(v)
		if got <= prev {
			t.Fatalf("log_wiener_var(%d) = %g not greater than prev %g", v, got, prev)
		}
		prev = got
	}
}

func TestVP9PerceptualKMeansSplitsBimodalDistribution(t *testing.T) {
	values := make([]float64, 0, 64)
	for i := range 32 {
		values = append(values, 1.0+float64(i)*0.01)
	}
	for i := range 32 {
		values = append(values, 100.0+float64(i)*0.01)
	}
	var centers, bounds [vp9PerceptualAQClusters]float64
	vp9PerceptualKMeans(values, &centers, &bounds)
	for i := 1; i < len(centers); i++ {
		if centers[i] < centers[i-1] {
			t.Fatalf("centers not monotonic: %v", centers)
		}
	}
	if centers[0] > 2.0 {
		t.Fatalf("low-cluster centroid = %g, want <= 2.0; centers=%v",
			centers[0], centers)
	}
	if centers[vp9PerceptualAQClusters-1] < 99.0 {
		t.Fatalf("high-cluster centroid = %g, want >= 99.0; centers=%v",
			centers[vp9PerceptualAQClusters-1], centers)
	}
}

func TestVP9PerceptualGroupIndexBoundaries(t *testing.T) {
	bounds := [vp9PerceptualAQClusters]float64{
		1, 2, 3, 4, 5, 6, 7, math.Inf(1),
	}
	cases := []struct {
		v    float64
		want int
	}{
		{0.5, 0},
		{1.5, 1},
		{2.5, 2},
		{3.5, 3},
		{4.5, 4},
		{5.5, 5},
		{6.5, 6},
		{7.5, 7},
		{1e9, 7},
	}
	for _, tc := range cases {
		if got := vp9PerceptualGroupIndex(tc.v, &bounds); got != tc.want {
			t.Fatalf("group(%g) = %d, want %d", tc.v, got, tc.want)
		}
	}
}

func TestVP9PerceptualQIndexQStepRoundtrip(t *testing.T) {
	for q := 4; q < 200; q += 17 {
		step := vp9PerceptualQIndexToQStep(q)
		back := vp9PerceptualQStepToQIndex(step)
		if back != q {
			t.Fatalf("qindex %d -> step %g -> qindex %d", q, step, back)
		}
	}
}

func TestVP9EncoderPerceptualAQValidation(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*VP9EncoderOptions)
		err  error
	}{
		{"lossless", func(o *VP9EncoderOptions) {
			o.Lossless = true
		}, ErrInvalidConfig},
		{"static segmentation", func(o *VP9EncoderOptions) {
			o.Segmentation.Enabled = true
		}, ErrInvalidConfig},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := VP9EncoderOptions{
				Width:  64,
				Height: 64,
				FPS:    30,
				AQMode: VP9AQPerceptual,
			}
			tc.mut(&opts)
			if _, err := NewVP9Encoder(opts); !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
		})
	}
}

func TestVP9EncoderPerceptualAQAcceptsConfiguration(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:  64,
		Height: 64,
		FPS:    30,
		AQMode: VP9AQPerceptual,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if !e.perceptualAQ.enabled {
		t.Fatal("perceptualAQ.enabled = false, want true")
	}
}

func TestVP9EncoderPerceptualAQEncodesFrame(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:  64,
		Height: 64,
		FPS:    30,
		AQMode: VP9AQPerceptual,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	// A non-flat checker pattern exercises both ZERO and AC coefficients.
	src := newVP9CheckerYCbCrForTest(64, 64, 32, 224, 128, 128)
	dst := make([]byte, 65536)
	n, err := e.EncodeInto(src, dst)
	if err != nil {
		t.Fatalf("EncodeInto: %v", err)
	}
	if n <= 0 {
		t.Fatalf("EncodeInto returned %d bytes", n)
	}
	if !e.perceptualAQ.ready {
		t.Fatal("perceptualAQ.ready = false after encode")
	}
	hdr, _ := parseVP9EncoderHeaderForTest(t, dst[:n])
	if !hdr.Seg.Enabled {
		t.Fatal("segmentation header disabled; perceptual AQ expected to enable it")
	}
}

func TestVP9PerceptualSegmentationParamsEmitsAltQOnIntra(t *testing.T) {
	state := vp9PerceptualAQState{
		enabled: true,
		ready:   true,
	}
	state.deltas[0] = -10
	state.deltas[1] = -5
	state.deltas[4] = 0
	state.deltas[7] = 12
	seg := state.segmentationParams(true)
	if !seg.Enabled || !seg.UpdateMap || !seg.UpdateData {
		t.Fatalf("seg flags = enabled:%t updateMap:%t updateData:%t",
			seg.Enabled, seg.UpdateMap, seg.UpdateData)
	}
	// libvpx (vp9_segmentation.c:86, :90, :99) enables the SEG_LVL_ALT_Q
	// feature for every segment, including the mid segment with
	// delta=0.
	for i, want := range state.deltas {
		hasAltQ := seg.FeatureMask[i]&(1<<uint(vp9dec.SegLvlAltQ)) != 0
		if !hasAltQ {
			t.Fatalf("segment %d missing AltQ mask (libvpx enables for every segment)", i)
		}
		if seg.FeatureData[i][vp9dec.SegLvlAltQ] != want {
			t.Fatalf("segment %d FeatureData = %d, want %d", i,
				seg.FeatureData[i][vp9dec.SegLvlAltQ], want)
		}
	}
}

func TestVP9PerceptualSegmentationParamsInterInheritsData(t *testing.T) {
	state := vp9PerceptualAQState{enabled: true, ready: true}
	state.deltas[2] = -3
	seg := state.segmentationParams(false)
	if !seg.Enabled || !seg.UpdateMap {
		t.Fatalf("seg flags = enabled:%t updateMap:%t", seg.Enabled, seg.UpdateMap)
	}
	if seg.UpdateData {
		t.Fatal("inter frame must not write UpdateData under perceptual AQ")
	}
}

// TestVP9PerceptualAQModeSetupMatchesLibvpxFormula asserts the
// Go-side per-cluster delta_q formula is byte-equal to libvpx
// vp9_perceptual_aq_mode_setup (vp9_segmentation.c:63) for a fixed
// set of synthetic cluster centers and base qindex. This is the
// arithmetic identity that fixes the algorithm's sign convention:
// clusters below mid get negative delta (finer Q, MORE bits),
// mid gets zero, clusters above mid get positive delta (coarser Q,
// fewer bits).
//
// Reference values use the libvpx formula:
//
//	base_qstep   = ac_quant[base] / 4
//	for i < mid:  target_qstep = base_qstep / (1 + (mid_ctr - ctr[i]) / 4)
//	for i = mid:  delta = 0
//	for i > mid:  target_qstep = base_qstep * (1 + (ctr[i] - mid_ctr) / 4)
//	delta        = vp9_convert_q_to_qindex(target_qstep) - base_qindex
//
// ac_quant lookup at base = 32 is vp9PerceptualAcQuant8[32] = 39
// (libvpx ac_qlookup[32]; vp9_quant_common.c:89), so
// base_qstep = 39/4 = 9.75.
//
// Cluster centers: linearly spaced 0..7 with mid = ctr[4] = 4.
// Each delta is computed by the libvpx formula and then mapped through
// the libvpx ac_qlookup table via the linear-scan
// vp9_convert_q_to_qindex.
func TestVP9PerceptualAQModeSetupMatchesLibvpxFormula(t *testing.T) {
	centers := []float64{0, 1, 2, 3, 4, 5, 6, 7}
	var deltas [vp9dec.MaxSegments]int16
	vp9PerceptualAQModeSetup(centers, 32, deltas[:])
	// Cross-check via an independent inline implementation of the
	// libvpx formula so the assertion is not "code agrees with
	// itself".
	const base = 32
	baseQ := float64(vp9PerceptualAcQuant8[base]) / 4.0
	mid := 4
	wantDeltas := func(i int) int16 {
		if i == mid {
			return 0
		}
		var ts float64
		if i < mid {
			diff := centers[mid] - centers[i]
			ts = baseQ / (1.0 + diff/4.0)
		} else {
			diff := centers[i] - centers[mid]
			ts = baseQ * (1.0 + diff/4.0)
		}
		// Inline libvpx vp9_convert_q_to_qindex: first index whose
		// ac_quant/4 >= ts; clamp to QINDEX_RANGE-1.
		idx := 255
		for j := range 256 {
			if float64(vp9PerceptualAcQuant8[j])/4.0 >= ts {
				idx = j
				break
			}
		}
		return int16(idx - base)
	}
	for i := range vp9PerceptualAQClusters {
		w := wantDeltas(i)
		if deltas[i] != w {
			t.Errorf("delta[%d] = %d, want %d (libvpx-formula reference)",
				i, deltas[i], w)
		}
	}
	// Sanity: delta is monotonically increasing across the segment
	// id sequence (libvpx invariant: lower-variance clusters get
	// finer Q, higher-variance clusters get coarser Q).
	for i := 1; i < vp9PerceptualAQClusters; i++ {
		if deltas[i] < deltas[i-1] {
			t.Errorf("delta[%d] = %d < delta[%d] = %d: not monotonically increasing",
				i, deltas[i], i-1, deltas[i-1])
		}
	}
}

// TestVP9PerceptualAQModeSetupSignConventionMatchesLibvpx asserts the
// upstream sign convention: clusters[i<mid] receive negative delta_q
// (finer Q than base, MORE bits), clusters[i>mid] receive positive
// delta_q (coarser Q, fewer bits). This is the libvpx behaviour
// (vp9_segmentation.c:79-100) that the prior hand-rolled
// cluster-0-anchor implementation inverted.
func TestVP9PerceptualAQModeSetupSignConventionMatchesLibvpx(t *testing.T) {
	centers := []float64{0, 1, 2, 3, 4, 5, 6, 7}
	var deltas [vp9dec.MaxSegments]int16
	vp9PerceptualAQModeSetup(centers, 100, deltas[:])
	for i := range vp9PerceptualAQClusters / 2 {
		if deltas[i] >= 0 {
			t.Errorf("delta[%d] = %d, want negative (libvpx low-cluster sign)",
				i, deltas[i])
		}
	}
	if deltas[vp9PerceptualAQClusters/2] != 0 {
		t.Errorf("delta[mid] = %d, want 0 (libvpx mid-cluster anchor)",
			deltas[vp9PerceptualAQClusters/2])
	}
	for i := vp9PerceptualAQClusters/2 + 1; i < vp9PerceptualAQClusters; i++ {
		if deltas[i] <= 0 {
			t.Errorf("delta[%d] = %d, want positive (libvpx high-cluster sign)",
				i, deltas[i])
		}
	}
}

// TestVP9PerceptualKMeansInitMatchesLibvpxQuantilePicks asserts the
// k-means center initialization picks the (size*(2j+1))/(2k)
// quantiles of the sorted data, matching libvpx
// vp9_encodeframe.c:5565.
//
// For size = 32 and k = 8, the picks are:
//
//	j=0 -> 32*1/16 = 2
//	j=1 -> 32*3/16 = 6
//	j=2 -> 32*5/16 = 10
//	j=3 -> 32*7/16 = 14
//	j=4 -> 32*9/16 = 18
//	j=5 -> 32*11/16 = 22
//	j=6 -> 32*13/16 = 26
//	j=7 -> 32*15/16 = 30
//
// We construct a sorted-once-already array values[i] = float64(i),
// which after the libvpx 10 Lloyd iterations on uniform data
// converges to the bin-center means: (left_bound+right_bound)/2.
// On 32 uniform 0..31 samples split into 8 bins, the bins are
// [0..3], [4..7], ..., [28..31] with means 1.5, 5.5, 9.5, ...,
// 29.5. We assert convergence to these means within 1e-9.
func TestVP9PerceptualKMeansInitMatchesLibvpxQuantilePicks(t *testing.T) {
	values := make([]float64, 32)
	for i := range values {
		values[i] = float64(i)
	}
	var centers, bounds [vp9PerceptualAQClusters]float64
	vp9PerceptualKMeans(values, &centers, &bounds)
	want := [vp9PerceptualAQClusters]float64{1.5, 5.5, 9.5, 13.5, 17.5, 21.5, 25.5, 29.5}
	for i, w := range want {
		if math.Abs(centers[i]-w) > 1e-9 {
			t.Errorf("center[%d] = %.6f, want %.6f", i, centers[i], w)
		}
	}
	// libvpx compute_boundary_ls (vp9_encodeframe.c:5528): boundary[j]
	// is midpoint of centers[j] and centers[j+1], last is +Inf.
	for i := range vp9PerceptualAQClusters - 1 {
		wantBound := (want[i] + want[i+1]) / 2
		if math.Abs(bounds[i]-wantBound) > 1e-9 {
			t.Errorf("bound[%d] = %.6f, want %.6f", i, bounds[i], wantBound)
		}
	}
	if !math.IsInf(bounds[vp9PerceptualAQClusters-1], 1) {
		t.Errorf("last bound = %g, want +Inf", bounds[vp9PerceptualAQClusters-1])
	}
}

// TestVP9PerceptualMBWienerVarianceConstantBlock asserts that a
// constant 16x16 block (all samples equal) yields zero AC and
// therefore zero Wiener variance, matching libvpx's
// set_mb_wiener_variance behaviour after the DC has been zeroed.
func TestVP9PerceptualMBWienerVarianceConstantBlock(t *testing.T) {
	const stride = 16
	src := make([]byte, stride*16)
	for i := range src {
		src[i] = 128
	}
	var mbVar [1]int64
	vp9PerceptualSetMBWienerVariance(src, stride, 16, 16, 1, 1, mbVar[:])
	if mbVar[0] != 0 {
		t.Fatalf("constant-block Wiener variance = %d, want 0", mbVar[0])
	}
}

// TestVP9PerceptualWienerVarSegmentMajorityVote asserts the SB-level
// segment assignment is the majority cluster among the 16 MBs that
// make up a 64x64 SB, matching libvpx wiener_var_segment
// (vp9_encodeframe.c:3560).
func TestVP9PerceptualWienerVarSegmentMajorityVote(t *testing.T) {
	mbVar := make([]int64, 16) // 4x4 MBs = 1 SB
	// Put 10 MBs with a value that lands in cluster 0 (variance 0,
	// log2(1+0) = 0). Put 6 MBs with a high value that lands in
	// cluster 7.
	mbVar[0] = 0
	mbVar[1] = 0
	mbVar[2] = 0
	mbVar[3] = 0
	mbVar[4] = 0
	mbVar[5] = 0
	mbVar[6] = 0
	mbVar[7] = 0
	mbVar[8] = 0
	mbVar[9] = 0
	for i := 10; i < 16; i++ {
		mbVar[i] = 1 << 30 // very high
	}
	bounds := [vp9PerceptualAQClusters]float64{1, 2, 3, 4, 5, 6, 7, math.Inf(1)}
	seg := vp9PerceptualWienerVarSegment(mbVar, 4, 4, 0, 0, &bounds)
	if seg != 0 {
		t.Fatalf("majority vote = cluster %d, want 0 (10 of 16 MBs)", seg)
	}
	// Swap: 10 high, 6 low.
	for i := range 6 {
		mbVar[i] = 0
	}
	for i := 6; i < 16; i++ {
		mbVar[i] = 1 << 30
	}
	seg = vp9PerceptualWienerVarSegment(mbVar, 4, 4, 0, 0, &bounds)
	if seg != 7 {
		t.Fatalf("majority vote = cluster %d, want 7 (10 of 16 MBs)", seg)
	}
}
