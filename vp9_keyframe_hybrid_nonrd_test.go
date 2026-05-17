package govpx

import (
	"fmt"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
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
