package govpx

import (
	"bytes"
	"errors"
	"image"
	"testing"

	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func TestVP9EncoderWideFrameUsesMinimumLegalTileColumns(t *testing.T) {
	const width, height = 4160, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	img := newVP9YCbCrForTest(width, height, 91, 143, 37)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	h, tileStart := parseVP9EncoderHeaderForTest(t, packet)
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
	img := newVP9YCbCrForTest(width, height, 82, 123, 211)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	h, tileStart := parseVP9EncoderHeaderForTest(t, packet)
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
	if e.vp9TilePool != nil {
		t.Fatal("denoiser initialized VP9 tile worker pool")
	}
	img := newVP9YCbCrForTest(width, height, 82, 123, 211)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	h, _ := parseVP9EncoderHeaderForTest(t, packet)
	if h.Tile.Log2TileCols != 2 {
		t.Fatalf("Log2TileCols = %d, want 2 for Threads=4",
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
		src := newVP9PanningYCbCrForRateTest(width, height, frame)
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
		src := newVP9PanningYCbCrForRateTest(width, height, frame)
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
	src := newVP9YCbCrForTest(width, height, 82, 123, 211)
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
			len(packet), len(wantPacket), firstVP9PacketDiffForTest(packet, wantPacket))
	}

	h, tileStart := parseVP9EncoderHeaderForTest(t, packet)
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
	packet, err := e.Encode(newVP9YCbCrForTest(width, height, 82, 123, 211))
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	h, _ := parseVP9EncoderHeaderForTest(t, packet)
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
	if _, err := e.Encode(newVP9YCbCrForTest(smallWidth, smallHeight, 82, 123, 211)); err != nil {
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
	packet, err := e.Encode(newVP9YCbCrForTest(wideWidth, wideHeight, 91, 143, 37))
	if err != nil {
		t.Fatalf("wide Encode: %v", err)
	}
	h, tileStart := parseVP9EncoderHeaderForTest(t, packet)
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
		frames[i] = newVP9YCbCrForTest(width, height, 128, 128, 128)
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
		newVP9YCbCrForTest(width, height, 96, 128, 128),
		newVP9YCbCrForTest(width, height, 112, 128, 128),
		newVP9YCbCrForTest(width, height, 128, 128, 128),
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
