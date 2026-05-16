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
	for i, want := range state.deltas {
		hasAltQ := seg.FeatureMask[i]&(1<<uint(vp9dec.SegLvlAltQ)) != 0
		if want == 0 {
			if hasAltQ {
				t.Fatalf("segment %d zero delta has AltQ mask set", i)
			}
			continue
		}
		if !hasAltQ {
			t.Fatalf("segment %d non-zero delta missing AltQ mask", i)
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
