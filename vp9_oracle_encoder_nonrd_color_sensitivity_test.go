//go:build govpx_oracle_trace

package govpx_test

import (
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9oracle"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

// TestVP9EncoderNonrdMlPartitionColorSensitivityByteParity pins the
// frames-0..9 byte-exact prefix of the {0,0,0,0,0} long-fixture gap seed
// (CBR 300kbps kf=999 realtime cpu8 on the panning source) that the
// ML_BASED_PARTITION color_sensitivity fix opened.
//
// libvpx selects ML_BASED_PARTITION at cpu_used=8 for w*h <= 352*288
// (vp9_speed_features.c:762-763,825-826). That dispatch
// (vp9_encodeframe.c:5313-5321) runs get_estimated_pred + nonrd_pick_partition
// and never calls choose_partitioning, so x->color_sensitivity stays at the
// per-SB reset [0,0] (vp9_encodeframe.c:5245-5246) and x->variance_low stays
// all-zero (only touched inside choose_partitioning @ vp9_encodeframe.c:1336).
// govpx previously ran its choose_partitioning prepass (chroma_check @
// vp9_encodeframe.c:1165-1199) for every non-VAR_BASED path, spuriously
// flagging color_sensitivity[V] and adding a UV model-RD term
// (vp9_pickmode.c:2388-2402) to nonrd inter candidates — flipping inter blocks
// to intra at frame 4. This guards that regression. The seed itself stays
// deferred on its frame-10 golden-frame-refresh (rate-control) gap.
func TestVP9EncoderNonrdMlPartitionColorSensitivityByteParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height, frames = 64, 64, 10
	sources := vp9test.NewPanningSources(width, height, frames)
	opts := govpx.VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     govpx.RateControlCBR,
		TargetBitrateKbps:   300,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		MaxKeyframeInterval: 999,
		Deadline:            govpx.DeadlineRealtime,
		CpuUsed:             8,
	}
	args := []string{
		"--end-usage=cbr",
		"--target-bitrate=300",
		"--cpu-used=8",
		"--kf-min-dist=0",
		"--kf-max-dist=999",
		"--buf-sz=600",
		"--buf-initial-sz=400",
		"--buf-optimal-sz=500",
		"--drop-frame=0",
	}

	govpxFrames := vp9oracle.EncodeFramesWithGovpx(t, opts, sources, nil)
	libvpxFrames := vp9test.VpxencPackets(t, sources, args...)
	if len(govpxFrames) < frames || len(libvpxFrames) < frames {
		t.Fatalf("encoded frame counts govpx=%d libvpx=%d, want >= %d",
			len(govpxFrames), len(libvpxFrames), frames)
	}
	for i := 0; i < frames; i++ {
		g, l := govpxFrames[i], libvpxFrames[i]
		if len(g) != len(l) {
			t.Fatalf("frame %d length govpx=%d libvpx=%d", i, len(g), len(l))
		}
		for k := range g {
			if g[k] != l[k] {
				t.Fatalf("frame %d byte %d differs: govpx=%#x libvpx=%#x",
					i, k, g[k], l[k])
			}
		}
	}
}
