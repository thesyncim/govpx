// Package rtp holds shared mechanical helpers for RTP payload assembly.
package rtp

import vpxerrors "github.com/thesyncim/govpx/internal/vpx/errors"

// PayloadFragment is one RTP payload body plus the RTP marker-bit value
// the caller should put in the RTP header for that body.
//
// Payload contains codec-specific payload-descriptor bytes followed by the
// codec payload fragment. It does not include an RTP header.
type PayloadFragment struct {
	Payload []byte
	Marker  bool
}

// FramePacketizationSize returns the packet count and total payload-body bytes
// for a frame split into RTP bodies with a fixed descriptor size.
func FramePacketizationSize(frameLen, descriptorSize, mtu int) (int, int, error) {
	if frameLen == 0 || descriptorSize <= 0 || mtu <= descriptorSize {
		return 0, 0, vpxerrors.ErrInvalidConfig
	}
	payloadBytesPerPacket := mtu - descriptorSize
	packets := (frameLen + payloadBytesPerPacket - 1) / payloadBytesPerPacket

	maxInt := int(^uint(0) >> 1)
	if packets > (maxInt-frameLen)/descriptorSize {
		return 0, 0, vpxerrors.ErrInvalidConfig
	}
	return packets, frameLen + packets*descriptorSize, nil
}

// AddPayloadSize adds n to total while rejecting negative sizes and int
// overflow.
func AddPayloadSize(total, n int) (int, error) {
	maxInt := int(^uint(0) >> 1)
	if n < 0 || total > maxInt-n {
		return 0, vpxerrors.ErrInvalidConfig
	}
	return total + n, nil
}
