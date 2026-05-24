package encoder

import (
	"math"
	"testing"
)

// TestVP9NNPredictReLUMonotonic exercises the verbatim port of libvpx's
// nn_predict to confirm:
//
//  1. The hidden layer's ReLU clipping is enforced (negative weighted sums
//     are zeroed before the output projection).
//  2. The output layer is purely linear (bias adds, no activation).
//
// libvpx: vp9/encoder/vp9_encodeframe.c:2994-3038.
//
// Constructs a degenerate 2-input / 1-hidden / 1-output config where the
// hidden node has weights [1, 1] and bias 0, and the output node has
// weight [1] and bias 10. A negative input pair like (-2, -3) hits ReLU,
// yielding hidden=0 and output=10. A positive pair (2, 3) skips ReLU,
// yielding hidden=5 and output=15.
func TestVP9NNPredictReLUMonotonic(t *testing.T) {
	w0 := []float32{1, 1}
	b0 := []float32{0}
	w1 := []float32{1}
	b1 := []float32{10}
	cfg := &NNConfig{
		NumInputs:       2,
		NumOutputs:      1,
		NumHiddenLayers: 1,
	}
	cfg.NumHiddenNodes[0] = 1
	cfg.Weights[0] = w0
	cfg.Weights[1] = w1
	cfg.Bias[0] = b0
	cfg.Bias[1] = b1

	out := []float32{0}
	NNPredict([]float32{-2, -3}, cfg, out)
	if out[0] != 10 {
		t.Fatalf("ReLU-clipped output: got %v, want 10", out[0])
	}
	out[0] = 0
	NNPredict([]float32{2, 3}, cfg, out)
	if out[0] != 15 {
		t.Fatalf("Linear output: got %v, want 15", out[0])
	}
}

// TestVP9VarPartNNConfigsShape locks the high-level shape of the three
// variance-partition NN configs against the libvpx upstream definitions.
//
// libvpx:
//   - vp9_var_part_nnconfig_64 (vp9_partition_models.h:636-651)
//   - vp9_var_part_nnconfig_32 (vp9_partition_models.h:678-693)
//   - vp9_var_part_nnconfig_16 (vp9_partition_models.h:720-735)
//
// All three share FEATURES=6, num_outputs=1, num_hidden_layers=1,
// num_hidden_nodes=[8].
func TestVP9VarPartNNConfigsShape(t *testing.T) {
	for name, cfg := range map[string]*NNConfig{
		"64": &VarPartNNConfig64,
		"32": &VarPartNNConfig32,
		"16": &VarPartNNConfig16,
	} {
		if cfg.NumInputs != 6 {
			t.Errorf("nnconfig_%s NumInputs=%d, want 6", name, cfg.NumInputs)
		}
		if cfg.NumOutputs != 1 {
			t.Errorf("nnconfig_%s NumOutputs=%d, want 1", name, cfg.NumOutputs)
		}
		if cfg.NumHiddenLayers != 1 {
			t.Errorf("nnconfig_%s NumHiddenLayers=%d, want 1", name, cfg.NumHiddenLayers)
		}
		if cfg.NumHiddenNodes[0] != 8 {
			t.Errorf("nnconfig_%s NumHiddenNodes[0]=%d, want 8",
				name, cfg.NumHiddenNodes[0])
		}
		if got := len(cfg.Weights[0]); got != 6*8 {
			t.Errorf("nnconfig_%s Weights[0] len=%d, want %d", name, got, 6*8)
		}
		if got := len(cfg.Weights[1]); got != 8 {
			t.Errorf("nnconfig_%s Weights[1] len=%d, want 8", name, got)
		}
		if got := len(cfg.Bias[0]); got != 8 {
			t.Errorf("nnconfig_%s Bias[0] len=%d, want 8", name, got)
		}
		if got := len(cfg.Bias[1]); got != 1 {
			t.Errorf("nnconfig_%s Bias[1] len=%d, want 1", name, got)
		}
	}
}

// TestVP9VarPartNNTablesMatchLibvpx pins every constant in
// vp9_var_part_nn_{weights,bias}_{64,32,16}_layer{0,1} against libvpx
// vp9/encoder/vp9_partition_models.h:610-735.
//
// Order of entries follows libvpx's row-major layout (FEATURES=6 inputs
// by 8 hidden nodes for layer 0, 8 hidden inputs by 1 output for layer 1).
// Constants are reproduced verbatim from the published header.
func TestVP9VarPartNNTablesMatchLibvpx(t *testing.T) {
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
		{"weights_64_layer0", VarPartNNWeights64Layer0[:], wantW64L0[:]},
		{"bias_64_layer0", VarPartNNBias64Layer0[:], wantB64L0[:]},
		{"weights_64_layer1", VarPartNNWeights64Layer1[:], wantW64L1[:]},
		{"bias_64_layer1", VarPartNNBias64Layer1[:], wantB64L1[:]},
		{"weights_32_layer0", VarPartNNWeights32Layer0[:], wantW32L0[:]},
		{"bias_32_layer0", VarPartNNBias32Layer0[:], wantB32L0[:]},
		{"weights_32_layer1", VarPartNNWeights32Layer1[:], wantW32L1[:]},
		{"bias_32_layer1", VarPartNNBias32Layer1[:], wantB32L1[:]},
		{"weights_16_layer0", VarPartNNWeights16Layer0[:], wantW16L0[:]},
		{"bias_16_layer0", VarPartNNBias16Layer0[:], wantB16L0[:]},
		{"weights_16_layer1", VarPartNNWeights16Layer1[:], wantW16L1[:]},
		{"bias_16_layer1", VarPartNNBias16Layer1[:], wantB16L1[:]},
	}
	for _, tr := range tables {
		if len(tr.got) != len(tr.want) {
			t.Errorf("%s: len mismatch got=%d want=%d", tr.name, len(tr.got), len(tr.want))
			continue
		}
		for i := range tr.got {
			if tr.got[i] != tr.want[i] {
				t.Errorf("%s[%d]: got %v want %v", tr.name, i, tr.got[i], tr.want[i])
			}
		}
	}
}

// TestVP9NNPredictVarPart64InferenceFixed pins NNPredict on the
// vp9_var_part_nnconfig_64 NN with a fixed feature vector. The expected
// score is computed manually here using the same float32 sequence of
// operations as libvpx's nn_predict (vp9_encodeframe.c:2994-3036).
func TestVP9NNPredictVarPart64InferenceFixed(t *testing.T) {
	// Fixed features: dc_q == 32, var == 1000, sub_vars == [200, 300, 250, 250].
	//
	// features[0] = log((32*32)/256 + 1) = log(5).
	// features[1] = log(1000 + 1).
	// factor = 1/1000 = 0.001.
	// features[2..5] = factor * sub_vars = [0.2, 0.3, 0.25, 0.25].
	features := [6]float32{
		float32(math.Log(5.0)),
		float32(math.Log(1001.0)),
		0.2, 0.3, 0.25, 0.25,
	}

	want := referenceNNPredictVarPart64(features)

	var got [1]float32
	NNPredict(features[:], &VarPartNNConfig64, got[:])
	if got[0] != want {
		t.Fatalf("NNPredict on vp9_var_part_nnconfig_64 features=%v: got %v want %v (delta=%v)",
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
	w0 := VarPartNNWeights64Layer0
	b0 := VarPartNNBias64Layer0
	var hidden [8]float32
	for node := range 8 {
		var val float32
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
	w1 := VarPartNNWeights64Layer1
	b1 := VarPartNNBias64Layer1
	var out float32
	for i := range 8 {
		out += w1[i] * hidden[i]
	}
	out += b1[0]
	return out
}
