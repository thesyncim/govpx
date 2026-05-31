package govpx

import (
	"fmt"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func TestVP9RealtimeSpeed5KeyframeHybridNonRDDispatch(t *testing.T) {
	for _, cpuUsed := range []int8{5, 7} {
		t.Run(fmt.Sprintf("cpu%d", cpuUsed), func(t *testing.T) {
			e, err := NewVP9Encoder(VP9EncoderOptions{
				Width:    320,
				Height:   240,
				Deadline: DeadlineRealtime,
				CpuUsed:  cpuUsed,
			})
			if err != nil {
				t.Fatalf("NewVP9Encoder: %v", err)
			}
			defer e.Close()

			ctx := e.vp9DefaultSpeedFrameContext()
			ctx.frameType = common.KeyFrame
			ctx.intraOnly = false
			ctx.showFrame = true

			var sf SpeedFeatures
			speed := e.vp9SpeedFeatureCPUUsed()
			vp9SetSpeedFeaturesFramesizeIndependent(e, &sf, speed, ctx)
			vp9SetSpeedFeaturesFramesizeDependent(e, &sf, speed, ctx)

			if sf.UseNonrdPickMode != 1 {
				t.Fatalf("UseNonrdPickMode = %d, want 1 (libvpx vp9_speed_features.c:606)", sf.UseNonrdPickMode)
			}
			if sf.NonrdKeyframe != 0 {
				t.Fatalf("NonrdKeyframe = %d, want 0 before speed 8 (libvpx vp9_speed_features.c:751)", sf.NonrdKeyframe)
			}
			if sf.TxSizeSearchMethod != UseLargestAll {
				t.Fatalf("TxSizeSearchMethod = %d, want USE_LARGESTALL on keyframes (libvpx vp9_speed_features.c:622)", sf.TxSizeSearchMethod)
			}
			for tx := range common.TxSizes {
				if sf.IntraUvModeMask[tx] != sfIntraDC {
					t.Fatalf("IntraUvModeMask[%d] = %#x, want %#x (libvpx vp9_speed_features.c:565)",
						tx, sf.IntraUvModeMask[tx], sfIntraDC)
				}
			}
		})
	}
}

func TestVP9KeyframeHybridNonRDIntraModeGate(t *testing.T) {
	var e VP9Encoder
	e.sf.UseNonrdPickMode = 1
	e.sf.NonrdKeyframe = 0

	if e.useVP9KeyframeNonRDIntraMode(common.Block8x8) {
		t.Fatalf("Block8x8 used non-RD; want RD arm when nonrd_keyframe=0")
	}
	if !e.useVP9KeyframeNonRDIntraMode(common.Block16x16) {
		t.Fatalf("Block16x16 used RD; want non-RD arm when use_nonrd_pick_mode=1")
	}

	e.sf.NonrdKeyframe = 1
	if !e.useVP9KeyframeNonRDIntraMode(common.Block8x8) {
		t.Fatalf("Block8x8 used RD; want non-RD arm when nonrd_keyframe=1")
	}

	e.sf.UseNonrdPickMode = 0
	if e.useVP9KeyframeNonRDIntraMode(common.Block64x64) {
		t.Fatalf("Block64x64 used non-RD without use_nonrd_pick_mode")
	}
}

func TestVP9KeyframeVariancePartitionRequiresNonRDRow(t *testing.T) {
	key := &vp9KeyframeEncodeState{
		hdr: &vp9dec.UncompressedHeader{FrameType: common.KeyFrame},
		dq:  &vp9dec.DequantTables{},
	}
	var e VP9Encoder
	e.rc.enabled = true
	e.sf.PartitionSearchType = VarBasedPartition

	if e.vp9KeyframeVariancePartitionEnabled(key) {
		t.Fatalf("keyframe variance partition enabled without use_nonrd_pick_mode")
	}

	e.sf.UseNonrdPickMode = 1
	if !e.vp9KeyframeVariancePartitionEnabled(key) {
		t.Fatalf("keyframe variance partition disabled on non-RD var-based row")
	}

	key.lossless = true
	if e.vp9KeyframeVariancePartitionEnabled(key) {
		t.Fatalf("lossless keyframe enabled variance partition")
	}
}

func TestVP9KeyframeRDPartitionRequiresVarBasedRDRow(t *testing.T) {
	key := &vp9KeyframeEncodeState{
		hdr: &vp9dec.UncompressedHeader{FrameType: common.KeyFrame},
		dq:  &vp9dec.DequantTables{},
	}
	var e VP9Encoder
	e.opts = VP9EncoderOptions{
		Width:              128,
		Height:             64,
		RateControlModeSet: true,
		RateControlMode:    RateControlQ,
		TargetBitrateKbps:  700,
	}
	e.sf.PartitionSearchType = VarBasedPartition

	if !e.vp9KeyframeRDPartitionEnabled(key) {
		t.Fatalf("keyframe RD partition disabled on fixed-Q var-based RD row")
	}

	e.sf.UseNonrdPickMode = 1
	if e.vp9KeyframeRDPartitionEnabled(key) {
		t.Fatalf("keyframe RD partition enabled on non-RD row")
	}

	e.sf.UseNonrdPickMode = 0
	e.sf.PartitionSearchType = SearchPartition
	if e.vp9KeyframeRDPartitionEnabled(key) {
		t.Fatalf("keyframe RD partition enabled for generic search partition")
	}

	key.lossless = true
	e.sf.PartitionSearchType = VarBasedPartition
	if e.vp9KeyframeRDPartitionEnabled(key) {
		t.Fatalf("lossless keyframe enabled RD partition")
	}
}

func TestVP9KeyframeRDPartitionBreakoutThresholds(t *testing.T) {
	var e VP9Encoder
	e.sf.PartitionSearchBreakoutThr.Dist = 1 << 19
	e.sf.PartitionSearchBreakoutThr.Rate = 80

	dist, rate := e.vp9KeyframeRDPartitionBreakoutThresholds(common.Block8x8)
	if dist != 1<<13 {
		t.Fatalf("Block8x8 dist threshold = %d, want %d", dist, 1<<13)
	}
	if rate != 80*int(common.NumPelsLog2Lookup[common.Block8x8]) {
		t.Fatalf("Block8x8 rate threshold = %d, want %d",
			rate, 80*int(common.NumPelsLog2Lookup[common.Block8x8]))
	}

	dist, rate = e.vp9KeyframeRDPartitionBreakoutThresholds(common.Block64x64)
	if dist != 1<<19 {
		t.Fatalf("Block64x64 dist threshold = %d, want %d", dist, 1<<19)
	}
	if rate != 80*int(common.NumPelsLog2Lookup[common.Block64x64]) {
		t.Fatalf("Block64x64 rate threshold = %d, want %d",
			rate, 80*int(common.NumPelsLog2Lookup[common.Block64x64]))
	}
}

func TestVP9KeyframeRDRectAllowedAfterSplitMiss(t *testing.T) {
	var e VP9Encoder

	if !e.vp9KeyframeRDRectAllowedAfterSplitMiss(common.Block8x8, true, true) {
		t.Fatalf("Block8x8 split miss suppressed sub-8 rectangular candidates")
	}
	if e.vp9KeyframeRDRectAllowedAfterSplitMiss(common.Block16x16, true, true) {
		t.Fatalf("Block16x16 split miss kept rectangular candidates with NONE allowed")
	}
	if !e.vp9KeyframeRDRectAllowedAfterSplitMiss(common.Block16x16, false, true) {
		t.Fatalf("edge split miss suppressed rectangular candidates")
	}
	if e.vp9KeyframeRDRectAllowedAfterSplitMiss(common.Block8x8, true, false) {
		t.Fatalf("disabled rectangular search was re-enabled")
	}
}
