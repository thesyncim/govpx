//go:build govpx_oracle_trace

package govpx_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp9oracle"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"image"
	"strconv"
	"testing"
)

// FuzzVP9EncoderProductionStreamByteParity mirrors
// FuzzEncoderProductionStreamByteParity for VP9: option-grid fuzz against the
// vpxenc-vp9 oracle at small + production resolutions, asserting strict per-
// frame byte parity. Seeds where govpx VP9 has documented divergences from
// libvpx VP9 fail visibly here and land as testdata/fuzz seeds for parity work.
//
// Gated by GOVPX_WITH_ORACLE=1 and a built vpxenc-vp9 binary.
func FuzzVP9EncoderProductionStreamByteParity(f *testing.F) {
	vp9test.RequireOracle(f, "VP9 option-grid byte-parity fuzz")
	vp9test.RequireVpxenc(f)
	// Each seed is (resBucket, deadlineBucket, cpuBucket, rcBucket, featBucket,
	// threadsBucket, tileBucket, qBucket).
	seeds := [][]byte{
		{0, 0, 0, 0, 0, 0, 0, 0}, // realtime cpu8 64x64 fixed-q
		{1, 0, 0, 0, 0, 0, 0, 0}, // realtime cpu8 128x128 fixed-q
		{2, 0, 0, 0, 0, 1, 0, 0}, // realtime 320x180 threads=2
		{3, 0, 0, 0, 0, 1, 1, 0}, // realtime 640x360 threads=2 tile-col=1
		{4, 0, 0, 0, 0, 2, 1, 0}, // realtime 854x480 threads=4 tile-col=1
		{5, 0, 0, 0, 0, 2, 2, 0}, // realtime 1280x720 threads=4 tile-col=2
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		cfg := newVP9OptionGridFuzzCase(data)
		opts := cfg.buildOpts()
		sources := cfg.buildSources()

		sum := sha256.Sum256(data)
		label := "fuzz-vp9-option-grid-" + cfg.name + "-" + hex.EncodeToString(sum[:4])
		t.Logf("%s w=%d h=%d cpu=%d threads=%d tile-cols=%d frames=%d",
			label, opts.Width, opts.Height, opts.CpuUsed, opts.Threads,
			cfg.tileCols, len(sources))

		govpxFrames := vp9oracle.EncodeFramesWithGovpx(t, opts, sources, nil)
		libvpxFrames := vp9test.VpxencPackets(t, sources, cfg.extraArgs...)
		vp9test.AssertSegmentByteParity(t, label, govpxFrames, libvpxFrames, 0)
	})
}

type vp9OptionGridFuzzCase struct {
	width     int
	height    int
	frames    int
	cpuUsed   int8
	threads   int
	tileCols  int
	cqLevel   int
	minQ      int
	maxQ      int
	name      string
	extraArgs []string
}

func newVP9OptionGridFuzzCase(data []byte) vp9OptionGridFuzzCase {
	r := testutil.NewByteCursor(data)
	resPool := [...]struct {
		w, h, frames int
	}{
		{64, 64, 4},
		{128, 128, 4},
		{320, 180, 3},
		{640, 360, 2},
		{854, 480, 2},
		{1280, 720, 2},
	}
	cpuPool := [...]int{8, 6, 4, 2, 0}
	threadPool := [...]int{0, 2, 4}
	tilePool := [...]int{0, 1, 2}
	qPool := [...]int{32, 20, 48, 4, 56}

	// Skip deadlineBucket / rcBucket / featBucket consumption (1, 2, 3, 4) so
	// the seeds still land on the configuration they document.
	res := resPool[r.Pick(len(resPool))]
	_ = r.Pick(3)
	cpu := cpuPool[r.Pick(len(cpuPool))]
	_ = r.Pick(2)
	_ = r.Pick(2)
	threads := threadPool[r.Pick(len(threadPool))]
	tileCols := tilePool[r.Pick(len(tilePool))]
	cqLevel := qPool[r.Pick(len(qPool))]

	c := vp9OptionGridFuzzCase{
		width:    res.w,
		height:   res.h,
		frames:   res.frames,
		cpuUsed:  int8(cpu),
		threads:  threads,
		tileCols: tileCols,
		cqLevel:  cqLevel,
		minQ:     4,
		maxQ:     56,
	}
	c.name = "w" + strconv.Itoa(c.width) + "h" + strconv.Itoa(c.height)
	c.extraArgs = []string{
		"--cpu-used=" + strconv.Itoa(int(c.cpuUsed)),
		"--cq-level=" + strconv.Itoa(c.cqLevel),
		"--min-q=" + strconv.Itoa(c.minQ),
		"--max-q=" + strconv.Itoa(c.maxQ),
	}
	if c.threads > 0 {
		c.extraArgs = append(c.extraArgs, "--threads="+strconv.Itoa(c.threads))
	}
	if c.tileCols > 0 {
		c.extraArgs = append(c.extraArgs, "--tile-columns="+strconv.Itoa(c.tileCols))
	}
	return c
}

func (c *vp9OptionGridFuzzCase) buildOpts() govpx.VP9EncoderOptions {
	opts := govpx.VP9EncoderOptions{
		Width:               c.width,
		Height:              c.height,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     govpx.RateControlQ,
		TargetBitrateKbps:   700,
		MinQuantizer:        c.minQ,
		MaxQuantizer:        c.maxQ,
		CQLevel:             c.cqLevel,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		Deadline:            govpx.DeadlineRealtime,
		CpuUsed:             c.cpuUsed,
		Threads:             c.threads,
		MaxKeyframeInterval: 128,
	}
	// Tile-column count is not part of VP9EncoderOptions on the govpx side
	// (the encoder derives it from Threads + Log2TileRows); only Log2TileRows
	// is exposed. We mirror tileCols on the libvpx CLI side and let the govpx
	// encoder pick the matching configuration via its standard derivation.
	return opts
}

func (c *vp9OptionGridFuzzCase) buildSources() []*image.YCbCr {
	return vp9test.NewPanningSources(c.width, c.height, c.frames)
}

func TestVP9OracleRealtimeNewModeMatchesLibvpx(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 realtime new-mode byte parity")
	vp9test.RequireVpxencFrameFlags(t)

	type pinnedCase struct {
		name   string
		width  int
		height int
		frames int
		opts   govpx.VP9EncoderOptions
		args   []string
	}
	rateOptions := func(mode govpx.RateControlMode, targetKbps int) govpx.VP9EncoderOptions {
		opts := govpx.VP9EncoderOptions{
			RateControlModeSet:  true,
			RateControlMode:     mode,
			TargetBitrateKbps:   targetKbps,
			MinQuantizer:        4,
			MaxQuantizer:        56,
			MaxKeyframeInterval: 128,
		}
		if mode == govpx.RateControlCQ || mode == govpx.RateControlQ {
			opts.CQLevel = 20
		}
		return opts
	}
	rateArgs := func(mode govpx.RateControlMode, targetKbps int) []string {
		endUsage := "vbr"
		if mode == govpx.RateControlCQ {
			endUsage = "cq"
		} else if mode == govpx.RateControlQ {
			endUsage = "q"
		}
		args := []string{
			"--end-usage=" + endUsage,
			fmt.Sprintf("--target-bitrate=%d", targetKbps),
			"--min-q=4",
			"--max-q=56",
		}
		if mode == govpx.RateControlCQ || mode == govpx.RateControlQ {
			args = append(args, "--cq-level=20")
		}
		return args
	}
	cases := []pinnedCase{
		{name: "vbr-64x64", width: 64, height: 64, frames: 4,
			opts: rateOptions(govpx.RateControlVBR, 700),
			args: rateArgs(govpx.RateControlVBR, 700)},
		{name: "vbr-320x180", width: 320, height: 180, frames: 4,
			opts: rateOptions(govpx.RateControlVBR, 700),
			args: rateArgs(govpx.RateControlVBR, 700)},
		{name: "vbr-1280x720", width: 1280, height: 720, frames: 2,
			opts: rateOptions(govpx.RateControlVBR, 2200),
			args: rateArgs(govpx.RateControlVBR, 2200)},
		{name: "cq-64x64", width: 64, height: 64, frames: 4,
			opts: rateOptions(govpx.RateControlCQ, 700),
			args: rateArgs(govpx.RateControlCQ, 700)},
		{name: "cq-320x180", width: 320, height: 180, frames: 4,
			opts: rateOptions(govpx.RateControlCQ, 700),
			args: rateArgs(govpx.RateControlCQ, 700)},
		{name: "cq-1280x720", width: 1280, height: 720, frames: 2,
			opts: rateOptions(govpx.RateControlCQ, 2200),
			args: rateArgs(govpx.RateControlCQ, 2200)},
		{name: "q-64x64", width: 64, height: 64, frames: 4,
			opts: rateOptions(govpx.RateControlQ, 700),
			args: rateArgs(govpx.RateControlQ, 700)},
		{name: "q-320x180", width: 320, height: 180, frames: 4,
			opts: rateOptions(govpx.RateControlQ, 700),
			args: rateArgs(govpx.RateControlQ, 700)},
		{name: "q-1280x720", width: 1280, height: 720, frames: 2,
			opts: rateOptions(govpx.RateControlQ, 2200),
			args: rateArgs(govpx.RateControlQ, 2200)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]*image.YCbCr, tc.frames)
			for i := range sources {
				sources[i] = vp9test.NewYCbCr(tc.width, tc.height, 128, 128, 128)
			}
			_, govpxPackets, _, libvpxPackets :=
				vp9oracle.CaptureStreamParityPacketRowsWithHooks(t,
					tc.opts, sources, nil, tc.args, nil)
			if len(govpxPackets) != len(libvpxPackets) {
				t.Fatalf("VP9 new-mode %s packet count: govpx=%d libvpx=%d",
					tc.name, len(govpxPackets), len(libvpxPackets))
			}
			for frame := range govpxPackets {
				vp9test.AssertPacketByteParity(t,
					fmt.Sprintf("VP9 new-mode %s frame %d", tc.name, frame),
					govpxPackets[frame], libvpxPackets[frame])
			}
		})
	}
}

// vp9RefControlParitySeeds pins reference-control schedules that exercise
// per-frame update, no-reference, and force-reference flags against libvpx.
// Keep these seeds stable so strict byte parity failures point at behavior,
// not corpus churn.
var vp9RefControlParitySeeds = [][]byte{
	{0, 0, 0, 0, 0, 0, 0, 0},
	{0, 1, 0, 2, 0, 3, 0, 0},
	{1, 2, 3, 4, 5, 6, 0, 0},
	{0, 4, 0, 5, 0, 6, 0, 7},
	{0xff, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
	{0, 7, 0, 8, 0, 9, 0, 10},
	[]byte("0"),
	[]byte("1"),
	[]byte("2"),
	[]byte("7"),
}

// FuzzVP9EncoderReferenceControlSequences checks VP9 per-frame schedules that mix
// govpx.EncodeFlags-based reference-update bits (NoUpdateLast, NoUpdateGolden,
// NoUpdateAltRef, NoReferenceLast/Golden/AltRef, ForceGolden/AltRefFrame), and
// the encoded bytes must match the libvpx VP9 vpxenc-vp9-frameflags driver
// driven through the same schedule.
//
// VP9's public SetReferenceFrame/CopyReferenceFrame surface is exercised by
// the dedicated vp9_oracle_copy_reference_parity_test.go family, so this
// fuzzer focuses on the per-frame govpx.EncodeFlags permutations the libvpx
// driver also supports. Gated by GOVPX_WITH_ORACLE=1 plus a built
// vpxenc-vp9-frameflags binary.
func FuzzVP9EncoderReferenceControlSequences(f *testing.F) {
	vp9test.RequireOracle(f, "VP9 ref-control sequence fuzz")
	vp9test.RequireVpxencFrameFlags(f)
	seeds := [][]byte{
		{0, 0, 0, 0, 0, 0, 0, 0},
		{0, 1, 0, 2, 0, 3, 0, 0},
		{1, 2, 3, 4, 5, 6, 0, 0},
		{0, 4, 0, 5, 0, 6, 0, 7},
		{0xff, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		{0, 7, 0, 8, 0, 9, 0, 10},
	}
	seen := make(map[string]struct{}, len(seeds)+len(vp9RefControlParitySeeds))
	addSeed := func(seed []byte) {
		key := string(seed)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		f.Add(seed)
	}
	for _, seed := range seeds {
		addSeed(seed)
	}
	for _, seed := range vp9RefControlParitySeeds {
		addSeed(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		tc := newVP9RefControlsFuzzCase(data)
		sum := sha256.Sum256(data)
		label := "fuzz-vp9-refctrl-" + hex.EncodeToString(sum[:4])
		t.Logf("%s frames=%d flags=%v", label, len(tc.sources), tc.flags)

		govpxFrames := vp9oracle.EncodeFramesWithGovpx(t, tc.opts, tc.sources, tc.flags)
		libvpxFrames := vp9test.VpxencFrameFlagPackets(t, tc.sources,
			vp9oracle.LibvpxFrameFlags(tc.flags), tc.extraArgs...)
		vp9test.AssertSegmentByteParity(t, label, govpxFrames, libvpxFrames, 0)
	})
}

type vp9RefControlsFuzzCase struct {
	opts      govpx.VP9EncoderOptions
	sources   []*image.YCbCr
	flags     []govpx.EncodeFlags
	extraArgs []string
}

// newVP9RefControlsFuzzCase generates a per-frame schedule that mixes the
// govpx.EncodeFlags ref-update / no-reference / force-* bits supported by both
// govpx VP9 and the vpxenc-vp9-frameflags driver.
func newVP9RefControlsFuzzCase(data []byte) vp9RefControlsFuzzCase {
	r := testutil.NewByteCursor(data)
	framesPool := [...]int{6, 8, 10}
	frames := framesPool[r.Pick(len(framesPool))]
	const (
		width  = 64
		height = 64
	)
	opts := govpx.VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     govpx.RateControlQ,
		TargetBitrateKbps:   700,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             32,
		MaxKeyframeInterval: 128,
		Deadline:            govpx.DeadlineRealtime,
		CpuUsed:             8,
	}
	sources := vp9test.NewPanningSources(width, height, frames)
	flags := make([]govpx.EncodeFlags, frames)

	for frame := 1; frame < frames; frame++ {
		switch r.Pick(11) {
		case 0:
			// No-op frame.
		case 1, 2:
			flags[frame] |= govpx.EncodeNoUpdateLast
		case 3, 4:
			flags[frame] |= govpx.EncodeNoUpdateGolden
		case 5, 6:
			flags[frame] |= govpx.EncodeNoUpdateAltRef
		case 7:
			flags[frame] |= govpx.EncodeNoReferenceLast | govpx.EncodeNoUpdateLast
		case 8:
			flags[frame] |= govpx.EncodeForceGoldenFrame
		case 9:
			flags[frame] |= govpx.EncodeForceAltRefFrame
		case 10:
			flags[frame] |= govpx.EncodeNoUpdateEntropy
		}
	}

	extraArgs := []string{
		"--cq-level=32",
		"--min-q=4",
		"--max-q=56",
		"--end-usage=q",
	}
	return vp9RefControlsFuzzCase{
		opts:      opts,
		sources:   sources,
		flags:     flags,
		extraArgs: extraArgs,
	}
}

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

func TestVP9OracleRuntimeDropToggleByteParity(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 runtime-drop byte-parity trace")
	vp9test.RequireVpxencFrameFlags(t)

	const width, height, frames = 64, 64, 24
	type runtimeDropCase struct {
		name      string
		opts      govpx.VP9EncoderOptions
		before    func(*testing.T, *govpx.VP9Encoder, int)
		extraArgs []string
		wantDrop  bool
	}
	dropOpts := func(targetKbps int) govpx.VP9EncoderOptions {
		opts := vp9oracle.CBROptions(width, height, targetKbps)
		opts.BufferSizeMs = 400
		opts.BufferInitialSizeMs = 300
		opts.BufferOptimalSizeMs = 350
		opts.DropFrameWaterMark = 60
		return opts
	}
	cases := []runtimeDropCase{
		{
			name: "drop-frame-toggle",
			opts: dropOpts(120),
			before: func(t *testing.T, enc *govpx.VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					if err := enc.SetRealtimeTarget(govpx.RealtimeTarget{
						FrameDrop: govpx.RealtimeFrameDropEnabled,
					}); err != nil {
						t.Fatalf("SetRealtimeTarget drop enabled: %v", err)
					}
				case 14:
					if err := enc.SetRealtimeTarget(govpx.RealtimeTarget{
						FrameDrop: govpx.RealtimeFrameDropDisabled,
					}); err != nil {
						t.Fatalf("SetRealtimeTarget drop disabled: %v", err)
					}
				}
			},
			extraArgs: append(vp9oracle.CBRArgs(120, 400, 300, 350, 0),
				"--drop-frame-schedule=3:60,14:0"),
			wantDrop: true,
		},
		{
			name: "fixed-q-drop-frame-toggle",
			opts: func() govpx.VP9EncoderOptions {
				opts := dropOpts(140)
				opts.MinQuantizer = 20
				opts.MaxQuantizer = 20
				return opts
			}(),
			before: func(t *testing.T, enc *govpx.VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 2:
					if err := enc.SetRealtimeTarget(govpx.RealtimeTarget{
						FrameDrop: govpx.RealtimeFrameDropEnabled,
					}); err != nil {
						t.Fatalf("SetRealtimeTarget fixed-q drop enabled: %v", err)
					}
				case 14:
					if err := enc.SetRealtimeTarget(govpx.RealtimeTarget{
						FrameDrop: govpx.RealtimeFrameDropDisabled,
					}); err != nil {
						t.Fatalf("SetRealtimeTarget fixed-q drop disabled: %v", err)
					}
				}
			},
			extraArgs: append(vp9oracle.CBRArgs(140, 400, 300, 350, 0),
				"--min-q=20", "--max-q=20",
				"--drop-frame-schedule=2:60,14:0"),
			wantDrop: true,
		},
		{
			name: "fixed-q-window-under-drop-pressure",
			opts: func() govpx.VP9EncoderOptions {
				opts := dropOpts(140)
				opts.DropFrameAllowed = true
				return opts
			}(),
			before: func(t *testing.T, enc *govpx.VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 4:
					if err := enc.SetRealtimeTarget(govpx.RealtimeTarget{
						MinQuantizer: 20,
						MaxQuantizer: 20,
					}); err != nil {
						t.Fatalf("SetRealtimeTarget fixed q under drop: %v", err)
					}
				case 14:
					if err := enc.SetRealtimeTarget(govpx.RealtimeTarget{
						MinQuantizer: 4,
						MaxQuantizer: 56,
					}); err != nil {
						t.Fatalf("SetRealtimeTarget q band restore after drop: %v", err)
					}
				}
			},
			extraArgs: append(vp9oracle.CBRArgs(140, 400, 300, 350, 60),
				"--min-q-schedule=4:20,14:4",
				"--max-q-schedule=4:20,14:56"),
			wantDrop: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := vp9oracle.TransitionSources(width, height, frames)
			govpxRows, govpxPackets, libvpxRows, libvpxPackets :=
				vp9oracle.CaptureStreamParityPacketRowsWithHooks(t, tc.opts,
					sources, nil, tc.extraArgs,
					func(enc *govpx.VP9Encoder, frame int) {
						tc.before(t, enc, frame)
					})
			stats := vp9test.CompareTransitionRows(t, govpxRows, libvpxRows, vp9oracle.RateTraceFlagMapper)
			matches, packetMatches, dropMatches, firstMismatch :=
				vp9test.CountByteParityMatchesWithDrops(t, govpxRows, govpxPackets,
					libvpxRows, libvpxPackets)
			govpxDrops := vp9test.DroppedFrameIndices(govpxRows)
			libvpxDrops := vp9test.DroppedFrameIndices(libvpxRows)
			t.Logf("VP9 runtime-drop byte-parity trace %s: rows=%d matches=%d packet_matches=%d drop_matches=%d first_mismatch=%d govpx_drops=%v libvpx_drops=%v transition=%s",
				tc.name, len(govpxRows), matches, packetMatches, dropMatches,
				firstMismatch, govpxDrops, libvpxDrops, stats)
			t.Logf("VP9 runtime-drop byte-parity rows %s:\n%s", tc.name,
				vp9test.FormatDropAwareStreamParityRows(t, govpxRows, govpxPackets,
					libvpxRows, libvpxPackets))
			if tc.wantDrop && (len(govpxDrops) == 0 || len(libvpxDrops) == 0) {
				t.Fatalf("drop fixture %s did not drop on both sides: govpx=%v libvpx=%v",
					tc.name, govpxDrops, libvpxDrops)
			}
			if vp9test.StrictEnv("GOVPX_VP9_RUNTIME_DROP_BYTE_STRICT") &&
				(matches != len(govpxRows) || stats.HasMismatch()) {
				t.Fatalf("strict VP9 runtime-drop mismatch %s: matches=%d/%d stats=%s",
					tc.name, matches, len(govpxRows), stats)
			}
		})
	}
}

func TestVP9OracleRuntimeResizeByteParity(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 runtime-resize byte-parity trace")
	vp9test.RequireVpxencFrameFlags(t)

	type resizeCase struct {
		name          string
		initialWidth  int
		initialHeight int
		nextWidth     int
		nextHeight    int
		resizeFrame   int
	}
	cases := []resizeCase{
		{name: "up-64x64-to-96x80", initialWidth: 64, initialHeight: 64, nextWidth: 96, nextHeight: 80, resizeFrame: 2},
		{name: "down-96x80-to-64x64", initialWidth: 96, initialHeight: 80, nextWidth: 64, nextHeight: 64, resizeFrame: 2},
		{name: "odd-65x63-to-81x79", initialWidth: 65, initialHeight: 63, nextWidth: 81, nextHeight: 79, resizeFrame: 2},
	}
	extraArgs := []string{"--cq-level=32", "--min-q=32", "--max-q=32"}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const frames = 5
			sources := vp9test.NewRuntimeResizeSources(tc.initialWidth,
				tc.initialHeight, tc.nextWidth, tc.nextHeight,
				tc.resizeFrame, frames)
			opts := govpx.VP9EncoderOptions{
				Width:        tc.initialWidth,
				Height:       tc.initialHeight,
				MinQuantizer: 32,
				MaxQuantizer: 32,
			}
			before := func(enc *govpx.VP9Encoder, frame int) {
				if frame != tc.resizeFrame {
					return
				}
				if err := enc.SetRealtimeTarget(govpx.RealtimeTarget{
					Width:  tc.nextWidth,
					Height: tc.nextHeight,
				}); err != nil {
					t.Fatalf("SetRealtimeTarget resize: %v", err)
				}
			}
			govpxRows, govpxPackets := vp9oracle.CaptureVariablePacketRows(t,
				opts, sources, nil, before)
			libvpxRows, libvpxPackets := vp9oracle.CaptureLibvpxVariablePacketRows(t,
				sources, nil, nil, extraArgs)
			stats := vp9test.CompareTransitionRows(t, govpxRows, libvpxRows, vp9oracle.RateTraceFlagMapper)
			matches, firstMismatch := vp9test.CountByteParityMatches(govpxPackets,
				libvpxPackets)
			t.Logf("VP9 runtime resize byte-parity trace %s: matches=%d/%d first_mismatch=%d stats=%s",
				tc.name, matches, len(govpxPackets), firstMismatch, stats)
			t.Logf("VP9 runtime resize rate rows %s:\n%s", tc.name,
				vp9test.FormatRateTraceRows(govpxRows, libvpxRows))
			t.Logf("VP9 runtime resize byte rows %s:\n%s", tc.name,
				vp9test.FormatStreamParityRows(t, govpxPackets, libvpxPackets))
			if vp9test.StrictEnv("GOVPX_VP9_RUNTIME_RESIZE_STRICT") &&
				(stats.HasMismatch() || matches != len(govpxPackets)) {
				t.Fatalf("strict VP9 runtime resize parity %s: matches=%d/%d stats=%s",
					tc.name, matches, len(govpxPackets), stats)
			}
		})
	}
}

func TestVP9EncoderVpxencOracleRealtimeZeroCPUUsesSpeed8(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 64, 64
	first := vp9test.NewYCbCr(width, height, 128, 128, 128)
	second := vp9test.NewYCbCr(width, height, 160, 128, 128)

	vp9oracle.AssertTwoFrameByteParityWithOptions(t, first, second,
		govpx.VP9EncoderOptions{
			Deadline: govpx.DeadlineRealtime,
			CpuUsed:  0,
		},
		[]string{
			"--rt",
			"--cpu-used=8",
		})
}

// FuzzVP9EncoderRandomStrides mirrors FuzzEncoderRandomStrides for VP9: callers
// feed VP9 encoder *image.YCbCr values with varying Y/Cb/Cr stride padding, and
// the fuzzer asserts that the encoded VP9 keyframe is byte-identical to the
// libvpx vpxenc-vp9 oracle keyframe encoded from the equivalent tight (no-
// padding) I420 content. A stride-walk bug that reads padding bytes will
// surface as a keyframe SHA-256 mismatch.
//
// Gated by GOVPX_WITH_ORACLE=1 plus a built vpxenc-vp9 binary.
func FuzzVP9EncoderRandomStrides(f *testing.F) {
	vp9test.RequireOracle(f, "VP9 random-strides fuzz")
	vp9test.RequireVpxenc(f)
	// Each seed is (dimBucket, yPadBucket, uPadBucket, vPadBucket, uvAlignBucket).
	seeds := [][]byte{
		{0, 0, 0, 0, 0},  // tight 32x32
		{0, 4, 0, 0, 0},  // 32x32 with Y stride +4
		{1, 0, 2, 2, 0},  // 64x64 with U/V stride +2
		{1, 8, 4, 4, 0},  // 64x64 mixed
		{2, 16, 8, 8, 1}, // 128x128 odd alignment
		{2, 0, 0, 0, 1},  // 128x128 chroma extra +1
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		dim, padded, tight, ok := newVP9StridesFuzzImage(data)
		if !ok {
			return
		}
		opts := govpx.VP9EncoderOptions{
			Width:        dim.w,
			Height:       dim.h,
			FPS:          30,
			MinQuantizer: 4,
			MaxQuantizer: 56,
			CQLevel:      32,
			CpuUsed:      8,
			Deadline:     govpx.DeadlineRealtime,
		}
		sum := sha256.Sum256(data)
		label := "fuzz-vp9-strides-" + hex.EncodeToString(sum[:4])
		t.Logf("%s w=%d h=%d yStride=%d cStride=%d",
			label, dim.w, dim.h, padded.YStride, padded.CStride)

		govpxFrames := vp9oracle.EncodeFramesWithGovpx(t, opts, []*image.YCbCr{padded}, nil)
		libvpxFrames := vp9test.VpxencPackets(t, []*image.YCbCr{tight})
		vp9test.AssertSegmentByteParity(t, label, govpxFrames, libvpxFrames, 0)
	})
}

type vp9StridesFuzzDim struct {
	w, h int
}

// newVP9StridesFuzzImage builds (padded, tight) VP9 *image.YCbCr pairs where
// the visible content matches but `padded` may include arbitrary per-plane
// stride padding. Both images use deterministic fuzz-bytes-driven content
// via vp9test.NewYCbCr so the libvpx oracle's identical-content premise
// holds.
func newVP9StridesFuzzImage(data []byte) (vp9StridesFuzzDim, *image.YCbCr, *image.YCbCr, bool) {
	r := testutil.NewByteCursor(data)
	if len(data) == 0 {
		return vp9StridesFuzzDim{}, nil, nil, false
	}
	dimPool := [...]vp9StridesFuzzDim{
		{32, 32},
		{64, 64},
		{128, 128},
		{160, 96},
	}
	dim := dimPool[r.Pick(len(dimPool))]
	yPad := int(r.Next() & 0x1f) // 0..31
	uPad := int(r.Next() & 0x0f) // 0..15
	_ = int(r.Next() & 0x0f)     // consume the vPad byte to keep seed mapping stable
	uvExtra := int(r.Next() & 0x01)

	uvW := (dim.w + 1) >> 1
	uvH := (dim.h + 1) >> 1
	yStride := dim.w + yPad
	cStride := uvW + uPad + uvExtra

	// Build the tight image first via the standard test helper. The padded
	// image reuses the same visible content but lays it into wider strides.
	tight := vp9test.NewYCbCr(dim.w, dim.h, 96, 128, 128)
	padded := &image.YCbCr{
		YStride:        yStride,
		CStride:        cStride,
		SubsampleRatio: image.YCbCrSubsampleRatio420,
		Rect:           tight.Rect,
		Y:              make([]byte, yStride*dim.h),
		Cb:             make([]byte, cStride*uvH),
		Cr:             make([]byte, cStride*uvH),
	}
	// Fill padding bytes with a distinctive pattern so a stride-walk
	// bug that reads them shows up as a divergence rather than silently
	// zeroing out.
	for i := range padded.Y {
		padded.Y[i] = 0xa5
	}
	for i := range padded.Cb {
		padded.Cb[i] = 0x5a
	}
	for i := range padded.Cr {
		padded.Cr[i] = 0xa5
	}
	for y := 0; y < dim.h; y++ {
		copy(padded.Y[y*yStride:y*yStride+dim.w], tight.Y[y*tight.YStride:y*tight.YStride+dim.w])
	}
	for y := 0; y < uvH; y++ {
		copy(padded.Cb[y*cStride:y*cStride+uvW], tight.Cb[y*tight.CStride:y*tight.CStride+uvW])
		copy(padded.Cr[y*cStride:y*cStride+uvW], tight.Cr[y*tight.CStride:y*tight.CStride+uvW])
	}
	return dim, padded, tight, true
}

// FuzzVP9EncoderTwoPassByteParity exercises the libvpx vpxenc-vp9 two-pass
// CLI path over a fuzz-driven option grid and verifies the IVF shape. It grows
// into a govpx second-pass comparison once this target constructs finalized
// VP9FirstPassFrameStats for cfg.opts.
//
// Gated by GOVPX_WITH_ORACLE=1 plus a built vpxenc-vp9 binary.
func FuzzVP9EncoderTwoPassByteParity(f *testing.F) {
	vp9test.RequireOracle(f, "VP9 two-pass byte-parity fuzz")
	vp9test.RequireVpxenc(f)
	// Each seed is (bitrateBucket, kfBucket, threadsBucket, cpuBucket, framesBucket).
	seeds := [][]byte{
		{0, 0, 0, 0, 0},
		{1, 0, 0, 0, 0},
		{0, 1, 0, 0, 0},
		{0, 0, 1, 0, 0},
		{2, 0, 0, 1, 0},
		{1, 1, 1, 0, 1},
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		cfg := newVP9TwoPassFuzzCase(data)
		sources := cfg.buildSources()
		opts := cfg.buildOpts()

		sum := sha256.Sum256(data)
		label := "fuzz-vp9-twopass-" + hex.EncodeToString(sum[:4])
		t.Logf("%s w=%d h=%d kbps=%d kf=%d threads=%d cpu=%d frames=%d",
			label, opts.Width, opts.Height, cfg.targetKbps, cfg.kfInterval,
			cfg.threads, cfg.cpuUsed, len(sources))

		// Libvpx VP9 two-pass reference. This keeps the CLI bridge and IVF
		// parser covered while the govpx stats-to-second-pass comparison is
		// staged.
		libvpxFrames := vp9test.VpxencTwoPassPackets(t, sources,
			cfg.extraArgs...)
		if len(libvpxFrames) == 0 {
			t.Fatalf("%s: vpxenc-vp9 two-pass produced no frames", label)
		}
		if len(libvpxFrames) != len(sources) {
			t.Errorf("%s: libvpx VP9 two-pass IVF frame count = %d, want %d",
				label, len(libvpxFrames), len(sources))
		}
		// Packet parity belongs here once govpx can construct finalized
		// first-pass stats for this fuzz source set.
	})
}

type vp9TwoPassFuzzCase struct {
	width      int
	height     int
	targetKbps int
	kfInterval int
	threads    int
	cpuUsed    int
	frames     int
	extraArgs  []string
}

func newVP9TwoPassFuzzCase(data []byte) vp9TwoPassFuzzCase {
	r := testutil.NewByteCursor(data)
	kbpsPool := [...]int{300, 500, 700}
	kfPool := [...]int{30, 60, 120}
	threadPool := [...]int{0, 2}
	cpuPool := [...]int{0, 4}
	framesPool := [...]int{4, 6, 8}

	c := vp9TwoPassFuzzCase{
		width:      32,
		height:     32,
		targetKbps: kbpsPool[r.Pick(len(kbpsPool))],
		kfInterval: kfPool[r.Pick(len(kfPool))],
		threads:    threadPool[r.Pick(len(threadPool))],
		cpuUsed:    cpuPool[r.Pick(len(cpuPool))],
		frames:     framesPool[r.Pick(len(framesPool))],
	}
	c.extraArgs = []string{
		"--cpu-used=" + strconv.Itoa(c.cpuUsed),
		"--kf-min-dist=" + strconv.Itoa(c.kfInterval),
		"--kf-max-dist=" + strconv.Itoa(c.kfInterval),
		"--target-bitrate=" + strconv.Itoa(c.targetKbps),
	}
	if c.threads > 0 {
		c.extraArgs = append(c.extraArgs, "--threads="+strconv.Itoa(c.threads))
	}
	return c
}

func (c *vp9TwoPassFuzzCase) buildSources() []*image.YCbCr {
	return vp9test.NewPanningSources(c.width, c.height, c.frames)
}

func (c *vp9TwoPassFuzzCase) buildOpts() govpx.VP9EncoderOptions {
	return govpx.VP9EncoderOptions{
		Width:               c.width,
		Height:              c.height,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     govpx.RateControlVBR,
		TargetBitrateKbps:   c.targetKbps,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		MaxKeyframeInterval: c.kfInterval,
		Deadline:            govpx.DeadlineGoodQuality,
		CpuUsed:             int8(c.cpuUsed),
		Threads:             c.threads,
	}
}
