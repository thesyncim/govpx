package govpx

import "image"

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

// RTPPacketizationSize returns the RTP payload fragment count and payload-body
// bytes needed to packetize every coded spatial layer in r at mtu bytes.
//
// mtu includes each VP9 RTP payload descriptor but excludes the RTP header.
// The access unit is packetized as one VP9 RTP frame per spatial layer, in
// ascending spatial-layer order. The base layer carries the parent scalability
// structure; enhancement layers carry only their layer indices.
func (r VP9SpatialSVCEncodeResult) RTPPacketizationSize(mtu int) (int, int, error) {
	count, err := r.vp9SpatialSVCLayerCount()
	if err != nil {
		return 0, 0, err
	}
	packets := 0
	payloadBytes := 0
	for layerID := 0; layerID < count; layerID++ {
		desc, frame, err := r.vp9SpatialSVCRTPFrame(layerID)
		if err != nil {
			return 0, 0, err
		}
		layerPackets, layerBytes, err := VP9RTPFramePacketizationSize(desc,
			frame, mtu)
		if err != nil {
			return 0, 0, err
		}
		packets, err = rtpAddPayloadSize(packets, layerPackets)
		if err != nil {
			return 0, 0, err
		}
		payloadBytes, err = rtpAddPayloadSize(payloadBytes, layerBytes)
		if err != nil {
			return 0, 0, err
		}
	}
	return packets, payloadBytes, nil
}

// PacketizeRTPInto packetizes every coded spatial layer in r into caller-owned
// RTP payload storage. dst receives fragment metadata and payloadBuf receives
// the payload bodies. On [ErrBufferTooSmall], the returned fragment and byte
// counts are the required capacities.
//
// Payload bodies do not include RTP headers. Fragments are written in
// spatial-layer order, and each layer frame uses its own VP9 RTP start/end and
// marker semantics.
func (r VP9SpatialSVCEncodeResult) PacketizeRTPInto(dst []RTPPayloadFragment,
	payloadBuf []byte, mtu int,
) (int, int, error) {
	needPackets, needBytes, err := r.RTPPacketizationSize(mtu)
	if err != nil {
		return 0, 0, err
	}
	if len(dst) < needPackets || len(payloadBuf) < needBytes {
		return needPackets, needBytes, ErrBufferTooSmall
	}
	count := int(r.LayerCount)
	packetOff := 0
	byteOff := 0
	for layerID := 0; layerID < count; layerID++ {
		desc, frame, err := r.vp9SpatialSVCRTPFrame(layerID)
		if err != nil {
			return 0, 0, err
		}
		layerPackets, layerBytes, err := VP9RTPFramePacketizationSize(desc,
			frame, mtu)
		if err != nil {
			return 0, 0, err
		}
		writtenPackets, writtenBytes, err := PacketizeVP9RTPFrameInto(
			dst[packetOff:packetOff+layerPackets],
			payloadBuf[byteOff:byteOff+layerBytes], desc, frame, mtu)
		if err != nil {
			return 0, 0, err
		}
		packetOff += writtenPackets
		byteOff += writtenBytes
	}
	return needPackets, needBytes, nil
}

// PacketizeRTP returns RTP payload bodies for every coded spatial layer in r.
// For a zero-allocation packetization path, use [VP9SpatialSVCEncodeResult.PacketizeRTPInto].
func (r VP9SpatialSVCEncodeResult) PacketizeRTP(mtu int) ([]RTPPayloadFragment, error) {
	packets, payloadBytes, err := r.RTPPacketizationSize(mtu)
	if err != nil {
		return nil, err
	}
	out := make([]RTPPayloadFragment, packets)
	payloadBuf := make([]byte, payloadBytes)
	n, _, err := r.PacketizeRTPInto(out, payloadBuf, mtu)
	if err != nil {
		return nil, err
	}
	return out[:n], nil
}

func (r VP9SpatialSVCEncodeResult) vp9SpatialSVCLayerCount() (int, error) {
	if r.LayerCount == 0 || r.LayerCount > VP9MaxSpatialLayers {
		return 0, ErrInvalidConfig
	}
	return int(r.LayerCount), nil
}

func (r VP9SpatialSVCEncodeResult) vp9SpatialSVCRTPFrame(
	layerID int,
) (VP9RTPPayloadDescriptor, []byte, error) {
	count, err := r.vp9SpatialSVCLayerCount()
	if err != nil {
		return VP9RTPPayloadDescriptor{}, nil, err
	}
	if layerID < 0 || layerID >= count {
		return VP9RTPPayloadDescriptor{}, nil, ErrInvalidConfig
	}
	layer := r.Layers[layerID]
	if layer.Dropped || len(layer.Data) == 0 ||
		layer.SpatialLayerID != uint8(layerID) ||
		layer.SpatialLayerCount != r.LayerCount {
		return VP9RTPPayloadDescriptor{}, nil, ErrInvalidConfig
	}
	desc := layer.RTPPayloadDescriptor()
	if layerID == 0 {
		if !r.ScalabilityStructure.isZero() {
			desc.ScalabilityStructurePresent = true
			desc.ScalabilityStructure = r.ScalabilityStructure
		}
	} else {
		desc.ScalabilityStructurePresent = false
		desc.ScalabilityStructure = VP9RTPScalabilityStructure{}
	}
	return desc, layer.Data, nil
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
	for i := 0; i < count; i++ {
		layer := opts.Layers[i]
		if layer.SpatialScalability.Enabled ||
			layer.LookaheadFrames != 0 || layer.AutoAltRef ||
			layer.DropFrameAllowed {
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
			heights, count, temporalMode, temporalEnabled),
	}
	for i := 0; i < count; i++ {
		layerOpts := opts.Layers[i]
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
) VP9RTPScalabilityStructure {
	ss := VP9RTPScalabilityStructure{
		SpatialLayerCount: layerCount,
		ResolutionPresent: true,
		Width:             widths,
		Height:            heights,
	}
	if temporalEnabled {
		vp9SetScalabilityStructureTemporalPattern(&ss, temporalMode)
	}
	return ss
}

func vp9SetScalabilityStructureTemporalPatternFromConfig(
	ss *VP9RTPScalabilityStructure,
	cfg TemporalScalabilityConfig,
) {
	if cfg.Enabled {
		vp9SetScalabilityStructureTemporalPattern(ss, cfg.Mode)
		return
	}
	vp9ClearScalabilityStructureTemporalPattern(ss)
}

func vp9SetScalabilityStructureTemporalPattern(
	ss *VP9RTPScalabilityStructure,
	mode TemporalLayeringMode,
) {
	groups := vp9TemporalScalabilityPictureGroups(mode)
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

func vp9TemporalScalabilityPictureGroups(mode TemporalLayeringMode) []VP9RTPPictureGroup {
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
	count := int(e.layerCount)
	if len(srcs) != count {
		return VP9SpatialSVCEncodeResult{}, ErrInvalidConfig
	}
	for i := 0; i < count; i++ {
		if err := e.layers[i].validateVP9EncoderSource(srcs[i]); err != nil {
			return VP9SpatialSVCEncodeResult{}, err
		}
	}

	maxIndexSize := 2 + count*4
	if len(dst) < count*vp9MinEncodeIntoBuffer+maxIndexSize {
		return VP9SpatialSVCEncodeResult{}, ErrBufferTooSmall
	}

	var result VP9SpatialSVCEncodeResult
	var frameSizes [VP9MaxSpatialLayers]int
	offset := 0
	encodeLimit := len(dst) - maxIndexSize
	for i := 0; i < count; i++ {
		if encodeLimit-offset < vp9MinEncodeIntoBuffer {
			return VP9SpatialSVCEncodeResult{}, ErrBufferTooSmall
		}
		layer := e.layers[i]
		var layerResult VP9EncodeResult
		var err error
		if i > 0 && e.interLayerPrediction {
			if !layer.seedVP9InterLayerReference(e.layers[i-1]) {
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
		if err != nil {
			return VP9SpatialSVCEncodeResult{}, err
		}
		if layerResult.Dropped || len(layerResult.Data) == 0 {
			return VP9SpatialSVCEncodeResult{}, ErrInvalidConfig
		}
		if i == 0 && layerResult.ScalabilityStructurePresent {
			layerResult.SpatialScalabilityStructure = e.scalabilityStructure
		}
		size := len(layerResult.Data)
		frameSizes[i] = size
		layerResult.Data = dst[offset : offset+size]
		result.Layers[i] = layerResult
		offset += size
	}

	indexSize, err := appendVP9SpatialSVCSuperframeIndex(dst[offset:], &frameSizes, count)
	if err != nil {
		return VP9SpatialSVCEncodeResult{}, err
	}
	result.Data = dst[:offset+indexSize]
	result.SizeBytes = len(result.Data)
	result.LayerCount = e.layerCount
	result.InterLayerPrediction = e.interLayerPrediction
	result.ScalabilityStructure = e.scalabilityStructure
	return result, nil
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
// target update, matching [VP9Encoder.SetRealtimeTarget]. Size-changing
// updates are rejected while the layer is owned by the SVC encoder.
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

// SetLayerFrameDropAllowed enables or disables one spatial layer's VP9 CBR
// frame dropping, matching [VP9Encoder.SetFrameDropAllowed].
func (e *VP9SpatialSVCEncoder) SetLayerFrameDropAllowed(layerID uint8, enabled bool) error {
	layer, err := e.layerEncoder(layerID)
	if err != nil {
		return err
	}
	return layer.SetFrameDropAllowed(enabled)
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

// SetTemporalScalability configures the same VP9 temporal-layer schedule on
// every spatial layer. Each layer derives its temporal bitrate split from that
// layer's TargetBitrateKbps.
func (e *VP9SpatialSVCEncoder) SetTemporalScalability(cfg TemporalScalabilityConfig) error {
	if e == nil || e.closed {
		return ErrClosed
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
	vp9SetScalabilityStructureTemporalPatternFromConfig(&nextScalabilityStructure, cfg)
	for i := 0; i < int(e.layerCount); i++ {
		layer := e.layers[i]
		layer.temporal = next[i]
		layer.opts.TemporalScalability = next[i].config
	}
	e.scalabilityStructure = nextScalabilityStructure
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

// ForceKeyFrame requests that the next access unit encode every spatial layer
// as a key frame.
func (e *VP9SpatialSVCEncoder) ForceKeyFrame() {
	if e == nil || e.closed {
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
	for i := 0; i < int(e.layerCount); i++ {
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
		e.refSignBias[slot] = 0
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
		e.refSignBias[vp9LastRefSlot] = 0
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
	maxSize := 0
	for i := 0; i < count; i++ {
		size := frameSizes[i]
		if size <= 0 || uint64(size) > uint64(^uint32(0)) {
			return 0, ErrInvalidConfig
		}
		if size > maxSize {
			maxSize = size
		}
	}
	sizeBytes := vp9SuperframeSizeBytes(maxSize)
	indexSize := 2 + count*sizeBytes
	if len(dst) < indexSize {
		return indexSize, ErrBufferTooSmall
	}
	marker := vp9SuperframeMarker(count, sizeBytes)
	dst[0] = marker
	off := 1
	for i := 0; i < count; i++ {
		size := frameSizes[i]
		for j := 0; j < sizeBytes; j++ {
			dst[off+j] = byte(size >> (8 * j))
		}
		off += sizeBytes
	}
	dst[off] = marker
	return indexSize, nil
}
