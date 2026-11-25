//go:build govpx_oracle_trace

package codec

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// FuzzEncoderReferenceControlSequences schedules arbitrary per-frame
// SetReferenceFrame, CopyReferenceFrame, and EncodeFlags ref-update operations.
// The encoded bytes must match the libvpx vpxenc-frameflags driver driven
// through the same script.
//
// The existing vp8_oracle_encoder_copy_reference_parity_test.go covers
// hand-picked sequences; FuzzVP8OracleEncoderRuntimeControlTransitions
// covers ref-control as one action among many. This fuzzer is
// focused exclusively on the SET / COPY / ref-update permutation
// space, so its seed corpus stays tightly scoped to that surface
// without competing with the broader runtime-control fuzzer for
// regression-coverage attention.
func FuzzEncoderReferenceControlSequences(f *testing.F) {
	vp8test.RequireOracleF(f, "ref-control sequence fuzz")
	seeds := [][]byte{
		{0, 0, 0, 0, 0, 0, 0, 0},                 // all-default
		{0, 1, 0, 2, 0, 3, 0, 0},                 // SET last/golden/altref
		{4, 0, 5, 0, 6, 0, 0, 0},                 // COPY last/golden/altref
		{1, 2, 3, 4, 5, 6, 0, 0},                 // mixed SET+COPY
		{0xff, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, // long mixed sequence
		{0, 7, 0, 8, 0, 9, 0, 10},                // EFLAG NoUpdate variants
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		driver := vp8test.VpxencFrameFlags(t)
		tc := newRefControlsFuzzCase(data)
		sum := sha256.Sum256(data)
		label := "fuzz-refctrl-" + hex.EncodeToString(sum[:4])
		t.Logf("%s script=%s", label, strings.Join(tc.script, ","))

		govpxFrames := encodeFramesWithGovpxRuntimeControls(t, tc.opts, tc.sources, tc.flags, tc.apply)
		extraArgs := []string{
			"--copy-ref-log=" + filepath.Join(t.TempDir(), "copy-reference.log"),
			"--control-script=" + strings.Join(tc.script, ","),
		}
		libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, label, tc.opts, tc.targetKbps, tc.sources, tc.flags, extraArgs)
		assertSegmentByteParity(t, label, govpxFrames, libvpxFrames, 0)
	})
}

// newRefControlsFuzzCase generates a per-frame schedule that mixes
// SET, COPY, and EncodeFlags ref-update operations. Each fuzz byte
// past the prefix picks one operation kind from the focused pool.
func newRefControlsFuzzCase(data []byte) vp8OracleRuntimeControlFuzzCase {
	r := testutil.NewByteCursor(data)
	const (
		width   = 64
		height  = 64
		bitrate = 700
	)
	framesPool := [...]int{6, 8, 10}
	frames := framesPool[r.Pick(len(framesPool))]
	opts := vp8OracleRuntimeBaseFuzzOptions(width, height, bitrate, 0)
	sources := vp8OracleRuntimeFuzzSources(width, height, frames, 0)
	flags := make([]EncodeFlags, frames)
	script := runtimeControlScript(frames, nil)
	apply := make(map[int]func(*testing.T, *VP8Encoder), frames)

	refNames := [...]string{"last", "golden", "altref"}
	refs := [...]ReferenceFrame{ReferenceLast, ReferenceGolden, ReferenceAltRef}

	for frame := 1; frame < frames; frame++ {
		switch r.Pick(11) {
		case 0:
			// No-op frame.
		case 1, 2, 3:
			// SetReferenceFrame.
			idx := r.Pick(len(refs))
			imageIndex := 8 + r.Pick(8)
			script[frame] = "setref:" + refNames[idx] + ":panning:" + strconv.Itoa(imageIndex)
			apply[frame] = setReferencePanningApply(refs[idx], imageIndex, refNames[idx])
		case 4, 5, 6:
			// CopyReferenceFrame.
			idx := r.Pick(len(refs))
			script[frame] = "copyref:" + refNames[idx]
			capturedIdx := idx
			capturedName := refNames[idx]
			apply[frame] = func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				dst := newTestImage(e.opts.Width, e.opts.Height)
				mustRuntime(t, "CopyReferenceFrame("+capturedName+")", e.CopyReferenceFrame(refs[capturedIdx], &dst))
			}
		case 7:
			flags[frame] |= EncodeNoUpdateLast
		case 8:
			flags[frame] |= EncodeNoUpdateGolden
		case 9:
			flags[frame] |= EncodeNoUpdateAltRef
		case 10:
			flags[frame] |= EncodeNoReferenceLast | EncodeNoUpdateLast
		}
	}

	return vp8OracleRuntimeControlFuzzCase{
		name:       "refctrl",
		opts:       opts,
		targetKbps: bitrate,
		sources:    sources,
		flags:      flags,
		script:     script,
		apply:      apply,
		copyRefLog: true,
	}
}

// FuzzVP8OracleEncoderRuntimeControlTransitions compares generated runtime-control
// schedules against the libvpx frame-flags driver. Go writes failing fuzz inputs
// to testdata/fuzz/FuzzVP8OracleEncoderRuntimeControlTransitions, and those corpus
// files are replayed by ordinary go test runs as regression tests.
func FuzzVP8OracleEncoderRuntimeControlTransitions(f *testing.F) {
	vp8test.RequireOracleF(f, "runtime-control fuzz parity")
	seeds := [][]byte{
		{0, 0, 1, 2, 3, 4, 5, 6, 7, 8},
		{0, 2, 7, 7, 7, 3, 5, 1, 4, 6, 8},
		{0, 4, 3, 4, 8, 2, 2, 5, 6, 7, 1},
		{1, 0, 0, 0},
		{2, 0, 1, 2, 3, 4, 5, 6},
		{0xff, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9},
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		driver := vp8test.VpxencFrameFlags(t)
		tc := vp8OracleRuntimeControlFuzzCaseFromBytes(data)
		sum := sha256.Sum256(data)
		label := "fuzz-runtime-controls-" + tc.name + "-" + hex.EncodeToString(sum[:4])
		t.Logf("%s script=%s", label, strings.Join(tc.script, ","))

		govpxFrames := encodeFramesWithGovpxRuntimeControls(t, tc.opts, tc.sources, tc.flags, tc.apply)
		extraArgs := append([]string(nil), tc.extraArgs...)
		if tc.copyRefLog {
			extraArgs = append(extraArgs, "--copy-ref-log="+filepath.Join(t.TempDir(), "copy-reference.log"))
		}
		extraArgs = append(extraArgs, "--control-script="+strings.Join(tc.script, ","))
		libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, label, tc.opts, tc.targetKbps, tc.sources, tc.flags, extraArgs)
		assertSegmentByteParity(t, label, govpxFrames, libvpxFrames,
			vp8OracleRuntimeControlFuzzMatchLimit(t.Name()))
	})
}

func TestVP8OracleEncoderStreamByteParityInvalidRuntimeControlsNoop(t *testing.T) {
	vp8test.RequireOracle(t, "invalid runtime-control byte-parity gate")
	driver := vp8test.VpxencFrameFlags(t)

	const (
		fps        = 30
		targetKbps = 700
		frames     = 10
		width      = 64
		height     = 64
	)
	opts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               fps,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  999,
		Deadline:          DeadlineRealtime,
		CpuUsed:           0,
		Tuning:            TunePSNR,
	}
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(width, height, i)
	}
	apply := map[int]func(*testing.T, *VP8Encoder){
		1: func(t *testing.T, e *VP8Encoder) {
			t.Helper()
			expectInvalidRuntime(t, "SetRateControl(minq>maxq)", ErrInvalidQuantizer, e.SetRateControl(RateControlConfig{
				Mode:              RateControlCBR,
				TargetBitrateKbps: targetKbps,
				MinQuantizer:      50,
				MaxQuantizer:      10,
			}))
		},
		2: func(t *testing.T, e *VP8Encoder) {
			t.Helper()
			expectInvalidRuntime(t, "SetRealtimeTarget(width-only)", ErrInvalidConfig, e.SetRealtimeTarget(RealtimeTarget{Width: 32}))
		},
		3: func(t *testing.T, e *VP8Encoder) {
			t.Helper()
			expectInvalidRuntime(t, "SetCQLevel(64)", ErrInvalidQuantizer, e.SetCQLevel(64))
		},
		4: func(t *testing.T, e *VP8Encoder) {
			t.Helper()
			expectInvalidRuntime(t, "SetTemporalLayerID(1)", ErrInvalidConfig, e.SetTemporalLayerID(1))
		},
		5: func(t *testing.T, e *VP8Encoder) {
			t.Helper()
			rows := encoderMacroblockRows(e.opts.Height)
			cols := encoderMacroblockCols(e.opts.Width)
			expectInvalidRuntime(t, "SetActiveMap(wrong rows)", ErrInvalidConfig, e.SetActiveMap(make([]uint8, rows*cols), rows+1, cols))
		},
		6: func(t *testing.T, e *VP8Encoder) {
			t.Helper()
			rows := encoderMacroblockRows(e.opts.Height)
			cols := encoderMacroblockCols(e.opts.Width)
			expectInvalidRuntime(t, "SetROIMap(wrong rows)", ErrInvalidConfig, e.SetROIMap(&ROIMap{
				Enabled:   true,
				Rows:      rows + 1,
				Cols:      cols,
				SegmentID: make([]uint8, rows*cols),
			}))
		},
		7: func(t *testing.T, e *VP8Encoder) {
			t.Helper()
			wrongSize := encoderValidationPanningFrame(width/2, height, 0)
			expectInvalidRuntime(t, "SetReferenceFrame(wrong size)", ErrInvalidConfig, e.SetReferenceFrame(ReferenceLast, wrongSize))
		},
	}

	govpxFrames := encodeFramesWithGovpxRuntimeControls(t, opts, sources, nil, apply)
	libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, "invalid-runtime-controls-noop", opts, targetKbps, sources, nil, nil)
	assertSegmentByteParity(t, "invalid-runtime-controls-noop", govpxFrames, libvpxFrames, 0)
}

func TestVP8OracleEncoderStreamByteParityRuntimeControlsLongTail(t *testing.T) {
	vp8test.RequireOracle(t, "long-tail runtime-control byte-parity gate")
	driver := vp8test.VpxencFrameFlags(t)

	const (
		fps        = 30
		targetKbps = 700
		frames     = 32
	)
	type fixture struct {
		name   string
		w, h   int
		source func(w, h, i int) Image
	}
	panning64 := fixture{name: "panning-64x64", w: 64, h: 64, source: encoderValidationPanningFrame}
	segmented64 := fixture{name: "segmented-64x64", w: 64, h: 64, source: encoderValidationSegmentedFrame}
	baseOpts := func(fx fixture) EncoderOptions {
		return EncoderOptions{
			Width:             fx.w,
			Height:            fx.h,
			FPS:               fps,
			RateControlMode:   RateControlCBR,
			TargetBitrateKbps: targetKbps,
			MinQuantizer:      4,
			MaxQuantizer:      56,
			KeyFrameInterval:  999,
			Deadline:          DeadlineRealtime,
			CpuUsed:           0,
			Tuning:            TunePSNR,
		}
	}
	type longTailCase struct {
		name      string
		fx        fixture
		opts      EncoderOptions
		flags     []EncodeFlags
		script    []string
		apply     map[int]func(*testing.T, *VP8Encoder)
		extraArgs []string
	}
	cases := []longTailCase{
		{
			name:   "active-roi-toggle-state-drift",
			fx:     segmented64,
			opts:   baseOpts(segmented64),
			script: runtimeControlScript(frames, map[int]string{2: "active:checker+roi:border1", 16: "active:off+roi:off"}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					activeMapApply("checker")(t, e)
					roiMapApply("border1")(t, e)
				},
				16: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				},
			},
		},
		{
			name:   "reference-and-denoise-state-drift",
			fx:     panning64,
			opts:   baseOpts(panning64),
			script: runtimeControlScript(frames, map[int]string{1: "noise:4", 5: "setref:last:panning:9", 18: "noise:0", 24: "setref:golden:panning:10"}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(4)", e.SetNoiseSensitivity(4))
				},
				5: setReferencePanningApply(ReferenceLast, 9, "last"),
				18: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(0)", e.SetNoiseSensitivity(0))
				},
				24: setReferencePanningApply(ReferenceGolden, 10, "golden"),
			},
		},
		{
			name:   "mode-control-state-drift",
			fx:     panning64,
			opts:   baseOpts(panning64),
			script: runtimeControlScript(frames, map[int]string{4: "static:500+screen:1+sharpness:4", 12: "deadline:good+cpu:4", 20: "deadline:rt+cpu:-8", 24: "static:0+screen:0+sharpness:0"}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				4: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetStaticThreshold(500)", e.SetStaticThreshold(500))
					mustRuntime(t, "SetScreenContentMode(1)", e.SetScreenContentMode(1))
					mustRuntime(t, "SetSharpness(4)", e.SetSharpness(4))
				},
				12: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetDeadline(good)", e.SetDeadline(DeadlineGoodQuality))
					mustRuntime(t, "SetCPUUsed(4)", e.SetCPUUsed(4))
				},
				20: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetDeadline(rt)", e.SetDeadline(DeadlineRealtime))
					mustRuntime(t, "SetCPUUsed(-8)", e.SetCPUUsed(-8))
				},
				24: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetStaticThreshold(0)", e.SetStaticThreshold(0))
					mustRuntime(t, "SetScreenContentMode(0)", e.SetScreenContentMode(0))
					mustRuntime(t, "SetSharpness(0)", e.SetSharpness(0))
				},
			},
		},
		{
			name:   "temporal-enable-disable-state-drift",
			fx:     panning64,
			opts:   baseOpts(panning64),
			flags:  temporalScalabilityEnableDisableFlags(frames),
			script: temporalScalabilityEnableDisableScript(frames),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalScalability(two-layer)", e.SetTemporalScalability(runtimeTemporalConfig(TemporalLayeringTwoLayers, targetKbps)))
					mustRuntime(t, "SetTemporalLayerID(0)", e.SetTemporalLayerID(0))
				},
				3: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalLayerID(1)", e.SetTemporalLayerID(1))
				},
				4: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalLayerID(0)", e.SetTemporalLayerID(0))
				},
				5: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalLayerID(1)", e.SetTemporalLayerID(1))
				},
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalScalability(off)", e.SetTemporalScalability(TemporalScalabilityConfig{}))
				},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]Image, frames)
			for i := range sources {
				sources[i] = tc.fx.source(tc.fx.w, tc.fx.h, i)
			}
			govpxFrames := encodeFramesWithGovpxRuntimeControls(t, tc.opts, sources, tc.flags, tc.apply)
			extraArgs := append([]string(nil), tc.extraArgs...)
			extraArgs = append(extraArgs, "--control-script="+strings.Join(tc.script, ","))
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, "runtime-long-tail-"+tc.name, tc.opts, tc.opts.TargetBitrateKbps, sources, tc.flags, extraArgs)
			assertSegmentByteParity(t, "runtime-long-tail-"+tc.name, govpxFrames, libvpxFrames, 0)
		})
	}
}

// TestVP8OracleEncoderStreamByteParityBuffer widens the strict byte-parity
// matrix beyond the buffer-size combinations already pinned by
// [TestVP8OracleEncoderStreamByteParity] (which covers
// {500/500/500, 1000/500/600, 2000/1000/1500, 4000/1000/3000}). It
// targets the buffer-model edge cases — very tight buffers, very
// large buffers, near-zero buffers, asymmetric initial/optimal vs.
// total — plus their cross-products with bitrate, drop-frame
// threshold, max-intra-rate, gf-cbr-boost, and undershoot/overshoot
// extremes. The probe surface here is the libvpx rate-control buffer
// model arithmetic, so any divergence in the buffer-level update or
// drop-frame gate becomes visible in the per-frame logs.
//
// Each subtest follows the protocol of the base matrix: feed the
// same I420 fixture to govpx and to the patched vpxenc-oracle under
// matching options, then assert the encoded frame payloads
// byte-match. Cases that diverge are pinned with `limit:` so the
// gap stays visible without regressing the strict gate.
func TestVP8OracleEncoderStreamByteParityBuffer(t *testing.T) {
	vp8test.RequireOracle(t, "encoder stream byte-parity gate")
	vpxencOracle := vp8test.VpxencOracle(t)

	const (
		fps        = 30
		targetKbps = 700
		frames     = 16
	)

	type fixture struct {
		name   string
		w, h   int
		source func(w, h, i int) Image
	}
	panning32 := fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}
	panning64 := fixture{name: "panning-64x64", w: 64, h: 64, source: encoderValidationPanningFrame}

	cases := []struct {
		name                string
		deadline            Deadline
		cpuUsed             int
		fx                  fixture
		limit               int
		rcMode              RateControlMode
		rcModeSet           bool
		maxIntraBitratePct  int
		gfCBRBoostPct       int
		undershootPct       int
		overshootPct        int
		bufferSizeMs        int
		bufferInitialSizeMs int
		bufferOptimalSizeMs int
		dropFrameAllowed    bool
		dropFrameWaterMark  int
		targetKbpsOverride  int
		extraArgs           []string
	}{
		// 1. Tight buffer at small fixtures. 200/100/150 is the
		// real-time WebRTC "tight" preset; 500/100/300 is a
		// smaller initial-vs-optimal asymmetry. Both pin the
		// libvpx buffer-level clamp at sub-1s buffer durations.
		{name: "buffer-200-100-150-realtime-cpu-3-32x32", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, bufferSizeMs: 200, bufferInitialSizeMs: 100, bufferOptimalSizeMs: 150, extraArgs: []string{"--buf-sz=200", "--buf-initial-sz=100", "--buf-optimal-sz=150"}},
		{name: "buffer-200-100-150-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, bufferSizeMs: 200, bufferInitialSizeMs: 100, bufferOptimalSizeMs: 150, extraArgs: []string{"--buf-sz=200", "--buf-initial-sz=100", "--buf-optimal-sz=150"}},
		{name: "buffer-500-100-300-realtime-cpu-3-32x32", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, bufferSizeMs: 500, bufferInitialSizeMs: 100, bufferOptimalSizeMs: 300, extraArgs: []string{"--buf-sz=500", "--buf-initial-sz=100", "--buf-optimal-sz=300"}},
		{name: "buffer-500-100-300-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, bufferSizeMs: 500, bufferInitialSizeMs: 100, bufferOptimalSizeMs: 300, extraArgs: []string{"--buf-sz=500", "--buf-initial-sz=100", "--buf-optimal-sz=300"}},

		// 1b. Near-zero buffer. 1/1/1 stresses the path where
		// libvpx clamps tiny buffer values inside the
		// vp8_rc_init_minq_luts allocator. Pinned at strict
		// byte parity (was a divergence gap until the
		// CBR-full-buffer active-worst arithmetic and
		// raw-target-rate cap were ported; see
		// libvpxCBRFullBufferActiveWorst and
		// libvpxRawTargetRateCapKbps).
		{name: "buffer-1-1-1-realtime-cpu-3-32x32", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, bufferSizeMs: 1, bufferInitialSizeMs: 1, bufferOptimalSizeMs: 1, extraArgs: []string{"--buf-sz=1", "--buf-initial-sz=1", "--buf-optimal-sz=1"}},
		{name: "buffer-1-1-1-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, bufferSizeMs: 1, bufferInitialSizeMs: 1, bufferOptimalSizeMs: 1, extraArgs: []string{"--buf-sz=1", "--buf-initial-sz=1", "--buf-optimal-sz=1"}},

		// 2. Very large buffer (10000/5000/7500). Pins the
		// buffer-saturation path where the rate controller has
		// plenty of slack — libvpx still applies its minimum-q
		// floor here, so byte parity should hold.
		{name: "buffer-10000-5000-7500-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, bufferSizeMs: 10000, bufferInitialSizeMs: 5000, bufferOptimalSizeMs: 7500, extraArgs: []string{"--buf-sz=10000", "--buf-initial-sz=5000", "--buf-optimal-sz=7500"}},
		{name: "buffer-10000-5000-7500-realtime-cpu0-64x64", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning64, bufferSizeMs: 10000, bufferInitialSizeMs: 5000, bufferOptimalSizeMs: 7500, extraArgs: []string{"--buf-sz=10000", "--buf-initial-sz=5000", "--buf-optimal-sz=7500"}},

		// Asymmetric: size >> initial + optimal (large
		// over-provisioned reservoir with low starting fill).
		{name: "buffer-8000-200-500-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, bufferSizeMs: 8000, bufferInitialSizeMs: 200, bufferOptimalSizeMs: 500, extraArgs: []string{"--buf-sz=8000", "--buf-initial-sz=200", "--buf-optimal-sz=500"}},

		// 3. Drop-frame threshold sweep at 64x64 across cpu=-3
		// and cpu=8. The drop-frame gate is per-frame; with the
		// smooth panning fixture and the default 700kbps target,
		// none of these should actually drop, so they pin the
		// "drop gate enabled, no drops" branch of the buffer
		// model.
		{name: "drop-frame5-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, dropFrameAllowed: true, dropFrameWaterMark: 5, extraArgs: []string{"--drop-frame=5"}},
		{name: "drop-frame25-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, dropFrameAllowed: true, dropFrameWaterMark: 25, extraArgs: []string{"--drop-frame=25"}},
		{name: "drop-frame75-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, dropFrameAllowed: true, dropFrameWaterMark: 75, extraArgs: []string{"--drop-frame=75"}},
		{name: "drop-frame99-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, dropFrameAllowed: true, dropFrameWaterMark: 99, extraArgs: []string{"--drop-frame=99"}},
		{name: "drop-frame5-realtime-cpu8-64x64", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning64, dropFrameAllowed: true, dropFrameWaterMark: 5, extraArgs: []string{"--drop-frame=5"}},
		{name: "drop-frame25-realtime-cpu8-64x64", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning64, dropFrameAllowed: true, dropFrameWaterMark: 25, extraArgs: []string{"--drop-frame=25"}},
		{name: "drop-frame75-realtime-cpu8-64x64", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning64, dropFrameAllowed: true, dropFrameWaterMark: 75, extraArgs: []string{"--drop-frame=75"}},
		{name: "drop-frame99-realtime-cpu8-64x64", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning64, dropFrameAllowed: true, dropFrameWaterMark: 99, extraArgs: []string{"--drop-frame=99"}},

		// 4. Undershoot/overshoot grid. (0,0) pins the
		// zero-percent path; (100,0)/(0,100) pin the asymmetric
		// trims; (100,100) is the libvpx upper bound — libvpx
		// rejects rc_undershoot_pct/rc_overshoot_pct > 100
		// ("out of range [..100]"), so the task's (200,200)
		// suggestion can never be checked against the oracle
		// and is mapped to the documented edge instead.
		{name: "undershoot0-overshoot0-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, undershootPct: 0, overshootPct: 0, extraArgs: []string{"--undershoot-pct=0", "--overshoot-pct=0"}},
		{name: "undershoot100-overshoot0-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, undershootPct: 100, overshootPct: 0, extraArgs: []string{"--undershoot-pct=100", "--overshoot-pct=0"}},
		{name: "undershoot0-overshoot100-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, undershootPct: 0, overshootPct: 100, extraArgs: []string{"--undershoot-pct=0", "--overshoot-pct=100"}},
		{name: "undershoot100-overshoot100-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, undershootPct: 100, overshootPct: 100, extraArgs: []string{"--undershoot-pct=100", "--overshoot-pct=100"}},

		// 5. max-intra-rate extremes. 50 caps the keyframe budget
		// well below the inter target; 500/2000/10000 progressively
		// relax it. Frame 0 is the only keyframe in this 16-frame
		// budget so this primarily pins the KF-cap arithmetic.
		{name: "max-intra-rate50-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, maxIntraBitratePct: 50, extraArgs: []string{"--max-intra-rate=50"}},
		{name: "max-intra-rate500-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, maxIntraBitratePct: 500, extraArgs: []string{"--max-intra-rate=500"}},
		{name: "max-intra-rate2000-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, maxIntraBitratePct: 2000, extraArgs: []string{"--max-intra-rate=2000"}},
		{name: "max-intra-rate10000-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, maxIntraBitratePct: 10000, extraArgs: []string{"--max-intra-rate=10000"}},

		// 6. gf-cbr-boost extremes. 0 disables the golden-frame
		// CBR boost; 500/2000 push the upper-bound clamp.
		{name: "gf-cbr-boost0-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, gfCBRBoostPct: 0, extraArgs: []string{"--gf-cbr-boost=0"}},
		{name: "gf-cbr-boost500-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, gfCBRBoostPct: 500, extraArgs: []string{"--gf-cbr-boost=500"}},
		{name: "gf-cbr-boost2000-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, gfCBRBoostPct: 2000, extraArgs: []string{"--gf-cbr-boost=2000"}},

		// 7. CBR bitrate extremes at 64x64. 25 kbps is below the
		// libvpx-recommended VP8 minimum (the rate allocator
		// clamps internally); 5000/10000 push the upper-band
		// clamp.
		{name: "low-bitrate25-cbr-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, targetKbpsOverride: 25, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=25"}},
		{name: "low-bitrate50-cbr-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, targetKbpsOverride: 50, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=50"}},
		{name: "high-bitrate5000-cbr-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, targetKbpsOverride: 5000, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=5000"}},
		{name: "high-bitrate10000-cbr-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, targetKbpsOverride: 10000, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=10000"}},

		// 8. VBR bitrate extremes at 64x64. Mirrors the CBR
		// sweep so the VBR allocator's clamp band is also
		// pinned.
		{name: "low-bitrate25-vbr-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, rcMode: RateControlVBR, rcModeSet: true, targetKbpsOverride: 25, extraArgs: []string{"--end-usage=vbr", "--target-bitrate=25"}},
		{name: "low-bitrate50-vbr-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, rcMode: RateControlVBR, rcModeSet: true, targetKbpsOverride: 50, extraArgs: []string{"--end-usage=vbr", "--target-bitrate=50"}},
		{name: "high-bitrate5000-vbr-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, rcMode: RateControlVBR, rcModeSet: true, targetKbpsOverride: 5000, extraArgs: []string{"--end-usage=vbr", "--target-bitrate=5000"}},
		{name: "high-bitrate10000-vbr-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, rcMode: RateControlVBR, rcModeSet: true, targetKbpsOverride: 10000, extraArgs: []string{"--end-usage=vbr", "--target-bitrate=10000"}},

		// Cross with bitrate extremes: tight buffer + low bitrate
		// (the underflow-prone combination) and large buffer +
		// high bitrate (the overflow-prone one).
		{name: "buffer-200-100-150-low-bitrate50-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, bufferSizeMs: 200, bufferInitialSizeMs: 100, bufferOptimalSizeMs: 150, targetKbpsOverride: 50, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=50", "--buf-sz=200", "--buf-initial-sz=100", "--buf-optimal-sz=150"}},
		{name: "buffer-10000-5000-7500-high-bitrate5000-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, bufferSizeMs: 10000, bufferInitialSizeMs: 5000, bufferOptimalSizeMs: 7500, targetKbpsOverride: 5000, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=5000", "--buf-sz=10000", "--buf-initial-sz=5000", "--buf-optimal-sz=7500"}},

		// Cross with drop-frame: tight buffer + drop gate. With
		// 25 kbps the under-provisioned buffer plus aggressive
		// drop threshold should still byte-match (the panning
		// fixture is smooth enough that the per-frame bit
		// consumption stays within bounds).
		{name: "buffer-200-100-150-drop-frame50-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, bufferSizeMs: 200, bufferInitialSizeMs: 100, bufferOptimalSizeMs: 150, dropFrameAllowed: true, dropFrameWaterMark: 50, extraArgs: []string{"--buf-sz=200", "--buf-initial-sz=100", "--buf-optimal-sz=150", "--drop-frame=50"}},

		// Cross with max-intra-rate / gf-cbr-boost: tight
		// buffer interacts with the KF/GF allocator caps.
		{name: "buffer-200-100-150-max-intra-rate500-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, bufferSizeMs: 200, bufferInitialSizeMs: 100, bufferOptimalSizeMs: 150, maxIntraBitratePct: 500, extraArgs: []string{"--buf-sz=200", "--buf-initial-sz=100", "--buf-optimal-sz=150", "--max-intra-rate=500"}},
		{name: "buffer-200-100-150-gf-cbr-boost500-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, bufferSizeMs: 200, bufferInitialSizeMs: 100, bufferOptimalSizeMs: 150, gfCBRBoostPct: 500, extraArgs: []string{"--buf-sz=200", "--buf-initial-sz=100", "--buf-optimal-sz=150", "--gf-cbr-boost=500"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]Image, frames)
			for i := range sources {
				sources[i] = tc.fx.source(tc.fx.w, tc.fx.h, i)
			}
			rcMode := tc.rcMode
			if !tc.rcModeSet {
				rcMode = RateControlCBR
			}
			caseTargetKbps := targetKbps
			if tc.targetKbpsOverride > 0 {
				caseTargetKbps = tc.targetKbpsOverride
			}
			opts := EncoderOptions{
				Width:               tc.fx.w,
				Height:              tc.fx.h,
				FPS:                 fps,
				RateControlMode:     rcMode,
				TargetBitrateKbps:   caseTargetKbps,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				KeyFrameInterval:    999,
				Deadline:            tc.deadline,
				CpuUsed:             tc.cpuUsed,
				Tuning:              TunePSNR,
				MaxIntraBitratePct:  tc.maxIntraBitratePct,
				GFCBRBoostPct:       tc.gfCBRBoostPct,
				UndershootPct:       tc.undershootPct,
				OvershootPct:        tc.overshootPct,
				BufferSizeMs:        tc.bufferSizeMs,
				BufferInitialSizeMs: tc.bufferInitialSizeMs,
				BufferOptimalSizeMs: tc.bufferOptimalSizeMs,
				DropFrameAllowed:    tc.dropFrameAllowed,
				DropFrameWaterMark:  tc.dropFrameWaterMark,
			}

			govpxFrames := encodeFramesWithGovpx(t, opts, sources)
			extraArgs := libvpxEndUsageArgs(tc.extraArgs)
			libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, tc.name, opts, caseTargetKbps, sources, extraArgs)

			if len(govpxFrames) != len(libvpxFrames) {
				if tc.limit < 0 {
					t.Logf("frame count mismatch (not asserted, known gap): govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
					assertStrictGateKnownGapMatchedPrefix(t, tc.name, govpxFrames, libvpxFrames, 1)
					return
				}
				t.Fatalf("frame count mismatch: govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
			}

			limit := len(govpxFrames)
			switch {
			case tc.limit < 0:
				limit = 0
			case tc.limit > 0 && tc.limit < limit:
				limit = tc.limit
			}
			for i := 0; i < len(govpxFrames); i++ {
				gHash := sha256.Sum256(govpxFrames[i])
				lHash := sha256.Sum256(libvpxFrames[i])
				gFP, gIsKey := parseVP8FramePartitionSizes(govpxFrames[i])
				lFP, lIsKey := parseVP8FramePartitionSizes(libvpxFrames[i])
				if gHash == lHash {
					t.Logf("frame %d byte MATCH: len=%d first_part=%d keyframe=%t", i, len(govpxFrames[i]), gFP, gIsKey)
					continue
				}
				firstDiff := testutil.FirstByteDiff(govpxFrames[i], libvpxFrames[i])
				firstNonTagDiff := testutil.FirstByteDiff(govpxFrames[i][3:], libvpxFrames[i][3:])
				if firstNonTagDiff >= 0 {
					firstNonTagDiff += 3
				}
				if i >= limit {
					t.Logf("frame %d byte mismatch (not asserted, limit=%d): govpx_len=%d libvpx_len=%d first_diff=%d non_tag_diff=%d govpx_first_part=%d libvpx_first_part=%d",
						i, limit, len(govpxFrames[i]), len(libvpxFrames[i]), firstDiff, firstNonTagDiff, gFP, lFP)
					continue
				}
				t.Errorf("frame %d byte mismatch: govpx_len=%d libvpx_len=%d first_diff=%d govpx_first_part=%d libvpx_first_part=%d govpx_keyframe=%t libvpx_keyframe=%t govpx_sha=%s libvpx_sha=%s",
					i, len(govpxFrames[i]), len(libvpxFrames[i]), firstDiff,
					gFP, lFP, gIsKey, lIsKey,
					hex.EncodeToString(gHash[:8]), hex.EncodeToString(lHash[:8]))
			}
		})
	}
}

func TestVP8OracleEncoderStreamByteParityRTCExternalRateControl(t *testing.T) {
	vp8test.RequireOracle(t, "RTC external-rate-control byte-parity gate")
	driver := vp8test.VpxencFrameFlags(t)

	const (
		fps    = 30
		frames = 16
		width  = 64
		height = 64
	)
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(width, height, i)
	}

	cases := []struct {
		name                string
		targetKbps          int
		undershootPct       int
		overshootPct        int
		bufferSizeMs        int
		bufferInitialSizeMs int
		bufferOptimalSizeMs int
		dropFrameAllowed    bool
		dropFrameWaterMark  int
		tokenPartitions     int
		threads             int
		errorResilient      bool
		errorResilientParts bool
		screenContentMode   int
		sharpness           int
		extraArgs           []string
	}{
		{
			name:                "drop-buffer-low-bitrate",
			targetKbps:          80,
			bufferSizeMs:        200,
			bufferInitialSizeMs: 100,
			bufferOptimalSizeMs: 150,
			dropFrameAllowed:    true,
			dropFrameWaterMark:  60,
			extraArgs:           []string{"--buf-sz=200", "--buf-initial-sz=100", "--buf-optimal-sz=150", "--drop-frame=60"},
		},
		{
			name:       "default-buffer-mid-bitrate",
			targetKbps: 700,
		},
		{
			name:          "undershoot-overshoot-edges",
			targetKbps:    700,
			undershootPct: 0,
			overshootPct:  100,
			extraArgs:     []string{"--undershoot-pct=0", "--overshoot-pct=100"},
		},
		{
			name:                "tight-buffer-mid-bitrate",
			targetKbps:          400,
			bufferSizeMs:        200,
			bufferInitialSizeMs: 100,
			bufferOptimalSizeMs: 150,
			dropFrameAllowed:    true,
			dropFrameWaterMark:  50,
			extraArgs:           []string{"--buf-sz=200", "--buf-initial-sz=100", "--buf-optimal-sz=150", "--drop-frame=50"},
		},
		{
			name:            "token-parts4-mid-bitrate",
			targetKbps:      700,
			tokenPartitions: 2,
			extraArgs:       []string{"--token-parts=2"},
		},
		{
			name:       "threads2-mid-bitrate",
			targetKbps: 700,
			threads:    2,
			extraArgs:  []string{"--threads=2"},
		},
		{
			name:                "er3-token-parts4-mid-bitrate",
			targetKbps:          700,
			tokenPartitions:     2,
			errorResilient:      true,
			errorResilientParts: true,
			extraArgs:           []string{"--error-resilient=3", "--token-parts=2"},
		},
		{
			name:              "screen-content2-mid-bitrate",
			targetKbps:        700,
			screenContentMode: 2,
			extraArgs:         []string{"--screen-content-mode=2"},
		},
		{
			name:       "sharpness4-mid-bitrate",
			targetKbps: 700,
			sharpness:  4,
			extraArgs:  []string{"--sharpness=4"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := EncoderOptions{
				Width:                    width,
				Height:                   height,
				FPS:                      fps,
				RateControlMode:          RateControlCBR,
				TargetBitrateKbps:        tc.targetKbps,
				MinQuantizer:             4,
				MaxQuantizer:             56,
				KeyFrameInterval:         999,
				Deadline:                 DeadlineRealtime,
				CpuUsed:                  -3,
				Tuning:                   TunePSNR,
				UndershootPct:            tc.undershootPct,
				OvershootPct:             tc.overshootPct,
				BufferSizeMs:             tc.bufferSizeMs,
				BufferInitialSizeMs:      tc.bufferInitialSizeMs,
				BufferOptimalSizeMs:      tc.bufferOptimalSizeMs,
				DropFrameAllowed:         tc.dropFrameAllowed,
				DropFrameWaterMark:       tc.dropFrameWaterMark,
				RTCExternalRateControl:   true,
				TokenPartitions:          tc.tokenPartitions,
				Threads:                  tc.threads,
				ErrorResilient:           tc.errorResilient,
				ErrorResilientPartitions: tc.errorResilientParts,
				ScreenContentMode:        tc.screenContentMode,
				Sharpness:                tc.sharpness,
			}
			extraArgs := []string{"--end-usage=cbr", "--rtc-external=1"}
			extraArgs = append(extraArgs, tc.extraArgs...)
			govpxFrames := encodeFramesWithGovpx(t, opts, sources)
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, "rtc-external-"+tc.name, opts, tc.targetKbps, sources, nil, extraArgs)
			assertSegmentByteParity(t, "rtc-external-rate-control-"+tc.name, govpxFrames, libvpxFrames, 0)
		})
	}
}

// TestVP8OracleEncoderStreamByteParityComboAdaptiveKF widens AdaptiveKeyFrames
// strict-parity coverage to the standard sizes the existing matrices skip:
// panning + segmented at 16x16 / 48x48 / 72x40 / 96x96.
//
// The extended matrix already pins panning AdaptiveKeyFrames at
// 16x16 / 32x32 / 48x48 / 64x64; the segmentation matrix pins
// segmented AdaptiveKeyFrames at 32x32 / 64x64 / 128x128. This batch
// fills in the missing sizes: panning at 72x40 / 96x96 (and 16x16
// adds the cpu_used variants the extended matrix does not cover);
// segmented at 16x16 / 48x48 / 72x40 / 96x96.
//
// The panning fixture is smooth so libvpx's scene-cut detector must
// never fire; the segmented fixture is MB-grid-aligned high-contrast
// so scene-cut behavior is the most informative cross-axis to pin.
func TestVP8OracleEncoderStreamByteParityComboAdaptiveKF(t *testing.T) {
	vp8test.RequireOracle(t, "encoder stream byte-parity gate")
	vpxencOracle := vp8test.VpxencOracle(t)

	const (
		fps        = 30
		targetKbps = 700
		frames     = 16
	)

	type fixture struct {
		name   string
		w, h   int
		source func(w, h, i int) Image
	}
	panning16 := fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}
	panning48 := fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}
	panning72 := fixture{name: "panning-72x40", w: 72, h: 40, source: encoderValidationPanningFrame}
	panning96 := fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}
	segmented16 := fixture{name: "segmented-16x16", w: 16, h: 16, source: encoderValidationSegmentedFrame}
	segmented48 := fixture{name: "segmented-48x48", w: 48, h: 48, source: encoderValidationSegmentedFrame}
	segmented72 := fixture{name: "segmented-72x40", w: 72, h: 40, source: encoderValidationSegmentedFrame}
	segmented96 := fixture{name: "segmented-96x96", w: 96, h: 96, source: encoderValidationSegmentedFrame}

	cases := []struct {
		name     string
		deadline Deadline
		cpuUsed  int
		fx       fixture
		limit    int
	}{
		// Panning fixtures: smooth content, no scene cuts expected.
		// CPU sweep at each size to round out the cpu_used axis the
		// extended matrix only covers at 16x16/32x32.
		{name: "adaptive-kf-panning-16x16-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning16},
		{name: "adaptive-kf-panning-16x16-cpu-8", deadline: DeadlineRealtime, cpuUsed: -8, fx: panning16},
		{name: "adaptive-kf-panning-48x48-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning48},
		{name: "adaptive-kf-panning-48x48-cpu4", deadline: DeadlineRealtime, cpuUsed: 4, fx: panning48},
		{name: "adaptive-kf-panning-48x48-cpu8", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning48},
		{name: "adaptive-kf-panning-72x40-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning72},
		{name: "adaptive-kf-panning-72x40-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning72},
		{name: "adaptive-kf-panning-72x40-cpu4", deadline: DeadlineRealtime, cpuUsed: 4, fx: panning72},
		{name: "adaptive-kf-panning-72x40-cpu8", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning72},
		{name: "adaptive-kf-panning-96x96-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning96},
		{name: "adaptive-kf-panning-96x96-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning96},
		{name: "adaptive-kf-panning-96x96-cpu4", deadline: DeadlineRealtime, cpuUsed: 4, fx: panning96},
		{name: "adaptive-kf-panning-96x96-cpu8", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning96},

		// Segmented fixtures: scene cuts may be detected. Pinning the
		// gate matches libvpx's detection behaviour byte-for-byte.
		{name: "adaptive-kf-segmented-16x16-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: segmented16},
		{name: "adaptive-kf-segmented-16x16-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: segmented16},
		{name: "adaptive-kf-segmented-48x48-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: segmented48},
		{name: "adaptive-kf-segmented-48x48-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: segmented48},
		{name: "adaptive-kf-segmented-48x48-cpu8", deadline: DeadlineRealtime, cpuUsed: 8, fx: segmented48},
		{name: "adaptive-kf-segmented-72x40-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: segmented72},
		{name: "adaptive-kf-segmented-72x40-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: segmented72},
		{name: "adaptive-kf-segmented-72x40-cpu8", deadline: DeadlineRealtime, cpuUsed: 8, fx: segmented72},
		{name: "adaptive-kf-segmented-96x96-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: segmented96},
		{name: "adaptive-kf-segmented-96x96-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: segmented96},
		{name: "adaptive-kf-segmented-96x96-cpu8", deadline: DeadlineRealtime, cpuUsed: 8, fx: segmented96},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]Image, frames)
			for i := range sources {
				sources[i] = tc.fx.source(tc.fx.w, tc.fx.h, i)
			}
			opts := EncoderOptions{
				Width:             tc.fx.w,
				Height:            tc.fx.h,
				FPS:               fps,
				RateControlMode:   RateControlCBR,
				TargetBitrateKbps: targetKbps,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				KeyFrameInterval:  999,
				AdaptiveKeyFrames: true,
				Deadline:          tc.deadline,
				CpuUsed:           strictByteParityCPUUsed(tc.deadline, tc.cpuUsed),
				Tuning:            TunePSNR,
			}

			govpxFrames := encodeFramesWithGovpx(t, opts, sources)
			extraArgs := libvpxEndUsageArgs(nil)
			libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, tc.name, opts, targetKbps, sources, extraArgs)

			if len(govpxFrames) != len(libvpxFrames) {
				if tc.limit < 0 {
					t.Logf("frame count mismatch (not asserted, known gap): govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
					assertStrictGateKnownGapMatchedPrefix(t, tc.name, govpxFrames, libvpxFrames, 1)
					return
				}
				t.Fatalf("frame count mismatch: govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
			}

			limit := len(govpxFrames)
			switch {
			case tc.limit < 0:
				limit = 0
			case tc.limit > 0 && tc.limit < limit:
				limit = tc.limit
			}
			for i := 0; i < len(govpxFrames); i++ {
				gHash := sha256.Sum256(govpxFrames[i])
				lHash := sha256.Sum256(libvpxFrames[i])
				gFP, gIsKey := parseVP8FramePartitionSizes(govpxFrames[i])
				lFP, lIsKey := parseVP8FramePartitionSizes(libvpxFrames[i])
				if gHash == lHash {
					t.Logf("frame %d byte MATCH: len=%d first_part=%d keyframe=%t", i, len(govpxFrames[i]), gFP, gIsKey)
					continue
				}
				firstDiff := testutil.FirstByteDiff(govpxFrames[i], libvpxFrames[i])
				firstNonTagDiff := testutil.FirstByteDiff(govpxFrames[i][3:], libvpxFrames[i][3:])
				if firstNonTagDiff >= 0 {
					firstNonTagDiff += 3
				}
				if i >= limit {
					t.Logf("frame %d byte mismatch (not asserted, limit=%d): govpx_len=%d libvpx_len=%d first_diff=%d non_tag_diff=%d govpx_first_part=%d libvpx_first_part=%d",
						i, limit, len(govpxFrames[i]), len(libvpxFrames[i]), firstDiff, firstNonTagDiff, gFP, lFP)
					continue
				}
				t.Errorf("frame %d byte mismatch: govpx_len=%d libvpx_len=%d first_diff=%d govpx_first_part=%d libvpx_first_part=%d govpx_keyframe=%t libvpx_keyframe=%t govpx_sha=%s libvpx_sha=%s",
					i, len(govpxFrames[i]), len(libvpxFrames[i]), firstDiff,
					gFP, lFP, gIsKey, lIsKey,
					hex.EncodeToString(gHash[:8]), hex.EncodeToString(lHash[:8]))
			}
		})
	}
}
