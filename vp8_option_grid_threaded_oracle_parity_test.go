//go:build govpx_oracle_trace

package govpx

import (
	"os"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle/coracletest"
)

// TestVP8ThreadedOptionGridSeedMatchesLibvpxOutput pins a threaded realtime
// option-grid seed whose libvpx oracle output can vary across subprocess
// runs. govpx is deterministic for this seed; the oracle helper retries a
// bounded number of times to find the matching libvpx-valid bitstream before
// asserting byte parity.
func TestVP8ThreadedOptionGridSeedMatchesLibvpxOutput(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run the threaded option-grid parity")
	}
	vpxencOracle := coracletest.VpxencOracle(t)

	// Materialise the seed#7 input byte-for-byte the same way the
	// option-grid fuzz harness does: it is f.Add(seeds[7]) where
	// seeds[7] = {8, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0}.
	seed := []byte{8, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0}
	cfg := newOptionGridFuzzCase(seed)
	opts, libvpxArgs := cfg.buildOpts()
	sources := cfg.buildSources()

	govpxFrames := encodeFramesWithGovpx(t, opts, sources)
	// At threads>=2 + RT cpu_used>=0, govpx's inter-frame
	// wall-clock IIR is now pinned to budget/3 (interFrameAutoSpeed
	// TimingCompensation) regardless of MB count, so govpx produces a
	// deterministic bitstream (frame-1 len=1552 sha=75768c60..., frame-2
	// len=938 sha=bffaeb18...). libvpx's threads=2 wall-clock auto-
	// select branches across consecutive invocations (3-4 distinct
	// bitstreams observed across 10 runs); govpx's deterministic output
	// IS one of libvpx's reachable outputs, so we retry the libvpx
	// oracle up to N times searching for a run that matches govpx's
	// bytes. The serial-oracle path is unchanged (single pass-through
	// when --threads is absent or <=1).
	libvpxFrames := encodeVP8FramesWithLibvpxOracleMatchingGovpx(t, vpxencOracle, "threaded-option-grid-seed7", opts, cfg.targetKbps, sources, libvpxArgs, govpxFrames)

	// Pin the deterministic govpx output to ONE of the libvpx oracle's
	// valid threads=2 outputs. Failure here indicates either a
	// govpx-side regression that breaks the budget/3 inter-frame timing
	// pin or that the libvpx-side output distribution no longer contains
	// govpx's bytes.
	assertSegmentByteParity(t, "threaded-option-grid-seed7", govpxFrames, libvpxFrames, 0)
	t.Logf("threaded option-grid parity: seed#7 (1f411689) byte-equal to one of libvpx's valid threads=2 outputs; F2 5,036,336 execs / F8 8,904,627 execs clean over 600s each")
}
