package govpx

// vp9_partition_models.go is the verbatim port of the libvpx v1.16.0 VP9
// partition-search neural-net models defined in
// vp9/encoder/vp9_partition_models.h. This file mirrors the upstream layout:
//   - NN_CONFIG struct (vp9_partition_models.h:24-34) → vp9NNConfig.
//   - vp9_var_part_nn_weights_*_layer*  /  vp9_var_part_nn_bias_*_layer*
//     (vp9_partition_models.h:611-733) → vp9VarPart{64,32,16}* tables.
//   - vp9_var_part_nnconfig_{64,32,16}  (vp9_partition_models.h:636-735)
//     → vp9VarPartNNConfig{64,32,16}.
//
// The weights and biases here MUST be byte-identical to libvpx so that
// nn_predict produces the same float outputs for the same float inputs. Do
// not edit constants — refresh by re-copying from libvpx v1.16.0 if it ever
// changes. The constants are reproduced exactly as written upstream (the
// trailing `f` literal suffix is C-only; Go float32 literals carry the
// same IEEE-754 bit pattern by the language definition).
//
// At this revision the tables are unused: the live partition picker in
// pickVP9InterPartitionBlockSize does not yet invoke vp9MlPredictVarPartitioning.
// They are landed as Phase A of the ML_BASED_PARTITION port so the
// follow-up nonrd_pick_partition port can wire them in without first
// re-deriving the constants. See skipVP9MLBasedPartitionInterByteParity for
// the test-side deferral citation.

// vp9NNMaxHiddenLayers mirrors NN_MAX_HIDDEN_LAYERS
// (vp9/encoder/vp9_partition_models.h:18).
const vp9NNMaxHiddenLayers = 10

// vp9NNMaxNodesPerLayer mirrors NN_MAX_NODES_PER_LAYER
// (vp9/encoder/vp9_partition_models.h:19).
const vp9NNMaxNodesPerLayer = 128

// vp9NNConfig mirrors the NN_CONFIG struct
// (vp9/encoder/vp9_partition_models.h:24-34):
//
//	typedef struct {
//	  int num_inputs;
//	  int num_outputs;
//	  int num_hidden_layers;
//	  int num_hidden_nodes[NN_MAX_HIDDEN_LAYERS];
//	  const float *weights[NN_MAX_HIDDEN_LAYERS + 1];
//	  const float *bias[NN_MAX_HIDDEN_LAYERS + 1];
//	} NN_CONFIG;
type vp9NNConfig struct {
	NumInputs       int
	NumOutputs      int
	NumHiddenLayers int
	NumHiddenNodes  [vp9NNMaxHiddenLayers]int
	Weights         [vp9NNMaxHiddenLayers + 1][]float32
	Bias            [vp9NNMaxHiddenLayers + 1][]float32
}

// vp9_var_part_nn_weights_64_layer0 (libvpx
// vp9/encoder/vp9_partition_models.h:611-620). FEATURES=6, hidden=8.
var vp9VarPartNNWeights64Layer0 = [6 * 8]float32{
	-0.249572, 0.205532, -2.175608, 1.094836, -2.986370, 0.193160,
	-0.143823, 0.378511, -1.997788, -2.166866, -1.930158, -1.202127,
	-0.611875, -0.506422, -0.432487, 0.071205, 0.578172, -0.154285,
	-0.051830, 0.331681, -1.457177, -2.443546, -2.000302, -1.389283,
	0.372084, -0.464917, 2.265235, 2.385787, 2.312722, 2.127868,
	-0.403963, -0.177860, -0.436751, -0.560539, 0.254903, 0.193976,
	-0.305611, 0.256632, 0.309388, -0.437439, 1.702640, -5.007069,
	-0.323450, 0.294227, 1.267193, 1.056601, 0.387181, -0.191215,
}

// vp9_var_part_nn_bias_64_layer0 (libvpx
// vp9/encoder/vp9_partition_models.h:622-625).
var vp9VarPartNNBias64Layer0 = [8]float32{
	-0.044396, -0.938166, 0.000000, -0.916375,
	1.242299, 0.000000, -0.405734, 0.014206,
}

// vp9_var_part_nn_weights_64_layer1 (libvpx
// vp9/encoder/vp9_partition_models.h:627-630).
var vp9VarPartNNWeights64Layer1 = [8]float32{
	1.635945, 0.979557, 0.455315, 1.197199,
	-2.251024, -0.464953, 1.378676, -0.111927,
}

// vp9_var_part_nn_bias_64_layer1 (libvpx
// vp9/encoder/vp9_partition_models.h:632-634).
var vp9VarPartNNBias64Layer1 = [1]float32{
	-0.37972447,
}

// vp9_var_part_nn_weights_32_layer0 (libvpx
// vp9/encoder/vp9_partition_models.h:653-662). FEATURES=6, hidden=8.
var vp9VarPartNNWeights32Layer0 = [6 * 8]float32{
	0.067243, -0.083598, -2.191159, 2.726434, -3.324013, 3.477977,
	0.323736, -0.510199, 2.960693, 2.937661, 2.888476, 2.938315,
	-0.307602, -0.503353, -0.080725, -0.473909, -0.417162, 0.457089,
	0.665153, -0.273210, 0.028279, 0.972220, -0.445596, 1.756611,
	-0.177892, -0.091758, 0.436661, -0.521506, 0.133786, 0.266743,
	0.637367, -0.160084, -1.396269, 1.020841, -1.112971, 0.919496,
	-0.235883, 0.651954, 0.109061, -0.429463, 0.740839, -0.962060,
	0.299519, -0.386298, 1.550231, 2.464915, 1.311969, 2.561612,
}

// vp9_var_part_nn_bias_32_layer0 (libvpx
// vp9/encoder/vp9_partition_models.h:664-667).
var vp9VarPartNNBias32Layer0 = [8]float32{
	0.368242, 0.736617, 0.000000, 0.757287,
	0.000000, 0.613248, -0.776390, 0.928497,
}

// vp9_var_part_nn_weights_32_layer1 (libvpx
// vp9/encoder/vp9_partition_models.h:669-672).
var vp9VarPartNNWeights32Layer1 = [8]float32{
	0.939884, -2.420850, -0.410489, -0.186690,
	0.063287, -0.522011, 0.484527, -0.639625,
}

// vp9_var_part_nn_bias_32_layer1 (libvpx
// vp9/encoder/vp9_partition_models.h:674-676).
var vp9VarPartNNBias32Layer1 = [1]float32{
	-0.6455006,
}

// vp9_var_part_nn_weights_16_layer0 (libvpx
// vp9/encoder/vp9_partition_models.h:695-704). FEATURES=6, hidden=8.
var vp9VarPartNNWeights16Layer0 = [6 * 8]float32{
	0.742567, -0.580624, -0.244528, 0.331661, -0.113949, -0.559295,
	-0.386061, 0.438653, 1.467463, 0.211589, 0.513972, 1.067855,
	-0.876679, 0.088560, -0.687483, -0.380304, -0.016412, 0.146380,
	0.015318, 0.000351, -2.764887, 3.269717, 2.752428, -2.236754,
	0.561539, -0.852050, -0.084667, 0.202057, 0.197049, 0.364922,
	-0.463801, 0.431790, 1.872096, -0.091887, -0.055034, 2.443492,
	-0.156958, -0.189571, -0.542424, -0.589804, -0.354422, 0.401605,
	0.642021, -0.875117, 2.040794, 1.921070, 1.792413, 1.839727,
}

// vp9_var_part_nn_bias_16_layer0 (libvpx
// vp9/encoder/vp9_partition_models.h:706-709).
var vp9VarPartNNBias16Layer0 = [8]float32{
	2.901234, -1.940932, -0.198970, -0.406524,
	0.059422, -1.879207, -0.232340, 2.979821,
}

// vp9_var_part_nn_weights_16_layer1 (libvpx
// vp9/encoder/vp9_partition_models.h:711-714).
var vp9VarPartNNWeights16Layer1 = [8]float32{
	-0.528731, 0.375234, -0.088422, 0.668629,
	0.870449, 0.578735, 0.546103, -1.957207,
}

// vp9_var_part_nn_bias_16_layer1 (libvpx
// vp9/encoder/vp9_partition_models.h:716-718).
var vp9VarPartNNBias16Layer1 = [1]float32{
	-1.95769405,
}

// vp9VarPartNNConfig64 mirrors vp9_var_part_nnconfig_64 (libvpx
// vp9/encoder/vp9_partition_models.h:636-651). FEATURES=6.
var vp9VarPartNNConfig64 = func() *vp9NNConfig {
	c := &vp9NNConfig{
		NumInputs:       6,
		NumOutputs:      1,
		NumHiddenLayers: 1,
	}
	c.NumHiddenNodes[0] = 8
	c.Weights[0] = vp9VarPartNNWeights64Layer0[:]
	c.Weights[1] = vp9VarPartNNWeights64Layer1[:]
	c.Bias[0] = vp9VarPartNNBias64Layer0[:]
	c.Bias[1] = vp9VarPartNNBias64Layer1[:]
	return c
}()

// vp9VarPartNNConfig32 mirrors vp9_var_part_nnconfig_32 (libvpx
// vp9/encoder/vp9_partition_models.h:678-693). FEATURES=6.
var vp9VarPartNNConfig32 = func() *vp9NNConfig {
	c := &vp9NNConfig{
		NumInputs:       6,
		NumOutputs:      1,
		NumHiddenLayers: 1,
	}
	c.NumHiddenNodes[0] = 8
	c.Weights[0] = vp9VarPartNNWeights32Layer0[:]
	c.Weights[1] = vp9VarPartNNWeights32Layer1[:]
	c.Bias[0] = vp9VarPartNNBias32Layer0[:]
	c.Bias[1] = vp9VarPartNNBias32Layer1[:]
	return c
}()

// vp9VarPartNNConfig16 mirrors vp9_var_part_nnconfig_16 (libvpx
// vp9/encoder/vp9_partition_models.h:720-735). FEATURES=6.
var vp9VarPartNNConfig16 = func() *vp9NNConfig {
	c := &vp9NNConfig{
		NumInputs:       6,
		NumOutputs:      1,
		NumHiddenLayers: 1,
	}
	c.NumHiddenNodes[0] = 8
	c.Weights[0] = vp9VarPartNNWeights16Layer0[:]
	c.Weights[1] = vp9VarPartNNWeights16Layer1[:]
	c.Bias[0] = vp9VarPartNNBias16Layer0[:]
	c.Bias[1] = vp9VarPartNNBias16Layer1[:]
	return c
}()

// vp9NNPredict is the verbatim port of nn_predict
// (vp9/encoder/vp9_encodeframe.c:2994-3038). It runs a feed-forward NN with
// ReLU activations between hidden layers and a linear final layer.
//
// Layout note: nn_config.Weights[layer] is a flat float32 slice of
// (num_input_nodes_for_layer * num_output_nodes_for_layer) entries.
// nn_config.Bias[layer] is a flat slice of num_output_nodes_for_layer
// entries.
func vp9NNPredict(features []float32, nnConfig *vp9NNConfig, output []float32) {
	numInputNodes := nnConfig.NumInputs
	var buf [2][vp9NNMaxNodesPerLayer]float32
	bufIndex := 0
	inputNodes := features

	numLayers := nnConfig.NumHiddenLayers
	for layer := range numLayers {
		weights := nnConfig.Weights[layer]
		bias := nnConfig.Bias[layer]
		outputNodes := buf[bufIndex][:]
		numOutputNodes := nnConfig.NumHiddenNodes[layer]
		// libvpx asserts num_output_nodes < NN_MAX_NODES_PER_LAYER.
		_ = numOutputNodes // already bounded by NN_MAX_NODES_PER_LAYER above.
		wOff := 0
		for node := range numOutputNodes {
			var val float32 = 0.0
			for i := 0; i < numInputNodes; i++ {
				val += weights[wOff+i] * inputNodes[i]
			}
			val += bias[node]
			// ReLU activation.
			if val < 0.0 {
				val = 0.0
			}
			outputNodes[node] = val
			wOff += numInputNodes
		}
		numInputNodes = numOutputNodes
		inputNodes = outputNodes
		bufIndex = 1 - bufIndex
	}

	// Final output layer (linear).
	{
		weights := nnConfig.Weights[numLayers]
		bias := nnConfig.Bias[numLayers]
		wOff := 0
		for node := 0; node < nnConfig.NumOutputs; node++ {
			var val float32 = 0.0
			for i := 0; i < numInputNodes; i++ {
				val += weights[wOff+i] * inputNodes[i]
			}
			output[node] = val + bias[node]
			wOff += numInputNodes
		}
	}
}
