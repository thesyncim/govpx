//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle/coracletest"
	"github.com/thesyncim/govpx/internal/testutil"
)

// FuzzEncoderFrameFlags drives randomized per-frame [EncodeFlags] schedules
// through both govpx and the libvpx-side `vpxenc-frameflags` companion driver
// and asserts byte-exact stream parity at every emitted frame.
//
// The shape mirrors FuzzOracleEncoderRuntimeControlTransitions: the fuzz []byte
// derives a (width, height, cpu_used, frames) config plus a per-frame flag-set
// sequence drawn from a fixed pool, and govpx writes failing inputs to
// testdata/fuzz/FuzzEncoderFrameFlags for replay as plain regression tests
// under ordinary go test runs.
//
// Note: under 16-way parallel fuzz workers this harness has been observed to
// report intermittent parity divergences that do not reproduce in
// single-worker regression mode (the minimized seed always replays as PASS
// when fed back through go test -run=). That intermittent gap is not yet
// understood and the corresponding minimized seeds are intentionally NOT
// committed under testdata/fuzz/FuzzEncoderFrameFlags so the regression gate
// stays deterministic. When a truly reproducible divergence is found, follow
// the existing TestOracleEncoderStreamByteParityFrameFlags pattern (pin it
// with a `limit:` case in oracle_encoder_stream_parity_frame_flags_test.go)
// rather than relying on a fuzz seed alone.
func FuzzEncoderFrameFlags(f *testing.F) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		f.Skip("set GOVPX_WITH_ORACLE=1 to run frame-flags fuzz parity")
	}

	seeds := [][]byte{
		// Force-KF on frame 1 (smallest scenario that produces both a real key
		// and a real inter packet).
		{0, 0, 0, 0, 0, 1},
		// Hidden frame on frame 1, then force-KF on frame 3.
		{0, 0, 0, 0, 0, 8, 0, 1},
		// Run of NO_UPD_LAST inter frames — exercises the libvpx upd-mask path.
		{0, 0, 0, 0, 0, 0, 4, 4, 4, 4, 4, 4, 4},
		// Mixed: ForceGF, ForceARF, NoUpdateEntropy.
		{0, 0, 0, 0, 0, 0, 7, 0, 6, 0, 5},
		// All-byte saturation seed; r.pick cycles through the flag pool.
		{0xff, 0xaa, 0x55, 0x33, 0x11, 0x77, 0x88, 0x99, 0x44, 0x22, 0x10},
		// Long zero seed (all default flags) — ensures the parity infrastructure
		// still trips a recordable failure if the no-op driver call regresses.
		{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		driver := coracletest.VpxencFrameFlags(t)
		tc := encoderFrameFlagsFuzzCaseFromBytes(data)
		sum := sha256.Sum256(data)
		label := "fuzz-frame-flags-" + tc.name + "-" + hex.EncodeToString(sum[:4])
		t.Logf("%s w=%d h=%d cpu=%d frames=%d flags=%s", label, tc.opts.Width, tc.opts.Height, tc.opts.CpuUsed, len(tc.sources), encoderFrameFlagsLogString(tc.flags))

		govpxFrames := encodeFramesWithGovpxFrameFlags(t, tc.opts, tc.sources, tc.flags)
		libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, label, tc.opts, tc.targetKbps, tc.sources, tc.flags, nil)
		assertSegmentByteParity(t, label, govpxFrames, libvpxFrames, 0)
	})
}

type encoderFrameFlagsFuzzCase struct {
	name       string
	opts       EncoderOptions
	targetKbps int
	sources    []Image
	flags      []EncodeFlags
}

// encoderFrameFlagsFuzzFlagPool is the closed set of per-frame EncodeFlags
// values the fuzz body can pick from. EncodeInvisibleFrame is excluded
// because the frame-flags driver routes it through a separate
// `--invisible-frames` argv (see encodeFramesWithFrameFlagsDriver) which we
// keep off the fuzz menu to keep the driver invocation deterministic with
// the per-frame `--frame-flags` schedule.
var encoderFrameFlagsFuzzFlagPool = []EncodeFlags{
	0,
	EncodeForceKeyFrame,
	EncodeNoUpdateEntropy,
	EncodeNoUpdateLast,
	EncodeNoUpdateGolden,
	EncodeNoUpdateAltRef,
	EncodeForceGoldenFrame,
	EncodeForceAltRefFrame,
	EncodeNoUpdateLast | EncodeNoUpdateGolden | EncodeNoUpdateAltRef,
	EncodeNoUpdateLast | EncodeNoUpdateEntropy,
	EncodeNoReferenceGolden | EncodeNoReferenceAltRef,
}

func encoderFrameFlagsFuzzCaseFromBytes(data []byte) encoderFrameFlagsFuzzCase {
	r := testutil.NewByteCursor(data)

	dims := [...]struct {
		w int
		h int
	}{
		{16, 16},
		{32, 16},
		{32, 32},
	}
	speeds := [...]int{0, -3, -8}
	targets := [...]int{300, 700, 1200}

	dim := dims[r.Pick(len(dims))]
	cpuUsed := speeds[r.Pick(len(speeds))]
	targetKbps := targets[r.Pick(len(targets))]
	frames := 4 + r.Pick(5) // 4..8 frames

	// Threads is fixed at the single-threaded default. Threads>=2 on the VP8
	// encoder produces non-deterministic byte output (goroutine scheduling
	// affects token-partition packing), so it cannot participate in a
	// byte-parity fuzz harness. A separate threaded oracle parity test
	// (TestOracleEncoderStreamByteParity with `threads:2`) drives the threaded
	// path with fixed inputs.
	opts := oracleRuntimeBaseFuzzOptions(dim.w, dim.h, targetKbps, cpuUsed)
	opts.TargetBitrateKbps = targetKbps
	sources := oracleRuntimeFuzzSources(dim.w, dim.h, frames, r.Pick(2))

	flags := make([]EncodeFlags, frames)
	// Frame 0 is always the initial keyframe; libvpx forces it regardless of
	// flags. Leave flags[0]=0 to match the existing parity harness and avoid
	// duplicate force-kf bits.
	for i := 1; i < frames; i++ {
		flags[i] = encoderFrameFlagsFuzzFlagPool[r.Pick(len(encoderFrameFlagsFuzzFlagPool))]
	}

	return encoderFrameFlagsFuzzCase{
		name:       "general",
		opts:       opts,
		targetKbps: targetKbps,
		sources:    sources,
		flags:      flags,
	}
}

func encoderFrameFlagsLogString(flags []EncodeFlags) string {
	parts := make([]string, len(flags))
	for i, f := range flags {
		parts[i] = strconv.FormatUint(uint64(f), 16)
	}
	return strings.Join(parts, ",")
}
