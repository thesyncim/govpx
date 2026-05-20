//go:build govpx_oracle_trace

package govpx

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestOracleEncoderCopyReferenceFrameParity(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder reference-copy parity gate")
	}
	driver := findVpxencFrameFlags(t)

	t.Run("refreshed-references", func(t *testing.T) {
		opts := copyReferenceParityOptions(16, 16)
		sources := makePanningSources(opts.Width, opts.Height, 6, 0)
		flags := []EncodeFlags{
			0,
			0,
			EncodeForceGoldenFrame,
			0,
			EncodeForceAltRefFrame,
			0,
		}
		script := emptyCopyReferenceScript(len(sources))
		script[1] = "copyref:last+copyref:golden+copyref:altref"
		script[3] = "copyref:golden"
		script[5] = "copyref:altref"
		checks := map[int][]copyReferenceCheck{
			1: {
				{ref: ReferenceLast, name: "last"},
				{ref: ReferenceGolden, name: "golden"},
				{ref: ReferenceAltRef, name: "altref"},
			},
			3: {{ref: ReferenceGolden, name: "golden"}},
			5: {{ref: ReferenceAltRef, name: "altref"}},
		}

		want := captureLibvpxCopyReferenceChecksums(t, driver, "copyref-refresh", opts, sources, flags, script)
		got := captureGovpxCopyReferenceChecksums(t, opts, sources, flags, nil, checks)
		assertCopyReferenceChecksumsEqual(t, got, want)
	})

	t.Run("external-set-reference", func(t *testing.T) {
		opts := copyReferenceParityOptions(33, 17)
		sources := makePanningSources(opts.Width, opts.Height, 4, 0)
		script := emptyCopyReferenceScript(len(sources))
		script[1] = "setref:last:panning:8+copyref:last"
		script[2] = "setref:golden:panning:9+copyref:golden"
		script[3] = "setref:altref:panning:10+copyref:altref"
		sets := map[int][]copyReferenceSet{
			1: {{ref: ReferenceLast, name: "last", panningIndex: 8}},
			2: {{ref: ReferenceGolden, name: "golden", panningIndex: 9}},
			3: {{ref: ReferenceAltRef, name: "altref", panningIndex: 10}},
		}
		checks := map[int][]copyReferenceCheck{
			1: {{ref: ReferenceLast, name: "last"}},
			2: {{ref: ReferenceGolden, name: "golden"}},
			3: {{ref: ReferenceAltRef, name: "altref"}},
		}

		want := captureLibvpxCopyReferenceChecksums(t, driver, "copyref-setref", opts, sources, nil, script)
		got := captureGovpxCopyReferenceChecksums(t, opts, sources, nil, sets, checks)
		assertCopyReferenceChecksumsEqual(t, got, want)
	})

	t.Run("runtime-active-roi-copy-reference", func(t *testing.T) {
		opts := copyReferenceParityOptions(64, 64)
		sources := makePanningSources(opts.Width, opts.Height, 5, 0)
		script := emptyCopyReferenceScript(len(sources))
		script[1] = "active:checker+copyref:last"
		script[2] = "roi:border1+copyref:golden"
		script[4] = "active:off+roi:off+copyref:last+copyref:golden"
		checks := map[int][]copyReferenceCheck{
			1: {{ref: ReferenceLast, name: "last"}},
			2: {{ref: ReferenceGolden, name: "golden"}},
			4: {
				{ref: ReferenceLast, name: "last"},
				{ref: ReferenceGolden, name: "golden"},
			},
		}
		apply := map[int]func(*testing.T, *VP8Encoder){
			1: activeMapApply("checker"),
			2: roiMapApply("border1"),
			4: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
				mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
			},
		}

		want := captureLibvpxCopyReferenceChecksums(t, driver, "copyref-runtime-active-roi", opts, sources, nil, script)
		got := captureGovpxCopyReferenceChecksumsWithApply(t, opts, sources, nil, nil, apply, checks)
		assertCopyReferenceChecksumsEqual(t, got, want)
	})

	t.Run("temporal-copy-reference", func(t *testing.T) {
		opts := copyReferenceParityOptions(64, 64)
		opts.TemporalScalability = runtimeTemporalConfig(TemporalLayeringTwoLayers, opts.TargetBitrateKbps)
		sources := makePanningSources(opts.Width, opts.Height, 8, 0)
		flags := temporalTwoLayerFlags(len(sources))
		script := emptyCopyReferenceScript(len(sources))
		script[2] = "copyref:last+copyref:golden"
		script[5] = "copyref:last+copyref:altref"
		checks := map[int][]copyReferenceCheck{
			2: {
				{ref: ReferenceLast, name: "last"},
				{ref: ReferenceGolden, name: "golden"},
			},
			5: {
				{ref: ReferenceLast, name: "last"},
				{ref: ReferenceAltRef, name: "altref"},
			},
		}

		want := captureLibvpxCopyReferenceChecksumsWithExtraArgs(t, driver, "copyref-temporal", opts, sources, flags, script, runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, opts.TargetBitrateKbps))
		got := captureGovpxCopyReferenceChecksums(t, opts, sources, flags, nil, checks)
		assertCopyReferenceChecksumsEqual(t, got, want)
	})

	t.Run("denoiser-copy-reference", func(t *testing.T) {
		opts := copyReferenceParityOptions(64, 64)
		opts.NoiseSensitivity = 3
		sources := makePanningSources(opts.Width, opts.Height, 6, 0)
		script := emptyCopyReferenceScript(len(sources))
		script[1] = "copyref:last+copyref:golden"
		script[4] = "copyref:last+copyref:altref"
		checks := map[int][]copyReferenceCheck{
			1: {
				{ref: ReferenceLast, name: "last"},
				{ref: ReferenceGolden, name: "golden"},
			},
			4: {
				{ref: ReferenceLast, name: "last"},
				{ref: ReferenceAltRef, name: "altref"},
			},
		}

		want := captureLibvpxCopyReferenceChecksumsWithExtraArgs(t, driver, "copyref-denoiser", opts, sources, nil, script, []string{"--noise-sensitivity=3"})
		got := captureGovpxCopyReferenceChecksums(t, opts, sources, nil, nil, checks)
		assertCopyReferenceChecksumsEqual(t, got, want)
	})

	t.Run("resize-copy-reference", func(t *testing.T) {
		opts := copyReferenceParityOptions(64, 64)
		sources := make([]Image, 6)
		for i := range sources {
			if i < 2 {
				sources[i] = encoderValidationPanningFrame(64, 64, i)
			} else {
				sources[i] = encoderValidationPanningFrame(32, 32, i)
			}
		}
		script := emptyCopyReferenceScript(len(sources))
		script[2] = "resize:32x32"
		script[3] = "copyref:last+copyref:golden+copyref:altref"
		apply := map[int]func(*testing.T, *VP8Encoder){
			2: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetRealtimeTarget(32x32)", e.SetRealtimeTarget(RealtimeTarget{Width: 32, Height: 32}))
			},
		}
		checks := map[int][]copyReferenceCheck{
			3: {
				{ref: ReferenceLast, name: "last"},
				{ref: ReferenceGolden, name: "golden"},
				{ref: ReferenceAltRef, name: "altref"},
			},
		}

		want := captureLibvpxCopyReferenceChecksums(t, driver, "copyref-resize", opts, sources, nil, script)
		got := captureGovpxCopyReferenceChecksumsWithApply(t, opts, sources, nil, nil, apply, checks)
		assertCopyReferenceChecksumsEqual(t, got, want)
	})

	t.Run("error-resilient-token-copy-reference", func(t *testing.T) {
		opts := copyReferenceParityOptions(64, 64)
		opts.ErrorResilient = true
		opts.ErrorResilientPartitions = true
		opts.TokenPartitions = 2
		sources := makePanningSources(opts.Width, opts.Height, 8, 0)
		script := emptyCopyReferenceScript(len(sources))
		script[1] = "copyref:last+copyref:golden"
		script[4] = "copyref:last+copyref:altref"
		script[6] = "copyref:last+copyref:golden+copyref:altref"
		checks := map[int][]copyReferenceCheck{
			1: {
				{ref: ReferenceLast, name: "last"},
				{ref: ReferenceGolden, name: "golden"},
			},
			4: {
				{ref: ReferenceLast, name: "last"},
				{ref: ReferenceAltRef, name: "altref"},
			},
			6: {
				{ref: ReferenceLast, name: "last"},
				{ref: ReferenceGolden, name: "golden"},
				{ref: ReferenceAltRef, name: "altref"},
			},
		}

		want := captureLibvpxCopyReferenceChecksumsWithExtraArgs(t, driver, "copyref-er-token", opts, sources, nil, script, []string{
			"--error-resilient=3",
			"--token-parts=2",
		})
		got := captureGovpxCopyReferenceChecksums(t, opts, sources, nil, nil, checks)
		assertCopyReferenceChecksumsEqual(t, got, want)
	})

	t.Run("screen-static-copy-reference", func(t *testing.T) {
		opts := copyReferenceParityOptions(64, 64)
		sources := makePanningSources(opts.Width, opts.Height, 8, 0)
		script := emptyCopyReferenceScript(len(sources))
		script[1] = "screen:2+static:500+copyref:last+copyref:golden"
		script[5] = "screen:0+static:0+copyref:last+copyref:golden+copyref:altref"
		apply := map[int]func(*testing.T, *VP8Encoder){
			1: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetScreenContentMode(2)", e.SetScreenContentMode(2))
				mustRuntime(t, "SetStaticThreshold(500)", e.SetStaticThreshold(500))
			},
			5: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetScreenContentMode(0)", e.SetScreenContentMode(0))
				mustRuntime(t, "SetStaticThreshold(0)", e.SetStaticThreshold(0))
			},
		}
		checks := map[int][]copyReferenceCheck{
			1: {
				{ref: ReferenceLast, name: "last"},
				{ref: ReferenceGolden, name: "golden"},
			},
			5: {
				{ref: ReferenceLast, name: "last"},
				{ref: ReferenceGolden, name: "golden"},
				{ref: ReferenceAltRef, name: "altref"},
			},
		}

		want := captureLibvpxCopyReferenceChecksums(t, driver, "copyref-screen-static", opts, sources, nil, script)
		got := captureGovpxCopyReferenceChecksumsWithApply(t, opts, sources, nil, nil, apply, checks)
		assertCopyReferenceChecksumsEqual(t, got, want)
	})

	t.Run("rtc-external-copy-reference", func(t *testing.T) {
		opts := copyReferenceParityOptions(64, 64)
		sources := makePanningSources(opts.Width, opts.Height, 8, 0)
		script := emptyCopyReferenceScript(len(sources))
		script[2] = "rtc:1+copyref:last+copyref:golden"
		script[6] = "rtc:0+copyref:last+copyref:golden+copyref:altref"
		apply := map[int]func(*testing.T, *VP8Encoder){
			2: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetRTCExternalRateControl(true)", e.SetRTCExternalRateControl(true))
			},
			6: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetRTCExternalRateControl(false)", e.SetRTCExternalRateControl(false))
			},
		}
		checks := map[int][]copyReferenceCheck{
			2: {
				{ref: ReferenceLast, name: "last"},
				{ref: ReferenceGolden, name: "golden"},
			},
			6: {
				{ref: ReferenceLast, name: "last"},
				{ref: ReferenceGolden, name: "golden"},
				{ref: ReferenceAltRef, name: "altref"},
			},
		}

		want := captureLibvpxCopyReferenceChecksums(t, driver, "copyref-rtc-external", opts, sources, nil, script)
		got := captureGovpxCopyReferenceChecksumsWithApply(t, opts, sources, nil, nil, apply, checks)
		assertCopyReferenceChecksumsEqual(t, got, want)
	})

	t.Run("copy-reference-checks-do-not-change-bytestream", func(t *testing.T) {
		opts := copyReferenceParityOptions(32, 32)
		sources := makePanningSources(opts.Width, opts.Height, 6, 0)
		flags := []EncodeFlags{
			0,
			0,
			EncodeForceGoldenFrame,
			0,
			EncodeForceAltRefFrame,
			0,
		}
		script := emptyCopyReferenceScript(len(sources))
		script[1] = "copyref:last+copyref:golden+copyref:altref"
		script[3] = "copyref:golden"
		script[5] = "copyref:altref"
		apply := map[int]func(*testing.T, *VP8Encoder){
			1: copyReferenceCheckApply("frame1", ReferenceLast, ReferenceGolden, ReferenceAltRef),
			3: copyReferenceCheckApply("frame3", ReferenceGolden),
			5: copyReferenceCheckApply("frame5", ReferenceAltRef),
		}
		logPath := filepath.Join(t.TempDir(), "copyref-bytestream.log")

		want := encodeFramesWithFrameFlagsDriver(t, driver, "copyref-bytestream", opts, opts.TargetBitrateKbps, sources, flags, []string{
			"--control-script=" + strings.Join(script, ","),
			"--copy-ref-log=" + logPath,
		})
		got := encodeFramesWithGovpxRuntimeControls(t, opts, sources, flags, apply)
		assertSegmentByteParity(t, "copyref-bytestream", got, want, 0)
	})

	t.Run("copy-reference-checks-under-active-map-do-not-change-bytestream", func(t *testing.T) {
		opts := copyReferenceParityOptions(64, 64)
		sources := makePanningSources(opts.Width, opts.Height, 8, 0)
		script := emptyCopyReferenceScript(len(sources))
		script[1] = "active:checker+copyref:last+copyref:golden"
		script[4] = "copyref:last+copyref:altref"
		script[6] = "active:off+copyref:last+copyref:golden+copyref:altref"
		apply := map[int]func(*testing.T, *VP8Encoder){
			1: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				activeMapApply("checker")(t, e)
				copyReferenceCheckApply("frame1", ReferenceLast, ReferenceGolden)(t, e)
			},
			4: copyReferenceCheckApply("frame4", ReferenceLast, ReferenceAltRef),
			6: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
				copyReferenceCheckApply("frame6", ReferenceLast, ReferenceGolden, ReferenceAltRef)(t, e)
			},
		}
		logPath := filepath.Join(t.TempDir(), "copyref-active-map.log")

		want := encodeFramesWithFrameFlagsDriver(t, driver, "copyref-active-map-bytestream", opts, opts.TargetBitrateKbps, sources, nil, []string{
			"--control-script=" + strings.Join(script, ","),
			"--copy-ref-log=" + logPath,
		})
		got := encodeFramesWithGovpxRuntimeControls(t, opts, sources, nil, apply)
		assertSegmentByteParity(t, "copyref-active-map-bytestream", got, want, 0)
	})

	t.Run("copy-reference-checks-under-roi-map-do-not-change-bytestream", func(t *testing.T) {
		opts := copyReferenceParityOptions(64, 64)
		sources := makePanningSources(opts.Width, opts.Height, 8, 0)
		script := emptyCopyReferenceScript(len(sources))
		script[1] = "roi:border1+copyref:golden"
		script[4] = "copyref:last+copyref:golden"
		script[6] = "roi:off+copyref:last+copyref:golden+copyref:altref"
		apply := map[int]func(*testing.T, *VP8Encoder){
			1: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				roiMapApply("border1")(t, e)
				copyReferenceCheckApply("frame1", ReferenceGolden)(t, e)
			},
			4: copyReferenceCheckApply("frame4", ReferenceLast, ReferenceGolden),
			6: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				copyReferenceCheckApply("frame6", ReferenceLast, ReferenceGolden, ReferenceAltRef)(t, e)
			},
		}
		logPath := filepath.Join(t.TempDir(), "copyref-roi-map.log")

		want := encodeFramesWithFrameFlagsDriver(t, driver, "copyref-roi-map-bytestream", opts, opts.TargetBitrateKbps, sources, nil, []string{
			"--control-script=" + strings.Join(script, ","),
			"--copy-ref-log=" + logPath,
		})
		got := encodeFramesWithGovpxRuntimeControls(t, opts, sources, nil, apply)
		assertSegmentByteParity(t, "copyref-roi-map-bytestream", got, want, 0)
	})

	t.Run("copy-reference-checks-under-denoiser-do-not-change-bytestream", func(t *testing.T) {
		opts := copyReferenceParityOptions(64, 64)
		opts.NoiseSensitivity = 3
		sources := makePanningSources(opts.Width, opts.Height, 8, 0)
		script := emptyCopyReferenceScript(len(sources))
		script[1] = "copyref:last+copyref:golden"
		script[4] = "copyref:last+copyref:altref"
		script[6] = "copyref:last+copyref:golden+copyref:altref"
		apply := map[int]func(*testing.T, *VP8Encoder){
			1: copyReferenceCheckApply("frame1", ReferenceLast, ReferenceGolden),
			4: copyReferenceCheckApply("frame4", ReferenceLast, ReferenceAltRef),
			6: copyReferenceCheckApply("frame6", ReferenceLast, ReferenceGolden, ReferenceAltRef),
		}
		logPath := filepath.Join(t.TempDir(), "copyref-denoiser.log")

		want := encodeFramesWithFrameFlagsDriver(t, driver, "copyref-denoiser-bytestream", opts, opts.TargetBitrateKbps, sources, nil, []string{
			"--control-script=" + strings.Join(script, ","),
			"--copy-ref-log=" + logPath,
			"--noise-sensitivity=3",
		})
		got := encodeFramesWithGovpxRuntimeControls(t, opts, sources, nil, apply)
		assertSegmentByteParity(t, "copyref-denoiser-bytestream", got, want, 0)
	})

	t.Run("copy-reference-checks-under-temporal-do-not-change-bytestream", func(t *testing.T) {
		opts := copyReferenceParityOptions(64, 64)
		opts.TemporalScalability = runtimeTemporalConfig(TemporalLayeringTwoLayers, opts.TargetBitrateKbps)
		sources := makePanningSources(opts.Width, opts.Height, 8, 0)
		flags := temporalTwoLayerFlags(len(sources))
		script := emptyCopyReferenceScript(len(sources))
		script[2] = "copyref:last+copyref:golden"
		script[5] = "copyref:last+copyref:altref"
		apply := map[int]func(*testing.T, *VP8Encoder){
			2: copyReferenceCheckApply("frame2", ReferenceLast, ReferenceGolden),
			5: copyReferenceCheckApply("frame5", ReferenceLast, ReferenceAltRef),
		}
		logPath := filepath.Join(t.TempDir(), "copyref-temporal.log")

		extraArgs := append(runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, opts.TargetBitrateKbps),
			"--control-script="+strings.Join(script, ","),
			"--copy-ref-log="+logPath,
		)
		want := encodeFramesWithFrameFlagsDriver(t, driver, "copyref-temporal-bytestream", opts, opts.TargetBitrateKbps, sources, flags, extraArgs)
		got := encodeFramesWithGovpxRuntimeControls(t, opts, sources, flags, apply)
		assertSegmentByteParity(t, "copyref-temporal-bytestream", got, want, 0)
	})

	t.Run("copy-reference-checks-under-error-resilient-token-partitions-do-not-change-bytestream", func(t *testing.T) {
		opts := copyReferenceParityOptions(64, 64)
		opts.ErrorResilient = true
		opts.ErrorResilientPartitions = true
		opts.TokenPartitions = 2
		sources := makePanningSources(opts.Width, opts.Height, 8, 0)
		script := emptyCopyReferenceScript(len(sources))
		script[1] = "copyref:last+copyref:golden"
		script[4] = "copyref:last+copyref:altref"
		script[6] = "copyref:last+copyref:golden+copyref:altref"
		apply := map[int]func(*testing.T, *VP8Encoder){
			1: copyReferenceCheckApply("frame1", ReferenceLast, ReferenceGolden),
			4: copyReferenceCheckApply("frame4", ReferenceLast, ReferenceAltRef),
			6: copyReferenceCheckApply("frame6", ReferenceLast, ReferenceGolden, ReferenceAltRef),
		}
		logPath := filepath.Join(t.TempDir(), "copyref-er-token.log")

		want := encodeFramesWithFrameFlagsDriver(t, driver, "copyref-er-token-bytestream", opts, opts.TargetBitrateKbps, sources, nil, []string{
			"--error-resilient=3",
			"--control-script=" + strings.Join(script, ","),
			"--copy-ref-log=" + logPath,
		})
		got := encodeFramesWithGovpxRuntimeControls(t, opts, sources, nil, apply)
		assertSegmentByteParity(t, "copyref-er-token-bytestream", got, want, 0)
	})

	t.Run("copy-reference-checks-after-resize-do-not-change-bytestream", func(t *testing.T) {
		opts := copyReferenceParityOptions(64, 64)
		sources := make([]Image, 8)
		for i := range sources {
			if i < 3 {
				sources[i] = encoderValidationPanningFrame(64, 64, i)
			} else {
				sources[i] = encoderValidationPanningFrame(32, 32, i)
			}
		}
		script := emptyCopyReferenceScript(len(sources))
		script[3] = "resize:32x32"
		script[4] = "copyref:last+copyref:golden+copyref:altref"
		script[6] = "copyref:last+copyref:golden"
		apply := map[int]func(*testing.T, *VP8Encoder){
			3: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetRealtimeTarget(32x32)", e.SetRealtimeTarget(RealtimeTarget{Width: 32, Height: 32}))
			},
			4: copyReferenceCheckApply("frame4", ReferenceLast, ReferenceGolden, ReferenceAltRef),
			6: copyReferenceCheckApply("frame6", ReferenceLast, ReferenceGolden),
		}
		logPath := filepath.Join(t.TempDir(), "copyref-resize.log")

		want := encodeFramesWithFrameFlagsDriver(t, driver, "copyref-resize-bytestream", opts, opts.TargetBitrateKbps, sources, nil, []string{
			"--control-script=" + strings.Join(script, ","),
			"--copy-ref-log=" + logPath,
		})
		got := encodeFramesWithGovpxRuntimeControls(t, opts, sources, nil, apply)
		assertSegmentByteParity(t, "copyref-resize-bytestream", got, want, 0)
	})

	t.Run("copy-reference-checks-under-auto-alt-ref-do-not-change-bytestream", func(t *testing.T) {
		opts := copyReferenceParityOptions(64, 64)
		opts.LookaheadFrames = 4
		opts.AutoAltRef = true
		sources := makePanningSources(opts.Width, opts.Height, 10, 0)
		script := emptyCopyReferenceScript(len(sources))
		script[4] = "copyref:last+copyref:golden+copyref:altref"
		script[7] = "copyref:last+copyref:altref"
		script[9] = "copyref:last+copyref:golden+copyref:altref"
		apply := map[int]func(*testing.T, *VP8Encoder){
			4: copyReferenceCheckApply("frame4", ReferenceLast, ReferenceGolden, ReferenceAltRef),
			7: copyReferenceCheckApply("frame7", ReferenceLast, ReferenceAltRef),
			9: copyReferenceCheckApply("frame9", ReferenceLast, ReferenceGolden, ReferenceAltRef),
		}
		logPath := filepath.Join(t.TempDir(), "copyref-auto-alt-ref.log")

		want := encodeFramesWithFrameFlagsDriver(t, driver, "copyref-auto-alt-ref-bytestream", opts, opts.TargetBitrateKbps, sources, nil, []string{
			"--control-script=" + strings.Join(script, ","),
			"--copy-ref-log=" + logPath,
			"--lag-in-frames=4",
			"--auto-alt-ref=1",
		})
		got := encodeFramesWithGovpxRuntimeControls(t, opts, sources, nil, apply)
		assertSegmentByteParity(t, "copyref-auto-alt-ref-bytestream", got, want, 0)
	})

	t.Run("copy-reference-checks-under-active-roi-runtime-controls-do-not-change-bytestream", func(t *testing.T) {
		opts := copyReferenceParityOptions(64, 64)
		sources := makePanningSources(opts.Width, opts.Height, 8, 0)
		script := emptyCopyReferenceScript(len(sources))
		script[1] = "active:checker+copyref:last+copyref:golden"
		script[2] = "roi:border1+copyref:golden"
		script[4] = "copyref:last"
		script[6] = "active:off+roi:off+copyref:last+copyref:golden+copyref:altref"
		apply := map[int]func(*testing.T, *VP8Encoder){
			1: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				activeMapApply("checker")(t, e)
				copyReferenceCheckApply("frame1", ReferenceLast, ReferenceGolden)(t, e)
			},
			2: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				roiMapApply("border1")(t, e)
				copyReferenceCheckApply("frame2", ReferenceGolden)(t, e)
			},
			4: copyReferenceCheckApply("frame4", ReferenceLast),
			6: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
				mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				copyReferenceCheckApply("frame6", ReferenceLast, ReferenceGolden, ReferenceAltRef)(t, e)
			},
		}
		logPath := filepath.Join(t.TempDir(), "copyref-active-roi-runtime-controls.log")

		want := encodeFramesWithFrameFlagsDriver(t, driver, "copyref-active-roi-runtime-controls", opts, opts.TargetBitrateKbps, sources, nil, []string{
			"--control-script=" + strings.Join(script, ","),
			"--copy-ref-log=" + logPath,
		})
		got := encodeFramesWithGovpxRuntimeControls(t, opts, sources, nil, apply)
		assertSegmentByteParity(t, "copyref-active-roi-runtime-controls", got, want, 0)
	})

	t.Run("copy-reference-checks-under-runtime-denoiser-transition-do-not-change-bytestream", func(t *testing.T) {
		opts := copyReferenceParityOptions(64, 64)
		sources := makePanningSources(opts.Width, opts.Height, 8, 0)
		script := emptyCopyReferenceScript(len(sources))
		script[2] = "noise:3+copyref:last+copyref:golden"
		script[5] = "noise:0+copyref:last+copyref:golden+copyref:altref"
		apply := map[int]func(*testing.T, *VP8Encoder){
			2: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetNoiseSensitivity(3)", e.SetNoiseSensitivity(3))
				copyReferenceCheckApply("frame2", ReferenceLast, ReferenceGolden)(t, e)
			},
			5: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetNoiseSensitivity(0)", e.SetNoiseSensitivity(0))
				copyReferenceCheckApply("frame5", ReferenceLast, ReferenceGolden, ReferenceAltRef)(t, e)
			},
		}
		logPath := filepath.Join(t.TempDir(), "copyref-runtime-denoiser-transition.log")

		want := encodeFramesWithFrameFlagsDriver(t, driver, "copyref-runtime-denoiser-transition", opts, opts.TargetBitrateKbps, sources, nil, []string{
			"--control-script=" + strings.Join(script, ","),
			"--copy-ref-log=" + logPath,
		})
		got := encodeFramesWithGovpxRuntimeControls(t, opts, sources, nil, apply)
		assertSegmentByteParity(t, "copyref-runtime-denoiser-transition", got, want, 0)
	})

	t.Run("copy-reference-checks-under-screen-static-runtime-controls-do-not-change-bytestream", func(t *testing.T) {
		opts := copyReferenceParityOptions(64, 64)
		sources := makePanningSources(opts.Width, opts.Height, 8, 0)
		script := emptyCopyReferenceScript(len(sources))
		script[1] = "screen:2+static:500+copyref:last+copyref:golden"
		script[5] = "screen:0+static:0+copyref:last+copyref:golden+copyref:altref"
		apply := map[int]func(*testing.T, *VP8Encoder){
			1: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetScreenContentMode(2)", e.SetScreenContentMode(2))
				mustRuntime(t, "SetStaticThreshold(500)", e.SetStaticThreshold(500))
				copyReferenceCheckApply("frame1", ReferenceLast, ReferenceGolden)(t, e)
			},
			5: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetScreenContentMode(0)", e.SetScreenContentMode(0))
				mustRuntime(t, "SetStaticThreshold(0)", e.SetStaticThreshold(0))
				copyReferenceCheckApply("frame5", ReferenceLast, ReferenceGolden, ReferenceAltRef)(t, e)
			},
		}
		logPath := filepath.Join(t.TempDir(), "copyref-screen-static-runtime-controls.log")

		want := encodeFramesWithFrameFlagsDriver(t, driver, "copyref-screen-static-runtime-controls", opts, opts.TargetBitrateKbps, sources, nil, []string{
			"--control-script=" + strings.Join(script, ","),
			"--copy-ref-log=" + logPath,
		})
		got := encodeFramesWithGovpxRuntimeControls(t, opts, sources, nil, apply)
		assertSegmentByteParity(t, "copyref-screen-static-runtime-controls", got, want, 0)
	})

	t.Run("copy-reference-checks-under-rtc-external-runtime-controls-do-not-change-bytestream", func(t *testing.T) {
		opts := copyReferenceParityOptions(64, 64)
		sources := makePanningSources(opts.Width, opts.Height, 8, 0)
		script := emptyCopyReferenceScript(len(sources))
		script[2] = "rtc:1+copyref:last+copyref:golden"
		script[6] = "rtc:0+copyref:last+copyref:golden+copyref:altref"
		apply := map[int]func(*testing.T, *VP8Encoder){
			2: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetRTCExternalRateControl(true)", e.SetRTCExternalRateControl(true))
				copyReferenceCheckApply("frame2", ReferenceLast, ReferenceGolden)(t, e)
			},
			6: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetRTCExternalRateControl(false)", e.SetRTCExternalRateControl(false))
				copyReferenceCheckApply("frame6", ReferenceLast, ReferenceGolden, ReferenceAltRef)(t, e)
			},
		}
		logPath := filepath.Join(t.TempDir(), "copyref-rtc-runtime-controls.log")

		want := encodeFramesWithFrameFlagsDriver(t, driver, "copyref-rtc-runtime-controls", opts, opts.TargetBitrateKbps, sources, nil, []string{
			"--control-script=" + strings.Join(script, ","),
			"--copy-ref-log=" + logPath,
		})
		got := encodeFramesWithGovpxRuntimeControls(t, opts, sources, nil, apply)
		assertSegmentByteParity(t, "copyref-rtc-runtime-controls", got, want, 0)
	})

	t.Run("copy-reference-checks-under-runtime-controls-do-not-change-bytestream", func(t *testing.T) {
		opts := copyReferenceParityOptions(64, 64)
		sources := makePanningSources(opts.Width, opts.Height, 8, 0)
		script := emptyCopyReferenceScript(len(sources))
		script[1] = "active:checker+copyref:last+copyref:golden"
		script[2] = "roi:border1+copyref:golden"
		script[4] = "noise:3+copyref:last"
		script[6] = "noise:0+active:off+roi:off+copyref:last+copyref:golden+copyref:altref"
		apply := map[int]func(*testing.T, *VP8Encoder){
			1: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				activeMapApply("checker")(t, e)
				copyReferenceCheckApply("frame1", ReferenceLast, ReferenceGolden)(t, e)
			},
			2: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				roiMapApply("border1")(t, e)
				copyReferenceCheckApply("frame2", ReferenceGolden)(t, e)
			},
			4: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetNoiseSensitivity(3)", e.SetNoiseSensitivity(3))
				copyReferenceCheckApply("frame4", ReferenceLast)(t, e)
			},
			6: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetNoiseSensitivity(0)", e.SetNoiseSensitivity(0))
				mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
				mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				copyReferenceCheckApply("frame6", ReferenceLast, ReferenceGolden, ReferenceAltRef)(t, e)
			},
		}
		logPath := filepath.Join(t.TempDir(), "copyref-runtime-controls.log")

		want := encodeFramesWithFrameFlagsDriver(t, driver, "copyref-runtime-controls", opts, opts.TargetBitrateKbps, sources, nil, []string{
			"--control-script=" + strings.Join(script, ","),
			"--copy-ref-log=" + logPath,
		})
		got := encodeFramesWithGovpxRuntimeControls(t, opts, sources, nil, apply)
		assertSegmentByteParity(t, "copyref-runtime-controls", got, want, 0)
	})
}

type copyReferenceChecksum struct {
	Frame    int
	Ref      string
	YAdler32 uint32
	UAdler32 uint32
	VAdler32 uint32
}

type copyReferenceCheck struct {
	ref  ReferenceFrame
	name string
}

type copyReferenceSet struct {
	ref          ReferenceFrame
	name         string
	panningIndex int
}

func copyReferenceCheckApply(label string, refs ...ReferenceFrame) func(*testing.T, *VP8Encoder) {
	return func(t *testing.T, e *VP8Encoder) {
		t.Helper()
		dst := newTestImage(e.opts.Width, e.opts.Height)
		for _, ref := range refs {
			mustRuntime(t, label+" CopyReferenceFrame("+copyReferenceName(ref)+")", e.CopyReferenceFrame(ref, &dst))
		}
	}
}

func copyReferenceName(ref ReferenceFrame) string {
	switch ref {
	case ReferenceLast:
		return "last"
	case ReferenceGolden:
		return "golden"
	case ReferenceAltRef:
		return "altref"
	default:
		return strconv.Itoa(int(ref))
	}
}

func copyReferenceParityOptions(width, height int) EncoderOptions {
	return EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		Deadline:            DeadlineRealtime,
		CpuUsed:             0,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	}
}

func emptyCopyReferenceScript(frames int) []string {
	script := make([]string, frames)
	for i := range script {
		script[i] = "-"
	}
	return script
}

func captureLibvpxCopyReferenceChecksums(t *testing.T, driver, name string, opts EncoderOptions, sources []Image, flags []EncodeFlags, script []string) []copyReferenceChecksum {
	t.Helper()
	return captureLibvpxCopyReferenceChecksumsWithExtraArgs(t, driver, name, opts, sources, flags, script, nil)
}

func captureLibvpxCopyReferenceChecksumsWithExtraArgs(t *testing.T, driver, name string, opts EncoderOptions, sources []Image, flags []EncodeFlags, script []string, extraArgs []string) []copyReferenceChecksum {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), name+".log")
	args := []string{
		"--control-script=" + strings.Join(script, ","),
		"--copy-ref-log=" + logPath,
	}
	args = append(args, extraArgs...)
	_ = encodeFramesWithFrameFlagsDriver(t, driver, name, opts, opts.TargetBitrateKbps, sources, flags, args)
	return readCopyReferenceChecksumLog(t, logPath)
}

func captureGovpxCopyReferenceChecksums(t *testing.T, opts EncoderOptions, sources []Image, flags []EncodeFlags, sets map[int][]copyReferenceSet, checks map[int][]copyReferenceCheck) []copyReferenceChecksum {
	t.Helper()
	return captureGovpxCopyReferenceChecksumsWithApply(t, opts, sources, flags, sets, nil, checks)
}

func captureGovpxCopyReferenceChecksumsWithApply(t *testing.T, opts EncoderOptions, sources []Image, flags []EncodeFlags, sets map[int][]copyReferenceSet, apply map[int]func(*testing.T, *VP8Encoder), checks map[int][]copyReferenceCheck) []copyReferenceChecksum {
	t.Helper()
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	defer enc.Close()

	maxPixels := opts.Width * opts.Height
	for _, src := range sources {
		if pixels := src.Width * src.Height; pixels > maxPixels {
			maxPixels = pixels
		}
	}
	buf := make([]byte, maxPixels*4+4096)
	out := make([]copyReferenceChecksum, 0)
	for i, src := range sources {
		for _, set := range sets[i] {
			img := encoderValidationPanningFrame(enc.opts.Width, enc.opts.Height, set.panningIndex)
			if err := enc.SetReferenceFrame(set.ref, img); err != nil {
				t.Fatalf("frame %d SetReferenceFrame(%s): %v", i, set.name, err)
			}
		}
		if fn := apply[i]; fn != nil {
			fn(t, enc)
		}
		for _, check := range checks[i] {
			dst := testImage(enc.opts.Width, enc.opts.Height)
			if err := enc.CopyReferenceFrame(check.ref, &dst); err != nil {
				t.Fatalf("frame %d CopyReferenceFrame(%s): %v", i, check.name, err)
			}
			out = append(out, copyReferenceImageChecksum(i, check.name, dst))
		}
		var frameFlags EncodeFlags
		if i < len(flags) {
			frameFlags = flags[i]
		}
		if _, err := enc.EncodeInto(buf, src, uint64(i), 1, frameFlags); err != nil {
			t.Fatalf("EncodeInto frame %d: %v", i, err)
		}
	}
	return out
}

func copyReferenceImageChecksum(frame int, ref string, img Image) copyReferenceChecksum {
	uvWidth := (img.Width + 1) >> 1
	uvHeight := (img.Height + 1) >> 1
	return copyReferenceChecksum{
		Frame:    frame,
		Ref:      ref,
		YAdler32: planeAdler32(img.Y, img.Width, img.Height, img.YStride),
		UAdler32: planeAdler32(img.U, uvWidth, uvHeight, img.UStride),
		VAdler32: planeAdler32(img.V, uvWidth, uvHeight, img.VStride),
	}
}

func readCopyReferenceChecksumLog(t *testing.T, path string) []copyReferenceChecksum {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open copy-ref log %s: %v", path, err)
	}
	defer file.Close()

	var out []copyReferenceChecksum
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		fields := parseCopyReferenceLogFields(t, line)
		out = append(out, copyReferenceChecksum{
			Frame:    parseCopyReferenceLogInt(t, fields, "frame"),
			Ref:      fields["ref"],
			YAdler32: parseCopyReferenceLogUint32(t, fields, "y_adler32"),
			UAdler32: parseCopyReferenceLogUint32(t, fields, "u_adler32"),
			VAdler32: parseCopyReferenceLogUint32(t, fields, "v_adler32"),
		})
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan copy-ref log %s: %v", path, err)
	}
	if len(out) == 0 {
		t.Fatalf("copy-ref log %s had no entries", path)
	}
	return out
}

func parseCopyReferenceLogFields(t *testing.T, line string) map[string]string {
	t.Helper()
	out := make(map[string]string)
	for _, field := range strings.Fields(line) {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			t.Fatalf("invalid copy-ref log field %q in %q", field, line)
		}
		out[key] = value
	}
	for _, key := range []string{"frame", "ref", "y_adler32", "u_adler32", "v_adler32"} {
		if out[key] == "" {
			t.Fatalf("copy-ref log line %q missing %s", line, key)
		}
	}
	return out
}

func parseCopyReferenceLogInt(t *testing.T, fields map[string]string, key string) int {
	t.Helper()
	v, err := strconv.Atoi(fields[key])
	if err != nil {
		t.Fatalf("parse %s=%q: %v", key, fields[key], err)
	}
	return v
}

func parseCopyReferenceLogUint32(t *testing.T, fields map[string]string, key string) uint32 {
	t.Helper()
	v, err := strconv.ParseUint(fields[key], 10, 32)
	if err != nil {
		t.Fatalf("parse %s=%q: %v", key, fields[key], err)
	}
	return uint32(v)
}

func assertCopyReferenceChecksumsEqual(t *testing.T, got []copyReferenceChecksum, want []copyReferenceChecksum) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CopyReferenceFrame checksum mismatch\n govpx: %s\nlibvpx: %s", formatCopyReferenceChecksums(got), formatCopyReferenceChecksums(want))
	}
}

func formatCopyReferenceChecksums(checksums []copyReferenceChecksum) string {
	parts := make([]string, len(checksums))
	for i, c := range checksums {
		parts[i] = fmt.Sprintf("frame=%d ref=%s y=%08x u=%08x v=%08x", c.Frame, c.Ref, c.YAdler32, c.UAdler32, c.VAdler32)
	}
	return strings.Join(parts, "; ")
}
