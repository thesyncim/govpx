package govpx

import (
	vp8rtp "github.com/thesyncim/govpx/internal/vp8/rtp"
	vpxrtp "github.com/thesyncim/govpx/internal/vpx/rtp"
)

// VP8RTPPayloadDescriptor describes the VP8 RTP payload descriptor from RFC
// 7741. It is the bytes after the RTP header and before the raw VP8 payload.
type VP8RTPPayloadDescriptor = vp8rtp.PayloadDescriptor

// ParseVP8RTPPayloadDescriptor parses the VP8 RTP payload descriptor at the
// front of packet and returns the descriptor plus the remaining raw VP8
// payload bytes.
func ParseVP8RTPPayloadDescriptor(packet []byte) (VP8RTPPayloadDescriptor, []byte, error) {
	return vp8rtp.ParsePayloadDescriptor(packet)
}

// VP8RTPPayloadSize returns the number of bytes needed to pack desc and the
// raw VP8 payload into one RTP payload body.
func VP8RTPPayloadSize(desc VP8RTPPayloadDescriptor, payload []byte) (int, error) {
	return vpxrtp.PayloadSize(desc, payload)
}

// PackVP8RTPPayloadInto writes desc followed by payload into dst and returns
// the RTP payload length. It does not write an RTP header.
func PackVP8RTPPayloadInto(dst []byte, desc VP8RTPPayloadDescriptor, payload []byte) (int, error) {
	return vpxrtp.PackPayloadInto(dst, desc, payload)
}

// PackVP8RTPPayload returns desc followed by payload as one RTP payload body.
// It does not include an RTP header.
func PackVP8RTPPayload(desc VP8RTPPayloadDescriptor, payload []byte) ([]byte, error) {
	return vpxrtp.PackPayload(desc, payload)
}

// VP8RTPFramePacketizationSize returns the number of RTP payload bodies and
// total payload-body bytes needed to packetize one raw VP8 frame at mtu bytes.
//
// mtu includes the VP8 RTP payload descriptor but excludes the RTP header.
// The packetizer manages StartOfPartition and always emits partition 0.
func VP8RTPFramePacketizationSize(desc VP8RTPPayloadDescriptor, frame []byte, mtu int) (int, int, error) {
	return vp8rtp.FramePacketizationSize(desc, frame, mtu)
}

// PacketizeVP8RTPFrameInto packetizes one raw VP8 frame into caller-owned
// RTP payload storage. dst receives packet metadata; payloadBuf receives the
// payload bodies. On [ErrBufferTooSmall], the returned packet and byte counts
// are the required capacities.
//
// The returned payload bodies do not include RTP headers. Marker is true only
// on the last payload body.
func PacketizeVP8RTPFrameInto(dst []RTPPayloadFragment, payloadBuf []byte,
	desc VP8RTPPayloadDescriptor, frame []byte, mtu int,
) (int, int, error) {
	return vp8rtp.PacketizeFrameInto(dst, payloadBuf, desc, frame, mtu)
}

// PacketizeVP8RTPFrame returns RTP payload bodies for one raw VP8 frame.
// Payloads do not include RTP headers; Marker is true only on the last body.
func PacketizeVP8RTPFrame(desc VP8RTPPayloadDescriptor, frame []byte, mtu int) ([]RTPPayloadFragment, error) {
	return vp8rtp.PacketizeFrame(desc, frame, mtu)
}

// VP8RTPFrameAssemblySize validates an ordered set of VP8 RTP payload bodies
// for one frame and returns the raw VP8 frame size.
//
// The caller owns RTP sequence-number validation, loss handling, and jitter
// buffering. Payloads must be in decode order and must include the marker bit
// value from each RTP header.
func VP8RTPFrameAssemblySize(payloads []RTPPayloadFragment) (int, error) {
	return vp8rtp.FrameAssemblySize(payloads)
}

// AssembleVP8RTPFrameInto writes the raw VP8 frame carried by payloads into
// dst and returns the frame length. On [ErrBufferTooSmall], the returned
// length is the required capacity.
func AssembleVP8RTPFrameInto(dst []byte, payloads []RTPPayloadFragment) (int, error) {
	return vp8rtp.AssembleFrameInto(dst, payloads)
}

// AssembleVP8RTPFrame returns the raw VP8 frame carried by an ordered set of
// RTP payload bodies.
func AssembleVP8RTPFrame(payloads []RTPPayloadFragment) ([]byte, error) {
	return vp8rtp.AssembleFrame(payloads)
}

// DecodeRTP assembles one ordered set of VP8 RTP payload bodies and decodes
// the resulting raw VP8 frame. The decoded image is returned by NextFrame when
// the frame is visible.
func (d *VP8Decoder) DecodeRTP(payloads []RTPPayloadFragment) error {
	return d.DecodeRTPWithPTS(payloads, 0)
}

// DecodeRTPWithPTS is DecodeRTP with an explicit presentation timestamp.
func (d *VP8Decoder) DecodeRTPWithPTS(payloads []RTPPayloadFragment, pts uint64) error {
	if d == nil || d.closed {
		return ErrClosed
	}
	frame, err := AssembleVP8RTPFrame(payloads)
	if err != nil {
		return err
	}
	return d.DecodeWithPTS(frame, pts)
}

// DecodeRTPInto assembles one ordered set of VP8 RTP payload bodies into
// frameBuf and decodes the resulting raw VP8 frame. On ErrBufferTooSmall, the
// returned integer is the required frameBuf capacity. The decoded image is
// returned by NextFrame when the frame is visible.
func (d *VP8Decoder) DecodeRTPInto(frameBuf []byte, payloads []RTPPayloadFragment) (int, error) {
	return d.DecodeRTPIntoWithPTS(frameBuf, payloads, 0)
}

// DecodeRTPIntoWithPTS is DecodeRTPInto with an explicit presentation
// timestamp.
func (d *VP8Decoder) DecodeRTPIntoWithPTS(frameBuf []byte, payloads []RTPPayloadFragment, pts uint64) (int, error) {
	if d == nil || d.closed {
		return 0, ErrClosed
	}
	n, err := AssembleVP8RTPFrameInto(frameBuf, payloads)
	if err != nil {
		return n, err
	}
	return n, d.DecodeWithPTS(frameBuf[:n], pts)
}
