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
	ScalabilityStructure VP9RTPScalabilityStructure
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
	svc := &VP9SpatialSVCEncoder{
		layerCount:           opts.LayerCount,
		interLayerPrediction: opts.InterLayerPrediction,
		scalabilityStructure: VP9RTPScalabilityStructure{
			SpatialLayerCount: count,
			ResolutionPresent: true,
			Width:             widths,
			Height:            heights,
		},
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
		svc.layers[i] = layer
	}
	return svc, nil
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
				EncodeNoReferenceGolden|EncodeNoReferenceAltRef)
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
// change its spatial scalability configuration; close the parent SVC encoder.
func (e *VP9SpatialSVCEncoder) LayerEncoder(layerID uint8) (*VP9Encoder, error) {
	if e == nil || e.closed {
		return nil, ErrClosed
	}
	if layerID >= e.layerCount {
		return nil, ErrInvalidConfig
	}
	return e.layers[layerID], nil
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
	return true
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
