package govpx

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
	cfg := &vp9NNConfig{
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
	vp9NNPredict([]float32{-2, -3}, cfg, out)
	if out[0] != 10 {
		t.Fatalf("ReLU-clipped output: got %v, want 10", out[0])
	}
	out[0] = 0
	vp9NNPredict([]float32{2, 3}, cfg, out)
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
	for name, cfg := range map[string]*vp9NNConfig{
		"64": vp9VarPartNNConfig64,
		"32": vp9VarPartNNConfig32,
		"16": vp9VarPartNNConfig16,
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

// TestVP9VarPartNNConfig64Constants spot-checks a few constants in the
// 64x64 model against the published libvpx values. If a constant ever
// drifts the test catches it at build time.
//
// libvpx: vp9/encoder/vp9_partition_models.h:611-634.
func TestVP9VarPartNNConfig64Constants(t *testing.T) {
	// First and last weights of layer 0 — most-likely-to-rot positions.
	if got := vp9VarPartNNWeights64Layer0[0]; !floatEq(got, -0.249572) {
		t.Errorf("weights64_layer0[0]=%v, want -0.249572", got)
	}
	if got := vp9VarPartNNWeights64Layer0[6*8-1]; !floatEq(got, -0.191215) {
		t.Errorf("weights64_layer0[last]=%v, want -0.191215", got)
	}
	if got := vp9VarPartNNBias64Layer1[0]; !floatEq(got, -0.37972447) {
		t.Errorf("bias64_layer1[0]=%v, want -0.37972447", got)
	}
}

// TestVP9VarPartNNConfig32Constants spot-checks the 32x32 model.
//
// libvpx: vp9/encoder/vp9_partition_models.h:653-676.
func TestVP9VarPartNNConfig32Constants(t *testing.T) {
	if got := vp9VarPartNNWeights32Layer0[0]; !floatEq(got, 0.067243) {
		t.Errorf("weights32_layer0[0]=%v, want 0.067243", got)
	}
	if got := vp9VarPartNNWeights32Layer0[6*8-1]; !floatEq(got, 2.561612) {
		t.Errorf("weights32_layer0[last]=%v, want 2.561612", got)
	}
	if got := vp9VarPartNNBias32Layer1[0]; !floatEq(got, -0.6455006) {
		t.Errorf("bias32_layer1[0]=%v, want -0.6455006", got)
	}
}

// TestVP9VarPartNNConfig16Constants spot-checks the 16x16 model.
//
// libvpx: vp9/encoder/vp9_partition_models.h:695-718.
func TestVP9VarPartNNConfig16Constants(t *testing.T) {
	if got := vp9VarPartNNWeights16Layer0[0]; !floatEq(got, 0.742567) {
		t.Errorf("weights16_layer0[0]=%v, want 0.742567", got)
	}
	if got := vp9VarPartNNWeights16Layer0[6*8-1]; !floatEq(got, 1.839727) {
		t.Errorf("weights16_layer0[last]=%v, want 1.839727", got)
	}
	if got := vp9VarPartNNBias16Layer1[0]; !floatEq(got, -1.95769405) {
		t.Errorf("bias16_layer1[0]=%v, want -1.95769405", got)
	}
}

func floatEq(a, b float32) bool {
	const eps = 1e-7
	d := float64(a) - float64(b)
	if d < 0 {
		d = -d
	}
	return d < eps || math.IsNaN(d)
}
