//go:build govpx_oracle_trace

package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
	"path/filepath"
	"strings"
	"testing"
)

func TestVP8OracleEncoderCopyReferenceFrameParity(t *testing.T) {
	vp8test.RequireOracle(t, "encoder reference-copy parity gate")
	driver := vp8test.VpxencFrameFlags(t)

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
