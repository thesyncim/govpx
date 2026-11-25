//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"fmt"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"testing"
)

func assertVP9EncoderVpxdecI420Match(t *testing.T, width, height int, packets ...[]byte) {
	t.Helper()
	ivf := vp9IVFForTest(width, height, packets...)
	want := vp9test.VpxdecI420(t, ivf)
	got := vp9DecodeVisibleI420ForTest(t, packets...)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for encoder stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

// vp9OracleCyclicRefreshCBROptions is the libvpx-shaped realtime speed-8
// CBR + CYCLIC_REFRESH_AQ profile exercised by cyclic-refresh parity tests.
func vp9OracleCyclicRefreshCBROptions(width, height, targetKbps int) VP9EncoderOptions {
	opts := vp9OracleCBROptions(width, height, targetKbps)
	opts.AQMode = VP9AQCyclicRefresh
	opts.Deadline = DeadlineRealtime
	opts.CpuUsed = -8
	return opts
}

func vp9OracleCyclicRefreshCBRArgs(targetKbps, bufSizeMs, bufInitialMs, bufOptimalMs, dropFrame int) []string {
	return append(vp9OracleCBRArgs(targetKbps, bufSizeMs, bufInitialMs, bufOptimalMs, dropFrame),
		"--cpu-used=8",
		"--aq-mode=3",
	)
}

// vp9OracleCyclicRefreshCBRVpxencArgs is the vpxenc-vp9 CLI profile for
// keyframe byte-parity tests. It omits --exact-fps-timebase, which only the
// frameflags driver accepts.
func vp9OracleCyclicRefreshCBRVpxencArgs(targetKbps, bufSizeMs, bufInitialMs, bufOptimalMs, dropFrame int) []string {
	return []string{
		"--end-usage=cbr",
		fmt.Sprintf("--target-bitrate=%d", targetKbps),
		fmt.Sprintf("--buf-sz=%d", bufSizeMs),
		fmt.Sprintf("--buf-initial-sz=%d", bufInitialMs),
		fmt.Sprintf("--buf-optimal-sz=%d", bufOptimalMs),
		fmt.Sprintf("--drop-frame=%d", dropFrame),
		"--cpu-used=8",
		"--aq-mode=3",
	}
}
