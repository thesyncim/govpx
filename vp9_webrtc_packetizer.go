package govpx

import "errors"

// VP9WebRTCPacketizer owns the 15-bit VP9 RTP PictureID sequence for a
// WebRTC sender. It advances the PictureID after a frame/access unit has been
// successfully packetized, and also advances across encoder-dropped frames.
// Dropped frames emit no RTP payloads, but they still consume a VP9 temporal
// slot; leaving a PictureID gap keeps the RTP timeline aligned with the
// encoder timeline. Emitted payloads use VP9 flexible mode with explicit
// reference diffs so receivers do not have to infer dependencies from a stale
// GOF pattern.
type VP9WebRTCPacketizer struct {
	pictureID             uint16
	consumedDropPending   bool
	consumedDropSignature vp9WebRTCDroppedFrameSignature
	keyFrameRequired      bool
	references            vp9WebRTCReferenceTracker
	svcLayerCount         int
	haveSVCLayerCount     bool
}

// NewVP9WebRTCPacketizer returns a VP9 WebRTC packetizer whose first emitted
// frame/access unit will use initialPictureID.
func NewVP9WebRTCPacketizer(initialPictureID uint16) VP9WebRTCPacketizer {
	return VP9WebRTCPacketizer{
		pictureID: initialPictureID & VP9RTPPictureID15BitMask,
	}
}

// PictureID returns the PictureID that will be used for the next successfully
// packetized frame/access unit.
func (p *VP9WebRTCPacketizer) PictureID() uint16 {
	if p == nil {
		return 0
	}
	return p.pictureID
}

// NeedsKeyFrame reports whether a prior encoder-dropped VP9 temporal slot or
// unpacketizable inter-frame dependency requires the sender to force a keyframe
// before emitting more RTP payloads. Top temporal-layer drops can be
// represented as ordinary PictureID gaps, but dropped base/intermediate
// temporal layers and stale flexible-mode references can strand receiver
// dependency tracking on references that will never arrive.
func (p *VP9WebRTCPacketizer) NeedsKeyFrame() bool {
	return p != nil && p.keyFrameRequired
}

// MarkAccessUnitUnsent tells the packetizer that a VP9 frame/access unit
// produced by the encoder was intentionally withheld by the sender after it
// was encoded or packetized. This covers local pacing/backpressure drops where
// WebRTC will report no RTP packet loss but later inter frames may still
// reference the unsent PictureID. The next emitted VP9 RTP payload must be a
// TL0 recovery keyframe.
func (p *VP9WebRTCPacketizer) MarkAccessUnitUnsent() {
	if p == nil {
		return
	}
	p.consumedDropPending = false
	p.keyFrameRequired = true
}

// PacketizationSize returns the RTP payload count and payload-body bytes
// needed to packetize r with the packetizer's current PictureID. Size queries
// are non-mutating for emittable frames; encoder-dropped frames are consumed
// immediately because they need no follow-up Packetize call. sent is false
// when r is an encoder-dropped frame.
func (p *VP9WebRTCPacketizer) PacketizationSize(
	r VP9EncodeResult,
	mtu int,
) (packets int, payloadBytes int, sent bool, err error) {
	if p == nil {
		return 0, 0, false, ErrInvalidConfig
	}
	if r.Dropped {
		p.consumeDroppedFrame(r)
		return 0, 0, false, nil
	}
	if err = p.requireVP9RecoveryKey(r); err != nil {
		return 0, 0, false, err
	}
	p.consumedDropPending = false
	packets, payloadBytes, err = p.vp9WebRTCPacketizationSize(r, mtu)
	if err != nil {
		p.requireVP9RecoveryKeyAfterPacketizationError(err)
	}
	return packets, payloadBytes, err == nil, err
}

// PacketizeInto packetizes r into caller-owned RTP payload storage using the
// packetizer's current PictureID. It advances the PictureID only after a
// successful packetization, or after consuming an encoder-dropped temporal
// slot. sent is false for encoder-dropped frames and for errors; callers can
// retry the same frame with larger buffers after ErrBufferTooSmall.
func (p *VP9WebRTCPacketizer) PacketizeInto(
	r VP9EncodeResult,
	dst []RTPPayloadFragment,
	payloadBuf []byte,
	mtu int,
) (packets int, payloadBytes int, sent bool, err error) {
	if p == nil {
		return 0, 0, false, ErrInvalidConfig
	}
	if r.Dropped {
		p.consumeDroppedFrame(r)
		return 0, 0, false, nil
	}
	if err = p.requireVP9RecoveryKey(r); err != nil {
		return 0, 0, false, err
	}
	pictureID := p.pictureID
	packets, payloadBytes, err = p.vp9PacketizeWebRTCInto(r, dst,
		payloadBuf, mtu)
	if err != nil {
		if !errors.Is(err, ErrBufferTooSmall) {
			p.requireVP9RecoveryKeyAfterPacketizationError(err)
		}
		return packets, payloadBytes, false, err
	}
	p.consumedDropPending = false
	p.keyFrameRequired = false
	p.commitVP9WebRTCReferences(r, pictureID)
	p.advancePictureID()
	return packets, payloadBytes, true, nil
}

// Packetize packetizes r into allocated RTP payload bodies using the
// packetizer's current PictureID. sent is false when r is an encoder-dropped
// frame; the dropped temporal slot still advances PictureID.
func (p *VP9WebRTCPacketizer) Packetize(
	r VP9EncodeResult,
	mtu int,
) ([]RTPPayloadFragment, bool, error) {
	if p == nil {
		return nil, false, ErrInvalidConfig
	}
	if r.Dropped {
		p.consumeDroppedFrame(r)
		return nil, false, nil
	}
	if err := p.requireVP9RecoveryKey(r); err != nil {
		return nil, false, err
	}
	pictureID := p.pictureID
	payloads, err := p.vp9PacketizeWebRTC(r, mtu)
	if err != nil {
		p.requireVP9RecoveryKeyAfterPacketizationError(err)
		return nil, false, err
	}
	p.consumedDropPending = false
	p.keyFrameRequired = false
	p.commitVP9WebRTCReferences(r, pictureID)
	p.advancePictureID()
	return payloads, true, nil
}

// SpatialSVCWebRTCPacketizationSize returns the RTP payload count and
// payload-body bytes needed to packetize r with the packetizer's current
// PictureID.
func (p *VP9WebRTCPacketizer) SpatialSVCWebRTCPacketizationSize(
	r VP9SpatialSVCEncodeResult,
	mtu int,
) (int, int, error) {
	if p == nil {
		return 0, 0, ErrInvalidConfig
	}
	if err := p.requireVP9SpatialSVCRecoveryKey(r); err != nil {
		return 0, 0, err
	}
	p.consumedDropPending = false
	packets, payloadBytes, err := p.vp9SpatialSVCWebRTCPacketizationSize(r,
		mtu)
	if err != nil {
		p.requireVP9RecoveryKeyAfterPacketizationError(err)
	}
	return packets, payloadBytes, err
}

// PacketizeSpatialSVCWebRTCInto packetizes r into caller-owned RTP payload
// storage using the packetizer's current PictureID. It advances the PictureID
// only after successful packetization.
func (p *VP9WebRTCPacketizer) PacketizeSpatialSVCWebRTCInto(
	r VP9SpatialSVCEncodeResult,
	dst []RTPPayloadFragment,
	payloadBuf []byte,
	mtu int,
) (int, int, error) {
	if p == nil {
		return 0, 0, ErrInvalidConfig
	}
	if err := p.requireVP9SpatialSVCRecoveryKey(r); err != nil {
		return 0, 0, err
	}
	pictureID := p.pictureID
	packets, payloadBytes, err := p.vp9PacketizeSpatialSVCWebRTCInto(r,
		dst, payloadBuf, mtu)
	if err != nil {
		if !errors.Is(err, ErrBufferTooSmall) {
			p.requireVP9RecoveryKeyAfterPacketizationError(err)
		}
		return packets, payloadBytes, err
	}
	p.consumedDropPending = false
	p.keyFrameRequired = false
	p.commitVP9SpatialSVCWebRTCReferences(r, pictureID)
	p.commitVP9SpatialSVCLayerCount(r)
	p.advancePictureID()
	return packets, payloadBytes, nil
}

// PacketizeSpatialSVCWebRTC packetizes r into allocated RTP payload bodies
// using the packetizer's current PictureID.
func (p *VP9WebRTCPacketizer) PacketizeSpatialSVCWebRTC(
	r VP9SpatialSVCEncodeResult,
	mtu int,
) ([]RTPPayloadFragment, error) {
	if p == nil {
		return nil, ErrInvalidConfig
	}
	if err := p.requireVP9SpatialSVCRecoveryKey(r); err != nil {
		return nil, err
	}
	pictureID := p.pictureID
	payloads, err := p.vp9PacketizeSpatialSVCWebRTC(r, mtu)
	if err != nil {
		p.requireVP9RecoveryKeyAfterPacketizationError(err)
		return nil, err
	}
	p.consumedDropPending = false
	p.keyFrameRequired = false
	p.commitVP9SpatialSVCWebRTCReferences(r, pictureID)
	p.commitVP9SpatialSVCLayerCount(r)
	p.advancePictureID()
	return payloads, nil
}

func (p *VP9WebRTCPacketizer) advancePictureID() {
	p.pictureID = NextVP9RTPPictureID(p.pictureID)
}

var errVP9WebRTCRecoveryKeyRequired = errors.New("govpx: VP9 WebRTC recovery key required")

func vp9WebRTCRecoveryKeyRequiredError() error {
	return errors.Join(ErrInvalidConfig, errVP9WebRTCRecoveryKeyRequired)
}

func (p *VP9WebRTCPacketizer) requireVP9RecoveryKeyAfterPacketizationError(
	err error,
) {
	if p == nil || !errors.Is(err, errVP9WebRTCRecoveryKeyRequired) {
		return
	}
	p.keyFrameRequired = true
}

func (p *VP9WebRTCPacketizer) consumeDroppedFrame(r VP9EncodeResult) {
	signature := r.vp9WebRTCDroppedFrameSignature()
	if p.consumedDropPending && p.consumedDropSignature == signature {
		return
	}
	p.consumedDropPending = true
	p.consumedDropSignature = signature
	if vp9WebRTCDroppedFrameNeedsKeyFrame(r) {
		p.keyFrameRequired = true
	}
	p.advancePictureID()
}

func (p *VP9WebRTCPacketizer) requireVP9RecoveryKey(r VP9EncodeResult) error {
	if p == nil {
		return ErrInvalidConfig
	}
	if !p.keyFrameRequired || vp9WebRTCResultIsRecoveryKey(r) {
		return nil
	}
	return ErrInvalidConfig
}

func (p *VP9WebRTCPacketizer) requireVP9SpatialSVCRecoveryKey(
	r VP9SpatialSVCEncodeResult,
) error {
	if p == nil {
		return ErrInvalidConfig
	}
	count, err := r.vp9SpatialSVCLayerCount()
	if err != nil {
		return err
	}
	if p.haveSVCLayerCount && count != p.svcLayerCount {
		p.keyFrameRequired = true
	}
	if !p.keyFrameRequired || vp9WebRTCSpatialSVCResultIsRecoveryKey(r) {
		return nil
	}
	return ErrInvalidConfig
}

func (p *VP9WebRTCPacketizer) commitVP9SpatialSVCLayerCount(
	r VP9SpatialSVCEncodeResult,
) {
	if p == nil {
		return
	}
	count, err := r.vp9SpatialSVCLayerCount()
	if err != nil {
		return
	}
	p.svcLayerCount = count
	p.haveSVCLayerCount = true
}

func vp9WebRTCDroppedFrameNeedsKeyFrame(r VP9EncodeResult) bool {
	if r.TemporalLayerCount <= 1 {
		return true
	}
	return r.TemporalLayerID < r.TemporalLayerCount-1
}

func vp9WebRTCResultIsRecoveryKey(r VP9EncodeResult) bool {
	return r.KeyFrame && !r.vp9RTPInterPicturePredicted() &&
		r.TemporalLayerID == 0
}

func vp9WebRTCSpatialSVCResultIsRecoveryKey(
	r VP9SpatialSVCEncodeResult,
) bool {
	count, err := r.vp9SpatialSVCLayerCount()
	if err != nil || count == 0 {
		return false
	}
	base := r.Layers[0]
	if !vp9WebRTCResultIsRecoveryKey(base) ||
		!base.ScalabilityStructurePresent {
		return false
	}
	for i := 1; i < count; i++ {
		layer := r.Layers[i]
		if layer.Dropped || len(layer.Data) == 0 || !layer.ShowFrame ||
			layer.TemporalLayerID != 0 ||
			layer.vp9RTPInterPicturePredicted() {
			return false
		}
	}
	return true
}

type vp9WebRTCDroppedFrameSignature struct {
	frameIndex         int
	temporalLayerID    int
	temporalLayerCount int
	tl0PicIdx          uint8
}

func (r VP9EncodeResult) vp9WebRTCDroppedFrameSignature() vp9WebRTCDroppedFrameSignature {
	return vp9WebRTCDroppedFrameSignature{
		frameIndex:         r.vp9FrameIndex,
		temporalLayerID:    r.TemporalLayerID,
		temporalLayerCount: r.TemporalLayerCount,
		tl0PicIdx:          r.TL0PICIDX,
	}
}
