//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"fmt"
	"os"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle/coracletest"
)

// TestVP8SVCMatchedPrefixFloorsD59809a7 pins the matched-prefix floor
// for the FuzzVP8MultiResSVCByteParity regression seed
//
//	testdata/fuzz/FuzzVP8MultiResSVCByteParity/regression_svc2tl_mode1_cpu0_t2_er1_d59809a7
//
// (decoded from the seed bytes "1007!c") at the matched-prefix state observed
// after the KF picker zbin_extra refresh under segmentation_enabled was
// ported. Prior to that closure the dispatcher's generic known-gap floor
// (matched-prefix >= 1) was the only assertion for this cohort — the seed
// achieved matched-prefix=1 on both output layers (every layer keyframe-matched
// libvpx, every later frame diverged).
//
// The matched-prefix measurement confirms the seed now consistently achieves:
//
//	layer-0 (base): matched-prefix=3 (was 1)
//	layer-1 (enh):  matched-prefix=6 (was 1)
//
// across the full 6-frame run (frames 6 in the seed's frame-count knob).
// The improvement traces back to the KF picker zbin_extra refresh
// (vp8/encoder/encodeframe.c:427-438 segmentation_enabled gate honored by
// govpx) closing the KF reconstruction byte-divergence at
// 64×64 — once the keyframe matches libvpx the post-KF inter MVs and
// modes stay aligned for a longer prefix before activity-mask drift
// re-flips a per-MB decision and breaks the byte chain.
//
// The fuzz dispatcher keeps its generic floor=1 gate so newly-discovered
// cases still surface; this test pins the matched-prefix state for the one
// regression seed in the corpus so any regression past the d59809a7
// improvement is caught here.
//
// References:
//   - libvpx v1.16.0 vp8/encoder/encodeframe.c:427-438 — segmentation_enabled
//     vp8cx_mb_init_quantizer call honored by govpx.
//   - testdata/fuzz/FuzzVP8MultiResSVCByteParity/regression_svc2tl_mode1_cpu0_t2_er1_d59809a7 —
//     the corpus seed this test re-runs.
//   - vp8_multires_svc_fuzz_test.go (runVP8TemporalSVCFuzzCase) — the
//     dispatcher whose floor=1 stays in place for generic discovery.
func TestVP8SVCMatchedPrefixFloorsD59809a7(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run the SVC matched-prefix pin")
	}
	// Decode seed "1007!c" (the d59809a7 corpus entry) exactly the way the
	// fuzz dispatcher does, so any future seed-decoder change is caught
	// against the canonical case shape this test pins.
	const seedString = "1007!c"
	c := vp8SVCFuzzCaseFromBytes([]byte(seedString))
	if c.shape != 1 {
		t.Fatalf("seed %q decoded to unexpected shape: got=%d want=1", seedString, c.shape)
	}
	if c.mode != 0 {
		t.Fatalf("seed %q decoded to unexpected mode: got=%d want=0", seedString, c.mode)
	}
	if c.cpuUsed != 0 {
		t.Fatalf("seed %q decoded to unexpected cpuUsed: got=%d want=0", seedString, c.cpuUsed)
	}
	if c.threads != 2 {
		t.Fatalf("seed %q decoded to unexpected threads: got=%d want=2", seedString, c.threads)
	}
	if !c.errorResilient {
		t.Fatalf("seed %q decoded to unexpected errorResilient: got=%v want=true", seedString, c.errorResilient)
	}

	// Drive the same harness the fuzz dispatcher runs.
	svcEncoder := coracletest.VpxTemporalSVCEncoder(t)
	const (
		w   = 64
		h   = 64
		fps = 30
	)
	fx := struct {
		name   string
		w, h   int
		source func(w, h, i int) Image
	}{name: "vp8-svc", w: w, h: h, source: encoderValidationPanningFrame}

	sources := make([]Image, c.frames)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(w, h, i)
	}
	bitrates := [5]int{420, 700, 0, 0, 0}
	const numLayers = 2

	speed := -c.cpuUsed
	if speed < 0 {
		speed = 0
	}
	govStreams := encodeFramesWithGovpxTemporalSVC(t, fx, TemporalLayeringTwoLayers, numLayers, bitrates, speed, c.errorResilient, c.threads, sources)
	libStreams := encodeFramesWithLibvpxTemporalSVC(t, svcEncoder, fx, 1, numLayers, bitrates, speed, c.errorResilient, c.threads, sources)
	if len(govStreams) != numLayers || len(libStreams) != numLayers {
		t.Fatalf("layer count drift: gov=%d lib=%d want=%d", len(govStreams), len(libStreams), numLayers)
	}

	// Pinned matched-prefix floors.
	wantPrefix := []int{3, 6}
	for layer := 0; layer < numLayers; layer++ {
		got, lib := govStreams[layer], libStreams[layer]
		common := len(got)
		if len(lib) < common {
			common = len(lib)
		}
		matched := 0
		for i := 0; i < common; i++ {
			if sha256.Sum256(got[i]) == sha256.Sum256(lib[i]) {
				matched++
			} else {
				break
			}
		}
		label := fmt.Sprintf("d59809a7/layer-%d", layer)
		t.Logf("%s matched-prefix matched=%d floor=%d (gov_frames=%d lib_frames=%d)", label, matched, wantPrefix[layer], len(got), len(lib))
		if matched < wantPrefix[layer] {
			t.Errorf("%s matched-prefix=%d below matched-prefix floor=%d", label, matched, wantPrefix[layer])
		}
	}
}
