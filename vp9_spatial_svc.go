package govpx

import (
	"image"

	vp9bits "github.com/thesyncim/govpx/internal/vp9/bitstream"
)

// VP9SpatialSVCEncoderOptions configures a VP9 spatial-SVC access-unit
// encoder. Layer options are ordered from base layer to highest spatial layer;
// each layer keeps independent rate-control, entropy, and reference state.
type VP9SpatialSVCEncoderOptions struct {
	// LayerCount is the number of coded spatial layers. Valid values are
	// [2, VP9MaxSpatialLayers].
	LayerCount uint8
	// Layers holds the per-layer VP9 encoder options. Entries above LayerCount
	// are ignored. SpatialScalability is owned by the SVC encoder and must be
	// left disabled in each layer option. Lookahead, auto-alt-ref, and frame
	// dropping are not accepted because access-unit emission is synchronous.
	// If TemporalScalability is enabled in layer options, every coded layer
	// must enable the same temporal mode; per-layer target bitrates may differ.
	Layers [VP9MaxSpatialLayers]VP9EncoderOptions
	// InterLayerPrediction seeds each enhancement layer's LAST reference from
	// the just-encoded lower spatial layer and marks enhancement packets with
	// VP9 RTP inter-layer dependency metadata.
	InterLayerPrediction bool
}

// VP9SpatialSVCEncodeResult describes one VP9 spatial-SVC access unit. Data
// aliases the caller-owned output buffer and contains a VP9 superframe with one
// coded frame per spatial layer.
type VP9SpatialSVCEncodeResult struct {
	Data      []byte
	SizeBytes int

	LayerCount uint8
	Layers     [VP9MaxSpatialLayers]VP9EncodeResult

	InterLayerPrediction bool
	// ScalabilityStructure is the RTP VP9 scalability structure for the
	// access unit. It always carries spatial resolutions and carries temporal
	// picture groups when parent-owned temporal SVC uses a fixed pattern.
	ScalabilityStructure VP9RTPScalabilityStructure
}

type vp9SpatialSVCActiveLayerMetadata struct {
	spatial VP9SpatialScalabilityConfig
	svc     int
}

// LastLayerQuantizers mirrors libvpx's
// VP9E_GET_LAST_QUANTIZER_SVC_LAYERS control. It returns the public 0..63
// quantizer, internal VP9 qindex, and validity flag for each configured spatial
// layer's most recently committed encoded frame. Entries above the configured
// layer count, nil encoders, closed encoders, and layers that have not committed
// a frame report ok=false.
func (e *VP9SpatialSVCEncoder) LastLayerQuantizers() (
	public [VP9MaxSpatialLayers]int,
	internal [VP9MaxSpatialLayers]int,
	ok [VP9MaxSpatialLayers]bool,
) {
	if e == nil || e.closed {
		return public, internal, ok
	}
	for i := 0; i < int(e.layerCount); i++ {
		layer := e.layers[i]
		if layer == nil {
			continue
		}
		pub, q, valid := layer.LastQuantizer()
		if !valid {
			continue
		}
		public[i] = pub
		internal[i] = q
		ok[i] = true
	}
	return public, internal, ok
}

// VP9SpatialSVCEncoder encodes a complete VP9 spatial-SVC access unit into one
// VP9 superframe. The zero value is closed; construct with
// [NewVP9SpatialSVCEncoder].
type VP9SpatialSVCEncoder struct {
	closed               bool
	layerCount           uint8
	interLayerPrediction bool
	layers               [VP9MaxSpatialLayers]*VP9Encoder
	scalabilityStructure VP9RTPScalabilityStructure
	temporalMode         TemporalLayeringMode
	temporalEnabled      bool
	// libvpx carries cpi->alt_fb_idx across no-temporal SVC layers; fixed
	// temporal modes overwrite it from their per-frame slot tables.
	noTemporalAltRefIdx uint8
}

// NewVP9SpatialSVCEncoder creates a VP9 spatial-SVC encoder with one internal
// VP9Encoder per spatial layer.
func NewVP9SpatialSVCEncoder(opts VP9SpatialSVCEncoderOptions) (*VP9SpatialSVCEncoder, error) {
	if opts.LayerCount < 2 || opts.LayerCount > VP9MaxSpatialLayers {
		return nil, ErrInvalidConfig
	}
	count := int(opts.LayerCount)
	var widths [VP9RTPMaxSpatialLayers]uint16
	var heights [VP9RTPMaxSpatialLayers]uint16
	for i := range count {
		layer := opts.Layers[i]
		if layer.SpatialScalability.Enabled ||
			layer.LookaheadFrames != 0 || layer.AutoAltRef ||
			layer.DropFrameAllowed || layer.PostEncodeDrop {
			return nil, ErrInvalidConfig
		}
		if !validVP9Dimension(layer.Width) || !validVP9Dimension(layer.Height) ||
			layer.Width > int(^uint16(0)) || layer.Height > int(^uint16(0)) {
			return nil, ErrInvalidConfig
		}
		if i > 0 {
			prev := opts.Layers[i-1]
			if layer.Width < prev.Width || layer.Height < prev.Height ||
				(layer.Width == prev.Width && layer.Height == prev.Height) {
				return nil, ErrInvalidConfig
			}
			if opts.InterLayerPrediction &&
				!validVP9SpatialSVCInterLayerScale(prev.Width, prev.Height,
					layer.Width, layer.Height) {
				return nil, ErrInvalidConfig
			}
		}
		widths[i] = uint16(layer.Width)
		heights[i] = uint16(layer.Height)
	}
	temporalMode, temporalEnabled, err := vp9SpatialSVCTemporalMode(opts)
	if err != nil {
		return nil, err
	}
	svc := &VP9SpatialSVCEncoder{
		layerCount:           opts.LayerCount,
		interLayerPrediction: opts.InterLayerPrediction,
		scalabilityStructure: vp9SpatialSVCScalabilityStructure(widths,
			heights, count, temporalMode, temporalEnabled,
			opts.InterLayerPrediction),
		temporalMode:        temporalMode,
		temporalEnabled:     temporalEnabled,
		noTemporalAltRefIdx: vp9AltRefSlot,
	}
	// libvpx: vp9_svc_layercontext.c vp9_init_layer_context() — derive
	// number_temporal_layers from cpi->oxcf.ts_number_layers, which on the
	// VP9 spatial-SVC encoder is the LAYER_COUNT temporal pattern for every
	// spatial layer. govpx asserts every layer carries the same temporal
	// scalability mode in vp9SpatialSVCTemporalMode above.
	numberTemporalLayers := 1
	if temporalEnabled {
		if pattern, ok := temporalLayeringPattern(temporalMode); ok &&
			pattern.Layers > 1 {
			numberTemporalLayers = pattern.Layers
		}
	}
	for i := range count {
		layerOpts := opts.Layers[i]
		// libvpx examples/vp9_spatial_svc_encoder.c emits VP9 spatial SVC in
		// error-resilient mode, including spatial-only streams.
		layerOpts.ErrorResilient = true
		spatial := VP9SpatialScalabilityConfig{
			Enabled:                    true,
			LayerCount:                 opts.LayerCount,
			LayerID:                    uint8(i),
			InterLayerDependency:       opts.InterLayerPrediction && i > 0,
			NotRefForUpperSpatialLayer: !opts.InterLayerPrediction || i == count-1,
		}
		if i == 0 {
			spatial.ResolutionPresent = true
			spatial.Width = widths
			spatial.Height = heights
		}
		layerOpts.SpatialScalability = spatial
		layer, err := NewVP9Encoder(layerOpts)
		if err != nil {
			_ = svc.Close()
			return nil, err
		}
		layer.resetVP9EncoderFrameContexts()
		layer.spatialScalabilityLocked = true
		layer.temporalScalabilityLocked = true
		// libvpx: vp9_svc_layercontext.c vp9_init_layer_context() and
		// vp9_one_pass_svc_start_layer() — populate cpi->use_svc and
		// cpi->svc.{spatial_layer_id, number_spatial_layers,
		// number_temporal_layers} before any speed-features dispatch.
		layer.svc.UseSvc = true
		layer.svc.SpatialLayerID = i
		layer.svc.NumberSpatialLayers = count
		layer.svc.NumberTemporalLayers = numberTemporalLayers
		layer.vp9ApplySpeedFeatures(layer.vp9DefaultSpeedFrameContext())
		svc.layers[i] = layer
	}
	return svc, nil
}

func vp9SpatialSVCTemporalMode(opts VP9SpatialSVCEncoderOptions) (TemporalLayeringMode, bool, error) {
	count := int(opts.LayerCount)
	if count <= 0 || count > VP9MaxSpatialLayers {
		return TemporalLayeringOneLayer, false, ErrInvalidConfig
	}
	base := opts.Layers[0].TemporalScalability
	for i := 1; i < count; i++ {
		cfg := opts.Layers[i].TemporalScalability
		if cfg.Enabled != base.Enabled {
			return TemporalLayeringOneLayer, false, ErrInvalidConfig
		}
		if cfg.Enabled && cfg.Mode != base.Mode {
			return TemporalLayeringOneLayer, false, ErrInvalidConfig
		}
	}
	if !base.Enabled {
		return TemporalLayeringOneLayer, false, nil
	}
	return base.Mode, true, nil
}

func vp9SpatialSVCScalabilityStructure(
	widths, heights [VP9RTPMaxSpatialLayers]uint16,
	layerCount int,
	temporalMode TemporalLayeringMode,
	temporalEnabled bool,
	interLayerPrediction bool,
) VP9RTPScalabilityStructure {
	ss := VP9RTPScalabilityStructure{
		SpatialLayerCount: layerCount,
		ResolutionPresent: true,
		Width:             widths,
		Height:            heights,
	}
	if temporalEnabled {
		vp9SetScalabilityStructureTemporalPattern(&ss, temporalMode,
			interLayerPrediction)
	}
	return ss
}

func vp9SetScalabilityStructureTemporalPatternFromConfig(
	ss *VP9RTPScalabilityStructure,
	cfg TemporalScalabilityConfig,
	interLayerPrediction bool,
) {
	if cfg.Enabled {
		vp9SetScalabilityStructureTemporalPattern(ss, cfg.Mode,
			interLayerPrediction)
		return
	}
	vp9ClearScalabilityStructureTemporalPattern(ss)
}

func vp9SetScalabilityStructureTemporalPattern(
	ss *VP9RTPScalabilityStructure,
	mode TemporalLayeringMode,
	interLayerPrediction bool,
) {
	groups := vp9TemporalScalabilityPictureGroups(mode,
		interLayerPrediction)
	if len(groups) == 0 {
		vp9ClearScalabilityStructureTemporalPattern(ss)
		return
	}
	ss.PictureGroupPresent = true
	ss.PictureGroups = groups
}

func vp9ClearScalabilityStructureTemporalPattern(ss *VP9RTPScalabilityStructure) {
	ss.PictureGroupPresent = false
	ss.PictureGroups = nil
}

func vp9TemporalScalabilityPictureGroups(
	mode TemporalLayeringMode,
	interLayerPrediction bool,
) []VP9RTPPictureGroup {
	if interLayerPrediction {
		if groups, ok := vp9WebRTCTemporalScalabilityPictureGroups(mode); ok {
			return groups
		}
	}
	return vp9GenericTemporalScalabilityPictureGroups(mode)
}

func vp9GenericTemporalScalabilityPictureGroups(mode TemporalLayeringMode) []VP9RTPPictureGroup {
	pattern, ok := temporalLayeringPattern(mode)
	if !ok || pattern.Layers <= 1 || pattern.Periodicity <= 0 ||
		pattern.FlagPeriodicity <= 0 ||
		pattern.Periodicity > maxTemporalPeriodSize {
		return nil
	}
	groups := make([]VP9RTPPictureGroup, pattern.Periodicity)
	var refIndex [temporalReferenceCount]int
	for i := range refIndex {
		refIndex[i] = -1
	}
	var refLayer [temporalReferenceCount]int
	for frame := 0; frame < pattern.Periodicity*2; frame++ {
		patternIndex := frame % pattern.Periodicity
		flags := vp9TemporalPatternFlagsAt(pattern, mode, frame)
		layerID := pattern.LayerID[patternIndex]
		keyFrame := flags&EncodeForceKeyFrame != 0
		group := VP9RTPPictureGroup{
			TemporalID:       uint8(layerID),
			SwitchingUpPoint: vp9TemporalPatternLayerSync(layerID, flags, refLayer),
		}
		if !keyFrame {
			vp9AddTemporalPictureGroupReference(&group, frame, refIndex[temporalReferenceLast],
				flags&EncodeNoReferenceLast == 0)
			vp9AddTemporalPictureGroupReference(&group, frame, refIndex[temporalReferenceGolden],
				flags&EncodeNoReferenceGolden == 0)
			vp9AddTemporalPictureGroupReference(&group, frame, refIndex[temporalReferenceAltRef],
				flags&EncodeNoReferenceAltRef == 0)
		}
		if frame >= pattern.Periodicity {
			groups[patternIndex] = group
		}
		vp9UpdateTemporalPatternReferences(&refIndex, &refLayer, frame, layerID, flags,
			keyFrame)
	}
	return groups
}

func vp9WebRTCTemporalScalabilityPictureGroups(
	mode TemporalLayeringMode,
) ([]VP9RTPPictureGroup, bool) {
	switch mode {
	case TemporalLayeringTwoLayers:
		return []VP9RTPPictureGroup{
			{
				TemporalID:          0,
				ReferenceIndexCount: 1,
				ReferenceIndices: [VP9RTPMaxReferenceIndices]uint8{
					2,
				},
			},
			{
				TemporalID:          1,
				SwitchingUpPoint:    true,
				ReferenceIndexCount: 1,
				ReferenceIndices: [VP9RTPMaxReferenceIndices]uint8{
					1,
				},
			},
		}, true
	case TemporalLayeringThreeLayers:
		return []VP9RTPPictureGroup{
			{
				TemporalID:          0,
				ReferenceIndexCount: 1,
				ReferenceIndices: [VP9RTPMaxReferenceIndices]uint8{
					4,
				},
			},
			{
				TemporalID:          2,
				SwitchingUpPoint:    true,
				ReferenceIndexCount: 1,
				ReferenceIndices: [VP9RTPMaxReferenceIndices]uint8{
					1,
				},
			},
			{
				TemporalID:          1,
				SwitchingUpPoint:    true,
				ReferenceIndexCount: 1,
				ReferenceIndices: [VP9RTPMaxReferenceIndices]uint8{
					2,
				},
			},
			{
				TemporalID:          2,
				SwitchingUpPoint:    true,
				ReferenceIndexCount: 1,
				ReferenceIndices: [VP9RTPMaxReferenceIndices]uint8{
					1,
				},
			},
		}, true
	default:
		return nil, false
	}
}

func vp9TemporalPatternFlagsAt(
	pattern temporalPattern,
	mode TemporalLayeringMode,
	frame int,
) EncodeFlags {
	flags := pattern.Flags[frame%pattern.FlagPeriodicity]
	if mode != TemporalLayeringFiveLayers && frame > 0 &&
		frame%pattern.FlagPeriodicity == 0 {
		flags &^= EncodeForceKeyFrame
	}
	return flags
}

func vp9TemporalPatternLayerSync(
	layerID int,
	flags EncodeFlags,
	refLayer [temporalReferenceCount]int,
) bool {
	if layerID <= 0 {
		return false
	}
	if flags&EncodeNoReferenceLast == 0 &&
		refLayer[temporalReferenceLast] >= layerID {
		return false
	}
	if flags&EncodeNoReferenceGolden == 0 &&
		refLayer[temporalReferenceGolden] >= layerID {
		return false
	}
	if flags&EncodeNoReferenceAltRef == 0 &&
		refLayer[temporalReferenceAltRef] >= layerID {
		return false
	}
	return true
}

func vp9AddTemporalPictureGroupReference(
	group *VP9RTPPictureGroup,
	frame int,
	ref int,
	allowed bool,
) {
	if group == nil || !allowed || ref < 0 || ref >= frame ||
		group.ReferenceIndexCount >= VP9RTPMaxReferenceIndices {
		return
	}
	diff := frame - ref
	if diff <= 0 || diff > int(^uint8(0)) {
		return
	}
	refDiff := uint8(diff)
	for i := 0; i < group.ReferenceIndexCount; i++ {
		if group.ReferenceIndices[i] == refDiff {
			return
		}
	}
	group.ReferenceIndices[group.ReferenceIndexCount] = refDiff
	group.ReferenceIndexCount++
}

func vp9UpdateTemporalPatternReferences(
	refIndex *[temporalReferenceCount]int,
	refLayer *[temporalReferenceCount]int,
	frame int,
	layerID int,
	flags EncodeFlags,
	keyFrame bool,
) {
	if keyFrame {
		for i := range refIndex {
			refIndex[i] = frame
			refLayer[i] = 0
		}
		return
	}
	if flags&EncodeNoUpdateLast == 0 {
		refIndex[temporalReferenceLast] = frame
		refLayer[temporalReferenceLast] = layerID
	}
	if flags&EncodeNoUpdateGolden == 0 {
		refIndex[temporalReferenceGolden] = frame
		refLayer[temporalReferenceGolden] = layerID
	}
	if flags&EncodeNoUpdateAltRef == 0 {
		refIndex[temporalReferenceAltRef] = frame
		refLayer[temporalReferenceAltRef] = layerID
	}
}

// EncodeInto encodes one source frame per spatial layer into dst and returns
// the number of bytes written. It is equivalent to EncodeIntoWithResult while
// discarding access-unit metadata.
func (e *VP9SpatialSVCEncoder) EncodeInto(srcs []*image.YCbCr, dst []byte) (int, error) {
	result, err := e.EncodeIntoWithResult(srcs, dst)
	return len(result.Data), err
}

// EncodeIntoWithResult encodes one source frame per spatial layer into dst as
// a VP9 superframe. srcs must contain exactly LayerCount images ordered from
// base layer to highest layer.
func (e *VP9SpatialSVCEncoder) EncodeIntoWithResult(srcs []*image.YCbCr, dst []byte) (VP9SpatialSVCEncodeResult, error) {
	if e == nil || e.closed {
		return VP9SpatialSVCEncodeResult{}, ErrClosed
	}
	return e.EncodeActiveLayersIntoWithResult(srcs, dst, int(e.layerCount))
}

// EncodeActiveLayersIntoWithResult encodes a base-to-layer-prefix VP9
// spatial-SVC access unit into dst. activeLayerCount selects how many
// configured layers are encoded and described in the returned result; srcs may
// contain either that prefix or the full configured layer list.
//
// WebRTC senders should force a key access unit before changing
// activeLayerCount across calls. The returned access unit is ready for
// PacketizeWebRTCRTP without needing LimitSpatialLayersForRTP, so lowering the
// active count also avoids encoding hidden enhancement layers.
func (e *VP9SpatialSVCEncoder) EncodeActiveLayersIntoWithResult(
	srcs []*image.YCbCr,
	dst []byte,
	activeLayerCount int,
) (VP9SpatialSVCEncodeResult, error) {
	if e == nil || e.closed {
		return VP9SpatialSVCEncodeResult{}, ErrClosed
	}
	parentCount := int(e.layerCount)
	if activeLayerCount <= 0 || activeLayerCount > parentCount ||
		len(srcs) < activeLayerCount || len(srcs) > parentCount {
		return VP9SpatialSVCEncodeResult{}, ErrInvalidConfig
	}
	for i := range activeLayerCount {
		if err := e.layers[i].validateVP9EncoderSource(srcs[i]); err != nil {
			return VP9SpatialSVCEncodeResult{}, err
		}
	}

	maxIndexSize := 2 + activeLayerCount*4
	if len(dst) < activeLayerCount*vp9MinEncodeIntoBuffer+maxIndexSize {
		return VP9SpatialSVCEncodeResult{}, ErrBufferTooSmall
	}

	if activeLayerCount == parentCount {
		return e.encodeActiveLayersIntoWithResult(srcs, dst, activeLayerCount,
			maxIndexSize)
	}
	var oldMetadata [VP9MaxSpatialLayers]vp9SpatialSVCActiveLayerMetadata
	e.applyActiveLayerMetadata(activeLayerCount, &oldMetadata)
	result, err := e.encodeActiveLayersIntoWithResult(srcs, dst,
		activeLayerCount, maxIndexSize)
	e.restoreActiveLayerMetadata(activeLayerCount, &oldMetadata)
	return result, err
}

func (e *VP9SpatialSVCEncoder) encodeActiveLayersIntoWithResult(
	srcs []*image.YCbCr,
	dst []byte,
	activeLayerCount int,
	maxIndexSize int,
) (VP9SpatialSVCEncodeResult, error) {
	var result VP9SpatialSVCEncodeResult
	var frameSizes [VP9MaxSpatialLayers]int
	offset := 0
	encodeLimit := len(dst) - maxIndexSize
	baseKeyFrame := false
	baseTL0PICIDX := uint8(0)
	for i := range activeLayerCount {
		if encodeLimit-offset < vp9MinEncodeIntoBuffer {
			return VP9SpatialSVCEncodeResult{}, ErrBufferTooSmall
		}
		layer := e.layers[i]
		if i > 0 && e.interLayerPrediction {
			layer.forceKeyFrame = false
			if baseKeyFrame {
				layer.temporal.restartInterLayerKeyAccessUnitAtTL0(
					baseTL0PICIDX)
			}
		}
		if e.interLayerPrediction {
			svcFrameIndex := layer.frameIndex
			if e.temporalEnabled {
				svcFrameIndex = int(layer.temporal.frameIndex)
			}
			if cfg, ok := vp9SpatialSVCReferenceFrameConfig(i,
				activeLayerCount,
				e.temporalMode, e.temporalEnabled, svcFrameIndex,
				baseKeyFrame, e.noTemporalAltRefIdx); ok {
				layer.svcRefConfig = cfg
				if !e.temporalEnabled {
					e.noTemporalAltRefIdx = cfg.refIndex[2]
				}
			} else {
				layer.svcRefConfig = vp9SVCReferenceFrameConfig{}
			}
		} else {
			layer.svcRefConfig = vp9SVCReferenceFrameConfig{}
		}
		var layerResult VP9EncodeResult
		var err error
		if i > 0 && e.interLayerPrediction {
			if !layer.seedVP9InterLayerReference(e.layers[i-1]) {
				layer.svcRefConfig = vp9SVCReferenceFrameConfig{}
				return VP9SpatialSVCEncodeResult{}, ErrInvalidConfig
			}
			layerResult, err = layer.encodeVP9InterLayerIntoWithFlagsResult(
				srcs[i], dst[offset:encodeLimit],
				vp9SpatialSVCInterLayerEncodeFlags(uint8(i)))
		} else if i == 0 {
			layerResult, err = layer.encodeVP9SpatialSVCBaseIntoWithFlagsResult(
				srcs[i], dst[offset:encodeLimit], 0)
		} else {
			layerResult, err = layer.EncodeIntoWithResult(srcs[i],
				dst[offset:encodeLimit])
		}
		layer.svcRefConfig = vp9SVCReferenceFrameConfig{}
		if err != nil {
			return VP9SpatialSVCEncodeResult{}, err
		}
		if layerResult.Dropped || len(layerResult.Data) == 0 {
			return VP9SpatialSVCEncodeResult{}, ErrInvalidConfig
		}
		if i == 0 {
			baseKeyFrame = layerResult.KeyFrame
			baseTL0PICIDX = layerResult.TL0PICIDX
		}
		if i == 0 && layerResult.ScalabilityStructurePresent {
			layerResult.SpatialScalabilityStructure =
				limitVP9RTPScalabilityStructure(e.scalabilityStructure,
					activeLayerCount)
		}
		size := len(layerResult.Data)
		frameSizes[i] = size
		layerResult.Data = dst[offset : offset+size]
		result.Layers[i] = layerResult
		offset += size
	}

	indexSize, err := appendVP9SpatialSVCSuperframeIndex(dst[offset:], &frameSizes, activeLayerCount)
	if err != nil {
		return VP9SpatialSVCEncodeResult{}, err
	}
	result.Data = dst[:offset+indexSize]
	result.SizeBytes = len(result.Data)
	result.LayerCount = uint8(activeLayerCount)
	result.InterLayerPrediction = e.interLayerPrediction
	result.ScalabilityStructure = limitVP9RTPScalabilityStructure(
		e.scalabilityStructure, activeLayerCount)
	return result, nil
}

func (e *VP9SpatialSVCEncoder) applyActiveLayerMetadata(
	activeLayerCount int,
	old *[VP9MaxSpatialLayers]vp9SpatialSVCActiveLayerMetadata,
) {
	for i := range activeLayerCount {
		layer := e.layers[i]
		old[i] = vp9SpatialSVCActiveLayerMetadata{
			spatial: layer.opts.SpatialScalability,
			svc:     layer.svc.NumberSpatialLayers,
		}
		spatial := layer.opts.SpatialScalability
		spatial.LayerCount = uint8(activeLayerCount)
		spatial.NotRefForUpperSpatialLayer = !e.interLayerPrediction ||
			i == activeLayerCount-1
		for j := activeLayerCount; j < VP9RTPMaxSpatialLayers; j++ {
			spatial.Width[j] = 0
			spatial.Height[j] = 0
		}
		layer.opts.SpatialScalability = spatial
		layer.svc.NumberSpatialLayers = activeLayerCount
	}
}

func (e *VP9SpatialSVCEncoder) restoreActiveLayerMetadata(
	activeLayerCount int,
	old *[VP9MaxSpatialLayers]vp9SpatialSVCActiveLayerMetadata,
) {
	for i := range activeLayerCount {
		layer := e.layers[i]
		layer.opts.SpatialScalability = old[i].spatial
		layer.svc.NumberSpatialLayers = old[i].svc
	}
}

// LayerEncoder returns the internal encoder for layerID so callers can apply
// VP9 runtime controls to one layer. Do not close the returned encoder or
// change its spatial or temporal scalability configuration. Size-changing
// realtime target updates are rejected while the layer is owned by the SVC
// encoder; close the parent SVC encoder and use the parent's temporal controls.
func (e *VP9SpatialSVCEncoder) LayerEncoder(layerID uint8) (*VP9Encoder, error) {
	return e.layerEncoder(layerID)
}

func (e *VP9SpatialSVCEncoder) layerEncoder(layerID uint8) (*VP9Encoder, error) {
	if e == nil || e.closed {
		return nil, ErrClosed
	}
	if layerID >= e.layerCount {
		return nil, ErrInvalidConfig
	}
	return e.layers[layerID], nil
}

// SetLayerBitrateKbps changes one spatial layer's VP9 explicit rate-control
// target bitrate, in kbps. The target layer must have VP9 rate control
// enabled, matching [VP9Encoder.SetBitrateKbps].
func (e *VP9SpatialSVCEncoder) SetLayerBitrateKbps(layerID uint8, kbps int) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetBitrateKbps(kbps)
}

// SetLayerMaxIntraBitratePct changes one spatial layer's VP9 key-frame
// bitrate cap, matching [VP9Encoder.SetMaxIntraBitratePct].
func (e *VP9SpatialSVCEncoder) SetLayerMaxIntraBitratePct(layerID uint8, pct int) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetMaxIntraBitratePct(pct)
}

// SetLayerMaxInterBitratePct changes one spatial layer's VP9 inter-frame
// bitrate cap, matching [VP9Encoder.SetMaxInterBitratePct].
func (e *VP9SpatialSVCEncoder) SetLayerMaxInterBitratePct(layerID uint8, pct int) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetMaxInterBitratePct(pct)
}

// SetLayerGFCBRBoostPct changes one spatial layer's VP9 CBR golden-frame
// boost percentage, matching [VP9Encoder.SetGFCBRBoostPct].
func (e *VP9SpatialSVCEncoder) SetLayerGFCBRBoostPct(layerID uint8, pct int) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetGFCBRBoostPct(pct)
}

// SetLayerRateControl replaces one spatial layer's VP9 runtime-updatable
// rate-control configuration, matching [VP9Encoder.SetRateControl].
func (e *VP9SpatialSVCEncoder) SetLayerRateControl(layerID uint8, cfg RateControlConfig) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetRateControl(cfg)
}

// SetLayerRealtimeTarget applies one spatial layer's sparse VP9 realtime
// target update, matching [VP9Encoder.SetRealtimeTarget]. Size-changing and
// frame-drop-enabling updates are rejected while the layer is owned by the SVC
// encoder.
func (e *VP9SpatialSVCEncoder) SetLayerRealtimeTarget(layerID uint8,
	target RealtimeTarget,
) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetRealtimeTarget(target)
}

// SetLayerCQLevel changes one spatial layer's VP9 public CQ/Q level,
// matching [VP9Encoder.SetCQLevel].
func (e *VP9SpatialSVCEncoder) SetLayerCQLevel(layerID uint8, level int) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetCQLevel(level)
}

// SetLayerAQMode changes one spatial layer's VP9 adaptive quantization mode,
// matching [VP9Encoder.SetAQMode].
func (e *VP9SpatialSVCEncoder) SetLayerAQMode(layerID uint8, mode VP9AQMode) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetAQMode(mode)
}

// SetLayerAutoAltRef changes one spatial layer's VP9 auto-alt-ref toggle,
// matching [VP9Encoder.SetAutoAltRef]. Enabling remains invalid for
// synchronous spatial SVC layers because they have no lookahead queue.
func (e *VP9SpatialSVCEncoder) SetLayerAutoAltRef(layerID uint8, enabled bool) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetAutoAltRef(enabled)
}

// SetLayerFrameParallelDecoding changes one spatial layer's VP9
// frame-parallel decoding signal, matching
// [VP9Encoder.SetFrameParallelDecoding].
func (e *VP9SpatialSVCEncoder) SetLayerFrameParallelDecoding(layerID uint8, enabled bool) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetFrameParallelDecoding(enabled)
}

// SetLayerFrameParallelEncoderThreads changes one spatial layer's VP9
// frame-parallel encoder thread setting, matching
// [VP9Encoder.SetFrameParallelEncoderThreads].
func (e *VP9SpatialSVCEncoder) SetLayerFrameParallelEncoderThreads(layerID uint8, threads int) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetFrameParallelEncoderThreads(threads)
}

// SetLayerFrameDropAllowed changes one spatial layer's VP9 CBR frame-drop
// toggle, matching [VP9Encoder.SetFrameDropAllowed]. Enabling is rejected
// because spatial-SVC access-unit emission is synchronous.
func (e *VP9SpatialSVCEncoder) SetLayerFrameDropAllowed(layerID uint8, enabled bool) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetFrameDropAllowed(enabled)
}

// SetLayerPostEncodeDrop changes one spatial layer's VP9 CBR post-encode drop
// path, matching [VP9Encoder.SetPostEncodeDrop]. Enabling is rejected because
// spatial-SVC access-unit emission is synchronous.
func (e *VP9SpatialSVCEncoder) SetLayerPostEncodeDrop(layerID uint8, enabled bool) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetPostEncodeDrop(enabled)
}

// SetLayerDisableOvershootMaxQCBR changes one spatial layer's VP9 CBR
// overshoot max-Q guard, matching [VP9Encoder.SetDisableOvershootMaxQCBR].
func (e *VP9SpatialSVCEncoder) SetLayerDisableOvershootMaxQCBR(layerID uint8, enabled bool) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetDisableOvershootMaxQCBR(enabled)
}

// SetLayerRateControlBuffer changes one spatial layer's VP9 CBR buffer model,
// matching [VP9Encoder.SetRateControlBuffer].
func (e *VP9SpatialSVCEncoder) SetLayerRateControlBuffer(layerID uint8,
	sizeMs, initialMs, optimalMs int,
) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetRateControlBuffer(sizeMs, initialMs, optimalMs)
}

// SetLayerTwoPassStats replaces one spatial layer's VP9 second-pass stats,
// matching [VP9Encoder.SetTwoPassStats].
func (e *VP9SpatialSVCEncoder) SetLayerTwoPassStats(layerID uint8,
	stats []VP9FirstPassFrameStats,
) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetTwoPassStats(stats)
}

// SetLayerDeadline changes one spatial layer's VP9 speed/quality deadline,
// matching [VP9Encoder.SetDeadline].
func (e *VP9SpatialSVCEncoder) SetLayerDeadline(layerID uint8, deadline Deadline) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetDeadline(deadline)
}

// SetLayerCPUUsed changes one spatial layer's VP9 cpu-used speed preset,
// matching [VP9Encoder.SetCPUUsed].
func (e *VP9SpatialSVCEncoder) SetLayerCPUUsed(layerID uint8, cpuUsed int) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetCPUUsed(cpuUsed)
}

// SetLayerTuning changes one spatial layer's VP9 tuning model, matching
// [VP9Encoder.SetTuning].
func (e *VP9SpatialSVCEncoder) SetLayerTuning(layerID uint8, tuning Tuning) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetTuning(tuning)
}

// SetLayerLossless enables or disables one spatial layer's VP9 lossless mode,
// matching [VP9Encoder.SetLossless].
func (e *VP9SpatialSVCEncoder) SetLayerLossless(layerID uint8, enabled bool) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetLossless(enabled)
}

// SetLayerColorSpace changes one spatial layer's VP9 bitstream color-space
// tag, matching [VP9Encoder.SetColorSpace].
func (e *VP9SpatialSVCEncoder) SetLayerColorSpace(layerID uint8, cs VP9ColorSpace) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetColorSpace(cs)
}

// SetLayerColorRange changes one spatial layer's VP9 bitstream color-range
// tag, matching [VP9Encoder.SetColorRange].
func (e *VP9SpatialSVCEncoder) SetLayerColorRange(layerID uint8, cr VP9ColorRange) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetColorRange(cr)
}

// SetLayerRenderSize changes one spatial layer's VP9 display-render size hint,
// matching [VP9Encoder.SetRenderSize].
func (e *VP9SpatialSVCEncoder) SetLayerRenderSize(layerID uint8, width, height int) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetRenderSize(width, height)
}

// SetLayerTargetLevel changes one spatial layer's VP9 target level constraint,
// matching [VP9Encoder.SetTargetLevel].
func (e *VP9SpatialSVCEncoder) SetLayerTargetLevel(layerID uint8, level int) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetTargetLevel(level)
}

// SetLayerDisableLoopfilter changes one spatial layer's VP9 loopfilter disable
// mode, matching [VP9Encoder.SetDisableLoopfilter].
func (e *VP9SpatialSVCEncoder) SetLayerDisableLoopfilter(layerID uint8, mode VP9DisableLoopfilter) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetDisableLoopfilter(mode)
}

// SetLayerDeltaQUV changes one spatial layer's VP9 chroma quantizer delta,
// matching [VP9Encoder.SetDeltaQUV].
func (e *VP9SpatialSVCEncoder) SetLayerDeltaQUV(layerID uint8, delta int) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetDeltaQUV(delta)
}

// SetLayerScreenContentMode changes one spatial layer's VP9 content tuning,
// matching [VP9Encoder.SetScreenContentMode].
func (e *VP9SpatialSVCEncoder) SetLayerScreenContentMode(layerID uint8, mode int) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetScreenContentMode(mode)
}

// SetLayerNoiseSensitivity changes one spatial layer's VP9 temporal denoiser
// level, matching [VP9Encoder.SetNoiseSensitivity].
func (e *VP9SpatialSVCEncoder) SetLayerNoiseSensitivity(layerID uint8, level int) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetNoiseSensitivity(level)
}

// SetLayerSharpness changes one spatial layer's VP9 loop-filter sharpness,
// matching [VP9Encoder.SetSharpness].
func (e *VP9SpatialSVCEncoder) SetLayerSharpness(layerID uint8, sharpness uint8) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetSharpness(sharpness)
}

// SetLayerStaticThreshold changes one spatial layer's VP9 static-block
// breakout threshold, matching [VP9Encoder.SetStaticThreshold].
func (e *VP9SpatialSVCEncoder) SetLayerStaticThreshold(layerID uint8, threshold int) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetStaticThreshold(threshold)
}

// SetLayerKeyFrameInterval changes one spatial layer's VP9 maximum GOP
// distance, matching [VP9Encoder.SetKeyFrameInterval].
func (e *VP9SpatialSVCEncoder) SetLayerKeyFrameInterval(layerID uint8, frames int) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetKeyFrameInterval(frames)
}

// SetLayerKeyFrameIntervalRange changes one spatial layer's VP9 minimum and
// maximum key-frame distances, matching [VP9Encoder.SetKeyFrameIntervalRange].
func (e *VP9SpatialSVCEncoder) SetLayerKeyFrameIntervalRange(layerID uint8,
	minFrames, maxFrames int,
) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetKeyFrameIntervalRange(minFrames, maxFrames)
}

// SetLayerAdaptiveKeyFrames enables or disables one spatial layer's VP9
// content-driven keyframe promotion, matching [VP9Encoder.SetAdaptiveKeyFrames].
func (e *VP9SpatialSVCEncoder) SetLayerAdaptiveKeyFrames(layerID uint8, enabled bool) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetAdaptiveKeyFrames(enabled)
}

// SetLayerRTCExternalRateControl changes one spatial layer's VP9 external RTC
// rate-control toggle, matching [VP9Encoder.SetRTCExternalRateControl].
func (e *VP9SpatialSVCEncoder) SetLayerRTCExternalRateControl(layerID uint8, enabled bool) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetRTCExternalRateControl(enabled)
}

// SetLayerRowMT changes one spatial layer's VP9 row-multithreading toggle,
// matching [VP9Encoder.SetRowMT].
func (e *VP9SpatialSVCEncoder) SetLayerRowMT(layerID uint8, enabled bool) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetRowMT(enabled)
}

// SetLayerARNR changes one spatial layer's VP9 auto-alt-ref temporal filtering
// controls, matching [VP9Encoder.SetARNR].
func (e *VP9SpatialSVCEncoder) SetLayerARNR(layerID uint8,
	maxFrames int, strength int, filterType int,
) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetARNR(maxFrames, strength, filterType)
}

// SetLayerMinGFInterval changes one spatial layer's VP9 minimum golden-frame
// interval, matching [VP9Encoder.SetMinGFInterval].
func (e *VP9SpatialSVCEncoder) SetLayerMinGFInterval(layerID uint8, interval int) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetMinGFInterval(interval)
}

// SetLayerMaxGFInterval changes one spatial layer's VP9 maximum golden-frame
// interval, matching [VP9Encoder.SetMaxGFInterval].
func (e *VP9SpatialSVCEncoder) SetLayerMaxGFInterval(layerID uint8, interval int) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetMaxGFInterval(interval)
}

// SetLayerFramePeriodicBoost changes one spatial layer's VP9 periodic
// golden-frame boost toggle, matching [VP9Encoder.SetFramePeriodicBoost].
func (e *VP9SpatialSVCEncoder) SetLayerFramePeriodicBoost(layerID uint8, enabled bool) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetFramePeriodicBoost(enabled)
}

// SetLayerAltRefAQ changes one spatial layer's VP9 alt-ref AQ toggle,
// matching [VP9Encoder.SetAltRefAQ].
func (e *VP9SpatialSVCEncoder) SetLayerAltRefAQ(layerID uint8, enabled bool) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetAltRefAQ(enabled)
}

// SetLayerEnableKeyFrameFiltering changes one spatial layer's VP9 key-frame
// filtering toggle, matching [VP9Encoder.SetEnableKeyFrameFiltering].
func (e *VP9SpatialSVCEncoder) SetLayerEnableKeyFrameFiltering(layerID uint8, enabled bool) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetEnableKeyFrameFiltering(enabled)
}

// SetLayerEnableTPL changes one spatial layer's VP9 TPL toggle, matching
// [VP9Encoder.SetEnableTPL]. Enabling remains invalid for synchronous spatial
// SVC layers because TPL requires auto-alt-ref lookahead.
func (e *VP9SpatialSVCEncoder) SetLayerEnableTPL(layerID uint8, enabled bool) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetEnableTPL(enabled)
}

// SetLayerNextFrameQIndex changes one spatial layer's one-shot qindex
// override, matching [VP9Encoder.SetNextFrameQIndex].
func (e *VP9SpatialSVCEncoder) SetLayerNextFrameQIndex(layerID uint8, qindex int) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetNextFrameQIndex(qindex)
}

// GetLayerActiveMap snapshots one spatial layer's VP9 active map into
// activeMap, matching [VP9Encoder.GetActiveMap].
func (e *VP9SpatialSVCEncoder) GetLayerActiveMap(layerID uint8,
	activeMap []uint8, rows int, cols int,
) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.GetActiveMap(activeMap, rows, cols)
}

// SetLayerActiveMap installs one spatial layer's VP9 active map, matching
// [VP9Encoder.SetActiveMap].
func (e *VP9SpatialSVCEncoder) SetLayerActiveMap(layerID uint8,
	activeMap []uint8, rows int, cols int,
) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetActiveMap(activeMap, rows, cols)
}

// SetLayerROIMap installs one spatial layer's VP9 ROI map, matching
// [VP9Encoder.SetROIMap].
func (e *VP9SpatialSVCEncoder) SetLayerROIMap(layerID uint8, roi *ROIMap) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetROIMap(roi)
}

// SetLayerReferenceFrame replaces one spatial layer's VP9 encoder reference
// slot, matching [VP9Encoder.SetReferenceFrame].
func (e *VP9SpatialSVCEncoder) SetLayerReferenceFrame(layerID uint8,
	ref ReferenceFrame, src Image,
) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetReferenceFrame(ref, src)
}

// CopyLayerReferenceFrame copies one spatial layer's VP9 encoder reference slot
// into dst, matching [VP9Encoder.CopyReferenceFrame].
func (e *VP9SpatialSVCEncoder) CopyLayerReferenceFrame(layerID uint8,
	ref ReferenceFrame, dst *Image,
) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.CopyReferenceFrame(ref, dst)
}

// SetInterLayerPrediction enables or disables VP9 spatial inter-layer
// prediction for subsequent access units. When enabled, each enhancement layer
// references the just-coded lower spatial layer; when disabled, all spatial
// layers encode independently while keeping the same superframe/RTP structure.
func (e *VP9SpatialSVCEncoder) SetInterLayerPrediction(enabled bool) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	count := int(e.layerCount)
	if enabled {
		for i := 1; i < count; i++ {
			lower := e.layers[i-1]
			upper := e.layers[i]
			if lower == nil || upper == nil {
				return ErrClosed
			}
			if !validVP9SpatialSVCInterLayerScale(lower.opts.Width,
				lower.opts.Height, upper.opts.Width, upper.opts.Height) {
				return ErrInvalidConfig
			}
		}
	}
	for i := range count {
		layer := e.layers[i]
		if layer == nil {
			return ErrClosed
		}
		spatial := layer.opts.SpatialScalability
		spatial.InterLayerDependency = enabled && i > 0
		spatial.NotRefForUpperSpatialLayer = !enabled || i == count-1
		layer.opts.SpatialScalability = spatial
	}
	if e.temporalEnabled {
		vp9SetScalabilityStructureTemporalPattern(&e.scalabilityStructure,
			e.temporalMode, enabled)
	} else {
		vp9ClearScalabilityStructureTemporalPattern(&e.scalabilityStructure)
	}
	e.interLayerPrediction = enabled
	return nil
}

// SetTemporalScalability configures the same VP9 temporal-layer schedule on
// every spatial layer. Each layer derives its temporal bitrate split from that
// layer's TargetBitrateKbps.
func (e *VP9SpatialSVCEncoder) SetTemporalScalability(cfg TemporalScalabilityConfig) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	temporalMode := TemporalLayeringOneLayer
	temporalEnabled := cfg.Enabled
	if temporalEnabled {
		temporalMode = cfg.Mode
	}
	var next [VP9MaxSpatialLayers]temporalState
	for i := 0; i < int(e.layerCount); i++ {
		layer := e.layers[i]
		if layer == nil {
			return ErrClosed
		}
		if err := next[i].configure(cfg, layer.opts.TargetBitrateKbps); err != nil {
			return err
		}
	}
	nextScalabilityStructure := e.scalabilityStructure
	vp9SetScalabilityStructureTemporalPatternFromConfig(&nextScalabilityStructure,
		cfg, e.interLayerPrediction)
	// libvpx: vp9_svc_layercontext.c vp9_update_layer_context_change_config() —
	// number_temporal_layers tracks oxcf->ts_number_layers across change_config
	// calls. Mirror that here so the speed-features dispatcher sees the new
	// layer count after SetTemporalScalability runs.
	numberTemporalLayers := 1
	if cfg.Enabled {
		if pattern, ok := temporalLayeringPattern(cfg.Mode); ok &&
			pattern.Layers > 1 {
			numberTemporalLayers = pattern.Layers
		}
	}
	for i := 0; i < int(e.layerCount); i++ {
		layer := e.layers[i]
		layer.temporal = next[i]
		layer.opts.TemporalScalability = next[i].config
		if cfg.Enabled {
			layer.opts.ErrorResilient = true
		}
		layer.svc.NumberTemporalLayers = numberTemporalLayers
		layer.vp9ApplySpeedFeatures(layer.vp9DefaultSpeedFrameContext())
	}
	e.scalabilityStructure = nextScalabilityStructure
	e.temporalMode = temporalMode
	e.temporalEnabled = temporalEnabled
	return nil
}

// SetTemporalLayerID overrides the temporal layer ID for every spatial layer
// in subsequent access units. The override remains active until changed or
// SetTemporalScalability replaces the schedule.
func (e *VP9SpatialSVCEncoder) SetTemporalLayerID(layerID int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	var next [VP9MaxSpatialLayers]temporalState
	for i := 0; i < int(e.layerCount); i++ {
		layer := e.layers[i]
		if layer == nil {
			return ErrClosed
		}
		next[i] = layer.temporal
		if err := next[i].setLayerID(layerID); err != nil {
			return err
		}
	}
	for i := 0; i < int(e.layerCount); i++ {
		e.layers[i].temporal = next[i]
	}
	vp9ClearScalabilityStructureTemporalPattern(&e.scalabilityStructure)
	return nil
}

// ForceKeyFrame requests that the next access unit start with a key frame.
// Inter-layer SVC mirrors libvpx and forces only the base spatial layer; higher
// spatial layers remain inter frames so they refresh their own reference slots
// without overwriting lower-layer decoder state.
func (e *VP9SpatialSVCEncoder) ForceKeyFrame() {
	if e == nil || e.closed {
		return
	}
	if e.interLayerPrediction {
		e.layers[0].ForceKeyFrame()
		return
	}
	for i := 0; i < int(e.layerCount); i++ {
		e.layers[i].ForceKeyFrame()
	}
}

// IsKeyFrameNext reports whether any layer in the next access unit is due to
// emit a key frame.
func (e *VP9SpatialSVCEncoder) IsKeyFrameNext() bool {
	if e == nil || e.closed {
		return false
	}
	count := int(e.layerCount)
	if e.interLayerPrediction {
		count = 1
	}
	for i := 0; i < count; i++ {
		if e.layers[i].IsKeyFrameNext() {
			return true
		}
	}
	return false
}

// Close releases all per-layer encoders. Subsequent encode calls return
// [ErrClosed].
func (e *VP9SpatialSVCEncoder) Close() error {
	if e == nil {
		return ErrClosed
	}
	if e.closed {
		return nil
	}
	for i := 0; i < int(e.layerCount); i++ {
		if e.layers[i] != nil {
			_ = e.layers[i].Close()
		}
	}
	e.closed = true
	return nil
}

func (e *VP9Encoder) seedVP9InterLayerReference(lower *VP9Encoder) bool {
	if e == nil || lower == nil ||
		!lower.refValid[vp9LastRefSlot] || !lower.refFrames[vp9LastRefSlot].valid {
		return false
	}
	for slot := range e.refFrames {
		if !lower.refValid[slot] || !lower.refFrames[slot].valid {
			continue
		}
		e.refFrames[slot].store(lower.refFrames[slot].img)
		e.refWidth[slot] = lower.refWidth[slot]
		e.refHeight[slot] = lower.refHeight[slot]
		e.refValid[slot] = true
		e.refMap[slot] = lower.refMap[slot]
		if e.nextRefMapID < lower.refMap[slot] {
			e.nextRefMapID = lower.refMap[slot]
		}
		// Imported lower-layer buffers share the current display frame, so
		// vp9InterRefSignBias yields bias 0 (cur == refFrameIndex), matching
		// the old per-buffer refSignBias=0.
		e.refFrameIndex[slot] = e.frameIndex
	}
	lowerLayerID := e.opts.SpatialScalability.LayerID
	if lowerLayerID > 0 {
		lowerLayerID--
	}
	lowerLayerSlot := vp9SpatialSVCLayerReferenceSlot(lowerLayerID)
	if lower.refValid[lowerLayerSlot] && lower.refFrames[lowerLayerSlot].valid {
		e.refFrames[vp9LastRefSlot].store(lower.refFrames[lowerLayerSlot].img)
		e.refWidth[vp9LastRefSlot] = lower.refWidth[lowerLayerSlot]
		e.refHeight[vp9LastRefSlot] = lower.refHeight[lowerLayerSlot]
		e.refValid[vp9LastRefSlot] = true
		e.nextRefMapID++
		e.refMap[vp9LastRefSlot] = e.nextRefMapID
		e.refFrameIndex[vp9LastRefSlot] = e.frameIndex
	}
	return true
}

func vp9SpatialSVCInterLayerEncodeFlags(layerID uint8) EncodeFlags {
	flags := EncodeNoReferenceGolden | EncodeNoReferenceAltRef
	switch vp9SpatialSVCLayerReferenceSlot(layerID) {
	case vp9GoldenRefSlot:
		flags |= EncodeNoUpdateLast | EncodeNoUpdateAltRef
	case vp9AltRefSlot:
		flags |= EncodeNoUpdateLast | EncodeNoUpdateGolden
	}
	return flags
}

func vp9SpatialSVCLayerReferenceSlot(layerID uint8) int {
	switch layerID {
	case 0:
		return vp9LastRefSlot
	case 1:
		return vp9GoldenRefSlot
	default:
		return vp9AltRefSlot
	}
}

func validVP9SpatialSVCInterLayerScale(lowerW, lowerH, upperW, upperH int) bool {
	return 2*upperW >= lowerW && 2*upperH >= lowerH &&
		upperW <= 16*lowerW && upperH <= 16*lowerH
}

func appendVP9SpatialSVCSuperframeIndex(dst []byte, frameSizes *[VP9MaxSpatialLayers]int, count int) (int, error) {
	if frameSizes == nil || count < 1 || count > VP9MaxSpatialLayers {
		return 0, ErrInvalidConfig
	}
	return vp9bits.PackSuperframeIndexInto(dst, frameSizes[:count])
}
