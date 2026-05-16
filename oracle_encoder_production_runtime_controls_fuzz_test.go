//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// FuzzOracleEncoderProductionRuntimeControls drives the same per-frame
// runtime-control schedule machinery as FuzzOracleEncoderRuntimeControlTransitions
// but at production resolutions (640×360, 854×480, 1280×720) with an explicit
// Threads axis (0/1/2/4). This closes G1 (no strict gate above ~160×96) and G2
// (multi-threaded encode parity is weak in the strict gate) under
// fuzz-generated control schedules. It lives in a separate fuzzer so its
// regression corpus and the smaller-resolution one stay independent, and so a
// future "long mode" can run only this one.
//
// Per-iteration cost is meaningfully higher than the small-resolution fuzzer,
// so the case generator caps frames at 2–4 on the heaviest resolutions and
// reuses the same per-frame action pool.
func FuzzOracleEncoderProductionRuntimeControls(f *testing.F) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		f.Skip("set GOVPX_WITH_ORACLE=1 to run production runtime-control fuzz parity")
	}
	seeds := [][]byte{
		// (resolution-bucket, threads-bucket, frames, cpu, kind, then actions)
		{0, 0, 0, 0, 0, 0, 1, 2, 3},
		{0, 1, 1, 0, 0, 7, 7, 5},
		{1, 1, 0, 1, 0, 5, 8, 6},
		{1, 2, 1, 0, 0, 0, 9, 13},
		{2, 1, 0, 0, 0, 11, 6, 1},
		{2, 2, 0, 1, 1, 0, 5, 8, 4},
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		driver := findVpxencFrameFlags(t)
		tc := oracleProductionRuntimeControlFuzzCaseFromBytes(data)
		sum := sha256.Sum256(data)
		label := "fuzz-prod-runtime-controls-" + tc.name + "-" + hex.EncodeToString(sum[:4])
		t.Logf("%s w=%d h=%d threads=%d frames=%d script=%s",
			label, tc.opts.Width, tc.opts.Height, tc.opts.Threads, len(tc.sources), strings.Join(tc.script, ","))

		govpxFrames := encodeFramesWithGovpxRuntimeControls(t, tc.opts, tc.sources, tc.flags, tc.apply)
		extraArgs := append([]string(nil), tc.extraArgs...)
		if tc.copyRefLog {
			extraArgs = append(extraArgs, "--copy-ref-log="+filepath.Join(t.TempDir(), "copy-reference.log"))
		}
		extraArgs = append(extraArgs, "--control-script="+strings.Join(tc.script, ","))
		libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, label, tc.opts, tc.targetKbps, tc.sources, tc.flags, extraArgs)
		// Strict byte parity at every production resolution. Seeds that
		// hit the documented runtime-control state-propagation gap
		// (gap D) fail here until the relevant fix lands. Failure logs
		// pinpoint the exact frame index and the byte delta size.
		assertSegmentByteParity(t, label, govpxFrames, libvpxFrames, 0)
	})
}

// oracleProductionRuntimeControlFuzzCaseFromBytes maps a fuzz seed onto a
// production-resolution runtime-control case. The first five bytes pick the
// scenario shape (resolution, threads, frames, cpu, source-kind); subsequent
// bytes feed the per-frame action selector through the shared
// oracleRuntimeRandomFuzzAction infrastructure.
func oracleProductionRuntimeControlFuzzCaseFromBytes(data []byte) oracleRuntimeControlFuzzCase {
	r := oracleRuntimeControlFuzzBytes{data: data}
	dims := [...]struct {
		w int
		h int
	}{
		{640, 360},
		{854, 480},
		{1280, 720},
	}
	threadPool := [...]int{0, 1, 2, 4}
	speeds := [...]int{0, -3, -8}
	targets := [...]int{300, 700, 1200}

	dim := dims[r.pick(len(dims))]
	threads := threadPool[r.pick(len(threadPool))]
	framesBucket := r.pick(3)
	frames := 2 + framesBucket
	if dim.w >= 1280 && frames > 3 {
		frames = 3
	}
	cpuUsed := speeds[r.pick(len(speeds))]
	kind := r.pick(2)
	targetKbps := targets[r.pick(len(targets))]

	opts := oracleRuntimeBaseFuzzOptions(dim.w, dim.h, targetKbps, cpuUsed)
	opts.Threads = threads
	sources := oracleRuntimeFuzzSources(dim.w, dim.h, frames, kind)
	flags := make([]EncodeFlags, frames)
	script := runtimeControlScript(frames, nil)
	apply := make(map[int]func(*testing.T, *VP8Encoder), frames)
	copyRefLog := false

	for frame := 1; frame < frames; frame++ {
		actionCount := 1 + r.pick(3)
		actions := make([]oracleRuntimeFuzzAction, 0, actionCount)
		haveConfig := false
		for range actionCount {
			action, flag, usesCopyRef := oracleRuntimeRandomFuzzAction(&r, targets[:])
			if flag != 0 {
				flags[frame] = flag
				continue
			}
			if action.token == "" {
				continue
			}
			if action.phase == oracleRuntimeFuzzConfigPhase {
				if haveConfig {
					continue
				}
				haveConfig = true
			}
			copyRefLog = copyRefLog || usesCopyRef
			actions = append(actions, action)
		}
		oracleRuntimeShuffleActions(&r, actions)
		oracleRuntimeInstallFuzzActions(script, apply, frame, actions)
	}

	return oracleRuntimeControlFuzzCase{
		name:       "prod-general",
		opts:       opts,
		targetKbps: targetKbps,
		sources:    sources,
		flags:      flags,
		script:     script,
		apply:      apply,
		copyRefLog: copyRefLog,
	}
}
