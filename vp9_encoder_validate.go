package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func validateVP9EncoderOptions(opts VP9EncoderOptions) error {
	if !validVP9Dimension(opts.Width) || !validVP9Dimension(opts.Height) {
		return ErrInvalidConfig
	}
	if opts.Threads < 0 {
		return ErrInvalidConfig
	}
	if err := validateVP9TileRowOptions(opts.Width, opts.Height, opts.Log2TileRows); err != nil {
		return err
	}
	if opts.TargetBitrateKbps < 0 || opts.Quantizer < 0 {
		return ErrInvalidConfig
	}
	if err := validateVP9KeyFrameIntervalOptions(
		opts.MinKeyframeInterval, opts.MaxKeyframeInterval); err != nil {
		return err
	}
	if opts.LookaheadFrames < 0 || opts.LookaheadFrames > vp9MaxLookaheadFrames {
		return ErrInvalidConfig
	}
	if opts.ARNRMaxFrames < 0 || opts.ARNRMaxFrames > maxARNRFrames ||
		opts.ARNRStrength < 0 || opts.ARNRStrength > 6 ||
		opts.ARNRType < 0 || opts.ARNRType > 3 {
		return ErrInvalidConfig
	}
	if opts.Tuning < TunePSNR || opts.Tuning > TuneSSIM {
		return ErrInvalidConfig
	}
	if opts.ScreenContentMode < int8(VP9ScreenContentDefault) ||
		opts.ScreenContentMode > int8(VP9ScreenContentFilm) {
		return ErrInvalidConfig
	}
	if opts.NoiseSensitivity < 0 || opts.NoiseSensitivity > 6 {
		return ErrInvalidConfig
	}
	if opts.Sharpness > 7 {
		return ErrInvalidConfig
	}
	if opts.StaticThreshold < 0 {
		return ErrInvalidConfig
	}
	if err := validateVP9TwoPassOptions(opts); err != nil {
		return err
	}
	if err := validateVP9RateControlOptions(opts); err != nil {
		return err
	}
	if err := validateVP9AQOptions(opts); err != nil {
		return err
	}
	if err := validateVP9AutoAltRefOptions(opts); err != nil {
		return err
	}
	if err := validateVP9TPLOptions(opts); err != nil {
		return err
	}
	if opts.DeltaQUV < -15 || opts.DeltaQUV > 15 {
		return ErrInvalidQuantizer
	}
	if opts.Lossless && opts.DeltaQUV != 0 {
		return ErrInvalidQuantizer
	}
	if err := validateVP9ColorOptions(opts); err != nil {
		return err
	}
	if err := validateVP9RenderSizeOptions(opts); err != nil {
		return err
	}
	if err := validateVP9TargetLevel(opts.TargetLevel); err != nil {
		return err
	}
	if opts.DisableLoopfilter > VP9LoopfilterDisableAll {
		return ErrInvalidConfig
	}
	if _, err := normalizeVP9SpatialScalabilityConfig(opts.SpatialScalability,
		opts.Width, opts.Height); err != nil {
		return err
	}
	// Lookahead now composes with libvpx-style rate control modes (CBR, VBR,
	// Q) and temporal SVC. Cyclic-refresh AQ keeps its own lookahead block in
	// validateVP9AQOptions because its segment-map updates run in
	// committed-frame order and would re-target a queued source.
	if err := validateVP9FrameParallelEncoderOptions(opts); err != nil {
		return err
	}
	if opts.Quantizer > 255 {
		return ErrInvalidQuantizer
	}
	if err := validateVP9PublicQuantizerOptions(opts); err != nil {
		return err
	}
	if opts.Lossless && opts.Quantizer != 0 {
		return ErrInvalidQuantizer
	}
	if opts.FPS < 0 {
		return ErrInvalidConfig
	}
	if (opts.TimebaseNum < 0) || (opts.TimebaseDen < 0) {
		return ErrInvalidConfig
	}
	// Either FPS xor both timebase components must be set, or all
	// three may be zero (defaults to 30 fps in libvpx).
	if (opts.TimebaseNum != 0) != (opts.TimebaseDen != 0) {
		return ErrInvalidConfig
	}
	if err := validateVP9SegmentationOptions(opts.Segmentation); err != nil {
		return err
	}
	if opts.Lossless {
		if err := validateVP9LosslessSegmentationOptions(opts.Segmentation); err != nil {
			return err
		}
	}
	return nil
}

func normalizeVP9SpatialScalabilityConfig(cfg VP9SpatialScalabilityConfig,
	width, height int,
) (VP9SpatialScalabilityConfig, error) {
	if !cfg.Enabled {
		return VP9SpatialScalabilityConfig{}, nil
	}
	if cfg.LayerCount == 0 || cfg.LayerCount > VP9MaxSpatialLayers ||
		cfg.LayerID >= cfg.LayerCount {
		return VP9SpatialScalabilityConfig{}, ErrInvalidConfig
	}
	if cfg.InterLayerDependency && cfg.LayerID == 0 {
		return VP9SpatialScalabilityConfig{}, ErrInvalidConfig
	}
	if !cfg.ResolutionPresent {
		for i := range VP9RTPMaxSpatialLayers {
			if cfg.Width[i] != 0 || cfg.Height[i] != 0 {
				return VP9SpatialScalabilityConfig{}, ErrInvalidConfig
			}
		}
		return cfg, nil
	}
	for i := 0; i < int(cfg.LayerCount); i++ {
		if cfg.Width[i] == 0 || cfg.Height[i] == 0 {
			return VP9SpatialScalabilityConfig{}, ErrInvalidConfig
		}
	}
	for i := int(cfg.LayerCount); i < VP9RTPMaxSpatialLayers; i++ {
		if cfg.Width[i] != 0 || cfg.Height[i] != 0 {
			return VP9SpatialScalabilityConfig{}, ErrInvalidConfig
		}
	}
	if int(cfg.Width[cfg.LayerID]) != width ||
		int(cfg.Height[cfg.LayerID]) != height {
		return VP9SpatialScalabilityConfig{}, ErrInvalidConfig
	}
	return cfg, nil
}

func (e *VP9Encoder) vp9SpatialResultFields() (
	layerID uint8,
	layerCount uint8,
	interLayerDependency bool,
	notRefForUpperSpatialLayer bool,
	scalabilityStructurePresent bool,
	scalabilityStructure VP9RTPScalabilityStructure,
) {
	cfg := e.opts.SpatialScalability
	if !cfg.Enabled {
		return 0, 1, false, false, false, VP9RTPScalabilityStructure{}
	}
	if cfg.ResolutionPresent {
		scalabilityStructurePresent = true
		scalabilityStructure = VP9RTPScalabilityStructure{
			SpatialLayerCount: int(cfg.LayerCount),
			ResolutionPresent: true,
			Width:             cfg.Width,
			Height:            cfg.Height,
		}
	}
	return cfg.LayerID, cfg.LayerCount, cfg.InterLayerDependency,
		cfg.NotRefForUpperSpatialLayer, scalabilityStructurePresent,
		scalabilityStructure
}

// validateVP9ColorOptions rejects out-of-range ColorSpace/ColorRange
// values and the Profile 0 / SRGB combination libvpx rejects.
func validateVP9ColorOptions(opts VP9EncoderOptions) error {
	if opts.ColorSpace > VP9ColorSpaceSRGB {
		return ErrInvalidConfig
	}
	if opts.ColorRange > VP9ColorRangeFull {
		return ErrInvalidConfig
	}
	// Profile 0 streams use 4:2:0 chroma; SRGB requires 4:4:4 sampling
	// (allowed only on profiles 1 and 3) so the writer would emit a
	// stream the decoder rejects.
	if opts.ColorSpace == VP9ColorSpaceSRGB {
		return ErrInvalidConfig
	}
	return nil
}

// validateVP9RenderSizeOptions enforces the (0,0)-or-(positive,positive)
// shape of RenderWidth/RenderHeight and caps each at the 16-bit field
// width libvpx writes.
func validateVP9RenderSizeOptions(opts VP9EncoderOptions) error {
	w := opts.RenderWidth
	h := opts.RenderHeight
	if w == 0 && h == 0 {
		return nil
	}
	if w <= 0 || h <= 0 {
		return ErrInvalidConfig
	}
	if w > (1<<16) || h > (1<<16) {
		return ErrInvalidConfig
	}
	return nil
}

// vp9DisableLoopfilterForFrame reports whether the loop filter should
// be suppressed for the given frame, mirroring libvpx's
// VP9E_SET_DISABLE_LOOPFILTER semantics: mode 1 disables the filter
// on every non-keyframe; mode 2 disables it on every frame.
func vp9DisableLoopfilterForFrame(mode VP9DisableLoopfilter, isKey bool) bool {
	switch mode {
	case VP9LoopfilterDisableAll:
		return true
	case VP9LoopfilterDisableInter:
		return !isKey
	default:
		return false
	}
}

// vp9CommonColorSpace maps the public VP9ColorSpace enum onto the
// shared internal/vp9/common ColorSpace identifier.
func vp9CommonColorSpace(c VP9ColorSpace) common.ColorSpace {
	return common.ColorSpace(c)
}

// vp9CommonColorRange maps the public VP9ColorRange enum onto the
// shared internal/vp9/common ColorRange identifier.
func vp9CommonColorRange(c VP9ColorRange) common.ColorRange {
	return common.ColorRange(c)
}

func validateVP9TileRowOptions(width, height int, log2TileRows int8) error {
	if log2TileRows < 0 || log2TileRows > 2 {
		return ErrInvalidConfig
	}
	if log2TileRows == 0 {
		return nil
	}
	if !validVP9Dimension(width) || !validVP9Dimension(height) {
		return ErrInvalidConfig
	}
	tileRows := 1 << uint(log2TileRows)
	miRows := (height + 7) >> 3
	sbRows := (miRows + (1 << common.MiBlockSizeLog2) - 1) >> common.MiBlockSizeLog2
	if tileRows > sbRows {
		return ErrInvalidConfig
	}
	return nil
}

func validateVP9AutoAltRefOptions(opts VP9EncoderOptions) error {
	if !opts.AutoAltRef {
		return nil
	}
	if opts.LookaheadFrames <= 1 || opts.ErrorResilient {
		return ErrInvalidConfig
	}
	return nil
}

func validateVP9FrameParallelEncoderOptions(opts VP9EncoderOptions) error {
	if opts.FrameParallelEncoderThreads < 0 ||
		opts.FrameParallelEncoderThreads > vp9MaxLookaheadFrames {
		return ErrInvalidConfig
	}
	if opts.FrameParallelEncoderThreads >= 2 {
		if opts.LookaheadFrames <= 0 {
			return ErrInvalidConfig
		}
		if opts.AutoAltRef {
			return ErrInvalidConfig
		}
	}
	return nil
}

func validateVP9AQOptions(opts VP9EncoderOptions) error {
	switch opts.AQMode {
	case VP9AQNone:
		return nil
	case VP9AQVariance:
		if opts.Lossless || opts.Segmentation.Enabled {
			return ErrInvalidConfig
		}
		return nil
	case VP9AQComplexity:
		if !opts.RateControlModeSet || opts.TargetBitrateKbps <= 0 ||
			opts.Lossless || opts.Segmentation.Enabled {
			return ErrInvalidConfig
		}
		return nil
	case VP9AQEquator360:
		if opts.Lossless || opts.Segmentation.Enabled {
			return ErrInvalidConfig
		}
		return nil
	case VP9AQPerceptual:
		if opts.Lossless || opts.Segmentation.Enabled {
			return ErrInvalidConfig
		}
		return nil
	case VP9AQCyclicRefresh:
	default:
		return ErrInvalidConfig
	}
	if !opts.RateControlModeSet || opts.RateControlMode != RateControlCBR {
		return ErrInvalidConfig
	}
	if opts.LookaheadFrames > 0 || opts.TemporalScalability.Enabled ||
		opts.Lossless || opts.Segmentation.Enabled {
		return ErrInvalidConfig
	}
	return nil
}

func validateVP9SegmentationOptions(seg VP9SegmentationOptions) error {
	if !seg.Enabled {
		return nil
	}
	if seg.SegmentID >= vp9dec.MaxSegments {
		return ErrInvalidConfig
	}
	if !seg.UpdateMap && seg.SegmentID != 0 {
		return ErrInvalidConfig
	}
	for i := range vp9dec.MaxSegments {
		if seg.AltQEnabled[i] && (seg.AltQ[i] < -255 || seg.AltQ[i] > 255) {
			return ErrInvalidQuantizer
		}
		if seg.AltLFEnabled[i] && (seg.AltLF[i] < -63 || seg.AltLF[i] > 63) {
			return ErrInvalidConfig
		}
		if seg.RefFrameEnabled[i] &&
			(seg.RefFrame[i] < vp9dec.IntraFrame || seg.RefFrame[i] > vp9dec.AltrefFrame) {
			return ErrInvalidConfig
		}
	}
	return nil
}

func validateVP9LosslessSegmentationOptions(seg VP9SegmentationOptions) error {
	if !seg.Enabled {
		return nil
	}
	for i := range vp9dec.MaxSegments {
		if seg.AltQEnabled[i] {
			qindex := max(int(seg.AltQ[i]), 0)
			if qindex != 0 {
				return ErrInvalidQuantizer
			}
		}
		if seg.AltLFEnabled[i] {
			filterLevel := max(int(seg.AltLF[i]), 0)
			if filterLevel != 0 {
				return ErrInvalidConfig
			}
		}
	}
	return nil
}
