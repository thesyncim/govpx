package govpx

import (
	"bytes"
	"errors"
	"image"
	"runtime"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func TestVP9EncoderWideFrameUsesMinimumLegalTileColumns(t *testing.T) {
	const width, height = 4160, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	img := vp9test.NewYCbCr(width, height, 91, 143, 37)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	h, tileStart := vp9test.ParseHeader(t, packet)
	minLog2, _ := vp9dec.TileNBits(int((uint32(width) + 7) >> 3))
	if minLog2 < 1 {
		t.Fatalf("test frame min tile columns = %d, want >= 1", minLog2)
	}
	if h.Tile.Log2TileCols != minLog2 {
		t.Fatalf("Log2TileCols = %d, want minimum legal %d",
			h.Tile.Log2TileCols, minLog2)
	}
	assertVP9EncoderTilePrefixForTest(t, packet, tileStart)

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after multi-tile keyframe")
	}
	assertVP9FilledFrameWithin(t, frame, width, height, 91, 143, 37, 1)
}

func TestVP9EncoderThreadsHintIncreasesTileColumns(t *testing.T) {
	const width, height = 1280, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width:   width,
		Height:  height,
		Threads: 4,
	})
	img := vp9test.NewYCbCr(width, height, 82, 123, 211)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	h, tileStart := vp9test.ParseHeader(t, packet)
	if h.Tile.Log2TileCols != 2 {
		t.Fatalf("Log2TileCols = %d, want 2 for Threads=4",
			h.Tile.Log2TileCols)
	}
	if got, want := len(e.vp9CountWorkers), 4; got != want {
		t.Fatalf("VP9 count workers = %d, want %d", got, want)
	}
	if e.vp9TilePool == nil {
		t.Fatal("VP9 tile worker pool was not initialized")
	}
	if got, want := e.vp9TilePool.workerCount, 4; got != want {
		t.Fatalf("VP9 tile worker count = %d, want %d", got, want)
	}
	for i := range e.vp9TilePool.encodeJobs {
		if e.vp9TilePool.encodeJobs[i].size == 0 {
			t.Fatalf("VP9 tile worker job %d wrote zero bytes", i)
		}
	}
	if len(e.vp9CountWorkers[0].miGrid) == 0 || len(e.miGrid) == 0 {
		t.Fatal("VP9 threaded count worker miGrid was not initialized")
	}
	if &e.vp9CountWorkers[0].miGrid[0] == &e.miGrid[0] {
		t.Fatal("VP9 threaded count worker aliases encoder miGrid")
	}
	assertVP9EncoderTilePrefixForTest(t, packet, tileStart)

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after threaded-tile keyframe")
	}
	assertVP9FilledFrameWithin(t, frame, width, height, 82, 123, 211, 1)
}

func TestVP9RealtimeCBRAutoThreadHint(t *testing.T) {
	base := VP9EncoderOptions{
		Width:              1280,
		Height:             720,
		Deadline:           DeadlineRealtime,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		TargetBitrateKbps:  1200,
	}
	tests := []struct {
		name string
		opts VP9EncoderOptions
		cpus int
		want int
	}{
		{
			name: "low-resolution-stays-serial",
			opts: func() VP9EncoderOptions {
				opts := base
				opts.Width = 320
				opts.Height = 180
				return opts
			}(),
			cpus: 8,
			want: 1,
		},
		{
			name: "360p-uses-two-columns",
			opts: func() VP9EncoderOptions {
				opts := base
				opts.Width = 640
				opts.Height = 360
				return opts
			}(),
			cpus: 8,
			want: 2,
		},
		{
			name: "720p-uses-four-columns",
			opts: base,
			cpus: 8,
			want: 4,
		},
		{
			name: "three-cpus-stays-power-of-two",
			opts: base,
			cpus: 3,
			want: 2,
		},
		{
			name: "target-level-clamps-columns",
			opts: func() VP9EncoderOptions {
				opts := base
				opts.TargetLevel = 20
				return opts
			}(),
			cpus: 8,
			want: 1,
		},
		{
			name: "explicit-serial-opt-out",
			opts: func() VP9EncoderOptions {
				opts := base
				opts.Threads = 1
				return opts
			}(),
			cpus: 8,
			want: 1,
		},
		{
			name: "non-cbr-stays-serial",
			opts: func() VP9EncoderOptions {
				opts := base
				opts.RateControlMode = RateControlVBR
				return opts
			}(),
			cpus: 8,
			want: 1,
		},
		{
			name: "denoiser-stays-serial",
			opts: func() VP9EncoderOptions {
				opts := base
				opts.NoiseSensitivity = 3
				return opts
			}(),
			cpus: 8,
			want: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := vp9RealtimeAutoThreadHint(tc.opts, tc.cpus); got != tc.want {
				t.Fatalf("vp9RealtimeAutoThreadHint = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestVP9RealtimeCBRAutoThreadingDispatchesTileWorkers(t *testing.T) {
	const width, height = 640, 360
	opts := VP9EncoderOptions{
		Width:              width,
		Height:             height,
		Deadline:           DeadlineRealtime,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		TargetBitrateKbps:  700,
	}
	wantThreads := vp9RealtimeAutoThreadHint(opts, runtime.NumCPU())
	if wantThreads <= 1 {
		t.Skip("runtime exposes only one usable VP9 realtime tile thread")
	}
	e, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()
	if e.opts.Threads != 0 {
		t.Fatalf("stored Threads = %d, want caller auto value 0", e.opts.Threads)
	}
	if e.vp9TilePool == nil {
		t.Fatal("auto-threaded realtime CBR encoder did not prewarm tile pool")
	}
	if got := e.vp9TilePool.workerCount; got != wantThreads {
		t.Fatalf("prewarmed auto tile worker count = %d, want %d",
			got, wantThreads)
	}

	packet, err := e.Encode(vp9test.NewPanningYCbCr(width, height, 0))
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	h, tileStart := vp9test.ParseHeader(t, packet)
	if got, want := 1<<uint(h.Tile.Log2TileCols), wantThreads; got != want {
		t.Fatalf("auto tile columns = %d, want %d", got, want)
	}
	if e.vp9TilePool == nil {
		t.Fatal("auto-threaded realtime CBR encode did not initialize tile worker pool")
	}
	if got := e.vp9TilePool.workerCount; got != wantThreads {
		t.Fatalf("auto tile worker count = %d, want %d", got, wantThreads)
	}
	assertVP9EncoderTilePrefixForTest(t, packet, tileStart)
}

func TestVP9SpatialSVCRealtimeCBRAutoThreadingDispatchesTopLayerTileWorkers(t *testing.T) {
	opts, widths, heights := vp9RealtimeWebRTCSVCAutoThreadOptionsForTest()
	topLayer := int(opts.LayerCount) - 1
	wantThreads := vp9RealtimeAutoThreadHint(opts.Layers[topLayer],
		runtime.NumCPU())
	if wantThreads <= 1 {
		t.Skip("runtime exposes only one usable VP9 realtime tile thread")
	}
	svc, err := NewVP9SpatialSVCEncoder(opts)
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder: %v", err)
	}
	defer svc.Close()
	top := svc.layers[topLayer]
	if top.opts.Threads != 0 {
		t.Fatalf("top-layer stored Threads = %d, want caller auto value 0",
			top.opts.Threads)
	}
	if top.vp9TilePool == nil {
		t.Fatal("auto-threaded SVC top layer did not prewarm tile pool")
	}
	if got := top.vp9TilePool.workerCount; got != wantThreads {
		t.Fatalf("prewarmed SVC top-layer tile worker count = %d, want %d",
			got, wantThreads)
	}

	srcs := make([]*image.YCbCr, opts.LayerCount)
	for layer := 0; layer < int(opts.LayerCount); layer++ {
		srcs[layer] = vp9test.NewPanningYCbCr(widths[layer],
			heights[layer], layer)
	}
	dst := make([]byte, 1<<22)
	result, err := svc.EncodeIntoWithResult(srcs, dst)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult: %v", err)
	}
	header, tileStart := vp9test.ParseHeader(t,
		result.Layers[topLayer].Data)
	if got := 1 << uint(header.Tile.Log2TileCols); got != wantThreads {
		t.Fatalf("SVC top-layer auto tile columns = %d, want %d",
			got, wantThreads)
	}
	if top.vp9TilePool == nil {
		t.Fatal("auto-threaded SVC top layer did not initialize tile worker pool")
	}
	if got := top.vp9TilePool.workerCount; got != wantThreads {
		t.Fatalf("SVC top-layer auto tile worker count = %d, want %d",
			got, wantThreads)
	}
	assertVP9EncoderTilePrefixForTest(t, result.Layers[topLayer].Data,
		tileStart)
}

func vp9RealtimeWebRTCSVCAutoThreadOptionsForTest() (
	VP9SpatialSVCEncoderOptions,
	[VP9MaxSpatialLayers]int,
	[VP9MaxSpatialLayers]int,
) {
	widths := [VP9MaxSpatialLayers]int{160, 320, 640}
	heights := [VP9MaxSpatialLayers]int{90, 180, 360}
	bitrates := [VP9MaxSpatialLayers]int{96, 288, 416}
	temporal := TemporalScalabilityConfig{
		Enabled: true,
		Mode:    TemporalLayeringThreeLayers,
	}
	var layers [VP9MaxSpatialLayers]VP9EncoderOptions
	for layer := range 3 {
		layers[layer] = VP9EncoderOptions{
			Width:                    widths[layer],
			Height:                   heights[layer],
			FPS:                      30,
			Deadline:                 DeadlineRealtime,
			RateControlModeSet:       true,
			RateControlMode:          RateControlCBR,
			TargetBitrateKbps:        bitrates[layer],
			MinQuantizer:             4,
			MaxQuantizer:             56,
			MaxKeyframeInterval:      128,
			TemporalScalability:      temporal,
			ErrorResilient:           true,
			FrameParallelDecodingSet: true,
			FrameParallelDecoding:    true,
		}
	}
	return VP9SpatialSVCEncoderOptions{
		LayerCount:           3,
		InterLayerPrediction: true,
		Layers:               layers,
	}, widths, heights
}

func TestVP9RealtimeCBRAutoThreadingResizePromotesTileWorkers(t *testing.T) {
	const (
		smallWidth  = 320
		smallHeight = 180
		wideWidth   = 1280
		wideHeight  = 720
	)
	opts := VP9EncoderOptions{
		Width:              smallWidth,
		Height:             smallHeight,
		Deadline:           DeadlineRealtime,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		TargetBitrateKbps:  1200,
	}
	e, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()
	small, err := e.Encode(vp9test.NewPanningYCbCr(smallWidth, smallHeight, 0))
	if err != nil {
		t.Fatalf("small Encode: %v", err)
	}
	smallHeader, _ := vp9test.ParseHeader(t, small)
	if smallHeader.Tile.Log2TileCols != 0 {
		t.Fatalf("small auto tile columns log2 = %d, want 0", smallHeader.Tile.Log2TileCols)
	}
	if e.vp9TilePool != nil {
		t.Fatalf("small auto tile pool = %d workers, want nil", e.vp9TilePool.workerCount)
	}

	if err := e.SetRealtimeTarget(RealtimeTarget{Width: wideWidth, Height: wideHeight}); err != nil {
		t.Fatalf("SetRealtimeTarget resize: %v", err)
	}
	resizedOpts := opts
	resizedOpts.Width = wideWidth
	resizedOpts.Height = wideHeight
	wantThreads := vp9RealtimeAutoThreadHint(resizedOpts, runtime.NumCPU())
	if wantThreads <= 1 {
		t.Skip("runtime exposes only one usable VP9 realtime tile thread")
	}
	wide, err := e.Encode(vp9test.NewPanningYCbCr(wideWidth, wideHeight, 1))
	if err != nil {
		t.Fatalf("wide Encode: %v", err)
	}
	wideHeader, tileStart := vp9test.ParseHeader(t, wide)
	if got, want := 1<<uint(wideHeader.Tile.Log2TileCols), wantThreads; got != want {
		t.Fatalf("resized auto tile columns = %d, want %d", got, want)
	}
	if e.vp9TilePool == nil {
		t.Fatal("resized auto-threaded encode did not initialize tile worker pool")
	}
	if got := e.vp9TilePool.workerCount; got != wantThreads {
		t.Fatalf("resized auto tile worker count = %d, want %d", got, wantThreads)
	}
	assertVP9EncoderTilePrefixForTest(t, wide, tileStart)
}

func TestVP9RealtimeCBRAutoThreadingPrewarmsTileWorkers(t *testing.T) {
	const width, height = 640, 360
	opts := VP9EncoderOptions{
		Width:              width,
		Height:             height,
		Deadline:           DeadlineRealtime,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		TargetBitrateKbps:  700,
	}
	wantThreads := vp9RealtimeAutoThreadHint(opts, runtime.NumCPU())
	if wantThreads <= 1 {
		t.Skip("runtime exposes only one usable VP9 realtime tile thread")
	}
	e, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()
	if e.vp9TilePool == nil {
		t.Fatal("auto-threaded realtime CBR construction did not prewarm tile worker pool")
	}
	if got := e.vp9TilePool.workerCount; got != wantThreads {
		t.Fatalf("auto-threaded realtime CBR workers = %d, want %d",
			got, wantThreads)
	}
	for i, output := range e.vp9TilePool.outputs {
		if len(output) == 0 {
			t.Fatalf("prewarmed tile worker output %d has no buffer", i)
		}
	}
}

func TestVP9RealtimeCBRAutoThreadingReleasesTileWorkersWhenIneligible(t *testing.T) {
	const width, height = 640, 360
	opts := VP9EncoderOptions{
		Width:              width,
		Height:             height,
		Deadline:           DeadlineRealtime,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		TargetBitrateKbps:  700,
	}
	wantThreads := vp9RealtimeAutoThreadHint(opts, runtime.NumCPU())
	if wantThreads <= 1 {
		t.Skip("runtime exposes only one usable VP9 realtime tile thread")
	}
	e, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()
	if _, err := e.Encode(vp9test.NewPanningYCbCr(width, height, 0)); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if e.vp9TilePool == nil {
		t.Fatal("auto-threaded realtime CBR encode did not initialize tile worker pool")
	}

	if err := e.SetRateControl(RateControlConfig{
		Mode:              RateControlVBR,
		TargetBitrateKbps: 700,
	}); err != nil {
		t.Fatalf("SetRateControl(VBR): %v", err)
	}
	if e.vp9EffectiveThreadHint() != 1 {
		t.Fatalf("VBR auto effective threads = %d, want 1", e.vp9EffectiveThreadHint())
	}
	if e.vp9TilePool != nil || len(e.vp9CountWorkers) != 0 ||
		len(e.vp9CountCounts) != 0 || len(e.vp9CountJobs) != 0 {
		t.Fatal("SetRateControl(VBR) left auto VP9 tile worker state installed")
	}

	if err := e.SetRateControl(RateControlConfig{
		Mode:              RateControlCBR,
		TargetBitrateKbps: 700,
	}); err != nil {
		t.Fatalf("SetRateControl(CBR): %v", err)
	}
	if _, err := e.Encode(vp9test.NewPanningYCbCr(width, height, 1)); err != nil {
		t.Fatalf("CBR Encode: %v", err)
	}
	if e.vp9TilePool == nil {
		t.Fatal("auto-threaded realtime CBR encode did not reinitialize tile worker pool")
	}
	if err := e.SetDeadline(DeadlineGoodQuality); err != nil {
		t.Fatalf("SetDeadline(GoodQuality): %v", err)
	}
	if e.vp9EffectiveThreadHint() != 1 {
		t.Fatalf("good-quality auto effective threads = %d, want 1", e.vp9EffectiveThreadHint())
	}
	if e.vp9TilePool != nil || len(e.vp9CountWorkers) != 0 ||
		len(e.vp9CountCounts) != 0 || len(e.vp9CountJobs) != 0 {
		t.Fatal("SetDeadline(GoodQuality) left auto VP9 tile worker state installed")
	}
}

func TestVP9RealtimeCBRAutoThreadingResizeDownReleasesTileWorkers(t *testing.T) {
	const (
		wideWidth   = 1280
		wideHeight  = 720
		smallWidth  = 320
		smallHeight = 180
	)
	opts := VP9EncoderOptions{
		Width:              wideWidth,
		Height:             wideHeight,
		Deadline:           DeadlineRealtime,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		TargetBitrateKbps:  1200,
	}
	wantThreads := vp9RealtimeAutoThreadHint(opts, runtime.NumCPU())
	if wantThreads <= 1 {
		t.Skip("runtime exposes only one usable VP9 realtime tile thread")
	}
	e, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()
	if _, err := e.Encode(vp9test.NewPanningYCbCr(wideWidth, wideHeight, 0)); err != nil {
		t.Fatalf("wide Encode: %v", err)
	}
	if e.vp9TilePool == nil {
		t.Fatal("wide auto-threaded encode did not initialize tile worker pool")
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{
		Width:  smallWidth,
		Height: smallHeight,
	}); err != nil {
		t.Fatalf("SetRealtimeTarget shrink: %v", err)
	}
	if e.vp9TilePool != nil || len(e.vp9CountWorkers) != 0 ||
		len(e.vp9CountCounts) != 0 || len(e.vp9CountJobs) != 0 {
		t.Fatal("SetRealtimeTarget shrink left auto VP9 tile worker state installed")
	}
	packet, err := e.Encode(vp9test.NewPanningYCbCr(smallWidth, smallHeight, 1))
	if err != nil {
		t.Fatalf("small Encode: %v", err)
	}
	h, _ := vp9test.ParseHeader(t, packet)
	if h.Tile.Log2TileCols != 0 {
		t.Fatalf("small auto tile columns log2 = %d, want 0", h.Tile.Log2TileCols)
	}
	if e.vp9TilePool != nil {
		t.Fatalf("small auto tile pool = %d workers, want nil", e.vp9TilePool.workerCount)
	}
}

func TestVP9EncoderNoiseSensitivityUsesSerialTileWorkers(t *testing.T) {
	const width, height = 1280, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:            width,
		Height:           height,
		Threads:          4,
		NoiseSensitivity: 3,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()
	if got := e.vp9EffectiveThreadHint(); got != 4 {
		t.Fatalf("effective thread hint = %d, want caller hint 4", got)
	}
	if got := e.vp9TileWorkerThreadHint(); got != 1 {
		t.Fatalf("tile-worker thread hint = %d, want 1 while denoiser is active", got)
	}
	if e.vp9TilePool != nil {
		t.Fatal("denoiser initialized VP9 tile worker pool")
	}
	img := vp9test.NewYCbCr(width, height, 82, 123, 211)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	h, _ := vp9test.ParseHeader(t, packet)
	if h.Tile.Log2TileCols != 0 {
		t.Fatalf("Log2TileCols = %d, want 0 while denoiser keeps tile workers disabled",
			h.Tile.Log2TileCols)
	}
	if e.vp9TilePool != nil {
		t.Fatal("denoiser encode created VP9 tile worker pool")
	}
	if len(e.vp9CountWorkers) != 0 {
		t.Fatalf("denoiser count workers = %d, want 0", len(e.vp9CountWorkers))
	}
}

func TestVP9EncoderSetNoiseSensitivityClosesTilePool(t *testing.T) {
	const width, height = 1280, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:   width,
		Height:  height,
		Threads: 4,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if e.vp9TilePool == nil {
		t.Fatal("VP9 tile worker pool was not initialized")
	}
	if err := e.SetNoiseSensitivity(3); err != nil {
		t.Fatalf("SetNoiseSensitivity: %v", err)
	}
	if e.vp9TilePool != nil || len(e.vp9CountWorkers) != 0 ||
		len(e.vp9CountCounts) != 0 || len(e.vp9CountJobs) != 0 {
		t.Fatal("SetNoiseSensitivity left VP9 tile worker state installed")
	}
}

func TestVP9EncoderThreadsHintDeterministicAcrossRuns(t *testing.T) {
	const width, height = 1024, 64
	opts := VP9EncoderOptions{
		Width:   width,
		Height:  height,
		Threads: 4,
	}
	a, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder(a): %v", err)
	}
	b, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder(b): %v", err)
	}
	dstA := make([]byte, 1<<20)
	dstB := make([]byte, 1<<20)
	for frame := range 2 {
		src := vp9test.NewPanningYCbCr(width, height, frame)
		nA, err := a.EncodeInto(src, dstA)
		if err != nil {
			t.Fatalf("a EncodeInto[%d]: %v", frame, err)
		}
		nB, err := b.EncodeInto(src, dstB)
		if err != nil {
			t.Fatalf("b EncodeInto[%d]: %v", frame, err)
		}
		if !bytes.Equal(dstA[:nA], dstB[:nB]) {
			t.Fatalf("threaded VP9 packet %d differs across runs: %d/%d bytes",
				frame, nA, nB)
		}
	}
}

func TestVP9EncoderLog2TileRowsRowOnlyMatchesSerial(t *testing.T) {
	const width, height = 64, 128
	serial, err := NewVP9Encoder(VP9EncoderOptions{
		Width:        width,
		Height:       height,
		Threads:      1,
		Log2TileRows: 1,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder(serial): %v", err)
	}
	threaded, err := NewVP9Encoder(VP9EncoderOptions{
		Width:        width,
		Height:       height,
		Threads:      2,
		Log2TileRows: 1,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder(threaded): %v", err)
	}

	dstSerial := make([]byte, 1<<20)
	dstThreaded := make([]byte, 1<<20)
	for frame := range 3 {
		src := vp9test.NewPanningYCbCr(width, height, frame)
		nSerial, err := serial.EncodeInto(src, dstSerial)
		if err != nil {
			t.Fatalf("serial EncodeInto[%d]: %v", frame, err)
		}
		nThreaded, err := threaded.EncodeInto(src, dstThreaded)
		if err != nil {
			t.Fatalf("threaded EncodeInto[%d]: %v", frame, err)
		}
		if !bytes.Equal(dstSerial[:nSerial], dstThreaded[:nThreaded]) {
			t.Fatalf("tile-row threaded packet %d differs from serial: %d/%d bytes",
				frame, nThreaded, nSerial)
		}
		if frame == 0 {
			d, err := NewVP9Decoder(VP9DecoderOptions{})
			if err != nil {
				t.Fatalf("NewVP9Decoder: %v", err)
			}
			if err := d.Decode(dstSerial[:nSerial]); err != nil {
				t.Fatalf("Decode serial tile-row keyframe: %v", err)
			}
			if _, ok := d.NextFrame(); !ok {
				t.Fatal("NextFrame returned !ok after serial tile-row keyframe")
			}
		}
	}
	if threaded.vp9TilePool != nil {
		t.Fatalf("row-only tile configuration initialized unsafe pool with %d workers",
			threaded.vp9TilePool.workerCount)
	}
}

func TestVP9EncoderLog2TileRowsWithTileColumnsMatchesSerial(t *testing.T) {
	const width, height = 4104, 128
	serial, err := NewVP9Encoder(VP9EncoderOptions{
		Width:        width,
		Height:       height,
		Threads:      1,
		Log2TileRows: 1,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder(serial): %v", err)
	}
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:        width,
		Height:       height,
		Threads:      2,
		Log2TileRows: 1,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	src := vp9test.NewYCbCr(width, height, 82, 123, 211)
	wantPacket, err := serial.Encode(src)
	if err != nil {
		t.Fatalf("serial Encode: %v", err)
	}
	packet, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Equal(packet, wantPacket) {
		t.Fatalf("tile-row packet differs from serial: %d/%d bytes first_diff=%d",
			len(packet), len(wantPacket), testutil.FirstByteDiff(packet, wantPacket))
	}

	h, tileStart := vp9test.ParseHeader(t, packet)
	if h.Tile.Log2TileRows != 1 {
		t.Fatalf("Log2TileRows = %d, want 1", h.Tile.Log2TileRows)
	}
	if h.Tile.Log2TileCols != 1 {
		t.Fatalf("Log2TileCols = %d, want 1 for minimum wide-frame tiling",
			h.Tile.Log2TileCols)
	}
	if e.vp9TilePool != nil {
		t.Fatalf("tile-row configuration initialized unsafe pool with %d workers",
			e.vp9TilePool.workerCount)
	}
	assertVP9EncoderTilePrefixForTest(t, packet, tileStart)

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after tile-row threaded keyframe")
	}
	assertVP9FilledFrameWithin(t, frame, width, height, 82, 123, 211, 1)
}

func TestVP9EncoderLog2TileRowsSerialMultiColumnDecodes(t *testing.T) {
	const width, height = 4104, 128
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:        width,
		Height:       height,
		Threads:      1,
		Log2TileRows: 1,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	packet, err := e.Encode(vp9test.NewYCbCr(width, height, 82, 123, 211))
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	h, _ := vp9test.ParseHeader(t, packet)
	if h.Tile.Log2TileRows != 1 || h.Tile.Log2TileCols == 0 {
		t.Fatalf("tile grid = rows:%d cols:%d, want row tiles and multi-column minimum",
			h.Tile.Log2TileRows, h.Tile.Log2TileCols)
	}
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode serial tile grid: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after serial tile-grid keyframe")
	}
}

func TestVP9EncoderLog2TileRowsResizeValidation(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:        64,
		Height:       128,
		Threads:      2,
		Log2TileRows: 1,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{Width: 64, Height: 64}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetRealtimeTarget invalid tile rows error = %v, want ErrInvalidConfig", err)
	}
	if e.opts.Width != 64 || e.opts.Height != 128 {
		t.Fatalf("encoder dimensions changed after rejected resize: %dx%d",
			e.opts.Width, e.opts.Height)
	}
}

func TestVP9EncoderRuntimeResizeRebuildsTileWorkerPool(t *testing.T) {
	const smallWidth, smallHeight = 64, 64
	const wideWidth, wideHeight = 1280, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:   smallWidth,
		Height:  smallHeight,
		Threads: 4,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if _, err := e.Encode(vp9test.NewYCbCr(smallWidth, smallHeight, 82, 123, 211)); err != nil {
		t.Fatalf("small Encode: %v", err)
	}
	if e.vp9TilePool != nil {
		t.Fatalf("small threaded pool = %d workers, want nil before multi-tile resize",
			e.vp9TilePool.workerCount)
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{
		Width:  wideWidth,
		Height: wideHeight,
	}); err != nil {
		t.Fatalf("SetRealtimeTarget resize: %v", err)
	}
	packet, err := e.Encode(vp9test.NewYCbCr(wideWidth, wideHeight, 91, 143, 37))
	if err != nil {
		t.Fatalf("wide Encode: %v", err)
	}
	h, tileStart := vp9test.ParseHeader(t, packet)
	if h.Tile.Log2TileCols != 2 {
		t.Fatalf("Log2TileCols after resize = %d, want 2 for Threads=4",
			h.Tile.Log2TileCols)
	}
	if e.vp9TilePool == nil {
		t.Fatal("VP9 tile worker pool was not rebuilt after resize")
	}
	if got, want := e.vp9TilePool.workerCount, 4; got != want {
		t.Fatalf("resized VP9 tile worker count = %d, want %d", got, want)
	}
	for i := range e.vp9TilePool.encodeJobs {
		if e.vp9TilePool.encodeJobs[i].size == 0 {
			t.Fatalf("resized VP9 tile worker job %d wrote zero bytes", i)
		}
	}
	assertVP9EncoderTilePrefixForTest(t, packet, tileStart)
}

func TestVP9EncoderSetTargetLevelRebuildsTileWorkerPoolForTileClamp(t *testing.T) {
	const width, height = 8192, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:   width,
		Height:  height,
		Threads: 8,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()
	packet, err := e.Encode(vp9test.NewYCbCr(width, height, 82, 123, 211))
	if err != nil {
		t.Fatalf("Encode before target level: %v", err)
	}
	h, _ := vp9test.ParseHeader(t, packet)
	if h.Tile.Log2TileCols != 3 {
		t.Fatalf("initial Log2TileCols = %d, want 3 for Threads=8",
			h.Tile.Log2TileCols)
	}
	if e.vp9TilePool == nil || e.vp9TilePool.workerCount != 8 {
		t.Fatalf("initial tile pool workers = %v, want 8", e.vp9TilePool)
	}
	if err := e.SetTargetLevel(30); err != nil {
		t.Fatalf("SetTargetLevel(30): %v", err)
	}
	if e.vp9TilePool != nil || len(e.vp9CountWorkers) != 0 ||
		len(e.vp9CountCounts) != 0 || len(e.vp9CountJobs) != 0 {
		t.Fatal("SetTargetLevel tile clamp left stale VP9 tile worker state installed")
	}
	packet, err = e.Encode(vp9test.NewYCbCr(width, height, 91, 143, 37))
	if err != nil {
		t.Fatalf("Encode after target level: %v", err)
	}
	if len(packet) == 0 {
		t.Fatal("Encode after target level returned empty packet")
	}
	tileInfo := vp9EncoderTileInfoForTargetLevel((width+7)>>3, width, height,
		e.vp9EffectiveThreadHint(), e.opts.Log2TileRows, e.opts.TargetLevel)
	tileCols := 1 << uint(tileInfo.Log2TileCols)
	if tileCols >= 8 {
		t.Fatalf("target-level tile columns = %d, want clamp below 8", tileCols)
	}
	if tileCols <= 1 && e.vp9TilePool != nil {
		t.Fatalf("single-column target-level tile pool = %d workers, want nil",
			e.vp9TilePool.workerCount)
	}
	if tileCols > 1 && (e.vp9TilePool == nil || e.vp9TilePool.workerCount != tileCols) {
		t.Fatalf("target-level tile pool workers = %v, want %d",
			e.vp9TilePool, tileCols)
	}
}

func TestVP9TileWorkerPoolOutputSizeCache(t *testing.T) {
	pool := &vp9TileWorkerPool{
		outputs: make([][]byte, 4),
	}
	pool.ensureOutputSize(256)
	if got, want := pool.outputSize, 256; got != want {
		t.Fatalf("outputSize = %d, want %d", got, want)
	}
	first := make([]*byte, len(pool.outputs))
	for i := range pool.outputs {
		if len(pool.outputs[i]) != 256 {
			t.Fatalf("output %d len = %d, want 256", i, len(pool.outputs[i]))
		}
		first[i] = &pool.outputs[i][0]
	}
	pool.ensureOutputSize(256)
	for i := range pool.outputs {
		if &pool.outputs[i][0] != first[i] {
			t.Fatalf("output %d changed on cached ensure", i)
		}
	}

	allocs := testing.AllocsPerRun(vp9EncoderInterAllocRuns, func() {
		pool.ensureOutputSize(256)
	})
	if allocs != 0 {
		t.Fatalf("cached ensureOutputSize allocs = %f, want 0", allocs)
	}

	pool.ensureOutputSize(128)
	if got, want := pool.outputSize, 128; got != want {
		t.Fatalf("shrunk outputSize = %d, want %d", got, want)
	}
	for i := range pool.outputs {
		if len(pool.outputs[i]) != 128 {
			t.Fatalf("shrunk output %d len = %d, want 128", i, len(pool.outputs[i]))
		}
		if &pool.outputs[i][0] != first[i] {
			t.Fatalf("output %d reallocated while shrinking", i)
		}
	}
}

func TestVP9EncoderThreadedTileEncodeSteadyStateAlloc(t *testing.T) {
	const width, height = 1280, 720
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:   width,
		Height:  height,
		Threads: 4,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	frames := [2]*image.YCbCr{}
	for i := range frames {
		frames[i] = vp9test.NewYCbCr(width, height, 128, 128, 128)
	}
	dstSize, err := vp9AllocatingEncodeBufferSize(width, height)
	if err != nil {
		t.Fatalf("vp9AllocatingEncodeBufferSize: %v", err)
	}
	dst := make([]byte, dstSize)
	for i := range frames {
		if _, err := e.EncodeInto(frames[i], dst); err != nil {
			t.Fatalf("warm EncodeInto[%d]: %v", i, err)
		}
	}
	if e.vp9TilePool == nil {
		t.Fatal("threaded 720p encode did not initialize VP9 tile worker pool")
	}
	if got, want := e.vp9TilePool.workerCount, 4; got != want {
		t.Fatalf("threaded 720p tile worker count = %d, want %d", got, want)
	}
	idx := 0
	allocs := testing.AllocsPerRun(1, func() {
		frame := frames[idx&1]
		idx++
		if _, err := e.EncodeInto(frame, dst); err != nil {
			t.Fatalf("EncodeInto threaded alloc run: %v", err)
		}
	})
	if allocs != 0 {
		t.Fatalf("threaded tile EncodeInto steady-state allocs = %f, want 0", allocs)
	}
	for i := 0; i < e.vp9TilePool.workerCount; i++ {
		if e.vp9TilePool.encodeJobs[i].size <= 0 {
			t.Fatalf("threaded 720p tile job %d wrote %d bytes",
				i, e.vp9TilePool.encodeJobs[i].size)
		}
	}
}

func TestVP9EncoderThreadedTileFeaturePathsSteadyStateAlloc(t *testing.T) {
	const width, height = 1280, 720
	baseCBR := func() VP9EncoderOptions {
		return VP9EncoderOptions{
			Width:              width,
			Height:             height,
			Threads:            4,
			FPS:                30,
			TargetBitrateKbps:  2200,
			RateControlModeSet: true,
			RateControlMode:    RateControlCBR,
			MinQuantizer:       4,
			MaxQuantizer:       56,
		}
	}
	cases := []struct {
		name          string
		opts          VP9EncoderOptions
		before        func(*testing.T, *VP9Encoder)
		wantMaxAllocs float64
	}{
		{
			// Non-CBR threaded frames can run slowly enough that helper tile
			// workers exhaust the idle-spin window and park on their start
			// channels between frames; the runtime may allocate those sudogs.
			name: "vbr",
			opts: func() VP9EncoderOptions {
				opts := baseCBR()
				opts.RateControlMode = RateControlVBR
				return opts
			}(),
			wantMaxAllocs: 4,
		},
		{
			name: "cq",
			opts: func() VP9EncoderOptions {
				opts := baseCBR()
				opts.RateControlMode = RateControlCQ
				opts.CQLevel = 20
				return opts
			}(),
			wantMaxAllocs: 4,
		},
		{
			name: "q",
			opts: func() VP9EncoderOptions {
				opts := baseCBR()
				opts.RateControlMode = RateControlQ
				opts.CQLevel = 20
				return opts
			}(),
			wantMaxAllocs: 4,
		},
		{
			name: "cyclic-aq",
			opts: func() VP9EncoderOptions {
				opts := baseCBR()
				opts.AQMode = VP9AQCyclicRefresh
				return opts
			}(),
		},
		{
			name: "active-map",
			opts: func() VP9EncoderOptions {
				opts := baseCBR()
				opts.MinQuantizer = 20
				opts.MaxQuantizer = 20
				return opts
			}(),
			before: func(t *testing.T, e *VP9Encoder) {
				t.Helper()
				rows := encoderMacroblockRows(height)
				cols := encoderMacroblockCols(width)
				activeMap := make([]uint8, rows*cols)
				for row := range rows {
					for col := range cols {
						activeMap[row*cols+col] = 1
						if (row+col)&1 == 0 {
							activeMap[row*cols+col] = 0
						}
					}
				}
				if err := e.SetActiveMap(activeMap, rows, cols); err != nil {
					t.Fatalf("SetActiveMap: %v", err)
				}
			},
		},
		{
			name: "roi",
			opts: func() VP9EncoderOptions {
				opts := baseCBR()
				opts.MinQuantizer = 20
				opts.MaxQuantizer = 20
				return opts
			}(),
			before: func(t *testing.T, e *VP9Encoder) {
				t.Helper()
				miRows := (height + 7) >> 3
				miCols := (width + 7) >> 3
				roi := ROIMap{
					Enabled:   true,
					Rows:      miRows,
					Cols:      miCols,
					SegmentID: make([]uint8, miRows*miCols),
				}
				for row := range miRows {
					for col := range miCols {
						if row == col || row+col == miCols-1 {
							roi.SegmentID[row*miCols+col] = 1
						}
					}
				}
				roi.DeltaQuantizer[1] = -4
				roi.DeltaLoopFilter[1] = 3
				if err := e.SetROIMap(&roi); err != nil {
					t.Fatalf("SetROIMap: %v", err)
				}
			},
		},
	}
	frames := [3]*image.YCbCr{
		vp9test.NewYCbCr(width, height, 96, 128, 128),
		vp9test.NewYCbCr(width, height, 112, 128, 128),
		vp9test.NewYCbCr(width, height, 128, 128, 128),
	}
	dstSize, err := vp9AllocatingEncodeBufferSize(width, height)
	if err != nil {
		t.Fatalf("vp9AllocatingEncodeBufferSize: %v", err)
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, err := NewVP9Encoder(tc.opts)
			if err != nil {
				t.Fatalf("NewVP9Encoder: %v", err)
			}
			if tc.before != nil {
				tc.before(t, e)
			}
			dst := make([]byte, dstSize)
			for i := range len(frames) * 8 {
				if _, err := e.EncodeInto(frames[i%len(frames)], dst); err != nil {
					t.Fatalf("warm EncodeInto[%d]: %v", i, err)
				}
			}
			if e.vp9TilePool == nil {
				t.Fatal("threaded feature path did not initialize VP9 tile worker pool")
			}
			if got, want := e.vp9TilePool.workerCount, 4; got != want {
				t.Fatalf("threaded feature path worker count = %d, want %d",
					got, want)
			}
			idx := 0
			allocs := vp9SteadyStateAllocsPerRun(len(frames)*4, len(frames)*2, func() {
				frame := frames[idx%len(frames)]
				idx++
				if _, err := e.EncodeInto(frame, dst); err != nil {
					t.Fatalf("EncodeInto threaded feature alloc run: %v", err)
				}
			})
			// The threaded feature path keeps worker scratch behind pools. The
			// fixed-P measurement window should stay effectively warm, with one
			// refill of headroom for pool scheduling jitter.
			wantMaxAllocs := tc.wantMaxAllocs
			if wantMaxAllocs == 0 {
				wantMaxAllocs = 1.0
			}
			if allocs > wantMaxAllocs {
				t.Fatalf("threaded feature path steady-state allocs = %f, want <= %f",
					allocs, wantMaxAllocs)
			}
			for i := 0; i < e.vp9TilePool.workerCount; i++ {
				if e.vp9TilePool.encodeJobs[i].size <= 0 {
					t.Fatalf("threaded feature tile job %d wrote %d bytes",
						i, e.vp9TilePool.encodeJobs[i].size)
				}
			}
		})
	}
}
