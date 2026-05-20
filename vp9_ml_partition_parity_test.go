package govpx

import (
	"math"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// vp9_ml_partition_parity_test.go pins the verbatim port of the
// ML_BASED_PARTITION (ml_predict_var_partitioning) inputs / inference loop
// against libvpx v1.16.0. Task #171 audit substrate — anchors the
// NEGATIVE FINDING that the RefControl byte-9 cluster's divergence is NOT
// in this layer.
//
// Cluster recap (vp9_oracle_encoder_refcontrols_fuzz_test.go):
//
//	9 of 10 RefControl deferred seeds diverge at byte 9 of the inter
//	frame (FirstPartitionSize literal). Historical ML nonrd measurements
//	pinned per-seed aggregate size_delta in the -179..+295 byte range.
//
// Per-component audit (each component byte-exact against libvpx; if a
// future regression breaks this layer one of these pins fires before the
// fuzz harness):
//
//  1. NN coefficient tables.
//     vp9_var_part_nn_{weights,bias}_{64,32,16}_layer{0,1} byte-for-byte
//     against libvpx vp9/encoder/vp9_partition_models.h:610-735.
//     Tested by TestVP9VarPartNNTablesByteExact below — every constant
//     in all six tables is pinned to its libvpx-printed value.
//
//  2. NN inference loop.
//     vp9NNPredict (vp9_partition_models.go:207-254) mirrors libvpx
//     nn_predict at vp9/encoder/vp9_encodeframe.c:2994-3036. Tested by
//     TestVP9NNPredictVarPart64InferenceFixed below — runs the
//     vp9_var_part_nnconfig_64 NN with a fixed feature vector and pins
//     the float32 output to a hand-computed forward pass.
//
//  3. Feature extraction (ml_predict_var_partitioning body).
//     vp9MLPredictVarPartitioning (vp9_nonrd_pick_partition.go:372-493)
//     mirrors libvpx ml_predict_var_partitioning at
//     vp9_encodeframe.c:4530-4593. Tested by
//     TestVP9MLPredictVarPartitioningSyntheticUniform below — drives the
//     full features[0..5] -> nn_predict -> threshold path on a synthetic
//     uniform 64x64 estPred / source pair and pins the output.
//
//  4. vp9PredVariance (vp9_nonrd_pick_partition.go:499-523).
//     Mirrors libvpx vpx_variance_NxN at vpx_dsp/variance.c:128-135 +
//     the underlying `variance` helper at lines 52-70. Tested by
//     TestVP9PredVarianceAgainstReference below — pins the (sse, mean,
//     variance) tuple against an independent uint64 reference.
//
// If any of these tests fail, the byte-9 cluster has gained a new failure
// mode at this layer and the audit conclusion needs revisiting. If all
// pass, the closure path remains the upstream vp9_pick_inter_mode port at
// vp9/encoder/vp9_pickmode.c:1696 (~4000 LOC), tracked as task #162.

// TestVP9VarPartNNTablesByteExact pins every constant in
// vp9_var_part_nn_{weights,bias}_{64,32,16}_layer{0,1} against libvpx
// vp9/encoder/vp9_partition_models.h:610-735.
//
// Order of entries follows libvpx's row-major layout (FEATURES=6 inputs
// × 8 hidden nodes for layer 0, 8 hidden inputs × 1 output for layer 1).
// Constants are reproduced verbatim from the published header.
func TestVP9VarPartNNTablesByteExact(t *testing.T) {
	// vp9_var_part_nn_weights_64_layer0 (libvpx
	// vp9/encoder/vp9_partition_models.h:611-620). FEATURES=6, hidden=8.
	wantW64L0 := [6 * 8]float32{
		-0.249572, 0.205532, -2.175608, 1.094836, -2.986370, 0.193160,
		-0.143823, 0.378511, -1.997788, -2.166866, -1.930158, -1.202127,
		-0.611875, -0.506422, -0.432487, 0.071205, 0.578172, -0.154285,
		-0.051830, 0.331681, -1.457177, -2.443546, -2.000302, -1.389283,
		0.372084, -0.464917, 2.265235, 2.385787, 2.312722, 2.127868,
		-0.403963, -0.177860, -0.436751, -0.560539, 0.254903, 0.193976,
		-0.305611, 0.256632, 0.309388, -0.437439, 1.702640, -5.007069,
		-0.323450, 0.294227, 1.267193, 1.056601, 0.387181, -0.191215,
	}
	wantB64L0 := [8]float32{
		-0.044396, -0.938166, 0.000000, -0.916375,
		1.242299, 0.000000, -0.405734, 0.014206,
	}
	wantW64L1 := [8]float32{
		1.635945, 0.979557, 0.455315, 1.197199,
		-2.251024, -0.464953, 1.378676, -0.111927,
	}
	wantB64L1 := [1]float32{-0.37972447}

	// vp9_var_part_nn_weights_32_layer0 (libvpx
	// vp9/encoder/vp9_partition_models.h:653-662).
	wantW32L0 := [6 * 8]float32{
		0.067243, -0.083598, -2.191159, 2.726434, -3.324013, 3.477977,
		0.323736, -0.510199, 2.960693, 2.937661, 2.888476, 2.938315,
		-0.307602, -0.503353, -0.080725, -0.473909, -0.417162, 0.457089,
		0.665153, -0.273210, 0.028279, 0.972220, -0.445596, 1.756611,
		-0.177892, -0.091758, 0.436661, -0.521506, 0.133786, 0.266743,
		0.637367, -0.160084, -1.396269, 1.020841, -1.112971, 0.919496,
		-0.235883, 0.651954, 0.109061, -0.429463, 0.740839, -0.962060,
		0.299519, -0.386298, 1.550231, 2.464915, 1.311969, 2.561612,
	}
	wantB32L0 := [8]float32{
		0.368242, 0.736617, 0.000000, 0.757287,
		0.000000, 0.613248, -0.776390, 0.928497,
	}
	wantW32L1 := [8]float32{
		0.939884, -2.420850, -0.410489, -0.186690,
		0.063287, -0.522011, 0.484527, -0.639625,
	}
	wantB32L1 := [1]float32{-0.6455006}

	// vp9_var_part_nn_weights_16_layer0 (libvpx
	// vp9/encoder/vp9_partition_models.h:695-704).
	wantW16L0 := [6 * 8]float32{
		0.742567, -0.580624, -0.244528, 0.331661, -0.113949, -0.559295,
		-0.386061, 0.438653, 1.467463, 0.211589, 0.513972, 1.067855,
		-0.876679, 0.088560, -0.687483, -0.380304, -0.016412, 0.146380,
		0.015318, 0.000351, -2.764887, 3.269717, 2.752428, -2.236754,
		0.561539, -0.852050, -0.084667, 0.202057, 0.197049, 0.364922,
		-0.463801, 0.431790, 1.872096, -0.091887, -0.055034, 2.443492,
		-0.156958, -0.189571, -0.542424, -0.589804, -0.354422, 0.401605,
		0.642021, -0.875117, 2.040794, 1.921070, 1.792413, 1.839727,
	}
	wantB16L0 := [8]float32{
		2.901234, -1.940932, -0.198970, -0.406524,
		0.059422, -1.879207, -0.232340, 2.979821,
	}
	wantW16L1 := [8]float32{
		-0.528731, 0.375234, -0.088422, 0.668629,
		0.870449, 0.578735, 0.546103, -1.957207,
	}
	wantB16L1 := [1]float32{-1.95769405}

	type tableRef struct {
		name string
		got  []float32
		want []float32
	}
	tables := []tableRef{
		{"weights_64_layer0", vp9VarPartNNWeights64Layer0[:], wantW64L0[:]},
		{"bias_64_layer0", vp9VarPartNNBias64Layer0[:], wantB64L0[:]},
		{"weights_64_layer1", vp9VarPartNNWeights64Layer1[:], wantW64L1[:]},
		{"bias_64_layer1", vp9VarPartNNBias64Layer1[:], wantB64L1[:]},
		{"weights_32_layer0", vp9VarPartNNWeights32Layer0[:], wantW32L0[:]},
		{"bias_32_layer0", vp9VarPartNNBias32Layer0[:], wantB32L0[:]},
		{"weights_32_layer1", vp9VarPartNNWeights32Layer1[:], wantW32L1[:]},
		{"bias_32_layer1", vp9VarPartNNBias32Layer1[:], wantB32L1[:]},
		{"weights_16_layer0", vp9VarPartNNWeights16Layer0[:], wantW16L0[:]},
		{"bias_16_layer0", vp9VarPartNNBias16Layer0[:], wantB16L0[:]},
		{"weights_16_layer1", vp9VarPartNNWeights16Layer1[:], wantW16L1[:]},
		{"bias_16_layer1", vp9VarPartNNBias16Layer1[:], wantB16L1[:]},
	}
	for _, tr := range tables {
		if len(tr.got) != len(tr.want) {
			t.Errorf("%s: len mismatch got=%d want=%d", tr.name, len(tr.got), len(tr.want))
			continue
		}
		for i := range tr.got {
			if tr.got[i] != tr.want[i] {
				t.Errorf("%s[%d]: got %v want %v (libvpx %v)",
					tr.name, i, tr.got[i], tr.want[i], tr.want[i])
			}
		}
	}
}

// TestVP9NNPredictVarPart64InferenceFixed pins vp9NNPredict on the
// vp9_var_part_nnconfig_64 NN with a fixed feature vector. The expected
// score is computed manually here using the same float32 sequence of
// operations as libvpx's nn_predict (vp9_encodeframe.c:2994-3036) and
// the libvpx layer-0 weights / biases / layer-1 weights / bias inlined
// above.
//
// If vp9NNPredict ever drifts (e.g. someone reorders the MAC chain or
// alters the ReLU sequence), this test catches the regression directly
// against the libvpx-defined floating-point semantics.
func TestVP9NNPredictVarPart64InferenceFixed(t *testing.T) {
	// Fixed features: dc_q == 32, var == 1000, sub_vars == [200, 300, 250, 250].
	//
	// features[0] = log((32*32)/256 + 1) = log(5) ≈ 1.609438.
	// features[1] = log(1000 + 1) ≈ 6.908755.
	// factor = 1/1000 = 0.001.
	// features[2..5] = factor * sub_vars = [0.2, 0.3, 0.25, 0.25].
	features := [6]float32{
		float32(math.Log(5.0)),
		float32(math.Log(1001.0)),
		0.2, 0.3, 0.25, 0.25,
	}

	// Expected score via the exact libvpx float32 sequence of
	// operations applied to vp9_var_part_nnconfig_64.
	want := referenceNNPredictVarPart64(features)

	var got [1]float32
	vp9NNPredict(features[:], vp9VarPartNNConfig64, got[:])
	if got[0] != want {
		t.Fatalf("vp9NNPredict on vp9_var_part_nnconfig_64 features=%v: got %v want %v (delta=%v)",
			features, got[0], want, got[0]-want)
	}
}

// referenceNNPredictVarPart64 reproduces the libvpx nn_predict forward
// pass against vp9_var_part_nnconfig_64 using the same MAC ordering as
// libvpx (sequential left-to-right accumulation per output node, then
// add bias, then ReLU on hidden layers, linear final layer). The float32
// arithmetic is bit-exact with libvpx given identical inputs.
func referenceNNPredictVarPart64(features [6]float32) float32 {
	// Layer 0: 6 inputs -> 8 hidden, ReLU.
	w0 := vp9VarPartNNWeights64Layer0
	b0 := vp9VarPartNNBias64Layer0
	var hidden [8]float32
	for node := range 8 {
		var val float32 = 0
		for i := range 6 {
			val += w0[node*6+i] * features[i]
		}
		val += b0[node]
		if val < 0 {
			val = 0
		}
		hidden[node] = val
	}
	// Layer 1: 8 hidden -> 1 output, linear.
	w1 := vp9VarPartNNWeights64Layer1
	b1 := vp9VarPartNNBias64Layer1
	var out float32 = 0
	for i := range 8 {
		out += w1[i] * hidden[i]
	}
	out += b1[0]
	return out
}

// TestVP9MLPredictVarPartitioningSyntheticUniform pins the full
// vp9MLPredictVarPartitioning body on a synthetic uniform 64x64 source
// and estPred pair. With identical src == pred, the variance is zero
// and features[1..5] = (0, 1, 1, 1, 1) per the libvpx (var == 0) fast
// path at vp9_encodeframe.c:4571 + 4583. features[0] depends only on
// dc_q (here qindex=128 -> dc_q from VpxDcQuant).
//
// The expected score is the reference forward pass on vp9_var_part_-
// nnconfig_64 evaluated on this exact feature vector. The threshold at
// libvpx vp9_encodeframe.c:4549 is 0.0f for speed > 5 (here speed=8).
// So the returned partition is PARTITION_SPLIT iff score > 0, NONE iff
// score < 0, -1 if exactly zero.
func TestVP9MLPredictVarPartitioningSyntheticUniform(t *testing.T) {
	// Build a synthetic uniform 64x64 source + matching uniform estPred
	// so the variance is exactly 0 over the whole 64x64 and every
	// 32x32 quadrant.
	src := make([]uint8, 64*64)
	for i := range src {
		src[i] = 128
	}
	estPred := [64 * 64]uint8{}
	for i := range estPred {
		estPred[i] = 128
	}
	ctx := &vp9MLPartitionContext{
		estPred:     estPred,
		sbMiRow:     0,
		sbMiCol:     0,
		src:         src,
		srcStride:   64,
		srcOriginX:  0,
		srcOriginY:  0,
		srcVisibleW: 64,
		srcVisibleH: 64,
		baseQindex:  128,
		speed:       8,
		ready:       true,
		frameValid:  true,
	}

	// Compute expected score via the libvpx float-exact reference
	// (var == 0 fast path):
	//   features[0] = log(dc_q*dc_q/256 + 1)
	//   features[1] = log(0 + 1) = 0
	//   features[2..5] = 1
	dcQ := int(vp9dec.VpxDcQuant(128, 0, vp9dec.BitDepth8))
	features := [6]float32{
		float32(math.Log(float64(dcQ*dcQ)/256.0 + 1.0)),
		0,
		1, 1, 1, 1,
	}
	expectedScore := referenceNNPredictVarPart64(features)

	got := vp9MLPredictVarPartitioning(common.Block64x64, 0, 0, ctx)

	// libvpx (vp9_encodeframe.c:4549 + 4590-4592):
	//   thresh = speed <= 5 ? 1.25f : 0.0f;
	//   if (score > thresh) return PARTITION_SPLIT;
	//   if (score < -thresh) return PARTITION_NONE;
	//   return -1;
	var want vp9MLPredictResult
	switch {
	case expectedScore > 0:
		want = vp9MLPredictSplit
	case expectedScore < 0:
		want = vp9MLPredictNone
	default:
		want = vp9MLPredictNone1
	}
	if got != want {
		t.Fatalf("vp9MLPredictVarPartitioning(uniform-128 src+pred, qindex=128, speed=8): got %d want %d (expected_score=%v)",
			got, want, expectedScore)
	}
}

// TestVP9PredVarianceAgainstReference pins vp9PredVariance against an
// independent uint64 reference implementation that mirrors libvpx's
// `variance` helper at vpx_dsp/variance.c:52-70 plus the per-W*H wrapper
// at lines 128-135:
//
//	uint32_t vpx_variance_WxH_c(const uint8_t *src, int src_stride,
//	                            const uint8_t *ref, int ref_stride,
//	                            uint32_t *sse) {
//	  int sum;
//	  variance(src, src_stride, ref, ref_stride, W, H, sse, &sum);
//	  return *sse - (uint32_t)(((int64_t)sum * sum) / (W * H));
//	}
//
// The pin uses non-uniform input pairs sized 16x16 / 32x32 / 64x64 so
// the path through vp9PredVariance + the early-return clause is fully
// exercised.
func TestVP9PredVarianceAgainstReference(t *testing.T) {
	type tc struct {
		name      string
		w, h      int
		seedSrc   uint8
		seedPred  uint8
		srcDelta  int
		predDelta int
	}
	tcs := []tc{
		{"identical_uniform_64x64", 64, 64, 128, 128, 0, 0},
		{"shifted_uniform_64x64", 64, 64, 200, 100, 0, 0},
		{"linear_ramp_32x32", 32, 32, 0, 0, 1, 2},
		{"linear_ramp_16x16", 16, 16, 0, 0, 3, 1},
		{"alternating_32x32", 32, 32, 0, 255, 1, 1},
	}
	for _, c := range tcs {
		t.Run(c.name, func(t *testing.T) {
			stride := c.w
			src := make([]uint8, stride*c.h)
			pred := make([]uint8, stride*c.h)
			for y := range c.h {
				for x := range c.w {
					sv := int(c.seedSrc) + (x+y)*c.srcDelta
					pv := int(c.seedPred) + (x+y)*c.predDelta
					src[y*stride+x] = uint8(sv & 0xff)
					pred[y*stride+x] = uint8(pv & 0xff)
				}
			}
			got := vp9PredVariance(src, stride, 0, 0, pred, stride, 0, 0, c.w, c.h)
			want := refVariance(src, stride, pred, stride, c.w, c.h)
			if got != want {
				t.Errorf("vp9PredVariance %dx%d: got %d want %d", c.w, c.h, got, want)
			}
		})
	}
}

// refVariance mirrors libvpx vpx_variance_WxH_c byte-for-byte using the
// exact `*sse - (uint32_t)((int64_t)sum*sum / (W*H))` integer formula at
// vpx_dsp/variance.c:128-135. The intermediate sum is signed int (32
// bits in libvpx) — we use int64 here for headroom; the cast back to
// uint32 reproduces libvpx's modular reduction.
func refVariance(src []uint8, srcStride int,
	ref []uint8, refStride int, w, h int,
) uint32 {
	var sum int64
	var sse uint64
	for y := range h {
		for x := range w {
			diff := int64(src[y*srcStride+x]) - int64(ref[y*refStride+x])
			sum += diff
			sse += uint64(diff * diff)
		}
	}
	// libvpx: return *sse - (uint32_t)(((int64_t)sum * sum) / (W * H));
	n := int64(w * h)
	q := (sum * sum) / n
	return uint32(sse) - uint32(q)
}
