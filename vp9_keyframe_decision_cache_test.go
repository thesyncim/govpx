package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func TestVP9LeafKeyframeDecisionCache(t *testing.T) {
	var e VP9Encoder
	e.ensureVP9LeafKeyframeDecisionCache(2, 3)

	decision := vp9KeyframeModeDecision{
		mode: common.HPred,
		bmi: [4]vp9dec.Bmi{
			{AsMode: common.VPred},
			{AsMode: common.DcPred},
			{AsMode: common.TmPred},
			{AsMode: common.HPred},
		},
		txSize: common.Tx8x8,
		uvMode: common.TmPred,
	}
	e.storeVP9LeafKeyframeDecision(1, 2, common.Block8x8, decision)

	if got, ok := e.lookupVP9LeafKeyframeDecision(1, 2, common.Block8x8); !ok {
		t.Fatalf("lookup miss")
	} else if got != decision {
		t.Fatalf("lookup = %+v, want %+v", got, decision)
	}
	if _, ok := e.lookupVP9LeafKeyframeDecision(1, 2, common.Block16x16); ok {
		t.Fatalf("lookup hit for wrong block size")
	}

	e.ensureVP9LeafKeyframeDecisionCache(2, 3)
	if _, ok := e.lookupVP9LeafKeyframeDecision(1, 2, common.Block8x8); ok {
		t.Fatalf("lookup hit after frame invalidation")
	}
}

func TestVP9TileEncodeWorkerPreservesCountDecisionCaches(t *testing.T) {
	var src VP9Encoder
	vp9dec.SetupBlockPlanes(&src.planes, 1, 1)
	src.ensureVP9EncoderModeBuffers(2, 2)

	var worker VP9Encoder
	vp9dec.SetupBlockPlanes(&worker.planes, 1, 1)
	worker.vp9LeafInterDecisions = make([]vp9LeafInterDecisionEntry, 4)
	worker.vp9LeafInterDecisionsRows = 2
	worker.vp9LeafInterDecisionsCols = 2
	worker.vp9LeafInterDecisionsVer = src.vp9LeafInterDecisionsVer
	interDecision := vp9InterModeDecision{
		mode:         common.NearestMv,
		interpFilter: vp9dec.InterpEighttap,
		rate:         17,
		distortion:   23,
		score:        41,
	}
	worker.storeVP9LeafInterDecision(0, 1, common.Block8x8, interDecision)

	worker.vp9LeafKeyframeDecisions = make([]vp9LeafKeyframeDecisionEntry, 4)
	worker.vp9LeafKeyframeDecisionsRows = 2
	worker.vp9LeafKeyframeDecisionsCols = 2
	worker.vp9LeafKeyframeDecisionsVer = src.vp9LeafKeyframeDecisionsVer
	keyDecision := vp9KeyframeModeDecision{
		mode: common.HPred,
		bmi: [4]vp9dec.Bmi{
			{AsMode: common.VPred},
			{AsMode: common.DcPred},
			{AsMode: common.TmPred},
			{AsMode: common.HPred},
		},
		txSize: common.Tx8x8,
		uvMode: common.TmPred,
	}
	worker.storeVP9LeafKeyframeDecision(1, 0, common.Block8x8, keyDecision)

	worker.prepareVP9TileEncodeWorker(&src, 2, 2)

	if got, ok := worker.lookupVP9LeafInterDecision(0, 1, common.Block8x8); !ok {
		t.Fatalf("inter lookup miss after tile encode worker prep")
	} else if got != interDecision {
		t.Fatalf("inter lookup = %+v, want %+v", got, interDecision)
	}
	if got, ok := worker.lookupVP9LeafKeyframeDecision(1, 0, common.Block8x8); !ok {
		t.Fatalf("keyframe lookup miss after tile encode worker prep")
	} else if got != keyDecision {
		t.Fatalf("keyframe lookup = %+v, want %+v", got, keyDecision)
	}
}

func TestVP9FrameParallelWorkerUsesPrivateDecisionCaches(t *testing.T) {
	var src VP9Encoder
	vp9dec.SetupBlockPlanes(&src.planes, 1, 1)
	src.ensureVP9EncoderModeBuffers(2, 2)

	var worker VP9Encoder
	vp9dec.SetupBlockPlanes(&worker.planes, 1, 1)
	worker.vp9LeafInterDecisions = make([]vp9LeafInterDecisionEntry, 4)
	worker.vp9LeafKeyframeDecisions = make([]vp9LeafKeyframeDecisionEntry, 4)

	worker.prepareVP9FrameParallelWorker(&src, 2, 2, 16, 16)

	if len(worker.vp9LeafInterDecisions) != 4 {
		t.Fatalf("worker inter decision cache len = %d, want 4",
			len(worker.vp9LeafInterDecisions))
	}
	if len(worker.vp9LeafKeyframeDecisions) != 4 {
		t.Fatalf("worker keyframe decision cache len = %d, want 4",
			len(worker.vp9LeafKeyframeDecisions))
	}
	if &worker.vp9LeafInterDecisions[0] == &src.vp9LeafInterDecisions[0] {
		t.Fatalf("frame-parallel worker aliases parent inter decisions")
	}
	if &worker.vp9LeafKeyframeDecisions[0] == &src.vp9LeafKeyframeDecisions[0] {
		t.Fatalf("frame-parallel worker aliases parent keyframe decisions")
	}
}
