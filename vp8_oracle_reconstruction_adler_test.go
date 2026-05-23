//go:build govpx_oracle_trace

package govpx

import (
	"math"
	"os"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
)

// TestVP8OracleReconstructionAdler32Match locks in the byte-identity reconstruction
// win by comparing per-frame y/u/v Adler32, q_index, and size_bytes against the
// libvpx oracle on a small panning fixture.
func TestVP8OracleReconstructionAdler32Match(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder oracle reconstruction comparison")
	}
	vpxencOracle := vp8test.VpxencOracle(t)

	const (
		width      = 64
		height     = 64
		fps        = 30
		targetKbps = 700
		frames     = 4
	)
	panningSources := make([]Image, frames)
	for i := range panningSources {
		panningSources[i] = encoderValidationPanningFrame(width, height, i)
	}
	splitMVSources := make([]Image, frames)
	for i := range splitMVSources {
		splitMVSources[i] = encoderValidationSplitMVQuadrantFrame(width, height, i)
	}
	baseOpts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               fps,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  999,
	}
	type fixture struct {
		name    string
		sources []Image
	}
	panning := fixture{name: "panning", sources: panningSources}
	splitmv := fixture{name: "splitmv-quadrant", sources: splitMVSources}
	cases := []struct {
		name     string
		deadline Deadline
		cpuUsed  int
		fixture  fixture
		// frameLimit pins how many leading frames must byte-match
		// (Adler32 + q_index + size_bytes). The trailing frames in
		// (frames - frameLimit) are exercised but excluded from all
		// per-frame assertions, mirroring the residual inter-frame drift
		// on later frames that lives outside the keyframe-recon scope of
		// this ratchet (see r9-4 capture).
		frameLimit int
	}{
		{name: "realtime-cbr-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fixture: panning, frameLimit: frames},
		{name: "realtime-cbr-cpu4", deadline: DeadlineRealtime, cpuUsed: 4, fixture: panning, frameLimit: frames},
		{name: "realtime-cbr-cpu8", deadline: DeadlineRealtime, cpuUsed: 8, fixture: panning, frameLimit: frames},
		{name: "good-quality-cbr-cpu5", deadline: DeadlineGoodQuality, cpuUsed: 5, fixture: panning, frameLimit: frames},
		// r9-4: the BestQuality keyframe RD picker now byte-matches libvpx
		// across all 4 frames of the SPLITMV-quadrant fixture (was: Y
		// reconstruction diverged starting at the keyframe due to the
		// per-block trellis optimizer running on B_PRED Y sub-blocks,
		// which libvpx never does in vp8_encode_intra4x4mby).
		{name: "best-quality-cbr-cpu0-splitmv", deadline: DeadlineBestQuality, cpuUsed: 0, fixture: splitmv, frameLimit: frames},
		// r9-4: GoodQuality cpu=0 byte-matches frames 0-1 of the SPLITMV
		// fixture; frames 2-3 still drift in the inter pipeline (separate
		// from the keyframe-picker trellis fix). Pin what we have.
		{name: "good-quality-cbr-cpu0-splitmv", deadline: DeadlineGoodQuality, cpuUsed: 0, fixture: splitmv, frameLimit: 2},
	}
	for _, cfg := range cases {
		t.Run(cfg.name, func(t *testing.T) {
			opts := baseOpts
			opts.Deadline = cfg.deadline
			opts.CpuUsed = cfg.cpuUsed
			govpxTrace := captureGovpxEncoderTrace(t, opts, cfg.fixture.sources)
			libvpxTrace := captureLibvpxEncoderTrace(t, vpxencOracle, "recon-adler-"+cfg.name, opts, targetKbps, cfg.fixture.sources, []string{"--end-usage=cbr"})

			gFrames, err := coracle.TraceFrameRows(govpxTrace)
			if err != nil {
				t.Fatalf("parse govpx trace frames: %v", err)
			}
			lFrames, err := coracle.TraceFrameRows(libvpxTrace)
			if err != nil {
				t.Fatalf("parse libvpx trace frames: %v", err)
			}
			if len(gFrames) != frames {
				t.Fatalf("[%s] govpx frame rows = %d, want %d", cfg.name, len(gFrames), frames)
			}
			if len(lFrames) != frames {
				t.Fatalf("[%s] libvpx frame rows = %d, want %d", cfg.name, len(lFrames), frames)
			}
			for i := 0; i < cfg.frameLimit; i++ {
				g := gFrames[i]
				l := lFrames[i]
				if g["y_adler32"] != l["y_adler32"] {
					t.Errorf("[%s] frame %d y_adler32 govpx=%v libvpx=%v", cfg.name, i, g["y_adler32"], l["y_adler32"])
				}
				if g["u_adler32"] != l["u_adler32"] {
					t.Errorf("[%s] frame %d u_adler32 govpx=%v libvpx=%v", cfg.name, i, g["u_adler32"], l["u_adler32"])
				}
				if g["v_adler32"] != l["v_adler32"] {
					t.Errorf("[%s] frame %d v_adler32 govpx=%v libvpx=%v", cfg.name, i, g["v_adler32"], l["v_adler32"])
				}
				if g["q_index"] != l["q_index"] {
					t.Errorf("[%s] frame %d q_index govpx=%v libvpx=%v", cfg.name, i, g["q_index"], l["q_index"])
				}
				gSize := coracle.TraceFloat(g["size_bytes"])
				lSize := coracle.TraceFloat(l["size_bytes"])
				if lSize <= 0 {
					t.Errorf("[%s] frame %d libvpx size_bytes = %v, want >0", cfg.name, i, l["size_bytes"])
					continue
				}
				if math.Abs((gSize-lSize)/lSize) > 0.01 {
					t.Errorf("[%s] frame %d size_bytes govpx=%v libvpx=%v exceeds ±1.0%%", cfg.name, i, gSize, lSize)
				}
			}
		})
	}
}
