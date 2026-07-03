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

func TestVP9InterPartitionDecisionCache(t *testing.T) {
	var e VP9Encoder
	e.ensureVP9InterPartitionDecisionCache(3, 4)

	e.storeVP9InterPartitionDecision(2, 3, common.Block32x32, common.Block16x16)
	if got, ok := e.lookupVP9InterPartitionDecision(2, 3, common.Block32x32); !ok {
		t.Fatalf("lookup miss")
	} else if got != common.Block16x16 {
		t.Fatalf("lookup = %v, want Block16x16", got)
	}
	if _, ok := e.lookupVP9InterPartitionDecision(2, 3, common.Block16x16); ok {
		t.Fatalf("lookup hit for wrong root")
	}

	e.ensureVP9InterPartitionDecisionCache(3, 4)
	if _, ok := e.lookupVP9InterPartitionDecision(2, 3, common.Block32x32); ok {
		t.Fatalf("lookup hit after frame invalidation")
	}
}

func TestVP9KeyframeDecisionRegionSnapshotRestoresOnlyRegion(t *testing.T) {
	var e VP9Encoder
	e.ensureVP9LeafKeyframeDecisionCache(4, 4)
	e.ensureVP9KeyframePartitionDecisionCache(4, 4)

	inside := vp9KeyframeModeDecision{
		mode:   common.HPred,
		txSize: common.Tx8x8,
		uvMode: common.TmPred,
	}
	outside := vp9KeyframeModeDecision{
		mode:   common.VPred,
		txSize: common.Tx4x4,
		uvMode: common.DcPred,
	}
	e.storeVP9LeafKeyframeDecision(1, 1, common.Block8x8, inside)
	e.storeVP9KeyframePartitionDecision(1, 1, common.Block16x16, common.Block8x8)
	e.storeVP9LeafKeyframeDecision(0, 0, common.Block8x8, outside)
	e.storeVP9KeyframePartitionDecision(0, 0, common.Block16x16, common.Block16x16)

	var snap vp9KeyframeDecisionRegionSnapshot
	if !e.snapshotVP9KeyframeDecisionRegion(4, 4, 1, 1, common.Block16x16, &snap) {
		t.Fatalf("snapshot failed")
	}

	mutatedInside := vp9KeyframeModeDecision{
		mode:   common.D45Pred,
		txSize: common.Tx16x16,
		uvMode: common.HPred,
	}
	mutatedOutside := vp9KeyframeModeDecision{
		mode:   common.D135Pred,
		txSize: common.Tx16x16,
		uvMode: common.VPred,
	}
	e.storeVP9LeafKeyframeDecision(1, 1, common.Block16x16, mutatedInside)
	e.storeVP9KeyframePartitionDecision(1, 1, common.Block16x16, common.Block4x4)
	e.storeVP9LeafKeyframeDecision(0, 0, common.Block16x16, mutatedOutside)
	e.storeVP9KeyframePartitionDecision(0, 0, common.Block16x16, common.Block4x4)

	e.restoreVP9KeyframeDecisionRegion(snap)

	if got, ok := e.lookupVP9LeafKeyframeDecision(1, 1, common.Block8x8); !ok {
		t.Fatalf("inside leaf lookup miss after restore")
	} else if got != inside {
		t.Fatalf("inside leaf = %+v, want %+v", got, inside)
	}
	if _, ok := e.lookupVP9LeafKeyframeDecision(1, 1, common.Block16x16); ok {
		t.Fatalf("inside mutated leaf survived restore")
	}
	if got, ok := e.lookupVP9KeyframePartitionDecision(1, 1, common.Block16x16); !ok {
		t.Fatalf("inside partition lookup miss after restore")
	} else if got != common.Block8x8 {
		t.Fatalf("inside partition = %v, want Block8x8", got)
	}

	if got, ok := e.lookupVP9LeafKeyframeDecision(0, 0, common.Block16x16); !ok {
		t.Fatalf("outside leaf lookup miss after restore")
	} else if got != mutatedOutside {
		t.Fatalf("outside leaf = %+v, want %+v", got, mutatedOutside)
	}
	if got, ok := e.lookupVP9KeyframePartitionDecision(0, 0, common.Block16x16); !ok {
		t.Fatalf("outside partition lookup miss after restore")
	} else if got != common.Block4x4 {
		t.Fatalf("outside partition = %v, want Block4x4", got)
	}
}

func TestVP9LeafDecisionTxSizeClamp(t *testing.T) {
	var e VP9Encoder
	e.ensureVP9LeafInterDecisionCache(2, 2)
	e.ensureVP9LeafKeyframeDecisionCache(2, 2)

	interDecision := vp9InterModeDecision{
		intra:  true,
		mode:   common.DcPred,
		txSize: common.Tx32x32,
	}
	e.storeVP9LeafInterDecision(0, 0, common.Block64x64, interDecision)
	keyDecision := vp9KeyframeModeDecision{
		mode:   common.HPred,
		txSize: common.Tx16x16,
		uvMode: common.DcPred,
	}
	e.storeVP9LeafKeyframeDecision(1, 1, common.Block16x16, keyDecision)

	e.clampVP9LeafDecisionTxSizes(common.Tx8x8)

	if got, ok := e.lookupVP9LeafInterDecision(0, 0, common.Block64x64); !ok {
		t.Fatalf("inter lookup miss after clamp")
	} else if got.txSize != common.Tx8x8 {
		t.Fatalf("inter tx size = %v, want %v", got.txSize, common.Tx8x8)
	}
	if got, ok := e.lookupVP9LeafKeyframeDecision(1, 1, common.Block16x16); !ok {
		t.Fatalf("keyframe lookup miss after clamp")
	} else if got.txSize != common.Tx8x8 {
		t.Fatalf("keyframe tx size = %v, want %v", got.txSize, common.Tx8x8)
	}
}

func TestVP9LeafInterDecisionCacheClearsTransientLumaPred(t *testing.T) {
	var e VP9Encoder
	e.ensureVP9LeafInterDecisionCache(2, 2)
	e.ensureVP9LeafInterRDDecisionCache(2, 2)

	decision := vp9InterModeDecision{
		mode:          common.NearestMv,
		refFrame:      vp9dec.LastFrame,
		refSlot:       0,
		interpFilter:  vp9dec.InterpEighttap,
		txSize:        common.Tx8x8,
		lumaPredReady: true,
	}
	e.storeVP9LeafInterDecision(0, 0, common.Block8x8, decision)
	if got, ok := e.lookupVP9LeafInterDecision(0, 0, common.Block8x8); !ok {
		t.Fatalf("inter lookup miss after store")
	} else if got.lumaPredReady {
		t.Fatalf("leaf cache preserved transient luma predictor readiness")
	}

	e.storeVP9LeafInterRDDecision(0, 1, common.Block8x8, decision)
	if got, ok := e.lookupVP9LeafInterRDDecision(0, 1, common.Block8x8); !ok {
		t.Fatalf("deep inter lookup miss after store")
	} else if got.lumaPredReady {
		t.Fatalf("deep leaf cache preserved transient luma predictor readiness")
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
