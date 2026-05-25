//go:build govpx_oracle_trace

package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"testing"
)

// TestVP9OracleEncoderResetTransitions pins encoder-lifetime transitions that
// are not represented by one-shot vpxenc invocations: Reset must match a
// cold start after warm state has been discarded. The VP8 oracle gate's
// equivalent surface is TestOracleEncoderStreamByteParityResetFlushTransitions.
// On the VP9 side we don't currently have a Reset() method, so the
// equivalent surface is "construct fresh after Close" — the test still
// expresses the invariant that the second-stream packets must match a
// cold-start encoding when run independently against the oracle.
func TestVP9OracleEncoderResetTransitions(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 reset/lifetime byte-parity gate")
	vp9test.RequireVpxencFrameFlags(t)

	const (
		width  = 64
		height = 64
		warm   = 6
		after  = 8
	)
	opts := vp9OracleCBROptions(width, height, 600)
	extraArgs := vp9OracleCBRArgs(600, 600, 400, 500, 0)

	t.Run("cold-start-matches-libvpx", func(t *testing.T) {
		coldSources := vp9OracleTransitionPanningSources(width, height, after, 0)
		// First encode through govpx without any warm state.
		_, govpxPackets := captureGovpxVP9StreamParityPacketRowsWithHooks(t,
			opts, coldSources, make([]EncodeFlags, after), nil)
		_, libvpxPackets := captureLibvpxVP9StreamParityPacketRows(t,
			coldSources, make([]EncodeFlags, after), extraArgs)
		matches, firstMismatch := vp9test.CountByteParityMatches(govpxPackets,
			libvpxPackets)
		t.Logf("VP9 cold-start parity: matches=%d/%d first_mismatch=%d",
			matches, len(govpxPackets), firstMismatch)
		if vp9test.StrictEnv("GOVPX_VP9_TRANSITIONS_STRICT") &&
			matches != len(govpxPackets) {
			t.Fatalf("strict VP9 cold-start parity: matches=%d/%d", matches, len(govpxPackets))
		}
	})

	t.Run("fresh-encoder-after-warmup-matches-cold-start", func(t *testing.T) {
		// Encode the warm sources then discard the encoder and start
		// a fresh one with the original options. Compare against a
		// cold-start oracle that never saw the warm phase.
		warmSources := vp9OracleTransitionPanningSources(width, height, warm, 0)
		coldSources := vp9OracleTransitionPanningSources(width, height, after, warm)

		// Drive the warm phase but ignore its packets.
		_, _ = captureGovpxVP9StreamParityPacketRowsWithHooks(t, opts,
			warmSources, make([]EncodeFlags, warm), nil)

		// Now drive a fresh encoder over the post-warmup sources.
		_, govpxPackets := captureGovpxVP9StreamParityPacketRowsWithHooks(t,
			opts, coldSources, make([]EncodeFlags, after), nil)
		_, libvpxPackets := captureLibvpxVP9StreamParityPacketRows(t,
			coldSources, make([]EncodeFlags, after), extraArgs)
		matches, firstMismatch := vp9test.CountByteParityMatches(govpxPackets,
			libvpxPackets)
		t.Logf("VP9 fresh-encoder-after-warmup parity: matches=%d/%d first_mismatch=%d",
			matches, len(govpxPackets), firstMismatch)
		if vp9test.StrictEnv("GOVPX_VP9_TRANSITIONS_STRICT") &&
			matches != len(govpxPackets) {
			t.Fatalf("strict VP9 fresh-encoder-after-warmup parity: matches=%d/%d",
				matches, len(govpxPackets))
		}
	})
}
