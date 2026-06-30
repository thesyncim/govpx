package govpx

import (
	"math"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9enc "github.com/thesyncim/govpx/internal/vp9/encoder"
)

// vp9_ml_partition_parity_test.go pins the root-local ML_BASED_PARTITION
// helpers that have not moved into internal/vp9/encoder yet:
// vp9MLPredictVarPartitioning and vp9PredVariance. The model table and
// NNPredict pins live beside the internal encoder package that owns them.
//
// Reference-control oracle context:
//
//	9 of 10 RefControl parity-gap seeds diverge at byte 9 of the inter
//	frame (FirstPartitionSize literal). ML non-RD measurements pin
//	per-seed aggregate size_delta in the -179..+295 byte range.

// referenceNNPredictVarPart64 reproduces the libvpx nn_predict forward
// pass against vp9_var_part_nnconfig_64 using the same MAC ordering as
// libvpx (sequential left-to-right accumulation per output node, then
// add bias, then ReLU on hidden layers, linear final layer). The float32
// arithmetic is bit-exact with libvpx given identical inputs.
func referenceNNPredictVarPart64(features [6]float32) float32 {
	// Layer 0: 6 inputs -> 8 hidden, ReLU.
	w0 := vp9enc.VarPartNNWeights64Layer0
	b0 := vp9enc.VarPartNNBias64Layer0
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
	w1 := vp9enc.VarPartNNWeights64Layer1
	b1 := vp9enc.VarPartNNBias64Layer1
	var out float32
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

var vp9PredVarianceBenchmarkSink uint32

func BenchmarkVP9PredVariance64x64(b *testing.B) {
	const size = 64
	src := make([]uint8, size*size)
	pred := make([]uint8, size*size)
	for i := range src {
		src[i] = uint8((i*17 + i/size*11) & 0xff)
		pred[i] = uint8((i*5 + i/size*23 + 7) & 0xff)
	}
	b.Run("Optimized", func(b *testing.B) {
		var sum uint32
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			sum += vp9PredVariance(src, size, 0, 0, pred, size, 0, 0, size, size)
		}
		vp9PredVarianceBenchmarkSink = sum
	})
	b.Run("ScalarReference", func(b *testing.B) {
		var sum uint32
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			sum += refVariance(src, size, pred, size, size, size)
		}
		vp9PredVarianceBenchmarkSink = sum
	})
}
