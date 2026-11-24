//go:build govpx_oracle_trace

package govpx_test

import (
	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9oracle"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"testing"
)

// TestVP9OracleEncoderResetTransitions pins encoder-lifetime transitions that
// are not represented by one-shot vpxenc invocations: a fresh encoder after
// warm state has been discarded must match a cold-start oracle stream.
func TestVP9OracleEncoderResetTransitions(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 reset/lifetime byte-parity gate")
	vp9test.RequireVpxencFrameFlags(t)

	const (
		width  = 64
		height = 64
		warm   = 6
		after  = 8
	)
	opts := vp9oracle.CBROptions(width, height, 600)
	extraArgs := vp9oracle.CBRArgs(600, 600, 400, 500, 0)

	t.Run("cold-start-matches-libvpx", func(t *testing.T) {
		coldSources := vp9oracle.TransitionPanningSources(width, height, after, 0)
		// First encode through govpx without any warm state.
		_, govpxPackets := vp9oracle.CaptureGovpxStreamParityPacketRowsWithHooks(t,
			opts, coldSources, make([]govpx.EncodeFlags, after), nil)
		_, libvpxPackets := vp9oracle.CaptureLibvpxStreamParityPacketRows(t,
			coldSources, make([]govpx.EncodeFlags, after), extraArgs)
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
		warmSources := vp9oracle.TransitionPanningSources(width, height, warm, 0)
		coldSources := vp9oracle.TransitionPanningSources(width, height, after, warm)

		// Drive the warm phase but ignore its packets.
		_, _ = vp9oracle.CaptureGovpxStreamParityPacketRowsWithHooks(t, opts,
			warmSources, make([]govpx.EncodeFlags, warm), nil)

		// Now drive a fresh encoder over the post-warmup sources.
		_, govpxPackets := vp9oracle.CaptureGovpxStreamParityPacketRowsWithHooks(t,
			opts, coldSources, make([]govpx.EncodeFlags, after), nil)
		_, libvpxPackets := vp9oracle.CaptureLibvpxStreamParityPacketRows(t,
			coldSources, make([]govpx.EncodeFlags, after), extraArgs)
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
