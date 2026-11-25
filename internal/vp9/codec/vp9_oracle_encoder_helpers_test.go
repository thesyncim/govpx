//go:build govpx_oracle_trace

package codec

import (
	"bytes"
	"fmt"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"image"
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

func captureVP9StreamParityPackets(t *testing.T, opts VP9EncoderOptions,
	sources []*image.YCbCr, flags []EncodeFlags, extraArgs []string,
) ([][]byte, [][]byte) {
	t.Helper()
	return captureVP9StreamParityPacketsWithHooks(t, opts, sources, flags,
		extraArgs, nil)
}

func captureVP9StreamParityPacketsWithHooks(t *testing.T, opts VP9EncoderOptions,
	sources []*image.YCbCr, flags []EncodeFlags, extraArgs []string,
	beforeFrame func(*VP9Encoder, int),
) ([][]byte, [][]byte) {
	t.Helper()
	return captureVP9StreamParityPacketsWithFrameHooks(t, opts, sources,
		flags, extraArgs, beforeFrame, nil)
}

func captureVP9StreamParityPacketsWithFrameHooks(t *testing.T, opts VP9EncoderOptions,
	sources []*image.YCbCr, flags []EncodeFlags, extraArgs []string,
	beforeFrame func(*VP9Encoder, int), afterFrame func(*VP9Encoder, int),
) ([][]byte, [][]byte) {
	t.Helper()
	if len(sources) == 0 {
		t.Fatal("empty VP9 stream parity source")
	}
	if len(flags) > len(sources) {
		t.Fatalf("VP9 stream parity flag count = %d, want <= %d",
			len(flags), len(sources))
	}
	width := sources[0].Rect.Dx()
	height := sources[0].Rect.Dy()
	for i, src := range sources {
		if src.Rect.Dx() != width || src.Rect.Dy() != height {
			t.Fatalf("source %d dimension mismatch: got %dx%d want %dx%d",
				i, src.Rect.Dx(), src.Rect.Dy(), width, height)
		}
	}

	opts.Width = width
	opts.Height = height
	enc, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dstSize, err := vp9AllocatingEncodeBufferSize(width, height)
	if err != nil {
		t.Fatalf("vp9AllocatingEncodeBufferSize: %v", err)
	}
	dst := make([]byte, dstSize)
	govpxPackets := make([][]byte, len(sources))
	for i, src := range sources {
		if beforeFrame != nil {
			beforeFrame(enc, i)
		}
		var f EncodeFlags
		if i < len(flags) {
			f = flags[i]
		}
		if f&EncodeInvisibleFrame != 0 {
			t.Fatalf("frame %d uses EncodeInvisibleFrame, which has no VP9 libvpx flag bit", i)
		}
		result, err := enc.EncodeIntoWithFlagsResult(src, dst, f)
		if err != nil {
			t.Fatalf("EncodeIntoWithFlagsResult frame %d: %v", i, err)
		}
		if result.Dropped {
			t.Fatalf("EncodeIntoWithFlagsResult frame %d unexpectedly dropped", i)
		}
		if afterFrame != nil {
			afterFrame(enc, i)
		}
		govpxPackets[i] = append([]byte(nil), result.Data...)
	}

	libvpxPackets := vp9test.VpxencFrameFlagPackets(t, sources,
		vp9LibvpxFrameFlags(flags), extraArgs...)
	return govpxPackets, libvpxPackets
}

func TestVP9OracleStreamSelectedThreadedAndActiveMapCasesMatchLibvpx(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 selected threaded and active-map byte parity")
	vp9test.RequireVpxencFrameFlags(t)

	type selectedCase struct {
		name        string
		width       int
		height      int
		frames      int
		opts        VP9EncoderOptions
		flags       []EncodeFlags
		extraArgs   []string
		source      func(width, height, frame int) *image.YCbCr
		before      func(*testing.T, *VP9Encoder, int)
		exactPrefix int
		strictBytes bool
		tileJobs    int
	}
	fixedQOpts := VP9EncoderOptions{
		MinQuantizer: 20,
		MaxQuantizer: 20,
	}
	fixedQArgs := []string{
		"--cq-level=20",
		"--min-q=20",
		"--max-q=20",
		"--disable-warning-prompt",
	}
	threadedFixedQOpts := fixedQOpts
	threadedFixedQOpts.Threads = 4
	threadedFixedQArgs := append([]string{"--tile-columns=2"}, fixedQArgs...)
	steppedSource := func(width, height, frame int) *image.YCbCr {
		return vp9test.NewYCbCr(width, height, uint8(96+frame*8), 128, 128)
	}

	cases := []selectedCase{
		{
			name:        "fixed-q-threaded-stepped-720p",
			width:       1280,
			height:      720,
			frames:      2,
			opts:        threadedFixedQOpts,
			extraArgs:   threadedFixedQArgs,
			exactPrefix: 1,
			tileJobs:    4,
			source:      steppedSource,
		},
		{
			name:        "fixed-q-threaded-force-key-stepped-720p",
			width:       1280,
			height:      720,
			frames:      2,
			opts:        threadedFixedQOpts,
			flags:       vp9OracleRepeatAllFramesFlag(2, EncodeForceKeyFrame),
			extraArgs:   threadedFixedQArgs,
			exactPrefix: 2,
			strictBytes: true,
			tileJobs:    4,
			source:      steppedSource,
		},
		{
			name:        "fixed-q-threaded-block-checker-keyframe-720p",
			width:       1280,
			height:      720,
			frames:      1,
			opts:        threadedFixedQOpts,
			extraArgs:   threadedFixedQArgs,
			exactPrefix: 1,
			strictBytes: true,
			tileJobs:    4,
			source:      vp9test.NewBlockCheckerYCbCr,
		},
		{
			name:      "active-map-fixed-q-constant-320",
			width:     320,
			height:    180,
			frames:    2,
			opts:      fixedQOpts,
			extraArgs: append(fixedQArgs, "--control-script=-,active:checker"),
			before: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				if frame != 1 {
					return
				}
				activeMap, rows, cols := vp9test.ActiveMap(320, 180, "checker")
				mustVP9Runtime(t, "SetActiveMap checker",
					enc.SetActiveMap(activeMap, rows, cols))
			},
			exactPrefix: 2,
			strictBytes: true,
			source: func(width, height, frame int) *image.YCbCr {
				return vp9test.NewYCbCr(width, height, 128, 128, 128)
			},
		},
		{
			name:   "control-cross-threaded-ref-refresh",
			width:  1024,
			height: 64,
			frames: 6,
			opts: VP9EncoderOptions{
				Threads: 4,
			},
			flags:       vp9OracleRefRefreshTransitions(6),
			extraArgs:   []string{"--tile-columns=2"},
			source:      steppedSource,
			exactPrefix: 3,
			tileJobs:    4,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]*image.YCbCr, tc.frames)
			source := tc.source
			if source == nil {
				source = func(width, height, frame int) *image.YCbCr {
					return vp9test.NewYCbCr(width, height, 128, 128, 128)
				}
			}
			for i := range sources {
				sources[i] = source(tc.width, tc.height, i)
			}
			var beforeFrame func(*VP9Encoder, int)
			var afterFrame func(*VP9Encoder, int)
			if tc.tileJobs > 0 || tc.before != nil {
				beforeFrame = func(enc *VP9Encoder, frame int) {
					if tc.tileJobs > 0 {
						resetVP9OracleThreadedTileJobsForTest(enc)
					}
					if tc.before != nil {
						tc.before(t, enc, frame)
					}
				}
			}
			if tc.tileJobs > 0 {
				afterFrame = func(enc *VP9Encoder, frame int) {
					assertVP9OracleThreadedTileWriterUsed(t, enc, frame, tc.tileJobs)
				}
			}
			govpxPackets, libvpxPackets := captureVP9StreamParityPacketsWithFrameHooks(t,
				tc.opts, sources, tc.flags, tc.extraArgs, beforeFrame, afterFrame)
			matches, firstMismatch := vp9test.CountByteParityMatches(govpxPackets,
				libvpxPackets)
			t.Logf("VP9 selected stream threaded/active-map gate %s: matches=%d/%d first_mismatch=%d exact_prefix=%d",
				tc.name, matches, len(govpxPackets), firstMismatch, tc.exactPrefix)
			for frame := 0; frame < tc.exactPrefix; frame++ {
				vp9test.AssertPacketByteParity(t,
					fmt.Sprintf("%s frame %d", tc.name, frame),
					govpxPackets[frame], libvpxPackets[frame])
			}
			if tc.strictBytes && matches != len(govpxPackets) {
				t.Fatalf("strict VP9 selected stream threaded/active-map byte parity %s: matches=%d/%d",
					tc.name, matches, len(govpxPackets))
			}
		})
	}
}

func TestVP9EncoderVpxdecOracleMatchesQuarterPelMotion(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 128, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := vp9test.NewMotionYCbCr(width, height)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := predictedVP9ReferenceYCbCrForTest(t,
		e.refFrames[0].img, vp9dec.MV{Col: 58})
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	assertVP9EncoderVpxdecI420Match(t, width, height, key, inter)
}

func TestVP9EncoderVpxdecOracleMatchesEighthPelMotion(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 128, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := vp9test.NewMotionYCbCr(width, height)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := predictedVP9ReferenceYCbCrForTest(t,
		e.refFrames[0].img, vp9dec.MV{Col: 57})
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	assertVP9EncoderVpxdecI420Match(t, width, height, key, inter)
}

// TestVP9EncoderVpxdecOracleAcceptsInterSkipFrame covers the first
// non-intra inter tile shape the public decoder now parses: one
// LAST/ZeroMv skipped block referencing the prior keyframe.
func TestVP9EncoderVpxdecOracleAcceptsInterSkipFrame(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9test.StubPacket(t, 64, 64, 0, common.DcPred)
	inter := vp9InterSkipFrameForTest(t, 64, 64)
	vp9test.VpxdecAccepts(t, "the inter skip frame", 64, 64, key, inter)
}

func TestVP9EncoderVpxencFrameFlagsRepeatNoReferenceAllModeTxShape(t *testing.T) {
	vp9test.RequireVpxencFrameFlags(t)

	cases := []struct {
		name   string
		width  int
		height int
		frames int
		source func(width, height, frame int) *image.YCbCr
	}{
		{
			name:   "constant-64x64",
			width:  64,
			height: 64,
			frames: 3,
			source: func(width, height, frame int) *image.YCbCr {
				return vp9test.NewYCbCr(width, height, uint8(96+frame*8), 128, 128)
			},
		},
		{
			name:   "stepped-320x180",
			width:  320,
			height: 180,
			frames: 4,
			source: func(width, height, frame int) *image.YCbCr {
				return vp9test.NewYCbCr(width, height, uint8(96+frame*8), 128, 128)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]*image.YCbCr, tc.frames)
			for i := range sources {
				sources[i] = tc.source(tc.width, tc.height, i)
			}
			flags := make([]EncodeFlags, tc.frames)
			for i := 1; i < tc.frames; i++ {
				flags[i] = EncodeNoReferenceLast | EncodeNoReferenceGolden |
					EncodeNoReferenceAltRef
			}

			e, err := NewVP9Encoder(VP9EncoderOptions{
				Width:  tc.width,
				Height: tc.height,
			})
			if err != nil {
				t.Fatalf("NewVP9Encoder: %v", err)
			}
			dstSize, err := vp9AllocatingEncodeBufferSize(tc.width, tc.height)
			if err != nil {
				t.Fatalf("vp9AllocatingEncodeBufferSize: %v", err)
			}
			dst := make([]byte, dstSize)
			govpxPackets := make([][]byte, tc.frames)
			for i, src := range sources {
				result, err := e.EncodeIntoWithFlagsResult(src, dst, flags[i])
				if err != nil {
					t.Fatalf("EncodeIntoWithFlagsResult frame %d: %v", i, err)
				}
				if result.Dropped {
					t.Fatalf("EncodeIntoWithFlagsResult frame %d unexpectedly dropped", i)
				}
				govpxPackets[i] = append([]byte(nil), result.Data...)
			}

			libvpxPackets := vp9test.VpxencFrameFlagPackets(t, sources,
				vp9LibvpxFrameFlags(flags))

			govpxGrids := decodeVP9SequenceMiGridsForOracleTest(t, govpxPackets)
			libvpxGrids := decodeVP9SequenceMiGridsForOracleTest(t, libvpxPackets)
			totalShape, matchedShape := 0, 0
			totalMode, matchedMode := 0, 0
			totalTx, matchedTx := 0, 0
			totalSkip, matchedSkip := 0, 0
			totalRef, matchedRef := 0, 0
			for frameIdx := 1; frameIdx < tc.frames; frameIdx++ {
				gGrid := govpxGrids[frameIdx]
				lGrid := libvpxGrids[frameIdx]
				if len(gGrid) != len(lGrid) {
					t.Fatalf("frame %d MI grid length mismatch: govpx=%d libvpx=%d",
						frameIdx, len(gGrid), len(lGrid))
				}
				firstMismatch := -1
				for miIdx := range gGrid {
					g := gGrid[miIdx]
					l := lGrid[miIdx]
					shapeMatch := g.SbType == l.SbType && g.Mode == l.Mode &&
						g.TxSize == l.TxSize && g.Skip == l.Skip &&
						g.RefFrame == l.RefFrame
					totalShape++
					if shapeMatch {
						matchedShape++
					} else if firstMismatch < 0 {
						firstMismatch = miIdx
					}
					totalMode++
					if g.Mode == l.Mode {
						matchedMode++
					}
					totalTx++
					if g.TxSize == l.TxSize {
						matchedTx++
					}
					totalSkip++
					if g.Skip == l.Skip {
						matchedSkip++
					}
					totalRef++
					if g.RefFrame == l.RefFrame {
						matchedRef++
					}
				}
				gFirst, gLast := firstLastVP9MiForOracleTest(gGrid)
				lFirst, lLast := firstLastVP9MiForOracleTest(lGrid)
				firstByteDiff := testutil.FirstByteDiff(govpxPackets[frameIdx],
					libvpxPackets[frameIdx])
				t.Logf("VP9 repeat no-reference-all mode/tx frame %d: first_shape_mismatch=%d first_byte_diff=%d govpx_bytes=%d libvpx_bytes=%d govpx_first=%+v govpx_last=%+v libvpx_first=%+v libvpx_last=%+v",
					frameIdx, firstMismatch, firstByteDiff,
					len(govpxPackets[frameIdx]), len(libvpxPackets[frameIdx]),
					gFirst, gLast, lFirst, lLast)
			}
			t.Logf("VP9 repeat no-reference-all mode/tx trace: shape=%d/%d mode=%d/%d tx=%d/%d skip=%d/%d ref=%d/%d",
				matchedShape, totalShape, matchedMode, totalMode, matchedTx, totalTx,
				matchedSkip, totalSkip, matchedRef, totalRef)
			if matchedShape != totalShape {
				t.Fatalf("VP9 no-reference-all mode/tx shape matched %d/%d",
					matchedShape, totalShape)
			}
		})
	}
}

func TestVP9EncoderVpxencOracleInterModeDistributionParity(t *testing.T) {
	vp9test.RequireVpxencFrameFlags(t)

	const width, height, frames = 64, 64, 6
	sources := make([]*image.YCbCr, frames)
	for i := range sources {
		sources[i] = vp9test.NewPanningYCbCr(width, height, i)
	}
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:  width,
		Height: height,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dstSize, err := vp9AllocatingEncodeBufferSize(width, height)
	if err != nil {
		t.Fatalf("vp9AllocatingEncodeBufferSize: %v", err)
	}
	dst := make([]byte, dstSize)
	govpxPackets := make([][]byte, frames)
	for i, src := range sources {
		result, err := e.EncodeIntoWithResult(src, dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", i, err)
		}
		if result.Dropped {
			t.Fatalf("EncodeIntoWithResult frame %d unexpectedly dropped", i)
		}
		govpxPackets[i] = append([]byte(nil), result.Data...)
	}

	libvpxPackets := vp9test.VpxencFrameFlagPackets(t, sources, nil)

	govpxGrids := decodeVP9SequenceMiGridsForOracleTest(t, govpxPackets)
	libvpxGrids := decodeVP9SequenceMiGridsForOracleTest(t, libvpxPackets)
	var totalModeDistance, totalBlockDistance, totalSkipDistance int
	for i := range govpxGrids {
		g := collectVP9ModeDistribution(govpxGrids[i])
		l := collectVP9ModeDistribution(libvpxGrids[i])
		modeDistance := vp9ModeDistributionDistance(g.Modes, l.Modes)
		blockDistance := vp9BlockDistributionDistance(g.Blocks, l.Blocks)
		skipDistance := vp9AbsIntForOracleTest(g.Skip - l.Skip)
		totalModeDistance += modeDistance
		totalBlockDistance += blockDistance
		totalSkipDistance += skipDistance
		t.Logf("VP9 inter-mode distribution frame %d: mode_distance=%d block_distance=%d skip_distance=%d govpx=%s libvpx=%s",
			i, modeDistance, blockDistance, skipDistance,
			g.String(), l.String())
	}
	t.Logf("VP9 inter-mode distribution trace: total_mode_distance=%d total_block_distance=%d total_skip_distance=%d",
		totalModeDistance, totalBlockDistance, totalSkipDistance)
	if vp9test.StrictEnv("GOVPX_VP9_MODE_DIST_STRICT") &&
		(totalModeDistance != 0 || totalBlockDistance != 0 ||
			totalSkipDistance != 0) {
		t.Fatalf("strict VP9 inter-mode distribution mismatch: mode=%d block=%d skip=%d",
			totalModeDistance, totalBlockDistance, totalSkipDistance)
	}
}

func TestVP9EncoderVpxencOracleFlat64KeyframeModeParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 64, 64
	src := vp9test.NewYCbCr(width, height, 80, 128, 128)
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	govpxPacket, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode govpx keyframe: %v", err)
	}

	libvpxPacket := vp9test.VpxencPackets(t, []*image.YCbCr{src})[0]

	govpxGrid := decodeVP9MiGridForOracleTest(t, govpxPacket)
	libvpxGrid := decodeVP9MiGridForOracleTest(t, libvpxPacket)
	if len(govpxGrid) != len(libvpxGrid) {
		t.Fatalf("mi grid length: govpx=%d libvpx=%d", len(govpxGrid), len(libvpxGrid))
	}
	modeMatches := 0
	blockMatches := 0
	skipMatches := 0
	for i := range govpxGrid {
		if govpxGrid[i].Mode == libvpxGrid[i].Mode {
			modeMatches++
		}
		if govpxGrid[i].SbType == libvpxGrid[i].SbType {
			blockMatches++
		}
		if govpxGrid[i].Skip == libvpxGrid[i].Skip {
			skipMatches++
		}
	}
	t.Logf("VP9 flat 64x64 keyframe mode trace: modes=%d/%d blocks=%d/%d skips=%d/%d govpx_bytes=%d libvpx_bytes=%d",
		modeMatches, len(govpxGrid), blockMatches, len(govpxGrid),
		skipMatches, len(govpxGrid), len(govpxPacket), len(libvpxPacket))
	vp9test.AssertPacketByteParity(t, "flat 64x64 keyframe", govpxPacket, libvpxPacket)
	if blockMatches != len(govpxGrid) || skipMatches != len(govpxGrid) {
		t.Fatalf("flat keyframe block/skip regression: block_matches=%d/%d skip_matches=%d/%d",
			blockMatches, len(govpxGrid), skipMatches, len(govpxGrid))
	}
	if vp9test.StrictEnv("GOVPX_VP9_KEYFRAME_MODE_STRICT") &&
		modeMatches != len(govpxGrid) {
		t.Fatalf("strict VP9 keyframe mode parity matched %d/%d modes",
			modeMatches, len(govpxGrid))
	}
}
