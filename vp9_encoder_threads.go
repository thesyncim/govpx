package govpx

import "runtime"

const vp9RealtimeAutoMaxThreads = 4

// VP9RealtimeCBRAutoThreadHint reports the tile-thread hint govpx uses when a
// VP9 realtime CBR encoder leaves VP9EncoderOptions.Threads at zero with
// denoising and tile rows disabled. A return value of 1 means the frame size or
// host CPU count keeps the encoder on the serial tile path.
func VP9RealtimeCBRAutoThreadHint(width, height int) int {
	return vp9RealtimeCBRAutoThreadHint(width, height, runtime.NumCPU())
}

func vp9RealtimeCBRAutoThreadHint(width, height, cpus int) int {
	if width <= 0 || height <= 0 {
		return 1
	}
	return vp9RealtimeAutoThreadHint(VP9EncoderOptions{
		Width:              width,
		Height:             height,
		Deadline:           DeadlineRealtime,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		NoiseSensitivity:   0,
	}, cpus)
}

func (e *VP9Encoder) vp9EffectiveThreadHint() int {
	if e == nil {
		return 0
	}
	return vp9EffectiveThreadHint(e.opts)
}

func vp9EffectiveThreadHint(opts VP9EncoderOptions) int {
	if opts.Threads != 0 {
		return opts.Threads
	}
	return vp9RealtimeAutoThreadHint(opts, runtime.NumCPU())
}

func vp9RealtimeAutoThreadHint(opts VP9EncoderOptions, cpus int) int {
	if !vp9RealtimeAutoThreadingEligible(opts) || cpus < 2 {
		return 1
	}
	threadHint := 2
	if cpus >= 4 {
		threadHint = vp9RealtimeAutoMaxThreads
	}
	miCols := (opts.Width + 7) >> 3
	tileInfo := vp9EncoderTileInfoForTargetLevel(miCols, opts.Width,
		opts.Height, threadHint, opts.Log2TileRows, opts.TargetLevel)
	if tileInfo.Log2TileRows != 0 {
		return 1
	}
	tileCols := 1 << uint(tileInfo.Log2TileCols)
	if tileCols <= 1 {
		return 1
	}
	if tileCols < threadHint {
		return tileCols
	}
	return threadHint
}

func vp9RealtimeAutoThreadingEligible(opts VP9EncoderOptions) bool {
	return opts.Threads == 0 &&
		opts.Deadline == DeadlineRealtime &&
		opts.RateControlModeSet &&
		opts.RateControlMode == RateControlCBR &&
		opts.NoiseSensitivity == 0 &&
		opts.Log2TileRows == 0
}
