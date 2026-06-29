package govpx

import (
	vp9rtp "github.com/thesyncim/govpx/internal/vp9/rtp"
	vpxrtp "github.com/thesyncim/govpx/internal/vpx/rtp"
)

const (
	// VP9RTPMaxReferenceIndices is the maximum number of reference-index
	// entries in a VP9 RTP payload descriptor or scalability-structure entry.
	VP9RTPMaxReferenceIndices = vp9rtp.MaxReferenceIndices
	// VP9RTPMaxSpatialLayers is the maximum number of spatial layers described
	// by a VP9 RTP scalability structure.
	VP9RTPMaxSpatialLayers = vp9rtp.MaxSpatialLayers
)

// VP9RTPPayloadDescriptor describes the VP9 RTP payload descriptor from RFC
// 9628. It is the bytes after the RTP header and before the raw VP9 payload.
type VP9RTPPayloadDescriptor = vp9rtp.PayloadDescriptor

// VP9RTPScalabilityStructure describes the optional VP9 RTP scalability
// structure. SpatialLayerCount defaults to one when marshaling.
type VP9RTPScalabilityStructure = vp9rtp.ScalabilityStructure

// VP9RTPPictureGroup describes one picture-group entry in a VP9 RTP
// scalability structure.
type VP9RTPPictureGroup = vp9rtp.PictureGroup

// ParseVP9RTPPayloadDescriptor parses the VP9 RTP payload descriptor at the
// front of packet and returns the descriptor plus the remaining raw VP9
// payload bytes.
func ParseVP9RTPPayloadDescriptor(packet []byte) (VP9RTPPayloadDescriptor, []byte, error) {
	return vp9rtp.ParsePayloadDescriptor(packet)
}

// VP9RTPPayloadSize returns the number of bytes needed to pack desc and the
// raw VP9 payload into one RTP payload body.
func VP9RTPPayloadSize(desc VP9RTPPayloadDescriptor, payload []byte) (int, error) {
	return vpxrtp.PayloadSize(desc, payload)
}

// PackVP9RTPPayloadInto writes desc followed by payload into dst and returns
// the RTP payload length. It does not write an RTP header.
func PackVP9RTPPayloadInto(dst []byte, desc VP9RTPPayloadDescriptor, payload []byte) (int, error) {
	return vpxrtp.PackPayloadInto(dst, desc, payload)
}

// PackVP9RTPPayload returns desc followed by payload as one RTP payload body.
// It does not include an RTP header.
func PackVP9RTPPayload(desc VP9RTPPayloadDescriptor, payload []byte) ([]byte, error) {
	return vpxrtp.PackPayload(desc, payload)
}

// VP9RTPFramePacketizationSize returns the number of RTP payload bodies and
// total payload-body bytes needed to packetize one raw VP9 Profile 0 frame at
// mtu bytes.
//
// mtu includes the VP9 RTP payload descriptor but excludes the RTP header.
// This helper packetizes one VP9 frame per call. Layer indices, flexible-mode
// references, and scalability structures are carried from desc. Scalability
// structure data is emitted on the first fragment only; later fragments carry
// the same frame descriptor without repeating that optional metadata.
func VP9RTPFramePacketizationSize(desc VP9RTPPayloadDescriptor, frame []byte, mtu int) (int, int, error) {
	return vp9rtp.FramePacketizationSize(desc, frame, mtu)
}

// PacketizeVP9RTPFrameInto packetizes one raw VP9 frame into caller-owned
// RTP payload storage. dst receives packet metadata; payloadBuf receives the
// payload bodies. On [ErrBufferTooSmall], the returned packet and byte counts
// are the required capacities.
//
// The returned payload bodies do not include RTP headers. Marker is true only
// on the last payload body. If desc contains VP9 scalability structure data,
// only the first payload body carries it.
func PacketizeVP9RTPFrameInto(dst []RTPPayloadFragment, payloadBuf []byte,
	desc VP9RTPPayloadDescriptor, frame []byte, mtu int,
) (int, int, error) {
	return vp9rtp.PacketizeFrameInto(dst, payloadBuf, desc, frame, mtu)
}

// PacketizeVP9RTPFrame returns RTP payload bodies for one raw VP9 frame.
// Payloads do not include RTP headers; Marker is true only on the last body.
func PacketizeVP9RTPFrame(desc VP9RTPPayloadDescriptor, frame []byte, mtu int) ([]RTPPayloadFragment, error) {
	return vp9rtp.PacketizeFrame(desc, frame, mtu)
}

// VP9RTPFrameAssemblySize validates an ordered set of VP9 RTP payload bodies
// for one frame and returns the raw VP9 frame size.
//
// The caller owns RTP sequence-number validation, loss handling, and jitter
// buffering. Payloads must be in decode order and must include the marker bit
// value from each RTP header. For spatial scalability, lower spatial-layer
// frames end with E=true and Marker=false; early Marker bits are rejected, but
// the final payload body does not need Marker=true.
func VP9RTPFrameAssemblySize(payloads []RTPPayloadFragment) (int, error) {
	return vp9rtp.FrameAssemblySize(payloads)
}

// AssembleVP9RTPFrameInto writes the raw VP9 frame carried by payloads into
// dst and returns the frame length. On [ErrBufferTooSmall], the returned
// length is the required capacity.
func AssembleVP9RTPFrameInto(dst []byte, payloads []RTPPayloadFragment) (int, error) {
	return vp9rtp.AssembleFrameInto(dst, payloads)
}

// AssembleVP9RTPFrame returns the raw VP9 frame carried by an ordered set of
// RTP payload bodies.
func AssembleVP9RTPFrame(payloads []RTPPayloadFragment) ([]byte, error) {
	return vp9rtp.AssembleFrame(payloads)
}

// DecodeRTP assembles one ordered set of VP9 RTP payload bodies and decodes
// the resulting raw VP9 frame. The decoded image is returned by NextFrame when
// the frame is visible.
func (d *VP9Decoder) DecodeRTP(payloads []RTPPayloadFragment) error {
	return d.DecodeRTPWithPTS(payloads, 0)
}

// DecodeRTPWithPTS is DecodeRTP with an explicit presentation timestamp.
func (d *VP9Decoder) DecodeRTPWithPTS(payloads []RTPPayloadFragment, pts uint64) error {
	if d == nil || d.closed {
		return ErrClosed
	}
	frame, err := AssembleVP9RTPFrame(payloads)
	if err != nil {
		return err
	}
	return d.DecodeWithPTS(frame, pts)
}

// DecodeRTPInto assembles one ordered set of VP9 RTP payload bodies into
// frameBuf and decodes the resulting raw VP9 frame. On ErrBufferTooSmall, the
// returned integer is the required frameBuf capacity. The decoded image is
// returned by NextFrame when the frame is visible.
func (d *VP9Decoder) DecodeRTPInto(frameBuf []byte, payloads []RTPPayloadFragment) (int, error) {
	return d.DecodeRTPIntoWithPTS(frameBuf, payloads, 0)
}

// DecodeRTPIntoWithPTS is DecodeRTPInto with an explicit presentation
// timestamp.
func (d *VP9Decoder) DecodeRTPIntoWithPTS(frameBuf []byte, payloads []RTPPayloadFragment, pts uint64) (int, error) {
	if d == nil || d.closed {
		return 0, ErrClosed
	}
	n, err := AssembleVP9RTPFrameInto(frameBuf, payloads)
	if err != nil {
		return n, err
	}
	return n, d.DecodeWithPTS(frameBuf[:n], pts)
}
